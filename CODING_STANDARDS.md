# Coding Standards

## Change and review boundaries

- Before adding code or reviewing changed behavior, read exports, immediate callers, and shared utilities.
- When patterns conflict, choose the more recent or better-tested one, explain why, and flag the other for cleanup rather than blending them.
- Keep dependencies directed toward the domain: HTTP handlers call services; services use domain models and consumer-owned repository interfaces; MySQL repositories implement those interfaces. Keep domain models independent of API and persistence layers. See [service conventions](internal/service/README.md) and [repository conventions](internal/repository/mysql/README.md).
- Wire concrete dependencies with manual constructor injection through `api.Dependencies` in the [server bootstrap](cmd/server/README.md).

## Persistence and API contracts

- Use raw `database/sql` with explicit scanning and serialization for MySQL persistence, not an ORM.
- Express schema changes as goose SQL migrations in `migrations/`, with Up/Down sections. Apply migrations explicitly, never automatically on server startup. For database operations, read [migration guidance](README.md#migration-commands), including destructive-reset warnings.
- Maintain the YAML-first contract in `internal/openapi/openapi.yaml` alongside API changes. Use Scalar for interactive docs, not swaggo/swag or Swagger UI. Before API changes, read the [OpenAPI update rules](internal/openapi/README.md).

## Tests and verification gates

- Tests must encode why behavior matters and fail when the relevant business logic changes incorrectly.
- Use interface-based fakes for service tests and the existing fake-repository/real-router [handler-test infrastructure](internal/api/README.md). Database behavior that fakes cannot prove belongs in integration coverage.
- Keep integration and OpenAPI fuzz tests on disposable Testcontainers databases, never the daily local `controlhub` database.
- Run `make test` for every commit; run `make test-integration` and `make test-openapi-fuzz` before merge. Also run fuzz tests after API changes and `make openapi-validate` when changing the contract.
- Before running gates, read [test prerequisites](README.md#test) and the [Makefile](Makefile). Run aggregate targets, not cached subsets of their recipes.
- For code review, identify missing coverage or gate evidence.
