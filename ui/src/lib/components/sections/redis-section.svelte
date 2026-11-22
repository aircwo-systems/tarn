<script lang="ts">
  import { matchesInfrastructureFilter } from "#lib/filter-utils";
  import { onMount } from "svelte";
  import {
    ArrowSquareOutIcon,
    DatabaseIcon,
    MagnifyingGlassIcon,
    PlusIcon,
    SidebarSimpleIcon,
  } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcListRow from "#lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "#lib/components/rack/rc-resizable-aside.svelte";
  import RedisTargetDetail, {
    type RedisSubTab,
  } from "#lib/components/redis/redis-target-detail.svelte";
  import { isRedisProbeKind, redisProbeId } from "#lib/redis";
  import {
    getAccountSettings,
    getDashboard,
    getDashboardFilters,
    getVisibleInfra,
  } from "#lib/state.svelte";

  let {
    sidebarCollapsed = false,
    popout = false,
    onToggleSidebar = () => {},
    onNavigate = (_tab: string) => {},
  }: {
    sidebarCollapsed?: boolean;
    popout?: boolean;
    onToggleSidebar?: () => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const accountSettings = getAccountSettings();

  const viewerEnabled = $derived(dashboard.data?.config?.redisViewerEnabled ?? false);

  const targets = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? [])
      .filter((p) => isRedisProbeKind(p.kind) && matchesInfrastructureFilter("redis", filters.tagFilter))
      .map((probe) => ({ id: redisProbeId(probe), probe })),
  );

  const connectedCount = $derived(
    targets.filter((t) => t.probe.status === "connected").length,
  );

  let listCollapsed = $state(false);
  let selectedTargetId = $state<string | null>(null);
  let selectedDb = $state(0);
  let activeSubTab = $state<RedisSubTab>("overview");
  let query = $state("");

  const selected = $derived(
    targets.find((t) => t.id === selectedTargetId) ?? targets[0] ?? null,
  );

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return targets;
    return targets.filter((t) =>
      [t.probe.name, t.probe.host, String(t.probe.port), t.probe.status, t.probe.version ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(q),
    );
  });

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-redis-list-collapsed", String(listCollapsed));
    } catch {}
  }

  function normalizeSubTab(raw: string | null): RedisSubTab {
    if (raw === "keys" || raw === "cli" || raw === "integrations") return raw;
    return "overview";
  }

  function syncHash(targetId: string | null, db: number, tab: RedisSubTab) {
    if (!targetId) return;
    const qs = new URLSearchParams();
    qs.set("target", targetId);
    if (db > 0) qs.set("db", String(db));
    if (tab !== "overview") qs.set("tab", tab);
    history.replaceState(null, "", `#redis?${qs.toString()}`);
  }

  function select(id: string) {
    selectedTargetId = id;
    selectedDb = 0;
    syncHash(id, 0, activeSubTab);
  }

  function selectDb(db: number) {
    selectedDb = db;
    syncHash(selected?.id ?? selectedTargetId, db, activeSubTab);
  }

  function selectSubTab(tab: RedisSubTab) {
    activeSubTab = tab;
    syncHash(selected?.id ?? selectedTargetId, selectedDb, tab);
  }

  function openPopout() {
    const params = new URLSearchParams();
    params.set("popout", "redis");
    params.set("account", accountSettings.activeAccountId);

    const hashParams = new URLSearchParams();
    const target = selected?.id ?? selectedTargetId;
    if (target) hashParams.set("target", target);
    if (selectedDb > 0) hashParams.set("db", String(selectedDb));
    if (activeSubTab !== "overview") hashParams.set("tab", activeSubTab);
    const hashSuffix = hashParams.toString() ? `?${hashParams.toString()}` : "";
    const url = `${window.location.pathname}?${params.toString()}#redis${hashSuffix}`;

    const win = window.open(
      url,
      `tarn-redis-${accountSettings.activeAccountId}`,
      "popup=yes,width=1280,height=860,menubar=no,toolbar=no,location=yes,resizable=yes,scrollbars=yes",
    );
    win?.focus();
  }

  function openFullDashboard() {
    const target = selected?.id ?? selectedTargetId;
    const hashParams = new URLSearchParams();
    if (target) hashParams.set("target", target);
    if (selectedDb > 0) hashParams.set("db", String(selectedDb));
    if (activeSubTab !== "overview") hashParams.set("tab", activeSubTab);
    const hashSuffix = hashParams.toString() ? `?${hashParams.toString()}` : "";
    onNavigate(`redis${hashSuffix}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((t) => t.id === selected?.id);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].id);
  }

  function readRouteFromHash() {
    const qs = new URLSearchParams(window.location.hash.split("?")[1] ?? "");
    const target = qs.get("target");
    if (target) selectedTargetId = target;
    const db = Number(qs.get("db") ?? "0");
    if (Number.isInteger(db) && db >= 0 && db <= 15) selectedDb = db;
    activeSubTab = normalizeSubTab(qs.get("tab"));
  }

  onMount(() => {
    readRouteFromHash();
    try {
      const saved = localStorage.getItem("tarn-redis-list-collapsed");
      if (saved !== null) listCollapsed = saved === "true";
    } catch {}
    if (popout) listCollapsed = true;
    window.addEventListener("hashchange", readRouteFromHash);
    return () => {
      window.removeEventListener("hashchange", readRouteFromHash);
    };
  });
</script>

<div class="redis-section" class:popout>
  {#if popout}
    <header class="popout-bar">
      <div class="popout-brand">
        <DatabaseIcon size={15} />
        <span class="popout-title">Redis Viewer</span>
        <span class="scope-badge" title="Redis sits outside Tarn and is shared across all Tarn accounts">
          shared across accounts
        </span>
      </div>

      <div class="popout-controls">
        {#if targets.length > 0}
          <label class="popout-select">
            <span>Instance</span>
            <select
              value={selected?.id ?? ""}
              onchange={(e) => select((e.currentTarget as HTMLSelectElement).value)}
            >
              {#each targets as t (t.id)}
                <option value={t.id}>
                  {t.probe.name} ({t.probe.host}:{t.probe.port})
                </option>
              {/each}
            </select>
          </label>
        {/if}

        <RcButton small variant="ghost" onclick={openFullDashboard}>
          <ArrowSquareOutIcon size={12} /> Full dashboard
        </RcButton>
      </div>
    </header>
  {:else}
    <SectionHeader
      title="Redis"
      description="{targets.length} instance{targets.length === 1 ? '' : 's'} · {connectedCount} reachable · external service shared across account IDs"
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet actions()}
        <div class="header-actions-row">
          <div class="header-filter">
            <MagnifyingGlassIcon size={12} />
            <input placeholder="Filter instances..." bind:value={query} aria-label="Filter Redis instances" />
            {#if query}
              <button type="button" class="clear-query-btn" onclick={() => (query = "")} aria-label="Clear filter">
                &times;
              </button>
            {/if}
          </div>
          <RcButton small variant="ghost" onclick={() => onNavigate("settings?section=infra&add=service")} title="Register another Redis endpoint">
            <PlusIcon size={12} /> Add
          </RcButton>
          {#if viewerEnabled}
            <RcButton small onclick={openPopout} title="Open Redis viewer in a pop-out window">
              <ArrowSquareOutIcon size={12} /> Pop out
            </RcButton>
          {/if}
        </div>
      {/snippet}
    </SectionHeader>
  {/if}

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(4) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if !viewerEnabled}
    <div class="blank">
      <h2>Redis viewer is disabled</h2>
      <p>Start Tarn with <code>--redis-viewer</code> (or <code>--rv</code>), or enable it in Settings.</p>
      <div class="blank-actions">
        <RcButton small onclick={() => onNavigate("settings?section=infra")}>Open Settings</RcButton>
      </div>
    </div>
  {:else if targets.length === 0}
    <div class="blank">
      <h2>No Redis instances probed</h2>
      <p>Enable the Redis probe or register a <code>redis://host:port</code> service in Settings → Infrastructure.</p>
      <div class="blank-actions">
        <RcButton small onclick={() => onNavigate("settings?section=infra&add=service")}>Configure Infrastructure</RcButton>
      </div>
    </div>
  {:else}
    <div class="layout" class:list-collapsed={listCollapsed} class:popout-layout={popout}>
      {#if !popout && listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Redis instances overview">
          <div class="toolbar-leading">
            <button type="button" class="expand-list-btn" onclick={toggleListCollapse} title="Expand instance list" aria-label="Expand instance list">
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Instances</span>
              <span class="count-badge">{targets.length}</span>
            </button>
          </div>
          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Redis instance switcher" onkeydown={onKeydown}>
              {#each visible as t (t.id)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={t.id === selected?.id}
                  aria-selected={t.id === selected?.id}
                  onclick={() => select(t.id)}
                >
                  <span
                    class="chip-dot"
                    style:background={t.probe.status === "connected" ? "var(--accent-green, #10b981)" : t.probe.status === "refused" ? "var(--accent-amber, #f59e0b)" : "var(--accent-red, #fb7185)"}
                  ></span>
                  <span class="chip-name">{t.probe.name}</span>
                  <span class="chip-badge">{t.probe.host}:{t.probe.port}</span>
                </button>
              {/each}
            </div>
          </div>
        </div>
      {/if}

      {#if !popout && !listCollapsed}
        <RcResizableAside
          storageKey="tarn-redis-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="instance-list" onkeydown={onKeydown}>
            <div class="list-intro">
              <div>
                <span class="eyebrow">Instances</span>
                <p>External Redis · shared across accounts.</p>
              </div>
              <span class="list-count">{targets.length}</span>
            </div>

            <div class="search-row">
              <label class="search">
                <MagnifyingGlassIcon size={12} />
                <input placeholder="Filter instances" bind:value={query} aria-label="Filter Redis instances" />
                <span class="count">{visible.length}</span>
              </label>
              <button type="button" class="collapse-list-btn" onclick={toggleListCollapse} title="Collapse instance list" aria-label="Collapse instance list">
                <SidebarSimpleIcon size={13} />
              </button>
            </div>

            <div class="rows">
              {#each visible as t (t.id)}
                <RcListRow
                  mono
                  title={t.probe.name}
                  sub={`${t.probe.host}:${t.probe.port}${t.probe.version ? ` · v${t.probe.version}` : ""}`}
                  selected={t.id === selected?.id}
                  onclick={() => select(t.id)}
                >
                  {#snippet trailing()}
                    <span class="row-meta">
                      <span class="status" data-state={t.probe.status.toLowerCase()}>{t.probe.status.toLowerCase()}</span>
                      {#if t.probe.status === "connected" && t.probe.latencyMs > 0}
                        <span class="latency">{Math.round(t.probe.latencyMs)}ms</span>
                      {/if}
                    </span>
                  {/snippet}
                </RcListRow>
              {:else}
                <p class="none">No match for “{query}”</p>
              {/each}
            </div>
          </div>
        </RcResizableAside>
      {/if}

      {#if selected}
        {#key selected.id}
          <RedisTargetDetail
            probe={selected.probe}
            {selectedDb}
            {activeSubTab}
            {popout}
            onSelectDb={selectDb}
            onSelectSubTab={selectSubTab}
            onPopout={openPopout}
            {onNavigate}
          />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .redis-section { display: flex; flex-direction: column; min-height: 100%; }
  .header-actions-row { display: flex; align-items: center; gap: 8px; }

  .popout-bar {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    flex-wrap: wrap; padding: 0 0 12px; border-bottom: 1px solid var(--border-subtle);
  }
  .popout-brand { display: flex; align-items: center; gap: 8px; color: var(--text-primary); }
  .popout-title { font-size: 13.5px; font-weight: 600; }
  .scope-badge {
    padding: 2px 7px; border-radius: 5px; background: var(--bg-element);
    border: 1px solid var(--border-subtle); font-size: 10.5px; color: var(--text-tertiary);
  }
  .popout-controls { display: flex; align-items: center; gap: 10px; }
  .popout-select { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-tertiary); }
  .popout-select select {
    height: 28px; padding: 0 8px; border-radius: 6px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); font: 11.5px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }

  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1380px; align-items: start;
  }
  .layout.list-collapsed { display: flex; flex-direction: column; gap: 16px; }
  .layout.popout-layout { grid-template-columns: minmax(0, 1fr); max-width: none; padding-top: 14px; }
  @media (max-width: 900px) { .layout { grid-template-columns: minmax(0, 1fr); } }

  .instance-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .list-intro { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 1px 10px 3px; }
  .eyebrow { display: block; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .list-intro p { margin-top: 3px; font-size: 11px; color: var(--text-secondary); }
  .list-count { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .search-row { display: flex; align-items: center; gap: 6px; }
  .search {
    flex: 1; display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: #ffffff; color: var(--text-tertiary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04); transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }
  :global(.dark) .search { background: var(--bg-element); box-shadow: none; }

  .collapse-list-btn {
    width: 30px; height: 30px; border-radius: 8px; border: 1px solid var(--border-subtle);
    background: var(--bg-element); color: var(--text-tertiary); display: inline-flex;
    align-items: center; justify-content: center; cursor: pointer;
  }
  .collapse-list-btn:hover { color: var(--text-primary); border-color: var(--border-default); }

  .rows { display: flex; flex-direction: column; gap: 2px; }
  .row-meta { display: flex; align-items: center; gap: 7px; }
  .status { font-size: 10px; color: var(--text-tertiary); text-transform: lowercase; }
  .status[data-state="connected"] { color: var(--accent-green); }
  .status[data-state="refused"] { color: var(--accent-amber); }
  .status[data-state="unreachable"], .status[data-state="error"] { color: var(--accent-red); }
  .latency { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .list-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .toolbar-leading, .toolbar-trailing { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .expand-list-btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element); color: var(--text-secondary);
    font-size: 11.5px; cursor: pointer;
  }
  .expand-list-btn:hover { color: var(--text-primary); border-color: var(--border-default); }
  .count-badge { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .chips-row { display: flex; align-items: center; gap: 6px; overflow-x: auto; }
  .item-chip {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element); color: var(--text-secondary);
    font-size: 11.5px; cursor: pointer; white-space: nowrap;
  }
  .item-chip.selected { border-color: var(--border-focus); color: var(--text-primary); }
  .chip-dot { width: 6px; height: 6px; border-radius: 50%; }
  .chip-badge { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .header-filter {
    display: inline-flex; align-items: center; gap: 7px; height: 28px; padding: 0 9px;
    border-radius: 8px; background: var(--bg-element); border: 1px solid var(--border-subtle);
    color: var(--text-tertiary); width: 165px;
  }
  .header-filter input {
    flex: 1; min-width: 0; border: 0; outline: none; background: transparent;
    font-size: 11.5px; color: var(--text-primary);
  }
  .clear-query-btn { border: 0; background: transparent; color: var(--text-tertiary); cursor: pointer; font-size: 14px; }

  .blank { padding: 64px 0; text-align: center; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 6px; font-size: 12px; color: var(--text-secondary); }
  .blank-actions { margin-top: 14px; display: flex; justify-content: center; }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
  .skeleton-list span {
    height: 42px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
</style>
