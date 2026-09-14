<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import type { GatewaySummary } from "$lib/types";

  let {
    gateways,
    selectedId,
    onselect,
  }: {
    gateways: GatewaySummary[];
    selectedId: string | null;
    onselect: (apiId: string) => void;
  } = $props();

  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return gateways;
    return gateways.filter((gateway) =>
      `${gateway.name} ${gateway.protocolType} ${gateway.defaultStage}`.toLowerCase().includes(q),
    );
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((gateway) => gateway.apiId === selectedId);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].apiId);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="gateway-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter gateways" bind:value={query} aria-label="Filter gateways" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as gateway (gateway.apiId)}
      <RcListRow
        mono
        title={gateway.name}
        sub="{gateway.protocolType} {gateway.version} · {gateway.routes} routes"
        selected={gateway.apiId === selectedId}
        onclick={() => onselect(gateway.apiId)}
      >
        {#snippet trailing()}
          {#if gateway.integrations > 0}
            <span class="calls" title="{gateway.integrations} integrations">{gateway.integrations}</span>
          {/if}
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .gateway-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
  .calls { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
