<script lang="ts">
  import { onMount } from "svelte";
  import { replaceState } from "$app/navigation";
  import { MagnifyingGlassIcon, PlusIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import { getDashboard, getVisibleInfra } from "$lib/state.svelte";
  import { infraKindCssVar, normalizeTopologyInfraKind } from "$lib/components/topology/topology-canvas-theme";
  import type { InfraConnection, InfraProbe } from "$lib/types";
  import { formatDate, timeAgo } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
    onNavigate = (_tab: string) => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  const dashboard = getDashboard();

  const KIND_LABELS: Record<string, string> = {
    docker: "Docker",
    postgresql: "PostgreSQL",
    redis: "Redis",
    mysql: "MySQL",
    mongodb: "MongoDB",
    http: "HTTP service",
    tcp: "TCP service",
  };

  const EVIDENCE_LABELS: Record<string, string> = {
    env: "env var",
    secret: "secret value",
  };

  type Service = InfraProbe & { id: string; userAdded: boolean; usedBy: InfraConnection[] };

  // Same id the canvas and backend connection inference use, so "used by" lines up with the edges.
  const serviceId = (p: InfraProbe) => `${p.kind}-${p.host}-${p.port}`;

  const services = $derived.by<Service[]>(() => {
    const connections = dashboard.data?.connections ?? [];
    return getVisibleInfra(dashboard.data?.infrastructure ?? [])
      .map((p) => {
        const id = serviceId(p);
        return {
          ...p,
          id,
          userAdded: p.source === "user",
          usedBy: connections.filter((c) => c.targetId === id),
        };
      })
      .sort(
        (a, b) =>
          Number(b.status === "connected") - Number(a.status === "connected") ||
          Number(b.userAdded) - Number(a.userAdded) ||
          a.name.localeCompare(b.name),
      );
  });

  const userAddedCount = $derived(services.filter((s) => s.userAdded).length);
  const connectedCount = $derived(services.filter((s) => s.status === "connected").length);

  // Keyed by id: polling replaces the objects, so holding one would freeze the panel.
  let selectedId = $state<string | null>(null);
  const selected = $derived(services.find((s) => s.id === selectedId) ?? services[0] ?? null);

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-services-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return services;
    return services.filter(
      (s) => s.name.toLowerCase().includes(q) || s.kind.toLowerCase().includes(q) || address(s).includes(q),
    );
  });

  function statusTone(status: string): Exclude<Tone, "neutral"> {
    if (status === "connected") return "green";
    if (status === "refused") return "amber";
    return "red";
  }

  // Docker is probed over its socket, so it has no port worth showing.
  function address(s: InfraProbe): string {
    if (s.url) return s.url.replace(/^https?:\/\//, "");
    return s.port > 0 ? `${s.host}:${s.port}` : normalizeTopologyInfraKind(s.kind) === "docker" ? "daemon socket" : s.host;
  }

  function kindLabel(kind: string): string {
    const k = kind.toLowerCase();
    return KIND_LABELS[k === "https" ? "http" : k] ?? KIND_LABELS[normalizeTopologyInfraKind(kind)] ?? kind;
  }

  // Adding lives in settings; deep link straight to the infra form.
  function openAddService() {
    onNavigate("settings?section=infra&add=service");
  }

  function select(id: string) {
    selectedId = id;
    replaceState(`#services?${new URLSearchParams({ id })}`, {});
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((s) => s.id === selected?.id);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].id);
  }

  onMount(() => {
    selectedId = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("id");
    try {
      const saved = localStorage.getItem("tarn-services-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
  });

  const details = $derived(
    selected
      ? [
          { label: "Name", value: selected.name },
          { label: "Kind", value: kindLabel(selected.kind) },
          { label: "Address", value: selected.url || address(selected), mono: true },
          { label: "Host", value: `${selected.host}:${selected.port}`, mono: true, dim: true },
          { label: "Source", value: selected.userAdded ? "Added in settings" : "Backend probe" },
          { label: "Version", value: selected.version || "--", mono: true, dim: true },
          { label: "Last probed", value: formatDate(selected.probedAt) },
        ]
      : [],
  );
</script>

<div class="services">
  <SectionHeader
    title="Services"
    description="{services.length} service{services.length === 1 ? '' : 's'} · {userAddedCount} added by you · {connectedCount} reachable"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter services..."
          bind:value={query}
          aria-label="Filter services"
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
      <button type="button" class="add-service" onclick={openAddService}>
        <PlusIcon size={12} weight="bold" />
        Add service
      </button>
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(5) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if services.length === 0}
    <div class="blank">
      <h2>No services yet</h2>
      <p>Enable probes or add your own services (APIs, local apps) in settings and they show up here.</p>
      <button type="button" class="link" onclick={openAddService}>Add a service</button>
    </div>
  {:else}
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="services-toolbar" role="toolbar" aria-label="Services overview">
          <!-- Leading: expand toggle and optional stats (hidden when main sidebar is expanded) -->
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand service list"
              aria-label="Expand service list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Service list</span>
              <span class="count-badge">{services.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>

              <div class="toolbar-stats">
                <span class="toolbar-stat connected" title="{connectedCount} of {services.length} services reachable">
                  <span class="status-dot green"></span>
                  <span>{connectedCount} reachable</span>
                </span>
                {#if services.length - connectedCount > 0}
                  <span class="toolbar-stat offline" title="{services.length - connectedCount} services offline or refused">
                    <span class="status-dot amber"></span>
                    <span>{services.length - connectedCount} offline</span>
                  </span>
                {/if}
                {#if userAddedCount > 0}
                  <span class="toolbar-stat user-added" title="{userAddedCount} services added in settings">
                    <span>{userAddedCount} custom</span>
                  </span>
                {/if}
              </div>
            {/if}
          </div>

          <!-- Trailing: individual rounded-lg service items with white fill -->
          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="service-chips-row" role="tablist" tabindex="0" aria-label="Service switcher" onkeydown={onKeydown}>
              {#each visible as s (s.id)}
                <button
                  type="button"
                  role="tab"
                  class="service-chip"
                  class:selected={s.id === selected?.id}
                  aria-selected={s.id === selected?.id}
                  onclick={() => select(s.id)}
                  title="{s.name} ({kindLabel(s.kind)} · {address(s)})"
                >
                  <span
                    class="chip-dot"
                    style:background={s.status === "connected" ? "var(--accent-green, #10b981)" : s.status === "refused" ? "var(--accent-amber, #f59e0b)" : "var(--accent-red, #fb7185)"}
                  ></span>
                  <span class="chip-name">{s.name}</span>
                  {#if s.status === "connected" && s.latencyMs > 0}
                    <span class="chip-latency">{Math.round(s.latencyMs)}ms</span>
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
          storageKey="tarn-services-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="service-list" onkeydown={onKeydown}>
            <div class="search-row">
              <label class="search">
                <MagnifyingGlassIcon size={12} />
                <input placeholder="Filter services" bind:value={query} aria-label="Filter services" />
                <span class="count">{visible.length}</span>
              </label>
              <button
                type="button"
                class="collapse-list-btn"
                onclick={toggleListCollapse}
                title="Collapse service list"
                aria-label="Collapse service list"
              >
                <SidebarSimpleIcon size={13} />
              </button>
            </div>
            <div class="rows">
              {#each visible as s (s.id)}
                <RcListRow
                  title={s.name}
                  sub="{kindLabel(s.kind)} · {address(s)}"
                  selected={s.id === selected?.id}
                  onclick={() => select(s.id)}
                >
                  {#snippet trailing()}
                    <span class="trail">
                      {#if s.userAdded}<span class="added">added</span>{/if}
                      <span class="kind-bar" style:background={infraKindCssVar(s.kind)}></span>
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
          <div class="detail">
            <header class="hero">
              <div class="identity">
                <h1 title={selected.name}>{selected.name}</h1>
                <div class="subline">
                  <span>{kindLabel(selected.kind)}</span><i></i>
                  <span class="mono">{address(selected)}</span>
                </div>
              </div>
              <RcTonePill tone={statusTone(selected.status)}>{selected.status}</RcTonePill>
            </header>

            <div class="stats">
              <RcStat
                label="Status"
                value={selected.status}
                tone={statusTone(selected.status)}
                sub={selected.error || "last probe"}
              />
              <RcStat
                label="Latency"
                value={selected.status === "connected" ? `${Math.round(selected.latencyMs)}ms` : "--"}
                sub="probe round-trip"
              />
              <RcStat label="Used by" value={selected.usedBy.length} sub="inferred links" />
              <RcStat label="Probed" value={timeAgo(selected.probedAt)} sub={formatDate(selected.probedAt)} />
            </div>

            <RcPanel title="Details" index={0}>
              <RcKv items={details} labelWidth="7rem" />
            </RcPanel>

            <RcPanel
              title="Used by"
              description="Resources linked to this service on the canvas"
              index={1}
            >
              {#if selected.usedBy.length}
                <ul class="links">
                  {#each selected.usedBy as c (c.sourceFunction + c.source)}
                    <li>
                      <button type="button" class="link-row" onclick={() => onNavigate("functions")}>
                        <span class="mono">{c.sourceFunction}</span>
                        <span class="evidence">{EVIDENCE_LABELS[c.evidence] ?? c.evidence}{c.source ? ` · ${c.source.replace(/^secret:/, "")}` : ""}</span>
                      </button>
                    </li>
                  {/each}
                </ul>
              {:else}
                <p class="note">
                  No links detected yet. A function links here when an env var, or a secret it
                  references by name or ARN, contains this host and port. Hosts must match as
                  written (an IP won't match a hostname for the same machine).
                </p>
              {/if}
            </RcPanel>

            {#if selected.error}
              <RcPanel title="Last error" index={2}>
                <pre>{selected.error}</pre>
              </RcPanel>
            {/if}
          </div>
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .services { display: flex; flex-direction: column; min-height: 100%; }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  .layout.list-collapsed {
    display: flex; flex-direction: column; gap: 16px;
  }
  .layout.list-collapsed .detail {
    width: 100%;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  /* Header filter with white fill */
  .header-filter {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    padding: 0 9px;
    border-radius: 8px; /* rounded-lg */
    background: #ffffff; /* white fill */
    border: 1px solid var(--border-subtle);
    color: var(--text-tertiary);
    width: 160px;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
    transition: width 160ms var(--ease-snappy, ease), border-color 120ms ease, box-shadow 120ms ease;
  }
  .header-filter:hover {
    border-color: var(--border-default);
  }
  .header-filter:focus-within {
    width: 200px;
    border-color: var(--border-focus, #3b82f6);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--border-focus, #3b82f6) 15%, transparent);
    color: var(--text-primary);
  }
  .header-filter input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: none;
    background: transparent;
    font-size: 11.5px;
    color: var(--text-primary);
  }
  .header-filter input::placeholder {
    color: var(--text-tertiary);
  }
  .clear-query-btn {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
    padding: 0 2px;
  }
  .clear-query-btn:hover {
    color: var(--text-primary);
  }

  /* Horizontal bar shown when service list is collapsed - transparent row, no full-width grey background */
  .services-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 0 0 4px;
    background: transparent;
    border: none;
    box-shadow: none;
    animation: barIn 200ms var(--ease-snappy, cubic-bezier(0.16, 1, 0.3, 1)) both;
    min-height: 32px;
    max-width: 100%;
  }
  @keyframes barIn {
    from {
      opacity: 0;
      transform: translateY(-3px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .toolbar-leading {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .expand-list-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 10px;
    border-radius: 8px; /* rounded-lg */
    background: #ffffff; /* white fill */
    border: 1px solid var(--border-subtle);
    color: var(--text-primary);
    font-size: 11.5px;
    font-weight: 500;
    cursor: pointer;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
    transition: background 120ms ease, border-color 120ms ease, transform 120ms ease, box-shadow 120ms ease;
  }
  .expand-list-btn:hover {
    border-color: var(--border-default);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
  }
  .expand-list-btn:active {
    transform: scale(0.97);
  }
  .count-badge {
    font-size: 10px;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-variant-numeric: tabular-nums;
    padding: 1px 5px;
    border-radius: 5px;
    background: var(--bg-sidebar-subtle, #f3f4f7);
    color: var(--text-secondary);
    border: 1px solid var(--border-subtle);
  }
  .toolbar-divider {
    width: 1px;
    height: 16px;
    background: var(--border-subtle);
    flex-shrink: 0;
  }
  .toolbar-stats {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 11.5px;
    white-space: nowrap;
  }
  .toolbar-stat {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--text-secondary);
  }
  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 9999px;
    flex-shrink: 0;
  }
  .status-dot.green {
    background: var(--accent-green, #10b981);
    box-shadow: 0 0 5px color-mix(in srgb, var(--accent-green, #10b981) 60%, transparent);
  }
  .status-dot.amber {
    background: var(--accent-amber, #f59e0b);
  }
  .toolbar-stat.user-added {
    font-size: 10.5px;
    padding: 2px 7px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    color: var(--text-tertiary);
  }

  .toolbar-trailing {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex: 1;
    justify-content: flex-end;
  }

  .service-chips-row {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    scrollbar-width: none;
    -ms-overflow-style: none;
    padding: 2px 0;
    min-width: 0;
    flex-shrink: 1;
  }
  .service-chips-row::-webkit-scrollbar {
    display: none;
  }

  /* Individual services in rounded-lg buttons with white fill instead of grey */
  .service-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 10px;
    border-radius: 8px; /* rounded-lg */
    border: 1px solid var(--border-subtle);
    background: #ffffff; /* white fill instead of grey */
    color: var(--text-secondary);
    font-size: 11.5px;
    font-family: var(--font-mono, ui-monospace, monospace);
    white-space: nowrap;
    cursor: pointer;
    flex-shrink: 0;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
    transition: background 120ms ease, color 120ms ease, border-color 120ms ease, transform 120ms ease, box-shadow 120ms ease;
  }
  .service-chip:hover {
    border-color: var(--border-default);
    color: var(--text-primary);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
  }
  .service-chip:active {
    transform: scale(0.97);
  }
  .service-chip.selected {
    background: #ffffff;
    color: var(--text-primary);
    font-weight: 600;
    border-color: var(--border-default);
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  }
  .chip-dot {
    width: 6px;
    height: 6px;
    border-radius: 9999px;
    flex-shrink: 0;
  }
  .chip-name {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .chip-latency {
    font-size: 10px;
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }
  .chips-none {
    font-size: 11px;
    color: var(--text-tertiary);
    padding: 0 8px;
    white-space: nowrap;
  }

  /* Dark mode support */
  :global(.dark) .header-filter,
  :global(.dark) .add-service,
  :global(.dark) .expand-list-btn,
  :global(.dark) .service-chip,
  :global(.dark) .search {
    background: var(--bg-element);
    box-shadow: none;
  }
  :global(.dark) .service-chip.selected {
    background: var(--bg-element-active, #1f1f23);
    border-color: var(--border-default);
  }
  :global(.dark) .count-badge {
    background: var(--bg-element);
  }

  @media (max-width: 1050px) {
    .services-toolbar {
      flex-wrap: wrap;
      gap: 10px;
    }
    .toolbar-leading, .toolbar-trailing {
      width: 100%;
      justify-content: flex-start;
    }
  }

  /* Aside service list */
  .service-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .search-row .search {
    flex: 1;
    min-width: 0;
  }
  .collapse-list-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: #ffffff;
    color: var(--text-tertiary);
    cursor: pointer;
    flex-shrink: 0;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
    transition: border-color 120ms ease, color 120ms ease, background 120ms ease, transform 120ms ease;
  }
  .collapse-list-btn:hover {
    border-color: var(--border-default);
    color: var(--text-primary);
  }
  .collapse-list-btn:active {
    transform: scale(0.96);
  }

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
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
  .trail { display: inline-flex; align-items: center; gap: 8px; }
  .added {
    height: 18px; padding: 0 6px; border-radius: 6px; font-size: 10px; line-height: 16px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary);
  }
  .kind-bar { width: 3px; height: 14px; border-radius: 2px; }

  .add-service {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 10px;
    border-radius: 8px; border: 1px solid var(--border-subtle); font-size: 12px;
    background: #ffffff; /* white fill */
    color: var(--text-primary); transition: border-color 120ms ease, transform 120ms ease, box-shadow 120ms ease;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
  }
  .add-service:hover { border-color: var(--border-default); box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06); }
  .add-service:active { transform: scale(0.96); }

  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .hero { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .links { display: flex; flex-direction: column; gap: 2px; }
  .link-row {
    display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%;
    height: 32px; padding: 0 10px; border-radius: 8px; text-align: left; font-size: 12px;
    color: var(--text-primary); transition: background 120ms ease;
  }
  .link-row:hover { background: var(--bg-element-hover); }
  .evidence { font-size: 11px; color: var(--text-tertiary); }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  pre {
    padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--accent-red);
    white-space: pre-wrap; word-break: break-word;
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .link {
    margin-top: 12px; height: 28px; padding: 0 12px; border-radius: 8px; font-size: 12px;
    border: 1px solid var(--border-subtle); color: var(--text-primary); transition: border-color 120ms ease;
  }
  .link:hover { border-color: var(--border-default); }
  .link:active { transform: scale(0.96); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .stats, .blank, .skeleton-list span { animation: none; }
  }
</style>
