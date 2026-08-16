#!/usr/bin/env bash
# Query E2E fixture state regression (run with `bash scripts/query-e2e-mysql-state.test.sh`).
# input: a temporary XDG_STATE_HOME and a stub Docker executable
# output: proves legacy migration, identity-isolated per-user state paths, cross-directory reuse, down scoping, and missing-state fail-fast
# pos: regression guard for the per-identity per-user fixture handoff used by run-query-dev.sh
# note: keep this Docker-free; it tests only credential-state ownership and lifecycle decisions
set -euo pipefail

ROOT="$(mktemp -d "${TMPDIR:-/tmp}/query-e2e-state.XXXXXX")"
trap 'rm -rf "$ROOT"' EXIT
SCRIPT="$(cd "$(dirname "$0")" && pwd)/query-e2e-mysql.sh"
BIN="$ROOT/bin"
STATE_HOME="$ROOT/state"
mkdir -p "$BIN" "$ROOT/legacy" "$ROOT/fresh" "$ROOT/missing" \
  "$ROOT/dir-a" "$ROOT/dir-b" "$ROOT/dir-a-fresh" "$ROOT/legacy-mismatch"

# Stub Docker: identity-aware so the script exercises every state-lifecycle
# path without any real container. fx-a/fx-b are "not present" (fresh-create
# path with generated state); the default container and fx-c are "running"
# (state-reuse path, and fail-fast when no state exists). `ps` claims a
# container exists so `down` reaches its state cleanup.
cat > "$BIN/docker" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  inspect)
    case "${4:-}" in
      fx-a|fx-b) exit 1 ;;
      fx-c) printf 'true\n' ;;
      *) printf 'true\n' ;;
    esac
    ;;
  exec) exit 0 ;;
  ps) printf 'fx-a\n' ;;
  rm) exit 0 ;;
  start) exit 0 ;;
  run) exit 0 ;;
  *) echo "unexpected docker command" >&2; exit 97 ;;
esac
EOF
chmod 700 "$BIN/docker"

cat > "$ROOT/legacy/.query-e2e-mysql.env" <<'EOF'
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO="query_e2e_ro:0123456789abcdef@tcp(127.0.0.1:13306)/query_e2e?parseTime=true&charset=utf8mb4"
QUERY_E2E_MYSQL_READONLY_PASSWORD=0123456789abcdef
EOF

run_up() {
  local directory="$1"; shift
  (
    cd "$directory"
    PATH="$BIN:$PATH" XDG_STATE_HOME="$STATE_HOME" QUERY_E2E_MYSQL_READY_TIMEOUT=1 \
      env "$@" bash "$SCRIPT" up
  )
}

env_file() {
  PATH="$BIN:$PATH" XDG_STATE_HOME="$STATE_HOME" env "$@" bash "$SCRIPT" env-file
}

# --- Legacy migration (default identity) ---
run_up "$ROOT/legacy" >/dev/null 2>&1
DEFAULT_STATE="$STATE_HOME/controlhub/query-e2e-mysql--controlhub-query-e2e-mysql:13306:query_e2e:query_e2e_ro.env"
[ -f "$DEFAULT_STATE" ]
[ ! -e "$ROOT/legacy/.query-e2e-mysql.env" ]
[ "$(stat -f '%Lp' "$DEFAULT_STATE" 2>/dev/null || stat -c '%a' "$DEFAULT_STATE")" = "600" ]
[ "$(env_file)" = "$DEFAULT_STATE" ]

run_up "$ROOT/fresh" >/dev/null 2>&1
[ ! -e "$ROOT/fresh/.query-e2e-mysql.env" ]

# --- Identity isolation: two identities, distinct non-clobbering state ---
ID_A="QUERY_E2E_MYSQL_CONTAINER=fx-a QUERY_E2E_MYSQL_PORT=13310 QUERY_E2E_MYSQL_DATABASE=query_e2e QUERY_E2E_MYSQL_READONLY_USER=query_e2e_ro"
ID_B="QUERY_E2E_MYSQL_CONTAINER=fx-b QUERY_E2E_MYSQL_PORT=13311 QUERY_E2E_MYSQL_DATABASE=query_e2e QUERY_E2E_MYSQL_READONLY_USER=query_e2e_ro"
# shellcheck disable=SC2086
run_up "$ROOT/dir-a" $ID_A >/dev/null 2>&1
# shellcheck disable=SC2086
P_A="$(env_file $ID_A)"
cp "$P_A" "$ROOT/pa.before"
# shellcheck disable=SC2086
run_up "$ROOT/dir-b" $ID_B >/dev/null 2>&1
# shellcheck disable=SC2086
P_B="$(env_file $ID_B)"
[ -n "$P_A" ] && [ -n "$P_B" ] && [ "$P_A" != "$P_B" ]
[ -f "$P_A" ] && [ -f "$P_B" ]
cmp -s "$P_A" "$ROOT/pa.before"   # identity B's up must not clobber identity A's state
grep -q '13310' "$P_A"
grep -q '13311' "$P_B"

# --- Cross-directory reuse: identity A reuses its state from a fresh dir ---
# shellcheck disable=SC2086
run_up "$ROOT/dir-a-fresh" $ID_A >/dev/null 2>&1
cmp -s "$P_A" "$ROOT/pa.before"   # reused (same stored password), not regenerated

# --- down removes only the current identity's state ---
# shellcheck disable=SC2086
(
  cd "$ROOT/dir-a"
  PATH="$BIN:$PATH" XDG_STATE_HOME="$STATE_HOME" env $ID_A bash "$SCRIPT" down
) >/dev/null 2>&1
[ ! -e "$P_A" ]
[ -f "$P_B" ]   # identity B's state survives identity A's down

# --- A mismatched legacy handoff is never migrated and fails fast ---
cat > "$ROOT/legacy-mismatch/.query-e2e-mysql.env" <<'EOF'
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO="query_e2e_ro:0123456789abcdef@tcp(127.0.0.1:13444)/other_db?parseTime=true&charset=utf8mb4"
QUERY_E2E_MYSQL_READONLY_PASSWORD=0123456789abcdef
EOF
ID_C="QUERY_E2E_MYSQL_CONTAINER=fx-c QUERY_E2E_MYSQL_PORT=13312 QUERY_E2E_MYSQL_DATABASE=query_e2e QUERY_E2E_MYSQL_READONLY_USER=query_e2e_ro"
# shellcheck disable=SC2086
if (
  cd "$ROOT/legacy-mismatch"
  PATH="$BIN:$PATH" XDG_STATE_HOME="$STATE_HOME" QUERY_E2E_MYSQL_READY_TIMEOUT=1 env $ID_C bash "$SCRIPT" up
) > /dev/null 2> "$ROOT/mismatch.log"; then
  echo "expected a running fixture with a mismatched legacy handoff to fail fast" >&2
  exit 1
fi
grep -q 'shared fixture state is missing' "$ROOT/mismatch.log"
[ -f "$ROOT/legacy-mismatch/.query-e2e-mysql.env" ]   # mismatched legacy left untouched

# --- Missing state with a running fixture fails fast (no password guessing) ---
if (
  cd "$ROOT/missing"
  PATH="$BIN:$PATH" XDG_STATE_HOME="$ROOT/empty-state" QUERY_E2E_MYSQL_READY_TIMEOUT=1 bash "$SCRIPT" up
) > /dev/null 2> "$ROOT/missing.log"; then
  echo "expected a running fixture without state to fail" >&2
  exit 1
fi
grep -q 'shared fixture state is missing' "$ROOT/missing.log"
