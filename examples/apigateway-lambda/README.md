# API Gateway -> Lambda Example

This example creates an HTTP API (v2), attaches Lambda AWS proxy integrations, and invokes the API through the local path-based endpoint.

Today, OpenStack auto-creates a single API Gateway stage: `$default`. The example exposes that stage as `API_STAGE` so the stage is explicit in the setup flow and state file.

## What gets created

- Lambda function: `apigw-orders-handler`
- API Gateway HTTP API: `apigw-orders-api`
- Routes:
  - `GET /hello`
  - `GET /orders/{id}`

## Prerequisites

- OpenStack server running at `http://localhost:4566`
- OpenStack CLI binary (`./build/openstack` or `openstack` in `PATH`)
- `curl` and `zip`

## Usage

```bash
# 1) Deploy lambda + api + integration + routes
./examples/apigateway-lambda/setup.sh

# 2) Invoke the routes and validate output
./examples/apigateway-lambda/invoke.sh
```

Expected output includes a successful validation line:

```text
Validation passed.
```

## Configurable env vars

- `OPENSTACK_ENDPOINT` (default: `http://localhost:4566`)
- `AWS_DEFAULT_REGION` (default: `us-east-1`)
- `OPENSTACK_ACCOUNT_ID` (default: `000000000000`)
- `OPENSTACK_BIN` (optional path to `openstack` CLI)
- `FUNCTION_NAME` (default: `apigw-orders-handler`)
- `API_NAME` (default: `apigw-orders-api`)
- `API_STAGE` (default: `$default`; only `$default` is supported today)
- `RUNTIME` (default: `nodejs24.x`)
- `ROLE_ARN` (default: `arn:aws:iam::<account-id>:role/lambda-role`)
- `NAME` (default: `openstack`, for `invoke.sh`)
- `ORDER_ID` (default: `123`, for `invoke.sh`)
