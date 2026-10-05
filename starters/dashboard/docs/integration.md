# Integration guide

## The replacement boundary

The demo is deliberately local. `fixtures.ts` supplies sample workers and events. `store.svelte.ts` creates state for each layout instance, and `models.ts` defines the example domain. The root layout passes counts, descriptions and information rows to the generic shell. Replace those values with your product’s section summaries; the sidebar only knows labels and values.

`createDemo` uses Svelte context instead of a module-level mutable singleton. That prevents shared user state when the app is later rendered by a server. Keep the same ownership rule when replacing the demo.

The worker table and activity feed are examples of domain-specific wrappers. Replace or rename them along with the routes. Keep the shell, button, table, panel, badge, metric strip, and data-state components.

## Connecting a service

1. Define your resource model from the service's actual states and fields.
2. Parse external responses at the API boundary. Validate JSON with your project's schema library before using domain types.
3. Model the request lifecycle separately from resource status. A request can fail while the last known worker remains running.
4. Load the first result, retain filters, and show loading, empty, or error states in the existing layout.
5. Connect mutations. Disable a pending action and render the confirmed result or an inline failure.
6. Add refresh or subscriptions only after a plain request works. Stop polling on teardown and when the page is hidden.

The starter ships without an API proxy. Configure Vite's development proxy or your server's API routes for the consuming service. Keep API credentials on the server. Do not place secrets in public environment variables or static files.

For a browser-facing API, use the service's authentication and authorization rules. A hidden button is not an authorization boundary.

## A loaded request model

This is a suggested shape for the consuming data layer, not another dependency in the starter:

```ts
type ResourceRequest<T> =
  | { kind: "loading" }
  | { kind: "ready"; items: readonly T[]; updatedAt: string }
  | { kind: "error"; message: string };
```

An empty `ready.items` produces the empty state. A nonempty result produces the table. An error produces the retry state. If you need stale results during refresh, model that case explicitly and show the last successful update time.

Do not cast arbitrary JSON to `Worker[]`. Do not show "Live" just because a timer is running. A failed refresh should not look healthy indefinitely.

## Adapting for Goblin Workers

These are proposed mappings. Confirm the Goblin service's actual concepts and API before writing its data layer.

| Starter                         | Possible Goblin equivalent             | Where to adapt                    |
| ------------------------------- | -------------------------------------- | --------------------------------- |
| Console / operations workspace  | Goblin Workers / worker control        | `src/lib/config.ts`               |
| Workers                         | Worker instances or agents             | Resource model and workers route  |
| Pools                           | Queues, capabilities, or worker groups | Worker model and detail pane      |
| Completed tasks                 | Completed jobs or runs                 | Metric calculations               |
| Running / idle / paused / error | Goblin's real lifecycle states         | Model and status-to-tone map      |
| Current task                    | Active job, run, or lease              | Detail pane                       |
| Activity                        | Job events and worker heartbeats       | Activity model, feed, and filters |
| Pause / resume / retry          | Supported service actions              | Mutation handlers and feedback    |

Keep the distinction between a worker, its task, and the queue it consumes. If Goblin has separate worker and job lifecycles, give them separate types and screens. Do not make one status enum cover both.

The six example workers are synthetic. Their pool names, capacities, counts, and traces are not a claim about Goblin's API.

## Static hosting and server rendering

The starter prerenders six routes using the static adapter. Every route has its own directory `index.html`. A static file server can serve the output directly.

Worker selection uses a query parameter read inside a browser effect. The query-specific state is not rendered during prerendering. Direct links such as `/workers/?worker=wrk_06` select the requested worker after hydration.

For a hosted base path, set `kit.paths.base` in `svelte.config.js`. Navigation uses `resolve` and the favicon uses `asset`, so their URLs respect the base path.

If you need server-only authentication, private credentials, or server-side data loading, choose the appropriate SvelteKit server adapter and revisit `prerender` in `src/routes/+layout.ts`. Never prerender private user data into public HTML.

## Theme and branding

Change the product identity in config, the navigation mark in `Sidebar`, and `static/favicon.svg`. The starter's mark is generic and does not reuse Tarn's logo.

Change both dark and light token values together. Preserve readable secondary text and semantic status colors. The brand accent and success currently share a hue but have separate tokens.

The early theme script uses `console:theme`, matching `product.themeStorageKey`. If your product uses another storage key, update both. Storage failure leaves the toggle functional for the current page.

Fonts are packaged locally. Keep the font packages and their licenses when retaining Geist, or replace the imports and font tokens together.

## Before replacing the demo label

- Every visible control calls a supported action and reports its outcome.
- Unavailable, stale, loading, empty, and failed data remain distinguishable.
- The overview metrics derive from the same data as the inventory.
- Back, forward, refresh, and direct links work with the deployment path.
- Keyboard, narrow-screen, light-theme, and reduced-motion checks pass.

Remove sample labels only after the displayed values come from the connected service.
