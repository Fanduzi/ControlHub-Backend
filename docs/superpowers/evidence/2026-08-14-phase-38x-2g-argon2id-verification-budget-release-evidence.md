# Phase 38X-2G Argon2id Verification Budget Release Evidence

Date: 2026-08-14
Issue: [#27, `38X-2G: Prove Argon2id verification budget on the lowest supported deployment`](https://github.com/Fanduzi/ControlHub-Backend/issues/27)

This backend-only release establishes the reproducible Argon2id
verification-budget proof required to unblock Issue #25's independent
re-verification. It documents the target environment, adds a fail-loud budget
gate at the real password-verification seam, and records the measurement
method and thresholds. It does not change the production Argon2id parameters
or the legacy successful-login migration path.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and preflight `origin/main` | `2e864b08cbb33df33b12f8f70bc71dd3929e067b` |
| Task branch | `issue-27-argon2id-budget-20260814-214034` |
| Task worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-27-argon2id-budget-20260814-214034` |
| Budget-gate commit | `7282ba0` (`test(auth): prove Argon2id verification budget at the password-verification seam`) |
| Evidence commit | this commit (docs) |

Changed files (budget-gate commit):

- `internal/service/password_hasher_budget_test.go` (new)
- `Makefile` (new `argon2id-budget` target, `.PHONY`)
- `.github/workflows/backend-ci.yml` (dedicated budget gate step + raw artifact upload)
- `.gitignore` (`/.argon2id-budget/`)
- `internal/service/README.md` (module file table)

Docs commit (this one):

- `docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md` (new)
- `docs/superpowers/evidence/2026-08-14-phase-38x-2g-argon2id-verification-budget-release-evidence.md` (this file)

`internal/service/password_hasher.go` (production parameters `m=65536,t=3,p=1`)
and `internal/service/auth_service.go` (legacy migration login path) have zero
diff lines against base — confirmed by the independent review.

## Documented Target Environment

The repository documents no deployment specification (exhaustive search of
`README.md`, `Makefile`, `.env.example`, `.github/`, `docs/`, `cmd/`; no
Dockerfile, compose, Kubernetes, or deployment manifest; sibling frontend
checkout has none either). Per Issue #27's acceptance criteria, the
documented CI environment therefore serves as the budget target:

| Property | Value |
| --- | --- |
| Environment | GitHub Actions standard `ubuntu-latest` runner (this is a private repository) |
| CPU | 2 vCPU, x86_64 |
| Memory | 8 GB RAM |
| Disk | 14 GB SSD |
| OS | Ubuntu 24.04 LTS image (fresh VM per job) |

Hardware per GitHub's published hosted-runners reference
(docs.github.com/en/actions/reference/runners/github-hosted-runners);
rationale and limitations recorded in
`docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md`.
`ubuntu-latest` is an alias that may migrate; the exact image of the CI run is
recorded in that run's "Set up job" log and is captured as evidence below.

## Acceptance Budget

Written verbatim in the test and decision doc, not implied:

- median ≤ **250 ms**
- p95 ≤ **300 ms** (250 ms + 20% tolerance)

Measured at the production seam: `VerifyPassword` (as called by production
login at `auth_service.go:89`) against an Argon2id hash produced with the
exact production parameters `m=65536,t=3,p=1`. Collection method: one
discarded warm-up sample, then 20 measured sequential in-process samples
(no `t.Parallel`); median is the mean of the two middle sorted samples; p95
uses the nearest-rank method (sample 19 of 20 sorted). Fixed non-secret
sample input `controlhub-argon2id-budget-sample`; neither input nor hash is
ever logged.

## RED To GREEN

- **RED:** with the budget constants temporarily set to an impossible 1 ms
  budget, `go test ./internal/service -run '^TestArgon2idVerificationBudget$'
  -count=1 -v` exited non-zero and reported the measured statistics:
  `result=FAIL samples=20 median=99.037375ms p95=100.48875ms
  min=98.009458ms max=101.232375ms budget_median<=1ms budget_p95<=1ms` —
  the fail-loud assertion triggers on a real breach without touching the
  Argon2id parameters. Raw: `/tmp/issue27-preflight/RED-run.txt` (task-local,
  not committed).
- **GREEN:** with the real thresholds restored, the same command passed:
  `result=PASS samples=20 median=99.888437ms p95=103.143708ms
  min=97.917417ms max=109.768334ms budget_median<=250ms budget_p95<=300ms`.
- **Fail-loud through the Make target:** `make argon2id-budget` under the
  impossible budget exited `2` (pipefail propagates the failing `go test`),
  and `make argon2id-budget` under the real budget exited `0` and wrote
  `.argon2id-budget/raw-output.txt`.

## Benchmark Command And Result Artifact

Exact command (local and CI):

```bash
make argon2id-budget
# equivalently:
go test ./internal/service -run '^TestArgon2idVerificationBudget$' -count=1 -v
```

Raw artifact: `.argon2id-budget/raw-output.txt` (gitignored), uploaded in CI
as the `argon2id-budget-evidence` artifact (7-day retention, matching the
existing schemathesis-reports convention).

**Local sanity reference (Apple M4 Pro — NOT acceptance evidence, per the
issue's out-of-scope note; developer hardware may only serve as an
upper-bound sanity reference):**

```
argon2id-budget: result=PASS samples=20 median=99.427771ms p95=101.527167ms min=98.064625ms max=102.313417ms budget_median<=250ms budget_p95<=300ms
argon2id-budget: environment goos=darwin goarch=arm64 ncpu=12 image_os=
```

**Acceptance measurement (documented target — GitHub Actions `ubuntu-latest`
runner):** pending. The acceptance numbers for the documented target
environment must come from the CI gate, which requires pushing the task
branch; push is not authorized at the time of writing. The CI run link, exact
runner image (from the "Set up job" log), and resulting median/p95 will be
recorded here once the branch is pushed and the workflow runs.

## Candidate Gates (task worktree, base `2e864b08`)

| Command | Result |
| --- | --- |
| `git diff --check 2e864b08...` | PASS, exit 0 |
| `go test -count=1 ./...` | PASS; 1730 tests in 14 packages, no failures |
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `make openapi-validate` | PASS; `TestOpenAPIYAMLIsValid` |
| `make argon2id-budget` | PASS (sanity reference above); exit 0; artifact written |
| `/Users/fan/.codex/skills/check-three-level-doc/scripts/check_three_level_doc.sh` | `[three-level-doc] OK` |

The three-level-doc L1 reminder was reviewed: module `internal/service`
README changed, but root `README.md` does not require an update because no
routes, handlers, or `Dependencies` changed (same reasoning as the
38X-2F release). The new test file is `gofmt`-clean.

## Independent Standards And Spec Review

Two fresh-context, read-only reviewer runs independently inspected the
changes against base `2e864b08cbb33df33b12f8f70bc71dd3929e067b`:

- **Standards verdict:** PASS; P1 `0`, P2 `0`. P3 judgement calls: the
  breach condition is evaluated twice (harmless), and `nearestRankIndex` is
  used once (defensible — the decision doc cites the nearest-rank method by
  name).
- **Spec verdict:** PASS; P1 `0`, P2 `1`, P3 `1`. The P2 was the absence of
  this evidence document, resolved by this commit. The P3 notes that
  `ubuntu-latest` is unpinned; the decision doc records the exact image per
  run instead, which the issue's CI reference (`backend-ci.yml`) permits.

## Root WIP Preservation

Before fetch or repository mutation, root was on `main` at
`2e864b08cbb33df33b12f8f70bc71dd3929e067b` with seven registered worktrees.
The root WIP manifest was captured to `/tmp/issue27-preflight/`
(`origin-main.txt`, `root-status.txt`, `worktrees.txt`, `root-wip.patch`,
`root-untracked.txt`) before any change and is unchanged afterwards:

| Status | Path |
| --- | --- |
| modified | `CLAUDE.md` |
| modified | `advisor-plans/README.md` |
| untracked | `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md` |
| untracked | `docs/agents/domain.md`, `docs/agents/issue-tracker.md`, `docs/agents/triage-labels.md` |
| untracked | `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/decisions/2026-08-09-operator-session-boundary.md` |
| untracked | `docs/superpowers/plans/2026-08-04-phase-38w-...`, `docs/superpowers/specs/2026-08-04-phase-38w-...`, `docs/superpowers/specs/2026-08-09-phase-38x-...` |

None of these paths overlaps the product diff. Existing root services,
Docker containers, fixtures, historical worktrees, and branches were not
started, stopped, reset, stashed, cleaned, relocated, or deleted. The old
blocked Issue #25 candidate worktrees and branches
(`issue-25-release-verification-20260814-004338`,
`issue-25-reverify-20260814-100748`) were not touched.

## Cleanup And Issue Safety

- Task-created temporary files under `/tmp/issue27-preflight/` are
  task-local evidence; `.argon2id-budget/` in the worktree is gitignored and
  remains as the raw artifact.
- Issues #27, #25, #20, and #7 were all open at preflight and remain open;
  no issue was commented on, closed, relabeled, or otherwise modified. After
  this proof lands, Issue #25's independent re-verification runs against the
  published refs; #20 and #7 remain open until that passes.
- The task worktree and branch are retained for the required post-evidence
  independent verification; removal requires separate authorization.
