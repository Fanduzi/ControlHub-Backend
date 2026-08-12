# Evidence: 38X-1D E2E Fixture Provisioning Release (Issue #19)

Release closure evidence for the test/CI-only `cmd/e2e-fixture-bootstrap`
provisioning seam. Issue #19 unblocks frontend Issue #15 only; it does NOT
publish #15. #15 remains OPEN.

## Verified candidate facts (recorded before merge)

- Repository: `Fanduzi/ControlHub-Backend` (backend root:
  `/Users/fan/GolangProjects/ControlHub`)
- Base: `4bd661db03e344907b07ab499adcdef89af6563a`
- `origin/main` at gate time: `4bd661db03e344907b07ab499adcdef89af6563a`
  (unchanged from base; candidate is a descendant, so an ff-only merge is
  possible)
- Candidate branch: `e2e-fixture-bootstrap-20260811`
  (worktree `/Users/fan/GolangProjects/ControlHub-wt-issue-15-e2e-fixtures-20260811`)
- Candidate HEAD at gate time: `c4b40d33acf284dd5b90deced8e69b149c80d7ed`
- Candidate diff vs base: 7 commits, all in scope (new command, its tests,
  integration coverage, module/decision docs, root README module table)
- Candidate worktree clean at gate time; candidate diff and the backend-root
  WIP manifest share no paths

## Safety contract (implemented and verified before any mutation)

1. `CONTROLHUB_E2E_FIXTURE_MODE=1` is required; missing or any other value
   fails loudly.
2. `E2E_FIXTURE_DATABASE_DSN` is the only DSN read; the generic
   `DATABASE_DSN` is never read. The DSN must parse (driver errors are
   sanitized, never echoed), the host must be a literal loopback address
   (`127.0.0.1` / `::1`; hostnames such as `localhost` are refused), and the
   database name must match `^controlhub_[a-z0-9_]*e2e$` (default
   `controlhub` and production-like names rejected).
3. Migration 00016 must have an APPLIED row (`version_id = 16 AND
   is_applied = 1`; a later applied version does not satisfy the gate) AND
   both retired 0002 seed accounts (`admin@example.com` /
   `editor@example.com`) must exist and be inactive.
4. Fixture identities are printable-ASCII-only, must end with `.invalid`
   (RFC 2606), admin and editor emails must be distinct, and the published
   seed email/password are refused outright.
5. Provisioning is transactional: the admin and editor upserts commit or
   roll back together; a partial failure never leaves a usable fixture
   administrator behind. Reactivation rotates `authorization_version`.
6. Output prints only identities, roles, and outcomes — never a DSN,
   password, hash, Bearer token, or session key.

## Real CLI verification (isolated disposable MySQL)

Method: a fresh `mysql:8.0` container on a random loopback port (the
pre-existing local MySQL fixture was never touched), a dedicated
`controlhub_issue15_e2e` database migrated with goose to 00016, and the real
`go run ./cmd/e2e-fixture-bootstrap` command driven by environment. All
credentials lived in a chmod-600 temp file and were never echoed; the
container was terminated afterward.

Negative matrix (all fail loudly, before mutation):

- missing capability flag; wrong capability value
- non-loopback host (`db.example.com`)
- default database name (`controlhub`); production-like name
  (`production_e2e`)
- published seed email identity; published seed password
- identical admin/editor emails
- unmigrated database (refused; zero tables created — no mutation)
- active retired seed (refused; seed row left untouched)
- migration 00016 unapplied despite a version-17 applied row (refused)

Positive and idempotent path:

- random `.invalid` admin+editor fixtures provisioned with roles
  `admin`/`editor`, both active, retired seeds stay inactive, output is
  secret-free
- re-run reactivates both and rotates `authorization_version` (1 -> 2); old
  credentials die
- simulated second-identity failure (editor role lookup broken) fails loudly
  and rolls back the whole transaction: zero partial rows, existing admin
  row untouched

Result: 21/21 checks PASS on 2026-08-12.

## Candidate gates (run at the candidate HEAD above)

- `git diff --check <base>...HEAD`: PASS
- `gofmt -d` on all changed Go files: clean
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `go test -count=1 ./...`: PASS (13 packages)
- `go test -race -count=1 ./...`: PASS (13 packages)
- `make openapi-validate`: PASS
- `make test-integration`: PASS (full Docker-backed suite, including the new
  real-CLI test `TestE2EFixtureBootstrapCommandProvisionsAndRollsBackAgainstMySQL`)
- Three-level documentation check (`check_three_level_doc.sh`): PASS (exit 0;
  root README L1 module table updated in the same change set)

## Independent reviews (fresh-context, read-only; Standards / Spec / Security)

Six review rounds were run with the pi-subagents `reviewer` role over
`<base>...HEAD`. Final verdicts at the candidate HEAD: Standards P1=0 P2=0;
Spec P1=0 P2=0; Security P1=0 P2=0. Fixes landed along the way, all within
the candidate branch:

- distinct admin/editor identity gate (identical emails would silently drop
  the administrator)
- migration gate requires an applied row for version 16 itself
- real-MySQL CLI integration coverage (create / reactivate / rollback /
  secret hygiene / retired-seed preservation)
- printable-ASCII-only fixture emails (collation-equivalent identity key)
- control-byte rejection (ignorable in `utf8mb4_0900_ai_ci`)
- retired-seed identity guard made reachable (checked before the generic
  `.invalid` gate) with discriminating tests
- editor-seed negative coverage and discriminating secret-leak assertions
  (plaintext + SHA-256 hashes; malformed-DSN error echo)

## WIP preservation (backend root)

The backend-root dirty manifest (whitelist: `CLAUDE.md`,
`advisor-plans/README.md`, `*.bak-pre-gitnexus-uninstall`, `CONTEXT.md`,
`docs/agents/`, `docs/decisions/`, `docs/superpowers/plans/`,
`docs/superpowers/specs/`) was captured before, during, and after the task
and remained byte-identical. No backend-root file was created by this task;
transient subagent artifacts were removed immediately.

## Scope boundary

- #19 unblocks #15 only; it does not publish #15. #15 must remain OPEN.
- Frontend worktrees/branches, Issue #12/#13 worktrees, the rescue branch,
  backend root services, and the existing MySQL fixture were not touched.
- No production authorization behavior changed; fixtures are ordinary users
  under the server-enforced role matrix.

## Post-merge facts

Added by a follow-up docs-only commit after the ff-only merge and push, once
the exact merged SHA and CI results were known.
