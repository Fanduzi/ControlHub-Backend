# Frontend Phase 5 Theme, i18n, and Visual Foundation Worker Prompt

```text
You are the frontend phase-5 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Current goal:
Build the frontend foundation for Monochrome Resource Console styling, light/dark/system theme switching, and Chinese/English i18n. Also investigate any remaining frontend runtime error before changing UI infrastructure.

Context:
- ControlHub is an internal unified resource console / CMDB foundation.
- Current product scope is asset/resource foundation only.
- Do not add SQL work orders, SQL query workbench, asset edit/create flows, advanced permissions, topology graph, or backend profile APIs in this phase.
- Frontend stack is Next.js App Router, TypeScript, Tailwind CSS v4, shadcn v4, base-nova style, Base UI-backed shadcn components, React 19.
- Do not migrate to shadcn "new-york". Keep shadcn v4 + base-nova and refine ControlHub's own console theme.
- Backend runs at http://localhost:8080 and uses camelCase contracts.
- Latest known frontend commit before this phase: 0485f74 fix: align view-model lookup maps to real backend seed IDs.

Product direction:
Monochrome Resource Console.

Meaning:
- Neutral black/white/gray surfaces.
- Dense table-first workspace.
- Strong border and background layering.
- Minimal shadows.
- One primary accent only.
- Semantic status colors remain fixed and limited to default/success/warning/error/info.
- Use mono typography for IDs, timestamps, resource keys, and system labels.
- Avoid traditional admin dashboard cards, large colorful gradients, glassmorphism, and marketing-page styling.
- The target feeling is closer to a professional shadcn v4 command/workbench surface than an OA/CRM/admin template.

Required dependency direction:
- Use next-themes for light/dark/system theme switching.
- Use next-intl for i18n.
- As checked on 2026-04-12, npm showed:
  - next-themes latest visible version: 0.4.6
  - next-intl latest visible version: 4.9.1
- Before installing, verify current package versions with npm and install the current compatible versions.

Important routing decision:
- Keep existing routes stable in this phase:
  - /login
  - /overview
  - /resources
  - /resources/[id]
  - /cmdb
  - /databases
  - /audits
  - /settings
- Do not introduce visible locale-prefixed routes like /zh-CN/overview unless next-intl cannot be implemented cleanly without them.
- Preferred behavior: default locale is zh-CN, language can switch to en, and the current route remains stable.
- If next-intl's current official App Router guidance makes stable non-prefixed routes unsafe or awkward, stop and report the tradeoff before changing route structure.

Read these files first:
1. /Users/fan/JsProjects/ControlHub/package.json
2. /Users/fan/JsProjects/ControlHub/components.json
3. /Users/fan/JsProjects/ControlHub/app/layout.tsx
4. /Users/fan/JsProjects/ControlHub/app/globals.css
5. /Users/fan/JsProjects/ControlHub/components/app-shell/sidebar.tsx
6. /Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx
7. /Users/fan/JsProjects/ControlHub/app/login/page.tsx
8. /Users/fan/JsProjects/ControlHub/lib/navigation.ts
9. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml

Task 0: investigate reported frontend error first
1. Run:
   - git status --short
   - npx vitest run
   - npm run build
   - npm run lint
2. Start frontend against the live backend:
   - NEXT_PUBLIC_API_BASE_URL=http://localhost:8080 npm run dev
3. Use the browser to smoke-test:
   - /login
   - /overview
   - /resources
   - row click detail sheet
   - /resources/{real-seed-resource-id}
   - /audits
   - /settings
4. Capture browser console and network errors.
5. If a runtime error is reproducible, fix that root cause before theme/i18n work.
6. If no error is reproducible, state that clearly in the completion report and continue.

Task 1: add theme foundation
1. Install next-themes if not already present.
2. Add a small provider layer, for example:
   - components/providers/theme-provider.tsx
   - components/providers/app-providers.tsx if useful
3. Wire the provider in app/layout.tsx.
4. Add suppressHydrationWarning on html if required by next-themes.
5. Add a ThemeToggle component with Light, Dark, and System options.
6. Place the theme control in the Topbar user/workspace controls or Settings page, whichever best preserves density.
7. Keep theme state persistent.
8. Do not make theme switching depend on backend.

Task 2: refine theme tokens for Monochrome Resource Console
1. Update app/globals.css tokens only as needed.
2. Preserve CSS variable strategy and shadcn v4 base-nova structure.
3. Light theme should feel like a crisp technical workspace:
   - near-white background
   - white/card surfaces
   - neutral gray borders
   - black or near-black foreground
   - restrained primary accent
4. Dark theme should be equal-quality, not just inverted:
   - deep neutral background
   - slightly lifted panels
   - visible but not noisy borders
   - readable muted text
   - restrained primary accent
5. Do not introduce broad gradients, glass effects, or heavy shadows.
6. Keep status badge semantic colors recognizable in both themes.

Task 3: add i18n foundation
1. Install next-intl if not already present.
2. Add locale files for at least:
   - zh-CN
   - en
3. Default locale must be zh-CN.
4. Add a LanguageSwitcher component with Chinese and English.
5. Place language switching in Topbar user/workspace controls or Settings page.
6. Translate core product shell first:
   - Sidebar navigation titles and descriptions
   - Topbar section labels and global search placeholder
   - Login page
   - PageHeader titles/descriptions for overview/resources/cmdb/databases/audits/settings/resource detail
   - Empty/error/loading states
   - Common action labels
7. Do not try to translate every internal data value from the backend in this phase.
8. Resource names, owner names, environment names, audit event types, IDs, and backend data should remain backend-provided or view-model formatted.
9. Do not concatenate translated strings. Use complete translation keys.
10. Date/time formatting should use Intl.DateTimeFormat and respect current locale where practical.

Task 4: preserve API and view-model boundaries
1. Do not change backend API request/response shapes.
2. Do not add actorName, targetResourceName, environmentName, or ownerName to wire types.
3. View-model enrichment can remain frontend-only.
4. Do not replace real API services with mock data.

Task 5: tests and verification
1. Update or add tests for:
   - Sidebar still renders core navigation.
   - Resource detail sheet still renders with current view-model shape.
   - Theme toggle renders without crashing.
   - Language switcher renders both language options.
2. Run:
   - npx vitest run
   - npm run build
   - npm run lint
3. Run a manual browser smoke test in both light and dark modes.
4. Run a manual browser smoke test in zh-CN and en.
5. Report any remaining TanStack Table React Compiler warnings separately; do not rewrite table architecture in this phase.

Commit rules:
- Commit only frontend changes in /Users/fan/JsProjects/ControlHub.
- Do not commit .next, .playwright-mcp, .idea, screenshots, or local env files.
- Do not include AI co-author metadata in commit messages.
- If the route structure must change for i18n, stop and report before committing.

Completion report must include:
- changed files
- dependency versions installed
- whether any pre-existing runtime error was reproduced
- root cause and fix if an error was reproduced
- theme implementation summary
- i18n implementation summary
- supported locales
- whether existing routes stayed unchanged
- manual smoke results for light/dark/system
- manual smoke results for zh-CN/en
- verification command results
- commit hash if committed
- remaining risks

Do not stop after analysis unless route structure must change or a backend contract change is required.
```
