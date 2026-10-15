# Functions backend deep dive

Reviewed 13 October 2026 on `develop-mvp` at `5f2bd17cd286c14a5cb47789d9d1c1d16847b18b`.

The Functions backend has useful existing optimisations, but its largest immediate problems are correctness under concurrent updates and layer execution. All 32 concurrent tag updates succeeded while only two tags survived. Concurrent publishes of one layer reused version numbers. A function with two local layers failed to invoke against Docker with `Duplicate mount point: /opt`.

Two small performance candidates are tested and saved as patches. Counting event records before taking the metrics lock improves the parallel metrics benchmark by roughly 3–4 times. Trying a ready RIE port immediately removes the initial 100 ms polling delay. Neither figure measures total function throughput.

The original review left application code unchanged. Its candidate patches were tested in an isolated archive of the reviewed commit. The implementation below now addresses its findings. The measurements and failing probes later in this document describe the original reviewed commit. Existing unrelated edits were preserved.

## Implementation, 14 October 2026

All seven numbered findings, both small performance candidates, and deployment revision work are implemented:

- Configuration, code metadata, tags, and Pending-state promotion now mutate one atomically persisted function record under a stable per-function lock. Function locks survive deletion/recreation. Lifecycle retirement stays under that lock, and function generations prevent late completions from changing recreated functions' metrics or reusing their environments. Failed ZIP/config preparation retains the last working deployment.
- Layer publication allocates and persists its version under a per-layer lock, stages complete versions before publication, rejects overwrites, and retains its sequence after deletion/restart. Function/layer ZIP traversal shares one private implementation.
- Layers merge in configured order into one immutable directory, mounted once at `/opt`. Attached layer content survives layer deletion. Injected Secrets/DB proxies mount at `/tarn-runtime` to avoid nested mounts beneath read-only `/opt`.
- Parsed resource names and filesystem paths reject traversal and storage symlinks before access. Typed validation errors map to HTTP 400.
- The HTTP adapter no longer owns live alias/policy/version maps. Durable module operations return detached values and deterministic listings, including published versions. Versions, aliases, policies, and reservations persist in the existing readable configuration record under `_tarn`.
- Version/alias qualifiers select immutable code/configuration and distinct container revisions. Caller helpers preserve qualifiers across API Gateway, SNS, EventBridge, event-source mappings, Step Functions, and Cognito triggers.
- Image functions persist their URI/configuration, use image execution defaults, support code-image updates, and pin resolved image IDs. Published image versions remain unchanged after retagging. Reserved concurrency is enforced across a function's versions, including zero; accepted async jobs wait for capacity within the bounded acquisition window rather than being dropped immediately on throttling.
- Cold-start and image-pull gates respect cancellation. Acquisition has a bounded context covering cold preparation/readiness. Warm recovery reacquires using the invocation deadline. Pool release notifications replace polling with subscription-before-check ordering and no notification-channel allocation on the steady warm path.
- Metrics parse record counts before locking. Readiness attempts TCP immediately. Metadata caches validate file identity/size/mtime, and legacy ZIP edits prepare fresh content without changing acquired paths. Revision leases protect invocation and container lifetimes; obsolete revisions retire after Docker confirms removal. Startup prunes abandoned preparation directories while retaining current/published revisions.

Regression tests exercise the module's persistence/preparation interfaces and HTTP operations. Docker tests execute one and two layers, reversed collision order, published versions and aliases, code updates/deletion during running invocations, reservation throttling, queued async work while paused, and image-version immutability after retagging. Existing Secrets extension execution is also checked with the new mount layout. A startup regression removes abandoned preparation directories and verifies that current and published code remain readable.

Implementation and regression coverage:

- [Atomic store and cache](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/store.go), [deployment preparation and leases](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/deployment.go), [layer publication](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/layers.go), and [durable metadata](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/metadata.go).
- [Service regressions](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/regression_test.go), [HTTP regressions](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/lambda/regression_test.go), [pool/image/readiness regressions](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/lambda_revision_test.go), and [Docker execution regressions](https://github.com/aircwo-systems/tarn/blob/develop-mvp/test/functions_regression_e2e_test.go).
- [Updated capability documentation](../services/lambda.md).
- [Completion audit by report requirement](functions-completion-audit.md).

Validation passed on the final implementation: full race-enabled `make test`, including the Docker suite, `make lint` with zero issues, `go vet ./...`, formatting, and `git diff --check`. The final Docker suite completed in 44.5 seconds, including the additional single-layer execution regression. The final lifecycle tests also verify that deletion retirement completes before recreation and late old invocations cannot update recreated functions' metrics. See the [implementation validation record](functions-implementation-validation.txt).

Reservations remain subject to the configured local concurrency ceiling. Weighted aliases, account-wide quota enforcement, IAM authorization, and asynchronous retry/DLQ policies remain outside the report's implementation scope. Acquired invocations finish on their original deployment; updates and deletion stop subsequent reuse rather than interrupting running handlers.

## Module and callers

The UI's Functions module corresponds to the Lambda backend:

- [Service](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) owns creation, updates, invocation, metrics, layers, and asynchronous submission.
- [Store](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/store.go) persists configuration and ZIPs, extracts function/layer code, and supplies filesystem paths.
- [Engine](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/container.go) creates Docker containers and owns account-scoped pools. [WarmPool](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/pool.go) reaps idle containers. [Invoker](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/invoker.go) handles readiness and RIE HTTP requests.
- The [Lambda HTTP adapter](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/lambda/handler.go) also owns versions, aliases, and policies in memory. The [admin adapter](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/admin/handler.go) reads function summaries, metrics, environment, event examples, and invokes functions.

API Gateway v1/v2, SNS, EventBridge, event-source mappings, Step Functions, and Cognito triggers call the invocation interface. [Account initialisation](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/cli/server.go#L163) creates separate stores and services while sharing the engine and warm pool. Fixes inside the module therefore reach these callers without changing their interfaces.

Keep the existing account-scoped container keys, exclusive container acquisition, overflow reaping, runtime-image cache and pull serialisation, bounded async workers/backlog, incremental log cursor, and nonblocking trace completion. Configuration reads already return detached objects because they decode JSON from disk. Adding another copy to these reads would increase their cost without addressing the defects below.

The useful new internal seam is a prepared deployment revision, containing matching configuration, code, and merged layers. Callers should continue to invoke by function reference and payload. A generic repository or engine wrapper would not, by itself, fix any reproduced failure.

## Confirmed failures and recommended repairs

### 1. Metadata updates lose successful concurrent changes

[TagResource](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) and UntagResource load a full configuration, mutate it, and save it. The store locks each read and write independently. Configuration updates and code-metadata updates also use separate read/modify/save steps. Atomic file rename protects the JSON file from partial reads, but does not make these operations atomic.

Three rounds of 32 concurrent tag requests each returned success and persisted only two tags. This is a semantic concurrency defect, so the race detector does not need to report it.

Add a store operation that performs load, mutation, and atomic persistence under one stable per-function lock. Use it for tags, untagging, configuration changes, and Pending-state promotion. Merge code metadata into the current configuration rather than saving an earlier detached copy. Serialising full code/deployment publication needs the revision work described below; a metadata callback alone does not solve that problem.

[DeleteFunction](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/store.go) also deletes the function's mutex from its registry. Existing waiters can retain the old mutex while new callers acquire a replacement. The probe confirms the lock identity changes. Keep lock identity stable until users and waiters are gone. Retaining locks until store shutdown is the simplest first repair, with an explicit memory cost per distinct name; safe reclamation can follow if needed.

Regression coverage should require all distinct tags to survive concurrent successful writes, preserve unrelated fields during simultaneous config/code/tag updates, and exercise delete/recreate with queued operations.

### 2. Layer version allocation is separate from publication

[PublishLayerVersion](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) calls NextLayerVersion and then SaveLayer. These acquire the layer lock separately. Multiple publishers can choose the same number and overwrite the same version directory.

The first probe produced three persisted versions from 32 successful publishes. A second run produced two. The saved evidence is the second run.

Allocate and persist a new layer version within one store operation. Commit the ZIP and configuration before exposing that version in listings. Preserve existing version numbers and the current on-disk layout in the first repair. Later, use per-layer or per-version locking so extraction of one layer does not block unrelated layers throughout the account.

Require concurrent publishes to return distinct versions with matching retained code/configuration. Add a failure test proving a failed publish cannot replace a successful version.

### 3. Multiple local layers cannot execute

[CreateContainer](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/container.go) appends one bind mount to `/opt` for every layer directory. Two local layers therefore produce duplicate mount targets.

The Docker probe published two small Node modules, created a function using both, and invoked it. Creation succeeded; invocation returned HTTP 500 with the daemon error `Duplicate mount point: /opt`. Existing TestLambdaLayers covers publication/listing/deletion of one layer, but never executes a function using a layer.

Prepare one merged layer directory and mount it once. Preserve configured layer order, executable permissions, nested files, and managed-external-layer skipping. AWS documents that layer contents merge in configured order, and that layer versions are immutable. See [adding layers](https://docs.aws.amazon.com/lambda/latest/dg/adding-layers.html).

Tests should execute a function using one layer and two layers, verify both independent modules are available, and cover a colliding filename in reversed layer orders. Do not implement merging by mutating the stored layer version directories.

### 4. Resource names can escape their storage root

Function creation checks that the name is nonempty, but passes it into filesystem joins without validating it. The store probe created `../outside` and found `config.json` in `DataDir/outside`, outside `FunctionsDir`. All files used by this probe were inside its temporary test directory.

Validate a parsed bare resource name before filesystem access, including service/store calls that bypass HTTP. Keep ARN parsing separate from path construction. Apply containment checks to function and layer paths, and reject traversal segments and separators. Sanitising only the Docker container name does not protect these paths.

This changes invalid input behaviour, so return a typed validation error and preserve valid name/ARN use. The existing ZIP-entry traversal guard addresses a different input and should remain.

### 5. HTTP responses expose live alias and policy state

[GetPolicy](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/lambda/handler.go) copies a map reference under the handler lock, then checks and iterates that map after unlocking. AddPermission and RemovePermission mutate it. A concurrent read/write probe reports data races in GetPolicy and AddPermission. Unsynchronised Go map iteration also risks a process failure.

Alias create/get/list/update responses similarly encode pointers that UpdateAlias can mutate after the lock is released. A deterministic paused-response probe creates an alias for version `1`, updates it to `2`, then resumes the first response. The create response incorrectly reports version `2`.

Copy policy statements and alias values while holding the lock, then encode detached results after unlocking. Alias fields and policy statement fields are scalar strings, so value copies suffice. Cover all response paths, including create and update, rather than only GET. Holding the lock during HTTP writes would unnecessarily let a slow reader block other requests.

### 6. Published versions are metadata in the HTTP adapter

[PublishVersion](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/lambda/handler.go) records a response map in `h.versions`. [ListVersionsByFunction](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/lambda/handler.go) ignores that map and returns only `$LATEST`. The HTTP probe publishes version `1` successfully, then lists only `$LATEST`.

Code inspection also finds no read of the published-version map, no persistence of versions/aliases/policies, and no qualifier resolution in the direct Invoke path. PackageType Image is echoed in metadata, but ImageURI is excluded from persisted JSON; invocation still extracts a ZIP and selects a predefined runtime image. Reserved concurrency endpoints echo/acknowledge values without enforcing them. The configured container cap is a separate, working mechanism.

These are capability gaps, not performance optimisations. Do not claim that adding version listing makes published-version execution work. Move durable version/alias state into the module, bind each version to immutable deployment content, resolve names/ARNs/qualifiers consistently, and update capability documentation as those behaviours become executable. AWS describes published versions as snapshots of code and configuration in [CreateFunction](https://docs.aws.amazon.com/lambda/latest/api/API_CreateFunction.html).

### 7. Cold-start waiting ignores cancellation while acquiring a mutex

[acquireContainer](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) waits on a plain mutex held throughout Docker creation/start/readiness. Its cancellation select runs only once it reaches the concurrency-cap polling branch. The probe holds that mutex and starts an already-cancelled acquisition. It remains blocked for the full observation window, then attempts cold-start work after the lock is released.

Check cancellation before claiming any environment and use a cancellable per-function start gate. An initial context check fixes the pre-cancelled case, but cannot interrupt cancellation that occurs while queued. Keep the cap invariant intact, recheck idle containers after taking the gate, and avoid one global gate across functions.

At the concurrency cap, poll intervals back off to 250 ms. A release notification could reduce waiting latency and timer churn, but it needs lost-wakeup tests and a mixed burst benchmark before replacing polling. The existing 60-second acquisition bound does not bound time blocked on the mutex or all cold-start work. Recovery also reacquires using the parent request context rather than the invocation timeout context. Those limits should be tested as explicit interface behaviour.

## Measurements and tested small candidates

Go 1.26.0, macOS arm64, Apple M5. Benchmarks used `-cpu=8`, `-benchtime=1s`, three samples, and setup outside the timed region. Filesystem data was local and warm. Runtime varies on this shared machine, so these are development fixtures, not production load measurements.

| Operation | Median time | Allocated bytes per call | Allocations per call |
| --- | ---: | ---: | ---: |
| Warm preparation, no layers | 57.7 µs | 16,936 | 184 |
| Warm preparation, two cached layers | 92.7 µs | 21,608 | 217 |
| List 100 functions | 1.91 ms | 220,873 | 2,220 |
| List 1,000 functions | 15.55 ms | 2,198,175 | 22,026 |
| Parallel metrics recording, current code | 47.9 µs/op | about 22,100 | 116 |
| Parallel metrics recording, actual candidate patch | 12.7 µs/op | 22,096 | 116 |

Warm preparation reads and decodes a function with 50 environment entries, checks its extracted code, and resolves its cached layers. It excludes engine acquisition, Docker HTTP, runtime execution, logs, and metrics. List fixtures have one 64-byte environment value per function. Metrics use an event with 100 records and roughly 14 KB of JSON; the time is elapsed benchmark time divided by completed operations under parallel load.

### Parse metrics before taking the lock

[recordInvocation](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) parses the payload while holding the account-wide metrics lock. This blocks completion recording for other functions and dashboard metric readers.

The [candidate patch](https://github.com/aircwo-systems/tarn/blob/develop-mvp/docs/reviews/functions-metrics-candidate.patch) moves the existing countProcessedMessages call before the lock, leaving counter mutation and timestamp assignment inside. Parsing semantics and allocations remain the same. The actual patched implementation improves the synthetic benchmark by about 3.8 times; the initial in-test prototype improved it by about 3.2 times. Neither improvement should be applied to an end-to-end invocation latency estimate.

Existing Lambda, HTTP adapter, engine, and asyncqueue tests passed with `-race` in the isolated candidate checkout. Scoped `go vet` passed too. Existing message-count cases cover empty payload, generic events, invalid JSON, multiple records, and an explicitly empty Records list.

### Probe readiness immediately

[WaitForReady](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/invoker.go) waits for the first 100 ms tick before trying TCP, even if the port already accepts connections. Three already-listening-port checks took 100.8–102.6 ms.

The [candidate patch](https://github.com/aircwo-systems/tarn/blob/develop-mvp/docs/reviews/functions-readiness-candidate.patch) checks the port immediately, then retains polling and the existing readiness timeout. It also uses DialContext so cancellation can interrupt connection setup. The same probe takes 126–172 µs with the candidate. Existing engine and Lambda tests pass with `-race`, including readiness timeout and invocation timeout tests; scoped vet passes.

This removes an avoidable readiness delay when the port is ready. It does not promise a 100 ms reduction for every Docker cold start, whose runtime and image costs vary.

### Cache only after deployment state has a consistent revision

Every warm invocation rereads JSON, stats ZIPs, reads extraction markers, and acquires exclusive extraction locks. Layer extraction uses an account-wide lock even for cache hits. The [CPU profile](functions-cpu-profile.txt) of two-layer warm preparation is dominated by filesystem syscalls. The [allocation profile](functions-allocation-profile.txt) attributes about 69% of sampled allocation space to configuration loading. These profiles include fixture setup and do not represent a complete invoke.

A prepared revision can retain detached configuration and extracted/merged paths in memory, invalidating them on committed updates. First decide how direct filesystem edits remain observable; silently caching forever would change the current disk-backed behaviour. Rebuild from persisted state after restart, preserve deterministic listings, and keep response mutation isolation.

The measured preparation cost is modest compared with Docker/runtime execution. At 1,000 functions, dashboard listing costs about 15.5 ms and 2.2 MB of allocations per call, so an indexed metadata cache may be more useful there. No cache candidate or end-to-end throughput improvement was measured in this review.

## Deployment revision work

The extraction probe retains the path returned for code `v1`, updates/extracts `v2`, and reads `v2` through the old path. [ExtractCode](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/store.go) deletes and rebuilds the same directory. The store also writes the live ZIP directly; UpdateFunctionCode changes/extracts code before evicting the existing pool and committing configuration metadata.

The mutable-path behaviour is reproduced. Mixed configuration/code under a concurrent production invocation is an inference from that behaviour and the update ordering, not a complete Docker reproduction in this review.

Prepare code and merged layers in a revision-specific directory, validate preparation, then publish matching metadata atomically. Give acquired containers a revision identity so an earlier snapshot cannot create or reuse an environment for a newer deployment. Keep directories alive while containers use them, and retire old revisions after release. Define update/delete behaviour for running invocations explicitly rather than changing it accidentally during a cache optimisation.

Keep revision preparation private to the module. Extract the duplicated function/layer ZIP traversal into a common private helper when introducing staged publication, preserving ZIP traversal checks, directory modes, executable files, nested entries, marker behaviour, and managed-layer handling. Failure-path tests must show that an unsuccessful preparation leaves the last working deployment intact.

## Implementation order

1. Validate filesystem-bound resource names and return typed validation failures. Preserve valid names and supported ARN forms.
2. Return detached alias and policy responses. Add concurrent HTTP regression tests.
3. Apply the tested metrics and readiness candidates as small performance changes.
4. Make configuration mutations atomic, keep function lock identity stable, and combine layer version allocation/publication. Cover concurrent callers and failure rollback.
5. Compose one ordered layer directory and execute one/two-layer functions against Docker, including overlapping paths and sidecar injection.
6. Add revision-specific code/layer preparation and a cancellable start gate. Only then consider caching metadata/preparation and release notifications.
7. Address durable versions, qualifier/alias invocation, image functions, and reserved-concurrency semantics as separately scoped functionality work.

The first five steps fix current behaviour without redesigning caller interfaces. Keep persistence-layout changes and revision migration in their own later change.

## Validation and reproduction artifacts

Existing tests passed for Lambda, the Lambda HTTP adapter, engine, and asyncqueue with `-race -count=1`. Seven existing Docker end-to-end tests passed for Node, Python, DryRun, config updates, tags, sequential reuse, and layer metadata. The new Docker timeout-recovery probe also passed: a three-second invocation timed out, then a zero-delay invocation succeeded in roughly 82 ms. The suspected next-invocation timeout failure was not reproduced and is excluded from the findings.

The new failure probes deliberately fail against the reviewed implementation. They are saved as text so normal tests remain runnable. No full-repository test/lint result is claimed for this review.

- [Store/service probes](functions-evidence_test.go.txt) and [failure output](functions-correctness-evidence.txt).
- [HTTP probes](functions-http-evidence_test.go.txt), [snapshot/version output](functions-http-correctness-evidence.txt), and [policy race output](functions-policy-race-evidence.txt).
- [Docker probes](functions-docker-evidence_test.go.txt) and [two-layer failure plus timeout-recovery output](functions-docker-evidence.txt).
- [Readiness probe](functions-readiness-evidence_test.go.txt), [baseline output](functions-readiness-evidence.txt), and [candidate test output](functions-readiness-candidate-tests.txt).
- [Baseline benchmarks](functions-baseline-bench.txt), [actual metrics candidate benchmarks](functions-metrics-candidate-bench.txt), [candidate tests](functions-metrics-candidate-tests.txt), and [candidate vet output](functions-metrics-candidate-vet.txt).
- [Existing baseline tests](functions-baseline-tests.txt) and [existing Docker test output](functions-baseline-docker-tests.txt).

To reproduce, extract the reviewed commit into a temporary checkout, copy the appropriate `.go.txt` probe to its original package as a `_test.go` file, then run its named tests. Do not run the known-failing probes as a normal regression suite. Benchmarks live in the store/service probe file and run with:

```sh
go test ./internal/lambda -run '^$' -bench '^BenchmarkReview' -benchmem -benchtime=1s -count=3 -cpu=8
```

Apply the candidate patch in that temporary checkout to compare its behaviour. The metrics benchmark labelled `ParseBeforeLock=false` calls the actual Service method, so it becomes the candidate measurement after applying the patch. The `true` benchmark is only the initial in-test prototype. Run Docker probes with `go test ./test -run '^TestReviewFunction' -count=1 -v`; they use the existing TestMain to start a temporary Tarn instance and clean up their functions/layers.
