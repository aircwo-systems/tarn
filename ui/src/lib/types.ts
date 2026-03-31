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
    topics: number;
    subscriptions: number;
    secrets: number;
    buckets: number;
    dynamodbTables?: number;
    dynamodbStreams?: number;
    logGroups: number;
    eventSourceMappings: number;
    eventBridgeRules: number;
  };
  gateways: GatewaySummary[];
  functions: FunctionSummary[];
  queues: QueueSummary[];
  topics: TopicSummary[];
  subscriptions: SubscriptionSummary[];
  secrets: SecretSummary[];
  buckets: BucketSummary[];
  dynamodbTables?: DynamoDBTableSummary[];
  dynamodbStreams?: DynamoDBStreamSummary[];
  eventSourceMappings: EventSourceMappingSummary[];
  eventBridgeRules?: EventBridgeRuleSummary[];
  infrastructure: InfraProbe[];
  connections?: InfraConnection[];
  recentTraces?: RequestTrace[];
  warnings?: string[];
}

export interface TraceSpan {
  kind: "gateway" | "lambda" | "topic" | "queue" | "dlq" | "eventbridge" | string;
  name: string;
  durationMs: number;
  status: "ok" | "error" | "client_error" | string;
  meta?: Record<string, string>;
}

export interface RequestTrace {
  id: string;
  correlationId?: string;
  startedAt: string;
  durationMs: number;
  status: number;
  method?: string;
  path?: string;
  gatewayId?: string;
  gatewayName?: string;
  spans: TraceSpan[];
}

export interface InfraProbe {
  name: string;
  kind: string;
  host: string;
  port: number;
  status: "connected" | "unreachable" | "refused" | string;
  latencyMs: number;
  version?: string;
  error?: string;
  probedAt: string;
}

export interface FilterCriteriaFilter {
  Pattern: string;
}

export interface FilterCriteria {
  Filters: FilterCriteriaFilter[];
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
  filterCriteria?: FilterCriteria;
}

export interface RouteDetail {
  routeKey: string;
  method?: string;
  path?: string;
  integrationType?: string;
  integrationTarget?: string;
  requestTemplates?: Record<string, string>;
  requestParameters?: Record<string, string>;
  /** v1 method-level params: "method.request.header.X-Foo" → required */
  methodRequestParams?: Record<string, boolean>;
  /** pre-populated body from the Lambda's events/ folder */
  bodyExample?: unknown;
}

export interface ProbeBody {
  label: string;
  body?: string; // JSON string, omitted for nil body
  headers?: Record<string, string>;
  malformed?: boolean;
}

export interface SchemaField {
  name: string;
  kind: "string" | "number" | "bool" | "enum" | "literal" | "array" | "object" | "unknown";
  format?: "datetime" | "email" | "url" | "uuid" | "";
  optional?: boolean;
  enum?: string[];
  literal?: string;
}

export interface SchemaExport {
  name: string;
  fields: SchemaField[];
  isHeader: boolean;
}

export interface ScanMatch {
  functionName: string;
  dir: string;
  schemasTs?: string;
  eventFiles?: string[];
  score: number;
  schemas?: SchemaExport[];
  probeBodies?: ProbeBody[];
  /** Method-specific probe sets — preferred over probeBodies when available. */
  probesByMethod?: Record<string, ProbeBody[]>;
}

export interface ScanSourceResponse {
  matches: ScanMatch[];
  unmatched: string[];
  discovered: string[];
}

export interface ChaosRoundExample {
  statusCode: number;
  body?: string;
  headers?: Record<string, string>;
  durationMs: number;
  /** Headers sent in this probe attempt */
  requestHeaders?: Record<string, string>;
  /** Body sent in this probe attempt */
  requestBody?: string;
  /** Probe label from schema-driven mode (e.g. "baseline", "enum:status=ACTIVE") */
  label?: string;
}

export interface ChaosRound {
  routeKey: string;
  method: string;
  path: string;
  statusCode?: number;
  headers?: Record<string, string>;
  body?: string;
  durationMs: number;
  /** >1 when iterative header/body discovery was used */
  attempts?: number;
  error?: string;
  /** All probe attempts — embedded as named Postman example responses */
  examples?: ChaosRoundExample[];
  /** Probe hit an enum validation error — user needs to supply the correct value */
  needsInput?: boolean;
  stuckFields?: string[];
  /** Known valid options for each stuck enum field */
  stuckOptions?: Record<string, string[]>;
}

export interface GatewaySummary {
  apiId: string;
  name: string;
  description?: string;
  protocolType: string;
  /** "v1" for REST API (original), "v2" for HTTP API */
  version: string;
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
  routeDetails?: RouteDetail[];
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
  approxStale: number;
  createdTimestamp: number;
  dlqName?: string;
  maxReceiveCount?: number;
  tags?: Record<string, string>;
  tagCount: number;
  recentMessages?: QueueMessageSummary[];
}

export interface TopicSummary {
  name: string;
  arn: string;
  fifo: boolean;
  subscriptions: number;
  createdTimestamp: number;
  tags?: Record<string, string>;
  tagCount: number;
}

export interface SubscriptionSummary {
  subscriptionArn: string;
  topicArn: string;
  topicName: string;
  protocol: string;
  endpoint: string;
  rawMessageDelivery: boolean;
  filterPolicy?: string;
  filterPolicyScope?: string;
}

export interface QueueMessageSummary {
  id: string;
  body: string;
  state: "visible" | "inflight" | "delayed" | string;
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
  valueType: "string" | "binary" | "empty" | string;
}

export interface BucketSummary {
  name: string;
  objects: number;
  totalSize: number;
  createdDate: string;
  previewObjects?: BucketObjectPreview[];
}

export interface BucketObjectPreview {
  key: string;
  size: number;
  lastModified: string;
  contentType: string;
}

export interface DynamoDBTableSummary {
  name: string;
  arn: string;
  status: string;
  createdDate: string;
  billingMode?: string;
  itemCount: number;
  keySchema: string;
  localIndexes: number;
  globalIndexes: number;
  streamEnabled: boolean;
  streamArn?: string;
  streamViewType?: string;
  latestStreamLabel?: string;
}

export interface DynamoDBStreamSummary {
  tableName: string;
  streamArn: string;
  streamLabel: string;
  streamStatus: string;
  streamViewType: string;
  createdDate: string;
  shardCount: number;
}

export interface EventSourceMappingSummary {
  eventSourceArn?: string;
  sourceType?: string;
  sourceName?: string;
  uuid: string;
  queueName: string;
  functionName: string;
  batchSize: number;
  state: string;
  lastResult: string;
  filterCriteria?: FilterCriteria;
}

export interface EventBridgeTargetSummary {
  id: string;
  arn: string;
  lastResult?: string;
  lastInvokedAt?: string;
}

export interface EventBridgeRuleSummary {
  name: string;
  arn: string;
  scheduleExpression: string;
  state: "ENABLED" | "DISABLED" | string;
  description?: string;
  lastRunAt?: string;
  nextRunAt?: string;
  lastResult?: string;
  targets?: EventBridgeTargetSummary[];
}

export interface EventBridgeFireResult {
  ruleName: string;
  traceId?: string;
  firedAt: string;
  targets: number;
  successful: number;
  failed: number;
}

export interface EventBridgeRaceResult {
  sessionId: string;
  ruleName: string;
  runs: number;
  concurrency: number;
  successful: number;
  failed: number;
  traceIds?: string[];
  startedAt: string;
  finishedAt: string;
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
