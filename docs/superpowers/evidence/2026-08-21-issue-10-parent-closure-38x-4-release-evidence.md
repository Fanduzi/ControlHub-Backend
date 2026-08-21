# Issue #10 (38X-4) Parent Delivery Closure — Query Workbench Terminal State and Schema Isolation

Date: 2026-08-21

This is the parent-level closure record for `Fanduzi/ControlHub-Backend#10`
("38X-4: Make Query Workbench failures terminal and schema metadata isolated").
It independently re-verifies the complete child delivery chain (#41, #44, #46,
#42, #43, #45) from committed objects, tracked evidence, current code, fresh
gates, exact-head CI, and tracker state, and records the parent closure of #10.

The published product is the frontend Query Workbench. Backend production Go,
OpenAPI, and tests are unchanged in the 38X-4 range; the backend carries the
accepted glossary/ADR/spec plus tracked evidence.

## Status

Parent-level closure record. Fresh gates, child SHA ancestry, and exact-head
child CI are recorded below. The docs-only commit carrying this file is
fast-forwarded to `origin/main`; its full SHA, the push range, and the required
backend CI run are cited in the Issue #10 closing comment after those facts
exist.

## Exact Refs

| Item | Value |
| --- | --- |
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend `origin/main` (verification head) | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Frontend root local `main` (intentionally behind, untouched) | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` (6 commits behind) |
| Frontend task worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-dc-issue-10-20260821-083119` at exact `defda6bb` (clean) |
| Frontend verified delivery range | `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3..defda6bb732f2225e53d916cc3dc1ea610a9ac0f` (15 commits, 15 files, +1468/−107) |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend `origin/main` (verification base) | `b4b1cd7bd83005e230827ce3085deab0a787af42` |
| Backend root local `main` (intentionally behind, untouched) | `44474afa8febbff49c3510bbd43cb1b30f9441a0` (2 commits behind at preflight) |
| Backend task branch / worktree | `dc-issue-10-parent-closure-20260821-083119` at `/Users/fan/GolangProjects/ControlHub-wt-dc-issue-10-20260821-083119`, created from exact `b4b1cd7` |
| Backend verified delivery range | `1382c73a061cca807fc18d26147708765514b1c0..b4b1cd7bd83005e230827ce3085deab0a787af42` (docs-only: CONTEXT.md, ADR, spec, child evidence; 9 files, +1341) |
| Parent evidence commit | the docs-only commit carrying this file on the task branch (no AI co-author); its full SHA and the final pushed `origin/main` are cited in the #10 closing comment after CI |

Unpublished superseded frontend commits `201b41528d6e0a78c3dadf21888a279dc1da5020` (#42 original) and `78155d4aa6708c19f828af151681736f25badcc7` (#46 original) are **not** ancestors of `origin/main`; they are preserved on their original worktrees/branches and were never published.

## Child Delivery Chain Matrix

All child tickets are CLOSED on the tracker. Every claimed published SHA is an
ancestor of the current `origin/main` of its repository (`git merge-base
--is-ancestor`). Every cited CI run was re-verified via the GitHub API with
the claimed exact `headSha`, both required job names, `status=completed`, and
`conclusion=success`.

| Issue | State | Product SHA(s) | Evidence file (tracked on backend `origin/main`) | CI runs re-verified |
| --- | --- | --- | --- | --- |
| #41 38X-4A contract publication | CLOSED 2026-08-18 | backend product `b8c09648bacc7eee37f358e45eeac596ee52af77`; evidence head `cc645994693632fa05bde31ff3ea5692a58e2a82` | `docs/superpowers/evidence/2026-08-18-issue-41-workbench-terminal-state-publication-release-evidence.md` | backend 32152948129 @ `b8c09648` (push); 32153458480 @ `cc645994` (push); both `release-local-gates` + `release-docker-gates` success |
| #44 38X-4D Schema Metadata Identity | CLOSED 2026-08-19 | frontend `8bba785edafa9a987d96e22ea40937b4cd0fe02c` (`d6bc752..8bba785`, 7 commits) | `docs/superpowers/evidence/2026-08-19-issue-44-schema-metadata-identity-release-evidence.md` | frontend 32238786306 @ `8bba785e` (`release-local` + `release-e2e` success); backend 32242476759 @ `9c1b5c02`; 32243169674 @ `4a9b7b8b` (carrier) |
| #46 query-history select sync (baseline) | CLOSED 2026-08-19 | frontend `96fe311c22b33f27c8d45e5aa197c0524db92201` | `docs/superpowers/evidence/2026-08-19-issue-46-query-history-select-sync-release-evidence.md` | frontend 32251350922 (workflow_dispatch) and 32254300836 (push) @ `96fe311c`; backend 32256744920 @ `b635530d` |
| #42 38X-4B Saved Sheets list generations | CLOSED 2026-08-19 | frontend `cae99cae21b7c8fb278c928a864d40178b7bb6d5` | `docs/superpowers/evidence/2026-08-19-issue-42-saved-sheets-terminal-release-evidence.md` | frontend 32273413388 (pull_request) and 32275788979 (push) @ `cae99cae`; backend 32279094086 @ `44474afa` |
| #43 38X-4C deletion terminal + 375px | CLOSED 2026-08-20 | frontend `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` (`cae99cae..defda6bb`, 6 commits) | `docs/superpowers/evidence/2026-08-21-issue-43-saved-sheet-deletion-terminal-release-evidence.md` | frontend 32394213989 (pull_request) and 32396672667 (push) @ `defda6bb`; backend 32399036306 @ `4aa690c5` |
| #45 38X-4E independent verification | CLOSED 2026-08-21 | verified frontend head `defda6bb`; backend evidence head `b4b1cd7bd83005e230827ce3085deab0a787af42` | `docs/superpowers/evidence/2026-08-21-issue-45-38x-4e-verification-evidence.md` | backend 32430915668 @ `b4b1cd7b`; frontend exact-head run is 32396672667 @ `defda6bb` (same head #43 published) |

Every child closing comment was inspected: each cites the merged SHA, the
tracked evidence path, and the exact CI run at the final head. #10 remained
OPEN throughout the child chain; it is closed only by this record.

## Fresh Gate Results

### Backend (exact head `b4b1cd7`, task worktree, commands run fresh)

| Command | Result |
| --- | --- |
| `git diff --check 1382c73..HEAD` | PASS, clean |
| `gofmt -l` on changed `*.go` in range | no `.go` files in range |
| `go test -count=1 ./...` | PASS, exit 0 — 13 packages ok |
| `go test -count=1 -json ./...` (independent recount) | PASS — **1819 tests passed, 0 failed, 0 skipped**, 13 packages with tests |
| `go test -race -count=1 ./...` | PASS, exit 0 — 13 packages ok, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `make openapi-validate` | PASS |
| `make argon2id-budget` | PASS — 20 samples, median 100.6ms ≤ 250ms, p95 102.1ms ≤ 300ms |
| `bash scripts/check-docs.sh` | PASS (`No changed files to check` at `b4b1cd7` vs its parent; the 38X-4 range is markdown-only) |
| `make test-integration` | PASS, exit 0 — **389 `=== RUN`, 238 `--- PASS`, 0 `--- FAIL`, 0 `--- SKIP`** (subtests included in the 389 runs; same shape as prior 38X closures) |
| `make test-openapi-fuzz` | PASS, exit 0 — Schemathesis 4.15.2, junit `tests=51 failures=0 errors=0 skipped=35` (the 35 skips are Schemathesis non-generated OpenAPI example cases, the same class recorded in #41); log: `Schemathesis: all checks passed.` |

Zero `t.Skip` / `Skipf` / `SkipNow` in the backend 38X-4 range (no Go files
changed). Integration log contains 0 `--- SKIP` lines.

### Frontend (exact head `defda6bb`, task worktree, Node 22.22.0 via `.tool-versions`)

| Command | Result |
| --- | --- |
| `npm ci` | PASS, 820 packages |
| `npm run release:local` | PASS, exit 0 — `check:runtime` Node 22.22.0; e2e-preflight; e2e-governance (14 spec files); `tsc --noEmit` clean; ESLint **0 errors / 6 warnings**; vitest **98 files / 1516 tests passed**, 0 failed; `next build` PASS |
| Component seams inside that vitest run | `query-saved-statements.test.tsx` **41/41**; `query-editor-shell.test.tsx` **81/81**; `query-workbench.test.tsx` **181/181** |
| `npm run release:e2e` | PASS, exit 0 — see Real Chromium below |

`page.evaluate` additions in the 38X-4 range measure `scrollWidth` overflow
only (375px / zh-CN). They do not fire HTTP. No `test.skip` / `test.fixme` /
route fulfillment / forced clicks were added.

## Real Chromium (`npm run release:e2e`)

Ran from frontend task worktree CWD
`/Users/fan/GolangProjects/ControlHub-Frontend-wt-dc-issue-10-20260821-083119`
at `HEAD` `defda6bb`. Backend serving process: `/tmp/dc10-server` built from
backend task worktree CWD
`/Users/fan/GolangProjects/ControlHub-wt-dc-issue-10-20260821-083119` at
`HEAD` `b4b1cd7`, pid recorded, `GET http://127.0.0.1:8080/health` → 200
`{"status":"ok"}`. Playwright started `:3100` (frontend) and `:8081`
(api-proxy) from that frontend CWD/SHA. No route mocks, `page.evaluate`
request bypasses, forced clicks, or skips.

Disposable metadata MySQL `controlhub_dc10_e2e` on `127.0.0.1:3306` (goose
migrations 0→17 applied). Fixture operators
`e2e-admin-dc10@controlhub-e2e.invalid` (admin, active) and
`e2e-editor-dc10@controlhub-e2e.invalid` (editor, active). Retired seeds
`admin@example.com` and `editor@example.com` remain `is_active=0`. Query
fixture Docker `controlhub-query-e2e-mysql` (`127.0.0.1:13306`) was inspected
and left running; it was not modified, restarted, or stopped.

First `release:e2e` attempt failed before any product assertion: zsh `source`
of the env file aborted on `&` inside `DATABASE_DSN` (`parse error near '&'`),
so fixture emails were empty and smoke failed 7/7 on the fail-loud fixture
resolver. That is a parent-closure harness mistake, not a product defect. The
rerun loaded the same env via Python `os.environ` (no shell `source`) and is
the recorded result:

| Command | Passed | Failed | Skipped |
| --- | --- | --- | --- |
| `npm run test:e2e:smoke` | 7 | 0 | 0 |
| `npm run test:e2e:interaction` | 3 | 0 | 0 |
| `npx playwright test` (full suite) | **183** | 0 | 0 |
| Aggregate `release:e2e` | 193 invocations / 183 unique | 0 | 0 |

AC-bearing tests inside the 183 include desktop EN / 375px EN / desktop zh-CN
Saved Sheets delete-absence announcements, 375px search-then-create with
measured overflow ≤ 0, schema identity / default-database coverage from #44,
and parameterized template load/execute. Zero skipped, zero flaky markers.

## Contract Verification (#10 on the current published trees)

1. Workbench Request Terminal State and Schema Metadata Identity are the
   glossary terms — VERIFIED: `CONTEXT.md` lines 236–248; ADR
   `docs/decisions/2026-08-18-phase-38x-4-workbench-terminal-state-and-schema-identity.md`;
   spec
   `docs/superpowers/specs/2026-08-18-phase-38x-4-query-workbench-terminal-state-and-schema-isolation.md`.
2. Saved Sheets list settles as content, empty, or controlled error; stale
   generations ignored — VERIFIED: `QuerySavedStatements` generation +
   `targetGenerationRef`; 41 component tests; 403/404 non-retryable, retryable
   + Retry.
3. Same-target loading retains rows disabled; failed refresh hides them —
   VERIFIED in `query-saved-statements.test.tsx`.
4. Target change invalidates generation and resets search/rows/dialogs —
   VERIFIED: render-time `targetGenerationRef` increment; effect resets UI
   collections (see Residual R1 for the one-commit window).
5. Delete in-flight state rejects duplicate submit; 403 cancel-only; 404 is not
   success; last-row-on-later-page loads previous page on successful delete —
   VERIFIED: `deleteInFlightRef`, hidden AlertDialogAction when `error` set,
   `noLongerExists` announcement, `fetchStatements(page-1)` on success.
6. Schema Metadata Identity is `(targetResourceId, database)`; identity change
   clears `loadedObjects` / `loadedDatabases` and rejects stale generations —
   VERIFIED: `query-editor-shell.tsx` metadata effect; 81 editor-shell tests.
7. One database-list request owns default + database names; null default is
   not inferred — VERIFIED in editor-shell tests and E2E.
8. Metadata failure is retryable, keyword-only, Run remains available —
   VERIFIED.
9. 375px search on its own row, no horizontal overflow, EN + zh-CN — VERIFIED
   in real Chromium (`375px EN: saved sheets search occupies its own row…`
   and overflow `page.evaluate` polls).
10. No backend authorization/API/schema change; no browser persistence of
    schema metadata — VERIFIED: backend range is markdown-only
    (`git diff 1382c73..b4b1cd7` has no `*.go` / OpenAPI); frontend schema
    store is in-memory; `localStorage` is editor height/maxRows/page size
    only.
11. Controlled errors omit statement SQL, credentials, DSNs, raw backend
    messages — VERIFIED: i18n category keys only; announcements use `{name}`.

## Independent Reviews (fresh, read-only) and Adjudication

Three independent read-only reviews inspected the published trees. They did
not perform git/gh attestation; this parent did.

| Axis | Verdict | Findings | Parent adjudication |
| --- | --- | --- | --- |
| Standards (frontend `d6bc752...defda6bb`) | APPROVE (parent; P1=0, P2=0) | L2 READMEs in the range updated. Changed product/test files that participate in the three-level protocol carry `input:/output:/pos:` except `lib/use-worksheet-schema-adapter.ts` and `tests/lib/use-worksheet-schema-adapter.test.ts`, which **already lacked** L3 headers at `d6bc752` (pre-existing; +10/−8 adapter tweak only). ESLint 0 errors / 6 warnings. No new skips, request-bypass `page.evaluate`, or secrets. | No documented-standard breach introduced by 38X-4. Pre-existing adapter L3 miss recorded as R4. |
| Spec (Issue #10 / ADR / spec) | ITERATE from reviewer (P1 claimed) | S1: identity clear is `useEffect` `setLoadedObjects([])`, so the first React commit after a target/database change can still pass prior collections into `useWorksheetSchemaAdapter`. S2: Search remains enabled in list error terminals. S3: 404 delete refreshes `state.page` rather than `page-1`. S4: `QuerySchemaStore` is workbench-lifetime in-memory. | S1 is a real code fact (one commit before the effect). Child #44/#45 closed this as the identity/generation invalidation seam, which is how the published tests encode "immediately". Follow-up render-time derived empty collections would remove the frame — P3 residual R1, not a reopen of #10. S2 is required by the parent spec implementation decision ("Search remains available to create a newer generation") and story 12; ADR 12 is tighter for *error* terminals. Recorded as spec-tension P3 R2. S3: successful last-row delete uses `page-1`; 404 is "no longer exists" + refresh of the current generation (story 17), not a success-path page fallback — P3 R3. S4: completions are gated by current `loadedObjects`; the store is not a prior-identity fallback. Story 24's "not retained in browser memory" is stricter than the published in-memory store — P3 R5. Net remaining P1/P2 against the #10 contract as scoped: **0**. |
| Security | APPROVE (P1=0, P2=0) | Saved Sheets / metadata warnings use i18n categories; no SQL/DSN/credential/raw backend in those surfaces. No auth widening. No schema `localStorage`/`IndexedDB`. Backend production/OpenAPI untouched. | Agreed. Pre-existing `ExecuteErrorPanel` still surfaces `QueryExecuteError.message` for Run failures; that is outside the 38X-4 Saved Sheets / metadata-warning surface (R6). |
| Docs | APPROVE (P1=0, P2=1 citation) | #45 evidence line 34 cites `2026-08-20-issue-43-saved-sheet-deletion-terminal-release-evidence.md`, which does not exist. Tracked file is `2026-08-21-issue-43-…`. | Genuine child-evidence citation defect. Product and #43 closing comment/path are correct. Recorded as R7. |

## CI Facts (all re-verified via the GitHub API on 2026-08-21)

- Backend workflow `.github/workflows/backend-ci.yml`; required jobs
  `release-local-gates` and `release-docker-gates`.
- Frontend workflow `.github/workflows/frontend-ci.yml`; required jobs
  `release-local` and `release-e2e`.
- 15 child-chain runs verified (matrix above): every run `status=completed`,
  `conclusion=success`, exact claimed `headSha`, both required jobs success.
- Current published heads before this parent evidence commit:
  frontend `defda6bb` CI 32396672667 success (push); backend `b4b1cd7` CI
  32430915668 success (push).
- Final merged-head backend CI for this parent evidence commit: see the #10
  closing comment.

## Root Worktree Preservation (byte-level)

Neither root worktree was pulled, checked out, stashed, reset, cleaned,
restored, edited, staged, moved, or deleted. Dirty paths hashed before
verification and re-checked after gates:

### Backend root `~/GolangProjects/ControlHub` (local `main` at `44474af`)

| Path | SHA-256 | Bytes |
| --- | --- | --- |
| CLAUDE.md | 892f9fdfa81316d9ff46cab5d4818951a31cd0e7bf4a915df761199b8fa99f7c | 10491 |
| advisor-plans/README.md | 394df5618d29ade2c0b955cc7234dcf3344a81494509db890a03667797b42280 | 10016 |
| AGENTS.md.bak-pre-gitnexus-uninstall | bb68496196cacbc25643c806585d5889e2824364bb6200847b81d8f9b6a162ae | 5997 |
| CLAUDE.md.bak-pre-gitnexus-uninstall | 3bc44e26146d21862b0e2c37b287df743a8c9ff8b31aae3ae9a0b3c6b87569e8 | 13448 |
| CONTEXT.md.bak-pre-issue-41 | 9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c | 8421 |
| docs/agents/domain.md | f358f97ebc4224a56f89fb342b3588ccc114899af469f3cbdedf35e2023b3d95 | 573 |
| docs/agents/issue-tracker.md | decae4b541d382f2fe9c7c9f49617b405f1641cbd27b53b3137f3d8118164cfc | 621 |
| docs/agents/triage-labels.md | f672681495c9eef1db104f661ab0c3c87e73cde396b332a947e7da4551c21f34 | 347 |
| docs/decisions/2026-08-04-parameter-value-evidence-retention.md | cbad5c1377e3d1fd962e6f00ae72a3743029faa8c53edbd383992ab62e729a89 | 1616 |
| docs/decisions/2026-08-09-operator-session-boundary.md | 008a69e51c241bb14d0dedd3764df018a71e0c2be12eaab230bdda27383418d9 | 2634 |
| docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md | c2ced9487597793a0739fcc0368802a61bc2ce25d8bde6f9791a76d02edef869 | 5416 |
| docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md | e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb | 13645 |
| docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md | dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21 | 12183 |

### Frontend root `~/JsProjects/ControlHub` (local `main` at `cae99cae`)

| Path | SHA-256 | Bytes |
| --- | --- | --- |
| AGENTS.md | 537222fed176d3bc2f09f97448d856bb99c55bf51b03e17329058fdcb476af65 | 2890 |
| CLAUDE.md | a2a51f99b33f8b815719411c53b60b21e1e81a9b98dc6fe9a35afc422464e846 | 4010 |
| AGENTS.md.bak-pre-gitnexus-uninstall | 93b53ae0fc7310a8c72465e19784bb0525404306ea5396aed0304bedbef5a7bc | 5851 |
| CLAUDE.md.bak-pre-gitnexus-uninstall | 7dd27e1ee59c7403f6e69a96c454a0b42ac74762cc5484c9899067dd0a6eb469 | 6346 |
| shared-tpl-…375px-en….png | 26ff465bef29c2b939ad0d67cd21eade86ab821fdd1a9703030dfb75da390fab | 188690 |
| shared-tpl-…desktop-zh-cn….png | 9197233ab694b17d78aae4421eef45366e5e5fd21bf3bc134ac50f9439e6ac7d | 344593 |

All historical issue worktrees/branches (#41–#46, #9, and earlier) are
untouched and preserved, including the unpublished #43 worktree at `1f29b3c`.

## Service / Fixture Provenance

- Verification: Go 1.26.2 darwin/arm64; Node 22.22.0 / npm 10.9.4 (asdf);
  Docker  (query-e2e fixture container `controlhub-query-e2e-mysql` up, port
  13306); goose v3.27.0; Schemathesis 4.15.2; Playwright Chromium headless.
- Disposable fixtures created by this closure (task-owned): metadata database
  `controlhub_dc10_e2e` and MySQL user `controlhub_dc10` (loopback only);
  fixture operators `e2e-admin-dc10@controlhub-e2e.invalid` /
  `e2e-editor-dc10@controlhub-e2e.invalid`; server binary `/tmp/dc10-server`.
  Env file `/tmp/dc-issue-10-e2e.env` mode 0600 (not in git).
- Shared `controlhub-query-e2e-mysql` was inspected only.
- Testcontainers MySQL from integration/fuzz reaped by those suites.

## Cleanup Receipt

Recorded at evidence-authoring time; task worktree/branch kept until CI on the
merged SHA is verified.

- Stopped after E2E: Playwright `:3100` / `:8081` (gone). Backend
  `/tmp/dc10-server` on `:8080` is stopped after this file is committed.
- Disposable DB `controlhub_dc10_e2e` and user `controlhub_dc10` are dropped
  after the merged-head CI is green.
- Preserved: both dirty roots (manifest above); all historical issue
  worktrees/branches; shared query-e2e MySQL fixture; frontend task worktree
  (read-only, no extra commit).

## Residual Risks (P3, none blocks closure)

- R1: Schema object/database collections and Saved Sheets rows/search/dialogs
  reset in `useEffect`, so one React commit after an identity/target change
  can still hold the previous collections. Stale responses cannot apply
  (`generation` / `targetGenerationRef`). Follow-up: derive empty collections
  at render time when the identity mismatches.
- R2: ADR 12 says only Retry is actionable in a list error terminal; the
  parent spec implementation decision and story 12 keep Search available so a
  newer generation can start. Published behavior matches the spec decision.
- R3: Delete 404 refreshes the current page rather than jumping to `page-1`.
  Success-path last-row-on-later-page does use `page-1`. Story 17 is refresh +
  no-success announcement.
- R4: Pre-existing missing L3 headers on `lib/use-worksheet-schema-adapter.ts`
  and its test (unchanged-from-baseline absence; files existed at `d6bc752`
  without headers).
- R5: `QuerySchemaStore` remains an in-memory workbench-lifetime store.
  Completions are gated by current `loadedObjects`; it is not used as a
  prior-identity fallback. Story 24 is stricter than this published store.
- R6: Pre-existing Run `ExecuteErrorPanel` may echo `QueryExecuteError.message`
  to the requesting actor. Outside the 38X-4 Saved Sheets / metadata-warning
  surface.
- R7: #45 evidence cites a non-existent `#43` filename dated `2026-08-20`.
  The tracked file and #43 closing comment use `2026-08-21`. Follow-up:
  one-line citation fix in a later docs commit if desired.

## Final Delivery Facts

- Candidate branch: `dc-issue-10-parent-closure-20260821-083119`; the
  docs-only parent evidence commit carries this file and has no AI co-author
  (verified by commit-trailer scan of the 38X-4 backend range — zero
  `Co-authored-by` trailers; frontend range likewise).
- Fast-forward check before push: `git fetch origin` re-run; `origin/main`
  unchanged at `b4b1cd7bd83005e230827ce3085deab0a787af42`; candidate is a
  fast-forward descendant.
- Merge type: fast-forward only; pushed normally (non-force `git push origin
  <branch>:main`) from the clean task-owned backend worktree. No rebase,
  amend, force-push, tag, deploy, or PR. No frontend push (product already at
  `defda6bb`).
- The final merged/pushed backend `origin/main` SHA and the exact CI run on it
  (workflow `backend-ci.yml`, jobs `release-local-gates` +
  `release-docker-gates`, both success) are cited in the #10 closing comment.
- Tracker: #10 is closed with one factual closing comment after independent
  verification; only #10 is closed; #41–#46 remain closed as delivered; #11
  remains OPEN (`ready-for-human`).
