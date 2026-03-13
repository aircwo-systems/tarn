<script lang="ts">
  import * as Command from "$lib/components/ui/command/index.js";
  import {
    ArrowsInSimpleIcon,
    ArrowsOutSimpleIcon,
    CaretDownIcon,
    CaretUpIcon,
    CrosshairSimpleIcon,
    HandPalmIcon,
  } from "phosphor-svelte";
  import GatewayDetailsPanel from "$lib/components/topology/gateway-details-panel.svelte";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import Button from "$lib/components/ui/button/button.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    getVisibleInfra,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import type { InfraProbe } from "$lib/types";
  import type { InfraNodePosition } from "./topology-connection-model";
  import TopologyComponentsView from "./TopologyComponentsView.svelte";
  import TopologyConnectionView from "./TopologyConnectionView.svelte";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();

  const gateways = $derived(
    (dashboard.data?.gateways ?? []).filter((gw) =>
      matchesTagFilter(gw.tags, filters.tagFilter),
    ),
  );
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesTagFilter(fn.tags, filters.tagFilter),
    ),
  );
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((q) =>
      matchesTagFilter(q.tags, filters.tagFilter),
    ),
  );
  const secrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesTagFilter(s.tags, filters.tagFilter),
    ),
  );
  const buckets = $derived(dashboard.data?.buckets ?? []);
  const eventSourceMappings = $derived(
    dashboard.data?.eventSourceMappings ?? [],
  );
  const infra = $derived(getVisibleInfra(dashboard.data?.infrastructure ?? []));
  const infraConnections = $derived(dashboard.data?.connections ?? []);
  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);

  let {
    onNavigate = (_tab: string) => {},
    onExpandedChange = (_expanded: boolean) => {},
  }: {
    onNavigate?: (tab: string) => void;
    onExpandedChange?: (expanded: boolean) => void;
  } = $props();

  let viewMode = $state<"components" | "connections">("connections");
  let selectedGatewayId = $state("");
  let canvasExpanded = $state(false);
  let panEnabled = $state(false);
  let viewportResetToken = $state(0);
  let exploreHelpOpen = $state(false);
  let infraOrderIds = $state<string[]>([]);
  let infraOrderHydrated = $state(false);
  let infraNodePositions = $state<Record<string, InfraNodePosition>>({});
  let infraPositionsHydrated = $state(false);

  const INFRA_ORDER_STORAGE_KEY = "openstack-ui-topology-infra-order-v1";
  const INFRA_POSITIONS_STORAGE_KEY = "openstack-ui-topology-infra-position-v1";

  function infraNodeId(probe: InfraProbe): string {
    return `${probe.kind}-${probe.host}-${probe.port}`;
  }

  // Infra nodes ordered for the toolbar — mirrors what TopologyConnectionView computes
  const infraNodesForToolbar = $derived(
    (() => {
      const visible = infra
        .slice(0, 4)
        .map((probe) => ({ id: infraNodeId(probe), probe }));
      if (visible.length === 0) return [] as { id: string; label: string }[];
      const byId = new Map(visible.map((e) => [e.id, e.probe]));
      const orderedIds = [
        ...infraOrderIds.filter((id) => byId.has(id)),
        ...visible.map((e) => e.id).filter((id) => !infraOrderIds.includes(id)),
      ];
      return orderedIds.map((id) => ({
        id,
        label: byId.get(id)!.name.slice(0, 13),
      }));
    })(),
  );

  const resourceCount = $derived(
    gateways.length +
      functions.length +
      queues.length +
      buckets.length +
      secrets.length +
      infra.length,
  );

  const connectionCount = $derived(
    infraConnections.length + eventSourceMappings.length,
  );

  const selectedGateway = $derived(
    gateways.find((gw) => gw.apiId === selectedGatewayId) ?? null,
  );

  $effect(() => {
    if (
      selectedGatewayId &&
      !gateways.some((gw) => gw.apiId === selectedGatewayId)
    ) {
      selectedGatewayId = "";
    }
  });

  $effect(() => {
    const visibleIds = infra.slice(0, 4).map((probe) => infraNodeId(probe));

    if (typeof window !== "undefined" && !infraOrderHydrated) {
      infraOrderHydrated = true;
      try {
        const raw = localStorage.getItem(INFRA_ORDER_STORAGE_KEY);
        if (raw) {
          const parsed = JSON.parse(raw);
          if (Array.isArray(parsed)) {
            infraOrderIds = parsed.filter(
              (v): v is string => typeof v === "string",
            );
          }
        }
      } catch {
        infraOrderIds = [];
      }
    }

    const normalized = [
      ...infraOrderIds.filter((id) => visibleIds.includes(id)),
      ...visibleIds.filter((id) => !infraOrderIds.includes(id)),
    ];

    if (
      normalized.length !== infraOrderIds.length ||
      normalized.some((id, i) => id !== infraOrderIds[i])
    ) {
      infraOrderIds = normalized;
    }
  });

  $effect(() => {
    if (typeof window === "undefined" || !infraOrderHydrated) return;
    localStorage.setItem(
      INFRA_ORDER_STORAGE_KEY,
      JSON.stringify(infraOrderIds),
    );
  });

  $effect(() => {
    if (typeof window !== "undefined" && !infraPositionsHydrated) {
      infraPositionsHydrated = true;
      try {
        const raw = localStorage.getItem(INFRA_POSITIONS_STORAGE_KEY);
        if (raw) {
          const parsed = JSON.parse(raw);
          if (parsed && typeof parsed === "object") {
            const next: Record<string, InfraNodePosition> = {};
            for (const [id, position] of Object.entries(parsed)) {
              const candidate = position as {
                x?: unknown;
                y?: unknown;
              };
              if (
                position &&
                typeof position === "object" &&
                typeof candidate.x === "number" &&
                typeof candidate.y === "number"
              ) {
                next[id] = {
                  x: candidate.x,
                  y: candidate.y,
                };
              }
            }
            infraNodePositions = next;
          }
        }
      } catch {
        infraNodePositions = {};
      }
    }
  });

  $effect(() => {
    if (typeof window === "undefined" || !infraPositionsHydrated) return;
    localStorage.setItem(
      INFRA_POSITIONS_STORAGE_KEY,
      JSON.stringify(infraNodePositions),
    );
  });

  $effect(() => {
    if (!canvasExpanded || viewMode !== "connections") {
      panEnabled = false;
      exploreHelpOpen = false;
    }
  });

  function moveInfraNode(id: string, direction: -1 | 1) {
    const index = infraOrderIds.indexOf(id);
    if (index === -1) return;
    const nextIndex = index + direction;
    if (nextIndex < 0 || nextIndex >= infraOrderIds.length) return;
    const next = [...infraOrderIds];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    infraOrderIds = next;
  }

  function openGateway(apiId: string) {
    selectedGatewayId = apiId;
  }

  function setInfraNodePosition(id: string, position: InfraNodePosition) {
    infraNodePositions = {
      ...infraNodePositions,
      [id]: position,
    };
  }

  function resetCanvasViewport() {
    viewportResetToken += 1;
  }

  function togglePanMode() {
    if (panEnabled) {
      panEnabled = false;
      exploreHelpOpen = false;
      resetCanvasViewport();
      return;
    }

    panEnabled = true;
    exploreHelpOpen = true;
  }

  function handleShortcutKeydown(event: KeyboardEvent) {
    if (!canvasExpanded || viewMode !== "connections") return;
    if (!(event.metaKey || event.ctrlKey)) return;
    if (isEditableTarget(event.target)) return;

    const key = event.key.toLowerCase();
    if (key === "m") {
      event.preventDefault();
      togglePanMode();
      return;
    }

    if (key === "c") {
      event.preventDefault();
      resetCanvasViewport();
    }
  }

  function isEditableTarget(target: EventTarget | null): boolean {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.tagName === "SELECT")
    );
  }

  function closeGatewayPanel() {
    selectedGatewayId = "";
  }

  $effect(() => {
    onExpandedChange(canvasExpanded);
  });
</script>

<svelte:document onkeydown={handleShortcutKeydown} />

<div
  class="h-full w-full min-w-0 overflow-hidden rounded-t-lg border-x border-t border-border bg-background flex flex-col"
>
  <!-- Toolbar -->
  <div
    class="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2"
  >
    <!-- <h3
      class="shrink-0 text-xs font-mono uppercase tracking-wider text-muted-foreground"
    >
      Topology
    </h3> -->

    <Badge variant="secondary" class="shrink-0 font-mono text-[10px]">
      {viewMode === "components"
        ? `${resourceCount} resources`
        : `${connectionCount} links`}
    </Badge>

    <!-- {#if viewMode === "connections" && infraNodesForToolbar.length > 1}
      <span
        class="shrink-0 text-[10px] font-mono uppercase tracking-wide text-muted-foreground/70"
      >
        Infra Order
      </span>
      {#each infraNodesForToolbar as node, index (node.id)}
        <div
          class="inline-flex shrink-0 items-center gap-1 rounded-md border border-border bg-muted px-1 py-0.5"
        >
          <span
            class="max-w-[8.5rem] truncate text-[10px] text-muted-foreground"
            >{node.label}</span
          >
          <div class="inline-flex gap-0.5">
            <Button
              variant="outline"
              size="icon"
              class="h-5 w-5"
              disabled={index === 0}
              onclick={() => moveInfraNode(node.id, -1)}
              aria-label={`Move ${node.label} up`}
            >
              <CaretUpIcon size={10} />
            </Button>
            <Button
              variant="outline"
              size="icon"
              class="h-5 w-5"
              disabled={index === infraNodesForToolbar.length - 1}
              onclick={() => moveInfraNode(node.id, 1)}
              aria-label={`Move ${node.label} down`}
            >
              <CaretDownIcon size={10} />
            </Button>
          </div>
        </div>
      {/each}
    {/if} -->

    <div class="ml-auto flex flex-wrap items-center justify-end gap-2">
      {#if canvasExpanded && viewMode === "connections"}
        <div
          class="inline-flex items-center gap-1 rounded-xl border border-primary/20 bg-primary/10 p-1 shadow-sm"
        >
          <Button
            variant={panEnabled ? "default" : "ghost"}
            size="sm"
            class="h-7 gap-1.5 rounded-lg px-2 text-[11px]"
            aria-pressed={panEnabled}
            aria-label={panEnabled
              ? "Disable canvas explore mode"
              : "Enable canvas explore mode"}
            title={panEnabled
              ? "Disable canvas explore mode"
              : "Enable canvas explore mode"}
            onclick={togglePanMode}
          >
            <HandPalmIcon size={12} />
            <span class="hidden sm:inline">Explore</span>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            class="h-7 gap-1.5 rounded-lg px-2 text-[11px]"
            aria-label="Re-centre canvas"
            title="Re-centre canvas"
            onclick={resetCanvasViewport}
          >
            <CrosshairSimpleIcon size={12} />
            <span class="hidden md:inline">Re-centre</span>
          </Button>
        </div>
      {/if}

      {#if canvasExpanded && viewMode === "connections"}
        <Badge variant={panEnabled ? "default" : "outline"} class="shrink-0">
          {panEnabled ? "keys + d-pad enabled" : "drag infra to persist"}
        </Badge>
      {/if}

      <Button
        variant="outline"
        size="icon"
        class="h-7 w-7 shrink-0"
        aria-label={canvasExpanded ? "Collapse canvas" : "Expand canvas"}
        title={canvasExpanded ? "Collapse canvas" : "Expand canvas"}
        onclick={() => (canvasExpanded = !canvasExpanded)}
      >
        {#if canvasExpanded}
          <ArrowsInSimpleIcon size={13} />
        {:else}
          <ArrowsOutSimpleIcon size={13} />
        {/if}
      </Button>
    </div>
  </div>

  <!-- Canvas + detail panel -->
  <div class="flex flex-col lg:flex-row flex-1 min-h-0">
    <div class="relative min-w-0 flex-1 overflow-auto overscroll-contain">
      {#if viewMode === "components"}
        <TopologyComponentsView
          {gateways}
          {functions}
          {queues}
          {secrets}
          {buckets}
          {infra}
          {canvasExpanded}
          // onGatewayClick={openGateway}
          {onNavigate}
        />
      {:else}
        <TopologyConnectionView
          {gateways}
          {functions}
          {queues}
          {secrets}
          {buckets}
          {infra}
          {infraNodePositions}
          {eventSourceMappings}
          {infraConnections}
          {infraOrderIds}
          {recentTraces}
          {canvasExpanded}
          {panEnabled}
          {viewportResetToken}
          onGatewayClick={openGateway}
          onInfraNodePositionChange={setInfraNodePosition}
          {onNavigate}
        />
      {/if}
    </div>

    {#if selectedGateway}
      <div
        class="w-full border-t border-border bg-muted/40 p-3 lg:w-[22rem] lg:border-l lg:border-t-0"
      >
        <GatewayDetailsPanel
          gateway={selectedGateway}
          onClose={closeGatewayPanel}
        />
      </div>
    {/if}
  </div>
</div>

<Command.Dialog
  bind:open={exploreHelpOpen}
  title="Explore topology"
  description="Keyboard controls for navigating the topology canvas"
>
  <Command.List>
    <Command.Group heading="Explore shortcuts">
      <Command.Item disabled>
        <span>Use arrow keys to move around the canvas</span>
        <Command.Shortcut>&larr; &uarr; &darr; &rarr;</Command.Shortcut>
      </Command.Item>
      <Command.Item
        onclick={() => {
          resetCanvasViewport();
          exploreHelpOpen = false;
        }}
      >
        <span>Re-centre the canvas</span>
        <Command.Shortcut>Cmd/Ctrl+C</Command.Shortcut>
      </Command.Item>
      <Command.Item disabled>
        <span>Toggle explore mode</span>
        <Command.Shortcut>Cmd/Ctrl+M</Command.Shortcut>
      </Command.Item>
    </Command.Group>
    <Command.Separator />
    <Command.Group heading="Infra layout">
      <Command.Item disabled>
        <span>Drag infra nodes to pin their position on the canvas</span>
      </Command.Item>
      <Command.Item onclick={() => (exploreHelpOpen = false)}>
        <span>Continue exploring</span>
        <Command.Shortcut>Esc</Command.Shortcut>
      </Command.Item>
    </Command.Group>
  </Command.List>
</Command.Dialog>
