#!/usr/bin/env bash
# input: CONTROLHUB_FUZZ_BEARER_TOKEN, running ControlHub server, Schemathesis
# output: authenticated OpenAPI fuzz report
# pos: Exercises protected API operations against the OpenAPI contract
# note: if this file changes, update scripts/README.md
# note: fuzz exclusions are a governed contract — see the OpenAPI Fuzz Exclusion Contract in scripts/README.md
# openapi-fuzz.sh — Run Schemathesis against a ControlHub server.
#
# Usage: openapi-fuzz.sh <base_url>
#   base_url: e.g. http://127.0.0.1:38001
#
# Requires: schemathesis (pip install schemathesis)
#
# Exit codes:
#   0 — all checks passed
#   1 — Schemathesis found contract violations
#   2 — prerequisites missing or invocation error

set -euo pipefail

BASE_URL="${1:?Usage: $0 <base_url>}"
OPENAPI_URL="${BASE_URL}/openapi.yaml"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPORT_DIR="${SCRIPT_DIR}/../.schemathesis-reports"
CONFIG_FILE="${SCRIPT_DIR}/schemathesis.toml"

# Bounded run configuration suitable for AI agents and local development.
MAX_EXAMPLES=50
SEED=42
CHECKS="not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance"

# Verify schemathesis is available.
if ! command -v sth &>/dev/null && ! command -v schemathesis &>/dev/null; then
    echo "ERROR: schemathesis CLI not found." >&2
    echo "Install with: pip install schemathesis" >&2
    echo "Or with: pipx install schemathesis" >&2
    exit 2
fi

# Use 'sth' if available (newer Schemathesis), else 'schemathesis'.
STH_BIN="sth"
if ! command -v sth &>/dev/null; then
    STH_BIN="schemathesis"
fi

echo "=== Schemathesis OpenAPI Fuzz ==="
echo "Server:       ${BASE_URL}"
echo "OpenAPI spec: ${OPENAPI_URL}"
echo "CLI:          ${STH_BIN}"
echo "Max examples: ${MAX_EXAMPLES}"
echo "Seed:         ${SEED}"
echo "Checks:       ${CHECKS}"
echo ""

# Verify server is reachable.
if ! curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
    echo "ERROR: server at ${BASE_URL} is not reachable." >&2
    exit 2
fi

echo "Server health check passed."
echo ""

# Create report directory.
mkdir -p "${REPORT_DIR}"

# Run Schemathesis.
echo "Starting fuzz run..."
echo ""

if [ -z "${CONTROLHUB_FUZZ_BEARER_TOKEN:-}" ]; then
    echo "CONTROLHUB_FUZZ_BEARER_TOKEN is required for protected API fuzzing." >&2
    exit 2
fi

# executeSavedStatement is excluded per the OpenAPI Fuzz Exclusion Contract
# (scripts/README.md): no stable disposable fuzz fixture can construct a valid
# governed execution, and fuzzed values must never reach a real query target.
# Narrow single-operation scope only; TestOpenAPIFuzzExclusionContract enforces it.
set +e
"$STH_BIN" --config-file "${CONFIG_FILE}" run "${OPENAPI_URL}" \
    --url "${BASE_URL}" \
    --max-examples "${MAX_EXAMPLES}" \
    --seed "${SEED}" \
    --checks "${CHECKS}" \
    --exclude-operation-id executeSavedStatement \
    --mode all \
    --phases examples,fuzzing \
    --header "Authorization: Bearer ${CONTROLHUB_FUZZ_BEARER_TOKEN}" \
    --report junit \
    --report-dir "${REPORT_DIR}"
STH_EXIT=$?
set +e

echo ""
if [ "$STH_EXIT" -eq 0 ]; then
    echo "Schemathesis: all checks passed."
else
    echo "Schemathesis: found contract violations (exit ${STH_EXIT})."
    echo "Reports saved to ${REPORT_DIR}/"
fi

exit $STH_EXIT
