# Frontend Phase 23 Worker Prompt — Database Read-Model Consistency

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 23 — Database Read-Model Consistency**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-01-phase-23-database-read-model-consistency.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-01-phase-23-database-read-model-consistency.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-23-database-read-model-consistency
```

Branch:

```text
feat/phase-23-database-read-model-consistency
```

Base it on current frontend `main`, after Phase 22B and its follow-up fixes have
been merged.

## Goal

Add a read-only consistency layer to database detail pages so operators can see
whether members, relations, topology, and profile data agree.

The page should answer:

```text
Can I trust the read-only database picture, and if not, what data is missing or inconsistent?
```

This phase must not add remediation actions.

## Required Deliverables

Implement the plan deliverables:

1. Pure consistency helpers:

```text
lib/database-read-model-consistency.ts
tests/lib/database-read-model-consistency.test.ts
```

Required helpers:

- `buildClusterConsistency`
- `buildInstanceConsistency`

2. Consistency panel:

```text
components/resources/database-consistency-panel.tsx
tests/components/database-consistency-panel.test.tsx
```

3. Instance context panel:

```text
components/resources/database-instance-context-panel.tsx
tests/components/database-instance-context-panel.test.tsx
```

4. Resource detail page wiring:

```text
app/(console)/resources/[id]/page.tsx
tests/resource-detail-page.test.tsx
```

5. i18n:

```text
messages/en.json
messages/zh-CN.json
```

Required namespace:

```text
databaseConsistency
```

6. E2E:

```text
e2e/operator-database-workflow.spec.ts
```

## Product Requirements

### Preserve Phase 22B Layout

Do not undo Phase 22B.

Database detail order must remain:

```text
compact/diagnostic decision deck
resource topology
read-model consistency / instance context
lower supporting details
```

Healthy resources must stay compact. Do not reintroduce a large first-screen
diagnostic wall for healthy resources.

### Cluster Consistency

For database clusters, compare available frontend data:

- `members`
- resolved `relations`
- topology response already used by the page
- member `profileSummary`

Report:

- member missing role
- member missing host/port
- member missing from topology
- topology database instance node missing from member table
- member relation missing when relation data is available

Show a compact OK state when all visible signals agree.

### Instance Consistency

For database instances, show:

- parent cluster present or missing
- role present or missing
- connection present or missing
- instance appears in topology or not

The instance context panel should make these facts easy to read without forcing
the user to scan profile JSON or raw relations.

### Severity

Use read-only data status, not resource health:

- `ok`
- `warning`
- `unknown`

Do not map consistency warnings directly to health status.

### Copy Rules

Chinese copy must be operator-readable and must not use machine-style field
equations.

Allowed examples:

```text
数据一致
需要数据复核
后端未提供角色信息
拓扑未包含该成员
成员关系缺失
```

Forbidden examples:

```text
role=null
health=critical
caused by
根因是
```

## Required Tests

### Unit Tests

Cover:

- healthy cluster with matching members/topology/relations
- cluster member missing role
- cluster member missing connection
- member in table but not topology
- topology database node not in members
- healthy instance with parent cluster, role, and connection
- instance missing role
- instance missing parent cluster
- instance missing connection

### Component Tests

Cover:

- consistency panel OK state
- consistency panel warning issue list
- instance context panel role/host/parent facts
- localized missing-data copy

### E2E

Cover:

- `/resources/14`
  - diagnostic deck still works
  - topology remains directly below deck
  - data consistency panel appears below topology
- `/resources/22`
  - compact deck remains compact
  - no topology button in compact deck
  - instance context panel appears
  - data consistency panel appears
- no console errors
- no CORS errors
- no English leakage in Chinese locale for new copy

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

## Live Browser Verification

Start frontend from the Phase 23 worktree:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

- `http://localhost:3000/resources/14`
  - diagnostic deck visible
  - topology follows deck
  - consistency panel below topology
  - no console errors
- `http://localhost:3000/resources/22`
  - compact deck visible
  - compact deck has no topology button
  - instance context panel visible
  - consistency panel visible
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
- add broad output suppression
- tag, push, or release
- add AI co-author

## Commit Guidance

Use small commits aligned to the implementation plan:

```text
feat: add database read-model consistency helpers
feat: render database consistency panel
feat: add database instance context panel
feat: add database consistency to resource detail
test: cover database read-model consistency workflow
```

Do not squash unless explicitly asked.

## Final Report Requirements

Include:

- worktree path
- branch
- commit list
- files changed
- exact consistency rules implemented
- `/resources/14` live result
- `/resources/22` live result
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

