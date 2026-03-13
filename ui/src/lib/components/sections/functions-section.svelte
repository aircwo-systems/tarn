<script lang="ts">
  import { LightningIcon } from "phosphor-svelte";
  import { TableRow, TableCell } from "$lib/components/ui/table";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { formatBytes, formatDate } from "$lib/utils";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesTagFilter(fn.tags, filters.tagFilter),
    ),
  );

  function stateBadgeVariant(state: string) {
    const s = state.toLowerCase();
    if (s === "active") return "default" as const;
    if (s === "pending") return "amber" as const;
    if (s === "failed" || s === "inactive") return "destructive" as const;
    return "outline" as const;
  }
</script>

<ResourceTable
  title="Lambda Functions"
  count={functions.length}
  loading={dashboard.loading && !dashboard.data}
  empty={functions.length === 0}
  emptyMessage="No functions created yet."
  emptyIcon={LightningIcon}
  columns={[
    "Name",
    "Runtime",
    "State",
    "Messages Processed",
    "Memory",
    "Timeout",
    "Code",
    "Layers",
    "Tags",
    "Updated",
  ]}
>
  {#each functions as fn}
    <TableRow>
      <TableCell><ArnCell name={fn.name} arn={fn.arn} /></TableCell>
      <TableCell><Badge variant="secondary">{fn.runtime}</Badge></TableCell>
      <TableCell
        ><Badge variant={stateBadgeVariant(fn.state)}>{fn.state}</Badge
        ></TableCell
      >
      <TableCell class="font-mono text-muted-foreground"
        >{fn.messagesProcessed}</TableCell
      >
      <TableCell class="font-mono text-muted-foreground"
        >{fn.memoryMB} MB</TableCell
      >
      <TableCell class="font-mono text-muted-foreground"
        >{fn.timeoutSec}s</TableCell
      >
      <TableCell class="font-mono text-muted-foreground"
        >{formatBytes(fn.codeSize)}</TableCell
      >
      <TableCell class="text-muted-foreground">{fn.layers}</TableCell>
      <TableCell class="text-muted-foreground">{fn.tagCount}</TableCell>
      <TableCell class="text-muted-foreground/70 text-xs"
        >{formatDate(fn.lastModified)}</TableCell
      >
    </TableRow>
  {/each}
</ResourceTable>
