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
| `--ui` | `TARN_UI_ENABLED` | `false` | Enable dashboard UI (localhost:4566) |
| `--ui-dir` | `TARN_UI_DIR` | `./ui/build` | Path to built UI assets |
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
