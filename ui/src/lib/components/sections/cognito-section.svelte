<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import PoolDetail from "$lib/components/cognito/pool-detail.svelte";
  import { getDashboard, getDashboardFilters } from "$lib/state.svelte";
  import { matchesResourceFilter } from "$lib/filter-utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const pools = $derived(
    (dashboard.data?.cognitoPools ?? []).filter((p) => matchesResourceFilter("userpool", filters.tagFilter, p.tags)),
  );
  const totalCodes = $derived(pools.reduce((n, p) => n + p.pendingCodes, 0));

  let selectedId = $state<string | null>(null);
  let query = $state("");

  // Keyed by ID: polling replaces the objects, so holding one would freeze the detail.
  const selected = $derived(pools.find((p) => p.id === selectedId) ?? pools[0] ?? null);
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return pools;
    return pools.filter((p) => p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q));
  });

  function select(id: string) {
    selectedId = id;
    history.replaceState(null, "", `#cognito?${new URLSearchParams({ pool: id })}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((p) => p.id === selected?.id);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].id);
  }

  onMount(() => {
    selectedId = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("pool");
  });
</script>

<div class="cognito">
  <SectionHeader
    title="Cognito"
    description="{pools.length} user pool{pools.length === 1 ? '' : 's'}{totalCodes ? ` · ${totalCodes} code${totalCodes === 1 ? '' : 's'} waiting` : ''} · email and SMS are never sent"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Filter <span>{filters.tagFilter}</span></span>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(4) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if pools.length === 0}
    <div class="blank">
      <h2>{filters.tagFilter ? "No user pools match this filter" : "No user pools yet"}</h2>
      <p>Create one with the AWS CLI, an SDK or Terraform (endpoint key <code>cognitoidp</code>) and it shows up here.</p>
      <pre>aws cognito-idp create-user-pool --pool-name my-app --endpoint-url {dashboard.data?.config.endpoint ?? "http://localhost:4566"}</pre>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-cognito-list-width">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="pool-list" onkeydown={onKeydown}>
          {#if pools.length > 6}
            <label class="search">
              <MagnifyingGlassIcon size={12} />
              <input placeholder="Filter pools" bind:value={query} aria-label="Filter user pools" />
              <span class="count">{visible.length}</span>
            </label>
          {/if}
          <div class="rows">
            {#each visible as p (p.id)}
              <RcListRow
                title={p.name}
                sub="{p.users} user{p.users === 1 ? '' : 's'} · {p.id}"
                selected={p.id === selected?.id}
                onclick={() => select(p.id)}
              >
                {#snippet trailing()}
                  {#if p.pendingCodes}<span class="codes" title="Pending codes">{p.pendingCodes}</span>{/if}
                {/snippet}
              </RcListRow>
            {:else}
              <p class="none">No match for “{query}”</p>
            {/each}
          </div>
        </div>
      </RcResizableAside>

      {#if selected}
        {#key selected.id}
          <PoolDetail summary={selected} refreshKey={dashboard.data?.timestamp} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .cognito { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) { .layout { grid-template-columns: minmax(0, 1fr); } }

  .filter {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  .pool-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary); transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .codes {
    min-width: 20px; height: 20px; padding: 0 6px; border-radius: 6px; font-size: 10.5px; line-height: 18px; text-align: center;
    font-variant-numeric: tabular-nums; color: var(--accent-amber);
    border: 1px solid color-mix(in srgb, var(--accent-amber) 45%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 10%, transparent);
  }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); }
  .blank pre {
    display: inline-block; margin-top: 12px; padding: 8px 12px; border-radius: 8px; background: var(--bg-app);
    border: 1px solid var(--border-subtle); font: 11.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary); user-select: all;
  }
  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) { .blank, .skeleton-list span { animation: none; } }
</style>
