# Frontend Phase 17C: Database Operator Workflow E2E

You are implementing Frontend Phase 17C for ControlHub: end-to-end browser coverage for the database operator drilldown workflow.

Repository:
`/Users/fan/JsProjects/ControlHub`

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-27-phase-17-database-operator-drilldown-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-27-phase-17-database-operator-drilldown.md`
- `/Users/fan/JsProjects/ControlHub/e2e/harness/backend-health.ts`
- `/Users/fan/JsProjects/ControlHub/e2e/harness/auth.ts`
- `/Users/fan/JsProjects/ControlHub/e2e/harness/console-guards.ts`
- `/Users/fan/JsProjects/ControlHub/e2e/operator-console-smoke.spec.ts`

## Startup Check

Create a dedicated worktree from frontend main after Phase 17B is merged:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-17c-operator-workflow-e2e -b feat/phase-17c-operator-workflow-e2e main
cd .worktrees/frontend-phase-17c-operator-workflow-e2e
git status --short --branch
git log --oneline -8
```

Expected:

- path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is `feat/phase-17c-operator-workflow-e2e`
- worktree is clean
- frontend main includes Phase 17B

Stop and report if any condition is false.

## Exact Scope

Allowed:

- Add Playwright workflow tests.
- Add small accessibility labels/test IDs only if needed for stable selectors.
- Fix browser-console/network guard violations caused by frontend code.

Not allowed:

- backend changes
- product feature changes
- SQL execution
- work orders
- topology editing
- restoring CMDB navigation
- restoring demo `resourceSummaries`
- broad visual redesign

## Required Test

Create:

`e2e/operator-database-workflow.spec.ts`

The workflow must:

1. Verify backend health.
2. Login as admin through the real UI.
3. Open `/resources`.
4. Search or filter to a database cluster.
5. Open the cluster detail page.
6. Assert the cluster operator summary is visible.
7. Assert the member instances table is visible and contains readable names.
8. Click one member instance.
9. Assert the instance detail page shows parent cluster, hostname/port/profile fields where present.
10. Assert topology or audit context is reachable from the detail page.
11. Assert no unexpected browser console warnings/errors.
12. Assert no unexpected 4xx/5xx network responses.

Use the existing console/network guard from Phase 16C. Do not weaken it.

## Selector Policy

Prefer accessible selectors:

- role/name
- headings
- links with visible names
- table row text

Only add `data-testid` if:

- accessible selector is unstable or ambiguous
- the test ID describes product semantics, not layout

## Verification

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

With backend running:

```bash
npm run test:e2e:smoke
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Audit smoke/workflow output for:

```bash
rg "Warning.*NO_COLOR|Warning.*FORCE_COLOR|has either width or height modified|Unexpected console warnings|Console errors|Network errors" /tmp/controlhub-*.log
```

Expected: no matches.

If backend is not running, start it only if instructed or if local workflow allows. If you start it, stop it after verification.

## Artifact Cleanup

After E2E:

```bash
rm -rf .next test-results playwright-report smoke-*.png
git status --short --branch
find . -maxdepth 2 \( -name '.next' -o -name 'test-results' -o -name 'playwright-report' -o -name 'smoke-*.png' -o -name 'qa-test*.js' \) -print
```

Expected:

- clean git status except intended source changes before commit
- no generated artifacts

## Commit

Commit after checks pass:

```bash
git add e2e package.json app components tests
git commit -m "test: cover database operator drilldown workflow (Phase 17C)"
```

Only add files you changed.

No AI co-author. No tag. No push. No release.

## Final Report

Return:

1. Worktree path, branch, commit hash.
2. Exact workflow covered.
3. Changed files table.
4. Verification matrix.
5. Smoke/workflow output audit result.
6. Backend process handling:
   - already running or started temporarily
   - stopped after verification if started by you
7. Artifact cleanup result.
8. Confirmation:
   - no backend changes
   - no product feature changes
   - no SQL execution/work orders
   - no topology editing
   - no CMDB nav restore
   - no demo resourceSummaries restore
   - no tag/push/release
   - no AI co-author
   - clean `git status`

