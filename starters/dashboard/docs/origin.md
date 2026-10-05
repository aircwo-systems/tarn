# Origin

This starter was extracted from Tarn's Svelte dashboard in September 2026. Tarn is licensed under Apache 2.0. The starter includes that license so it can travel with a copied project.

`Sidebar.svelte` directly adapts Tarn's sidebar behavior and styles. `AppShell`, `PageHeader`, controls, and sections preserve its geometry while exposing product-independent inputs. The entire Tarn UI directory is not a dependency.

## Source patterns

These links point into the parent Tarn repository. They serve as provenance when the starter is copied elsewhere.

| Tarn source                                                                                  | Pattern carried forward                                                    |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| [app.css](../../../ui/src/app.css)                                                           | Black/graphite surfaces, green accent, light theme, UI/data typography     |
| [app-sidebar.svelte](../../../ui/src/lib/components/layout/app-sidebar.svelte)               | Resize/collapse gestures, moving pills, grouped navigation, footer dock    |
| [rc-button.svelte](../../../ui/src/lib/components/rack/rc-button.svelte)                     | Restrained button variants and keyboard focus                              |
| [rc-tone-pill.svelte](../../../ui/src/lib/components/rack/rc-tone-pill.svelte)               | Named operational status with semantic colors                              |
| [rc-stat.svelte](../../../ui/src/lib/components/rack/rc-stat.svelte)                         | Small labels, monospace values, tabular numbers                            |
| [rc-list-row.svelte](../../../ui/src/lib/components/rack/rc-list-row.svelte)                 | Compact resource rows and selection emphasis                               |
| [theme-toggle.svelte](../../../ui/src/lib/components/layout/theme-toggle.svelte)             | Explicit light/dark switching                                              |
| [section-header.svelte](../../../ui/src/lib/components/sections/section-header.svelte)       | Compact heading with sidebar toggle                                        |
| [rc-panel.svelte](../../../ui/src/lib/components/rack/rc-panel.svelte)                       | Rounded inset detail sections and flat variants                            |
| [settings-section.svelte](../../../ui/src/lib/components/sections/settings-section.svelte)   | Sticky scrolling submenu, moving section marker and rounded setting panels |
| [functions-section.svelte](../../../ui/src/lib/components/sections/functions-section.svelte) | Vertical resource list, collapse toggle and horizontal status pills        |
| [Overview layout](../../../ui/src/routes/+page.svelte)                                       | Metrics above a dotted canvas and adjacent activity                        |

The 8px stage inset, 14px stage radius, 12px panel radius, compact header, resizable sidebar, and moving navigation pills are preserved.

The starter consolidates Tarn's overlapping token families into one theme contract. It keeps the compact operational feel while providing readable secondary text, locally bundled fonts, and responsive layouts.

## Goblin sidebar additions

The compact rail and wider information rows were added from the local Goblin console reference (`task-goblin-console/ui/sidebar.ts` and `ui/styles.css`). That implementation also cites Tarn for its moving pills and edge resizing. This starter keeps its 56px rail, centered icons, underline marker and 296px information threshold while expressing them as Svelte components with generic labels and values. The expanded default is 320px so the information is visible immediately.

The section skeleton is a new static recommendation. It intentionally leaves domain fields and actions undecided.

`ResourceLayout` adapts Tarn's resource submenu and collapsed toolbar from `functions-section.svelte` and the shared `.list-toolbar` / `.item-chip` rules in `app.css`. It uses the starter's theme tokens and keeps one set of resource tabs in both views. `ResourceBrowserExample` supplies generic sample workers without importing Tarn's function model, API client, or resource details.

## Deliberately excluded

AWS service types, account IDs, Tarn logos, polling, API paths, resource topology code, settings migrations, embedded Go assets, and product-specific dialogs belong to Tarn. They are not dependencies of this starter.

The starter retains Tarn's resizable sidebar and inset stage. It leaves out the larger component dependency set. The generic connection canvas follows the overview layout without copying AWS topology registries or API relationships.

Tarn remains an independent app. Future changes to its UI do not automatically change the copied starter.
