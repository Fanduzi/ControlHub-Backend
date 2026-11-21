# Non-production Redis worksheet

Select an approved Redis target in `/query`. The existing Objects pane lists its approved keys. Type `GET demo:order:101` in the worksheet and click **Run**. Edit the key and run the command again to read a different value. Each successful run returns one `value` cell, with an explicit NULL marker for an absent key. An empty Redis string stays empty. The shared history records the outcome as `Redis GET` and omits the key and result. The editor uses the ordinary private workspace-draft behavior.

## Enable a target

1. Register a Redis database-instance profile in `Staging`, `Development` or `Test`. Use a standalone TLS Redis server with DB 0. The verified engine baseline is Redis Open Source `8.10.3`.
2. On the Redis server, provision a dedicated password-authenticated read account with `-@all +multi +exists +getrange +exec` and an explicit read-only key pattern that covers the approved keys. Give the application account only these privileges. Reserve ACL and database maintenance for a separate operator identity. Use a certificate valid for the target profile's exact host.
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
    "demo:order:101": "raw_copy_allowed",
    "demo:order:102": "masked_no_copy"
  }
}
```

The configuration authorizes one target. Target discovery and Run read the current file and credential metadata independently. Removing the file, disabling the credential, or removing an approved user or key blocks the next Run against that approval. A request in progress uses the file snapshot acquired for that attempt. Redis itself applies its current ACL to the transaction. The configured CA validates TLS independently on every Redis connection, and the server never follows redirects. An unset configuration leaves Redis Run disabled.

## Execution and evidence

`GET` is case-insensitive. Each command reads one key with 1–128 ASCII letters, digits, colons, underscores or hyphens. The backend issues a fixed Redis transaction with `EXISTS` and `GETRANGE` using a 1,025-byte probe. This distinguishes a missing key from an empty value and rejects values above 1,024 bytes without fetching their full content. Complete UTF-8 strings within the bound pass through the existing disclosure service. Masked results cannot be copied. Non-string or invalid UTF-8 values, writes, batched commands and keys without an applicable policy return a controlled refusal. The frontend uses the single-response result contract.

Each attempt uses a two-second query deadline. Successful, rejected, failed, canceled and timed-out attempts on a resolved target record a history/audit pair before delivery. The backend persists a fixed preview and controlled outcome, with no key, value, credential or full command in the evidence. A persistence failure returns `502 query_backend_error` with no rows. A disconnect before a complete Redis reply does not prove the server stopped the transaction.

## Verify

```bash
go test -tags=integration -count=1 -v -run '^TestRedisWorksheetHTTP$' ./internal/integration
```

The test starts disposable Redis over TLS and MySQL metadata, provisions the Redis target and users through the standard assembly, and exercises the ordinary API. Set `CONTROLHUB_REDIS_BROWSER_CHECKOUT` to the companion frontend checkout (Node and Playwright Chromium installed) to also run the worksheet acceptance in real Chromium. It runs two different keys and checks permissions, audit rollback, stale-grid clearing and the execution history.
