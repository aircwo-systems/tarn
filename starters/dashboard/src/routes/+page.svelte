<script lang="ts">
  import TopologyCanvas, {
    type FlowNode,
    type FlowEdge,
  } from "$lib/components/TopologyCanvas.svelte";
  import { statusTone } from "$lib/demo/models";
  import PageHeader from "$lib/components/PageHeader.svelte";
  import { resolve } from "$app/paths";
  import { goto } from "$app/navigation";
  import { product } from "$lib/config";
  import { getDemo } from "$lib/demo/store.svelte";
  import { throughput } from "$lib/demo/fixtures";
  import MetricStrip, { type Metric } from "$lib/components/MetricStrip.svelte";
  import Panel from "$lib/components/Panel.svelte";
  import WorkerTable from "$lib/demo/components/WorkerTable.svelte";
  import ActivityFeed from "$lib/demo/components/ActivityFeed.svelte";
  import { formatCompact } from "$lib/format";

  const demo = getDemo();
  let canvasExpanded = $state(false);
  const layout: Record<string, { x: number; y: number }> = {
    wrk_01: { x: 55, y: 125 },
    wrk_02: { x: 365, y: 125 },
    wrk_03: { x: 675, y: 125 },
    wrk_06: { x: 55, y: 285 },
    wrk_05: { x: 365, y: 285 },
    wrk_04: { x: 675, y: 285 },
  };
  const flow = $derived<FlowNode[]>(
    demo.data.workers.map((worker) => ({
      id: worker.id,
      label: worker.name,
      detail: worker.status,
      tone: statusTone[worker.status],
      ...(layout[worker.id] ?? { x: 55, y: 125 }),
    })),
  );
  const edges: FlowEdge[] = [
    { from: "wrk_01", to: "wrk_02" },
    { from: "wrk_02", to: "wrk_03" },
    { from: "wrk_06", to: "wrk_05" },
    { from: "wrk_05", to: "wrk_04" },
  ];
  const running = $derived(
    demo.data.workers.filter((worker) => worker.status === "running"),
  );
  const errors = $derived(
    demo.data.workers.filter((worker) => worker.status === "error"),
  );
  const completed = $derived(
    demo.data.workers.reduce((sum, worker) => sum + worker.completed, 0),
  );
  const metrics = $derived<Metric[]>([
    {
      label: "Active workers",
      value: `${running.length}/${demo.data.workers.length}`,
      description: "Running across all pools",
    },
    {
      label: "Completed tasks",
      value: formatCompact(completed),
      description: "Total in the sample dataset",
      trend: throughput,
    },
    {
      label: "Average duration",
      value: running.length
        ? Math.round(
            running.reduce((sum, worker) => sum + worker.latencyMs, 0) /
              running.length,
          )
        : 0,
      unit: "ms",
      description: "Running worker samples",
    },
    {
      label: "Needs attention",
      value: errors.length,
      unit: "worker",
      description: errors.length
        ? "Upstream connection timeout"
        : "No workers reporting errors",
    },
  ]);
</script>

<svelte:head><title>Overview | {product.name}</title></svelte:head>
<PageHeader
  title="Overview"
  description={`${demo.data.workers.length} workers · ${demo.data.activity.length} recent events`}
/>
{#if !canvasExpanded}<MetricStrip {metrics} />{/if}
<div class="overview-grid" class:canvas-expanded={canvasExpanded}>
  <div>
    <TopologyCanvas
      nodes={flow}
      {edges}
      bind:expanded={canvasExpanded}
      onselect={(id) =>
        goto(`${resolve("/workers")}?worker=${encodeURIComponent(id)}`)}
    />
    {#if !canvasExpanded}
      <div class="divider"></div>
      <Panel
        flat
        title="Worker inventory"
        description="Select a worker to inspect its current state"
      >
        <WorkerTable
          workers={demo.data.workers}
          onselect={(worker) =>
            goto(
              `${resolve("/workers")}?worker=${encodeURIComponent(worker.id)}`,
            )}
        />
      </Panel>
    {/if}
  </div>
  {#if !canvasExpanded}<div class="activity-pane">
      <Panel
        flat
        title="Recent activity"
        description="Sample events · times in UTC"
      >
        {#snippet actions()}<a class="text-link" href={resolve("/activity")}
            >View all</a
          >{/snippet}
        <ActivityFeed events={demo.data.activity.slice(0, 4)} compact />
      </Panel>
      <p class="sample-note">
        This workspace uses sample data. Worker actions update this browser
        session.
      </p>
    </div>{/if}
</div>

<style>
  .overview-grid.canvas-expanded {
    grid-template-columns: minmax(0, 1fr);
    margin-top: 16px;
  }
  .overview-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 280px;
    gap: var(--space-8);
  }
  .activity-pane {
    border-left: 1px solid var(--line);
    padding-left: var(--space-6);
  }
  .sample-note {
    font-size: 11px;
    color: var(--ink-tertiary);
    margin-top: var(--space-5);
  }
  @media (max-width: 1100px) {
    .overview-grid {
      grid-template-columns: minmax(0, 1fr);
    }
    .activity-pane {
      border-left: 0;
      border-top: 1px solid var(--line);
      padding-left: 0;
      padding-top: var(--space-6);
    }
  }
</style>
