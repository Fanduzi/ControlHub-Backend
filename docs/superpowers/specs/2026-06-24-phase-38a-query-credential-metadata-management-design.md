# Phase 38A Query Credential Metadata Management Design

## Background

Phase 36 made query targets visible. Phase 37 added the MySQL/TiDB read-only
execution sandbox. Phase 37F wired the frontend execution UI. Phase 37G/37H
proved local ready-target execution with a dev-only seed path and a dedicated
Docker MySQL fixture.

The remaining product gap is credential configuration. A real user can create a
database instance resource and profile, but there is no product API or UI to
bind that target to a read-only query credential reference. Today readiness is
reachable only through dev tooling or migrations.

Phase 38A turns this into a product surface without storing secrets.

## Goal

Add authenticated query credential metadata management for MySQL/TiDB query
targets, plus an admin-only frontend management surface under settings/admin.
The Query Workbench only consumes readiness/status and never exposes credential
configuration controls to normal query users.

An admin should be able to:

- open a query credential administration page,
- search/filter database query targets,
- see whether credential metadata is missing, disabled, policy-blocked, secret
  missing, binding-mismatched, or ready,
- configure an opaque read-only credential reference,
- choose enabled/disabled and environment policy,
- save or remove the metadata,
- see the target readiness update after save.

A non-admin query user should only see query readiness in the Query Workbench:

- target locked/unlocked state,
- human-readable reason,
- no credential editing form,
- no credential reference unless the backend explicitly allows read exposure.

## Non-Goals

- No plaintext DSN or password storage in ControlHub tables.
- No browser credential input field.
- No secret write API.
- No secret manager integration.
- No export, saved queries, approvals, masking, or query templates.
- No new query engines.
- No schema introspection.
- No production query enablement without explicit confirmation.
- No changes to the SQL guard or execution semantics.
- No GitHub Actions workflow changes.
- No tag, release, or deployment.

## Product Boundary

Phase 38A manages credential metadata only:

```text
resource_id
engine
credential_ref
enabled
environment_policy
```

The actual credential remains outside ControlHub and is resolved by the backend
from:

```text
CONTROLHUB_QUERY_CREDENTIAL_<credential_ref>
```

The UI can tell the user which reference is bound and whether the backend can
resolve and bind it. It must never collect or display the DSN value.

Most companies let DBAs provision standardized read-only accounts for query
platforms. Phase 38A models that operational reality as metadata binding:

- Standard account path: DBA/platform pre-provisions server-side credential
  secrets using a naming convention; admins bind many query targets to the
  corresponding opaque refs.
- Cluster-specific path: an admin binds a specific cluster/instance target to a
  different opaque ref when the standard account cannot be used.

ControlHub still stores only the ref. The actual DSN/password remains in server
environment or a future secret manager. This is intentionally not full
credential management. It is the smallest safe product step between dev-only
seeding and future secret-store integration.

## Auth And Authorization

Credential metadata routes are write-capable and security-sensitive. They must
require a fresh bearer token.

Additional authorization rule:

- only `admin` role may create, update, or delete credential metadata;
- non-admin authenticated users receive `403 forbidden`.

Read access to credential metadata also requires a fresh bearer token because
`credential_ref` names can reveal operational naming conventions.

The actor user id comes from the verified token. The request body never accepts
`actorUserId`.

## Backend API

Add three endpoints:

```text
GET    /query-targets/{id}/credential
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
```

All three routes require bearer auth with the same token freshness rule used by
query execution. Write/delete additionally require `role == "admin"`.

### GET Response

`GET /query-targets/{id}/credential` returns a stable object even when no
metadata row exists:

```json
{
  "resourceId": 4000000000000000002,
  "configured": false,
  "engine": "mysql",
  "credentialRef": "",
  "enabled": false,
  "environmentPolicy": "disabled",
  "runtimeStatus": "missing_metadata",
  "executionEligible": false,
  "message": "No read-only credential reference is configured."
}
```

When configured:

```json
{
  "resourceId": 4000000000000000002,
  "configured": true,
  "engine": "mysql",
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "non_prod_only",
  "runtimeStatus": "secret_resolved",
  "executionEligible": true,
  "message": "Read-only credential is configured and bound to this target."
}
```

### PUT Request

```json
{
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "non_prod_only"
}
```

For `all_environments`, the request must include explicit confirmation:

```json
{
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "all_environments",
  "confirmAllEnvironments": true
}
```

Rules:

- `credentialRef` must pass `model.ValidateCredentialRef`.
- `environmentPolicy` must pass `QueryEnvironmentPolicy.Validate`.
- `all_environments` requires `confirmAllEnvironments == true`.
- target must exist and be a complete MySQL/TiDB query target.
- engine in metadata is derived from the target, not accepted from the request.
- request body must not contain DSN, password, host, port, or actor fields.
- save succeeds even when the backend cannot resolve the secret yet; the
  returned `runtimeStatus` explains the missing secret and the target remains
  locked.

### DELETE Response

`DELETE /query-targets/{id}/credential` removes metadata and returns `204`.

After delete, the target remains visible and locked as `credential_required`.

## Runtime Status

Add a backend runtime status enum:

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

Meanings:

- `missing_metadata`: no row in `query_target_credentials`.
- `invalid_ref`: row exists but `credential_ref` is invalid; fail closed.
- `disabled`: metadata exists but `enabled=false`.
- `policy_blocked`: metadata exists but environment policy disallows execution.
- `secret_missing`: metadata exists but resolver cannot resolve the DSN.
- `binding_mismatch`: resolver returns a DSN, but host/port does not bind to
  the selected target.
- `secret_resolved`: resolver returns a DSN, policy allows it, and host/port
  binds to the target.
- `unsupported_target`: target is not MySQL/TiDB.
- `incomplete_connection`: target connection metadata is incomplete.

Only `secret_resolved` can produce `executionEligible=true`.

## Query Target Readiness Correction

Phase 38A must correct readiness derivation so `GET /query-targets` does not
mark a target ready solely because metadata exists.

Ready requires:

- complete MySQL/TiDB target,
- enabled metadata row,
- valid `credential_ref`,
- policy allows the target environment,
- backend resolver can resolve the DSN,
- resolved DSN binds to the target host and port.

If metadata exists but the secret is missing or binding mismatched, the target
must stay locked with `readiness=credential_required` or `readiness=disabled`
as appropriate. The governance panel should expose a human-readable credential
state such as "Secret missing on server" or "Credential does not match target".

Any new `credentialState` values emitted by `GET /query-targets` are part of
the frontend contract. Phase 38A must update the backend schema description,
OpenAPI examples, frontend known-state list, English and Chinese labels, and
component tests for these values. Raw enum strings such as `secret_missing` or
`binding_mismatch` must not leak into the UI.

## Audit

Every successful metadata write/delete records an audit event:

```text
event_type = query.credential.updated
event_type = query.credential.deleted
target_resource_id = query target resource id
actor_user_id = authenticated actor id
result = success
```

Rejected validation/auth attempts do not need audit rows in Phase 38A, but they
must return controlled 4xx responses and never 500.

Audit rows must not include DSN or password. The existing `audit_events` table
is enough.

## Frontend UX

Add an admin-only credential metadata management surface under settings/admin,
not inside the Query Workbench.

Recommended route for Phase 38A:

```text
/settings/query-credentials
```

If the frontend already has a stronger admin route convention, use that
convention instead, but keep the surface outside `/query`.

The admin page should show:

- searchable target list,
- engine/environment/cluster/host:port,
- credential state label,
- credential reference if configured,
- environment policy,
- enabled/disabled state,
- runtime status,
- admin-only actions:
  - "Configure credential metadata" when missing,
  - "Edit credential metadata" when configured,
  - "Remove credential metadata" when configured.

The admin form fields:

- credential reference,
- enabled checkbox,
- environment policy select,
- all-environments confirmation checkbox shown only when policy is
  `all_environments`.

The form must not include:

- DSN,
- username,
- password,
- host,
- port,
- actor user id.

The page should also make the two DBA patterns clear:

- standardized read-only account: reuse a documented ref/naming convention
  across targets when server secrets are already provisioned;
- cluster override: bind a single cluster/instance to a different ref.

After save/delete:

- refresh credential status,
- refresh query targets,
- keep Run enabled only when backend `availableActions.run` is true.

### Query Workbench Consumer UI

For a selected target, the Query Workbench governance panel should show only:

- credential state label,
- credential reference only if the backend intentionally exposes it to the
  current actor,
- environment policy,
- enabled/disabled state,
- runtime status,
- action button:
  - admin users may see "Open credential settings",
  - non-admin users see "Contact administrator" or a disabled explanatory
    state.

The Query Workbench must not render a credential edit form, even for admin
users. Keeping configuration outside the workbench prevents the product from
implying that query users can configure database access.

## Error Handling

Backend controlled errors:

| HTTP | Code | Meaning |
| --- | --- | --- |
| 400 | `validation_failed` | invalid id, invalid ref, invalid policy, all-environments confirmation missing |
| 401 | `unauthorized` | missing/invalid/stale bearer token |
| 403 | `forbidden` | authenticated actor is not admin |
| 404 | `query_target_not_found` | target does not exist or is not a query target |
| 409 | `query_credential_conflict` | persistence conflict if encountered |
| 500 | `internal_error` | unexpected server failure only |

Frontend renders controlled messages and never displays stack traces or raw
response dumps.

## Security Invariants

- DSN/password never enters request JSON.
- DSN/password never enters response JSON.
- DSN/password never enters browser state.
- DSN/password never enters audit rows.
- `credential_ref` is validated before resolver lookup.
- ready state never depends on metadata alone.
- frontend disabled buttons are advisory; backend enforces every policy.
- production/all-environments enablement requires explicit confirmation.

## Verification Expectations

Backend:

- model tests for request/response validation and runtime status.
- repository tests for get/upsert/delete metadata.
- service tests for policy matrix, resolver status, binding mismatch, audit.
- handler tests for auth, admin-only writes, no actor body, error mapping.
- integration tests for end-to-end metadata config -> target readiness.
- OpenAPI validation.
- Schemathesis fuzz with declared 401/403/400 responses.

Frontend:

- service tests for request shapes with no DSN/password/actor fields.
- admin page/component tests for missing/configured/secret-missing/
  binding-mismatch/ready states.
- workbench component tests proving credential configuration controls are not
  rendered in `/query`.
- i18n tests for no raw enum leakage.
- page/workbench tests for save/delete refresh.
- E2E against local backend and Phase 37H dedicated query fixture.

## Deferred Work

- Real secret manager write/read integration.
- Role/permission model beyond current `admin` token role.
- Approval workflow for production query enablement.
- Credential rotation.
- Masking/export governance.
- Multi-engine credential UI for PostgreSQL, ClickHouse, Redis, and MongoDB.
