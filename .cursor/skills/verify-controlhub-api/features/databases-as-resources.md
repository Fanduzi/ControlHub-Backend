# Databases as resources

There is **no** backend `/databases` path. Database clusters/instances/proxies are inventory rows filtered with repeated `resourceType` query params: `database_instance`, `database_cluster`, `database_proxy` (as the FE `listDatabaseResources()` does).

## Sub-features

- `filter-database-types` — `GET /resources?resourceType=database_instance&resourceType=database_cluster&resourceType=database_proxy` with bearer → HTTP 200 JSON.
- `env-filter` — optional combine with environment query params used by the console (slug→id via `/environments` when needed).

## How to get to it (user POV)

- As an API client with `inventory:read`, request `/resources` with the database `resourceType` filters above.

## Driving it with curl

Preconditions: login token available.

```bash
PORT="${APP_PORT:-8080}"
curl -fsS -G "http://127.0.0.1:${PORT}/resources" \
  --data-urlencode "resourceType=database_instance" \
  --data-urlencode "resourceType=database_cluster" \
  --data-urlencode "resourceType=database_proxy" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o "${EVIDENCE_DIR}/databases-as-resources.json"
```

## Gotchas

- Hitting `/databases` on the API is expected to 404 — that is not a regression; the FE route is UI-only composition.
- Empty result with 200 is success for the filter contract when no database-typed rows are seeded.
- Query Workbench targets (`/query-targets`) are a different surface — do not conflate with this filter.
