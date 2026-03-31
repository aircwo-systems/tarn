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
};

export const CONNECTION_CANVAS = {
  width: 3400,
  height: 1800,
  nodeHalfWidth: 96,
  nodeHalfHeight: 32,
  cacheHalfWidth: 98,
  cacheHalfHeight: 34,
  infraHalfWidth: 108,
  colGateway: 600,
  colEventBridge: 960,
  colTopic: 1320,
  colQueue: 1680,
  colDynamodb: 3000,
  colFunction: 2400,
  colSecret: 2760,
  colBucket: 3120,
  colInfra: 2400,
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

export interface TopologyGraphModel {
  hasData: boolean;
  eventBridgeRuleById: Map<string, EventBridgeRuleSummary>;
  functionById: Map<string, FunctionSummary>;
  dynamodbById: Map<string, DynamoDBTableSummary>;
  infraById: Map<string, InfraProbe>;
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
  recentTraces: RequestTrace[];
  now?: number;
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
    recentTraces,
    now = Date.now(),
  } = input;

  const connGateways = gateways.map(
    (gw, i): ConnectionNode => ({
      id: gw.apiId,
      x: CONNECTION_CANVAS.colGateway,
      y: distributedColumnY(i, gateways.length, 170, 790),
      label: trimLabel(gw.name, 13),
      sub: `${gw.routes} routes`,
      kind: "gateway",
    }),
  );

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
  const connEventBridges = [...eventBridgeTargetCounts.entries()].map(
    ([ruleName, targetCount], i): ConnectionNode => ({
      id: ruleName,
      x: CONNECTION_CANVAS.colEventBridge,
      y: distributedColumnY(i, eventBridgeTargetCounts.size, 140, 770),
      label: trimLabel(ruleName, 13),
      sub: `${targetCount} target${targetCount === 1 ? "" : "s"}`,
      kind: "eventbridge",
    }),
  );

  const connTopics = topics.map(
    (t, i): ConnectionNode => ({
      id: t.name,
      x: CONNECTION_CANVAS.colTopic,
      y: distributedColumnY(i, topics.length, 140, 740),
      label: trimLabel(t.name, 13),
      sub: `${t.subscriptions} sub`,
      kind: "topic",
    }),
  );

  const connQueues = queues.map(
    (q, i): ConnectionNode => ({
      id: q.name,
      x: CONNECTION_CANVAS.colQueue,
      y: distributedColumnY(i, queues.length, 150, 855),
      label: trimLabel(q.name, 13),
      sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
      kind: "queue",
    }),
  );

  const connDynamodbs = dynamodbTables.map(
    (table, i): ConnectionNode => ({
      id: table.name,
      x: CONNECTION_CANVAS.colDynamodb,
      y: distributedColumnY(i, dynamodbTables.length, 170, 900),
      label: trimLabel(table.name, 13),
      sub: table.streamEnabled
        ? `${table.itemCount} item · stream`
        : `${table.itemCount} item`,
      kind: "dynamodb",
    }),
  );

  const connFunctions = functions.map(
    (fn, i): ConnectionNode => ({
      id: fn.name,
      x: CONNECTION_CANVAS.colFunction,
      y: distributedColumnY(i, functions.length, 170, 915),
      label: trimLabel(fn.name, 13),
      sub: fn.runtime,
      kind: "function",
    }),
  );

  const connBuckets = buckets.map(
    (b, i): ConnectionNode => ({
      id: b.name,
      x: CONNECTION_CANVAS.colBucket,
      y: distributedColumnY(i, buckets.length, 120, 1080),
      label: trimLabel(b.name, 13),
      sub: `${b.objects} obj`,
      kind: "bucket",
      bucket: b,
    }),
  );

  const connSecrets = secrets.map(
    (s, i): ConnectionNode => ({
      id: s.name,
      x: CONNECTION_CANVAS.colSecret,
      y: distributedColumnY(i, secrets.length, 190, 985),
      label: trimLabel(s.name, 13),
      sub: `v${s.versionId.slice(0, 6)}`,
      kind: "secret",
    }),
  );

  const mainGroups = [
    connGateways, connEventBridges, connTopics, connQueues,
    connFunctions, connSecrets, connDynamodbs, connBuckets,
  ];

  // Apply size/side overrides BEFORE collision resolution so the force-field
  // uses each node's actual rendered dimensions. Each override is scoped to
  // the individual node by ID — connected nodes are never affected.
  for (const group of mainGroups) {
    for (const node of group) {
      applyNodeOverride(node, allNodeOverrides[`${node.kind}:${node.id}`]);
    }
  }

  applyForceFieldCollisions(mainGroups);

  // Apply dragged positions AFTER collision so user-pinned nodes stay put.
  for (const group of mainGroups) {
    for (const node of group) {
      const pos = getPersistedNodePosition(allNodePositions, node);
      if (!pos) continue;
      const nextPosition = resolveNodeSpacing(node, pos, mainGroups.flat());
      node.x = nextPosition.x;
      node.y = nextPosition.y;
    }
  }

  const connInfraNodes = buildInfraNodes(infra, infraOrderIds, allNodePositions);
  for (const node of connInfraNodes) applyNodeOverride(node, allNodeOverrides[`${node.kind}:${node.id}`]);

  const connCacheExtension: ConnectionNode | null =
    connSecrets.length > 0
      ? {
          id: "secrets-cache-extension",
          x: cacheExtensionX(),
          y: CONNECTION_CANVAS.height / 2 + 46,
          label: "Secrets Cache",
          sub: "localhost:2773",
          kind: "extension",
        }
      : null;
  if (connCacheExtension) applyNodeOverride(connCacheExtension, allNodeOverrides[`${connCacheExtension.kind}:${connCacheExtension.id}`]);
  if (connCacheExtension) {
    const pos = getPersistedNodePosition(allNodePositions, connCacheExtension);
    const nextPosition = clampNodePosition(
      connCacheExtension,
      pos?.x ?? connCacheExtension.x,
      pos?.y ?? connCacheExtension.y,
    );
    connCacheExtension.x = nextPosition.x;
    connCacheExtension.y = nextPosition.y;
  }

  applyForceFieldCollisions(
    [
      ...mainGroups,
      ...(connCacheExtension ? [[connCacheExtension]] : []),
      connInfraNodes,
    ],
    TOPOLOGY_MIN_NODE_GAP,
    12,
    "both",
    new Set(mainGroups.flat().map((node) => graphNodeKey(node))),
  );

  const infraLane = buildInfraLane(connInfraNodes);
  const infraRoute = {
    x: infraLane.x + infraLane.width / 2,
    y: infraLane.y - 26,
  };

  const gatewayIdByName = new Map(gateways.map((gw) => [gw.name, gw.apiId]));
  const queueByName = new Map(queues.map((q) => [q.name, q]));
  const functionByName = new Map(functions.map((fn) => [fn.name, fn]));
  const infraByNodeId = new Map(
    connInfraNodes.map((node) => [node.id, infra.find((p) => infraNodeId(p) === node.id)]),
  );

  const traceEdgeActivity = buildTraceEdgeActivity(recentTraces, now);

  const apigwToQueue = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "apigw-sqs") return [];
      const gwId = gatewayIdByName.get(c.sourceFunction) ?? c.sourceFunction;
      const from = connGateways.find((n) => n.id === gwId);
      const queueName = c.targetId || c.targetName;
      const to = connQueues.find((n) => n.id === queueName);
      if (!from || !to) return [];
      const queue = queueByName.get(queueName);
      const total =
        (queue?.approxVisible ?? 0) + (queue?.approxInFlight ?? 0) + (queue?.approxDelayed ?? 0);
      const activity = traceEdgeActivity.get(`gw::${gwId}→${queueName}`);
      return [{ from, to, active: total > 0, activity }];
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
      const from = connGateways.find((n) => n.id === gwId);
      const fnName = c.targetId || c.targetName;
      const to = connFunctions.find((n) => n.id === fnName);
      if (!from || !to) return [];
      const fn = functionByName.get(fnName);
      const activity = traceEdgeActivity.get(`gw::${gwId}→${fnName}`);
      return [{ from, to, active: (fn?.messagesProcessed ?? 0) > 0, activity }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const seenEbEdges = new Set<string>();
  const ebEdgeCandidates: { from: ConnectionNode; to: ConnectionNode; activity?: EdgeActivity }[] = [];
  for (const c of infraConnections) {
    if (c.targetKind !== "eventbridge-lambda") continue;
    const from = connEventBridges.find((n) => n.id === c.sourceFunction);
    const fnName = c.targetId || c.targetName;
    const to = connFunctions.find((n) => n.id === fnName);
    if (!from || !to) continue;
    const key = `${from.id}→${to.id}`;
    if (seenEbEdges.has(key)) continue;
    seenEbEdges.add(key);
    const activity = traceEdgeActivity.get(`eventbridge::${from.id}→${to.id}`);
    ebEdgeCandidates.push({ from, to, activity });
  }
  for (const rule of eventBridgeRules) {
    const from = connEventBridges.find((n) => n.id === rule.name);
    if (!from) continue;
    for (const target of rule.targets ?? []) {
      const fnName = lambdaNameFromArn(target.arn);
      const to = connFunctions.find((n) => n.id === fnName);
      if (!to) continue;
      const key = `${from.id}→${to.id}`;
      if (seenEbEdges.has(key)) continue;
      seenEbEdges.add(key);
      ebEdgeCandidates.push({ from, to, activity: undefined });
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
      const from = connTopics.find((n) => n.id === c.sourceFunction);
      const queueName = c.targetId || c.targetName;
      const to = connQueues.find((n) => n.id === queueName);
      if (!from || !to) return [];
      const activity = traceEdgeActivity.get(`sns::${from.id}→${to.id}`);
      return [{ from, to, activity }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const snsToFunction = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "sns-lambda") return [];
      const from = connTopics.find((n) => n.id === c.sourceFunction);
      const fnName = c.targetId || c.targetName;
      const to = connFunctions.find((n) => n.id === fnName);
      if (!from || !to) return [];
      const activity = traceEdgeActivity.get(`sns::${from.id}→${to.id}`);
      return [{ from, to, activity }];
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
  const hasQueueFnPair = (queueId: string, fnId: string) =>
    queueToFunctionPairs.some((pair) => pair.queueId === queueId && pair.fnId === fnId);

  for (const mapping of eventSourceMappings) {
    if (hasQueueFnPair(mapping.queueName, mapping.functionName)) continue;
    queueToFunctionPairs.push({
      queueId: mapping.queueName,
      fnId: mapping.functionName,
      filterCriteria: mapping.filterCriteria,
    });
  }

  for (const c of infraConnections) {
    if (c.targetKind !== "sqs-lambda") continue;
    const fnId = c.targetId || c.targetName;
    if (hasQueueFnPair(c.sourceFunction, fnId)) continue;
    queueToFunctionPairs.push({
      queueId: c.sourceFunction,
      fnId,
      filterCriteria: c.filterCriteria,
    });
  }

  const queueToFunction = withLanes(
    queueToFunctionPairs.flatMap(({ queueId, fnId, filterCriteria }) => {
      const from = connQueues.find((n) => n.id === queueId);
      const to = connFunctions.find((n) => n.id === fnId);
      if (!from || !to) return [];
      const activity = traceEdgeActivity.get(`queue::${queueId}→${fnId}`);
      return [
        {
          from,
          to,
          filterLabel: filterLabel(filterCriteria),
          activity,
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
  const hasDynamoPair = (tableId: string, fnId: string) =>
    dynamodbMappingPairs.some((pair) => pair.tableId === tableId && pair.fnId === fnId);

  for (const mapping of eventSourceMappings) {
    if ((mapping.sourceType ?? "").toLowerCase() !== "dynamodb-stream") continue;
    const tableId = mapping.sourceName || mapping.queueName;
    if (!tableId || hasDynamoPair(tableId, mapping.functionName)) continue;
    dynamodbMappingPairs.push({
      tableId,
      fnId: mapping.functionName,
      filterCriteria: mapping.filterCriteria,
    });
  }

  for (const c of infraConnections) {
    if (c.targetKind !== "dynamodb-stream-lambda") continue;
    const fnId = c.targetId || c.targetName;
    if (!c.sourceFunction || hasDynamoPair(c.sourceFunction, fnId)) continue;
    dynamodbMappingPairs.push({
      tableId: c.sourceFunction,
      fnId,
      filterCriteria: c.filterCriteria,
    });
  }

  const dynamodbToFunction = withLanes(
    dynamodbMappingPairs.flatMap(({ tableId, fnId, filterCriteria }) => {
      const from = connDynamodbs.find((node) => node.id === tableId);
      const to = connFunctions.find((node) => node.id === fnId);
      if (!from || !to) return [];
      return [
        {
          from,
          to,
          filterLabel: filterLabel(filterCriteria),
          activity: undefined,
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
    const from = connQueues.find((n) => n.id === c.sourceFunction);
    const to = connQueues.find((n) => n.id === (c.targetId || c.targetName));
    if (!from || !to || from.id === to.id) return [];
    const activity = traceEdgeActivity.get(`dlq::${from.id}→${to.id}`);
    return [
      {
        id: `${from.id}→${to.id}`,
        from,
        to,
        path: dlqArcPath(from, to),
        activity,
      },
    ];
  });

  const seenBucketFnEdges = new Set<string>();
  const bucketFnCandidates: Array<{ from: ConnectionNode; to: ConnectionNode }> = [];

  for (const c of infraConnections) {
    if (c.targetKind !== "s3-lambda") continue;
    const from = connBuckets.find((n) => n.id === c.sourceFunction);
    const fnName = c.targetId || c.targetName;
    const to = connFunctions.find((n) => n.id === fnName);
    if (!from || !to) continue;
    const key = `${from.id}→${to.id}`;
    if (seenBucketFnEdges.has(key)) continue;
    seenBucketFnEdges.add(key);
    bucketFnCandidates.push({ from, to });
  }

  for (const trace of recentTraces) {
    const s3Span = trace.spans.find((span) => span.kind === "s3");
    const lambdaSpan = trace.spans.find((span) => span.kind === "lambda");
    if (!s3Span || !lambdaSpan) continue;

    const bucketName = bucketNameFromTraceSpanName(s3Span.name);
    const from = connBuckets.find((n) => n.id === bucketName);
    const to = connFunctions.find((n) => n.id === lambdaSpan.name);
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
      const from = connFunctions.find((n) => n.id === c.sourceFunction);
      const tableName = c.targetId || c.targetName;
      const to = connDynamodbs.find((n) => n.id === tableName);
      if (!from || !to) return [];
      return [{ from, to, activity: fnActivity(traceEdgeActivity, from.id) }];
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
      const from = connFunctions.find((n) => n.id === c.sourceFunction);
      const to = connInfraNodes.find((n) => n.id === c.targetId);
      if (!from || !to) return [];
      const probe = infraByNodeId.get(to.id);
      const activity = fnActivity(traceEdgeActivity, from.id);
      return [{ from, to, probe, isConnected: probe?.status === "connected", activity }];
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
            activity: fnActivity(traceEdgeActivity, fn.id),
          })),
        (edge) => `${edge.from.id}→cache`,
      ).map((edge) => ({
        ...edge,
        id: `${edge.from.id}→cache`,
        path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
      }))
    : [];

  const cacheActivity = aggregateActivity([
    ...functionToCache.flatMap((edge) => (edge.activity ? [edge.activity] : [])),
    ...(traceEdgeActivity.get("cache::global") ? [traceEdgeActivity.get("cache::global")!] : []),
  ]);

  const cacheToSecret = connCacheExtension
    ? withLanes(
        [...connSecrets]
          .sort((a, b) => a.y - b.y)
          .map((secret) => ({
            from: connCacheExtension,
            to: secret,
            activity: cacheActivity,
          })),
        (edge) => `cache→${edge.to.id}`,
      ).map((edge) => ({
        ...edge,
        id: `cache→${edge.to.id}`,
        path: portConnectPath(edge.from, edge.to, edge.lane, edge.laneCount),
      }))
    : [];

  return {
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
    eventBridgeRuleById: new Map(eventBridgeRules.map((rule) => [rule.name, rule])),
    functionById: new Map(functions.map((fn) => [fn.name, fn])),
    dynamodbById: new Map(dynamodbTables.map((table) => [table.name, table])),
    infraById: new Map(
      [...infraByNodeId.entries()].filter((entry): entry is [string, InfraProbe] => !!entry[1]),
    ),
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
    infraLane,
    infraRoute,
    traces: {
      ticker: recentTraces.slice(0, 8),
      edgeActivity: traceEdgeActivity,
      cacheActivity,
    },
  };
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
  return Math.min(2.4, base + activity.count * 0.2);
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
  const dynamodbSpan = trace.spans.find(
    (span) => span.kind === "dynamodb" || span.kind === "ddb",
  );
  const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
  const cacheSpan = trace.spans.find(
    (span) => span.kind === "cache_extension" || span.kind === "cache-extension",
  );
  const secretsSpan = trace.spans.find((span) => span.kind === "secrets" || span.kind === "secret");

  const matchedGateway = trace.gatewayId
    ? model.nodes.gateways.find((node) => node.id === trace.gatewayId)
    : undefined;
  const matchedFunction = lambdaSpan
    ? model.nodes.functions.find((node) => node.id === lambdaSpan.name)
    : undefined;
  const matchedEventBridge = eventBridgeSpan
    ? model.nodes.eventbridges.find((node) => node.id === eventBridgeSpan.name)
    : undefined;
  const matchedTopic = topicSpan
    ? model.nodes.topics.find((node) => node.id === topicSpan.name)
    : undefined;
  const matchedQueue = queueSpan
    ? model.nodes.queues.find((node) => node.id === queueSpan.name)
    : undefined;
  const matchedDynamodb = dynamodbSpan
    ? model.nodes.dynamodbs.find((node) => node.id === dynamodbSpan.name)
    : undefined;
  const matchedDlq = dlqSpan
    ? model.nodes.queues.find((node) => node.id === dlqSpan.name)
    : undefined;
  const matchedCache =
    cacheSpan || secretsSpan ? (model.nodes.cacheExtension ?? undefined) : undefined;
  const matchedSecret = secretsSpan
    ? model.nodes.secrets.find((node) => node.id === secretsSpan.name)
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
  const baseHW =
    node.kind === "infra" ? CONNECTION_CANVAS.infraHalfWidth :
    node.kind === "extension" ? CONNECTION_CANVAS.cacheHalfWidth :
    CONNECTION_CANVAS.nodeHalfWidth;
  const baseHH =
    node.kind === "extension" ? CONNECTION_CANVAS.cacheHalfHeight :
    CONNECTION_CANVAS.nodeHalfHeight;

  let halfWidth: number;
  let halfHeight: number;
  switch (node.size) {
    case "medium":
      halfWidth = baseHW;
      halfHeight = baseHW; // square: height equals width
      break;
    case "large":
      halfWidth = baseHW * 2;
      halfHeight = baseHW * 2; // 2× medium
      break;
    default:
      halfWidth = baseHW;
      halfHeight = baseHH;
  }

  return {
    left: node.x - halfWidth,
    right: node.x + halfWidth,
    top: node.y - halfHeight,
    bottom: node.y + halfHeight,
  };
}

function applyNodeOverride(node: ConnectionNode, override: NodeOverride | undefined): void {
  if (override?.inputSide) node.inputSide = override.inputSide;
  if (override?.outputSide) node.outputSide = override.outputSide;
  node.size = resolveTopologyNodeSize(node.kind, override?.size ?? node.size);
  node.view = resolveTopologyNodeView(node.kind, override?.view, node.size ?? "small");
}

export function findNodeAt(model: TopologyGraphModel, x: number, y: number): ConnectionNode | null {
  const candidates: ConnectionNode[] = [
    ...model.nodes.infra,
    ...model.nodes.secrets,
    ...(model.nodes.cacheExtension ? [model.nodes.cacheExtension] : []),
    ...model.nodes.functions,
    ...model.nodes.dynamodbs,
    ...model.nodes.eventbridges,
    ...model.nodes.topics,
    ...model.nodes.queues,
    ...model.nodes.buckets,
    ...model.nodes.gateways,
  ];

  for (let i = candidates.length - 1; i >= 0; i -= 1) {
    const node = candidates[i];
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
  options: { expanded?: boolean } = {},
): ViewportTransform {
  const safeWidth = Math.max(1, viewportWidth);
  const safeHeight = Math.max(1, viewportHeight);
  const paddingX = safeWidth < 900 ? 10 : 16;
  const paddingY = safeHeight < 640 ? 10 : 16;
  const fitScale = Math.min(
    safeWidth / (CONNECTION_CANVAS.width + paddingX * 2),
    safeHeight / (CONNECTION_CANVAS.height + paddingY * 2),
  );
  const referenceFitScale = Math.min(
    safeWidth / (TOPOLOGY_VIEWPORT_REFERENCE.width + paddingX * 2),
    safeHeight / (TOPOLOGY_VIEWPORT_REFERENCE.height + paddingY * 2),
  );
  const zoomFactor = options.expanded
    ? safeWidth < 640
      ? 1.12
      : safeWidth < 1024
        ? 1.18
        : 1.26
    : safeWidth < 900
      ? 1.02
      : 1.08;
  const scale = Math.max(fitScale, referenceFitScale) * zoomFactor;

  return {
    scale,
    offsetX: (safeWidth - CONNECTION_CANVAS.width * scale) / 2,
    offsetY: (safeHeight - CONNECTION_CANVAS.height * scale) / 2,
  };
}

export function clampViewportTransform(
  viewportWidth: number,
  viewportHeight: number,
  transform: ViewportTransform,
): ViewportTransform {
  const safeWidth = Math.max(1, viewportWidth);
  const safeHeight = Math.max(1, viewportHeight);
  const contentWidth = CONNECTION_CANVAS.width * transform.scale;
  const contentHeight = CONNECTION_CANVAS.height * transform.scale;
  const overscrollX = contentWidth > safeWidth ? Math.min(420, safeWidth * 0.35) : 0;
  const overscrollY = contentHeight > safeHeight ? Math.min(320, safeHeight * 0.28) : 0;

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

export function clampInfraNodePosition(x: number, y: number): InfraNodePosition {
  return {
    x: clamp(
      x,
      CONNECTION_CANVAS.infraHalfWidth + 24,
      CONNECTION_CANVAS.width - CONNECTION_CANVAS.infraHalfWidth - 24,
    ),
    y: clamp(y, 120, CONNECTION_CANVAS.height - CONNECTION_CANVAS.nodeHalfHeight - 24),
  };
}

export function resolveSnappedNodePosition(
  model: TopologyGraphModel,
  nodeKey: { id: string; kind: ConnectionNode["kind"] },
  x: number,
  y: number,
): InfraNodePosition {
  const node = findGraphNode(model, nodeKey);
  if (!node) return clampInfraNodePosition(x, y);

  return resolveNodeSpacing(
    node,
    { x, y },
    allGraphNodes(model),
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

  const allEdges: Array<{ id: string; from: ConnectionNode; to: ConnectionNode }> = [
    ...model.edges.apigwToQueue,
    ...model.edges.apigwToFunction,
    ...model.edges.eventbridgeToFunction,
    ...model.edges.snsToQueue,
    ...model.edges.snsToFunction,
    ...model.edges.queueToFunction,
    ...model.edges.dynamodbToFunction,
    ...model.edges.queueToDlq,
    ...model.edges.bucketToFunction,
    ...model.edges.functionToDynamodb,
    ...model.edges.functionToCache,
    ...model.edges.cacheToSecret,
    ...model.edges.functionToInfra,
  ];

  const forwardByNode = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();
  const reverseByNode = new Map<
    string,
    Array<{ id: string; from: ConnectionNode; to: ConnectionNode }>
  >();

  for (const edge of allEdges) {
    const forwardEdges = forwardByNode.get(edge.from.id) ?? [];
    forwardEdges.push(edge);
    forwardByNode.set(edge.from.id, forwardEdges);

    const reverseEdges = reverseByNode.get(edge.to.id) ?? [];
    reverseEdges.push(edge);
    reverseByNode.set(edge.to.id, reverseEdges);
  }

  walkHoverFlow(hoveredNodeId, forwardByNode, true, nodeIds, edgeIds);
  walkHoverFlow(hoveredNodeId, reverseByNode, false, nodeIds, edgeIds);

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
    node.kind === "extension" ? CONNECTION_CANVAS.cacheHalfWidth :
    node.kind === "infra" ? CONNECTION_CANVAS.infraHalfWidth :
    CONNECTION_CANVAS.nodeHalfWidth;
  if (node.size === "large") return base * 2;
  return base; // small and medium both use base width
}

function nodeHalfHeight(node: ConnectionNode): number {
  const baseHW =
    node.kind === "extension" ? CONNECTION_CANVAS.cacheHalfWidth :
    node.kind === "infra" ? CONNECTION_CANVAS.infraHalfWidth :
    CONNECTION_CANVAS.nodeHalfWidth;
  const baseHH =
    node.kind === "extension" ? CONNECTION_CANVAS.cacheHalfHeight :
    CONNECTION_CANVAS.nodeHalfHeight;
  switch (node.size) {
    case "medium": return baseHW;
    case "large":  return baseHW * 2;
    default:       return baseHH;
  }
}

/**
 * Iteratively push overlapping nodes apart along the Y axis.
 * X positions (columns) are intentional layout and are not modified.
 * Runs on all nodes across all supplied column groups so cross-column
 * proximity (e.g. EventBridge ↔ Topic) is also resolved.
 */
function applyForceFieldCollisions(
  nodeGroups: ConnectionNode[][],
  padding = TOPOLOGY_MIN_NODE_GAP,
  iterations = 8,
  axis: "y" | "both" = "y",
  lockedNodeKeys: ReadonlySet<string> = new Set(),
): void {
  const all = nodeGroups.flat();
  if (all.length <= 1) return;

  for (let iter = 0; iter < iterations; iter++) {
    let moved = false;
    for (let i = 0; i < all.length; i++) {
      for (let j = i + 1; j < all.length; j++) {
        const a = all[i];
        const b = all[j];
        const lockA = lockedNodeKeys.has(graphNodeKey(a));
        const lockB = lockedNodeKeys.has(graphNodeKey(b));
        if (lockA && lockB) continue;

        const requiredX = nodeHalfWidth(a) + nodeHalfWidth(b) + padding;
        const requiredY = nodeHalfHeight(a) + nodeHalfHeight(b) + padding;
        const deltaX = b.x - a.x;
        const deltaY = b.y - a.y;
        const overlapX = requiredX - Math.abs(deltaX);
        const overlapY = requiredY - Math.abs(deltaY);

        if (overlapX <= 0 || overlapY <= 0) continue;

        const pushAxis = axis === "y" ? "y" : overlapX < overlapY ? "x" : "y";

        if (pushAxis === "x") {
          const direction =
            deltaX === 0 ? (b.x >= CONNECTION_CANVAS.width / 2 ? 1 : -1) : Math.sign(deltaX);
          const totalPush = overlapX + 1;
          if (lockA) {
            const nextB = clampNodePosition(b, b.x + direction * totalPush, b.y);
            b.x = nextB.x;
            b.y = nextB.y;
          } else if (lockB) {
            const nextA = clampNodePosition(a, a.x - direction * totalPush, a.y);
            a.x = nextA.x;
            a.y = nextA.y;
          } else {
            const push = totalPush / 2;
            const nextA = clampNodePosition(a, a.x - direction * push, a.y);
            const nextB = clampNodePosition(b, b.x + direction * push, b.y);
            a.x = nextA.x;
            a.y = nextA.y;
            b.x = nextB.x;
            b.y = nextB.y;
          }
        } else {
          const direction = deltaY === 0 ? (b.y >= a.y ? 1 : -1) : Math.sign(deltaY);
          const totalPush = overlapY + 1;
          if (lockA) {
            const nextB = clampNodePosition(b, b.x, b.y + direction * totalPush);
            b.x = nextB.x;
            b.y = nextB.y;
          } else if (lockB) {
            const nextA = clampNodePosition(a, a.x, a.y - direction * totalPush);
            a.x = nextA.x;
            a.y = nextA.y;
          } else {
            const push = totalPush / 2;
            const nextA = clampNodePosition(a, a.x, a.y - direction * push);
            const nextB = clampNodePosition(b, b.x, b.y + direction * push);
            a.x = nextA.x;
            a.y = nextA.y;
            b.x = nextB.x;
            b.y = nextB.y;
          }
        }
        moved = true;
      }
    }
    if (!moved) break;
  }
}

function resolveNodeSpacing(
  node: ConnectionNode,
  desiredPosition: InfraNodePosition,
  nodes: ConnectionNode[],
  minGap = TOPOLOGY_MIN_NODE_GAP,
): InfraNodePosition {
  let candidate = clampNodePosition(node, desiredPosition.x, desiredPosition.y);
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
        const direction =
          deltaX === 0
            ? candidate.x >= CONNECTION_CANVAS.width / 2
              ? 1
              : -1
            : Math.sign(deltaX);
        candidate = clampNodePosition(
          node,
          candidate.x + direction * (overlapX + 1),
          candidate.y,
        );
      } else {
        const direction =
          deltaY === 0 ? (candidate.y >= other.y ? 1 : -1) : Math.sign(deltaY);
        candidate = clampNodePosition(
          node,
          candidate.x,
          candidate.y + direction * (overlapY + 1),
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

function distributedColumnY(
  index: number,
  total: number,
  gap: number,
  centerY: number,
): number {
  if (total <= 1) return centerY;

  const totalSpan = gap * (total - 1);
  const topBound = CONNECTION_CANVAS.nodeHalfHeight + 80;
  const bottomBound = CONNECTION_CANVAS.height - CONNECTION_CANVAS.nodeHalfHeight - 120;
  const start = clamp(
    centerY - totalSpan / 2,
    topBound,
    Math.max(topBound, bottomBound - totalSpan),
  );
  return start + index * gap;
}

function infraNodeId(probe: InfraProbe): string {
  return `${probe.kind}-${probe.host}-${probe.port}`;
}

function buildInfraNodes(
  infra: InfraProbe[],
  infraOrderIds: string[],
  allNodePositions: Record<string, InfraNodePosition>,
): ConnectionNode[] {
  const visible = infra.map((probe) => ({ id: infraNodeId(probe), probe }));
  if (visible.length === 0) return [];

  const byId = new Map(visible.map((entry) => [entry.id, entry.probe]));
  const orderedIds = [
    ...infraOrderIds.filter((id) => byId.has(id)),
    ...visible.map((entry) => entry.id).filter((id) => !infraOrderIds.includes(id)),
  ];

  const laneBottomY = CONNECTION_CANVAS.height - 240;
  const horizontalMinX = 320;
  const horizontalMaxX = CONNECTION_CANVAS.width - 220;
  const availableWidth = horizontalMaxX - horizontalMinX;
  const minCenterGap = CONNECTION_CANVAS.infraHalfWidth * 2 + TOPOLOGY_MIN_NODE_GAP;
  const maxColumns = Math.max(1, Math.floor(availableWidth / minCenterGap) + 1);
  const columns = Math.min(maxColumns, orderedIds.length);
  const rowGap = CONNECTION_CANVAS.nodeHalfHeight * 2 + TOPOLOGY_MIN_NODE_GAP + 26;
  const totalRows = Math.max(1, Math.ceil(orderedIds.length / columns));

  const nodes = orderedIds.map((id, i): ConnectionNode => {
    const probe = byId.get(id)!;
    const rowIndex = Math.floor(i / columns);
    const columnIndex = i % columns;
    const rowIds = orderedIds.slice(rowIndex * columns, rowIndex * columns + columns);
    const rowCount = rowIds.length;
    const rowStep = rowCount > 1 ? availableWidth / (rowCount - 1) : 0;
    const fallbackX =
      rowCount === 1
        ? (horizontalMinX + horizontalMaxX) / 2
        : horizontalMinX + columnIndex * rowStep;
    const fallbackY = laneBottomY - (totalRows - 1 - rowIndex) * rowGap;
    const persistedPosition =
      allNodePositions[`infra:${id}`] ?? allNodePositions[id];
    const position = persistedPosition
      ? clampInfraNodePosition(persistedPosition.x, persistedPosition.y)
      : { x: fallbackX, y: fallbackY };
    return {
      id,
      x: position.x,
      y: position.y,
      label: trimLabel(probe.name, 13),
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

  for (const node of nodes) {
    const persistedPosition = getPersistedNodePosition(allNodePositions, node);
    if (!persistedPosition) continue;
    const nextPosition = resolveNodeSpacing(node, persistedPosition, nodes);
    node.x = nextPosition.x;
    node.y = nextPosition.y;
  }

  return nodes;
}

function clampOffsetAxis(
  viewportSize: number,
  contentSize: number,
  offset: number,
  overscroll: number,
): number {
  if (contentSize <= viewportSize) {
    return (viewportSize - contentSize) / 2;
  }

  const min = viewportSize - contentSize - overscroll;
  const max = overscroll;
  return clamp(offset, min, max);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function clampNodePosition(
  node: ConnectionNode,
  x: number,
  y: number,
): InfraNodePosition {
  return {
    x: clamp(
      x,
      nodeHalfWidth(node) + 24,
      CONNECTION_CANVAS.width - nodeHalfWidth(node) - 24,
    ),
    y: clamp(
      y,
      nodeHalfHeight(node) + 24,
      CONNECTION_CANVAS.height - nodeHalfHeight(node) - 24,
    ),
  };
}

function allGraphNodes(model: TopologyGraphModel): ConnectionNode[] {
  return [
    ...model.nodes.gateways,
    ...model.nodes.eventbridges,
    ...model.nodes.topics,
    ...model.nodes.queues,
    ...model.nodes.dynamodbs,
    ...model.nodes.functions,
    ...model.nodes.buckets,
    ...model.nodes.secrets,
    ...model.nodes.infra,
    ...(model.nodes.cacheExtension ? [model.nodes.cacheExtension] : []),
  ];
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
  return (
    allGraphNodes(model).find(
      (node) => node.id === nodeKey.id && node.kind === nodeKey.kind,
    ) ?? null
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

function cacheExtensionX(): number {
  const targetX = Math.round(
    (CONNECTION_CANVAS.colFunction +
      CONNECTION_CANVAS.nodeHalfWidth +
      (CONNECTION_CANVAS.colSecret - CONNECTION_CANVAS.nodeHalfWidth)) /
      2,
  );

  const minX =
    CONNECTION_CANVAS.colFunction +
    CONNECTION_CANVAS.nodeHalfWidth +
    CONNECTION_CANVAS.cacheHalfWidth +
    20;
  const maxX =
    CONNECTION_CANVAS.colSecret -
    CONNECTION_CANVAS.nodeHalfWidth -
    CONNECTION_CANVAS.cacheHalfWidth -
    20;

  if (minX > maxX) return targetX;
  return Math.max(minX, Math.min(maxX, targetX));
}

/**
 * Returns the canvas coordinates of a node's input or output connector port.
 * When both ports share the same side they are offset ±14px from centre so
 * they sit as two distinct dots rather than overlapping.
 */
export function portPos(
  node: ConnectionNode,
  role: "input" | "output",
): { x: number; y: number } {
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
      return { x: bounds.left,   y: node.y + (shared ? sign * offset : 0) };
    case "right":
      return { x: bounds.right,  y: node.y + (shared ? sign * offset : 0) };
    case "top":
      return { x: node.x + (shared ? sign * offset : 0), y: bounds.top    };
    case "bottom":
      return { x: node.x + (shared ? sign * offset : 0), y: bounds.bottom };
  }
}

/** Unit vector pointing outward from each side. */
function sideDir(side: NodeSide): { x: number; y: number } {
  switch (side) {
    case "right":  return { x: 1,  y: 0  };
    case "left":   return { x: -1, y: 0  };
    case "top":    return { x: 0,  y: -1 };
    case "bottom": return { x: 0,  y: 1  };
  }
}

/**
 * Generates an SVG cubic bezier connecting the output port of `from` to the
 * input port of `to`, respecting each node's configured port sides.
 * Lanes offset parallel edges perpendicular to the primary flow direction.
 */
function portConnectPath(
  from: ConnectionNode,
  to: ConnectionNode,
  lane: number,
  laneCount: number,
): string {
  const start = portPos(from, "output");
  const end   = portPos(to,   "input");

  const outSide: NodeSide = from.outputSide ?? "right";
  const inSide:  NodeSide = to.inputSide    ?? "left";
  const outDir = sideDir(outSide);
  const inDir  = sideDir(inSide);

  const dx = end.x - start.x;
  const dy = end.y - start.y;
  const dist = Math.sqrt(dx * dx + dy * dy);
  const tension = Math.min(Math.max(dist * 0.42, 40), 180);

  // Lane offset is applied perpendicular to the output direction
  const laneOff = laneCount > 1 ? (lane - (laneCount - 1) / 2) * 14 : 0;
  // Perpendicular to outDir: rotate 90°
  const perpX = -outDir.y;
  const perpY =  outDir.x;

  const c1x = start.x + outDir.x * tension + perpX * laneOff;
  const c1y = start.y + outDir.y * tension + perpY * laneOff;
  const c2x = end.x   + inDir.x  * tension + perpX * laneOff;
  const c2y = end.y   + inDir.y  * tension + perpY * laneOff;

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
    const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
    const s3Span = trace.spans.find((span) => span.kind === "s3");
    const hasCacheFlow = trace.spans.some(
      (span) =>
        span.kind === "cache_extension" ||
        span.kind === "cache-extension" ||
        span.kind === "secrets" ||
        span.kind === "secret",
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
