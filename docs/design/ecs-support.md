# ECS support in Tarn

Status: T1-T10 are implemented and wired for the local Docker-backed runtime. The low-to-medium
follow-up tranche and the Step Functions ECS `.sync` integration are also implemented locally.

The code contains ECS wire types, a persisted control plane, the JSON handler and root dispatch
branch, Docker task primitives, the task runner, log forwarding, service reconciliation, ECS
account wiring, ECS-aware `tarn flush` teardown, EventBridge ECS target code, and an ECS
overview/UI. The normal account bundle attaches ECS to the API, EventBridge, admin overview, and
background runner. ECS unit and handler tests use fake engine and runner implementations. The
Docker E2E harness includes `TestECSDockerTaskE2E`. CI runs the broad Go test command, but has no
separate ECS E2E step.

## Low-to-medium follow-up status

This table describes the implementation in the current worktree. "Implemented locally" denotes
Docker-backed behavior rather than production AWS parity.

| Improvement | Status | Evidence and current boundary |
|---|---|---|
| ECS Docker E2E coverage | Implemented in the E2E harness | [`TestECSDockerTaskE2E`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/test/e2e_test.go) covers a direct Docker task, while [`TestECSFullPipelineE2E`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/test/e2e_test.go) covers EventBridge → ECS Node task → SQS → Lambda. [`.github/workflows/ci.yml`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/.github/workflows/ci.yml) includes the package through `go test ./...`, but has no separate ECS E2E step. |
| `ListTaskDefinitions` API | Implemented | [`internal/api/ecs/handler.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/api/ecs/handler.go) exposes family-prefix/status filtering, revision sort order, and continuation tokens backed by [`internal/ecs/store.go`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/ecs/store.go). |
| CPU and memory enforcement | Implemented locally | ECS CPU units and MiB memory values map to Docker CPU shares/NanoCPUs and memory limits/reservations through [`TaskContainerSpec`](https://github.com/aircwo-systems/tarn/blob/develop-mvp/internal/engine/task.go). Task-level values are distributed across containers without explicit values. |
| Docker network-mode handling | Implemented locally | `bridge`, `host`, `none`, and local `awsvpc` compatibility behavior map to Docker settings, with invalid port/network combinations rejected. |

## Why it is not just "more Lambda"

`internal/engine` already owns a Docker client, image pulls, container create/start/stop, port
mapping and log reading. That is most of what ECS needs. What does **not** carry over is the
lifecycle model:

| | Lambda (current) | ECS (core implemented) |
|---|---|---|
| Container lifetime | per-invoke, reaped when idle | RunTask containers run to exit; service tasks are reconciled to `desiredCount` |
| Concurrency model | pool of identical warm envs, `Busy` flag | N replicas reconciled to `desiredCount` |
| Entrypoint | AWS RIE on `8080/tcp` | the image's own entrypoint |
| Image | keyed by `types.Runtime` | arbitrary registry reference |
| Logs | `ContainerLogs` read once after invoke | followed stream written to a CloudWatch-style group |

So ECS gets a **sibling runner**, not an extension of `internal/engine/pool.go`. `WarmPool`,
`AcquireIdle`, `Release` and the `Busy` flag are invoke-scoped and must stay that way.

## Components

### 1. `internal/ecs/` service and store

Follows the existing per-service pattern (`NewStore(cfg).Init()`, `NewService(...)`, JSON-on-disk
under the account data dir).

Entities:

- **Cluster**: name, ARN, status, and counts.
- **TaskDefinition**: family and revision, `containerDefinitions` (image, command, env,
  `portMappings`, `logConfiguration`), plus stored cpu/memory and `networkMode` fields. The local
  Docker runner applies CPU/memory values and maps the supported network modes to Docker.
- **Service**: cluster, task definition ARN, `desiredCount`, running and pending counts,
  launch type, and status. There is no load balancer target in the current service record.
- **Task**: ARN, cluster, task definition, `lastStatus` (`PROVISIONING` → `PENDING` →
  `RUNNING` → `STOPPED`), `desiredStatus`, per-container Docker ID and host ports.

### 2. Task runner

The implementation lives in `internal/ecs/runner.go` and uses the shared `*engine.Engine` Docker
client.

The runner implements a narrow interface, mirroring `LambdaInterface` at
`internal/eventbridge/service.go:33`:

```go
// TaskRunner defines the ECS behaviour required by callers (EventBridge, Step Functions).
type TaskRunner interface {
    RunTask(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error)
    StopTask(ctx context.Context, cluster, taskArn, reason string) error
}
```

The interface keeps future EventBridge and Step Functions consumers testable without Docker. It
also matches the pattern the repository uses for cross-service dependencies.

- **Image resolution.** `EnsureImageRef` handles arbitrary image references. It checks local
  images before pulling, so a locally built image does not require a registry.
- **Run.** The runner creates containers with the task definition's environment, command,
  entrypoint, and port mappings. It publishes declared TCP ports to ephemeral host ports and
  records the assigned ports on the Task. It also injects
  `AWS_ENDPOINT_URL=http://host.docker.internal:<port>` and the Docker host gateway so task code
  can reach Tarn's local services.
- **Labels.** Every task container gets `tarn.account`, `tarn.cluster`, and `tarn.task-arn`
  labels. Service tasks also get `tarn.service`. These labels scope Docker queries to the right
  account, cluster, and service.
- **Reconcile loop.** One ticker per account counts running service containers and starts or stops
  tasks to match `desiredCount`. It replaces tasks whose containers exit and performs bounded
  one-task-at-a-time replacement when `UpdateService` changes the task definition.
- **Log pump.** The runner follows each container's stream and writes lines through
  `CreateLogGroup` and `PutLogEvents`. The default group is `/ecs/<family>`; a task definition's
  `awslogs-group` option overrides it. The stream name is the task ID and container name.
- **Exit status.** `WaitTaskContainer` wraps Docker's `ContainerWait`, so a task record can retain
  the real exit code and distinguish it from a wait error.
- **Cleanup ordering.** Containers are not auto-removed. The runner reads the exit code, marks the
  container stopped, drains the log stream, and then removes the Docker container. The Task record
  remains available to `DescribeTasks` after cleanup.

### 3. `internal/api/ecs/handler.go`

ECS is `awsJson1_1` on `POST /` with target prefix `AmazonEC2ContainerServiceV20141113.`.
The root dispatcher in `internal/api/server.go` recognizes this prefix, and
`internal/api/ecs/handler.go` dispatches the implemented actions:

- Clusters: `CreateCluster`, `ListClusters`, `DescribeClusters`, `DeleteCluster`.
- Task definitions: `RegisterTaskDefinition`, `DescribeTaskDefinition`,
  `DeregisterTaskDefinition`.
- Tasks: `RunTask`, `StopTask`, `ListTasks`, `DescribeTasks`.
- Services: `CreateService`, `UpdateService`, `DeleteService`, `ListServices`,
  `DescribeServices`.

The handler returns an empty `200` response for unknown actions to match the repository's
compatibility behavior. `RunTask` and `StopTask` require a configured `TaskRunner`.

### 4. Wiring

- `HandlerSet` in `internal/api/server.go` has an `ECS *ecshandler.Handler` field, and the root
  dispatcher has an ECS branch.
- `initAccountBundle` in `internal/cli/server.go` constructs the ECS store, service, handler, and
  runner, registers the handler, starts the runner, and stops it before account shutdown. ECS API
  requests therefore reach the account-local service.
- `tarn flush` in `internal/cli/flush.go` has teardown handlers for ECS clusters, services, tasks,
  and task definitions. It reads the live ECS records from the admin overview, honors the existing
  filters and dry-run mode, and stops tasks before deleting services and clusters. The admin ECS
  overview includes all task-definition revisions, including inactive revisions, so live flush can
  discover them directly.

### 5. Reaching a task over HTTP

The local runner maps `bridge`, `host`, `none`, and `awsvpc` to Docker network settings. In bridge
and local awsvpc compatibility mode, each declared TCP port is bound to an ephemeral host port on
`127.0.0.1` and recorded on the Task's `NetworkBindings`. Host and none modes do not publish ports;
none mode also omits the Docker host gateway. API Gateway proxy routing is not implemented by the
ECS work.

Tarn does not create per-cluster Docker networks or provide service discovery. Those remain later
work if service-to-service discovery becomes necessary.

### 6. Observability and UI

- The X-Ray fallback work is complete. `ui/src/lib/trace-utils.ts` names an ECS trace from its
  `ecs` span and anchors nested spans to either a Lambda or ECS span. The X-Ray section handles ECS
  log links under `/ecs/<span name>`.
- The admin handler has an ECS overview seam, `initAccountBundle` attaches the account-local ECS
  service, and the UI has an `#ecs` tab, navigation entry, and ECS section.

## EventBridge to ECS RunTask

The EventBridge service now accepts ECS targets with `EcsParameters`, validates the cluster and
task definition ARNs, injects the event JSON into the `EVENT_PAYLOAD` environment variable for
each container override, and calls the shared task runner. A per-rule in-flight limit defaults to
10. `initAccountBundle` attaches the account-local runner to EventBridge.

It remains the first planned consumer because it needs only one-shot tasks, without service
discovery.

### Lifecycle shape

Three distinct shapes should stay distinct in the code:

- **Lambda**: create once, reuse many, invoke over HTTP to the RIE on `8080/tcp`. The container
  outlives the request.
- **ECS RunTask**: one container per event, runs to completion, and uses the exit code as the
  result.
  `docker run` semantics: create, start, wait for exit, drain logs, record status.
- **ECS Service**: `docker run -d` plus a supervisor loop reconciling to `desiredCount`.

EventBridge only needs the middle one.

### Payload delivery

Lambda targets pass event JSON straight to the handler. ECS targets have no payload channel.
AWS puts `EcsParameters` on the target and the event reaches the container through
`containerOverrides`, using command arguments or environment variables usually produced by an
`InputTransformer`.

`buildTargetPayload` in `internal/eventbridge/service.go` produces the event JSON, and the ECS
target path injects it into an override. The local form is one environment variable on each
overridden container.

`types.EventBridgeTarget` now carries `EcsParameters` with the cluster ARN, task definition ARN,
task count, launch type, and container overrides.

### What remains

`internal/eventbridge/service.go` has the ECS target branch and task-runner seam, and the account
factory attaches the already-created `ecsRunner` with `eventbridgeSvc.SetTaskRunner` before
EventBridge starts.

The target dispatcher refactor is complete in T2. T9 added the ECS branch without changing the
two EventBridge call sites.

### Timeouts and concurrency

The ECS dispatch path uses the existing rule-fire timeout while waiting for `RunTask`, and limits
in-flight ECS deliveries per rule to 10 by default. A fast-firing rule still starts a new task for
each accepted event until that limit is reached.

### Tracing

The EventBridge ECS path records an `ecs` span with the task definition family as the name and
uses the task-runner result to set duration and status.

## Phasing

0. **EventBridge target dispatcher.** Complete in T2. The two existing EventBridge call sites
   now use one kind-switching dispatcher.
1. **ECS core.** Complete through T7. This includes the wire types, persisted control plane, API
   handler, Docker task primitives, one-shot task lifecycle, log pump, labels, and service
   reconciliation.
2. **Account wiring and teardown.** Complete in T8. The account bundle constructs the ECS
   components, starts the runner, registers the handler, and includes ECS resources in `tarn
   flush` teardown.
3. **EventBridge ECS targets.** Complete in T9. `EcsParameters`, ARN validation, container
   overrides, task dispatch, a per-rule concurrency policy, and account-level runner wiring are
   present.
4. **Dashboard support.** Complete in T10. The admin ECS overview, account-level service wiring,
   and ECS UI section are present.
5. **Low-to-medium follow-ups.** Complete. Task-definition listing, CPU and memory enforcement,
   network-mode handling, and bounded service replacement are implemented for the local Docker
   runtime. The shared test package includes both a direct ECS Docker case and a full
   EventBridge → ECS → SQS → Lambda E2E case; CI has no dedicated ECS E2E step.
6. **Networking and other integrations.** Service discovery, ALB-style routing, API Gateway
   `HTTP_PROXY` to a task remain future work. The `states:::ecs:runTask.sync` Step Functions
   integration is implemented and waits for task completion.

## Work breakdown

Ten chunks. The dependency column records the original implementation order. T1-T7 are the ECS
core; T8-T10 are the integration work now present in source.

| ID | Status | Scope | Files | Depends on |
|---|---|---|---|---|
| T1 | done | ECS wire types and the `TaskRunner` seam | `pkg/types/ecs.go`, `pkg/types/ecs_test.go` | none |
| T2 | done | EventBridge target dispatcher refactor | `internal/eventbridge/service.go` | none |
| T3 | done | X-Ray ECS fallback chain | `ui/src/lib/components/sections/xray-section.svelte`, `ui/src/lib/trace-utils.ts` | none |
| T4 | done | ECS store and control-plane service | `internal/ecs/store.go`, `internal/ecs/service.go` | T1 |
| T5 | done | Docker engine task-container primitives | `internal/engine/container.go`, `internal/engine/task.go`, `internal/config/config.go` | T1 |
| T6 | done | ECS API handler and root dispatch branch | `internal/api/ecs/handler.go`, `internal/api/server.go` | T1, T4 |
| T7 | done | Task runner, log pump, and service reconciliation | `internal/ecs/runner.go` | T4, T5 |
| T8 | done | Account wiring and `tarn flush` teardown | `internal/cli/server.go`, `internal/cli/flush.go` | T4, T6, T7 |
| T9 | done | EventBridge ECS targets, overrides, and concurrency policy | `internal/eventbridge/service.go`, `pkg/types/eventbridge.go` | T2, T7 |
| T10 | done | Admin overview payload and ECS UI section | `internal/api/admin/`, `ui/src/lib/` | T4, T8 |

The original waves are complete through Wave 5 in source.

### Resolved in T1

T1 landed `pkg/types/ecs.go`. Four things it had to name that this doc left unnamed. Downstream
chunks use these rather than inventing parallels:

- **`NetworkBinding`** (`ContainerPort`, `HostPort`, `Protocol`, `BindIP`) is the runtime
  counterpart to a task definition's `PortMapping`, carried on `TaskContainer.NetworkBindings`.
  T5 and T7 record published host ports here. Section 5's host-port approach reads them from here.
- **`Failure`** (`Arn`, `Reason`, `Detail`) is the batched-error shape every ECS `Describe*`
  returns alongside its results. T4 and T6 use it.
- **`TaskContainer.ExitCode` is `*int64`**, so nil means "not yet exited" and marshals as absent.
  T7 preserves that distinction.
- **`StopTask` signatures diverge on purpose.** `TaskRunner.StopTask` takes
  `(cluster, taskArn, reason string)` positionally, while the wire API has a `StopTaskInput`
  struct. Do not unify them: the interface is the narrow consumer-facing seam, the struct is the
  wire shape. T6 unmarshals the struct and calls the interface.

`ECSService` carries only `Status`, `RunningCount` and `PendingCount`, not a per-deployment list.
`UpdateService` changes the stored task definition or desired count; the runner performs bounded
replacement from the current task records without persisting a separate deployment object.

Note `TaskContainer.ContainerID` is tagged `RuntimeId` and `ECSService.TaskDefinitionArn` is
tagged `TaskDefinition`. Both match the real AWS wire shapes; they are not mistakes.

### Resolved in T2

The two dispatch loops were **not** identical. Three real differences, now preserved behind a
`targetDispatchOptions` struct rather than unified:

- **`LastInvokedAt` source.** The event-pattern path stamps the real post-invoke time; the
  scheduled path stamps every target with the rule's fire-start time.
- **Early-failure spans.** The scheduled path records a span for targets that fail before
  reaching `Invoke` (bad ARN, bad payload); the event-pattern path records none.
- **Error span metadata.** The scheduled path adds an `error` key to a failed invoke's span
  `Meta`; the event-pattern path does not.

T9 added an `"ecs"` case to `targetKind` and a matching branch in `dispatchTarget` without
changing the two EventBridge call sites. Whether the three existing divergences above are bugs
worth collapsing is a separate question from ECS support.

### Resolved in T3 and T5

The X-Ray display path is `traceTitle` in `ui/src/lib/trace-utils.ts`. T3 added the `ecs` branch.
The same change also lets the retry-dedup check use an ECS compute span.

T3 also changed `buildWaterfall` in `trace-utils.ts` so nested DB, cache, and secrets spans anchor
to either a Lambda or ECS span.

Left alone deliberately: `topology-connection-model.ts` resolves spans against a fixed node-kind
graph with no ECS node type. Adding one is a structural graph change, not a fallback chain. That is
new scope, not part of T10.

**T5 boundary note for T7.** `StartContainer` hardcodes `8080` port detection, so T7 cannot reuse
it for multi-port task containers. T5's `CreateAndStartTaskContainer` combines create, start, and
port-binding resolution into one call because host ports must be read back from Docker after start.
T7 gets `[]types.NetworkBinding` from that call. `Engine` gained two fields,
`imageRefKnown` and `refPullMus`, mirroring the runtime-keyed caches but keyed by image reference.

`internal/config/config.go` gained `ECSDir()` and `ECSStatePath()`, matching the EventBridge
convention exactly. Purely additive.

### Resolved in T4

Three things T4 hit that constrain the chunks after it:

- **Service-to-task linkage is a convention, not a field.** `Task` has no service reference, so
  `ListTasks` filtering by service name uses `Group == "service:<name>"` via `serviceGroup()`. T7
  sets that Group when it creates a service task.
- **`Task` cannot persist run overrides.** `NewTaskRecord`'s overrides parameter is a documented
  no-op. T7 keeps them in memory. Open question: does the reconcile loop need them across a
  restart? If yes, that is an additive `pkg/types` field.
- **Missing wire-type pairs.** `pkg/types/ecs.go` has no `DeleteClusterInput`/`Output` or
  `DeregisterTaskDefinitionInput`/`Output`, and no list types for task-definition families or
  revisions. The handler uses local request/response shapes for the two implemented actions. The
  store supports listing, but the API does not expose those list operations.

The control-plane service carries its own mutex around every read-modify-write against the store.
The runner uses those methods from concurrent lifecycle goroutines and the reconcile loop.

### Implementation conventions

- JSON tags are the **AWS wire shape** (`PascalCase`), matching `pkg/types/eventbridge.go`.
- API errors use the per-service `ServiceError{Code, Message, HTTPStatus}` pattern, as in
  `internal/eventbridge/service.go`.
- Stores are JSON-on-disk under the account data dir, with `Init()` called by the caller.
- Cross-service dependencies are consumed through a narrow interface defined by the *consumer*,
  never a concrete `*Service`. See `LambdaInterface` at `internal/eventbridge/service.go:33`.
- ECS unit tests use fake engine and task-runner implementations, so they do not require a Docker
  daemon. The runner keeps Docker calls behind the `taskEngine` interface.

## Current limitations

- The runner uses local Docker for both reported launch types. `FARGATE` and `EC2` are reported
  distinctions, not separate execution environments.
- Task-definition CPU and memory values map to Docker resource limits locally. ECS CPU and memory
  semantics are simplified, and there is no host-capacity scheduler.
- Task-definition network modes map to Docker's local modes. Tarn does not create per-cluster
  Docker networks or provide service discovery.
- `ListTaskDefinitions` supports local family/revision listing, status filtering, sorting, and
  pagination, but does not model every AWS-specific lifecycle state.
- The admin ECS overview exposes clusters, services, tasks, and task-definition revisions. It does
  not expose full container-definition payloads because the dashboard and flush path only need
  resource identity and lifecycle metadata.
- The ECS Docker E2E case lives in the shared `test` package. CI includes that package through the
  broad Go test command, but does not provide a separate ECS E2E step. The package skips when a
  Docker daemon is unavailable.
- Service replacement is intentionally local and bounded: the runner stops one old task before
  starting its replacement, without deployment health checks or a persisted deployment history.
- `RunTask` overrides are consumed during launch but are not stored on the Task record. The runner's
  Docker-container bookkeeping is rebuilt from persisted runtime IDs and account-scoped Docker
  labels during startup. Overrides still are not persisted, so a service task that needs them would
  not be reproducible after restart.
- Multi-container tasks are marked stopped after all containers exit. The runner does not implement
  ECS essential-container semantics.
- IAM roles, ALB routing, API Gateway proxy routing, and service discovery are not implemented.

## Open questions

- Does Tarn build images from a local `Dockerfile`, or only run pre-built ones? Scanner-style
  fuzzy matching of task definitions to local source dirs (as `internal/collection/scanner.go`
  does for Lambda) would make the local-dev path much better, but it is scope.
- Whether to add local Dockerfile builds or source-directory matching for task images.
- Whether the runner should persist `RunTask` overrides so service tasks that need them can be
  reproduced after a restart.
