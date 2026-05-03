# Phase 25 Database Detail Semantic Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make database list and detail pages semantically self-consistent by clarifying operator verdict vs resource status, deduplicating audit context/history, making parent cluster navigable, and fixing supporting-detail layout.

**Architecture:** Frontend-only cleanup. Keep backend contracts and existing helper logic. Add small pure presentation helpers where needed, adjust database detail components, update i18n, and strengthen E2E assertions against the exact regressions found in live review.

**Tech Stack:** Next.js App Router, React, TypeScript, next-intl, existing database operator helpers, Vitest, Playwright, Phase 18C E2E governance.

---

## File Structure

- Modify `components/resources/database-decision-deck.tsx`
  - Clarify operator verdict vs resource self status.
  - Remove raw field hints from visible decision deck.
- Modify `components/resources/database-operator-workbench.tsx`
  - Reduce audit context to summary-only.
  - Hide diagnostic details when they only repeat visible deck evidence.
- Modify `components/resources/database-instance-facts-panel.tsx`
  - Render parent cluster as link when id exists.
  - Use specific missing copy for parent cluster.
- Modify `lib/database-read-model-consistency.ts`
  - Include parent cluster id in instance consistency facts.
  - Keep compatibility with existing facts.
- Modify `components/resources/database-consistency-panel.tsx`
  - Rename user-facing copy to read-model consistency.
- Modify `components/resources/database-supporting-details.tsx`
  - Support two-column primary details and full-width audit history.
- Modify `app/(console)/resources/[id]/page.tsx`
  - Pass parent cluster id into facts.
  - Place profile/relations and audit history using the new supporting layout.
  - Pass localized empty audit text to `ActivityTimeline`.
- Modify `components/blocks/activity-timeline.tsx`
  - Prefer i18n defaults for empty state instead of hardcoded English.
- Modify `components/databases/database-table.tsx`
  - Clarify status column/cell semantics for database cluster rows.
  - Add member-derived signal only if existing data allows it without new API calls.
- Modify `messages/en.json`
  - Add semantic cleanup copy.
- Modify `messages/zh-CN.json`
  - Add matching Chinese copy and remove English leaks.
- Modify tests:
  - `tests/components/database-decision-deck.test.tsx`
  - `tests/components/database-operator-workbench.test.tsx`
  - `tests/components/database-instance-facts-panel.test.tsx`
  - `tests/components/database-consistency-panel.test.tsx`
  - `tests/components/database-supporting-details.test.tsx`
  - `tests/components/database-table.test.tsx`
  - `tests/resource-detail-page.test.tsx`
  - `e2e/operator-database-workflow.spec.ts`

## Phase Constraints

- Frontend only.
- Use a dedicated frontend worktree.
- No backend code.
- No backend API contract changes.
- No SQL execution.
- No work orders.
- No write operations.
- No topology layout editing.
- No full-page tabs.
- No broad output suppression.
- No AI co-author in commits.

---

## Task 1: Clarify Decision Deck Status Semantics

**Files:**
- Modify: `components/resources/database-decision-deck.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-decision-deck.test.tsx`

- [ ] **Step 1: Add failing tests for explicit status subjects**

Update `tests/components/database-decision-deck.test.tsx` to cover abnormal cluster copy.

Required assertions:

```tsx
expect(screen.getByText("Operator verdict")).toBeInTheDocument();
expect(screen.getByText("Needs attention")).toBeInTheDocument();
expect(screen.getByText("Resource status")).toBeInTheDocument();
expect(screen.getByText("Healthy / Running")).toBeInTheDocument();
expect(screen.getByText("Member signal")).toBeInTheDocument();
expect(screen.getByText("1 member is warning or critical")).toBeInTheDocument();
expect(screen.queryByText(/members\\[\\]\\.healthStatus/)).not.toBeInTheDocument();
```

For Chinese test/mocks, assert:

```tsx
expect(screen.getByText("运维判定")).toBeInTheDocument();
expect(screen.getByText("资源自身状态")).toBeInTheDocument();
expect(screen.getByText("成员信号")).toBeInTheDocument();
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
npm run test -- tests/components/database-decision-deck.test.tsx
```

Expected: fail because the decision deck still renders unlabeled resource
badges and raw hints.

- [ ] **Step 3: Add i18n keys**

Add to `messages/en.json` under `databaseDecisionDeck`:

```json
{
  "statusSubjects": {
    "operatorVerdict": "Operator verdict",
    "resourceStatus": "Resource status",
    "memberSignal": "Member signal",
    "resourceStatusValue": "{health} / {lifecycle}",
    "memberWarningOrCritical": "{count, plural, one {# member is warning or critical} other {# members are warning or critical}}",
    "memberStoppedOrDegraded": "{count, plural, one {# member is stopped or degraded} other {# members are stopped or degraded}}"
  }
}
```

Add matching `messages/zh-CN.json`:

```json
{
  "statusSubjects": {
    "operatorVerdict": "运维判定",
    "resourceStatus": "资源自身状态",
    "memberSignal": "成员信号",
    "resourceStatusValue": "{health} / {lifecycle}",
    "memberWarningOrCritical": "{count} 个成员处于告警或严重状态",
    "memberStoppedOrDegraded": "{count} 个成员已停止或降级"
  }
}
```

- [ ] **Step 4: Update diagnostic deck rendering**

In `DiagnosticDeck`, replace the unlabeled status row:

```tsx
<StatusBadge status={resource.healthStatus} tone="health" />
<StatusBadge status={resource.lifecycleStatus} tone="lifecycle" />
```

with labeled compact facts:

```tsx
<div className="mt-3 grid gap-2 text-sm md:grid-cols-3">
  <div className="rounded-lg border border-border bg-background px-3 py-2">
    <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
      {td("statusSubjects.operatorVerdict")}
    </p>
    <p className="mt-1 font-semibold text-foreground">
      {to(`verdict.${verdictLevel}`)}
    </p>
  </div>
  <div className="rounded-lg border border-border bg-background px-3 py-2">
    <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
      {td("statusSubjects.resourceStatus")}
    </p>
    <div className="mt-1 flex flex-wrap gap-2">
      <StatusBadge status={resource.healthStatus} tone="health" />
      <StatusBadge status={resource.lifecycleStatus} tone="lifecycle" />
    </div>
  </div>
  <div className="rounded-lg border border-border bg-background px-3 py-2">
    <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
      {td("statusSubjects.memberSignal")}
    </p>
    <p className="mt-1 font-medium text-foreground">
      {verdict.warningOrCritical > 0
        ? td("statusSubjects.memberWarningOrCritical", { count: verdict.warningOrCritical })
        : verdict.stoppedOrDegraded > 0
          ? td("statusSubjects.memberStoppedOrDegraded", { count: verdict.stoppedOrDegraded })
          : to("evidence.empty")}
    </p>
  </div>
</div>
```

Remove raw hint rendering from visible evidence cards:

```tsx
<p className="mt-1 font-mono text-xs text-muted-foreground">
  {to("evidence.rawHint")}: {item.rawHint}
</p>
```

Do not remove raw hints from collapsed diagnostic details in the workbench.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
npm run test -- tests/components/database-decision-deck.test.tsx
```

Commit:

```bash
git add components/resources/database-decision-deck.tsx messages/en.json messages/zh-CN.json tests/components/database-decision-deck.test.tsx
git commit -m "fix: clarify database detail status semantics"
```

---

## Task 2: Rename Data Consistency To Read-Model Consistency

**Files:**
- Modify: `components/resources/database-consistency-panel.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-consistency-panel.test.tsx`

- [ ] **Step 1: Add failing tests for read-model wording**

Update `tests/components/database-consistency-panel.test.tsx`:

```tsx
expect(screen.getByText("Read-model consistency")).toBeInTheDocument();
expect(screen.getByText("Read-model consistent")).toBeInTheDocument();
expect(screen.queryByText("Data consistency")).not.toBeInTheDocument();
expect(screen.queryByText("Data consistent")).not.toBeInTheDocument();
```

For Chinese mocks:

```tsx
expect(screen.getByText("读模型一致性")).toBeInTheDocument();
expect(screen.getByText("读模型一致")).toBeInTheDocument();
expect(screen.queryByText("数据一致")).not.toBeInTheDocument();
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
npm run test -- tests/components/database-consistency-panel.test.tsx
```

- [ ] **Step 3: Update i18n**

In `messages/en.json`, change `databaseConsistency` user-facing strings:

```json
{
  "title": "Read-model consistency",
  "description": "Checks whether members, relations, topology, and profile data agree.",
  "status": {
    "ok": "Read-model consistent",
    "warning": "Read-model needs review",
    "unknown": "Not enough read-model data"
  },
  "summary": {
    "ok": "Current read-model signals agree.",
    "warning": "Some read-model signals need review.",
    "unknown": "Not enough read-model data to compare signals."
  }
}
```

In `messages/zh-CN.json`:

```json
{
  "title": "读模型一致性",
  "description": "检查成员、关系、拓扑和画像数据是否互相匹配。",
  "status": {
    "ok": "读模型一致",
    "warning": "读模型需检查",
    "unknown": "读模型数据不足"
  },
  "summary": {
    "ok": "当前可见的读模型信号一致。",
    "warning": "部分读模型信号需要检查。",
    "unknown": "读模型数据不足，无法完成对比。"
  }
}
```

- [ ] **Step 4: Update component if it hardcodes copy**

If `DatabaseConsistencyPanel` only reads i18n keys, no TypeScript change is
needed. If it hardcodes `Data consistency` or `Data consistent`, replace those
with the i18n keys above.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
npm run test -- tests/components/database-consistency-panel.test.tsx
```

Commit:

```bash
git add components/resources/database-consistency-panel.tsx messages/en.json messages/zh-CN.json tests/components/database-consistency-panel.test.tsx
git commit -m "fix: clarify read-model consistency copy"
```

---

## Task 3: Make Instance Parent Cluster Navigable

**Files:**
- Modify: `lib/database-read-model-consistency.ts`
- Modify: `components/resources/database-instance-facts-panel.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-instance-facts-panel.test.tsx`
- Test: `tests/lib/database-read-model-consistency.test.ts`
- Test: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Add failing helper test for parent cluster id**

In `tests/lib/database-read-model-consistency.test.ts`, add:

```ts
it("includes parent cluster id in instance consistency facts", () => {
  const result = buildInstanceConsistency({
    resource: {
      ...resource,
      id: 22,
      resourceType: "database_instance",
      clusterInfo: {
        id: 14,
        displayName: "Analytics ClickHouse Cluster Production",
        healthStatus: "healthy",
        lifecycleStatus: "running",
      },
      profileSummary: {
        role: "replica",
        hostname: "prod-ch-host-01.internal",
        port: 8123,
      },
    },
  });

  expect(result.facts.parentClusterId).toBe(14);
  expect(result.facts.parentClusterName).toBe("Analytics ClickHouse Cluster Production");
});
```

- [ ] **Step 2: Add failing component tests for cluster link**

In `tests/components/database-instance-facts-panel.test.tsx`, add:

```tsx
it("renders parent cluster as a link when id is available", async () => {
  const { DatabaseInstanceFactsPanel } = await import(
    "@/components/resources/database-instance-facts-panel"
  );

  render(
    <DatabaseInstanceFactsPanel
      result={{
        status: "ok",
        facts: {
          parentClusterId: 14,
          parentClusterName: "Analytics ClickHouse Cluster Production",
          role: "replica",
          connection: "prod-ch-host-01.internal:8123",
        },
        issues: [],
      }}
    />,
  );

  expect(
    screen.getByRole("link", { name: "Analytics ClickHouse Cluster Production" }),
  ).toHaveAttribute("href", "/resources/14");
});

it("uses explicit missing parent cluster copy", async () => {
  const { DatabaseInstanceFactsPanel } = await import(
    "@/components/resources/database-instance-facts-panel"
  );

  render(
    <DatabaseInstanceFactsPanel
      result={{
        status: "warning",
        facts: {
          role: "replica",
          connection: "prod-ch-host-01.internal:8123",
        },
        issues: [],
      }}
    />,
  );

  expect(screen.getByText("Parent cluster not provided by backend")).toBeInTheDocument();
});
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
npm run test -- tests/lib/database-read-model-consistency.test.ts tests/components/database-instance-facts-panel.test.tsx
```

- [ ] **Step 4: Update types/helper**

In `lib/database-read-model-consistency.ts`, extend `InstanceConsistencyResult`:

```ts
facts: {
  parentClusterId?: number;
  parentClusterName?: string;
  role?: string;
  connection?: string;
};
```

Return id:

```ts
facts: {
  parentClusterId: resource.clusterInfo?.id,
  parentClusterName: resource.clusterInfo?.displayName,
  role,
  connection: hostname && port != null ? `${hostname}:${port}` : undefined,
},
```

- [ ] **Step 5: Update component**

In `components/resources/database-instance-facts-panel.tsx`, import `Link`:

```tsx
import Link from "next/link";
```

Replace the parent cluster fact with a specialized render:

```tsx
<Fact
  label={t("instanceFacts.parentCluster")}
  value={result.facts.parentClusterName}
  href={result.facts.parentClusterId ? `/resources/${result.facts.parentClusterId}` : undefined}
  missing={t("instanceFacts.parentClusterMissing")}
/>
```

Update `Fact` props:

```tsx
function Fact({
  label,
  value,
  href,
  missing,
}: {
  label: string;
  value?: string;
  href?: string;
  missing: string;
}) {
  return (
    <div className="rounded-lg border border-border bg-background px-3 py-2">
      <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </p>
      {value && href ? (
        <Link href={href} className="mt-1 block text-sm font-medium text-primary hover:underline">
          {value}
        </Link>
      ) : (
        <p className="mt-1 text-sm font-medium text-foreground">
          {value || missing}
        </p>
      )}
    </div>
  );
}
```

- [ ] **Step 6: Add i18n**

Add:

```json
"parentClusterMissing": "Parent cluster not provided by backend"
```

and Chinese:

```json
"parentClusterMissing": "后端未提供所属集群信息"
```

- [ ] **Step 7: Run tests and commit**

Run:

```bash
npm run test -- tests/lib/database-read-model-consistency.test.ts tests/components/database-instance-facts-panel.test.tsx tests/resource-detail-page.test.tsx
```

Commit:

```bash
git add lib/database-read-model-consistency.ts components/resources/database-instance-facts-panel.tsx messages/en.json messages/zh-CN.json tests/lib/database-read-model-consistency.test.ts tests/components/database-instance-facts-panel.test.tsx tests/resource-detail-page.test.tsx
git commit -m "fix: link database instance parent cluster"
```

---

## Task 4: Deduplicate Audit Context And Localize Audit History Empty State

**Files:**
- Modify: `components/resources/database-operator-workbench.tsx`
- Modify: `components/blocks/activity-timeline.tsx`
- Modify: `app/(console)/resources/[id]/page.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-operator-workbench.test.tsx`
- Test: `tests/components/activity-timeline.test.tsx` if it exists; otherwise create it.
- Test: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Add failing workbench tests**

In `tests/components/database-operator-workbench.test.tsx`, add:

```tsx
it("renders audit context as summary only without event rows", async () => {
  const { DatabaseOperatorWorkbench } = await import(
    "@/components/resources/database-operator-workbench"
  );

  render(
    <DatabaseOperatorWorkbench
      resource={resourceWithAudits}
      members={[]}
      recentAudits={[
        audit("resource.updated"),
        audit("relation.created"),
        audit("access.login"),
      ]}
    />,
  );

  expect(screen.getByText("Audit context")).toBeInTheDocument();
  expect(screen.getByText("Recent audits include 2 resource or relation changes. Use as context, not root cause.")).toBeInTheDocument();
  expect(screen.queryByText("resource.updated")).not.toBeInTheDocument();
  expect(screen.queryByText("relation.created")).not.toBeInTheDocument();
});

it("does not claim recent five events when there are no audits", async () => {
  const { DatabaseOperatorWorkbench } = await import(
    "@/components/resources/database-operator-workbench"
  );

  render(
    <DatabaseOperatorWorkbench
      resource={resource}
      members={[]}
      recentAudits={[]}
    />,
  );

  expect(screen.getByText("No recent audit events.")).toBeInTheDocument();
  expect(screen.queryByText("Last 5 audit events for this resource.")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Add failing ActivityTimeline localization test**

If `tests/components/activity-timeline.test.tsx` does not exist, create it:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("next-intl", () => ({
  useLocale: () => "zh-CN",
  useTranslations: () => {
    const t = (key: string) => {
      const keys: Record<string, string> = {
        "emptyTitle": "暂无审计活动",
        "emptyDescription": "后端审计流接入后，最近变更会显示在这里。",
      };
      return keys[key] ?? key;
    };
    t.has = (key: string) => ["emptyTitle", "emptyDescription"].includes(key);
    return t;
  },
}));

describe("ActivityTimeline", () => {
  it("uses localized default empty state", async () => {
    const { ActivityTimeline } = await import("@/components/blocks/activity-timeline");

    render(<ActivityTimeline events={[]} />);

    expect(screen.getByText("暂无审计活动")).toBeInTheDocument();
    expect(screen.queryByText("No audit activity yet")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx tests/components/activity-timeline.test.tsx
```

- [ ] **Step 4: Update audit context copy**

In `messages/en.json`, replace audit context description/summary keys with:

```json
"auditBuckets": {
  "title": "Audit context",
  "description": "Recent audit activity used as diagnostic context.",
  "noEvents": "No recent audit events.",
  "noRelevantChanges": "Recent audits do not include resource or relation changes.",
  "relevantChanges": "Recent audits include {count} resource or relation changes. Use as context, not root cause.",
  "viewAuditHistory": "View audit history"
}
```

In `messages/zh-CN.json`:

```json
"auditBuckets": {
  "title": "审计上下文",
  "description": "用于辅助判断最近变更是否相关。",
  "noEvents": "暂无最近审计事件。",
  "noRelevantChanges": "最近审计中没有资源或关系变更。",
  "relevantChanges": "最近审计中有 {count} 条资源或关系变更，仅作为排查线索，不代表根因。",
  "viewAuditHistory": "查看审计历史"
}
```

- [ ] **Step 5: Update DatabaseOperatorWorkbench**

Replace event-list rendering with summary-only:

```tsx
const relevantChangeCount = auditBuckets.resourceChanges + auditBuckets.relationChanges;
const auditSummary = auditBuckets.total === 0
  ? t("auditBuckets.noEvents")
  : relevantChangeCount > 0
    ? t("auditBuckets.relevantChanges", { count: relevantChangeCount })
    : t("auditBuckets.noRelevantChanges");
```

Render:

```tsx
<DetailPanel
  title={t("auditBuckets.title")}
  description={t("auditBuckets.description")}
>
  <div className="space-y-3">
    <p className="text-sm text-muted-foreground">{auditSummary}</p>
    {auditBuckets.total > 0 ? (
      <div className="flex justify-end">
        <Link
          href={`/audits?targetResourceId=${resource.id}`}
          className="text-sm text-primary hover:underline"
        >
          {t("auditBuckets.viewAuditHistory")}
        </Link>
      </div>
    ) : null}
  </div>
</DetailPanel>
```

Delete the `recentAudits.map(...)` event row rendering from this component.

- [ ] **Step 6: Localize ActivityTimeline default empty state**

In `components/blocks/activity-timeline.tsx`, change default params so they are
resolved through i18n:

```tsx
export function ActivityTimeline({
  events,
  emptyTitle,
  emptyDescription,
  locale,
}: ActivityTimelineProps) {
  const t = useTranslations("activityTimeline");
  // ...
  if (!events.length) {
    return (
      <EmptyState
        title={emptyTitle ?? t("emptyTitle")}
        description={emptyDescription ?? t("emptyDescription")}
      />
    );
  }
}
```

Ensure `messages/en.json` has:

```json
"activityTimeline": {
  "emptyTitle": "No audit activity yet",
  "emptyDescription": "Recent resource changes will appear here once the backend audit feed is connected."
}
```

and `messages/zh-CN.json` has:

```json
"activityTimeline": {
  "emptyTitle": "暂无审计活动",
  "emptyDescription": "后端审计流接入后，最近变更会显示在这里。"
}
```

- [ ] **Step 7: Run tests and commit**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx tests/components/activity-timeline.test.tsx tests/resource-detail-page.test.tsx
```

Commit:

```bash
git add components/resources/database-operator-workbench.tsx components/blocks/activity-timeline.tsx app/\\(console\\)/resources/\\[id\\]/page.tsx messages/en.json messages/zh-CN.json tests/components/database-operator-workbench.test.tsx tests/components/activity-timeline.test.tsx tests/resource-detail-page.test.tsx
git commit -m "fix: deduplicate database audit context"
```

---

## Task 5: Fix Supporting Details Layout

**Files:**
- Modify: `components/resources/database-supporting-details.tsx`
- Modify: `app/(console)/resources/[id]/page.tsx`
- Test: `tests/components/database-supporting-details.test.tsx`
- Test: `tests/resource-detail-page.test.tsx`
- Test: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Add failing layout tests**

Update `tests/components/database-supporting-details.test.tsx`:

```tsx
it("renders audit history in a full-width slot", async () => {
  const { DatabaseSupportingDetails } = await import(
    "@/components/resources/database-supporting-details"
  );

  render(
    <DatabaseSupportingDetails
      primary={<section>Operational profile</section>}
      secondary={<section>Relations</section>}
      fullWidth={<section>Audit history</section>}
    />,
  );

  expect(screen.getByTestId("database-supporting-primary")).toBeInTheDocument();
  expect(screen.getByTestId("database-supporting-secondary")).toBeInTheDocument();
  expect(screen.getByTestId("database-supporting-full-width")).toHaveClass("xl:col-span-2");
});
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
npm run test -- tests/components/database-supporting-details.test.tsx
```

- [ ] **Step 3: Change component API**

Replace children-only API with named slots:

```tsx
"use client";

import type { ReactNode } from "react";
import { useTranslations } from "next-intl";

export function DatabaseSupportingDetails({
  primary,
  secondary,
  fullWidth,
}: {
  primary: ReactNode;
  secondary: ReactNode;
  fullWidth: ReactNode;
}) {
  const t = useTranslations("databaseReadonlyIA.supportingDetails");

  return (
    <section className="space-y-4" data-slot="database-supporting-details">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("description")}</p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[1fr_1fr]">
        <div data-testid="database-supporting-primary">{primary}</div>
        <div data-testid="database-supporting-secondary">{secondary}</div>
        <div
          data-testid="database-supporting-full-width"
          className="xl:col-span-2"
        >
          {fullWidth}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 4: Update resource detail page usage**

In `app/(console)/resources/[id]/page.tsx`, replace:

```tsx
<DatabaseSupportingDetails>
  <DetailPanel title={...profile...}>...</DetailPanel>
  <DetailPanel title={...relations...}>...</DetailPanel>
  <DetailPanel title={...audit...}>...</DetailPanel>
</DatabaseSupportingDetails>
```

with:

```tsx
<DatabaseSupportingDetails
  primary={
    <DetailPanel
      title={t("pages.resourceDetail.profile.title")}
      description={t("pages.resourceDetail.profile.description")}
    >
      {/* existing profile content */}
    </DetailPanel>
  }
  secondary={
    <DetailPanel
      title={t("pages.resourceDetail.relations.title")}
      description={t("pages.resourceDetail.relations.description")}
    >
      <ResourceRelationPanel relations={resource.relations} resourceId={resource.id} />
    </DetailPanel>
  }
  fullWidth={
    <DetailPanel
      title={t("pages.resourceDetail.audit.title")}
      description={t("pages.resourceDetail.audit.description")}
    >
      <ActivityTimeline events={resource.auditEvents} locale={locale} />
    </DetailPanel>
  }
/>
```

- [ ] **Step 5: Update E2E assertion**

In `e2e/operator-database-workflow.spec.ts`, add for `/resources/22`:

```ts
const auditPanel = page.locator("[data-testid='database-supporting-full-width']");
await expect(auditPanel).toBeVisible();
await expect(auditPanel.locator("h3", { hasText: /Audit history|审计历史/i })).toBeVisible();
```

- [ ] **Step 6: Run tests and commit**

Run:

```bash
npm run test -- tests/components/database-supporting-details.test.tsx tests/resource-detail-page.test.tsx
```

Commit:

```bash
git add components/resources/database-supporting-details.tsx app/\\(console\\)/resources/\\[id\\]/page.tsx tests/components/database-supporting-details.test.tsx tests/resource-detail-page.test.tsx e2e/operator-database-workflow.spec.ts
git commit -m "fix: make database audit history full width"
```

---

## Task 6: Database List Status Semantics

**Files:**
- Modify: `components/databases/database-table.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-table.test.tsx`
- Test: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Inspect available row data**

Before implementation, inspect `components/databases/database-table.tsx` and
`types/view-models.ts`.

If row data includes enough information to compute member-derived signals, add
the signal. If not, do not fetch per row. Use explicit resource self status copy.

- [ ] **Step 2: Add failing test for status label clarity**

At minimum, update `tests/components/database-table.test.tsx` to assert the
status header/cell clarifies resource self status:

```tsx
expect(screen.getByText(/Resource status|资源自身状态/)).toBeInTheDocument();
```

If derived member signal is feasible from existing row data, add:

```tsx
expect(screen.getByText(/1 critical member|1 个成员严重/)).toBeInTheDocument();
```

- [ ] **Step 3: Implement minimal safe copy**

If no derived data is available in list rows, rename the column header/copy only:

```text
资源自身状态
Resource status
```

Do not imply overall operator health in the list.

If derived data is available, render a secondary badge below resource status:

```text
成员严重 1
1 critical member
```

- [ ] **Step 4: Run tests and commit**

Run:

```bash
npm run test -- tests/components/database-table.test.tsx
```

Commit:

```bash
git add components/databases/database-table.tsx messages/en.json messages/zh-CN.json tests/components/database-table.test.tsx e2e/operator-database-workflow.spec.ts
git commit -m "fix: clarify database list status semantics"
```

---

## Task 7: Full Verification And Live QA

**Files:**
- Modify only if verification finds a real bug.

- [ ] **Step 1: Run forbidden text audit**

Run:

```bash
rg -n "健康=|No audit activity yet|Recent resource changes will appear|该资源最近 5 条审计事件|字段: members\\[\\]\\.healthStatus|数据一致" app components messages tests e2e
```

Expected:

- no `健康=`
- no English empty-state leak in Chinese-visible defaults
- no `该资源最近 5 条审计事件`
- no visible decision deck raw hint tests/copy
- no generic `数据一致` user-facing copy

If `字段: members[].healthStatus` remains in tests for collapsed diagnostic
details, document why it is acceptable.

- [ ] **Step 2: Run verification matrix**

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

- [ ] **Step 3: Live browser QA**

With frontend and backend running, verify:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Required observations:

- `/resources/14` clearly says operator verdict is needs attention and resource
  self status is healthy/running.
- `/resources/14` says read-model consistency, not generic data consistency.
- `/resources/14` audit context is summary-only.
- `/resources/22` parent cluster in facts panel links to `/resources/14`.
- `/resources/22` audit context does not claim five events when none exist.
- `/resources/22` audit history empty state is localized.
- audit history is full width in supporting details.
- no console errors.

- [ ] **Step 4: Final commit if needed**

If verification required fixes:

```bash
git add <changed-files>
git commit -m "fix: complete database detail semantic cleanup"
```

- [ ] **Step 5: Final report**

Report:

- worktree path
- branch
- commit list
- changed files
- each live issue and how it was fixed
- verification matrix
- live browser results
- forbidden text audit results
- scope confirmation

