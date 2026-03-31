<script lang="ts">
  import {
    ArrowsInSimpleIcon,
    ArrowsOutSimpleIcon,
    CommandIcon,
  } from "phosphor-svelte";
  import GatewayDetailsPanel from "$lib/components/topology/gateway-details-panel.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    getVisibleInfra,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import type { InfraProbe } from "$lib/types";
  import type {
    InfraNodePosition,
    NodeOverride,
  } from "./topology-connection-model";
  import type { NodeSide, NodeSize } from "./topology-connection-model";
  import { resolveTopologyNodeSize, resolveTopologyNodeView } from "./registry";
  import { normalizeTopologyExternalKind } from "./topology-canvas-theme";
  import TopologyComponentsView from "./TopologyComponentsView.svelte";
  import TopologyConnectionView from "./TopologyConnectionView.svelte";
  import type { NodeKind } from "./types";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const directTopologyFilter = $derived(
    resolveDirectTopologyFilter(filters.tagFilter),
  );
  const tagOnlyTopologyFilterQuery = $derived(
    extractTagOnlyTopologyFilterQuery(filters.tagFilter),
  );

  const gateways = $derived(
    (dashboard.data?.gateways ?? []).filter((gw) =>
      matchesTopologyResourceFilter("gateway", filters.tagFilter, gw.tags),
    ),
  );
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesTopologyResourceFilter("function", filters.tagFilter, fn.tags),
    ),
  );
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((q) =>
      matchesTopologyResourceFilter("queue", filters.tagFilter, q.tags),
    ),
  );
  const dynamodbTables = $derived(
    (dashboard.data?.dynamodbTables ?? []).filter(() =>
      matchesTopologyResourceFilter("dynamodb", filters.tagFilter),
    ),
  );
  const topics = $derived(
    (dashboard.data?.topics ?? []).filter((t) =>
      matchesTopologyResourceFilter("topic", filters.tagFilter, t.tags),
    ),
  );
  const secrets = $derived(
    (dashboard.data?.secrets ?? []).filter((s) =>
      matchesTopologyResourceFilter("secret", filters.tagFilter, s.tags),
    ),
  );
  const buckets = $derived(
    (dashboard.data?.buckets ?? []).filter((bucket) =>
      matchesTopologyResourceFilter("bucket", filters.tagFilter),
    ),
  );
  const eventSourceMappings = $derived(
    directTopologyFilter ? [] : (dashboard.data?.eventSourceMappings ?? []),
  );
  const eventBridgeRules = $derived(
    directTopologyFilter?.kind && directTopologyFilter.kind !== "eventbridge"
      ? []
      : (dashboard.data?.eventBridgeRules ?? []).filter(
          () =>
            !directTopologyFilter ||
            directTopologyFilter.kind === "eventbridge",
        ),
  );
  const infra = $derived(
    getVisibleInfra(dashboard.data?.infrastructure ?? []).filter((probe) =>
      matchesTopologyInfraFilter(probe, filters.tagFilter),
    ),
  );
  const infraConnections = $derived(
    directTopologyFilter ? [] : (dashboard.data?.connections ?? []),
  );
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
  let viewportResetToken = $state(0);
  let infraOrderIds = $state<string[]>([]);
  let infraOrderHydrated = $state(false);
  let allNodePositions = $state<Record<string, InfraNodePosition>>({});
  let allPositionsHydrated = $state(false);
  let allNodeOverrides = $state<Record<string, NodeOverride>>({});
  let allOverridesHydrated = $state(false);

  const INFRA_ORDER_STORAGE_KEY = "tarn-ui-topology-infra-order-v1";
  const ALL_POSITIONS_STORAGE_KEY = "tarn-ui-topology-all-positions-v1";
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

  type DirectTopologyFilter = {
    kind: NodeKind;
    infraKind?: string;
  } | null;

  function resolveDirectTopologyFilter(query: string): DirectTopologyFilter {
    for (const token of parseTopologyFilterTokens(query)) {
      const normalized = token.toLowerCase();
      switch (normalized) {
        case "gateway":
        case "gateways":
        case "api":
        case "apigateway":
        case "api-gateway":
        case "api gateway":
          return { kind: "gateway" };
        case "eventbridge":
        case "event-bridge":
        case "event bridge":
        case "schedule":
        case "schedules":
        case "rule":
        case "rules":
          return { kind: "eventbridge" };
        case "topic":
        case "topics":
        case "sns":
          return { kind: "topic" };
        case "queue":
        case "queues":
        case "sqs":
          return { kind: "queue" };
        case "dynamodb":
        case "dynamo":
        case "ddb":
        case "table":
        case "tables":
        case "stream":
        case "streams":
          return { kind: "dynamodb" };
        case "lambda":
        case "lambdas":
        case "function":
        case "functions":
          return { kind: "function" };
        case "secret":
        case "secrets":
          return { kind: "secret" };
        case "bucket":
        case "buckets":
        case "s3":
        case "storage":
          return { kind: "bucket" };
        case "extension":
        case "extensions":
        case "cache":
        case "secrets cache":
          return { kind: "extension" };
        case "external":
        case "externals":
        case "infra":
        case "infrastructure":
          return { kind: "infra" };
        case "postgres":
        case "postgresql":
          return { kind: "infra", infraKind: "postgresql" };
        case "mysql":
          return { kind: "infra", infraKind: "mysql" };
        case "redis":
          return { kind: "infra", infraKind: "redis" };
        case "mongo":
        case "mongodb":
          return { kind: "infra", infraKind: "mongodb" };
        case "docker":
          return { kind: "infra", infraKind: "docker" };
        case "http":
        case "https":
          return { kind: "infra", infraKind: "http" };
      }
    }
    return null;
  }

  function parseTopologyFilterTokens(query: string): string[] {
    return query
      .trim()
      .split(/\s+/)
      .map((token) => token.trim())
      .filter(Boolean);
  }

  function extractTagOnlyTopologyFilterQuery(query: string): string {
    return parseTopologyFilterTokens(query)
      .filter((token) => !resolveDirectTopologyFilter(token))
      .join(" ");
  }

  function matchesTopologyResourceFilter(
    kind: NodeKind,
    query: string,
    tags?: Record<string, string>,
  ): boolean {
    if (directTopologyFilter) {
      if (directTopologyFilter.kind !== kind) {
        return false;
      }
      if (!tagOnlyTopologyFilterQuery) return true;
      return matchesTagFilter(tags, tagOnlyTopologyFilterQuery);
    }
    return matchesTagFilter(tags, query);
  }

  function matchesTopologyInfraFilter(
    probe: InfraProbe,
    query: string,
  ): boolean {
    if (!directTopologyFilter) return true;
    if (directTopologyFilter.kind !== "infra") return false;
    if (!directTopologyFilter.infraKind) return true;
    return (
      normalizeTopologyExternalKind(probe.kind) ===
      directTopologyFilter.infraKind
    );
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

  function organizeCanvasLayout() {
    allNodePositions = {};
    resetCanvasViewport();
  }

  function handleShortcutKeydown(event: KeyboardEvent) {
    if (!canvasExpanded || viewMode !== "connections") return;
    if (!(event.metaKey || event.ctrlKey)) return;
    if (isEditableTarget(event.target)) return;

    const key = event.key.toLowerCase();
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

<div class="h-full w-full min-w-0 flex flex-col">
  <!-- Toolbar — above the bordered canvas area, outside the card -->
  <div class="flex shrink-0 items-center gap-2 pb-2">
    <span class="font-mono text-[10px] text-muted-foreground/50">
      {viewMode === "components"
        ? `Explore and arrange ${resourceCount} resources of your infra below`
        : `Explore and arrange ${connectionCount} links of your infra below`}
    </span>

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
        class="relative flex h-6 w-6 items-center justify-center rounded text-muted-foreground/60 hover:bg-muted/60 hover:text-foreground transition-colors {canvasExpanded
          ? ''
          : 'group'}"
        aria-label={canvasExpanded ? "Collapse canvas" : "Expand canvas"}
        title={canvasExpanded ? "Collapse canvas" : "Expand canvas"}
        onclick={() => (canvasExpanded = !canvasExpanded)}
      >
        {#if !canvasExpanded}
          <span
            class="absolute inset-0 rounded animate-[expand-ping_2.5s_ease-in-out_infinite] bg-primary/10"
          ></span>
        {/if}
        {#if canvasExpanded}
          <ArrowsInSimpleIcon size={13} />
        {:else}
          <ArrowsOutSimpleIcon size={13} class="relative" />
        {/if}
      </button>
    </div>
  </div>

  <!-- Canvas area -->
  <div class="flex-1 min-h-0 overflow-hidden rounded-md">
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
            {viewportResetToken}
            onGatewayClick={openGateway}
            onNodePositionChange={setNodePosition}
            onNodeOverrideChange={setNodeOverride}
            onAutoOrganize={organizeCanvasLayout}
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
