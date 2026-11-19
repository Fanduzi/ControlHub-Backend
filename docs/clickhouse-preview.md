# Non-production ClickHouse table preview

The workbench offers an approved user a table picker, column selection and a 1–20 row preview. Free SQL execution remains separately governed. The HTTP surface is `GET` (catalog) and `POST` (preview) on `/query-targets/{id}/clickhouse-preview`. Both require a fresh user session. The catalog carries an opaque generation that the POST must echo. A changed approval or generation CA invalidates the selection. The target list advertises `availableActions.clickHousePreview` only for an approved user and target. The result contains only disclosed values and is released after the MySQL history/audit transaction commits.

## Supported installation

Use a single, local, controller-owned non-production instance with a private state directory on a local filesystem supporting Unix `flock`. The controller owns the ClickHouse instance, certificates, all DDL and its credential configuration. The serving phase has read-only SQL users, a local TLS listener, no external maintenance access, no configuration hot reload by other operators, and one fixed database. Shared servers, other DDL writers, multiple hosts, remote endpoints and production targets fall outside this implementation's isolation guarantee.

1. Set `CONTROLHUB_ENVIRONMENT=nonproduction` and `CONTROLHUB_CH_PREVIEW_DIR` to the absolute private state directory at backend startup. An absent directory setting keeps the feature closed. `lease` is a permanent regular file. Previews hold a shared lock through result preparation and committed audit. Maintenance must acquire an exclusive lock on **the same inode**, remove `active.json`, drain/replace the old instance, then install its new approval and CA before releasing the lock. Never replace the lease inode. A failed transition leaves `active.json` absent. Use a fresh CA per generation. The backend needs read access only, and must not have access to the controller's private signing key.
2. Register a ClickHouse database instance profile in `Staging`, `Development` or `Test`, with the exact loopback IP and TLS port. Use the existing admin credential endpoint for this target, with an enabled credential and `environmentPolicy=non_prod_only`. Resolve its reference using `CONTROLHUB_QUERY_CREDENTIAL_<REF>` with the form `clickhouses://<reader>:<url-encoded-password>@<loopback>:<tls-port>/<database>` and no query parameters. Only the backend receives this value. Install the generation's trusted PEM in `ca.pem`; TLS verifies the server's IP SAN.
3. Grant only SELECT on the approved ordinary stored columns. Configure immutable `readonly=1`, `allow_ddl=0`, execution deadline at most 5 s, read cap at most 100,000 rows, result cap at most 1 MiB, memory cap at most 128 MiB, result rows capped at 4,096, and `throw` overflow modes for read, result and timeout. Keep the host/instance under the exclusive maintenance controller. Set per-column disclosure policies via the existing admin endpoints, scoped to resource, database, empty schema, table and column.
4. Under the exclusive lease, publish `active.json` atomically. The following illustrative IDs and table UUID must be replaced with the actual approved identities.

```json
{
  "TargetID": 42, "Environment": "Staging", "Address": "127.0.0.1:9440",
  "Database": "analytics", "User": "preview_reader", "CredentialRef": "CH_PREVIEW_RO",
  "Users": [17], "Tables": {
    "events": {"UUID": "c9990000-0000-4000-8000-000000000001", "Columns": ["id", "label"]}
  }
}
```

Only local `MergeTree` base tables and plain `UInt32`, `String`, or `Nullable(String)` columns are admitted. Identifiers use ASCII letters, digits and underscores and start with a letter or underscore. The preview admits up to eight columns, validates UTF-8, rejects cells over 1 KiB and JSON replies over 64 KiB, and discards partial rows. There is no sorting guarantee. A terminal server response and a client disconnect carry different remote-state evidence. When the target's end state cannot be confirmed, history records `unknown`. Removing `active.json` under the exclusive lock closes admission. Leaving `CONTROLHUB_CH_PREVIEW_DIR` unset at startup closes the whole facility.

## Verification

`go test -tags=integration -count=1 -v -run '^TestClickHousePreviewHTTP$' ./internal/integration` starts disposable MySQL and ClickHouse. Requires Docker, Python 3, and OpenSSL. It covers default-off behavior, binding, fresh users, maintenance exclusion, policy lookup, the Native driver over verified TLS, cancellation, budgets, masking, stale approvals and atomic audit. The fixture forcibly releases only its own containers and volume.

For the real Chromium path set `CONTROLHUB_CH_BROWSER_CHECKOUT` to the companion frontend checkout (with the prescribed Node version, `npm ci` and Playwright Chromium installed) and run the same command. The test starts the actual console/BFF on loopback, logs in through the UI and previews real rows. PostgreSQL rollout and persistent infrastructure are independent of these tests.
