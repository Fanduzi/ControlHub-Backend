#!/usr/bin/env bash
# drive-login-resources.sh — POST /auth/login (QA files) → GET /resources → evidence (no token on disk)
set -euo pipefail

APP_PORT="${APP_PORT:-8080}"
BASE="http://127.0.0.1:${APP_PORT}"
EMAIL_FILE="${CONTROLHUB_QA_EMAIL_FILE:-/workspace/controlhub-qa-login.email}"
PASS_FILE="${CONTROLHUB_QA_PASS_FILE:-/workspace/controlhub-qa-login.pass}"

RUN_DIR="${RUN_DIR:-}"
if [[ -z "$RUN_DIR" ]]; then
  RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$$}"
  RUN_DIR="${CONTROLHUB_API_VERIFY_STATE:-/tmp/controlhub-api-verify}/${RUN_ID}"
fi
EVIDENCE_DIR="${EVIDENCE_DIR:-${RUN_DIR}/evidence}"
mkdir -p "$EVIDENCE_DIR"

fail() { echo "drive-login-resources FAIL: $*" >&2; exit 1; }

[[ -f "$EMAIL_FILE" ]] || fail "email file missing: $EMAIL_FILE"
[[ -f "$PASS_FILE" ]] || fail "password file missing: $PASS_FILE"

EMAIL="$(cat "$EMAIL_FILE" | tr -d '\r\n')"
PASS="$(cat "$PASS_FILE" | tr -d '\r\n')"
[[ -n "$EMAIL" && -n "$PASS" ]] || fail "empty credential file(s)"

# Login — capture body to temp, never leave token in evidence
TMP_LOGIN="$(mktemp)"
trap 'rm -f "$TMP_LOGIN"' EXIT
HTTP_LOGIN="$(curl -sS -o "$TMP_LOGIN" -w '%{http_code}' -X POST "${BASE}/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASS}\"}")"

# Parse keys without storing token: use python
META="$(python3 - "$TMP_LOGIN" "$HTTP_LOGIN" <<'PY'
import json, sys
path, code = sys.argv[1], sys.argv[2]
raw = open(path, "r", encoding="utf-8").read()
print(f"http_status={code}")
try:
    data = json.loads(raw)
except Exception as e:
    print(f"json_error={e}")
    print("keys=")
    sys.exit(0)
keys = sorted(data.keys()) if isinstance(data, dict) else []
print("keys=" + ",".join(keys))
if isinstance(data, dict):
    print(f"has_token={'token' in data}")
    if "role" in data:
        print(f"role={data.get('role')}")
    if "userId" in data:
        print(f"userId={data.get('userId')}")
PY
)"
printf '%s\n' "$META" >"${EVIDENCE_DIR}/login-response.meta.txt"
echo "$META"

[[ "$HTTP_LOGIN" == "200" ]] || fail "login HTTP $HTTP_LOGIN (see meta; body not saved)"

TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("token",""))' "$TMP_LOGIN")"
[[ -n "$TOKEN" ]] || fail "login response missing token"

HTTP_RES="$(curl -sS -o "${EVIDENCE_DIR}/resources.json" -w '%{http_code}' \
  "${BASE}/resources" -H "Authorization: Bearer ${TOKEN}")"
echo "resources_http_status=${HTTP_RES}" | tee "${EVIDENCE_DIR}/resources.http_code"
[[ "$HTTP_RES" == "200" ]] || fail "GET /resources HTTP $HTTP_RES"

# Scrub any accidental token string from resources.json (unlikely) — ensure meta has no token
unset TOKEN PASS EMAIL
rm -f "$TMP_LOGIN"
trap - EXIT

echo "drive-login-resources OK: evidence under ${EVIDENCE_DIR}"
ls -la "${EVIDENCE_DIR}/login-response.meta.txt" "${EVIDENCE_DIR}/resources.json"
