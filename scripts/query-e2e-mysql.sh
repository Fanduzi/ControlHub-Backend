#!/usr/bin/env bash
# Phase 37H/38I dedicated Query E2E MySQL fixture (dev/test only).
# input: docker, openssl, XDG_STATE_HOME (or HOME) and optional QUERY_E2E_MYSQL_* overrides
# output: per-user mode-0600 fixture credential state isolated per fixture identity (container/port/database/readonly user); `env-file` prints the current identity's path only
# pos: owns the shared Query E2E fixture credential handoff used by run-query-dev.sh
# note: update scripts/README.md and run-query-dev.sh when this handoff changes
#
# up/down/status for a disposable Docker MySQL used by the Query Workbench
# ready-target E2E. It creates a query_e2e schema (seed table, stable rows),
# a query_e2e_aux schema (parent/child tables, view, composite index,
# secondary index, foreign key), and a SELECT-only user with access to both
# databases, then writes a per-user credential state file (isolated per
# fixture identity: container, port, database, read-only user) containing the
# read-only credential DSN. A legacy gitignored .query-e2e-mysql.env is
# migrated once when present and matching the current identity's port and
# database.
#
# Safety: this script NEVER prints the credential DSN or any password. It only
# logs safe facts (container name, host, port, database name, readiness). The
# password is generated as hex, reused across worktrees via the state file,
# passed to mysql via MYSQL_PWD (never on the command line), and state is mode 0600.
# Every external input (database/user/password/port/timeout/container) is
# whitelisted against a safe charset before any docker call, SQL heredoc, or
# state-file write, so a stray quote/space/shell-metachar can never reach those
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
LEGACY_ENV_FILE=".query-e2e-mysql.env"
STATE_HOME="${XDG_STATE_HOME:-${HOME:?HOME is required when XDG_STATE_HOME is unset}/.local/state}"
STATE_DIR="$STATE_HOME/controlhub"
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

# Read a stored password from the per-user state so worktrees reuse it.
stored_password() {
  [ -f "$STATE_FILE" ] || return 1
  grep "^QUERY_E2E_MYSQL_READONLY_PASSWORD=" "$STATE_FILE" 2>/dev/null | head -1 \
    | sed 's/^QUERY_E2E_MYSQL_READONLY_PASSWORD=//' | tr -d '\r\n'
}

# Move the old worktree-local handoff into the CURRENT identity's state once,
# without ever displaying it. The legacy file records no container or user, so
# migration only proceeds when its DSN port and database match the current
# identity; a mismatched legacy file is left untouched for the worktree that
# owns it.
migrate_legacy_state() {
  [ -f "$STATE_FILE" ] && return 0
  [ -f "$LEGACY_ENV_FILE" ] || return 0

  # Compare only the port and database parsed from the DSN we previously
  # wrote (validated shape; never displayed). No match => not this identity.
  local legacy_port_db
  legacy_port_db="$(sed -n 's#^CONTROLHUB_QUERY_CREDENTIAL_[A-Za-z0-9_]*="[^"]*@tcp([^:]*:\([0-9][0-9]*\))/\([^?]*\)?[^"]*"$#\1 \2#p' "$LEGACY_ENV_FILE" | head -1)"
  if [ "$legacy_port_db" != "$PORT $DATABASE" ]; then
    return 0
  fi

  local tmp
  umask 077
  mkdir -p "$STATE_DIR"
  tmp="$(mktemp "$STATE_DIR/.query-e2e-mysql.env.XXXXXX")"
  trap 'rm -f "$tmp"' RETURN
  cat "$LEGACY_ENV_FILE" > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$STATE_FILE"
  rm -f "$LEGACY_ENV_FILE"
  trap - RETURN
  log "migrated legacy fixture state to the per-user state directory (not printed)"
}

write_state() {
  local password="$1" tmp
  umask 077
  mkdir -p "$STATE_DIR"
  tmp="$(mktemp "$STATE_DIR/.query-e2e-mysql.env.XXXXXX")"
  trap 'rm -f "$tmp"' RETURN
  {
    printf 'CONTROLHUB_QUERY_CREDENTIAL_%s="%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4"\n' \
      "$REF" "$RO_USER" "$password" "$HOST" "$PORT" "$DATABASE"
    printf 'QUERY_E2E_MYSQL_READONLY_PASSWORD=%s\n' "$password"
  } > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$STATE_FILE"
  trap - RETURN
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
# SQL heredoc interpolation, or state-file write. This is defense in depth: even
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

  # A legacy worktree handoff is upgraded before deciding whether a running
  # fixture can be reused. The shared state is then the only credential source.
  if [ ! -f "$STATE_FILE" ] && [ -f "$LEGACY_ENV_FILE" ]; then
    migrate_legacy_state
  fi

  local running
  running="$(container_running)"

  # Determine the fixture password: explicit env > stored (reuse) > generated.
  if [ -n "${QUERY_E2E_MYSQL_READONLY_PASSWORD:-}" ]; then
    PW="$QUERY_E2E_MYSQL_READONLY_PASSWORD"
  elif stored="$(stored_password)" && [ -n "$stored" ]; then
    PW="$stored"
  else
    if [ -n "$running" ]; then
      log "error: $CONTAINER exists but shared fixture state is missing; run query-e2e-mysql.sh once from the worktree that still has $LEGACY_ENV_FILE"
      exit 1
    fi
    PW="$(gen_password)"
    [ -n "$PW" ] || { log "error: could not generate a fixture password"; exit 1; }
  fi

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

  AUX_DATABASE="query_e2e_aux"

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

create table if not exists qe_explain_big (
  id bigint unsigned not null primary key,
  payload varchar(64) not null,
  created_at timestamp not null default current_timestamp
) engine=InnoDB;

insert ignore into qe_explain_big (id, payload) values
  (1, 'row-001'),
  (2, 'row-002'),
  (3, 'row-003'),
  (4, 'row-004'),
  (5, 'row-005'),
  (6, 'row-006'),
  (7, 'row-007'),
  (8, 'row-008'),
  (9, 'row-009'),
  (10, 'row-010'),
  (11, 'row-011'),
  (12, 'row-012'),
  (13, 'row-013'),
  (14, 'row-014'),
  (15, 'row-015'),
  (16, 'row-016'),
  (17, 'row-017'),
  (18, 'row-018'),
  (19, 'row-019'),
  (20, 'row-020'),
  (21, 'row-021'),
  (22, 'row-022'),
  (23, 'row-023'),
  (24, 'row-024'),
  (25, 'row-025'),
  (26, 'row-026'),
  (27, 'row-027'),
  (28, 'row-028'),
  (29, 'row-029'),
  (30, 'row-030'),
  (31, 'row-031'),
  (32, 'row-032'),
  (33, 'row-033'),
  (34, 'row-034'),
  (35, 'row-035'),
  (36, 'row-036'),
  (37, 'row-037'),
  (38, 'row-038'),
  (39, 'row-039'),
  (40, 'row-040'),
  (41, 'row-041'),
  (42, 'row-042'),
  (43, 'row-043'),
  (44, 'row-044'),
  (45, 'row-045'),
  (46, 'row-046'),
  (47, 'row-047'),
  (48, 'row-048'),
  (49, 'row-049'),
  (50, 'row-050'),
  (51, 'row-051'),
  (52, 'row-052'),
  (53, 'row-053'),
  (54, 'row-054'),
  (55, 'row-055'),
  (56, 'row-056'),
  (57, 'row-057'),
  (58, 'row-058'),
  (59, 'row-059'),
  (60, 'row-060'),
  (61, 'row-061'),
  (62, 'row-062'),
  (63, 'row-063'),
  (64, 'row-064'),
  (65, 'row-065'),
  (66, 'row-066'),
  (67, 'row-067'),
  (68, 'row-068'),
  (69, 'row-069'),
  (70, 'row-070'),
  (71, 'row-071'),
  (72, 'row-072'),
  (73, 'row-073'),
  (74, 'row-074'),
  (75, 'row-075'),
  (76, 'row-076'),
  (77, 'row-077'),
  (78, 'row-078'),
  (79, 'row-079'),
  (80, 'row-080'),
  (81, 'row-081'),
  (82, 'row-082'),
  (83, 'row-083'),
  (84, 'row-084'),
  (85, 'row-085'),
  (86, 'row-086'),
  (87, 'row-087'),
  (88, 'row-088'),
  (89, 'row-089'),
  (90, 'row-090'),
  (91, 'row-091'),
  (92, 'row-092'),
  (93, 'row-093'),
  (94, 'row-094'),
  (95, 'row-095'),
  (96, 'row-096'),
  (97, 'row-097'),
  (98, 'row-098'),
  (99, 'row-099'),
  (100, 'row-100');
SQL

  # Auxiliary database with richer schema objects for schema metadata tests.
  docker exec -i -e MYSQL_PWD="$PW" "$CONTAINER" mysql -uroot >&2 <<SQL
create database if not exists \`$AUX_DATABASE\`;
use \`$AUX_DATABASE\`;

create table if not exists schema_parent (
  id bigint unsigned not null auto_increment,
  parent_code varchar(32) not null,
  label varchar(128) not null default '',
  created_at timestamp not null default current_timestamp,
  primary key (id),
  unique key uq_schema_parent_code (parent_code),
  key idx_schema_parent_label (label)
) engine=InnoDB;

create table if not exists schema_child (
  id bigint unsigned not null auto_increment,
  parent_id bigint unsigned not null,
  child_name varchar(64) not null,
  sort_order int not null default 0,
  primary key (id),
  key idx_schema_child_parent (parent_id, sort_order),
  constraint fk_schema_child_parent foreign key (parent_id) references schema_parent (id) on update cascade on delete restrict
) engine=InnoDB;

-- Pagination fixture: 26 deterministic tables so schema object page 2 is real.
-- Names sort after schema_parent (zz prefix) but before any future view.
-- Each table has a minimal shape: numeric PK + non-sensitive label.

create table if not exists schema_zz_page_01 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_02 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_03 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_04 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_05 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_06 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_07 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_08 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_09 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_10 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_11 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_12 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_13 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_14 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_15 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_16 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_17 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_18 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_19 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_20 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_21 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_22 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_23 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_24 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_25 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create table if not exists schema_zz_page_26 (
  id bigint unsigned not null primary key,
  label varchar(128) not null default ''
) engine=InnoDB;

create or replace view schema_parent_summary as
  select id, parent_code, label from schema_parent;

insert ignore into schema_parent (id, parent_code, label) values
  (1, 'P_ALPHA', 'Alpha Parent'),
  (2, 'P_BETA',  'Beta Parent');

insert ignore into schema_child (id, parent_id, child_name, sort_order) values
  (1, 1, 'child_a1', 1),
  (2, 1, 'child_a2', 2),
  (3, 2, 'child_b1', 1);
SQL

  # Grant SELECT on both application databases only.
  docker exec -i -e MYSQL_PWD="$PW" "$CONTAINER" mysql -uroot >&2 <<SQL
create user if not exists '$RO_USER'@'%' identified by '$PW';
grant select on \`$DATABASE\`.* to '$RO_USER'@'%';
grant select on \`$AUX_DATABASE\`.* to '$RO_USER'@'%';
flush privileges;
SQL

  # Write shared state atomically. The double-quoted DSN remains shell-sourceable.
  write_state "$PW"

  log "$CONTAINER ready (host=$HOST port=$PORT database=$DATABASE)"
  log "credential DSN written to per-user state (mode 0600; not printed)"
}

cmd_down() {
  require_docker
  if [ -n "$(docker ps -aq -f name="^${CONTAINER}\$")" ]; then
    log "removing $CONTAINER"
    docker rm -f "$CONTAINER" >/dev/null
  else
    log "$CONTAINER not present"
  fi
  # Remove only the CURRENT identity's state (regenerated on next up). Other
  # fixture identities keep their own state; the legacy worktree env file is
  # left for migration, never removed here.
  rm -f "$STATE_FILE"
  log "done"
}

cmd_env_file() {
  printf '%s\n' "$STATE_FILE"
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
  log "usage: $0 {up|down|status|env-file}"
  exit 2
}

# Fail closed on bad external input before any docker call, heredoc, or state
# write (all subcommands validate).
validate_inputs

# Identity-scoped state path, derived only from the VALIDATED fixture identity
# (container, port, database, read-only user) so worktrees using different
# fixture identities never share — or clobber — each other's credential state.
# The identity is joined with ':' because no component charset validated above
# contains ':', making the join unambiguous and collision-free.
STATE_FILE="$STATE_DIR/query-e2e-mysql--${CONTAINER}:${PORT}:${DATABASE}:${RO_USER}.env"

case "${1:-}" in
  up) cmd_up ;;
  down) cmd_down ;;
  status) cmd_status ;;
  env-file) cmd_env_file ;;
  *) usage ;;
esac
