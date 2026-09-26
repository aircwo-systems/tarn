# AWS parity and performance audit

Date: 2026-09-26
Scope: operations missing per emulated service, then reliability and performance under sustained load.

Coverage was taken from the dispatch tables in `internal/api/*/handler.go` and the route registrations in `internal/api/server.go`. Performance findings cite the code path. One finding (SQS FIFO) was reproduced with a throwaway test.

## Part 1: missing operations

Priority reflects how often real applications and Terraform/CDK deployments hit the operation.

### DynamoDB (highest impact)

| Missing | Why it matters |
|---|---|
| `BatchWriteItem`, `BatchGetItem` | Used by nearly every DynamoDB app, the Document Client and seed scripts. |
| `TransactWriteItems`, `TransactGetItems` | Idempotency and multi-item consistency patterns (Powertools idempotency, outbox). |
| `ExecuteStatement`, `BatchExecuteStatement`, `ExecuteTransaction` | PartiQL clients. |
| `DescribeLimits`, `DescribeEndpoints` | Called by some SDKs and tools on startup. |
| `CreateBackup`, `ListBackups`, `DescribeBackup`, `DeleteBackup`, `RestoreTableFromBackup` | Lower priority. |

### API Gateway

Both versions are core Tarn surfaces, and several gaps break Terraform applies.

**HTTP API (v2)**

| Missing | Why it matters |
|---|---|
| `CreateStage`, `DeleteStage` (`POST`/`DELETE /v2/apis/{id}/stages`) | Terraform `aws_apigatewayv2_stage`. Only list, get and patch exist. |
| Authorizers (`/v2/apis/{id}/authorizers`) | JWT and Lambda authorizers: `aws_apigatewayv2_authorizer`. |
| Deployments (`/v2/apis/{id}/deployments`) | `aws_apigatewayv2_deployment`. |
| Route responses, integration responses, models, custom domains and API mappings | Lower priority. |

**REST API (v1)**

| Missing | Why it matters |
|---|---|
| `PUT /restapis/{id}` (OpenAPI import) and `PATCH /restapis/{id}` | `aws_api_gateway_rest_api` with `body`, and any update to the API itself. |
| `PATCH` on stages, resources and deployments | Stage variables, logging and method settings updates. |
| Authorizers | Lambda and Cognito authorizers. |
| API keys, usage plans, usage plan keys | Common for x-api-key protected routes. |
| Models, request validators, gateway responses, custom domains and base path mappings | Lower priority. |

### Lambda

Tagging, aliases, versions, layers, permissions and concurrency are present.

| Missing | Why it matters |
|---|---|
| Asynchronous invocation semantics | `InvocationType: Event` runs synchronously and waits for the function (`lambda/service.go:635`). There are no async retries, `MaximumRetryAttempts`, on-failure DLQ, or destinations. |
| `Put/Get/Update/Delete/ListFunctionEventInvokeConfig(s)` (2019-09-25) | Terraform `aws_lambda_function_event_invoke_config`. It currently hits the NotFound catch-all. |
| Function URLs: `Create/Get/Update/Delete/ListFunctionUrlConfig(s)` (2021-10-31) | Common for simple HTTP functions. |
| `GetFunctionConcurrency` (2019-09-30) | Only PUT/DELETE exist, under 2017-10-31. |
| `InvokeWithResponseStream` | Streaming handlers. |
| `GetLayerVersionByArn`, `Add/Get/RemoveLayerVersionPermission` | Cross-account layers and CDK. |
| Provisioned concurrency, runtime management config, recursion config, code signing config CRUD | Lower priority. Mostly Terraform refresh noise. |

### SQS

| Missing | Why it matters |
|---|---|
| `ChangeMessageVisibilityBatch` | Used by batch consumers and Powertools batch processing. |
| `StartMessageMoveTask`, `ListMessageMoveTasks`, `CancelMessageMoveTask` | DLQ redrive. |
| `ListDeadLetterSourceQueues` | |
| `AddPermission`, `RemovePermission` | |

### S3

| Missing | Why it matters |
|---|---|
| Range GET (`Range` header) | Resumable downloads and media. `getObject` ignores it. |
| Conditional requests (`If-Match`, `If-None-Match`, `If-Modified-Since`) | Optimistic locking and cache validation. |
| `ListObjects` (v1) | Every bucket GET returns the V2 shape. |
| `ListObjectVersions` | Versioning can be enabled but versions can't be listed. |
| `ListMultipartUploads`, `ListParts`, `UploadPartCopy` | Large copies and multipart resume (the SDK's managed uploader). |
| `GetObjectAttributes`, object ACL get/put, object lock retention and legal hold | Lower priority. |

### Step Functions

| Missing | Why it matters |
|---|---|
| `SendTaskSuccess`, `SendTaskFailure`, `SendTaskHeartbeat` | Required for `.waitForTaskToken` integrations. |
| `CreateActivity`, `GetActivityTask`, `DescribeActivity`, `DeleteActivity`, `ListActivities` | |
| `StartSyncExecution` | Express workflows. |
| `DescribeStateMachineForExecution`, versions and aliases, Map runs | |

### Secrets Manager

| Missing | Why it matters |
|---|---|
| `BatchGetSecretValue` | Newer SDK pattern for loading config. |
| `RestoreSecret`, `ListSecretVersionIds`, `UpdateSecretVersionStage` | |
| `RotateSecret`, `CancelRotateSecret` | |
| `GetRandomPassword` | Used by CDK and Terraform. |
| `PutResourcePolicy`, `DeleteResourcePolicy`, `ValidateResourcePolicy` | Only `GetResourcePolicy` exists. |

### EventBridge

| Missing | Why it matters |
|---|---|
| `CreateEventBus`, `DescribeEventBus`, `ListEventBuses`, `DeleteEventBus` | Custom buses are standard in event-driven apps. |
| `TestEventPattern` | Pattern debugging. |
| Archives and replays, connections and API destinations, `PutPermission` | Lower priority. |

### IAM

| Missing | Why it matters |
|---|---|
| `CreatePolicy`, `GetPolicy`, `GetPolicyVersion`, `ListPolicyVersions`, `DeletePolicy`, `ListPolicies` | Terraform `aws_iam_policy` plus `aws_iam_role_policy_attachment` is the most common IAM pattern. Roles alone are not enough. |
| Users, groups, access keys | Low priority for a local emulator. |

### ECS

| Missing | Why it matters |
|---|---|
| `UpdateCluster`, `PutClusterCapacityProviders`, `DescribeCapacityProviders` | Terraform cluster refresh. |
| `ExecuteCommand`, task sets, container instances, account settings | Lower priority. |

### SNS (operations only)

`ConfirmSubscription`, `AddPermission`, `RemovePermission` and the data protection policy calls are missing. The `http`, `https` and `email` protocols are accepted but silently dropped at publish time.

## Part 2: reliability and performance under sustained load

Ordered by expected impact. P0 means it degrades or breaks under sustained load today.

### P0-1: Lambda log ingestion re-reads the whole container log on every invoke

**Status: partly fixed** (`e768249`, `5ebf541`). Tarn now asks Docker only for output since the last ingested line, the Tail result covers only the current invocation, and Docker log files are capped at 10 MB × 2. Docker still decodes its log file from the start on every read, so the per-invoke cost is bounded by the cap (about 20 MB) rather than removed. Following each container's log stream once is the complete fix.

`invokeWithRetry` calls `ingestContainerLogs` after every invoke (`lambda/service.go:765`). That calls `ContainerLogs` with no `Since` (`engine/container.go`), downloads the container's full stdout/stderr from Docker, and slices it by a byte offset. A warm container lives for up to 10 minutes, so invoke N transfers and demuxes all output from invokes 1 through N-1. The cost grows quadratically over the container's life. `LogType: Tail` reads the full log a second time.

Lambda containers are also created without `LogConfig`, so Docker's json-file logs grow without bound on the user's disk.

Fix:
- Stream logs once per container: `Follow: true` in a goroutine started at cold start, feeding the logs service.
- Alternatively, pass `Since` with the last timestamp.
- Set `LogConfig{Type: "json-file", Config: {"max-size": "10m", "max-file": "2"}}`.

### P0-2: async invokes are synchronous, which cascades latency

- `InvocationType: Event` executes the function inline and only then returns 202.
- SNS `Publish` invokes each Lambda subscriber serially and inline (`sns/service.go:203`). A publish with three subscribers costs the sum of three function durations, including cold starts of up to 60s. The invoke uses the publish request's context: when the publisher times out and its SDK retries, the in-flight work is cancelled and repeated, and duplicate deliveries follow.
- EventBridge `PutEvents` also dispatches inline: `PutEvents`, then `dispatchEvent`, then `fireTargets`, one target at a time (`eventbridge/service.go:753`, `:1317`). It uses a detached timeout context, so it isn't cancelled by the caller, but the caller still waits for every matched target to finish.

Fix: add a per-account async invoke queue with a bounded worker pool, detached from the request context. Return 202 on enqueue, then add AWS's retry behaviour (two retries with backoff). A DLQ and destinations can follow.

### P0-3: SQS event source throughput is capped at BatchSize messages per second

The poller (`eventsource/poller.go:141`) makes one `ReceiveMessage` call per tick. The tick is at least 1s. It then invokes the function once for the batch and waits before the next tick. There is no drain loop and no fan-out. With the default batch size of 10, a mapping drains at most 10 messages per second, however much concurrency is configured. Sustained producers above that rate grow the backlog without bound. AWS scales pollers with queue depth.

Fix: when a poll returns a full batch, poll again immediately. Run up to `ScalingConfig.MaximumConcurrency` (default: `LambdaMaxConcurrency`) poll loops concurrently. Keep FIFO groups serialized.

### P0-4: SQS FIFO delivers the next message of a group while an earlier one is in flight

**Status: fixed** (`5b21464`).

This is a correctness bug. `ReceiveMessage` (`sqs/store.go:437`) skips invisible messages with `continue` *before* it records the group in `seenGroups`. The in-flight message therefore doesn't block its group.

Reproduced:

1. Send `m1` and `m2` to group `A`.
2. Receive and get `m1`.
3. Receive again and get `m2` while `m1` is still in flight.

AWS would return nothing on the second receive. Ordering guarantees break under any concurrency.

Fix: mark the group as seen for every non-deleted, unexpired message in the group, including invisible and delayed ones, before any skip.

### P0-5: HTTP WriteTimeout (300s) is shorter than the Lambda maximum (900s)

**Status: fixed** (`5e349c5`).

`api/server.go:163` has the comment "Lambda can run up to 15 min", but the value is 5 minutes. Synchronous invokes over 5 minutes have their connection cut.

`ReadTimeout` of 30s also cuts large S3 uploads on slow links.

Fix: `WriteTimeout: 0` with per-handler deadlines via `http.ResponseController`, or at least 15m+. Use `ReadHeaderTimeout` instead of `ReadTimeout`.

### P1-1: DynamoDB and SQS persistence rewrites the whole state every 250ms

**Status: flush on shutdown fixed** (`7ccabc3`) for all ten JSON-snapshot stores, with snapshot writes serialized. Whole-state rewrites remain open.

Any write marks the store dirty. Every 250ms the flusher `json.Marshal`s the *entire* store and rewrites one file (`dynamodb/store.go:158`, `sqs/store.go:983`). For DynamoDB the marshal runs under `s.mu.RLock`, so all writers stall for the whole marshal. Both the stall and the disk I/O grow with total data size, not with write rate. A 200 MB table under steady writes means about 800 MB/s of serialization.

Neither service flushes on shutdown either. `Stop()` doesn't call `Flush()`, so up to 250ms of acknowledged writes are lost on Ctrl-C.

Fix:
- Now: flush on stop.
- Next: snapshot a copy-on-write view outside the lock, or shard the file per table or queue.
- Longer term: use an append-only log or SQLite. SQLite is already a dependency via the trace store.

### P1-2: SQS receive and delete are linear scans

- `ReceiveMessage` walks every message, including in-flight, delayed and deleted ones.
- `DeleteMessage` finds the receipt handle by linear search.
- Deleted messages stay in the slice until the next reap.

With a 100k backlog, every receive and every delete costs 100k iterations under the queue lock.

Fix: keep a `receiptHandle → *msg` map, and a ready list plus an in-flight heap ordered by `VisibleAt`.

### P1-3: every invoke reads the function config from disk

`GetFunction` reads and unmarshals `config.json` on every invoke, twice when the function is Pending. `ExtractCode` takes the function's *exclusive* lock and stats the zip and marker file on every invoke, which briefly serializes concurrent invokes of one function.

Fix: cache the config in memory, invalidated on update, create or delete. Cache the extraction check keyed on the zip mtime.

### P1-4: log queries copy and sort everything under the store lock

`GetAllLogEvents` (`logs/store.go:461`) copies every matching event from every group and sorts it, all while holding `s.mu.RLock`. The dashboard's live tail repeats this every 2s. `PutLogEvents`, which runs on every invoke, waits behind it.

Each group also preallocates a 10k-event ring up front.

Fix:
- Take a k-way merge of the already-ordered per-group rings.
- Stop at `limit + cursor`.
- Copy out under the lock and sort outside it.
- Allocate rings lazily.

### P2: hardening

- **Request bodies are unbounded.** There are 28 `io.ReadAll(r.Body)` sites and only 1 `MaxBytesReader`. Cap JSON APIs at around 10 MB; Lambda's payload limit is 6 MB.
- **Container names can collide.** Lambda containers are named by millisecond timestamp, so two cold starts in the same millisecond collide and one create fails. Add a random suffix.
- **Every request logs to stdout.** One synchronous `log.Printf` per request is fine locally but noisy at high rates. Put it behind a verbosity flag.
- **Flushers never stop.** `startFlusher` goroutines (DynamoDB, SQS, event source) have no stop channel.

## Suggested order

1. Build a load harness first, so every fix has before and after numbers. It should cover sustained `Invoke`, `SendMessage` to an ESM-backed Lambda, `PutItem`/`Query`, and SNS fan-out.
2. P0-4 (FIFO), P0-5 (timeouts) and flush-on-stop from P1-1: small, contained correctness fixes.
3. P0-1 (log streaming plus log caps): the biggest CPU and disk win for Lambda-heavy users.
4. P0-2 and P0-3 (async queue, poller scaling): the throughput ceiling.
5. Parity: DynamoDB `BatchWriteItem`/`BatchGetItem`/`Transact*`, then API Gateway v2 stages and authorizers and the v1 import, then Step Functions task tokens and IAM managed policies.
6. P1 items, then the remaining parity gaps by service priority.
