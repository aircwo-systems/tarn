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
    title="Lambda functions"
    description="Runtime, state, throughput and deployment footprint in one place."
    icon={LightningIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="flex flex-wrap items-center gap-4 text-xs font-mono text-muted-foreground">
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{functions.length}</span>
        <span class="text-muted-foreground/70">visible</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <LedDot color="green" />
        <span class="font-mono text-foreground">{activeFunctions}</span>
        <span class="text-muted-foreground/70">active</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{runtimeCount}</span>
        <span class="text-muted-foreground/70">runtimes</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">
          {numberFormatter.format(totalMessagesProcessed)}
        </span>
        <span class="text-muted-foreground/70">messages processed</span>
      </span>
      {#if filters.tagFilter}
        <span class="text-muted-foreground/50">filter</span>
        <span class="truncate text-foreground/85" title={filters.tagFilter}>
          {filters.tagFilter}
        </span>
      {/if}
      </div>
    {/snippet}
  </SectionHeader>

  <div class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border/70 bg-background/60">
    {#if dashboard.loading && !dashboard.data}
      <div class="space-y-2 p-3">
        {#each Array(6) as _, index (index)}
          <Skeleton class="h-11 w-full" />
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
          <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
            <TableRow class="hover:bg-transparent">
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
                class="cursor-pointer transition-colors hover:bg-muted/30"
                onclick={() => openDetail(fn)}
              >
                <TableCell><ArnCell name={fn.name} arn={fn.arn} /></TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {fn.runtime}
                </TableCell>
                <TableCell>
                  <span class="inline-flex items-center gap-1.5 text-xs">
                    <LedDot color={stateColor(fn.state)} />
                    <span class="text-muted-foreground">{fn.state}</span>
                  </span>
                </TableCell>
                <TableCell class="font-mono text-muted-foreground">
                  {numberFormatter.format(fn.messagesProcessed)}
                </TableCell>
                <TableCell class="font-mono text-muted-foreground">
                  {fn.memoryMB} MB
                </TableCell>
                <TableCell class="font-mono text-muted-foreground">
                  {fn.timeoutSec}s
                </TableCell>
                <TableCell class="font-mono text-muted-foreground">
                  {formatBytes(fn.codeSize)}
                </TableCell>
                <TableCell class="text-muted-foreground">{fn.layers}</TableCell>
                <TableCell class="text-muted-foreground">{fn.tagCount}</TableCell>
                <TableCell class="text-xs text-muted-foreground/70">
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
