# Phase 38W-2 Personal Templates Release Evidence

Date: 2026-08-06
Issue: #3, `38W-2: Save and load personal parameterized templates`

## Candidates

| Item | Backend | Frontend |
|---|---|---|
| Base SHA (`origin/main` merge-base) | `9f68be108fdc393e0fcae118453709a5451460f4` | `26172ac6dc2efae3d773ecf3885bba523bb7ff65` |
| Candidate branch | `phase-38w-2-personal-templates-backend-20260805` | `phase-38w-2-personal-templates-frontend-20260805` |
| Candidate SHA | `cfec6fb789791b060f2df9c108813e296678fa1e` | `2797c226337f9b205c78950fea2a14945d44a42d` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-38w-2-backend-20260805` | `/Users/fan/JsProjects/ControlHub-38w-2-frontend-20260805` |
| Candidate status | Clean | Clean |
| Candidate range | `9f68be1...cfec6fb` | `26172ac...2797c` |
| Candidate commits | 12 (11 feature + 2 repair) | 19 |
| Fast-forward ancestry | Candidate descends from the exact base and current `origin/main` | Candidate descends from the exact base and current `origin/main` |

Backend candidate ownership note: the previously recorded candidate `84e35a19b2996c7f5c06b5e50e3e3fbb2c44c44f` is an
ancestor of `cfec6fb`; the single commit `84e35a19..cfec6fb` is
`fix(query): reject repeated template placeholders` (2 files: compiler declaration + test), which directly
implements issue #3's invalid-placeholder-declaration rejection. No unrelated work was mixed in; the candidate
was updated to `cfec6fb` without reset or new worktree.

## Backend Gates

All commands ran from the backend candidate worktree at `cfec6fb`.

| Command | Result |
|---|---|
| `git diff --check 9f68be1...HEAD` | PASS, exit 0 |
| `make release-readiness-gates` | PASS, exit 0: `go test -count=1 ./...` (10 packages ok), `go vet ./...`, `go build ./...`, `make openapi-validate` (`TestOpenAPIYAMLIsValid` passed), `make test-integration` (Testcontainers MySQL 8.0, migrations 1-14 applied, no failed/skipped) |
| `make test-openapi-fuzz` | PASS, exit 0: disposable MySQL + Schemathesis bounded run, no contract violations |
| `go test ./... -cover` | PASS, exit 0; 1371 tests passed in 10 packages |
| `golangci-lint run --new-from-rev=origin/main ./...` | 2 pre-existing `errcheck` notes (`defer rows.Close()` / `defer db.Close()` in repository + test); matches repo-wide existing style, not introduced by the candidate |
| `go mod tidy -diff` | Pre-existing untidy declarations (vitess.io/vitess, go-sqlmock, x/sync listed indirect); not introduced by the candidate; flagged as note only |

## Frontend Gates

All commands ran from the frontend candidate worktree at `2797c`.

| Command | Result |
|---|---|
| `git diff --check 26172ac...HEAD` | PASS, exit 0 |
| `npm run release:local` | PASS: runtime check, E2E preflight (`:3100`/`:8081` free), E2E governance (13 specs scanned), `tsc --noEmit`, lint (0 errors, 5 pre-existing warnings), Vitest 87 files / 1359 tests, `next build` |
| `npm run release:e2e` against candidate backend | PASS: smoke 7/7, interaction 3/3, full 143/143 (includes the three parameterized-template no-side-effect tests) |
| Focused `playwright test e2e/query-workbench.spec.ts --grep '...parameterized personal template loads typed form'` | PASS 3/3 (desktop EN, 375px EN, desktop zh-CN) |

### Real E2E service provenance

- Backend binary built from `cfec6fb`, CWD = backend candidate worktree, served `:8082`.
- `PLAYWRIGHT_PROXY_TARGET=http://localhost:8082` routed the E2E API proxy to the candidate server.
- Query fixture: pre-existing `controlhub-query-e2e-mysql` container (host `127.0.0.1`, port `13306`, database `query_e2e`, running 13 days); idempotent `QUERY_DEV_ALLOW_TARGET_FIXTURE=true QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target` confirmed target 616 ready with `availableActions.run=true`.
- The three no-side-effect tests assert zero execute/explain/schema/`/query-targets/{id}/executions`/related-record/disclosure requests during template load, in all three locales/viewports.

## Review

Fresh read-only review of the exact candidate ranges, using the repository review skill's Standards and Spec
axes (sub-agent review model was unavailable due to a provider opt-in error; the review was completed read-only
in-session with the full spec and diffs).

- Backend `9f68be1...cfec6fb` (25 files, +980/-135): verdict APPROVE. P1 findings: 0. P2 findings: 0.
- Frontend `26172ac...2797c` (10 files, +1058/-42): verdict APPROVE against issue #3's five acceptance
  criteria. P1 findings: 0. P2 findings: 0.
- Non-blocking P3 notes: (a) the template-execution route
  (`POST /query-targets/{id}/saved-statements/{statementId}/execute`) and paged template execution are
  described in the 38W specification but are outside issue #3's body ("Loading ... never executes SQL");
  consistent across both repositories, and the spec anticipates follow-on ticketing. (b) Client-side
  declaration validation duplicates server rules by design (UX pre-check; server stays authoritative).
- E2E hygiene: the candidate adds no route mocks, forced clicks, `page.evaluate`, skipped cases, fixmes,
  global timeout relaxation, or fixed sleeps (verified against the full E2E diff).

## Root Preservation

Root repositories: `/Users/fan/GolangProjects/ControlHub` (backend), `/Users/fan/JsProjects/ControlHub` (frontend).

Preserved backend root dirty paths:

- `CLAUDE.md`
- `advisor-plans/README.md`
- `AGENTS.md.bak-pre-gitnexus-uninstall`
- `CLAUDE.md.bak-pre-gitnexus-uninstall`
- `CONTEXT.md`
- `docs/agents/`
- `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`
- `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`
- `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`

Preserved frontend root dirty paths:

- `AGENTS.md`
- `CLAUDE.md`
- `.codegraph/`
- `AGENTS.md.bak-pre-gitnexus-uninstall`
- `CLAUDE.md.bak-pre-gitnexus-uninstall`

The candidate changed-path sets and the root dirty-path sets have no overlap. Root WIP was not staged,
stashed, reset, cleaned, relocated, or restored in either repository.

## CI and Cleanup State

At evidence-commit time, neither candidate branch has a remote ref (`gh pr list` returned none for either
branch). No candidate CI conclusion exists before the merge/push step. This documentation-only evidence
commit intentionally omits its own SHA and does not pre-record merged or CI results; the authorized
fast-forward merge, push, required CI verification, and issue #3 closing comment are recorded at closure
time. Candidate worktrees and branches remain present for that sequence.

## Post-Merge Closure

Fast-forward merge and normal push range:

- Backend: `9f68be108fdc393e0fcae118453709a5451460f4` to `6fe2a24c4366a71616cfc9e90acd2bb6f8f33b23` (`main`).
- Frontend: `26172ac6dc2efae3d773ecf3885bba523bb7ff65` to `2797c226337f9b205c78950fea2a14945d44a42d` (`main`).

Both roots: `git merge --ff-only <candidate-branch>` then `git push origin main`; `HEAD == origin/main`
verified after push. Root dirty-path whitelists from the Root Preservation section were re-verified before
and after the merge; no WIP was staged, stashed, reset, cleaned, relocated, or restored.

Merged-root gates (commands re-run from the merged roots, matching CI jobs):

- Backend root `make release-local-gates`: PASS (go test, vet, build, openapi-validate).
- Frontend root `npm run release:local`: PASS (preflight, governance, tsc, lint 0 errors / 5 pre-existing
  warnings, Vitest 87 files / 1359 tests, `next build`).

CI:

- Backend run [31070414945](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31070414945),
  head `6fe2a24c4366a71616cfc9e90acd2bb6f8f33b23`, completed, conclusion success; required jobs
  `release-local-gates` and `release-docker-gates` both success.
- Frontend run [31070414967](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31070414967),
  head `2797c226337f9b205c78950fea2a14945d44a42d`, completed, conclusion success after one rerun; required
  jobs `release-local` and `release-e2e` both success.

First-attempt flake note: the initial frontend run failed on two tests in files outside the candidate
changed-path set. (a) `tests/components/query-workbench.test.tsx` "Filter Apply triggers a replace fetch
with filter params" — Radix Select option-render race; reproduced locally in isolation at 2/3 failure
rate, passes in the full suite; test file and component byte-identical to the pre-candidate base.
(b) `e2e/resource-archive.spec.ts:68` — test-created resource visibility race in a spec untouched by the
candidate. The rerun passed 1359/1359 (release-local) and 143/143 (release-e2e). Neither failure was
attributable to the candidate.

Cleanup: candidate worktrees and branches are intentionally preserved; nothing was deleted. Pre-existing
services (`:8080` backend root process, `controlhub-query-e2e-mysql` container, mac-connector) were not
modified or stopped.

The SHA of this documentation-only post-merge evidence update is intentionally omitted from this file.
