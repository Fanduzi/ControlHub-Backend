# Resources list

Authenticated inventory list/detail: `GET /resources` and `GET /resources/{id}` requiring user or machine scope `inventory:read`.

## Sub-features

- `list` — bearer `GET /resources` returns HTTP 200 with object `{ "items": [...], "pageInfo": {...} }` (not a bare array). Defaults page=1, pageSize=20 (capped at 500).
- `detail` — `GET /resources/{id}` returns `{ "resource": { ... } }`. Invalid id → 400; missing → 404.
- `unauthorized` — missing bearer → 401 (optional negative check).

## How to get to it (user POV)

- Obtain a token via `/auth/login`, then call `/resources` with `Authorization: Bearer <token>`.

## Driving it with curl

Preconditions: successful login; token in memory only.

```bash
PORT="${APP_PORT:-8080}"
curl -fsS "http://127.0.0.1:${PORT}/resources" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o "${EVIDENCE_DIR}/resources.json"
# Optional first-id detail:
# curl -fsS "http://127.0.0.1:${PORT}/resources/${ID}" -H "Authorization: Bearer ${TOKEN}" \
#   -o "${EVIDENCE_DIR}/resource-detail.json"

# Helper covers login→list:
.cursor/skills/verify-controlhub-api/scripts/drive-login-resources.sh
```

## Gotchas

- Empty `items` can be valid on a freshly migrated DB without demo seed — record the empty payload as proof of auth+route.
- Do not write the bearer token into `${EVIDENCE_DIR}`.
- List supports additional filters (`resourceType`, `environmentId`, archive flags, `q`, …) documented in OpenAPI; baseline proof only requires `items` + `pageInfo`.
- FE inventory UI is separate; this feature is API-only.
