# Phase 38F Query Workbench SQL Editor Foundation — Evidence

## Frontend Repository

Repository: `/Users/fan/JsProjects/ControlHub`

| Field | Value |
|---|---|
| Old main HEAD | `68a3e50` |
| New main HEAD | `499c235` |
| Pushed range | `68a3e50..499c235` |

## Merged Commit Summary

Commits pushed to `origin/main` implement a frontend-only SQL editor foundation:

- CodeMirror 6 SQL editor replaces the plain textarea in Query Workbench.
- Local SQL formatting via `sql-formatter` with engine-aware dialect selection (MySQL/TiDB → mysql, fallback → sql).
- Multi-worksheet state model with per-worksheet tab bar, add/rename/close controls, and local-only state (no persistence).
- Per-worksheet target synchronization: each worksheet owns its target; switching worksheets restores target context in schema/governance.
- Race guards: async work is keyed by both worksheet id and target id; stale results never paint into the wrong worksheet.
- Keyboard shortcuts: `Cmd/Ctrl+Enter` to run, `Cmd/Ctrl+Shift+F` to format.
- Format button with controlled error display.
- History and result isolation per worksheet.
- Real E2E coverage: 16/16 Query Workbench tests passed against real backend and dedicated query MySQL fixture.

## Feature Outcome

| Capability | Status |
|---|---|
| CodeMirror SQL editor | Replaces textarea; syntax highlighting, line numbers, bracket matching, fold gutter |
| Local SQL formatting | `sql-formatter` with `formatterLanguageForEngine()` helper; no server round-trip |
| Multi-worksheet state | Per-worksheet: statement, target, result, error, history, result tab, max rows, execution status |
| Target synchronization | Worksheet ↔ target navigator bidirectional sync; `targetSelectionVersion` prevents worksheet-switch false clears |
| Race guards | `activeRequestRef` keyed by `(worksheetId, targetId)` guards all async state writes |
| Keyboard shortcuts | `Mod-Enter` run, `Mod-Shift-f` format; disabled when target is locked or execution is in progress |
| History isolation | Per-worksheet history refresh; stale worksheet/target combo silently discarded |
| Result isolation | Per-worksheet `activeResultTab`, result, error; no cross-worksheet leak |

## Real E2E Result

| Spec | Result |
|---|---|
| `npm run test:e2e -- --grep "Query Workbench"` | 16/16 passed |

Environment:

- Backend: Phase 38D main on `:8080`.
- Dedicated query DB: `controlhub-query-e2e-mysql` on `127.0.0.1:13306`.
- Ready target: resourceId `616`, engine `mysql`, readiness `ready`.
- Frontend: Next.js dev server on `:3100`.

## Stale Claim Correction

An earlier report claimed "backend unavailable / E2E not run." That claim was
**wrong**. Real E2E with a live backend and dedicated query MySQL fixture passed
16/16 on the feature branch and 16/16 post-merge on main. The stale claim is
superseded by the evidence above.

## Frontend CI

| Field | Value |
|---|---|
| Run URL | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/28948152448 |
| Status | success |

## Verification Matrix

| Check | Result |
|---|---|
| `git diff --check` | Clean |
| `npm run check:e2e-preflight` | PASS |
| `npm run check:e2e-governance` | PASS |
| `npx tsc --noEmit` | PASS |
| `npm run lint` | 0 errors, 4 warnings |
| `npm run test` | 800/800 passed |
| `npm run build` | PASS |
| `npm run test:e2e -- --grep "Query Workbench"` | 16/16 passed (feature branch) |
| `npm run test:e2e -- --grep "Query Workbench"` (post-merge main) | 16/16 passed |

## Scope Confirmation

- [x] No backend product edits
- [x] No SQL guard changes
- [x] No DSN/password browser state
- [x] No `actorUserId` sent
- [x] No Workbench credential edit controls
- [x] No saved query/export/approval/worksheet persistence
- [x] No tag/release/deploy
- [x] No AI co-author
