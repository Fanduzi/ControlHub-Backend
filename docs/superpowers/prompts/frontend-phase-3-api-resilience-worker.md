# Frontend Phase 3 API Resilience Worker Prompt

```text
You are the frontend phase-3 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Read these files first:
1. /Users/fan/JsProjects/ControlHub/README.md
2. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
3. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md

Current frontend baseline:
- The frontend calls real backend endpoints for auth, resources, relations, audits, environments, owners, and roles.
- Wire types use backend camelCase contracts.
- View-model fields remain frontend-only presentation fields.
- Backend live MySQL verification may still be in progress.

Your role:
- Make the frontend resilient when real backend APIs are slow, unavailable, empty, or return errors.
- Preserve the existing product shell and visual direction.
- Do not add new product scope.

Scope for this round:
1. Add consistent error and empty states for pages that fetch backend data:
   - /overview
   - /resources
   - /resources/[id]
   - /cmdb
   - /databases
   - /audits
   - /settings
2. Add loading states where App Router route-level loading files make sense.
3. Ensure API failures do not crash the entire console with an unhelpful stack trace.
4. Ensure empty data sets render useful EmptyState blocks.
5. Keep login failure messages clear when backend is unavailable or credentials fail.
6. Keep service functions real-backend based; do not revert to mock data.
7. Update README with local integration steps against backend:
   - NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
   - npm run dev
   - pages to smoke-test

Do not do:
- Do not redesign the UI.
- Do not add SQL work orders, SQL review, query workbench, advanced permissions, topology graphs, or ClickHouse ingestion.
- Do not introduce global state management.
- Do not reintroduce mock data as primary runtime behavior.
- Do not change backend API contracts.
- Do not put presentation-only fields into wire types.

Implementation guidance:
- Prefer small reusable blocks over per-page duplicated error markup.
- Reuse existing EmptyState if present; improve it if necessary.
- Use Next.js route-level error.tsx/loading.tsx where appropriate.
- For server components that call backend services, handle expected empty arrays through UI and unexpected fetch failures through route error boundaries.
- For login, keep client-side form validation and display backend/API failures as readable messages.
- Keep the professional, dense control-console style.

Pages to verify manually:
- /login
- /overview
- /resources
- /resources/[id] for at least one known resource ID
- /cmdb
- /databases
- /audits
- /settings

Verification:
- npx vitest run
- npm run build
- npm run lint

If backend is available:
- NEXT_PUBLIC_API_BASE_URL=http://localhost:8080 npm run dev
- Smoke-test the pages listed above.

If backend is unavailable:
- Verify that pages show useful error boundaries or readable API error states rather than crashing silently.
- Report which live checks could not be completed.

Completion report must include:
- changed files
- tests run
- manual smoke-test status
- remaining API resilience gaps
- commit hash

Do not stop after analysis. Start implementing.
```
