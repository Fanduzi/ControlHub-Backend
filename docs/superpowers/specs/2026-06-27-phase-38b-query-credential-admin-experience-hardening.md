# Phase 38B Query Credential Admin Experience Hardening Design

## Background

Phase 38A completed the product-safe credential metadata path:

- backend metadata API for `GET/PUT/DELETE /query-targets/{id}/credential`;
- admin-only writes and fresh-token protection;
- metadata-only persistence with no DSN/password storage;
- frontend settings page for target-level credential metadata management;
- Query Workbench status display with no edit controls;
- real backend E2E against the Phase 37H dedicated query MySQL fixture.

The remaining problem is not core capability. It is operability.

The Phase 38A UI lets an admin configure one target at a time. That is enough to
prove the contract, but it is not how DBAs operate a query platform. In most
teams, DBAs provision standardized read-only accounts for query platforms and
then apply the corresponding opaque credential references across groups of
targets. Some clusters still need overrides. Admins need to see coverage,
failure modes, and exceptions at a glance.

Phase 38B hardens the management experience without changing the secret boundary.

## Goal

Make `/settings/query-credentials` usable as an admin operations surface for
credential metadata coverage.

An admin should be able to:

- understand credential coverage across all query-capable targets;
- group targets by environment, cluster, engine, and runtime status;
- quickly find `missing_metadata`, `secret_missing`, `binding_mismatch`,
  `policy_blocked`, `disabled`, and `ready` targets;
- select multiple compatible targets and apply a standardized credential
  reference;
- remove metadata from selected targets when needed;
- see per-target success/failure after a bulk operation;
- keep using the single-target edit panel for cluster-specific overrides;
- confirm explicitly before applying `all_environments`;
- never handle DSN/password values in the browser.

A non-admin should:

- never see the management surface;
- never see credential edit controls;
- continue seeing read-only Query Workbench status only.

## Non-Goals

- No DSN/password input.
- No secret manager UI.
- No secret write API.
- No new backend credential table or migration.
- No Redis, MongoDB, PostgreSQL, or ClickHouse execution.
- No approval workflow.
- No export flow.
- No saved queries.
- No production default enablement.
- No Workbench credential edit controls.
- No new CI workflow.
- No tag, release, or deployment.

## Product Model

Phase 38B treats query credential metadata as an admin-operated binding:

```text
target resource -> opaque credential ref + enabled flag + environment policy
```

The binding does not imply that a target is executable. The backend runtime
status remains authoritative:

```text
missing_metadata
invalid_ref
disabled
policy_blocked
secret_missing
binding_mismatch
secret_resolved
unsupported_target
incomplete_connection
```

The admin UI must distinguish three concepts:

- **Metadata configured:** a credential metadata row exists.
- **Secret resolvable:** backend can resolve `CONTROLHUB_QUERY_CREDENTIAL_<ref>`.
- **Execution eligible:** backend reports `secret_resolved`, policy permits the
  environment, and DSN host/port binds to the target.

Only the last state should be displayed as ready.

## UI Information Architecture

Phase 38B keeps credential configuration under settings/admin:

```text
/settings/query-credentials
```

Query Workbench remains a consumer:

- status labels;
- admin settings link for admin users;
- contact-admin copy for non-admin users;
- no credential input;
- no save/delete controls.

The settings page should evolve into three zones.

### 1. Coverage Overview

Summary cards at the top:

- total query-capable targets;
- ready targets;
- missing metadata;
- secret missing;
- binding mismatch;
- policy blocked;
- disabled;
- unsupported/incomplete targets.

The numbers are derived from existing `GET /query-targets` data and per-target
credential status responses.

Phase 38B may fetch credential status with a bounded client-side fan-out over
the visible query targets:

```text
GET /query-targets/{id}/credential
```

This is acceptable for the current target scale and keeps the phase frontend
first. The UI must show loading/partial-error states for credential status
fetches instead of blocking the whole page indefinitely. If the fan-out is too
slow or too noisy in review, that is evidence for a later backend aggregate API;
it is not a reason to introduce a speculative backend endpoint in Phase 38B.

### 2. Operations Table

A table or structured list with:

- target name;
- engine;
- environment;
- cluster;
- host/port;
- credential state;
- runtime status;
- credential ref, when metadata is configured and backend returns it;
- environment policy;
- enabled flag;
- last operation result in the current page session.

`GET /query-targets` remains the source for target inventory and governance
state. `GET /query-targets/{id}/credential` is the source for configured
metadata fields such as credential ref, enabled flag, environment policy, and
runtime status. Do not infer these configured metadata fields from display-only
governance strings.

Required filters:

- search;
- environment;
- cluster;
- engine;
- runtime status;
- credential state;
- configured/unconfigured;
- ready/not ready.

Required grouping:

- environment-first view;
- cluster-first view.

Grouping can be client-side in Phase 38B. A backend aggregate API is deferred
unless the frontend implementation exposes a clear performance problem.

### 3. Bulk Operation Panel

Bulk operations are admin-only and metadata-only:

- apply credential ref to selected targets;
- set enabled;
- set environment policy;
- remove metadata from selected targets.

The initial implementation may use existing single-target Phase 38A APIs as a
fan-out:

```text
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
```

This means bulk operations are not transactional across targets. The UI must
make that explicit and show per-target results.

A future phase can add a backend batch API if transactional semantics or server
side aggregation become necessary.

## Selection Rules

Bulk apply must be conservative:

- only query-capable MySQL/TiDB targets are selectable;
- incomplete connection targets are visible but disabled for bulk apply;
- unsupported engines are visible but disabled for bulk apply;
- production targets require explicit `all_environments` confirmation before
  applying that policy;
- selected targets can span clusters, but the UI should warn when selected
  host/ports differ across environments or clusters.

The backend remains the final authority. The UI pre-checks are usability guardrails
only.

## Security Boundary

Phase 38B must preserve the Phase 38A boundary:

- the browser never accepts DSN/password input;
- request bodies never contain DSN, password, host, port, engine, or actor id;
- actor id comes only from the bearer token;
- write/delete actions remain admin-only;
- non-admin users see a restricted view, not a disabled copy of the form;
- Query Workbench has no edit controls;
- all operations use existing authenticated API client behavior.

## Failure Handling

Bulk operations must be explicit about partial success:

```text
Target A -> saved, secret_missing
Target B -> failed, 403 forbidden
Target C -> failed, binding_mismatch
Target D -> removed
```

The UI must not collapse these into a single success banner. It should show:

- operation started;
- per-target pending/success/failure;
- final count summary;
- retry affordance for failed rows only.

If any operation fails with 401, the existing auth client redirect behavior
applies.

## API Strategy

Phase 38B is frontend-first.

Use existing APIs:

```text
GET    /query-targets
GET    /query-targets/{id}/credential
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
```

Do not add backend APIs in the first implementation unless review proves the
fan-out approach is not acceptable.

Deferred backend candidates:

- `GET /query-credentials/coverage` for server-side aggregate counts;
- `POST /query-credentials/bulk-apply` for transactional or server-audited bulk
  operations;
- `POST /query-credentials/bulk-delete`;
- audit read model dedicated to credential metadata changes.

These are not Phase 38B requirements.

## Test Strategy

Frontend unit/component tests must cover:

- admin-only gate stays hydration-safe;
- non-admin never sees form controls;
- summary counts derive from target/status fixtures;
- filters and grouping derive expected rows;
- bulk apply sends only whitelisted fields;
- polluted inputs do not leak forbidden fields;
- `all_environments` requires confirmation;
- bulk operation partial success is displayed per target;
- Query Workbench remains read-only.

E2E should run against a real backend and the Phase 37H dedicated query MySQL
fixture when available:

- admin opens query credential operations page;
- coverage summary renders;
- admin filters by missing metadata and ready;
- admin applies a metadata ref to one or more safe targets;
- ready target remains executable in Query Workbench;
- non-admin cannot access management controls;
- Workbench has no edit controls.

If backend or fixture is unavailable, the E2E gap must be reported. Do not claim
real E2E success from mocks.

## Acceptance Criteria

Phase 38B is complete when:

- `/settings/query-credentials` clearly behaves like an admin operations page;
- coverage summary and grouping are present;
- bulk metadata apply/remove works using existing APIs;
- bulk results show per-target outcomes;
- no DSN/password path is introduced;
- non-admin and Workbench boundaries are preserved;
- frontend tests and build pass;
- real backend E2E passes when the backend and fixture are available;
- evidence is recorded in backend docs.
