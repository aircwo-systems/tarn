<script lang="ts">
  import PageHeader from "$lib/components/PageHeader.svelte";
  import { product } from "$lib/config";
  import Panel from "$lib/components/Panel.svelte";
  import Button from "$lib/components/Button.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import StateView, { type ViewState } from "$lib/components/StateView.svelte";
  let message = $state("");
  let previewState = $state<ViewState>({
    kind: "empty",
    title: "No workers yet",
    description: "Connect your first worker to see it in this inventory.",
  });
  const surfaces = [
    "canvas",
    "surface",
    "surface-raised",
    "surface-hover",
    "control",
  ];
  function showState(kind: ViewState["kind"]) {
    switch (kind) {
      case "loading":
        previewState = { kind, message: "Loading the worker inventory…" };
        break;
      case "empty":
        previewState = {
          kind,
          title: "No workers yet",
          description: "Connect your first worker to see it in this inventory.",
        };
        break;
      case "error":
        previewState = {
          kind,
          title: "Unable to load workers",
          description:
            "The service could not be reached. Your filters are saved.",
          onretry: () => {
            showState("empty");
            message = "Retry completed. The empty state is now visible.";
          },
        };
        break;
      default: {
        const exhaustive: never = kind;
        return exhaustive;
      }
    }
  }
</script>

<svelte:head><title>UI foundation | {product.name}</title></svelte:head>
<PageHeader
  title="UI foundation"
  description="Tarn's shell, controls, and spacing for future dashboards"
/>
<div class="intro">
  <p>
    Start with the operator's task. Use Tarn's rounded inset workspace, quiet
    dividers, and readable data. Reserve color for an action or a state that
    matters.
  </p>
  <p>
    Identity and example data live apart from the components. Replace the worker
    vocabulary with your domain while keeping the spacing, typography, and
    interaction rules consistent.
  </p>
</div>
<div class="foundation-sections">
  <Panel
    flat
    title="Surfaces"
    description="Neutral in dark mode. Clear and restrained in light mode."
  >
    <div class="surface-strip">
      {#each surfaces as surface}<div>
          <div class="swatch" style:background={`var(--${surface})`}></div>
          <code>--{surface}</code>
        </div>{/each}
    </div>
  </Panel>
  <Panel
    flat
    title="Type and rhythm"
    description="Geist for the interface. Geist Mono for identifiers, metrics, and time."
  >
    <div class="type-ledger">
      <div>
        <span class="eyebrow">Page title · 15px</span><span class="title-sample"
          >Workers</span
        >
      </div>
      <div>
        <span class="eyebrow">Body · 13px</span><span
          >Inspect the current task.</span
        >
      </div>
      <div>
        <span class="eyebrow">Data · tabular figures</span><span class="mono"
          >wrk_01 · 12,842 · 142 ms</span
        >
      </div>
    </div>
    <p class="guidance">
      Use the 4px spacing scale. Group related controls with 8–12px gaps.
      Separate sections with 24–32px of space and a quiet rule.
    </p>
  </Panel>
  <Panel
    flat
    title="Controls"
    description="One primary action per workflow. Every control has keyboard focus and pressed feedback."
  >
    <div class="examples">
      <Button
        variant="primary"
        onclick={() => (message = "Primary action example activated.")}
        >Primary action</Button
      ><Button onclick={() => (message = "Secondary action example activated.")}
        >Secondary</Button
      ><Button
        variant="ghost"
        onclick={() => (message = "Quiet action example activated.")}
        >Quiet action</Button
      ><Button
        variant="danger"
        onclick={() =>
          (message = "Danger action example activated. No data was removed.")}
        >Danger action</Button
      ><Button disabled>Unavailable</Button>
    </div>
  </Panel>
  <Panel
    flat
    title="Status"
    description="A word and a dot, so color never carries the meaning alone."
  >
    <div class="examples">
      <StatusBadge label="Running" tone="success" /><StatusBadge
        label="Paused"
        tone="warning"
      /><StatusBadge label="Failed" tone="danger" /><StatusBadge
        label="Queued"
        tone="info"
      /><StatusBadge label="Idle" />
    </div>
    <p class="guidance">
      Brand accent and success are separate tokens. Change the brand without
      changing what healthy, waiting, or failed means.
    </p>
  </Panel>
  <Panel
    flat
    title="Data states"
    description="Loading, empty, and error belong in the workflow from the beginning."
  >
    <div
      class="filter-strip state-controls"
      role="group"
      aria-label="Preview a data state"
    >
      <button
        aria-pressed={previewState.kind === "loading"}
        onclick={() => showState("loading")}>Loading</button
      ><button
        aria-pressed={previewState.kind === "empty"}
        onclick={() => showState("empty")}>Empty</button
      ><button
        aria-pressed={previewState.kind === "error"}
        onclick={() => showState("error")}>Error</button
      >
    </div>
    <StateView state={previewState} />
  </Panel>
  <Panel
    flat
    title="Make it yours"
    description="Keep the foundation small and change the product-specific parts."
  >
    <ol class="adaptation">
      <li>
        Set the product name and environment in <code>src/lib/config.ts</code>.
      </li>
      <li>
        Adjust theme roles in <code>src/lib/styles/tokens.css</code>. Keep
        status colors distinct.
      </li>
      <li>
        Replace <code>src/lib/demo/</code> with your resource model and data source.
      </li>
      <li>
        Compose routes with the shell, panels, metric strip, and resource table.
      </li>
      <li>
        Use the design and integration guides in <code>docs/</code> before adding
        new patterns.
      </li>
    </ol>
  </Panel>
</div>
<p class="feedback" role="status" aria-live="polite">{message}</p>

<style>
  .intro {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-8);
    padding-block: var(--space-5) var(--space-8);
    border-top: 1px solid var(--line);
    color: var(--ink-secondary);
    font-size: 13px;
    max-width: 1000px;
  }
  .foundation-sections {
    max-width: 1000px;
    display: grid;
    gap: var(--space-8);
  }
  .foundation-sections :global(.panel + .panel) {
    border-top: 1px solid var(--line);
    padding-top: var(--space-6);
  }
  .surface-strip {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: var(--space-3);
  }
  .swatch {
    height: 64px;
    border: 1px solid var(--line-strong);
    margin-bottom: var(--space-2);
  }
  code {
    font-size: 11px;
    color: var(--ink-secondary);
    overflow-wrap: anywhere;
  }
  .type-ledger {
    display: grid;
    gap: var(--space-4);
  }
  .type-ledger > div {
    display: grid;
    grid-template-columns: 220px 1fr;
    align-items: center;
    gap: var(--space-4);
  }
  .title-sample {
    font-size: 15px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .examples {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  .guidance {
    font-size: 12px;
    color: var(--ink-secondary);
    margin-top: var(--space-4);
    max-width: 75ch;
  }
  .state-controls {
    margin-bottom: var(--space-4);
  }
  .adaptation {
    margin: 0;
    padding-left: 20px;
    color: var(--ink-secondary);
    font-size: 13px;
    display: grid;
    gap: var(--space-3);
  }
  .feedback {
    color: var(--accent);
    font-size: 12px;
    min-height: 20px;
    margin-top: var(--space-6);
  }
  @media (max-width: 640px) {
    .intro {
      grid-template-columns: 1fr;
      gap: var(--space-4);
    }
    .surface-strip {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .type-ledger > div {
      grid-template-columns: 1fr;
      gap: var(--space-2);
    }
  }
</style>
