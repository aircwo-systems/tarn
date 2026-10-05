# Verification

## Required local checks

```sh
bun install --frozen-lockfile
bun run fmt:check
bun run check
bun run build
```

Use `bun run preview` to check the production build. The dev server can hide issues with prerendering or direct-route hosting, so a successful dev page alone is not sufficient.

## Acceptance flows

| Flow                                                   | Expected result                                                             |
| ------------------------------------------------------ | --------------------------------------------------------------------------- |
| Open overview                                          | Six sample workers, three active, one needs attention                       |
| Select `sync-01` from overview                         | Workers route opens with that worker selected                               |
| Search for an unknown worker                           | No matching workers, clear-filters action available                         |
| Filter running, select a worker, pause it              | Row leaves the filter, count updates, action feedback appears               |
| Clear the status filter and select that worker         | Paused state and resume action appear                                       |
| Resume a paused worker                                 | Running state and an activity event appear                                  |
| Retry the error worker                                 | Error becomes running, overview attention count drops                       |
| Visit activity after an action                         | The new event is first, with a UTC timestamp                                |
| Reset the demo                                         | Original workers and events return                                          |
| Toggle the theme and reload                            | Chosen theme persists without a bright initial flash                        |
| Collapse and expand the sidebar                        | 56px rail retains centered icons, accessible labels and an underline marker |
| Click settings submenu entries and scroll manually     | Section jumps work; the active marker tracks scrolling                      |
| Change theme in settings, then use the footer toggle   | Both controls stay synchronized and the theme persists                      |
| Open Layout examples                                   | A working resource browser opens with the first sample worker selected      |
| Select a worker, then collapse and expand its submenu  | Selection and details remain; the list becomes horizontal status pills      |
| Search in either submenu view, then clear the search   | Results update; an unknown query has an empty state and clear action        |
| Use arrows, Home, and End on resource tabs             | Selection and focus move together; the active pill scrolls into view        |
| Pause, resume, or retry a worker in the layout example | Status, details, activity and action feedback update in both menu views     |
| Choose Section skeleton or open its example query URL  | Static generic slots are labeled as a recommendation                        |
| Preview loading, empty, error, and retry on foundation | Each state has its message and the error has an operational retry button    |
| Navigate directly to every built route                 | Each route loads from its generated HTML                                    |

Also verify sidebar information appears at 296px and hides below that width. Verify sidebar drag resize, drag-to-collapse, compact rail navigation and edge expansion, Cmd/Ctrl+B, keyboard resize, double-click reset, and persisted width. Check the topology's node selection, node movement, reset, and expanded/restored layout.

Check the shell and primary resource screen at desktop, tablet, and 375px widths, including landscape. At 375px, the page should have no horizontal overflow, the navigation should toggle inline, and buttons/inputs should provide 44px targets.

Inspect both themes independently. Check focus order with Tab, visible focus, status text, input labels, and Escape on the mobile menu. Enable reduced motion and increase browser zoom to 200% before changing density or fixed dimensions.

## Extending the checks

Add automated tests around the consuming application's API parsing, permissions, supported transitions, and async failures. The sample UI does not add tests that merely restate its markup. For large datasets, verify pagination, sorting, selection retention, and request cancellation with real data volume.
