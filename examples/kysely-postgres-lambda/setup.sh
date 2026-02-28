#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
FUNCTION_NAME="${FUNCTION_NAME:-kysely-postgres-lambda-test}"
RUNTIME="${RUNTIME:-nodejs24.x}"
HANDLER="${HANDLER:-index.handler}"
ROLE_ARN="${ROLE_ARN:-arn:aws:iam::${ACCOUNT_ID}:role/lambda-role}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@host.docker.internal:5432/postgres}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

require_command curl
require_command bun
require_command zip

OPENSTACK_BIN="${OPENSTACK_BIN:-}"
if [[ -z "$OPENSTACK_BIN" ]]; then
  if [[ -x "$ROOT_DIR/build/openstack" ]]; then
    OPENSTACK_BIN="$ROOT_DIR/build/openstack"
  else
    OPENSTACK_BIN="openstack"
  fi
fi

if ! command -v "$OPENSTACK_BIN" >/dev/null 2>&1; then
  echo "openstack CLI not found. Set OPENSTACK_BIN to the binary path." >&2
  exit 1
fi

export OPENSTACK_ENDPOINT="$ENDPOINT"
export AWS_DEFAULT_REGION="$REGION"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
zip_path="$tmp_dir/function.zip"

echo "Installing Lambda dependencies with bun"
(
  cd "$SCRIPT_DIR/lambda"
  bun install >/dev/null
)

echo "Packaging Lambda"
(
  cd "$SCRIPT_DIR/lambda"
  zip -q -r "$zip_path" index.js package.json node_modules
)

if "$OPENSTACK_BIN" lambda list | awk 'NR>1 {print $1}' | grep -qx "$FUNCTION_NAME"; then
  echo "Function exists; deleting and recreating: $FUNCTION_NAME"
  "$OPENSTACK_BIN" lambda delete --name "$FUNCTION_NAME" >/dev/null
fi

echo "Creating function '$FUNCTION_NAME'"
"$OPENSTACK_BIN" lambda create \
  --name "$FUNCTION_NAME" \
  --runtime "$RUNTIME" \
  --handler "$HANDLER" \
  --role "$ROLE_ARN" \
  --zip "$zip_path" \
  >/dev/null

echo "Applying Lambda environment"
curl -sS -X PUT "$ENDPOINT/2015-03-31/functions/$FUNCTION_NAME/configuration" \
  -H 'Content-Type: application/json' \
  -d "{\"Environment\":{\"Variables\":{\"DATABASE_URL\":\"$DATABASE_URL\"}}}" \
  >/dev/null

echo "Ready."
echo "- Function:     $FUNCTION_NAME"
echo "- DATABASE_URL: $DATABASE_URL"
echo ""
echo "Next step:"
echo "  ./examples/kysely-postgres-lambda/invoke.sh"
