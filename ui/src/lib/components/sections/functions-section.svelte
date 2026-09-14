<script lang="ts">
  import { LightningIcon } from "phosphor-svelte";
  import {
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import MetricCard from "$lib/components/common/metric-card.svelte";
  import SectionHeader from "./section-header.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { formatBytes, formatDate } from "$lib/utils";
  import type { FunctionSummary } from "$lib/types";
  import LambdaDetailDialog from "./lambda-detail-dialog.svelte";

  let selectedFn = $state<FunctionSummary | null>(null);
  let dialogOpen = $state(false);

  function openDetail(fn: FunctionSummary) {
    selectedFn = fn;
    dialogOpen = true;
  }

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesTagFilter(fn.tags, filters.tagFilter),
    ),
  );

  const activeFunctions = $derived(
    functions.filter((fn) => fn.state.toLowerCase() === "active").length,
  );
  const runtimeCount = $derived(new Set(functions.map((fn) => fn.runtime)).size);
  const totalMessagesProcessed = $derived(
    functions.reduce((total, fn) => total + fn.messagesProcessed, 0),
  );

  const numberFormatter = new Intl.NumberFormat("en-GB");

  function stateColor(state: string): "green" | "amber" | "red" | "gray" {
    const normalized = state.toLowerCase();
    if (normalized === "active") return "green";
    if (normalized === "pending") return "amber";
    if (normalized === "failed" || normalized === "inactive") return "red";
    return "gray";
  }
</script>

<div class="flex min-h-full flex-col gap-4">
  <SectionHeader
    title="Lambda Functions"
    description="Runtime, state, throughput and deployment footprint in one place."
    icon={LightningIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <div class="flex items-center gap-1.5 rounded-md border border-border/70 bg-card/60 px-2.5 py-1 text-xs">
          <span class="text-muted-foreground">Filter:</span>
          <span class="font-mono text-foreground" title={filters.tagFilter}>
            {filters.tagFilter}
          </span>
        </div>
      {/if}
    {/snippet}
  </SectionHeader>

  <!-- Metric Cards Row -->
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
    <MetricCard
      label="Total Functions"
      value={functions.length}
      sub="{runtimeCount} unique runtime{runtimeCount === 1 ? '' : 's'}"
    />
    <MetricCard
      label="Active Functions"
      value={activeFunctions}
      valueColor="var(--accent-green)"
      sub="{functions.length - activeFunctions} pending / inactive"
    />
    <MetricCard
      label="Messages Processed"
      value={numberFormatter.format(totalMessagesProcessed)}
      sub="Total cumulative invocations"
    />
    <MetricCard
      label="Runtime Environments"
      value={runtimeCount}
      sub="Active engines"
    />
  </div>

  <div class="min-h-0 flex-1 overflow-hidden rounded-md border border-border/70 bg-card/30">
    {#if dashboard.loading && !dashboard.data}
      <div class="space-y-2 p-3">
        {#each Array(6) as _, index (index)}
          <Skeleton class="h-10 w-full" />
        {/each}
      </div>
    {:else if functions.length === 0}
      <div class="flex h-full min-h-[18rem] items-center justify-center">
        <EmptyState
          message="No functions created yet."
          icon={LightningIcon}
        />
      </div>
    {:else}
      <div class="h-full overflow-auto">
        <Table>
          <TableHeader class="sticky top-0 z-10">
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Runtime</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Messages Processed</TableHead>
              <TableHead>Memory</TableHead>
              <TableHead>Timeout</TableHead>
              <TableHead>Code</TableHead>
              <TableHead>Layers</TableHead>
              <TableHead>Tags</TableHead>
              <TableHead>Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each functions as fn}
              <TableRow
                class="cursor-pointer"
                onclick={() => openDetail(fn)}
              >
                <TableCell><ArnCell name={fn.name} arn={fn.arn} /></TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {fn.runtime}
                </TableCell>
                <TableCell>
                  <span class="inline-flex items-center gap-1.5 text-xs">
                    <LedDot color={stateColor(fn.state)} />
                    <span class="text-muted-foreground capitalize">{fn.state}</span>
                  </span>
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {numberFormatter.format(fn.messagesProcessed)}
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {fn.memoryMB} MB
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {fn.timeoutSec}s
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {formatBytes(fn.codeSize)}
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {fn.layers > 0 ? fn.layers : "—"}
                </TableCell>
                <TableCell>
                  {#if fn.tags && Object.keys(fn.tags).length > 0}
                    <span class="font-mono text-xs text-muted-foreground">
                      {Object.keys(fn.tags).length} tags
                    </span>
                  {:else}
                    <span class="text-xs text-muted-foreground/40">—</span>
                  {/if}
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {formatDate(fn.lastModified)}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      </div>
    {/if}
  </div>
</div>

<LambdaDetailDialog bind:open={dialogOpen} fn={selectedFn} />
