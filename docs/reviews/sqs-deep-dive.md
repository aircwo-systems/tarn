# SQS backend deep dive

Reviewed 13 October 2026 at commit `6c357d634420da5b9553b20b6fcc98697456a907`, against the working tree. The reviewed backend files have no pre-existing changes. Other ongoing work in the repository was left alone.

The SQS module can improve without changing its public method signatures or persistence format. Start by making returned data safe to read and DLQ movement safe to retry. The smallest measured performance improvement is replacing four metric scans with one pass. I would defer a queue scheduler rewrite until a real workload shows that scanning is a bottleneck.

The original review used isolated experiments and did not change application code. The [metrics patch](sqs-metrics-candidate.patch) and [evidence probes](sqs-evidence_test.go.txt) record that original baseline. The metrics patch is now applied by the follow-up below.

## Quick-win implementation, 13 October 2026

The code changes in this batch are committed as `97a2a3f` on `develop-mvp`.

The working-tree follow-up implements one-pass message counts, transactional queue attribute updates, config snapshots from creation/get/list, isolated caller-owned tags, complete binary-attribute copying through the existing helper, and request cancellation in both SQS HTTP protocols. Already-cancelled receives return without claiming messages.

Regression tests cover all combinations of message-count states and requested metrics, config snapshots during concurrent updates, rejected-update state and dirty-flag preservation, binary attributes, and both HTTP cancellation paths. The SQS, handler, admin, event-source, SNS, and EventBridge suites pass with the race detector. Full `make test` passes with the race detector, including Docker end-to-end tests. `make lint` reports zero issues, and `go vet ./...` passes. Changed Go files are formatted.

The follow-up metrics benchmark at 100,000 entries has a median of 147.51 microseconds across three runs, compared with the original 488.71 microseconds. That is about 3.3x faster locally, with the same 400 bytes and 10 allocations per request. The benchmark now lives in `internal/sqs/metrics_test.go`; HTTP latency was not measured.

The remaining findings from this batch are resolved by the follow-up below. The analysis and raw failure logs later in this document describe the original baseline.

## Remaining-finding implementation, 14 October 2026

All six numbered correctness findings and the shutdown/cancellation opportunities are now addressed. The first batch fixed config snapshots, rejected-update rollback, binary copying, metrics, and HTTP cancellation. This follow-up completes message ownership and fixes DLQ transfer, persistence retry, FIFO wakeups, and reaper lifecycle:

- Send, duplicate-send, receive, and peek return detached messages while holding the queue lock. Incoming attributes are copied, including nested structs and binary slices. A later receive cannot rewrite an earlier response's receipt or counters. Nil and empty maps/slices retain their shape.
- One private DLQ transfer operation pins both live queues, locks them in lexical order shared with redrive, and rechecks the stored message's receipt, counters, retention, and current policy. A missing destination or failed delivery retains the source. Repeated and concurrent reaper/poller transfers cannot append it twice. Both paths preserve the group/dedup IDs and attach `TarnRetryCount`; redrive still clears retry metadata, clones attributes, creates a fresh ID, and applies destination retention.
- Caller policies remain explicit. The reaper waits for visibility expiry and bypasses send disruption, tracing, default destination delay, and send deduplication. The poller retains those send behaviours and records its trace after queue locks are released. Messages below the threshold still return "not moved" without requiring a destination.
- Persistence failures are logged and re-mark the store dirty, scheduling another attempt through the existing 250 ms flusher interval. Writes still clear dirty before snapshotting so concurrent changes schedule their own write. Failed temporary files are removed. Shutdown makes a final pending-state attempt; a continuing failure stays dirty and is reported without making shutdown retry forever. The shared persistence interface and JSON format are unchanged.
- Reaping expiry, stale parking, and successful DLQ movement signals the source FIFO queue. Unchanged queues stay asleep. FIFO long polls also consider a blocker's retention deadline so they can progress without waiting for the reaper tick.
- Start/Stop are idempotent and serialized. Stop waits for an active reap to finish, including concurrent calls and stop-before-start setups. Close joins the reaper before the final persistence flush.

Regression evidence:

| Requirement | Coverage |
| --- | --- |
| Message/input ownership and old receipt isolation | [Ownership tests](../../internal/sqs/ownership_test.go), including concurrent response serialization/redelivery and duplicate sends |
| Missing/failed DLQ, current receipt/eligibility, concurrent transfers, FIFO destinations, trace/disruption policies, and redrive | [DLQ tests](../../internal/sqs/dlq_test.go); existing service and event-source failure-routing tests |
| Failed writes recover without new mutations; rename cleanup, bounded final write, and mutation during a flush | [Persistence retry tests](../../internal/sqs/persistence_retry_test.go); existing persistence round-trip/final-write tests and shared flusher tests |
| Expiry/stale/DLQ FIFO wakeups, no unchanged wakeups, retention deadline, and joined shutdown | [Reaper/lifecycle tests](../../internal/sqs/reaper_test.go); existing long-poll/compaction tests |
| Earlier config, binary-copy, metrics, and HTTP cancellation fixes | Existing attributes/metrics tests and both protocol cancellation tests |

The completed receive fixture ran on Apple M5/macOS arm64 with `-cpu=1 -benchtime=200ms -count=3`. Median one-message receives cost 307.8 ns, 232 bytes and four allocations without attributes, or 535.7 ns, 872 bytes and nine allocations with one string and one 256-byte binary attribute. Ten-message batches cost 2,681 ns/2,432 bytes/32 allocations and 4,975 ns/8,832 bytes/82 allocations respectively. This measures the complete in-memory receive, including receipts and snapshots, not isolated copying or HTTP throughput. It establishes the safety cost without claiming a speedup. See [snapshot benchmark output](sqs-snapshot-bench.txt).

The [validation record](sqs-implementation-validation.txt) records final checks. No correctness finding remains open. Receipt/ID indexes, cached persistence snapshots, timer heaps, and storage replacement remain optional workload-driven work; they are not required to resolve these failures.

## Original review scope and design

The module includes `internal/sqs`, with its callers in the SQS HTTP handlers, Lambda event-source poller, and admin overview. Persistence uses the shared `internal/persist` flusher. SNS and EventBridge are producer integrations checked through their tests.

The external seam is `Service`. Its interface already hides substantial implementation: standard/FIFO queues, delayed delivery, visibility, receipts, retries, filtering releases, tags, processed counts, DLQ movement, redrive, chaos injection, tracing, and persistence. A new repository interface would add complexity without a second adapter to justify it.

Internally, `Store` owns a queue-name map behind an `RWMutex`. Each queue owns a message slice, config, FIFO dedup map, and wake channel behind a mutex. Message order is slice order. Deleted messages remain as tombstones until the one-second reaper compacts the slice. Receipt-based operations scan it.

These existing choices should stay:

- Long polling uses a channel and the next visibility/delay deadline rather than timer-based rescanning. See [receive](../../internal/sqs/store.go#L447) and [long polling](../../internal/sqs/service.go#L165).
- Empty receives do not mark persistence dirty. The dedicated test protects this.
- Reaping compacts in place and clears removed references. The dedicated test protects this too.
- JSON encoding and file writing happen after queue locks are released. The shared flusher coalesces writes and serializes them.
- Queue locks allow independent queues to operate concurrently. Redrive already takes two queue locks in lexical name order.
- The admin overview already caches computations for 1.5 seconds and combines concurrent requests. Extra overview caching would duplicate existing work.

## Measured performance

Go 1.26.0, macOS arm64, Apple M5. Each benchmark ran three times with `-cpu=1 -benchtime=200ms`; values below are medians. Fixtures and message construction are outside timed sections. These are local module microbenchmarks, not HTTP latency or application throughput measurements.

The metrics fixture has equal proportions of visible, in-flight, delayed, stale, and deleted messages. All messages have future retention deadlines. The candidate preserves the existing predicates and only emits requested keys.

| Queue entries | Four scans today | One-pass candidate | Local speedup |
| --- | ---: | ---: | ---: |
| 1,000 | 2.95 microseconds | 1.56 microseconds | 1.89x |
| 10,000 | 33.99 microseconds | 13.64 microseconds | 2.49x |
| 100,000 | 488.71 microseconds | 154.01 microseconds | 3.17x |

Allocation counts did not change. At 100,000 entries both implementations allocate 400 bytes in 10 allocations per metrics request. A config-only `QueueArn` request still avoids scanning: medians were 135 ns before and 137 ns after. A visible-count-only request was 123.28 microseconds before and 120.13 microseconds after. The small differences in those controls are within the variation of this short experiment.

The real caller is [admin overview](../../internal/api/admin/handler.go#L1066), which requests all four counts. The implementation is [GetQueueAttributes](../../internal/sqs/store.go#L341). The larger queues benefit from reading fewer message pointers and fields while holding the queue mutex. At normal developer queue sizes, the absolute saving is small, and the existing overview cache limits how frequently it occurs.

Other current costs:

| Operation | 1,000 entries | 10,000 entries | 100,000 entries |
| --- | ---: | ---: | ---: |
| Delete by receipt at slice tail | 1.58 microseconds | 15.47 microseconds | 182.64 microseconds |
| Empty receive, all messages in-flight | 1.29 microseconds | 15.50 microseconds | 225.74 microseconds |
| Clean reaper pass, nothing removed | 1.73 microseconds | 18.86 microseconds | 226.27 microseconds |

All three allocated zero bytes per timed operation in these fixtures. The deletion benchmark resets the target's deletion flag each iteration to measure the same tail lookup. This is a scan-cost experiment, not an end-to-end drain benchmark. Empty receive benchmarks measure one attempt; an actual long poll sleeps between relevant events.

Raw measurements are in [baseline benchmarks](sqs-baseline-bench.txt), [candidate benchmarks](sqs-metrics-candidate-bench.txt), [baseline controls](sqs-baseline-subset.txt), and [candidate controls](sqs-candidate-subset.txt). The runs were sequential and short, so the speedup is directional evidence rather than a statistically established production result.

## Confirmed correctness findings

The existing race-enabled suites passed. Focused probes exposed gaps those tests do not cover. Fixing these changes broken edge-case behavior while preserving intended functionality; they should have their own commits rather than being hidden inside a performance refactor.

### 1. Returned configs and messages share mutable store state

**Priority: high.** [GetQueue](../../internal/sqs/store.go#L183), `ListQueues`, and `CreateQueue` return stored config pointers. Queue mutations use `q.mu`, but callers read these pointers after the lock is gone. The overview reads config fields and tags, and the receive module reads default visibility and redrive policy this way.

[receive](../../internal/sqs/store.go#L512) returns the stored message pointer after assigning its receipt and counters. A later receive can overwrite those fields while the first consumer serializes or processes them. `SendMessage` also returns a stored pointer and retains the caller's attribute map; queue creation retains the caller's tags.

Two focused `-race` tests reproduced concurrent config reads versus attribute updates, and message reads versus re-receives. The second probe uses zero visibility to make the overlap immediate; a slow consumer surviving a nonzero visibility deadline can encounter the same ownership problem. See [race evidence](sqs-race-evidence.txt).

Return snapshots while holding the owning queue lock, and copy mutable input maps on entry. Strings can remain shared. Binary slices need deep copies. Keep the same return types so callers gain safe data without learning locks or ownership rules. Creation, existing-queue creation, listing, sending, duplicate sends, and receiving must all follow the same rule.

Some current tests alter returned messages to simulate expiry. Once snapshots are explicit, those tests should arrange internal state under the test's queue lock or use an internal clock seam. Do not retain unsafe public aliasing solely to keep those test fixtures convenient. Benchmark snapshot allocation costs before calling this a performance improvement.

### 2. DLQ transfer has two implementations with different failure behavior

**Priority: high.** [Reap](../../internal/sqs/store.go#L878) removes messages from the source before checking whether the destination exists. At [destination lookup](../../internal/sqs/store.go#L912), a missing DLQ logs a drop. The probe confirmed that the source has zero messages afterward.

The reaper also drops `MessageDeduplicationId` and does not add `TarnRetryCount`. [MoveToDLQIfExceeded](../../internal/sqs/service.go#L245), used by the poller, preserves the dedup ID and attaches the retry count. The same failure can therefore produce different DLQ metadata depending on which path handles it.

The poller path sends to the destination before deleting the source, with no single transfer claim. A deterministic retry of `MoveToDLQIfExceeded` with the same message produced two destination messages; the second source deletion fails only after its destination send. Reaper/poller concurrency can create another overlap, though that specific interleaving was not dynamically reproduced.

Use one private transfer implementation that resolves the destination, locks both queues in the existing consistent order, revalidates the current source receipt and eligibility, and appends/removes once. Retain source messages if the target is missing or delivery cannot proceed. Return the existing moved/name/error results.

The two callers currently have different timing and side effects. The reaper waits for visibility expiry, bypasses the send disruptor, and does not record a send trace. The poller transfers after a failed invocation, calls `SendMessage`, and can hit the disruptor and tracing. Share transfer correctness while preserving those policies explicitly. A blind replacement of the reaper with `Service.SendMessage` would change behavior. Keep redrive's removal of `TarnRetryCount`, new message IDs, fresh destination retention, and cloned original attributes.

### 3. Persistence failure clears the only retry signal

**Priority: high when persistence is enabled.** [Flusher.Flush](../../internal/persist/flusher.go#L106) clears dirty state before invoking its callback. [flushToDisk](../../internal/sqs/store.go#L1147) silently returns on encoding, directory, write, or rename errors. The callback cannot report failure.

A probe replaces the freshly created queue directory in its temporary data directory with a regular file, then flushes a dirty queue. The dirty flag becomes false despite the failed write. Without another mutation or explicit flush, an automatic retry is not scheduled. `Close` also skips a final write when the flag is false.

Report the failure and retain a bounded retry mechanism. Clearing dirty before a write is necessary to detect mutations during that write; simply moving the clear afterward can lose concurrent changes. Either make the callback return an error and let the shared flusher re-mark dirty with backoff, or handle this locally in SQS without changing other modules. A shared callback change requires tests for every store using it. Preserve serialized atomic file replacement and final pending-state writes.

This is error recovery, not a proposal to change the current asynchronous durability model or persistence format.

### 4. Binary data escapes the existing clone

**Priority: medium, part of the ownership fix.** [cloneMessage](../../internal/sqs/store.go#L1035) copies attribute structs but shares their `BinaryValue` slices. Mutating a binary value returned by `PeekMessages` changed the next peek's stored value from 1 to 9 in the probe.

[cloneMessageAttributes](../../internal/sqs/service.go#L305) already copies binary slices correctly. Reuse one correct helper across snapshots and transfers, preserving nil/empty-map behavior where the existing interface needs it. Add checks for maps, nested attribute structs, binary slices, and caller-owned send attributes.

### 5. A rejected config update still changes the queue

**Priority: medium.** [applyQueueAttributes](../../internal/sqs/store.go#L942) assigns FIFO throughput settings before validating their combination. `SetQueueAttributes` passes it the live config.

Setting `FifoThroughputLimit=perMessageGroupId` without changing default `DeduplicationScope=queue` returns an error but leaves the queue's throughput field changed. The probe confirmed this through `GetQueueAttributes`. The failure path does not mark persistence dirty or update the modification timestamp, so memory and persisted state can also diverge.

Clone the config under `q.mu`, apply and validate on the candidate, then commit only on success. Keep current accepted values and echoed Terraform compatibility fields. Stricter parsing, numeric limits, and additional attribute support would be separate behavior changes.

### 6. Reaping can unblock a FIFO group without waking its long poll

**Priority: medium.** [receive](../../internal/sqs/store.go#L483) blocks later FIFO messages behind an earlier unavailable one. Its timer considers visibility and delay, not retention expiry. [Reap](../../internal/sqs/store.go#L871) removes expired blockers, or marks them stale, without signaling the source queue.

A deterministic probe registered an empty receive's wake channel behind an in-flight FIFO head, expired that head, and reaped it. The wake channel remained open despite a now-available following message. The already-computed timer still pointed at the old visibility deadline.

Signal the source queue when reaping changes FIFO receivability. Cover expiry, stale parking, and DLQ movement. An eligible FIFO head's retention deadline can also bound the long-poll timer so it does not depend solely on the reaper tick. Preserve existing wakeups on send, release, visibility change, delete, redrive, queue deletion, and shutdown. Avoid signaling unchanged queues on every tick.

## Further opportunities supported by code inspection

### Index receipt lookups when backlog size justifies it

[DeleteMessage](../../internal/sqs/store.go#L565), `ChangeMessageVisibility`, `ReleaseMessage`, and `DeleteMessageByID` scan the entire slice. A private map from current receipt handle to message would make receipt lookup expected constant time. An ID map can support admin deletion separately. Keep the ordered slice for receive, FIFO, peeking, and snapshot iteration.

The maintenance cost is real. Every new receive must remove the old receipt mapping. Deletion, purge, reaping, DLQ movement, redrive, and persistence reload must rebuild or update indexes. Tombstones must not remain in them. Preserve existing errors for obsolete and missing receipts. Empty receipts currently match unreceived messages in the scan implementation, so tightening that behavior must be an explicit validation fix rather than an unnoticed consequence of indexing only nonempty receipts.

Measure a drain workload and mixed send/receive/delete latency before introducing the maps. The observed 100,000-entry tail lookup costs about 183 microseconds, but many developer queues will never need the extra memory or maintenance. No index prototype was benchmarked here.

### Cancel HTTP long polls when the request ends

[Query receive](../../internal/api/sqs/handler.go#L345) and [JSON receive](../../internal/api/sqs/json.go#L322) call `ReceiveMessage` with no request cancellation. The module already provides `ReceiveMessageUntil` and the Lambda poller uses it.

Pass `r.Context().Done()` through both handlers. The JSON helper needs the request or cancel channel added to its private parameters. Check for an already-cancelled request before claiming messages. This removes work for abandoned requests while preserving behavior for active clients. Add cancellation tests through both wire protocols, not only the existing direct-service test. This opportunity was inspected, not measured with a disconnect load test.

### Measure snapshot cost before changing storage

Every dirty flush clones and serializes all queues and their messages. A busy queue can therefore trigger work over unrelated queues. [The snapshot filter](../../internal/sqs/store.go#L1176) only skips a message when it is both deleted and expired, so it can also write tombstones awaiting reaping.

Measure snapshot bytes, flush duration, allocation volume, dirty age, and queue-lock wait time with realistic body sizes and persistence enabled. A per-queue cached snapshot might avoid repeated cloning of unchanged queues while retaining the current file format, but total-file serialization and rewriting would remain. Streaming encoding could reduce the extra encoded byte buffer; it must remain outside queue locks and preserve the atomic rename flow.

Do not promise throughput gains from a new database or journal based on these scan benchmarks. Persistence was not benchmarked here.

### Make shutdown actually wait for the reaper

[Stop](../../internal/sqs/service.go#L50) closes the done channel but does not join the reaper. A tick already executing can still mutate the store. [Account shutdown](../../internal/cli/server.go#L376) then closes persistence after stopping workers and assumes there are no more writes.

Use an internal completion signal and idempotent stop so shutdown can wait for an active reap before the final store flush. Keep zero-work service test setups usable. This lifecycle risk is supported by inspection; it was not reproduced with a forced shutdown interleaving.

## Preservation requirements

| Area | Keep during the work | Verification |
| --- | --- | --- |
| Standard delivery | Delay/default visibility, new receipt per receive, counters, expiry | Existing store tests plus old/new receipt checks |
| FIFO | Same-group messages may share one batch; subsequent receives wait for all earlier in-flight group messages; independent groups progress | Existing ordering and batch tests; expiry/stale/DLQ unblock tests |
| Filtering | Release reverses a receive-count bump and clears first-receive time when count reaches zero | Existing release and event-source filter tests |
| Metrics | Visible and stale counts can overlap; delayed and in-flight counts can overlap; expired/deleted messages do not count; only requested keys appear | All 32 state combinations, 16 requested metric subsets, nil and All |
| DLQ/redrive | Retry metadata, original attributes, destination retention, redrive retry reset, intended chaos and trace policies | Current service tests plus missing target, repeated transfer, reaper/poller overlap, FIFO destination tests |
| Persistence | Existing JSON keys, saved visibility/delay/expiry/receipts/dedup/counts, endpoint rewriting, tolerant decoder, serialized rename, final pending write | Existing round-trip/close tests plus failed-write retry and concurrent mutation tests |
| Wire compatibility | Query/XML and JSON shapes, partial batch failures, tags, Terraform attribute echoes, request-derived queue URLs | SQS handler/disruptor tests plus cancellation in both protocols |
| Account isolation | One store/disruptor per account, no receipt or index sharing between accounts | Account-scoped handler tests |

The candidate metrics parity test intentionally preserves the current predicates. For example, a stale but visible message contributes to both visible and stale totals even though receive skips it. Reclassifying messages into mutually exclusive states would change dashboard behavior.

Some stored settings are currently compatibility fields rather than implemented send behavior. `SendMessage` only enters deduplication for an explicit nonempty dedup ID; it does not implement content-based hashing or use `DeduplicationScope` in its key. JSON receive also treats explicit zero visibility as a request for the default, while query receive passes zero through. These are separate behavior questions. Do not quietly change them while claiming an optimisation preserves functionality.

## Suggested commit sequence

1. Make config updates transactional and message/config copies complete. Add the failure probes as regressions, replace intentional public-pointer mutation in test fixtures, and race-test actual concurrent reads and receives.
2. Consolidate DLQ transfer correctness, retain source messages on failure, prevent repeated transfers, and preserve each caller's timing, tracing, and disruptor policy. Fix source FIFO wakeups in a separate small commit if that keeps the transfer change easier to review.
3. Apply the one-pass metrics patch. It is independent, keeps current predicates and persistence format, and has a measured local benefit.
4. Connect HTTP request cancellation and make worker shutdown wait for completion. Verify both wire protocols and pending-state shutdown behavior.
5. Repair persistence error reporting/retries. If the shared flusher changes, expand checks across all snapshot stores.
6. Add receipt/ID indexes only after a representative backlog benchmark justifies them. Defer timer heaps, per-group scheduling, cached counters, and storage replacement until measurements require them.

Steps 1 and 2 address the highest risk to current functionality. Step 3 is the smallest performance patch. Keep them separate so gains and behavioral fixes can each be assessed.

## Validation and reproduction

Current application code passed:

```sh
go test -race ./internal/sqs ./internal/api/sqs ./internal/eventsource ./internal/sns ./internal/eventbridge
```

The isolated metrics candidate passed all existing SQS tests with `-race`, plus the metrics predicate/subset test. The other focused probes intentionally failed against current code and are evidence of the findings, not additions to the normal test suite. There are seven failing correctness probes and two failing race probes. See the [correctness output](sqs-correctness-evidence.txt) and [race output](sqs-race-evidence.txt). The full repository suite, lint, Docker end-to-end tests, and a real HTTP load test were not run for this review.

To reproduce using a disposable package copy within the repository, run from the repository root:

```sh
review_dir=$(mktemp -d internal/sqs_review_XXXXXX)
cp internal/sqs/*.go "$review_dir/"
cp docs/reviews/sqs-evidence_test.go.txt "$review_dir/review_evidence_test.go"

go test -race "./$review_dir" -run 'TestReview(Config|Receive)' -count=1
go test "./$review_dir" -run 'TestReview(Missing|Reaper|Peek|Rejected|Failed|DLQTransfer|ReapWakes)' -count=1
go test "./$review_dir" -run '^TestReviewMetricsPredicates$' -count=1
go test "./$review_dir" -run '^$' -bench '^BenchmarkReview' -benchmem -benchtime=200ms -count=3 -cpu=1
```

The probes access private queue state to create deterministic fixtures. Their binary-copy and metrics-subset checks exercise the public store methods. The scratch package does not alter the application's real SQS package.

To test the candidate, apply the attached patch to the scratch copy's `store.go`, then run the existing tests and predicate parity check:

```sh
patch "$review_dir/store.go" < docs/reviews/sqs-metrics-candidate.patch
go test -race "./$review_dir" -run 'Test[^R]|TestR[^e]|TestRe[^v]|TestReviewMetrics' -count=1
go test "./$review_dir" -run '^$' -bench '^BenchmarkReviewMetric' -benchmem -benchtime=200ms -count=3 -cpu=1
```

The test-selection expression includes the existing tests and metrics parity check while excluding the deliberately failing `TestReview` probes. Remove the disposable directory after inspection. The original review scratch package was removed before delivery.
