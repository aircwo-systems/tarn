# Design guide

## The operator and the task

This interface is for a developer or operator checking a running system. They need to locate stalled work, inspect a resource, and decide whether to intervene. The dashboard should feel precise and settled, even when a worker has failed.

The example domain includes worker pools, capacity slots, queued tasks, upstream timeouts, retries, and activity events. These give the layout its hierarchy. In another product, replace those nouns and keep the operator's next action visible.

## What makes the design recognizable

Tarn's console uses a neutral canvas, a slightly raised work area, thin separators, a restrained green accent, and monospace data. This starter preserves Tarn's rounded inset workspace and the sidebar's original resize and highlight geometry.

The same structure appears in five places:

- The navigation's active and hover pills slide between compact rows. Its active seam uses the foreground color, matching Tarn.
- Metrics share one divided strip.
- The dotted topology canvas keeps resource connections spatial and opens into an expanded view.
- The inventory uses ledger rows and a narrow selection seam.
- The detail pane and activity feed share the workspace's rules and typography.

These details matter more than adding another decorative container.

| Common dashboard default            | Starter pattern                                  |
| ----------------------------------- | ------------------------------------------------ |
| Separate rounded metric cards       | One metric strip with internal dividers          |
| A large branded hero                | A short task-oriented page header                |
| Different colors for every resource | Neutral resource rows and semantic status colors |

## Palette and themes

The palette comes from Tarn's operational language. Black and graphite establish structure. The light theme uses Tarn's cool gray outer canvas and white inset stage. Chalk text carries the hierarchy. Green marks actions and healthy work. Amber denotes waiting or caution, rose denotes failure, and blue denotes information.

All color declarations live in `src/lib/styles/tokens.css`. Components consume named roles. Do not add raw hex values to a route or component.

| Role               | Dark      | Light     | Use                            |
| ------------------ | --------- | --------- | ------------------------------ |
| `--canvas`         | `#000000` | `#e6e9ef` | Navigation and outer workspace |
| `--surface`        | `#0a0a0a` | `#ffffff` | Main work area                 |
| `--surface-raised` | `#141414` | `#f6f7f8` | Table headers and quiet groups |
| `--surface-hover`  | `#1a1a1a` | `#eceef1` | General hover surfaces         |
| `--control`        | `#060606` | `#f2f3f5` | Inset input fields             |
| `--ink`            | `#f5f5f5` | `#17191d` | Titles and primary content     |
| `--ink-secondary`  | `#888888` | `#576070` | Supporting content             |
| `--ink-tertiary`   | `#999999` | `#576070` | Metadata and labels            |
| `--accent`         | `#34d399` | `#067453` | Brand action and selection     |
| `--success`        | `#34d399` | `#067453` | Healthy operational state      |
| `--warning`        | `#fbbf24` | `#925300` | Paused work or caution         |
| `--danger`         | `#fb7185` | `#bd2440` | Failure and destructive action |
| `--info`           | `#93b4fa` | `#3157b1` | Informational state            |

`--ink-disabled` is for inactive or decorative content. It is not a readable body-text color. Status labels use their matching `*-soft` background. Check contrast against that background when changing a status token.

Brand and success initially share a green value, but remain separate tokens. A purple Goblin brand can change `--accent`, `--accent-soft`, and `--focus` while healthy workers stay green.

The starter defaults to dark, matching the Tarn reference. The theme toggle applies `data-theme` to the document and saves the explicit choice. It does not follow OS appearance changes. Changing that behavior requires updating both the early bootstrap and the toggle.

## Depth and shape

Use Tarn's actual shape hierarchy. The main stage is inset 8px from the canvas with a 14px radius. Detail sections use the rack panel's 12px radius and 16px by 18px padding. Use `Panel flat` when a section should share the workspace directly.

`--line` separates rows and regions. `--line-strong` defines controls and emphasized boundaries. `--focus` makes the keyboard target clear. Do not make every divider as strong as an input border.

Buttons, inputs, and status badges use 8px radii. Navigation pills use 5px, matching Tarn. The header's sidebar toggle uses 6px. Rounded corners have different jobs at different scales; preserve those measurements when adapting the product.

## Typography

Geist Variable is bundled for UI text. Geist Mono Variable is bundled for numbers, identifiers, timestamps, and code. There are no external font requests.

| Content              | Treatment                                  |
| -------------------- | ------------------------------------------ |
| Page title           | 15px, weight 600, slight negative tracking |
| Section title        | 13px, weight 600                           |
| Body                 | 13px with 1.5 line height                  |
| Row/control text     | 12px                                       |
| Supporting text      | 11–13px                                    |
| Eyebrow and metadata | 10px monospace, restrained tracking        |
| Metric value         | 20px monospace with tabular numbers        |

This is a compact desktop operations interface. On narrow screens, text inputs use 16px and interactive targets expand to 44px. Keep long explanatory copy at a readable measure. Avoid shrinking data until it fits.

## Spacing and density

The base unit is 4px. The token scale is 4, 8, 12, 16, 20, 24, 32, 40, and 48px.

Use 4–8px for icon gaps, 8–16px inside rows, 24px between related panes, and 32px between sections. The stage has 24px horizontal and 20px vertical padding, reducing to 16px on phones. The sidebar starts at 320px, resizes between 180px and 520px, and collapses to a 56px icon rail. At 296px and wider, navigation items reveal descriptions and supporting information, following the Goblin sidebar pattern.

Pixel-sized spacing keeps the operational grid predictable. Font sizes use the browser's zoom behavior. Verify layouts at increased zoom before adding fixed heights.

## Sidebar interaction

The sidebar is adapted directly from Tarn's `app-sidebar.svelte`. Preserve its expanding and collapsing behavior as part of the design.

- Drag the edge to resize. A narrow handle follows the pointer and displays the current width.
- Drag below 110px and release to collapse. The prior width remains available for expansion.
- Use the header toggle or Cmd/Ctrl+B to collapse and expand. The shortcut does not intercept text fields.
- The compact rail keeps every navigation icon centered in a 40px by 52px target. A 16px by 3px underline pill marks the current route. Links retain accessible names and native title hints.
- Drag the rail’s edge or use the header toggle to expand.
- Focus the separator and use left/right arrows to adjust in 10px steps. Enter, Space, or double click restores 320px.
- Expanded width persists under the product’s configured storage key. Compact navigation remains interactive. Mobile navigation is hidden until opened.

The active and hover pills are separate elements positioned from the navigation rows. They remain aligned when the container resizes or scrolls. Reduced motion disables their transitions.

## Settings and section templates

The settings screen carries Tarn’s sticky submenu, moving selection pill, rounded sections and scroll tracking. Selecting a submenu entry scrolls to its section; scrolling the workspace updates the selection. Below 1000px, the submenu becomes a sticky horizontal strip. Jump motion respects reduced motion.

Theme preferences work and persist. Compact/expanded navigation changes the current session. Settings examples explain only implemented behavior; they do not invent refresh settings or backend connections.

The Layout examples route offers a working resource browser and a static section skeleton. The browser adapts Tarn's vertical resource submenu and horizontal pill switcher. Collapsing the submenu keeps the same resource selected and lets details span the work area. Search remains available in either view, status dots have text labels, and arrow keys move focus with selection. This menu expands independently of the main sidebar.

The skeleton's neutral placeholders show a toolbar, summary strip, primary content and secondary details. It has no product fields or fake actions, no shimmer, and no loading semantics. Adapt or remove each slot as the next product requires.

## Screen structure

An overview starts with Tarn's compact section header and a divided metric strip. A dotted connection canvas and recent activity follow. The canvas expands to fill the work area, hiding secondary overview content. The worker inventory remains available below the canvas in the regular view.

A resource screen starts with search and status filters, followed by a count, inventory, and detail pane. The first column contains an actual selection button. The row's color alone is not an interaction target.

The generic `ResourceTable` renders cells through a Svelte snippet. Resource-specific formatting belongs in a wrapper such as the example `WorkerTable`, not in the generic table.

Avoid showing every value on the overview. The detail pane is the home for concurrency, identifiers, the current task, historical samples, and actions.

## Responsive behavior

| Width           | Behavior                                                              |
| --------------- | --------------------------------------------------------------------- |
| Above 1100px    | Inventory and details sit side by side; overview has an activity pane |
| 641–1100px      | Secondary panes follow the primary content                            |
| 640px and below | Navigation opens inline; filters wrap; secondary table columns hide   |

The app fits the viewport. The inset main stage scrolls independently and the sidebar has its own navigation scroll. The mobile navigation opens in document flow, avoiding a modal focus trap. Escape closes it. Opening and closing use a labeled button with `aria-expanded`.

Tables may scroll inside their own wrapper when a consuming project's columns require it. They must not widen the whole page. Hide secondary columns only when the detail view still exposes their information.

## Interaction and motion

Hover and press feedback should respond immediately. Control transitions use 120ms or 180ms. The sidebar expands over 200ms, its active pill moves over 220ms, and its hover pill follows over 130ms. Do not add looping animation or bounce to operational status.

The starter preserves the sidebar motion without adding fake live telemetry. Reduced motion disables nonessential transitions. Historical charts and timestamps explicitly describe sample data.

Every action needs visible feedback. Use a live status region for a completed action, and preserve inline errors for a failed action. Do not announce every metric refresh through a screen reader.

## Accessibility checks when extending

- Use semantic links, buttons, headings, tables, labels, and timestamps.
- Keep text and icon controls readable in both themes.
- Keep keyboard focus visible and unobscured.
- Give icon buttons an accessible name and hide decorative icons.
- Pair status color with a word. Pair a chart with a meaningful text alternative.
- Keep input labels visible instead of relying on placeholders.
- Preserve user-entered filters when a request fails.
- Verify the page at 375px and at 200% zoom.

The included patterns help meet these requirements. They do not establish accessibility compliance for every consuming application.
