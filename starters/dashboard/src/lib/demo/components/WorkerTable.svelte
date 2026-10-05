<script lang="ts">
  import ResourceTable, {
    type Column,
  } from "$lib/components/ResourceTable.svelte";
  import StatusBadge from "$lib/components/StatusBadge.svelte";
  import { statusTone, type Worker } from "$lib/demo/models";
  import { formatNumber } from "$lib/format";
  let {
    workers,
    selectedId,
    onselect,
  }: {
    workers: readonly Worker[];
    selectedId?: string;
    onselect?: (worker: Worker) => void;
  } = $props();
  const columns: readonly Column[] = [
    { key: "name", label: "Worker" },
    { key: "status", label: "Status" },
    { key: "pool", label: "Pool", hideOnMobile: true },
    { key: "completed", label: "Completed", hideOnMobile: true },
  ];
</script>

<ResourceTable
  label="Worker inventory"
  rows={workers}
  {columns}
  {selectedId}
  {onselect}
>
  {#snippet cell(worker: Worker, column: Column)}
    {#if column.key === "name"}<span class="worker-name">{worker.name}</span
      ><span class="worker-id">{worker.id}</span>
    {:else if column.key === "status"}<StatusBadge
        label={worker.status}
        tone={statusTone[worker.status]}
      />
    {:else if column.key === "pool"}{worker.pool}
    {:else if column.key === "completed"}<span class="mono"
        >{formatNumber(worker.completed)}</span
      >{/if}
  {/snippet}
</ResourceTable>

<style>
  .worker-name {
    display: block;
    color: var(--ink);
    font-weight: 500;
    white-space: nowrap;
  }
  .worker-id {
    display: block;
    font: 10px var(--font-data);
    color: var(--ink-tertiary);
    margin-top: 3px;
  }
</style>
