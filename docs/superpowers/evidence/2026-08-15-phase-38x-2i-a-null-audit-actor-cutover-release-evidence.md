# Phase 38X-2I-A Null Audit Actor Cutover Release Evidence

Date: 2026-08-15
Issue: [#30, `38X-2I-A: Publish authentication hardening decisions and preserve anonymous audit cutover`](https://github.com/Fanduzi/ControlHub-Backend/issues/30)

This backend-only release publishes the accepted 2026-08-12 Phase 38X
authentication-hardening decision on main without merging its historical
branch, and makes cutover import preserve valid anonymous authentication
audit events: a source audit row with no actor arrives at the target with no
actor, while a non-empty source actor that cannot be mapped still stops the
cutover loudly inside one target transaction. No authentication or
authorization runtime behavior, audit taxonomy, retention, budget, or
production user data changes; no frontend changes.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and preflight `origin/main` | `877381f7905aca2ed145485f44804089ea59afb9` |
| Task branch | `issue-30-audit-null-actor-20260815-191005` |
| Task worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-30-20260815-191005` |
| Product commit | `ffaeead5f9eed04be1fb90ac66e448570c15cbd4` (`fix(cutover): preserve NULL audit actors and publish 38X hardening ADR`) |
| Merge | Fast-forward only (`git merge --ff-only`), then normal `git push origin main` (`877381f..ffaeead`); no rebase, amend, force-push, tag, or deploy |
| Evidence commit | this commit (docs) |

Changed files (product commit, `git diff 877381f..ffaeead --stat`:
6 files, +318/−5):

- `docs/decisions/2026-08-12-phase-38x-authentication-hardening-decisions.md`
  (new; byte-identical to the historical ADR blob `60d9b910…` at
  `b4f20bf8e69a7a0143d6662a38d4dd5a8a09c4e9`, same path)
- `internal/cutover/import.go` (production fix; only code change)
- `internal/integration/legacy_import_test.go` (new real-MySQL tests)
- `internal/cutover/README.md` (new L2 module doc), `internal/integration/README.md`,
  root `README.md` (three-level docs)

## What The Fix Proves

`internal/cutover/import.go` scanned the legacy `audit_events.actor_user_id`
into a plain `string`, so a valid NULL source actor (migration 00017 makes
the target column nullable for anonymous authentication outcomes) failed the
entire import at scan time: `sql: Scan error on column index 0, name
"actor_user_id": converting NULL to string is unsupported`. The row type now
carries `sql.NullString`:

- a NULL source actor imports as NULL, preserving the fixed
  event/result/target/created-at metadata without fabricated attribution;
- a non-NULL source actor still requires a successful user mapping; an
  unknown actor returns `missing actor user mapping for audit event <type>`;
- the whole import remains one target transaction (`defer tx.Rollback()`), so
  a mapping failure leaves no partial rows in any business table.

## Real-MySQL Integration Evidence

Both new tests run against disposable Testcontainers MySQL 8.0 via the
existing integration harness (`internal/integration/legacy_import_test.go`),
following the pre-existing `TestImportLegacyData_*` pattern — no new test
abstraction.

- **RED (baseline, before fix):** `TestImportLegacyData_PreservesNullAuditActor`
  failed with `scan source audit event: sql: Scan error on column index 0,
  name "actor_user_id": converting NULL to string is unsupported`.
- **GREEN (after fix):** all five `TestImportLegacyData_*` tests PASS.
  - `TestImportLegacyData_PreservesNullAuditActor` seeds a legacy source with
    a nullable actor column and an `auth.bearer`/`rejected` row with NULL
    actor and NULL target (`created_at 2026-01-05 00:00:00`); after import the
    target row has `actor_user_id = NULL`, `target_resource_id = NULL`,
    `event_type = auth.bearer`, `result = rejected`, preserved `created_at`,
    and the mapped non-NULL event imports alongside it (`audit_events` count
    = 2).
  - `TestImportLegacyData_UnknownAuditActorFailsLoudWithoutPartialImport`
    seeds an `auth.login`/`succeeded` row whose actor is not present in the
    legacy `users` table; import fails loudly with `missing actor user
    mapping for audit event auth.login` and all eleven business tables
    (`roles`, `users`, `environments`, `owners`, `resources`,
    `resource_profiles_host`, `resource_profiles_database_instance`,
    `resource_profiles_database_cluster`, `resource_profiles_service`,
    `resource_relations`, `audit_events`) have count 0 after rollback.
  - The three pre-existing import tests (full UUID-to-bigint migration,
    non-empty target rejection, parseTime validation) remain green.

## Candidate Gates

| Command | Result |
| --- | --- |
| `git diff --check` (candidate worktree, exact SHA) | PASS, exit 0 |
| `gofmt -l` on changed Go files | clean |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | 1729 PASS, 0 FAIL (14 packages) |
| `go test -race -count=1 ./...` | 1729 PASS, 0 FAIL (14 packages) |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | 232 PASS, 0 FAIL |
| `go test -tags=integration -count=1 -run TestOpenAPIFuzz ./internal/integration` | 2 PASS (Schemathesis) |
| Three-level docs (`scripts/check-docs.sh` + `check_three_level_doc.sh`) | PASS, 0 errors |
| CodeGraph | index available; `importAuditEvents` has exactly one caller (`importer.run`), now exercised by the new integration tests |

Merged-root re-run (after `git merge --ff-only` + push, from the root
worktree at the merged SHA): `go test -count=1 ./...` and
`go test -race -count=1 ./...` both 1729 PASS / 0 FAIL; `go vet`, `go build`,
OpenAPI validation PASS.

## CI On Product SHA

CI run
[31883490781](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31883490781)
on merged `main` at head `ffaeead5f9eed04be1fb90ac66e448570c15cbd4`:
`status=completed`, `conclusion=success`.

- `release-local-gates` job
  [95009201245](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31883490781/job/95009201245)
  — `success`.
- `release-docker-gates` job
  [95009201315](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31883490781/job/95009201315)
  — `success` (Testcontainers MySQL integration + Schemathesis fuzz).

## Independent Review

Two-axis `/code-review` of the candidate range (`877381f..ffaeead`) with
fresh-context read-only reviewers:

- **Standards axis:** no P1; one P2 fixed before merge (L3 header of
  `import.go` claimed exports that live in `local.go` — corrected to
  `ImportLegacyData, ImportConfig`); one P3 accepted (test setup duplication
  matches the file's existing explicit-setup convention).
- **Spec axis:** verdict APPROVE, P1=0, P2=0. One spec P2 fixed before merge
  (no-partial-import assertion extended from 6 to all 11 business tables).
  One spec P1 adjudicated false positive against repo convention: the
  release-evidence document is recorded post-merge on main (all prior
  evidence commits are `docs(evidence)` post-merge commits citing merged
  SHAs); this commit is that evidence.
- No scope creep: diff touches only the ADR publication, the cutover audit
  import fix, its real-MySQL tests, and the required three-level docs.

## Root WIP Preservation

Before fetch or repository mutation, root was on `main` at
`877381f7905aca2ed145485f44804089ea59afb9`. The root WIP manifest was
captured (tracked-modified, staged, NUL-safe `git status --porcelain -z`,
untracked) before any change and re-verified byte-identical after merge,
push, and evidence commit:

| Status | Path |
| --- | --- |
| modified | `CLAUDE.md`, `advisor-plans/README.md` |
| untracked | `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md` |
| untracked | `docs/agents/domain.md`, `docs/agents/issue-tracker.md`, `docs/agents/triage-labels.md` |
| untracked | `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/decisions/2026-08-09-operator-session-boundary.md` |
| untracked | `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`, `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`, `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` |

None of these paths overlaps the product diff. The root worktree was
observe-only: no stash, restore, reset, clean, relocate, or overwrite
occurred. Existing root services, Docker containers, fixtures, historical
worktrees, and branches were not started, stopped, or modified.

## Known ADR Cross-Reference P3

The published ADR
(`docs/decisions/2026-08-12-phase-38x-authentication-hardening-decisions.md`)
is byte-identical to its historical blob and therefore still references two
paths not yet tracked on main: `docs/decisions/2026-08-09-operator-session-boundary.md`
(in root WIP, untracked) and
`docs/superpowers/evidence/2026-08-12-phase-38x-1-operator-access-boundary-parent-release-evidence.md`
(historical branch only). These references are accepted as-is (P3) because
the ADR must be published verbatim; they close naturally when those documents
land on main.

## Cleanup And Issue Safety

- At the time this evidence was committed, no task resource had been deleted:
  the task worktree and the local candidate branch are retained for the
  mandated post-evidence independent verification. Per the authorized closure
  protocol, they are deleted only after the final CI run and the independent
  verification both pass; the deletion receipt is recorded in the final
  report. All other worktrees, branches, services, fixtures, and root WIP are
  preserved.
- Issue #30 is open at evidence time. After this release passes independent
  verification, #30 closes with a factual comment (final SHA, evidence path,
  CI URL); #29, #31, #32, #20, and #7 must remain open.
