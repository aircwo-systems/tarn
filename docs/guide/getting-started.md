# Getting Started

Welcome to Tarn! This guide will help you get up and running in minutes.

::: warning Active Development
Tarn is under active development. Expect breaking changes as we finalize the MVP.

Known limitations:
- EventBridge: Scheduled rules only; event patterns are not supported yet
- S3: Path-style only; virtual-hosted style is not supported

Learn more about what we want to build with Tarn in the [project roadmap](https://github.com/aircwo-systems/tarn/blob/develop-docs/ROADMAP.md).
:::

## Quick Start

### Prerequisites

- **Go 1.26+** (for building from source)
- **Docker** (required for Lambda execution)

### Installation

```bash
# Build from source
make build && make start
```

The server will start on `http://127.0.0.1:4566`

### Point Your Tools

Configure the AWS CLI, SDK, or Terraform to use Tarn:

```bash
export AWS_ENDPOINT_URL=http://127.0.0.1:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
```

### Deploy a Function

Use Tarn commands directly by default. If you prefer AWS-compatible workflows, the `AWS CLI` tab uses `awslocal`.

::: code-group

```bash [Tarn]
tarn lambda create \
  --name hello-tarn \
  --runtime nodejs20.x \
  --handler index.handler \
  --zip ./function.zip

tarn lambda invoke \
  --name hello-tarn \
  --payload '{"hello":"tarn"}'
```

```bash [AWS CLI]
awslocal lambda create-function \
  --function-name hello-tarn \
  --runtime nodejs20.x \
  --handler index.handler \
  --role arn:aws:iam::000000000000:role/lambda-role \
  --zip-file fileb://function.zip

awslocal lambda invoke \
  --function-name hello-tarn \
  response.json
```

:::

### First Commands

::: code-group

```bash [Tarn]
# List Lambda functions
tarn lambda list
```

```bash [AWS CLI]
awslocal lambda list-functions
```

:::

## What's Next?

- **[Services Overview](/services/)** — See what AWS services are supported
- **[Lambda Guide](/services/lambda)** — Deploy and invoke functions
- **[Configuration](/guide/configuration)** — Customize ports, regions, storage
- **[Contributing](/guide/contributing)** — Help build Tarn
