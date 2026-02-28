#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
STATE_FILE="$SCRIPT_DIR/.state.env"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
API_ID="${API_ID:-}"
API_FUNCTION_NAME="${API_FUNCTION_NAME:-r10-catalog-api}"
WORKER_FUNCTION_NAME="${WORKER_FUNCTION_NAME:-r10-fulfillment-worker}"
QUEUE_NAME="${QUEUE_NAME:-r10-orders}"
SECRET_NAME="${SECRET_NAME:-r10-shared-config}"
FEATURE_TAG_KEY="${FEATURE_TAG_KEY:-feature}"
FEATURE_TAG_VALUE="${FEATURE_TAG_VALUE:-r10}"

OPENSTACK_BIN="${OPENSTACK_BIN:-}"
if [[ -z "$OPENSTACK_BIN" ]]; then
  if [[ -x "$ROOT_DIR/build/openstack" ]]; then
    OPENSTACK_BIN="$ROOT_DIR/build/openstack"
  else
    OPENSTACK_BIN="openstack"
  fi
fi

if [[ -f "$STATE_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$STATE_FILE"
else
  "$OPENSTACK_BIN" flush --tag "${FEATURE_TAG_KEY}:${FEATURE_TAG_VALUE}" >/dev/null 2>&1 || true
  echo "Deleted feature-r10 example resources"
  exit 0
fi

delete_if_present() {
  "$@" >/dev/null 2>&1 || true
}

if [[ -n "$API_ID" ]]; then
  curl -sS -o /dev/null -X DELETE "$ENDPOINT/v2/apis/$API_ID" || true
fi

delete_if_present "$OPENSTACK_BIN" lambda delete --name "$API_FUNCTION_NAME"
delete_if_present "$OPENSTACK_BIN" lambda delete --name "$WORKER_FUNCTION_NAME"
delete_if_present "$OPENSTACK_BIN" sqs delete-queue --name "$QUEUE_NAME"
delete_if_present "$OPENSTACK_BIN" secrets delete --name "$SECRET_NAME"

rm -f "$STATE_FILE"

echo "Deleted feature-r10 example resources"
