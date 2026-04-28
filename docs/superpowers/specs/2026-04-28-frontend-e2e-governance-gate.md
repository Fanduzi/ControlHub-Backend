# Frontend E2E Governance Gate Design

## Background

Phase 18A added an interaction-stability E2E gate. Phase 18B cleaned up the
E2E suite so smoke, interaction, and full Playwright tests are green and no
longer rely on broad stderr suppression.

The remaining risk is process drift. Future frontend workers may reintroduce
the same failure modes:

- using `loginViaApi()` for pages that render server components and therefore
  need real UI login/session setup
- adding `stderr: "ignore"` or equivalent broad output suppression
- generating screenshots on every successful test run
- treating `npm run test:e2e` failures as "pre-existing" without a concrete
  classification table
- adding browser tests that do not use console/network guards
- changing sheets, dropdowns, links, theme/accent, or navigation without running
  the interaction gate

Phase 18C exists to turn those lessons into hard project documentation and a
small automated policy check so the rules are discoverable and enforceable.

## Goal

Create a durable frontend E2E governance gate that documents and verifies the
minimum rules for browser QA:

- smoke E2E must stay green
- interaction E2E must stay green
- full E2E must stay green
- server-component page tests must use `loginViaUI()` unless explicitly
  justified
- browser tests must use console and network guards
- success-path screenshots are forbidden
- broad stderr/stdout suppression is forbidden
- known runtime noise must use exact, documented allowlists only

## Non-Goals

- Do not change backend code.
- Do not add product UI.
- Do not redesign resource, database, topology, audit, or settings pages.
- Do not add new E2E product journeys.
- Do not restore `/cmdb` navigation.
- Do not restore demo `resourceSummaries`.
- Do not replace Playwright.

## Required Policy Decisions

### UI Login Is Default For SSR Page E2E

Use `loginViaUI(page)` for E2E tests that navigate to application pages.

Rationale:

- `loginViaApi()` only seeds client-side/sessionStorage state.
- Next.js server components fetch during server rendering and cannot read that
  client-only state.
- Using `loginViaApi()` for SSR pages caused empty tables, auth redirects, and
  false failures.

Allowed exception:

- API-only helper tests may use `loginViaApi()` if they do not depend on server
  component rendering.
- Any exception must include an inline comment explaining why SSR auth is not
  involved.

### Console And Network Guards Are Mandatory

Every browser E2E spec that loads application pages must collect and assert:

- browser `console.error`
- unexpected browser `console.warning`
- 4xx/5xx network responses

Allowed messages must be minimal and local to the spec. Do not add global
warning suppression unless the exact message is understood and documented.

### No Broad Process Output Suppression

Forbidden:

- `stderr: "ignore"`
- `stdout: "ignore"` for application web servers
- shell redirection that drops complete logs, such as `2>/dev/null`
- broad regex filters such as `grep -v "TypeError"` or `grep -v "Warning"`

Allowed:

- an exact allowlist for one known runtime noise line, documented with the
  upstream/runtime reason and implemented in a visible wrapper.

Current known allowed pattern:

```text
controller[kState].transformAlgorithm
```

This is the Node.js v22 TransformStream race observed in dev-server stderr. Any
filter for it must match only that specific pattern and pass all other output
through.

### Success-Path Screenshots Are Forbidden

E2E tests must not take screenshots after every passing test.

Allowed:

- failure-only screenshots guarded by:

```ts
if (testInfo.status !== testInfo.expectedStatus) {
  await page.screenshot({ path, fullPage: true });
}
```

All screenshot patterns must be gitignored.

### Full E2E Failures Need Classification

If `npm run test:e2e` is not fully green, workers cannot write "pre-existing"
as a blanket statement.

Each failing test must be classified as one of:

- `obsolete-test`
- `real-regression`
- `environment-dependent`
- `covered-by-new-gate`
- `needs-product-decision`

The classification must include the failing locator/assertion, affected URL,
root cause, and next action.

## Required Artifacts

Frontend Phase 18C must add:

- `docs/e2e-governance.md` in the frontend repo
- `scripts/check-e2e-governance.mjs` in the frontend repo
- `npm run check:e2e-governance`

The policy check must fail on:

- `stderr: "ignore"` or `stdout: "ignore"` in Playwright config
- broad shell output suppression in Playwright webServer commands
- success-path screenshots in E2E specs
- application E2E specs using `loginViaApi()` without an inline exception marker
- E2E specs importing neither `collectConsoleMessages` nor `assertClean`

## Acceptance Criteria

- `npm run check:e2e-governance` passes.
- `npm run test:e2e:smoke` passes.
- `npm run test:e2e:interaction` passes.
- `npm run test:e2e` passes.
- `npx tsc --noEmit -p tsconfig.json` passes.
- `npm run lint` passes.
- `npm run test` passes.
- `npm run build` passes.
- No backend files are modified.
- No product UI files are modified unless the policy check exposes a real
  existing violation that cannot be fixed in tests/docs only.

