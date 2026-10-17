# Auth login

Operator (or fixture) login via `POST /auth/login`. Returns OpenAPI `LoginResponse`: `token`, `role`, `userId`. Token is used as `Authorization: Bearer` for subsequent calls.

## Sub-features

- `login-success` — valid email/password → HTTP 200 with `token`, `role`, `userId` keys.
- `login-reject` — wrong password / inactive / unknown user → `401` with `invalid_credentials` (malformed JSON → `400 invalid_payload`; unexpected failures → `500 internal_error`).

## How to get to it (user POV)

- POST JSON `{ "email", "password" }` to `/auth/login` on the API base URL. No prior session cookie required (API is bearer-oriented; console BFF is a separate FE concern).
- Accounts are **not** seeded at server startup. After migrations, provision an admin with `BOOTSTRAP_ADMIN_EMAIL` / `BOOTSTRAP_ADMIN_PASSWORD` and `go run ./cmd/bootstrap-admin`. Migration `00016_disable_seed_users.sql` retires historical `admin@example.com` / `editor@example.com` identities.

## Driving it with curl

Preconditions: `/health` ok; an active admin exists (bootstrap-admin); QA credential files exist at `/workspace/controlhub-qa-login.email` and `/workspace/controlhub-qa-login.pass`.

```bash
PORT="${APP_PORT:-8080}"
EMAIL_FILE=/workspace/controlhub-qa-login.email
PASS_FILE=/workspace/controlhub-qa-login.pass
# Preferred: scripts/drive-login-resources.sh (redacts token from disk)
.cursor/skills/verify-controlhub-api/scripts/drive-login-resources.sh

# Manual equivalent (token only in shell memory — do not tee raw JSON to evidence):
RESP="$(curl -sS -X POST "http://127.0.0.1:${PORT}/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$(cat "$EMAIL_FILE")\",\"password\":\"$(cat "$PASS_FILE")\"}")"
# Assert keys without writing token → ${EVIDENCE_DIR}/login-meta.txt
```

Evidence from helper: `${EVIDENCE_DIR}/login-meta.txt` and `${EVIDENCE_DIR}/login.redacted.json` (status + role / userId; **no** token value).

## Gotchas

- Never commit or log the password file contents or JWT.
- Do not use retired `admin@example.com` / `secret123` on a migrated DB.
- Handler does not enforce OpenAPI `format: email` beyond decode + normalize; do not assert format rejection unless implementation changes.
- Console BFF session is **not** proven by this API login alone — use the FE verify skill for that.
