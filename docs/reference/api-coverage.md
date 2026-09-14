# API coverage

Tarn implements **250+ AWS API actions** across 12 services, with service-specific compatibility fallbacks to keep Terraform and SDK workflows moving when optional APIs are probed.

## Coverage matrix

| Service | Protocol | Actions | Terraform Endpoint |
|---------|----------|--------:|-------------------|
| **SNS** | Query/XML | 16 | `sns` |
| **SQS** | Query + JSON | 16 | `sqs` |
| **Lambda** | REST/JSON | 30 | `lambda` |
| **S3** | REST/XML | 55+ | `s3` |
| **DynamoDB** | JSON | 27 | `dynamodb` |
| **Secrets Manager** | JSON-RPC | 10 | `secretsmanager` |
| **EventBridge** | JSON | 14 | `events` |
| **ECS** | JSON 1.1 | 17 | `ecs` |
| **Step Functions** | JSON (1.0) | 13 | `stepfunctions` |
| **API Gateway v2** | REST/JSON | 18 | `apigatewayv2` |
| **API Gateway v1** | REST/JSON | 19 | `apigateway` |
| **IAM** | Query/XML | 17 | default |
| **Event Source Mapping** | REST/JSON | 5 | `lambda` (shared) |

## ECS

The ECS handler uses `X-Amz-Target: AmazonEC2ContainerServiceV20141113.<Action>` on `POST /`.
It implements these 17 actions:

| Resource | Actions |
|---|---|
| Clusters | `CreateCluster`, `ListClusters`, `DescribeClusters`, `DeleteCluster` |
| Task definitions | `RegisterTaskDefinition`, `DescribeTaskDefinition`, `DeregisterTaskDefinition`, `ListTaskDefinitions` |
| Tasks | `RunTask`, `StopTask`, `ListTasks`, `DescribeTasks` |
| Services | `CreateService`, `UpdateService`, `DeleteService`, `ListServices`, `DescribeServices` |

The actions are implemented in `internal/api/ecs/handler.go` and backed by the persisted control
plane in `internal/ecs`. The root dispatcher recognizes ECS, and `internal/cli/server.go` creates
and registers the account-local ECS handler and runner. `RunTask` and `StopTask` require the task
runner to be configured. `ListTaskDefinitions` supports family-prefix filtering, status filtering,
ascending/descending revision order, and continuation tokens.

EventBridge ECS target types, validation, payload overrides, dispatch, the default per-rule
in-flight limit, and account-level runner wiring are implemented in `pkg/types/eventbridge.go`,
`internal/eventbridge/service.go`, and `internal/cli/server.go`. The admin overview and UI ECS
section are also implemented and wired to the account-local service. `tarn flush` includes ECS
resource teardown for the records it receives from the overview.

The local runner uses Docker for both reported launch types. Task-definition CPU and memory values
are applied to Docker CPU shares, NanoCPUs, memory, and memory reservations. `bridge`, `host`,
`none`, and the local `awsvpc` compatibility behavior are supported with validation for invalid
port combinations. IAM roles, service discovery, ALB routing, and API Gateway proxy routing are
not implemented. `UpdateService` is reconciled locally with bounded one-task-at-a-time
replacement when the task definition changes.

### ECS follow-up status

The low-to-medium follow-up work is complete in this worktree:

| Item | Status | Current behavior |
|---|---|---|
| ECS Docker E2E coverage | Implemented in the E2E harness | [`test/e2e_test.go`](../../test/e2e_test.go) includes `TestECSDockerTaskE2E` and `TestECSFullPipelineE2E` (EventBridge → ECS Node task → SQS → Lambda). The broad CI test command includes the package; it skips when Docker is unavailable. |
| `ListTaskDefinitions` | Implemented | The ECS handler exposes family-prefix/status filtering, sort order, and pagination backed by the persisted task-definition store. |
| CPU and memory enforcement | Implemented locally | ECS CPU units and MiB memory values map to Docker CPU shares/NanoCPUs and memory limits/reservations. Task-level values are distributed across containers without explicit values. |
| Network-mode handling | Implemented locally | `bridge`, `host`, `none`, and local `awsvpc` behavior map to Docker settings; unsupported port/network combinations are rejected. |

The admin ECS overview emits clusters, services, tasks, and task-definition revisions, so the live
`tarn flush` path can discover definitions directly.
The Step Functions `arn:aws:states:::ecs:runTask.sync` integration is implemented and waits for
task completion. ECS unit and handler tests use fakes. RunTask overrides are launch-time inputs
and are not stored on Task records; the runner rebuilds its container bookkeeping from persisted
runtime IDs and account-scoped Docker labels after a restart.

## Stub behavior

When Tarn receives an API action it doesn't explicitly implement, the fallback depends on the service protocol. Query and JSON-style services usually return an empty success response for Terraform compatibility, while Lambda and S3 use AWS-style "not configured" responses for unsupported sub-resources.

**Per-protocol stub format:**

| Protocol | Stub Response |
|----------|--------------|
| Query/XML (SNS, SQS, IAM) | `200 OK` with `<{Action}Response><{Action}Result/></...>` |
| JSON-RPC (EventBridge, Secrets) | `200 OK` with `{}` |
| JSON (DynamoDB, DynamoDB Streams) | `200 OK` with `{}` |
| JSON Wire (SQS v2) | `200 OK` with `{}` |
| JSON 1.1 (ECS) | `200 OK` with `{}` for unknown actions |
| REST/JSON (Lambda sub-resources) | `404` with `ResourceNotFoundException` |
| REST/XML (S3) | Specific error codes per sub-resource (e.g. `NoSuchCORSConfiguration`) |

All stubbed actions are logged: `[service] unhandled action (returning empty OK): ActionName`

## S3 bucket sub-resources

Terraform's S3 provider probes many bucket sub-resources during every plan/apply. Most are now fully implemented and persisted to disk. A few remain as stubs for Terraform compatibility:

| Sub-resource | Implementation | Notes |
|---|---|---|
| `?versioning` | Full | GET/PUT persisted |
| `?encryption` | Full | GET/PUT/DELETE persisted |
| `?cors` | Full | GET/PUT/DELETE persisted |
| `?logging` | Full | GET/PUT persisted |
| `?acl` | Full | GET/PUT persisted |
| `?tagging` | Full | GET/PUT/DELETE persisted |
| `?lifecycle` | Full | GET/PUT/DELETE persisted |
| `?policy` | Full | GET/PUT/DELETE persisted |
| `?publicAccessBlock` | Full | GET/PUT/DELETE persisted |
| `?ownershipControls` | Full | GET/PUT/DELETE persisted |
| `?object-lock` | Full | GET/PUT persisted |
| `?replication` | Stub | GET → `ReplicationConfigurationNotFoundError`; PUT/DELETE accept and discard |
| `?accelerate` | Stub | GET → `Suspended`; PUT accepts and discards |
| `?request-payment` | Stub | GET → `BucketOwner`; PUT accepts and discards |

## Protocol routing

All services share a single endpoint (`localhost:4566`). Requests are routed by:

1. **`X-Amz-Target` header:** EventBridge (`AWSEvents.*`), Step Functions (`AWSStepFunctions.*`), SQS JSON (`AmazonSQS.*`), Secrets Manager (`secretsmanager.*`), DynamoDB (`DynamoDB_20120810.*`), DynamoDB Streams (`DynamoDBStreams_20120810.*`), and ECS (`AmazonEC2ContainerServiceV20141113.*`)
2. **`Version` form parameter:** IAM (`2010-05-08`) and SNS (`2010-03-31`)
3. **URL path:** Lambda (`/2015-03-31/functions/`), S3 (`/_s3/`), and API Gateway (`/v2/apis/`, `/restapis/`)
4. **Fallback:** SQS query protocol, the default for `POST /`
