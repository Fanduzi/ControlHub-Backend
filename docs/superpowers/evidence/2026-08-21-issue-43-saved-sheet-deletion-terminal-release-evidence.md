# Issue #43 — Saved Sheet Deletion Terminal State and Mobile Layout Release Evidence

## Summary

Issue #43 `38X-4C: Make Saved Sheet deletion terminal and mobile-safe` is a
frontend product delivery. The candidate was built on published frontend
`origin/main` `cae99cae21b7c8fb278c928a864d40178b7bb6d5` (Issue #42 already
merged). An earlier unpublished implementation at `1f29b3c` (parent `201b415`)
was preserved and not published. The published candidate is the cherry-pick of
that product plus follow-up commits for 403 cancel-only, announcement-safe
assertions, and overlapping-submit protection.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` at start) | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` |
| Candidate / merged SHA | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Candidate branch | `issue-43-saved-sheet-deletion-terminal-final-20260820` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-43-final-20260820` |
| Push | Fast-forward `cae99cae..defda6bb` (6 commits) as `defda6bb:main`; normal push, no force |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `44474afa8febbff49c3510bbd43cb1b30f9441a0` |
| Backend evidence head | the docs-only commit carrying this file |
| Preserved unpublished original | `1f29b3cd6036474654b2fa06601e03f382f1f3fb` on `issue-43-saved-sheet-deletion-terminal-20260820` |

## Frontend Merged Commits (fast-forward `cae99cae..defda6bb`)

| SHA | Message |
|-----|---------|
| `9ef5ae679f9e79e0f9e4ad280c7e1dddaaf39f8b` | fix(query): make saved sheet deletion terminal and mobile-safe (#43) |
| `0f4cac2a03bd2c79ee74944e21ed02c825fb3f53` | fix(query): keep only retry/cancel after terminal delete errors (#43) |
| `16d510df2e9557e9148ee827afc00d4f82a3b63e` | test(e2e): wait for 201 on saved-statement create and open workbench before 375px (#43) |
| `41664bac20e539fea4d2ca15a2e2ff5a0da57575` | test(e2e): assert 404 delete by missing row control and BFF proxy URL (#43) |
| `acbf1f477f51d89e3b877c53bd1ecd804591ec7d` | test(e2e): assert saved-sheet removal by row control after delete announcements (#43) |
| `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` | fix(query): ignore overlapping delete submits and leftover announcement assertions (#43) |

## Changed Files (`cae99cae..defda6bb`)

```
components/query/README.md
components/query/query-saved-statements.tsx
e2e/README.md
e2e/api.helpers.ts
e2e/query-workbench.spec.ts
messages/en.json
messages/zh-CN.json
tests/components/README.md
tests/components/query-saved-statements.test.tsx
```

9 files, 539 insertions, 38 deletions.

## Local Candidate Gates

All commands ran from
`/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-43-final-20260820`
at `HEAD` `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` except `npm run release:local`
which was first recorded at `acbf1f4` and then the SHA-specific `tsc` /
related vitest / eslint re-ran at `defda6b`. Full `vitest run` at `acbf1f4`
was 98 files / 1515 tests; `defda6b` added one network-failure component test
(41 in `query-saved-statements.test.tsx`).

| Gate | Result |
|------|--------|
| `git diff --check origin/main...HEAD` | clean |
| `npm run check:runtime` | Node 22.22.0 |
| `npm run check:e2e-preflight` | pass (`:3100` and `:8081` free before the E2E run) |
| `npm run check:e2e-governance` | pass (14 spec files) |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors at `defda6b` |
| `npx eslint` on changed TS/TSX | 0 errors; 1 warning in `e2e/query-workbench.spec.ts:2804` (`historyItemCount` unused) identical to base |
| `npx vitest run` (full, `acbf1f4`) | 98/98 files, 1515/1515 tests |
| related vitest at `defda6b` | 3 files, 303 tests (saved-statements 41, workbench 181, editor-shell 81) |
| `npm run build` / `next build` (`acbf1f4` `release:local`) | success |
| `npm run release:local` (`acbf1f4`) | green |

## Real Chromium (`npm run release:e2e`)

Command: `npm run release:e2e` (`test:e2e:smoke && test:e2e:interaction && test:e2e`)

| Item | Value |
|------|-------|
| Frontend CWD | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-43-final-20260820` |
| Frontend SHA | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Backend CWD | `/tmp/controlhub-issue-43-e2e-backend-20260821` |
| Backend SHA | `44474afa8febbff49c3510bbd43cb1b30f9441a0` |
| Backend listen | `localhost:8080` |
| Metadata DB | `controlhub_43_e2e` (goose v17) |
| Query fixture | Docker `controlhub-query-e2e-mysql` `:13306` |
| Fixture operators | `e2e-admin-issue43@controlhub-e2e.invalid`, `e2e-editor-issue43@controlhub-e2e.invalid` |
| Chromium | Playwright bundled, headless |

| Command | Passed | Failed | Skipped | Duration |
|---------|--------|--------|---------|----------|
| `npm run test:e2e:smoke` | 7 | 0 | 0 | 10.5s |
| `npm run test:e2e:interaction` | 3 | 0 | 0 | 10.9s |
| `npm run test:e2e` | 183 | 0 | 0 | 4.9m |
| `npm run release:e2e` (aggregate) | 193 command-invocations / 183 unique in the full suite | 0 | 0 | 316.59s |

No route mocks, forced clicks, skips, or `page.route` were added to obtain green.

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Candidate PR ([#2](https://github.com/Fanduzi/ControlHub-Frontend/pull/2)) | [32394213989](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32394213989) | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` | SUCCESS (5m20s) | SUCCESS (24m38s) |
| Main push | [32396672667](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32396672667) | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` | SUCCESS (5m21s) | SUCCESS (24m39s) |

## Standards / Spec Verdict

Review tool: two-axis `code-review` (Standards + Spec) against
`cae99cae...HEAD`, then the remaining Standards P1 (announcement vs
`getByText` cleanup) was fixed in `defda6b`.

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | Prior leftover `getByText(name)` cleanup after `{name} deleted.` announcements was fixed in `defda6b` and re-proven by 183/183 Chromium |
| P2 | 0 | Duplicated 404 E2E bodies remain a judgement-only duplication; not a documented-standard breach |

### Spec

| AC | Status |
|----|--------|
| Pending cannot be dismissed or submitted twice | Covered: disabled confirm/cancel, Escape ignored while pending, synchronous `deleteInFlightRef` |
| Network and 5xx Retry + Cancel | Covered: 500 component test + `TypeError("Failed to fetch")` network test; confirm Delete hidden |
| HTTP 403 cancel-only | Covered: no Retry, no Delete, Cancel enabled, `role="alert"` |
| HTTP 404 closes, refreshes, announces absence without success | Covered in component tests and Chromium desktop EN / 375px EN / desktop zh-CN |
| Last row on page > 1 loads previous page | Component test |
| Polite status / alert / no forced announcement focus | `role="status"` `aria-live="polite"`; `role="alert"` for inline errors; live region is not focused. Dialog close still restores trigger focus (existing a11y pattern, not announcement-focus steal) |
| 375px search own row, create wrap, no overflow | Chromium overflow + search-below-create |
| Component tests: pending, retry, 403, 404, pagination, a11y, target cleanup | Present (41 tests in the file) |
| Real Chromium EN / 375 / zh-CN without route mocks | Present |

| Severity | Count |
|----------|-------|
| P1 | 0 |
| P2 | 0 |

## Root WIP Preservation

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

- `AGENTS.md` modified (user WIP)
- `CLAUDE.md` modified (user WIP)
- 4 untracked files preserved (bak files, screenshot PNGs)

Fast-forward of `main` was performed with `git push origin defda6bb:refs/heads/main` from the clean candidate worktree. The dirty root was not stash/reset/clean/checked-out.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

- `CLAUDE.md` modified (user WIP)
- `advisor-plans/README.md` modified (user WIP)
- untracked bak files, `docs/agents/`, and older specs/decisions preserved

Evidence was committed from `/tmp/controlhub-evidence-43-20260821`, not from the dirty root.

## Cleanup (after push and CI)

- Disposable metadata DB `controlhub_43_e2e` dropped after the local E2E run
- Fixture credential files under `/tmp/e2e43_dc_creds` removed after the local E2E run
- Local E2E backend process on `:8080` stopped
- Shared Docker `controlhub-query-e2e-mysql` preserved
- Old unpublished #43 clone/branch at `1f29b3c` preserved
- Candidate worktree/branch retained until verifier confirms push + CI
