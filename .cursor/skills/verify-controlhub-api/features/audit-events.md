# Audit events

Baseline audit feed via `GET /audit-events` (admin user or machine with `audit:read`). Resource-scoped: `GET /resources/{id}/audit-events`.

## Sub-features

- `audit-feed` — bearer `GET /audit-events` → HTTP 200 JSON feed (or explicit 401/403 if the QA user lacks admin/audit scope — report as unmet RBAC precondition, not a silent skip).
- `resource-audit` — optional `GET /resources/{id}/audit-events` when a resource id is known.

## How to get to it (user POV)

- Sign in as an admin-capable operator (QA admin file identities), then GET `/audit-events` with the bearer token. The console mirrors this on `/audits` (FE skill).

## Driving it with curl

Preconditions: login as a principal with audit access.

```bash
PORT="${APP_PORT:-8080}"
code="$(curl -sS -o "${EVIDENCE_DIR}/audit-events.json" -w '%{http_code}' \
  "http://127.0.0.1:${PORT}/audit-events" \
  -H "Authorization: Bearer ${TOKEN}")"
echo "$code" | tee "${EVIDENCE_DIR}/audit-events.http_code"
# Expect 200 for admin/audit:read; if 401/403, record role from login meta and stop — do not invent a bypass.
```

## Gotchas

- Non-admin tokens may legitimately fail — that is RBAC, not a broken route. Capture status code.
- Do not dump unrelated PII from audit payloads into chat; keep evidence on disk under `${EVIDENCE_DIR}`.
- FE `/audits` is `adminOnly` in nav; API proof remains the HTTP path above.
