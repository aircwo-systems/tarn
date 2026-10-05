# Dashboard starter

A standalone Svelte dashboard drawn from Tarn's rack console. It preserves the expanding sidebar, moving navigation pills, rounded inset workspace, graphite palette, green accents, and compact data typography. Product identity, example data, and reusable components have separate homes.

Use it for worker consoles, job runners, service dashboards, or another operations tool. The included worker workflow demonstrates the patterns without requiring a backend.

## Run it

From the Tarn repository:

```sh
cd starters/dashboard
bun install --frozen-lockfile
bun run dev
```

Open [localhost:4174](http://localhost:4174). Tarn does not need to be running. Use Bun 1.3 or newer and a Node version supported by Vite 6.

| Route                | Purpose                                                             |
| -------------------- | ------------------------------------------------------------------- |
| `/`                  | Metrics, topology, inventory, and recent activity                   |
| `/workers/`          | Search, status filters, selection, details, pause/resume/retry      |
| `/activity/`         | Searchable events with level filters and UTC timestamps             |
| `/settings/`         | Tarn’s scrolling submenu, theme preferences and navigation controls |
| `/section-template/` | Resource browser with a collapsible submenu and a generic skeleton  |
| `/foundation/`       | Interactive examples of themes, controls, status, and data states   |

Actions update sample data in this browser session. Navigation preserves it. Reloading restores the fixtures. The theme and expanded sidebar width persist in local storage. The sidebar has a 56px icon rail and reveals section information at widths of 296px and above. No action calls a real worker.

Layout examples opens a working resource browser adapted from Tarn. Its vertical worker submenu collapses into horizontal pills, leaving more room for the selected worker's details. Search, status, actions, and selection work in both views. Arrow keys move between resources; Home and End select the first and last result. Switch to the section skeleton for static structural placeholders, or open it directly with `/section-template/?example=skeleton`.

## Start another project

Run this from the repository root. Choose a destination that does not already contain a project.

```sh
mkdir ../my-dashboard
rsync -a \
  --exclude node_modules --exclude .svelte-kit --exclude build \
  starters/dashboard/ ../my-dashboard/
cd ../my-dashboard
bun install --frozen-lockfile
bun run dev
```

The copy includes its own dependency lockfile, static adapter, fonts, and license. It has no imports from Tarn's application or backend.

## Change the product

1. Set the name, subtitle, and environment in [config.ts](src/lib/config.ts). Rename the package in `package.json`.
2. Edit navigation in [the root layout](src/routes/+layout.svelte), and replace the generic mark and favicon.
3. Adjust both themes in [tokens.css](src/lib/styles/tokens.css). Brand and status colors have separate roles.
4. Replace the worker model, fixtures, and session store in `src/lib/demo/` with your data layer.
5. Adapt the example routes. Keep `src/lib/components/` independent of your resource models.

If you change the theme storage key, change its early read in `src/app.html` too. That script applies the saved theme before the page paints.

## Guides

- [Design guide](docs/design-guide.md): visual rules, tokens, density, responsive behavior, and what to preserve.
- [Component guide](docs/components.md): interfaces and examples for composing a new screen.
- [Integration guide](docs/integration.md): replace the demo, connect an API, handle data states, and adapt for Goblin Workers.
- [Origin](docs/origin.md): which Tarn patterns informed the starter and what was deliberately excluded.
- [Verification](docs/verification.md): checks and manual acceptance flows.

## Validate and build

```sh
bun run fmt:check
bun run check
bun run build
bun run preview
```

The build writes prerendered HTML and bundled assets to `build/`. Serve that folder with a static host that supports directory `index.html` files. Each route has its own HTML, so direct navigation works without a catch-all rewrite.

The starter includes no authentication, authorization, API client, real polling, or production worker controls. Add those in the consuming project. [The integration guide](docs/integration.md) explains the relevant boundaries.

## License

Apache 2.0, inherited from Tarn. Keep [LICENSE](LICENSE) and [the origin note](docs/origin.md) when copying the starter. Dependency fonts and icons retain their own package licenses.
