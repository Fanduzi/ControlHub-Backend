# Frontend Phase 18A Worker Prompt — Interaction Stability QA Gate

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 18A — Frontend Interaction Stability QA Gate**

## Required Input Documents

Read these documents before changing code:

```text
docs/superpowers/specs/2026-04-28-frontend-interaction-stability-qa-gate.md
docs/superpowers/plans/2026-04-28-frontend-interaction-stability-qa-gate.md
```

The implementation plan is authoritative. Follow it task-by-task unless you find a factual mismatch with the current codebase. If there is a mismatch, stop and report the exact mismatch before inventing a new approach.

## Context

Recent frontend regressions were missed because verification covered individual actions, not full browser interaction chains:

- engine dropdown selected `mysql` but the menu stayed open / page felt frozen
- database/resource row sheet opened but blank-click close left follow-up interactions broken
- after closing sheet, resource name link could hang
- after navigating to resource detail and using browser Back, accent color could reset to blue and row/dropdown interactions could stop working

The goal of this phase is **not** to add product UI. The goal is to add a reusable E2E QA gate that fails if those regressions return.

## Hard Requirements

Implement a dedicated browser QA gate:

```text
e2e/operator-interaction-stability.spec.ts
```

and shared helper:

```text
e2e/harness/interaction-stability.ts
```

Add npm script:

```text
npm run test:e2e:interaction
```

The interaction gate must cover all of these flows:

1. `/resources?environment=prod&page=1`
   - set accent to `purple`
   - click first resource table name link
   - land on `/resources/:id`
   - browser Back
   - assert accent is still purple
   - assert no dialog / overlay / inert residue
   - assert row click opens sheet
   - assert blank click closes sheet
   - assert a multi-select filter menu can open

2. `/resources?environment=prod&page=1`
   - set accent to `purple`
   - click a table row to open sheet
   - click `打开完整详情` / `Open full detail`
   - land on `/resources/:id`
   - browser Back
   - assert accent is still purple
   - assert no dialog / overlay / inert residue
   - assert row click opens sheet
   - assert blank click closes sheet
   - assert a multi-select filter menu can open

3. `/databases?environment=prod&page=1`
   - set accent to `purple`
   - open engine multi-select
   - choose `mysql`
   - assert URL includes `resourceSubtype=mysql`
   - assert menu closes
   - click database row to open sheet
   - blank-click close sheet
   - click first database resource name link
   - land on `/resources/:id`
   - browser Back
   - assert accent is still purple
   - assert no dialog / overlay / inert residue
   - assert row click opens sheet
   - assert blank click closes sheet
   - assert a multi-select filter menu can open

## Required Invariants

Every relevant restored-list state must prove:

- `document.documentElement.dataset.accent === "purple"`
- CSS `--primary` is not the default blue primary value
- `[role="dialog"]` count is `0` after close/back restore
- `[data-slot="sheet-overlay"]` count is `0` after close/back restore
- `[inert]` count is `0` after close/back restore
- table row click still opens sheet
- blank click still closes sheet
- first multi-select dropdown still opens
- no browser console errors
- no unexpected browser console warnings
- no 4xx/5xx network responses

Use existing guards:

```text
e2e/harness/console-guards.ts
e2e/harness/backend-health.ts
e2e/harness/auth.ts
```

Do not introduce broad warning suppression.

## TDD Requirement

This phase is test infrastructure, but still follow TDD discipline:

1. Add the new E2E test/helper.
2. Run `npm run test:e2e:interaction`.
3. Confirm it fails if the current app does not satisfy the gate, or passes if the current bugfix already satisfies it.
4. Only change production code if the new gate exposes a real failure.

If production code needs changing:

- keep the change minimal
- rerun the specific interaction gate
- rerun full frontend verification
- document the root cause

## Verification Commands

Backend must be running on `:8080`.

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If backend is not running, start it from the backend repo:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

Then rerun E2E commands from the frontend repo.

## Negative Scope

Do not:

- modify backend code
- change backend API contracts
- add product UI
- redesign resource/database tables
- change topology behavior
- restore `/cmdb` navigation
- reintroduce demo `resourceSummaries`
- suppress warnings broadly
- tag, push, release
- add AI co-author attribution

## Commit Requirements

Commit all intended changes.

Commit message should be concise, for example:

```text
test: add frontend interaction stability qa gate
```

Do not include:

```text
Co-Authored-By
Claude
Anthropic
AI
```

## Final Report Format

Return a final report with:

```markdown
## Phase 18A Final Report

### Commit
- Hash:
- Branch:
- Worktree:

### Files Changed
| File | Purpose |
|---|---|

### Interaction Gate Coverage
| Flow | Result |
|---|---|
| Resources link → detail → Back → interactions | PASS/FAIL |
| Resources sheet → full detail → Back → interactions | PASS/FAIL |
| Databases filter → sheet close → link → Back → interactions | PASS/FAIL |

### Invariants Verified
- Accent remains purple after Back:
- No dialog/overlay/inert residue:
- Row click works after Back:
- Dropdown opens after Back:
- Console clean:
- Network clean:

### Verification
| Command | Result |
|---|---|
| `npx tsc --noEmit -p tsconfig.json` | |
| `npm run lint` | |
| `npm run test` | |
| `npm run build` | |
| `npm run test:e2e:smoke` | |
| `npm run test:e2e:interaction` | |
| `npm run test:e2e` | |

### Scope Confirmation
- No backend changes:
- No product UI changes:
- No broad warning suppression:
- No tag/push/release:
- No AI co-author:
- Git status clean:
```

