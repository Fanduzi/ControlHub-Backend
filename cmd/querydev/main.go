// Package main is the local/dev-only query credential METADATA seed command.
//
// input: os, strconv, strings, database/sql, context, fmt, log, config, mysql repos, service seeder + target service
// output: main() — dev-only binary (go run ./cmd/querydev / make seed-query-dev-credential)
// pos: Explicit, idempotent local/dev seed of one query target's credential metadata so the Query Workbench can reach readiness. NOT auto-enabled in production.
// note: Writes METADATA only (resource_id, engine, credential_ref, enabled, environment_policy). The DSN is read from CONTROLHUB_QUERY_CREDENTIAL_<REF> by the resolver and validated to bind to the target, but it is never stored, logged, or printed.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := loadSeedConfig()
	if err != nil {
		log.Fatalf("invalid seed config: %v", err)
	}

	dsn := strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	if dsn == "" {
		log.Fatal("DATABASE_DSN is not set (set it in .env or export it)")
	}
	// The ControlHub bootstrap DSN is passed straight to the driver and never
	// stored or printed. The credential DSN (CONTROLHUB_QUERY_CREDENTIAL_<REF>)
	// is read only by the resolver, never here.
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open controlhub db: %v", err)
	}
	defer db.Close()

	targetRepo := mysql.NewQueryTargetRepository(db)
	execRepo := mysql.NewQueryExecutionRepository(db)
	seeder := service.NewQueryDevCredentialSeeder(targetRepo, service.NewEnvCredentialResolver(), execRepo)

	meta, err := seeder.Seed(context.Background(), cfg)
	if err != nil {
		log.Fatalf("seed query credential metadata: %v", err)
	}

	// Re-derive readiness through the real read model so the printed readiness
	// is the same truth the Query Workbench API reports — not an assumption.
	readiness, runEnabled := deriveReadiness(context.Background(), targetRepo, execRepo, meta.ResourceID)

	printReport(meta, readiness, runEnabled)
}

// loadSeedConfig reads the seed inputs from the environment. Environment policy
// defaults to non_prod_only (the safe default); all_environments requires the
// explicit QUERY_DEV_ALLOW_ALL_ENVIRONMENTS override, enforced inside the
// seeder. The credential DSN itself is intentionally NOT read here.
func loadSeedConfig() (service.QueryDevCredentialSeedConfig, error) {
	targetID, err := parseUint64Env("QUERY_DEV_TARGET_RESOURCE_ID")
	if err != nil {
		return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("QUERY_DEV_TARGET_RESOURCE_ID: %w", err)
	}
	ref := strings.TrimSpace(os.Getenv("QUERY_DEV_CREDENTIAL_REF"))
	if ref == "" {
		return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("QUERY_DEV_CREDENTIAL_REF is not set")
	}
	policyStr := strings.TrimSpace(os.Getenv("QUERY_DEV_ENVIRONMENT_POLICY"))
	if policyStr == "" {
		policyStr = string(model.QueryEnvPolicyNonProdOnly)
	}
	allowAll, _ := parseBoolEnv("QUERY_DEV_ALLOW_ALL_ENVIRONMENTS")

	return service.QueryDevCredentialSeedConfig{
		TargetResourceID:     targetID,
		CredentialRef:        ref,
		EnvironmentPolicy:    model.QueryEnvironmentPolicy(policyStr),
		AllowAllEnvironments: allowAll,
	}, nil
}

// deriveReadiness re-lists query targets through the credential-aware read
// model and returns the derived readiness + run flag for the seeded target. It
// mirrors exactly what GET /query-targets would report after the seed.
func deriveReadiness(ctx context.Context, targetRepo *mysql.QueryTargetRepository, execRepo *mysql.QueryExecutionRepository, resourceID uint64) (model.QueryTargetReadiness, bool) {
	svc := service.NewQueryTargetService(targetRepo).WithCredentialReader(execRepo)
	targets, err := svc.List(ctx, model.QueryTargetListQuery{})
	if err != nil {
		return model.ReadinessCredentialRequired, false
	}
	for _, t := range targets {
		if t.ResourceID == resourceID {
			return t.Readiness, t.AvailableActions.Run
		}
	}
	return model.ReadinessCredentialRequired, false
}

// printReport prints ONLY safe metadata plus the derived readiness. The DSN and
// password are never part of the report.
func printReport(meta model.QueryCredentialMetadata, readiness model.QueryTargetReadiness, runEnabled bool) {
	fmt.Println("seeded query credential metadata (dev-only)")
	fmt.Printf("  target resource id: %d\n", meta.ResourceID)
	fmt.Printf("  credential ref:     %s\n", meta.CredentialRef)
	fmt.Printf("  engine:             %s\n", meta.Engine)
	fmt.Printf("  environment policy: %s\n", meta.EnvironmentPolicy)
	fmt.Printf("  enabled:            %t\n", meta.Enabled)
	fmt.Printf("  readiness:          %s\n", readiness)
	fmt.Printf("  run available:      %t\n", runEnabled)
	fmt.Println("  stored dsn:         none (DSN stays in CONTROLHUB_QUERY_CREDENTIAL_<REF>)")
}

func parseUint64Env(key string) (uint64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, fmt.Errorf("is not set")
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid unsigned integer: %w", raw, err)
	}
	return v, nil
}

func parseBoolEnv(key string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s %q is not a valid boolean: %w", key, raw, err)
	}
	return v, nil
}
