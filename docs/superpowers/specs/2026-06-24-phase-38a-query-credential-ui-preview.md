# Phase 38A Query Credential Admin UI Preview

## Purpose

This preview defines the Phase 38A frontend shape after correcting the product
boundary: credential metadata is an administrator function, not a Query
Workbench function.

The Query Workbench is for querying. Credential metadata management belongs in a
settings/admin surface where administrators and DBAs manage access policy.

## Placement

Use a settings/admin page, not `/query`:

```text
/settings/query-credentials
```

If the frontend has an established admin route convention, use it instead. The
important boundary is:

- credential edit/create/delete controls live outside `/query`;
- Query Workbench shows only readiness/status and does not imply ordinary query
  users can configure database access.

## DBA Operating Model

Phase 38A should reflect how this works in most companies:

- DBAs/platform teams provision standardized read-only accounts for the query
  platform.
- The backend exposes those credentials through server-side secret/env refs.
- ControlHub stores only metadata that binds a target to an opaque ref.
- Some clusters/instances can override the standard ref with a specialized
  cluster-specific ref.

The UI should therefore present two concepts:

- Standard binding: use the expected organization credential ref for this target
  family.
- Cluster override: bind this specific target to a different ref.

Neither path asks the browser user for a password or DSN.

## Admin Page Layout

Recommended page structure:

```text
Settings / Query Credentials

+-------------------------------+----------------------------------------+
| Query targets                 | Credential detail                      |
|                               |                                        |
| Search / filters              | Runtime status                         |
| engine / env / cluster        | Credential reference                   |
| state badges                  | Enabled / policy                       |
|                               | Standard ref guidance                  |
| target list                   | Cluster override controls              |
+-------------------------------+----------------------------------------+
```

The list should support:

- search by target name, host, environment, cluster;
- filters by engine, environment, credential state, policy;
- clear state badges:
  - missing metadata,
  - server secret missing,
  - binding mismatch,
  - disabled,
  - policy blocked,
  - ready.

## Admin Detail Panel

For a selected target:

```text
Credential binding

Target
local-mysql-query-dev · mysql · dev · 127.0.0.1:13306

Runtime
[Ready] server secret resolved and bound to target

Credential reference
[ LOCAL_QUERY_RO_______________________ ]

Mode
(*) Standard read-only account
( ) Cluster-specific override

Enabled
[x]

Environment policy
[ Non-production only v ]

Boundary
ControlHub stores only the reference. The DSN/password stays server-side as:
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO

[Save metadata] [Remove metadata]
```

The "mode" is UI copy/organization only in Phase 38A. It does not require a new
database column. The persisted metadata remains:

```text
resource_id
engine
credential_ref
enabled
environment_policy
```

## All-Environments Confirmation

If an admin selects `all_environments`:

```text
[ ] I understand this can enable read-only query execution for production
    targets when the backend credential resolves and binds to the selected
    target.
```

Save remains disabled until checked. Backend validation must still enforce the
same rule.

## Query Workbench Behavior

The Query Workbench governance panel should show only read-only facts:

```text
Credential state
Ready / Missing / Secret missing / Binding mismatch

Policy
Non-production only

Action
Admin: Open credential settings
Non-admin: Contact administrator
```

It must not render:

- credential reference input,
- enabled checkbox,
- environment policy select,
- save/remove buttons.

This prevents the product from implying every query user can configure database
credentials.

## Localized Copy Requirements

Add English and Chinese labels for runtime states:

- `missing_metadata`
- `invalid_ref`
- `disabled`
- `policy_blocked`
- `secret_missing`
- `binding_mismatch`
- `secret_resolved`
- `unsupported_target`
- `incomplete_connection`

Add Query Workbench copy for:

- "Open credential settings" for admins,
- "Contact administrator" for non-admins,
- "Credential configuration is managed by administrators."

Component tests must assert raw enum strings are not rendered.

## E2E Preview Scenario

The Phase 37H dedicated query MySQL fixture should drive real E2E:

1. Start backend and dedicated query fixture.
2. Open `/settings/query-credentials` as admin.
3. Select the dedicated local query target.
4. Configure `LOCAL_QUERY_RO`.
5. Confirm target becomes ready.
6. Open `/query`.
7. Verify Query Workbench shows ready status but no credential edit form.
8. Run a safe SELECT.
9. Verify unsafe query rejection and history.
10. Assert DSN/password never appears on either page.
