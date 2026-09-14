<script lang="ts">
  import { onMount } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import FunctionList from "$lib/components/functions/function-list.svelte";
  import FunctionDetail from "$lib/components/functions/function-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";

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

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  const selected = $derived(
    functions.find((fn) => fn.name === selectedName) ?? functions[0] ?? null,
  );

  const activeCount = $derived(functions.filter((fn) => fn.state.toLowerCase() === "active").length);
  const totalInvocations = $derived(functions.reduce((n, fn) => n + (fn.invocations ?? 0), 0));

  function select(name: string) {
    selectedName = name;
    history.replaceState(null, "", `#functions?fn=${encodeURIComponent(name)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const fn = qs ? new URLSearchParams(qs).get("fn") : null;
    if (fn) selectedName = fn;
  });
</script>

<div class="functions">
  <SectionHeader
    title="Lambda Functions"
    description="{functions.length} functions · {activeCount} active · {totalInvocations.toLocaleString('en-GB')} invocations"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Tag <span>{filters.tagFilter}</span></span>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if functions.length === 0}
    <div class="blank">
      <h2>No functions yet</h2>
      <p>Create one with <code>aws lambda create-function</code> or deploy through your IaC, and it appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-functions-list-width">
        <FunctionList {functions} selectedName={selected?.name ?? null} onselect={select} />
      </RcResizableAside>
      {#if selected}
        {#key selected.name}
          <FunctionDetail fn={selected} data={dashboard.data} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .functions { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .filter {
    display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
