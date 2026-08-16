#!/usr/bin/env bash
# Local Query Workbench dev launcher (dev-only, invoked via `make run-query-dev`).
#
# Ensures the dedicated Query E2E Docker MySQL fixture is up (idempotent),
# SOURCES the gitignored .query-e2e-mysql.env it writes — the credential DSN in
# that file is double-quoted and shell-sourceable by design, so it must be
# sourced, never copied/parsed — idempotently ensures the Local MySQL Query Dev
# target metadata, then starts the backend on APP_PORT (default 8080) with a
# fresh ephemeral JWT_SECRET that overrides any placeholder exported from .env.
#
# Safety: this script NEVER prints the credential DSN, passwords, tokens, or the
# ephemeral JWT_SECRET. It fails loudly when the fixture env cannot be loaded.
set -euo pipefail

# 1. Ensure the dedicated query fixture is available (idempotent). Writes
#    .query-e2e-mysql.env with the read-only credential DSN when it changes.
bash scripts/query-e2e-mysql.sh up

# 2. Safely source the fixture env file instead of extracting values: a manual
#    copy of the double-quoted DSN keeps the surrounding quotes and breaks
#    schema requests. `set -a` exports every sourced var for the seed and the
#    server below. Fail loudly if the file cannot be loaded.
ENV_FILE=".query-e2e-mysql.env"
if [ ! -r "$ENV_FILE" ]; then
  echo "error: $ENV_FILE not found after fixture up; the query fixture env could not be loaded" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
. "./$ENV_FILE"
set +a

REF="${QUERY_DEV_CREDENTIAL_REF:-LOCAL_QUERY_RO}"
# Fail closed on a crafted REF before it reaches env/grep or the seed command.
case "$REF" in
  *[!A-Za-z0-9_]*) echo "error: QUERY_DEV_CREDENTIAL_REF must be alphanumeric/underscore (rejected; value not shown)" >&2; exit 1 ;;
esac
CRED_VAR="CONTROLHUB_QUERY_CREDENTIAL_${REF}"
if ! env | grep -q "^${CRED_VAR}="; then
  echo "error: $CRED_VAR not set after sourcing $ENV_FILE; the fixture env failed to load" >&2
  exit 1
fi

# 3. Idempotently ensure the Local MySQL Query Dev target metadata (resource,
#    profile, credential metadata, disclosure policies). Direct go run keeps
#    .env precedence out of the way; DATABASE_DSN comes from .env or the shell.
QUERY_DEV_ALLOW_TARGET_FIXTURE=true \
QUERY_DEV_CREDENTIAL_REF="$REF" \
go run ./cmd/querydev

# 4. Start the backend on APP_PORT (default 8080) with a fresh ephemeral
#    JWT_SECRET. The env prefix overrides any placeholder exported from .env,
#    and godotenv never overwrites an already-set variable. Never printed.
if ! command -v openssl >/dev/null 2>&1; then
  echo "error: openssl is required to generate the ephemeral JWT_SECRET" >&2
  exit 1
fi
JWT_SECRET="$(openssl rand -hex 32)" \
go run ./cmd/server
