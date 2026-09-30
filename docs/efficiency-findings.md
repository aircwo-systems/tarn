# Resource efficiency findings

Status: open items from the September 2026 resource audit
Scope: idle CPU and wakeups, containers left running, and memory growth on a developer laptop

The audit covered three areas: containers and processes, background loops and pollers, and in-memory data. Every item below was traced through the code. Items marked **verified** were also checked by hand against the source. Line numbers are as of commit `fb961df`.

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

## Open: medium, one focused change each

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

Still open: the poller wakes once a second rather than on write. Each wake is now
cheap, but a per-stream signal would remove it, as SQS long polls already do.

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

`internal/ecs/runner.go` `recoverTask` (around line 1553), `internal/engine/task.go` log pump

- Recovered tasks follow logs with no `Since` or `Tail`, so every restart re-ingests each container's full log, duplicating events.
- `spawnHealthPoller` is not called on the recovery path, so recovered containers with health checks are never polled.

Fix: pass `Since` (Tarn start time, or the last ingested time) for recovered containers, and start the health poller in recovery.

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

## Open: small

- **Log stream map grows per container.** Each cold start adds a stream entry to the function's log group (`internal/logs/store.go` `PutLogEvents`), cleared only by `ClearGroup`.
- **Event source `Stop` doesn't wait.** `internal/eventsource/service.go` `Stop` closes `done` but does not join poll or drain goroutines, so an update during a backlog can briefly run old and new pollers together (up to twice `MaximumConcurrency`). Track a `WaitGroup` per poller, or cancel the invoke context on stop.
- **One timer per traced invoke.** `internal/trace/collector.go` `Finish` schedules a 60 ms `AfterFunc` per invoke. There is no idle cost, only churn under load.
- **Raw log levels for Node JSON lines.** The Node runtime stores `console.log` output as INFO, so `{"level":"error",...}` lines show as INFO in the events view and in `level=ERROR` filters on `events-all`. The log summary already uses the line's own level. Fix at ingestion (`internal/logs/service.go` `classifyLambdaLogEvent`).
- **Placeholder UI shell gets overwritten.** `make ui-build` replaces `internal/api/ui-dist/200.html`, so a real build shell can be committed again and break `TestRootDispatchServesReferencedUIAsset` in CI. Skip `200.html` in the Makefile copy, or mark it skip-worktree locally.
- **Flaky test.** `TestRunnerRemovesTaskScopedVolumeOnStopButNotShared` (`internal/ecs/runner_test.go`) fails about 1 run in 6 to 10 under `-race`, before and after this work. Its 2-second wait for STOPPED looks too tight under load.

## Older audit items still open

From `docs/aws-parity-and-performance-audit.md`:

- **P0-1**: one `ContainerLogs` Docker call per invoke. Now incremental, but still a Docker round trip per invoke.
- **P1-1**: whole-state JSON rewrites per flush for SQS and DynamoDB (only with persistence on; the flusher now runs only when dirty).
- **P1-2**: linear scans of queue messages per receive, delete and visibility change.
- **P1-5**: DynamoDB Query and Scan cost.
