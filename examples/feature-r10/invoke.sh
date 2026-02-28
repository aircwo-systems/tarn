#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="$SCRIPT_DIR/.state.env"

if [[ ! -f "$STATE_FILE" ]]; then
  echo "state file not found at $STATE_FILE. Run ./examples/feature-r10/setup.sh first." >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$STATE_FILE"

ENDPOINT="${OPENSTACK_ENDPOINT:-$ENDPOINT}"
INVOKE_BASE="${ENDPOINT}/_apigateway/${API_ID}/${API_STAGE}"

feature_resp="$(curl -sS "$INVOKE_BASE/feature")"
order_resp="$(curl -sS "$INVOKE_BASE/orders/123")"
worker_resp="$(curl -sS -X POST "$ENDPOINT/2015-03-31/functions/$WORKER_FUNCTION_NAME/invocations" -H 'Content-Type: application/json' -d '{"feature":"r10","job":"reindex"}')"

echo "GET /feature -> $feature_resp"
echo "GET /orders/{id} -> $order_resp"
echo "Invoke worker -> $worker_resp"
