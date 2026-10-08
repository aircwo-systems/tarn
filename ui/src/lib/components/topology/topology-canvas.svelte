<script lang="ts">
  import {
    ArrowsInSimpleIcon,
    ArrowsOutSimpleIcon,
    CommandIcon,
  } from "phosphor-svelte";

  const isMac = /mac/i.test(navigator.platform);
  import GatewayDetailsPanel from "$lib/components/topology/gateway-details-panel.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    getVisibleInfra,
  } from "$lib/state.svelte";
  import type { InfraProbe } from "$lib/types";
  import type {
    InfraNodePosition,
    NodeOverride,
  } from "./topology-connection-model";
  import type { NodeSide, NodeSize, TopologyArrangement } from "./topology-connection-model";
  import { resolveTopologyNodeSize, resolveTopologyNodeView } from "./registry";
  import { normalizeTopologyExternalKind } from "./topology-canvas-theme";
  import TopologyComponentsView from "./TopologyComponentsView.svelte";
  import TopologyConnectionView from "./TopologyConnectionView.svelte";
  import type { NodeKind } from "./types";
  import { resolveDirectPrototypeFilter, matchesResourceFilter, matchesInfrastructureFilter } from "$lib/filter-utils";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const directTopologyFilter = $derived(
    resolveDirectPrototypeFilter(filters.tagFilter),
  );
  const gateways = $derived(
    (dashboard.data?.gateways ?? []).filter((gw) =>
      matchesResourceFilter("gateway", filters.tagFilter, gw.tags),
    ),
  );
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesResourceFilter("function", filters.tagFilter, fn.tags),
    ),
  );
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((q) =>
      matchesResourceFilter("queue", filters.tagFilter, q.tags),
    ),
  );
  const dynamodbTables = $derived(
    (dashboard.data?.dynamodbTables ?? []).filter(() =>
      matchesResourceFilter("dynamodb", filters.tagFilter),
    ),
  );
  const topics = $derived(
    (dashboard.data?.topics ?? []).filter((t) =>
      matchesResourceFilter("topic", filters.tagFilter, t.tags),
    ),
  );
  const secrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesResourceFilter("secret", filters.tagFilter, s.tags),
    ),
  );
  const buckets = $derived(
    (dashboard.data?.buckets ?? []).filter((bucket) =>
      matchesResourceFilter("bucket", filters.tagFilter),
    ),
  );
  const eventSourceMappings = $derived(
    directTopologyFilter ? [] : (dashboard.data?.eventSourceMappings ?? []),
  );
  const eventBridgeRules = $derived(
    (dashboard.data?.eventBridgeRules ?? []).filter(() => matchesResourceFilter("eventbridge", filters.tagFilter)),
  );
  const infra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []).filter((probe) =>
      matchesInfrastructureFilter(normalizeTopologyExternalKind(probe.kind), filters.tagFilter),
    ),
  );
  const infraConnections = $derived(
    directTopologyFilter ? [] : (dashboard.data?.connections ?? []),
  );
  const recentTraces = $derived(dashboard.data?.recentTraces ?? []);

  let {
    canvasExpanded = false,
    bare = false,
    onNavigate = (_tab: string) => {},
    onExpandedChange = (_expanded: boolean) => {},
  }: {
    canvasExpanded?: boolean;
    /** no toolbar or frame: the host gives it its edges and controls */
    bare?: boolean;
    onNavigate?: (tab: string) => void;
    onExpandedChange?: (expanded: boolean) => void;
  } = $props();

  let viewMode = $state<"components" | "connections">("connections");
  let selectedGatewayId = $state("");
  let viewportResetToken = $state(0);
  let infraOrderIds = $state<string[]>([]);
  let infraOrderHydrated = $state(false);
  let allNodePositions = $state<Record<string, InfraNodePosition>>({});
  let allPositionsHydrated = $state(false);
  let allNodeOverrides = $state<Record<string, NodeOverride>>({});
  let allOverridesHydrated = $state(false);

  const INFRA_ORDER_STORAGE_KEY = "tarn-ui-topology-infra-order-v1";
  // v4: only cards outside any section (the secrets cache) are placed
  // freely; the rest live in the arrangement, relative to the layout.
  const ALL_POSITIONS_STORAGE_KEY = "tarn-ui-topology-all-positions-v4";
  const ARRANGEMENT_STORAGE_KEY = "tarn-ui-topology-arrangement-v1";
  let arrangement = $state<TopologyArrangement>(readArrangement());

  function readArrangement(): TopologyArrangement {
    try {
      const parsed = JSON.parse(localStorage.getItem(ARRANGEMENT_STORAGE_KEY) ?? "{}");
      return parsed && typeof parsed === "object" ? parsed : {};
    } catch {
      return {};
    }
  }

  function saveArrangement(next: TopologyArrangement) {
    arrangement = next;
    localStorage.setItem(ARRANGEMENT_STORAGE_KEY, JSON.stringify(next));
  }
  const ALL_OVERRIDES_STORAGE_KEY = "tarn-ui-topology-node-overrides-v1";
  const TOPOLOGY_NODE_KINDS: NodeKind[] = [
    "gateway",
    "eventbridge",
    "topic",
    "queue",
    "dynamodb",
    "bucket",
    "function",
    "secret",
    "extension",
    "infra",
  ];

  function infraNodeId(probe: InfraProbe): string {
    return `${probe.kind}-${probe.host}-${probe.port}`;
  }

  function nodePositionStorageKey(kind: NodeKind, id: string): string {
    return `${kind}:${id}`;
  }

  function parseOverrideKind(overrideKey: string): NodeKind | null {
    const separatorIndex = overrideKey.indexOf(":");
    const maybeKind =
      separatorIndex === -1
        ? overrideKey
        : overrideKey.slice(0, separatorIndex);
    return TOPOLOGY_NODE_KINDS.includes(maybeKind as NodeKind)
      ? (maybeKind as NodeKind)
      : null;
  }

  const resourceCount = $derived(
    gateways.length +
      functions.length +
      queues.length +
      dynamodbTables.length +
      topics.length +
      buckets.length +
      secrets.length +
      infra.length,
  );

  const connectionCount = $derived(
    infraConnections.length + eventSourceMappings.length,
  );
  const panEnabled = $derived(canvasExpanded && viewMode === "connections");

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
    const visibleIds = infra.map((probe) => infraNodeId(probe));

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
    if (typeof window !== "undefined" && !allPositionsHydrated) {
      allPositionsHydrated = true;
      try {
        const raw = localStorage.getItem(ALL_POSITIONS_STORAGE_KEY);
        if (raw) {
          const parsed = JSON.parse(raw);
          if (parsed && typeof parsed === "object") {
            const next: Record<string, InfraNodePosition> = {};
            for (const [id, position] of Object.entries(parsed)) {
              const candidate = position as { x?: unknown; y?: unknown };
              if (
                position &&
                typeof position === "object" &&
                typeof candidate.x === "number" &&
                typeof candidate.y === "number"
              ) {
                next[id] = { x: candidate.x, y: candidate.y };
              }
            }
            allNodePositions = next;
          }
        }
      } catch {
        allNodePositions = {};
      }
    }
  });

  $effect(() => {
    if (typeof window === "undefined" || !allPositionsHydrated) return;
    localStorage.setItem(
      ALL_POSITIONS_STORAGE_KEY,
      JSON.stringify(allNodePositions),
    );
  });

  $effect(() => {
    if (typeof window !== "undefined" && !allOverridesHydrated) {
      allOverridesHydrated = true;
      try {
        const raw = localStorage.getItem(ALL_OVERRIDES_STORAGE_KEY);
        if (raw) {
          const parsed = JSON.parse(raw);
          if (parsed && typeof parsed === "object") {
            const validSides = new Set(["top", "bottom", "left", "right"]);
            const validSizes = new Set(["small", "medium", "large"]);
            const next: Record<string, NodeOverride> = {};
            for (const [id, ov] of Object.entries(parsed)) {
              if (!ov || typeof ov !== "object") continue;
              const candidate = ov as Record<string, unknown>;
              const entry: NodeOverride = {};
              const kind = parseOverrideKind(id);
              if (validSides.has(candidate.inputSide as string))
                entry.inputSide = candidate.inputSide as NodeSide;
              if (validSides.has(candidate.outputSide as string))
                entry.outputSide = candidate.outputSide as NodeSide;
              if (candidate.expanded === true) entry.expanded = true;
              if (kind && validSizes.has(candidate.size as string)) {
                entry.size = resolveTopologyNodeSize(
                  kind,
                  candidate.size as NodeSize,
                );
              } else if (validSizes.has(candidate.size as string)) {
                entry.size = candidate.size as NodeSize;
              }
              if (kind && typeof candidate.view === "string") {
                entry.view = resolveTopologyNodeView(
                  kind,
                  candidate.view,
                  entry.size ?? "small",
                );
              }
              if (
                entry.inputSide ||
                entry.outputSide ||
                entry.size ||
                entry.view
              )
                next[id] = entry;
            }
            allNodeOverrides = next;
          }
        }
      } catch {
        allNodeOverrides = {};
      }
    }
  });

  $effect(() => {
    if (typeof window === "undefined" || !allOverridesHydrated) return;
    localStorage.setItem(
      ALL_OVERRIDES_STORAGE_KEY,
      JSON.stringify(allNodeOverrides),
    );
  });

  function openGateway(apiId: string) {
    selectedGatewayId = apiId;
  }

  function setNodePosition(
    id: string,
    kind: NodeKind,
    position: InfraNodePosition,
  ) {
    allNodePositions = {
      ...allNodePositions,
      [nodePositionStorageKey(kind, id)]: position,
    };
  }

  function setNodeOverride(id: string, override: NodeOverride) {
    const kind = parseOverrideKind(id);
    const nextOverride = { ...allNodeOverrides[id], ...override };
    if (kind) {
      nextOverride.size = resolveTopologyNodeSize(kind, nextOverride.size);
      nextOverride.view = resolveTopologyNodeView(
        kind,
        nextOverride.view,
        nextOverride.size ?? "small",
      );
    }

    allNodeOverrides = {
      ...allNodeOverrides,
      [id]: nextOverride,
    };
  }

  function resetCanvasViewport() {
    viewportResetToken += 1;
  }

  /** Back to the fitted view, for hosts that bring their own controls. */
  export function recentre() {
    resetCanvasViewport();
  }

  function handleShortcutKeydown(event: KeyboardEvent) {
    if (!canvasExpanded) return;
    if (!(event.metaKey || event.ctrlKey)) return;
    if (isEditableTarget(event.target)) return;

    const key = event.key.toLowerCase();
    // Done exploring (and not the browser's bookmark).
    if (key === "d") {
      event.preventDefault();
      onExpandedChange(false);
      return;
    }
    if (viewMode !== "connections") return;
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

</script>

<svelte:document onkeydown={handleShortcutKeydown} />

<div class="h-full w-full min-w-0 flex flex-col">
  <!-- Toolbar — above the bordered canvas area, outside the card -->
  {#if !bare}
  <div class="flex shrink-0 items-center gap-2 pb-2">
    <span class="font-mono text-[10px] text-muted-foreground/50">
      {viewMode === "components"
        ? `Explore and arrange ${resourceCount} resources of your infra below`
        : `Explore and arrange ${connectionCount} links of your infra below`}
    </span>
    {#if canvasExpanded && viewMode === "connections"}
      <span class="font-mono text-[10px] text-muted-foreground/40">
        Shift+Click to multi-select
      </span>
    {/if}

    <div class="ml-auto flex items-center gap-1.5">
      {#if canvasExpanded && viewMode === "connections"}
        <button
          type="button"
          class="flex items-center gap-1 rounded px-2 py-1 font-mono text-[10px] text-muted-foreground/60 hover:bg-muted/60 hover:text-foreground transition-colors"
          aria-label="Re-centre canvas"
          title="Re-centre canvas (Cmd/Ctrl+C)"
          onclick={resetCanvasViewport}
        >
          <CommandIcon size={11} />
          <span
            >Re-centre <span class="text-muted-foreground/40">(cmd + c)</span
            ></span
          >
        </button>
      {/if}

      <button
        type="button"
        class="relative flex items-center justify-center rounded text-muted-foreground/60 hover:bg-muted/60 hover:text-foreground transition-colors {canvasExpanded
          ? 'min-w-6 h-6 px-1.5 gap-2'
          : 'group h-6 w-6'}"
        aria-label={canvasExpanded ? "Collapse canvas" : "Expand canvas"}
        title={canvasExpanded ? "Done (⌘D)" : "Expand canvas"}
        onclick={() => onExpandedChange(!canvasExpanded)}
      >
        {#if !canvasExpanded}
          <span
            class="absolute inset-0 rounded animate-[expand-ping_2.5s_ease-in-out_infinite] bg-primary/10"
          ></span>
        {/if}
        {#if canvasExpanded}
          <ArrowsInSimpleIcon size={13} />
          <span class="font-mono text-[10px] leading-none rounded px-[5px] py-px border border-[color-mix(in_srgb,currentColor_30%,transparent)] opacity-70 aria-hidden" aria-hidden="true">{isMac ? "⌘D" : "Ctrl D"}</span>
        {:else}
          <ArrowsOutSimpleIcon size={13} class="relative" />
        {/if}
      </button>
    </div>
  </div>

  {/if}

  <!-- Canvas area -->
  <div class="flex-1 min-h-0 overflow-hidden {bare ? '' : 'rounded-md'}">
    <div class="flex flex-col lg:flex-row h-full min-h-0">
      <div
        class={`relative min-w-0 flex-1 ${
          viewMode === "connections"
            ? "overflow-hidden"
            : "overflow-auto overscroll-contain"
        }`}
      >
        {#if viewMode === "components"}
          <TopologyComponentsView
            {gateways}
            {functions}
            {queues}
            {dynamodbTables}
            {topics}
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
            {dynamodbTables}
            {topics}
            {secrets}
            {buckets}
            {infra}
            {allNodePositions}
            {allNodeOverrides}
            {eventSourceMappings}
            {infraConnections}
            {eventBridgeRules}
            {infraOrderIds}
            {recentTraces}
            {canvasExpanded}
            {panEnabled}
            {bare}
            {viewportResetToken}
            onGatewayClick={openGateway}
            {arrangement}
            onNodePositionChange={setNodePosition}
            onNodeOverrideChange={setNodeOverride}
            onSectionOrderChange={(id, keys) =>
              saveArrangement({ ...arrangement, order: { ...arrangement.order, [id]: keys } })}
            onSectionMove={(id, offset) =>
              saveArrangement({ ...arrangement, offsets: { ...arrangement.offsets, [id]: offset } })}
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
</div>

<style>
  @keyframes expand-ping {
    0%,
    100% {
      opacity: 0;
      transform: scale(1);
    }
    50% {
      opacity: 1;
      transform: scale(1.35);
    }
  }
</style>
