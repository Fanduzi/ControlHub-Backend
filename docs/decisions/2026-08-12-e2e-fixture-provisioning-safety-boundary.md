# Decision: E2E Fixture Provisioning Safety Boundary

## Status

Accepted for the 38X-1D backend prerequisite (Issue #19, blocks frontend
Issue #15). Supersedes the initial `cmd/e2e-fixture-bootstrap` design whose
safety claim rested on `.invalid` fixture emails; production safety is now
provided by an explicit capability flag plus a dedicated disposable-DSN gate.

## Context

Frontend real E2E requires explicit, per-run, non-public fixture operators
(admin + editor). Migration 00016 disabled the published 0002 seed accounts,
and the frontend suite must never use, reactivate, or fall back to them.
The initial provisioning seam validated fixture identities (`.invalid` emails,
retired-seed refusal) but still accepted any reachable `DATABASE_DSN`: a
misconfigured production DSN could let the command create a usable
production administrator. RFC 2606 `.invalid` emails only prevent colliding
with existing real mailboxes; they do not prevent creating a new one.

## Decision

`cmd/e2e-fixture-bootstrap` refuses to run unless ALL of the following hold
before any mutation:

1. `CONTROLHUB_E2E_FIXTURE_MODE=1` — an explicit test-only capability;
   missing or any other value fails loudly.
2. `E2E_FIXTURE_DATABASE_DSN` — a dedicated E2E metadata DSN; the generic
   `DATABASE_DSN` is never read. The DSN must parse (MySQL driver parser,
   errors never echoed), the host must be a literal loopback address
   (`127.0.0.1` / `::1`; hostnames such as `localhost` are refused because
   their resolution cannot be verified locally), and the database name must
   match the disposable naming rule `^controlhub_[a-z0-9_]*e2e$`. The
   default `controlhub` database and production-like names are rejected.
3. Migration 00016 must have an applied row in `goose_db_version` (version
   16 itself, `is_applied = 1`; a later applied version does not satisfy the
   gate), and both retired seed accounts must exist and be inactive;
   otherwise provisioning refuses.
4. Fixture emails must be printable-ASCII-only (control bytes are
   ignorable in MySQL collation, so they could collide) and end with
   `.invalid`, the retired seed identities are refused, and the admin and
   editor fixture emails must be distinct (identical emails would
   silently drop the administrator) — additional guards only, not the
   primary boundary. The printable-ASCII rule mirrors the `users.email`
   unique key's `utf8mb4_0900_ai_ci` collation, which Go string equality
   cannot replicate.

All gates are verifiable: DSN/capability validation happens before opening a
connection, migration/seed verification happens before the first upsert, and
the upserts are transactional (a partial failure rolls back), so a
misconfigured production-like DSN produces no user mutation. The boundary
protects against accidental misconfiguration; it is not an adversarial
capability claim.

## Consequences

- The frontend CI and local E2E runs must provision a dedicated disposable
  `*_e2e` database (CI: created inside the ephemeral MySQL service) and run
  migrations against it before invoking the seam.
- `cmd/bootstrap-admin` remains the operator path for real administrators and
  is unaffected.
- No production authorization behavior changes; fixtures are ordinary users
  under the server-enforced role matrix.
