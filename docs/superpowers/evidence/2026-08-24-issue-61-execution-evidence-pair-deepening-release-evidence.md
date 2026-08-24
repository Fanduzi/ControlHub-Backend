# Issue #61 — Execution Evidence Pair Persistence Deepening Release Evidence

Date: 2026-08-24

## Summary

Issue #61 deepens the existing Execution Evidence Pair persistence seam. All
terminal outcomes after a template execution target is resolved now persist
through the shared evidence helper. Request and actor validation failures remain
rejected without evidence; internal target-read failures persist failed evidence.
Persisted evidence contains no template ID, SQL text, parameter name, or parameter
value. Persistence remains fail closed and the external HTTP contract is unchanged.

## Refs

| Item | Value |
|------|-------|
| Repository | `Fanduzi/ControlHub-Backend` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/61 |
| Base (`origin/main` before delivery) | `dfdeb19ab3e56a561c376ccfb9e90c2d179cbc09` |
| Candidate product SHA | `075427e8073d0f8372281eabaf9330a62489fe21` |
| Candidate branch | `issue-61-delivery-20260824` |
| Candidate worktree | `/private/tmp/controlhub-issue-61-delivery-20260824-yqjHmr` |
| Candidate push | Normal push to the candidate branch; no force |
| Publication method | Fast-forward-only from the recorded base through this evidence commit, followed by a normal push to `main`; no force |
| Candidate CI | [Backend CI 32678370016](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32678370016) |

## Product Commits

| SHA | Message |
|-----|---------|
| `cde66ce2b293c945c70c538d01b2b9751dd1b1cd` | `refactor(evidence): deepen pair persistence (#61)` |
| `075427e8073d0f8372281eabaf9330a62489fe21` | `fix(evidence): close review gaps (#61)` |

Both commits were replayed onto the current `origin/main`. The replay resolved
overlap with Issue #48 by preserving its disclosure-error behavior together with
Issue #61 evidence behavior. Neither commit has an AI `Co-Authored-By` trailer.

## Changed Product Files

```text
docs/decisions/2026-08-18-phase-38x-3b-evidence-persistence-window.md
internal/service/README.md
internal/service/query_execution_service.go
internal/service/query_template_execution_service.go
internal/service/query_template_execution_service_test.go
```

Product diff `dfdeb19...075427e`: 5 files, 184 insertions, 54 deletions.
`git diff --check` and `gofmt -l` were clean. The three-level documentation
loop reported `OK`; the root README did not require an architecture-map change.

## Local Candidate Gates

All commands ran in the clean candidate worktree at exact product SHA
`075427e8073d0f8372281eabaf9330a62489fe21`.

| Gate | Result |
|------|--------|
| `make release-local-gates` | PASS: `go test -count=1 ./...`, `go vet ./...`, `go build ./...`, OpenAPI YAML validation |
| `go test -json -count=1 ./...` | 1831 passed, 0 failed, 0 skipped, 0 package failures |
| `go test -json -race -count=1 ./...` | 1831 passed, 0 failed, 0 skipped, 0 package failures |
| `make argon2id-budget` | PASS: 20 samples; median 97.980104 ms, p95 99.3345 ms; limits 250/300 ms |
| `make release-docker-gates` | PASS |
| `go test -tags=integration -count=1 -run '^Test' -skip '^TestOpenAPIFuzz$' ./internal/integration` | 389 passed, 0 failed, 0 skipped, 0 package failures |
| `go test -tags=integration -count=1 -run '^TestOpenAPIFuzz$' ./internal/integration` | 1 passed, 0 failed, 0 skipped, 0 package failures |

This is a backend-only change, so no browser UI E2E applies. The Docker release
gate is the end-to-end database boundary: it ran from the candidate worktree and
candidate SHA against an ephemeral Testcontainers MySQL 8 instance.

## Candidate CI

[Backend CI 32678370016](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32678370016)
completed successfully for exact `headSha`
`075427e8073d0f8372281eabaf9330a62489fe21`.

| Required job | Job ID | Result |
|--------------|--------|--------|
| `release-local-gates` | `97290543063` | SUCCESS (1m20s) |
| `release-docker-gates` | `97290542896` | SUCCESS (2m53s) |

The Node.js runtime deprecation annotation was non-failing and did not skip a
required job.

## Independent Review

Two read-only reviewers inspected `dfdeb19...075427e` after conflict resolution;
neither edited files.

| Axis | Verdict | P1 | P2 | Verification |
|------|---------|----|----|--------------|
| Standards | PASS | 0 | 0 | `go test ./internal/service` passed |
| Issue #61 specification | APPROVE | 0 | 0 | `go test ./internal/service ./internal/api` passed with no skips |

The specification review confirmed all terminal outcomes after target resolution
persist evidence, validation remains rejected without evidence, internal target
read failures become failed evidence, sensitive query/template/parameter material
is absent, persistence fails closed, and Issue #48 HTTP behavior is preserved.

## Root WIP Preservation

The root worktree `/Users/fan/GolangProjects/ControlHub` remained at
`5ea82550035113144c790cee304352d75fecdcc0`. No stash, reset, clean, rebase,
amend, overwrite, force push, tag, or deploy was used. The following user-owned
dirty files were allowlisted and retained with their pre-delivery SHA-256 values:

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

The delivery worktree, local delivery branch, remote candidate branch, and its
ignored `.argon2id-budget/` output are intentionally retained until the `main`
push, final CI, independent closure verification, and Issue #61 closure finish.
No other worktree or branch is a cleanup target.

## Published Main Attestation

Backend `main` was pushed normally, without force, from
`dfdeb19ab3e56a561c376ccfb9e90c2d179cbc09` to the merged product-and-evidence
body SHA `f4f7efa3f3d5ab554c437f1d93605b205acf9d0b`.

[Backend CI 32678870332](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32678870332)
completed successfully for exact `headSha`
`f4f7efa3f3d5ab554c437f1d93605b205acf9d0b`.

| Required job | Job ID | Result |
|--------------|--------|--------|
| `release-local-gates` | `97291868220` | SUCCESS (1m19s) |
| `release-docker-gates` | `97291868141` | SUCCESS (2m16s) |

This attestation update is a separate docs-only closure commit and intentionally
does not name its own commit SHA. The independent verifier records that final
evidence-commit SHA and confirms its exact CI run in the Issue #61 closing
comment, avoiding a self-referential SHA/documentation loop.
