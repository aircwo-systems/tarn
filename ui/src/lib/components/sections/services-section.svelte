<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import { getDashboard, getInfraSettings, getVisibleInfra } from "$lib/state.svelte";
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
  const infraSettings = getInfraSettings();

  const KIND_LABELS: Record<string, string> = {
    docker: "Docker",
    postgresql: "PostgreSQL",
    redis: "Redis",
    mysql: "MySQL",
    mongodb: "MongoDB",
    http: "HTTP service",
  };

  type Service = InfraProbe & { id: string; userAdded: boolean; usedBy: InfraConnection[] };

  // Same id the canvas and backend connection inference use, so "used by" lines up with the edges.
  const serviceId = (p: InfraProbe) => `${p.kind}-${p.host}-${p.port}`;

  const services = $derived.by<Service[]>(() => {
    const connections = dashboard.data?.connections ?? [];
    const userTargets = infraSettings.frontendTargets;
    return getVisibleInfra(dashboard.data?.infrastructure ?? [])
      .map((p) => {
        const id = serviceId(p);
        return {
          ...p,
          id,
          userAdded: userTargets.some((t) => t.host === p.host && t.port === p.port && t.name === p.name),
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
    return s.port > 0 ? `${s.host}:${s.port}` : normalizeTopologyInfraKind(s.kind) === "docker" ? "daemon socket" : s.host;
  }

  function kindLabel(kind: string): string {
    return KIND_LABELS[normalizeTopologyInfraKind(kind)] ?? kind;
  }

  function select(id: string) {
    selectedId = id;
    history.replaceState(null, "", `#services?${new URLSearchParams({ id })}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((s) => s.id === selected?.id);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].id);
  }

  onMount(() => {
    selectedId = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("id");
  });

  const details = $derived(
    selected
      ? [
          { label: "Name", value: selected.name },
          { label: "Kind", value: kindLabel(selected.kind) },
          { label: "Address", value: address(selected), mono: true },
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
  />

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
      <button type="button" class="link" onclick={() => onNavigate("settings")}>Open settings</button>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-services-list-width">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="service-list" onkeydown={onKeydown}>
          <label class="search">
            <MagnifyingGlassIcon size={12} />
            <input placeholder="Filter services" bind:value={query} aria-label="Filter services" />
            <span class="count">{visible.length}</span>
          </label>
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
                        <span class="evidence">{c.evidence}{c.source ? ` · ${c.source}` : ""}</span>
                      </button>
                    </li>
                  {/each}
                </ul>
              {:else}
                <p class="note">
                  No links detected yet. Links are inferred from function environment variables
                  that point at this host and port.
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
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .service-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
  .trail { display: inline-flex; align-items: center; gap: 8px; }
  .added {
    height: 18px; padding: 0 6px; border-radius: 6px; font-size: 10px; line-height: 16px;
    border: 1px solid var(--border-subtle); color: var(--text-tertiary);
  }
  .kind-bar { width: 3px; height: 14px; border-radius: 2px; }

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
