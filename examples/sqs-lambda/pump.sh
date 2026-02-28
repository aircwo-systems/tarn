#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

ENDPOINT="${OPENSTACK_ENDPOINT:-http://localhost:4566}"
ACCOUNT_ID="${OPENSTACK_ACCOUNT_ID:-000000000000}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
QUEUE_NAME="${QUEUE_NAME:-orders-queue}"
FUNCTION_NAME="${FUNCTION_NAME:-orders-consumer}"
WAIT_SECONDS="${WAIT_SECONDS:-10}"

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
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

export AWS_DEFAULT_REGION="$REGION"
export OPENSTACK_ENDPOINT="$ENDPOINT"

queue_url="$ENDPOINT/$ACCOUNT_ID/$QUEUE_NAME"

echo "Polling queue '$QUEUE_NAME' at $queue_url"
echo "Invoking Lambda '$FUNCTION_NAME' when messages arrive"

while true; do
  receive_output="$("$OPENSTACK_BIN" sqs receive --queue "$QUEUE_NAME" --max 1 --wait "$WAIT_SECONDS" 2>&1 || true)"

  if [[ "$receive_output" == *"No messages available."* ]]; then
    continue
  fi

  message_id="$(printf '%s\n' "$receive_output" | sed -n 's/^MessageId:[[:space:]]*//p' | head -n1)"
  receipt_handle="$(printf '%s\n' "$receive_output" | sed -n 's/^ReceiptHandle:[[:space:]]*//p' | head -n1)"
  body="$(printf '%s\n' "$receive_output" | sed -n 's/^Body:[[:space:]]*//p' | head -n1)"

  if [[ -z "$message_id" || -z "$receipt_handle" ]]; then
    echo "[pump] unable to parse receive output; skipping" >&2
    echo "$receive_output" >&2
    continue
  fi

  payload="$(MESSAGE_ID="$message_id" RECEIPT_HANDLE="$receipt_handle" BODY="$body" REGION="$REGION" python3 - <<'PY'
import json
import os

payload = {
    "Records": [
        {
            "messageId": os.environ["MESSAGE_ID"],
            "receiptHandle": os.environ["RECEIPT_HANDLE"],
            "body": os.environ["BODY"],
            "attributes": {},
            "messageAttributes": {},
            "md5OfBody": "",
            "eventSource": "aws:sqs",
            "eventSourceARN": "",
            "awsRegion": os.environ["REGION"],
        }
    ]
}
print(json.dumps(payload))
PY
)"

  echo "[pump] invoking $FUNCTION_NAME for message $message_id"

  if "$OPENSTACK_BIN" lambda invoke --name "$FUNCTION_NAME" --payload "$payload"; then
    delete_response="$(curl -sS -X POST "$ENDPOINT/$ACCOUNT_ID/$QUEUE_NAME" \
      --data-urlencode "Action=DeleteMessage" \
      --data-urlencode "QueueUrl=$queue_url" \
      --data-urlencode "ReceiptHandle=$receipt_handle")"

    if [[ "$delete_response" == *"<Error>"* ]]; then
      echo "[pump] delete failed for message $message_id" >&2
      echo "$delete_response" >&2
    else
      echo "[pump] acked message $message_id"
    fi
  else
    echo "[pump] invoke failed; message will reappear after visibility timeout" >&2
  fi
done
