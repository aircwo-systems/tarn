<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import SecretDetail from "$lib/components/secrets/secret-detail.svelte";
  import { getDashboard, getDashboardFilters, matchesTagFilter } from "$lib/state.svelte";
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
    (dashboard.data?.secrets ?? []).filter((s) => matchesTagFilter(s.tags, filters.tagFilter)),
  );

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  const selected = $derived(secrets.find((s) => s.name === selectedName) ?? secrets[0] ?? null);

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return secrets;
    return secrets.filter(
      (s) => s.name.toLowerCase().includes(q) || (s.description ?? "").toLowerCase().includes(q),
    );
  });

  function select(name: string) {
    selectedName = name;
    history.replaceState(null, "", `#secrets?${new URLSearchParams({ name })}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((s) => s.name === selected?.name);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    selectedName = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("name");
  });
</script>

<div class="secrets">
  <SectionHeader
    title="Secrets Manager"
    description="{secrets.length} secret{secrets.length === 1 ? '' : 's'} · values stay hidden until revealed"
    {sidebarCollapsed}
    {onToggleSidebar}
  />

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
    <div class="layout">
      <RcResizableAside storageKey="tarn-secrets-list-width">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="secret-list" onkeydown={onKeydown}>
          <label class="search">
            <MagnifyingGlassIcon size={12} />
            <input placeholder="Filter secrets" bind:value={query} aria-label="Filter secrets" />
            <span class="count">{visible.length}</span>
          </label>
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

  .secret-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
