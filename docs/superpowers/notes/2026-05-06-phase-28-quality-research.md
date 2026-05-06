# Phase 28 Quality Research Notes

## Research Questions

1. How should Playwright prevent stale dev server reuse when proxy env vars matter?
2. How should ControlHub split PR, merge, and nightly gates?
3. How should OpenAPI validation, fuzzing, and frontend types work together?
4. Should ControlHub add visual regression now?
5. How should flaky E2E failures be classified?

## Findings

### 1. Playwright webServer reuseExistingServer and Stale Dev Server Risk

| Aspect | Detail |
|---|---|
| **Source** | [Playwright Docs -- Web Server](https://playwright.dev/docs/test-webserver), [Playwright API -- testConfig.webServer](https://playwright.dev/docs/test-configuration) |
| **Finding** | `reuseExistingServer: true` checks only whether a process is listening on the configured port/URL. It performs **no validation** of the server's configuration, environment variables, or code version. If the server was started with different env vars (e.g., missing `E2E_API_PROXY_PORT`), Playwright will reuse it silently and tests will fail with confusing errors. |
| **ControlHub risk** | Phase 27 and 27B both experienced failures from a stale `:3100` frontend dev server started without E2E proxy env vars, and a stale `:8081` API proxy not recording requests. |
| **Decision** | **Adopted** — Preserve the preflight check in the release hardening checklist for `:3100` and `:8081`. The existing `check:e2e-governance` script does not cover port/process state. The release checklist now documents the exact `lsof` commands and the rule to verify before killing. Do not auto-kill. |

### 2. Playwright Traces and Reporters for Flaky Failure Diagnosis

| Aspect | Detail |
|---|---|
| **Source** | [Playwright Docs -- Trace Viewer](https://playwright.dev/docs/trace-viewer-intro), [Playwright Docs -- Test Reporters](https://playwright.dev/docs/test-reporters) |
| **Finding** | Playwright traces capture action log, DOM snapshots, screenshots, network requests, and console output per action. The `on-first-retry` mode captures only the failing attempt without overhead on passing tests. Official docs explicitly warn against `trace: "on"` for every test due to performance overhead. |
| **ControlHub risk** | E2E failures in recent phases were difficult to classify because the only diagnostic was the Playwright error message. Traces would have shown the exact DOM state, pending network requests, and console errors at the point of failure. |
| **Decision** | **Adopted** — Set `trace: "on-first-retry"` in the Playwright config. This adds zero overhead for passing tests and captures full diagnostics on failures. The `html` reporter should be used so traces are viewable with `npx playwright show-report`. |

### 3. OpenAPI Validation and Schemathesis Fuzzing as API Contract Gates

| Aspect | Detail |
|---|---|
| **Source** | [Schemathesis Docs](https://schemathesis.readthedocs.io/en/stable/), ControlHub `internal/integration/openapi_fuzz_test.go`, `scripts/openapi-fuzz.sh` |
| **Finding** | OpenAPI schema validation (`make openapi-validate`) checks YAML structure and JSON Schema compliance. Schemathesis fuzzing adds runtime verification: it generates inputs from the schema, sends real HTTP requests, and checks for 5xx errors, status code conformance, content-type conformance, and response schema conformance. These are complementary layers — static validation catches schema errors, fuzzing catches runtime contract violations. |
| **ControlHub risk** | Phases 16–26 added new read-model fields and response shapes. Schema validation alone cannot catch runtime divergences between the spec and actual handler behavior. Fuzzing caught issues during development that schema validation missed. |
| **Decision** | **Adopted** — Keep `make openapi-validate` as a per-commit gate. Keep `make test-openapi-fuzz` as a merge gate for API changes and as a nightly/release gate otherwise. The fuzz run is Docker-dependent and too slow for per-commit, but too valuable to skip entirely. |

### 4. PR vs Merge vs Nightly Gate Layering

| Aspect | Detail |
|---|---|
| **Source** | ControlHub `docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`, established project patterns from Phases 16–27 |
| **Finding** | ControlHub has no CI runner. All gates are local. The practical layering for a 1–2 developer project without CI is: **per-commit** gates (fast, no Docker, no backend), **merge** gates (full local suite including Docker and E2E), and **nightly/release** gates (fuzz, visual regression if adopted). Industry standard CI layering (PR fast, merge medium, nightly slow) maps to this local pattern. |
| **ControlHub risk** | Without documented gate layers, developers may skip slow gates or run unnecessary slow gates on trivial changes. |
| **Decision** | **Adopted** — Document the three tiers in the release hardening checklist: per-commit (typecheck, lint, unit, build, openapi-validate), merge (full E2E + Docker integration when applicable), nightly/release (fuzz, visual regression if adopted). |

### 5. Visual Regression Tradeoffs for This Internal Data Console

| Aspect | Detail |
|---|---|
| **Source** | ControlHub `docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md` (explicitly lists visual regression as a non-goal), Playwright screenshot comparison API |
| **Finding** | Visual regression tools (Playwright `toHaveScreenshot()`, Chromatic, Percy) capture pixel-level snapshots and diff against baselines. For a data-heavy internal console with no marketing or brand review requirements, the maintenance cost is high: baselines break on data changes, font rendering differences, and theme updates. The current ControlHub pain points are semantic/interactivity bugs (wrong signal, missing data, stale proxy), not pixel drift. |
| **ControlHub risk** | Adding visual regression would create a high-maintenance baseline that breaks on every seed data change or component refactor without catching the actual bugs the project experiences. |
| **Decision** | **Rejected** for Phase 28. The current testing investment should focus on semantic correctness and interactivity stability, not pixel comparison. If layout churn becomes a recurring issue, reconsider. The Playwright `toHaveScreenshot()` API is available without additional infrastructure if a targeted need arises. |

## Adopted Recommendations

1. **Stale process preflight** — The release hardening checklist includes `lsof` checks for `:3100` and `:8081` with explicit verify-before-kill guidance.
2. **Playwright trace on first retry** — Set `trace: "on-first-retry"` to capture failure diagnostics without overhead on passing tests.
3. **OpenAPI dual-layer gate** — Schema validation per-commit, fuzzing per-merge/nightly.
4. **Three-tier gate layering** — Per-commit (fast), merge (full), nightly/release (heavy). Documented in the release hardening checklist.
5. **Full E2E as phase-close gate** — Already enforced by project convention. Now documented explicitly.

## Deferred Recommendations

1. **Browser matrix expansion** — All E2E runs use Chromium. Defer Firefox/WebKit until cross-browser issues appear. No internal users have reported browser-specific bugs.
2. **Automated contract smoke between OpenAPI and TypeScript types** — The E2E recorded-request harness and full E2E provide indirect coverage. A direct contract smoke (e.g., generating TypeScript types from OpenAPI) could be added if frontend/backend drift becomes a recurring issue.
3. **Seed data constants** — E2E tests reference seed resources by name and ID. These are stable in migration `0004` but not centralized as typed constants. Defer until seed data churn occurs.

## Rejected Recommendations

1. **Visual regression** — Current pain is semantic/interactivity, not pixel drift. Maintenance cost exceeds value for an internal data console with no brand/marketing review.
2. **Broad retries as flake management** — Adding `retries: 2` to Playwright config would hide root causes. Failures must be classified, not retried away. The only retry-related adoption is `trace: "on-first-retry"` for diagnostics, which does not suppress failures.
3. **Skipping known failures without evidence** — Every E2E failure must be classified with the failure classification table from the release hardening checklist. "Pre-existing" is not a valid classification without identical main-branch comparison evidence.

## Source Links

| Topic | Source |
|---|---|
| Playwright webServer | https://playwright.dev/docs/test-webserver |
| Playwright trace viewer | https://playwright.dev/docs/trace-viewer-intro |
| Playwright reporters | https://playwright.dev/docs/test-reporters |
| Schemathesis docs | https://schemathesis.readthedocs.io/en/stable/ |
| ControlHub quality gates design | `docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md` |
| ControlHub E2E governance | `docs/superpowers/specs/2026-04-28-frontend-e2e-governance-gate.md` |
