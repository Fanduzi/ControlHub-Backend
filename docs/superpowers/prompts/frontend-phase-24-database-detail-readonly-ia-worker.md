# Frontend Phase 24 Worker Prompt — Database Detail Read-Only IA Closure

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 24 — Database Detail Read-Only IA Closure**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-02-phase-24-database-detail-readonly-ia.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-02-phase-24-database-detail-readonly-ia.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Visual Reference

Use the approved static preview for layout intent:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase24-db-detail-readonly-ia/content/index.html
```

The preview is not product code. It defines the intended information hierarchy:

```text
decision deck
topology
context/consistency
supporting details
```

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-24-database-detail-readonly-ia
```

Branch:

```text
feat/phase-24-database-detail-readonly-ia
```

Base it on current frontend `main`, after Phase 23 has been merged.

Before editing, report:

```bash
git worktree list
git status --short --branch
git log --oneline -3
```

If the worktree already exists, verify it is on the correct branch and clean
before using it.

## Goal

Reduce duplicate cards on database detail pages and turn the current read-only
operator view into a stable three-layer page:

1. first-screen judgement
2. topology and trust
3. supporting details

This phase is about **de-duplication and information architecture**, not new
backend data or new operations.

## Required Deliverables

Implement the plan deliverables:

1. Merged instance facts panel:

```text
components/resources/database-instance-facts-panel.tsx
tests/components/database-instance-facts-panel.test.tsx
```

2. Database supporting details wrapper:

```text
components/resources/database-supporting-details.tsx
tests/components/database-supporting-details.test.tsx
```

3. Resource detail page composition:

```text
app/(console)/resources/[id]/page.tsx
tests/resource-detail-page.test.tsx
```

4. i18n:

```text
messages/en.json
messages/zh-CN.json
```

Required namespace:

```text
databaseReadonlyIA
```

5. E2E:

```text
e2e/operator-database-workflow.spec.ts
```

## Product Requirements

### Preserve Top Structure

Do not undo Phase 22B / 23.

Database detail top order must remain:

```text
compact/diagnostic decision deck
resource topology
```

Do not move topology down. Do not put topology behind tabs.

### Merge Duplicate Instance Cards

For database instances, replace the repeated high-priority cards with one merged
panel:

```text
Instance context and consistency
```

It must include:

- parent cluster
- role
- connection
- topology presence
- consistency status
- issue list when consistency warnings exist

The old full-card sections should not still appear immediately below it:

- `Parent cluster`
- `Connection info`

Raw profile remains available in supporting details.

### Keep Healthy Pages Quiet

Healthy database resources must stay compact:

- no topology button inside compact deck
- no top evidence / next checks / abnormal members
- no repeated parent cluster / connection cards
- no `0 members`
- no `0 个成员`

### Keep Abnormal Pages Diagnostic

Abnormal or unknown database resources must still show:

- diagnostic deck
- top evidence
- next checks
- abnormal members
- topology
- consistency issues

Do not weaken abnormal workflows while simplifying healthy pages.

### Supporting Details

Introduce a database-only supporting details area below the operator view.

It should contain:

- raw profile / operational profile
- relations
- audit history
- cluster member table when it is not the primary diagnostic focus

Do not remove information. Only lower its visual priority and reduce repeated
facts.

## Copy Rules

Chinese copy must be clear operator copy.

Allowed:

```text
实例上下文与一致性
支撑明细
后端未提供
该实例出现在拓扑中
```

Forbidden:

```text
role=null
health=critical
caused by
根因是
```

## Required Tests

### Unit / Component Tests

Cover:

- merged instance facts panel renders parent cluster, role, connection,
  topology, and consistency status
- missing facts show localized missing copy
- consistency issues render in the merged panel
- supporting details wrapper renders children
- healthy compact deck still has no topology button

### Page Tests

Cover:

- database instance page renders `Instance context and consistency`
- database instance page does not render duplicate `Parent cluster` heading
- database instance page does not render duplicate `Connection info` heading
- database instance page does not render `0 members` or `0 个成员`
- database cluster page still renders diagnostic deck and cluster members
- supporting details section appears on database detail pages

### E2E

Cover:

- `/resources/22`
  - compact deck visible
  - topology follows deck
  - merged instance context and consistency panel appears
  - old parent cluster and connection full cards are gone
  - supporting details section exists
  - no `0 members`
- `/resources/14`
  - diagnostic deck visible
  - topology follows deck
  - data consistency panel visible
  - cluster members still available
  - supporting details section exists
- no console errors
- no CORS errors
- API requests go through `/__api`

## Verification Commands

Run before final report:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If backend is not running on `:8080`, start it from:

```text
/Users/fan/GolangProjects/ControlHub
```

with:

```bash
go run ./cmd/server
```

Stop any backend or frontend process you start for verification.

If E2E fails because port `3000` is already occupied:

```bash
lsof -nP -iTCP:3000 -sTCP:LISTEN
```

Only stop the process if you started it or can prove it is the Phase 24 dev
server. Do not kill unknown user processes.

## Live Browser Verification

Start frontend from the Phase 24 worktree:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

- `http://localhost:3000/resources/22`
  - compact deck remains compact
  - topology follows deck
  - merged instance context and consistency panel appears
  - old parent cluster and connection full cards are gone
  - supporting details section exists
  - no `0 members`
  - no console errors
- `http://localhost:3000/resources/14`
  - diagnostic deck remains diagnostic
  - topology follows deck
  - data consistency panel appears
  - cluster members remain available
  - supporting details section exists
  - no console errors

## Scope Constraints

Do not:

- change backend code
- change API contracts
- execute SQL
- add work orders
- add write operations
- edit topology
- add full-page tabs
- move topology below supporting details
- add broad output suppression
- tag, push, or release
- add AI co-author

## Commit Guidance

Use small commits aligned to the implementation plan:

```text
feat: add database instance facts panel
refactor: merge duplicate database instance detail panels
feat: group database supporting details
test: cover database detail read-only IA
```

Do not squash unless explicitly asked.

## Final Report Requirements

Include:

- worktree path
- branch
- commit list
- files changed
- exact duplicate sections removed
- `/resources/22` live result
- `/resources/14` live result
- full verification matrix
- E2E results
- `git status --short --branch`
- scope confirmation:
  - no backend changes
  - no API contract changes
  - no SQL
  - no work orders
  - no write operations
  - no topology editing
  - no tag/push/release
  - no AI co-author

