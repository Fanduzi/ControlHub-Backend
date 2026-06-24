#!/usr/bin/env bash
# Phase 37H dedicated Query E2E MySQL fixture (dev/test only).
#
# up/down/status for a disposable Docker MySQL used by the Query Workbench
# ready-target E2E. It creates a query_e2e schema, a seed table, stable seed
# rows, and a SELECT-only user, then writes a gitignored .query-e2e-mysql.env
# containing the read-only credential DSN.
#
# Safety: this script NEVER prints the credential DSN or any password. It only
# logs safe facts (container name, host, port, database name, readiness). The
# password is generated as hex, reused across runs via the env file, passed to
# mysql via MYSQL_PWD (never on the command line), and the env file is mode 0600.
# Every external input (database/user/password/port/timeout/container) is
# whitelisted against a safe charset before any docker call, SQL heredoc, or
# env-file write, so a stray quote/space/shell-metachar can never reach those
# contexts. Validation error strings name only the variable and the failing
# category — they never echo the value, the password, or the DSN.
#
# It does NOT touch the ControlHub metadata database.
set -euo pipefail

CONTAINER="${QUERY_E2E_MYSQL_CONTAINER:-controlhub-query-e2e-mysql}"
PORT="${QUERY_E2E_MYSQL_PORT:-13306}"
DATABASE="${QUERY_E2E_MYSQL_DATABASE:-query_e2e}"
RO_USER="${QUERY_E2E_MYSQL_READONLY_USER:-query_e2e_ro}"
HOST="127.0.0.1"
ENV_FILE=".query-e2e-mysql.env"
REF="LOCAL_QUERY_RO"
IMAGE="mysql:8.0"
READY_TIMEOUT="${QUERY_E2E_MYSQL_READY_TIMEOUT:-90}"

log() { printf '%s\n' "$*" >&2; }

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    log "error: docker is not available"
    exit 1
  fi
}

# Generate an alphanumeric (hex) password — safe inside a MySQL DSN.
gen_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 18
  else
    LC_ALL=C tr -dc 'a-f0-9' </dev/urandom 2>/dev/null | head -c 36 || true
  fi
}

# Read a stored password from the env file so re-runs reuse it (idempotent).
stored_password() {
  [ -f "$ENV_FILE" ] || return 1
  grep "^QUERY_E2E_MYSQL_READONLY_PASSWORD=" "$ENV_FILE" 2>/dev/null | head -1 \
    | sed 's/^QUERY_E2E_MYSQL_READONLY_PASSWORD=//' | tr -d '\r\n'
}

container_running() {
  docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || true
}

wait_ready() {
  # Use a real authenticated query, NOT `mysqladmin ping`: ping can report
  # success against the entrypoint's temporary init server before the real
  # server (with the configured root password) is up, which would let setup
  # run against the temp server and fail. This returns only once root with the
  # fixture password can actually execute a statement.
  local i=0
  while [ "$i" -lt "$READY_TIMEOUT" ]; do
    if docker exec -e MYSQL_PWD="$1" "$CONTAINER" mysql -uroot -N -e "select 1" >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  log "error: $CONTAINER did not become ready within ${READY_TIMEOUT}s"
  exit 1
}

# --- Input validation -------------------------------------------------------
# Every externally-supplied value is whitelisted before any docker run/exec,
# SQL heredoc interpolation, or env-file write. This is defense in depth: even
# though these values flow into SQL, Docker args, and a shell-sourceable env
# file, they are first restricted to a safe charset so a stray quote, space, or
# shell/SQL metacharacter can never reach those contexts. Error messages are
# FIXED strings naming only the variable and the failing category — they NEVER
# echo the supplied value, the password, or the DSN.

# require_match exits 1 unless value fully matches the regex. The regex is held
# in a variable and used unquoted so bash treats it as a regex, not a literal.
require_match() {
  local name="$1" value="$2" regex="$3" category="$4"
  if [[ ! "$value" =~ $regex ]]; then
    log "error: $name must match $category (rejected; value not shown)"
    exit 1
  fi
}

# require_port exits 1 unless value is an integer in the TCP port range 1..65535.
require_port() {
  local name="$1" value="$2"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -lt 1 ] || [ "$value" -gt 65535 ]; then
    log "error: $name must be an integer in 1..65535"
    exit 1
  fi
}

# require_positive_int exits 1 unless value is a positive integer.
require_positive_int() {
  local name="$1" value="$2"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -lt 1 ]; then
    log "error: $name must be a positive integer"
    exit 1
  fi
}

# validate_explicit_password exits 1 if a user-supplied password fails the
# whitelist. Only an EXPLICITLY set password is validated; the generated hex
# password and the stored (previously written) password are always safe and are
# not validated here.
validate_explicit_password() {
  local pw="$1"
  local len=${#pw}
  if ! [[ "$pw" =~ ^[A-Za-z0-9_]+$ ]] || [ "$len" -lt 12 ] || [ "$len" -gt 128 ]; then
    log "error: QUERY_E2E_MYSQL_READONLY_PASSWORD must be alphanumeric/underscore, 12..128 chars (rejected; value not shown)"
    exit 1
  fi
}

# validate_inputs whitelists every external input once, before the command
# dispatch, so up/down/status all fail closed on bad input regardless of docker
# availability.
validate_inputs() {
  require_match "QUERY_E2E_MYSQL_DATABASE" "$DATABASE" '^[A-Za-z0-9_]+$' "alphanumeric/underscore only"
  require_match "QUERY_E2E_MYSQL_READONLY_USER" "$RO_USER" '^[A-Za-z0-9_]+$' "alphanumeric/underscore only"
  require_match "QUERY_E2E_MYSQL_CONTAINER" "$CONTAINER" '^[A-Za-z0-9_.-]+$' "a safe container name (alphanumeric/_/./-)"
  require_port "QUERY_E2E_MYSQL_PORT" "$PORT"
  require_positive_int "QUERY_E2E_MYSQL_READY_TIMEOUT" "$READY_TIMEOUT"
  if [ -n "${QUERY_E2E_MYSQL_READONLY_PASSWORD:-}" ]; then
    validate_explicit_password "$QUERY_E2E_MYSQL_READONLY_PASSWORD"
  fi
}

cmd_up() {
  require_docker

  # Determine the fixture password: explicit env > stored (reuse) > generated.
  if [ -n "${QUERY_E2E_MYSQL_READONLY_PASSWORD:-}" ]; then
    PW="$QUERY_E2E_MYSQL_READONLY_PASSWORD"
  elif stored="$(stored_password)" && [ -n "$stored" ]; then
    PW="$stored"
  else
    PW="$(gen_password)"
    [ -n "$PW" ] || { log "error: could not generate a fixture password"; exit 1; }
  fi

  local running
  running="$(container_running)"
  if [ "$running" = "true" ]; then
    log "$CONTAINER already running (host=$HOST port=$PORT database=$DATABASE)"
  elif [ -n "$running" ]; then
    log "starting existing $CONTAINER"
    docker start "$CONTAINER" >/dev/null
  else
    log "creating $CONTAINER (host=$HOST port=$PORT database=$DATABASE)"
    # MYSQL_ROOT_PASSWORD is required by the image on first init only; the value
    # is the local fixture password (hex), not printed here.
    docker run -d --name "$CONTAINER" \
      -p "${PORT}:3306" \
      -e MYSQL_ROOT_PASSWORD="$PW" \
      -e MYSQL_DATABASE="$DATABASE" \
      "$IMAGE" >/dev/null
  fi

  wait_ready "$PW"

  # Schema/database is created by MYSQL_DATABASE on init; ensure table, seed rows,
  # and the SELECT-only user. Safe to re-run. MYSQL_PWD authenticates root without
  # putting the password on the command line.
  docker exec -i -e MYSQL_PWD="$PW" "$CONTAINER" mysql -uroot "$DATABASE" >&2 <<SQL
create table if not exists query_e2e_items (
  id bigint unsigned not null primary key,
  name varchar(64) not null,
  category varchar(32) not null,
  created_at timestamp not null default current_timestamp
);
insert into query_e2e_items (id, name, category) values
  (1, 'alpha', 'sample'),
  (2, 'beta',  'sample')
on duplicate key update
  name = values(name),
  category = values(category);
create user if not exists '$RO_USER'@'%' identified by '$PW';
grant select on $DATABASE.* to '$RO_USER'@'%';
flush privileges;
SQL

  # Write the gitignored env file (credential DSN + reuse password). Mode 0600.
  # The DSN value is double-quoted so the file is shell-sourceable (`set -a; . file`
  # would otherwise break on the DSN's '&'); godotenv also strips the quotes.
  # Never echoed to stdout/stderr.
  (
    umask 077
    {
      printf 'CONTROLHUB_QUERY_CREDENTIAL_%s="%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4"\n' \
        "$REF" "$RO_USER" "$PW" "$HOST" "$PORT" "$DATABASE"
      printf 'QUERY_E2E_MYSQL_READONLY_PASSWORD=%s\n' "$PW"
    } > "$ENV_FILE"
  )

  log "$CONTAINER ready (host=$HOST port=$PORT database=$DATABASE)"
  log "credential DSN written to $ENV_FILE (gitignored; not printed)"
  log "source it for the server and seed: set -a; . ./$ENV_FILE; set +a"
}

cmd_down() {
  require_docker
  if [ -n "$(docker ps -aq -f name="^${CONTAINER}\$")" ]; then
    log "removing $CONTAINER"
    docker rm -f "$CONTAINER" >/dev/null
  else
    log "$CONTAINER not present"
  fi
  # Remove the local env file (regenerated on next up); ignore if absent.
  [ -f "$ENV_FILE" ] && rm -f "$ENV_FILE"
  log "done"
}

cmd_status() {
  require_docker
  local running
  running="$(container_running)"
  if [ "$running" = "true" ]; then
    log "$CONTAINER running (host=$HOST port=$PORT database=$DATABASE)"
    exit 0
  elif [ -n "$running" ]; then
    log "$CONTAINER exists but is not running"
    exit 0
  else
    log "$CONTAINER not present"
    exit 1
  fi
}

usage() {
  log "usage: $0 {up|down|status}"
  exit 2
}

# Fail closed on bad external input before any docker call, heredoc, or env-file
# write (all subcommands validate).
validate_inputs

case "${1:-}" in
  up) cmd_up ;;
  down) cmd_down ;;
  status) cmd_status ;;
  *) usage ;;
esac
