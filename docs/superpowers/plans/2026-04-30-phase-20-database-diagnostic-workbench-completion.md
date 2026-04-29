# Phase 20 Database Diagnostic Workbench Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the read-only database diagnostic workbench by normalizing diagnostic copy, sorting members by operational priority, explaining missing/unknown states, and adding audit/topology investigation context.

**Architecture:** Add shared pure helpers for status reasons, member priority sorting, and audit context summaries. Reuse them in overview attention queue, database operator workbench, and cluster member table. Keep backend contracts unchanged and rely on existing QA gates.

**Tech Stack:** Next.js App Router, React, existing resource/audit view models, next-intl, Vitest, Playwright, Phase 18C/18D E2E governance.

---

## File Structure

- Create `lib/diagnostic-copy.ts`
  - `buildStatusReasonLabel`
  - `buildStatusReasonSentence`
  - `buildMissingDataLabel`
  - deterministic i18n key helpers
- Modify `lib/database-operator-workbench.ts`
  - natural verdict fact keys
  - member priority sorting helper
  - audit context summary helper
- Modify `components/overview/overview-content.tsx`
  - use normalized attention reason copy
- Modify `components/blocks/cluster-members-table.tsx`
  - sort members by operational priority
  - display explicit missing role/profile/connection text
  - add topology/detail navigation for abnormal members
- Modify `components/resources/database-operator-workbench.tsx`
  - render natural diagnostic facts
  - render audit context summary
  - render expanded topology links
- Modify `messages/en.json` and `messages/zh-CN.json`
  - add diagnostic copy keys
- Tests:
  - `tests/lib/diagnostic-copy.test.ts`
  - `tests/lib/database-operator-workbench.test.ts`
  - `tests/components/cluster-members-table.test.tsx`
  - `tests/components/database-operator-workbench.test.tsx`
  - overview/resource detail tests as needed

---

## 20A — Diagnostic Expression And Ordering

### Task 1: Add Shared Diagnostic Copy Helpers

**Files:**
- Create: `lib/diagnostic-copy.ts`
- Test: `tests/lib/diagnostic-copy.test.ts`

- [ ] **Step 1: Write failing tests**

Create `tests/lib/diagnostic-copy.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  buildMissingDataKey,
  buildStatusReasonKey,
  buildStatusReasonSentenceKey,
} from "@/lib/diagnostic-copy";

describe("diagnostic copy helpers", () => {
  it("builds field/value status reason keys for health status", () => {
    expect(buildStatusReasonKey("healthStatus", "critical")).toEqual({
      fieldKey: "diagnostics.fields.healthStatus",
      valueKey: "statusValues.critical",
      fallbackKey: "diagnostics.reasons.healthStatus.critical",
    });
  });

  it("builds field/value status reason keys for lifecycle status", () => {
    expect(buildStatusReasonKey("lifecycleStatus", "stopped")).toEqual({
      fieldKey: "diagnostics.fields.lifecycleStatus",
      valueKey: "statusValues.stopped",
      fallbackKey: "diagnostics.reasons.lifecycleStatus.stopped",
    });
  });

  it("builds sentence keys for critical health", () => {
    expect(buildStatusReasonSentenceKey("healthStatus", "critical")).toBe(
      "diagnostics.sentences.healthStatus.critical",
    );
  });

  it("builds missing data keys", () => {
    expect(buildMissingDataKey("role")).toBe("diagnostics.missing.role");
    expect(buildMissingDataKey("profile")).toBe("diagnostics.missing.profile");
    expect(buildMissingDataKey("connection")).toBe("diagnostics.missing.connection");
  });
});
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/lib/diagnostic-copy.test.ts
```

Expected: fails because helper does not exist.

- [ ] **Step 3: Implement helper**

Create `lib/diagnostic-copy.ts`:

```ts
export type DiagnosticStatusField = "healthStatus" | "lifecycleStatus";
export type MissingDataKind = "role" | "profile" | "connection" | "audit";

export function buildStatusReasonKey(
  field: DiagnosticStatusField,
  value: string,
) {
  return {
    fieldKey: `diagnostics.fields.${field}`,
    valueKey: `statusValues.${value}`,
    fallbackKey: `diagnostics.reasons.${field}.${value}`,
  };
}

export function buildStatusReasonSentenceKey(
  field: DiagnosticStatusField,
  value: string,
) {
  return `diagnostics.sentences.${field}.${value}`;
}

export function buildMissingDataKey(kind: MissingDataKind) {
  return `diagnostics.missing.${kind}`;
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/lib/diagnostic-copy.test.ts
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add lib/diagnostic-copy.ts tests/lib/diagnostic-copy.test.ts
git commit -m "feat: add diagnostic copy helpers"
```

---

### Task 2: Normalize Attention Queue Reasons

**Files:**
- Modify: `components/overview/overview-content.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: existing overview tests or create/update relevant component tests

- [ ] **Step 1: Add i18n keys**

Add both English and Chinese:

```json
"diagnostics": {
  "fields": {
    "healthStatus": "健康状态",
    "lifecycleStatus": "生命周期状态"
  },
  "reasons": {
    "healthStatus": {
      "critical": "健康状态：严重",
      "warning": "健康状态：告警",
      "unknown": "健康状态：未知"
    },
    "lifecycleStatus": {
      "stopped": "生命周期状态：已停止",
      "degraded": "生命周期状态：降级",
      "pending": "生命周期状态：待处理"
    }
  },
  "sentences": {
    "healthStatus": {
      "critical": "该资源当前处于严重健康状态。",
      "warning": "该资源当前处于告警健康状态。",
      "unknown": "该资源健康状态未知。"
    },
    "lifecycleStatus": {
      "stopped": "该资源生命周期状态为已停止。",
      "degraded": "该资源生命周期状态为降级。",
      "pending": "该资源生命周期状态为待处理。"
    }
  },
  "missing": {
    "role": "后端未提供角色信息",
    "profile": "后端未提供画像信息",
    "connection": "连接地址未提供",
    "audit": "暂无最近审计事件"
  }
}
```

Use equivalent English strings in `messages/en.json`.

- [ ] **Step 2: Replace mechanical reason construction**

Find current code producing strings like:

```text
健康=严重
生命周期=已停止
```

Replace it with a helper that renders:

```text
健康状态：严重
生命周期状态：已停止
```

Do not hardcode Chinese in TypeScript. Use i18n keys.

- [ ] **Step 3: Add tests**

Add or update tests to assert:

- no attention reason contains `=`
- zh-CN critical health reason is `健康状态：严重`
- zh-CN stopped lifecycle reason is `生命周期状态：已停止`
- English reason is `Health status: Critical`

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/components/overview-content.test.tsx tests/lib/diagnostic-copy.test.ts
```

If the overview test file has a different name, use the existing nearest test.

- [ ] **Step 5: Commit**

```bash
git add components/overview/overview-content.tsx messages/en.json messages/zh-CN.json tests
git commit -m "fix: normalize diagnostic status reason copy"
```

---

### Task 3: Add Member Operational Sorting

**Files:**
- Modify: `lib/database-operator-workbench.ts`
- Modify: `components/blocks/cluster-members-table.tsx`
- Test: `tests/lib/database-operator-workbench.test.ts`
- Test: `tests/components/cluster-members-table.test.tsx`

- [ ] **Step 1: Add sorting tests**

Extend `tests/lib/database-operator-workbench.test.ts`:

```ts
import { sortClusterMembersForOperations } from "@/lib/database-operator-workbench";

it("sorts critical primary before warning replica and healthy replica", () => {
  const sorted = sortClusterMembersForOperations([
    member({ id: 3, displayName: "healthy replica", healthStatus: "healthy", lifecycleStatus: "running", profileSummary: { role: "replica" } }),
    member({ id: 2, displayName: "warning replica", healthStatus: "warning", lifecycleStatus: "running", profileSummary: { role: "replica" } }),
    member({ id: 1, displayName: "critical primary", healthStatus: "critical", lifecycleStatus: "running", profileSummary: { role: "primary" } }),
  ]);

  expect(sorted.map((m) => m.displayName)).toEqual([
    "critical primary",
    "warning replica",
    "healthy replica",
  ]);
});

it("sorts stopped members before healthy running members", () => {
  const sorted = sortClusterMembersForOperations([
    member({ id: 1, displayName: "healthy", healthStatus: "healthy", lifecycleStatus: "running" }),
    member({ id: 2, displayName: "stopped", healthStatus: "healthy", lifecycleStatus: "stopped" }),
  ]);

  expect(sorted[0].displayName).toBe("stopped");
});
```

- [ ] **Step 2: Implement sorting helper**

In `lib/database-operator-workbench.ts`, add:

```ts
function healthPriority(status: string) {
  if (status === "critical") return 0;
  if (status === "warning") return 1;
  if (status === "unknown") return 2;
  return 3;
}

function lifecyclePriority(status: string) {
  if (status === "stopped") return 0;
  if (status === "degraded") return 1;
  if (status === "pending") return 2;
  return 3;
}

function rolePriority(role: string) {
  const normalized = role.toLowerCase();
  if (["primary", "master", "writer"].includes(normalized)) return 0;
  if (["replica", "secondary", "reader"].includes(normalized)) return 1;
  return 2;
}

export function sortClusterMembersForOperations<T extends ClusterMember>(
  members: T[],
): T[] {
  return [...members].sort((left, right) => {
    return (
      healthPriority(left.healthStatus) - healthPriority(right.healthStatus) ||
      lifecyclePriority(left.lifecycleStatus) - lifecyclePriority(right.lifecycleStatus) ||
      rolePriority(left.profileSummary?.role ?? "") - rolePriority(right.profileSummary?.role ?? "") ||
      left.displayName.localeCompare(right.displayName)
    );
  });
}
```

- [ ] **Step 3: Use sorting in member table**

In `components/blocks/cluster-members-table.tsx`, sort the input before
rendering:

```ts
const sortedMembers = sortClusterMembersForOperations(members);
```

Render `sortedMembers` instead of `members`.

- [ ] **Step 4: Add component assertion**

Update cluster member table tests to assert row order puts abnormal members
first.

- [ ] **Step 5: Run tests**

Run:

```bash
npm run test -- tests/lib/database-operator-workbench.test.ts tests/components/cluster-members-table.test.tsx
```

- [ ] **Step 6: Commit**

```bash
git add lib/database-operator-workbench.ts components/blocks/cluster-members-table.tsx tests
git commit -m "feat: sort cluster members by operational priority"
```

---

### Task 4: Improve Missing And Unknown State Copy

**Files:**
- Modify: `components/blocks/cluster-members-table.tsx`
- Modify: `components/resources/database-operator-workbench.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Tests: component tests

- [ ] **Step 1: Add tests for missing data**

Assert:

- missing role shows `后端未提供角色信息` in zh-CN test setup or equivalent key in mocked translator
- missing host/port shows `连接地址未提供`
- missing audits shows `暂无最近审计事件`

- [ ] **Step 2: Implement missing state rendering**

Do not render blank cells for role/profile/connection. Render explicit muted
text using diagnostics missing keys.

- [ ] **Step 3: Run tests**

Run:

```bash
npm run test -- tests/components/cluster-members-table.test.tsx tests/components/database-operator-workbench.test.tsx
```

- [ ] **Step 4: Commit**

```bash
git add components/blocks/cluster-members-table.tsx components/resources/database-operator-workbench.tsx messages/en.json messages/zh-CN.json tests
git commit -m "fix: explain missing database diagnostic data"
```

---

## 20B — Diagnostic Navigation Context

### Task 5: Add Audit Context Summary

**Files:**
- Modify: `lib/database-operator-workbench.ts`
- Modify: `components/resources/database-operator-workbench.tsx`
- Tests: helper + component tests

- [ ] **Step 1: Add helper tests**

Add tests:

```ts
import { buildAuditContextSummary } from "@/lib/database-operator-workbench";

it("summarizes missing audits", () => {
  expect(buildAuditContextSummary([])).toEqual({
    count: 0,
    summaryKey: "diagnostics.audit.none",
    hasResourceChange: false,
  });
});

it("summarizes resource change audits", () => {
  expect(buildAuditContextSummary([
    { eventType: "resource.updated" },
    { eventType: "relation.created" },
  ] as any)).toEqual({
    count: 2,
    summaryKey: "diagnostics.audit.resourceChanges",
    hasResourceChange: true,
  });
});
```

- [ ] **Step 2: Implement helper**

```ts
export function buildAuditContextSummary(
  audits: Array<{ eventType: string }>,
) {
  if (audits.length === 0) {
    return {
      count: 0,
      summaryKey: "diagnostics.audit.none",
      hasResourceChange: false,
    };
  }

  const hasResourceChange = audits.some((event) =>
    event.eventType.startsWith("resource.") ||
    event.eventType.startsWith("relation."),
  );

  return {
    count: audits.length,
    summaryKey: hasResourceChange
      ? "diagnostics.audit.resourceChanges"
      : "diagnostics.audit.recentEvents",
    hasResourceChange,
  };
}
```

- [ ] **Step 3: Render summary**

In workbench recent audit panel:

- show summary before audit rows
- show empty state when no recent audits
- link to `/audits?targetResourceId={resource.id}`

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/lib/database-operator-workbench.test.ts tests/components/database-operator-workbench.test.tsx
```

- [ ] **Step 5: Commit**

```bash
git add lib/database-operator-workbench.ts components/resources/database-operator-workbench.tsx messages/en.json messages/zh-CN.json tests
git commit -m "feat: summarize recent audit context"
```

---

### Task 6: Add Topology Navigation For Abnormal Members

**Files:**
- Modify: `components/blocks/cluster-members-table.tsx`
- Tests: `tests/components/cluster-members-table.test.tsx`
- Optional E2E: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Add helper or inline predicate**

Abnormal member definition:

```ts
function isAbnormalMember(member: ClusterMember) {
  return (
    member.healthStatus === "critical" ||
    member.healthStatus === "warning" ||
    member.healthStatus === "unknown" ||
    member.lifecycleStatus === "stopped" ||
    member.lifecycleStatus === "degraded"
  );
}
```

- [ ] **Step 2: Render topology link for abnormal members**

For abnormal members, render a link:

```ts
href={`/resources/${member.resourceId}?topologyDepth=2&topologyExpanded=1`}
```

Label:

- zh-CN: `查看拓扑`
- en: `View topology`

Do not implement node highlighting in this phase.

- [ ] **Step 3: Add component test**

Assert abnormal member row has topology link and healthy member does not.

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/components/cluster-members-table.test.tsx
```

- [ ] **Step 5: Commit**

```bash
git add components/blocks/cluster-members-table.tsx messages/en.json messages/zh-CN.json tests/components/cluster-members-table.test.tsx
git commit -m "feat: add topology entry for abnormal database members"
```

---

### Task 7: Full Verification And Live Review

**Files:**
- No further code changes expected.

- [ ] **Step 1: Run full static and unit checks**

Run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Expected: all pass.

- [ ] **Step 2: Run E2E gates**

Backend must be running on `:8080`.

Run:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

Expected: all pass.

- [ ] **Step 3: Live browser verification**

Run frontend on `3000`:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

```text
http://localhost:3000/overview
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Check:

- overview attention queue does not show `健康=` or `生命周期=`
- cluster detail shows natural diagnostic facts
- cluster member table sorts abnormal members first
- missing role/profile/connection states are explicit
- recent audit context summary appears
- abnormal member topology link opens expanded topology
- no CORS errors
- no browser console errors

- [ ] **Step 4: Final status**

Run:

```bash
git status --short --branch
git log --oneline -8
```

Expected: clean working tree on `feat/phase-20-database-diagnostic-workbench`.

---

## Final Verification Matrix

Before final report, run:

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

All commands must pass.

## Final Report Requirements

Report:

- worktree path, branch, commit hashes
- files changed
- status reason copy behavior
- member sorting behavior
- diagnostic summary behavior
- audit context behavior
- topology navigation behavior
- missing/unknown state behavior
- live browser verification for overview, one cluster, one instance
- full verification matrix
- no backend changes
- no product write operations
- no SQL execution
- no work orders
- no topology editing
- no broad output suppression
- no tag/push/release
- no AI co-author

