# Phase 38J Governed FK Record Navigation — Evidence

Status: **Implementation complete — ready for human review.**

## Worktrees

| Repo | Path | Branch | Base | Tip |
|------|------|--------|------|-----|
| Backend | `.worktrees/backend-phase-38j-fk-navigation-contract` | `phase-38j-fk-navigation-contract` | `b0e8f2e` (main) | `903c7d8` |

## Endpoint

```text
POST /query-targets/{id}/related-records
```

**Authentication**: Fresh Bearer token required (8-hour TTL, same as query execution).

**Request body**:
```json
{
  "source": {
    "database": "orders",
    "object": "order_items",
    "kind": "table",
    "foreignKey": "fk_order_items_order"
  },
  "localValues": ["42"],
  "maxRows": 100
}
```

**Response**: Bounded result shape (`columns`, `rows`, `rowCount`, `truncated`, `durationMs`, `limitApplied`) plus relation metadata (`sourceDatabase`, `sourceObject`, `foreignKey`, `referencedDatabase`, `referencedObject`, `referencedColumns`, `executedAt`).

## Security Boundaries

| Boundary | Enforcement |
|----------|-------------|
| SQL injection | `quoteMySQLIdentifier` doubles embedded backticks; all identifiers from live schema inspector, not user input |
| Parameter binding | `localValues` bound via `database/sql` placeholders (`?`), never interpolated |
| Identifier quoting | All schema/table/column names backtick-quoted after trusted resolution |
| Production cap | `productionHardMaxRows = 100` enforced in service layer before query construction |
| Error safety | Fixed sentinel messages only; raw inspector/executor errors never exposed |
| Audit secrecy | `persistNavigationAttempt` logs only relation identity (schema.table/fk_name), no localValues/SQL/credentials |
| History secrecy | `statement_preview` and `statement_digest` contain only relation metadata, never user values |
| Credential protection | DSN in private `access.dsn` field; never in response, errors, or logs |

## Integration Proof

The integration test `TestNavigateRelatedRecords_Integration_Success` in `internal/integration/navigate_related_records_integration_test.go`:

1. Creates `schema_child → schema_parent` FK fixture in disposable Testcontainers MySQL
2. Provisions a governed query target with credential
3. Calls `POST /query-targets/{id}/related-records` via real HTTP
4. Asserts HTTP 200 with correct `referencedDatabase` and `referencedObject`
5. **Reads `query_executions` table** to prove:
   - `statement_preview` does NOT contain localValues, SQL, or credential markers
   - `statement_digest` does NOT contain localValues, SQL, or credential markers
   - `error_message` does NOT contain localValues, SQL, or credential markers
6. **Reads `audit_events` table** to prove:
   - `event_type = "related_record_navigation"` (fixed action)
   - `result = "success"` (correct status)

## Gates

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `gofmt -d` (changed files) | clean |
| `go test -count=1 ./...` | 749 passed in 10 packages |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `make openapi-validate` | PASS |
| `make test-integration` | 152 passed |
| `make test-openapi-fuzz` | PASS (exit code 0) |
| `gitnexus detect_changes` | low risk, 0 affected processes |

## Commits

1. `9c686a6` feat: add governed FK record navigation endpoint (Phase 38J Delivery B)
2. `903c7d8` phase-38j: repair FK navigation with typed executor, production cap, and integration tests

## Negative Scope Confirmation

- No SQL guard change
- No query-engine addition / browser DB connection
- No DSN/password/username secrets in browser state, API responses, or display
- No credential edit controls in `/query`
- No schema persistence / browser persistence
- No SHOW CREATE / DDL / ER diagram / Visual Explain / export / saved queries / approval/JIT / notebook / AI / MCP / visual builder / editable grid
- No migrations
- No frontend changes
- No tag, release, or deployment
- No AI co-author trailers

## Remaining P1/P2

**None** after adversarial self-review and integration verification.
