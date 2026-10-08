import type {
  BucketSummary,
  DynamoDBTableSummary,
  EventBridgeRuleSummary,
  EventSourceMappingSummary,
  FilterCriteria,
  FunctionSummary,
  GatewaySummary,
  InfraConnection,
  InfraProbe,
  QueueSummary,
  TopicSummary,
  RequestTrace,
  SecretSummary,
} from "$lib/types";
import { resolveTopologyNodeSize, resolveTopologyNodeView } from "./registry";
import type { ConnectionNode, NodeSide, NodeSize, NodeView } from "./types";
export type { NodeSide, NodeSize } from "./types";

/** Per-node appearance overrides saved to localStorage. */
export type NodeOverride = {
  inputSide?: NodeSide;
  outputSide?: NodeSide;
  size?: NodeSize;
  view?: NodeView;
  /** Card opened to show its details. */
  expanded?: boolean;
};

export const CONNECTION_CANVAS = {
  width: 3400,
  height: 1800,
  // Wide enough for a kind tag and a readable name side by side.
  nodeHalfWidth: 124,
  nodeHalfHeight: 34,
  cacheHalfWidth: 124,
  cacheHalfHeight: 34,
  infraHalfWidth: 124,
} as const;

export const TOPOLOGY_GRID_STEP = 30;
export const TOPOLOGY_MIN_NODE_GAP = TOPOLOGY_GRID_STEP * 2;
const TOPOLOGY_VIEWPORT_REFERENCE = {
  width: 2200,
  height: 1000,
} as const;

const TRACE_WINDOW_MS = 60_000;

export interface EdgeActivity {
  count: number;
  hasError: boolean;
  latestMs: number;
}

export interface LaneEdge {
  id: string;
  from: ConnectionNode;
  to: ConnectionNode;
  lane: number;
  laneCount: number;
  path: string;
}

export interface GwEdge extends LaneEdge {
  active: boolean;
  activity?: EdgeActivity;
}

export interface QueueFnEdge extends LaneEdge {
  activity?: EdgeActivity;
  filterLabel: string | null;
}

export interface SnsEdge extends LaneEdge {
  activity?: EdgeActivity;
}

export interface DlqEdge {
  id: string;
  from: ConnectionNode;
  to: ConnectionNode;
  path: string;
  activity?: EdgeActivity;
}

export interface InfraEdge extends LaneEdge {
  probe?: InfraProbe;
  isConnected: boolean;
  activity?: EdgeActivity;
}

/** A labelled run of one kind of node in a column, drawn behind them. */
export interface TopologySection {
  id: string;
  label: string;
  count: number;
  /** Graph keys of the nodes inside; the frame is drawn around wherever they are. */
  nodeKeys: string[];
  /** How far the user moved it from where the layout put it. */
  offset: { x: number; y: number };
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TopologyGraphModel {
  hasData: boolean;
  canvasSize: { width: number; height: number };
  sections: TopologySection[];
  /**
   * Any function may read any secret through the Secrets Cache: a bracket
   * from the functions into it and one out of it to the secrets, rather than
   * a line from every function and to every secret.
   */
  cacheBus: { in: string | null; out: string | null } | null;
  eventBridgeRuleById: Map<string, EventBridgeRuleSummary>;
  functionById: Map<string, FunctionSummary>;
  dynamodbById: Map<string, DynamoDBTableSummary>;
  infraById: Map<string, InfraProbe>;
  allNodes: ConnectionNode[];
  hitTestNodes: ConnectionNode[];
  nodeByGraphKey: Map<string, ConnectionNode>;
  nodeById: Map<string, ConnectionNode>;
  allEdges: Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>;
  forwardAdjacency: Map<string, Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>>;
  reverseAdjacency: Map<string, Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>>;
  nodes: {
    gateways: ConnectionNode[];
    eventbridges: ConnectionNode[];
    topics: ConnectionNode[];
    queues: ConnectionNode[];
    dynamodbs: ConnectionNode[];
    functions: ConnectionNode[];
    buckets: ConnectionNode[];
    secrets: ConnectionNode[];
    infra: ConnectionNode[];
    cacheExtension: ConnectionNode | null;
  };
  edges: {
    apigwToQueue: GwEdge[];
    apigwToFunction: GwEdge[];
    eventbridgeToFunction: SnsEdge[];
    snsToQueue: SnsEdge[];
    snsToFunction: SnsEdge[];
    queueToFunction: QueueFnEdge[];
    dynamodbToFunction: QueueFnEdge[];
    queueToDlq: DlqEdge[];
    bucketToFunction: LaneEdge[];
    functionToDynamodb: Array<LaneEdge & { activity?: EdgeActivity }>;
    functionToCache: Array<LaneEdge & { activity?: EdgeActivity }>;
    cacheToSecret: Array<LaneEdge & { activity?: EdgeActivity }>;
    functionToInfra: InfraEdge[];
  };
  infraLane: {
    x: number;
    y: number;
    width: number;
    height: number;
  };
  infraRoute: {
    x: number;
    y: number;
  };
  traces: {
    ticker: RequestTrace[];
    edgeActivity: Map<string, EdgeActivity>;
    cacheActivity?: EdgeActivity;
  };
}

export interface ViewportTransform {
  scale: number;
  offsetX: number;
  offsetY: number;
}

export interface InfraNodePosition {
  x: number;
  y: number;
}

export interface HoverFocusState {
  active: boolean;
  nodeIds: Set<string>;
  edgeIds: Set<string>;
}

export interface BuildTopologyGraphInput {
  gateways: GatewaySummary[];
  functions: FunctionSummary[];
  queues: QueueSummary[];
  dynamodbTables: DynamoDBTableSummary[];
  topics: TopicSummary[];
  buckets: BucketSummary[];
  secrets: SecretSummary[];
  infra: InfraProbe[];
  allNodePositions?: Record<string, InfraNodePosition>;
  allNodeOverrides?: Record<string, NodeOverride>;
  infraConnections: InfraConnection[];
  eventSourceMappings: EventSourceMappingSummary[];
  eventBridgeRules?: EventBridgeRuleSummary[];
  infraOrderIds: string[];
  recentTraces?: RequestTrace[];
  now?: number;
  /** Height over width of the view the graph is shown in; the layout fills it. */
  aspect?: number;
  /** How the user rearranged things: card order and section moves. */
  arrangement?: TopologyArrangement;
}

/**
 * The user's rearranging, kept relative to the layout so it survives the
 * layout changing: the order of cards in a section (by graph key), and how
 * far each section was moved from where the layout put it.
 */
export interface TopologyArrangement {
  order?: Record<string, string[]>;
  offsets?: Record<string, { x: number; y: number }>;
}

export function buildTopologyGraph(input: BuildTopologyGraphInput): TopologyGraphModel {
  const {
    gateways,
    functions,
    queues,
    dynamodbTables,
    topics,
    buckets,
    secrets,
    infra,
    allNodePositions = {},
    allNodeOverrides = {},
    infraConnections,
    eventSourceMappings,
    eventBridgeRules = [],
    infraOrderIds,
    aspect,
    arrangement,
  } = input;

  const eventBridgeTargetCounts = new Map<string, number>();
  for (const connection of infraConnections) {
    if (connection.targetKind !== "eventbridge-lambda") continue;
    const key = connection.sourceFunction;
    eventBridgeTargetCounts.set(key, (eventBridgeTargetCounts.get(key) ?? 0) + 1);
  }
  for (const rule of eventBridgeRules) {
    if (!eventBridgeTargetCounts.has(rule.name)) {
      eventBridgeTargetCounts.set(rule.name, rule.targets?.length ?? 0);
    }
  }

  const connGateways = gateways.map(
    (gw, i): ConnectionNode => ({
      id: gw.apiId,
      x: 0,
      y: 0,
      label: trimLabel(gw.name, 13),
      fullLabel: gw.name,
      sub: `${gw.routes} routes`,
      kind: "gateway",
    }),
  );

  const connEventBridges = [...eventBridgeTargetCounts.entries()].map(
    ([ruleName, targetCount], i): ConnectionNode => ({
      id: ruleName,
      x: 0,
      y: 0,
      label: trimLabel(ruleName, 13),
      fullLabel: ruleName,
      sub: `${targetCount} target${targetCount === 1 ? "" : "s"}`,
      kind: "eventbridge",
    }),
  );

  const connTopics = topics.map(
    (t, i): ConnectionNode => ({
      id: t.name,
      x: 0,
      y: 0,
      label: trimLabel(t.name, 13),
      fullLabel: t.name,
      sub: `${t.subscriptions} sub`,
      kind: "topic",
    }),
  );

  const connQueues = queues.map(
    (q, i): ConnectionNode => ({
      id: q.name,
      x: 0,
      y: 0,
      label: trimLabel(q.name, 13),
      fullLabel: q.name,
      sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
      kind: "queue",
    }),
  );

  const connDynamodbs = dynamodbTables.map(
    (table, i): ConnectionNode => ({
      id: table.name,
      x: 0,
      y: 0,
      label: trimLabel(table.name, 13),
      fullLabel: table.name,
      sub: table.streamEnabled ? `${table.itemCount} item · stream` : `${table.itemCount} item`,
      kind: "dynamodb",
    }),
  );

  const connFunctions = functions.map(
    (fn, i): ConnectionNode => ({
      id: fn.name,
      x: 0,
      y: 0,
      label: trimLabel(fn.name, 13),
      fullLabel: fn.name,
      sub: fn.runtime,
      kind: "function",
    }),
  );

  const connBuckets = buckets.map(
    (b, i): ConnectionNode => ({
      id: b.name,
      x: 0,
      y: 0,
      label: trimLabel(b.name, 13),
      fullLabel: b.name,
      sub: `${b.objects} obj`,
      kind: "bucket",
      bucket: b,
    }),
  );

  const connSecrets = secrets.map(
    (s, i): ConnectionNode => ({
      id: s.name,
      x: 0,
      y: 0,
      label: trimLabel(s.name, 13),
      fullLabel: s.name,
      sub: `v${s.versionId.slice(0, 6)}`,
      kind: "secret",
    }),
  );

  const mainGroups = [
    connGateways,
    connEventBridges,
    connTopics,
    connQueues,
    connFunctions,
    connSecrets,
    connDynamodbs,
    connBuckets,
  ];

  // Apply size/side overrides BEFORE layout so the packer uses each node's
  // actual rendered dimensions. Each override is scoped to the individual node
  // by ID — connected nodes are never affected.
  for (const group of mainGroups) {
    for (const node of group) {
      applyNodeOverride(node, allNodeOverrides[`${node.kind}:${node.id}`]);
    }
  }

  const connInfraNodes = buildInfraNodes(infra, infraOrderIds);
  for (const node of connInfraNodes)
    applyNodeOverride(node, allNodeOverrides[`${node.kind}:${node.id}`]);

  const connCacheExtension: ConnectionNode | null =
    connSecrets.length > 0
      ? {
          id: "secrets-cache-extension",
          x: 0,
          y: 0,
          label: "Secrets Cache",
          sub: "localhost:2773",
          kind: "extension",
        }
      : null;
  if (connCacheExtension)
    applyNodeOverride(
      connCacheExtension,
      allNodeOverrides[`${connCacheExtension.kind}:${connCacheExtension.id}`],
    );

  const gatewayIdByName = new Map(gateways.map((gw) => [gw.name, gw.apiId]));
  const queueByName = new Map(queues.map((q) => [q.name, q]));
  const functionByName = new Map(functions.map((fn) => [fn.name, fn]));
  const gatewayNodeById = new Map(connGateways.map((node) => [node.id, node]));
  const eventBridgeNodeById = new Map(connEventBridges.map((node) => [node.id, node]));
  const topicNodeById = new Map(connTopics.map((node) => [node.id, node]));
  const queueNodeById = new Map(connQueues.map((node) => [node.id, node]));
  const dynamodbNodeById = new Map(connDynamodbs.map((node) => [node.id, node]));
  const functionNodeById = new Map(connFunctions.map((node) => [node.id, node]));
  const bucketNodeById = new Map(connBuckets.map((node) => [node.id, node]));
  const infraNodeById = new Map(connInfraNodes.map((node) => [node.id, node]));
  const infraProbeByNodeId = new Map(infra.map((probe) => [infraNodeId(probe), probe]));
  const infraByNodeId = new Map(
    connInfraNodes.map((node) => [node.id, infraProbeByNodeId.get(node.id)]),
  );

  const apigwToQueue = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "apigw-sqs") return [];
      const gwId = gatewayIdByName.get(c.sourceFunction) ?? c.sourceFunction;
      const from = gatewayNodeById.get(gwId);
      const queueName = c.targetId || c.targetName;
      const to = queueNodeById.get(queueName);
      if (!from || !to) return [];
      const queue = queueByName.get(queueName);
      const total =
        (queue?.approxVisible ?? 0) + (queue?.approxInFlight ?? 0) + (queue?.approxDelayed ?? 0);
      return [{ from, to, active: total > 0 }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const apigwToFunction = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "apigw-lambda") return [];
      const gwId = gatewayIdByName.get(c.sourceFunction) ?? c.sourceFunction;
      const from = gatewayNodeById.get(gwId);
      const fnName = c.targetId || c.targetName;
      const to = functionNodeById.get(fnName);
      if (!from || !to) return [];
      const fn = functionByName.get(fnName);
      return [{ from, to, active: (fn?.messagesProcessed ?? 0) > 0 }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const seenEbEdges = new Set<string>();
  const ebEdgeCandidates: { from: ConnectionNode; to: ConnectionNode }[] = [];
  for (const c of infraConnections) {
    if (c.targetKind !== "eventbridge-lambda") continue;
    const from = eventBridgeNodeById.get(c.sourceFunction);
    const fnName = c.targetId || c.targetName;
    const to = functionNodeById.get(fnName);
    if (!from || !to) continue;
    const key = `${from.id}→${to.id}`;
    if (seenEbEdges.has(key)) continue;
    seenEbEdges.add(key);
    ebEdgeCandidates.push({ from, to });
  }
  for (const rule of eventBridgeRules) {
    const from = eventBridgeNodeById.get(rule.name);
    if (!from) continue;
    for (const target of rule.targets ?? []) {
      const fnName = lambdaNameFromArn(target.arn);
      const to = functionNodeById.get(fnName);
      if (!to) continue;
      const key = `${from.id}→${to.id}`;
      if (seenEbEdges.has(key)) continue;
      seenEbEdges.add(key);
      ebEdgeCandidates.push({ from, to });
    }
  }
  const eventbridgeToFunction = withLanes(
    ebEdgeCandidates,
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const snsToQueue = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "sns-sqs") return [];
      const from = topicNodeById.get(c.sourceFunction);
      const queueName = c.targetId || c.targetName;
      const to = queueNodeById.get(queueName);
      if (!from || !to) return [];
      return [{ from, to }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const snsToFunction = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "sns-lambda") return [];
      const from = topicNodeById.get(c.sourceFunction);
      const fnName = c.targetId || c.targetName;
      const to = functionNodeById.get(fnName);
      if (!from || !to) return [];
      return [{ from, to }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const queueToFunctionPairs: {
    queueId: string;
    fnId: string;
    filterCriteria?: FilterCriteria;
  }[] = [];
  const queueFnPairKeys = new Set<string>();
  const queueFnPairKey = (queueId: string, fnId: string) => `${queueId}→${fnId}`;

  for (const mapping of eventSourceMappings) {
    const pairKey = queueFnPairKey(mapping.queueName, mapping.functionName);
    if (queueFnPairKeys.has(pairKey)) continue;
    queueFnPairKeys.add(pairKey);
    queueToFunctionPairs.push({
      queueId: mapping.queueName,
      fnId: mapping.functionName,
      filterCriteria: mapping.filterCriteria,
    });
  }

  for (const c of infraConnections) {
    if (c.targetKind !== "sqs-lambda") continue;
    const fnId = c.targetId || c.targetName;
    const pairKey = queueFnPairKey(c.sourceFunction, fnId);
    if (queueFnPairKeys.has(pairKey)) continue;
    queueFnPairKeys.add(pairKey);
    queueToFunctionPairs.push({
      queueId: c.sourceFunction,
      fnId,
      filterCriteria: c.filterCriteria,
    });
  }

  const queueToFunction = withLanes(
    queueToFunctionPairs.flatMap(({ queueId, fnId, filterCriteria }) => {
      const from = queueNodeById.get(queueId);
      const to = functionNodeById.get(fnId);
      if (!from || !to) return [];
      return [
        {
          from,
          to,
          filterLabel: filterLabel(filterCriteria),
        },
      ];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const dynamodbMappingPairs: {
    tableId: string;
    fnId: string;
    filterCriteria?: FilterCriteria;
  }[] = [];
  const dynamoPairKeys = new Set<string>();
  const dynamoPairKey = (tableId: string, fnId: string) => `${tableId}→${fnId}`;

  for (const mapping of eventSourceMappings) {
    if ((mapping.sourceType ?? "").toLowerCase() !== "dynamodb-stream") continue;
    const tableId = mapping.sourceName || mapping.queueName;
    const pairKey = tableId ? dynamoPairKey(tableId, mapping.functionName) : "";
    if (!tableId || dynamoPairKeys.has(pairKey)) continue;
    dynamoPairKeys.add(pairKey);
    dynamodbMappingPairs.push({
      tableId,
      fnId: mapping.functionName,
      filterCriteria: mapping.filterCriteria,
    });
  }

  for (const c of infraConnections) {
    if (c.targetKind !== "dynamodb-stream-lambda") continue;
    const fnId = c.targetId || c.targetName;
    const pairKey = c.sourceFunction ? dynamoPairKey(c.sourceFunction, fnId) : "";
    if (!c.sourceFunction || dynamoPairKeys.has(pairKey)) continue;
    dynamoPairKeys.add(pairKey);
    dynamodbMappingPairs.push({
      tableId: c.sourceFunction,
      fnId,
      filterCriteria: c.filterCriteria,
    });
  }

  const dynamodbToFunction = withLanes(
    dynamodbMappingPairs.flatMap(({ tableId, fnId, filterCriteria }) => {
      const from = dynamodbNodeById.get(tableId);
      const to = functionNodeById.get(fnId);
      if (!from || !to) return [];
      return [
        {
          from,
          to,
          filterLabel: filterLabel(filterCriteria),
        },
      ];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const queueToDlq = infraConnections.flatMap((c) => {
    if (c.targetKind !== "queue-dlq") return [];
    const from = queueNodeById.get(c.sourceFunction);
    const to = queueNodeById.get(c.targetId || c.targetName);
    if (!from || !to || from.id === to.id) return [];
    return [
      {
        id: `${from.id}→${to.id}`,
        from,
        to,
        path: dlqArcPath(from, to),
      },
    ];
  });

  const seenBucketFnEdges = new Set<string>();
  const bucketFnCandidates: Array<{ from: ConnectionNode; to: ConnectionNode }> = [];

  for (const c of infraConnections) {
    if (c.targetKind !== "s3-lambda") continue;
    const from = bucketNodeById.get(c.sourceFunction);
    const fnName = c.targetId || c.targetName;
    const to = functionNodeById.get(fnName);
    if (!from || !to) continue;
    const key = `${from.id}→${to.id}`;
    if (seenBucketFnEdges.has(key)) continue;
    seenBucketFnEdges.add(key);
    bucketFnCandidates.push({ from, to });
  }

  const bucketToFunction = withLanes(
    bucketFnCandidates,
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    id: `${edge.from.id}→${edge.to.id}`,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const awsServiceKinds = [
    "apigw-sqs",
    "apigw-lambda",
    "sns-sqs",
    "sns-lambda",
    "s3-lambda",
    "sqs-lambda",
    "dynamodb-stream-lambda",
    "queue-dlq",
  ];

  const functionToDynamodb = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "dynamodb-table") return [];
      const from = functionNodeById.get(c.sourceFunction);
      const tableName = c.targetId || c.targetName;
      const to = dynamodbNodeById.get(tableName);
      if (!from || !to) return [];
      return [{ from, to }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    id: `${edge.from.id}→${edge.to.id}`,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const functionToInfra = withLanes(
    infraConnections.flatMap((c) => {
      if (awsServiceKinds.includes(c.targetKind)) return [];
      const from = functionNodeById.get(c.sourceFunction);
      const to = infraNodeById.get(c.targetId);
      if (!from || !to) return [];
      const probe = infraByNodeId.get(to.id);
      return [{ from, to, probe, isConnected: probe?.status === "connected" }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    id: `${edge.from.id}→${edge.to.id}`,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const functionToCache = connCacheExtension
    ? withLanes(
        [...connFunctions]
          .sort((a, b) => a.y - b.y)
          .map((fn) => ({
            from: fn,
            to: connCacheExtension,
          })),
        (edge) => `${edge.from.id}→cache`,
      ).map((edge) => ({
        ...edge,
        id: `${edge.from.id}→cache`,
        path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
      }))
    : [];

  const cacheActivity: EdgeActivity | undefined = undefined;

  const cacheToSecret = connCacheExtension
    ? withLanes(
        [...connSecrets]
          .sort((a, b) => a.y - b.y)
          .map((secret) => ({
            from: connCacheExtension,
            to: secret,
          })),
        (edge) => `cache→${edge.to.id}`,
      ).map((edge) => ({
        ...edge,
        id: `cache→${edge.to.id}`,
        path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
      }))
    : [];

  // What an opened card shows.
  const byName = <T extends { name: string }>(items: T[]) =>
    new Map(items.map((item) => [item.name, item]));
  const lookups = {
    gateway: new Map(gateways.map((gw) => [gw.apiId, gw])),
    function: byName(functions),
    queue: byName(queues),
    topic: byName(topics),
    secret: byName(secrets),
    dynamodb: byName(dynamodbTables),
    bucket: byName(buckets),
    eventbridge: byName(eventBridgeRules),
  };
  const describe = (node: ConnectionNode): Array<[string, string]> => {
    switch (node.kind) {
      case "gateway": {
        const gw = lookups.gateway.get(node.id);
        return gw
          ? [
              ["type", gw.version === "v1" ? "REST" : "HTTP"],
              ["routes", String(gw.routes)],
              ["stage", gw.defaultStage || "—"],
            ]
          : [];
      }
      case "function": {
        const fn = lookups.function.get(node.id);
        return fn
          ? [
              ["runtime", fn.runtime],
              ["memory", `${fn.memoryMB} MB`],
              ["timeout", `${fn.timeoutSec}s`],
              ["invocations", String(fn.invocations ?? 0)],
              ["state", fn.state],
            ]
          : [];
      }
      case "queue": {
        const q = lookups.queue.get(node.id);
        return q
          ? [
              ["visible", String(q.approxVisible)],
              ["in flight", String(q.approxInFlight)],
              ["delayed", String(q.approxDelayed)],
              ...(q.dlqName ? [["dead letters", q.dlqName] as [string, string]] : []),
            ]
          : [];
      }
      case "topic": {
        const t = lookups.topic.get(node.id);
        return t
          ? [
              ["subscriptions", String(t.subscriptions)],
              ["fifo", t.fifo ? "yes" : "no"],
            ]
          : [];
      }
      case "secret": {
        const sec = lookups.secret.get(node.id);
        return sec
          ? [
              ["version", sec.versionId.slice(0, 8)],
              ["changed", sec.lastChangedDate?.slice(0, 10) || "—"],
            ]
          : [];
      }
      case "dynamodb": {
        const t = lookups.dynamodb.get(node.id);
        return t
          ? [
              ["items", String(t.itemCount)],
              ["key", t.keySchema || "—"],
              ["stream", t.streamEnabled ? (t.streamViewType ?? "on") : "off"],
            ]
          : [];
      }
      case "bucket": {
        const b = lookups.bucket.get(node.id) as { objectCount?: number } | undefined;
        return b?.objectCount !== undefined ? [["objects", String(b.objectCount)]] : [];
      }
      case "eventbridge": {
        const r = lookups.eventbridge.get(node.id);
        return r
          ? [
              ["schedule", r.scheduleExpression || "event pattern"],
              ["state", r.state.toLowerCase()],
              ...(r.nextRunAt ? [["next run", r.nextRunAt.slice(11, 19)] as [string, string]] : []),
            ]
          : [];
      }
      case "infra": {
        const probe = infraProbeByNodeId.get(node.id);
        return probe
          ? [
              ["address", `${probe.host}:${probe.port}`],
              ["status", probe.status],
              ...(probe.status === "connected"
                ? [["latency", `${probe.latencyMs}ms`] as [string, string]]
                : []),
              ...(probe.version ? [["version", probe.version] as [string, string]] : []),
            ]
          : [];
      }
      default:
        return [];
    }
  };

  const allNodes = [
    ...connGateways,
    ...connEventBridges,
    ...connTopics,
    ...connQueues,
    ...connDynamodbs,
    ...connFunctions,
    ...connBuckets,
    ...connSecrets,
    ...(connCacheExtension ? [connCacheExtension] : []),
    ...connInfraNodes,
  ];
  for (const node of allNodes) node.details = describe(node);

  const hitTestNodes = [
    ...connInfraNodes,
    ...connSecrets,
    ...(connCacheExtension ? [connCacheExtension] : []),
    ...connFunctions,
    ...connDynamodbs,
    ...connEventBridges,
    ...connTopics,
    ...connQueues,
    ...connBuckets,
    ...connGateways,
  ];

  const nodeByGraphKey = new Map(allNodes.map((node) => [graphNodeKey(node), node]));
  const nodeById = new Map<string, ConnectionNode>();
  for (const node of allNodes) {
    if (!nodeById.has(node.id)) nodeById.set(node.id, node);
  }

  const allEdges = [
    ...apigwToQueue,
    ...apigwToFunction,
    ...eventbridgeToFunction,
    ...snsToQueue,
    ...snsToFunction,
    ...queueToFunction,
    ...dynamodbToFunction,
    ...queueToDlq,
    ...bucketToFunction,
    ...functionToDynamodb,
    ...functionToCache,
    ...cacheToSecret,
    ...functionToInfra,
  ];

  const forwardAdjacency = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();
  const reverseAdjacency = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();
  for (const edge of allEdges) {
    const forwardEdges = forwardAdjacency.get(edge.from.id) ?? [];
    forwardEdges.push(edge);
    forwardAdjacency.set(edge.from.id, forwardEdges);

    const reverseEdges = reverseAdjacency.get(edge.to.id) ?? [];
    reverseEdges.push(edge);
    reverseAdjacency.set(edge.to.id, reverseEdges);
  }

  // Lay the graph out by how requests flow, now that the links are known.
  const triggerBuckets = new Set(bucketToFunction.map((edge) => edge.from.id));
  const dlqSource = new Map(queueToDlq.map((edge) => [edge.to.id, edge.from.id]));
  const fnEdgeCount = (kinds: ConnectionNode["kind"][]) =>
    allEdges.filter((edge) => edge.from.kind === "function" && kinds.includes(edge.to.kind)).length;
  const dataStage: FlowStage = {
    id: "data",
    sections: [
      { id: "dynamodb", label: "DynamoDB", nodes: connDynamodbs },
      {
        id: "bucket",
        label: "Buckets",
        nodes: connBuckets.filter((b) => !triggerBuckets.has(b.id)),
      },
      { id: "secret", label: "Secrets", nodes: connSecrets },
    ],
  };
  const servicesStage: FlowStage = {
    id: "services",
    sections: [{ id: "infra", label: "Services", nodes: connInfraNodes }],
  };
  // Whichever of data and services the functions reach more sits nearer them.
  const servicesFirst = fnEdgeCount(["infra"]) > fnEdgeCount(["dynamodb", "bucket", "secret"]);
  const cacheStage: FlowStage = {
    id: "cache",
    sections: [
      { id: "extension", label: "", nodes: connCacheExtension ? [connCacheExtension] : [] },
    ],
  };
  const layout = layoutFlow(
    [
      {
        id: "entry",
        sections: [
          { id: "gateway", label: "API gateways", nodes: connGateways },
          { id: "eventbridge", label: "EventBridge rules", nodes: connEventBridges },
          {
            id: "bucket-trigger",
            label: "Buckets",
            nodes: connBuckets.filter((b) => triggerBuckets.has(b.id)),
          },
        ],
      },
      // Topics fan out to queues, so they get a column before them.
      { id: "topics", sections: [{ id: "topic", label: "Topics", nodes: connTopics }] },
      { id: "queues", sections: [{ id: "queue", label: "Queues", nodes: connQueues }] },
      { id: "compute", sections: [{ id: "function", label: "Functions", nodes: connFunctions }] },
      ...(servicesFirst
        ? [servicesStage, cacheStage, dataStage]
        : [cacheStage, dataStage, servicesStage]),
    ],
    allEdges.filter((edge) => !(edge.from.kind === "function" && edge.to.kind === "extension")),
    dlqSource,
    aspect,
    arrangement,
  );

  // Nodes the user dragged stay where they put them.
  for (const node of allNodes) {
    const pos = getPersistedNodePosition(allNodePositions, node);
    if (!pos) continue;
    const next = resolveNodeSpacing(
      node,
      pos,
      allNodes,
      TOPOLOGY_MIN_NODE_GAP,
      layout.height,
      layout.width,
    );
    node.x = next.x;
    node.y = next.y;
  }

  const model: TopologyGraphModel = {
    hasData:
      gateways.length > 0 ||
      connEventBridges.length > 0 ||
      topics.length > 0 ||
      functions.length > 0 ||
      queues.length > 0 ||
      dynamodbTables.length > 0 ||
      buckets.length > 0 ||
      secrets.length > 0 ||
      infra.length > 0,
    canvasSize: { width: layout.width, height: layout.height },
    sections: layout.sections,
    cacheBus: null,
    eventBridgeRuleById: new Map(eventBridgeRules.map((rule) => [rule.name, rule])),
    functionById: new Map(functions.map((fn) => [fn.name, fn])),
    dynamodbById: new Map(dynamodbTables.map((table) => [table.name, table])),
    infraById: new Map(
      [...infraByNodeId.entries()].filter((entry): entry is [string, InfraProbe] => !!entry[1]),
    ),
    allNodes,
    hitTestNodes,
    nodeByGraphKey,
    nodeById,
    allEdges,
    forwardAdjacency,
    reverseAdjacency,
    nodes: {
      gateways: connGateways,
      eventbridges: connEventBridges,
      topics: connTopics,
      queues: connQueues,
      dynamodbs: connDynamodbs,
      functions: connFunctions,
      buckets: connBuckets,
      secrets: connSecrets,
      infra: connInfraNodes,
      cacheExtension: connCacheExtension,
    },
    edges: {
      apigwToQueue,
      apigwToFunction,
      eventbridgeToFunction,
      snsToQueue,
      snsToFunction,
      queueToFunction,
      dynamodbToFunction,
      queueToDlq,
      bucketToFunction,
      functionToDynamodb,
      functionToCache,
      cacheToSecret,
      functionToInfra,
    },
    infraLane: buildInfraLane(connInfraNodes),
    infraRoute: { x: 0, y: 0 },
    traces: {
      ticker: [],
      edgeActivity: new Map(),
      cacheActivity,
    },
  };
  refreshEdges(model);
  return model;
}

export function withTopologyTraceActivity(
  model: TopologyGraphModel,
  recentTraces: RequestTrace[],
  now = Date.now(),
): TopologyGraphModel {
  const traceEdgeActivity = buildTraceEdgeActivity(recentTraces, now);

  const seenBucketFnEdges = new Set(
    model.edges.bucketToFunction.map((edge) => `${edge.from.id}→${edge.to.id}`),
  );
  const inferredBucketFnEdges: Array<{ from: ConnectionNode; to: ConnectionNode }> = [];

  for (const trace of recentTraces) {
    const s3Span = trace.spans.find((span) => span.kind === "s3");
    const lambdaSpan = trace.spans.find((span) => span.kind === "lambda");
    if (!s3Span || !lambdaSpan) continue;

    const bucketName = bucketNameFromTraceSpanName(s3Span.name);
    const from = model.nodeByGraphKey.get(graphNodeKey({ kind: "bucket", id: bucketName }));
    const to = model.nodeByGraphKey.get(graphNodeKey({ kind: "function", id: lambdaSpan.name }));
    if (!from || !to) continue;

    const key = `${from.id}→${to.id}`;
    if (seenBucketFnEdges.has(key)) continue;
    seenBucketFnEdges.add(key);
    inferredBucketFnEdges.push({ from, to });
  }

  const bucketToFunction = withLanes(
    [
      ...model.edges.bucketToFunction.map((edge) => ({
        from: edge.from,
        to: edge.to,
      })),
      ...inferredBucketFnEdges,
    ],
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    id: `${edge.from.id}→${edge.to.id}`,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const apigwToQueue = model.edges.apigwToQueue.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`gw::${edge.from.id}→${edge.to.id}`),
  }));
  const apigwToFunction = model.edges.apigwToFunction.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`gw::${edge.from.id}→${edge.to.id}`),
  }));
  const eventbridgeToFunction = model.edges.eventbridgeToFunction.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`eventbridge::${edge.from.id}→${edge.to.id}`),
  }));
  const snsToQueue = model.edges.snsToQueue.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`sns::${edge.from.id}→${edge.to.id}`),
  }));
  const snsToFunction = model.edges.snsToFunction.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`sns::${edge.from.id}→${edge.to.id}`),
  }));
  const queueToFunction = model.edges.queueToFunction.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`queue::${edge.from.id}→${edge.to.id}`),
  }));
  const dynamodbToFunction = model.edges.dynamodbToFunction.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`dynamodb::${edge.from.id}→${edge.to.id}`),
  }));
  const queueToDlq = model.edges.queueToDlq.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`dlq::${edge.from.id}→${edge.to.id}`),
  }));
  const functionToDynamodb = model.edges.functionToDynamodb.map((edge) => ({
    ...edge,
    activity: fnActivity(traceEdgeActivity, edge.from.id),
  }));
  const functionToInfra = model.edges.functionToInfra.map((edge) => ({
    ...edge,
    activity: fnActivity(traceEdgeActivity, edge.from.id),
  }));
  const functionToCache = model.edges.functionToCache.map((edge) => ({
    ...edge,
    activity: fnActivity(traceEdgeActivity, edge.from.id),
  }));

  const cacheActivity = aggregateActivity([
    ...functionToCache.flatMap((edge) => (edge.activity ? [edge.activity] : [])),
    ...(traceEdgeActivity.get("cache::global") ? [traceEdgeActivity.get("cache::global")!] : []),
  ]);

  const cacheToSecret = model.edges.cacheToSecret.map((edge) => ({
    ...edge,
    activity: traceEdgeActivity.get(`cache::secret:${edge.to.id}`),
  }));

  const allEdges = [
    ...apigwToQueue,
    ...apigwToFunction,
    ...eventbridgeToFunction,
    ...snsToQueue,
    ...snsToFunction,
    ...queueToFunction,
    ...dynamodbToFunction,
    ...queueToDlq,
    ...bucketToFunction,
    ...functionToDynamodb,
    ...functionToCache,
    ...cacheToSecret,
    ...functionToInfra,
  ];

  const forwardAdjacency = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();
  const reverseAdjacency = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();
  for (const edge of allEdges) {
    const forwardEdges = forwardAdjacency.get(edge.from.id) ?? [];
    forwardEdges.push(edge);
    forwardAdjacency.set(edge.from.id, forwardEdges);

    const reverseEdges = reverseAdjacency.get(edge.to.id) ?? [];
    reverseEdges.push(edge);
    reverseAdjacency.set(edge.to.id, reverseEdges);
  }

  return {
    ...model,
    allEdges,
    forwardAdjacency,
    reverseAdjacency,
    edges: {
      ...model.edges,
      apigwToQueue,
      apigwToFunction,
      eventbridgeToFunction,
      snsToQueue,
      snsToFunction,
      queueToFunction,
      dynamodbToFunction,
      queueToDlq,
      bucketToFunction,
      functionToDynamodb,
      functionToCache,
      cacheToSecret,
      functionToInfra,
    },
    traces: {
      ticker: recentTraces.slice(0, 8),
      edgeActivity: traceEdgeActivity,
      cacheActivity,
    },
  };
}

export function applyPreviewNodePositions(
  model: TopologyGraphModel,
  positions: Record<string, InfraNodePosition>,
): void {
  if (Object.keys(positions).length === 0) return;

  for (const [key, position] of Object.entries(positions)) {
    const node = model.nodeByGraphKey.get(key);
    if (!node) continue;
    node.x = position.x;
    node.y = position.y;
  }

  refreshEdges(model);
}

export function activityStroke(activity: EdgeActivity | undefined, defaultStroke: string): string {
  if (!activity) return defaultStroke;
  return activity.hasError ? "destructive" : "primary";
}

export function activityOpacity(activity: EdgeActivity | undefined, base: number): number {
  if (!activity) return base;
  return Math.min(0.95, base + activity.count * 0.12);
}

export function activityWidth(activity: EdgeActivity | undefined, base: number): number {
  if (!activity) return base;
  return base + Math.min(1.2, activity.count * 0.2);
}

export function traceStatusTone(status: number): "destructive" | "warning" | "primary" {
  if (status >= 500) return "destructive";
  if (status >= 400) return "warning";
  return "primary";
}

export function infraKindTone(kind: string): "db" | "cache" | "service" | "primary" {
  switch (kind?.toLowerCase()) {
    case "postgres":
    case "postgresql":
    case "mysql":
      return "db";
    case "redis":
      return "cache";
    case "http":
      return "service";
    default:
      return "primary";
  }
}

export function selectedTraceNodes(
  model: TopologyGraphModel,
  trace: RequestTrace | null,
): ConnectionNode[] {
  if (!trace) return [];

  const lambdaSpan = trace.spans.find((span) => span.kind === "lambda");
  const eventBridgeSpan = trace.spans.find((span) => span.kind === "eventbridge");
  const topicSpan = trace.spans.find((span) => span.kind === "topic");
  const queueSpan = trace.spans.find((span) => span.kind === "queue");
  const dynamodbSpan = trace.spans.find((span) => span.kind === "dynamodb" || span.kind === "ddb");
  const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
  const cacheSpan = trace.spans.find(
    (span) => span.kind === "cache_extension" || span.kind === "cache-extension",
  );
  const secretsSpan = trace.spans.find((span) => span.kind === "secrets" || span.kind === "secret");

  const matchedGateway = trace.gatewayId
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "gateway", id: trace.gatewayId }))
    : undefined;
  const matchedFunction = lambdaSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "function", id: lambdaSpan.name }))
    : undefined;
  const matchedEventBridge = eventBridgeSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "eventbridge", id: eventBridgeSpan.name }))
    : undefined;
  const matchedTopic = topicSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "topic", id: topicSpan.name }))
    : undefined;
  const matchedQueue = queueSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "queue", id: queueSpan.name }))
    : undefined;
  const matchedDynamodb = dynamodbSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "dynamodb", id: dynamodbSpan.name }))
    : undefined;
  const matchedDlq = dlqSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "queue", id: dlqSpan.name }))
    : undefined;
  const matchedCache =
    cacheSpan || secretsSpan ? (model.nodes.cacheExtension ?? undefined) : undefined;
  const matchedSecret = secretsSpan
    ? model.nodeByGraphKey.get(graphNodeKey({ kind: "secret", id: secretsSpan.name }))
    : undefined;

  return [
    matchedGateway,
    matchedEventBridge,
    matchedFunction,
    matchedTopic,
    matchedQueue,
    matchedDynamodb,
    matchedDlq,
    matchedCache,
    matchedSecret,
  ].filter((node): node is ConnectionNode => !!node);
}

export function nodeBounds(node: ConnectionNode): {
  left: number;
  right: number;
  top: number;
  bottom: number;
} {
  const halfWidth = nodeHalfWidth(node);
  const halfHeight = nodeHalfHeight(node);
  return {
    left: node.x - halfWidth,
    right: node.x + halfWidth,
    top: node.y - halfHeight,
    bottom: node.y + halfHeight,
  };
}

/** Height of each row in an opened card's details, and the room around them. */
export const DETAIL_ROW = 22;
const DETAIL_PAD = 16;

/** Whether a card is open with its details showing. */
export function nodeOpen(node: ConnectionNode): boolean {
  return !!node.expanded && (node.size ?? "small") === "small" && !!node.details?.length;
}

/** The middle of a card's header: its centre, or the top strip of an open card. */
export function nodeHeaderY(node: ConnectionNode): number {
  if (!nodeOpen(node)) return node.y;
  return node.y - nodeHalfHeight(node) + compactHalfHeight(node);
}

/** How far in from a card's right edge its open/shut toggle sits. */
export const TOGGLE_INSET = 20;

/** Whether a canvas point is on a card's open/shut toggle. */
export function onNodeToggle(node: ConnectionNode, x: number, y: number): boolean {
  if (!node.details?.length || (node.size ?? "small") !== "small") return false;
  const cx = nodeBounds(node).right - TOGGLE_INSET;
  return Math.abs(x - cx) <= 16 && Math.abs(y - nodeHeaderY(node)) <= 18;
}

function compactHalfHeight(node: ConnectionNode): number {
  return node.kind === "extension"
    ? CONNECTION_CANVAS.cacheHalfHeight
    : CONNECTION_CANVAS.nodeHalfHeight;
}

function applyNodeOverride(node: ConnectionNode, override: NodeOverride | undefined): void {
  node.expanded = !!override?.expanded;
  if (override?.inputSide) node.inputSide = override.inputSide;
  if (override?.outputSide) node.outputSide = override.outputSide;
  node.size = resolveTopologyNodeSize(node.kind, override?.size ?? node.size);
  node.view = resolveTopologyNodeView(node.kind, override?.view, node.size ?? "small");
}

export function findNodeAt(model: TopologyGraphModel, x: number, y: number): ConnectionNode | null {
  for (let i = model.hitTestNodes.length - 1; i >= 0; i -= 1) {
    const node = model.hitTestNodes[i];
    const bounds = nodeBounds(node);
    if (x >= bounds.left && x <= bounds.right && y >= bounds.top && y <= bounds.bottom) {
      return node;
    }
  }

  return null;
}

export function computeViewportTransform(
  viewportWidth: number,
  viewportHeight: number,
  options: { expanded?: boolean; canvasWidth?: number; canvasHeight?: number } = {},
): ViewportTransform {
  const canvasW = options.canvasWidth ?? CONNECTION_CANVAS.width;
  const canvasH = options.canvasHeight ?? CONNECTION_CANVAS.height;
  const safeWidth = Math.max(1, viewportWidth);
  const safeHeight = Math.max(1, viewportHeight);
  const paddingX = safeWidth < 900 ? 10 : 16;
  const paddingY = safeHeight < 640 ? 10 : 16;
  const fitScale = Math.min(
    safeWidth / (canvasW + paddingX * 2),
    safeHeight / (canvasH + paddingY * 2),
  );
  const referenceFitScale = Math.min(
    safeWidth / (TOPOLOGY_VIEWPORT_REFERENCE.width + paddingX * 2),
    safeHeight / (TOPOLOGY_VIEWPORT_REFERENCE.height + paddingY * 2),
  );
  // In place (Home, Overview) the whole graph is in view, down to where
  // labels get too small to read. Exploring opens close to full size, so
  // names read like any other text; the minimap shows the rest.
  const scale = options.expanded
    ? Math.min(Math.max(fitScale, 0.85), 1.15)
    : Math.min(Math.max(fitScale, referenceFitScale * 0.55), 1.25);

  // Too wide to see whole: start where requests come in, at the left.
  const wider = canvasW * scale > safeWidth;
  return {
    scale,
    offsetX: options.expanded && wider ? 0 : (safeWidth - canvasW * scale) / 2,
    offsetY: (safeHeight - canvasH * scale) / 2,
  };
}

export function clampViewportTransform(
  viewportWidth: number,
  viewportHeight: number,
  transform: ViewportTransform,
  options: { canvasWidth?: number; canvasHeight?: number } = {},
): ViewportTransform {
  const canvasWidth = options.canvasWidth ?? CONNECTION_CANVAS.width;
  const canvasHeight = options.canvasHeight ?? CONNECTION_CANVAS.height;
  const safeWidth = Math.max(1, viewportWidth);
  const safeHeight = Math.max(1, viewportHeight);
  const contentWidth = canvasWidth * transform.scale;
  const contentHeight = canvasHeight * transform.scale;
  // Room to drag past the edges, whether or not it all fits: the graph can
  // always be moved about, never lost off screen.
  const overscrollX = Math.min(420, safeWidth * 0.35);
  const overscrollY = Math.min(320, safeHeight * 0.28);

  return {
    scale: transform.scale,
    offsetX: clampOffsetAxis(safeWidth, contentWidth, transform.offsetX, overscrollX),
    offsetY: clampOffsetAxis(safeHeight, contentHeight, transform.offsetY, overscrollY),
  };
}

export function viewportToCanvasPoint(
  x: number,
  y: number,
  transform: ViewportTransform,
): { x: number; y: number } {
  return {
    x: (x - transform.offsetX) / transform.scale,
    y: (y - transform.offsetY) / transform.scale,
  };
}

export function clampInfraNodePosition(
  x: number,
  y: number,
  canvasH: number = CONNECTION_CANVAS.height,
  canvasW: number = CONNECTION_CANVAS.width,
): InfraNodePosition {
  return {
    x: clamp(
      x,
      CONNECTION_CANVAS.infraHalfWidth + 24,
      canvasW - CONNECTION_CANVAS.infraHalfWidth - 24,
    ),
    y: clamp(y, 120, canvasH - CONNECTION_CANVAS.nodeHalfHeight - 24),
  };
}

export function resolveSnappedNodePosition(
  model: TopologyGraphModel,
  nodeKey: { id: string; kind: ConnectionNode["kind"] },
  x: number,
  y: number,
): InfraNodePosition {
  const { width: canvasW, height: canvasH } = model.canvasSize;
  const node = findGraphNode(model, nodeKey);
  if (!node) return clampInfraNodePosition(x, y, canvasH, canvasW);

  return resolveNodeSpacing(
    node,
    { x, y },
    model.allNodes,
    TOPOLOGY_MIN_NODE_GAP,
    canvasH,
    canvasW,
  );
}

export function hoverFocusState(
  model: TopologyGraphModel,
  hoveredNodeId: string | null,
): HoverFocusState {
  if (!hoveredNodeId) {
    return {
      active: false,
      nodeIds: new Set(),
      edgeIds: new Set(),
    };
  }

  const nodeIds = new Set<string>([hoveredNodeId]);
  const edgeIds = new Set<string>();

  walkHoverFlow(hoveredNodeId, model.forwardAdjacency, true, nodeIds, edgeIds);
  walkHoverFlow(hoveredNodeId, model.reverseAdjacency, false, nodeIds, edgeIds);

  return {
    active: true,
    nodeIds,
    edgeIds,
  };
}

function walkHoverFlow(
  startNodeId: string,
  adjacency: Map<string, Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>>,
  forward: boolean,
  nodeIds: Set<string>,
  edgeIds: Set<string>,
) {
  const queue = [startNodeId];
  const visited = new Set<string>([startNodeId]);

  while (queue.length > 0) {
    const currentNodeId = queue.shift()!;
    const edges = adjacency.get(currentNodeId) ?? [];

    for (const edge of edges) {
      edgeIds.add(edge.id);
      nodeIds.add(edge.from.id);
      nodeIds.add(edge.to.id);

      const nextNodeId = forward ? edge.to.id : edge.from.id;
      if (visited.has(nextNodeId)) continue;
      visited.add(nextNodeId);
      queue.push(nextNodeId);
    }
  }
}

function nodeHalfWidth(node: ConnectionNode): number {
  const base =
    node.kind === "extension"
      ? CONNECTION_CANVAS.cacheHalfWidth
      : node.kind === "infra"
        ? CONNECTION_CANVAS.infraHalfWidth
        : CONNECTION_CANVAS.nodeHalfWidth;
  if (node.size === "large") return base * 2;
  return base; // small and medium both use base width
}

function nodeHalfHeight(node: ConnectionNode): number {
  const baseHW =
    node.kind === "extension"
      ? CONNECTION_CANVAS.cacheHalfWidth
      : node.kind === "infra"
        ? CONNECTION_CANVAS.infraHalfWidth
        : CONNECTION_CANVAS.nodeHalfWidth;
  const baseHH =
    node.kind === "extension"
      ? CONNECTION_CANVAS.cacheHalfHeight
      : CONNECTION_CANVAS.nodeHalfHeight;
  switch (node.size) {
    case "medium":
      return baseHW;
    case "large":
      return baseHW * 2;
    default:
      return nodeOpen(node)
        ? baseHH + (node.details!.length * DETAIL_ROW + DETAIL_PAD) / 2
        : baseHH;
  }
}

function resolveNodeSpacing(
  node: ConnectionNode,
  desiredPosition: InfraNodePosition,
  nodes: ConnectionNode[],
  minGap = TOPOLOGY_MIN_NODE_GAP,
  canvasH: number = CONNECTION_CANVAS.height,
  canvasW: number = CONNECTION_CANVAS.width,
): InfraNodePosition {
  let candidate = clampNodePosition(node, desiredPosition.x, desiredPosition.y, canvasH, canvasW);
  const others = nodes.filter((other) => other !== node);
  if (others.length === 0) return candidate;

  for (let iteration = 0; iteration < others.length * 3; iteration += 1) {
    let moved = false;

    for (const other of others) {
      const requiredX = nodeHalfWidth(node) + nodeHalfWidth(other) + minGap;
      const requiredY = nodeHalfHeight(node) + nodeHalfHeight(other) + minGap;
      const deltaX = candidate.x - other.x;
      const deltaY = candidate.y - other.y;
      const overlapX = requiredX - Math.abs(deltaX);
      const overlapY = requiredY - Math.abs(deltaY);

      if (overlapX <= 0 || overlapY <= 0) continue;

      if (overlapX < overlapY) {
        const direction = deltaX === 0 ? (candidate.x >= canvasW / 2 ? 1 : -1) : Math.sign(deltaX);
        candidate = clampNodePosition(
          node,
          candidate.x + direction * (overlapX + 1),
          candidate.y,
          canvasH,
          canvasW,
        );
      } else {
        const direction = deltaY === 0 ? (candidate.y >= other.y ? 1 : -1) : Math.sign(deltaY);
        candidate = clampNodePosition(
          node,
          candidate.x,
          candidate.y + direction * (overlapY + 1),
          canvasH,
          canvasW,
        );
      }

      moved = true;
    }

    if (!moved) break;
  }

  return candidate;
}

function lambdaNameFromArn(arn: string): string {
  const parts = arn.split(":");
  return parts[parts.length - 1];
}

function bucketNameFromTraceSpanName(name: string): string {
  const slashIndex = name.indexOf("/");
  return slashIndex === -1 ? name : name.slice(0, slashIndex);
}

function trimLabel(label: string, max = 14): string {
  return label.length <= max ? label : `${label.slice(0, max - 1)}…`;
}

function infraNodeId(probe: InfraProbe): string {
  return `${probe.kind}-${probe.host}-${probe.port}`;
}

function buildInfraNodes(infra: InfraProbe[], infraOrderIds: string[]): ConnectionNode[] {
  const byId = new Map(infra.map((probe) => [infraNodeId(probe), probe]));
  // The order the user gave them first, then the rest by id; the layout
  // reorders by what they connect to and keeps this for ties.
  const orderedIds = [
    ...infraOrderIds.filter((id) => byId.has(id)),
    ...[...byId.keys()].filter((id) => !infraOrderIds.includes(id)).sort(),
  ];
  return orderedIds.map((id): ConnectionNode => {
    const probe = byId.get(id)!;
    return {
      id,
      x: 0,
      y: 0,
      label: trimLabel(probe.name, 13),
      fullLabel: probe.name,
      sub:
        probe.version && probe.version.length > 0
          ? probe.version
          : probe.latencyMs > 0
            ? `${probe.latencyMs.toFixed(0)}ms`
            : "",
      kind: "infra",
      status: probe.status,
    };
  });
}

function clampOffsetAxis(
  viewportSize: number,
  contentSize: number,
  offset: number,
  overscroll: number,
): number {
  const min = Math.min(0, viewportSize - contentSize) - overscroll;
  const max = Math.max(0, viewportSize - contentSize) + overscroll;
  return clamp(offset, min, max);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function clampNodePosition(
  node: ConnectionNode,
  x: number,
  y: number,
  canvasH: number = CONNECTION_CANVAS.height,
  canvasW: number = CONNECTION_CANVAS.width,
): InfraNodePosition {
  return {
    x: clamp(x, nodeHalfWidth(node) + 24, canvasW - nodeHalfWidth(node) - 24),
    y: clamp(y, nodeHalfHeight(node) + 24, canvasH - nodeHalfHeight(node) - 24),
  };
}

function graphNodeKey(node: { id: string; kind: ConnectionNode["kind"] }): string {
  return `${node.kind}:${node.id}`;
}

function getPersistedNodePosition(
  allNodePositions: Record<string, InfraNodePosition>,
  node: { id: string; kind: ConnectionNode["kind"] },
): InfraNodePosition | undefined {
  return allNodePositions[graphNodeKey(node)] ?? allNodePositions[node.id];
}

function findGraphNode(
  model: TopologyGraphModel,
  nodeKey: { id: string; kind: ConnectionNode["kind"] },
): ConnectionNode | null {
  return model.nodeByGraphKey.get(graphNodeKey(nodeKey)) ?? null;
}

// ── Flow layout ──────────────────────────────────────────────────
// Columns in the order requests move: in through gateways, rules and
// bucket events, across topics and queues, into functions, then out to
// the data and services they reach. A stage with nothing in it takes no
// room. Each column stacks one labelled section per kind. A few passes put
// every node near the middle of what it connects to (fewer crossings),
// a dead-letter queue goes under its queue, and a long section wraps into
// side-by-side columns. It depends only on the graph's shape, so the
// same resources land in the same places on every poll.

interface FlowSection {
  id: string;
  label: string;
  nodes: ConnectionNode[];
}

interface FlowStage {
  id: string;
  sections: FlowSection[];
}

const FLOW = {
  pad: 100,
  header: 40,
  nodeGap: 46,
  sectionGap: 88,
  colGap: 170,
  subColGap: 56,
  wrapRows: 9,
  minWidth: 1800,
  minHeight: 1200,
} as const;

function layoutFlow(
  allStages: FlowStage[],
  edges: Array<{ from: ConnectionNode; to: ConnectionNode }>,
  dlqSource: Map<string, string>,
  aspect?: number,
  arrangement: TopologyArrangement = {},
): { width: number; height: number; sections: TopologySection[] } {
  const stages = allStages
    .map((stage) => ({
      ...stage,
      sections: stage.sections.filter((section) => section.nodes.length > 0),
    }))
    .filter((stage) => stage.sections.length > 0);
  for (const stage of stages) {
    for (const section of stage.sections) {
      section.nodes = [...section.nodes].sort((a, b) => a.id.localeCompare(b.id));
    }
  }

  const key = graphNodeKey;
  const stageOf = new Map<string, number>();
  stages.forEach((stage, i) =>
    stage.sections.forEach((section) => section.nodes.forEach((node) => stageOf.set(key(node), i))),
  );
  const neighbours = new Map<string, string[]>();
  for (const edge of edges) {
    const a = key(edge.from);
    const b = key(edge.to);
    if (!stageOf.has(a) || !stageOf.has(b) || stageOf.get(a) === stageOf.get(b)) continue;
    neighbours.set(a, [...(neighbours.get(a) ?? []), b]);
    neighbours.set(b, [...(neighbours.get(b) ?? []), a]);
  }

  // Where each node sits in its column, 0 at the top to 1 at the foot.
  const rank = new Map<string, number>();
  const rankStage = (stage: FlowStage) => {
    const nodes = stage.sections.flatMap((section) => section.nodes);
    nodes.forEach((node, i) => rank.set(key(node), (i + 0.5) / nodes.length));
  };
  stages.forEach(rankStage);

  const sortStage = (stage: FlowStage) => {
    for (const section of stage.sections) {
      const pull = new Map(
        section.nodes.map((node) => {
          const ranks = (neighbours.get(key(node)) ?? []).map((k) => rank.get(k)!);
          return [
            node,
            ranks.length
              ? ranks.reduce((sum, r) => sum + r, 0) / ranks.length
              : rank.get(key(node))!,
          ];
        }),
      );
      section.nodes.sort((a, b) => pull.get(a)! - pull.get(b)! || a.id.localeCompare(b.id));
      if (section.id === "queue") section.nodes = dlqsUnderSources(section.nodes, dlqSource);
    }
    rankStage(stage);
  };
  for (let pass = 0; pass < 4; pass += 1) {
    (pass % 2 === 0 ? stages : [...stages].reverse()).forEach(sortStage);
  }
  // Cards the user put in order keep it; new ones follow in layout order.
  for (const stage of stages) {
    for (const section of stage.sections) {
      const saved = arrangement.order?.[`${stage.id}:${section.id}`];
      if (!saved?.length) continue;
      const at = new Map(saved.map((k, i) => [k, i]));
      const before = new Map(section.nodes.map((node, i) => [node, i]));
      const slot = (node: ConnectionNode) => at.get(key(node)) ?? saved.length + before.get(node)!;
      section.nodes.sort((a, b) => slot(a) - slot(b));
    }
  }

  // Place: columns left to right, sections stacked down each. With a
  // viewport shape to fill, try a few wrap lengths, and stacking what comes
  // after the functions in bands (services above, cache and data below)
  // rather than all in a row; keep whichever shows the graph largest, then
  // open the gaps up to use the height.
  type Column = { bottom: number; nodes: ConnectionNode[]; sections: TopologySection[] };
  const place = (wrapRows: number, gapK: number, stackTail: boolean) => {
    const nodeGap = FLOW.nodeGap * gapK;
    const sectionGap = FLOW.sectionGap * gapK;
    const layColumn = (stage: FlowStage, x: number, y0: number) => {
      const nodes = stage.sections.flatMap((section) => section.nodes);
      const halfW = Math.max(...nodes.map(nodeHalfWidth));
      const halfH = Math.max(...nodes.map(nodeHalfHeight));
      const subColsOf = (section: FlowSection) => Math.ceil(section.nodes.length / wrapRows);
      const widthOf = (subCols: number) => subCols * halfW * 2 + (subCols - 1) * FLOW.subColGap;
      const colW = Math.max(...stage.sections.map((section) => widthOf(subColsOf(section))));
      const column: Column = { bottom: 0, nodes, sections: [] };
      let y = y0;
      for (const section of stage.sections) {
        const top = y;
        if (section.label) y += FLOW.header;
        const subCols = subColsOf(section);
        const left = x + (colW - widthOf(subCols)) / 2;
        if (subCols > 1) {
          // Each row as tall as its tallest card, so an opened one fits.
          const rows = Math.ceil(section.nodes.length / subCols);
          const rowH = Array.from({ length: rows }, () => 0);
          section.nodes.forEach((node, i) => {
            rowH[i % rows] = Math.max(rowH[i % rows], nodeHalfHeight(node) * 2);
          });
          const rowTop = rowH.map((_, r) => y + rowH.slice(0, r).reduce((a, h) => a + h + nodeGap, 0));
          section.nodes.forEach((node, i) => {
            node.x = left + halfW + Math.floor(i / rows) * (halfW * 2 + FLOW.subColGap);
            node.y = rowTop[i % rows] + nodeHalfHeight(node);
          });
          y += rowH.reduce((a, h) => a + h + nodeGap, 0) - nodeGap;
        } else {
          for (const node of section.nodes) {
            node.x = left + halfW;
            y += nodeHalfHeight(node);
            node.y = y;
            y += nodeHalfHeight(node) + nodeGap;
          }
          y -= nodeGap;
        }
        if (section.label) {
          column.sections.push({
            id: `${stage.id}:${section.id}`,
            label: section.label,
            count: section.nodes.length,
            nodeKeys: section.nodes.map(graphNodeKey),
            offset: { x: 0, y: 0 },
            x: x - 18,
            y: top,
            width: colW + 36,
            height: y - top + 18,
          });
        }
        y += sectionGap;
      }
      column.bottom = y - sectionGap;
      return { colW, column };
    };

    const compute = stages.findIndex((stage) => stage.id === "compute");
    const tail = stackTail && compute >= 0 ? stages.slice(compute + 1) : [];
    const bands = tail.length >= 2 ? [tail.slice(0, 1), tail.slice(1)] : [];
    const row = bands.length ? stages.slice(0, compute + 1) : stages;
    const columns: Column[] = [];
    let x: number = FLOW.pad;
    for (const stage of row) {
      const { colW, column } = layColumn(stage, x, 0);
      columns.push(column);
      x += colW + FLOW.colGap;
    }
    if (bands.length) {
      // One column for centring, its bands one under another.
      const stack: Column = { bottom: 0, nodes: [], sections: [] };
      let right = x;
      let y = 0;
      for (const band of bands) {
        let bx = x;
        let bottom = y;
        for (const stage of band) {
          const { colW, column } = layColumn(stage, bx, y);
          stack.nodes.push(...column.nodes);
          stack.sections.push(...column.sections);
          bottom = Math.max(bottom, column.bottom);
          bx += colW + FLOW.colGap;
        }
        right = Math.max(right, bx);
        y = bottom + sectionGap * 1.5;
      }
      stack.bottom = y - sectionGap * 1.5;
      columns.push(stack);
      x = right;
    }
    const contentW = x - FLOW.colGap - FLOW.pad;
    const contentH = Math.max(0, ...columns.map((column) => column.bottom));
    return { contentW, contentH, columns };
  };

  let wrapRows: number = FLOW.wrapRows;
  let stackTail = false;
  let gapK = 1;
  if (aspect && aspect > 0) {
    // The graph shows at a scale set by whichever side fills the view first.
    // No section runs longer than ten rows: past that it reads as a list.
    const span = (r: number, stack: boolean) => {
      const { contentW, contentH } = place(r, 1, stack);
      return Math.max(contentW + FLOW.pad * 2, (contentH + FLOW.pad * 2) / aspect);
    };
    let best = span(wrapRows, stackTail);
    for (const stack of [false, true]) {
      for (const r of [4, 5, 6, 7, 8, 10]) {
        const next = span(r, stack);
        if (next < best - 1) [best, wrapRows, stackTail] = [next, r, stack];
      }
    }
    // Height grows in step with the gaps, so two trial runs give the stretch.
    const a = place(wrapRows, 1, stackTail);
    const b = place(wrapRows, 2, stackTail);
    const want = Math.max(FLOW.minWidth, a.contentW + FLOW.pad * 2) * aspect - FLOW.pad * 2;
    if (b.contentH > a.contentH && want > a.contentH) {
      gapK = Math.min(2.2, 1 + (want - a.contentH) / (b.contentH - a.contentH));
    }
  }
  const { contentW, contentH, columns } = place(wrapRows, gapK, stackTail);

  const width = Math.max(FLOW.minWidth, contentW + FLOW.pad * 2);
  const height = Math.max(aspect ? width * aspect : FLOW.minHeight, contentH + FLOW.pad * 2);
  // Each column centred on the canvas's middle; the whole centred across it.
  const dx = (width - contentW) / 2 - FLOW.pad;
  const sections: TopologySection[] = [];
  for (const column of columns) {
    const dy = (height - column.bottom) / 2;
    for (const node of column.nodes) {
      node.x += dx;
      node.y += dy;
    }
    for (const section of column.sections) {
      sections.push({ ...section, x: section.x + dx, y: section.y + dy });
    }
  }

  // Sections the user moved go where they put them, unless the layout has
  // since changed so that spot runs into another section or off the canvas.
  const nodeOf = new Map(
    stages.flatMap((stage) => stage.sections.flatMap((s) => s.nodes)).map((n) => [key(n), n]),
  );
  const hits = (a: TopologySection, b: TopologySection) =>
    a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
  for (const section of sections) {
    const offset = arrangement.offsets?.[section.id];
    if (!offset || (!offset.x && !offset.y)) continue;
    const moved = { ...section, x: section.x + offset.x, y: section.y + offset.y };
    const inside =
      moved.x >= 0 && moved.y >= 0 && moved.x + moved.width <= width && moved.y + moved.height <= height;
    if (!inside || sections.some((other) => other !== section && hits(moved, other))) continue;
    Object.assign(section, { x: moved.x, y: moved.y, offset: { ...offset } });
    for (const k of section.nodeKeys) {
      const node = nodeOf.get(k);
      if (!node) continue;
      node.x += offset.x;
      node.y += offset.y;
    }
  }
  return { width, height, sections };
}

/** Each dead-letter queue straight after the queue that feeds it. */
function dlqsUnderSources(
  queues: ConnectionNode[],
  dlqSource: Map<string, string>,
): ConnectionNode[] {
  const ids = new Set(queues.map((q) => q.id));
  const tucked = (q: ConnectionNode) =>
    dlqSource.has(q.id) && ids.has(dlqSource.get(q.id)!) && dlqSource.get(q.id) !== q.id;
  const out: ConnectionNode[] = [];
  const place = (q: ConnectionNode, depth: number) => {
    out.push(q);
    if (depth > 4) return;
    for (const dlq of queues.filter((d) => tucked(d) && dlqSource.get(d.id) === q.id))
      place(dlq, depth + 1);
  };
  for (const q of queues) if (!tucked(q)) place(q, 0);
  // A loop of queues feeding each other has no head; keep them anyway.
  for (const q of queues) if (!out.includes(q)) out.push(q);
  return out;
}

/**
 * Lanes and paths for every edge from where its nodes are now. Edges
 * leaving the same node fan out a little; the arrays are sorted in place,
 * so adjacency maps that hold the same objects stay valid.
 */
/**
 * The frame round a section: wherever its nodes are, the header above the
 * topmost. `moved` stands in positions for nodes mid-drag.
 */
export function sectionFrame(
  model: TopologyGraphModel,
  section: TopologySection,
  moved: Record<string, InfraNodePosition> = {},
): { x: number; y: number; width: number; height: number } | null {
  let left = Infinity;
  let right = -Infinity;
  let top = Infinity;
  let bottom = -Infinity;
  for (const key of section.nodeKeys) {
    const node = model.nodeByGraphKey.get(key);
    if (!node) continue;
    const at = moved[key];
    const b = nodeBounds(at ? { ...node, x: at.x, y: at.y } : node);
    left = Math.min(left, b.left);
    right = Math.max(right, b.right);
    top = Math.min(top, b.top);
    bottom = Math.max(bottom, b.bottom);
  }
  if (left === Infinity) return null;
  return {
    x: left - 18,
    y: top - FLOW.header,
    width: right - left + 36,
    height: bottom - top + FLOW.header + 18,
  };
}

/** Whether moving these nodes would push one section's frame into another's. */
export function framesCollide(
  model: TopologyGraphModel,
  moved: Record<string, InfraNodePosition>,
): boolean {
  const frames = model.sections.map((section) => ({
    touched: section.nodeKeys.some((key) => key in moved),
    frame: sectionFrame(model, section, moved),
  }));
  for (let i = 0; i < frames.length; i++) {
    const a = frames[i];
    if (!a.touched || !a.frame) continue;
    for (let j = 0; j < frames.length; j++) {
      const b = frames[j];
      if (i === j || !b.frame || (b.touched && j < i)) continue;
      if (
        a.frame.x < b.frame.x + b.frame.width &&
        b.frame.x < a.frame.x + a.frame.width &&
        a.frame.y < b.frame.y + b.frame.height &&
        b.frame.y < a.frame.y + a.frame.height
      )
        return true;
    }
  }
  // A node outside every section (the secrets cache) stays out of them.
  const sectioned = new Set(model.sections.flatMap((section) => section.nodeKeys));
  for (const [key, at] of Object.entries(moved)) {
    const node = model.nodeByGraphKey.get(key);
    if (!node || sectioned.has(key)) continue;
    const box = nodeBounds({ ...node, x: at.x, y: at.y });
    for (const { frame } of frames) {
      if (
        frame &&
        box.left < frame.x + frame.width &&
        frame.x < box.right &&
        box.top < frame.y + frame.height &&
        frame.y < box.bottom
      )
        return true;
    }
  }
  return false;
}

/** The section whose header strip is under a canvas point, for dragging it whole. */
export function findSectionHeaderAt(
  model: TopologyGraphModel,
  x: number,
  y: number,
): TopologySection | null {
  for (const section of model.sections) {
    if (
      x >= section.x &&
      x <= section.x + section.width &&
      y >= section.y &&
      y <= section.y + FLOW.header
    )
      return section;
  }
  return null;
}

function refreshEdges(model: TopologyGraphModel): void {
  for (const section of model.sections) {
    const frame = sectionFrame(model, section);
    if (frame) Object.assign(section, frame);
  }
  const { queueToDlq, ...laned } = model.edges;
  for (const group of Object.values(laned) as LaneEdge[][]) {
    group.sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y || a.id.localeCompare(b.id));
    const bySource = new Map<string, LaneEdge[]>();
    for (const edge of group)
      bySource.set(edge.from.id, [...(bySource.get(edge.from.id) ?? []), edge]);
    for (const siblings of bySource.values()) {
      siblings.forEach((edge, lane) => {
        edge.lane = lane;
        edge.laneCount = siblings.length;
        edge.path = portConnectPath(edge.from, edge.to, lane, siblings.length);
      });
    }
  }
  for (const edge of queueToDlq) edge.path = dlqArcPath(edge.from, edge.to);
  // Lines out to services run square, like a tube map, each function in a
  // lane of its own so they sit side by side rather than on top of each other.
  const sources = [...new Set(model.edges.functionToInfra.map((edge) => edge.from))].sort(
    (a, b) => a.y - b.y || a.id.localeCompare(b.id),
  );
  // Card boxes once for every route, not once per route per candidate.
  const boxes = model.edges.functionToInfra.length ? model.allNodes.map(nodeBounds) : [];
  for (const edge of model.edges.functionToInfra) {
    edge.lane = sources.indexOf(edge.from);
    edge.laneCount = sources.length;
    edge.path = metroPath(edge.from, edge.to, edge.lane, boxes, model.sections);
  }
  const cache = model.nodes.cacheExtension;
  model.cacheBus = cache
    ? {
        in: busPath(model.nodes.functions, cache, "right"),
        out: busPath(model.nodes.secrets, cache, "left"),
      }
    : null;
  model.infraLane = buildInfraLane(model.nodes.infra);
  model.infraRoute = {
    x: model.infraLane.x + model.infraLane.width / 2,
    y: model.infraLane.y - 26,
  };
}

/**
 * A bracket down one side of a group of nodes and a single line from it to
 * the hub: on their right into its input (they feed it), or on their left
 * from its output (it feeds them). Every node in the group links to the
 * hub, so the bracket stands for all of them without a line each.
 */
function busPath(
  nodes: ConnectionNode[],
  hub: ConnectionNode,
  side: "left" | "right",
): string | null {
  if (nodes.length === 0) return null;
  const top = Math.min(...nodes.map((node) => nodeBounds(node).top)) + 12;
  const bottom = Math.max(...nodes.map((node) => nodeBounds(node).bottom)) - 12;
  const tick = side === "right" ? -12 : 12;
  const bx =
    side === "right"
      ? Math.max(...nodes.map((node) => nodeBounds(node).right)) + 28
      : Math.min(...nodes.map((node) => nodeBounds(node).left)) - 28;
  const port = portPos(hub, side === "right" ? "input" : "output");
  const mid = clamp(port.y, top, bottom);
  const reach = Math.max(40, Math.abs(port.x - bx) * 0.45);
  const dir = side === "right" ? 1 : -1;
  return (
    `M ${bx + tick} ${top} L ${bx} ${top} L ${bx} ${bottom} L ${bx + tick} ${bottom} ` +
    `M ${bx} ${mid} C ${bx + dir * reach} ${mid}, ${port.x - dir * reach} ${port.y}, ${port.x} ${port.y}`
  );
}

function buildInfraLane(infraNodes: ConnectionNode[]): {
  x: number;
  y: number;
  width: number;
  height: number;
} {
  const padX = 20;
  const padTop = 24;
  const padBottom = 24;

  if (infraNodes.length === 0) {
    return {
      x: 260,
      y: CONNECTION_CANVAS.height - 320,
      width: CONNECTION_CANVAS.width - 520,
      height: 92,
    };
  }

  const left = Math.min(...infraNodes.map((node) => node.x - CONNECTION_CANVAS.infraHalfWidth));
  const right = Math.max(...infraNodes.map((node) => node.x + CONNECTION_CANVAS.infraHalfWidth));
  const top = Math.min(...infraNodes.map((node) => node.y - CONNECTION_CANVAS.nodeHalfHeight));
  const bottom = Math.max(...infraNodes.map((node) => node.y + CONNECTION_CANVAS.nodeHalfHeight));

  return {
    x: left - padX,
    y: top - padTop,
    width: right - left + padX * 2,
    height: bottom - top + padTop + padBottom,
  };
}

/**
 * Returns the canvas coordinates of a node's input or output connector port.
 * When both ports share the same side they are offset ±14px from centre so
 * they sit as two distinct dots rather than overlapping.
 */
export function portPos(node: ConnectionNode, role: "input" | "output"): { x: number; y: number } {
  const bounds = nodeBounds(node);
  const inputSide: NodeSide = node.inputSide ?? "left";
  const outputSide: NodeSide = node.outputSide ?? "right";
  const side = role === "input" ? inputSide : outputSide;
  const shared = inputSide === outputSide;

  // When both ports share a side the input sits "before" the output.
  // On horizontal sides that means shifted left/right; on vertical sides up/down.
  const offset = shared ? 14 : 0;
  const sign = role === "input" ? -1 : 1;

  switch (side) {
    case "left":
      return { x: bounds.left, y: nodeHeaderY(node) + (shared ? sign * offset : 0) };
    case "right":
      return { x: bounds.right, y: nodeHeaderY(node) + (shared ? sign * offset : 0) };
    case "top":
      return { x: node.x + (shared ? sign * offset : 0), y: bounds.top };
    case "bottom":
      return { x: node.x + (shared ? sign * offset : 0), y: bounds.bottom };
  }
}

/** Unit vector pointing outward from each side. */
function sideDir(side: NodeSide): { x: number; y: number } {
  switch (side) {
    case "right":
      return { x: 1, y: 0 };
    case "left":
      return { x: -1, y: 0 };
    case "top":
      return { x: 0, y: -1 };
    case "bottom":
      return { x: 0, y: 1 };
  }
}

/**
 * Generates an SVG cubic bezier connecting the output port of `from` to the
 * input port of `to`, respecting each node's configured port sides.
 * Lanes offset parallel edges perpendicular to the primary flow direction.
 */
/** Lanes in a bundle of square lines sit this far apart. */
const METRO_LANE = 9;

/**
 * A square route from one node's output port to another's input port, on
 * whichever sides they sit, keeping to the gaps between cards. It leaves
 * the source and arrives at the target square to their ports, and between
 * those tries the simple L-shapes, then routes along the gap beside the
 * target's row or column, and keeps whichever crosses fewest cards (then
 * section labels, then the shortest). Each source has its own lane so
 * parallel lines sit side by side.
 */
function metroPath(
  from: ConnectionNode,
  to: ConnectionNode,
  lane: number,
  obstacles: Array<ReturnType<typeof nodeBounds>>,
  sections: TopologySection[],
): string {
  type Pt = { x: number; y: number };
  const start = portPos(from, "output");
  const end = portPos(to, "input");
  const outDir = sideDir(from.outputSide ?? "right");
  const inDir = sideDir(to.inputSide ?? "left");
  const offset = lane * METRO_LANE;
  // Leaving rightwards, clear the secrets-cache bracket 28 out first.
  const leave = (from.outputSide ?? "right") === "right" ? 64 : 24;
  const p: Pt = { x: start.x + outDir.x * (leave + offset), y: start.y + outDir.y * (leave + offset) };
  const q: Pt = { x: end.x + inDir.x * (22 + offset), y: end.y + inDir.y * (22 + offset) };
  const box = nodeBounds(to);

  const segmentCost = (a: Pt, b: Pt) => {
    let cards = 0;
    for (const n of obstacles) {
      const hit =
        Math.max(a.x, b.x) > n.left - 4 &&
        Math.min(a.x, b.x) < n.right + 4 &&
        Math.max(a.y, b.y) > n.top - 4 &&
        Math.min(a.y, b.y) < n.bottom + 4;
      if (hit) cards += 1;
    }
    let labels = 0;
    for (const section of sections) {
      const labelBottom = section.y + FLOW.header - 8;
      const hit =
        Math.max(a.x, b.x) > section.x &&
        Math.min(a.x, b.x) < section.x + section.width &&
        Math.max(a.y, b.y) > section.y &&
        Math.min(a.y, b.y) < labelBottom;
      if (hit) labels += 1;
    }
    return { cards, labels, length: Math.abs(a.x - b.x) + Math.abs(a.y - b.y) };
  };
  const routeCost = (middle: Pt[]) => {
    const pts = [p, ...middle, q];
    let cards = 0;
    let labels = 0;
    let length = 0;
    for (let i = 1; i < pts.length; i++) {
      const c = segmentCost(pts[i - 1], pts[i]);
      cards += c.cards;
      labels += c.labels;
      length += c.length;
    }
    return cards * 1e6 + labels * 1e4 + length + middle.length * 40;
  };

  const gapsY = [box.top - 18 - offset, box.bottom + 18 + offset];
  const gapsX = [box.left - 22 - offset, box.right + 22 + offset];
  const candidates: Pt[][] = [
    [{ x: p.x, y: q.y }],
    [{ x: q.x, y: p.y }],
    ...gapsY.map((gy) => [{ x: p.x, y: gy }, { x: q.x, y: gy }]),
    ...gapsX.map((gx) => [{ x: gx, y: p.y }, { x: gx, y: q.y }]),
    ...gapsY.flatMap((gy) =>
      gapsX.map((gx) => [{ x: p.x, y: gy }, { x: gx, y: gy }, { x: gx, y: q.y }]),
    ),
  ];
  let best = candidates[0];
  let bestCost = Infinity;
  for (const middle of candidates) {
    const cost = routeCost(middle);
    if (cost < bestCost) [best, bestCost] = [middle, cost];
  }
  return roundedPolyline([start, p, ...best, q, end], 12);
}

/** An SVG path along the points with each corner rounded to `radius`. */
function roundedPolyline(points: Array<{ x: number; y: number }>, radius: number): string {
  const pts = points.filter(
    (p, i) => i === 0 || Math.abs(p.x - points[i - 1].x) > 0.5 || Math.abs(p.y - points[i - 1].y) > 0.5,
  );
  let d = `M ${pts[0].x} ${pts[0].y}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const [a, b, c] = [pts[i - 1], pts[i], pts[i + 1]];
    const inLen = Math.hypot(b.x - a.x, b.y - a.y);
    const outLen = Math.hypot(c.x - b.x, c.y - b.y);
    const r = Math.min(radius, inLen / 2, outLen / 2);
    const p1 = { x: b.x - ((b.x - a.x) / inLen) * r, y: b.y - ((b.y - a.y) / inLen) * r };
    const p2 = { x: b.x + ((c.x - b.x) / outLen) * r, y: b.y + ((c.y - b.y) / outLen) * r };
    d += ` L ${p1.x} ${p1.y} Q ${b.x} ${b.y}, ${p2.x} ${p2.y}`;
  }
  const last = pts[pts.length - 1];
  return `${d} L ${last.x} ${last.y}`;
}

function portConnectPath(
  from: ConnectionNode,
  to: ConnectionNode,
  lane: number,
  laneCount: number,
): string {
  const start = portPos(from, "output");
  const end = portPos(to, "input");

  const outSide: NodeSide = from.outputSide ?? "right";
  const inSide: NodeSide = to.inputSide ?? "left";
  const outDir = sideDir(outSide);
  const inDir = sideDir(inSide);

  const dx = end.x - start.x;
  const dy = end.y - start.y;
  const dist = Math.sqrt(dx * dx + dy * dy);
  const tension = Math.min(Math.max(dist * 0.42, 40), 180);

  // Lane offset is applied perpendicular to the output direction
  const laneOff = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 14 : 0;
  // Perpendicular to outDir: rotate 90°
  const perpX = -outDir.y;
  const perpY = outDir.x;

  const c1x = start.x + outDir.x * tension + perpX * laneOff;
  const c1y = start.y + outDir.y * tension + perpY * laneOff;
  const c2x = end.x + inDir.x * tension + perpX * laneOff;
  const c2y = end.y + inDir.y * tension + perpY * laneOff;

  return `M ${start.x} ${start.y} C ${c1x} ${c1y}, ${c2x} ${c2y}, ${end.x} ${end.y}`;
}

// Keep laneAwarePath as a thin wrapper for the DLQ arc path which needs
// explicit half-width overrides not expressible through port sides.
function laneAwarePath(
  from: ConnectionNode,
  to: ConnectionNode,
  lane: number,
  laneCount: number,
  fromHalfWidth: number = CONNECTION_CANVAS.nodeHalfWidth,
  toHalfWidth: number = CONNECTION_CANVAS.nodeHalfWidth,
): string {
  const movingRight = to.x >= from.x;
  const startX = from.x + (movingRight ? fromHalfWidth : -fromHalfWidth);
  const endX = to.x + (movingRight ? -toHalfWidth : toHalfWidth);
  const deltaX = endX - startX;

  if (Math.abs(deltaX) < 2) {
    return `M ${startX} ${from.y} L ${endX} ${to.y}`;
  }

  const direction = deltaX >= 0 ? 1 : -1;
  const span = Math.abs(deltaX);
  const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 13 : 0;
  const midX = startX + deltaX / 2;
  const midY = (from.y + to.y) / 2 + laneOffset;
  const c1x = startX + direction * span * 0.24;
  const c2x = midX - direction * span * 0.18;
  const c3x = midX + direction * span * 0.18;
  const c4x = endX - direction * span * 0.24;

  return `M ${startX} ${from.y} C ${c1x} ${from.y}, ${c2x} ${midY}, ${midX} ${midY} C ${c3x} ${midY}, ${c4x} ${to.y}, ${endX} ${to.y}`;
}

function infraLadderPath(
  from: ConnectionNode,
  to: ConnectionNode,
  lane: number,
  laneCount: number,
  infraRoute: { x: number; y: number },
): string {
  const startX = from.x;
  const startY = from.y + CONNECTION_CANVAS.nodeHalfHeight;
  const endX = to.x;
  const endY = to.y - CONNECTION_CANVAS.nodeHalfHeight;
  const laneOffset = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 12 : 0;
  const routeY = Math.max(infraRoute.y + laneOffset, startY + 50);
  const midX = startX + (endX - startX) * 0.5;

  return `M ${startX} ${startY} C ${startX} ${startY + 26}, ${midX} ${routeY - 18}, ${midX} ${routeY} L ${endX} ${routeY} C ${endX} ${routeY + 16}, ${endX} ${endY - 18}, ${endX} ${endY}`;
}

function dlqArcPath(from: ConnectionNode, to: ConnectionNode): string {
  const startX = from.x - CONNECTION_CANVAS.nodeHalfWidth;
  const endX = to.x - CONNECTION_CANVAS.nodeHalfWidth;
  const bulge = 50;
  return `M ${startX} ${from.y} C ${startX - bulge} ${from.y}, ${endX - bulge} ${to.y}, ${endX} ${to.y}`;
}

function withLanes<T extends { from: ConnectionNode; to: ConnectionNode }>(
  edges: T[],
  edgeId: (edge: T) => string,
): Array<T & { lane: number; laneCount: number; id: string }> {
  const sorted = [...edges].sort((a, b) => a.from.y - b.from.y || a.to.y - b.to.y);
  return sorted.map((edge, lane, all) => ({
    ...edge,
    id: edgeId(edge),
    lane,
    laneCount: all.length,
  }));
}

function filterLabel(filterCriteria: FilterCriteria | undefined): string | null {
  if (!filterCriteria || filterCriteria.Filters.length === 0) return null;

  try {
    const pattern = JSON.parse(filterCriteria.Filters[0].Pattern);
    const bodyConditions = pattern.body;
    if (!bodyConditions || typeof bodyConditions !== "object") return null;

    const segments = Object.entries(bodyConditions).map(([key, value]) => {
      const values = Array.isArray(value) ? value : [value];
      return `${key}=${values.slice(0, 2).join("|")}`;
    });

    return segments.slice(0, 2).join(",");
  } catch {
    const raw = filterCriteria.Filters[0].Pattern;
    return raw.length > 16 ? `${raw.slice(0, 15)}…` : raw;
  }
}

function buildTraceEdgeActivity(
  recentTraces: RequestTrace[],
  now: number,
): Map<string, EdgeActivity> {
  const map = new Map<string, EdgeActivity>();
  const bump = (key: string, trace: RequestTrace) => {
    const activity = map.get(key) ?? {
      count: 0,
      hasError: false,
      latestMs: 0,
    };

    activity.count += 1;
    if (trace.status >= 500) activity.hasError = true;
    if (trace.durationMs > activity.latestMs) activity.latestMs = trace.durationMs;
    map.set(key, activity);
  };

  for (const trace of recentTraces) {
    const age = now - new Date(trace.startedAt).getTime();
    if (age > TRACE_WINDOW_MS) continue;

    const eventBridgeSpan = trace.spans.find((span) => span.kind === "eventbridge");
    const lambdaSpan = trace.spans.find((span) => span.kind === "lambda");
    const topicSpan = trace.spans.find((span) => span.kind === "topic");
    const queueSpan = trace.spans.find((span) => span.kind === "queue");
    const dynamodbSpan = trace.spans.find(
      (span) => span.kind === "dynamodb" || span.kind === "ddb",
    );
    const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
    const s3Span = trace.spans.find((span) => span.kind === "s3");
    const hasCacheFlow = trace.spans.some(
      (span) =>
        span.kind === "cache_extension" ||
        span.kind === "cache-extension" ||
        span.kind === "secrets" ||
        span.kind === "secret",
    );
    const secretNames = new Set(
      trace.spans
        .filter((span) => span.kind === "secrets" || span.kind === "secret")
        .map((span) => span.name)
        .filter(Boolean),
    );

    let key: string | null = null;
    if (trace.gatewayId) {
      const target = lambdaSpan ? lambdaSpan.name : queueSpan ? queueSpan.name : null;
      if (target) key = `gw::${trace.gatewayId}→${target}`;
    } else if (eventBridgeSpan && lambdaSpan) {
      key = `eventbridge::${eventBridgeSpan.name}→${lambdaSpan.name}`;
    } else if (topicSpan && queueSpan) {
      key = `sns::${topicSpan.name}→${queueSpan.name}`;
    } else if (topicSpan && lambdaSpan) {
      key = `sns::${topicSpan.name}→${lambdaSpan.name}`;
    } else if (queueSpan && dlqSpan) {
      key = `dlq::${queueSpan.name}→${dlqSpan.name}`;
    } else if (queueSpan && lambdaSpan) {
      key = `queue::${queueSpan.name}→${lambdaSpan.name}`;
    } else if (dynamodbSpan && lambdaSpan) {
      key = `dynamodb::${dynamodbSpan.name}→${lambdaSpan.name}`;
    } else if (s3Span && lambdaSpan) {
      key = `s3::${bucketNameFromTraceSpanName(s3Span.name)}→${lambdaSpan.name}`;
    }

    if (key) {
      bump(key, trace);
    }
    if (hasCacheFlow) {
      bump("cache::global", trace);
      if (lambdaSpan) {
        bump(`fn::${lambdaSpan.name}`, trace);
      }
      for (const secretName of secretNames) {
        bump(`cache::secret:${secretName}`, trace);
      }
    }
  }

  return map;
}

function fnActivity(
  traceEdgeActivity: Map<string, EdgeActivity>,
  functionName: string,
): EdgeActivity | undefined {
  const direct = traceEdgeActivity.get(`fn::${functionName}`);
  if (direct) {
    return direct;
  }
  for (const [key, activity] of traceEdgeActivity) {
    if (key.endsWith(`→${functionName}`)) {
      return activity;
    }
  }
  return undefined;
}

function aggregateActivity(activities: EdgeActivity[]): EdgeActivity | undefined {
  if (activities.length === 0) return undefined;
  let count = 0;
  let latestMs = 0;
  let hasError = false;

  for (const activity of activities) {
    count += activity.count;
    hasError = hasError || activity.hasError;
    latestMs = Math.max(latestMs, activity.latestMs);
  }

  return { count, latestMs, hasError };
}
