# Sidebar summaries

Drag Tarn's sidebar to 340px or wider to reveal focused information below selected navigation items. At the usual 256px width, it stays a compact list. The same edge control supports keyboard resizing. The sidebar remembers the width.

| Section   | Information                                                       | Source                                                 |
| --------- | ----------------------------------------------------------------- | ------------------------------------------------------ |
| Overview  | Resource count and last successful refresh                        | Filtered dashboard counts and refresh state            |
| Functions | Active functions and recorded invocations                         | Functions in the current dashboard filter              |
| Queues    | Approximate waiting and in-flight messages                        | Queues in the current dashboard filter                 |
| Services  | Reachability blocks and function links                            | Visible infrastructure probes and inferred connections |
| Logs      | Activity over 60 seconds, sampled errors and warnings, last event | Existing app-wide log pulse                            |
| Traces    | Recent request count and responses with status 500 or higher      | Dashboard's recent trace sample                        |

Each Services square represents one probe and uses the same service-kind colour as the Services list and topology. Squares sit beside the reachable count; the function-link count sits directly below it. Unreachable services are dimmed. A small mark indicates that a function uses the service. Hover a square for its name, state, latency when connected, and number of linked functions. Selecting the row opens Services. Connection IDs match the Services screen and topology.

Functions and Queues use the same two-row layout and 8px squares. Each function has a square beside the active count: green for active, red for failed, and dimmed amber for other states. Invocations sit below. Each queue has a square beside waiting messages: amber when it has waiting, in-flight, or delayed messages, dimmed when idle, and red when messages are stale or its disruptor is armed. In-flight messages sit below. Hover a square for that resource's name and details. Summary rows can supply a `blocks` array to reuse this pattern in another section.

The log pulse uses the existing four-second poll of the latest 200 log events. The counts describe this sample, not an exhaustive total. Its activity chart spans 60 seconds; error and warning counts cover sampled events from the last five minutes. A failed read shows an unavailable message and the last successful sample time. It does not present a failed read as zero errors.

Widgets add no requests or timers. Dashboard polling and the existing log pulse provide their data. Service states become visibly stale if dashboard refresh fails. Summaries follow the selected account and the same resource filters as the navigation counts.

`ui/src/lib/components/layout/sidebar-widget.svelte` renders the small set of widget shapes. `ui/src/routes/+page.svelte` supplies their data, and `app-sidebar.svelte` handles width disclosure. A resize observer keeps the active navigation pill aligned as widget heights change. Widget transitions respect reduced motion.

Prefer one or two useful figures per section. Add a summary only when it helps decide where to go next. Keep detailed logs, service actions, and configuration in their sections.
