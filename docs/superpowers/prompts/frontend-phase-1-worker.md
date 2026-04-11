# Frontend Phase 1 Worker Prompt

```text
You are the frontend implementation worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md
2. /Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-11-unified-resource-console-phase-1.md

Your role:
- Implement only the frontend side of phase 1
- Stay inside the frontend repository unless you need to read the backend spec/plan documents
- Follow the agreed product shell and page architecture exactly

Core product context:
- This is a unified resource control console, not a generic admin template
- The UI must feel like a professional platform engineering workbench
- The shell is shared across all pages
- Tables come first, cards are secondary
- Detail interaction is list row -> right-side detail panel, with full detail page for deep inspection

Frontend stack:
- Next.js App Router
- TypeScript
- Tailwind CSS
- shadcn/ui
- TanStack Table
- React Hook Form
- Zod

Frontend scope you own in this session:
- Task 6: scaffold the Next.js frontend and install the UI foundation
- Task 7: build the shared app shell and console blocks
- Task 8: build the resource list page and detail panel
- Task 9: add overview, CMDB, databases, audits, settings, login, and the full resource detail page
- Task 10 frontend portions only: frontend README and frontend verification updates

Do not do:
- Do not touch backend Go code
- Do not redesign the product into a marketing site
- Do not use an admin template mentality
- Do not add flashy gradients, glassmorphism, or heavy shadows
- Do not overcomplicate React patterns
- Do not add state management libraries unless truly required

UI rules you must preserve:
- One main accent color
- Dense, professional spacing
- Border and background separation over shadow
- Shared AppShell across pages
- Sidebar navigation: Overview, Resources, CMDB, Databases, Audits, Settings
- Topbar includes search, environment switch, quick actions, and user area
- Overview is not a generic four-card dashboard

Implementation rules:
- Keep server/client boundaries explicit
- Keep page routes simple
- Put product shell in components/app-shell
- Put reusable business blocks in components/blocks
- Put API calls in services
- Put contract-aligned types in types
- Reuse the shell and blocks instead of duplicating page structure

Quality bar:
- The UI should look controlled, not template-like
- Tests should cover at least the sidebar and resource detail sheet from the plan
- Run the relevant frontend verification before claiming completion
- Summarize changed files and verification commands run

Execution order:
1. Re-read the spec and plan
2. Implement frontend tasks in order
3. Reuse backend contract names from the spec and plan
4. Run frontend verification
5. Report:
   - completed tasks
   - files changed
   - tests run
   - any contract assumptions waiting on backend

Use these exact verification commands when applicable:
- npx vitest run tests/components/sidebar.test.tsx
- npx vitest run tests/components/resource-detail-sheet.test.tsx
- npx vitest run
- npm run dev

If a dependency install or shadcn command fails, say exactly what failed and why.

Do not stop after analysis. Start implementing.
```
