# Phase 19 Database Operator Workbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn database cluster and instance detail pages into a read-only operator workbench that explains health, membership, connection context, topology entry points, and recent scoped audit events.

**Architecture:** Add small frontend view-model helpers for database operator verdicts and member summaries, then render focused read-only panels on the existing resource detail page. Reuse existing backend contracts and existing E2E governance gates; do not add write paths.

**Tech Stack:** Next.js App Router, React, existing resource detail view models, existing services/resources and services/audits clients, next-intl messages, Vitest, Playwright.

---

## File Structure

- Create `lib/database-operator-workbench.ts`
  - Pure helpers:
    - `buildDatabaseOperatorVerdict`
    - `buildClusterMemberSummary`
    - `buildAuditContextSummary`
- Create `components/resources/database-operator-workbench.tsx`
  - Read-only UI panels for cluster/instance operator context.
- Modify `types/view-models.ts`
  - Add optional `recentAudits` or reuse existing audit view model if already present.
- Modify `lib/view-models.ts`
  - Fetch recent scoped audit events for database detail pages if not already fetched.
- Modify `app/(console)/resources/[id]/page.tsx`
  - Render the new workbench for database resources.
- Modify `messages/en.json` and `messages/zh-CN.json`
  - Add all new labels and verdict strings.
- Add tests:
  - `tests/lib/database-operator-workbench.test.ts`
  - `tests/components/database-operator-workbench.test.tsx`
  - update `tests/resource-detail-page.test.tsx`
- Update E2E only if needed:
  - existing `operator-database-workflow.spec.ts` should assert verdict/member summary if stable.

---

### Task 1: Add Pure Operator Verdict Helpers

**Files:**
- Create: `lib/database-operator-workbench.ts`
- Test: `tests/lib/database-operator-workbench.test.ts`

- [ ] **Step 1: Write failing tests**

Create `tests/lib/database-operator-workbench.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  buildClusterMemberSummary,
  buildDatabaseOperatorVerdict,
} from "@/lib/database-operator-workbench";
import type { ClusterMember, ResourceViewModel } from "@/types/view-models";

function resource(overrides: Partial<ResourceViewModel>): ResourceViewModel {
  return {
    id: 1,
    name: "db",
    displayName: "DB",
    resourceType: "database_cluster",
    resourceSubtype: "mysql",
    environmentId: 1,
    lifecycleStatus: "running",
    healthStatus: "healthy",
    isArchived: false,
    summary: "",
    tags: [],
    labels: {},
    createdAt: "",
    updatedAt: "",
    ...overrides,
  } as ResourceViewModel;
}

function member(overrides: Partial<ClusterMember>): ClusterMember {
  return {
    id: 10,
    resourceId: 10,
    name: "mysql-1",
    displayName: "MySQL 1",
    resourceType: "database_instance",
    resourceSubtype: "mysql",
    lifecycleStatus: "running",
    healthStatus: "healthy",
    profileSummary: { role: "replica", host: "db-1", port: 3306 },
    ...overrides,
  } as ClusterMember;
}

describe("buildClusterMemberSummary", () => {
  it("counts total, primary, replica, warning/critical, and stopped/degraded members", () => {
    const summary = buildClusterMemberSummary([
      member({ profileSummary: { role: "primary", host: "db-1", port: 3306 } }),
      member({ healthStatus: "warning", profileSummary: { role: "replica" } }),
      member({ healthStatus: "critical", lifecycleStatus: "stopped", profileSummary: { role: "replica" } }),
      member({ profileSummary: {} }),
    ]);

    expect(summary).toEqual({
      total: 4,
      primary: 1,
      replica: 2,
      roleUnknown: 1,
      warningOrCritical: 2,
      stoppedOrDegraded: 1,
    });
  });
});

describe("buildDatabaseOperatorVerdict", () => {
  it("returns critical when the resource itself is critical", () => {
    const verdict = buildDatabaseOperatorVerdict({
      resource: resource({ healthStatus: "critical" }),
      members: [],
    });

    expect(verdict.level).toBe("critical");
    expect(verdict.facts).toContain("resource_health_critical");
  });

  it("returns needs_attention when any member is warning or critical", () => {
    const verdict = buildDatabaseOperatorVerdict({
      resource: resource({ healthStatus: "healthy" }),
      members: [member({ healthStatus: "warning" })],
    });

    expect(verdict.level).toBe("needs_attention");
    expect(verdict.facts).toContain("members_warning_or_critical");
  });

  it("returns healthy when resource and known members are healthy and running", () => {
    const verdict = buildDatabaseOperatorVerdict({
      resource: resource({ healthStatus: "healthy", lifecycleStatus: "running" }),
      members: [member({ healthStatus: "healthy", lifecycleStatus: "running" })],
    });

    expect(verdict.level).toBe("healthy");
    expect(verdict.facts).toContain("all_known_members_healthy");
  });
});
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/lib/database-operator-workbench.test.ts
```

Expected: fails because helper does not exist.

- [ ] **Step 3: Implement helpers**

Create `lib/database-operator-workbench.ts`:

```ts
import type { ClusterMember, ResourceViewModel } from "@/types/view-models";

export type OperatorVerdictLevel =
  | "healthy"
  | "needs_attention"
  | "critical"
  | "unknown";

export interface ClusterMemberSummary {
  total: number;
  primary: number;
  replica: number;
  roleUnknown: number;
  warningOrCritical: number;
  stoppedOrDegraded: number;
}

export interface DatabaseOperatorVerdict {
  level: OperatorVerdictLevel;
  facts: string[];
}

function normalizedRole(member: ClusterMember): string {
  return (member.profileSummary?.role ?? "").toLowerCase();
}

export function buildClusterMemberSummary(
  members: ClusterMember[],
): ClusterMemberSummary {
  let primary = 0;
  let replica = 0;
  let roleUnknown = 0;
  let warningOrCritical = 0;
  let stoppedOrDegraded = 0;

  for (const member of members) {
    const role = normalizedRole(member);
    if (role === "primary" || role === "master" || role === "writer") {
      primary += 1;
    } else if (role === "replica" || role === "secondary" || role === "reader") {
      replica += 1;
    } else {
      roleUnknown += 1;
    }

    if (member.healthStatus === "warning" || member.healthStatus === "critical") {
      warningOrCritical += 1;
    }

    if (member.lifecycleStatus === "stopped" || member.lifecycleStatus === "degraded") {
      stoppedOrDegraded += 1;
    }
  }

  return {
    total: members.length,
    primary,
    replica,
    roleUnknown,
    warningOrCritical,
    stoppedOrDegraded,
  };
}

export function buildDatabaseOperatorVerdict({
  resource,
  members,
}: {
  resource: ResourceViewModel;
  members: ClusterMember[];
}): DatabaseOperatorVerdict {
  const facts: string[] = [];
  const summary = buildClusterMemberSummary(members);

  if (resource.healthStatus === "critical") {
    facts.push("resource_health_critical");
    return { level: "critical", facts };
  }

  if (summary.warningOrCritical > 0) {
    facts.push("members_warning_or_critical");
  }

  if (summary.stoppedOrDegraded > 0 || resource.lifecycleStatus === "stopped" || resource.lifecycleStatus === "degraded") {
    facts.push("lifecycle_needs_attention");
  }

  if (facts.length > 0) {
    return { level: "needs_attention", facts };
  }

  if (resource.healthStatus === "unknown") {
    facts.push("resource_health_unknown");
    return { level: "unknown", facts };
  }

  facts.push("all_known_members_healthy");
  return { level: "healthy", facts };
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/lib/database-operator-workbench.test.ts
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add lib/database-operator-workbench.ts tests/lib/database-operator-workbench.test.ts
git commit -m "feat: add database operator verdict helpers"
```

---

### Task 2: Add Database Operator Workbench Component

**Files:**
- Create: `components/resources/database-operator-workbench.tsx`
- Test: `tests/components/database-operator-workbench.test.tsx`

- [ ] **Step 1: Write component tests**

Create `tests/components/database-operator-workbench.test.tsx` with tests that
assert:

- verdict heading renders
- cluster member summary cards render
- member table includes role, host, port, health, lifecycle
- instance mode renders parent cluster and connection context

Use existing test render helpers and message mocks from nearby resource detail
tests.

- [ ] **Step 2: Run failing tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx
```

Expected: fails because component does not exist.

- [ ] **Step 3: Implement component**

Create `components/resources/database-operator-workbench.tsx`.

The component should accept:

```ts
interface DatabaseOperatorWorkbenchProps {
  resource: ResourceViewModel;
  members: ClusterMember[];
  clusterInfo?: ResourceViewModel | null;
  recentAudits?: AuditEventViewModel[];
}
```

Render:

- verdict panel
- cluster summary cards when `resource.resourceType === "database_cluster"`
- parent cluster card when `clusterInfo` exists
- connection facts from `resource.profileSummary`
- recent audits list up to 5
- link to expanded topology via current resource detail URL with
  `topologyDepth=2&topologyExpanded=1`

Do not add actions that mutate backend state.

- [ ] **Step 4: Run component tests**

Run:

```bash
npm run test -- tests/components/database-operator-workbench.test.tsx
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add components/resources/database-operator-workbench.tsx tests/components/database-operator-workbench.test.tsx
git commit -m "feat: add database operator workbench panel"
```

---

### Task 3: Wire Recent Audit Context Into Resource Detail View Model

**Files:**
- Modify: `types/view-models.ts`
- Modify: `lib/view-models.ts`
- Test: `tests/lib/view-models.test.ts` or `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Add failing test**

Add a test proving database resource detail view models include at most 5 recent
audits scoped to the resource id.

Expected behavior:

- calls audit list with `targetResourceId`
- limits to 5 items if service returns more
- handles audit fetch failure gracefully if current project pattern already
  treats optional panels as non-blocking; otherwise fail fast consistently with
  existing service behavior

- [ ] **Step 2: Implement view-model wiring**

Update `ResourceDetailViewModel` with:

```ts
recentAudits?: AuditEventViewModel[];
```

Update `toResourceDetailViewModel` to fetch recent audits for database resources.

Use existing audit service functions. Do not introduce a new backend endpoint.

- [ ] **Step 3: Run tests**

Run:

```bash
npm run test -- tests/lib/view-models.test.ts tests/resource-detail-page.test.tsx
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add types/view-models.ts lib/view-models.ts tests/lib/view-models.test.ts tests/resource-detail-page.test.tsx
git commit -m "feat: add recent audit context to database detail view model"
```

---

### Task 4: Render Workbench On Resource Detail Page

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/resource-detail-page.test.tsx`
- Optional E2E update: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Add page-level assertions**

Update resource detail page tests to assert:

- database cluster page renders operator verdict
- database cluster page renders member summary
- database instance page renders parent cluster and connection context
- recent audit section renders when `recentAudits` exists

- [ ] **Step 2: Add i18n keys**

Add keys under a clear namespace such as:

```json
"databaseOperator": {
  "title": "...",
  "verdict": {
    "healthy": "...",
    "needsAttention": "...",
    "critical": "...",
    "unknown": "..."
  },
  "facts": {
    "resource_health_critical": "...",
    "members_warning_or_critical": "...",
    "lifecycle_needs_attention": "...",
    "resource_health_unknown": "...",
    "all_known_members_healthy": "..."
  }
}
```

Add both English and Chinese. Do not leave English enum text in Chinese UI.

- [ ] **Step 3: Render component**

In `app/(console)/resources/[id]/page.tsx`, render
`DatabaseOperatorWorkbench` only for:

- `database_cluster`
- `database_instance`
- `database_proxy` if profile data makes it useful; otherwise keep proxy out of
  scope for this phase

- [ ] **Step 4: Run tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx tests/components/database-operator-workbench.test.tsx
```

Expected: pass.

- [ ] **Step 5: Update E2E if stable**

If selectors are stable, update `e2e/operator-database-workflow.spec.ts` to
assert the operator verdict and member summary. Do not make E2E brittle with
exact counts unless seed data is stable.

- [ ] **Step 6: Commit**

```bash
git add 'app/(console)/resources/[id]/page.tsx' messages/en.json messages/zh-CN.json tests/resource-detail-page.test.tsx
git add components/resources/database-operator-workbench.tsx tests/components/database-operator-workbench.test.tsx
git add e2e/operator-database-workflow.spec.ts
git commit -m "feat: render database operator workbench on resource detail"
```

---

### Task 5: Full Verification And Live Review

**Files:**
- No further code changes expected.

- [ ] **Step 1: Run static and unit checks**

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

Run frontend on `3000` with same-origin API:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

```text
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Check:

- cluster detail shows verdict and member summary
- instance detail shows parent cluster and connection context
- recent audit context appears if data exists
- topology still loads
- no CORS errors
- no browser console errors

- [ ] **Step 4: Final status**

Run:

```bash
git status --short --branch
git log --oneline -8
```

Expected: clean working tree on `feat/phase-19-database-operator-workbench`.

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
- operator verdict behavior
- cluster member summary behavior
- instance detail behavior
- recent audit context behavior
- live browser verification for one cluster and one instance
- full verification matrix
- no backend changes
- no product write operations
- no topology editing
- no SQL execution
- no broad output suppression
- no tag/push/release
- no AI co-author

