# Frontend Same-Origin API Proxy Cleanup Design

## Background

During manual acceptance, the frontend was started on `http://localhost:3000`
while `NEXT_PUBLIC_API_BASE_URL` pointed browser requests at
`http://localhost:8081`. The E2E API proxy returned:

```text
Access-Control-Allow-Origin: http://localhost:3100
```

That worked for Playwright's default dev server on `3100`, but failed for manual
acceptance on `3000`. Browser requests to topology and other API endpoints were
blocked by CORS even though the backend and proxy returned valid data.

Changing the frontend port from `3000` to `3100` is only a workaround. The
browser should not need to know whether the API target is the backend
(`8080`) or the E2E recording proxy (`8081`).

## Goal

Make browser-side API traffic same-origin by default:

```text
Browser → http://localhost:<frontend-port>/__api/... → Next rewrite → backend/proxy
```

This removes CORS from normal frontend development and manual acceptance while
preserving E2E request recording through `e2e/api-proxy.mjs`.

## Architecture

Use two API base concepts:

- **Server-side API base**
  - Environment variable: `CONTROLHUB_API_BASE_URL`
  - Default: `http://localhost:8080`
  - Used when `apiClient()` runs in server components or other server-side code.

- **Browser-side API base**
  - Environment variable: `NEXT_PUBLIC_API_BASE_URL`
  - Default: `/__api`
  - Used when `apiClient()` runs in the browser.

Use one rewrite target:

- **Next rewrite target**
  - Environment variable: `CONTROLHUB_API_PROXY_URL`
  - Default: `CONTROLHUB_API_BASE_URL ?? http://localhost:8080`
  - Rewrites `/__api/:path*` to `${CONTROLHUB_API_PROXY_URL}/:path*`.

For E2E:

- `CONTROLHUB_API_BASE_URL=http://localhost:8081`
- `CONTROLHUB_API_PROXY_URL=http://localhost:8081`
- `NEXT_PUBLIC_API_BASE_URL=/__api`

This keeps server-side fetches and browser-side fetches using the same E2E
recording proxy, but the browser only sees same-origin `/__api`.

## Why Not Just Fix CORS?

The E2E proxy should still support a small CORS whitelist as a defensive
fallback, but that is not the primary solution.

Reasons:

- Manual acceptance may use `3000`, while Playwright uses `3100`.
- More frontend ports may appear in future worktrees.
- Credentialed CORS cannot safely use `Access-Control-Allow-Origin: *`.
- Same-origin requests are simpler to reason about and avoid browser-only CORS
  failures that do not reproduce in `curl`.

## Required Behavior

### Browser Requests

In normal local development on `3000`, browser `fetch` calls should target:

```text
http://localhost:3000/__api/...
```

In Playwright on `3100`, browser `fetch` calls should target:

```text
http://localhost:3100/__api/...
```

The browser should not directly call `8080` or `8081` for application data.

### Server Requests

Server-side rendering can call the backend/proxy directly because CORS does not
apply to server-side fetches.

### E2E Request Recording

`e2e/api-proxy.mjs` must still record requests for:

- `/resources`
- `/audit-events`
- `/resources/*/topology`

The recorded request paths must remain backend paths, not `/__api/...`.

### Proxy CORS Fallback

`e2e/api-proxy.mjs` should no longer hardcode a single allowed origin.

Default allowed origins:

```text
http://localhost:3000
http://localhost:3100
```

Optional environment override:

```text
PLAYWRIGHT_PROXY_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3100
```

When the incoming `Origin` is allowed, the proxy should echo that exact origin.
If no `Origin` header exists, it should avoid inventing a misleading CORS
origin unless needed for an OPTIONS response.

Do not use `Access-Control-Allow-Origin: *` while
`Access-Control-Allow-Credentials: true` is present.

## Non-Goals

- Do not change backend API paths.
- Do not change authentication behavior.
- Do not change product UI.
- Do not remove `e2e/api-proxy.mjs`.
- Do not remove E2E request recording.
- Do not introduce a new proxy server dependency.
- Do not broaden CORS to all origins.

## Acceptance Criteria

- Manual frontend on `http://localhost:3000` can load resource topology without
  CORS errors.
- Playwright frontend on `http://localhost:3100` can load resource topology
  without CORS errors.
- `npm run test:e2e:smoke` passes.
- `npm run test:e2e:interaction` passes.
- `npm run test:e2e` passes.
- `npm run check:e2e-governance` passes.
- `npx tsc --noEmit -p tsconfig.json` passes.
- `npm run lint` passes.
- `npm run test` passes.
- `npm run build` passes.
- No backend files are modified.

