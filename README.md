# Tarn

<p align="center">
  <img src="ui/static/favicon.svg" alt="Tarn logo" width="140"/>
</p>

<p align="center">
  <strong>Local cloud, free and fast.</strong>
</p>

<p align="center">
  <a href="https://github.com/aircwo-systems/tarn/actions/workflows/ci.yml"><img src="https://github.com/aircwo-systems/tarn/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/aircwo-systems/tarn/releases/latest"><img src="https://img.shields.io/github/v/release/aircwo-systems/tarn?include_prereleases&label=version" alt="Latest Release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License" /></a>
</p>

Open-source AWS cloud emulator for local development and testing. Single binary, zero config. Free forever under Apache 2.0.

## Supported Services

| Service | Protocol | Notes |
|---------|----------|-------|
| **API Gateway v2** | HTTP APIs | Management + local invoke |
| **API Gateway v1** | REST APIs | Management + local invoke |
| **Lambda** | Container-based | AWS RIE, layers, extensions, secrets cache |
| **S3** | Path-style | `/_s3/{bucket}/{key}` |
| **SQS** | Query/XML + JSON | Event source mappings (SQS -> Lambda) |
| **SNS** | Query/XML | Publish, subscribe, fanout to SQS + Lambda |
| **Secrets Manager** | JSON-RPC | Full CRUD, auto-injected cache extension |
| **EventBridge** | JSON | Scheduled rules (rate + cron), Lambda targets |

## Quick Start

```bash
make build
./build/tarn start
```

Endpoint: `http://127.0.0.1:4566`

Point the AWS CLI, SDK, or Terraform at it:

```bash
export AWS_ENDPOINT_URL=http://127.0.0.1:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
```

## Prerequisites

- **Go 1.26+** (build)
- **Docker** (required for Lambda containers)

## Configuration

```bash
./build/tarn start --port 4566 --region us-east-1 --persist --ui
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--host` | `TARN_HOST` | `0.0.0.0` | Bind address |
| `--port` | `TARN_PORT` | `4566` | API port |
| `--data-dir` | `TARN_DATA_DIR` | `~/.tarn/data` | Persistent storage |
| `--region` | `TARN_REGION` | `us-east-1` | Emulated AWS region |
| `--persist` | `TARN_PERSIST` | `true` | Persist state across restarts |
| `--ui` | `TARN_UI_ENABLED` | `false` | Enable dashboard UI |
| `--ui-dir` | `TARN_UI_DIR` | `./ui/build` | Built UI assets path |

## CLI

```bash
tarn start                          # Start the server
tarn version [--check]              # Show version / check for updates

tarn lambda create|invoke|list|delete
tarn sqs create-queue|send|receive|list
tarn sns create-topic|publish|subscribe|list
tarn s3 mb|cp|ls|rm
tarn secrets create|get|list|delete
tarn flush                          # Clear all resources
```

Run `tarn <command> --help` for details.

## Dashboard UI

```bash
make ui-install && make ui-build
./build/tarn start --ui
```

For development with hot-reload: `make ui-dev`

## Docker

```bash
# Server only
make docker-build
make docker-run

# With dashboard
make docker-build-ui
docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock tarn:0.1.0-dev-ui
```

## SNS

Full Simple Notification Service emulation using the AWS Query/XML protocol (`POST /` with `Action=...`).

**Supported operations:** CreateTopic, DeleteTopic, ListTopics, GetTopicAttributes, SetTopicAttributes, Subscribe, Unsubscribe, ListSubscriptions, ListSubscriptionsByTopic, GetSubscriptionAttributes, SetSubscriptionAttributes, Publish, TagResource, UntagResource, ListTagsForResource.

**Subscription protocols:**
- **SQS** — fanout with SNS envelope (or raw via `RawMessageDelivery`)
- **Lambda** — asynchronous invocation with SNS event record

**Features:** FIFO topics with deduplication, message attribute filtering (`FilterPolicy` on attributes or message body), subscription filter scopes, tag management.

## EventBridge

Scheduled rule execution via JSON protocol (`X-Amz-Target: AWSEvents.*`).

**Supported operations:** PutRule, DescribeRule, ListRules, DeleteRule, EnableRule, DisableRule, PutTargets, RemoveTargets, ListTargetsByRule, ListRuleNamesByTarget, TagResource, UntagResource, ListTagsForResource.

**Schedule expressions:**
- Rate: `rate(5 minutes)`, `rate(1 hour)`, `rate(7 days)`
- Cron: Full AWS 6-field syntax — ranges, lists, steps, `L`, `W`, `#`

**Targets:** Lambda functions with optional `InputTransformer` (InputPathsMap + InputTemplate). Up to 10 targets per rule.

> **Note:** Only scheduled rules are supported. Event patterns and `PutEvents` are not yet implemented.

## Secrets Cache Extension

Lambda functions automatically get the [AWS Parameters and Secrets Lambda Extension](docs/lambda-extensions.md) injected at startup. This provides a local HTTP cache on `localhost:2773` inside the container, matching the real AWS extension API.

```bash
# Inside your Lambda function code — no config needed
curl "http://localhost:2773/secretsmanager/get?secretId=my-secret" \
  -H "X-Aws-Parameters-Secrets-Token: $AWS_SESSION_TOKEN"
```

The extension binary (`secrets-proxy`) is mounted to `/opt/tarn/secrets-proxy` and started automatically. It forwards `GetSecretValue` requests to the main Tarn Secrets Manager API.

For local development outside Lambda containers:

```bash
./build/tarn start --expose-secrets-proxy --secrets-proxy-port 2773
```

## How It Works

A single HTTP server multiplexes AWS-style APIs on one port:

- Lambda: `/2015-03-31/functions/...`
- API Gateway v2/v1: `/v2/apis/...`, `/restapis/...`
- API Gateway invoke: `/_apigateway/{apiId}/{stage}/...`
- SQS: `POST /` with `Action=...` or `X-Amz-Target: AmazonSQS.*`
- SNS: `POST /` with `Action=...` (Query/XML)
- S3: `/_s3/{bucket}/{key}`
- Secrets Manager: `X-Amz-Target: secretsmanager.*`
- EventBridge: `X-Amz-Target: AWSEvents.*`

Lambda functions run in Docker using AWS base images + RIE, with layers at `/opt` and the secrets cache extension auto-injected on `localhost:2773`.

## Documentation

- [API Gateway](docs/api-gateway.md)
- [Lambda](docs/lambda.md) | [Runtimes](docs/runtimes.md) | [Extensions](docs/lambda-extensions.md)
- [S3](docs/s3.md)
- [SQS](docs/sqs.md)
- [Secrets Manager](docs/secrets-manager.md)

## Development

```bash
make build       # Build binary
make test        # Run tests with race detector
make lint        # Run golangci-lint
make dev         # Build and start on :4566
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 — see [LICENSE](LICENSE).
