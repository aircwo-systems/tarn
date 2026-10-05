# Component guide

Reusable components live in `src/lib/components/`. Worker-specific examples live in `src/lib/demo/components/`. The reusable components do not import the worker model or sample store.

## Interfaces

| Component            | Main inputs                                                               | Responsibility                                                 |
| -------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------- |
| `AppShell`           | `navigation`, `children`, optional `settingsHref`                         | Rounded inset frame and shell keyboard controls                |
| `Sidebar`            | Navigation and bound collapsed/mobile state                               | Resizable detail column, centered icon rail, theme dock        |
| `SettingsLayout`     | `sections`, `section` snippet                                             | Sticky submenu, section jumps and scroll tracking              |
| `ResourceLayout`     | `label`, `items`, `selectedId`, `onselect`, `children`, bound `collapsed` | Vertical resource submenu that collapses into horizontal pills |
| `SectionSkeleton`    | No inputs                                                                 | Static generic layout recommendation                           |
| `PageHeader`         | Title, description, optional actions/data label                           | Compact heading and sidebar toggle                             |
| `TopologyCanvas`     | Nodes, edges, selection, bound expanded state                             | Dotted connection canvas with draggable nodes                  |
| `Button`             | Native button attributes, `variant`, `children`                           | Consistent action treatment                                    |
| `StatusBadge`        | `label`, `tone`                                                           | A named state with color and a dot                             |
| `MetricStrip`        | `metrics`                                                                 | A divided set of operational measurements                      |
| `Sparkline`          | `values`, `label`                                                         | A small historical line with a text alternative                |
| `Panel`              | `title`, optional description/actions, `children`                         | Section heading and composable content                         |
| `ResourceTable<Row>` | Rows, columns, cell snippet, optional selection                           | Semantic inventory table                                       |
| `StateView`          | A loading, empty, or error state                                          | A bounded message with a retry action when needed              |

All components use Svelte 5 runes, callback props, and snippets. Use the native button attribute types rather than introducing a separate event API.

## Compose a section

```svelte
<script lang="ts">
  import Panel from "$lib/components/Panel.svelte";
  import Button from "$lib/components/Button.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";

  let paused = $state(false);
</script>

<Panel title="Scheduler" description="Control the next batch of work">
  {#snippet actions()}
    <StatusBadge
      label={paused ? "Paused" : "Ready"}
      tone={paused ? "warning" : "success"}
    />
  {/snippet}
  <Button onclick={() => (paused = !paused)}
    >{paused ? "Resume" : "Pause"}</Button
  >
</Panel>
```

`Panel` defaults to Tarn's rack section: a quiet border, 12px radius, and 16px by 18px padding. Use `flat` for an overview section that should share the surrounding workspace.

## Render domain data in a table

Rows need stable string IDs. Columns need stable keys and labels. A cell snippet decides how domain values render.

```svelte
<script lang="ts">
  import ResourceTable, {
    type Column,
  } from "$lib/components/ResourceTable.svelte";

  type Queue = { id: string; name: string; waiting: number };
  const rows: Queue[] = [
    { id: "q_primary", name: "Primary queue", waiting: 12 },
  ];
  const columns: Column[] = [
    { key: "name", label: "Queue" },
    { key: "waiting", label: "Waiting" },
  ];
  let selectedId = $state("");
</script>

<ResourceTable
  label="Queue inventory"
  {rows}
  {columns}
  {selectedId}
  onselect={(queue) => (selectedId = queue.id)}
>
  {#snippet cell(queue: Queue, column: Column)}
    {#if column.key === "name"}{queue.name}
    {:else if column.key === "waiting"}<span class="mono">{queue.waiting}</span
      >{/if}
  {/snippet}
</ResourceTable>
```

When `onselect` is present, the first cell gets a button with `aria-pressed`. Put a clear resource name in that cell. Do not nest a second interactive element inside it. Without `onselect`, all cells render their content directly.

`hideOnMobile` removes a secondary column at 640px. Put its information in the detail view too. Do not hide the resource name or its operational state.

Empty inventory messages belong beside the table. See the workers route for a filtered empty result and a separate unselected detail pane.

## Model data states

`StateView` takes a discriminated union. A loading state cannot accidentally include an error retry action.

```ts
type ViewState =
  | { kind: "loading"; message: string }
  | { kind: "empty"; title: string; description: string }
  | { kind: "error"; title: string; description: string; onretry: () => void };
```

Render a loaded resource list in the route. Use `StateView` for the other branches. Errors use `role="alert"`; loading and empty messages use `role="status"`. The foundation page lets you inspect all three.

## Collapsible resource submenu

`ResourceLayout` adapts Tarn's resource list and horizontal pill switcher. Each `ResourceMenuItem` has a stable `id` and a `label`, with optional `description`, `badge`, and semantic `tone`. The component takes the selected ID and an `onselect(id)` callback; the consuming screen owns filtering and selection. Render the selected resource's detail content through `children`.

```svelte
<ResourceLayout
  label="Queues"
  items={[
    {
      id: "q_primary",
      label: "Primary queue",
      description: "Delivery",
      badge: "ready",
      tone: "success",
    },
  ]}
  selectedId="q_primary"
  onselect={(id) => selectQueue(id)}
>
  <h2>Primary queue</h2>
</ResourceLayout>
```

Import `ResourceLayout` and supply your selection callback in the consuming component. Bind `collapsed` when the surrounding screen needs to track the menu view. The same resource buttons remain mounted when the list collapses, preserving focus and selection. Arrow keys, Home, and End move selection and focus; Tab enters the selected resource and then its detail panel. Pills scroll inside the submenu instead of widening the page. The toggle and pills use 44px targets on phones.

See `src/lib/demo/components/ResourceBrowserExample.svelte` for search, a filtered empty state, worker actions, and details. Its selection falls back to the first search result, and restores the requested worker when the search clears. Menu expansion is local to this example and independent of the main sidebar.

## Metrics and charts

Each metric needs a label, value, and description. Units and a historical trend are optional.

```svelte
<MetricStrip
  metrics={[
    { label: "Waiting tasks", value: 12, description: "Primary queue" },
    {
      label: "Duration",
      value: 142,
      unit: "ms",
      description: "Median of the last 60 seconds",
    },
  ]}
/>
```

Import `MetricStrip` in the consuming component. Describe a real measurement window when connecting telemetry. Avoid percent changes without a baseline.

`Sparkline` scales its values to its own maximum. It is a local trend, not a comparative chart. Give it a label that names the measurement and summarizes the values. Use a larger chart with axes when absolute scale or comparison matters.

## Navigation

`createShell()` in the root layout owns collapsed and mobile navigation state. `AppShell`, `PageHeader` and settings read that context. It also owns the theme and `setTheme()`, so footer and settings controls stay synchronized. The root layout resolves route URLs and passes the shell `NavigationItem[]`. Each item has `href`, `label`, `icon`, and `group`, with an optional numeric count, `description` and `details`. Each detail has a `label` and string or numeric `value`. Descriptions and details appear at sidebar widths of 296px and above. The default expanded width is 320px; collapsing retains a 56px icon rail with accessible links and an underline marker. Keep items from the same group adjacent.

Counts come from the consuming product. The shell has no knowledge of workers. Phosphor icons use the same component interface, so another icon can replace the example's CPU or scroll glyph.

Set `PageHeader.dataLabel` and the header description to describe the actual data connection. Use a live label only when backed by a current source and a stale-data policy.

## When to add a component

Extract a pattern after two screens need it. A useful component owns layout or behavior that would otherwise drift between screens. A wrapper that merely renames a `div` adds little.

Keep API calls, worker transitions, product nouns, and routing decisions outside the generic table and section components. Add pagination or virtualization when real datasets require them. The six-row example does not pretend to solve large inventories.

## Connection canvas

`TopologyCanvas` accepts `FlowNode[]` and `FlowEdge[]`. A node has an ID, label, detail, semantic tone, and x/y coordinates in a 900 by 460 coordinate space. Edges reference node IDs. The component has no knowledge of workers or APIs.

Nodes can be dragged. Alt+arrow keys move a focused node, and Enter selects it. Reset restores the supplied positions. Expanded mode exposes a bound boolean so the route can hide surrounding content. The overview demonstrates that composition.

On phones, the canvas becomes a readable list of selectable resources. Do not shrink diagram text into an unreadable mobile graph. Connection positions are session-only; add persistence in the consuming project if it is needed.

## Scrolling settings

`SettingsLayout` accepts sections with stable selector-safe IDs and labels, plus a `section` snippet receiving each section. It owns the section containers, sticky index, smooth section jumps and scroll tracking. Render a heading and the product’s setting controls in the snippet. It tracks the nearest `main` scroll container, matching `AppShell`.

See `src/routes/settings/+page.svelte` for the working example. Theme changes persist immediately; navigation mode is session-only. Add a draft/save model only when the product needs one. Reduced motion changes jumps to immediate scrolling. Narrow screens use a horizontal sticky index.

## Suggested section structure

`SectionSkeleton` is a static visual scaffold. It describes toolbar, summary, main content and details without choosing a domain model. Its accessible description identifies it as a recommendation, not a loading state. Keep the recommendation copy when displaying it, and replace the placeholders with real components when adapting a screen.
