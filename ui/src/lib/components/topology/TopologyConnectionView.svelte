<script lang="ts">
  import type {
    GatewaySummary,
    FunctionSummary,
    QueueSummary,
    BucketSummary,
    SecretSummary,
    InfraProbe,
    EventSourceMappingSummary,
    InfraConnection,
    RequestTrace,
  } from "$lib/types";
  import type { ConnectionNode } from "./types";
  import {
    buildTopologyGraph,
    infraKindTone,
    type InfraNodePosition,
  } from "./topology-connection-model";
  import TopologyConnectionCanvas from "./canvas/TopologyConnectionCanvas.svelte";
  import TopologyNodeTooltip from "./canvas/TopologyNodeTooltip.svelte";

  let {
    gateways = [],
    functions = [],
    queues = [],
    buckets = [],
    secrets = [],
    infra = [],
    infraNodePositions = {},
    eventSourceMappings = [],
    infraConnections = [],
    infraOrderIds = [],
    recentTraces = [],
    canvasExpanded = false,
    panEnabled = false,
    viewportResetToken = 0,
    onGatewayClick = (_id: string) => {},
    onInfraNodePositionChange = (
      _id: string,
      _position: InfraNodePosition,
    ) => {},
    onNavigate: _onNavigate = (_tab: string) => {},
  }: {
    gateways?: GatewaySummary[];
    functions?: FunctionSummary[];
    queues?: QueueSummary[];
    buckets?: BucketSummary[];
    secrets?: SecretSummary[];
    infra?: InfraProbe[];
    infraNodePositions?: Record<string, InfraNodePosition>;
    eventSourceMappings?: EventSourceMappingSummary[];
    infraConnections?: InfraConnection[];
    infraOrderIds?: string[];
    recentTraces?: RequestTrace[];
    canvasExpanded?: boolean;
    panEnabled?: boolean;
    viewportResetToken?: number;
    onGatewayClick?: (id: string) => void;
    onInfraNodePositionChange?: (
      id: string,
      position: InfraNodePosition,
    ) => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  const model = $derived(
    buildTopologyGraph({
      gateways,
      functions,
      queues,
      buckets,
      secrets,
      infra,
      infraNodePositions,
      eventSourceMappings,
      infraConnections,
      infraOrderIds,
      recentTraces,
    }),
  );
  const selectedTrace: RequestTrace | null = null;

  type TooltipState = {
    visible: boolean;
    x: number;
    y: number;
    title: string;
    detail: string;
    status: string;
    color: string;
  };

  let tooltip = $state<TooltipState>({
    visible: false,
    x: 0,
    y: 0,
    title: "",
    detail: "",
    status: "",
    color: "var(--color-primary)",
  });

  function handleNodeHover(payload: {
    node: ConnectionNode;
    clientX: number;
    clientY: number;
  }) {
    tooltip = {
      visible: true,
      x: payload.clientX,
      y: payload.clientY,
      title: payload.node.label,
      detail: payload.node.kind.toUpperCase(),
      status:
        payload.node.kind === "infra"
          ? payload.node.status === "connected"
            ? "CONNECTED"
            : "DISCONNECTED"
          : "ACTIVE",
      color: nodeColor(payload.node),
    };
  }

  function handleNodeLeave() {
    tooltip.visible = false;
  }

  function nodeColor(node: ConnectionNode): string {
    switch (node.kind) {
      case "gateway":
        return "var(--color-chart-1)";
      case "queue":
        return "var(--color-chart-4)";
      case "function":
        return "var(--color-primary)";
      case "secret":
      case "extension":
        return "var(--color-chart-2)";
      case "bucket":
        return "var(--color-chart-5, var(--color-primary))";
      case "infra": {
        if (node.status !== "connected") return "var(--color-destructive)";
        const probe = model.infraById.get(node.id);
        const tone = infraKindTone(probe?.kind ?? "");
        if (tone === "db") return "var(--color-chart-2)";
        if (tone === "cache") return "var(--color-chart-4)";
        if (tone === "service")
          return "var(--color-chart-5, var(--color-primary))";
        return "var(--color-primary)";
      }
      default:
        return "var(--color-primary)";
    }
  }
</script>

<div class="h-full w-full overflow-hidden overscroll-contain">
  <TopologyConnectionCanvas
    {model}
    {selectedTrace}
    {canvasExpanded}
    {panEnabled}
    {viewportResetToken}
    {onGatewayClick}
    {onInfraNodePositionChange}
    onNodeHover={handleNodeHover}
    onNodeLeave={handleNodeLeave}
  />

  <TopologyNodeTooltip
    visible={tooltip.visible}
    x={tooltip.x}
    y={tooltip.y}
    title={tooltip.title}
    detail={tooltip.detail}
    status={tooltip.status}
    color={tooltip.color}
  />
</div>
