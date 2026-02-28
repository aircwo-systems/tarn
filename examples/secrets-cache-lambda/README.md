# Secrets Cache Lambda Example

This example creates:

- a secret named `test` with value `test-secret`
- a Lambda named `cache-extension-lambda-test`

The Lambda fetches the secret through the built-in Parameters and Secrets extension (`localhost:2773`) and returns it when invoked.

## Prerequisites

- OpenStack server running at `http://localhost:4566`
- OpenStack CLI binary (`./build/openstack` or `openstack` in `PATH`)
- `zip`

## Usage

```bash
# 1) Create/update secret + deploy lambda
./examples/secrets-cache-lambda/setup.sh

# 2) Invoke lambda (uses secretId=test by default)
./examples/secrets-cache-lambda/invoke.sh
```

Expected invoke output includes:

```json
{"function":"cache-extension-lambda-test","secretId":"test","secretValue":"test-secret"}
```

## Configurable env vars

- `OPENSTACK_ENDPOINT` (default: `http://localhost:4566`)
- `AWS_DEFAULT_REGION` (default: `us-east-1`)
- `OPENSTACK_ACCOUNT_ID` (default: `000000000000`)
- `SECRET_NAME` (default: `test`)
- `SECRET_VALUE` (default: `test-secret`)
- `FUNCTION_NAME` (default: `cache-extension-lambda-test`)
- `RUNTIME` (default: `nodejs24.x`)
- `HANDLER` (default: `index.handler`)
- `ROLE_ARN` (default: `arn:aws:iam::<account-id>:role/lambda-role`)
- `OPENSTACK_BIN` (optional path to the `openstack` CLI)
