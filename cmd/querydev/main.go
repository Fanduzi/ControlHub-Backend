// Package main is the local/dev-only query credential METADATA seed command.
//
// input: os, strconv, strings, errors, io, database/sql, context, fmt, log, config, mysql repos, service seeder + target service + dev target fixture
// output: main() — dev-only binary (go run ./cmd/querydev / make seed-query-dev-credential / make seed-query-dev-target)
// pos: Explicit, idempotent local/dev seed of one query target's credential metadata so the Query Workbench can reach readiness. With QUERY_DEV_ALLOW_TARGET_FIXTURE=true it also ENSURES a local database_instance target + profile (host:port from DATABASE_DSN) before seeding. NOT auto-enabled in production.
// note: Writes METADATA only (resource_id, engine, credential_ref, enabled, environment_policy). The DSN is read from CONTROLHUB_QUERY_CREDENTIAL_<REF> by the resolver and validated to bind to the target, but it is never stored, logged, or printed. DATABASE_DSN is parsed for host:port only.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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

// errFixtureExplicitResourceIDForbidden is returned when fixture mode is
// requested but QUERY_DEV_TARGET_RESOURCE_ID is also set. In fixture mode the
// target id is DERIVED (ensured), never supplied, so an explicit id is a
// fail-closed error. Fixed string — never carries a DSN.
var errFixtureExplicitResourceIDForbidden = errors.New("QUERY_DEV_TARGET_RESOURCE_ID must be unset in fixture mode")

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	dsn := strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	if dsn == "" {
		log.Fatal("DATABASE_DSN is not set (set it in .env or export it)")
	}
	// The ControlHub bootstrap DSN is passed straight to the driver and never
	// stored or printed. The credential DSN (CONTROLHUB_QUERY_CREDENTIAL_<REF>)
	// is read only by the resolver, never here. In fixture mode DATABASE_DSN is
	// parsed for host:port only (service.ParseControlHubDSNHostPort).
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open controlhub db: %v", err)
	}
	defer db.Close()

	cfg, err := resolveSeedConfig(context.Background(), db, dsn)
	if err != nil {
		log.Fatalf("%v", err)
	}

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

	printReport(os.Stdout, meta, readiness, runEnabled)
}

// resolveSeedConfig builds the credential seed config. In fixture mode it
// ensures a local database_instance target + profile first (host:port from
// DATABASE_DSN) and uses the ensured id; QUERY_DEV_TARGET_RESOURCE_ID must be
// unset. Otherwise it reads the explicit target id from the env (the original
// bind-only path, unchanged).
func resolveSeedConfig(ctx context.Context, db *sql.DB, dsn string) (service.QueryDevCredentialSeedConfig, error) {
	ref, policy, allowAll, err := loadCredentialRefPolicy()
	if err != nil {
		return service.QueryDevCredentialSeedConfig{}, err
	}

	fixtureCfg, allowFixture, err := loadFixtureConfig()
	if err != nil {
		return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("invalid fixture config: %w", err)
	}

	if !allowFixture {
		// Original bind-only path: explicit target id required.
		targetID, err := parseUint64Env("QUERY_DEV_TARGET_RESOURCE_ID")
		if err != nil {
			return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("QUERY_DEV_TARGET_RESOURCE_ID: %w", err)
		}
		return service.QueryDevCredentialSeedConfig{
			TargetResourceID:     targetID,
			CredentialRef:        ref,
			EnvironmentPolicy:    policy,
			AllowAllEnvironments: allowAll,
		}, nil
	}

	// Fixture mode: the target id is derived (ensured), never supplied.
	if err := fixtureModePreflight(os.Getenv("QUERY_DEV_TARGET_RESOURCE_ID")); err != nil {
		return service.QueryDevCredentialSeedConfig{}, err
	}
	host, port, err := service.ParseControlHubDSNHostPort(dsn)
	if err != nil {
		return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("parse controlhub dsn host/port: %w", err)
	}
	fixtureCfg.Host, fixtureCfg.Port = host, port
	fixture := service.NewQueryDevTargetFixture(
		mysql.NewDictionaryRepository(db),
		mysql.NewResourceRepository(db),
	)
	targetID, err := fixture.EnsureLocalQueryTarget(ctx, fixtureCfg)
	if err != nil {
		return service.QueryDevCredentialSeedConfig{}, fmt.Errorf("ensure local query target: %w", err)
	}
	return service.QueryDevCredentialSeedConfig{
		TargetResourceID:     targetID,
		CredentialRef:        ref,
		EnvironmentPolicy:    policy,
		AllowAllEnvironments: allowAll,
	}, nil
}

// loadCredentialRefPolicy reads the credential ref + environment policy shared
// by both the bind-only and fixture paths. The credential DSN itself is never
// read here — only the opaque ref.
func loadCredentialRefPolicy() (ref string, policy model.QueryEnvironmentPolicy, allowAll bool, err error) {
	ref = strings.TrimSpace(os.Getenv("QUERY_DEV_CREDENTIAL_REF"))
	if ref == "" {
		return "", model.QueryEnvPolicyNonProdOnly, false, fmt.Errorf("QUERY_DEV_CREDENTIAL_REF is not set")
	}
	policyStr := strings.TrimSpace(os.Getenv("QUERY_DEV_ENVIRONMENT_POLICY"))
	if policyStr == "" {
		policyStr = string(model.QueryEnvPolicyNonProdOnly)
	}
	allowAll, _ = parseBoolEnv("QUERY_DEV_ALLOW_ALL_ENVIRONMENTS")
	return ref, model.QueryEnvironmentPolicy(policyStr), allowAll, nil
}

// loadFixtureConfig reads the fixture-mode flag and the optional target naming
// overrides. It returns allowFixture=false (no error) when the flag is absent,
// so the original bind-only behavior is the default. Host/port are NOT read
// here — they are parsed from DATABASE_DSN by the caller.
func loadFixtureConfig() (fixture service.QueryDevTargetFixtureConfig, allowFixture bool, err error) {
	allow, err := parseBoolEnv("QUERY_DEV_ALLOW_TARGET_FIXTURE")
	if err != nil {
		return service.QueryDevTargetFixtureConfig{}, false, err
	}
	if !allow {
		return service.QueryDevTargetFixtureConfig{}, false, nil
	}
	return service.QueryDevTargetFixtureConfig{
		EnvironmentSlug: envOrDefault("QUERY_DEV_TARGET_ENV_SLUG", "dev"),
		OwnerEmail:      envOrDefault("QUERY_DEV_TARGET_OWNER_EMAIL", "dba@example.com"),
		ResourceName:    envOrDefault("QUERY_DEV_TARGET_NAME", "local-mysql-query-dev"),
		DisplayName:     envOrDefault("QUERY_DEV_TARGET_DISPLAY_NAME", "Local MySQL Query Dev"),
		Engine:          "mysql",
		Version:         "8.0",
		Role:            "primary",
	}, true, nil
}

// fixtureModePreflight rejects a supplied QUERY_DEV_TARGET_RESOURCE_ID in
// fixture mode. The target id must be derived (ensured), not supplied.
func fixtureModePreflight(explicitResourceID string) error {
	if strings.TrimSpace(explicitResourceID) != "" {
		return errFixtureExplicitResourceIDForbidden
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
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
func printReport(w io.Writer, meta model.QueryCredentialMetadata, readiness model.QueryTargetReadiness, runEnabled bool) {
	fmt.Fprintln(w, "seeded query credential metadata (dev-only)")
	fmt.Fprintf(w, "  target resource id: %d\n", meta.ResourceID)
	fmt.Fprintf(w, "  credential ref:     %s\n", meta.CredentialRef)
	fmt.Fprintf(w, "  engine:             %s\n", meta.Engine)
	fmt.Fprintf(w, "  environment policy: %s\n", meta.EnvironmentPolicy)
	fmt.Fprintf(w, "  enabled:            %t\n", meta.Enabled)
	fmt.Fprintf(w, "  readiness:          %s\n", readiness)
	fmt.Fprintf(w, "  run available:      %t\n", runEnabled)
	fmt.Fprintln(w, "  stored dsn:         none (DSN stays in CONTROLHUB_QUERY_CREDENTIAL_<REF>)")
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
