<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import SecretDetail from "$lib/components/secrets/secret-detail.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { timeAgo } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const secrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesTagFilter(s.tags, filters.tagFilter),
    ),
  );

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-secrets-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let selectedName = $state<string | null>(null);
  let query = $state("");

  // Keyed by name: polling replaces the objects, so holding one would freeze the detail panel.
  const selected = $derived(
    secrets.find((s) => s.name === selectedName) ?? secrets[0] ?? null,
  );

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return secrets;
    return secrets.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        (s.description ?? "").toLowerCase().includes(q),
    );
  });

  function select(name: string) {
    selectedName = name;
    history.replaceState(null, "", `#secrets?${new URLSearchParams({ name })}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((s) => s.name === selected?.name);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    selectedName = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("name");
    try {
      const saved = localStorage.getItem("tarn-secrets-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
  });
</script>

<div class="secrets">
  <SectionHeader
    title="Secrets Manager"
    description="{secrets.length} secret{secrets.length === 1 ? '' : 's'} · values stay hidden until revealed"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter secrets..."
          bind:value={query}
          aria-label="Filter secrets"
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
        {#each Array(5) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if secrets.length === 0}
    <div class="blank">
      <h2>{filters.tagFilter ? "No secrets match this tag filter" : "No secrets yet"}</h2>
      <p>Create one from your terminal, or deploy through your IaC, and it shows up here.</p>
      <pre>tarn secrets create --name my-secret --value '&#123;"password":"hunter2"&#125;'</pre>
    </div>
  {:else}
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Secrets overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand secret list"
              aria-label="Expand secret list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Secret list</span>
              <span class="count-badge">{secrets.length}</span>
            </button>
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Secret switcher" onkeydown={onKeydown}>
              {#each visible as s (s.name)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={s.name === selected?.name}
                  aria-selected={s.name === selected?.name}
                  onclick={() => select(s.name)}
                  title="{s.name} ({s.description || 'Secret'})"
                >
                  <span class="chip-dot" style:background="var(--accent-amber, #f59e0b)"></span>
                  <span class="chip-name">{s.name}</span>
                  <span class="chip-badge">{timeAgo(s.lastChangedDate)}</span>
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
          storageKey="tarn-secrets-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="secret-list" onkeydown={onKeydown}>
            <div class="search-row">
              <label class="search">
                <MagnifyingGlassIcon size={12} />
                <input placeholder="Filter secrets" bind:value={query} aria-label="Filter secrets" />
                <span class="count">{visible.length}</span>
              </label>
              <button
                type="button"
                class="collapse-list-btn"
                onclick={toggleListCollapse}
                title="Collapse secret list"
                aria-label="Collapse secret list"
              >
                <SidebarSimpleIcon size={13} />
              </button>
            </div>
            <div class="rows">
              {#each visible as s (s.name)}
                <RcListRow
                  mono
                  title={s.name}
                  sub={s.description || `${s.tagCount} tag${s.tagCount === 1 ? "" : "s"}`}
                  selected={s.name === selected?.name}
                  onclick={() => select(s.name)}
                >
                  {#snippet trailing()}<span class="age">{timeAgo(s.lastChangedDate)}</span>{/snippet}
                </RcListRow>
              {:else}
                <p class="none">No match for “{query}”</p>
              {/each}
            </div>
          </div>
        </RcResizableAside>
      {/if}

      {#if selected}
        {#key selected.name}
          <SecretDetail secret={selected} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .secrets { display: flex; flex-direction: column; min-height: 100%; }
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

  .secret-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: #ffffff; color: var(--text-tertiary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
    transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }

  :global(.dark) .search {
    background: var(--bg-element);
    box-shadow: none;
  }

  .rows { display: flex; flex-direction: column; gap: 2px; }
  .age { font-size: 10.5px; white-space: nowrap; color: var(--text-tertiary); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
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
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
