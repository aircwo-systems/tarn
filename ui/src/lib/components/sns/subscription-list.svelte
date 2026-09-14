<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import type { SubscriptionSummary } from "$lib/types";

  let {
    subscriptions,
    selectedArn,
    onselect,
  }: {
    subscriptions: SubscriptionSummary[];
    selectedArn: string | null;
    onselect: (subscriptionArn: string) => void;
  } = $props();

  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return subscriptions;
    return subscriptions.filter(
      (sub) =>
        sub.topicName.toLowerCase().includes(q) ||
        sub.protocol.toLowerCase().includes(q) ||
        sub.endpoint.toLowerCase().includes(q),
    );
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((sub) => sub.subscriptionArn === selectedArn);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].subscriptionArn);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="subscription-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter subscriptions" bind:value={query} aria-label="Filter subscriptions" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as sub (sub.subscriptionArn)}
      <RcListRow
        mono
        title={sub.endpoint}
        sub="{sub.topicName} · {sub.protocol}"
        selected={sub.subscriptionArn === selectedArn}
        onclick={() => onselect(sub.subscriptionArn)}
      >
        {#snippet trailing()}
          {#if sub.filterPolicy}
            <span class="filtered">filtered</span>
          {/if}
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .subscription-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-tertiary);
    transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .filtered {
    font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--accent-green);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
