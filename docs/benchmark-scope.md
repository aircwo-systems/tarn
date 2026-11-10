# Benchmark scope

Status: Proposed, for discussion\
Date: 2026-10-17\
Direction: Local Tarn workloads, with Lambda as the first target

## Product intent

Benchmark lets a developer run a repeatable local workload, inspect the work it performs, and compare it with an earlier run. Its most useful result is evidence that explains a change: more dependency calls, a slower startup, a new retry, or a longer invocation.

The primary question is "what work does this code perform, and what changed?" Local elapsed time supports that investigation. Benchmark does not predict deployed AWS latency, scaling capacity, or the best AWS memory allocation.

Example: run `process-order` against a fixed fixture before and after a change. The comparison shows that observed DynamoDB reads increased from 2 to 31 per invocation. The developer opens a representative trace to inspect the repeated operations. The call increase is useful evidence even when the laptop's timings differ from AWS.

Call counts still depend on the input, cache state, retries, and instrumentation coverage. Comparisons must preserve and display that context.

## Recommended first release

One synchronous Lambda target, a repeatable payload, sequential invocations, saved results, and comparison between runs. Include dependency analysis where spans can be attributed to the invocation exactly. Support basic execution measurements for existing Lambda runtimes; prove detailed analysis with one instrumented Node.js fixture before claiming broader coverage.

### Run a workload

- Select an account, function, and version or alias. Resolve aliases once. Retain the selected immutable deployment for the run; capture `$LATEST` as a deployment snapshot without publishing a version as a side effect.
- Provide one JSON event fixture. Keep its hash and byte size with the results. Fixtures that change downstream data need a developer-managed reset between runs.
- Choose warm-up and measured invocation counts. Suggested defaults are 3 warm-ups and 30 measured invocations, at concurrency 1. These defaults are for exploration, not statistical confidence.
- Show progress and allow cancellation. Bound invocation count and total run duration. Allow one active benchmark per Tarn instance initially to reduce interference.
- Execute normal function behaviour, including downstream writes and external calls. Make this visible beside the Run action. Benchmark does not roll back application effects.
- Exclude warm-ups from measured summaries, but retain their outcomes. Stop on a warm-up failure. Continue measured calls after application errors so the report includes their frequency; stop on target loss or run deadline.

### Report what happened

- Attempted, completed, successful, failed, timed-out, and cancelled invocation counts. A Lambda `FunctionError` is a failure even when the HTTP status is 200.
- End-to-end service duration per invocation, covering deployment acquisition through result or failure. Keep benchmark scheduling and telemetry-flush waits outside this duration.
- Separate acquisition time from the RIE request/response interval. The latter can include runtime initialization and transport overhead; label it accordingly rather than calling it pure handler time.
- Record whether each invocation used an existing environment or created a new one. Keep new-environment and reused-environment samples separate. Warm-up does not guarantee that every later invocation will reuse an environment.
- Show median, p95, min, max, and sample count for successful invocations in each group. Report failure durations separately. Mark small samples as exploratory and avoid p99 in the initial release.
- Link individual samples to their request ID, logs, and any captured trace. Distinguish missing, partial, and available telemetry.
- Record runtime, resolved code/deployment identity, memory limit, architecture, Tarn version, host architecture, and Docker resource allocation where available. Identify cross-architecture execution.

Initial execution may include image preparation. Record that separately when observed. A new local container is a local startup measurement, not an AWS cold-start estimate. Forced fresh-environment runs are a follow-up.

### Explain dependency work

For exactly attributed spans, group by dependency, operation, and resource. Report calls per invocation, failures, and summed observed span durations. Show which invocations supplied the data and whether capture was truncated.

Never derive dependency activity from unrelated traffic in the same time window. Never present missing spans as zero calls. Captured calls may omit SDK-internal retries unless the instrumentation exposes individual attempts.

Summed span durations can overlap or include nested spans. Do not subtract their sum from invocation wall time to infer CPU time, or present the sum as a percentage of wall time. Use existing trace inspection for a timeline where timestamped spans are available.

The first detailed-analysis path should use invocation IDs propagated into one supported tracing path, with a tested Node.js Lambda fixture. Reuse Tarn's existing trace intake where practical. This is a required feasibility check, not an assumption that Lambda dependency tracing already works automatically.

If that check fails, ship execution comparison as a smaller milestone and keep dependency analysis explicitly pending. Do not substitute the anonymous collector for exact attribution.

### Compare runs

Save an immutable run specification, summary, and bounded per-invocation measurements under the account's data directory. Keep benchmark measurements independently of ordinary trace retention. Bound retained runs and samples; document the limits when implementing storage.

Compare a selected baseline with a later run. Present absolute and relative timing differences alongside changes in observed calls and failures. Show both sample counts and any environment or coverage differences. A baseline of zero needs an absolute delta, not an infinite percentage.

Timing comparisons need matching fixture, warm-up policy, environment group, and execution environment. Code changes are expected. Changes to memory, runtime, architecture, dependencies, fixture state, or instrumentation must remain visible. Treat timing deltas as observations; the first release does not claim statistical significance or automatically fail CI.

Persist payload hashes rather than raw events in exported reports by default. Retained trace/log links can expire; expired evidence must not invalidate the saved measurements.

## Interaction

Add a Benchmark entry from Lambda detail and an account-scoped run history. The first screen needs a target, fixture, run settings, and Run. Results need a summary, baseline comparison, invocation list, and links to existing logs and traces.

Use the same server-side runner for UI and CLI. Proposed CLI shape, not an existing command:

```sh
tarn benchmark run --function process-order:7 --payload-file order.json \
  --warmup 3 --iterations 30
tarn benchmark compare <baseline-run-id> <candidate-run-id>
```

An API starts a run and returns a run ID; clients read progress, results, or request cancellation. Closing a browser should not cancel a run. A Tarn restart marks unfinished runs interrupted and does not replay them. MCP can follow once the run/result contract is stable.

## Explicit exclusions from the first release

- Real AWS execution, AWS cost estimates, and AWS memory/CPU tuning recommendations.
- Load generation, rate targets, concurrency sweeps, maximum throughput claims, and saturation testing.
- CPU stack sampling, allocation profiles, flame graphs, and automatic source-level bottleneck claims.
- Container CPU/memory sampling. It needs its own sampling and attribution design and can miss short invocation peaks.
- Forced cold starts, cache resets, automatic data reseeding, and isolated copies of the entire dependency stack.
- SQS-to-Lambda pipeline completion, multi-resource scenarios, and benchmarks of every supported AWS service.
- Scheduling, CI regression gates, automatic fixes, and natural-language performance verdicts.

These can extend Benchmark later. API routes are a reasonable next target once the Lambda measurement and comparison model works. Async pipelines need explicit completion criteria rather than treating message acceptance as completion.

## Existing implementation and gaps

- [`internal/lambda/service.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/lambda/service.go) already acquires immutable deployments and tracks whether container acquisition created a new environment. Its current `REPORT` duration begins after acquisition and is unavailable on some failure paths. Add structured invocation measurements at this service seam rather than parsing log text. Preserve normal invocation behaviour.
- [`internal/engine/invoker.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/invoker.go) considers the RIE ready when TCP connections succeed. Runtime initialization may continue inside the first invocation. Timing labels must reflect that boundary.
- [`internal/engine/container.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/container.go) applies the Lambda memory limit to Docker. Its Lambda container resource configuration does not set a corresponding CPU allocation. A memory sweep would not reproduce AWS's memory-to-CPU relationship.
- [`internal/trace/collector.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/trace/collector.go) broadcasts anonymous dependency spans to all in-flight invocations and keeps collecting for a short period after return. These spans are unsuitable for exact per-invocation benchmark counts, even with a sequential benchmark if other traffic is present.
- [`internal/trace/store.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/trace/store.go) retains ordinary traces for 24 hours with a count cap of 2,000. Benchmark results need their own bounded retention, with optional links to this store.
- [`internal/api/admin/handler.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/admin/handler.go) already exposes direct invocation, traces, and logs. New admin handlers should call a shared benchmark service, not duplicate the measurement logic.
- Tarn accepts imported Datadog traces. That offers a possible detailed-analysis path, but does not establish automatic tracing or context propagation inside every Lambda runtime.

## Build sequence and release checks

1. **Prove the measurements.** Establish structured success and failure measurements at the Lambda service seam. Pin deployment identity across a run. Confirm the timing boundaries using a fixture with known initialization and handler delays. Reuse this measurement record wherever existing invocation summaries need the same values.
2. **Prove attribution.** Connect one instrumented Node.js Lambda to its benchmark invocation. A fixture with a known number of dependency calls must report those calls exactly, even while unrelated invocations and requests run. Uncorrelated spans remain excluded. This determines whether detailed analysis can join the first release.
3. **Build the runner and results.** Add bounded sequential execution, warm-ups, cancellation, account isolation, result storage, interruption handling, JSON export, and baseline comparisons. Verify application failures, deadline expiry, deployment updates during a run, and cancellation without launching additional calls. Cancellation does not promise that already-started handler code or its side effects stop immediately.
4. **Expose the workflow.** Add the Lambda entry point, run history, comparison, and CLI using the same run API. A developer must be able to identify a deliberately introduced call-count regression and inspect its evidence.

The attribution check is the largest uncertainty. A general profiler would introduce separate work for each runtime; it should not enter this estimate through an unqualified promise to "show where time goes."

## Basis for the local/AWS boundary

AWS describes the [Runtime Interface Emulator](https://github.com/aws/aws-lambda-runtime-interface-emulator) as a local runtime API proxy and explicitly excludes Lambda orchestration. [AWS Lambda memory documentation](https://docs.aws.amazon.com/lambda/latest/dg/configuration-memory.html) describes CPU allocation increasing with configured memory. Together with Tarn's current Docker configuration, these support treating Benchmark as local development evidence rather than a predictor of AWS performance.

## Decisions still open

- Is dependency work and regression comparison the central value, or is CPU/allocation profiling essential to the first usable release? The proposal recommends the former.
- Is opt-in runtime instrumentation acceptable for detailed dependency analysis? Basic invocation timing can remain runtime-independent.
- Should the initial delivery include both UI and CLI? The proposal shares the API and stages the UI after measurement and attribution are proven.
