# Frontend Phase 20 Worker Prompt — Database Diagnostic Workbench Completion

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 20 — Database Diagnostic Workbench Completion**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-30-phase-20-database-diagnostic-workbench-completion.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-30-phase-20-database-diagnostic-workbench-completion.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-20-database-diagnostic-workbench
```

Branch:

```text
feat/phase-20-database-diagnostic-workbench
```

Base it on current frontend `main`, after Phase 19 has been merged.

## Goal

Complete the read-only database diagnostic experience:

- replace machine-like reasons such as `健康=严重`
- sort database members by operational importance
- show natural diagnostic summary facts
- explain unknown/missing role/profile/connection data
- summarize recent audit context without claiming false causality
- provide stable topology navigation for abnormal members

## Required Deliverables

### 20A — Diagnostic expression and ordering

Implement:

- `lib/diagnostic-copy.ts`
- normalized attention queue reason copy
- natural workbench verdict facts
- member operational sorting
- explicit missing/unknown states

### 20B — Diagnostic navigation context

Implement:

- recent audit context summary
- full audit link with `targetResourceId`
- abnormal member topology links:
  - `/resources/{memberId}?topologyDepth=2&topologyExpanded=1`

Do not implement topology node highlighting unless it is small, deterministic,
and fully tested. If risky, explicitly defer it.

## Must Fix

Current bad copy must disappear from affected surfaces:

```text
健康=严重
生命周期=已停止
健康=告警
生命周期=降级
```

Use:

```text
健康状态：严重
生命周期状态：已停止
```

or natural sentences:

```text
该资源当前处于严重健康状态。
存在 1 个成员处于告警或严重状态。
后端未提供角色信息。
```

## Constraints

Do not:

- modify backend code
- change backend API contracts
- add SQL execution
- add work orders
- add write actions
- edit topology layout
- add unstable topology node highlighting
- restore `/cmdb` navigation
- restore demo `resourceSummaries`
- add broad output suppression
- tag, push, release
- add AI co-author attribution

If backend data is missing, display an explicit unavailable state. Do not fake
metrics or causes.

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
http://localhost:3000/overview
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Expected:

- overview attention queue does not show `健康=` or `生命周期=`
- database cluster detail has natural diagnostic facts
- member table puts abnormal members first
- missing role/profile/connection text is explicit
- recent audit context summary appears
- abnormal member topology link opens expanded topology
- no CORS errors
- no browser console errors

## Commit Requirements

Commit all intended changes. Suggested commit messages:

```text
feat: add diagnostic copy helpers
fix: normalize database diagnostic reason copy
feat: sort database members by operational priority
feat: add audit and topology diagnostic context
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
## Phase 20 Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Diagnostic Copy
| Before | After |
|---|---|

### Member Sorting
| Rule | Status |
|---|---|

### Diagnostic Context
| Area | Behavior |
|---|---|
| Audit context | |
| Topology links | |
| Missing data | |

### Live Verification
| Page | Result |
|---|---|
| /overview | |
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

