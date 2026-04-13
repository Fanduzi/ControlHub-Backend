# Frontend Phase 7 Resource Profile Integration Worker Prompt

```text
You are the frontend phase-7 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Current goal:
Start replacing the frontend's static resource profile/details dependency with the real backend profile projection endpoint, while keeping the current UI stable.

Backend context:
- Backend already provides `GET /resources/{id}/profile`
- Backend commit for that work: `cfe82c8`
- Existing `/resources` and `/resources/{id}` contracts are unchanged
- The new profile endpoint is read-only and returns a typed profile projection

Scope:
- frontend only
- no backend changes
- no API contract changes
- no new business features
- no topology work
- no SQL work orders or SQL workbench
- no asset create/edit flows
- do not touch the new console-preferences feature unless required by the current files you touch

Primary problem to solve:
- the frontend still uses static local profile/detail maps in `lib/view-models.ts`
- this is acceptable for bootstrap, but it is now the wrong long-term source for resource profile information
- this phase should move profile rendering to the real backend endpoint in a controlled way

What to change:
1. Integrate `GET /resources/{id}/profile` into the resource view-model path
2. Replace static profile maps for:
   - full resource detail page
   - right-side resource detail sheet
3. Keep the current summary text behavior if necessary for now
4. Do not rewrite the entire view-model layer
5. Keep fallback behavior safe if profile is empty

Recommended approach:
- add a service function in `services/resources.ts` for `/resources/{id}/profile`
- add a wire type in `types/resource.ts` or a nearby type file for the profile response
- in `lib/view-models.ts`, fetch the real profile projection when building a `ResourceViewModel`
- normalize profile values to `Record<string, string>` only if the current UI requires that
- keep the UI components simple; do not push formatting complexity into them

Important guardrails:
- do not remove backend-driven core resource fields
- do not add fake joined names to wire types
- do not reintroduce mock data as the primary path
- do not widen to full topology integration
- do not change existing route structure

Read these files first:
1. /Users/fan/JsProjects/ControlHub/services/resources.ts
2. /Users/fan/JsProjects/ControlHub/lib/view-models.ts
3. /Users/fan/JsProjects/ControlHub/types/resource.ts
4. /Users/fan/JsProjects/ControlHub/types/view-models.ts
5. /Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx
6. /Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx
7. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml

Verification requirements:
1. Run:
   - npx vitest run
   - npm run build
   - npm run lint
2. Manually verify with live backend:
   - /resources
   - row click detail sheet
   - /resources/40000000-0000-0000-0000-000000000002
   - /resources/40000000-0000-0000-0000-000000000001
   - /databases
3. Confirm that profile sections now reflect backend `/profile` data, not only static local maps
4. Confirm empty profile handling does not break rendering

Nice-to-have only if low risk:
- reduce or isolate the remaining static summary/profile fallback data in `lib/view-models.ts`

Commit rules:
- frontend repo only
- no AI co-author metadata
- do not widen scope
- if you discover the backend `/profile` response is insufficient for current rendering, report the exact gap instead of inventing fields

Final report must include:
- changed files
- how `/resources/{id}/profile` was integrated
- what static profile data remains, if any
- test results
- manual verification results
- commit hash
- remaining risks

Do not stop at analysis. Implement and verify.
```
