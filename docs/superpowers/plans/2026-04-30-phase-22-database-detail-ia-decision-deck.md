# Phase 22 Database Detail IA Decision Deck Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure database resource detail pages around a first-screen decision deck so verdict, top evidence, next checks, topology entry, and abnormal members are visible without long scrolling.

**Architecture:** Keep backend and data contracts unchanged. Extract focused frontend presentation components for the database decision deck and compact sections, reuse existing Phase 19-21 helpers, and preserve full detail sections lower on the page.

**Tech Stack:** Next.js App Router, React, TypeScript, next-intl, existing resource/detail view models, Vitest, Playwright, Phase 18C E2E governance.

---

## File Structure

- Create `components/resources/database-decision-deck.tsx`
  - First-screen database decision deck.
  - Props: `resource`, `members`, `recentAudits`.
  - Uses existing helpers:
    - `buildDatabaseOperatorVerdict`
    - `buildDiagnosticEvidence`
    - `buildRunbookChecks`
    - `sortClusterMembersForOperations`
- Modify `app/(console)/resources/[id]/page.tsx`
  - Render the deck above long detail sections for database resources.
  - Keep non-database details unchanged.
  - Avoid duplicate verdict/evidence/runbook sections if the old workbench
    remains lower on the page.
- Modify `components/resources/database-operator-workbench.tsx`
  - Convert it from "everything expanded" into lower-page context, or split out
    pieces so the decision deck owns top evidence/checks.
- Modify `components/blocks/cluster-members-table.tsx` only if needed to expose
  compact abnormal-member display. Prefer new deck-specific rendering if
  changes would risk existing table behavior.
- Modify `messages/en.json`
  - Add deck titles/actions if needed.
- Modify `messages/zh-CN.json`
  - Add matching Chinese copy.
- Tests:
  - Create `tests/components/database-decision-deck.test.tsx`
  - Update `tests/resource-detail-page.test.tsx`
  - Update `tests/components/database-operator-workbench.test.tsx` if behavior
    changes.
  - Update `e2e/operator-database-workflow.spec.ts` for first-screen deck
    assertions.

## Phase Constraints

- Frontend only.
- No backend code.
- No SQL execution.
- No work orders.
- No write operations.
- No topology editing.
- No ReactFlow layout redesign.
- No full-page tabs in this phase.
- No AI co-author in commits.

---

## Task 1: Add Decision Deck Component Tests

**Files:**
- Create: `tests/components/database-decision-deck.test.tsx`
- Create later: `components/resources/database-decision-deck.tsx`

- [ ] **Step 1: Write failing tests**

Create `tests/components/database-decision-deck.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ClusterMember } from "@/types/resource";
import type { ResourceDetailViewModel } from "@/types/view-models";

function t(key: string, params?: Record<string, number>) {
  const keys: Record<string, string> = {
    "databaseDecisionDeck.title": "Decision deck",
    "databaseDecisionDeck.description": "Verdict, top evidence, next checks, topology, and abnormal members.",
    "databaseDecisionDeck.topEvidence": "Top evidence",
    "databaseDecisionDeck.nextChecks": "Next checks",
    "databaseDecisionDeck.topologyTitle": "Topology analysis",
    "databaseDecisionDeck.topologyDescription": "Open expanded topology to inspect upstream and downstream context.",
    "databaseDecisionDeck.openTopology": "Open topology",
    "databaseDecisionDeck.abnormalMembers": "Abnormal members",
    "databaseDecisionDeck.noAbnormalMembers": "No abnormal members.",
    "databaseOperator.verdict.healthy": "Healthy",
    "databaseOperator.verdict.needs_attention": "Needs attention",
    "databaseOperator.verdict.critical": "Critical",
    "databaseOperator.verdict.unknown": "Unknown",
    "databaseOperator.facts.all_known_members_healthy": "All known members are healthy.",
    "databaseOperator.facts.members_warning_or_critical": "Some members have warning or critical health.",
    "databaseOperator.facts.resource_health_critical": "Resource health is critical.",
    "databaseOperator.facts.lifecycle_needs_attention": "Some resources are stopped or degraded.",
    "databaseOperator.facts.resource_health_unknown": "Resource health is unknown.",
    "databaseOperator.evidence.resourceHealthCritical": "Resource health is critical.",
    "databaseOperator.evidence.memberHealthAbnormal": "Members with warning or critical health: 1.",
    "databaseOperator.evidence.memberLifecycleAbnormal": "Members stopped or degraded: 1.",
    "databaseOperator.evidence.sources.resourceStatus": "Resource status",
    "databaseOperator.evidence.sources.memberHealth": "Member health",
    "databaseOperator.evidence.sources.memberLifecycle": "Member lifecycle",
    "databaseOperator.evidence.rawHint": "Field",
    "databaseOperator.runbook.checks.criticalHealth": "Check instance process status, connection details, and recent resource changes.",
    "databaseOperator.runbook.checks.lifecycleState": "Confirm whether stopped or degraded state is expected maintenance or a recent change.",
    "databaseOperator.runbook.checks.noFindings": "No clear abnormal signal is available. Continue with topology and audit history.",
    "diagnostics.topology.viewTopology": "View topology",
  };
  let result = keys[key] ?? key;
  if (params) {
    for (const [name, value] of Object.entries(params)) {
      result = result.replace(`{${name}}`, String(value));
    }
  }
  return result;
}

(t as unknown as { has: (key: string) => boolean }).has = () => true;

vi.mock("next-intl", () => ({
  useTranslations: () => t,
}));

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
    name: "payment-mysql-replica-prod",
    displayName: "Payment MySQL Replica Production",
    resourceType: "database_instance",
    resourceSubtype: "mysql",
    lifecycleStatus: "running",
    healthStatus: "healthy",
    profileSummary: {
      role: "replica",
      hostname: "prod-db-host-04.internal",
      port: 3307,
    },
    ...overrides,
  };
}

describe("DatabaseDecisionDeck", () => {
  it("shows verdict, top evidence, next checks, and topology entry", async () => {
    const { DatabaseDecisionDeck } = await import(
      "@/components/resources/database-decision-deck"
    );

    render(
      <DatabaseDecisionDeck
        resource={resource({ healthStatus: "critical" })}
        members={[]}
        recentAudits={[]}
      />,
    );

    expect(screen.getByText("Decision deck")).toBeInTheDocument();
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.getByText("Top evidence")).toBeInTheDocument();
    expect(screen.getByText("Next checks")).toBeInTheDocument();
    expect(screen.getByText("Topology analysis")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open topology" })).toHaveAttribute(
      "href",
      "/resources/14?topologyDepth=2&topologyExpanded=1",
    );
  });

  it("limits first-screen evidence and checks to three items", async () => {
    const { DatabaseDecisionDeck } = await import(
      "@/components/resources/database-decision-deck"
    );

    render(
      <DatabaseDecisionDeck
        resource={resource({ healthStatus: "critical" })}
        members={[
          member({ healthStatus: "critical" }),
          member({ lifecycleStatus: "stopped" }),
        ]}
        recentAudits={[]}
      />,
    );

    expect(screen.getAllByTestId("decision-evidence-item")).toHaveLength(3);
    expect(screen.getAllByTestId("decision-runbook-item").length).toBeLessThanOrEqual(3);
  });

  it("shows abnormal members for clusters and hides healthy members from shortcut", async () => {
    const { DatabaseDecisionDeck } = await import(
      "@/components/resources/database-decision-deck"
    );

    render(
      <DatabaseDecisionDeck
        resource={resource()}
        members={[
          member({ id: 22, displayName: "Healthy Replica" }),
          member({ id: 23, displayName: "Critical Replica", healthStatus: "critical" }),
        ]}
        recentAudits={[]}
      />,
    );

    expect(screen.getByText("Abnormal members")).toBeInTheDocument();
    expect(screen.getByText("Critical Replica")).toBeInTheDocument();
    expect(screen.queryByText("Healthy Replica")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View topology" })).toHaveAttribute(
      "href",
      "/resources/23?topologyDepth=2&topologyExpanded=1",
    );
  });
});
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/components/database-decision-deck.test.tsx
```

Expected: FAIL because the component does not exist.

- [ ] **Step 3: Commit nothing**

Do not commit failing tests alone. Implement Task 2 first.

---

## Task 2: Implement Database Decision Deck

**Files:**
- Create: `components/resources/database-decision-deck.tsx`
- Test: `tests/components/database-decision-deck.test.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Add i18n keys**

Add to both locale files:

English:

```json
"databaseDecisionDeck": {
  "title": "Decision deck",
  "description": "Verdict, top evidence, next checks, topology, and abnormal members.",
  "topEvidence": "Top evidence",
  "nextChecks": "Next checks",
  "topologyTitle": "Topology analysis",
  "topologyDescription": "Open expanded topology to inspect upstream and downstream context.",
  "openTopology": "Open topology",
  "abnormalMembers": "Abnormal members",
  "noAbnormalMembers": "No abnormal members."
}
```

Chinese:

```json
"databaseDecisionDeck": {
  "title": "首屏决策台",
  "description": "集中展示判定、关键证据、下一步排查、拓扑入口和异常成员。",
  "topEvidence": "关键证据",
  "nextChecks": "下一步排查",
  "topologyTitle": "拓扑分析",
  "topologyDescription": "打开完整拓扑查看上下游和成员关系。",
  "openTopology": "打开完整拓扑",
  "abnormalMembers": "异常成员",
  "noAbnormalMembers": "暂无异常成员。"
}
```

- [ ] **Step 2: Implement component**

Create `components/resources/database-decision-deck.tsx`:

```tsx
"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";

import { StatusBadge } from "@/components/blocks/status-badge";
import {
  buildDatabaseOperatorVerdict,
  sortClusterMembersForOperations,
} from "@/lib/database-operator-workbench";
import {
  buildDiagnosticEvidence,
  buildRunbookChecks,
} from "@/lib/database-diagnostic-runbook";
import { cn } from "@/lib/utils";
import type { ClusterMember } from "@/types/resource";
import type { AuditEventViewModel, ResourceDetailViewModel } from "@/types/view-models";

type DatabaseDecisionDeckProps = {
  resource: ResourceDetailViewModel;
  members: ClusterMember[];
  recentAudits: AuditEventViewModel[];
};

const verdictTone: Record<string, string> = {
  healthy: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  needs_attention: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  critical: "bg-rose-500/10 text-rose-700 dark:text-rose-300",
  unknown: "bg-muted text-muted-foreground",
};

function isAbnormalMember(member: ClusterMember): boolean {
  return (
    member.healthStatus === "critical" ||
    member.healthStatus === "warning" ||
    member.healthStatus === "unknown" ||
    member.lifecycleStatus === "stopped" ||
    member.lifecycleStatus === "degraded"
  );
}

function localKey(key: string): string {
  return key.replace("databaseOperator.", "");
}

export function DatabaseDecisionDeck({
  resource,
  members,
  recentAudits,
}: DatabaseDecisionDeckProps) {
  const t = useTranslations();
  const td = useTranslations("databaseDecisionDeck");
  const to = useTranslations("databaseOperator");
  const diagnostics = useTranslations("diagnostics");

  const verdict = buildDatabaseOperatorVerdict({ resource, members });
  const evidence = buildDiagnosticEvidence({ resource, members, recentAudits }).slice(0, 3);
  const checks = buildRunbookChecks(
    buildDiagnosticEvidence({ resource, members, recentAudits }),
  ).slice(0, 3);
  const abnormalMembers = sortClusterMembersForOperations(members)
    .filter(isAbnormalMember)
    .slice(0, 3);

  return (
    <section className="rounded-2xl border border-border bg-card p-4 shadow-sm">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-primary">
            {td("title")}
          </p>
          <h2 className="mt-1 text-2xl font-semibold tracking-tight">
            {resource.displayName}
          </h2>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <span>{resource.resourceSubtype}</span>
            <span>{resource.environmentName}</span>
            <StatusBadge status={resource.healthStatus} tone="health" />
            <StatusBadge status={resource.lifecycleStatus} tone="lifecycle" />
          </div>
        </div>
        <div
          data-verdict-level={verdict.level}
          className={cn(
            "rounded-md px-3 py-2 text-sm font-semibold",
            verdictTone[verdict.level],
          )}
        >
          {to(`verdict.${verdict.level}`)}
        </div>
      </div>

      <div className="mt-4 grid gap-3 lg:grid-cols-[1fr_0.9fr]">
        <div className="rounded-xl border border-border bg-background p-3">
          <h3 className="text-sm font-semibold">{td("topEvidence")}</h3>
          <div className="mt-3 space-y-2">
            {evidence.length > 0 ? evidence.map((item) => (
              <div
                key={item.id}
                data-testid="decision-evidence-item"
                data-evidence-severity={item.severity}
                className="rounded-lg border border-border px-3 py-2"
              >
                <div className="flex items-start justify-between gap-3">
                  <p className="text-sm text-foreground">
                    {to(localKey(item.titleKey), { count: item.count })}
                  </p>
                  <span className="text-xs text-muted-foreground">
                    {to(localKey(item.sourceKey))}
                  </span>
                </div>
                <p className="mt-1 font-mono text-xs text-muted-foreground">
                  {to("evidence.rawHint")}: {item.rawHint}
                </p>
              </div>
            )) : (
              <p className="text-sm text-muted-foreground">
                {to("evidence.empty")}
              </p>
            )}
          </div>
        </div>

        <div className="rounded-xl border border-border bg-background p-3">
          <h3 className="text-sm font-semibold">{td("nextChecks")}</h3>
          <ol className="mt-3 space-y-2">
            {checks.map((check, index) => (
              <li
                key={check.id}
                data-testid="decision-runbook-item"
                className="flex gap-2 text-sm text-muted-foreground"
              >
                <span className="font-mono text-xs text-primary">
                  {index + 1}.
                </span>
                <span>{to(localKey(check.textKey))}</span>
              </li>
            ))}
          </ol>
        </div>
      </div>

      <div className="mt-3 rounded-xl border border-primary/20 bg-primary/5 p-3">
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <div>
            <h3 className="text-sm font-semibold">{td("topologyTitle")}</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              {td("topologyDescription")}
            </p>
          </div>
          <Link
            href={`/resources/${resource.id}?topologyDepth=2&topologyExpanded=1`}
            className="inline-flex items-center justify-center rounded-md bg-primary px-3 py-2 text-sm font-semibold text-primary-foreground"
          >
            {td("openTopology")}
          </Link>
        </div>
      </div>

      {resource.resourceType === "database_cluster" ? (
        <div className="mt-3 rounded-xl border border-border bg-background p-3">
          <h3 className="text-sm font-semibold">{td("abnormalMembers")}</h3>
          <div className="mt-3 space-y-2">
            {abnormalMembers.length > 0 ? abnormalMembers.map((member) => (
              <div
                key={member.id}
                className="flex flex-col gap-2 rounded-lg border border-border px-3 py-2 md:flex-row md:items-center md:justify-between"
              >
                <div>
                  <p className="text-sm font-medium text-foreground">
                    {member.displayName}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {member.profileSummary?.role ?? diagnostics("missing.role")}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge status={member.healthStatus} tone="health" />
                  <StatusBadge status={member.lifecycleStatus} tone="lifecycle" />
                  <Link
                    href={`/resources/${member.id}?topologyDepth=2&topologyExpanded=1`}
                    className="text-xs font-medium text-primary hover:underline"
                  >
                    {diagnostics("topology.viewTopology")}
                  </Link>
                </div>
              </div>
            )) : (
              <p className="text-sm text-muted-foreground">
                {td("noAbnormalMembers")}
              </p>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
}
```

- [ ] **Step 3: Run component tests**

Run:

```bash
npm run test -- tests/components/database-decision-deck.test.tsx
```

Expected: PASS.

- [ ] **Step 4: Commit deck component**

```bash
git add components/resources/database-decision-deck.tsx tests/components/database-decision-deck.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add database detail decision deck"
```

---

## Task 3: Wire Deck Into Database Detail Page

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`
- Test: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Add page-level tests**

Update `tests/resource-detail-page.test.tsx` to assert:

```ts
expect(screen.getByText("Decision deck")).toBeInTheDocument();
expect(screen.getByText("Topology analysis")).toBeInTheDocument();
expect(screen.getByText("Top evidence")).toBeInTheDocument();
expect(screen.getByText("Next checks")).toBeInTheDocument();
```

Also assert non-database resources do not render the deck if there is an
existing non-database fixture.

- [ ] **Step 2: Run failing page tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: FAIL until page wiring is implemented.

- [ ] **Step 3: Render deck above long sections**

In `app/(console)/resources/[id]/page.tsx`:

1. Import:

```ts
import { DatabaseDecisionDeck } from "@/components/resources/database-decision-deck";
```

2. Add helper:

```ts
const isDatabaseResource =
  resource.resourceType === "database_cluster" ||
  resource.resourceType === "database_instance";
```

3. Render `DatabaseDecisionDeck` near the top after identity/status and before
long panels:

```tsx
{isDatabaseResource ? (
  <DatabaseDecisionDeck
    resource={resource}
    members={resource.members ?? []}
    recentAudits={resource.recentAudits ?? []}
  />
) : null}
```

4. Avoid duplicate top-level `DatabaseOperatorWorkbench` sections above the
deck. If `DatabaseOperatorWorkbench` currently renders verdict/evidence/runbook
again immediately below, move it lower or reduce it to lower-page context in
Task 4.

- [ ] **Step 4: Run page tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx tests/components/database-decision-deck.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit page wiring**

```bash
git add app/(console)/resources/[id]/page.tsx tests/resource-detail-page.test.tsx
git commit -m "feat: place decision deck above database detail sections"
```

If shell globbing fails because of parentheses, quote the path:

```bash
git add 'app/(console)/resources/[id]/page.tsx' tests/resource-detail-page.test.tsx
```

---

## Task 4: Reduce Duplicate Expanded Workbench Content

**Files:**
- Modify: `components/resources/database-operator-workbench.tsx`
- Test: `tests/components/database-operator-workbench.test.tsx`

- [ ] **Step 1: Decide final responsibility split**

The decision deck owns:

- verdict badge summary
- top evidence
- top runbook checks
- topology entry
- abnormal member shortcut

The lower workbench may keep:

- full diagnostic evidence list
- full runbook list
- audit bucket detail
- member summary

It should not repeat the same top-level verdict block directly under the deck.

- [ ] **Step 2: Update tests**

Update `tests/components/database-operator-workbench.test.tsx` so it still
covers full evidence/runbook/audit bucket behavior, but does not require the
top-level verdict block if that block moved to the deck.

- [ ] **Step 3: Adjust component**

Modify `DatabaseOperatorWorkbench` to be a lower-page context section. Acceptable
approaches:

- remove the top verdict `DetailPanel`
- keep evidence/runbook/audit/member summary
- keep existing audit "view all" link

Do not remove data. Only reduce duplicated above-the-fold content.

- [ ] **Step 4: Run targeted tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx tests/components/database-decision-deck.test.tsx tests/resource-detail-page.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit dedupe**

```bash
git add components/resources/database-operator-workbench.tsx tests/components/database-operator-workbench.test.tsx
git commit -m "refactor: reduce duplicated database workbench content"
```

---

## Task 5: Keep Long Sections Available But Lower Priority

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`
- Optional modify: section wrappers/components already used on detail page
- Test: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Inspect current order**

Read the page and list the current order of:

- identity
- decision deck
- parent cluster
- connection info
- member summary/table
- topology
- audit history
- relations
- profile

- [ ] **Step 2: Reorder only if needed**

Target order for database cluster:

```text
identity/status
decision deck
full topology section or topology entry target
cluster members table
audit history/context
relations
profile / metadata
```

Target order for database instance:

```text
identity/status
decision deck
parent cluster / connection info
topology
audit history/context
relations
profile / metadata
```

Do not bury topology below full audit or relations.

- [ ] **Step 3: Add assertions**

If tests can reasonably assert order, add DOM order checks using text positions.
Example:

```ts
const text = document.body.textContent ?? "";
expect(text.indexOf("Decision deck")).toBeLessThan(text.indexOf("Resource topology"));
expect(text.indexOf("Resource topology")).toBeLessThan(text.indexOf("Audit history"));
```

Use exact existing headings from the project.

- [ ] **Step 4: Run page tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit section priority**

```bash
git add 'app/(console)/resources/[id]/page.tsx' tests/resource-detail-page.test.tsx
git commit -m "refactor: prioritize database detail sections"
```

If no page order changes were needed, do not create this commit.

---

## Task 6: E2E And Live Verification

**Files:**
- Modify: `e2e/operator-database-workflow.spec.ts`
- Optional modify: `e2e/operator-interaction-stability.spec.ts` only if needed

- [ ] **Step 1: Add E2E assertions**

In `e2e/operator-database-workflow.spec.ts`, after opening `/resources/14`, assert:

```ts
await expect(page.locator("h2", { hasText: /Decision deck/i })).toBeVisible();
await expect(page.locator("h3", { hasText: /Topology analysis/i })).toBeVisible();
await expect(page.getByRole("link", { name: /Open topology/i })).toBeVisible();
await expect(page.locator("h3", { hasText: /Abnormal members/i })).toBeVisible();
```

Use Chinese labels if the spec runs in Chinese locale.

- [ ] **Step 2: Assert topology is reachable without long scrolling**

Use a visibility assertion before manual scrolling:

```ts
await expect(page.getByRole("link", { name: /Open topology/i })).toBeVisible();
```

Do not assert exact pixel positions unless existing test infrastructure already
does so reliably.

- [ ] **Step 3: Run targeted E2E**

Run:

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Expected: PASS.

- [ ] **Step 4: Commit E2E update**

```bash
git add e2e/operator-database-workflow.spec.ts
git commit -m "test: cover database decision deck workflow"
```

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

Start frontend:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-22-database-detail-ia-decision-deck
CONTROLHUB_API_BASE_URL=http://localhost:8080 \
CONTROLHUB_API_PROXY_URL=http://localhost:8080 \
NEXT_PUBLIC_API_BASE_URL=/__api \
npm run dev -- -p 3000
```

Verify:

- `/resources/14`
  - decision deck visible near top
  - topology entry visible near top without long scrolling
  - abnormal member shortcut visible
  - full member table still available
  - audit context still available
  - no duplicate verdict/evidence wall directly below deck
- `/resources/22`
  - instance decision deck visible
  - parent cluster/connection context still available
  - topology entry visible near top
  - no cluster-only abnormal member section
- Browser console has no unexpected error/warning.
- API calls use `/__api`.

- [ ] **Step 3: Scope check**

Run:

```bash
git diff --name-only main...HEAD
```

Expected changed areas:

- `components/resources/database-decision-deck.tsx`
- `components/resources/database-operator-workbench.tsx`
- `app/(console)/resources/[id]/page.tsx`
- locale files
- tests/e2e

No backend files should be modified.

- [ ] **Step 4: Final status**

Run:

```bash
git status --short --branch
```

Expected: clean working tree on the Phase 22 branch.

- [ ] **Step 5: Final report**

Report:

- branch and worktree path
- commit list
- files changed
- layout before/after summary
- live browser verification
- verification matrix
- scope confirmation:
  - no backend changes
  - no SQL execution
  - no work orders
  - no write operations
  - no topology editing
  - no full-page tabs
  - no broad output suppression
  - no tag / push / release
  - no AI co-author

