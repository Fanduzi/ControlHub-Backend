# Frontend Phase 2 API Integration Worker Prompt

```text
You are the frontend phase-2 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md
2. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
3. /Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-1-worker.md
4. /Users/fan/JsProjects/ControlHub/README.md

Current frontend baseline:
- The app shell and phase-1 pages exist.
- Frontend wire types have been corrected to backend camelCase contracts.
- View-model fields such as environmentName, ownerName, actorLabel, targetResourceName, summary, and profile are frontend presentation fields, not backend wire fields.
- Current frontend services still use mock data.

Current backend contract:
- POST /auth/login
- GET /resources
- GET /resources/{id}
- GET /resources/{id}/relations
- GET /resources/{id}/audit-events
- GET /audit-events

Backend frozen wire rules:
- HTTP JSON uses camelCase.
- Resource has:
  id, resourceType, resourceSubtype, name, displayName, environmentId, ownerId, lifecycleStatus, healthStatus, source, externalId, labels, createdAt, updatedAt
- ResourceRelation has:
  id, fromResourceId, toResourceId, relationType, createdAt
- AuditEvent has:
  id, actorUserId, targetResourceId, eventType, result, createdAt
- Login request is:
  { email, password }
- Login response is:
  { token, role }
- Backend does not provide actorName, targetResourceName, environmentName, or ownerName in these endpoints.

Your role:
- Replace mock-only service behavior with real backend API integration for resources, relations, audits, and login.
- Preserve the existing UI shell, page structure, and view-model layer.
- Stay inside /Users/fan/JsProjects/ControlHub, except for reading backend docs/OpenAPI.

Scope for this round:
1. Add or complete a shared API client that reads:
   NEXT_PUBLIC_API_BASE_URL
   and defaults to http://localhost:8080 for local development.
2. Update services/resources.ts to call:
   - GET /resources
   - GET /resources/{id}
   - GET /resources/{id}/relations
3. Update services/audits.ts to call:
   - GET /audit-events
   - GET /resources/{id}/audit-events
4. Update services/auth.ts to call:
   - POST /auth/login
5. Preserve local view-model enrichment in lib/view-models.ts.
6. Keep settings dictionaries local for now unless backend /environments, /owners, and /roles are already available when you start. If they are available, integrate them through services/settings.ts using the same API client and camelCase fields.
7. Keep tests passing and update tests only where the service contract changed.
8. Update README with the backend URL requirement.

Do not do:
- Do not redesign the UI.
- Do not add SQL work orders, SQL review, query workbench, or change workflows.
- Do not add a global state management library.
- Do not put presentation-only fields into wire types.
- Do not reintroduce snake_case API fields.
- Do not assume actorName, targetResourceName, ownerName, or environmentName are backend fields.

Implementation guidance:
- Keep wire types in types/*.ts aligned with OpenAPI.
- If pages need display names, derive them in lib/view-models.ts from local dictionaries or resource lookup.
- If the backend is not running during tests, unit tests should not require a live backend.
- It is acceptable for component tests to mock service modules or use static fixtures.
- For runtime, real services should call the backend through the shared API client.

Expected service behavior:

login({ email, password })
returns:
{
  token: string,
  role: string
}

listResources()
returns Resource[] from GET /resources items.

getResourceById(id)
returns Resource | null from GET /resources/{id}; map 404 to null.

listResourceRelations(resourceId)
returns ResourceRelation[] from GET /resources/{id}/relations items.

listAuditEvents()
returns AuditEvent[] from GET /audit-events items.

listResourceAuditEvents(resourceId)
returns AuditEvent[] from GET /resources/{id}/audit-events items.

Verification:
Run and report:
- npx vitest run
- npm run build
- npm run lint

If possible, run local integration with backend:
- Start backend at http://localhost:8080
- Set NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
- Run npm run dev
- Smoke-test /login, /resources, /audits, /databases

If backend is not available, say exactly what could not be verified and why.

Completion report must include:
- changed files
- tests run
- whether services now call real backend endpoints
- remaining mock-only areas
- remaining contract assumptions
- commit hash

Do not stop after analysis. Start implementing.
```
