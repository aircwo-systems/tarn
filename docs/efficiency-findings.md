# Resource efficiency findings

Status: open items from the September 2026 resource audit
Scope: idle CPU and wakeups, containers left running, and memory growth on a developer laptop

The audit covered three areas: containers and processes, background loops and pollers, and in-memory data. Every item below was traced through the code. Items marked **verified** were also checked by hand against the source. Line numbers are as of commit `fb961df`, so treat them as a pointer to where to look rather than an exact location.

See also `docs/aws-parity-and-performance-audit.md`, which lists older items (P0/P1) that are still referenced here.

## Already fixed

| Commit | Fix |
|---|---|
| `745b205` | Lambda warm pools are keyed by account; the idle reaper untracks a container before removing it; containers are force-removed in parallel |
| `5e47cc3` | Settings flags stale accounts; archive stops an account's services and containers, restore and delete are available |
| `a053c4d` | SQS long polls wait on a per-queue signal; event source pollers long-poll 20s; empty receives no longer mark the store dirty; the reaper compacts in place |
| `9521eed` | EventBridge scheduler wakes once a minute; ECS health checks save only on change and slow to 5s when stable; Step Functions, ECS dependency and Lambda concurrency waits back off |
| `777eb65` | SIGHUP triggers clean shutdown; ECS task containers get a capped Docker log; db-proxy span reports time out and accept errors back off |
| `2e5111e` | The trace store prunes every 200 traces, not only at startup |
| `c736a81` | Persistence flushers sleep until a store is marked dirty |
| `32efac4` | Log group buffers grow on demand; pruning only rebuilds groups it removed events from |
| `46a99ce` | Infrastructure probes pause when no dashboard has read results for 2 minutes |
| `99328b8` | The in-container secrets proxy only runs for functions using the Parameters and Secrets extension, unless `--expose-secrets-proxy` is set |
| `fb961df` | The ECS reconcile loop idles with no services, backs off while services are steady, and wakes on service changes and task exits |
| `38ffd64` | Streams cap retained records, evict the oldest on append, bisect the checkpoint and clone only the returned window |
| `510da69` | Step Functions caps executions per state machine, deletes them with the machine, and filters before copying |
| `bd216fa` | Lambda burst containers get a short keep-alive; the startup sweep reclaims orphans from any port |
| `23e9db6` | DynamoDB stream mappings wait on a per-stream signal instead of a one-second poll |
| `453b49f` | ECS recovery resumes recovered containers' log pumps and starts their health pollers |
| `a42941e` | The volume-on-stop test no longer races a 2s log drain against a 2s deadline |
| `d187284` | A structured log line's own level is stored at ingestion, so ERROR filters see it |
| `65b0675` | Stopping an event source poller waits for its poll and drain loops to exit |
| `1c90e39` | Log stream names per group are capped, evicting the least recently written |
| `752d26d` | `make ui-build` leaves no committable `200.html`; a partial build skips in CI |

## Fixed: medium, one focused change each

Item 5 is the only one still open.

### 1. DynamoDB streams copy the whole retained stream every second (verified)

`internal/dynamodb/store.go` `StreamBatch` `GetRecords` `pruneStreamLocked` `appendStreamRecordLocked`

Fixed. `streamWindow` now bisects the checkpoint and deep-copies only the returned
window, outside the exclusive lock; `maxStreamRecords` (10 000) caps retention with
the oldest evicted on append; `pruneStreamLocked` clears the tail it compacts away so
pruned records stop being reachable through the backing array. A checkpoint behind the
oldest retained record resumes there rather than failing, matching how a real shard
iterator moves past records that aged out.

`BenchmarkStreamBatchIdlePoll` (10 000 retained records, checkpoint at the head, so the
poll finds nothing) went from 119 761 009 ns and 99 MB allocated per poll to 149 ns and
zero allocations — about 99 MB of garbage a second per idle mapping, gone.

Still open: nothing. `Store` now keeps one wake channel per stream that a reader
is parked on, closed and dropped by `appendStreamRecordLocked` and by the paths
that retire a stream (table delete, stream disable). `StreamBatchUntil` is the
wait-capable read the event source poller uses, bounded at
`streamLongPollSeconds` (20s) with the poller's own stop channel as the cancel,
so a mapping sleeps until a write instead of re-reading the stream every second.
The signal collapses a burst of writes into one wake, and every reader on that
stream is released. A one-shot `StreamBatch` creates no channel, so nothing
accumulates for reads that are not going to wait.

### 2. Step Functions executions are kept forever and cloned on every dashboard poll

`internal/stepfunctions/store.go` (executions map, `ListExecutions`), `internal/stepfunctions/service.go` `ListExecutions`, `internal/api/admin/handler.go` Overview

Fixed. `maxExecutionsPerMachine` (100) bounds retention, `evictExecutionsLocked` drops the
oldest finished executions first and never drops a running one, `DeleteStateMachine` takes
its executions with it, and `Init` re-caps snapshots written before the cap existed.
`ListExecutions` now filters and truncates to a limit before copying anything, and copies
after the lock is released. Overview asks for the 50 it surfaces instead of cloning every
execution of every machine and discarding most.

The history-free listing the original note suggested would have broken the dashboard:
`state-machine-detail.svelte` renders `execution.events` through `execution-history.svelte`,
so the events are load-bearing. Filtering before the copy was the fix that mattered.
`BenchmarkListExecutionsPerMachine` (8 machines, 20 history events each, 50 requested) went
from 198 536 ns and 1.3 MB allocated to 18 438 ns and 82 KB.

### 3. Warm containers linger after bursts, and other-port orphans are never swept

`internal/config/config.go` (`LambdaKeepAliveMS` 600000, `LambdaMaxConcurrency` 10), `internal/engine/pool.go`, `internal/engine/container.go` `SweepOrphanedLambdaContainers`

- A burst can start up to 10 containers for one function. `AcquireIdle` always picks the first idle one, so the rest sit idle for the full 10 minutes. JVM functions can hold several GB this way.
- The startup sweep only matches `tarn.port=<current port>`. After an unclean exit, restarting on another port leaves the old containers running indefinitely.

Fixed. The pool now has two windows: `LambdaOverflowKeepAliveMS` (30s,
`TARN_LAMBDA_OVERFLOW_KEEPALIVE_MS`) applies to every container after the first
in a function's pool, and `LambdaKeepAliveMS` to the first. Pools are append-
ordered by creation and the reaper's filter preserves that order, so index 0 is
the baseline and the rest are burst containers. The reaper's 30s tick puts actual
eviction at 30 to 60s after a burst ends, instead of 10 to 10.5 minutes. An
overflow window that is unset or not shorter than the baseline falls back to it.

`SweepOrphanedLambdaContainers` now lists every container labelled
`tarn.managed=lambda` and decides ownership per container from its `tarn.port`,
rather than filtering on the current port. A container is removed only once
nothing answers on its port; our own port is never probed, since the sweep runs
before the API server binds. A container with a missing or unreadable port label
is removed and logged, since no Tarn can be shown to be holding it. This is what
finds the orphans a restart on a different port used to leave running.

The trade-off is deliberate: a function running at steady concurrency above one
now cold-starts its extra environments every 30s of quiet, instead of holding
them for 10 minutes. The image stays cached, so that is a container start, not a
pull. Set `TARN_LAMBDA_OVERFLOW_KEEPALIVE_MS` to the full keep-alive to opt out.

### 4. ECS recovery replays whole logs and loses health checks

`internal/ecs/runner.go` `recoverTask`, `internal/engine/task.go` `FollowContainerLogs`

- Recovered tasks follow logs with no `Since` or `Tail`, so every restart re-ingests each container's full log, duplicating events.
- `spawnHealthPoller` is not called on the recovery path, so recovered containers with health checks are never polled.

Fixed. `FollowContainerLogs` takes a `since`, and recovery passes the runner's
start time: a recovered container predates this Tarn, so everything it logged
before the runner existed was already ingested and does not need reading twice.
A container the runner launched itself still passes a zero `since`, which makes
the Docker request identical to the old one.

`recoverTask` now starts a health poller for any recovered container whose
task definition declares a health check, mirroring the launch path. A recovered
container could otherwise go UNHEALTHY with nothing watching it, and its task
would sit RUNNING until the container exited. Containers on a task already
desired-stopped are skipped, since the recovery path stops them directly and
records the outcome itself. The health check is read off the resolved task
definition (via `recoveredHealthCheck`, alongside `recoveredLogGroup`), so a
container that never had one still costs no inspect per tick.

### 5. Any 12-digit access key creates a full account that is never released

`internal/api/server.go` `HandlerRegistry.get`, `internal/cli/server.go` `initAccountBundle` and `preInitPersistedAccounts`

- Each account bundle holds every store, 16 async invoke workers and background loops, and creates `accounts/<id>` on disk. It is then re-initialised on every start.
- Archiving (`5e47cc3`) now lets users clean these up by hand, but nothing happens automatically.

Fix: create the account directory on first write rather than first request, skip pre-initialising empty account directories, and consider unloading accounts with no resources and no traffic after an idle period.

## Open: larger, needs design

### 6. S3 multipart uploads are buffered in RAM (verified)

`internal/api/s3/handler.go` (`uploads` map, `uploadPart` `io.ReadAll`, complete)

Every part is held in memory until Complete or Abort, abandoned uploads are never freed, and Complete copies all parts into one buffer (about 2x the object size at peak). Plain PutObject and GetObject already stream through disk.

Fix: write parts to temp files under the bucket directory, stream-concatenate on Complete, and expire abandoned uploads after an idle TTL.

### 7. The dashboard overview recomputes everything on every poll

`internal/api/admin/handler.go` Overview and its helpers

Each 5-second poll:

- scans every S3 bucket's object metadata (`ListObjects` with a 1 000 000 limit to show 12 entries), plus count and size passes;
- re-reads and re-validates every Lambda's `events/*.json` fixtures for each gateway route;
- queries 50 traces, describes every ECS task definition revision (including INACTIVE), and parses every function config;
- encodes everything with no cache and no "not modified" response. Multiple tabs or an MCP client multiply the cost.

Fix: keep per-bucket counts and a recent-objects list updated on write; cache event examples by code SHA; share a 1 to 2 second cached response across pollers with a version counter for 304s; load task definition revisions on demand.

### 8. Log live tail copies and sorts every event

`internal/logs/store.go` `GetAllLogEvents`, `paginateEvents` (P1-4 in the older audit)

Every 2-second tail builds a new tagged string per matching event across all groups, sorts them all, builds a reversed copy for descending order, and only then applies the limit, all under the store read lock.

Fix: k-way merge the per-group rings from the requested end and stop at limit plus cursor; tag only returned events.

### 9. A cancelled invoke returns a container to the pool while its handler may still run

`internal/engine/invoker.go` (around line 95), `internal/lambda/service.go` release path

On client cancel the invoke returns a synthetic timeout and the container is released as idle. If the handler is still running, the next invoke goes to a busy runtime.

Fix: when the context ends before the function's own timeout, remove the container instead of releasing it. First confirm how the Lambda runtime interface emulator behaves on an abandoned request.

## Fixed: small

These were one focused change each, on top of the numbered items above.

- **Log stream map grows per container.** A Lambda stream is named after its
  container, so every cold start added an entry nothing removed except
  `ClearGroup`: a function that cold-started a thousand times held a thousand
  names whose events the ring had long since evicted. `maxStreamsPerGroup`
  (100) now caps them, evicting the least recently written on append. The
  group's event buffer, not this map, decides what is still readable, so
  dropping the oldest names loses nothing.
- **Event source `Stop` didn't wait.** Closing `done` was not enough, so a
  poller replaced by an update or a restart could still be inside a receive or
  a drain loop while its replacement started — two generations at once, up to
  twice the configured concurrency, both driving the same mapping. The poller
  now tracks its run and drain loops in a `WaitGroup` and `stop` waits for them
  (`pollerStopGrace`, 2s). Every wait in a poller takes `done` as its cancel, so
  a parked one exits at once; the grace is only ever spent by an invoke already
  in flight, which is then left to finish rather than holding up the API call
  that stopped it.
- **Raw log levels for Node JSON lines.** `classifyLambdaLogEvent` now takes a
  structured line's own level, reusing the `parseJSONObject`/`structuredLevel`
  pair the summary already used — so the events view and `level=ERROR` filters
  agree with it. Note most JSON lines happened to agree anyway, because the
  level name is itself a keyword `DetectLevel` finds in the text; what actually
  differed was a numeric pino level with a neutral message, and a line whose
  explicit level outranked a keyword in its own text.
- **Placeholder UI shell got overwritten.** The original suggestion — skip
  `200.html` in the `make ui-build` copy — is wrong: `adapter-static` is
  configured with `fallback: "200.html"`, so the built file is the dashboard's
  real SPA shell and skipping it would break the embedded UI. The build instead
  marks it `skip-worktree`, so a local build never leaves it as a pending
  change, and `TestRootDispatchServesReferencedUIAsset` now skips (rather than
  fails) when the shell references an asset that is not embedded — checking the
  embedded FS, so a real "asset present but served as the shell" regression
  still fails.
- **Flaky test.** `TestRunnerRemovesTaskScopedVolumeOnStopButNotShared` was not
  a tight deadline so much as a missing fixture: a task only reaches STOPPED
  after its log pump has been given `logDrainGracePeriod`, which defaults to
  2s — the same 2s the test's own wait allowed. It was racing a full drain
  against its own timeout, and every sibling test already shrank that grace
  except this one. 60 consecutive `-race` runs now pass.

- **One timer per traced invoke.** `internal/trace/collector.go` `Finish` schedules a 60 ms `AfterFunc` per invoke. There is no idle cost, only churn under load. *Not fixed:* a shared timer was written and reverted — three benchmark shapes (setup only, burst-and-drain, zero window) all showed identical allocations and no clear time difference, so it was not worth ~40 lines of rearm logic in a concurrency primitive. Revisit only if profiling ever shows the timer heap under load.

## Older audit items still open

From `docs/aws-parity-and-performance-audit.md`:

- **P0-1**: one `ContainerLogs` Docker call per invoke. Now incremental, but still a Docker round trip per invoke.
- **P1-1**: whole-state JSON rewrites per flush for SQS and DynamoDB (only with persistence on; the flusher now runs only when dirty).
- **P1-2**: linear scans of queue messages per receive, delete and visibility change.
- **P1-5**: DynamoDB Query and Scan cost.
