# Non-production ClickHouse table preview

The workbench offers an approved user a table picker, column selection and a 1–20 row preview. Free SQL execution remains separately governed. The HTTP surface is `GET` (catalog) and `POST` (preview) on `/query-targets/{id}/clickhouse-preview`. Both require a fresh user session. The catalog carries an opaque generation that the POST must echo. A changed approval or generation CA invalidates the selection. The target list advertises `availableActions.clickHousePreview` only for an approved user and target. The result contains only disclosed values and is released after the MySQL history/audit transaction commits.

This guide covers the optional picker and fixed count. Developer-authored ClickHouse SQL in the shared worksheet is the next engine-integration milestone; its [scope and acceptance](superpowers/specs/2026-06-20-query-workbench-roadmap.md#engine-integration-acceptance) are tracked separately.

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

Only local `MergeTree` base tables and ordinary `UInt32`, `UInt64`, `Date`, `DateTime` (including a declared timezone), `String`, or `Nullable(String)` columns are admitted. `UInt64` values, including `count()`, travel as decimal strings; `Date` travels as `YYYY-MM-DD` and `DateTime` as an RFC 3339 UTC instant at second precision. Identifiers use ASCII letters, digits and underscores and start with a letter or underscore. The preview admits up to eight columns, validates UTF-8, rejects cells over 1 KiB and JSON replies over 64 KiB, and discards partial rows. There is no sorting guarantee. A terminal server response and a client disconnect carry different remote-state evidence. When the target's end state cannot be confirmed, history records `unknown`. Removing `active.json` under the exclusive lock closes admission. Leaving `CONTROLHUB_CH_PREVIEW_DIR` unset at startup closes the whole facility.

## Last seven days by UTC day

To allow counting all matching rows in an approved table, add the time column to both `Columns` and `DailyCountColumns` in that table's controller approval, under the exclusive maintenance lease. The time column must be `Date` or `DateTime` and have current `raw_copy_allowed` disclosure. Existing approvals opt into no aggregates. Catalog responses advertise eligible `dailyCountColumns` per table; the server rechecks them on every request, including after policy revocation.

POST `{"generation":"<catalog generation>","table":"events","dailyCountColumn":"observed_at"}` to the preview endpoint. This request accepts no preview column list, limit, predicate, SQL or client clock. The server filters and groups by the same approved column and returns seven ascending `day`/`count` rows, including zeros for empty days. For a timestamp, the interval starts at 00:00 UTC six calendar days ago and ends at the request's UTC second (inclusive); a date column includes today's date. One matching table row counts once. The normal read budgets, terminal-result checks and atomic evidence commit still apply. No pagination or export is introduced.

## Verification

`go test -tags=integration -count=1 -v -run '^TestClickHousePreviewHTTP$' ./internal/integration` starts disposable MySQL and ClickHouse. Requires Docker, Python 3, and OpenSSL. It covers default-off behavior, binding, fresh users, maintenance exclusion, policy lookup, the Native driver over verified TLS, cancellation, budgets, masking, stale approvals and atomic audit. The fixture forcibly releases only its own containers and volume.

For the real Chromium path set `CONTROLHUB_CH_BROWSER_CHECKOUT` to the companion frontend checkout (with the prescribed Node version, `npm ci` and Playwright Chromium installed) and run the same command. The test starts the actual console/BFF on loopback, logs in through the UI and previews real rows. PostgreSQL rollout and persistent infrastructure are independent of these tests.
