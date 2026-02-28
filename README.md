# OpenStack

An open-source AWS cloud emulator for local development and testing. Built as a fast, developer-friendly alternative to LocalStack — permanently free under Apache 2.0.

**Current status:** API Gateway v2 (HTTP APIs), Lambda, SQS, and Secrets Manager services. API Gateway supports HTTP APIs with Lambda AWS proxy integrations, route matching, and local invoke URLs. Lambda has full container-based execution via AWS RIE, layers, tags, configuration updates, and **built-in Lambda Extensions support** (something LocalStack doesn't offer). SQS has standard and FIFO queues, long polling, batch operations, and message deduplication. Secrets Manager provides full CRUD with the AWS Parameters and Secrets Lambda Extension automatically injected into every Lambda container.

## Prerequisites

- **Go 1.22+** — [install](https://go.dev/dl/)
- **Docker** — running and accessible via `docker ps`

## Quick Start

### Build

```bash
make build
```

This produces `./build/openstack`.

### Install globally (optional)

```bash
make install
```

Installs `openstack` to your `$GOPATH/bin`.

### Start the server

```bash
./build/openstack start
```

```
   ____                   _____ __             __
  / __ \____  ___  ____  / ___// /_____ ______/ /__
 / / / / __ \/ _ \/ __ \ \__ \/ __/ __ '/ ___/ //_/
/ /_/ / /_/ /  __/ / / /___/ / /_/ /_/ / /__/ ,<
\____/ .___/\___/_/ /_//____/\__/\__,_/\___/_/|_|
    /_/

Region:   us-east-1
Endpoint: http://0.0.0.0:4566
Data Dir: /Users/you/.openstack/data
Services: apigatewayv2, lambda, sqs, secretsmanager
```

The API server listens on port **4566** by default (same as LocalStack for easy migration).

### Server options

```bash
./build/openstack start --port 4566 --region us-east-1 --data-dir ~/.openstack/data --ui
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--host` | `OPENSTACK_HOST` | `0.0.0.0` | Bind address |
| `--port` | `OPENSTACK_PORT` | `4566` | API port |
| `--data-dir` | `OPENSTACK_DATA_DIR` | `~/.openstack/data` | Persistent storage |
| `--region` | `OPENSTACK_REGION` | `us-east-1` | Emulated AWS region |
| `--ui` | `OPENSTACK_UI_ENABLED` | `false` | Enable built-in dashboard UI |
| `--ui-dir` | `OPENSTACK_UI_DIR` | `./ui/build` | Path to built dashboard assets |

### Flush resources

To clear provisioned resources from a running OpenStack instance:

```bash
./build/openstack flush
./build/openstack flush --tag feature=r10
./build/openstack flush --tag r10 --dry-run
```

### Dashboard UI (Bun + SvelteKit)

The repo now includes a dashboard frontend in `ui/` built with **Bun + SvelteKit**. It polls `GET /_openstack/admin/overview` to render a monitoring view for API Gateway, Lambda, SQS, and Secrets Manager resources.

```bash
# install deps once
make ui-install

# run the frontend in dev mode (serves on :4173)
make ui-dev
# optional: OPENSTACK_UI_PROXY_TARGET=http://localhost:4566 make ui-dev

# build static assets for the Go server
make ui-build

# start OpenStack with the dashboard enabled (serves UI at /)
./build/openstack start --ui --ui-dir ./ui/build
```

## API Gateway Usage

OpenStack provides an AWS-compatible API Gateway v2 HTTP API surface and a local invoke endpoint:

- Management API: `/v2/apis/...`
- Invoke path: `/_apigateway/{apiId}/{stage}/...`

### curl (management + invoke)

```bash
export ENDPOINT=http://localhost:4566
export ACCOUNT_ID=000000000000
export REGION=us-east-1

# Create HTTP API
api_id=$(curl -sS -X POST "$ENDPOINT/v2/apis" \
  -H "Content-Type: application/json" \
  -d '{"Name":"orders-api","ProtocolType":"HTTP"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["ApiId"])')

# Create Lambda integration (replace function ARN)
integration_id=$(curl -sS -X POST "$ENDPOINT/v2/apis/$api_id/integrations" \
  -H "Content-Type: application/json" \
  -d '{"IntegrationType":"AWS_PROXY","IntegrationUri":"arn:aws:lambda:us-east-1:000000000000:function:orders-handler","PayloadFormatVersion":"2.0","TimeoutInMillis":30000}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["IntegrationId"])')

# Create route
curl -sS -X POST "$ENDPOINT/v2/apis/$api_id/routes" \
  -H "Content-Type: application/json" \
  -d "{\"RouteKey\":\"GET /orders/{id}\",\"Target\":\"integrations/$integration_id\"}"

# Invoke through local API Gateway path
curl "$ENDPOINT/_apigateway/$api_id/\$default/orders/123"
```

### Example walkthrough

See `examples/apigateway-lambda` for a full setup and validation flow:

```bash
./examples/apigateway-lambda/setup.sh
./examples/apigateway-lambda/invoke.sh
```

For a feature-scoped dashboard demo, use `examples/feature-r10`. It provisions a tagged API Gateway, two Lambdas, an SQS queue, and a secret, all labeled with `feature=r10` so the UI can be filtered to that slice:

```bash
./examples/feature-r10/setup.sh
./examples/feature-r10/invoke.sh
./examples/feature-r10/cleanup.sh
```

The example accepts `API_STAGE`, but only `$default` is supported today because OpenStack auto-creates a single auto-deployed HTTP API stage.

### Supported API Gateway v2 operations

| Operation | Notes |
|-----------|-------|
| `CreateApi`, `GetApi`, `GetApis`, `UpdateApi`, `DeleteApi` | HTTP APIs only (`ProtocolType=HTTP`) |
| `CreateIntegration`, `GetIntegration`, `GetIntegrations`, `UpdateIntegration`, `DeleteIntegration` | Lambda `AWS_PROXY` only |
| `CreateRoute`, `GetRoute`, `GetRoutes`, `UpdateRoute`, `DeleteRoute` | Targets must be `integrations/{integrationId}` |
| `GetStage`, `GetStages`, `UpdateStage` | `$default` stage auto-created and auto-deployed |

## Lambda Usage

### CLI

**Create a function:**

```bash
# Package your handler into a zip first
zip function.zip index.js

./build/openstack lambda create \
  --name my-func \
  --runtime nodejs20.x \
  --handler index.handler \
  --zip function.zip \
  --env APP_ENV=staging \
  --env FEATURE_FLAG=true
```

**Invoke:**

```bash
./build/openstack lambda invoke --name my-func --payload '{"key": "value"}'
```

**List functions:**

```bash
./build/openstack lambda list
```

**Delete:**

```bash
./build/openstack lambda delete --name my-func
```

### AWS CLI

Point the AWS CLI at your local OpenStack instance:

```bash
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1

# Create
aws lambda create-function \
  --function-name my-func \
  --runtime nodejs20.x \
  --handler index.handler \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb://function.zip

# Create with tags
aws lambda create-function \
  --function-name my-func \
  --runtime nodejs20.x \
  --handler index.handler \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb://function.zip \
  --tags env=dev,team=backend

# Invoke
aws lambda invoke --function-name my-func --payload '{}' /dev/stdout

# List
aws lambda list-functions

# Update configuration (timeout, memory, env vars, etc.)
aws lambda update-function-configuration \
  --function-name my-func \
  --timeout 30 \
  --memory-size 256 \
  --environment '{"Variables":{"APP_ENV":"staging"}}'

# Update code
aws lambda update-function-code \
  --function-name my-func \
  --zip-file fileb://function.zip

# Delete
aws lambda delete-function --function-name my-func
```

### Kysely PostgreSQL Example

For a Lambda that uses Kysely to connect to PostgreSQL on port `5432`, use `examples/kysely-postgres-lambda`:

```bash
./examples/kysely-postgres-lambda/setup.sh
./examples/kysely-postgres-lambda/invoke.sh
```

### Tags

```bash
# List tags
aws lambda list-tags --resource arn:aws:lambda:us-east-1:000000000000:function:my-func

# Add/update tags
aws lambda tag-resource \
  --resource arn:aws:lambda:us-east-1:000000000000:function:my-func \
  --tags env=prod,version=2

# Remove tags
aws lambda untag-resource \
  --resource arn:aws:lambda:us-east-1:000000000000:function:my-func \
  --tag-keys env version
```

### Lambda Layers

```bash
# Publish a layer version
zip -r layer.zip python/
aws lambda publish-layer-version \
  --layer-name my-layer \
  --compatible-runtimes python3.12 python3.13 \
  --zip-file fileb://layer.zip

# List layers
aws lambda list-layers

# List versions of a layer
aws lambda list-layer-versions --layer-name my-layer

# Get a specific layer version
aws lambda get-layer-version --layer-name my-layer --version-number 1

# Create a function using a layer
aws lambda create-function \
  --function-name my-func \
  --runtime python3.12 \
  --handler index.handler \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb://function.zip \
  --layers arn:aws:lambda:us-east-1:000000000000:layer:my-layer:1

# Delete a layer version
aws lambda delete-layer-version --layer-name my-layer --version-number 1
```

Layer contents are mounted at `/opt` inside the container, matching AWS behavior.

### curl

```bash
# Health check
curl http://localhost:4566/_openstack/health

# List functions
curl http://localhost:4566/2015-03-31/functions

# Invoke
curl -X POST http://localhost:4566/2015-03-31/functions/my-func/invocations \
  -d '{"key": "value"}'

# Update configuration
curl -X PUT http://localhost:4566/2015-03-31/functions/my-func/configuration \
  -d '{"Timeout": 30, "MemorySize": 256}'

# Account settings
curl http://localhost:4566/2015-03-31/account-settings
```

## SQS Usage

### CLI

**Create a queue:**

```bash
./build/openstack sqs create-queue --name my-queue
./build/openstack sqs create-queue --name my-queue.fifo --fifo
```

**Send a message:**

```bash
./build/openstack sqs send --queue my-queue --body "hello world"
./build/openstack sqs send --queue my-queue.fifo --body "hello" --group-id g1 --dedup-id d1
```

**Receive messages:**

```bash
./build/openstack sqs receive --queue my-queue
./build/openstack sqs receive --queue my-queue --max 5 --wait 10
```

**List queues:**

```bash
./build/openstack sqs list
```

**Delete a queue:**

```bash
./build/openstack sqs delete-queue --name my-queue
```

### AWS CLI

```bash
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1

# Create queue
aws sqs create-queue --queue-name my-queue

# Create FIFO queue
aws sqs create-queue --queue-name my-queue.fifo \
  --attributes FifoQueue=true

# Send message
aws sqs send-message \
  --queue-url http://localhost:4566/000000000000/my-queue \
  --message-body "hello world"

# Receive messages
aws sqs receive-message \
  --queue-url http://localhost:4566/000000000000/my-queue

# Long polling (wait up to 20 seconds)
aws sqs receive-message \
  --queue-url http://localhost:4566/000000000000/my-queue \
  --wait-time-seconds 20

# Delete message (use receipt handle from receive)
aws sqs delete-message \
  --queue-url http://localhost:4566/000000000000/my-queue \
  --receipt-handle "..."

# Purge all messages
aws sqs purge-queue \
  --queue-url http://localhost:4566/000000000000/my-queue

# Get queue attributes
aws sqs get-queue-attributes \
  --queue-url http://localhost:4566/000000000000/my-queue \
  --attribute-names All

# List queues
aws sqs list-queues

# Delete queue
aws sqs delete-queue \
  --queue-url http://localhost:4566/000000000000/my-queue
```

### curl (SQS Query Protocol)

SQS uses form-encoded POST requests with XML responses:

```bash
# Create queue
curl -X POST http://localhost:4566 \
  -d "Action=CreateQueue&QueueName=my-queue"

# Send message
curl -X POST http://localhost:4566/000000000000/my-queue \
  -d "Action=SendMessage&MessageBody=hello+world"

# Receive message
curl -X POST http://localhost:4566/000000000000/my-queue \
  -d "Action=ReceiveMessage&MaxNumberOfMessages=1"

# List queues
curl -X POST http://localhost:4566 \
  -d "Action=ListQueues"
```

### SQS Operations

| Action | Description |
|--------|-------------|
| `CreateQueue` | Create standard or FIFO queue |
| `DeleteQueue` | Delete queue and all messages |
| `ListQueues` | List queues with optional prefix filter |
| `GetQueueUrl` | Get queue URL by name |
| `GetQueueAttributes` | Get queue attributes (incl. message counts) |
| `SetQueueAttributes` | Update queue attributes |
| `SendMessage` | Send a single message |
| `SendMessageBatch` | Send up to 10 messages |
| `ReceiveMessage` | Receive 1-10 messages (supports long polling) |
| `DeleteMessage` | Delete by receipt handle |
| `DeleteMessageBatch` | Delete up to 10 messages |
| `ChangeMessageVisibility` | Extend/reset visibility timeout |
| `PurgeQueue` | Delete all messages in queue |
| `TagQueue` | Add tags to queue |
| `UntagQueue` | Remove tags from queue |
| `ListQueueTags` | List queue tags |

### SQS -> Lambda Example (Queue Pump)

For an end-to-end queue processing demo, use the example bridge in `examples/sqs-lambda`.

It creates a queue and Lambda using the OpenStack CLI, then runs a local pump process that long-polls SQS and invokes Lambda with an SQS-style `Records` payload for each message.

```bash
# Setup queue + lambda
./examples/sqs-lambda/setup.sh

# Terminal A: start pump
./examples/sqs-lambda/pump.sh

# Terminal B: send test messages
./examples/sqs-lambda/send.sh
./examples/sqs-lambda/send.sh '{"orderId":"1002","status":"PAID"}'
```

## Secrets Manager Usage

### CLI

**Create a secret:**

```bash
./build/openstack secrets create --name my-secret --value "password123"
./build/openstack secrets create --name db-creds --value '{"user":"admin","pass":"secret"}' --description "Database credentials"
```

**Get a secret:**

```bash
./build/openstack secrets get --name my-secret
```

**List secrets:**

```bash
./build/openstack secrets list
```

**Update a secret:**

```bash
./build/openstack secrets update --name my-secret --value "new-password"
```

**Delete a secret:**

```bash
./build/openstack secrets delete --name my-secret
```

### AWS CLI

```bash
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1

# Create secret
aws secretsmanager create-secret \
  --name my-secret \
  --secret-string "password123"

# Get secret value
aws secretsmanager get-secret-value --secret-id my-secret

# Describe secret (metadata only)
aws secretsmanager describe-secret --secret-id my-secret

# Update secret value
aws secretsmanager put-secret-value \
  --secret-id my-secret \
  --secret-string "new-password"

# List secrets
aws secretsmanager list-secrets

# Tag a secret
aws secretsmanager tag-resource \
  --secret-id my-secret \
  --tags Key=env,Value=dev

# Delete secret
aws secretsmanager delete-secret \
  --secret-id my-secret \
  --force-delete-without-recovery
```

### curl (JSON-RPC Protocol)

Secrets Manager uses JSON-RPC style requests with `X-Amz-Target` header:

```bash
# Create secret
curl -X POST http://localhost:4566 \
  -H "Content-Type: application/x-amz-json-1.1" \
  -H "X-Amz-Target: secretsmanager.CreateSecret" \
  -d '{"Name":"my-secret","SecretString":"password123"}'

# Get secret value
curl -X POST http://localhost:4566 \
  -H "Content-Type: application/x-amz-json-1.1" \
  -H "X-Amz-Target: secretsmanager.GetSecretValue" \
  -d '{"SecretId":"my-secret"}'

# List secrets
curl -X POST http://localhost:4566 \
  -H "Content-Type: application/x-amz-json-1.1" \
  -H "X-Amz-Target: secretsmanager.ListSecrets" \
  -d '{}'
```

### Secrets Manager Operations

| Action | Description |
|--------|-------------|
| `CreateSecret` | Create a new secret |
| `GetSecretValue` | Retrieve secret value by name or ARN |
| `DescribeSecret` | Get secret metadata (no value) |
| `UpdateSecret` | Update secret value |
| `PutSecretValue` | Update secret value (alias) |
| `DeleteSecret` | Delete a secret |
| `ListSecrets` | List all secrets |
| `TagResource` | Add tags to a secret |
| `UntagResource` | Remove tags from a secret |

## Lambda Extensions (Secrets Cache)

OpenStack supports the **AWS Parameters and Secrets Lambda Extension** — a feature that LocalStack does not provide. This allows Lambda functions to retrieve secrets via `localhost:2773` inside the container, matching real AWS behavior.

### How it works

When `make build` runs, it cross-compiles a lightweight secrets-proxy binary for Linux (`build/secrets-proxy-linux`). This binary is bind-mounted into each Lambda container and started alongside the runtime entrypoint:

1. The proxy starts on port **2773** inside the container
2. Lambda code makes HTTP requests to `localhost:2773/secretsmanager/get?secretId=...`
3. The proxy forwards requests to the OpenStack Secrets Manager API on the host
4. The response matches the real AWS extension format

### Usage in Lambda code

```javascript
// Node.js — fetch secret from the extension (no SDK needed)
const http = require('http');

exports.handler = async () => {
  const secret = await new Promise((resolve, reject) => {
    http.get({
      hostname: 'localhost',
      port: 2773,
      path: '/secretsmanager/get?secretId=my-secret',
      headers: { 'X-Aws-Parameters-Secrets-Token': process.env.AWS_SESSION_TOKEN }
    }, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => resolve(JSON.parse(data)));
    }).on('error', reject);
  });

  return { statusCode: 200, body: secret.SecretString };
};
```

```python
# Python — fetch secret from the extension
import http.client, json, os

def lambda_handler(event, context):
    conn = http.client.HTTPConnection("localhost", 2773)
    conn.request("GET", "/secretsmanager/get?secretId=my-secret",
                 headers={"X-Aws-Parameters-Secrets-Token": os.environ.get("AWS_SESSION_TOKEN", "")})
    resp = conn.getresponse()
    secret = json.loads(resp.read())
    return {"statusCode": 200, "body": secret["SecretString"]}
```

### Extension API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/secretsmanager/get?secretId={id}` | Get secret value |
| `GET` | `/systemsmanager/parameters/get?name={name}` | SSM Parameters (not yet implemented) |

The `X-Aws-Parameters-Secrets-Token` header is required (set to `AWS_SESSION_TOKEN` env var).

### Zero configuration

The extension is **always injected** into every Lambda container. If your Lambda code doesn't use it, the proxy idles with minimal overhead. No flags, env vars, or opt-in needed — it just works.

### Secrets Cache Example

For a full secret + Lambda demo, use `examples/secrets-cache-lambda`.

This creates:

- secret `test` with value `test-secret`
- Lambda `cache-extension-lambda-test` that reads `test` from `localhost:2773`

```bash
# Setup secret + lambda
./examples/secrets-cache-lambda/setup.sh

# Invoke and print fetched secret
./examples/secrets-cache-lambda/invoke.sh
```

## Supported Runtimes

| Runtime | Image |
|---------|-------|
| `nodejs18.x` | `public.ecr.aws/lambda/nodejs:18` |
| `nodejs20.x` | `public.ecr.aws/lambda/nodejs:20` |
| `nodejs22.x` | `public.ecr.aws/lambda/nodejs:22` |
| `nodejs24.x` | `public.ecr.aws/lambda/nodejs:24` |
| `python3.9` — `python3.13` | `public.ecr.aws/lambda/python:*` |
| `provided.al2023` (Go, Rust) | `public.ecr.aws/lambda/provided:al2023` |
| `java11`, `java17`, `java21` | `public.ecr.aws/lambda/java:*` |
| `dotnet6`, `dotnet8` | `public.ecr.aws/lambda/dotnet:*` |
| `ruby3.2`, `ruby3.3` | `public.ecr.aws/lambda/ruby:*` |

Runtime images are pulled automatically on first function creation.

## API Endpoints

**Lambda** — REST/JSON under `/2015-03-31/`:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/_openstack/health` | Health check |
| `GET` | `/2015-03-31/account-settings` | Account limits and usage |
| `POST` | `/2015-03-31/functions` | Create function |
| `GET` | `/2015-03-31/functions` | List functions |
| `GET` | `/2015-03-31/functions/{name}` | Get function |
| `DELETE` | `/2015-03-31/functions/{name}` | Delete function |
| `PUT` | `/2015-03-31/functions/{name}/code` | Update function code |
| `PUT` | `/2015-03-31/functions/{name}/configuration` | Update function configuration |
| `POST` | `/2015-03-31/functions/{name}/invocations` | Invoke function |
| `GET` | `/2015-03-31/functions/{name}/tags` | List tags |
| `POST` | `/2015-03-31/functions/{name}/tags` | Add tags |
| `DELETE` | `/2015-03-31/functions/{name}/tags` | Remove tags |
| `POST` | `/2015-03-31/layers/{name}/versions` | Publish layer version |
| `GET` | `/2015-03-31/layers` | List layers |
| `GET` | `/2015-03-31/layers/{name}/versions` | List layer versions |
| `GET` | `/2015-03-31/layers/{name}/versions/{ver}` | Get layer version |
| `DELETE` | `/2015-03-31/layers/{name}/versions/{ver}` | Delete layer version |

**SQS** — Query protocol (form-encoded POST with `Action` parameter, XML responses):

| Route | Description |
|-------|-------------|
| `POST /` | Global SQS operations (CreateQueue, ListQueues, GetQueueUrl) |
| `POST /{accountId}/{queueName}` | Queue-specific operations (SendMessage, ReceiveMessage, etc.) |

Queue URLs follow the format: `http://localhost:4566/000000000000/{queueName}`

**Secrets Manager** — JSON-RPC protocol (POST with `X-Amz-Target` header, JSON responses):

| Route | Description |
|-------|-------------|
| `POST /` | All Secrets Manager operations (via `X-Amz-Target: secretsmanager.{Action}` header) |

`POST /` is multiplexed: if the `X-Amz-Target` header starts with `secretsmanager.`, it routes to Secrets Manager; otherwise it falls through to SQS.

## How It Works

```
┌──────────────────────────────────────────────────────────┐
│                 OpenStack API Server                      │  :4566
│  Lambda:   /2015-03-31/functions/...     (REST/JSON)      │
│  SQS:      POST / with Action=...        (Query/XML)      │
│  Secrets:  POST / with X-Amz-Target=...  (JSON-RPC)       │
└──────┬─────────────────┬──────────────────┬──────────────┘
       │                 │                  │
┌──────▼──────┐  ┌───────▼───────┐  ┌──────▼──────────┐
│Lambda Service│  │  SQS Service   │  │Secrets Service   │
│- extract zip │  │ - in-memory    │  │ - in-memory      │
│- resolve lyrs│  │ - std + FIFO   │  │ - CRUD + tags    │
│- ensure image│  │ - long poll    │  │ - ARN generation │
│- manage pool │  │ - visibility   │  │ - name/ARN       │
└──────┬──────┘  │ - dedup/reaper │  │   resolution     │
       │         └────────────────┘  └─────────┬────────┘
┌──────▼──────────────┐                        │
│ Container Engine     │                        │
│ - create/start       │                        │
│ - port mapping       │                        │
│ - warm pool reuse    │                        │
│ - inject extensions  │                        │
└──────┬──────────────┘                        │
       │                                        │
┌──────▼──────────────────────────────┐        │
│ Docker Container                     │        │
│ AWS Lambda Base Image + RIE          │        │
│ /var/task (code)  /opt (layers)      │        │
│                                      │        │
│  ┌──────────────────────────┐        │        │
│  │ Secrets Proxy  :2773     │ ◄──────┼────────┘
│  │ GET /secretsmanager/get  │        │  (via host.docker.internal)
│  │ → forwards to host:4566  │        │
│  └──────────────────────────┘        │
└──────────────────────────────────────┘
```

**Lambda:**
- Warm containers reused across invocations (10 min idle timeout)
- Cold starts pull image, create container, wait for RIE, then invoke
- Layers extracted and bind-mounted at `/opt`
- Per-function mutexes prevent duplicate cold starts
- Function code and config persist to disk
- Secrets extension proxy automatically injected (port 2773)

**SQS:**
- In-memory message store (transient, like real SQS)
- Standard queues: at-least-once delivery, best-effort ordering
- FIFO queues: exactly-once delivery, strict ordering within message groups
- Long polling: holds connection until messages arrive or timeout
- Visibility timeout prevents duplicate processing
- Background reaper handles message expiry and dedup cleanup

**Secrets Manager:**
- In-memory secret store with full CRUD
- ARN generation matching AWS format
- Secret resolution by name, full ARN, or partial ARN
- Tags, versioning, and binary secret support
- Parameters and Secrets Lambda Extension (proxy at `:2773` inside containers)

## Development

```bash
make build      # Build binary
make test       # Run tests with race detector
make vet        # Run go vet
make fmt        # Format code
make lint       # Run golangci-lint
make dev        # Build and start server
make start      # Starts openstack
```

## Docker

```bash
make docker-build
make docker-run
```

Build a single image with the dashboard assets baked in (enabled via Docker build arg):

```bash
make docker-build-ui
docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock openstack:0.1.0-dev-ui
```

Requires mounting the Docker socket so OpenStack can manage Lambda containers:

```bash
docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock openstack:0.1.0-dev
```

## License

GNU General Public License v3.0— see [LICENSE](LICENSE).
