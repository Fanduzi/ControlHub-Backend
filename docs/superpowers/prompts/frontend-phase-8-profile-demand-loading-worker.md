# Frontend Phase 8 Profile Demand Loading Worker Prompt

```text
You are the frontend phase-8 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Current goal:
Remove the new `/resources/{id}/profile` N+1 behavior from list pages by changing profile fetching to on-demand loading, while keeping the current detail UI stable.

Context:
- Frontend phase 7 already integrated `GET /resources/{id}/profile`.
- That removed the static profile map, which is correct directionally.
- But the current implementation now adds one profile request per resource when building `ResourceViewModel`.
- That creates an N+1 profile-fetch path on list pages.
- This phase is about fixing that read-path shape, not adding new features.

Scope:
- frontend only
- performance/read-path correction only
- no backend changes
- no API contract changes
- no new business features
- no topology graph
- no SQL work orders or query workbench
- do not touch the console-preferences feature except if shared files require minimal adjustment

Required outcome:
1. `/resources`, `/cmdb`, `/databases`, `/overview` should no longer trigger per-row profile fetches for every listed resource.
2. Full detail page should still render real backend profile data.
3. Right-side detail sheet should still render real backend profile data when opened.
4. Empty profile handling must remain safe.

Recommended approach:
- Split the current view-model path into:
  - lightweight list view-models without per-row profile fetch
  - detail-oriented view-models that fetch `/resources/{id}/profile` only when needed
- Keep summaries and relation/audit wiring stable.
- Prefer targeted service/view-model changes over component rewrites.

Likely files to inspect first:
1. /Users/fan/JsProjects/ControlHub/services/resources.ts
2. /Users/fan/JsProjects/ControlHub/lib/view-models.ts
3. /Users/fan/JsProjects/ControlHub/types/view-models.ts
4. /Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx
5. /Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx
6. /Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx
7. /Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx
8. /Users/fan/JsProjects/ControlHub/app/(console)/databases/page.tsx
9. /Users/fan/JsProjects/ControlHub/app/(console)/overview/page.tsx

Implementation guidance:
- Do not put async fetch logic directly into low-level presentational components if a cleaner route/container-level boundary is available.
- It is acceptable for list pages to show no profile data until a row is opened, as long as existing core columns remain correct.
- If the database list depends on one or two profile-derived fields, choose one of these:
  - keep a lightweight fallback only for list display
  - or do a targeted per-page enrichment only for database-focused pages
- Do not keep the blanket per-resource N+1 path across all list pages.
- If needed, add a focused client-side fetch when the detail sheet opens.

Verification requirements:
1. Run:
   - npx vitest run
   - npm run build
   - npm run lint
2. Manually verify with live backend:
   - /overview
   - /resources
   - open right-side detail sheet from /resources
   - /resources/40000000-0000-0000-0000-000000000002
   - /resources/40000000-0000-0000-0000-000000000001
   - /databases
3. Inspect network behavior and confirm list pages no longer do one `/profile` request per row by default.
4. Confirm detail page and sheet still show real backend profile fields.

Commit rules:
- frontend repo only
- no AI co-author metadata
- do not widen scope
- if you find a hard blocker that truly needs backend support, report the exact missing contract rather than inventing a workaround

Final report must include:
- changed files
- how N+1 was removed
- where real `/profile` fetching now happens
- what list pages still use as fallback, if anything
- test results
- manual verification results
- network verification summary
- commit hash
- remaining risks

Do not stop at analysis. Implement and verify.
```
