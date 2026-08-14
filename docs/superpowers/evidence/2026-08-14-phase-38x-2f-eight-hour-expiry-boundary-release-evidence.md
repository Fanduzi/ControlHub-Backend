# Phase 38X-2F Eight-Hour Expiry Boundary Release Evidence

Date: 2026-08-14
Issue: [#26, `38X-2F: Enforce the exact eight-hour credential expiry boundary`](https://github.com/Fanduzi/ControlHub-Backend/issues/26)

This backend-only release makes the fixed eight-hour bearer lifetime exclusive:
credentials are rejected once their age is eight hours or greater. It does not
perform the separate release-state verification owned by Issue #25.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and preflight `origin/main` | `664759a6b2fb1929bba913b3ec8a4210e7fb1b69` |
| Candidate branch | `fix-eight-hour-expiry-boundary-20260814` |
| Candidate, merged product, and product `origin/main` SHA | `e285aa6f6f3a738cd193fe3b74750e851e487568` |
| Candidate commit | `e285aa6 fix(auth): expire bearer credentials at eight-hour boundary` |
| Merge | Fast-forward only, `664759a..e285aa6` |
| Push | Normal push of `main`, `664759a..e285aa6`; no force, rebase, amend, tag, or deploy |

The product diff contains only:

- `internal/api/auth_middleware.go`
- `internal/api/auth_middleware_test.go`
- `internal/api/README.md`

Both bearer freshness checks now reject `age >= MaxQueryTokenAge`, the exact
boundary test covers both middleware paths and prevents either wrapped handler
from executing, and the API module documentation records the exclusive fixed
eight-hour lifetime. No Issue #25 verification work is included.

## RED To GREEN

- **RED:** In a disposable detached worktree at the candidate test revision,
  replacing only `internal/api/auth_middleware.go` with the base
  `664759a6b2fb1929bba913b3ec8a4210e7fb1b69` implementation and running
  `go test ./internal/api -run '^TestEightHourOldBearerIsExpired$' -count=1`
  exited `1`. Both subtests entered their wrapped handlers and returned `200`
  instead of the required generic `401`.
- **GREEN:** At exact product SHA
  `e285aa6f6f3a738cd193fe3b74750e851e487568`, the same command passed on the
  candidate and again from merged root.

## Candidate Gates

All commands ran against exact candidate SHA
`e285aa6f6f3a738cd193fe3b74750e851e487568`:

| Command | Result |
| --- | --- |
| `git diff --check 664759a6b2fb1929bba913b3ec8a4210e7fb1b69...HEAD` | PASS, exit 0 |
| `go test ./internal/api -run '^TestEightHourOldBearerIsExpired$' -count=1` | PASS |
| `go test ./internal/api -count=1` | PASS |
| `go test -count=1 ./...` | PASS; all listed packages passed, no failures |
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `make openapi-validate` | PASS; `TestOpenAPIYAMLIsValid` |
| `/Users/fan/.codex/skills/check-three-level-doc/scripts/check_three_level_doc.sh` | PASS, exit 0 |

The documentation checker reported no working-tree changes in the clean
candidate checkout, so the same script was also run against a disposable
base-reset snapshot containing the exact candidate content as its unstaged
diff. It reported `[three-level-doc] OK`; its L1 reminder was reviewed and the
root README did not require a change because routes, handlers, and
`Dependencies` did not change.

`internal/api/auth_middleware.go` is `gofmt`-clean. The known pre-existing
`gofmt` drift in `internal/api/auth_middleware_test.go` is confined to target
lines 534, 591, 634, and 662; candidate Go hunks are at lines 3, 62, 128, 144,
and 145. The ranges are disjoint, so changed hunks are formatted and unrelated
lines were not reformatted.

## Independent Standards And Spec Review

Two fresh-context, read-only reviewer runs independently inspected
`664759a6b2fb1929bba913b3ec8a4210e7fb1b69...e285aa6f6f3a738cd193fe3b74750e851e487568`:

- **Standards verdict:** PASS; P1 `0`, P2 `0`. One P3 judgement call noted the
  pre-existing duplicated freshness comparison in the two middleware paths;
  the release intentionally kept the surgical two-operator boundary fix.
- **Spec verdict:** PASS; P1 `0`, P2 `0`. Exact-boundary rejection, generic
  `401`, blocked handlers, younger/older coverage, shared fixed constant,
  module documentation, and absence of Issue #25 scope were all confirmed.

## Merged-Root Gates

After the fast-forward merge and normal product push, every candidate command
listed above was rerun from backend root at exact product SHA
`e285aa6f6f3a738cd193fe3b74750e851e487568`. Every command passed. The merged
documentation checker reported `[three-level-doc] OK`; the root README decision
remained unchanged for the same reason recorded above.

## Product CI

[Backend CI run 31777769335](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31777769335)
ran for exact head SHA `e285aa6f6f3a738cd193fe3b74750e851e487568`
and completed with conclusion `success`:

- [`release-local-gates`](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31777769335/job/94696721889): `success`
- [`release-docker-gates`](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31777769335/job/94696721877): `success`

## Root WIP Preservation

Before fetch or repository mutation, root was on `main` at
`664759a6b2fb1929bba913b3ec8a4210e7fb1b69`, with seven registered worktrees
and eight local branches. The allowed root WIP manifest was captured with file
mode, byte length, and SHA-256; its manifest digest was
`530ecbf08d01021f4e84d40087ce26db3761270a434abc2bb10a8036e341b410`.

| Status | Path | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| modified | `advisor-plans/README.md` | 10016 | `394df5618d29ade2c0b955cc7234dcf3344a81494509db890a03667797b42280` |
| untracked | `AGENTS.md.bak-pre-gitnexus-uninstall` | 5997 | `bb68496196cacbc25643c806585d5889e2824364bb6200847b81d8f9b6a162ae` |
| modified | `CLAUDE.md` | 10491 | `892f9fdfa81316d9ff46cab5d4818951a31cd0e7bf4a915df761199b8fa99f7c` |
| untracked | `CLAUDE.md.bak-pre-gitnexus-uninstall` | 13448 | `3bc44e26146d21862b0e2c37b287df743a8c9ff8b31aae3ae9a0b3c6b87569e8` |
| untracked | `CONTEXT.md` | 8421 | `9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c` |
| untracked | `docs/agents/domain.md` | 573 | `f358f97ebc4224a56f89fb342b3588ccc114899af469f3cbdedf35e2023b3d95` |
| untracked | `docs/agents/issue-tracker.md` | 621 | `decae4b541d382f2fe9c7c9f49617b405f1641cbd27b53b3137f3d8118164cfc` |
| untracked | `docs/agents/triage-labels.md` | 347 | `f672681495c9eef1db104f661ab0c3c87e73cde396b332a947e7da4551c21f34` |
| untracked | `docs/decisions/2026-08-04-parameter-value-evidence-retention.md` | 1616 | `cbad5c1377e3d1fd962e6f00ae72a3743029faa8c53edbd383992ab62e729a89` |
| untracked | `docs/decisions/2026-08-09-operator-session-boundary.md` | 2634 | `008a69e51c241bb14d0dedd3764df018a71e0c2be12eaab230bdda27383418d9` |
| untracked | `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md` | 5416 | `c2ced9487597793a0739fcc0368802a61bc2ce25d8bde6f9791a76d02edef869` |
| untracked | `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md` | 13645 | `e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb` |
| untracked | `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` | 12183 | `dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21` |

The path set and every byte-level manifest row matched before merge, after the
fast-forward merge, and after merged-root gates. None of these paths overlaps
the product diff. Existing root services, Docker containers, fixtures,
historical worktrees, historical branches, and the rescue branch were not
started, stopped, reset, stashed, cleaned, relocated, or deleted.

## Cleanup And Issue Safety

- Task-created documentation-check and RED-check temporary worktrees were
  removed after their checks.
- Task-created reviewer artifacts under the candidate worktree were removed,
  restoring the candidate checkout to its clean exact SHA.
- The Issue #26 candidate worktree and branch were intentionally retained for
  the required post-evidence independent verification and are the only task
  worktree and branch eligible for removal afterward.
- Issues #26, #25, #20, and #7 were all open at release preflight. Issues #25,
  #20, and #7 were not commented on, closed, relabeled, or otherwise modified.
  Issue #25 requires its own re-verification after this release.
