<script lang="ts">
  import {
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import { ArrowClockwiseIcon } from "phosphor-svelte";
  import EmptyState from "./empty-state.svelte";

  let {
    title,
    count = 0,
    loading = false,
    empty = false,
    emptyMessage = "No items created yet.",
    emptyIcon,
    columns = [],
    onRefresh,
    children,
  }: {
    title: string;
    count?: number;
    loading?: boolean;
    empty?: boolean;
    emptyMessage?: string;
    emptyIcon?: any;
    columns?: string[];
    onRefresh?: () => void;
    children?: import("svelte").Snippet;
  } = $props();
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden rounded-md border border-border/70 bg-card/30">
  <div
    class="flex items-center justify-between border-b border-border/70 bg-card/60 px-3.5 py-2.5"
  >
    <h3 class="text-[13px] font-medium text-foreground">{title}</h3>
    <div class="flex items-center gap-2">
      <span class="font-mono text-[11px] text-muted-foreground/80 tabular-nums"
        >{count} items</span
      >
      {#if onRefresh}
        <button
          type="button"
          onclick={onRefresh}
          class="flex h-6 w-6 items-center justify-center rounded-md border border-border/40 text-muted-foreground transition-colors hover:border-border hover:bg-muted/40 hover:text-foreground"
          aria-label="Refresh"
          title="Refresh"
        >
          <ArrowClockwiseIcon size={12} />
        </button>
      {/if}
    </div>
  </div>

  {#if loading}
    <div class="flex-1 p-3 space-y-2">
      {#each Array(5) as _, i (i)}
        <Skeleton class="h-8 w-full" />
      {/each}
    </div>
  {:else if empty}
    <div class="flex flex-1 items-center justify-center p-6">
      <EmptyState message={emptyMessage} icon={emptyIcon} />
    </div>
  {:else}
    <div class="flex-1 min-h-0 overflow-auto">
      {#if children}
        {@render children()}
      {:else}
        <Table>
          {#if columns.length > 0}
            <TableHeader>
              <TableRow>
                {#each columns as col}
                  <TableHead>{col}</TableHead>
                {/each}
              </TableRow>
            </TableHeader>
          {/if}
          <TableBody />
        </Table>
      {/if}
    </div>
  {/if}
</div>
