# Non-production MongoDB worksheet

Select an approved MongoDB target in `/query`. The existing Objects pane lists the approved collections, with approved fields nested under each collection. Type `db.orders.find({"status": "open"})` in the worksheet and click **Run**. An optional second argument is an inclusion projection, `db.orders.find({"status": "open"}, {"orderNo": 1, "status": 1})`. Each successful run returns bounded rows of the approved fields. The shared history records the outcome as `MongoDB find` and omits the collection, filter, projection and values. The editor uses the ordinary private workspace-draft behavior.

## Enable a target

1. Register a MongoDB database-instance profile in `Staging`, `Development` or `Test`. Use a TLS-enabled mongod reachable at the profile's exact host and port. The verified engine baseline is MongoDB `8.0.38`.
2. On the MongoDB deployment, provision a dedicated read account whose only privilege is `find` on the approved collections, one custom role per deployment such as `{resource: {db: "demo", collection: "orders"}, actions: ["find"]}`. Give the application account only these privileges. Reserve role and user administration for a separate operator identity. Use a certificate valid for the target profile's exact host.
3. Set `CONTROLHUB_ENVIRONMENT=nonproduction` and `CONTROLHUB_MONGO_WORKSHEET_CONFIG` to the absolute path of an operator-owned JSON approval file. Use a private parent directory and an ordinary file. The directory and file must be non-writable by group or other users. Keep the only write authority with the operator. Set `CONTROLHUB_QUERY_CREDENTIAL_<REF>` to `mongodb://<reader>:<url-encoded-password>@<host>:<port>/<database>` — the host and port must match the profile and approval, and the path database must match the approval `database`. One optional `authSource=<db>` query parameter is allowed; every other URI feature fails closed. Use the admin `PUT /query-targets/{id}/credential` route with the same reference, `enabled=true`, and `environmentPolicy=non_prod_only`.
4. Publish the approval file by atomic rename. Use the real target and user IDs and trusted PEM certificate authority in place of the examples below.

```json
{
  "targetId": 42,
  "environment": "Staging",
  "address": "mongo.example.test:27017",
  "user": "worksheet_reader",
  "credentialRef": "MONGO_WORKSHEET_RO",
  "caPem": "<trusted CA PEM>",
  "users": [17],
  "database": "demo",
  "collections": {
    "orders": {
      "orderNo": "raw_copy_allowed",
      "status": "raw_copy_allowed",
      "region": "raw_copy_allowed",
      "customer": "masked_no_copy",
      "note": "raw_copy_allowed"
    }
  },
  "maxTimeMS": 500,
  "limits": {
    "maxConcurrentRuns": 4,
    "maxConcurrentRunsPerUser": 1,
    "runsPerSecond": 10,
    "runsPerMinutePerUser": 30
  }
}
```

`database` and collection names match `^[A-Za-z0-9_-]{1,64}$`; field names are `_id` or `^[A-Za-z][A-Za-z0-9_]{0,63}$` and may not start with `__`. A collection approves 1–32 fields, each with a disclosure mode. `maxTimeMS` is optional, 50–2000, default 500. Any invalid field invalidates the whole approval and closes the target. The configuration authorizes one target. Removing the file, disabling the credential, or removing an approved user, collection or field blocks the next Run against that approval. The configured CA validates TLS independently on every connection. An unset configuration leaves MongoDB Run closed.

## Statement grammar

A Run accepts exactly `db.<collection>.find(<filter>[, <projection>])` in at most 4,096 bytes — no chaining, no other methods, no aggregate. Arguments are strict JSON objects parsed with exact numbers and duplicate-key rejection.

The filter is an implicit AND of at most 16 approved fields, and every filter field must hold `raw_copy_allowed` — filtering on a masked field would let a user confirm guessed values by equality, so a masked field is refused as a filter key even though it stays projectable (its cells arrive `[MASKED]`). A field value is a scalar — string, number, boolean, null, or an Extended JSON wrapper `{"$oid": "<24 hex>"}`, `{"$date": "<RFC3339>"}`, `{"$numberLong": "<int64>"}` — for equality, or an operator object whose keys all belong to `$eq`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`. `$in` takes an array of 1–100 scalars. Everything else is `validation_failed`: `$where`, `$function`, `$expr`, `$regex`, `$or`, `$and`, `$nor`, `$not`, `$ne`, `$nin`, `$exists`, `$elemMatch`, dotted paths, unknown `$` keys, and nested documents as values.

The projection is inclusion-only: approved fields with value `1` or `true`, plus `"_id": 0` to drop `_id`. An omitted projection returns all approved fields — `_id` first when approved, then the sorted field names. `_id` is returned only when approved. An unapproved collection, or an unapproved field in the filter or projection, returns `403 query_result_disclosure_blocked`. A request `database` must be empty or equal to the approval database; schema, pagination and ClickHouse fields are `validation_failed`. `maxRows` resolves to 50 when omitted and is capped at 100; the backend fetches one extra row to report truncation.

## Index gate

After the statement and disclosure checks pass, the backend runs `explain` on the same connection at `queryPlanner` verbosity and walks `queryPlanner.winningPlan`. Only trees made of `FETCH`, `LIMIT`, `PROJECTION_*` or `SHARDING_FILTER` wrappers around exactly one index access stage — `IXSCAN`, `IDHACK`, `RECORDID_FAST_PATH`, `EXPRESS_IXSCAN`, `EXPRESS_CLUSTERED_IXSCAN` — are admitted, and the index stage must carry an `indexName`. Anything containing `COLLSCAN`, `SUBPLAN`, `OR`, `AND_*`, `SORT`, `TEXT`, `GEO*` or any unmodeled stage returns `400 query_index_required` with a rejected evidence pair. The find then runs pinned to the explained index by `hint`, so a plan change between explain and find cannot silently reroute the read. Real winningPlan shapes captured from MongoDB 8.0.38 and 9.0.8 — including the `EXPRESS_IXSCAN` stage the server emits for `_id` point lookups and the `SUBPLAN` wrapper it emits for `$or` — are fixtures in `internal/service/mongo_worksheet_test.go`.

## Execution and evidence

Each Run opens one fresh TLS connection with SCRAM-SHA-256, `directConnection`, secondary-preferred read preference, pool size 1, retry reads and writes off, and appName `controlhub-worksheet`, and disconnects when the attempt ends. The find runs `singleBatch: true` with `limit` = rows + 1, `maxTimeMS` from the approval, the explained index as `hint`, and `comment: "controlhub-worksheet"`. A reply whose cursor id is not `0` is refused as a backend failure, so no server cursor can outlive the attempt.

Every approved field travels under a server-side projection that caps each value at 1,024 bytes: `field: {$cond: [{$lte: [{$bsonSize: {v: "$field"}}, 1024]}, "$field", "$$REMOVE"]}` plus a `__controlhub_over_<field>` flag computed on the server. An over-limit field arrives only as its flag — the payload never crosses the wire — and its cell is delivered empty with `cellTruncated` true. A missing field is an empty cell. Strings pass through; invalid UTF-8 is truncated rather than delivered. ObjectIDs render as hex, datetimes as RFC3339 UTC, int64 outside ±2^53 and Decimal128 as decimal strings, and documents, arrays and other BSON types as relaxed Extended JSON strings. All columns then pass through the ordinary disclosure service, so a `masked_no_copy` field arrives `[MASKED]` and cannot be copied.

A server `MaxTimeMSExpired` (code 50) maps to `408 query_timeout` with remote state completed; server `Unauthorized` (13) maps to `403 query_not_allowed`; other server or transport failures map to the controlled `502 query_backend_error`. Successful, rejected, failed, canceled and timed-out attempts on a resolved target record a history/audit pair before delivery. The backend persists the fixed preview `MongoDB find` and a controlled outcome, with no collection, filter, projection or value in the evidence. A persistence failure returns `502 query_backend_error` with no rows.

### Security boundary

The `find` privilege is broader than the worksheet grammar: on the server it also permits read-only `aggregate`, `listDatabases`, and server-side JavaScript through `$where` and `$function` under default settings. ControlHub's grammar — not the MongoDB role — is the guard that keeps a Run to a single index-backed find. Operators should harden the server side independently: set `security.javascriptEnabled: false`, set the `defaultMaxTimeMS` cluster parameter (supported on 8.0 and 9.0) as a global read cap, prefer a secondary for worksheet reads, and restrict network access so only ControlHub reaches the deployment. `notablescan` rejects every COLLSCAN instance-wide — it is an operator decision rather than a worksheet setting. These behaviors are measured against a real server in `internal/integration/mongo_find_bound_probe_test.go`.

## Load protection

Every admitted Run opens a fresh TLS connection and authentication exchange, so Runs are bounded per ControlHub process by the same optional `limits` object as the Redis worksheet. Omitted fields take the defaults; a present field outside its range invalidates the whole approval and closes the target.

| Field | Bound | Default | Range |
| --- | --- | --- | --- |
| `maxConcurrentRuns` | in-flight Runs on the target | 4 | 1–16 |
| `maxConcurrentRunsPerUser` | in-flight Runs per (target, user) | 1 | 1–4 |
| `runsPerSecond` | token-bucket Runs per second on the target | 10 | 1–50 |
| `runsPerMinutePerUser` | token-bucket Runs per minute per (target, user) | 30 | 1–600 |

An admitted Run holds both concurrency slots and one token from each bucket for the whole attempt, including connect, explain and find. A refusal consumes nothing: requests refused earlier by validation, disclosure or approval checks are also free. A refusal returns `429 query_rate_limited` with `Retry-After: 1`, records the usual `rejected` history/audit pair, and never contacts MongoDB. Shrinking a limit on republish clamps stored tokens to the new capacity.

The limiter is per ControlHub process. With N serving instances the effective ceiling is N × the configured limit — size limits for the real deployment accordingly. MongoDB-side backstops remain the operator's own decision: the `defaultMaxTimeMS` cluster parameter, `security.javascriptEnabled: false`, a network allowlist so only ControlHub reaches the deployment, and preferring a secondary for worksheet reads.

## Verify

```bash
go test -tags=integration -count=1 -v -run '^TestMongoWorksheetHTTP$' ./internal/integration
go test -tags=integration -count=1 -v -run '^TestMongoFindBoundProbe$' ./internal/integration
```

`TestMongoWorksheetHTTP` starts a disposable TLS MongoDB replica set and MySQL metadata database, provisions the MongoDB target and users through the standard assembly, and exercises the ordinary API: approved versus unapproved discovery, index-backed finds with projections and `$oid`/`$in` filters, the index gate on COLLSCAN plans, the 1 KiB per-field cap, masked disclosure, approval and role revocations, the atomic evidence pair, the Run limiter, and literal-free evidence. `TestMongoFindBoundProbe` is the real-server feasibility evidence for the bounds this worksheet relies on — find-only privileges, explain plan shapes, maxTimeMS enforcement, disconnect handling, `$bsonSize` server-side byte capping, and comment traceability on MongoDB 8.0.38 and 9.0.8.
