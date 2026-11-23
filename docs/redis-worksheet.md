# Non-production Redis worksheet

Select an approved Redis target in `/query`. The existing Objects pane lists its approved keys, with approved hash fields nested under hash keys. Type `GET demo:order:note:101` or `HGET demo:order:101 status` in the worksheet and click **Run**. Edit the key or field and run the command again to read a different value. Each successful run returns one `value` cell, with an explicit NULL marker for an absent key or field. An empty string stays empty. `TYPE` and `TTL` run the same way against separately approved metadata keys and return one `type` or `ttl_seconds` cell. The shared history records the outcome as `Redis GET`, `Redis HGET`, `Redis TYPE` or `Redis TTL` and omits the key, field and result. The editor uses the ordinary private workspace-draft behavior.

## Enable a target

1. Register a Redis database-instance profile in `Staging`, `Development` or `Test`. Use a standalone TLS Redis server with DB 0. The verified engine baseline is Redis Open Source `8.10.3`.
2. On the Redis server, provision a dedicated password-authenticated read account with `-@all +multi +exists +getrange +exec +watch +hstrlen +hget` and an explicit read-only key pattern such as `%R~demo:order:*` that covers the approved keys. Add `+type +ttl` only when the approval also grants `metadataKeys`. Give the application account only these privileges. Reserve ACL and database maintenance for a separate operator identity. Use a certificate valid for the target profile's exact host.
3. Set `CONTROLHUB_ENVIRONMENT=nonproduction` and `CONTROLHUB_REDIS_WORKSHEET_CONFIG` to the absolute path of an operator-owned JSON approval file. Use a private parent directory and an ordinary file. The directory and file must be non-writable by group or other users. Keep the only write authority with the operator. Set `CONTROLHUB_QUERY_CREDENTIAL_<REF>` to `rediss://<reader>:<url-encoded-password>@<host>:<tls-port>/0`. The host and port must match the profile and approval. Use the admin `PUT /query-targets/{id}/credential` route with the same reference, `enabled=true`, and `environmentPolicy=non_prod_only`.
4. Publish the approval file by atomic rename. Use the real target and user IDs and trusted PEM certificate authority in place of the examples below.

```json
{
  "targetId": 42,
  "environment": "Staging",
  "address": "redis.example.test:6379",
  "user": "worksheet_reader",
  "credentialRef": "REDIS_WORKSHEET_RO",
  "caPem": "<trusted CA PEM>",
  "users": [17],
  "keys": {
    "demo:order:note:101": "raw_copy_allowed",
    "demo:order:note:102": "masked_no_copy"
  },
  "hashFields": {
    "demo:order:101": {
      "status": "raw_copy_allowed",
      "region": "raw_copy_allowed",
      "masked": "masked_no_copy"
    }
  },
  "metadataKeys": [
    "demo:order:101",
    "demo:order:expiring"
  ],
  "limits": {
    "maxConcurrentRuns": 4,
    "maxConcurrentRunsPerUser": 1,
    "runsPerSecond": 10,
    "runsPerMinutePerUser": 30
  }
}
```

`keys` authorizes `GET` on a whole key; `hashFields` authorizes `HGET` on individual fields of a hash key. `metadataKeys` authorizes `TYPE` and `TTL` on exact literal keys — a separate permission, because both commands disclose key existence on their own. The three lists never authorize each other: approving `metadataKeys` for a masked or unlisted value key does not disclose its value, and `keys`/`hashFields` entries never imply metadata access. A `metadataKeys` entry can be the only approval for a key; metadata-only keys appear in Objects without children. At least one entry across the three lists is required. Field names and metadata keys use the same 1–128 character pattern as keys.

The configuration authorizes one target. Target discovery and Run read the current file and credential metadata independently. Removing the file, disabling the credential, or removing an approved user, key or field blocks the next Run against that approval. A request in progress uses the file snapshot acquired for that attempt. Redis itself applies its current ACL to the transaction. The configured CA validates TLS independently on every Redis connection, and the server never follows redirects. An unset configuration leaves Redis Run disabled.

## Execution and evidence

`GET`, `HGET`, `TYPE` and `TTL` are case-insensitive. Each command names one key (and for `HGET` one field) of 1–128 ASCII letters, digits, colons, underscores or hyphens.

For `TYPE` and `TTL` the backend issues one static O(1) command after `AUTH` — no `MULTI`, no `WATCH`, and no value payload is ever transferred. `TYPE` returns the built-in type name in a `type` column, or `none` for a missing key; replies naming module types or anything outside the trusted status words fail closed. `TTL` returns the canonical decimal text in a `ttl_seconds` column — nonnegative seconds for an expiring key, `-1` for no expiry and `-2` for a missing key. The value stays text end to end so precision-unsafe integers never pass through a JavaScript number. `PTTL`, `EXPIRE`, `SCAN`, batching and other command shapes are refused before dialing Redis.

For `GET` the backend issues a fixed transaction with `EXISTS` and `GETRANGE` using a 1,025-byte probe. This distinguishes a missing key from an empty value and rejects values above 1,024 bytes without fetching their full content.

For `HGET` the backend issues a fixed bounded-read protocol: `WATCH key`, then `HSTRLEN key field` as a size probe, then `MULTI`, `HGET`, `EXEC`. A field value above 1,024 bytes stops the protocol before `MULTI`, so the payload is never requested. Because `WATCH` makes `EXEC` abort whenever the key changes after `HSTRLEN` — another client growing the field, deleting, renaming or re-typing the key, `FLUSHDB`, or key/field expiry — a larger value can never be substituted between the probe and the read. An aborted `EXEC` is refused as `query_backend_error` with no rows and no retry. A `*-1` abort is the only accepted answer to a concurrent modification; this is verified against a real Redis server in `internal/integration/redis_hget_bound_probe_test.go`.

### Security boundary

Redis ACL cannot bound `HGET` reply size or approve individual hash fields. The reader account therefore holds `+hget` over its whole key pattern, and the 1 KiB value bound and per-field approval are enforced only by ControlHub's fixed protocol and approval file — not by Redis. Size, type and field checks happen before any oversized payload is requested or buffered on the server connection.

Complete UTF-8 strings within the bound pass through the existing disclosure service. Masked results cannot be copied. Non-string or invalid UTF-8 values, writes, batched commands and keys or fields without an applicable policy return a controlled refusal. The frontend uses the single-response result contract.

Each attempt uses a two-second query deadline. Successful, rejected, failed, canceled and timed-out attempts on a resolved target record a history/audit pair before delivery. The backend persists a fixed preview and controlled outcome, with no key, field, value, credential or full command in the evidence. A persistence failure returns `502 query_backend_error` with no rows. A disconnect before a complete Redis reply does not prove the server stopped the transaction.

## Load protection

Every admitted Redis Run opens a fresh TLS connection and `AUTH` exchange, so Runs are bounded per ControlHub process by an optional `limits` object in the approval file. Omitted fields take the defaults; a present field outside its range invalidates the whole approval and closes the target.

| Field | Bound | Default | Range |
| --- | --- | --- | --- |
| `maxConcurrentRuns` | in-flight Runs on the target | 4 | 1–16 |
| `maxConcurrentRunsPerUser` | in-flight Runs per (target, user) | 1 | 1–4 |
| `runsPerSecond` | token-bucket Runs per second on the target | 10 | 1–50 |
| `runsPerMinutePerUser` | token-bucket Runs per minute per (target, user) | 30 | 1–600 |

An admitted Run holds both concurrency slots and one token from each bucket for the whole Redis round trip. A refusal consumes nothing: requests refused earlier by validation, disclosure or approval checks are also free. A refusal returns `429 query_rate_limited` with `Retry-After: 1`, records the usual `rejected` history/audit pair, and never contacts Redis. Shrinking a limit on republish clamps stored tokens to the new capacity.

The limiter is per ControlHub process. With N serving instances the effective ceiling is N × the configured limit — size limits for the real Redis accordingly. Redis-side backstops remain the operator's own decision: a network allowlist so only ControlHub reaches Redis, `maxclients` on a dedicated instance, preferring a read replica, and `SLOWLOG`/latency monitoring. `client-output-buffer-limit normal` affects every normal client on the server, so it is an operator decision rather than a worksheet setting.

## Verify

```bash
go test -tags=integration -count=1 -v -run '^TestRedisWorksheetHTTP$' ./internal/integration
go test -tags=integration -count=1 -v -run '^TestRedisHGETBoundProbe$' ./internal/integration
go test -tags=integration -count=1 -v -run '^TestRedisWorksheetLimitsHTTP$' ./internal/integration
```

`TestRedisWorksheetHTTP` starts disposable Redis `8.10.3` over TLS and MySQL metadata, provisions the Redis target and users through the standard assembly, and exercises the ordinary API for `GET`, `HGET`, `TYPE` and `TTL`, including the separate `metadataKeys` approval, its revocation and audit rollback. `TestRedisWorksheetLimitsHTTP` proves the Run limits against the same fixture: floods refused by the per-user and per-target concurrency caps, a slot released after a timed-out Run, both token buckets, and `429 query_rate_limited` evidence. `TestRedisHGETBoundProbe` is the protocol evidence for the `HGET` bound, including concurrent-modification races. Set `CONTROLHUB_REDIS_BROWSER_CHECKOUT` to the companion frontend checkout (Node and Playwright Chromium installed) to also run the worksheet acceptance in real Chromium. It runs two different keys and checks permissions, audit rollback, stale-grid clearing and the execution history; the `type-ttl` feature argument additionally exercises `TYPE`/`TTL` in the real paired fixture.
