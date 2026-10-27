#!/usr/bin/env bash
# cleanup.sh — kill only RUN_DIR server.pid; keep evidence/; never pkill MySQL/BE by name
set -euo pipefail

RUN_DIR="${RUN_DIR:?set RUN_DIR from ensure-stack.sh}"
APP_PORT="${APP_PORT:-8080}"
STATE_ROOT="${CONTROLHUB_API_VERIFY_STATE:-/tmp/controlhub-api-verify}"
OWNER_FILE="${STATE_ROOT}/port-${APP_PORT}.owner"

if [[ -f "${RUN_DIR}/server.pid" ]]; then
  pid="$(cat "${RUN_DIR}/server.pid")"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.3
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  rm -f "${RUN_DIR}/server.pid"
fi

if [[ -f "$OWNER_FILE" ]]; then
  owner="$(cat "$OWNER_FILE" 2>/dev/null || true)"
  if [[ "$owner" == "${RUN_ID:-}" ]]; then
    rm -f "$OWNER_FILE"
  fi
fi

# Keep bin/ and evidence/; optional purge binary only
if [[ "${CONTROLHUB_API_VERIFY_PURGE_BIN:-0}" == "1" ]]; then
  rm -rf "${RUN_DIR}/bin"
fi

echo "cleanup OK; evidence kept at ${RUN_DIR}/evidence"
ls -la "${RUN_DIR}/evidence" 2>/dev/null || true
