<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
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

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-functions-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return functions;
    return functions.filter((fn) => fn.name.toLowerCase().includes(q) || fn.runtime.toLowerCase().includes(q));
  });

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  const selected = $derived(
    functions.find((fn) => fn.name === selectedName) ?? functions[0] ?? null,
  );

  const activeCount = $derived(functions.filter((fn) => fn.state.toLowerCase() === "active").length);
  const failedCount = $derived(functions.filter((fn) => fn.state.toLowerCase() === "failed").length);
  const totalInvocations = $derived(functions.reduce((n, fn) => n + (fn.invocations ?? 0), 0));

  function select(name: string) {
    selectedName = name;
    history.replaceState(null, "", `#functions?fn=${encodeURIComponent(name)}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((fn) => fn.name === selected?.name);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const fn = qs ? new URLSearchParams(qs).get("fn") : null;
    if (fn) selectedName = fn;
    try {
      const saved = localStorage.getItem("tarn-functions-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
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
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter functions..."
          bind:value={query}
          aria-label="Filter functions"
        />
        {#if query}
          <button
            type="button"
            class="clear-query-btn"
            onclick={() => (query = "")}
            aria-label="Clear filter"
          >
            &times;
          </button>
        {/if}
      </div>
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
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Functions overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand function list"
              aria-label="Expand function list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Function list</span>
              <span class="count-badge">{functions.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{activeCount} active functions">
                  <span class="status-dot green"></span>
                  <span>{activeCount} active</span>
                </span>
                <span class="toolbar-stat" title="{totalInvocations.toLocaleString('en-GB')} invocations">
                  <span class="status-dot blue"></span>
                  <span>{totalInvocations.toLocaleString('en-GB')} invocations</span>
                </span>
                {#if failedCount > 0}
                  <span class="toolbar-stat" title="{failedCount} failed functions">
                    <span class="status-dot red"></span>
                    <span>{failedCount} failed</span>
                  </span>
                {/if}
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Function switcher" onkeydown={onKeydown}>
              {#each visible as fn (fn.name)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={fn.name === selected?.name}
                  aria-selected={fn.name === selected?.name}
                  onclick={() => select(fn.name)}
                  title="{fn.name} ({fn.runtime} · {fn.memoryMB} MB)"
                >
                  <span
                    class="chip-dot"
                    style:background={fn.state.toLowerCase() === "active" ? "var(--accent-green, #10b981)" : fn.state.toLowerCase() === "failed" ? "var(--accent-red, #fb7185)" : "var(--accent-amber, #f59e0b)"}
                  ></span>
                  <span class="chip-name">{fn.name}</span>
                  {#if fn.invocations}
                    <span class="chip-badge">{fn.invocations} calls</span>
                  {:else}
                    <span class="chip-badge">{fn.runtime}</span>
                  {/if}
                </button>
              {:else}
                <span class="chips-none">No match for "{query}"</span>
              {/each}
            </div>
          </div>
        </div>
      {/if}

      {#if !listCollapsed}
        <RcResizableAside
          storageKey="tarn-functions-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <FunctionList
            {functions}
            selectedName={selected?.name ?? null}
            onselect={select}
            bind:query
            onToggleCollapse={toggleListCollapse}
          />
        </RcResizableAside>
      {/if}

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
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
    background: #ffffff;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  :global(.dark) .filter {
    background: var(--bg-element);
    box-shadow: none;
  }

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
