# Log viewer overhaul POC

The selected direction is A's activity pulse with optional group comparison, integrated into the existing LogsSection. The native table, query bar, sort, tail, pins, paging, formatted messages, resizable details and trace links remain the actual components used by the viewer.

Open `http://localhost:4173/?logs-poc=1#logs` (the selected log viewer), or run `cd ui && bun run logs:poc`. The development query opens All log groups directly; the same activity component is used by the normal logs section. The old `variant` query no longer chooses a separate table or group-focus layout.

## Selected design

- A quiet activity overview sits above the native log table. Thick rounded bars with a one-pixel gap represent loaded event volume; red ticks and amber dots indicate errors and warnings.
- Scrolling the table updates a soft background wash on the timeline. Its bottom border and the legend's underline are removed. A separate vertical line marks a selected event.
- Compare groups replaces the overview marks in the same space. It shows the current selection's loaded groups, ordered by errors then volume, with at most six groups. Single-group views disable comparison. Clicking a populated bucket navigates to that group's row within the current table; it does not change the query scope.
- The timeline stays full width while native details open beside the table. Native row selection and closing details preserve scroll position. Summary and pins-only modes use their existing UI without this activity overview.
- The header's back arrow closes an open log detail first and returns to the same list position. From the list, the next back click returns to log groups. Its accessible label follows the destination.
- Counts describe loaded rows, not the full retained store. Local time matches the existing table. Full-range metrics still need the backend work assessed below.

The styling follows Home's open sections, small labels, quiet controls and closely spaced volume bars. The selected design removes the permanent group rail and the POC's replacement search, severity controls and inspector. Group scope uses the viewer's existing group browser and selection controls.

The earlier standalone variants remain in `logs-prototype.svelte` and `prototype-switcher.svelte` as review source, but the application no longer renders them. They can be archived with the POC when preparing a commit. The selected component is `logs/log-activity.svelte`; its integration is in `sections/logs-section.svelte`. Backend files are unchanged by this work.

For deterministic browser review, `ui/src/testing/log-activity.browser.js` mounts the real LogsSection with synthetic API responses on a blank dev-server page. It verifies native controls, scroll tracking, the absence of the underline, native details, full-width timeline placement and comparison navigation. The helper can also mount the native viewer with demo rows for screenshots. Synthetic responses are confined to the browser review session and restored on cleanup.

## Reproduced selection jump

`ui/src/testing/log-selection.browser.js` mounts the real LogsSection with a deterministic 200-row response, scrolls to row 101 and selects row 106. Run in the collaborative browser on the dev server:

```js
await (await import('/src/testing/log-selection.browser.js')).checkLogSelection()
```

Before the layout fix, the result was `Selection moved stream: {"before":2400,"after":0,"sameElement":false}`. LogsSection rendered its stream snippet in different conditional branches depending on whether details were open. Opening details destroyed and recreated LogStream.

The left Pane and stream now stay mounted; only the detail Pane and resize handle are conditional. The same check passes with `before:2400`, `after:2400`, `sameElement:true`, and checks closing details too. This fixes the reproduced selection/close case. Other live-ingestion and pagination edge cases need the production checks described below.

## Frontend assessment

- `ui/src/lib/components/sections/logs-section.svelte` owns group selection, filters, cursor pages, polling, pins, traces and details. Ordinary pages default to 200 events. Live polling runs every 2 seconds and merges into a maximum 5,000-row buffer.
- `ui/src/lib/components/logs/log-stream.svelte` virtualizes fixed 24px rows and already anchors on a row key when arrivals shift the buffer. The optional viewport callback added for the POC reports the real visible range, rather than the overscan range. Its optional UTC display leaves the normal viewer's local time default intact.
- The uncommitted `log-histogram.svelte` derives 60 buckets from `displayEvents`. Its domain and counts change with the loaded page; it has no viewport input or explicit group lanes. Its apparent incidents are error-run heuristics, not backend incident data. A full-range chart cannot use this page data as its denominator.
- `keyEvents` derives identity from timestamp, stream, source and message, then adds an occurrence suffix. Exact duplicates can change occurrence identity across page or live-buffer boundaries. A stable server event ID would give selection, pins and anchors a stronger basis.
- The frontend API types expose group, severity, pattern, stream, cursor and order. They do not expose start/end bounds for raw event reads. A production overview should own an explicit shared query scope independently of list pagination and visible rows.
- API requests use the active account. Preserve account-scoped fetching, cancellation and component remounting when switching accounts. In production, store scoped query parameters in navigation history and distinguish overview range, list viewport and selected event.

## Backend and Tarn process assessment

| Path | Current behaviour | Implication for the overhaul |
| --- | --- | --- |
| `internal/logs/store.go`, `service.go` | Each account has an in-memory store, default 10,000 events per group. Group and stream metadata accompany retained events. | Metrics must disclose retained coverage and evictions; this is not persistent log history. |
| `internal/api/admin/handler.go` | Raw events expose timestamp cursors and totals. Cursor filtering is strictly before or after a timestamp. Raw handlers do not parse start/end bounds, although LogFilter supports them. | A page boundary through equal-timestamp events can skip the remaining ties. Add an event ID plus compound cursor and time bounds before relying on historical timeline navigation. |
| `Store.GetAllLogEvents` | Scans retained events under a read lock, prefixes the group into StreamName, then sorts by timestamp. Group iteration comes from a map; ties have no stable secondary ordering. | Return groupName separately, preserve streamName, and sort by timestamp plus stable identity. Keep the old response compatible during migration. |
| `internal/logs/summary.go` | Groups structured messages by a field, including cross-service correlation. It has scan limits and reports truncation. | Reuse filtering and truncation conventions, but keep time-bucket metrics separate from entity summaries. |
| `internal/lambda/service.go`, `internal/logs/service.go` | Reads Docker output since a per-container cursor around invocation/startup paths; parses timestamps and classifies runtime/output lines. Writes function lifecycle events to `/tarn/system`. | Lambda visibility follows its collection points. Keep event time and ingestion time distinct when comparing it with continuously streamed sources. |
| `internal/ecs/runner.go` | Follows container output continuously into the configured group/stream; uses ingestion time; detects severity and upgrades otherwise unclassified stderr to ERROR. | Error shares depend on classification and collection path. Show source context and avoid calling every ERROR an incident. |
| `internal/api/datadog.go` | Account-resolved service logs enter `/services/{service}` with provided timestamps and trace/span IDs. | Preserve service and trace context in metrics drill-downs. |
| `internal/api/server.go` | AWS requests become account-scoped `/tarn/api` events. Dashboard `/_tarn/` requests are excluded from stored API activity. HTTP requests also go to the process logger. | Separate application/service activity from Tarn API activity. Dashboard polling should not create its own stored chart traffic. |
| `internal/cli/server.go` and other Go process logging | Startup, account initialization, warnings and log-pump failures also use the standard process logger. These calls are not all mirrored into the per-account log store. | The dashboard is not a complete view of Tarn process diagnostics. If that is required, define routing for server-wide events and account-specific events explicitly. |
| `ui/src/lib/state.svelte.ts`, `Handler.PruneLogs` | Dashboard refresh triggers pruning at most once a minute, using its retention setting; the endpoint prunes logs and traces. | Time-based retention currently depends on dashboard activity. Move scheduling to the server and expose effective retention independently of whether a browser is open. |

The timestamp-cursor and equal-timestamp-order observations are implementation findings. They are separate from the browser-reproduced selection jump and are not presented as its cause. Backend files remain unchanged by this work.

## Proposed production scope, after choosing a layout

1. **Reader stability and scope.** Keep the mounted reader; define one query with account, groups, time window, levels, pattern and stream. Add stale-response protection for trace/detail reads and preserve anchors across paging and live arrivals. Scrolling updates the viewport indicator only; selecting updates the detail marker only; explicit timeline navigation moves the reader.
2. **Stable event identity and paging.** Assign account-local immutable IDs at ingestion and return groupName explicitly. Use timestamp plus ID for ordered paging, selection and anchors. Return an explicit result if an anchor has been evicted. Keep existing timestamp cursors working during migration.
3. **Bucket aggregation.** Add a proposed read-only `GET /_tarn/admin/logs/metrics` using the same scope as the list and a bounded bucket count. Return requested bounds, interval, per-bucket level counts, per-group lanes, matching totals, retained bounds and truncation/coverage flags. Aggregate before pagination. Define bucket boundaries as half-open, with a documented final-edge rule, and keep empty buckets. Snapshot or copy bounded candidates under the store lock, then aggregate outside it where practical.
4. **Time navigation.** Expose raw-event start/end bounds, then let timeline navigation fetch context around a chosen time or event ID. Keep full-range counts separate from loaded-row counts. Freeze the overview domain while browsing; extend it only under explicit live-follow behaviour.
5. **Process coverage and retention.** Decide whether the viewer includes server diagnostics, then route structured events without cross-account leakage or recursive logging. Make retention a server responsibility and report effective limits. Persistent storage is a separate choice if history must survive restarts.

Before implementation is considered ready, verify equal-timestamp pagination without gaps or duplicates, ascending and descending live arrivals while away from the edge, account switches, stale trace responses, ring eviction of an anchor, filter parity between metrics and events, empty time buckets, and keyboard timeline navigation. Limit or collapse group lanes for large group sets. Request rate and p95 latency should appear only if structured request/duration data supports them; the POC deliberately uses event counts and severity markers.

The user selected A with optional comparison and the native log table. The frontend component is now integrated into that viewer. No commit, PR or backend implementation was created; the earlier variants remain available as review evidence.

## Validation

- Back navigation regression: `checkLogSelection({ closeWithBack: true })` reproduced the old behaviour with `Back opened log groups instead of the log list`. The contextual handler now passes, preserves the same reader at scroll position 2,400, and verifies that a second back click returns to groups. The detail close button passes the same position check.
- Selected native integration: `checkLogActivity()` passed for native controls, scroll-driven highlighting, no underline, native details without a scroll jump, a full-width timeline during detail inspection, comparison without resizing the reader, and bucket navigation back into the native table.
- `bun run check`: zero errors and warnings.
- `bun run build`: passed.
- Real LogsSection browser regression: selection and closing details preserve the element and scroll position; opening details reduces the reader width as expected.
- Existing live-tail browser checks: ascending, descending and multiple-group cases passed, including new arrivals, display order and filters. These checks ran on a temporary blank dev-server page, as their harness requires, to avoid the dashboard pulse contaminating intercepted requests. That page was removed afterwards.
- Earlier prototype browser checks: scrolling changes the timeline window; selection and closing preserve position; switching variants preserves position and updates the URL; severity filtering returns 32 fixture errors; missing searches show the empty state; timeline clicks move the reader. Group selection returns 120 matching fixture rows without narrowing the reader. Opening comparison preserves reader position and desktop height; clicking a lane opens the correct group and time.
- The earlier revised layouts fit the tested 900px-wide browser without page overflow. The revised pulse was also visually checked in light and dark mode. Current-account snapshot requests succeeded, including the empty-retention case. Phone-sized layouts and a high group count still need production design work.
