# Log summary PRD

Status: Proposed  
Target: Operator tooling (MCP and admin API; dashboard later)  
Primary area: `internal/logs`, `internal/api/admin`, `internal/cli/mcp`

## 1. Summary

Tarn should return a grouped summary of log events, not only raw lines. The caller names a JSON
field to group by, such as `correlationId`, `certificateId` or `message`. Tarn returns one entry
per value with counts per level, each error in full, the first and last event, and only the
fields the caller asked for.

The main consumer is an AI agent using `tarn mcp`. It needs to answer questions like "what
happened to each message in this run?" without pulling hundreds of raw lines into its context.
The same endpoint serves `curl` and, later, the dashboard.

## 2. Problem

Tarn already stores log events for Lambda functions, ECS tasks and Tarn itself. It can filter them
by group, level, substring pattern, stream, order, limit and cursor
(`GET /_tarn/admin/logs/events-all`, `LogFilter` in `internal/logs/store.go`). `tarn_get_logs`
exposes this and withholds runtime noise.

What callers get back is still raw lines. In a real session (an archival pipeline: EventBridge →
ECS task → SQS → Lambda → six downstream APIs) the agent had to:

- fetch every event for the run,
- parse each structured JSON `message` itself,
- throw away most fields and shorten nested objects to one value,
- group lines by certificate by hand to build a per-message outcome table,
- pick out the one error among dozens of info lines.

Each of these steps cost tokens and a hand-written filter script. The information the agent needed
was small: 3 certificates, 3 outcomes, 1 error. Getting it meant paying for everything. A larger run,
such as a go-live backlog with thousands of messages, can't be handled this way at all.

## 3. Product intent

The primary user is an AI agent, or a developer, who has just triggered a flow in Tarn and wants to
know what happened. Typical questions:

- What was the final outcome for each message or entity?
- Which entities had errors, and what were those errors?
- What kinds of log line appeared, and how many of each? ("Did anything unusual happen?")
- For one correlation ID, what did every service log?

One call should return a compact answer, sized to the number of groups rather than the number of
lines, with errors never hidden.

`tarn_get_traces` already gives the hop-by-hop timeline for a correlation ID. This feature adds what
was logged at each hop, summarised. It does not replace traces.

## 4. Scope

### In scope

- A new admin endpoint that returns grouped summaries.
- Grouping by a top-level or dotted-path JSON field in structured (JSON) log messages.
- Projection of selected fields.
- Optional flattening of nested `{ "kind": ... }` style objects to their scalar value.
- Reuse of every existing filter (`groups`, `level`, `pattern`, `stream`, `cursor`) and a new
  `since` / `until` time window.
- A new MCP tool `tarn_summarize_logs`, or a `groupBy` mode on `tarn_get_logs` (see section 8).
- Bounded output: cap on groups, cap on errors per group, and explicit counts of what was dropped.

### Out of scope

- Dashboard UI. It is a natural follow-up, but this PRD only asks for the API to be shaped for it.
- Parsing non-JSON formats (logfmt, key=value). Non-JSON lines are counted, not grouped (see 5.4).
- Persisted or saved queries.
- Aggregations beyond counts: no sums, percentiles or histograms.
- Changes to log ingestion or storage.

## 5. Behaviour

### 5.1 Endpoint

```
GET /_tarn/admin/logs/summary
```

| Parameter | Required | Description |
|---|---|---|
| `groupBy` | yes | JSON field to group by. Dotted paths allowed: `outcomes.certificate.kind`. The special value `message` groups by the log line's own `message` field (the log "type"). |
| `fields` | no | Comma-separated fields to keep in each sample event (dotted paths allowed). Default: `message`, the `groupBy` field and `level`. `*` keeps everything. |
| `flatten` | no | Name of a key, typically `kind`. Any object value with that key is replaced by that key's value, so `{"kind":"deleted","statusCode":204}` becomes `"deleted"`. Applied after `fields`. |
| `groups`, `level`, `pattern`, `stream` | no | Same meaning as on `events-all`. `level` filters the events considered; group level counts still come only from matching events. |
| `since`, `until` | no | RFC3339 bounds on event timestamp. |
| `maxGroups` | no | Default 50, cap 500. |
| `maxErrorsPerGroup` | no | Default 5, cap 50. |
| `includeRuntime` | no | Same as `tarn_get_logs`: runtime chatter excluded by default. |

### 5.2 Response

```json
{
  "groupBy": "certificateId",
  "window": { "from": "2026-09-29T18:11:53Z", "to": "2026-09-29T18:12:04Z" },
  "totals": {
    "eventsScanned": 48,
    "eventsGrouped": 31,
    "ungrouped": 17,
    "groups": 3,
    "groupsReturned": 3,
    "errorGroups": 1
  },
  "groups": [
    {
      "key": "00000000-0000-4000-8000-00000000000c",
      "count": 3,
      "levels": { "INFO": 2, "ERROR": 1 },
      "logGroups": ["/aws/lambda/hce-lambda-certificate-archival-handler"],
      "firstAt": "2026-09-29T18:12:03.98Z",
      "lastAt": "2026-09-29T18:12:03.99Z",
      "errors": [
        { "timestamp": "…", "message": "HRT_PPC citizen holds certificates of another type", "otherTypes": ["LIS_HC2"] }
      ],
      "errorsDropped": 0,
      "first": { "timestamp": "…", "message": "…" },
      "last": {
        "timestamp": "…",
        "message": "certificate archival finished",
        "outcomes": { "certificate": "blocked", "citizen": "blocked", "notes": "deleted" }
      }
    }
  ],
  "ungroupedSample": [
    { "timestamp": "…", "logGroup": "/ecs/hce-data-archival-service", "message": "Datadog tracing enabled: false" }
  ]
}
```

### 5.3 Grouping rules

- An event is grouped only if its `message` parses as a JSON object and the `groupBy` path resolves
  to a scalar (string, number or boolean). Numbers and booleans are turned into strings for the key.
- Events where the path is missing, or the message isn't JSON, are counted in `totals.ungrouped`.
  Up to 5 of them are returned in `ungroupedSample`. They are never silently dropped. The consumer
  is often a model that cannot ask what was withheld.
- Groups are formed across all selected log groups, so one `correlationId` joins ECS and Lambda
  output.
- `groupBy=message` groups by the structured `message` field when the line is JSON. Otherwise it
  groups by the whole raw line, cut to 200 characters.

### 5.4 Ordering and bounds

- Groups are sorted with error groups first (by error count, descending), then the rest by
  `lastAt` descending.
- `maxGroups` caps the list. `totals.groups` versus `totals.groupsReturned` shows how many were cut.
- `errors` holds up to `maxErrorsPerGroup` ERROR events, oldest first, with `errorsDropped` counting
  the rest. WARN events are counted in `levels` but not listed, unless `level=WARN` is requested.
- `first` and `last` are always included, even when they are info lines. `last` is usually the
  outcome line.
- Every sample event (`errors`, `first`, `last`, `ungroupedSample`) is projected with `fields` and
  `flatten`, and always keeps `timestamp`.

### 5.5 Errors

- Missing `groupBy`: HTTP 400 with an explanation.
- An invalid path, e.g. an empty segment: 400.
- `since` after `until`: 400.
- Returning no groups is not an error. Return an empty `groups` list with the totals filled in.

## 6. MCP tool

Tool name: `tarn_summarize_logs`, read-only.

Input mirrors the endpoint, plus `function` and `logGroup` shortcuts like `tarn_get_logs`. With
neither given, it searches all groups, which is the useful default for pipelines.

The description should steer the model:

- Use this first after triggering a flow ("what happened to each X?"). Then use `tarn_get_logs`
  with a `pattern` to drill into a single group.
- Group by `correlationId` for "one run end to end", by an entity ID for "one outcome per entity",
  or by `message` for "what kinds of thing happened".
- Mention `flatten: "kind"` for outcome-style logs.
- Point at `tarn_get_traces` for hop timing.

## 7. Acceptance criteria

1. **Grouping by entity.** Given lambda log lines that are JSON with a `certificateId` field,
   `groupBy=certificateId` returns one group per certificate. Each group has the correct `count`,
   `levels`, `firstAt`/`lastAt`, and `last` event.
2. **Errors in full.** Given a group with ERROR events, `errors` contains them oldest first, cut to
   `maxErrorsPerGroup`, with `errorsDropped` correct. Error groups sort first.
3. **Cross-service join.** Given ECS and lambda lines sharing a `correlationId`,
   `groupBy=correlationId` returns one group whose `logGroups` lists both.
4. **Dotted paths.** `groupBy=outcomes.certificate.kind` groups by the nested value.
   `fields=outcomes.citizen.outcome.kind` keeps just that path, rebuilt inside its parent objects.
5. **Flatten.** With `flatten=kind`, `{"kind":"deleted","statusCode":204}` becomes `"deleted"`
   everywhere in the sample events, including inside arrays. Objects without `kind` are unchanged.
6. **Ungrouped lines are visible.** Non-JSON lines, and JSON lines missing the path, are counted in
   `totals.ungrouped` and sampled in `ungroupedSample` (up to 5).
7. **Filters compose.** `pattern`, `level`, `groups`, `stream`, `since` and `until` narrow the
   events considered, exactly as on `events-all`. Runtime chatter is excluded unless
   `includeRuntime=true`, using the same markers as `isRuntimeNoise`.
8. **Bounds.** More groups than `maxGroups` returns exactly `maxGroups` groups and a correct
   `totals.groups`. Parameter caps are enforced.
9. **Validation.** Missing or invalid `groupBy` and a reversed window return 400 with a message.
   No matches returns 200 with empty `groups`.
10. **MCP.** `tarn_summarize_logs` calls the endpoint and returns its result as structured output.
    With no `function` or `logGroup` it searches all groups. The tool is annotated read-only.
11. **Account scoping.** The endpoint is account-resolved like the other admin log routes.
12. **Size.** For the reference scenario in section 9, the summary is under 3 KB, compared with
    roughly 20 KB of raw events.

## 8. Design notes

- **Where it lives.** Put grouping in `internal/logs` (for example `Summarize(filter, SummaryOptions)`)
  so the admin handler, the MCP tool and a future dashboard share one implementation. The admin
  handler sits next to `AllLogEvents` in `internal/api/admin/handler.go`. Register the route in
  `internal/api/server.go` beside `/_tarn/admin/logs/events-all`.
- **Reuse the filter.** Build the candidate set with the same `LogFilter` path that
  `GetAllLogEvents` uses, then group in memory. Local log volumes make this fine. Add a hard scan
  cap (for example 50 000 events) and report `truncatedScan: true` if it is hit.
- **JSON parsing.** Parse each candidate message at most once. Skip parsing when the message
  doesn't start with `{`. Resolve dotted paths without reflection-heavy libraries.
- **Separate tool or `groupBy` on `tarn_get_logs`?** Prefer a separate tool. Its output shape is
  different, and a separate tool with its own description tells the model when to summarise and
  when to read raw lines.
- **Runtime noise.** Reuse `isRuntimeNoise` from `internal/cli/mcp/logs.go`, moving it into
  `internal/logs` if the server side needs it.

## 9. Reference scenario (for tests and docs)

Archival run: an EventBridge rule fires an ECS task, which publishes 3 certificate messages to SQS.
A Lambda processes them and calls downstream APIs. Lambda log lines are JSON from AWS Lambda
Powertools, for example:

```json
{"level":"info","message":"certificate archival finished","certificateId":"…000c","citizenId":"…","correlationId":"45fd…","outcomes":{"certificate":{"kind":"blocked","reason":"crossTypeCertificatesHeld"},"citizen":{"citizenId":"…","outcome":{"kind":"blocked"}},"notes":{"kind":"deleted","statusCode":204}}}
{"level":"error","message":"HRT_PPC citizen holds certificates of another type","certificateId":"…000c","otherTypes":["LIS_HC2"]}
```

Expected calls:

- `groupBy=certificateId&flatten=kind&fields=message,outcomes,otherTypes` returns 3 groups. The
  `…000c` group comes first, with 1 error, and its `last.outcomes.certificate` is `"blocked"`.
- `groupBy=message` returns counts per line type: `certificate archival finished` ×3,
  `HRT_PPC citizen holds certificates of another type` ×1, `certificate archival batch completed` ×1,
  plus the ECS lines.
- `groupBy=correlationId` returns one group spanning `/ecs/hce-data-archival-service` and
  `/aws/lambda/hce-lambda-certificate-archival-handler`.

## 10. Follow-ups (not this PRD)

- A dashboard "Summary" tab on the logs view, using the same endpoint.
- `since=lastInvoke` or `since=<rule fire id>` shorthands, so "the run I just triggered" needs no
  timestamps.
- Showing queue depth and DLQ counts next to the summary for queue-driven flows.
