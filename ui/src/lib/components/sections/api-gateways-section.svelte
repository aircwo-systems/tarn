<script lang="ts">
  import {
    GlobeHemisphereWestIcon,
    DownloadSimpleIcon,
    StackIcon,
  } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import { TableRow, TableCell } from "$lib/components/ui/table";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import GatewayDetailsPanel from "$lib/components/topology/gateway-details-panel.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
    refresh,
  } from "$lib/state.svelte";
  import { buildCombinedCollection, downloadJSON } from "$lib/postman";

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
  class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_38rem] 2xl:grid-cols-[minmax(0,1fr)_44rem]"
>
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
        tabindex="0"
        aria-label={`Open details for API Gateway ${gateway.name}`}
        onclick={() => selectGateway(gateway.apiId)}
        onkeydown={(event: KeyboardEvent) =>
          onGatewayRowKeydown(event, gateway.apiId)}
      >
        <TableCell><ArnCell name={gateway.name} arn={gateway.arn} /></TableCell>
        <TableCell>
          <div class="flex items-center gap-1.5">
            <Badge variant="secondary">{gateway.protocolType}</Badge>
            {#if gateway.version === "v1"}
              <Badge variant="outline" class="text-[10px] px-1 py-0 font-mono"
                >v1</Badge
              >
            {:else}
              <Badge variant="outline" class="text-[10px] px-1 py-0 font-mono"
                >v2</Badge
              >
            {/if}
          </div>
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

  {#if selectedGateway}
    <GatewayDetailsPanel
      gateway={selectedGateway}
      onClose={closeGatewayPanel}
    />
  {:else}
    <section class="rounded-lg border border-border bg-card">
      <div class="border-b border-border px-3 py-2">
        <h3 class="text-sm font-semibold text-foreground">Gateway Details</h3>
      </div>
      <p class="px-3 py-5 text-sm text-muted-foreground/70">
        Select a gateway to inspect routes, integrations, and request templates.
      </p>
    </section>
  {/if}
</div>

<!-- Combined export — slides in from right edge, rounded left only -->
{#if gateways.length > 0}
  <div
    transition:fly={{ x: 160, duration: 300, opacity: 1 }}
    class="fixed bottom-8 right-0 z-50 flex items-center gap-4 rounded-l-xl border border-r-0 border-border bg-card pl-4 pr-5 py-3"
  >
    <StackIcon size={13} class="text-muted-foreground/70 shrink-0" />
    <div class="leading-tight">
      <p class="text-xs font-medium text-foreground">Combined Collection</p>
      <p class="text-[11px] text-muted-foreground/70">
        {gateways.length}
        {gateways.length === 1 ? "gateway" : "gateways"} · sub-folders per gateway
      </p>
    </div>
    <button
      type="button"
      onclick={downloadAll}
      class="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs text-primary hover:bg-primary/20 transition-colors"
    >
      <DownloadSimpleIcon size={12} />
      Download all
    </button>
  </div>
{/if}
