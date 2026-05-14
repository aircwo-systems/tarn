# Configuration

Tarn can be configured via command-line flags or environment variables.

## Server Flags

```bash
./tarn start [options]
```

| Flag | Env Variable | Default | Description |
|------|---|---|---|
| `--host` | `TARN_HOST` | `0.0.0.0` | Bind address |
| `--port` | `TARN_PORT` | `4566` | API port |
| `--region` | `TARN_REGION` | `us-east-1` | Emulated AWS region |
| `--data-dir` | `TARN_DATA_DIR` | `~/.tarn/data` | Persistent storage location |
| `--persist` | `TARN_PERSIST` | `true` | Persist state across restarts |
| `--ui` | `TARN_UI_ENABLED` | `false` | Enable dashboard UI (localhost:4566) — full build only |
| `--ui-dir` | `TARN_UI_DIR` | `./ui/build` | Path to built UI assets — full build only |
| `--expose-secrets-proxy` | `TARN_EXPOSE_SECRETS_PROXY` | `false` | Run the local secrets extension-compatible proxy |
| `--secrets-proxy-host` | `TARN_SECRETS_PROXY_HOST` | `127.0.0.1` | Bind address for the local secrets proxy |
| `--secrets-proxy-port` | `TARN_SECRETS_PROXY_PORT` | `2773` | Port for the local secrets proxy |
| `--secrets-proxy-token` | `TARN_SECRETS_PROXY_TOKEN` | `local-dev-token` | Expected extension token value |
| `--secrets-proxy-require-token` | `TARN_SECRETS_PROXY_REQUIRE_TOKEN` | `true` | Require token validation on the secrets proxy |
| `--vault-key` | `TARN_VAULT_KEY` | `~/.tarn/vault.key` | AES-256 key file used to encrypt secrets at rest |

## Examples

### Local Development (with Dashboard)

```bash
./tarn start \
  --port 4566 \
  --region us-east-1 \
  --persist \
  --ui
```

Then open `http://127.0.0.1:4566` to see the dashboard.

### With Custom Data Directory

```bash
export TARN_DATA_DIR=/tmp/tarn-dev
./tarn start --persist
```

### Multiple Regions

```bash
# Terminal 1
./tarn start --port 4566 --region us-east-1

# Terminal 2
./tarn start --port 4567 --region eu-west-1

# In your code
const lambdaEast = new LambdaClient({ endpoint: "http://127.0.0.1:4566" });
const lambdaWest = new LambdaClient({ endpoint: "http://127.0.0.1:4567" });
```

## Multi-Account

Tarn supports multiple isolated AWS accounts in a single running instance — compatible with LocalStack's multi-account approach.

### How It Works

Account routing uses the SigV4 `Authorization` header. If the `Credential` field contains a **12-digit numeric** access key ID, Tarn treats it as the AWS account ID and routes the request to that account's isolated resource namespace. Any other value (including `"test"`) falls back to the default account (`000000000000`).

```
Authorization: AWS4-HMAC-SHA256 Credential=111111111111/...
```

### Data Layout

Each non-default account gets its own subdirectory under `data-dir`:

```
~/.tarn/data/
├── queues/, sns/, secrets/, s3/, ...  # default account (backward compatible)
└── accounts/
    └── 111111111111/
        ├── queues/, sns/, secrets/, s3/, ...
```

The trace database (`traces.db`) is shared across all accounts.

### AWS CLI / SDK

```bash
# Default account
AWS_ACCESS_KEY_ID=test aws --endpoint http://localhost:4566 sqs list-queues

# Account 111111111111
AWS_ACCESS_KEY_ID=111111111111 AWS_SECRET_ACCESS_KEY=test \
  aws --endpoint http://localhost:4566 sqs list-queues
```

### Tarn CLI

Use `TARN_ACCOUNT_ID` or `--account` (flush only) to target a specific account:

```bash
# All tarn subcommands respect TARN_ACCOUNT_ID
TARN_ACCOUNT_ID=111111111111 tarn sqs list
TARN_ACCOUNT_ID=111111111111 tarn lambda list

# flush accepts an explicit --account flag (takes precedence over env)
tarn flush --account 111111111111
tarn flush --account 111111111111 --tag feature=r10 --storage
```

### Terraform

Set `access_key` to a 12-digit numeric string in the AWS provider:

```hcl
provider "aws" {
  access_key = "111111111111"   # routes to account 111111111111
  secret_key = "test"
  # ...
}
```

See the [Terraform guide](/guide/terraform) for the full provider configuration.

### Dashboard UI

Switch accounts from the **Settings** dialog (gear icon in the sidebar). Add any 12-digit account ID, give it a label, and click **Switch** — the dashboard immediately reloads data for that account. The active account ID is shown in the sidebar footer.

## Infrastructure Probing

Infrastructure probing is enabled by default. Tarn can probe common local dependencies such as PostgreSQL, Redis, MySQL, and MongoDB, which is useful when frontend applications or local dashboards need a quick view of what backing services are reachable in a development environment.

Configure it with environment variables:

```bash
export TARN_INFRA_PROBE=true
export TARN_INFRA_TARGETS="postgresql:localhost:5432,redis:localhost:6379"

./tarn start
```

## Secrets Encryption

Secrets are encrypted at rest by default using an AES-256 vault key stored at `~/.tarn/vault.key`.

Use a custom key path:

```bash
./tarn start --vault-key ~/.tarn/vault.key
```

The key file is created automatically if it does not exist. To disable secrets encryption entirely, start Tarn with `--vault-key ""`.

## Environment-Based Configuration

You can also pass all flags via environment variables:

```bash
export TARN_PORT=4566
export TARN_REGION=us-east-1
export TARN_UI_ENABLED=true
export TARN_PERSIST=true
export TARN_EXPOSE_SECRETS_PROXY=false

./tarn start
```
