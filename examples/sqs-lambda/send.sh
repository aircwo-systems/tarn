#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
QUEUE_NAME="${QUEUE_NAME:-orders-queue}"

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

message_body="${1:-{\"orderId\":\"1001\",\"status\":\"NEW\"}}"
"$OPENSTACK_BIN" sqs send --queue "$QUEUE_NAME" --body "$message_body" >/dev/null

echo "Sent message to $QUEUE_NAME"
