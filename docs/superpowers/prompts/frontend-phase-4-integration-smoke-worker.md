# Frontend Phase 4 Integration Smoke Worker Prompt

```text
You are the frontend phase-4 integration smoke worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Current goal:
Only close the frontend/backend integration loop and clean frontend repository hygiene. Do not develop new product features.

Context:
- The backend runs at http://localhost:8080.
- The backend metadata store is MySQL 8.0+.
- The frontend already calls real backend APIs for auth, resources, relations, audits, environments, owners, and roles.
- Wire types must match backend camelCase contracts.
- View-model fields remain frontend-only presentation fields.
- Latest known frontend commit before this round: f2842fd feat: add API resilience with error boundaries, loading states, and empty states.

Read these files first:
1. /Users/fan/JsProjects/ControlHub/README.md
2. /Users/fan/JsProjects/ControlHub/services/api-client.ts
3. /Users/fan/JsProjects/ControlHub/services/auth.ts
4. /Users/fan/JsProjects/ControlHub/services/resources.ts
5. /Users/fan/JsProjects/ControlHub/services/audits.ts
6. /Users/fan/JsProjects/ControlHub/services/settings.ts
7. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml

Hard scope boundary:
- Do not add SQL work orders.
- Do not add SQL query workbench.
- Do not add asset edit/create flows.
- Do not add advanced permissions.
- Do not add topology graph.
- Do not redesign the UI.
- Do not introduce a new state management library.
- Do not reintroduce mock data as the primary runtime path.
- Do not change backend API contracts.

Tasks:
1. Check repository state first:
   - git status --short
   - If only .idea/ is untracked, add .idea/ to .gitignore.
   - Do not commit IDE private config.
   - If there are other untracked or modified files, report them before editing. Do not overwrite unrelated changes.

2. Confirm local integration docs:
   - README must clearly document:
     - NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
     - npm run dev
     - backend must be running at http://localhost:8080 first
   - If README is already clear, do not make noisy doc edits.

3. Run real frontend/backend smoke test:
   - Start frontend with:
     NEXT_PUBLIC_API_BASE_URL=http://localhost:8080 npm run dev
   - Use the backend seed user credentials documented in backend README or seed data.
   - Do not invent or hardcode new credentials.

4. Verify these pages in the browser:
   - /login
   - /overview
   - /resources
   - /resources/[id] for one real resource ID
   - /cmdb
   - /databases
   - /audits
   - /settings

5. Verify key interactions:
   - Login succeeds and stores the backend token.
   - Resource list shows the seeded backend resources.
   - Clicking a resource row opens the right-side detail sheet.
   - Full resource detail page loads by real resource ID.
   - Resource relations load from /resources/{id}/relations.
   - Resource audit timeline loads from /resources/{id}/audit-events.
   - Audit page loads from /audit-events.
   - Settings page loads /environments, /owners, and /roles.
   - If the backend is stopped, route-level error UI remains readable.

6. Fix only real integration bugs:
   - API base URL mistakes
   - sessionStorage token handling mistakes
   - Authorization header mistakes
   - field-name mismatches
   - nullable value crashes
   - route/link/resource ID mistakes
   - confusing login/network error messages

7. Do not fix by weakening the contract:
   - Do not add frontend assumptions that backend returns actorName, targetResourceName, environmentName, or ownerName.
   - Keep those as frontend view-model enrichment only.
   - Do not put presentation-only fields into wire types.

8. Run verification:
   - npx vitest run
   - npm run build
   - npm run lint

9. Commit rules:
   - If changes are limited to .gitignore/README or real integration bugfixes, commit them.
   - Do not include AI co-author metadata in the commit message.
   - If a fix requires backend API contract changes, do not work around it. Report the exact endpoint, field, and expected shape.

Completion report must include:
- changed files
- whether .idea/ was handled
- actual NEXT_PUBLIC_API_BASE_URL used
- login result
- per-page smoke-test result
- fixed integration issues
- unresolved issues needing backend help
- verification command results
- commit hash if committed

Do not stop after analysis. Start verification and only fix issues found inside this scope.
```
