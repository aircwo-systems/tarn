#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ROLE_ARN="${ROLE_ARN:-arn:aws:iam::${ACCOUNT_ID}:role/lambda-role}"
RUNTIME="${RUNTIME:-nodejs24.x}"
API_STAGE="${API_STAGE:-\$default}"
FEATURE_TAG_KEY="${FEATURE_TAG_KEY:-feature}"
FEATURE_TAG_VALUE="${FEATURE_TAG_VALUE:-r10}"
API_NAME="${API_NAME:-r10-public-api}"
API_FUNCTION_NAME="${API_FUNCTION_NAME:-r10-catalog-api}"
WORKER_FUNCTION_NAME="${WORKER_FUNCTION_NAME:-r10-fulfillment-worker}"
QUEUE_NAME="${QUEUE_NAME:-r10-orders}"
SECRET_NAME="${SECRET_NAME:-r10-shared-config}"
SECRET_VALUE="${SECRET_VALUE:-{\"feature\":\"r10\",\"owner\":\"catalog\",\"enabled\":true}}"
STATE_FILE="$SCRIPT_DIR/.state.env"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

require_command curl
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

json_string_field() {
  local json="$1"
  local key="$2"
  printf '%s' "$json" | tr -d '\n' | sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
}

function_exists() {
  "$OPENSTACK_BIN" lambda list | awk 'NR>1 {print $1}' | grep -qx "$1"
}

create_zip() {
  local source_dir="$1"
  local target_zip="$2"
  (
    cd "$source_dir"
    zip -q -r "$target_zip" index.js
  )
}

post_json() {
  local method="$1"
  local url="$2"
  local body="$3"
  curl -sS -X "$method" "$url" \
    -H 'Content-Type: application/json' \
    -d "$body"
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local context="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "expected $context to contain $needle" >&2
    echo "$haystack" >&2
    exit 1
  fi
}

function_tags() {
  local component="$1"
  printf '%s' "${FEATURE_TAG_KEY}=${FEATURE_TAG_VALUE},component=${component},example=feature-r10"
}

if [[ -f "$STATE_FILE" ]]; then
  "$SCRIPT_DIR/cleanup.sh" >/dev/null 2>&1 || true
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
api_zip="$tmp_dir/api.zip"
worker_zip="$tmp_dir/worker.zip"

create_zip "$SCRIPT_DIR/lambda/api" "$api_zip"
create_zip "$SCRIPT_DIR/lambda/worker" "$worker_zip"

if function_exists "$API_FUNCTION_NAME"; then
  "$OPENSTACK_BIN" lambda delete --name "$API_FUNCTION_NAME" >/dev/null
fi
if function_exists "$WORKER_FUNCTION_NAME"; then
  "$OPENSTACK_BIN" lambda delete --name "$WORKER_FUNCTION_NAME" >/dev/null
fi
if "$OPENSTACK_BIN" sqs list | grep -qx "$ENDPOINT/$ACCOUNT_ID/$QUEUE_NAME"; then
  "$OPENSTACK_BIN" sqs delete-queue --name "$QUEUE_NAME" >/dev/null
fi
if "$OPENSTACK_BIN" secrets list | awk 'NR>2 && $1 != "Total:" {print $1}' | grep -qx "$SECRET_NAME"; then
  "$OPENSTACK_BIN" secrets delete --name "$SECRET_NAME" >/dev/null
fi

"$OPENSTACK_BIN" lambda create \
  --name "$API_FUNCTION_NAME" \
  --runtime "$RUNTIME" \
  --handler index.handler \
  --role "$ROLE_ARN" \
  --tags "$(function_tags api)" \
  --zip "$api_zip" \
  >/dev/null

echo "Created Lambda: $API_FUNCTION_NAME"

"$OPENSTACK_BIN" lambda create \
  --name "$WORKER_FUNCTION_NAME" \
  --runtime "$RUNTIME" \
  --handler index.handler \
  --role "$ROLE_ARN" \
  --tags "$(function_tags worker)" \
  --zip "$worker_zip" \
  >/dev/null

echo "Created Lambda: $WORKER_FUNCTION_NAME"

"$OPENSTACK_BIN" sqs create-queue --name "$QUEUE_NAME" --tags "${FEATURE_TAG_KEY}=${FEATURE_TAG_VALUE},component=queue,example=feature-r10" >/dev/null
queue_url="$ENDPOINT/$ACCOUNT_ID/$QUEUE_NAME"

echo "Created Queue: $QUEUE_NAME"

"$OPENSTACK_BIN" secrets create \
  --name "$SECRET_NAME" \
  --value "$SECRET_VALUE" \
  --description 'R10 feature config' \
  --tags "${FEATURE_TAG_KEY}=${FEATURE_TAG_VALUE},component=config,example=feature-r10" \
  >/dev/null

echo "Created Secret: $SECRET_NAME"

api_payload=$(cat <<JSON
{"Name":"${API_NAME}","ProtocolType":"HTTP","Description":"Tagged r10 feature example","Tags":{"${FEATURE_TAG_KEY}":"${FEATURE_TAG_VALUE}","component":"gateway","example":"feature-r10"}}
JSON
)
api_resp="$(post_json POST "$ENDPOINT/v2/apis" "$api_payload")"
api_id="$(json_string_field "$api_resp" 'ApiId')"
if [[ -z "$api_id" ]]; then
  echo "failed to create api: $api_resp" >&2
  exit 1
fi

echo "Created API Gateway: $API_NAME ($api_id)"

lambda_arn="arn:aws:lambda:${REGION}:${ACCOUNT_ID}:function:${API_FUNCTION_NAME}"
integration_payload=$(cat <<JSON
{"IntegrationType":"AWS_PROXY","IntegrationUri":"${lambda_arn}","PayloadFormatVersion":"2.0","TimeoutInMillis":30000}
JSON
)
integration_resp="$(post_json POST "$ENDPOINT/v2/apis/$api_id/integrations" "$integration_payload")"
integration_id="$(json_string_field "$integration_resp" 'IntegrationId')"
if [[ -z "$integration_id" ]]; then
  echo "failed to create integration: $integration_resp" >&2
  exit 1
fi

route_payload() {
  local route_key="$1"
  cat <<JSON
{"RouteKey":"${route_key}","Target":"integrations/${integration_id}"}
JSON
}

post_json POST "$ENDPOINT/v2/apis/$api_id/routes" "$(route_payload 'GET /feature')" >/dev/null
post_json POST "$ENDPOINT/v2/apis/$api_id/routes" "$(route_payload 'GET /orders/{id}')" >/dev/null

api_function_tags="$(curl -sS "$ENDPOINT/2015-03-31/functions/$API_FUNCTION_NAME/tags")"
worker_function_tags="$(curl -sS "$ENDPOINT/2015-03-31/functions/$WORKER_FUNCTION_NAME/tags")"
queue_tags="$(curl -sS -X POST "$ENDPOINT" --data-urlencode 'Action=ListQueueTags' --data-urlencode "QueueUrl=$queue_url")"
secret_meta="$(curl -sS -X POST "$ENDPOINT/" -H 'Content-Type: application/x-amz-json-1.1' -H 'X-Amz-Target: secretsmanager.DescribeSecret' -d "{\"SecretId\":\"$SECRET_NAME\"}")"
api_meta="$(curl -sS "$ENDPOINT/v2/apis/$api_id")"

assert_contains "$api_function_tags" "\"${FEATURE_TAG_KEY}\":\"${FEATURE_TAG_VALUE}\"" "api function tags"
assert_contains "$worker_function_tags" "\"${FEATURE_TAG_KEY}\":\"${FEATURE_TAG_VALUE}\"" "worker function tags"
assert_contains "$queue_tags" "<key>${FEATURE_TAG_KEY}</key>" "queue tags"
assert_contains "$queue_tags" "<value>${FEATURE_TAG_VALUE}</value>" "queue tags"
assert_contains "$secret_meta" "\"Key\":\"${FEATURE_TAG_KEY}\"" "secret tags"
assert_contains "$secret_meta" "\"Value\":\"${FEATURE_TAG_VALUE}\"" "secret tags"
assert_contains "$api_meta" "\"Tags\":{" "api gateway tags"
assert_contains "$api_meta" "\"${FEATURE_TAG_KEY}\":\"${FEATURE_TAG_VALUE}\"" "api gateway tags"

invoke_base="$ENDPOINT/_apigateway/$api_id/$API_STAGE"
{
  printf 'API_ID=%q\n' "$api_id"
  printf 'API_NAME=%q\n' "$API_NAME"
  printf 'API_STAGE=%q\n' "$API_STAGE"
  printf 'API_FUNCTION_NAME=%q\n' "$API_FUNCTION_NAME"
  printf 'WORKER_FUNCTION_NAME=%q\n' "$WORKER_FUNCTION_NAME"
  printf 'QUEUE_NAME=%q\n' "$QUEUE_NAME"
  printf 'QUEUE_URL=%q\n' "$queue_url"
  printf 'SECRET_NAME=%q\n' "$SECRET_NAME"
  printf 'FEATURE_TAG_KEY=%q\n' "$FEATURE_TAG_KEY"
  printf 'FEATURE_TAG_VALUE=%q\n' "$FEATURE_TAG_VALUE"
  printf 'INVOKE_BASE=%q\n' "$invoke_base"
  printf 'ENDPOINT=%q\n' "$ENDPOINT"
} > "$STATE_FILE"

echo "Ready."
echo "Feature tag:  ${FEATURE_TAG_KEY}:${FEATURE_TAG_VALUE}"
echo "Dashboard:    filter by '${FEATURE_TAG_KEY}:${FEATURE_TAG_VALUE}'"
echo "Invoke base:  $invoke_base"
echo "State file:   $STATE_FILE"
echo "Try:"
echo "  ./examples/feature-r10/invoke.sh"
echo "  ./examples/feature-r10/cleanup.sh"
