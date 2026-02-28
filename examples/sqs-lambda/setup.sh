#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
QUEUE_NAME="${QUEUE_NAME:-orders-queue}"
FUNCTION_NAME="${FUNCTION_NAME:-orders-consumer}"
RUNTIME="${RUNTIME:-nodejs20.x}"
HANDLER="${HANDLER:-index.handler}"
ROLE_ARN="${ROLE_ARN:-arn:aws:iam::${ACCOUNT_ID}:role/lambda-role}"

if ! command -v zip >/dev/null 2>&1; then
  echo "zip is required" >&2
  exit 1
fi

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

(
  cd "$SCRIPT_DIR/lambda"
  zip -q -r "$zip_path" index.js
)

echo "Creating or reusing queue: $QUEUE_NAME"
"$OPENSTACK_BIN" sqs create-queue --name "$QUEUE_NAME" >/dev/null

queue_url="$ENDPOINT/$ACCOUNT_ID/$QUEUE_NAME"
echo "Queue URL: $queue_url"

if "$OPENSTACK_BIN" lambda list | awk 'NR>1 {print $1}' | grep -qx "$FUNCTION_NAME"; then
  echo "Function exists; deleting and recreating: $FUNCTION_NAME"
  "$OPENSTACK_BIN" lambda delete --name "$FUNCTION_NAME" >/dev/null
fi

echo "Creating function: $FUNCTION_NAME"
"$OPENSTACK_BIN" lambda create \
  --name "$FUNCTION_NAME" \
  --runtime "$RUNTIME" \
  --handler "$HANDLER" \
  --role "$ROLE_ARN" \
  --zip "$zip_path" \
  >/dev/null

echo "Ready."
echo "- Queue: $QUEUE_NAME"
echo "- Function: $FUNCTION_NAME"
