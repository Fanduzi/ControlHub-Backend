# Decision: Governed Table Definition Is A Separate, Table-Only Backend Contract

## Status

Proposed 2026-07-15. This decision refines the Delivery B boundary deferred by
the Phase 38K Object Inspector metadata decision.

## Context

The schema service already resolves target access, uses a MySQL read-only
transaction, and exposes normalized object metadata. Object Inspector consumes
that metadata locally. Definition text is different: MySQL requires an
identifier in `SHOW CREATE TABLE`, the output is engine-specific, and it can
contain table comments and default literals. Folding it into object detail
would silently widen a cached normalized contract and blur its disclosure
boundary.

## Decision

### Add A Dedicated Table-Definition Read

Add `GET /query-targets/{id}/schema/table-definition` for a database/name
pair. It is served by `QuerySchemaService`, protected by the current fresh
query actor middleware, and declared in OpenAPI. It does not modify the
existing object-detail shape.

The service resolves target access before inspecting the database. The
inspector uses a parameterized `information_schema.TABLES` lookup to verify an
exact `BASE TABLE`. It then uses a local MySQL identifier quote helper that
doubles embedded backticks to construct `SHOW CREATE TABLE` from the verified
identity. The browser provides only typed request parameters and never builds
DDL SQL.

### Initial Scope Is MySQL Base Tables Only

The endpoint accepts base tables only. It explicitly rejects views instead of
falling back to `SHOW CREATE VIEW`; MySQL view definitions may disclose a
`DEFINER` identity. Routines, triggers, grants, events, and generic metadata
commands are similarly excluded. A later engine or object-kind needs a separate
contract and security review.

### Treat Definition Text As Controlled, Ephemeral Schema Metadata

The response returns engine-generated `SHOW CREATE TABLE` text, capped to 64
KiB at a UTF-8 boundary with an explicit `truncated` flag. There is no ad-hoc
redaction: stripping SQL with regex would be unreliable and could claim safety
without delivering it. Instead, the endpoint has a deliberate narrow audience
(governed target actors), table-only scope, no caching, no history entry, and a
fixed audit record that omits definition text and object values.

This is a schema-disclosure capability, not a query-result capability. It must
not be sent to local storage, URLs, logs, query execution records, or audit
metadata. The lack of cache is intentional: it prevents retention beyond the
request and avoids stale definitions.

### Preserve Existing Schema And Query Boundaries

Reuse `TargetAccessResolver`, existing schema error mapping, and the existing
read-only MySQL connection pattern. Do not relax the SQL guard, introduce a
generic executor method, change credential policy, or add a browser database
connection. The endpoint owns no result rows and does not create a worksheet.

## Consequences

- Operators can inspect actual MySQL table DDL through the same governed target
  access boundary as schema metadata.
- The product has a reviewable response cap, explicit truncation signal, and a
  fixed audit trail without persisting definition content.
- A future frontend can be built against a stable typed endpoint rather than
  deriving DDL from normalized metadata or invoking arbitrary SQL.
- The initial delivery deliberately leaves views and non-MySQL engines
  unsupported.

## Rejected Alternatives

### Put Raw DDL In Object Detail

Rejected because object detail is normalized and cached. Raw engine text has a
different cap, disclosure, freshness, and audit policy.

### Let The Frontend Run `SHOW CREATE TABLE`

Rejected because it would require browser database access or broaden the
read-only query surface, bypassing the existing target resolver and audit
boundary.

### Expose A Generic Metadata SQL Endpoint

Rejected because a typed table-definition endpoint has a small, testable
authorization and identifier boundary. A generic endpoint would become an
unbounded escape hatch.

### Support Views In The Initial Slice

Rejected because `SHOW CREATE VIEW` has a `DEFINER` disclosure surface. A view
policy must be decided and tested independently.

### Cache Definitions Alongside Schema Metadata

Rejected because the existing cache retains values for minutes. Definition text
can carry comments/default literals and must remain request-ephemeral in this
initial capability.

## Verification Requirements

- Unit tests cover input validation, target access mapping, no-cache behavior,
  fixed audit identity, and controlled errors.
- Inspector tests cover base-table verification, view refusal, exact quoted
  identifiers, UTF-8-safe truncation, and no raw driver error propagation.
- Handler/OpenAPI tests cover fresh actor use, request parsing, all public
  status/code mappings, and response schema.
- A real MySQL integration test uses the schema fixture to prove an authorized
  read-only target returns a table definition, rejects a view, and leaves no
  definition content in `query_executions` or audit-event payload columns.
- The backend candidate runs the full existing quality gate suite on its final
  commit; focused tests alone are not completion evidence.
