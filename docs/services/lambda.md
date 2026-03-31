# Lambda

Deploy and invoke serverless functions in Tarn.

<span class="status-badge status-partial">Partial Support</span> — Core Lambda APIs, layers, and SQS/DynamoDB Streams event source mappings

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateFunction | Supported | Supports all runtimes (Node, Python, Go, etc.) |
| DeleteFunction | Supported | |
| GetFunction | Supported | |
| ListFunctions | Supported | |
| UpdateFunctionCode | Supported | |
| UpdateFunctionConfiguration | Supported | Timeout, memory, environment |
| Invoke | Supported | Sync and async, with payloads |
| PublishVersion | Supported | Creates a numbered version from $LATEST |
| CreateAlias | Supported | |
| GetAlias | Supported | |
| ListAliases | Supported | |
| UpdateAlias | Supported | |
| DeleteAlias | Supported | |
| PublishLayerVersion | Supported | For shared code |
| GetLayerVersion | Supported | |
| ListLayerVersions | Supported | |
| CreateEventSourceMapping | Supported | SQS queues and DynamoDB Streams |
| DeleteEventSourceMapping | Supported | |
| ListEventSourceMappings | Supported | |

## Features

### Container-Based Execution
Functions run in Docker containers using AWS Lambda base images and the Runtime Interface Emulator (RIE).

### Supported Runtimes
- Node.js (18, 20, 22, 24)
- Python (3.9, 3.10, 3.11, 3.12, 3.13)
- Go and Rust via `provided.al2023`
- Java (11, 17, 21)
- Ruby (3.2, 3.3)
- .NET (6, 8)

### Layers
Bundle shared code as layers for reuse across functions. Tarn exposes the AWS-compatible layer APIs, but there is not yet a native `tarn lambda publish-layer` command.

```bash
awslocal lambda publish-layer-version \
  --layer-name my-layer \
  --zip-file fileb://layer.zip \
  --compatible-runtimes nodejs20.x python3.11
```

### Event Source Mappings
Automatically invoke Lambda from SQS queues and DynamoDB Streams.

```bash
awslocal lambda create-event-source-mapping \
  --event-source-arn arn:aws:sqs:us-east-1:000000000000:my-queue \
  --function-name my-processor \
  --batch-size 10

awslocal lambda create-event-source-mapping \
  --event-source-arn arn:aws:dynamodb:us-east-1:000000000000:table/my-table/stream/2026-03-30T00:00:00.000 \
  --function-name my-stream-processor \
  --starting-position TRIM_HORIZON
```

### Environment Variables & Secrets
Pass secrets to functions automatically via the Lambda extension.

```bash
tarn lambda create \
  --name my-function \
  --runtime nodejs20.x \
  --handler index.handler \
  --zip function.zip \
  --env DB_SECRET=my-secret
```

Inside your function:
```javascript
const secret = await fetch(
  'http://localhost:2773/secretsmanager/get?secretId=my-secret',
  { headers: { 'X-Aws-Parameters-Secrets-Token': process.env.AWS_SESSION_TOKEN } }
).then(r => r.json())
```

## Examples

### Create and Invoke (AWS SDK)

<div class="example-block">
<div class="lang">JavaScript</div>

```javascript
import { LambdaClient, CreateFunctionCommand, InvokeCommand } from "@aws-sdk/client-lambda";
import fs from "fs";

const lambda = new LambdaClient({
  endpoint: "http://127.0.0.1:4566",
  region: "us-east-1",
  credentials: { accessKeyId: "test", secretAccessKey: "test" }
});

// Create function
const createRes = await lambda.send(new CreateFunctionCommand({
  FunctionName: "my-function",
  Role: "arn:aws:iam::000000000000:role/lambda-role",
  Code: { ZipFile: fs.readFileSync("function.zip") },
  Handler: "index.handler",
  Runtime: "nodejs20.x"
}));

console.log("Created:", createRes.FunctionArn);

// Invoke
const invokeRes = await lambda.send(new InvokeCommand({
  FunctionName: "my-function",
  Payload: JSON.stringify({ hello: "world" })
}));

const result = JSON.parse(invokeRes.Payload);
console.log("Result:", result);
```
</div>

### Deploy from the CLI

::: code-group

```bash [Tarn]
tarn lambda create \
  --name my-function \
  --runtime nodejs20.x \
  --handler index.handler \
  --zip ./function.zip

tarn lambda invoke \
  --name my-function \
  --payload '{"hello":"world"}'
```

```bash [AWS CLI]
awslocal lambda create-function \
  --function-name my-function \
  --runtime nodejs20.x \
  --handler index.handler \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb://function.zip

awslocal lambda invoke \
  --function-name my-function \
  response.json
```

:::

### Using Terraform

<div class="example-block">
<div class="lang">HCL</div>

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    lambda = "http://127.0.0.1:4566"
    sqs    = "http://127.0.0.1:4566"
    iam    = "http://127.0.0.1:4566"
  }
}

resource "aws_lambda_function" "example" {
  function_name = "my-function"
  role         = aws_iam_role.lambda.arn
  handler      = "index.handler"
  runtime      = "nodejs20.x"
  filename     = "function.zip"
}

resource "aws_iam_role" "lambda" {
  name = "lambda-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_sqs_queue" "events" {
  name = "lambda-events"
}

resource "aws_lambda_event_source_mapping" "sqs" {
  event_source_arn  = aws_sqs_queue.events.arn
  function_name     = aws_lambda_function.example.arn
  batch_size        = 10
}
```
</div>

DynamoDB stream mappings use the same `aws_lambda_event_source_mapping` resource with a DynamoDB stream ARN.

## Runtimes

Runtime images are pulled automatically on first function creation.

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

## Secrets Cache Extension

Tarn injects a lightweight Parameters/Secrets extension into every Lambda container. This matches the AWS extension API and allows `localhost:2773` lookups without any SDK changes.

- A small proxy binary is built for Linux and mounted into Lambda containers
- The proxy listens on port `2773` inside the container
- Requests are forwarded to the host Tarn Secrets Manager endpoint

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/secretsmanager/get?secretId={id}` | Get secret value |
| `GET` | `/systemsmanager/parameters/get?name={name}` | SSM parameters (not yet implemented) |

## Known Limitations

- Function timeout maximum: 900 seconds (15 minutes)
- Memory: 128MB - 10GB in 1MB increments (full range supported)
- No Lambda@Edge (CloudFront integration)
- No concurrent execution limits

## See Also

- [Event Source Mappings](/services/sqs#event-source-mappings)
- [Secrets Manager Integration](/services/secrets-manager)
