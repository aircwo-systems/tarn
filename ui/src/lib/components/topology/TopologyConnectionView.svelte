<script lang="ts">
  import type {
    GatewaySummary,
    FunctionSummary,
    QueueSummary,
    DynamoDBTableSummary,
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
    withTopologyTraceActivity,
    type InfraNodePosition,
    type NodeOverride,
    type TopologyArrangement,
  } from "./topology-connection-model";
  import { infraKindCssVar } from "./topology-canvas-theme";
  import TopologyConnectionCanvas from "./canvas/TopologyConnectionCanvas.svelte";

  let {
    gateways = [],
    functions = [],
    queues = [],
    dynamodbTables = [],
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
    bare = false,
    viewportResetToken = 0,
    onGatewayClick = (_id: string) => {},
    arrangement = {},
    onSectionOrderChange = (_sectionId: string, _keys: string[]) => {},
    onSectionMove = (_sectionId: string, _offset: InfraNodePosition) => {},
    onNodePositionChange = (
      _id: string,
      _kind: ConnectionNode["kind"],
      _position: InfraNodePosition,
    ) => {},
    onNodeOverrideChange = (_id: string, _override: NodeOverride) => {},
    onNavigate = (_tab: string) => {},
  }: {
    gateways?: GatewaySummary[];
    functions?: FunctionSummary[];
    queues?: QueueSummary[];
    dynamodbTables?: DynamoDBTableSummary[];
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
    bare?: boolean;
    viewportResetToken?: number;
    onGatewayClick?: (id: string) => void;
    /** The user's card order and section moves; see TopologyArrangement. */
    arrangement?: TopologyArrangement;
    onSectionOrderChange?: (sectionId: string, keys: string[]) => void;
    onSectionMove?: (sectionId: string, offset: InfraNodePosition) => void;
    onNodePositionChange?: (
      id: string,
      kind: ConnectionNode["kind"],
      position: InfraNodePosition,
    ) => void;
    onNodeOverrideChange?: (id: string, override: NodeOverride) => void;
    onNavigate?: (tab: string) => void;
  } = $props();

  // The layout fills the shape of the view it sits in. Coarse steps, and
  // held while exploring, so resizing doesn't keep reshuffling the graph.
  let frameW = $state(0);
  let frameH = $state(0);
  let aspect = $state<number | undefined>(undefined);
  $effect(() => {
    if (canvasExpanded || frameW < 200 || frameH < 200) return;
    const next = Math.round((frameH / frameW) * 10) / 10;
    if (next !== aspect) aspect = next;
  });

  const staticModel = $derived(
    buildTopologyGraph({
      gateways,
      functions,
      queues,
      dynamodbTables,
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
      aspect,
      arrangement,
    }),
  );
  const model = $derived(withTopologyTraceActivity(staticModel, recentTraces));
  const selectedTrace: RequestTrace | null = null;

</script>

<div
  class="h-full w-full overflow-hidden overscroll-contain"
  bind:clientWidth={frameW}
  bind:clientHeight={frameH}
>
  <TopologyConnectionCanvas
    {model}
    {selectedTrace}
    {canvasExpanded}
    {panEnabled}
    {bare}
    {viewportResetToken}
    {onGatewayClick}
    {onNodePositionChange}
    {onNodeOverrideChange}
    {onSectionOrderChange}
    {onSectionMove}
    {onNavigate}
  />
</div>
