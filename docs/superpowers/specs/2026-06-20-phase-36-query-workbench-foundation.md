# Phase 36 Query Workbench Foundation Design

## Background

ControlHub already tracks database clusters, database instances, profiles,
relations, health, topology, and ownership. That makes it a good source of
truth for deciding which systems could eventually be queried.

However, executing SQL or database commands is a high-risk capability. Phase 36
therefore builds the query workbench foundation without executing queries.

This phase follows:

```text
docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md
```

## Goal

Create a governed Query Workbench shell backed by a read-only query capability
inventory:

```text
existing database resources -> queryTargets read model -> locked Query Workbench shell
```

Users should be able to answer:

- Which database resources could become query targets?
- Which engine and query language apply?
- Which targets have complete connection metadata?
- Which targets are blocked because credentials or policy are missing?
- Which targets are unsupported?
- What the future query workspace will look like once execution is safely
  enabled?

After local Bytebase research, the product direction is explicit: the target
inventory must be embedded in a workbench shell with connection context, schema
browser, worksheet/editor/result/history/access placeholders. Do not implement a
flat inventory-only page.

## Non-Goals

- Do not execute SQL, Redis commands, or MongoDB queries.
- Do not add query credentials.
- Do not add connection pooling.
- Do not add saved queries.
- Do not add query history.
- Do not add export.
- Do not add approval workflows.
- Do not add Admin mode.
- Do not add batch query execution.
- Do not add AI query assistance.
- Do not change backend resource write behavior.
- Do not change migrations unless a later implementation proves a new table is
  required and receives explicit approval.
- Do not tag, release, or deploy.

## Backend Read Model

Add a query target context read model derived from existing resources and
profiles. This is not only a list API. It should provide the frontend with the
structured context needed by the locked Query Workbench shell.

Proposed endpoint:

```text
GET /query-targets
```

Initial response shape:

```json
{
  "items": [
    {
      "resourceId": 22,
      "resourceName": "analytics-ch-node-01-prod",
      "displayName": "Analytics ClickHouse Node 01 Production",
      "resourceType": "database_instance",
      "connectionContext": {
        "environment": "Production",
        "owner": "DBA Team",
        "engine": "clickhouse",
        "host": "prod-ch-host-01.internal",
        "port": 8123,
        "clusterId": 14,
        "clusterName": "Analytics ClickHouse Cluster Production"
      },
      "capability": {
        "queryKind": "sql",
        "editorMode": "sql",
        "languageLabel": "SQL"
      },
      "readiness": "credential_required",
      "missingFields": ["readonlyCredential"],
      "governance": {
        "executionEnabled": false,
        "credentialState": "missing_readonly_credential",
        "auditRequired": true,
        "safetyState": "credential_missing",
        "safetyNote": "Query execution is not enabled in this phase.",
        "policyNotes": [
          "Read-only credentials are required before execution.",
          "Production queries require stricter defaults."
        ]
      },
      "availableActions": {
        "run": false,
        "explain": false,
        "export": false,
        "saveSheet": false,
        "requestAccess": false
      },
      "schemaPreview": [
        {
          "kind": "database",
          "name": "analytics",
          "children": [
            { "kind": "table", "name": "events_daily" }
          ]
        }
      ]
    }
  ]
}
```

### Supported Engines

Initial engine classification:

| Engine | Query kind |
|---|---|
| `mysql` | `sql` |
| `tidb` | `sql` |
| `postgresql` | `sql` |
| `clickhouse` | `sql` |
| `redis` | `redis` |
| `mongodb` | `mongo` |

Unknown engines should not disappear. They should appear with:

```text
queryKind = unsupported
readiness = unsupported_engine
```

### Readiness States

Use explicit states instead of vague booleans:

```text
ready
missing_connection
credential_required
unsupported_engine
disabled
```

Phase 36 normally should not produce `ready` unless an existing source of
read-only credential metadata already exists. If no such metadata exists, use
`credential_required`.

### Missing Fields

Examples:

```text
host
port
engine
readonlyCredential
```

### Safety States

Examples:

```text
credential_missing
execution_disabled
unsupported_engine
connection_incomplete
```

The safety note must make the boundary clear:

```text
Query execution is not enabled in this phase.
```

### Workbench Action Contract

Do not let the frontend guess which actions are available. Phase 36 should
return explicit action flags:

```text
run = false
explain = false
export = false
saveSheet = false
requestAccess = false
```

All of these must remain false in Phase 36 because there is no execution API,
credential model, saved sheet API, export API, or access workflow.

### Schema Preview

`schemaPreview` is optional and lightweight. It may contain objects already
known to ControlHub, but Phase 36 must not connect to databases for live schema
introspection.

If no metadata exists, return an empty array. The frontend will still render a
locked schema placeholder.

## Frontend Workbench Shell

Add a query entry page:

```text
/query
```

The page should show:

- page title: Query Workbench
- safety banner: query execution is not enabled
- target switcher / connection context
- schema/object browser placeholder
- worksheet tabs placeholder
- searchable target list or drawer
- filters:
  - environment
  - engine
  - query kind
  - readiness
- target detail panel or sheet
- disabled editor placeholder explaining why execution is unavailable
- locked result area:
  - Result grid
  - JSON
  - Explain
  - Logs
  - Masking
- query-history placeholder
- access/governance panel:
  - credential state
  - execution disabled state
  - audit requirement
  - JIT/access future state
  - production safety notes

Do not render an enabled Run button.

Allowed disabled affordance:

```text
Run query (disabled)
Reason: read-only credentials are not configured.
```

## UX Copy Requirements

Avoid implying that users can execute queries today.

Good:

```text
Query execution is not enabled.
This target has complete connection metadata but needs read-only credentials.
```

Bad:

```text
Run
Execute SQL
Connected
Ready
```

Only use `ready` if backend evidence supports it.

## Preview And Research Notes

Bytebase-informed preview:

```text
docs/superpowers/previews/phase-36-query-workbench-ide/index.html
```

Research note:

```text
docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md
```

## Testing Strategy

Backend:

- unit tests for engine -> query kind mapping
- unit tests for readiness derivation
- handler tests for `GET /query-targets`
- OpenAPI schema validation
- integration tests if query targets depend on MySQL joins

Frontend:

- component tests for target data, filters, empty states, disabled editor,
  locked result area, schema/history/access placeholders
- service tests for query target fetching
- page tests for `/query`
- E2E smoke for `/query` loading and non-execution boundary

Cross-repo E2E:

- Add to manual E2E only after frontend and backend pieces are merged.
- Verify `/query` loads with real backend data.
- Verify no enabled Run/Execute action appears.

## Success Criteria

Phase 36 is complete when:

- backend exposes query targets from existing resource data
- OpenAPI documents the endpoint
- frontend shows `/query` as a locked Query Workbench shell backed by query
  target data
- query execution is visibly disabled
- tests cover derivation and UI boundary
- manual cross-repo E2E can include `/query` smoke after both repos merge

## Deferred Work

- Phase 37: read-only query sandbox for one SQL engine
- Phase 38: multi-engine query workbench
- Phase 39: governance
