# DynamoDB backend deep dive

Reviewed 13 October 2026 at commit `6c357d634420da5b9553b20b6fcc98697456a907`, against the current working tree. The DynamoDB backend has no pre-existing changes. The SQS quick wins and other ongoing work were left alone.

DynamoDB has a larger performance opportunity than the SQS metric scans. A ten-item read currently builds a detached copy of all eligible table items and sorts it with a comparator that repeatedly copies and JSON-encodes keys. A tested candidate computes comparison values once per item. It reduces allocated bytes per read by 66–70% in the 10,000-item fixture while keeping current sorting behavior.

The review also reproduced pagination, projection, key-validation, ordering, and stream-read failures. Keep those fixes separate from the performance patch. Numeric-key identity needs a migration plan before changing persisted lookup keys.

The original review did not change application code. The [sort candidate](https://github.com/aircwo-systems/tarn/blob/develop-mvp/docs/reviews/dynamodb-sort-candidate.patch) and [reproducible probes](dynamodb-evidence_test.go.txt) record that baseline. The first implementation batch below now applies the reviewed changes.

## First implementation batch, 13 October 2026

The code changes in this batch are committed as `5f2bd17` on `develop-mvp`.

The working tree now locks stream metadata reads, returns a detached stream key schema, starts LATEST after the current tail, includes both table and selected index keys in KEYS_ONLY/INCLUDE results, and computes sort comparison values once per item. The unlocked stream lookup and superseded schema comparator were removed.

New regression tests exercise public store operations for iterator boundaries, empty/existing streams and subsequent writes, concurrent item writes and stream reconfiguration, schema/result mutation isolation, all index projection modes in Query/Scan, sparse-index exclusion, descending paged reads, numeric precision, and base-key tie breaking. The existing encoded string ordering is explicitly preserved for a separate compatibility fix.

Public method signatures and the persistence format are unchanged. Pagination/filter limits, ScannedCount, query validation, typed key identity/migration, raw UTF-8 ordering, and persistence retries remain separate work. The analysis and failure logs below describe the original baseline.

The installed-code benchmark confirms the allocation reduction at 10,000 items: 77.1 MB for a ten-item Scan page, 93.3 MB for a missing-partition Query, and 85.3 MB for a ten-item Query page. These remain about 66–70% below the original baseline. Runtime varied across samples, so no fixed latency multiplier is claimed. See the [installed benchmark output](dynamodb-installed-bench.txt). Validation passed: the full race-enabled `make test`, an uncached `go test -race ./test -count=1` Docker end-to-end run, `go vet ./...`, formatting, and `make lint`. Lint reported zero issues after clearing stale generated linter cache; no unrelated source files were changed. Current regression tests are [stream metadata tests](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/stream_metadata_test.go) and [read-path tests](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/read_path_test.go).

## Module and callers

`internal/dynamodb/service.go` presents the external interface. `store.go` owns table definitions, items, index metadata, streams, compatibility settings, and snapshots. `expr.go` handles conditions, document paths, updates, projections, and comparisons.

The AWS JSON adapter is `internal/api/dynamodb/handler.go`. The Lambda event-source poller consumes `StreamBatchUntil`, and the admin overview reads table and stream descriptions. [Account initialization](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/cli/server.go#L210) creates an independent DynamoDB module per account.

The module is deep enough to keep these caller-facing methods. A generic repository interface would add a hypothetical seam without resolving the observed problems. The useful internal seams are request preparation, item selection/pagination, ordering/projection, and stream metadata reads. They can remain private.

The store has one `RWMutex` for all tables in an account. Writes hold its exclusive lock. Reads use [readTable](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1318), and returned items/table descriptions are normally deep copies. Query and Scan release the lock before expression evaluation and output projection, but take the complete item snapshot and sort while holding it. Other tables' writers therefore wait for that work too.

Keep the existing stream improvements. Records are capped at 10,000 per stream, trimmed with reference clearing, located by binary search on sequence, and copied only for the selected window. Waiting pollers use stream wake channels and cancellation. Those behaviors already have tests and should not be replaced with polling or a fresh full-stream scan.

## Measurements and tested candidate

Go 1.26.0, macOS arm64, Apple M5, `-cpu=1`. The fixture has 10,000 items across 100 partitions. Items contain string keys, a 256-character payload, status, and nested list/map attributes. Streams and persistence are disabled. Setup is outside benchmark timing.

The Scan and successful Query request ten items. The missing-partition Query returns none. Despite these small responses, [collectSortedItems](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1372) copies and orders the whole eligible table each time.

The candidate hoists `unionKeySchema` out of the item loop and caches each key's current encoded sort value and parsed numeric value. The sort then compares cached values. Secondary key order, base-key tie breaking, descending order, and current numeric comparison remain the same. The patch deliberately retains JSON-based string ordering so its optimisation does not also change the comparator contract.

Three longer runs per operation used `-benchtime=1s`. Medians:

| Operation, 10,000 table items | Current time | Candidate time | Current allocated bytes | Candidate allocated bytes | Byte reduction |
| --- | ---: | ---: | ---: | ---: | ---: |
| Scan, ten-item page | 326.6 ms | 93.1 ms | 257.1 MB | 77.1 MB | 70.0% |
| Query, missing partition | 553.2 ms | 103.8 ms | 273.2 MB | 93.3 MB | 65.9% |
| Query, ten-item page | 422.0 ms | 179.7 ms | 264.8 MB | 85.3 MB | 67.8% |

MB here means total allocated bytes per call divided by one million. It is not retained memory or peak process memory. Allocation counts also fall: a missing-partition Query drops from about 4.17 million to 1.38 million allocations per call.

Timing varied substantially between runs on this shared machine. Shorter runs put the same current operations near 238–240 ms and the candidate near 79–92 ms. Longer runs included a 939 ms current Query sample and a 220 ms candidate Query sample. The allocation reduction is repeatable; the exact runtime multiplier is not established well enough to promise it. No HTTP throughput, tail latency, or mixed reader/writer load test was measured.

A separate CPU/allocation profile of the current missing-partition Query points to sorting as the first target. The sort comparator accounts for about 66% of allocation-space samples. `cloneItem` accounts for about 21%. These cumulative call-chain percentages overlap and must not be added together. The profile includes fixture setup and runtime work, so it is supporting evidence, not a production traffic profile.

The candidate passed all existing DynamoDB tests with the race detector. Added parity checks compare its output with the original comparator for table/LSI/GSI order, both directions, numeric-equivalent values, binary attributes, escaped strings, and Unicode. Existing CRUD, conditional update, stream, tag, persistence, and concurrent-read tests passed. The deliberately failing compatibility probes remain failing because the candidate changes only sorting cost.

Artifacts:

- [Short baseline runs](dynamodb-baseline-bench.txt) and [candidate runs](dynamodb-candidate-bench.txt).
- [Long baseline runs](dynamodb-baseline-bench-long.txt) and [candidate runs](dynamodb-candidate-bench-long.txt).
- [CPU profile](dynamodb-cpu-profile.txt), [allocation profile](dynamodb-allocation-profile.txt), and [candidate test result](dynamodb-candidate-tests.txt).

## Confirmed behavior gaps

### Stream metadata escapes its lock

**Priority: high.** [findStream](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1356) returns a live `tableState` pointer after releasing `s.mu`. [GetShardIterator](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1118) reads `state.Stream.NextSeq` outside the lock while `PutItem` increments it under the lock. A focused `-race` test reproduced that conflict.

[DescribeStream](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1082) follows the same lookup pattern. A concurrent stream disable replaces `state.Stream` with nil, so the subsequent reads can race or dereference nil. That particular panic was not forced in a test. Its returned `KeySchema` is also the stored slice. A deterministic probe mutated the response and changed the table's real key schema.

Remove the unlocked-pointer helper. Resolve and copy needed metadata under the store's read lock, then encode responses/iterators from detached values. Copy the schema slice. Use the same not-found error if a stream disappears before the locked lookup. Add races against writes and stream disable/reconfiguration as well as response-mutation tests. Public method signatures can remain unchanged.

### LATEST replays the last existing record

**Priority: medium, small fix.** `NextSeq` is incremented before a record is appended, so it represents the last assigned sequence. `GetShardIterator` uses that value directly for `LATEST`, and `GetRecords` includes records at the iterator sequence. After one write, the probe's fresh LATEST iterator returned that existing record.

Use the next sequence after the current record, captured under the lock. Cover empty streams, existing records, a subsequent append, and every other iterator type. AWS documents LATEST as starting after the most recent record. [GetShardIterator reference](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_streams_GetShardIterator.html).

The Lambda poller has its own last-sequence checkpoint path through `StreamBatchUntil`. Preserve it; do not alter that path to fix the separate public iterator.

### Limit and ScannedCount apply to the wrong population

**Priority: high for client compatibility.** [Scan](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L882) and [Query](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L957) continue after filtered items and stop when enough returned items have been appended. Their results only contain a returned count. The [HTTP adapter](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/dynamodb/handler.go#L535) reports `ScannedCount = Count` for both operations.

The fixture contains two ordered items. The first fails the filter, the second passes, and Limit is one. Both operations return the second item instead of an empty first page with a continuation key at the first item. A separate HTTP probe, without a Limit, examines two items and returns one; both wire responses report ScannedCount one instead of two.

AWS applies the page limit before filtering and distinguishes evaluated from returned counts. An empty filtered page can still carry a continuation key. [Query reference](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Query.html), [Scan reference](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Scan.html).

Give read execution a separate evaluated count and last evaluated key. For Query, apply the key condition before the evaluated-page limit, then the result filter. For Scan, evaluate the page across eligible table/index entries before filtering. Forward both counts through the adapter. Keep a continuation key even when the page returns zero items.

The loops also have no 1 MB page-byte accounting. That is an additional gap established by inspection, not a separate size-limit probe. Address byte limits with pagination tests rather than quietly combining them with a sorting refactor.

### KEYS_ONLY and INCLUDE omit index keys

**Priority: medium, small fix.** [applyIndexProjection](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1690) allows base-table key names and requested included attributes. It never adds the index's own key schema. GSI probes lost `gsi_pk`/`gsi_sk`; LSI probes lost `lsi_sk`. Both projection modes reproduced the problem.

Pass the union of base and selected index keys to the projection helper. Test Query and Scan for both index kinds and all projection modes, including explicit document projection afterward. Keep sparse-index exclusion and base-key continuation values. AWS's projection definition includes the table and index keys, with INCLUDE adding selected attributes. [Index projection documentation](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html#GSI.Projections).

### Query accepts non-key and incomplete conditions

**Priority: high for validation.** Query only rejects a blank condition; the generic condition evaluator accepts a condition on `keep` without a partition key. The probe returned rows for `keep = :yes`.

The [expression evaluator](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/expr.go#L60) also does not check that its parser consumed every token. `pk = :p ignored` succeeded. That parser serves filters and conditional writes too, so a malformed suffix can be silently ignored outside Query.

Require complete token consumption at parsing. Validate Query key conditions against the selected schema: required partition equality and permitted sort-key predicates. Keep the generic evaluator for filters/conditional writes, with its broader operations. AWS requires partition equality for Query. [Query key-condition reference](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Query.html).

Make invalid-input handling a deliberate compatibility change. A prepared-expression optimisation must otherwise preserve when evaluation starts, short-circuit behavior, placeholder resolution, and errors on empty tables. Parsing eagerly can introduce errors that current empty-table reads never produce.

### Numeric identity and key types are inconsistent

**Priority: high before introducing real indexes.** [serializeKey](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1612) serializes the original attribute representation. Numbers `1` and `1.0` become different map keys even though the numeric comparator considers them equal. The probe could not read `1` using `1.0`, then stored both representations as two items.

[validateItemAgainstTable](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1587) checks presence but not declared key types. A numeric partition attribute was accepted in a table declaring that key as a string. This lets invalid data enter storage before any query planner or maintained index can rely on its schema.

Use typed key validation and an exact canonical identity for numeric lookup values. Keep number attributes as strings at the wire interface and preserve decimal precision. AWS stores numbers numerically and trims redundant zeroes; key attributes have declared scalar types. [Data types documentation](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.NamingRulesDataTypes.html).

Do not silently rekey old snapshots. Existing state can contain both numeric spellings with different data. Detect collisions, retain a recoverable copy, and define an explicit conflict/migration policy before enabling canonical identities. This is not a safe one-line optimisation.

### String order follows JSON escaping

**Priority: medium, separate comparator fix.** [compareAttributeValues](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L2029) compares JSON encodings for nonnumeric attributes. A newline sorts after `A` because its JSON escape begins with a backslash. The ascending Query probe returned `A` before newline.

Compare string keys by raw UTF-8 bytes and binary keys by decoded bytes, with separate numeric handling. A validated typed key representation makes this easier and avoids repeated encoding. AWS specifies UTF-8 string collation. [String data types](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.NamingRulesDataTypes.html).

The tested cache preserves the existing comparator, including this defect. Apply a corrected comparator in a separately tested behavior commit. It must cover controls, quotes, Unicode, binary values, numeric precision, and direction/ties.

### Deleted continuation items restart the read

**Priority: medium, confirmed behavior with an unverified AWS edge case.** [findExclusiveStart](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L1628) searches for an exact currently stored primary key. If it cannot find the continuation item, it returns zero.

The probe read items 01/02, deleted 02, and resumed with that page's continuation key. It got 01/03, repeating 01. The observed restart is definite; AWS's exact behavior for a deleted continuation item was not checked against a live service in this review.

Resume by the ordered key position rather than requiring the anchor item to still exist. Confirm the intended compatibility semantics before shipping. Cover ascending/descending table and secondary-index reads, including equivalent index keys with distinct base keys. Do not promise snapshot isolation across pages.

## Other opportunities

### Prepare expressions once per request

The same key/filter expression is tokenized and parsed for every visited item. The standalone condition benchmark is about 0.89 microseconds with 1,624 bytes and 27 allocations per evaluation. This contributes work, but the profile puts sorting first.

Introduce a private prepared evaluator for each read request, with immutable parsed nodes and that request's names/values. Resolve reusable paths where appropriate. Avoid a global expression cache without a bounded lifetime. Reuse an evaluator during Query/Scan while retaining current short-circuit/error behavior until validation fixes are shipped explicitly. No prepared-expression candidate was benchmarked here.

### Shorten global read-lock duration

The current lock covers whole-table copying and sorting, so a large read on one table blocks writes to another table in that account. The tested cache reduces the work but does not change this locking arrangement.

Sorting a complete detached snapshot can move outside the lock. Snapshot key schemas and projection metadata too. If later attempting to copy only references under the lock, first establish that stored item maps and nested values are immutable after insertion. Capturing live pointers and then assuming they stay unchanged would violate `readTable`'s current rule.

Measure mixed reader/writer latency before choosing per-table locks. A private table lock plus a short store lock for create/delete could reduce interference, but lifetime, deletion, persistence, stream configuration, and wakeup rules need one consistent design. No contention benchmark or per-table-lock candidate was built here.

### Avoid unnecessary cloning after selection

`collectSortedItems` deep-copies every eligible item, and returned items are cloned again. `cloneItem` uses JSON marshal/unmarshal. The standalone fixture costs about 3.59 microseconds, 4,576 bytes, and 67 allocations per clone.

A selected-page copy path or an explicit JSON-native deep copier could reduce work. Preserve response isolation and existing normalization of typed Go values, nil maps/lists, invalid strings, and unsupported values. The attached parity fixtures check several of those cases, but no alternative clone implementation was built or benchmarked. Do not substitute `normalizeAny` blindly; its fallback leaves typed slices unchanged.

### Add maintained indexes after read semantics are stable

LSI/GSI definitions are metadata used to construct, filter, and sort views on each read. They are not maintained lookup structures. Even a missing-partition Query currently snapshots and sorts the entire table.

A private partition lookup with ordered key entries can narrow candidates before copying or sorting. Keep base-item identity separate from secondary-index keys, which may repeat. Every Put/Update/Delete, sparse-index transition, reload, and table deletion must update or reconstruct the structure. Fix typed key identity and validation first; otherwise the index assumes invariants storage does not enforce.

Do not start with a new database adapter or change the public map-shaped request interface. A derived in-memory index can retain the existing snapshot format and rebuild on load, though its maintenance cost and memory need measurement.

### Resolve persistence recovery with the shared flusher

[flushToDisk](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/dynamodb/store.go#L191) marshals all tables while holding the global read lock and silently returns on write failures. The shared flusher clears dirty before writing, so it has the same failed-write retry issue found in SQS. `persist()` always returns nil, meaning mutation methods' persistence-error branches do not report the later flush failure.

Treat retry/error reporting as shared work rather than a second independent mechanism. Preserve serialized rename, dirty changes during a flush, and final pending writes. The SQS failure probe established the shared failure pattern; no new DynamoDB disk-failure probe was added.

## Preservation requirements and sequence

| Area | Requirements to retain |
| --- | --- |
| Item operations | Isolation of returned data, conditional-write failures, all supported update clauses and return-value modes |
| Query/Scan | Selected table/index schema, sparse indexes, ordering and base-key ties, both directions, filters, document projections, requested page behavior |
| Validation | Existing error shapes and alias/value handling; explicitly review changes to invalid-input acceptance |
| Streams | Sequence checkpoints, bounded retention, reference clearing, selected-window copies, wake-on-append/delete/disable, cancellation, partial-batch handling in the poller |
| Persistence | Existing JSON field shapes, number strings/precision, table/index definitions, compatibility settings, stream sequence and reload behavior |
| Client compatibility | Table-name and ARN inputs, AWS JSON response shapes, Terraform compatibility fields, per-account isolation |

Suggested small commits:

1. Lock and copy stream metadata, including response key schema. Add the race and isolation regressions.
2. Correct LATEST to start after the existing tail, with coverage for all iterator types.
3. Fix KEYS_ONLY/INCLUDE projection using the union of index and base keys, with table/LSI/GSI Query and Scan tests.
4. Apply cached sort values as a standalone performance change. Keep current order and numeric behavior, and retain the parity tests.
5. Fix parser completeness and Query schema validation. Keep malformed-input behavior changes separate from execution-cost changes.
6. Repair evaluated-page counting, ScannedCount, empty filtered pages, continuation handling, and page-byte limits together with HTTP tests.
7. Design numeric-key validation/canonicalization and snapshot migration before adding maintained indexes. Correct typed comparison in its own behavior change.
8. Then measure lock duration, selected-page copying, prepared expressions, and maintained partition/index lookup against representative workloads.

Steps 1–4 are the best first batch. Pagination and canonical key identity deserve their own review because they change client-visible behavior and, for keys, may affect existing persisted data.

## Validation and reproduction

The current working tree passes `go test -race ./internal/dynamodb ./internal/api/dynamodb ./internal/eventsource ./pkg/types`. These results were available in Go's test cache following the current working-tree runs. The sort candidate's complete DynamoDB suite ran with `-race -count=1` in a disposable package copy and passed in 8.2 seconds, including the added ordering and clone-parity checks.

Eleven focused correctness probes fail against the current implementation, plus one focused stream race probe. Their failures are intentional evidence, not additions to the repository's normal suite. See [correctness output](dynamodb-correctness-evidence.txt), [race output](dynamodb-stream-race-evidence.txt), and [baseline test result](dynamodb-baseline-tests.txt).

Only the current actual HTTP adapter is used by the ScannedCount probe. Other probes exercise the disposable Store copy. The sort patch does not change the real backend or make those compatibility failures pass.

The sort patch is already applied in the working tree. To reproduce the original before/after experiments, extract the reviewed commit rather than copying the current module. From the repository root:

```sh
review_dir=$(mktemp -d internal/ddb_review_XXXXXX)
git archive 6c357d634420da5b9553b20b6fcc98697456a907 internal/dynamodb | tar -x -C "$review_dir" --strip-components=2
cp docs/reviews/dynamodb-evidence_test.go.txt "$review_dir/review_evidence_test.go"

go test "./$review_dir" -run '^TestReview' -skip '^TestReviewLatestIteratorRace$' -count=1
go test -race "./$review_dir" -run '^TestReviewLatestIteratorRace$' -count=1
go test "./$review_dir" -run '^$' -bench '^BenchmarkReview' -benchmem -benchtime=200ms -count=3 -cpu=1

patch "$review_dir/store.go" < docs/reviews/dynamodb-sort-candidate.patch
go test -race "./$review_dir" -run 'Test[^R]|TestR[^e]|TestRe[^v]|TestReviewCachedSort|TestReviewCloneParity' -count=1
go test "./$review_dir" -run '^$' -bench '^BenchmarkReviewReadPath' -benchmem -benchtime=1s -count=3 -cpu=1
```

The candidate selection excludes deliberately failing TestReview probes and includes existing tests plus parity checks. Remove the disposable directory after inspection. The original review copy was removed before delivery. No full repository lint or Docker end-to-end rerun was needed for this review-only turn; both passed in the preceding SQS implementation turn.
