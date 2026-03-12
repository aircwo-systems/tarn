# OpenStack

An open-source AWS cloud emulator for local development and testing. Built as a fast, developer-friendly alternative to LocalStack — permanently free under Apache 2.0.

**Current status**
- API Gateway v2 (HTTP APIs) management + local invoke path
- API Gateway v1 (REST API) management + local invoke path
- Lambda (container-based execution via AWS RIE)
- S3 (path-style, `/\_s3/...`)
- SQS (query/XML + JSON wire protocol)
- Secrets Manager (JSON-RPC protocol)
- Event source mappings (SQS -> Lambda pollers)
- Built-in dashboard UI (SvelteKit)

## Quick Start

```bash
make build
./build/openstack start
```

Default endpoint: `http://127.0.0.1:4566` (binds to `0.0.0.0`, but clients should use IPv4 to avoid `::1` issues).

## Prerequisites

- Go 1.22+ (build)
- Docker (required for Lambda containers)

## Configuration

```bash
./build/openstack start \
  --host 0.0.0.0 \
  --port 4566 \
  --region us-east-1 \
  --data-dir ~/.openstack/data \
  --persist \
  --ui
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--host` | `OPENSTACK_HOST` | `0.0.0.0` | Bind address |
| `--port` | `OPENSTACK_PORT` | `4566` | API port |
| `--data-dir` | `OPENSTACK_DATA_DIR` | `~/.openstack/data` | Persistent storage |
| `--region` | `OPENSTACK_REGION` | `us-east-1` | Emulated AWS region |
| `--persist` | `OPENSTACK_PERSIST` | `true` | Persist non-Lambda services |
| `--ui` | `OPENSTACK_UI_ENABLED` | `false` | Enable built-in dashboard UI |
| `--ui-dir` | `OPENSTACK_UI_DIR` | `./ui/build` | Built UI assets path |

## CLI Overview

```bash
./build/openstack --help
./build/openstack start
./build/openstack version

# Lambda
./build/openstack lambda create --help
./build/openstack lambda invoke --help

# SQS
./build/openstack sqs create-queue --help
./build/openstack sqs send --help

# S3
./build/openstack s3 mb --help
./build/openstack s3 cp --help

# Secrets Manager
./build/openstack secrets create --help
./build/openstack secrets get --help

# Flush resources
./build/openstack flush --help
```

## AWS CLI / SDK / Terraform

```bash
export AWS_ENDPOINT_URL=http://127.0.0.1:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
```

Terraform provider endpoints should use `http://127.0.0.1:4566`.

## Dashboard UI

```bash
make ui-install
make ui-dev
make ui-build
./build/openstack start --ui --ui-dir ./ui/build
```

## Examples

- `examples/apigateway-lambda`
- `examples/feature-r10`
- `examples/terraform/develop-mvp-content-pipeline`
- `examples/terraform/develop-mvp-media-redis-pipeline`
- `examples/sqs-lambda`
- `examples/secrets-cache-lambda`

## How It Works

OpenStack runs a single HTTP server that multiplexes AWS-style APIs:

- Lambda API under `/2015-03-31/...`
- API Gateway v2 management under `/v2/apis/...`
- API Gateway v1 management under `/restapis/...`
- API Gateway invoke paths under `/_apigateway/{apiId}/{stage}/...` and `/_aws/execute-api/{apiId}/{stage}/...`
- SQS query API (`POST /` with `Action=...`) and JSON protocol (`X-Amz-Target: AmazonSQS.*`)
- S3 path-style under `/_s3/{bucket}/{key...}`
- Secrets Manager JSON-RPC (`X-Amz-Target: secretsmanager.*`)

Lambda runs in Docker using the AWS base images + RIE, with layers mounted at `/opt` and the Parameters/Secrets extension auto-injected on `localhost:2773`.

## Docs

- `/Users/arcwo/projects_26/OpenStack/docs/README.md`
- `/Users/arcwo/projects_26/OpenStack/docs/api-gateway.md`
- `/Users/arcwo/projects_26/OpenStack/docs/lambda.md`
- `/Users/arcwo/projects_26/OpenStack/docs/s3.md`
- `/Users/arcwo/projects_26/OpenStack/docs/sqs.md`
- `/Users/arcwo/projects_26/OpenStack/docs/secrets-manager.md`
- `/Users/arcwo/projects_26/OpenStack/docs/lambda-extensions.md`
- `/Users/arcwo/projects_26/OpenStack/docs/runtimes.md`

## Development

```bash
make build
make test
make vet
make fmt
make lint
make dev
make start
```

## Docker

```bash
make docker-build
make docker-run
```

Build a single image with dashboard assets:

```bash
make docker-build-ui
docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock openstack:0.1.0-dev-ui
```

## License

Apache 2.0 — see `LICENSE`.
