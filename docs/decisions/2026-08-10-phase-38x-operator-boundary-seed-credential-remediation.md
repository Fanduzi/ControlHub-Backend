# Decision: Seed Credential Remediation For The Operator Boundary

## Status

Accepted for Phase 38X Issue #13 (operator boundary). Applies to the published
seed users created by `migrations/0002_seed_reference_data.sql`, the bootstrap
path for a real administrator, and startup signing-secret validation. P1 and P2
review findings are remediated by this decision; P3 findings are explicitly
deferred (see item 7).

## Context

Migration 0002 seeds two users with published credentials
(`admin@example.com` / `editor@example.com`, both with the shared password hash
of `secret123`). README, CLAUDE.md, and OpenAPI publish those credentials.
Migration 00015 added `is_active` and `authorization_version`, and services now
reject inactive users. An operator review of the authentication boundary rated
the published credentials as P1/P2: on any deployed instance, anyone can sign
in with them.

Migration 0002 must not be rewritten. It has already shipped; every migrated
environment applied it, so editing it would let fresh environments and existing
environments diverge. The remediation therefore rides on new forward-only
change: disable the seeds, and create real administrators only through an
explicit operator-invoked command. The server never mutates user state on its
own.

## Decision

1. **Disable the seed users in a new forward-only migration.** A migration
   after 00015 (for example `00016_disable_seed_users.sql`) sets
   `is_active = 0` for `admin@example.com` and `editor@example.com`. Migration
   0002 is untouched. No later migration re-enables the seeds, and a release
   rollback does not restore them to active.

2. **Disable, do not delete.** `audit_events.actor_user_id` references the
   seeded users for the events created by 0002; deleting the users would orphan
   audit attribution. Disabling keeps history resolvable while the `is_active`
   check ends sign-in, with no FK or data changes.

3. **Bootstrap a real administrator through an explicit command.** A dedicated
   command (for example `cmd/bootstrap-admin`) runs only when explicitly
   invoked. The deployment operator supplies the administrator email and
   password at invocation time, via flags or environment. The command creates
   the admin user, or re-enables an existing disabled account while rotating
   `authorization_version` so previously issued tokens die. It exits after
   doing so. It is never invoked by the server process and never runs at
   startup; startup performs zero user mutations.

4. **Deployment migration behavior.** A release applies the disable migration
   first, then the operator runs `bootstrap-admin`, then the server starts
   serving traffic. From the moment the migration applies, the published
   credentials stop working; there is no grace period in which they remain
   valid. The same change set removes the published credentials from README,
   CLAUDE.md, and OpenAPI, and points deployment documentation at the
   bootstrap command.

5. **Local and integration fixture policy.** Local development and
   integration/E2E suites create their own users through fixtures and support
   helpers (for example the existing integration test support), never through
   the 0002 seed credentials. Tests must not assume the seed users are active;
   any test that needs an administrator provisions one itself.

6. **Startup rejects blank or placeholder signing secrets.** Configuration
   validation fails startup when the signing secret is empty or a known
   placeholder (for example `changeme`, `secret`, `your-secret-key`, or the
   demo value currently published in docs). A deployed instance must not boot
   with a guessable signing key. Local development generates a real random
   secret into `.env`.

7. **P3 is explicitly deferred.** P3 findings from the review are not part of
   this remediation and are tracked separately for a later issue. This ADR
   grants them no scope here.

## Consequences

- New deployments can no longer be signed into with the published credentials;
  the only admin path is the operator-invoked bootstrap command.
- Existing deployments get the fix by migration, with no 0002 rewrite and no
  data loss; audit attribution stays intact.
- Operators own bootstrap timing: an instance migrated but not yet bootstrapped
  has no usable administrator until the command runs.
- Tests and E2E continue to work from their own fixtures, which provision their
  own users.
- Startup is stricter: a missing or placeholder signing secret is a hard
  startup error rather than a running instance with guessable tokens.

## Rejected Alternatives

- **Delete the seed users outright.** Rejected because `audit_events` rows
  reference `actor_user_id` for the seeded events; deletion would break
  historical attribution. Disabling preserves the rows and the reference while
  ending sign-in.
- **Edit 0002 to remove the seed inserts.** Rejected because 0002 has already
  shipped; environments that applied it would keep the users while fresh
  environments would not, so the two diverge. A new forward-only migration
  keeps every environment on one path.
- **Auto-create an administrator at startup when none exists.** Rejected
  because it mutates the database on every boot, runs on credential-free
  defaults, and gives the operator no control over the bootstrap identity. An
  explicit command keeps the mutation one-time, logged, and operator-owned.
- **Keep the published credentials as a documented demo fallback.** Rejected
  because services now enforce `is_active` and the review rated the exposure as
  P1/P2; a published credential that works in production is a standing
  backdoor, not a demo.

## References

- Backend Issue #13: operator boundary, P1/P2 remediation
  (`Fanduzi/ControlHub-Backend#13`)
- Phase 38X operator authentication boundary spec:
  `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`
- Related migration: `migrations/00015_user_authorization_version.sql`
  (`is_active`, `authorization_version`)
- Candidate branch: `issue-13-operator-boundary-20260809-121228`
