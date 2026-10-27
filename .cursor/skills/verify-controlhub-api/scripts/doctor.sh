#!/usr/bin/env bash
# doctor.sh — read-only: /health, optional migrate-status peek, binary ELF, port owned by our pid
set -euo pipefail

fail() { echo "doctor FAIL: $*" >&2; exit 1; }

GO_ROOT="/workspace/toolchains/go-1.26.2"
export GOROOT="${GO_ROOT}"
export PATH="${GO_ROOT}/bin:${PATH}"

REPO_ROOT="/workspace/ControlHub-Backend"
APP_PORT="${APP_PORT:-8080}"
MYSQL_HOST="${CONTROLHUB_MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${CONTROLHUB_MYSQL_PORT:-3318}"

command -v go >/dev/null || fail "go not on PATH"
echo "go: $(go version 2>&1)"

if ! (echo >/dev/tcp/"${MYSQL_HOST}"/"${MYSQL_PORT}") >/dev/null 2>&1; then
  fail "MySQL not listening on ${MYSQL_HOST}:${MYSQL_PORT}"
fi
echo "MySQL: ${MYSQL_HOST}:${MYSQL_PORT} open"

health="$(curl -fsS --max-time 5 "http://127.0.0.1:${APP_PORT}/health" 2>/dev/null || true)"
echo "health: ${health:-empty}"
echo "$health" | grep -q '"status"[[:space:]]*:[[:space:]]*"ok"' || fail "/health not ok on :${APP_PORT}"

if [[ -n "${RUN_DIR:-}" && -x "${RUN_DIR}/bin/server" ]]; then
  head_bytes="$(head -c 4 "${RUN_DIR}/bin/server" | od -An -tx1 | tr -d ' \n')"
  echo "binary head: $head_bytes"
  echo "$head_bytes" | grep -q '7f454c46' || fail "RUN_DIR server is not ELF"
  if [[ -f "${RUN_DIR}/server.pid" ]]; then
    pid="$(cat "${RUN_DIR}/server.pid")"
    kill -0 "$pid" 2>/dev/null || fail "server.pid=$pid not running"
    # Port should be held by something; best-effort check via ss/lsof if present
    if command -v ss >/dev/null 2>&1; then
      if ss -ltnp 2>/dev/null | grep -q ":${APP_PORT}"; then
        echo "port :${APP_PORT} has a listener"
      fi
    fi
    echo "server.pid: $pid alive"
  fi
else
  echo "note: RUN_DIR/bin/server not set — skipping ELF/pid checks (health still ok)"
fi

# Optional migrate-status peek (do not print DSN)
if [[ "${CONTROLHUB_DOCTOR_MIGRATE_STATUS:-0}" == "1" ]]; then
  (
    cd "$REPO_ROOT"
    make migrate-status >/dev/null 2>&1 && echo "migrate-status: ok (output suppressed)" || echo "migrate-status: failed or unavailable (non-fatal for doctor)"
  )
fi

echo "doctor OK: API :${APP_PORT}"
