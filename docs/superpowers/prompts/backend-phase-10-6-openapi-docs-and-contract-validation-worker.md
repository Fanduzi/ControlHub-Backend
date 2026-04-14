# Backend Phase 10.6: OpenAPI Docs And Contract Validation

You are implementing the OpenAPI documentation and contract validation phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-10-5-goose-migration-management-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/README.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`

## Goal

ControlHub already has a YAML-first OpenAPI contract:

```text
internal/openapi/openapi.yaml
```

This phase makes that contract easier to inspect and harder to break:

- serve the OpenAPI YAML from the backend
- serve a modern OpenAPI documentation page for local development
- add contract validation tooling
- document the workflow

Do not convert the project to annotation-generated Swagger.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Keep `internal/openapi/openapi.yaml` as the single source of API contract truth.
- Do not introduce `swaggo/swag` or handler comment annotations.
- Do not generate OpenAPI from Go handler comments.
- Do not use Swagger UI.
- Use Scalar API Reference as the read/view layer over the existing YAML.
- Add validation for the YAML contract.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. expose `GET /openapi.yaml`
2. expose `GET /docs` with Scalar API Reference
3. add OpenAPI validation command
4. add tests for docs/spec routes
5. update README/CLAUDE docs

Do not add new business APIs, auth middleware, topology, frontend UI, or migration changes.

## Runtime Endpoints

### 1. `GET /openapi.yaml`

Serve the current OpenAPI YAML.

Requirements:

- response status `200`
- content type should be YAML-friendly, for example `application/yaml`, `text/yaml`, or `application/octet-stream` if the current router makes that easier
- response body must exactly reflect `internal/openapi/openapi.yaml` or an embedded copy of it
- no auth required for local dev phase

### 2. `GET /docs`

Serve a Scalar API Reference page that loads `/openapi.yaml`.

Requirements:

- response status `200`
- page title should mention ControlHub API
- UI must point to `/openapi.yaml`
- keep it local-dev focused and minimal
- do not build a custom docs frontend
- do not use Swagger UI assets

Implementation options are allowed inside this fixed direction:

- use an embedded static HTML page referencing Scalar browser assets from a CDN
- or vendor a minimal Scalar bundle if avoiding CDN is straightforward

Pick the smaller maintainable approach and document the tradeoff in the final report.

## Contract Validation

Add a validation command.

Preferred options:

- use a small Go test with `github.com/getkin/kin-openapi/openapi3`
- or add a Makefile target that runs a validator if already available

Recommendation:

- add a Go test so validation does not depend on a globally installed Node/npm tool

Expected target:

```bash
make openapi-validate
```

It should fail if `internal/openapi/openapi.yaml` is invalid OpenAPI.

Also include OpenAPI validation in a broader test path if clean:

```bash
make test
```

Only do this if it does not make normal tests flaky or network-dependent.

## Tests

Follow TDD.

Add/update tests covering:

- `GET /openapi.yaml` returns `200`
- `GET /openapi.yaml` contains expected OpenAPI marker fields, such as `openapi:` and `paths:`
- `GET /docs` returns `200`
- `GET /docs` references `/openapi.yaml`
- OpenAPI YAML parses and validates successfully

Tests must not require network access.

## Documentation

Update:

- `README.md`
- `CLAUDE.md`
- any relevant internal README

Document:

- local docs URL: `http://localhost:8080/docs`
- raw contract URL: `http://localhost:8080/openapi.yaml`
- validation command: `make openapi-validate`
- YAML-first policy
- no handler annotation generation
- Scalar-based docs viewer, not Swagger UI

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
```

If local backend can run, also smoke test:

```bash
curl -i http://localhost:8080/openapi.yaml
curl -i http://localhost:8080/docs
```

## Final Report

Your final report must include:

- changed files
- implementation approach for Scalar docs assets
- docs/spec endpoint status
- OpenAPI validation approach
- test/vet/build results
- curl smoke results if run
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not add business APIs
- do not add annotation-generated Swagger
- do not add Swagger UI
- do not require global Node/npm tools for validation
