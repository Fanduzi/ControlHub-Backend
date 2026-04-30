# Frontend Phase 21 Worker Prompt — Database Diagnostic Runbook

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 21 — Database Diagnostic Runbook**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-30-phase-21-database-diagnostic-runbook.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-30-phase-21-database-diagnostic-runbook.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-21-database-diagnostic-runbook
```

Branch:

```text
feat/phase-21-database-diagnostic-runbook
```

Base it on current frontend `main`, after Phase 20 has been merged.

## Goal

Turn the Phase 20 read-only database diagnostic summary into a read-only
diagnostic runbook.

The database detail page should help an operator answer:

- what facts caused the verdict
- where those facts came from
- what should be checked next
- which audit events are nearby resource/relation changes
- which backend data is missing and what that means

This phase is still read-only. Do not add remediation actions.

## Required Deliverables

Implement:

1. Pure helper module:

```text
lib/database-diagnostic-runbook.ts
```

Required helpers:

- `buildDiagnosticEvidence`
- `buildRunbookChecks`
- `buildAuditBuckets`

2. Workbench rendering:

```text
components/resources/database-operator-workbench.tsx
```

Required sections:

- diagnostic evidence
- next-check runbook
- grouped audit context
- cautious causality notice for nearby resource/relation changes

3. i18n:

```text
messages/en.json
messages/zh-CN.json
```

Required namespaces under `databaseOperator`:

- `evidence`
- `runbook`
- `auditBuckets`

4. Tests:

```text
tests/lib/database-diagnostic-runbook.test.ts
tests/components/database-operator-workbench.test.tsx
```

5. Optional E2E update if existing workflow does not cover the new visible
sections:

```text
e2e/operator-database-workflow.spec.ts
```

## Product Requirements

### Diagnostic Evidence

Each evidence item must show:

- localized user-facing title
- source label
- severity
- raw field hint

Examples:

```text
资源健康状态为严重。
来源：资源状态
字段：healthStatus=critical
```

```text
1 个成员处于告警或严重状态。
来源：成员健康
字段：members[].healthStatus
```

Raw field hints are allowed only as secondary metadata. Do not use raw field
dump as the main user-facing copy.

### Next-Check Runbook

Show deterministic read-only next checks based on available evidence.

Examples:

```text
检查实例进程状态、连接地址和最近资源变更。
确认停止或降级状态是否来自计划维护或最近变更。
检查后端画像同步是否提供角色、主机和端口数据。
对照最近资源或关系变更，确认是否与当前信号时间接近。
```

Do not use copy that implies the console can execute remediation:

```text
修复
执行
重启
切主
自动恢复
```

### Audit Buckets

Group recent audits by event type:

- `resource.*` → resource changes
- `relation.*` → relation changes
- all other event types → other events

Show summary:

```text
最近 5 条审计事件：2 条资源变更，1 条关系变更，2 条其他操作。
```

If resource or relation changes exist, show cautious notice:

```text
这些事件只表示时间邻近的变更，不代表已确认根因。
```

Never claim causality. Do not write "caused by", "root cause", "导致", or
"根因" as a confirmed statement.

### Empty And Unknown States

Keep explicit missing-data copy:

```text
后端未提供角色信息
后端未提供画像信息
连接地址未提供
暂无最近审计事件
当前没有明确异常信号，继续查看拓扑和审计历史。
```

Do not render blank cells or bare `unknown` as an explanation.

## Must Preserve From Phase 20

Do not regress:

- no `健康=严重` or `生命周期=已停止` style copy
- overview attention queue still uses normalized reason copy
- cluster member sorting remains operational-priority based
- abnormal members still have `查看拓扑` link
- audit "view all" link remains available
- topology links still use:

```text
?topologyDepth=2&topologyExpanded=1
```

## Constraints

Do not:

- modify backend code
- change backend API contracts
- add SQL execution
- add work orders
- add write actions
- edit topology layout
- add topology node highlighting
- restore `/cmdb` navigation
- restore demo `resourceSummaries`
- add broad output suppression
- tag, push, release
- add AI co-author attribution

If backend data is missing, display an explicit unavailable state. Do not fake
metrics, causes, replication lag, disk usage, backup status, QPS, or slow query
counts.

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

Verify:

```text
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Expected:

- `/resources/14` shows verdict, diagnostic evidence, next checks, audit bucket
  context, member summary, and topology links
- `/resources/14` shows causality notice only when resource/relation audit
  changes exist
- `/resources/22` shows healthy/unknown evidence appropriately and does not
  duplicate parent cluster or connection panels
- no blank missing-data cells
- no `健康=严重`, `生命周期=已停止`, `Health=`, or `Lifecycle=` patterns
- no CORS errors
- no browser console errors
- API requests use `/__api`

## Commit Requirements

Commit all intended changes. Suggested commit messages:

```text
feat: add database diagnostic runbook helpers
feat: add database diagnostic runbook copy
feat: render diagnostic evidence and next checks
feat: group database audit context
test: cover database diagnostic runbook workflow
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
## Phase 21 Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Diagnostic Evidence
| Evidence | Source | Raw Hint | Status |
|---|---|---|---|

### Runbook Checks
| Condition | Check Copy | Status |
|---|---|---|

### Audit Context
| Bucket | Behavior |
|---|---|
| Resource changes | |
| Relation changes | |
| Other events | |
| Causality notice | |

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

Do not claim completion until all required commands and live browser checks
pass.

