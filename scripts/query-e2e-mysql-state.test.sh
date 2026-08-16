#!/usr/bin/env bash
# Query E2E fixture state regression (run with `bash scripts/query-e2e-mysql-state.test.sh`).
# input: a temporary XDG_STATE_HOME and a stub Docker executable
# output: proves legacy migration, cross-directory state reuse, and missing-state fail-fast
# pos: regression guard for the per-user fixture handoff used by run-query-dev.sh
# note: keep this Docker-free; it tests only credential-state ownership and lifecycle decisions
set -euo pipefail

ROOT="$(mktemp -d "${TMPDIR:-/tmp}/query-e2e-state.XXXXXX")"
trap 'rm -rf "$ROOT"' EXIT
SCRIPT="$(cd "$(dirname "$0")" && pwd)/query-e2e-mysql.sh"
BIN="$ROOT/bin"
STATE_HOME="$ROOT/state"
mkdir -p "$BIN" "$ROOT/legacy" "$ROOT/fresh" "$ROOT/missing"

cat > "$BIN/docker" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  inspect) printf 'true\n' ;;
  exec) exit 0 ;;
  *) echo "unexpected docker command" >&2; exit 97 ;;
esac
EOF
chmod 700 "$BIN/docker"

cat > "$ROOT/legacy/.query-e2e-mysql.env" <<'EOF'
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO="query_e2e_ro:0123456789abcdef@tcp(127.0.0.1:13306)/query_e2e?parseTime=true&charset=utf8mb4"
QUERY_E2E_MYSQL_READONLY_PASSWORD=0123456789abcdef
EOF

run_up() {
  local directory="$1"
  (
    cd "$directory"
    PATH="$BIN:$PATH" XDG_STATE_HOME="$STATE_HOME" QUERY_E2E_MYSQL_READY_TIMEOUT=1 bash "$SCRIPT" up
  )
}

run_up "$ROOT/legacy" >/dev/null 2>&1
STATE_FILE="$STATE_HOME/controlhub/query-e2e-mysql.env"
[ -f "$STATE_FILE" ]
[ ! -e "$ROOT/legacy/.query-e2e-mysql.env" ]
[ "$(stat -f '%Lp' "$STATE_FILE" 2>/dev/null || stat -c '%a' "$STATE_FILE")" = "600" ]
[ "$(XDG_STATE_HOME="$STATE_HOME" bash "$SCRIPT" env-file)" = "$STATE_FILE" ]

run_up "$ROOT/fresh" >/dev/null 2>&1
[ ! -e "$ROOT/fresh/.query-e2e-mysql.env" ]

if (
  cd "$ROOT/missing"
  PATH="$BIN:$PATH" XDG_STATE_HOME="$ROOT/empty-state" QUERY_E2E_MYSQL_READY_TIMEOUT=1 bash "$SCRIPT" up
) > /dev/null 2> "$ROOT/missing.log"; then
  echo "expected a running fixture without state to fail" >&2
  exit 1
fi
grep -q 'shared fixture state is missing' "$ROOT/missing.log"
