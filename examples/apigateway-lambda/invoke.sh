#!/usr/bin/env bash
set -euo pipefail

API_NAME="${API_NAME:-apigw-orders-api}"
NAME="${NAME:-openstack}"
ORDER_ID="${ORDER_ID:-123}"
REQUESTED_API_STAGE="${API_STAGE:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${STATE_FILE:-$SCRIPT_DIR/.state.env}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

if [[ ! -f "$STATE_FILE" ]]; then
  echo "state file not found at $STATE_FILE. Run ./examples/apigateway-lambda/setup.sh first." >&2
  exit 1
fi

. "$STATE_FILE"

API_STAGE="${REQUESTED_API_STAGE:-${API_STAGE:-\$default}}"

if [[ -z "${INVOKE_BASE:-}" ]]; then
  echo "missing INVOKE_BASE in $STATE_FILE. Re-run setup.sh." >&2
  exit 1
fi

if [[ -n "${OPENSTACK_ENDPOINT:-}" && -n "${API_ID:-}" ]]; then
  INVOKE_BASE="${OPENSTACK_ENDPOINT}/_apigateway/${API_ID}/${API_STAGE}"
fi

request_json() {
  local url="$1"
  local output status body
  output="$(curl -sS -w $'\n%{http_code}' "$url")"
  status="${output##*$'\n'}"
  body="${output%$'\n'*}"
  printf '%s\n%s\n' "$status" "$body"
}

hello_result="$(request_json "$INVOKE_BASE/hello?name=$NAME")"
hello_status="${hello_result%%$'\n'*}"
hello_resp="${hello_result#*$'\n'}"

orders_result="$(request_json "$INVOKE_BASE/orders/$ORDER_ID")"
orders_status="${orders_result%%$'\n'*}"
orders_resp="${orders_result#*$'\n'}"

echo "GET /hello -> $hello_resp"
echo "GET /orders/{id} -> $orders_resp"

if [[ "$hello_status" != "200" ]]; then
  echo "hello request failed with status $hello_status" >&2
  exit 1
fi
if [[ "$orders_status" != "200" ]]; then
  echo "orders request failed with status $orders_status" >&2
  exit 1
fi

if [[ "$hello_resp" != *"\"name\":\"$NAME\""* ]]; then
  echo "unexpected hello response payload: $hello_resp" >&2
  exit 1
fi
if [[ "$orders_resp" != *"\"pathId\":\"$ORDER_ID\""* ]]; then
  echo "unexpected orders response payload: $orders_resp" >&2
  exit 1
fi

echo "Validation passed."
