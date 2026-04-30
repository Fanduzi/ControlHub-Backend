# Phase 21 Database Diagnostic Runbook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only diagnostic runbook to database detail pages: structured evidence, next checks, grouped audit context, and explicit missing-data explanations.

**Architecture:** Extend the existing Phase 19/20 database operator workbench with pure helper functions for evidence, runbook items, and audit buckets. Render those helpers inside existing `DetailPanel` sections and keep all backend contracts unchanged.

**Tech Stack:** Next.js App Router, React, TypeScript, next-intl, existing resource/audit view models, Vitest, Playwright, Phase 18C E2E governance.

---

## File Structure

- Create `lib/database-diagnostic-runbook.ts`
  - Pure helper functions:
    - `buildDiagnosticEvidence`
    - `buildRunbookChecks`
    - `buildAuditBuckets`
  - No React, no i18n calls, no API calls.
- Modify `components/resources/database-operator-workbench.tsx`
  - Render diagnostic evidence.
  - Render next-check runbook.
  - Render grouped audit context.
- Modify `messages/en.json`
  - Add `databaseOperator.evidence`, `databaseOperator.runbook`, and audit bucket labels.
- Modify `messages/zh-CN.json`
  - Add matching Chinese keys.
- Add `tests/lib/database-diagnostic-runbook.test.ts`
  - Unit tests for evidence, runbook, and audit buckets.
- Modify `tests/components/database-operator-workbench.test.tsx`
  - Component coverage for rendered evidence/runbook/audit bucket UI.
- Optional E2E update:
  - Modify `e2e/operator-database-workflow.spec.ts` only if current smoke/workflow tests do not cover the new visible sections.

## Phase Constraints

- Frontend only.
- No backend code.
- No SQL execution.
- No work orders.
- No write operations.
- No topology editing.
- No causal claims from audit events.
- No AI co-author in commits.

---

## Task 1: Add Diagnostic Runbook Helper Tests

**Files:**
- Create: `tests/lib/database-diagnostic-runbook.test.ts`
- Create later: `lib/database-diagnostic-runbook.ts`

- [ ] **Step 1: Write failing tests**

Create `tests/lib/database-diagnostic-runbook.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  buildAuditBuckets,
  buildDiagnosticEvidence,
  buildRunbookChecks,
} from "@/lib/database-diagnostic-runbook";
import type { ClusterMember } from "@/types/resource";
import type { AuditEventViewModel, ResourceDetailViewModel } from "@/types/view-models";

function resource(
  overrides: Partial<ResourceDetailViewModel> = {},
): ResourceDetailViewModel {
  return {
    id: 14,
    resourceType: "database_cluster",
    resourceSubtype: "mysql",
    name: "payment-mysql-cluster-prod",
    displayName: "Payment MySQL Cluster Production",
    environmentId: 1,
    ownerId: 1,
    lifecycleStatus: "running",
    healthStatus: "healthy",
    source: "seed",
    externalId: "dbaas-payment-mysql-cluster-prod",
    labels: {},
    createdAt: "",
    updatedAt: "",
    archivedAt: null,
    archivedBy: null,
    archiveReason: null,
    environmentName: "Production",
    ownerName: "DBA Team",
    summary: "",
    isArchived: false,
    profile: {},
    relations: [],
    auditEvents: [],
    recentAudits: [],
    members: [],
    ...overrides,
  };
}

function member(overrides: Partial<ClusterMember> = {}): ClusterMember {
  return {
    id: 22,
    name: "payment-mysql-primary-prod",
    displayName: "Payment MySQL Primary Production",
    resourceType: "database_instance",
    resourceSubtype: "mysql",
    lifecycleStatus: "running",
    healthStatus: "healthy",
    profileSummary: {
      role: "primary",
      hostname: "prod-db-host-02.internal",
      port: 3307,
    },
    ...overrides,
  };
}

function audit(eventType: string): AuditEventViewModel {
  return {
    id: Math.floor(Math.random() * 100000),
    actorUserId: 1,
    targetResourceId: 14,
    eventType,
    result: "success",
    createdAt: "2026-04-30T10:00:00Z",
    actorLabel: "admin",
    targetResourceName: "Payment MySQL Cluster Production",
    environmentLabel: "Production",
    summary: eventType,
  };
}

describe("buildDiagnosticEvidence", () => {
  it("emits critical resource health evidence", () => {
    const evidence = buildDiagnosticEvidence({
      resource: resource({ healthStatus: "critical" }),
      members: [],
      recentAudits: [],
    });

    expect(evidence).toContainEqual({
      id: "resource-health-critical",
      severity: "critical",
      titleKey: "databaseOperator.evidence.resourceHealthCritical",
      sourceKey: "databaseOperator.evidence.sources.resourceStatus",
      rawHint: "healthStatus=critical",
      count: 1,
    });
  });

  it("emits member health and lifecycle evidence", () => {
    const evidence = buildDiagnosticEvidence({
      resource: resource(),
      members: [
        member({ healthStatus: "warning" }),
        member({ lifecycleStatus: "stopped" }),
      ],
      recentAudits: [],
    });

    expect(evidence.map((item) => item.id)).toContain("member-health-abnormal");
    expect(evidence.map((item) => item.id)).toContain("member-lifecycle-abnormal");
  });

  it("emits missing role evidence", () => {
    const evidence = buildDiagnosticEvidence({
      resource: resource(),
      members: [member({ profileSummary: {} })],
      recentAudits: [],
    });

    expect(evidence).toContainEqual({
      id: "member-role-missing",
      severity: "unknown",
      titleKey: "databaseOperator.evidence.memberRoleMissing",
      sourceKey: "databaseOperator.evidence.sources.memberProfile",
      rawHint: "profileSummary.role",
      count: 1,
    });
  });

  it("emits no abnormal evidence for healthy complete data", () => {
    const evidence = buildDiagnosticEvidence({
      resource: resource(),
      members: [member()],
      recentAudits: [],
    });

    expect(evidence).toEqual([]);
  });
});

describe("buildRunbookChecks", () => {
  it("suggests critical health checks for critical resources", () => {
    const checks = buildRunbookChecks(
      buildDiagnosticEvidence({
        resource: resource({ healthStatus: "critical" }),
        members: [],
        recentAudits: [],
      }),
    );

    expect(checks).toContainEqual({
      id: "check-critical-health",
      textKey: "databaseOperator.runbook.checks.criticalHealth",
    });
  });

  it("suggests profile sync check for missing role evidence", () => {
    const checks = buildRunbookChecks([
      {
        id: "member-role-missing",
        severity: "unknown",
        titleKey: "databaseOperator.evidence.memberRoleMissing",
        sourceKey: "databaseOperator.evidence.sources.memberProfile",
        rawHint: "profileSummary.role",
        count: 2,
      },
    ]);

    expect(checks).toContainEqual({
      id: "check-profile-sync",
      textKey: "databaseOperator.runbook.checks.profileSync",
    });
  });

  it("returns a no-findings check when no evidence exists", () => {
    expect(buildRunbookChecks([])).toEqual([
      {
        id: "check-no-findings",
        textKey: "databaseOperator.runbook.checks.noFindings",
      },
    ]);
  });
});

describe("buildAuditBuckets", () => {
  it("groups resource, relation, and other audit events", () => {
    const buckets = buildAuditBuckets([
      audit("resource.updated"),
      audit("relation.created"),
      audit("auth.login"),
    ]);

    expect(buckets).toEqual({
      total: 3,
      resourceChanges: 1,
      relationChanges: 1,
      otherEvents: 1,
      hasPotentiallyRelevantChanges: true,
    });
  });

  it("returns zero buckets for no audits", () => {
    expect(buildAuditBuckets([])).toEqual({
      total: 0,
      resourceChanges: 0,
      relationChanges: 0,
      otherEvents: 0,
      hasPotentiallyRelevantChanges: false,
    });
  });
});
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/lib/database-diagnostic-runbook.test.ts
```

Expected: FAIL because `@/lib/database-diagnostic-runbook` does not exist.

- [ ] **Step 3: Commit nothing**

Do not commit failing tests alone. Implement Task 2 first.

---

## Task 2: Implement Pure Runbook Helpers

**Files:**
- Create: `lib/database-diagnostic-runbook.ts`
- Test: `tests/lib/database-diagnostic-runbook.test.ts`

- [ ] **Step 1: Add helper implementation**

Create `lib/database-diagnostic-runbook.ts`:

```ts
import type { ClusterMember } from "@/types/resource";
import type { AuditEventViewModel, ResourceDetailViewModel } from "@/types/view-models";

export type DiagnosticEvidenceSeverity = "critical" | "warning" | "info" | "unknown";

export type DiagnosticEvidence = {
  id: string;
  severity: DiagnosticEvidenceSeverity;
  titleKey: string;
  sourceKey: string;
  rawHint: string;
  count: number;
};

export type RunbookCheck = {
  id: string;
  textKey: string;
};

export type AuditBuckets = {
  total: number;
  resourceChanges: number;
  relationChanges: number;
  otherEvents: number;
  hasPotentiallyRelevantChanges: boolean;
};

function hasAbnormalHealth(status: string): boolean {
  return status === "critical" || status === "warning";
}

function hasAbnormalLifecycle(status: string): boolean {
  return status === "stopped" || status === "degraded";
}

function countMembers(
  members: ClusterMember[],
  predicate: (member: ClusterMember) => boolean,
): number {
  return members.filter(predicate).length;
}

export function buildDiagnosticEvidence({
  resource,
  members,
  recentAudits,
}: {
  resource: ResourceDetailViewModel;
  members: ClusterMember[];
  recentAudits: AuditEventViewModel[];
}): DiagnosticEvidence[] {
  const evidence: DiagnosticEvidence[] = [];

  if (resource.healthStatus === "critical") {
    evidence.push({
      id: "resource-health-critical",
      severity: "critical",
      titleKey: "databaseOperator.evidence.resourceHealthCritical",
      sourceKey: "databaseOperator.evidence.sources.resourceStatus",
      rawHint: "healthStatus=critical",
      count: 1,
    });
  } else if (resource.healthStatus === "warning") {
    evidence.push({
      id: "resource-health-warning",
      severity: "warning",
      titleKey: "databaseOperator.evidence.resourceHealthWarning",
      sourceKey: "databaseOperator.evidence.sources.resourceStatus",
      rawHint: "healthStatus=warning",
      count: 1,
    });
  } else if (resource.healthStatus === "unknown") {
    evidence.push({
      id: "resource-health-unknown",
      severity: "unknown",
      titleKey: "databaseOperator.evidence.resourceHealthUnknown",
      sourceKey: "databaseOperator.evidence.sources.resourceStatus",
      rawHint: "healthStatus=unknown",
      count: 1,
    });
  }

  const abnormalHealthCount = countMembers(members, (member) =>
    hasAbnormalHealth(member.healthStatus),
  );
  if (abnormalHealthCount > 0) {
    evidence.push({
      id: "member-health-abnormal",
      severity: members.some((member) => member.healthStatus === "critical")
        ? "critical"
        : "warning",
      titleKey: "databaseOperator.evidence.memberHealthAbnormal",
      sourceKey: "databaseOperator.evidence.sources.memberHealth",
      rawHint: "members[].healthStatus",
      count: abnormalHealthCount,
    });
  }

  const abnormalLifecycleCount = countMembers(members, (member) =>
    hasAbnormalLifecycle(member.lifecycleStatus),
  );
  if (abnormalLifecycleCount > 0) {
    evidence.push({
      id: "member-lifecycle-abnormal",
      severity: "warning",
      titleKey: "databaseOperator.evidence.memberLifecycleAbnormal",
      sourceKey: "databaseOperator.evidence.sources.memberLifecycle",
      rawHint: "members[].lifecycleStatus",
      count: abnormalLifecycleCount,
    });
  }

  const missingRoleCount = countMembers(
    members,
    (member) => !member.profileSummary?.role,
  );
  if (missingRoleCount > 0) {
    evidence.push({
      id: "member-role-missing",
      severity: "unknown",
      titleKey: "databaseOperator.evidence.memberRoleMissing",
      sourceKey: "databaseOperator.evidence.sources.memberProfile",
      rawHint: "profileSummary.role",
      count: missingRoleCount,
    });
  }

  const missingConnectionCount = countMembers(
    members,
    (member) => !member.profileSummary?.hostname || !member.profileSummary?.port,
  );
  if (missingConnectionCount > 0) {
    evidence.push({
      id: "member-connection-missing",
      severity: "unknown",
      titleKey: "databaseOperator.evidence.memberConnectionMissing",
      sourceKey: "databaseOperator.evidence.sources.memberProfile",
      rawHint: "profileSummary.hostname|profileSummary.port",
      count: missingConnectionCount,
    });
  }

  const auditBuckets = buildAuditBuckets(recentAudits);
  if (auditBuckets.hasPotentiallyRelevantChanges) {
    evidence.push({
      id: "audit-nearby-changes",
      severity: "info",
      titleKey: "databaseOperator.evidence.auditNearbyChanges",
      sourceKey: "databaseOperator.evidence.sources.auditEvents",
      rawHint: "recentAudits[].eventType",
      count: auditBuckets.resourceChanges + auditBuckets.relationChanges,
    });
  }

  return evidence;
}

export function buildRunbookChecks(evidence: DiagnosticEvidence[]): RunbookCheck[] {
  const ids = new Set(evidence.map((item) => item.id));
  const checks: RunbookCheck[] = [];

  if (
    ids.has("resource-health-critical") ||
    ids.has("resource-health-warning") ||
    ids.has("member-health-abnormal")
  ) {
    checks.push({
      id: "check-critical-health",
      textKey: "databaseOperator.runbook.checks.criticalHealth",
    });
  }

  if (ids.has("member-lifecycle-abnormal")) {
    checks.push({
      id: "check-lifecycle-state",
      textKey: "databaseOperator.runbook.checks.lifecycleState",
    });
  }

  if (ids.has("member-role-missing") || ids.has("member-connection-missing")) {
    checks.push({
      id: "check-profile-sync",
      textKey: "databaseOperator.runbook.checks.profileSync",
    });
  }

  if (ids.has("audit-nearby-changes")) {
    checks.push({
      id: "check-nearby-audits",
      textKey: "databaseOperator.runbook.checks.nearbyAudits",
    });
  }

  if (checks.length === 0) {
    checks.push({
      id: "check-no-findings",
      textKey: "databaseOperator.runbook.checks.noFindings",
    });
  }

  return checks;
}

export function buildAuditBuckets(audits: AuditEventViewModel[]): AuditBuckets {
  let resourceChanges = 0;
  let relationChanges = 0;
  let otherEvents = 0;

  for (const event of audits) {
    if (event.eventType.startsWith("resource.")) {
      resourceChanges += 1;
    } else if (event.eventType.startsWith("relation.")) {
      relationChanges += 1;
    } else {
      otherEvents += 1;
    }
  }

  return {
    total: audits.length,
    resourceChanges,
    relationChanges,
    otherEvents,
    hasPotentiallyRelevantChanges: resourceChanges + relationChanges > 0,
  };
}
```

- [ ] **Step 2: Run helper tests**

Run:

```bash
npm run test -- tests/lib/database-diagnostic-runbook.test.ts
```

Expected: PASS.

- [ ] **Step 3: Commit helper**

```bash
git add lib/database-diagnostic-runbook.ts tests/lib/database-diagnostic-runbook.test.ts
git commit -m "feat: add database diagnostic runbook helpers"
```

---

## Task 3: Add Runbook I18n Copy

**Files:**
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Add English keys under `databaseOperator`**

Add this structure under the existing `databaseOperator` object in `messages/en.json`:

```json
"evidence": {
  "title": "Diagnostic evidence",
  "description": "Facts used to explain the current operator verdict.",
  "source": "Source",
  "rawHint": "Field",
  "empty": "No abnormal diagnostic evidence is available.",
  "resourceHealthCritical": "Resource health is critical.",
  "resourceHealthWarning": "Resource health is warning.",
  "resourceHealthUnknown": "Resource health is unknown.",
  "memberHealthAbnormal": "{count} member has warning or critical health.",
  "memberLifecycleAbnormal": "{count} member is stopped or degraded.",
  "memberRoleMissing": "Role information is missing for {count} member.",
  "memberConnectionMissing": "Connection information is missing for {count} member.",
  "auditNearbyChanges": "{count} recent resource or relation change is near this diagnostic context.",
  "sources": {
    "resourceStatus": "Resource status",
    "memberHealth": "Member health",
    "memberLifecycle": "Member lifecycle",
    "memberProfile": "Member profile",
    "auditEvents": "Audit events"
  }
},
"runbook": {
  "title": "Next checks",
  "description": "Read-only investigation steps based on available data.",
  "checks": {
    "criticalHealth": "Check instance process status, connection details, and recent resource changes.",
    "lifecycleState": "Confirm whether stopped or degraded state is expected maintenance or a recent change.",
    "profileSync": "Check whether backend profile sync is providing role, host, and port data.",
    "nearbyAudits": "Compare recent resource or relation changes with the time of the current signal.",
    "noFindings": "No clear abnormal signal is available. Continue with topology and audit history."
  }
},
"auditBuckets": {
  "title": "Audit context",
  "summary": "Recent {total} audit events: {resourceChanges} resource changes, {relationChanges} relation changes, {otherEvents} other events.",
  "noEvents": "No recent audit events.",
  "causalityNotice": "These events are nearby changes only; they do not confirm root cause."
}
```

- [ ] **Step 2: Add Chinese keys under `databaseOperator`**

Add matching keys in `messages/zh-CN.json`:

```json
"evidence": {
  "title": "诊断证据",
  "description": "用于解释当前运维判定的事实。",
  "source": "来源",
  "rawHint": "字段",
  "empty": "当前没有可展示的异常诊断证据。",
  "resourceHealthCritical": "资源健康状态为严重。",
  "resourceHealthWarning": "资源健康状态为告警。",
  "resourceHealthUnknown": "资源健康状态未知。",
  "memberHealthAbnormal": "{count} 个成员处于告警或严重状态。",
  "memberLifecycleAbnormal": "{count} 个成员已停止或降级。",
  "memberRoleMissing": "后端未提供 {count} 个成员的角色信息。",
  "memberConnectionMissing": "后端未提供 {count} 个成员的连接信息。",
  "auditNearbyChanges": "最近有 {count} 条资源或关系变更与当前诊断时间相近。",
  "sources": {
    "resourceStatus": "资源状态",
    "memberHealth": "成员健康",
    "memberLifecycle": "成员生命周期",
    "memberProfile": "成员画像",
    "auditEvents": "审计事件"
  }
},
"runbook": {
  "title": "下一步排查",
  "description": "基于当前数据给出的只读排查路径。",
  "checks": {
    "criticalHealth": "检查实例进程状态、连接地址和最近资源变更。",
    "lifecycleState": "确认停止或降级状态是否来自计划维护或最近变更。",
    "profileSync": "检查后端画像同步是否提供角色、主机和端口数据。",
    "nearbyAudits": "对照最近资源或关系变更，确认是否与当前信号时间接近。",
    "noFindings": "当前没有明确异常信号，继续查看拓扑和审计历史。"
  }
},
"auditBuckets": {
  "title": "审计上下文",
  "summary": "最近 {total} 条审计事件：{resourceChanges} 条资源变更，{relationChanges} 条关系变更，{otherEvents} 条其他操作。",
  "noEvents": "暂无最近审计事件。",
  "causalityNotice": "这些事件只表示时间邻近的变更，不代表已确认根因。"
}
```

- [ ] **Step 3: Run JSON/build sanity check**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
```

Expected: PASS. If JSON syntax is invalid, this should fail during import/type checking.

- [ ] **Step 4: Commit i18n keys**

```bash
git add messages/en.json messages/zh-CN.json
git commit -m "feat: add database diagnostic runbook copy"
```

---

## Task 4: Render Evidence And Runbook In Workbench

**Files:**
- Modify: `components/resources/database-operator-workbench.tsx`
- Test: `tests/components/database-operator-workbench.test.tsx`

- [ ] **Step 1: Add component tests first**

Extend `tests/components/database-operator-workbench.test.tsx` with tests that assert:

```ts
it("renders diagnostic evidence with source and raw field hint", async () => {
  // render cluster with one critical member
  // expect "Diagnostic evidence"
  // expect "1 member has warning or critical health."
  // expect "Member health"
  // expect "members[].healthStatus"
});

it("renders next checks from evidence", async () => {
  // render cluster with one critical member
  // expect "Next checks"
  // expect "Check instance process status, connection details, and recent resource changes."
});

it("renders no-findings runbook for healthy resource", async () => {
  // render healthy instance/cluster
  // expect "No clear abnormal signal is available. Continue with topology and audit history."
});
```

Use existing test fixtures in that file. Do not introduce network calls.

- [ ] **Step 2: Run failing component tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx
```

Expected: FAIL because UI sections are not rendered yet.

- [ ] **Step 3: Render evidence and runbook**

In `components/resources/database-operator-workbench.tsx`:

1. Import helpers:

```ts
import {
  buildAuditBuckets,
  buildDiagnosticEvidence,
  buildRunbookChecks,
} from "@/lib/database-diagnostic-runbook";
```

2. Compute values:

```ts
const diagnosticEvidence = buildDiagnosticEvidence({
  resource,
  members,
  recentAudits: recentAudits ?? [],
});
const runbookChecks = buildRunbookChecks(diagnosticEvidence);
const auditBuckets = buildAuditBuckets(recentAudits ?? []);
```

3. Add a `DetailPanel` after the verdict facts:

```tsx
<DetailPanel
  title={t("evidence.title")}
  description={t("evidence.description")}
>
  {diagnosticEvidence.length > 0 ? (
    <div className="space-y-2">
      {diagnosticEvidence.map((item) => (
        <div
          key={item.id}
          data-evidence-severity={item.severity}
          className="rounded-lg border border-border bg-background px-3 py-2"
        >
          <div className="flex items-start justify-between gap-3">
            <p className="text-sm font-medium text-foreground">
              {t(item.titleKey.replace("databaseOperator.", ""), { count: item.count })}
            </p>
            <span className="rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground">
              {t(item.sourceKey.replace("databaseOperator.", ""))}
            </span>
          </div>
          <p className="mt-1 font-mono text-xs text-muted-foreground">
            {t("evidence.rawHint")}: {item.rawHint}
          </p>
        </div>
      ))}
    </div>
  ) : (
    <p className="text-sm text-muted-foreground">{t("evidence.empty")}</p>
  )}
</DetailPanel>
```

4. Add a runbook `DetailPanel` after evidence:

```tsx
<DetailPanel
  title={t("runbook.title")}
  description={t("runbook.description")}
>
  <ol className="space-y-2">
    {runbookChecks.map((check, index) => (
      <li key={check.id} className="flex gap-2 text-sm text-muted-foreground">
        <span className="font-mono text-xs text-muted-foreground">
          {index + 1}.
        </span>
        <span>{t(check.textKey.replace("databaseOperator.", ""))}</span>
      </li>
    ))}
  </ol>
</DetailPanel>
```

Keep helper output keys stable. If the existing component already uses
`useTranslations("databaseOperator")`, strip `databaseOperator.` as shown above.

- [ ] **Step 4: Run component tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit workbench rendering**

```bash
git add components/resources/database-operator-workbench.tsx tests/components/database-operator-workbench.test.tsx
git commit -m "feat: render database diagnostic evidence and runbook"
```

---

## Task 5: Replace Audit Summary With Bucketed Context

**Files:**
- Modify: `components/resources/database-operator-workbench.tsx`
- Test: `tests/components/database-operator-workbench.test.tsx`

- [ ] **Step 1: Add audit bucket component tests**

Add tests:

```ts
it("renders grouped audit bucket summary", async () => {
  // recentAudits: resource.updated, relation.created, auth.login
  // expect "Recent 3 audit events: 1 resource changes, 1 relation changes, 1 other events."
});

it("renders cautious causality notice for resource or relation audit changes", async () => {
  // recentAudits includes resource.updated
  // expect "nearby changes only; they do not confirm root cause"
});

it("does not render causality notice when audits are only other events", async () => {
  // recentAudits includes auth.login
  // expect summary
  // expect no causality notice
});
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx
```

Expected: FAIL until bucket UI is implemented.

- [ ] **Step 3: Render bucketed audit summary**

Replace the current audit summary paragraph with:

```tsx
<p className="text-sm text-muted-foreground">
  {auditBuckets.total > 0
    ? t("auditBuckets.summary", {
        total: auditBuckets.total,
        resourceChanges: auditBuckets.resourceChanges,
        relationChanges: auditBuckets.relationChanges,
        otherEvents: auditBuckets.otherEvents,
      })
    : t("auditBuckets.noEvents")}
</p>
{auditBuckets.hasPotentiallyRelevantChanges ? (
  <p className="text-xs text-muted-foreground">
    {t("auditBuckets.causalityNotice")}
  </p>
) : null}
```

Keep the existing recent audit event list and "view all audits" link.

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx tests/lib/database-diagnostic-runbook.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit audit bucket context**

```bash
git add components/resources/database-operator-workbench.tsx tests/components/database-operator-workbench.test.tsx
git commit -m "feat: group database audit context in workbench"
```

---

## Task 6: Live QA And E2E Coverage

**Files:**
- Optional modify: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Inspect existing workflow E2E**

Open `e2e/operator-database-workflow.spec.ts` and check whether it already
visits a database cluster detail page and asserts workbench content.

- [ ] **Step 2: Add E2E assertions if missing**

If the workflow test does not assert the new sections, add assertions after the
cluster detail page loads:

```ts
await expect(page.getByText("Diagnostic evidence")).toBeVisible();
await expect(page.getByText("Next checks")).toBeVisible();
await expect(page.getByText("Audit context")).toBeVisible();
```

For Chinese-locale E2E, use:

```ts
await expect(page.getByText("诊断证据")).toBeVisible();
await expect(page.getByText("下一步排查")).toBeVisible();
await expect(page.getByText("审计上下文")).toBeVisible();
```

Use whichever locale the spec already sets. Do not mix locales.

- [ ] **Step 3: Run targeted E2E**

Run:

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Expected: PASS.

- [ ] **Step 4: Commit E2E update if changed**

If the E2E file changed:

```bash
git add e2e/operator-database-workflow.spec.ts
git commit -m "test: cover database diagnostic runbook workflow"
```

If the E2E file did not need changes, do not create an empty commit.

---

## Task 7: Full Verification And Closeout

**Files:**
- No planned source changes.

- [ ] **Step 1: Run full checks**

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

Expected:

- governance passes
- TypeScript clean
- ESLint clean
- Vitest all pass
- build succeeds
- E2E smoke passes
- E2E interaction passes
- full E2E passes

- [ ] **Step 2: Live browser verification**

Start backend if not running:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

Start frontend with same-origin API proxy:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-21-database-diagnostic-runbook
CONTROLHUB_API_BASE_URL=http://localhost:8080 \
CONTROLHUB_API_PROXY_URL=http://localhost:8080 \
NEXT_PUBLIC_API_BASE_URL=/__api \
npm run dev -- -p 3000
```

Verify:

- `/resources/14`
  - shows verdict
  - shows diagnostic evidence
  - shows next checks
  - shows audit context buckets
  - shows causality notice only if resource/relation audit changes exist
  - topology link still works
- `/resources/22`
  - shows healthy/unknown evidence appropriately
  - does not duplicate parent cluster or connection panels
  - no blank missing-data cells
- browser console has no unexpected error or warning
- API calls use `/__api`

- [ ] **Step 3: Confirm no forbidden scope**

Run:

```bash
git diff --name-only main...HEAD
```

Expected changed areas:

- `lib/database-diagnostic-runbook.ts`
- `components/resources/database-operator-workbench.tsx`
- `messages/en.json`
- `messages/zh-CN.json`
- tests and optional E2E

No backend files should be modified.

- [ ] **Step 4: Final git status**

Run:

```bash
git status --short --branch
```

Expected: clean working tree on the Phase 21 branch.

- [ ] **Step 5: Final report**

Report:

- branch and worktree path
- commit list
- files changed
- verification matrix
- live browser verification
- scope confirmation:
  - no backend changes
  - no SQL execution
  - no work orders
  - no write operations
  - no topology editing
  - no broad output suppression
  - no tag / push / release
  - no AI co-author

