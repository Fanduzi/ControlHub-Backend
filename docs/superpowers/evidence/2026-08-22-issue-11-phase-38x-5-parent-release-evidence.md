# Phase 38X-5 Parent Release Evidence — Issue #11

Date: 2026-08-22
Issue: [#11, `38X-5: Automate API compatibility and remove release-test duplication`](https://github.com/Fanduzi/ControlHub-Backend/issues/11)

This is the final parent closure evidence for Phase 38X-5. All seven child
deliveries (38X-5A through 38X-5G) are published on both repositories'
`origin/main`, independently verified, and closed on the tracker with their
own tracked evidence. This record documents the published refs, the child
publication chain, fresh CI verdicts at the exact merged heads, and root WIP
preservation, and authorizes closure of issue #11 only.

## Exact Refs At Closure

| Item | Value |
| --- | --- |
| Backend repository | `Fanduzi/ControlHub-Backend` |
| Backend published head (`origin/main`) | `1083bd391bf806c18a9a97929e17e4a1b9a4232e` |
| Backend CI at that head | [run 32579862865](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32579862865) SUCCESS — `release-docker-gates`, `release-local-gates` |
| Frontend repository | `Fanduzi/ControlHub-Frontend` |
| Frontend published head (`origin/main`) | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Frontend CI at that head | [run 32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) SUCCESS — `release-e2e`, `release-local` |
| Phase base (after 38X-4 parent closure) | backend `518f791` (`docs(evidence): record Issue #10 38X-4 parent delivery closure`) |
| Evidence commit | this commit (docs-only, fast-forwarded to `main`) |

## Child Publication Chain (all CLOSED on the tracker)

| Child | Issue | Product SHA(s) | Product CI | Tracked evidence |
| --- | --- | --- | --- | --- |
| 38X-5A Console ingest preserves Controlled Error Code | #47 | FE `f645530dff9667ad68e7880e5e0627c401591640` | [FE run 32491035649](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32491035649) SUCCESS | `2026-08-22-issue-47-38x-5a-console-ingest-release-evidence.md` |
| 38X-5B Execute-path disclosure publishes `query_result_disclosure_blocked` | #48 | BE `85bb8e9` + P2 tests `ec8941a` | [BE run 32492850046](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32492850046) SUCCESS at product tip | `2026-08-22-issue-48-38x-5b-disclosure-http-release-evidence.md` |
| 38X-5C Query Workbench classifies by Controlled Error Code | #52 | FE `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` | [FE run 32494680509](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32494680509) SUCCESS | `2026-08-22-issue-52-38x-5c-workbench-codes-release-evidence.md` |
| 38X-5D Closed enum + OpenAPI compatibility checker | #53 | BE `b28cd34`, FE `53c9771` | [FE run 32496440389](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32496440389) SUCCESS | `2026-08-22-issue-53-38x-5d-enum-checker-release-evidence.md` |
| 38X-5E Release E2E runs full suite once | #49 | FE `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` | [FE run 32497331327](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32497331327) SUCCESS | `2026-08-22-issue-49-38x-5e-release-e2e-once-release-evidence.md` |
| 38X-5F Saved Statement E2E teardown guaranteed | #50 | FE final `175add77…` (original `a9d5002`) | [FE run 32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) SUCCESS; original push run [32498921106](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32498921106) SUCCESS | `2026-08-22-issue-50-38x-5f-saved-statement-teardown-release-evidence.md` |
| 38X-5G Console docs stop calling #19 a prerequisite | #51 | FE `ae1c134` | AC verified read-only against `175add77` content; unrelated docs-only flake at `ae1c134` ([run 32499385191](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32499385191) FAILURE) fixed by teardown fix-forward commits; head green per run 32570870769 | `2026-08-22-issue-51-38x-5g-console-docs-prerequisite-release-evidence.md` |

Child evidence integrity was re-checked mechanically on `origin/main`
(`git show origin/main:<file>`): none of the seven files contains unfilled
markers or unverified claims.

Tracker state verified 2026-08-22 before this record: every 38X-5 child issue
(#47–#53 and their re-filed mirrors #54–#60) is CLOSED; no other issue is
open in `Fanduzi/ControlHub-Backend` besides #11 itself.

## Phase Contract On The Published Refs

- **Closed enum**: backend commit `b28cd34` closes `ErrorResponse.error` as
  the Controlled Error Code enum in `/openapi.yaml`; OpenAPI validation gates
  remain required and green in Backend CI.
- **Drift gate**: frontend commit `53c9771` fails release when the console
  union drifts from the OpenAPI enum (checker in Frontend CI
  `release-local`/`release-e2e` graph); no view models are generated.
- **Honest execute path**: backend commits `efd6ea0`/`85bb8e9` publish
  `query_result_disclosure_blocked` from Apply/execute-path disclosure
  classification; covered by handler tests plus `ec8941a` sentinel-exclusivity
  tests.
- **Console ingest**: frontend commit `f645530` preserves `error` on
  `ApiError`; missing codes are not status-mapped into business codes; 401
  keeps the session handoff.
- **Workbench classification**: frontend commit `7ce7b8e` classifies feature
  failures by code; Retry follows the retryable-code table; localized copy;
  raw messages/codes stay out of the UI.
- **Release graph**: frontend commit `4de441e` runs the full Playwright suite
  once in CI (`release-e2e`); local smoke/interaction commands remain local.
- **Teardown guarantee**: frontend commits `a9d5002`, `d968b9e`, `91eaf6c`,
  `175add77` guarantee Saved Statement deletion after failed assertions,
  derive the target from the create URL, and clean up `beforeAll` creates;
  DELETE 404 counts as success, other teardown failures fail visibly.
- **Docs reconcile**: frontend commit `ae1c134` removes the "#19 unmerged
  prerequisite" wording from the three named surfaces.

## Fresh Gates At Closure

Backend at `1083bd39`: Backend CI run 32579862865 SUCCESS with required jobs
`release-docker-gates` (integration + Schemathesis fuzz) and
`release-local-gates`. Frontend at `175add77`: Frontend CI run 32570870769
SUCCESS with required jobs `release-e2e` (full real-Chromium suite once) and
`release-local`. No gate was skipped; no skip/mock/forced-click green exists
in the delivered range per each child's evidence.

## Root WIP Preservation

Backend root stayed on `main` at `44474afa8febbff49c3510bbd43cb1b30f9441a0`
(behind the published head, intentionally untouched). Its dirty paths are
user-owned and were preserved exactly: modified `CLAUDE.md`, `CONTEXT.md`,
`advisor-plans/README.md`; untracked bak files, `docs/agents/`, decision docs
(including the accepted 38X-5 contract decision), superpowers plan/spec docs.
Frontend root stayed on `main` at `cae99cae21b7c8fb278c928a864d40178b7bb6d5`
with modified `AGENTS.md`, `CLAUDE.md`, and two untracked screenshot PNGs.
No stash, restore, reset, clean, relocate, or amend occurred anywhere; this
closure was produced in the isolated worktree
`/private/tmp/controlhub-evidence-11-parent-closure-20260822` (branch
`issue-11-parent-closure-20260822`) off `1083bd39`.

## Cleanup And Issue Safety

- The evidence worktree/branch above are retained until CI confirms, then may
  be removed; nothing else was created or deleted.
- This evidence commit is docs-only, carries no AI co-author, and reaches
  `main` by normal push only; the pushed SHA and CI verdict are recorded in
  the closing comment.
- Only issue #11 closes with a factual comment (final SHA, evidence path, CI
  URL). No parent, successor, or unrelated ticket is touched.
