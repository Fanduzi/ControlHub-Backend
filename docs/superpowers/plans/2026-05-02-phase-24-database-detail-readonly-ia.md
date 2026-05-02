# Phase 24 Database Detail Read-Only IA Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce duplicate cards on database detail pages by merging instance context and consistency, preserving topology placement, and grouping supporting details lower on the page.

**Architecture:** Keep backend and data contracts unchanged. Reuse Phase 22B/23 helpers, add focused presentation components for merged instance facts and supporting details, and adjust resource detail page composition with targeted tests.

**Tech Stack:** Next.js App Router, React, TypeScript, next-intl, existing resource detail view model, Phase 23 consistency helpers, Vitest, Playwright, Phase 18C E2E governance.

---

## File Structure

- Create `components/resources/database-instance-facts-panel.tsx`
  - Merged instance context + consistency panel.
  - Replaces the separate high-priority parent cluster and connection cards for
    database instances.
- Create `tests/components/database-instance-facts-panel.test.tsx`
  - Component coverage for facts, missing values, and consistency issues.
- Create `components/resources/database-supporting-details.tsx`
  - Database-only lower-priority wrapper for profile, relations, audit history,
    and optional cluster member table.
- Create `tests/components/database-supporting-details.test.tsx`
  - Component coverage for section headings and no data removal.
- Modify `app/(console)/resources/[id]/page.tsx`
  - Replace instance context + standalone parent/connection cards with merged
    panel.
  - Group supporting details lower on database resource pages.
  - Preserve non-database page behavior.
- Modify `messages/en.json`
  - Add or update `databaseReadonlyIA` keys.
- Modify `messages/zh-CN.json`
  - Add matching Chinese keys.
- Modify `tests/resource-detail-page.test.tsx`
  - Page-level layout and dedupe tests.
- Modify `e2e/operator-database-workflow.spec.ts`
  - E2E assertions for healthy instance and abnormal cluster layouts.

## Phase Constraints

- Frontend only.
- No backend code.
- No backend API contract changes.
- No SQL execution.
- No work orders.
- No write operations.
- No topology editing.
- No full-page tabs.
- No broad output suppression.
- No AI co-author in commits.

---

## Task 1: Add Merged Instance Facts Panel

**Files:**
- Create: `components/resources/database-instance-facts-panel.tsx`
- Create: `tests/components/database-instance-facts-panel.test.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Write failing component tests**

Create `tests/components/database-instance-facts-panel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { InstanceConsistencyResult } from "@/lib/database-read-model-consistency";

function t(key: string) {
  const keys: Record<string, string> = {
    "title": "Instance context and consistency",
    "description": "Parent cluster, role, connection, topology, and data consistency.",
    "parentCluster": "Parent cluster",
    "role": "Role",
    "connection": "Connection",
    "topology": "Topology",
    "topologyPresent": "Instance appears in topology",
    "missing": "Not provided by backend",
    "status.ok": "Data consistent",
    "status.warning": "Needs data review",
    "issues.instanceRoleMissing": "Backend did not provide instance role information.",
  };
  return keys[key] ?? key;
}

vi.mock("next-intl", () => ({
  useTranslations: () => t,
}));

describe("DatabaseInstanceFactsPanel", () => {
  it("renders merged parent, role, connection, topology, and status facts", async () => {
    const { DatabaseInstanceFactsPanel } = await import(
      "@/components/resources/database-instance-facts-panel"
    );

    const result: InstanceConsistencyResult = {
      status: "ok",
      facts: {
        parentClusterName: "Payment MySQL Cluster Production",
        role: "replica",
        connection: "prod-db-host-02.internal:3307",
      },
      issues: [],
    };

    render(<DatabaseInstanceFactsPanel result={result} />);

    expect(screen.getByText("Instance context and consistency")).toBeInTheDocument();
    expect(screen.getByText("Data consistent")).toBeInTheDocument();
    expect(screen.getByText("Payment MySQL Cluster Production")).toBeInTheDocument();
    expect(screen.getByText("replica")).toBeInTheDocument();
    expect(screen.getByText("prod-db-host-02.internal:3307")).toBeInTheDocument();
    expect(screen.getByText("Instance appears in topology")).toBeInTheDocument();
  });

  it("renders explicit missing value copy and warning issues", async () => {
    const { DatabaseInstanceFactsPanel } = await import(
      "@/components/resources/database-instance-facts-panel"
    );

    const result: InstanceConsistencyResult = {
      status: "warning",
      facts: {
        parentClusterName: "Payment MySQL Cluster Production",
      },
      issues: [
        {
          id: "instance-role-missing",
          kind: "missing_profile",
          severity: "warning",
          messageKey: "databaseConsistency.issues.instanceRoleMissing",
          resourceName: "Payment MySQL Replica",
        },
      ],
    };

    render(<DatabaseInstanceFactsPanel result={result} />);

    expect(screen.getByText("Needs data review")).toBeInTheDocument();
    expect(screen.getAllByText("Not provided by backend").length).toBeGreaterThan(0);
    expect(screen.getByText("Backend did not provide instance role information.")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
npm run test -- tests/components/database-instance-facts-panel.test.tsx
```

Expected: fail because the component does not exist.

- [ ] **Step 3: Implement component**

Create `components/resources/database-instance-facts-panel.tsx`:

```tsx
"use client";

import { useTranslations } from "next-intl";

import { DetailPanel } from "@/components/blocks/detail-panel";
import { cn } from "@/lib/utils";
import type {
  ConsistencyStatus,
  InstanceConsistencyResult,
} from "@/lib/database-read-model-consistency";

const statusTone: Record<ConsistencyStatus, string> = {
  ok: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  warning: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  unknown: "bg-muted text-muted-foreground",
};

function localKey(key: string): string {
  return key.replace("databaseConsistency.", "");
}

function Fact({
  label,
  value,
  missing,
}: {
  label: string;
  value?: string;
  missing: string;
}) {
  return (
    <div className="rounded-lg border border-border bg-background px-3 py-2">
      <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-sm font-medium text-foreground">
        {value || missing}
      </p>
    </div>
  );
}

export function DatabaseInstanceFactsPanel({
  result,
}: {
  result: InstanceConsistencyResult;
}) {
  const t = useTranslations("databaseReadonlyIA");
  const tc = useTranslations("databaseConsistency");

  return (
    <DetailPanel title={t("instanceFacts.title")} description={t("instanceFacts.description")}>
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <span
            data-consistency-status={result.status}
            className={cn(
              "rounded-md px-2 py-1 text-xs font-semibold",
              statusTone[result.status],
            )}
          >
            {tc(`status.${result.status}`)}
          </span>
        </div>

        <div className="grid gap-3 md:grid-cols-4">
          <Fact
            label={t("instanceFacts.parentCluster")}
            value={result.facts.parentClusterName}
            missing={t("instanceFacts.missing")}
          />
          <Fact
            label={t("instanceFacts.role")}
            value={result.facts.role}
            missing={t("instanceFacts.missing")}
          />
          <Fact
            label={t("instanceFacts.connection")}
            value={result.facts.connection}
            missing={t("instanceFacts.missing")}
          />
          <Fact
            label={t("instanceFacts.topology")}
            value={result.issues.some((issue) => issue.id === "instance-missing-from-topology")
              ? undefined
              : t("instanceFacts.topologyPresent")}
            missing={t("instanceFacts.missing")}
          />
        </div>

        {result.issues.length > 0 ? (
          <ul className="space-y-2">
            {result.issues.map((issue) => (
              <li
                key={issue.id}
                data-consistency-issue-kind={issue.kind}
                className="rounded-lg border border-border bg-background px-3 py-2 text-sm"
              >
                <span className="font-medium text-foreground">
                  {issue.resourceName ?? issue.resourceId ?? ""}
                </span>
                <span className="ml-2 text-muted-foreground">
                  {tc(localKey(issue.messageKey))}
                </span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </DetailPanel>
  );
}
```

- [ ] **Step 4: Add i18n keys**

Add to `messages/en.json`:

```json
"databaseReadonlyIA": {
  "instanceFacts": {
    "title": "Instance context and consistency",
    "description": "Parent cluster, role, connection, topology, and data consistency.",
    "parentCluster": "Parent cluster",
    "role": "Role",
    "connection": "Connection",
    "topology": "Topology",
    "topologyPresent": "Instance appears in topology",
    "missing": "Not provided by backend"
  }
}
```

Add to `messages/zh-CN.json`:

```json
"databaseReadonlyIA": {
  "instanceFacts": {
    "title": "实例上下文与一致性",
    "description": "所属集群、角色、连接、拓扑和数据一致性。",
    "parentCluster": "所属集群",
    "role": "角色",
    "connection": "连接",
    "topology": "拓扑",
    "topologyPresent": "该实例出现在拓扑中",
    "missing": "后端未提供"
  }
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
npm run test -- tests/components/database-instance-facts-panel.test.tsx
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add components/resources/database-instance-facts-panel.tsx tests/components/database-instance-facts-panel.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add database instance facts panel"
```

---

## Task 2: Replace Duplicate Instance Cards On Detail Page

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`
- Modify: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Add page tests for dedupe**

Update `tests/resource-detail-page.test.tsx` to assert for a database instance:

```tsx
expect(screen.getByText("Instance context and consistency")).toBeInTheDocument();
expect(screen.queryByText("Parent cluster")).not.toBeInTheDocument();
expect(screen.queryByText("Connection info")).not.toBeInTheDocument();
expect(screen.queryByText(/0 members/)).not.toBeInTheDocument();
```

Use exact expected strings from existing mocks. If the test uses Chinese mocks,
assert the Chinese equivalents:

```tsx
expect(screen.getByText("实例上下文与一致性")).toBeInTheDocument();
expect(screen.queryByText("所属集群")).not.toBeInTheDocument();
expect(screen.queryByText("连接信息")).not.toBeInTheDocument();
expect(screen.queryByText(/0 个成员/)).not.toBeInTheDocument();
```

If the fact labels inside the new merged panel use the same words as old
headings, scope the assertions to headings:

```tsx
expect(screen.queryByRole("heading", { name: "Parent cluster" })).not.toBeInTheDocument();
expect(screen.queryByRole("heading", { name: "Connection info" })).not.toBeInTheDocument();
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: fail because old cards still render.

- [ ] **Step 3: Wire merged panel and remove duplicate high-priority cards**

In `app/(console)/resources/[id]/page.tsx`:

1. Import:

```tsx
import { DatabaseInstanceFactsPanel } from "@/components/resources/database-instance-facts-panel";
```

2. Replace the existing separate instance context + instance consistency block:

```tsx
{isDatabaseInstance && instanceConsistency ? (
  <>
    <DatabaseInstanceContextPanel result={instanceConsistency} />
    <DatabaseConsistencyPanel scope="instance" result={instanceConsistency} />
  </>
) : null}
```

with:

```tsx
{isDatabaseInstance && instanceConsistency ? (
  <DatabaseInstanceFactsPanel result={instanceConsistency} />
) : null}
```

3. Remove or guard the old standalone parent cluster / connection info grid:

```tsx
{resource.resourceType === "database_instance" && resource.clusterInfo && (...)}
```

For database instances, do not render those high-priority full cards anymore.
The raw profile section remains lower on the page.

4. Keep cluster behavior unchanged:

```tsx
{isDatabaseCluster && clusterConsistency ? (
  <DatabaseConsistencyPanel scope="cluster" result={clusterConsistency} />
) : null}
```

- [ ] **Step 4: Run page tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add app/(console)/resources/[id]/page.tsx tests/resource-detail-page.test.tsx
git commit -m "refactor: merge duplicate database instance detail panels"
```

---

## Task 3: Add Supporting Details Group

**Files:**
- Create: `components/resources/database-supporting-details.tsx`
- Create: `tests/components/database-supporting-details.test.tsx`
- Modify: `app/(console)/resources/[id]/page.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Write component tests**

Create `tests/components/database-supporting-details.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function t(key: string) {
  const keys: Record<string, string> = {
    "title": "Supporting details",
    "description": "Profile, relations, and audit history remain available below the operator view.",
  };
  return keys[key] ?? key;
}

vi.mock("next-intl", () => ({
  useTranslations: () => t,
}));

describe("DatabaseSupportingDetails", () => {
  it("renders supporting details wrapper and children", async () => {
    const { DatabaseSupportingDetails } = await import(
      "@/components/resources/database-supporting-details"
    );

    render(
      <DatabaseSupportingDetails>
        <section>Operational profile</section>
        <section>Relations</section>
        <section>Audit history</section>
      </DatabaseSupportingDetails>,
    );

    expect(screen.getByText("Supporting details")).toBeInTheDocument();
    expect(screen.getByText("Operational profile")).toBeInTheDocument();
    expect(screen.getByText("Relations")).toBeInTheDocument();
    expect(screen.getByText("Audit history")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
npm run test -- tests/components/database-supporting-details.test.tsx
```

Expected: fail because component does not exist.

- [ ] **Step 3: Implement component**

Create `components/resources/database-supporting-details.tsx`:

```tsx
"use client";

import type { ReactNode } from "react";
import { useTranslations } from "next-intl";

export function DatabaseSupportingDetails({ children }: { children: ReactNode }) {
  const t = useTranslations("databaseReadonlyIA.supportingDetails");

  return (
    <section className="space-y-4" data-slot="database-supporting-details">
      <div>
        <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("description")}</p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[1fr_1fr]">{children}</div>
    </section>
  );
}
```

- [ ] **Step 4: Add i18n keys**

Add to `databaseReadonlyIA` in `messages/en.json`:

```json
"supportingDetails": {
  "title": "Supporting details",
  "description": "Profile, relations, and audit history remain available below the operator view."
}
```

Add to `databaseReadonlyIA` in `messages/zh-CN.json`:

```json
"supportingDetails": {
  "title": "支撑明细",
  "description": "画像、关系和审计历史仍保留在运维视图下方。"
}
```

- [ ] **Step 5: Wire on database detail pages**

In `app/(console)/resources/[id]/page.tsx`, wrap database-only lower detail
sections with `DatabaseSupportingDetails`.

The wrapper should contain:

- raw profile panel
- relations panel
- audit history panel

Cluster member table may remain before supporting details if it is still part of
the diagnostic flow. Do not remove it.

Non-database resources should keep existing layout.

- [ ] **Step 6: Run tests**

Run:

```bash
npm run test -- tests/components/database-supporting-details.test.tsx tests/resource-detail-page.test.tsx
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add components/resources/database-supporting-details.tsx tests/components/database-supporting-details.test.tsx app/(console)/resources/[id]/page.tsx messages/en.json messages/zh-CN.json tests/resource-detail-page.test.tsx
git commit -m "feat: group database supporting details"
```

---

## Task 4: Update E2E Layout Assertions

**Files:**
- Modify: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Update healthy instance assertions**

For `/resources/22`, assert:

```ts
await expect(page.locator("[data-testid='database-compact-health-deck']")).toBeVisible();
await expect(page.locator("h3", { hasText: /Resource topology/i })).toBeVisible();
await expect(page.locator("h2", { hasText: /Instance context and consistency|实例上下文与一致性/i })).toBeVisible();
await expect(page.locator("h2", { hasText: /Supporting details|支撑明细/i })).toBeVisible();
await expect(page.getByRole("heading", { name: /Parent cluster/i })).toHaveCount(0);
await expect(page.getByRole("heading", { name: /Connection info/i })).toHaveCount(0);
```

If headings are `h3` instead of `h2`, use role-based selectors instead of tag
selectors:

```ts
page.getByRole("heading", { name: /Instance context and consistency/i })
```

- [ ] **Step 2: Update abnormal cluster assertions**

For `/resources/14`, assert:

```ts
await expect(page.locator("h3", { hasText: /Top evidence/i })).toBeVisible();
await expect(page.locator("h3", { hasText: /Next checks/i })).toBeVisible();
await expect(page.getByRole("heading", { name: /Data consistency/i })).toBeVisible();
await expect(page.getByRole("heading", { name: /Cluster members/i })).toBeVisible();
await expect(page.getByRole("heading", { name: /Supporting details/i })).toBeVisible();
```

Do not weaken existing console/network guards.

- [ ] **Step 3: Run targeted E2E**

Ensure backend is running and port `3000` is free, then run:

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add e2e/operator-database-workflow.spec.ts
git commit -m "test: cover database detail read-only IA"
```

---

## Task 5: Full Verification And Closeout

**Files:**
- No code files unless verification reveals issues.

- [ ] **Step 1: Run full checks**

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

Run:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

Expected: all pass. If backend is not running, start it from
`/Users/fan/GolangProjects/ControlHub` with:

```bash
go run ./cmd/server
```

Stop any server process you start after verification.

- [ ] **Step 3: Live browser verification**

Start frontend from `/Users/fan/JsProjects/ControlHub`:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

- `/resources/22`
  - compact deck remains compact
  - topology follows deck
  - merged instance context and consistency panel appears
  - old parent cluster and connection full cards are gone
  - supporting details section exists
  - no `0 members`
  - no console errors
- `/resources/14`
  - diagnostic deck remains diagnostic
  - topology follows deck
  - data consistency panel appears
  - cluster members remain available
  - supporting details section exists
  - no console errors

- [ ] **Step 4: Final status**

Run:

```bash
git status --short --branch
git log --oneline -8
```

Expected: clean working tree after commits.

- [ ] **Step 5: Final report**

Report:

- worktree path
- branch
- commit list
- files changed
- exact duplication removed
- `/resources/22` live result
- `/resources/14` live result
- full verification matrix
- E2E results
- scope confirmation:
  - no backend changes
  - no API contract changes
  - no SQL
  - no work orders
  - no write operations
  - no topology editing
  - no tag/push/release
  - no AI co-author

