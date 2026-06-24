.PHONY: test test-integration test-openapi-fuzz run openapi-validate migrate-up migrate-status migrate-down-one migrate-reset-dev cutover-local seed-query-dev-credential seed-query-dev-target query-e2e-mysql-up query-e2e-mysql-down query-e2e-mysql-status release-local-gates release-docker-gates release-readiness-gates

GOOSE := $(shell go env GOPATH)/bin/goose
GOOSE_DRIVER := mysql
GOOSE_MIGRATION_DIR := migrations

# Load .env if present
ifneq (,$(wildcard .env))
  include .env
  export
endif

# Strip parseTime/charset params from DATABASE_DSN for goose compatibility
GOOSE_DBSTRING := $(shell echo "$(DATABASE_DSN)" | sed 's/?parseTime=true&charset=utf8mb4//' | sed 's/?parseTime=true//' | sed 's/?charset=utf8mb4//')

test:
	go test ./...

test-integration: ## Run integration tests against disposable MySQL via Testcontainers (requires Docker)
	go test -tags=integration -count=1 -v -run '^Test[^O]' ./internal/integration

test-openapi-fuzz: ## Run Schemathesis OpenAPI fuzzing against disposable MySQL server (requires Docker + schemathesis)
	go test -tags=integration -count=1 -v -run TestOpenAPIFuzz ./internal/integration

openapi-validate: ## Validate internal/openapi/openapi.yaml
	go test ./internal/openapi -v -run TestOpenAPIYAMLIsValid

run:
	go run ./cmd/server

cutover-local: ## Preserve current controlhub as controlhub_v1, rebuild bigint controlhub, then import legacy data (DESTRUCTIVE — requires CONFIRM=yes)
	@if [ "$(CONFIRM)" != "yes" ]; then echo "Error: set CONFIRM=yes to run this target. This renames runtime tables, rebuilds the target database, and imports preserved data."; exit 1; fi
	go run ./cmd/cutover-local

seed-query-dev-credential: ## Seed local/dev query credential METADATA for one target (dev-only; DSN stays in env, never stored). Requires DATABASE_DSN, QUERY_DEV_TARGET_RESOURCE_ID, QUERY_DEV_CREDENTIAL_REF, and CONTROLHUB_QUERY_CREDENTIAL_<REF>.
	go run ./cmd/querydev

seed-query-dev-target: ## Dev-only: ENSURE a local database_instance query target + profile (host/port from DATABASE_DSN), then seed its credential metadata in one idempotent pass. Requires DATABASE_DSN, QUERY_DEV_CREDENTIAL_REF, CONTROLHUB_QUERY_CREDENTIAL_<REF>. DSN is never stored/printed.
	QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run ./cmd/querydev

query-e2e-mysql-up: ## Start the dedicated Query E2E Docker MySQL (dev/test). Writes gitignored .query-e2e-mysql.env with the read-only credential DSN (never printed). Does not touch the ControlHub metadata DB.
	bash scripts/query-e2e-mysql.sh up

query-e2e-mysql-down: ## Stop and remove the dedicated Query E2E Docker MySQL, and remove .query-e2e-mysql.env.
	bash scripts/query-e2e-mysql.sh down

query-e2e-mysql-status: ## Report whether the dedicated Query E2E Docker MySQL is running.
	bash scripts/query-e2e-mysql.sh status

migrate-up: ## Apply all pending migrations
	@if [ -z "$(GOOSE_DBSTRING)" ]; then echo "Error: DATABASE_DSN not set. Export it or add to .env"; exit 1; fi
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" GOOSE_MIGRATION_DIR=$(GOOSE_MIGRATION_DIR) $(GOOSE) up

migrate-status: ## Show migration status
	@if [ -z "$(GOOSE_DBSTRING)" ]; then echo "Error: DATABASE_DSN not set. Export it or add to .env"; exit 1; fi
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" GOOSE_MIGRATION_DIR=$(GOOSE_MIGRATION_DIR) $(GOOSE) status

migrate-down-one: ## Roll back one migration
	@if [ -z "$(GOOSE_DBSTRING)" ]; then echo "Error: DATABASE_DSN not set. Export it or add to .env"; exit 1; fi
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" GOOSE_MIGRATION_DIR=$(GOOSE_MIGRATION_DIR) $(GOOSE) down

release-local-gates: ## Run local backend release-readiness gates (no Docker)
	go test -count=1 ./...
	go vet ./...
	go build ./...
	$(MAKE) openapi-validate

release-docker-gates: ## Run Docker-backed backend release-readiness gates
	$(MAKE) test-integration
	$(MAKE) test-openapi-fuzz

release-readiness-gates: release-local-gates release-docker-gates ## Run all backend release-readiness gates

migrate-reset-dev: ## Drop and recreate DB, then apply all migrations (DESTRUCTIVE — requires CONFIRM=yes)
	@if [ "$(CONFIRM)" != "yes" ]; then echo "Error: set CONFIRM=yes to run this target. This drops and recreates the database."; exit 1; fi
	@if [ -z "$(GOOSE_DBSTRING)" ]; then echo "Error: DATABASE_DSN not set"; exit 1; fi
	@DB_NAME=$$(echo "$(GOOSE_DBSTRING)" | sed 's/.*\///' | sed 's/\?.*//'); \
	echo "Dropping and recreating database: $$DB_NAME"; \
	mysql -u root -e "DROP DATABASE IF EXISTS $$DB_NAME; CREATE DATABASE $$DB_NAME CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"; \
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" GOOSE_MIGRATION_DIR=$(GOOSE_MIGRATION_DIR) $(GOOSE) up
