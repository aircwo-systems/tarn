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
    redisViewerEnabled?: boolean;
    version?: string;
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
    stateMachines?: number;
    cognitoPools?: number;
    kmsKeys?: number;
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
  stateMachines?: StateMachineSummary[];
  cognitoPools?: CognitoPoolSummary[];
  kmsKeys?: KmsKeySummary[];
  ecs?: ECSOverview;
  infrastructure: InfraProbe[];
  connections?: InfraConnection[];
  recentTraces?: RequestTrace[];
  warnings?: string[];
}

export interface TraceSpan {
  id?: string;
  parentId?: string;
  startedAt?: string;
  durationNs?: number;
  kind: "gateway" | "lambda" | "topic" | "queue" | "dlq" | "eventbridge" | string;
  name: string;
  /** Emitting service of a distributed span; name says what the span did. */
  service?: string;
  durationMs: number;
  status: "ok" | "error" | "client_error" | string;
  meta?: Record<string, string>;
}

export interface RequestTrace {
  id: string;
  accountId?: string;
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
  /** full probed URL for http(s) services registered from the console */
  url?: string;
  /** "user" when registered from the console */
  source?: string;
}

/** A service registered from the console: an API, local app or LAN host. */
export type UserService = {
  name: string;
  /** http(s)://host[:port][/path] or tcp://host:port */
  url: string;
} & (
  | { scope: "account"; accountId: string }
  | { scope: "global"; accountId?: never }
);

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
  invocations?: number;
  lastInvokedAt?: string;
  version: string;
  lastModified: string;
  layers: number;
  tags?: Record<string, string>;
  tagCount: number;
  environmentKeys?: string[];
  kmsKeyArn?: string;
}

export interface FunctionEnvironmentResult {
  name: string;
  variables: Record<string, string>;
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
  processedCount: number;
  createdTimestamp: number;
  dlqName?: string;
  maxReceiveCount?: number;
  tags?: Record<string, string>;
  tagCount: number;
  recentMessages?: QueueMessageSummary[];
  /** SQS send-failure injection state (in-memory, not persisted). */
  disruptEnabled: boolean;
  disruptFailureRate?: number;
  disruptCode?: string;
}

export interface DisruptorRule {
  queue: string;
  enabled: boolean;
  failureRate: number;
  code: string;
  message?: string;
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
  retryCount?: number;
}

export interface QueueMessagesResponse {
  queue: string;
  messages: QueueMessageSummary[];
}

export interface SendQueueMessageInput {
  body: string;
  delaySeconds?: number;
  messageGroupId?: string;
  messageDeduplicationId?: string;
  attributes?: Record<string, { dataType: string; stringValue: string }>;
}

export interface SendQueueMessageResult {
  messageId: string;
  md5OfBody?: string;
}

export interface RedriveQueueResult {
  sourceQueue: string;
  targetQueue: string;
  moved: number;
}

export interface InvokeFunctionInput {
  payload: string;
  invocationType?: "RequestResponse" | "Event";
}

export interface InvokeFunctionResult {
  statusCode: number;
  payload: string;
  functionError?: string;
  logs?: string;
  requestId?: string;
  logGroup?: string;
  logStream?: string;
  durationMs: number;
  traceId?: string;
}

export interface SavedTestEvent {
  id: string;
  name: string;
  payload: string;
  createdAt: string;
  source?: string;
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
  kmsKeyId?: string;
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
  roleArn?: string;
  input?: string;
  inputPath?: string;
  inputPathsMap?: Record<string, string>;
  inputTemplate?: string;
  taskDefinition?: string;
}

export interface EventBridgeRuleSummary {
  name: string;
  arn: string;
  scheduleExpression: string;
  eventPattern?: string;
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

export interface StateMachineEventSummary {
  id: number;
  type: string;
  timestamp?: string;
  details?: Record<string, unknown>;
}

export interface StateMachineExecutionSummary {
  arn: string;
  name: string;
  status: "RUNNING" | "SUCCEEDED" | "FAILED" | "ABORTED" | "TIMED_OUT" | string;
  startDate?: string;
  stopDate?: string;
  output?: string;
  error?: string;
  cause?: string;
  traceId?: string;
  events?: StateMachineEventSummary[];
}

export interface StateMachineSummary {
  name: string;
  arn: string;
  type: "STANDARD" | "EXPRESS" | string;
  status: "ACTIVE" | "DELETING" | string;
  roleArn?: string;
  definition?: string;
  createdAt?: string;
  executions?: StateMachineExecutionSummary[];
}

/** ECS resources surfaced by the optional admin overview payload. */
export interface ECSOverview {
  clusters?: ECSClusterSummary[];
  services?: ECSServiceSummary[];
  tasks?: ECSTaskSummary[];
  taskDefinitions?: ECSTaskDefinitionSummary[];
}

export interface ECSClusterSummary {
  name: string;
  arn: string;
  status: string;
  runningTasks: number;
  pendingTasks: number;
  activeServices: number;
}

export interface ECSServiceSummary {
  name: string;
  arn: string;
  clusterArn: string;
  taskDefinitionArn: string;
  desiredCount: number;
  runningCount: number;
  pendingCount: number;
  launchType?: string;
  status: string;
}

export interface ECSTaskSummary {
  arn: string;
  clusterArn: string;
  taskDefinitionArn: string;
  group?: string;
  launchType?: string;
  lastStatus: string;
  desiredStatus: string;
  startedAt?: string;
  stoppedAt?: string;
  stoppedReason?: string;
  stopCode?: string;
  healthStatus?: string;
  containers?: ECSTaskContainerSummary[];
}

export interface ECSTaskContainerSummary {
  name: string;
  lastStatus: string;
  exitCode?: number;
  /** Why the container stopped or failed to start (e.g. a secret or image pull error). */
  reason?: string;
  healthStatus?: string;
  networkBindings?: ECSNetworkBinding[];
}

export interface ECSNetworkBinding {
  containerPort: number;
  hostPort: number;
  protocol?: string;
  bindIP?: string;
}

export interface ECSTaskDefinitionSummary {
  arn: string;
  taskDefinitionArn: string;
  name: string;
  family: string;
  revision: number;
  status: string;
}

export interface ECSTaskDefinitionContainer {
  name: string;
  image: string;
  essential: boolean;
}

export interface ECSTaskDefinitionDetail {
  family: string;
  revision: number;
  status: string;
  taskDefinitionArn: string;
  containers: ECSTaskDefinitionContainer[];
}

export interface ECSLaunchedTask {
  taskArn: string;
  clusterArn: string;
  taskDefinitionArn: string;
  lastStatus: string;
  desiredStatus: string;
  /** TaskFailedToStart when the task was created but its launch failed. */
  stopCode?: string;
  stoppedReason?: string;
}

export interface ECSRunFailure {
  arn?: string;
  reason?: string;
  detail?: string;
}

export interface ECSRunTaskResult {
  tasks: ECSLaunchedTask[];
  failures?: ECSRunFailure[];
}

export interface LogGroupSummary {
  name: string;
  createdAt: string;
  eventCount: number;
  streamCount: number;
  lastEvent?: string;
}

export interface LogEvent {
  traceId?: string;
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

// ── Cognito ──────────────────────────────────────────────────────

export interface CognitoPoolSummary {
  id: string;
  name: string;
  arn: string;
  issuer: string;
  jwksUrl: string;
  users: number;
  clients: number;
  groups: number;
  pendingCodes: number;
  mfaMode: "OFF" | "OPTIONAL" | "ON" | string;
  triggers?: string[];
  kmsKeyId?: string;
  domain?: string;
  tags?: Record<string, string>;
  created: string;
}

export interface KmsKeyUsage {
  encrypt: number;
  decrypt: number;
  dataKeys: number;
  reEncrypt: number;
  failures: number;
  lastUsed?: string;
  lastError?: string;
  lastErrorAt?: string;
}

/** A resource whose configuration names a KMS key. */
export interface KmsKeyReference {
  kind: "function" | "secret" | "queue" | "topic" | "table" | "userpool" | string;
  name: string;
  id?: string;
  field: string;
  tab: string;
}

export interface KmsKeySummary {
  id: string;
  arn: string;
  description?: string;
  state: "Enabled" | "Disabled" | "PendingDeletion" | string;
  manager: "CUSTOMER" | "AWS" | string;
  created: string;
  deletionDate?: string;
  aliases: string[];
  rotationEnabled: boolean;
  rotationPeriodDays?: number;
  tags?: Record<string, string>;
  tagCount: number;
  policy: string;
  /** Counted in memory since Tarn started. */
  usage: KmsKeyUsage;
  usedBy: KmsKeyReference[];
}

export interface CognitoClient {
  clientId: string;
  name: string;
  hasSecret: boolean;
  authFlows: string[];
  accessTokenValidity: string;
  idTokenValidity: string;
  refreshTokenValidity: string;
  readAttributes?: string[];
  writeAttributes?: string[];
  oauthFlows?: string[];
  oauthScopes?: string[];
  preventUserExistenceErrors: string;
  tokenRevocation: boolean;
  created: string;
}

export interface CognitoGroup {
  name: string;
  description?: string;
  precedence?: number;
  roleArn?: string;
  members: number;
  created: string;
}

export type CognitoUserStatus =
  | "UNCONFIRMED"
  | "CONFIRMED"
  | "FORCE_CHANGE_PASSWORD"
  | "RESET_REQUIRED"
  | string;

export interface CognitoUser {
  username: string;
  sub: string;
  status: CognitoUserStatus;
  enabled: boolean;
  email?: string;
  emailVerified: boolean;
  phone?: string;
  phoneVerified: boolean;
  groups: string[] | null;
  mfa?: string[];
  preferredMfa?: string;
  sessions: number;
  attributes: Record<string, string>;
  created: string;
  modified: string;
}

export interface CognitoPendingCode {
  poolId: string;
  username: string;
  purpose: string;
  code: string;
  attribute?: string;
  created: string;
  expires: string;
}

export interface CognitoPoolDetail extends CognitoPoolSummary {
  settings: {
    usernameAttributes?: string[];
    aliasAttributes?: string[];
    autoVerifiedAttributes?: string[];
    caseSensitive: boolean;
    passwordPolicy?: {
      MinimumLength?: number;
      RequireUppercase: boolean;
      RequireLowercase: boolean;
      RequireNumbers: boolean;
      RequireSymbols: boolean;
      TemporaryPasswordValidityDays: number;
    };
    mfaMode: string;
    smsMfa: boolean;
    emailMfa: boolean;
    adminCreateOnly: boolean;
    deletionProtection: string;
    customAttributes?: string[];
    requiredAttributes?: string[];
    tags?: Record<string, string>;
  };
  tarn: { issuerMode: string; fixedCode: boolean; tokenTtl?: string; triggers: boolean };
  lambdaConfig: Record<string, string>;
  preTokenGenerationVersion?: string;
  clientList: CognitoClient[] | null;
  groupList: CognitoGroup[] | null;
  userList: CognitoUser[] | null;
  usersTotal: number;
  usersOmitted: number;
  codes: CognitoPendingCode[];
  resourceServerScopes?: string[];
}

export type CognitoUserAction =
  | "confirm"
  | "enable"
  | "disable"
  | "reset-password"
  | "set-password"
  | "sign-out"
  | "delete";

export interface CognitoTokens {
  AccessToken: string;
  IdToken: string;
  RefreshToken?: string;
  ExpiresIn: number;
  TokenType: string;
}

export interface CognitoDecodedToken {
  header?: Record<string, unknown>;
  claims?: Record<string, unknown>;
  poolId?: string;
  knownPool: boolean;
  signatureValid: boolean;
  expired: boolean;
  expiresAt?: string;
  error?: string;
}

// ── DynamoDB Built-in Viewer ──────────────────────────────────────────

export interface DynamoAttributeValue {
  S?: string;
  N?: string;
  B?: string;
  BOOL?: boolean;
  NULL?: boolean;
  M?: Record<string, DynamoAttributeValue>;
  L?: DynamoAttributeValue[];
  SS?: string[];
  NS?: string[];
  BS?: string[];
}

export type DynamoItem = Record<string, DynamoAttributeValue>;

export interface DynamoKeySchemaItem {
  attributeName: string;
  keyType: "HASH" | "RANGE" | string;
}

export interface DynamoAttributeDefinition {
  attributeName: string;
  attributeType: "S" | "N" | "B" | string;
}

export interface DynamoProjection {
  projectionType?: string;
  nonKeyAttributes?: string[];
}

export interface DynamoThroughput {
  readCapacityUnits: number;
  writeCapacityUnits: number;
  numberOfDecreasesToday?: number;
}

export interface DynamoLSIDetail {
  indexName: string;
  indexArn?: string;
  keySchema: DynamoKeySchemaItem[];
  projection: DynamoProjection;
  itemCount: number;
  indexSizeBytes: number;
}

export interface DynamoGSIDetail {
  indexName: string;
  indexArn?: string;
  indexStatus?: string;
  keySchema: DynamoKeySchemaItem[];
  projection: DynamoProjection;
  provisionedThroughput?: DynamoThroughput;
  itemCount: number;
  indexSizeBytes: number;
}

export interface DynamoTriggerIntegration {
  uuid: string;
  functionName: string;
  state: string;
  batchSize: number;
  eventSourceArn?: string;
  lastResult?: string;
  filterCriteria?: FilterCriteria;
  logGroup?: string;
}

export interface DynamoLambdaIntegration {
  functionName: string;
  runtime: string;
  state: string;
  envKey: string;
  matchType: string;
  logGroup?: string;
}

export interface DynamoECSIntegration {
  kind: "service" | "task-definition" | string;
  name: string;
  arn: string;
  clusterArn?: string;
  clusterName?: string;
  taskDefinitionArn: string;
  containerName: string;
  envKey: string;
  matchType: string;
  status: string;
  desiredCount?: number;
  runningCount?: number;
  logGroup?: string;
}

export interface DynamoStepFunctionIntegration {
  name: string;
  arn: string;
  type: string;
  status: string;
  matchType: string;
  stateNames: string[];
}

export interface DynamoKMSIntegration {
  keyId: string;
  arn: string;
  aliases: string[];
  description?: string;
  state: string;
}

export interface DynamoTableDetailResponse {
  table: {
    name: string;
    arn: string;
    id: string;
    status: string;
    createdDate: string;
    billingMode?: string;
    tableClass?: string;
    itemCount: number;
    tableSizeBytes: number;
    deletionProtectionEnabled: boolean;
    attributeDefinitions: DynamoAttributeDefinition[];
    keySchema: DynamoKeySchemaItem[];
    provisionedThroughput?: DynamoThroughput;
    localSecondaryIndexes: DynamoLSIDetail[];
    globalSecondaryIndexes: DynamoGSIDetail[];
    streamEnabled: boolean;
    streamViewType?: string;
    latestStreamArn?: string;
    latestStreamLabel?: string;
    sseDescription?: {
      status: string;
      sseType: string;
      kmsMasterKeyArn?: string;
    };
  };
  ttl: {
    attributeName?: string;
    status: string;
  };
  continuousBackups: {
    status: string;
    pointInTimeRecoveryStatus: string;
    earliestRestorableDateTime?: string;
    latestRestorableDateTime?: string;
  };
  kinesisDestinations: {
    streamArn: string;
    destinationStatus: string;
  }[];
  tags: Record<string, string>;
  stream?: {
    arn: string;
    label?: string;
    status: string;
    viewType?: string;
    shards: {
      shardId: string;
      startingSequenceNumber?: string;
      endingSequenceNumber?: string;
    }[];
  };
  integrations: {
    triggers: DynamoTriggerIntegration[];
    lambdas: DynamoLambdaIntegration[];
    ecs: DynamoECSIntegration[];
    stepFunctions: DynamoStepFunctionIntegration[];
    kmsKey?: DynamoKMSIntegration;
  };
}

export interface DynamoReadRequest {
  indexName?: string;
  keyConditionExpression?: string;
  filterExpression?: string;
  projectionExpression?: string;
  expressionAttributeNames?: Record<string, string>;
  expressionAttributeValues?: DynamoItem;
  limit?: number;
  exclusiveStartKey?: DynamoItem;
  scanIndexForward?: boolean;
  select?: string;
}

export interface DynamoReadResponse {
  items: DynamoItem[];
  count: number;
  scannedCount: number;
  lastEvaluatedKey?: DynamoItem;
}

export interface DynamoStreamRecord {
  eventID: string;
  eventName: "INSERT" | "MODIFY" | "REMOVE" | string;
  eventVersion: string;
  eventSource: string;
  awsRegion: string;
  dynamodb: {
    ApproximateCreationDateTime: number;
    Keys?: DynamoItem;
    NewImage?: DynamoItem;
    OldImage?: DynamoItem;
    SequenceNumber: string;
    SizeBytes: number;
    StreamViewType: string;
  };
}

export interface DynamoStreamRecordsResponse {
  streamArn: string;
  streamViewType?: string;
  shardId: string;
  records: DynamoStreamRecord[];
  nextShardIterator?: string;
}

// ── Redis Viewer ──────────────────────────────────────────────────────

export interface RedisAuthOptions {
  db?: number;
  username?: string;
  password?: string;
  tls?: boolean;
}

export interface RedisDBKeyspace {
  db: number;
  keys: number;
  expires: number;
  avgTtlMs: number;
}

export interface RedisServerInfo {
  version: string;
  engine: "redis" | "valkey" | string;
  mode: string;
  os: string;
  role: string;
  uptimeSeconds: number;
  connectedClients: number;
  usedMemoryBytes: number;
  usedMemoryHuman: string;
  usedMemoryPeakHuman: string;
  maxMemoryBytes: number;
  maxMemoryHuman: string;
  opsPerSec: number;
  keyspaceHits: number;
  keyspaceMisses: number;
  totalKeys: number;
  selectedDb: number;
  selectedDbKeys: number;
  databases: RedisDBKeyspace[];
  totalCommandsHandled: number;
}

export interface RedisTargetSummary {
  id: string;
  name: string;
  kind: string;
  host: string;
  port: number;
  status: string;
  latencyMs: number;
  version?: string;
  error?: string;
  probedAt: string;
  source?: string;
  tls?: boolean;
}

export interface RedisLambdaIntegration {
  functionName: string;
  runtime: string;
  state: string;
  evidence: string;
  source: string;
  logGroup?: string;
}

export interface RedisECSIntegration {
  kind: "service" | "task-definition" | string;
  name: string;
  arn: string;
  clusterArn?: string;
  clusterName?: string;
  taskDefinitionArn: string;
  containerName: string;
  envKey: string;
  status: string;
  logGroup?: string;
}

export interface RedisSecretIntegration {
  name: string;
  arn: string;
  description?: string;
}

export interface RedisTraceIntegration {
  id: string;
  startedAt: string;
  durationMs: number;
  status: number;
  method?: string;
  path?: string;
  reason: "redis-span" | "linked-function" | string;
}

export interface RedisIntegrations {
  lambdas: RedisLambdaIntegration[];
  ecs: RedisECSIntegration[];
  secrets: RedisSecretIntegration[];
  traces: RedisTraceIntegration[];
  logGroups: string[];
}

export interface RedisTargetDetailResponse {
  target: RedisTargetSummary;
  info?: RedisServerInfo;
  infoError?: string;
  integrations: RedisIntegrations;
}

export interface RedisKeySummary {
  key: string;
  type: string;
  ttlMs: number;
  memoryBytes?: number;
}

export interface RedisScanRequest extends RedisAuthOptions {
  pattern?: string;
  type?: string;
  cursor?: string;
  count?: number;
}

export interface RedisScanResponse {
  cursor: string;
  hasMore: boolean;
  keys: RedisKeySummary[];
  scannedCount: number;
  dbSize: number;
}

export interface RedisHashEntry {
  field: string;
  value: string;
}

export interface RedisZSetMember {
  member: string;
  score: number;
}

export interface RedisStreamEntry {
  id: string;
  fields: Record<string, string>;
}

export interface RedisKeyDetail {
  found: boolean;
  key: string;
  type: string;
  ttlMs: number;
  encoding?: string;
  memoryBytes?: number;
  length: number;
  truncated?: boolean;
  hasMore?: boolean;
  nextCursor?: string;
  nextOffset?: number;
  isBinary?: boolean;
  stringValue?: string;
  hashEntries?: RedisHashEntry[];
  listItems?: string[];
  setMembers?: string[];
  zsetMembers?: RedisZSetMember[];
  streamEntries?: RedisStreamEntry[];
}

export interface RedisMutateKeyRequest extends RedisAuthOptions {
  key: string;
  action: "ttl" | "hset" | "hdel" | "rpush" | "lpush" | "lset" | "lrem" | "sadd" | "srem" | "zadd" | "zrem" | "xadd" | "xdel";
  field?: string;
  value?: string;
  member?: string;
  score?: number;
  index?: number;
  streamId?: string;
  streamFields?: Record<string, string>;
  ttlSeconds?: number | null;
}

export interface RedisSetKeyRequest extends RedisAuthOptions {
  key: string;
  type: "string" | "hash" | "list" | "set" | "zset" | "stream";
  stringValue?: string;
  hashEntries?: RedisHashEntry[];
  listItems?: string[];
  setMembers?: string[];
  zsetMembers?: RedisZSetMember[];
  streamFields?: Record<string, string>;
  streamId?: string;
  ttlSeconds?: number | null;
  keepTtl?: boolean;
}

export interface RedisCommandResult {
  command: string;
  resultType: string;
  result: unknown;
  formatted: string;
  durationMs: number;
}

