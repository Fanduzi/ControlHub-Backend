# Phase 38X-1B Operator Boundary Release Evidence (Release-Closure Governance)

Date: 2026-08-11
Issues: #18 (this delivery, `38X-1B: Complete release-closure evidence and Schemathesis exclusion governance`); #13 (released code under closure); parent #7 (left open)
Released code under closure: `70607363f57a6643b26a28b930e675e25a41bd14`

## Purpose

#13's code was already published at `7060736` with green CI before this task.
This delivery completes the missing closure evidence and formalizes Schemathesis
exclusion governance. It does not rework, roll back, or re-implement the
released authorization-boundary code.

## Released Code Acceptance Snapshot (`7060736`)

- Final commit of the #13 delivery: `fix(disclosure): map policy write errors`
  (11 files, +166/−16; disclosure policy-write error mapping and README/openapi
  sync).
- Acceptance contract: #13's criteria — anonymous access limited to
  health/login/published API docs; editors read Inventory and use existing
  governed query capabilities; only admins mutate Inventory or read audit
  events; router behavior, OpenAPI security declarations, and generated-client
  contract agree; API/integration/audit assertions cover the matrix.
- CI provenance (verified via `gh run view 31404587885`):
  - https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31404587885
  - headSha: `70607363f57a6643b26a28b930e675e25a41bd14`, conclusion: **success**
  - Jobs: `release-local-gates` success, `release-docker-gates` success.

## Schemathesis Exclusion Contract (the governance fix)

One audited exclusion exists; it is narrow to a single operation:

| Operation | Path / method | Reason | Stable-fixture gap | Dedicated coverage | Scope |
|---|---|---|---|---|---|
| `executeSavedStatement` | `POST /query-targets/{id}/saved-statements/{statementId}/execute` | Governed template execution requires a real reachable query target (env-resolved credential + matching schema) plus a stored statement with typed parameter declarations | The disposable fuzz DB seeds no query target whose DSN/credential resolves in the fuzz server env, so generated requests deterministically fail pre-execution (404/403), exercising the error envelope rather than the contract; a request that did reach a target would execute arbitrary generated SQL against it, which the disposable harness cannot admit | Service: `internal/service/query_template_execution_service_test.go` (12 `TestExecuteSavedStatement*`); integration (real MySQL): `internal/integration/query_template_execution_test.go` (6 `TestExecuteSavedStatementIntegration*`); API handler: `internal/api/query_saved_statement_execution_handler_test.go` (8 `TestTemplateExecute_*`) | Single operation only |

- Enforced by `TestOpenAPIFuzzExclusionContract`
  (`internal/integration/openapi_fuzz_contract_test.go`, runs in every
  `go test ./...`): every exclusion is a single-operation
  `--exclude-operation-id` flag in `openapi-fuzz.sh` within the canonical
  allowlist, broad path/method/tag flags are rejected (negative-verified), no
  exclusion directives in `schemathesis.toml`, and the contract section in
  `scripts/README.md` is present.
- `schemathesis.toml` carries only valid-parameter overrides
  (`createResourceRelation`, `patchResource`) — not exclusions.
- Run-scoping choices `--phases examples,fuzzing` and `[warnings] fail-on = []`
  are pre-existing, are not operation exclusions, and are recorded in the
  contract for audit completeness.

## Candidates

| Item | Value |
|---|---|
| Base (`origin/main` before candidate) | `70607363f57a6643b26a28b930e675e25a41bd14` |
| Candidate branch | `issue-13-closure-evidence-20260811` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-13-closure-20260811` |
| Candidate commits (implementation) | `7a2b4e6` (contract + drift-guard), `25fcc70` (review findings), `da61371` (restore broad-exclusion guard) |
| Candidate changed files | `internal/integration/openapi_fuzz_contract_test.go` (new), `scripts/README.md`, `scripts/openapi-fuzz.sh`, `internal/integration/README.md` (+124 lines) |

## Candidate Gates (exact candidate HEAD `da61371`, host macOS arm64, Go 1.26.2, schemathesis 4.15.2)

| Command | Result |
|---|---|
| `git diff --check 7060736...HEAD` | PASS |
| `gofmt -d` on changed `*.go` | PASS (empty) |
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `go test -count=1 ./...` | PASS: **1586** tests, 0 failed, 13 packages |
| `make openapi-validate` | PASS (`TestOpenAPIYAMLIsValid`) |
| `make test-integration` | PASS: **199** tests, 0 failed, 0 skipped |
| `make test-openapi-fuzz` | PASS: Schemathesis **all checks passed**, **2038 generated, 2038 passed** |
| `go test -race -count=1 ./...` | PASS: 1586 tests, 13 packages |

Verification history: two earlier fuzz-gate runs failed during implementation
(exit 127) because in-development comment placement broke the Schemathesis
argument list (a comment inside a backslash continuation, then a broken
continuation chain); both were code bugs in the WIP script, not Testcontainers
startup flakes, and were fixed before the recorded runs above.

## Independent Read-Only Review

Three independent read-only reviews were run as fresh pi RPC sub-processes
(separate model instances, no shared session context, read-only by
instruction) over `7060736...HEAD`, plus a round-2 confirmation pass after
fixes. No human re-review was performed; verdicts are the reviewers' actual
outputs.

| Axis | Verdict | Findings |
|---|---|---|
| Standards (round 1) | APPROVE | P1 0 · P2 2 (README overclaimed mechanical enforcement of row completeness; duplicated exclusion matching) — both fixed in `25fcc70` |
| Standards (round 2, confirmation) | APPROVE | P1 0 · P2 0; P3 2 (one confirmed and fixed in `da61371`: broad-flag scan had been dropped in the refactor; one cosmetic numbering note fixed) |
| Spec | APPROVE | P1 0 · P2 0; P3 1 (run-scoping skips ungoverned — recorded in the contract) |
| Security | APPROVE | P1 0 · P2 0; P3 3 (all accepted: P3 argv-token exposure correctly untouched, descriptive wording only, no-build-tag contract test documented) |

Final review state: **P1 0 · P2 0**.

## P3 Residual Risk (accepted, deferred, not silently fixed)

The Schemathesis bearer token is passed as
`--header "Authorization: Bearer ${CONTROLHUB_FUZZ_BEARER_TOKEN}"` in
`scripts/openapi-fuzz.sh`, making the token visible in the process argv
(e.g. `ps`) while a fuzz run is live. This is an accepted P3: it is
test/CI-only, token lifetime is bounded, and the harness token is
throwaway. This delivery deliberately does **not** change the behavior; the
risk is recorded here as deferred residual risk (a future change may move the
token to stdin/a config file, outside this task's scope).

## Root Dirty-Path Whitelist And Preservation

Root worktree (`~/GolangProjects/ControlHub`) preserved byte-for-byte before,
during, and after candidate work (verified identical `git status --porcelain`
shape and per-file SHA-256 at preflight and at evidence time):

- `M CLAUDE.md`, `M advisor-plans/README.md`
- Untracked: `AGENTS.md.bak-pre-gitnexus-uninstall`,
  `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/`,
  `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`,
  `docs/decisions/2026-08-09-operator-session-boundary.md`,
  `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`,
  `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`,
  `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`

None of the whitelisted paths are in the candidate diff. Historical evidence
and 38X specs (including the untracked 38X spec above) were not modified.

## Preserved State

- Unrelated worktrees preserved: `ControlHub-wt-issue-12-20260809-074637`
  (`83e8804`), `ControlHub-wt-issue-13-20260809-121228` (`f086b67`).
- Listening services (ControlCenter, MySQL, Docker, query/editor processes)
  untouched; no task service was started outside disposable Testcontainers,
  which are stopped on cleanup.
