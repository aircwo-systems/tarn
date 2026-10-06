# Datadog traces and logs from external applications

Tarn accepts traces from existing Datadog tracers while applications continue
running in Docker Compose or elsewhere. An application using `dd-trace` does
not need source changes. Enable and configure the tracer at process startup:

```yaml
environment:
  DD_TRACE_ENABLED: 'true'
  DD_TRACE_AGENT_URL: http://host.docker.internal:4571
  DD_TRACE_AGENT_PROTOCOL_VERSION: '0.4'
  DD_SERVICE: candidate-frontend
  DD_TAGS: tarn.account_id:123123123123
  DD_LOGS_INJECTION: 'true'
  DD_TRACE_TELEMETRY_ENABLED: 'false'
  DD_INSTRUMENTATION_TELEMETRY_ENABLED: 'false'
  NODE_OPTIONS: --require dd-trace/init
```

Use the port where Tarn is running. Host applications can use `localhost`
instead of `host.docker.internal`. Recreate the application container after
changing its environment. Tarn does not take over the container's lifecycle.
Preloading the existing tracer lets it instrument loggers before the
application imports them, including applications that import their logger
before calling `dd-trace.init()`.

## Supported intake

- `GET /info` advertises the supported agent trace protocol.
- `PUT /v0.4/traces` accepts Datadog v0.4 MessagePack or JSON trace batches.
- `POST /api/v2/logs` and `POST /v1/input` accept JSON arrays of log records.

All intake bodies may use gzip. Both compressed and decompressed bodies are
limited to 8 MiB. A trace or log batch can contain at most 10,000 records.
Other Datadog protocols, metrics, profiling and agent telemetry are not
implemented. Configure tracers to use v0.4.

The `tarn.account_id` span tag or `ddtags` log tag selects a twelve-digit Tarn
account. A signed numeric access key or `X-Tarn-Account-Id` header can also
select it. Conflicting selections, invalid IDs and archived accounts are
rejected. Untagged data uses Tarn's configured default account.

Imported traces appear only in their selected account. IDs, parent IDs,
nanosecond timestamps and durations are retained. Chunks from different
processes merge by trace and span IDs; retries do not duplicate spans.
The existing trace retention limit applies. AWS invocation traces recorded
before this integration retain their existing shared visibility.

## Logs and trace context

`dd-trace` emits traces. It does not send console logs to the intake endpoint.
Use a log shipper to forward application output, retaining the IDs injected
by `DD_LOGS_INJECTION`:

```json
[
  {
    "service": "candidate-frontend",
    "hostname": "candidate-frontend-container",
    "ddtags": "tarn.account_id:123123123123",
    "timestamp": "2026-10-06T18:00:00Z",
    "status": "info",
    "message": "Request completed",
    "dd.trace_id": "18446744073709551615",
    "dd.span_id": "18446744073709551614"
  }
]
```

Tarn also reads IDs from a nested `dd` object or a Winston JSON record wrapped
in `message`. IDs must remain strings or exact integers, never floating-point
numbers. Timestamps can be RFC3339 or epoch milliseconds. Log events appear
under `/services/<service>` in the account's Logs view, using the existing
bounded in-memory log retention.

Selecting a log with a trace ID shows its matching request and links to
Traces. The Traces view uses actual span timing and parent relationships for
imported requests, and links back to the service's log group. Ambiguous low
64-bit IDs are not linked to an arbitrary 128-bit trace.

Coverage depends on instrumentation in each process. A traced frontend can
record its outgoing API call without the API being instrumented, but spans
inside that API require its own tracer. Browser interactions and rendering
also need browser instrumentation.

## Joining requests to SNS, SQS, and Lambda

When a traced application publishes to Tarn's SNS or sends to its SQS,
Tarn reads the SDK's `_datadog` message attribute and joins its own messaging
spans to that request. This works for external Node services, including ECS
applications, as well as server-side frontends. Other tracers can use the same
Datadog or W3C text-map carrier, but their SDK instrumentation must propagate
it and their trace exporter must use the supported v0.4 intake.

SNS binary attributes and SQS string or binary attributes are supported.
Tarn preserves the carrier through raw SNS delivery, SNS notification
envelopes, and Lambda event source payloads. It retains the full 128-bit
trace ID and attaches native spans to the publishing span in the same Tarn
account. Configure the application's `tarn.account_id` tag to match the
account selected by its AWS SDK credentials.

The resulting trace can show the instrumented request, SNS publish, SQS
send and receive, and Lambda consumer together. Native spans merge even
when the application's trace arrives later. Redeliveries add separate
consumer attempts. A Lambda batch containing messages from several requests
appears in each participating trace, with a shared invocation ID and that
request's message IDs in its span details. Messages without valid context
keep their existing standalone traces.

This joins spans in Tarn's trace store; it does not export Tarn spans to a
Datadog agent. Browser clicks require browser instrumentation and context
propagation to the server. Java SDK integration has not been verified yet.
