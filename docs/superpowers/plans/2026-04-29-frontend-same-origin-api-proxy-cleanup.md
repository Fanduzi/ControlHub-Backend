# Frontend Same-Origin API Proxy Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make browser API requests same-origin through `/__api` so manual frontend acceptance on `3000` and Playwright on `3100` both work without CORS failures.

**Architecture:** Split API base selection by runtime: server-side code uses `CONTROLHUB_API_BASE_URL`, browser-side code defaults to `/__api`. Add a Next rewrite from `/__api/:path*` to `CONTROLHUB_API_PROXY_URL` or the backend. Keep `e2e/api-proxy.mjs` for recording and add a small allowed-origin fallback.

**Tech Stack:** Next.js rewrites, existing `services/api-client.ts`, Playwright webServer env, Node.js E2E API proxy, Vitest, Playwright.

---

## File Structure

- Modify `services/api-client.ts`
  - Runtime-aware API base selection.
- Modify `next.config.ts`
  - Add async `rewrites()` for `/__api/:path*`.
- Modify `playwright.config.ts`
  - Set server and browser API env consistently.
- Modify `e2e/api-proxy.mjs`
  - Replace hardcoded CORS origin with whitelist echo.
- Add or modify tests:
  - `tests/services/api-client.test.ts` or nearest existing API client test.
  - `tests/e2e-api-proxy.test.ts` if a proxy unit test pattern exists; otherwise test the CORS helper by extracting a pure helper.
- Run browser E2E and live checks.

Do not modify backend code. Do not change product UI.

---

### Task 1: Make API Base Runtime-Aware

**Files:**
- Modify: `services/api-client.ts`
- Test: nearest existing service/API client test file, or create `tests/services/api-client.test.ts`

- [ ] **Step 1: Add a failing test for runtime base selection**

If `tests/services/api-client.test.ts` does not exist, create it. Test a pure
exported helper so behavior is not tied to global fetch.

Expected helper signature:

```ts
export function resolveApiBaseUrl(): string
```

Test cases:

```ts
import { describe, expect, it, vi, afterEach } from "vitest";
import { resolveApiBaseUrl } from "@/services/api-client";

describe("resolveApiBaseUrl", () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    vi.unstubAllEnvs();
    if (originalWindow === undefined) {
      // @ts-expect-error test cleanup
      delete globalThis.window;
    } else {
      globalThis.window = originalWindow;
    }
  });

  it("uses /__api in the browser by default", () => {
    // @ts-expect-error test browser runtime
    globalThis.window = {} as Window & typeof globalThis;
    vi.stubEnv("NEXT_PUBLIC_API_BASE_URL", "");
    expect(resolveApiBaseUrl()).toBe("/__api");
  });

  it("uses NEXT_PUBLIC_API_BASE_URL in the browser when explicitly set", () => {
    // @ts-expect-error test browser runtime
    globalThis.window = {} as Window & typeof globalThis;
    vi.stubEnv("NEXT_PUBLIC_API_BASE_URL", "/custom-api");
    expect(resolveApiBaseUrl()).toBe("/custom-api");
  });

  it("uses CONTROLHUB_API_BASE_URL on the server when set", () => {
    // @ts-expect-error test server runtime
    delete globalThis.window;
    vi.stubEnv("CONTROLHUB_API_BASE_URL", "http://localhost:8081");
    expect(resolveApiBaseUrl()).toBe("http://localhost:8081");
  });

  it("uses localhost backend on the server by default", () => {
    // @ts-expect-error test server runtime
    delete globalThis.window;
    vi.stubEnv("CONTROLHUB_API_BASE_URL", "");
    expect(resolveApiBaseUrl()).toBe("http://localhost:8080");
  });
});
```

- [ ] **Step 2: Run the targeted test and verify it fails**

Run:

```bash
npm run test -- tests/services/api-client.test.ts
```

Expected: fails because `resolveApiBaseUrl` is not exported or does not implement
runtime-aware behavior.

- [ ] **Step 3: Implement runtime base selection**

Update `services/api-client.ts`:

```ts
export function resolveApiBaseUrl(): string {
  if (typeof window !== "undefined") {
    return process.env.NEXT_PUBLIC_API_BASE_URL || "/__api";
  }

  return process.env.CONTROLHUB_API_BASE_URL || "http://localhost:8080";
}
```

Change `apiClient()` to call `resolveApiBaseUrl()` at request time instead of
using a module-level constant.

- [ ] **Step 4: Run targeted test**

Run:

```bash
npm run test -- tests/services/api-client.test.ts
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add services/api-client.ts tests/services/api-client.test.ts
git commit -m "fix: resolve api base by runtime"
```

---

### Task 2: Add Same-Origin Rewrite

**Files:**
- Modify: `next.config.ts`

- [ ] **Step 1: Add `/__api/:path*` rewrite**

Update `next.config.ts` so `nextConfig` includes:

```ts
const apiProxyTarget =
  process.env.CONTROLHUB_API_PROXY_URL ??
  process.env.CONTROLHUB_API_BASE_URL ??
  "http://localhost:8080";

const nextConfig: NextConfig = {
  turbopack: {
    root: turbopackRoot,
  },
  async rewrites() {
    return [
      {
        source: "/__api/:path*",
        destination: `${apiProxyTarget}/:path*`,
      },
    ];
  },
};
```

- [ ] **Step 2: Run static checks**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
```

Expected: both pass.

- [ ] **Step 3: Commit**

```bash
git add next.config.ts
git commit -m "fix: proxy browser api calls through same-origin rewrite"
```

---

### Task 3: Update Playwright Environment

**Files:**
- Modify: `playwright.config.ts`

- [ ] **Step 1: Ensure Playwright uses `/__api` for browser and `8081` for server/proxy**

Set the frontend webServer command to include:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8081 CONTROLHUB_API_PROXY_URL=http://localhost:8081 NEXT_PUBLIC_API_BASE_URL=/__api
```

If the command currently calls `e2e/harness/dev-server-wrapper.sh`, preserve that
wrapper and add the env vars before it.

Expected command shape:

```ts
command:
  "CONTROLHUB_API_BASE_URL=http://localhost:8081 CONTROLHUB_API_PROXY_URL=http://localhost:8081 NEXT_PUBLIC_API_BASE_URL=/__api bash e2e/harness/dev-server-wrapper.sh -p 3100",
```

- [ ] **Step 2: Run governance check**

Run:

```bash
npm run check:e2e-governance
```

Expected: pass. The command must not introduce `stderr: "ignore"` or broad
output redirection.

- [ ] **Step 3: Commit**

```bash
git add playwright.config.ts
git commit -m "test: route playwright browser api calls through same-origin proxy"
```

---

### Task 4: Fix API Proxy CORS Fallback

**Files:**
- Modify: `e2e/api-proxy.mjs`
- Test: create `tests/e2e-api-proxy-cors.test.ts` if no existing proxy test exists.

- [ ] **Step 1: Extract allowed-origin helper**

In `e2e/api-proxy.mjs`, add:

```js
export function getAllowedOrigins() {
  return (
    process.env.PLAYWRIGHT_PROXY_ALLOWED_ORIGINS ??
    "http://localhost:3000,http://localhost:3100"
  )
    .split(",")
    .map((origin) => origin.trim())
    .filter(Boolean);
}

export function resolveCorsOrigin(requestOrigin) {
  if (!requestOrigin) return null;
  return getAllowedOrigins().includes(requestOrigin) ? requestOrigin : null;
}
```

Update `setCorsHeaders(request, response)` to:

```js
function setCorsHeaders(request, response) {
  const allowedOrigin = resolveCorsOrigin(request.headers.origin);
  if (allowedOrigin) {
    response.setHeader("Access-Control-Allow-Origin", allowedOrigin);
    response.setHeader("Vary", "Origin");
    response.setHeader("Access-Control-Allow-Credentials", "true");
  }
  response.setHeader("Access-Control-Allow-Headers", "Authorization, Content-Type");
  response.setHeader("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS");
}
```

Update all calls from:

```js
setCorsHeaders(response);
```

to:

```js
setCorsHeaders(request, response);
```

- [ ] **Step 2: Add CORS helper tests**

Create `tests/e2e-api-proxy-cors.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";

describe("api proxy CORS origin resolution", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("allows localhost 3000 by default", async () => {
    const { resolveCorsOrigin } = await import("../e2e/api-proxy.mjs");
    expect(resolveCorsOrigin("http://localhost:3000")).toBe("http://localhost:3000");
  });

  it("allows localhost 3100 by default", async () => {
    const { resolveCorsOrigin } = await import("../e2e/api-proxy.mjs");
    expect(resolveCorsOrigin("http://localhost:3100")).toBe("http://localhost:3100");
  });

  it("rejects unknown origins", async () => {
    const { resolveCorsOrigin } = await import("../e2e/api-proxy.mjs");
    expect(resolveCorsOrigin("http://evil.localhost:3000")).toBeNull();
  });

  it("supports explicit allowed-origin env override", async () => {
    vi.stubEnv("PLAYWRIGHT_PROXY_ALLOWED_ORIGINS", "http://localhost:4000");
    const { resolveCorsOrigin } = await import("../e2e/api-proxy.mjs");
    expect(resolveCorsOrigin("http://localhost:4000")).toBe("http://localhost:4000");
    expect(resolveCorsOrigin("http://localhost:3000")).toBeNull();
  });
});
```

If importing `e2e/api-proxy.mjs` starts the server during tests, refactor the
server startup behind:

```js
if (import.meta.url === `file://${process.argv[1]}`) {
  server.listen(PORT, () => {
    console.log(`Playwright API proxy listening on http://localhost:${PORT}`);
  });
}
```

- [ ] **Step 3: Run targeted tests**

Run:

```bash
npm run test -- tests/e2e-api-proxy-cors.test.ts
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add e2e/api-proxy.mjs tests/e2e-api-proxy-cors.test.ts
git commit -m "fix: echo allowed origins in e2e api proxy"
```

---

### Task 5: Verify Manual Port 3000 And Playwright Port 3100

**Files:**
- No product files expected.

- [ ] **Step 1: Verify backend health**

Run:

```bash
curl -fsS http://localhost:8080/health
```

Expected:

```json
{"status":"ok"}
```

- [ ] **Step 2: Manual dev server on 3000**

Run frontend manually:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Open/login at:

```text
http://localhost:3000/login
```

Verify in browser:

```text
http://localhost:3000/resources/14?topologyDepth=2
```

Expected:

- topology loads
- browser network requests use `/__api/resources/14/topology?...`
- no CORS errors

- [ ] **Step 3: Playwright port 3100**

Stop manual dev server if needed, then run:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

Expected: all pass.

---

## Final Verification Matrix

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
git status --short --branch
```

All commands must pass.

## Final Report Requirements

Report:

- worktree path, branch, commit hashes
- exact API base behavior for browser and server
- exact rewrite behavior
- exact CORS fallback behavior
- evidence that manual `3000` no longer CORS-fails
- evidence that Playwright `3100` still passes
- files changed
- tests added/changed
- full verification matrix
- no backend changes
- no product UI changes
- no broad output suppression
- no tag/push/release
- no AI co-author

