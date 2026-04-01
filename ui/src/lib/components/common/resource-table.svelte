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

<div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-border/70 bg-background/60">
  <div
    class="flex items-center justify-between border-b border-border/70 bg-background/35 px-3 py-2"
  >
    <h3 class="text-sm font-semibold text-foreground">{title}</h3>
    <div class="flex items-center gap-2">
      <span class="text-xs text-muted-foreground/70 font-mono"
        >{count} items</span
      >
      {#if onRefresh}
        <button
          type="button"
          onclick={onRefresh}
          class="flex h-6 w-6 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-background-subtle hover:text-foreground"
          aria-label="Refresh"
          title="Refresh"
        >
          <ArrowClockwiseIcon size={13} />
        </button>
      {/if}
    </div>
  </div>

  {#if loading}
    <div class="flex-1 p-3 space-y-2">
      {#each Array(3) as _}
        <Skeleton class="h-8 w-full" />
      {/each}
    </div>
  {:else if empty}
    <div class="flex flex-1 items-center justify-center">
      <EmptyState message={emptyMessage} icon={emptyIcon} />
    </div>
  {:else}
    <div class="min-h-0 flex-1 overflow-auto">
      <Table>
        <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
          <TableRow>
            {#each columns as col}
              <TableHead>{col}</TableHead>
            {/each}
          </TableRow>
        </TableHeader>
        <TableBody>
          {@render children?.()}
        </TableBody>
      </Table>
    </div>
  {/if}
</div>
