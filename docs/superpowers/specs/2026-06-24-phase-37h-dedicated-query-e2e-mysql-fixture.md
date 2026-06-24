# Phase 37H Dedicated Query E2E MySQL Fixture Design

## Context

Phase 37 added the backend read-only query execution API. Phase 37F added frontend query execution UI wiring. Phase 37G introduced a local ready-target fixture so the frontend query workbench can run the previously skipped ready-target E2E tests.

The first 37G approach proved the flow, but it used `DATABASE_DSN` as both:

- the ControlHub metadata database connection, and
- the query target credential DSN used by the workbench.

That is useful for a spike, but it is the wrong long-term boundary. ControlHub should not query its own metadata database as the sample query target. Query Workbench E2E needs a dedicated query database that behaves like an external database instance.

## Goal

Provide a dedicated Docker-backed MySQL database for local Query Workbench E2E.

The fixture must make one local `database_instance` target ready by pointing its resource profile at the dedicated query MySQL host and port, while the server resolves a read-only credential DSN from `CONTROLHUB_QUERY_CREDENTIAL_<REF>`.

Success means the frontend query E2E can run against a real backend, a real ready target, and a separate query database:

- no ready-target skips,
- safe `SELECT` succeeds,
- unsafe statement rejection is visible,
- query history is recorded,
- ControlHub metadata DB is not used as the query target.

## Non-Goals

- No credential management UI.
- No credential write HTTP API.
- No production auto-enable.
- No new query engines.
- No Redis, MongoDB, PostgreSQL, or ClickHouse execution.
- No export, saved queries, approvals, or governance workflow.
- No CI workflow changes in this phase.
- No migration.
- No change to Phase 37 credential binding semantics.
- No storage, logging, or printing of plaintext DSNs or passwords.
- No tag, release, or deployment.

## Design Summary

Phase 37H keeps the useful part of 37G: an explicit dev-only fixture mode that creates or reuses a `source='dev-fixture'` resource and seeds metadata-only credential configuration.

The key correction is where host and port come from.

`DATABASE_DSN` is only the ControlHub metadata database. It is used to open the repository connection.

The query target host and port must come from `CONTROLHUB_QUERY_CREDENTIAL_<REF>`, because this is the DSN that the server later resolves during query execution. The fixture reads that credential DSN, parses its `tcp(host:port)` address, and writes only host and port to the resource profile. The DSN itself is never stored or printed.

The resulting flow is:

```text
Dedicated Docker MySQL starts on 127.0.0.1:<port>
  -> script creates query_e2e schema, table, seed rows, read-only user
  -> CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO points to this Docker MySQL
  -> querydev fixture parses that credential DSN host:port
  -> querydev ensures local database_instance profile host:port
  -> querydev upserts metadata-only credential row
  -> backend server starts with the same credential env var
  -> frontend E2E sees one ready target and runs the ready-target tests
```

## Dedicated Query MySQL

Add a local Docker fixture controlled by a script and Makefile targets.

Default runtime values:

- container: `controlhub-query-e2e-mysql`
- port: `13306`
- database: `query_e2e`
- read-only user: `query_e2e_ro`
- read-only credential ref: `LOCAL_QUERY_RO`

The script should be idempotent:

- `up` creates or starts the container,
- schema creation is safe to re-run,
- seed rows are stable,
- read-only user grants are safe to re-run,
- `up` writes a local gitignored env file containing the query credential DSN,
- `down` stops and removes only the named fixture container.

The fixture database should contain a tiny deterministic dataset, for example:

```sql
create table if not exists query_e2e_items (
  id bigint unsigned not null primary key,
  name varchar(64) not null,
  category varchar(32) not null,
  created_at timestamp not null default current_timestamp
);

insert into query_e2e_items (id, name, category)
values
  (1, 'alpha', 'sample'),
  (2, 'beta', 'sample')
on duplicate key update
  name = values(name),
  category = values(category);
```

The read-only user must have `SELECT` only on the fixture schema.

## Environment Contract

The local command sequence uses these environment variables:

| Variable | Purpose |
| --- | --- |
| `DATABASE_DSN` | ControlHub metadata database only. Never used as the query target. |
| `QUERY_DEV_ALLOW_TARGET_FIXTURE` | Must be `true` to create or reuse the local dev fixture target. |
| `QUERY_DEV_CREDENTIAL_REF` | Credential ref to store in metadata, usually `LOCAL_QUERY_RO`. |
| `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` | Dedicated query MySQL read-only DSN. Parsed for host:port and later resolved by the server. |
| `QUERY_DEV_ENVIRONMENT_POLICY` | Defaults to `non_prod_only`. |
| `QUERY_E2E_MYSQL_PORT` | Optional dedicated query MySQL host port. Defaults to `13306`. |
| `QUERY_E2E_MYSQL_CONTAINER` | Optional container name. Defaults to `controlhub-query-e2e-mysql`. |
| `.query-e2e-mysql.env` | Local gitignored env file written by `make query-e2e-mysql-up`. Contains `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO`. |

The docs and command output may state:

```text
credential DSN supplied via env and matched dedicated query MySQL host:port
```

They must not print the DSN value.

The local env file is the only supported handoff mechanism for the generated dedicated query DSN. It must be added to `.gitignore`, must not be committed, and must not be printed by the fixture script. Manual command examples should source the file instead of spelling out the DSN value.

## Query Target Fixture Rules

The Phase 37G source-boundary fix remains required.

The fixture may reuse a resource only when all of these match:

- `name='local-mysql-query-dev'`,
- `resource_type='database_instance'`,
- environment slug is `dev`,
- `source='dev-fixture'`.

If a same-name non-fixture resource exists, fixture creation must fail closed and must not overwrite the profile.

The rollback filter must keep using `source='dev-fixture'` and the exact fixture id. Name-only rollback is not allowed.

## Querydev Correction

The `cmd/querydev` fixture mode must not derive the query target profile from `DATABASE_DSN`.

Instead, with `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`:

1. Open ControlHub metadata DB using `DATABASE_DSN`.
2. Resolve `QUERY_DEV_CREDENTIAL_REF`.
3. Read `CONTROLHUB_QUERY_CREDENTIAL_<REF>` using the existing credential resolver behavior.
4. Parse that credential DSN's explicit `tcp(host:port)` address.
5. Ensure the dev fixture target profile uses that host and port.
6. Run the existing metadata-only credential seed.
7. Print only safe metadata and derived readiness.

This preserves Phase 37 credential binding: execution succeeds only if the server later resolves the same credential DSN and that DSN matches the target profile host and port.

## Local Verification Flow

The expected local verification flow is:

```bash
make query-e2e-mysql-up

set -a
. ./.env
. ./.query-e2e-mysql.env
set +a

QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO \
make seed-query-dev-target

go run ./cmd/server
```

Then from the frontend repo:

```bash
npm run test:e2e -- --grep query
```

The result must include the ready-target tests with zero skips.

## Acceptance Criteria

- Dedicated Docker MySQL can be started and stopped without touching the ControlHub metadata database container or local MySQL instance.
- `DATABASE_DSN` is never used as the query target DSN.
- The dev target profile host and port match `CONTROLHUB_QUERY_CREDENTIAL_<REF>`.
- The server process receives the same credential env var used by the fixture seed.
- The dedicated query credential DSN is handed off through `.query-e2e-mysql.env`, not copied into docs, shell output, or reports.
- `/query-targets` shows exactly one ready local fixture target in the local test setup.
- Frontend query E2E passes with no ready-target skips.
- Unsafe query E2E still shows controlled rejection.
- Query history E2E shows the executed attempt.
- No DSN or password is printed, stored, logged, or returned.
- No CI workflow is changed in this phase.

## Relationship To Phase 37G

Phase 37G's implementation branch is useful and should not be discarded. Phase 37H should continue from that branch if it is clean, preserving:

- metadata-only credential seed,
- explicit `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`,
- `source='dev-fixture'` reuse boundary,
- same-name non-fixture fail-closed behavior,
- no migration and no new repository method.

The behavior to replace is only the use of `DATABASE_DSN` as the target host and port source.

If Phase 37G is merged before 37H, Phase 37H becomes a corrective follow-up. If Phase 37G is still local, Phase 37H should be applied on top of it before merging.

## Deferred Work

CI wiring is intentionally deferred. The Docker fixture is designed to be CI-portable, but this phase does not edit GitHub Actions workflows or cross-repo CI configuration.

Once local proof is stable, a later phase can run:

- backend start,
- dedicated query MySQL start,
- fixture seed,
- frontend cross-repo query E2E,

inside the existing frontend E2E workflow.
