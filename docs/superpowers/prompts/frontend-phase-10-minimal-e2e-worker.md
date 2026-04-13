# Frontend Phase 10: Minimal E2E Coverage

You are implementing the first real end-to-end test layer for ControlHub frontend.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`

## Goal

Add a small, stable, local-first E2E suite covering the critical control-console path.

This is not a visual regression project and not a giant browser automation effort.

## Scope

Build the minimum E2E coverage for:

1. login with seeded local backend user
2. `/resources` renders live data
3. clicking a resource opens the right-side detail sheet
4. detail sheet shows backend profile data
5. `/databases` can open the detail sheet without regression
6. `/settings` loads live backend-backed dictionaries

Do not add broad scenario coverage outside these paths.

## Constraints

- use a dedicated git worktree unless there is a strong reason not to
- follow TDD: write failing E2E coverage first, then implement or stabilize
- do not introduce heavy tracing, video, or artifact generation by default
- keep the suite small and reliable for local development
- do not widen business scope
- do not change backend contracts

## Tooling

Choose one lightweight E2E runner appropriate for this Next.js repo.

Preferred direction:

- Playwright

But keep configuration minimal and local-first.

## Expected Test Flow

At minimum, cover:

### Spec 1: Login

- open `/login`
- submit seeded credentials
- confirm navigation into the console

### Spec 2: Resources Sheet

- open `/resources`
- confirm live resource row is visible
- click a resource row
- assert the right-side detail sheet appears
- assert backend profile fields are visible

### Spec 3: Databases Sheet

- open `/databases`
- click a row
- assert the same sheet pattern appears

### Spec 4: Settings

- open `/settings`
- assert environments / owners / roles content loads from backend-backed data

## Environment Assumptions

Local backend is expected at:

- `http://localhost:8080`

Frontend is expected at:

- `http://localhost:3000`

If you need small test utilities or a script to start/coordinate local services, keep them simple and documented.

## Verification

You must run:

```bash
npm run lint
npm run build
npx vitest run
```

And also run the new E2E suite.

## Final Report

Your final report must include:

- chosen E2E runner
- changed files
- exact covered flows
- how local execution works
- lint/build/unit-test results
- E2E results
- commit hash
- remaining gaps

## Do Not

- do not build a giant end-to-end framework
- do not add flaky visual assertions
- do not introduce unrelated refactors
- do not turn this into a CI redesign
