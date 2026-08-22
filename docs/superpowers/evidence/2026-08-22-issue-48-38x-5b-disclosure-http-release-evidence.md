# Issue #48 — 38X-5B Execute-path Disclosure Publishes query_result_disclosure_blocked Release Evidence

Date: 2026-08-22

## Summary

Issue #48 `38X-5B: Execute-path disclosure publishes query_result_disclosure_blocked`
is a backend product delivery. Governed execute and related-record HTTP
responses that mean disclosure blocked publish Controlled Error Code
`query_result_disclosure_blocked`. They no longer collapse that outcome to
`query_not_allowed` on the wire. Target-not-enabled still returns
`query_not_allowed`. Policy-admin list already published the disclosure code
and now asserts the JSON field, not only HTTP 403.

The two product commits were already fast-forwarded to backend `origin/main`
on 2026-08-21. This closure lands the Apply-path exclusive-sentinel tests plus
this tracked evidence. Parent `Fanduzi/ControlHub-Backend#11` stays open.
Query Workbench feature remaps remain Issue #52. Closed enum / checker remain
Issue #53.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Backend` |
| Backend base before product (`origin/main` before #48) | `518f79121fd2315bd46befc6eb718b9042579871` |
| Product SHA 1 | `efd6ea0a7ff518be689403a5db5a3eb09c033c87` — `fix(query): publish query_result_disclosure_blocked on execute-path HTTP (issue #48)` |
| Product SHA 2 / product tip | `85bb8e9537929aa4be6deb979f0109267244e873` — `fix(query): return ErrQueryDisclosureBlocked from Apply-path classify (issue #48)` |
| Product branch | `issue-48-38x-5b-disclosure-http-20260821221533` |
| Product worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-48-38x-5b` (HEAD `85bb8e9`, porcelain empty) |
| Product push | Fast-forward `518f791..85bb8e9` (2 commits) onto `main`; normal push, no force (already on `origin/main` before this closure) |
| Backend `origin/main` at closure start | `db3e35fe0a38df2e6abc6a919156b2295b9a1ea5` (`85bb8e9` is an ancestor; later commits are #53 then #47 evidence) |
| P2 test candidate SHA | `ec8941a1929b56525279fca3379f832bdcedd963` — `test(query): lock Apply-path disclosure sentinel exclusive of not-allowed (#48)` |
| P2 / evidence candidate branch | `issue-48-p2-apply-tests-20260822` |
| P2 / evidence candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-48-p2-tests-20260822` |
| Evidence worktree | `/private/tmp/controlhub-evidence-48-20260822` |
| Evidence branch | `issue-48-publication-evidence-20260822` |
| Backend evidence body SHA | `af2a7c6b84260134df5ec478cb81e7cc146962bd` |
| Backend evidence push | Fast-forward `db3e35f..af2a7c6` (2 commits: P2 tests + this evidence body) as `af2a7c6:main`; normal push, no force |
| Backend `origin/main` after evidence body push | `af2a7c6b84260134df5ec478cb81e7cc146962bd` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/48 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (OPEN) |

## Product Commits (already on `origin/main`, range `518f791..85bb8e9`)

| SHA | Message |
|-----|---------|
| `efd6ea0a7ff518be689403a5db5a3eb09c033c87` | `fix(query): publish query_result_disclosure_blocked on execute-path HTTP (issue #48)` |
| `85bb8e9537929aa4be6deb979f0109267244e873` | `fix(query): return ErrQueryDisclosureBlocked from Apply-path classify (issue #48)` |

Author on both: `Fan() <18501341937@163.com>`. No AI `Co-Authored-By` trailer.

Changed files (`518f791..85bb8e9`):

```
internal/api/README.md
internal/api/navigate_related_records_handler_test.go
internal/api/query_execution_handler.go
internal/api/query_execution_handler_test.go
internal/service/README.md
internal/service/navigate_related_records_test.go
internal/service/query_execution_service.go
internal/service/query_execution_service_test.go
internal/service/query_template_execution_service_test.go
```

`git show --stat efd6ea0`: 9 files, 97 insertions, 18 deletions.
`git show --stat 85bb8e9`: 7 files, 321 insertions, 17 deletions.
`git diff --check 518f791...85bb8e9` is clean.

Production seams:

- `writeQueryExecutionError` and `writeNavigationError` match
  `ErrQueryDisclosureBlocked` **before** `ErrQueryNotAllowed` and write HTTP
  JSON `error=query_result_disclosure_blocked`.
- Execute Preflight rejections wrap both sentinels (`fmt.Errorf("%w: %w",
  ErrQueryNotAllowed, err)` where `err` is `ErrQueryDisclosureBlocked`); the
  handler match order is what publishes the disclosure code.
- `classifyExecutorError` returns `ErrQueryDisclosureBlocked` (not
  `ErrQueryNotAllowed`) for Apply-path policy blocks after a successful
  executor run. Evidence code stays `query_result_disclosure_blocked`.
- Client cancellation remains `query_canceled` in evidence with HTTP sentinel
  `ErrQueryBackendFailure` / `query_backend_error`. That collapse is out of
  scope.

## P2 Test Commit (unpushed at evidence write, range `db3e35f..ec8941a`)

| SHA | Message |
|-----|---------|
| `ec8941a1929b56525279fca3379f832bdcedd963` | `test(query): lock Apply-path disclosure sentinel exclusive of not-allowed (#48)` |

Author `Fan() <18501341937@163.com>`. No AI `Co-Authored-By` trailer.
`git diff --stat origin/main...HEAD` at evidence write: 6 files, 67 insertions,
18 deletions. `git diff --check origin/main...HEAD` is clean.

| File | Change |
|------|--------|
| `internal/api/query_disclosure_handler_test.go` | List blocked-by-policy asserts JSON `error=query_result_disclosure_blocked` |
| `internal/api/query_saved_statement_execution_handler_test.go` | Saved-statement execute disclosure asserts the same JSON code |
| `internal/service/query_execution_service_test.go` | `assertApplyPathDisclosureHTTPSentinel` fails if Apply also wraps `ErrQueryNotAllowed` |
| `internal/service/navigate_related_records_test.go` | Related-record Apply uses the same exclusive helper |
| `internal/api/README.md` | L2 rows for list and template-execute JSON code |
| `internal/service/README.md` | L2 rows for exclusive Apply sentinel |

Three-level-doc on `origin/main...ec8941a`: every changed Go file has `Package`
plus `input`/`output`/`pos`/`note`; both module README files are in the change
set.

## Local Candidate Gates (`ec8941a`, worktree `/Users/fan/GolangProjects/ControlHub-wt-issue-48-p2-tests-20260822`)

All commands ran from that worktree at exact `HEAD`
`ec8941a1929b56525279fca3379f832bdcedd963` (`echo SHA=$(git rev-parse HEAD)` at
gate start). Go `go1.26.2 darwin/arm64`. Candidate porcelain empty. Root was
not used. No process was killed. No test was made green by skip, mock, timeout
change, or weakened assertion.

| Gate | Result |
|------|--------|
| `git diff --check origin/main...HEAD` | clean |
| `make release-local-gates` (`go test -count=1 ./...`, `go vet ./...`, `go build ./...`, `make openapi-validate`) | PASS, exit 0. Packages: cmd/bootstrap-admin, cmd/cutover-local, cmd/e2e-fixture-bootstrap, cmd/querydev, cmd/server, internal/api, internal/config, internal/cutover, internal/integration, internal/model, internal/openapi, internal/repository/mysql, internal/service all `ok`. `internal/testsupport/operatoraccess` has no test files (pre-existing). `TestOpenAPIYAMLIsValid` PASS |
| Recount `go test -count=1 -json ./...` at same SHA | **1827** passed, **0** failed, **0** skipped |
| `make argon2id-budget` | PASS — `TestArgon2idVerificationBudget` samples=20 median=97.016916ms p95=98.342542ms min=96.28ms max=98.454875ms; budgets median<=250ms p95<=300ms |
| `make release-docker-gates` | PASS, exit 0 (see E2E / Docker section) |

## E2E / Docker-backed gates (backend equivalent; no Playwright in #48)

Issue #48 is backend HTTP emit only. Frontend Chromium / `release:e2e` is out
of scope (38X-5C is Issue #52). Required Docker-backed gates ran locally from
the candidate worktree.

Serving CWD: `/Users/fan/GolangProjects/ControlHub-wt-issue-48-p2-tests-20260822`
Serving SHA: `ec8941a1929b56525279fca3379f832bdcedd963`

Command: `make release-docker-gates` which is `make test-integration` then
`make test-openapi-fuzz`.

`make test-integration` is
`go test -tags=integration -count=1 -v -run '^Test' -skip '^TestOpenAPIFuzz$' ./internal/integration`.
The `-skip '^TestOpenAPIFuzz$'` split is the documented Makefile contract
(`TestOpenAPIFuzzExclusionContract` PASS); fuzz runs as the next target, not a
skip to obtain green.

| Command | Passed | Failed | Skipped |
|---------|--------|--------|---------|
| `make test-integration` + `make test-openapi-fuzz` (`--- PASS:` lines in the docker section of the gate log) | 239 | 0 | 0 (`--- SKIP:` count 0) |
| `TestOpenAPIFuzz` (Schemathesis v4.15.2, seed 42, max examples 50, checks `not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance`) | 2041 generated / 2041 passed | 0 | Examples phase: 16 passed / 35 skipped (operations without examples; fuzzing then tested 51 selected / 52 total operations). Coverage and Stateful phases disabled in `scripts/schemathesis.toml` |

Fuzz base URL `http://127.0.0.1:51674`. Disposable Testcontainers MySQL; the
daily `controlhub` database was not touched. Shared query-fixture containers
were not started or stopped.

## Product CI (exact product tip SHA)

[Backend CI run 32492850046](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32492850046)
— event `push`, branch `main`, `headSha`
**`85bb8e9537929aa4be6deb979f0109267244e873`** (exact product tip; `efd6ea0`
has no separate run because both product commits were pushed as one
fast-forward and CI ran on the tip):

| Required job | Result |
|--------------|--------|
| `release-local-gates` | SUCCESS |
| `release-docker-gates` | SUCCESS |

Argon2id budget ran inside `release-local-gates` and succeeded. The Node.js
action-runtime deprecation annotations and optional Schemathesis artifact
upload did not fail or skip either required job.

`origin/main` `db3e35f` (closure start) already has later green runs for #53
and #47 evidence; those SHAs are not this product tip. Product closure uses
the exact-head green run above.

## Backend Evidence CI

[Backend CI run 32542833340](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32542833340)
completed successfully at exact evidence body SHA
`af2a7c6b84260134df5ec478cb81e7cc146962bd`:

| Required job | Result |
|--------------|--------|
| `release-local-gates` | SUCCESS (1m16s) |
| `release-docker-gates` | SUCCESS (2m19s) |

The Node.js action-runtime deprecation annotations did not fail or skip either
required job. Argon2id budget ran as part of `release-local-gates` and
succeeded. Merged-root local gates, Argon2id budget, integration, and OpenAPI
fuzz were also re-run from `/private/tmp/controlhub-evidence-48-20260822` at
the same SHA before push; fuzz served `http://127.0.0.1:58606` and reported
2041 generated / 2041 passed.

## Standards / Spec Verdict

Review tool: two independent read-only `general-purpose` subagents, one
Standards axis and one Spec axis, against `518f791...85bb8e9` plus
`origin/main...ec8941a` in the clean candidate worktree. Neither agent edited
files.

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | No documented-standard breach |
| P2 | 0 | Exclusive Apply sentinel locked; L3/L2 present |
| P3 | 3 | Duplicated disclosure arm in `writeQueryExecutionError` / `writeNavigationError` (pre-existing helper split; Rule 3/11); API README still splices mapping prose into the Routes table; disclosure-handler L3 `input:` still omits `encoding/json` (pre-existing) |

Verdict: **APPROVE**. Remaining P1/P2: **0**.

### Spec

| AC | Status |
|----|--------|
| Execute and related-record disclosure blocks return HTTP JSON `error` equal to `query_result_disclosure_blocked` | PASS — mapper matches disclosure first; execute/related Preflight and Apply handler tests assert `body.Error` |
| Target-not-enabled / not-allowed still returns `query_not_allowed` | PASS — mapper second arm; `TestQueryExecution_Execute_DisabledTarget`; `TestNavigateRelatedRecords_NotAllowed` |
| Policy-admin endpoints that already emit `query_result_disclosure_blocked` remain correct | PASS — `writeDisclosureError`; P2 `TestDisclosure_ListBlockedByPolicy` asserts JSON `error` |
| Handler tests assert the JSON code, not only HTTP 403 | PASS — execute, related, saved-statement execute, and policy list unmarshal `error` |
| Other sentinel-to-HTTP collapses out of scope | PASS — canceled still `query_canceled` evidence + HTTP `query_backend_error` |

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | — |
| P2 | 0 | — |
| P3 | 1 | Not-allowed tests still `strings.Contains` rather than exact `error` (pre-existing) |

Verdict: **APPROVE**. Remaining P1/P2: **0**.

## Root WIP Preservation

Dirty-path SHA-256 manifests were taken before candidate gates, reviews, and
this evidence commit. No stash, reset, clean, relocation, overwrite, rebase,
amend, force push, tag, or deploy was used.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

Root `HEAD` remains `44474afa8febbff49c3510bbd43cb1b30f9441a0` (behind
`origin/main`). It was not fast-forwarded. Allowed preserved dirty paths
(hashes at preflight; identical to the Issue #47 whitelist):

| Path | SHA-256 |
|------|---------|
| `CLAUDE.md` | `892f9fdfa81316d9ff46cab5d4818951a31cd0e7bf4a915df761199b8fa99f7c` |
| `CONTEXT.md` | `0f915b7255d2e2095f9990f7516c96164b8114c3547e5791d7d2fe4d498caffa` |
| `advisor-plans/README.md` | `394df5618d29ade2c0b955cc7234dcf3344a81494509db890a03667797b42280` |
| `AGENTS.md.bak-pre-gitnexus-uninstall` | `bb68496196cacbc25643c806585d5889e2824364bb6200847b81d8f9b6a162ae` |
| `CLAUDE.md.bak-pre-gitnexus-uninstall` | `3bc44e26146d21862b0e2c37b287df743a8c9ff8b31aae3ae9a0b3c6b87569e8` |
| `CONTEXT.md.bak-pre-issue-41` | `9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c` |
| `docs/agents/domain.md` | `f358f97ebc4224a56f89fb342b3588ccc114899af469f3cbdedf35e2023b3d95` |
| `docs/agents/issue-tracker.md` | `decae4b541d382f2fe9c7c9f49617b405f1641cbd27b53b3137f3d8118164cfc` |
| `docs/agents/triage-labels.md` | `f672681495c9eef1db104f661ab0c3c87e73cde396b332a947e7da4551c21f34` |
| `docs/decisions/2026-08-04-parameter-value-evidence-retention.md` | `cbad5c1377e3d1fd962e6f00ae72a3743029faa8c53edbd383992ab62e729a89` |
| `docs/decisions/2026-08-09-operator-session-boundary.md` | `008a69e51c241bb14d0dedd3764df018a71e0c2be12eaab230bdda27383418d9` |
| `docs/decisions/2026-08-21-phase-38x-5-controlled-error-code-contract.md` | `15886c31b813f09796609d8777261a670eaf612fbd3eb5d5ff1b61a597fca609` |
| `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md` | `c2ced9487597793a0739fcc0368802a61bc2ce25d8bde6f9791a76d02edef869` |
| `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md` | `e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb` |
| `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` | `dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21` |
| `docs/superpowers/specs/2026-08-21-phase-38x-5-controlled-error-code-and-release-graph.md` | `6419566d44fecdd13437a4901beb210405613a4e53c982552c2a53ba3b4e6aae` |

## Cleanup

- Product worktree `/Users/fan/GolangProjects/ControlHub-wt-issue-48-38x-5b` and
  branch `issue-48-38x-5b-disclosure-http-20260821221533` are retained
- P2 / evidence worktree `/Users/fan/GolangProjects/ControlHub-wt-issue-48-p2-tests-20260822`
  and branch `issue-48-p2-apply-tests-20260822` are retained until the
  independent verifier confirms push and CI
- No unrelated worktree, branch, container, service, or user file was removed
- Shared Docker query-fixture containers and root listeners were not touched
- Root dirty paths listed above were not modified by this closure
