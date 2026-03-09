#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
FUNCTION_NAME="${FUNCTION_NAME:-apigw-orders-handler}"
API_NAME="${API_NAME:-apigw-orders-api}"
API_STAGE="${API_STAGE:-\$default}"
RUNTIME="${RUNTIME:-nodejs24.x}"
ROLE_ARN="${ROLE_ARN:-arn:aws:iam::${ACCOUNT_ID}:role/lambda-role}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

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

if [[ "$API_STAGE" != '$default' ]]; then
  echo "unsupported API_STAGE: $API_STAGE" >&2
  echo "This example currently supports only API_STAGE=\$default because OpenStack auto-creates a single \$default stage for HTTP APIs." >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
zip_path="$tmp_dir/function.zip"
state_file="$SCRIPT_DIR/.state.env"

(
  cd "$SCRIPT_DIR/lambda"
  zip -q -r "$zip_path" index.js
)

if "$OPENSTACK_BIN" lambda list | awk 'NR>1 {print $1}' | grep -qx "$FUNCTION_NAME"; then
  echo "Function exists; deleting and recreating: $FUNCTION_NAME"
  "$OPENSTACK_BIN" lambda delete --name "$FUNCTION_NAME" >/dev/null
fi

"$OPENSTACK_BIN" lambda create \
  --name "$FUNCTION_NAME" \
  --runtime "$RUNTIME" \
  --handler index.handler \
  --role "$ROLE_ARN" \
  --zip "$zip_path" \
  >/dev/null

echo "Created Lambda: $FUNCTION_NAME"

LAMBDA_ARN="arn:aws:lambda:${REGION}:${ACCOUNT_ID}:function:${FUNCTION_NAME}"

extract_json_string() {
  local json="$1"
  local key="$2"
  local value
  value="$(printf '%s' "$json" | tr -d '\n' | sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p")"
  printf '%s' "$value"
}

existing_apis_resp="$(curl -sS "$ENDPOINT/v2/apis")"
while IFS= read -r del_id; do
  [[ -z "$del_id" ]] && continue
  echo "API exists; deleting: $API_NAME ($del_id)"
  curl -sS -X DELETE "$ENDPOINT/v2/apis/$del_id" >/dev/null
done < <(printf '%s' "$existing_apis_resp" | python3 -c "
import json, sys
data = json.loads(sys.stdin.read())
for item in data.get('items', []):
    if item.get('name') == sys.argv[1]:
        print(item.get('apiId', ''))
" "$API_NAME" 2>/dev/null)

api_payload='{"Name":"'"$API_NAME"'","ProtocolType":"HTTP","Description":"API Gateway -> Lambda example"}'
api_resp="$(curl -sS -X POST "$ENDPOINT/v2/apis" \
  -H 'Content-Type: application/json' \
  -d "$api_payload")"

api_id="$(extract_json_string "$api_resp" "apiId")"

if [[ -z "$api_id" ]]; then
  echo "failed to create api: $api_resp" >&2
  exit 1
fi

echo "Created API: $api_id"

integration_payload='{"IntegrationType":"AWS_PROXY","IntegrationUri":"'"$LAMBDA_ARN"'","PayloadFormatVersion":"2.0","TimeoutInMillis":30000}'

integration_resp="$(curl -sS -X POST "$ENDPOINT/v2/apis/$api_id/integrations" \
  -H 'Content-Type: application/json' \
  -d "$integration_payload")"

integration_id="$(extract_json_string "$integration_resp" "integrationId")"

if [[ -z "$integration_id" ]]; then
  echo "failed to create integration: $integration_resp" >&2
  exit 1
fi

echo "Created Integration: $integration_id"

create_route() {
  local route_key="$1"
  local route_payload='{"RouteKey":"'"$route_key"'","Target":"integrations/'"$integration_id"'"}'
  curl -sS -X POST "$ENDPOINT/v2/apis/$api_id/routes" \
    -H 'Content-Type: application/json' \
    -d "$route_payload" >/dev/null
}

create_route "GET /hello"
create_route "GET /orders/{id}"

invoke_base="$ENDPOINT/_apigateway/$api_id/$API_STAGE"
{
  printf 'API_ID=%q\n' "$api_id"
  printf 'API_NAME=%q\n' "$API_NAME"
  printf 'API_STAGE=%q\n' "$API_STAGE"
  printf 'INVOKE_BASE=%q\n' "$invoke_base"
  printf 'ENDPOINT=%q\n' "$ENDPOINT"
} > "$state_file"

echo "Ready."
echo "API ID:      $api_id"
echo "Stage:       $API_STAGE"
echo "Invoke base: $invoke_base"
echo "State file:  $state_file"
echo "Try:"
echo "  curl '$invoke_base/hello?name=openstack'"
echo "  curl '$invoke_base/orders/123'"
