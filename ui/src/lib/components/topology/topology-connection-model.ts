import type {
  BucketSummary,
  EventSourceMappingSummary,
  FilterCriteria,
  FunctionSummary,
  GatewaySummary,
  InfraConnection,
  InfraProbe,
  QueueSummary,
  RequestTrace,
  SecretSummary,
} from "$lib/types";
import type { ConnectionNode } from "./types";

export const CONNECTION_CANVAS = {
  width: 1520,
  height: 1020,
  nodeHalfWidth: 104,
  nodeHalfHeight: 32,
  cacheHalfWidth: 98,
  cacheHalfHeight: 34,
  infraHalfWidth: 108,
  colGateway: 170,
  colQueue: 430,
  colFunction: 760,
  colSecret: 1120,
  colBucket: 1290,
  colInfra: 760,
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
  infraById: Map<string, InfraProbe>;
  nodes: {
    gateways: ConnectionNode[];
    queues: ConnectionNode[];
    functions: ConnectionNode[];
    buckets: ConnectionNode[];
    secrets: ConnectionNode[];
    infra: ConnectionNode[];
    cacheExtension: ConnectionNode | null;
  };
  edges: {
    apigwToQueue: GwEdge[];
    apigwToFunction: GwEdge[];
    queueToFunction: QueueFnEdge[];
    queueToDlq: DlqEdge[];
    bucketToFunction: LaneEdge[];
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
  buckets: BucketSummary[];
  secrets: SecretSummary[];
  infra: InfraProbe[];
  infraNodePositions?: Record<string, InfraNodePosition>;
  infraConnections: InfraConnection[];
  eventSourceMappings: EventSourceMappingSummary[];
  infraOrderIds: string[];
  recentTraces: RequestTrace[];
  now?: number;
}

export function buildTopologyGraph(input: BuildTopologyGraphInput): TopologyGraphModel {
  const {
    gateways,
    functions,
    queues,
    buckets,
    secrets,
    infra,
    infraNodePositions = {},
    infraConnections,
    eventSourceMappings,
    infraOrderIds,
    recentTraces,
    now = Date.now(),
  } = input;

  const connGateways = gateways.slice(0, 3).map(
    (gw, i): ConnectionNode => ({
      id: gw.apiId,
      x: CONNECTION_CANVAS.colGateway,
      y: 230 + i * 154,
      label: trimLabel(gw.name, 13),
      sub: `${gw.routes} routes`,
      kind: "gateway",
    }),
  );

  const connQueues = queues.slice(0, 4).map(
    (q, i): ConnectionNode => ({
      id: q.name,
      x: CONNECTION_CANVAS.colQueue,
      y: 260 + i * 146,
      label: trimLabel(q.name, 13),
      sub: `${q.approxVisible + q.approxInFlight + q.approxDelayed} msg`,
      kind: "queue",
    }),
  );

  const connFunctions = functions.slice(0, 4).map(
    (fn, i): ConnectionNode => ({
      id: fn.name,
      x: CONNECTION_CANVAS.colFunction,
      y: 260 + i * 146,
      label: trimLabel(fn.name, 13),
      sub: fn.runtime,
      kind: "function",
    }),
  );

  const connBuckets = buckets.slice(0, 4).map(
    (b, i): ConnectionNode => ({
      id: b.name,
      x: CONNECTION_CANVAS.colBucket,
      y: 540 + i * 88,
      label: trimLabel(b.name, 13),
      sub: `${b.objects} obj`,
      kind: "bucket",
    }),
  );

  const connSecrets = secrets.slice(0, 3).map(
    (s, i): ConnectionNode => ({
      id: s.name,
      x: CONNECTION_CANVAS.colSecret,
      y: 280 + i * 164,
      label: trimLabel(s.name, 13),
      sub: `v${s.versionId.slice(0, 6)}`,
      kind: "secret",
    }),
  );

  const connInfraNodes = buildInfraNodes(infra, infraOrderIds, infraNodePositions);
  const infraLane = buildInfraLane(connInfraNodes);
  const infraRoute = {
    x: infraLane.x + infraLane.width / 2,
    y: infraLane.y - 26,
  };

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
        (queue?.approxVisible ?? 0) +
        (queue?.approxInFlight ?? 0) +
        (queue?.approxDelayed ?? 0);
      const activity = traceEdgeActivity.get(`gw::${gwId}→${queueName}`);
      return [{ from, to, active: total > 0, activity }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    path: laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount),
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
    path: laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount),
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
    path: laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount),
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

  const bucketToFunction = withLanes(
    infraConnections.flatMap((c) => {
      if (c.targetKind !== "s3-lambda") return [];
      const from = connBuckets.find((n) => n.id === c.sourceFunction);
      const fnName = c.targetId || c.targetName;
      const to = connFunctions.find((n) => n.id === fnName);
      if (!from || !to) return [];
      return [{ from, to }];
    }),
    (edge) => `${edge.from.id}→${edge.to.id}`,
  ).map((edge) => ({
    ...edge,
    id: `${edge.from.id}→${edge.to.id}`,
    path: laneAwarePath(edge.from, edge.to, edge.lane, edge.laneCount),
  }));

  const awsServiceKinds = [
    "apigw-sqs",
    "apigw-lambda",
    "s3-lambda",
    "sqs-lambda",
    "queue-dlq",
  ];

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
    path: infraLadderPath(edge.from, edge.to, edge.lane, edge.laneCount, infraRoute),
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
        path: laneAwarePath(
          edge.from,
          edge.to,
          edge.lane,
          edge.laneCount,
          CONNECTION_CANVAS.nodeHalfWidth,
          CONNECTION_CANVAS.cacheHalfWidth,
        ),
      }))
    : [];

  const cacheActivity = aggregateActivity(
    [
      ...functionToCache.flatMap((edge) => (edge.activity ? [edge.activity] : [])),
      ...(traceEdgeActivity.get("cache::global")
        ? [traceEdgeActivity.get("cache::global")!]
        : []),
    ],
  );

  const cacheToSecret = connCacheExtension
    ? withLanes(
        [...connSecrets].sort((a, b) => a.y - b.y).map((secret) => ({
          from: connCacheExtension,
          to: secret,
          activity: cacheActivity,
        })),
        (edge) => `cache→${edge.to.id}`,
      ).map((edge) => ({
        ...edge,
        id: `cache→${edge.to.id}`,
        path: laneAwarePath(
          edge.from,
          edge.to,
          edge.lane,
          edge.laneCount,
          CONNECTION_CANVAS.cacheHalfWidth,
          CONNECTION_CANVAS.nodeHalfWidth,
        ),
      }))
    : [];

  return {
    hasData:
      gateways.length > 0 ||
      functions.length > 0 ||
      queues.length > 0 ||
      buckets.length > 0 ||
      secrets.length > 0 ||
      infra.length > 0,
    infraById: new Map(
      [...infraByNodeId.entries()].filter((entry): entry is [string, InfraProbe] => !!entry[1]),
    ),
    nodes: {
      gateways: connGateways,
      queues: connQueues,
      functions: connFunctions,
      buckets: connBuckets,
      secrets: connSecrets,
      infra: connInfraNodes,
      cacheExtension: connCacheExtension,
    },
    edges: {
      apigwToQueue,
      apigwToFunction,
      queueToFunction,
      queueToDlq,
      bucketToFunction,
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

export function activityStroke(
  activity: EdgeActivity | undefined,
  defaultStroke: string,
): string {
  if (!activity) return defaultStroke;
  return activity.hasError ? "destructive" : "primary";
}

export function activityOpacity(
  activity: EdgeActivity | undefined,
  base: number,
): number {
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
  const queueSpan = trace.spans.find((span) => span.kind === "queue");
  const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
  const cacheSpan = trace.spans.find(
    (span) => span.kind === "cache_extension" || span.kind === "cache-extension",
  );
  const secretsSpan = trace.spans.find(
    (span) => span.kind === "secrets" || span.kind === "secret",
  );

  const matchedGateway = trace.gatewayId
    ? model.nodes.gateways.find((node) => node.id === trace.gatewayId)
    : undefined;
  const matchedFunction = lambdaSpan
    ? model.nodes.functions.find((node) => node.id === lambdaSpan.name)
    : undefined;
  const matchedQueue = queueSpan
    ? model.nodes.queues.find((node) => node.id === queueSpan.name)
    : undefined;
  const matchedDlq = dlqSpan
    ? model.nodes.queues.find((node) => node.id === dlqSpan.name)
    : undefined;
  const matchedCache = cacheSpan || secretsSpan ? model.nodes.cacheExtension ?? undefined : undefined;
  const matchedSecret = secretsSpan
    ? model.nodes.secrets.find((node) => node.id === secretsSpan.name)
    : undefined;

  return [matchedGateway, matchedFunction, matchedQueue, matchedDlq, matchedCache, matchedSecret].filter(
    (node): node is ConnectionNode => !!node,
  );
}

export function nodeBounds(node: ConnectionNode): {
  left: number;
  right: number;
  top: number;
  bottom: number;
} {
  const halfWidth =
    node.kind === "infra"
      ? CONNECTION_CANVAS.infraHalfWidth
      : node.kind === "extension"
        ? CONNECTION_CANVAS.cacheHalfWidth
        : CONNECTION_CANVAS.nodeHalfWidth;

  const halfHeight =
    node.kind === "extension"
      ? CONNECTION_CANVAS.cacheHalfHeight
      : CONNECTION_CANVAS.nodeHalfHeight;

  return {
    left: node.x - halfWidth,
    right: node.x + halfWidth,
    top: node.y - halfHeight,
    bottom: node.y + halfHeight,
  };
}

export function findNodeAt(model: TopologyGraphModel, x: number, y: number): ConnectionNode | null {
  const candidates: ConnectionNode[] = [
    ...model.nodes.infra,
    ...model.nodes.secrets,
    ...(model.nodes.cacheExtension ? [model.nodes.cacheExtension] : []),
    ...model.nodes.functions,
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
  const zoomFactor = options.expanded
    ? safeWidth < 640
      ? 1.04
      : safeWidth < 1024
        ? 1.1
        : 1.18
    : 1;
  const scale = fitScale * zoomFactor;

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
    offsetX: clampOffsetAxis(
      safeWidth,
      contentWidth,
      transform.offsetX,
      overscrollX,
    ),
    offsetY: clampOffsetAxis(
      safeHeight,
      contentHeight,
      transform.offsetY,
      overscrollY,
    ),
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
): InfraNodePosition {
  return {
    x: clamp(
      x,
      CONNECTION_CANVAS.infraHalfWidth + 24,
      CONNECTION_CANVAS.width - CONNECTION_CANVAS.infraHalfWidth - 24,
    ),
    y: clamp(
      y,
      120,
      CONNECTION_CANVAS.height - CONNECTION_CANVAS.nodeHalfHeight - 24,
    ),
  };
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
    ...model.edges.queueToFunction,
    ...model.edges.queueToDlq,
    ...model.edges.bucketToFunction,
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

function trimLabel(label: string, max = 14): string {
  return label.length <= max ? label : `${label.slice(0, max - 1)}…`;
}

function infraNodeId(probe: InfraProbe): string {
  return `${probe.kind}-${probe.host}-${probe.port}`;
}

function buildInfraNodes(
  infra: InfraProbe[],
  infraOrderIds: string[],
  infraNodePositions: Record<string, InfraNodePosition>,
): ConnectionNode[] {
  const visible = infra.slice(0, 4).map((probe) => ({ id: infraNodeId(probe), probe }));
  if (visible.length === 0) return [];

  const byId = new Map(visible.map((entry) => [entry.id, entry.probe]));
  const orderedIds = [
    ...infraOrderIds.filter((id) => byId.has(id)),
    ...visible.map((entry) => entry.id).filter((id) => !infraOrderIds.includes(id)),
  ];

  const laneY = CONNECTION_CANVAS.height - 96;
  const horizontalMinX = 320;
  const horizontalMaxX = CONNECTION_CANVAS.width - 220;
  const count = orderedIds.length;
  const step = count > 1 ? (horizontalMaxX - horizontalMinX) / (count - 1) : 0;

  return orderedIds.map((id, i): ConnectionNode => {
    const probe = byId.get(id)!;
    const fallbackX =
      count === 1 ? (horizontalMinX + horizontalMaxX) / 2 : horizontalMinX + i * step;
    const fallbackY = laneY;
    const persistedPosition = infraNodePositions[id];
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
      y: CONNECTION_CANVAS.height - 170,
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

function buildTraceEdgeActivity(recentTraces: RequestTrace[], now: number): Map<string, EdgeActivity> {
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

    const lambdaSpan = trace.spans.find((span) => span.kind === "lambda");
    const queueSpan = trace.spans.find((span) => span.kind === "queue");
    const dlqSpan = trace.spans.find((span) => span.kind === "dlq");
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
    } else if (queueSpan && dlqSpan) {
      key = `dlq::${queueSpan.name}→${dlqSpan.name}`;
    } else if (queueSpan && lambdaSpan) {
      key = `queue::${queueSpan.name}→${lambdaSpan.name}`;
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
