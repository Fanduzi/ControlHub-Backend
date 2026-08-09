# Phase 38X-1A Authorization Version Release Evidence

Date: 2026-08-09
Issue: #12, `38X-1A: Version backend authorization state`
Parent: #7 (left open)

## Candidates

| Item | Backend |
|---|---|
| Base SHA (`origin/main` before merge) | `1115e1aff808c3a5482e64b628ce853fda087c08` |
| Candidate branch | `issue-12-authz-version-20260809-074637` |
| Candidate / merged SHA | `83e880427521a24b0055bc548fd492300774b64b` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-12-20260809-074637` |
| Candidate commits | `0238261` (feature), `2f642b6` (assertAuthorized 200), `83e8804` (gofmt alignment) |

Backend-only delivery. No frontend repository change.

## Merge And Push

- Merge type: fast-forward only (`git merge --ff-only issue-12-authz-version-20260809-074637`) in backend root.
- Push range: `1115e1a..83e8804` → `origin/main` = `83e880427521a24b0055bc548fd492300774b64b`.
- Local `main` == `origin/main` == merged SHA after push. No force push.

## Root Dirty-Path Whitelist And Preservation

Preserved byte-for-byte before, during, and after merge/push (identical `git status --porcelain` shape).

- Backend root whitelist: `M CLAUDE.md`, `M advisor-plans/README.md`; untracked `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/`, `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/decisions/2026-08-09-operator-session-boundary.md`, `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`, `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`, `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`.
- None of the whitelisted paths are in the candidate diff; the fast-forward merge preserved them exactly.

## Candidate Gates (exact candidate SHA `83e8804`)

| Command | Result |
|---|---|
| `git diff --check origin/main...HEAD` | PASS |
| `gofmt -l` on changed `*.go` | PASS (empty) |
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `go test -count=1 ./...` | PASS: **1429** tests, 0 failed, 10 packages |
| `make openapi-validate` | PASS (`TestOpenAPIYAMLIsValid`) |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | PASS: **206** tests (includes 5 `TestAuthorizationVersion_*`) |
| `make test-openapi-fuzz` | PASS: Schemathesis all checks passed; **2089** generated, **2089** passed; junit/report green |

## Merged-Root Gates (after FF merge at root `83e8804`)

| Command | Result |
|---|---|
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS: 1429 tests |
| `make openapi-validate` | PASS |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | PASS: 206 tests |
| `make test-openapi-fuzz` | PASS |

## Review

- Initial implement review: Standards/Spec axes; P2s fixed before first commit (mutator error taxonomy, memory/MySQL role parity, model L2 README, legacy role-embedded token test).
- Human re-review P2: `assertAuthorized` only rejected 401 — fixed in `2f642b6` to require HTTP 200.
- Final re-review: **P1 0 · P2 0**. Remaining notes are accepted P3 only.

## CI

- Backend: https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31289368620
- headSha: `83e880427521a24b0055bc548fd492300774b64b`
- Workflow: **Backend CI** — conclusion **success**
- Required jobs (from run): `release-local-gates` success, `release-docker-gates` success (see run jobs list)

## Authorization Behavior Delivered

- Backend Bearer claim: `id:authorizationVersion:issuedAt` (role not embedded).
- `VerifyToken` loads current `is_active`, `authorization_version`, and role from durable user state.
- Role change / disablement / password reset bump version; prior credentials fail with generic 401 on next protected request.
- Valid identity without required role remains distinct 403.
- Fixed eight-hour governed-query freshness unchanged.
- No user-management UI, no #13 route matrix, no Console BFF (#14/#15).

## Cleanup And Preserved State

- Task worktree intentionally preserved: `/Users/fan/GolangProjects/ControlHub-wt-issue-12-20260809-074637` @ `83e8804` on `issue-12-authz-version-20260809-074637`.
- Root WIP whitelist unchanged. No unrelated worktrees/branches/services deleted.
- Historical Phase 38R–38W evidence files not modified.

## Ticket

Issue #12 closed after this evidence is committed and the independent verifier confirms `merged/pushed/CI green`. Parent #7 remains open.
