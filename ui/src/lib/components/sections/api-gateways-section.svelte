<script lang="ts">
  import {
    GlobeHemisphereWestIcon,
    DownloadSimpleIcon,
    StackIcon,
  } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import { TableRow, TableCell } from "$lib/components/ui/table";
  import { PaneGroup, Pane, Handle } from "$lib/components/ui/resizable";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import GatewayDetailsPanel from "$lib/components/topology/gateway-details-panel.svelte";
  import MetricCard from "$lib/components/common/metric-card.svelte";
  import SectionHeader from "./section-header.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
    refresh,
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
  let selectedGatewayId = $state("");
  const selectedGateway = $derived(
    gateways.find((gateway) => gateway.apiId === selectedGatewayId) ?? null,
  );
  const totalRoutes = $derived(gateways.reduce((sum, g) => sum + (g.routes ?? 0), 0));
  const totalIntegrations = $derived(gateways.reduce((sum, g) => sum + (g.integrations ?? 0), 0));

  $effect(() => {
    if (
      selectedGatewayId &&
      !gateways.some((gateway) => gateway.apiId === selectedGatewayId)
    ) {
      selectedGatewayId = "";
    }
  });

  function selectGateway(apiId: string) {
    selectedGatewayId = apiId;
  }

  function closeGatewayPanel() {
    selectedGatewayId = "";
  }

  function onGatewayRowKeydown(event: KeyboardEvent, apiId: string) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      selectGateway(apiId);
    }
  }

  function downloadAll() {
    downloadJSON(
      "tarn-all-gateways.postman_collection.json",
      buildCombinedCollection(gateways),
    );
  }
</script>

<div
  class="flex min-h-full flex-col gap-4"
>
  <SectionHeader
    title="API Gateways"
    description="Gateway inventory, stages, routes and integration details."
    icon={GlobeHemisphereWestIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if gateways.length > 0}
        <button
          type="button"
          onclick={downloadAll}
          class="flex items-center gap-1.5 rounded-md border border-border/70 bg-card/60 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-border hover:bg-muted/50"
        >
          <DownloadSimpleIcon size={13} />
          Export Postman
        </button>
      {/if}
    {/snippet}
  </SectionHeader>

  <!-- Metrics Row -->
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
    <MetricCard
      label="Total Gateways"
      value={gateways.length}
      sub="Active HTTP & REST endpoints"
    />
    <MetricCard
      label="Provisioned Routes"
      value={totalRoutes}
      sub="Total route handlers"
    />
    <MetricCard
      label="Integrations"
      value={totalIntegrations}
      valueColor="var(--accent-green)"
      sub="Backend targets wired"
    />
  </div>

  <PaneGroup direction="horizontal" class="min-h-0 flex-1 rounded-md border border-border/70" style="height: calc(100vh - 15rem);">
    <Pane defaultSize={58} minSize={35} class="flex min-h-0 flex-col overflow-hidden bg-card/30">
  <ResourceTable
    title="API Gateways"
    count={gateways.length}
    loading={dashboard.loading && !dashboard.data}
    empty={gateways.length === 0}
    emptyMessage="No API Gateways created yet."
    emptyIcon={GlobeHemisphereWestIcon}
    columns={["Name", "Type", "Stage", "Routes", "Integrations"]}
    onRefresh={refresh}
  >
    {#each gateways as gateway}
      <TableRow
        class={`cursor-pointer focus-within:bg-muted/60 ${gateway.apiId === selectedGatewayId ? "bg-muted/60" : ""}`}
        role="button"
        tabindex={0}
        aria-label={`Open details for API Gateway ${gateway.name}`}
        onclick={() => selectGateway(gateway.apiId)}
        onkeydown={(event: KeyboardEvent) =>
          onGatewayRowKeydown(event, gateway.apiId)}
      >
        <TableCell><ArnCell name={gateway.name} arn={gateway.arn} /></TableCell>
        <TableCell class="font-mono text-xs text-muted-foreground">
          {gateway.protocolType} <span class="text-muted-foreground/50">{gateway.version}</span>
        </TableCell>
        <TableCell class="font-mono text-xs text-muted-foreground">
          {gateway.defaultStage || "—"}
        </TableCell>
        <TableCell class="font-mono text-muted-foreground"
          >{gateway.routes}</TableCell
        >
        <TableCell class="font-mono text-muted-foreground"
          >{gateway.integrations}</TableCell
        >
      </TableRow>
    {/each}
  </ResourceTable>
    </Pane>
    <Handle />
    <Pane defaultSize={42} minSize={25} class="flex min-h-0 flex-col overflow-hidden bg-card/30">
  {#if selectedGateway}
    <GatewayDetailsPanel
      gateway={selectedGateway}
      onClose={closeGatewayPanel}
    />
  {:else}
    <section class="flex h-full min-h-0 flex-col">
      <div class="border-b border-border/70 bg-background/35 px-3 py-2">
        <h3 class="text-sm font-semibold text-foreground">Gateway Details</h3>
      </div>
      <div class="flex flex-1 items-center justify-center px-6 py-5 text-center">
        <p class="max-w-sm text-sm text-muted-foreground/70">
        Select a gateway to inspect routes, integrations, and request templates.
        </p>
      </div>
    </section>
  {/if}
    </Pane>
  </PaneGroup>
</div>
