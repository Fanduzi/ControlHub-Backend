# Frontend Phase 19 Worker Prompt — Database Operator Workbench

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 19 — Database Operator Workbench**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-29-phase-19-database-operator-workbench.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-29-phase-19-database-operator-workbench.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-19-database-operator-workbench
```

Branch:

```text
feat/phase-19-database-operator-workbench
```

Base it on current frontend `main`, after Phase 18D has been merged.

## Goal

Improve database cluster and instance detail pages so a DBA can quickly judge:

- current health verdict
- member health/role distribution
- primary/replica/unknown role context
- stopped/degraded/critical member count
- connection facts
- parent cluster context
- recent scoped audit events
- topology entry point

This phase is read-only. Do not add remediation, SQL execution, work orders, or
topology editing.

## Required Deliverables

Implement:

1. Pure helper module:

```text
lib/database-operator-workbench.ts
```

2. UI component:

```text
components/resources/database-operator-workbench.tsx
```

3. Resource detail wiring:

```text
app/(console)/resources/[id]/page.tsx
lib/view-models.ts
types/view-models.ts
```

4. i18n:

```text
messages/en.json
messages/zh-CN.json
```

5. Tests:

```text
tests/lib/database-operator-workbench.test.ts
tests/components/database-operator-workbench.test.tsx
tests/resource-detail-page.test.tsx
```

6. E2E update if stable:

```text
e2e/operator-database-workflow.spec.ts
```

## Product Requirements

### Cluster Detail

Show:

- operator verdict
- total members
- primary count
- replica count
- role unknown count
- warning/critical count
- stopped/degraded count
- member table with role, host, port, version, health, lifecycle
- clear expanded topology entry
- up to 5 recent audit events scoped to the cluster

### Instance Detail

Show:

- operator verdict
- parent cluster link if available
- engine/version/host/port/role/readOnly if available
- health/lifecycle explanation
- recent audit events scoped to the instance
- topology entry

### Copy And Localization

No enum title-case leaks in Chinese.

Use Chinese labels for:

- 健康
- 需关注
- 严重
- 未知
- 主库
- 从库
- 角色未知
- 最近审计

Do not invent metrics that backend does not provide.

## Constraints

Do not:

- modify backend code
- change backend API contracts
- add SQL execution
- add work orders
- add write actions
- edit topology layout
- restore `/cmdb` navigation
- restore demo `resourceSummaries`
- add broad output suppression
- tag, push, release
- add AI co-author attribution

If backend data is missing, display unavailable state and report the gap. Do not
fake data.

## Required Commands

Run all:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
git status --short --branch
```

Manual browser verification on `3000` is required:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify at least:

```text
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Expected:

- cluster detail shows operator verdict and member summary
- instance detail shows parent cluster and connection context
- topology still loads
- no CORS errors
- no browser console errors

## Commit Requirements

Commit all intended changes. Suggested commit messages:

```text
feat: add database operator verdict helpers
feat: add database operator workbench detail panel
feat: show recent audit context on database details
```

Do not include:

```text
Co-Authored-By
Claude
Anthropic
AI
```

## Final Report Format

Return:

```markdown
## Phase 19 Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Operator Workbench Behavior
| Area | Behavior |
|---|---|
| Cluster verdict | |
| Member summary | |
| Instance detail | |
| Recent audits | |
| Topology entry | |

### Live Verification
| Page | Result |
|---|---|
| /resources/14 | |
| /resources/22 | |

### Verification
| Command | Result |
|---|---|

### Scope Confirmation
- No backend changes:
- No product write operations:
- No SQL execution:
- No work orders:
- No topology editing:
- No broad output suppression:
- No tag/push/release:
- No AI co-author:
- git status:
```

Do not claim completion until all required commands and live browser checks pass.

