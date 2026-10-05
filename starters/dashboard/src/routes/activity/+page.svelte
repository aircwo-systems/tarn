<script lang="ts">
  import PageHeader from "$lib/components/PageHeader.svelte";
  import { product } from "$lib/config";
  import { getDemo } from "$lib/demo/store.svelte";
  import type { Activity } from "$lib/demo/models";
  import ActivityFeed from "$lib/demo/components/ActivityFeed.svelte";
  import StateView from "$lib/components/StateView.svelte";
  const demo = getDemo();
  let query = $state("");
  let level = $state<"all" | Activity["level"]>("all");
  const levels: readonly ("all" | Activity["level"])[] = [
    "all",
    "info",
    "success",
    "warning",
    "error",
  ];
  const visible = $derived(
    demo.data.activity.filter(
      (event) =>
        (level === "all" || event.level === level) &&
        `${event.resource} ${event.message}`
          .toLowerCase()
          .includes(query.toLowerCase().trim()),
    ),
  );
</script>

<svelte:head><title>Activity | {product.name}</title></svelte:head>
<PageHeader title="Activity" description="Sample events · all times UTC" />
<div class="activity-toolbar">
  <div class="search">
    <label class="field-label" for="activity-search">Search activity</label
    ><input
      id="activity-search"
      type="search"
      class="search-input"
      placeholder="Search resource or message…"
      bind:value={query}
    />
  </div>
  <div class="filter-strip" role="group" aria-label="Filter activity by level">
    {#each levels as item}<button
        aria-pressed={level === item}
        onclick={() => (level = item)}
        >{item === "all"
          ? "All events"
          : item.charAt(0).toUpperCase() + item.slice(1)}</button
      >{/each}
  </div>
</div>
<div class="count" role="status">{visible.length} events</div>
{#if visible.length}<ActivityFeed events={visible} />{:else}<StateView
    state={{
      kind: "empty",
      title: "No matching events",
      description: "Change the level filter or search for a different message.",
    }}
  />{/if}

<style>
  .activity-toolbar {
    display: flex;
    align-items: end;
    flex-wrap: wrap;
    gap: var(--space-6);
    border-top: 1px solid var(--line);
    padding-top: var(--space-5);
  }
  .search {
    width: min(340px, 100%);
  }
  .count {
    font: 11px var(--font-data);
    color: var(--ink-tertiary);
    padding-block: var(--space-5) var(--space-4);
  }
</style>
