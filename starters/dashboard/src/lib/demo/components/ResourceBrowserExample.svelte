<script lang="ts">
  import { PauseIcon, PlayIcon, ArrowsClockwiseIcon } from "phosphor-svelte";
  import ResourceLayout from "$lib/components/ResourceLayout.svelte";
  import Button from "$lib/components/Button.svelte";
  import Panel from "$lib/components/Panel.svelte";
  import MetricStrip from "$lib/components/MetricStrip.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import Sparkline from "$lib/components/Sparkline.svelte";
  import StateView from "$lib/components/StateView.svelte";
  import ActivityFeed from "./ActivityFeed.svelte";
  import { getDemo } from "$lib/demo/store.svelte";
  import { statusTone } from "$lib/demo/models";
  import { formatNumber } from "$lib/format";

  const demo = getDemo();
  let query = $state("");
  let selectedId = $state<string | null>(null);
  let collapsed = $state(false);
  let feedback = $state("");
  const visible = $derived(
    demo.data.workers.filter((worker) =>
      `${worker.name} ${worker.pool} ${worker.id}`
        .toLowerCase()
        .includes(query.trim().toLowerCase()),
    ),
  );
  const selected = $derived(
    visible.find((worker) => worker.id === selectedId) ?? visible[0],
  );
  const items = $derived(
    visible.map((worker) => ({
      id: worker.id,
      label: worker.name,
      description: `${worker.pool} · ${worker.concurrency} slots`,
      badge: worker.status,
      tone: statusTone[worker.status],
    })),
  );
  const events = $derived(
    demo.data.activity
      .filter((event) => event.resource === selected?.name)
      .slice(0, 3),
  );
</script>

<div class="example-toolbar">
  <div>
    <h2>Resource browser</h2>
    <p>
      Select a worker to inspect its work. Collapse the list to switch with
      horizontal pills.
    </p>
  </div>
  <div class="search">
    <label class="field-label" for="example-worker-search">Find a worker</label>
    <input
      id="example-worker-search"
      class="search-input"
      type="search"
      placeholder="Name, pool, or ID"
      bind:value={query}
    />
  </div>
</div>
<p class="result-count" role="status">
  {visible.length} of {demo.data.workers.length} sample workers
</p>

<ResourceLayout
  label="Workers"
  {items}
  selectedId={selected?.id ?? null}
  onselect={(id) => {
    selectedId = id;
    feedback = "";
  }}
  bind:collapsed
  emptyMessage="No matching workers."
>
  {#if selected}
    <Panel flat title={selected.name} description={selected.id}>
      {#snippet actions()}<StatusBadge
          label={selected.status}
          tone={statusTone[selected.status]}
        />{/snippet}
      <MetricStrip
        metrics={[
          {
            label: "Capacity",
            value: selected.concurrency,
            unit: "slots",
            description: selected.pool,
          },
          {
            label: "Completed",
            value: formatNumber(selected.completed),
            description: "Tasks in this sample",
          },
          {
            label: "Duration",
            value: selected.latencyMs,
            unit: "ms",
            description: "Sample task duration",
          },
        ]}
      />
      <div class="detail-columns">
        <div>
          <section class="task" aria-label="Current task">
            <h3>Current task</h3>
            <p>
              {selected.currentTask ??
                (selected.status === "error"
                  ? "Upstream connection timed out. Retry to resume processing."
                  : "No task in progress.")}
            </p>
            <Button
              variant={selected.status === "running" ||
              selected.status === "idle"
                ? "default"
                : "primary"}
              onclick={() => {
                if (!selected) return;
                const worker = selected;
                demo.changeWorker(worker);
                feedback = `${worker.name} is now ${worker.status}.`;
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
          </section>
          <section class="history" aria-label="Task history">
            <div class="section-heading">
              <h3>Task history</h3>
              <span>12 sample intervals</span>
            </div>
            <Sparkline
              height={64}
              values={selected.history}
              label={`${selected.name} completed tasks per sample interval: ${selected.history.join(", ")}`}
            />
          </section>
        </div>
        <Panel
          flat
          title="Recent activity"
          description="Events for this worker"
        >
          <ActivityFeed {events} compact />
        </Panel>
      </div>
    </Panel>
  {:else}
    <StateView
      state={{
        kind: "empty",
        title: "No matching workers",
        description: "Try a different name, pool, or worker ID.",
      }}
    />
    <Button onclick={() => (query = "")}>Clear search</Button>
  {/if}
</ResourceLayout>
<p class="feedback" role="status" aria-live="polite">{feedback}</p>
<p class="sample-note">Actions update sample data in this browser session.</p>

<style>
  .example-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 16px 24px;
    padding-top: 24px;
  }
  .example-toolbar p {
    color: var(--ink-secondary);
    font-size: 12px;
    max-width: 54ch;
    margin-top: 4px;
  }
  .search {
    width: min(240px, 100%);
  }
  .result-count {
    padding: 16px 0;
    color: var(--ink-tertiary);
    font: 10px var(--font-data);
  }
  .detail-columns {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(200px, 1fr);
    gap: 24px;
  }
  .task {
    border-bottom: 1px solid var(--line);
    padding-bottom: 20px;
  }
  .task p {
    color: var(--ink-secondary);
    font-size: 12px;
    margin: 8px 0 16px;
  }
  .history {
    padding-top: 20px;
  }
  .history span,
  .sample-note {
    color: var(--ink-tertiary);
    font-size: 10px;
  }
  .feedback {
    color: var(--accent);
    font-size: 12px;
    min-height: 20px;
    margin-top: 16px;
  }
  @media (max-width: 1200px) {
    .detail-columns {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
