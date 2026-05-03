# Frontend Phase 25 Worker Prompt — Database Detail Semantic Cleanup

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 25 — Database Detail Semantic Cleanup**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-03-phase-25-database-detail-semantic-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-03-phase-25-database-detail-semantic-cleanup.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-25-database-detail-semantic-cleanup
```

Branch:

```text
feat/phase-25-database-detail-semantic-cleanup
```

Base it on current frontend `main`, after Phase 24 has been merged.

Before editing, report:

```bash
git worktree list
git status --short --branch
git log --oneline -3
```

If the worktree already exists, verify it is on the correct branch and clean
before using it.

## Goal

Fix the semantic contradictions found in live database detail review:

1. A database cluster can show `健康` in the list but `需关注` in detail without
   explaining that these are different subjects.
2. `数据一致` appears near member health problems and reads like health
   consistency.
3. `审计上下文` and `审计历史` duplicate each other and disagree about whether
   audit events exist.
4. Instance `所属集群` in the facts panel is plain text but should navigate to
   the cluster detail page.
5. Audit history is squeezed into a half-width card.
6. Raw field hints such as `字段: members[].healthStatus` are too prominent.

This phase is about **semantic clarity and information architecture**, not new
backend data or new operations.

## Required Live Problem References

Use these pages as the live examples:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Observed problems to eliminate:

```text
健康 / 运行中 + 需关注 + 严重 member appears contradictory.
审计上下文 says "该资源最近 5 条审计事件。" and then "暂无最近审计事件。"
审计历史 shows "No audit activity yet" on Chinese page.
所属集群 in 实例上下文与一致性 is not clickable.
审计历史 is half width under 支撑明细.
```

## Required Deliverables

Implement the plan deliverables:

1. Decision deck semantic clarity:

```text
components/resources/database-decision-deck.tsx
tests/components/database-decision-deck.test.tsx
messages/en.json
messages/zh-CN.json
```

2. Read-model consistency copy:

```text
components/resources/database-consistency-panel.tsx
tests/components/database-consistency-panel.test.tsx
messages/en.json
messages/zh-CN.json
```

3. Parent cluster navigation:

```text
lib/database-read-model-consistency.ts
components/resources/database-instance-facts-panel.tsx
tests/lib/database-read-model-consistency.test.ts
tests/components/database-instance-facts-panel.test.tsx
tests/resource-detail-page.test.tsx
messages/en.json
messages/zh-CN.json
```

4. Audit context/history cleanup:

```text
components/resources/database-operator-workbench.tsx
components/blocks/activity-timeline.tsx
tests/components/database-operator-workbench.test.tsx
tests/components/activity-timeline.test.tsx
messages/en.json
messages/zh-CN.json
```

5. Supporting details layout:

```text
components/resources/database-supporting-details.tsx
app/(console)/resources/[id]/page.tsx
tests/components/database-supporting-details.test.tsx
tests/resource-detail-page.test.tsx
e2e/operator-database-workflow.spec.ts
```

6. Database list status semantics:

```text
components/databases/database-table.tsx
tests/components/database-table.test.tsx
messages/en.json
messages/zh-CN.json
```

## Product Requirements

### Status Subject Clarity

The UI must clearly distinguish:

```text
运维判定 / Operator verdict
资源自身状态 / Resource status
成员信号 / Member signal
```

For `/resources/14`, the user must understand:

```text
Operator verdict: needs attention.
Resource self status: healthy/running.
Reason: one member is warning or critical.
```

Do not display unlabeled `健康` and `需关注` as same-level badges.

### Read-Model Consistency

Rename user-facing `数据一致性` semantics to read-model consistency:

```text
读模型一致性
读模型一致
读模型需检查
Read-model consistency
Read-model consistent
Read-model needs review
```

Do not imply that read-model consistency means health is healthy.

### Audit Context vs Audit History

Audit context:

- summary only
- no individual event rows
- no fixed "recent 5 events" description
- no root-cause overclaim

Audit history:

- the only detailed event list
- full width in supporting details
- localized empty state

### Parent Cluster Link

In `实例上下文与一致性`, parent cluster must be a link when id is available:

```text
Analytics ClickHouse Cluster Production -> /resources/14
```

If id is missing but name exists, render plain text.

If missing, render explicit copy:

```text
后端未提供所属集群信息
Parent cluster not provided by backend
```

### Supporting Details Layout

Desktop layout:

```text
运行画像 | 关系
审计历史 full width
```

Mobile layout:

```text
运行画像
关系
审计历史
```

### Raw Field Hints

Raw hints like:

```text
字段: members[].healthStatus
```

must not appear in the visible decision deck. They may remain only inside
collapsed diagnostic details.

### Database List

On `/databases?environment=prod`, cluster rows must not read as if the whole
database is fine when the detail page has a member-derived attention verdict.

Minimum acceptable fix:

- make status column clearly mean resource self status:
  `资源自身状态` / `Resource status`

Preferred if existing row data supports it without new API calls:

- add derived member signal badge:
  `成员严重 1` / `1 critical member`

Do not add per-row API calls.

## Forbidden Regressions

Do not reintroduce:

```text
DatabaseInstanceContextPanel
databaseConsistency.instanceContext
健康=严重
该资源最近 5 条审计事件
No audit activity yet on Chinese page
Recent resource changes will appear here... on Chinese page
generic 数据一致 as health-like copy
full-page tabs
compact healthy deck topology button
duplicated parent cluster / connection full cards
```

## Required Tests

Add/update tests for:

- decision deck subject labels
- raw field hint removed from visible deck
- read-model consistency copy
- parent cluster link
- explicit missing parent cluster copy
- audit context summary-only behavior
- localized audit timeline empty state
- supporting details full-width audit history
- database list status semantics
- `/resources/14` E2E semantic clarity
- `/resources/22` E2E parent cluster link and audit empty state

## Required Verification

Run:

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

Run forbidden text audit:

```bash
rg -n "健康=|No audit activity yet|Recent resource changes will appear|该资源最近 5 条审计事件|字段: members\\[\\]\\.healthStatus|数据一致" app components messages tests e2e
```

If `字段: members[].healthStatus` remains only in collapsed diagnostic details
tests/copy, report that explicitly.

## Live Browser Verification

Start backend and frontend if not already running, then verify:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Required live observations:

- `/resources/14`: no contradiction between healthy resource status and needs-attention verdict.
- `/resources/14`: read-model consistency does not read as health consistency.
- `/resources/14`: audit context is summary-only.
- `/resources/22`: parent cluster in facts panel is clickable and goes to `/resources/14`.
- `/resources/22`: no "recent 5 events" claim when there are no events.
- `/resources/22`: audit history empty state is localized.
- audit history is full width.
- no browser console errors.
- API calls still use `/__api`.

## Scope Constraints

- No backend changes.
- No API contract changes.
- No SQL.
- No work orders.
- No write operations.
- No topology layout editing.
- No full-page tabs.
- No broad output suppression.
- No tag/push/release.
- No AI co-author.

## Commit Guidance

Use small commits aligned with the plan tasks:

```text
fix: clarify database detail status semantics
fix: clarify read-model consistency copy
fix: link database instance parent cluster
fix: deduplicate database audit context
fix: make database audit history full width
fix: clarify database list status semantics
```

Final report must include:

1. Worktree path, branch, commit list.
2. Files changed.
3. Each live issue and exact fix.
4. Verification matrix.
5. Forbidden text audit results.
6. Live browser verification results for `/databases`, `/resources/14`, `/resources/22`.
7. Scope confirmation.

