<script lang="ts">
  import { matchesResourceFilter } from "#lib/filter-utils";
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "#lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "#lib/components/rack/rc-resizable-aside.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import KeyDetail from "#lib/components/kms/key-detail.svelte";
  import { getDashboard, getDashboardFilters } from "#lib/state.svelte";
  import { kmsKeyIsNamed, kmsKeyMatches, kmsKeyName, kmsOperationCount, kmsStateLabel, kmsStateTone } from "#lib/kms";
  import type { KmsKeySummary } from "#lib/types";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    onNavigate = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    onNavigate?: (target: string) => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const keys = $derived(
    (dashboard.data?.kmsKeys ?? []).filter((k) => matchesResourceFilter("kmskey", filters.tagFilter, k.tags)),
  );
  const aliasCount = $derived(keys.reduce((n, k) => n + k.aliases.length, 0));

  let listCollapsed = $state(false);
  // A key ID, or whatever a deep link named the key by (ARN or alias).
  let selectedRef = $state<string | null>(null);
  let query = $state("");

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-kms-list-collapsed", String(listCollapsed));
    } catch {}
  }

  // Resolved from fresh data each poll: polling replaces the objects, so
  // holding one would freeze the detail panel.
  const selected = $derived((selectedRef && keys.find((k) => kmsKeyIsNamed(k, selectedRef!))) || keys[0] || null);
  const visible = $derived(keys.filter((k) => kmsKeyMatches(k, query)));
  const groups = $derived(
    [
      { label: "Customer managed", keys: visible.filter((k) => k.manager !== "AWS") },
      { label: "AWS managed", keys: visible.filter((k) => k.manager === "AWS") },
    ].filter((g) => g.keys.length > 0),
  );
  // Keyboard order follows the grouped list.
  const ordered = $derived(groups.flatMap((g) => g.keys));

  function select(id: string) {
    selectedRef = id;
    history.replaceState(null, "", `#kms?${new URLSearchParams({ key: id })}`);
  }

  function subtitle(k: KmsKeySummary): string {
    if (k.description) return k.description;
    return k.aliases.length ? k.id : "No alias";
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (ordered.length === 0) return;
    e.preventDefault();
    const idx = ordered.findIndex((k) => k.id === selected?.id);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(ordered.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(ordered[next].id);
  }

  // The command palette and Cognito link to #kms?key=…, which may arrive while
  // this section is already open, or before the first poll.
  function readHash() {
    const ref = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("key");
    if (ref) selectedRef = ref;
  }

  onMount(() => {
    readHash();
    try {
      const saved = localStorage.getItem("tarn-kms-list-collapsed");
      if (saved !== null) listCollapsed = saved === "true";
    } catch {}
    window.addEventListener("hashchange", readHash);
    return () => window.removeEventListener("hashchange", readHash);
  });

  const endpoint = $derived(dashboard.data?.config?.endpoint ?? "http://localhost:4566");
</script>

<div class="kms">
  <SectionHeader
    title="KMS"
    description="{keys.length} key{keys.length === 1 ? '' : 's'} · {aliasCount} alias{aliasCount === 1 ? '' : 'es'} · usage counted since Tarn started"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input placeholder="Filter keys..." bind:value={query} aria-label="Filter keys" />
        {#if query}
          <button type="button" class="clear-query-btn" onclick={() => (query = "")} aria-label="Clear filter">&times;</button>
        {/if}
      </div>
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
  {:else if keys.length === 0}
    <div class="blank">
      <h2>{filters.tagFilter ? "No keys match this filter" : "No KMS keys yet"}</h2>
      <p>Create one with the AWS CLI or Terraform, or point a Cognito pool's <code>KMSKeyID</code> at one, and it shows up here.</p>
      <pre>aws --endpoint-url {endpoint} kms create-key --description app</pre>
    </div>
  {:else}
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Keys overview">
          <div class="toolbar-leading">
            <button type="button" class="expand-list-btn" onclick={toggleListCollapse} title="Expand key list" aria-label="Expand key list">
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Key list</span>
              <span class="count-badge">{keys.length}</span>
            </button>
          </div>
          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Key switcher" onkeydown={onKeydown}>
              {#each ordered as k (k.id)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={k.id === selected?.id}
                  aria-selected={k.id === selected?.id}
                  onclick={() => select(k.id)}
                  title="{kmsKeyName(k)} ({kmsStateLabel(k.state)})"
                >
                  <span class="chip-name">{kmsKeyName(k)}</span>
                  {#if k.state !== "Enabled"}<span class="chip-badge">{kmsStateLabel(k.state)}</span>{/if}
                </button>
              {:else}
                <span class="chips-none">No match for "{query}"</span>
              {/each}
            </div>
          </div>
        </div>
      {:else}
        <RcResizableAside storageKey="tarn-kms-list-width" collapsible={true} onToggleCollapse={toggleListCollapse}>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="key-list" onkeydown={onKeydown}>
            <div class="search-row">
              <label class="search">
                <MagnifyingGlassIcon size={12} />
                <input placeholder="Filter keys" bind:value={query} aria-label="Filter keys" />
                <span class="count">{visible.length}</span>
              </label>
              <button type="button" class="collapse-list-btn" onclick={toggleListCollapse} title="Collapse key list" aria-label="Collapse key list">
                <SidebarSimpleIcon size={13} />
              </button>
            </div>
            {#each groups as group (group.label)}
              <div class="group">
                {#if groups.length > 1 || group.label !== "Customer managed"}
                  <h3>{group.label}</h3>
                {/if}
                <div class="rows">
                  {#each group.keys as k (k.id)}
                    <RcListRow mono title={kmsKeyName(k)} sub={subtitle(k)} selected={k.id === selected?.id} onclick={() => select(k.id)}>
                      {#snippet trailing()}
                        {#if k.state !== "Enabled"}
                          <RcTonePill tone={kmsStateTone(k.state)}>{kmsStateLabel(k.state)}</RcTonePill>
                        {:else if k.usage.failures > 0}
                          <span class="ops fail">{k.usage.failures} failed</span>
                        {:else}
                          <span class="ops">{kmsOperationCount(k)} ops</span>
                        {/if}
                      {/snippet}
                    </RcListRow>
                  {/each}
                </div>
              </div>
            {:else}
              <p class="none">No match for “{query}”</p>
            {/each}
          </div>
        </RcResizableAside>
      {/if}

      {#if selected}
        {#key selected.id}
          <KeyDetail key={selected} {onNavigate} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .kms { display: flex; flex-direction: column; min-height: 100%; }
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
  :global(.dark) .filter { background: var(--bg-element); box-shadow: none; }

  .key-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: #ffffff; color: var(--text-tertiary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
    transition: border-color 120ms ease;
  }
  :global(.dark) .search { background: var(--bg-element); box-shadow: none; }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }

  .group + .group { margin-top: 10px; }
  h3 {
    padding: 4px 10px 6px; font-size: 10.5px; font-weight: 500; letter-spacing: 0.06em;
    text-transform: uppercase; color: var(--text-tertiary);
  }
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .ops { font-size: 10.5px; white-space: nowrap; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .ops.fail { color: var(--accent-red); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font: 11.5px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
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
