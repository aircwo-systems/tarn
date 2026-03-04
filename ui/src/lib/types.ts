export interface OverviewResponse {
  status: string;
  timestamp: string;
  services: string[];
  config: {
    region: string;
    accountId: string;
    endpoint: string;
    dataDir: string;
    uiEnabled: boolean;
  };
  counts: {
    gateways: number;
    functions: number;
    queues: number;
    secrets: number;
    buckets: number;
    logGroups: number;
    eventSourceMappings: number;
  };
  gateways: GatewaySummary[];
  functions: FunctionSummary[];
  queues: QueueSummary[];
  secrets: SecretSummary[];
  buckets: BucketSummary[];
  eventSourceMappings: EventSourceMappingSummary[];
  infrastructure: InfraProbe[];
  connections?: InfraConnection[];
  warnings?: string[];
}

export interface InfraProbe {
  name: string;
  kind: string;
  host: string;
  port: number;
  status: 'connected' | 'unreachable' | 'refused' | string;
  latencyMs: number;
  version?: string;
  error?: string;
  probedAt: string;
}

export interface InfraConnection {
  sourceFunction: string;
  targetId: string;
  targetName: string;
  targetKind: string;
  targetHost: string;
  targetPort: number;
  evidence: string;
  source: string;
}

export interface GatewaySummary {
  apiId: string;
  name: string;
  description?: string;
  protocolType: string;
  arn: string;
  apiEndpoint: string;
  defaultStage: string;
  invokeUrl: string;
  routes: number;
  integrations: number;
  stages: number;
  tags?: Record<string, string>;
  tagCount: number;
  routeKeys?: string[];
}

export interface FunctionSummary {
  name: string;
  arn: string;
  runtime: string;
  state: string;
  timeoutSec: number;
  memoryMB: number;
  codeSize: number;
  messagesProcessed: number;
  version: string;
  lastModified: string;
  layers: number;
  tags?: Record<string, string>;
  tagCount: number;
}

export interface QueueSummary {
  name: string;
  url: string;
  arn: string;
  fifo: boolean;
  visibilitySec: number;
  waitTimeSec: number;
  approxVisible: number;
  approxInFlight: number;
  approxDelayed: number;
  createdTimestamp: number;
  tags?: Record<string, string>;
  tagCount: number;
  recentMessages?: QueueMessageSummary[];
}

export interface QueueMessageSummary {
  id: string;
  body: string;
  state: 'visible' | 'inflight' | 'delayed' | string;
  sentAt: number;
  receiveCount: number;
}

export interface QueueMessagesResponse {
  queue: string;
  messages: QueueMessageSummary[];
}

export interface SecretSummary {
  name: string;
  arn: string;
  description?: string;
  versionId: string;
  tags?: Record<string, string>;
  tagCount: number;
  createdDate: string;
  lastChangedDate: string;
}

export interface SecretValueResult {
  name: string;
  value: string;
  valueType: 'string' | 'binary' | 'empty' | string;
}

export interface BucketSummary {
  name: string;
  objects: number;
  totalSize: number;
  createdDate: string;
}

export interface EventSourceMappingSummary {
  uuid: string;
  queueName: string;
  functionName: string;
  batchSize: number;
  state: string;
  lastResult: string;
}

export interface LogGroupSummary {
  name: string;
  createdAt: string;
  eventCount: number;
  streamCount: number;
  lastEvent?: string;
}

export interface LogEvent {
  timestamp: string;
  message: string;
  level: string;
  source?: string;
  streamName: string;
}

export interface LogGroupsResponse {
  groups: LogGroupSummary[];
}

export interface LogEventsResponse {
  events: LogEvent[];
  total: number;
  nextCursor?: string;
}
