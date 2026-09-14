<script lang="ts">
  import { onMount } from "svelte";
  import { DownloadSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import GatewayList from "$lib/components/gateways/gateway-list.svelte";
  import GatewayDetail from "$lib/components/gateways/gateway-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { buildCombinedCollection, downloadJSON } from "$lib/postman";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const gateways = $derived(
    (dashboard.data?.gateways ?? []).filter((gateway) =>
      matchesTagFilter(gateway.tags, filters.tagFilter),
    ),
  );

  // Keyed by apiId: polling replaces the objects, so holding one would freeze the panel.
  let selectedGatewayId = $state<string | null>(null);
  const selectedGateway = $derived(
    gateways.find((gateway) => gateway.apiId === selectedGatewayId) ?? gateways[0] ?? null,
  );
  const totalRoutes = $derived(gateways.reduce((sum, g) => sum + (g.routes ?? 0), 0));
  const totalIntegrations = $derived(gateways.reduce((sum, g) => sum + (g.integrations ?? 0), 0));

  function select(apiId: string) {
    selectedGatewayId = apiId;
    history.replaceState(null, "", `#gateways?api=${encodeURIComponent(apiId)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const api = qs ? new URLSearchParams(qs).get("api") : null;
    if (api) selectedGatewayId = api;
  });

  function downloadAll() {
    downloadJSON(
      "tarn-all-gateways.postman_collection.json",
      buildCombinedCollection(gateways),
    );
  }
</script>

<div class="gateways">
  <SectionHeader
    title="API Gateways"
    description="{gateways.length} gateways · {totalRoutes} routes · {totalIntegrations} integrations"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Tag <span>{filters.tagFilter}</span></span>
      {/if}
      {#if gateways.length > 0}
        <button type="button" onclick={downloadAll} class="export-btn">
          <DownloadSimpleIcon size={12} />
          Export Postman
        </button>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if gateways.length === 0}
    <div class="blank">
      <h2>No gateways yet</h2>
      <p>Create one with <code>aws apigatewayv2 create-api</code> or deploy through your IaC, and it appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-gateways-list-width">
        <GatewayList gateways={gateways} selectedId={selectedGateway?.apiId ?? null} onselect={select} />
      </RcResizableAside>
      {#if selectedGateway}
        {#key selectedGateway.apiId}
          <GatewayDetail gateway={selectedGateway} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .gateways { display: flex; flex-direction: column; min-height: 100%; }
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

  .export-btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .export-btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .export-btn:active { transform: scale(0.96); }

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
