# Frontend Phase 6 Console Preferences Worker Prompt

```text
You are the frontend phase-6 worker for ControlHub.

Repository:
/Users/fan/JsProjects/ControlHub

Read these docs first:
1. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-12-console-preferences-design.md
2. /Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-12-console-preferences-implementation.md

Current goal:
Implement the approved console preference refinement for the frontend only.

This phase must:
- replace the topbar language dropdown with a direct `中 / EN` segmented switch
- keep the theme control compact
- add a topbar accent-color picker with 4 fixed presets
- add a matching Appearance section in Settings
- preserve the existing monochrome console feel
- keep semantic success/warning/error/info colors unchanged

Scope boundaries:
- frontend only
- no backend changes
- no API contract changes
- no SQL work orders
- no topology graph
- no asset edit/create flows
- no route restructuring
- no locale-prefixed routes
- no arbitrary color picker

Approved interaction rules:
- language uses a direct topbar segmented control: `中 / EN`
- theme stays a single compact button
- accent color uses a small topbar trigger that opens a compact popover
- there are exactly 4 accent presets:
  - blue
  - purple
  - emerald
  - amber
- purple is mandatory
- settings page gets an Appearance section as a fallback surface

Approved accent-color rules:
- accent color may affect:
  - ControlHub wordmark emphasis
  - brand/avatar accent treatment
  - active nav state
  - primary button color
  - focus ring
  - selected state border/background
  - subtle interactive emphasis text
- accent color must not affect:
  - success semantic color
  - warning semantic color
  - error semantic color
  - info semantic color
  - health-status badge semantics
  - audit-result semantics
  - large neutral backgrounds
  - grayscale table hierarchy

Technical direction:
- keep next-intl cookie-based locale strategy
- keep next-themes
- add frontend-only accent preference persistence, e.g. localStorage key `controlhub.accent`
- apply accent through root-level attribute + CSS variables
- do not implement accent switching through large class-variant branching

Files likely involved:
- /Users/fan/JsProjects/ControlHub/app/globals.css
- /Users/fan/JsProjects/ControlHub/app/(console)/settings/page.tsx
- /Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx
- /Users/fan/JsProjects/ControlHub/components/providers/app-providers.tsx
- /Users/fan/JsProjects/ControlHub/components/settings/language-switcher.tsx
- /Users/fan/JsProjects/ControlHub/components/settings/theme-toggle.tsx
- create:
  - /Users/fan/JsProjects/ControlHub/components/providers/accent-provider.tsx
  - /Users/fan/JsProjects/ControlHub/components/settings/accent-switcher.tsx
  - /Users/fan/JsProjects/ControlHub/lib/preferences.ts
  - /Users/fan/JsProjects/ControlHub/tests/components/accent-switcher.test.tsx

Execution order:
1. Inspect current worktree
   - git status --short
   - git log --oneline -5
2. Implement the shared accent-preference foundation
3. Replace the language dropdown with segmented control
4. Add the topbar accent switcher
5. Add the settings Appearance section
6. Verify accent token behavior in globals.css for light and dark
7. Run tests/build/lint
8. Run light manual browser validation
9. Commit

Testing requirements:
- update/add tests for:
  - language switcher
  - theme toggle if needed
  - accent switcher
- run:
  - npx vitest run
  - npm run build
  - npm run lint

Manual validation requirements:
- run frontend against backend
- verify:
  - /overview
  - /resources
  - /resources/{id}
  - /audits
  - /settings
- verify:
  - `中 / EN` works
  - theme toggle works
  - all 4 accent presets apply
  - purple remains legible in light and dark
  - semantic health/audit colors do not change with accent

Commit rules:
- commit only frontend repo changes
- do not commit .next, node_modules, screenshots, local env files, .idea, .playwright-mcp
- no AI co-author metadata

Final completion report must include:
- changed files
- how language switching changed
- how accent persistence works
- which CSS tokens are accent-driven
- confirmation that semantic status colors remain fixed
- test results
- manual validation results
- commit hash
- remaining risks

Do not stop at analysis. Implement and verify.
```
