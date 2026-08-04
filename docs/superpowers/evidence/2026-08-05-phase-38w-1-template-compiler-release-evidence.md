# Phase 38W-1 Template Compiler Release Evidence

Date: 2026-08-05
Issue: #2, `38W-1: Prove governed template compiler feasibility`

## Candidate

| Item | Value |
|---|---|
| Base SHA | `8504a7196347301429730733736bcff26b8dce97` |
| Candidate branch | `phase-38w-1-template-compiler` |
| Candidate SHA | `6809255379c7eaafba8055f38dceddc5f23810d1` |
| Candidate worktree | `/tmp/controlhub-38w-1-template-compiler` |
| Candidate status | Clean |
| Candidate range | `8504a7196347301429730733736bcff26b8dce97..6809255379c7eaafba8055f38dceddc5f23810d1` |
| Candidate commits | 6 |
| Fast-forward ancestry | Candidate descends from the exact base and current `origin/main` |

## Gates

All commands ran from the candidate worktree at the candidate SHA.

| Command | Result |
|---|---|
| `git diff --check 8504a7196347301429730733736bcff26b8dce97...HEAD` | PASS, exit 0 |
| `git diff --name-only -z 8504a7196347301429730733736bcff26b8dce97...HEAD -- '*.go' \| xargs -0 -r gofmt -d` | PASS, exit 0; no formatting diff |
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `go test -count=1 ./...` | PASS, exit 0; 10 package results passed |
| `make openapi-validate` | PASS, exit 0; `TestOpenAPIYAMLIsValid` passed |
| `make test-integration` | PASS, exit 0; `internal/integration` passed with no failed or skipped tests reported |
| `make test-openapi-fuzz` | PASS, exit 0; 48/48 API operations and 2038/2038 generated cases passed; the run reported 20 authentication warnings and 3 schema-validation warnings |
| `go test -tags=integration -count=1 ./internal/integration` | PASS, exit 0; `internal/integration` passed |

## Acceptance Proof

- `TestTemplateStatementCompilerBindsValuesInSourceOrderAndPassesGuard` proves repeated named parameters become deterministic positional arguments and bound values do not enter SQL text.
- `TestTemplateStatementCompilerRejectsInvalidDeclarationsAndValues` covers missing, unknown, invalid, quoted, commented, identifier-position, list-marker, and multi-statement inputs; supplied values are absent from errors.
- `TestTemplateStatementCompilerLeavesReadOnlyDecisionsToQueryGuard` proves unsafe, locking, and side-effecting statements remain rejected by the existing guard.
- `GuardedTemplateStatement` has unexported query and argument fields; `CompileAndGuard` is the compiler-owned construction path.
- `TestMySQLQueryExecutorQueryTemplateBindsCompilerOwnedStatement` proves bound arguments use the read-only transaction and bounded scanner.
- `TestMySQLQueryExecutorQueryTemplatePreservesPaginatedPayloadCap` proves paginated payload overflow remains a controlled bounded-result rejection.
- `TestExecute_RecordsSuccessfulAttempt` proves ordinary `Execute` dispatches zero times through `QueryTemplate`.

## Review

Fresh read-only adversarial review: Oracle, exact candidate SHA `6809255379c7eaafba8055f38dceddc5f23810d1` against base `8504a7196347301429730733736bcff26b8dce97`.

Verdict: APPROVE. P1 findings: 0. P2 findings: 0. The review reported three non-blocking P3 notes concerning end-to-end compiler-to-executor coverage and header precision; no P1/P2 fix was required.

## Root Preservation

Root repository: `/Users/fan/GolangProjects/ControlHub`.

Root branch: `main`. Root HEAD and `origin/main` at pre-merge verification: `8504a7196347301429730733736bcff26b8dce97`.

Preserved root dirty paths:

- `CLAUDE.md`
- `advisor-plans/README.md`
- `CONTEXT.md`
- `docs/agents/domain.md`
- `docs/agents/issue-tracker.md`
- `docs/agents/triage-labels.md`
- `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`
- `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`
- `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`

The candidate changed only `internal/service/README.md`, `internal/service/query_execution_service.go`, `internal/service/query_execution_service_test.go`, `internal/service/query_executor.go`, `internal/service/query_executor_test.go`, `internal/service/query_template_compiler.go`, and `internal/service/query_template_compiler_test.go`. The root dirty-path set and candidate changed-path set have no overlap. Root WIP was not staged, stashed, reset, cleaned, relocated, or restored.

## CI and Cleanup State

The candidate branch has no remote ref, and `gh run list --repo Fanduzi/ControlHub-Backend --commit 6809255379c7eaafba8055f38dceddc5f23810d1 --limit 20` returned no runs. The Backend CI workflow is [`.github/workflows/backend-ci.yml`](https://github.com/Fanduzi/ControlHub-Backend/blob/main/.github/workflows/backend-ci.yml); its required jobs are `release-local-gates` and `release-docker-gates`. No candidate CI conclusion exists before the merge/push step.

At evidence-commit time, the candidate worktree and local candidate branch remained present for the authorized fast-forward, push, CI verification, independent read-only verification, and final cleanup sequence.

## Post-Merge Closure

The verified fast-forward merge and normal push range was `8504a7196347301429730733736bcff26b8dce97` to `d639ccd1ed5764eee4f41630f5decbd00c42e01c`.

Backend CI run: [30932614342](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/30932614342), head SHA `d639ccd1ed5764eee4f41630f5decbd00c42e01c`, status completed, conclusion success.

Required CI jobs:

- `release-local-gates`: [job 92070774860](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/30932614342/job/92070774860), completed, success.
- `release-docker-gates`: [job 92070774341](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/30932614342/job/92070774341), completed, success.

Cleanup receipt: `/tmp/controlhub-38w-1-template-compiler` was removed as a worktree and `phase-38w-1-template-compiler` was deleted as a local branch after CI and independent verification. The final worktree list contains only the root repository. No unrelated worktree, branch, service, container, or root WIP was changed.

The SHA of this documentation-only evidence update is intentionally omitted from this file.
