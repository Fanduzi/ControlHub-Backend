# Phase 17 Database Operator Drilldown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a read-only database operator drilldown that lets operators move from inventory to database cluster, instance, relations, topology, and audit context without guessing.

**Architecture:** Backend Phase 17A completes the read model first. Frontend Phase 17B consumes that contract in cluster/instance detail panels. Frontend Phase 17C proves the workflow in a real browser with backend-backed E2E.

**Tech Stack:** Go, chi, MySQL, goose, OpenAPI, Schemathesis, Next.js App Router, React, Vitest, Playwright.

---

## File Map

Backend likely files:

- `internal/model/resource.go` — read-model structs for profile summary, relation view, and member view.
- `internal/repository/mysql/resource_repository.go` — SQL projections for detail, relations, and members.
- `internal/service/resource_service.go` — read workflow orchestration.
- `internal/api/resource_handler.go` — route and response wiring.
- `internal/api/resource_handler_test.go` — handler unit tests.
- `internal/repository/mysql/resource_repository_test.go` or `internal/integration/resource_test.go` — real MySQL checks.
- `internal/openapi/openapi.yaml` — contract update.

Frontend likely files:

- `types/resource.ts` — relation/member/profile summary types.
- `services/resources.ts` — new read contract calls.
- `lib/view-models.ts` — operator detail view model assembly.
- `app/(console)/resources/[id]/page.tsx` — database-specific detail layout entry.
- `components/resources/resource-detail-sheet.tsx` — sheet parity where practical.
- `components/resources/database-operator-panel.tsx` — cluster/instance operator sections.
- `components/blocks/cluster-members-table.tsx` — member row display.
- `components/blocks/resource-relation-panel.tsx` — readable relation rows.
- `e2e/operator-database-workflow.spec.ts` — end-to-end workflow gate.

---

## Task 1: Backend Contract Completion

**Files:**
- Modify: `internal/model/resource.go`
- Modify: `internal/repository/mysql/resource_repository.go`
- Modify: `internal/service/resource_service.go`
- Modify: `internal/api/resource_handler.go`
- Test: `internal/api/resource_handler_test.go`
- Test: `internal/integration/resource_test.go`
- Modify: `internal/openapi/openapi.yaml`

- [ ] **Step 1: Add failing handler tests for readable relations**

Add a test that calls `GET /resources/{id}/relations` and asserts each relation contains existing IDs plus readable related resource display fields. The test should fail before implementation because the response only contains bare IDs.

Run:

```bash
go test -count=1 ./internal/api -run TestGetResourceRelations_IncludesRelatedResourceSummary -v
```

Expected before implementation: FAIL because `relatedResourceDisplayName` or equivalent field is missing.

- [ ] **Step 2: Add failing handler tests for cluster members**

Add a test for `GET /resources/{id}/members` or the chosen equivalent endpoint. If using a new endpoint, assert:

```json
{
  "members": [
    {
      "resourceId": 22,
      "displayName": "Payment MySQL Primary Production",
      "resourceType": "database_instance",
      "resourceSubtype": "mysql",
      "profileSummary": {
        "hostname": "...",
        "port": 3306
      }
    }
  ]
}
```

Run:

```bash
go test -count=1 ./internal/api -run TestGetResourceMembers_ReturnsDatabaseClusterMembers -v
```

Expected before implementation: FAIL because endpoint or fields are missing.

- [ ] **Step 3: Implement model structs**

Add explicit structs for readable relation rows and member rows. Keep IDs numeric and preserve existing fields. Do not remove existing relation fields.

Run:

```bash
go test -count=1 ./internal/model ./internal/api
```

Expected: compile errors move to repository/service wiring until implemented.

- [ ] **Step 4: Implement repository queries**

Use parameterized SQL joins against `resources` and `resource_relations`.

Requirements:

- Relation rows must resolve the resource on the opposite side of the relation.
- Member rows must use `member_of` relations where `to_resource_id = cluster_id`.
- Do not use string interpolation for IDs.
- Do not introduce N+1 queries for relation rows.

Run:

```bash
go test -count=1 ./internal/repository/mysql ./internal/api
```

Expected after implementation: repository and handler tests pass.

- [ ] **Step 5: Populate profileSummary where data exists**

Use existing profile data source or resource profile projection. Populate only real fields. Leave missing fields absent/null.

Run:

```bash
go test -count=1 ./internal/api -run 'TestListResources|TestGetResource' -v
```

Expected: list/detail tests pass and assert no invented data.

- [ ] **Step 6: Add integration tests against seed data**

Add integration checks for:

- a known database cluster returns members
- a known database instance detail includes `clusterId`
- a known relation response includes readable related resource names
- `profileSummary` fields appear where seed data supports them

Run:

```bash
make test-integration
```

Expected: all integration tests pass.

- [ ] **Step 7: Update OpenAPI**

Document:

- relation readable fields
- cluster members endpoint or equivalent
- profile summary fields and nullability

Run:

```bash
make openapi-validate
make test-openapi-fuzz
```

Expected: OpenAPI validates and fuzz passes.

- [ ] **Step 8: Backend verification and commit**

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
git status --short --branch
```

Commit:

```bash
git add internal/model internal/repository internal/service internal/api internal/openapi
git commit -m "feat: complete database operator read model (Phase 17A)"
```

---

## Task 2: Frontend Database Operator Detail UX

**Files:**
- Modify: `types/resource.ts`
- Modify: `services/resources.ts`
- Modify: `lib/view-models.ts`
- Modify: `app/(console)/resources/[id]/page.tsx`
- Create or modify: `components/resources/database-operator-panel.tsx`
- Modify: `components/blocks/cluster-members-table.tsx`
- Modify: `components/blocks/resource-relation-panel.tsx`
- Test: `tests/resource-detail-page.test.tsx`
- Test: `tests/components/resource-relation-panel.test.tsx`

- [ ] **Step 1: Add failing type/service tests**

Write tests proving frontend can parse:

- `profileSummary.hostname`
- `profileSummary.port`
- `profileSummary.nodeCount`
- readable relation fields
- cluster member rows

Run:

```bash
npx vitest run tests/services/resources.test.ts
```

Expected before implementation: FAIL on missing fields or service method.

- [ ] **Step 2: Implement service and type wiring**

Update `types/resource.ts` and `services/resources.ts` to match Backend 17A exactly. Do not use frontend-only mock fields.

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npx vitest run tests/services/resources.test.ts
```

Expected: typecheck and service tests pass.

- [ ] **Step 3: Add failing page tests for cluster detail**

Assert a database cluster detail page shows:

- operator summary
- member table
- readable member display names
- profile summary node count when present
- no demo `resourceSummaries`

Run:

```bash
npx vitest run tests/resource-detail-page.test.tsx
```

Expected before UI implementation: FAIL on missing operator sections.

- [ ] **Step 4: Add failing page tests for instance detail**

Assert a database instance detail page shows:

- parent cluster link
- hostname
- port
- readable relation names
- audit/topology sections still present

Run:

```bash
npx vitest run tests/resource-detail-page.test.tsx
```

Expected before UI implementation: FAIL on missing instance operator sections.

- [ ] **Step 5: Implement database operator panel**

Create a focused component for database-specific panels. It should render:

- cluster member table for clusters
- parent cluster card for instances
- profile summary card
- readable relation panel
- empty states only when data is truly absent

Run:

```bash
npx vitest run tests/resource-detail-page.test.tsx tests/components/resource-relation-panel.test.tsx
```

Expected: tests pass.

- [ ] **Step 6: Run frontend full verification**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
git status --short --branch
```

Commit:

```bash
git add types services lib app components tests
git commit -m "feat: add database operator detail workflow (Phase 17B)"
```

---

## Task 3: Frontend Operator Workflow E2E

**Files:**
- Create: `e2e/operator-database-workflow.spec.ts`
- Modify: `package.json` if adding a script
- Reuse: `e2e/harness/backend-health.ts`
- Reuse: `e2e/harness/auth.ts`
- Reuse: `e2e/harness/console-guards.ts`

- [ ] **Step 1: Add workflow spec**

The test must:

1. health-check backend
2. login via UI
3. open `/resources`
4. find/open a database cluster
5. assert member table is visible
6. open a member instance
7. assert parent cluster/profile fields are visible
8. navigate to audits or topology context
9. assert no console warnings/errors and no 4xx/5xx network responses

Run:

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Expected before final UI stability: FAIL if any workflow step is not implemented.

- [ ] **Step 2: Fix only workflow defects**

Allowed fixes:

- selectors that are too brittle
- missing accessible labels
- missing links between cluster/instance
- console/network guard violations caused by frontend code

Not allowed:

- backend contract changes
- new product features
- SQL operations
- topology editing

- [ ] **Step 3: Full browser gate**

Run:

```bash
npm run test:e2e:smoke
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Audit output must contain no unexpected browser console warnings/errors.

- [ ] **Step 4: Final verification and commit**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
rm -rf .next test-results playwright-report smoke-*.png
git status --short --branch
```

Commit:

```bash
git add e2e package.json
git commit -m "test: cover database operator drilldown workflow (Phase 17C)"
```

---

## Execution Order

1. Backend Phase 17A.
2. Merge Backend 17A to backend local `main`.
3. Frontend Phase 17B.
4. Merge Frontend 17B to frontend local `main`.
5. Frontend Phase 17C.
6. Merge Frontend 17C to frontend local `main`.

Do not run 17B before 17A contract is merged unless implementing only a clearly marked skeleton with no final completion claim.

