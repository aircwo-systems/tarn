# Kysely PostgreSQL Lambda Example

This example creates a Node.js Lambda that initializes a Kysely PostgreSQL dialect and opens a connection to a PostgreSQL instance exposed on port `5432`.

It does not run schema or data operations. The handler just connects, releases the connection, logs the target, and returns the connection metadata.

## What gets created

- Lambda function: `kysely-postgres-lambda-test`

## Prerequisites

- OpenStack server running at `http://localhost:4566`
- OpenStack CLI binary (`./build/openstack` or `openstack` in `PATH`)
- `bun`, `zip`, and `curl`
- A PostgreSQL instance reachable from the Lambda container at `host.docker.internal:5432`

## Usage

```bash
# 1) Install deps, package, and deploy the lambda
./examples/kysely-postgres-lambda/setup.sh

# 2) Invoke it
./examples/kysely-postgres-lambda/invoke.sh
```

Expected response shape:

```json
{"function":"kysely-postgres-lambda-test","library":"kysely","dialect":"postgres","host":"host.docker.internal","port":5432,"database":"postgres","connected":true}
```

## Configurable env vars

- `OPENSTACK_ENDPOINT` (default: `http://localhost:4566`)
- `AWS_DEFAULT_REGION` (default: `us-east-1`)
- `OPENSTACK_ACCOUNT_ID` (default: `000000000000`)
- `OPENSTACK_BIN` (optional path to the `openstack` CLI)
- `FUNCTION_NAME` (default: `kysely-postgres-lambda-test`)
- `RUNTIME` (default: `nodejs24.x`)
- `HANDLER` (default: `index.handler`)
- `ROLE_ARN` (default: `arn:aws:iam::<account-id>:role/lambda-role`)
- `DATABASE_URL` (default: `postgres://postgres:postgres@host.docker.internal:5432/postgres`)

If your local PostgreSQL uses different credentials or database name, override `DATABASE_URL` when running `setup.sh`.
