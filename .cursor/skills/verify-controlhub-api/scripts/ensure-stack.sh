#!/usr/bin/env bash
# ensure-stack.sh — require MySQL 3318; go build ELF to RUN_DIR/bin/server; start on APP_PORT; wait /health.
# Usage: eval "$(.cursor/skills/verify-controlhub-api/scripts/ensure-stack.sh)"
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
if [[ ! -f "${REPO_ROOT}/go.mod" ]]; then
  REPO_ROOT="/workspace/ControlHub-Backend"
fi

GO_ROOT="/workspace/toolchains/go-1.26.2"
export GOROOT="${GO_ROOT}"
export PATH="${GO_ROOT}/bin:${PATH}"

RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$$}"
STATE_ROOT="${CONTROLHUB_API_VERIFY_STATE:-/tmp/controlhub-api-verify}"
RUN_DIR="${STATE_ROOT}/${RUN_ID}"
BIN_DIR="${RUN_DIR}/bin"
EVIDENCE_DIR="${RUN_DIR}/evidence"
mkdir -p "$BIN_DIR" "$EVIDENCE_DIR"

MYSQL_HOST="${CONTROLHUB_MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${CONTROLHUB_MYSQL_PORT:-3318}"

fail() { echo "ensure-stack FAIL: $*" >&2; exit 1; }

command -v go >/dev/null || fail "go not on PATH (expected ${GO_ROOT}/bin)"
go version >&2 || fail "go version failed"

# --- MySQL ---
if ! (echo >/dev/tcp/"${MYSQL_HOST}"/"${MYSQL_PORT}") >/dev/null 2>&1; then
  fail "MySQL metadata not listening on ${MYSQL_HOST}:${MYSQL_PORT}"
fi

[[ -f "${REPO_ROOT}/.env" ]] || fail ".env missing under ${REPO_ROOT}"

# Load APP_PORT from .env without printing secrets
APP_PORT="$(grep -E '^APP_PORT=' "${REPO_ROOT}/.env" | head -1 | cut -d= -f2- | tr -d '\r' || true)"
APP_PORT="${APP_PORT:-8080}"
APP_PORT="${CONTROLHUB_APP_PORT:-$APP_PORT}"

# Refuse double-driving if another verify run owns this port
OWNER_FILE="${STATE_ROOT}/port-${APP_PORT}.owner"
if (echo >/dev/tcp/127.0.0.1/"${APP_PORT}") >/dev/null 2>&1; then
  if [[ -f "$OWNER_FILE" ]]; then
    other="$(cat "$OWNER_FILE" 2>/dev/null || true)"
    if [[ -n "$other" && "$other" != "$RUN_ID" ]]; then
      fail "port :${APP_PORT} owned by verify RUN_ID=${other}; set CONTROLHUB_APP_PORT / APP_PORT to isolate"
    fi
  fi
  # Reuse existing healthy server
  health="$(curl -fsS --max-time 5 "http://127.0.0.1:${APP_PORT}/health" 2>/dev/null || true)"
  if echo "$health" | grep -q '"status"[[:space:]]*:[[:space:]]*"ok"'; then
    echo "ensure-stack: reusing healthy server on :${APP_PORT}" >&2
    echo "$RUN_ID" >"$OWNER_FILE" 2>/dev/null || true
    export RUN_ID RUN_DIR BIN_DIR EVIDENCE_DIR APP_PORT
    export PATH="${GO_ROOT}/bin:${PATH}" GOROOT="${GO_ROOT}"
    echo "export RUN_ID=${RUN_ID}"
    echo "export RUN_DIR=${RUN_DIR}"
    echo "export BIN_DIR=${BIN_DIR}"
    echo "export EVIDENCE_DIR=${EVIDENCE_DIR}"
    echo "export APP_PORT=${APP_PORT}"
    echo "export PATH=${GO_ROOT}/bin:\$PATH"
    echo "export GOROOT=${GO_ROOT}"
    echo "export CONTROLHUB_API_STARTED_SERVER=0"
    exit 0
  fi
  fail "port :${APP_PORT} busy but /health not ok"
fi

# --- Build ELF (never use checked-in ./server Mach-O) ---
SERVER_BIN="${BIN_DIR}/server"
echo "ensure-stack: go build -o ${SERVER_BIN} ./cmd/server" >&2
(
  cd "$REPO_ROOT"
  go build -o "$SERVER_BIN" ./cmd/server
) || fail "go build failed"
# Sanity: ELF magic
magic="$(od -An -tx4 -N4 "$SERVER_BIN" 2>/dev/null | tr -d ' \n' || true)"
# Linux ELF is 7f454c46
head_bytes="$(head -c 4 "$SERVER_BIN" | od -An -tx1 | tr -d ' \n')"
echo "$head_bytes" | grep -q '7f454c46' || fail "built binary is not ELF (got head ${head_bytes}); refuse Mach-O"

# --- Start with .env ---
(
  cd "$REPO_ROOT"
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
  export APP_PORT
  nohup "$SERVER_BIN" >"${RUN_DIR}/server.log" 2>&1 &
  echo $! >"${RUN_DIR}/server.pid"
)
echo "$RUN_ID" >"$OWNER_FILE"

ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 2 "http://127.0.0.1:${APP_PORT}/health" 2>/dev/null | grep -q '"status"[[:space:]]*:[[:space:]]*"ok"'; then
    ready=1
    break
  fi
  sleep 0.5
done
[[ "$ready" -eq 1 ]] || fail "server did not become healthy on :${APP_PORT} (see ${RUN_DIR}/server.log)"

export RUN_ID RUN_DIR BIN_DIR EVIDENCE_DIR APP_PORT
export PATH="${GO_ROOT}/bin:${PATH}" GOROOT="${GO_ROOT}"

echo "export RUN_ID=${RUN_ID}"
echo "export RUN_DIR=${RUN_DIR}"
echo "export BIN_DIR=${BIN_DIR}"
echo "export EVIDENCE_DIR=${EVIDENCE_DIR}"
echo "export APP_PORT=${APP_PORT}"
echo "export PATH=${GO_ROOT}/bin:\$PATH"
echo "export GOROOT=${GO_ROOT}"
echo "export CONTROLHUB_API_STARTED_SERVER=1"
