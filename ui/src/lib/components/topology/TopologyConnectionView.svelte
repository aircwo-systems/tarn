<script lang="ts">
  import type {
    GatewaySummary,
    FunctionSummary,
    QueueSummary,
    TopicSummary,
    BucketSummary,
    SecretSummary,
    InfraProbe,
    EventBridgeRuleSummary,
    EventSourceMappingSummary,
    InfraConnection,
    RequestTrace
  } from "$lib/types";
  import type { ConnectionNode } from "./types";
  import {
    buildTopologyGraph,
    type InfraNodePosition,
    type NodeOverride,
  } from "./topology-connection-model";
  import { infraKindCssVar } from "./topology-canvas-theme";
  import TopologyConnectionCanvas from "./canvas/TopologyConnectionCanvas.svelte";
  import TopologyNodeTooltip from "./canvas/TopologyNodeTooltip.svelte";

  let {
    gateways = [],
    functions = [],
    queues = [],
    topics = [],
    buckets = [],
    secrets = [],
    infra = [],
    allNodePositions = {},
    allNodeOverrides = {},
    eventSourceMappings = [],
    infraConnections = [],
    eventBridgeRules = [],
    infraOrderIds = [],
    recentTraces = [],
    canvasExpanded = false,
    panEnabled = false,
    viewportResetToken = 0,
    onGatewayClick = (_id: string) => {},
    onNodePositionChange = (
      _id: string,
      _kind: ConnectionNode["kind"],
      _position: InfraNodePosition,
    ) => {},
    onNodeOverrideChange = (_id: string, _override: NodeOverride) => {},
    onAutoOrganize = () => {},
    onNavigate = (_tab: string) => {},
  }: {
    gateways?: GatewaySummary[];
    functions?: FunctionSummary[];
    queues?: QueueSummary[];
    topics?: TopicSummary[];
    buckets?: BucketSummary[];
    secrets?: SecretSummary[];
    infra?: InfraProbe[];
    allNodePositions?: Record<string, InfraNodePosition>;
    allNodeOverrides?: Record<string, NodeOverride>;
    eventSourceMappings?: EventSourceMappingSummary[];
    infraConnections?: InfraConnection[];
    eventBridgeRules?: EventBridgeRuleSummary[];
    infraOrderIds?: string[];
    recentTraces?: RequestTrace[];
    canvasExpanded?: boolean;
    panEnabled?: boolean;
    viewportResetToken?: number;
    onGatewayClick?: (id: string) => void;
    onNodePositionChange?: (
      id: string,
      kind: ConnectionNode["kind"],
      position: InfraNodePosition,
    ) => void;
    onNodeOverrideChange?: (id: string, override: NodeOverride) => void;
    onAutoOrganize?: () => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  const model = $derived(
    buildTopologyGraph({
      gateways,
      functions,
      queues,
      topics,
      buckets,
      secrets,
      infra,
      allNodePositions,
      allNodeOverrides,
      eventSourceMappings,
      infraConnections,
      eventBridgeRules,
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
        return "var(--topology-gateway)";
      case "queue":
        return "var(--color-chart-4)";
      case "eventbridge":
        return "var(--color-chart-5, var(--color-primary))";
      case "topic":
        return "var(--color-chart-1)";
      case "function":
        return "var(--color-primary)";
      case "secret":
      case "extension":
        return "var(--color-chart-2)";
      case "bucket":
        return "var(--color-chart-5, var(--color-primary))";
      case "infra": {
        if (node.status !== "connected") return "var(--color-destructive)";
        return infraKindCssVar(model.infraById.get(node.id)?.kind ?? "");
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
    {onNodePositionChange}
    {onNodeOverrideChange}
    {onAutoOrganize}
    {onNavigate}
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
