<script lang="ts">
  import PageHeader from "$lib/components/PageHeader.svelte";
  import { page } from "$app/state";
  import { PauseIcon, PlayIcon, ArrowsClockwiseIcon } from "phosphor-svelte";
  import { product } from "$lib/config";
  import { getDemo } from "$lib/demo/store.svelte";
  import { statusTone, type WorkerStatus } from "$lib/demo/models";
  import { formatNumber } from "$lib/format";
  import Button from "$lib/components/Button.svelte";
  import Panel from "$lib/components/Panel.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import StateView from "$lib/components/StateView.svelte";
  import Sparkline from "$lib/components/Sparkline.svelte";
  import WorkerTable from "$lib/demo/components/WorkerTable.svelte";

  const demo = getDemo();
  let query = $state("");
  let filter = $state<"all" | WorkerStatus>("all");
  let selectedId = $state("wrk_01");
  let feedback = $state("");
  const filters: readonly ("all" | WorkerStatus)[] = [
    "all",
    "running",
    "idle",
    "paused",
    "error",
  ];
  const visible = $derived(
    demo.data.workers.filter(
      (worker) =>
        (filter === "all" || worker.status === filter) &&
        `${worker.name} ${worker.pool} ${worker.id}`
          .toLowerCase()
          .includes(query.toLowerCase().trim()),
    ),
  );
  const selected = $derived(visible.find((worker) => worker.id === selectedId));

  // Effects run in the browser, so query-specific selection remains compatible with prerendering.
  $effect(() => {
    const requested = page.url.searchParams.get("worker");
    if (requested) selectedId = requested;
  });

  function reset() {
    demo.reset();
    query = "";
    filter = "all";
    selectedId = "wrk_01";
    feedback = "Sample workers and activity restored.";
  }
</script>

<svelte:head><title>Workers | {product.name}</title></svelte:head>
<PageHeader
  title="Workers"
  description="Inspect capacity, current tasks, and worker state"
  >{#snippet actions()}<Button onclick={reset}
      ><ArrowsClockwiseIcon size={14} aria-hidden="true" />Reset demo</Button
    >{/snippet}</PageHeader
>
<div class="toolbar">
  <div class="search">
    <label class="field-label" for="worker-search">Find a worker</label><input
      id="worker-search"
      class="search-input"
      type="search"
      placeholder="Search name, pool, or ID…"
      bind:value={query}
    />
  </div>
  <div>
    <span class="field-label">Status</span>
    <div
      class="filter-strip"
      role="group"
      aria-label="Filter workers by status"
    >
      {#each filters as status}<button
          aria-pressed={filter === status}
          onclick={() => (filter = status)}
          >{status === "all"
            ? "All workers"
            : status.charAt(0).toUpperCase() + status.slice(1)}</button
        >{/each}
    </div>
  </div>
</div>
<div class="result-count" role="status">
  {visible.length} of {demo.data.workers.length} workers
</div>
<div class="workers-layout">
  <div class="inventory">
    {#if visible.length}<WorkerTable
        workers={visible}
        {selectedId}
        onselect={(worker) => (selectedId = worker.id)}
      />
    {:else}<StateView
        state={{
          kind: "empty",
          title: "No matching workers",
          description: "Try another name or choose a different status.",
        }}
      /><Button
        variant="ghost"
        onclick={() => {
          query = "";
          filter = "all";
        }}>Clear filters</Button
      >{/if}
  </div>
  <aside class="details" aria-label="Worker details">
    {#if selected}
      <Panel title={selected.name} description={selected.id}>
        {#snippet actions()}<StatusBadge
            label={selected.status}
            tone={statusTone[selected.status]}
          />{/snippet}
        <dl>
          <div>
            <dt>Pool</dt>
            <dd>{selected.pool}</dd>
          </div>
          <div>
            <dt>Concurrency</dt>
            <dd class="mono">{selected.concurrency} slots</dd>
          </div>
          <div>
            <dt>Tasks completed</dt>
            <dd class="mono">{formatNumber(selected.completed)}</dd>
          </div>
          <div>
            <dt>Sample duration</dt>
            <dd class="mono">{selected.latencyMs} ms</dd>
          </div>
        </dl>
        <div class="task">
          <span class="eyebrow">Current task</span>
          <p>
            {selected.currentTask ??
              (selected.status === "error"
                ? "Upstream connection timed out. Retry to resume processing."
                : "No task in progress.")}
          </p>
        </div>
        <div class="history">
          <div class="section-heading">
            <span class="eyebrow">Task history</span><span
              class="history-caption">12 sample intervals</span
            >
          </div>
          <Sparkline
            values={selected.history}
            label={`${selected.name} completed tasks per sample interval: ${selected.history.join(", ")}`}
          />
        </div>
        <Button
          variant={selected.status === "running" || selected.status === "idle"
            ? "default"
            : "primary"}
          onclick={() => {
            if (selected) {
              const worker = selected;
              demo.changeWorker(worker);
              feedback = `${worker.name} is now ${worker.status}.`;
            }
          }}
        >
          {#if selected.status === "running" || selected.status === "idle"}<PauseIcon
              size={14}
              aria-hidden="true"
            />Pause worker
          {:else if selected.status === "error"}<ArrowsClockwiseIcon
              size={14}
              aria-hidden="true"
            />Retry worker
          {:else}<PlayIcon size={14} aria-hidden="true" />Resume worker{/if}
        </Button>
        <p class="demo-hint">Actions affect sample data in this session.</p>
      </Panel>
    {:else}<StateView
        state={{
          kind: "empty",
          title: "Select a worker",
          description: "Choose a worker from the inventory to inspect it here.",
        }}
      />{/if}
  </aside>
</div>
<p class="feedback" role="status" aria-live="polite">{feedback}</p>

<style>
  .toolbar {
    display: flex;
    gap: var(--space-6);
    align-items: end;
    flex-wrap: wrap;
    padding: var(--space-5) 0;
    border-block: 1px solid var(--line);
  }
  .search {
    width: min(300px, 100%);
  }
  .result-count {
    padding: var(--space-4) 0;
    color: var(--ink-tertiary);
    font: 11px var(--font-data);
  }
  .workers-layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 280px;
    gap: var(--space-6);
  }
  .inventory {
    min-width: 0;
  }
  .details {
    border-left: 1px solid var(--line);
    padding-left: var(--space-6);
  }
  dl {
    margin: 0;
    border-top: 1px solid var(--line);
  }
  dl > div {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--line);
    font-size: 12px;
  }
  dt {
    color: var(--ink-tertiary);
  }
  dd {
    margin: 0;
  }
  .task {
    padding: var(--space-5) 0;
    border-bottom: 1px solid var(--line);
  }
  .task p {
    margin-top: var(--space-2);
    font-size: 12px;
    color: var(--ink-secondary);
  }
  .history {
    padding-block: var(--space-5);
  }
  .history-caption,
  .demo-hint {
    font-size: 10px;
    color: var(--ink-tertiary);
  }
  .demo-hint {
    margin-top: var(--space-3);
  }
  .feedback {
    color: var(--accent);
    font-size: 12px;
    min-height: 20px;
    margin-top: var(--space-4);
  }
  @media (max-width: 1100px) {
    .workers-layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .details {
      padding: var(--space-6) 0 0;
      border-left: 0;
      border-top: 1px solid var(--line);
    }
  }
</style>
