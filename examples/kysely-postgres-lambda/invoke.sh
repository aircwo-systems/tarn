#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

FUNCTION_NAME="${FUNCTION_NAME:-kysely-postgres-lambda-test}"

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

"$OPENSTACK_BIN" lambda invoke --name "$FUNCTION_NAME" --payload '{}'
