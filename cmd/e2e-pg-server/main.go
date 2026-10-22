// Package main is the TEST/CI-ONLY long-lived E2E listener for the governed
// PostgreSQL query workbench browser path (issues #121/#122).
// input: config.LoadDotEnv/Load/ValidateJWTSecret, CONTROLHUB_E2E_FIXTURE_MODE, DATABASE_DSN (disposable *_e2e only), E2E_RUN_ID, serverboot.Build, api.NewRouter, net.Listen, os/signal NotifyContext (SIGTERM/SIGINT)
// output: main() fixture binary; GET /e2e/assembly run-identity document
// pos: TEST-ONLY listener serving the exact production dependency graph
// note: if the fixture gate or identity surface changes, update this header, cmd/e2e-pg-server/README.md, and the frontend E2E harness docs
//
// This binary serves the shared serverboot assembly plus the two
// construction-time gates a real browser needs — the execution service's
// WithPGHTTPEntries (the T12 default-closed release gate, opened only in
// this binary) and the target read model's WithConnectionLister
// connections[] projection. cmd/server never calls either, and no request
// field, header, or environment variable on the production binary can open
// them.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	gomysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/serverboot"
)

const (
	fixtureModeEnv      = "CONTROLHUB_E2E_FIXTURE_MODE"
	fixtureModeRequired = "1"
	runIDEnv            = "E2E_RUN_ID"
)

// disposableDBPattern mirrors the cmd/e2e-fixture-bootstrap boundary: the
// fixture may only serve a dedicated disposable metadata database.
var disposableDBPattern = regexp.MustCompile(`^controlhub_[a-z0-9_]*e2e$`)

// disposableDSNAllowed enforces the primary production-safety boundary: the
// metadata DSN must parse with the MySQL driver, use a literal loopback host,
// and name a dedicated controlhub_*_e2e database. Driver parse details are
// never echoed — they can carry secret-bearing parameter values.
func disposableDSNAllowed(dsn string) error {
	cfg, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return errors.New("DATABASE_DSN is not a valid MySQL DSN")
	}
	host := cfg.Addr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if host != "127.0.0.1" && host != "::1" && host != "[::1]" {
		return fmt.Errorf("DATABASE_DSN host must be a literal loopback address (got a non-loopback host); the E2E fixture listener never serves a remote metadata database")
	}
	if !disposableDBPattern.MatchString(cfg.DBName) {
		return fmt.Errorf("DATABASE_DSN database %q is not a disposable controlhub_*_e2e name; refusing to serve a non-E2E metadata database", cfg.DBName)
	}
	return nil
}

// buildInfoSHA returns the VCS revision stamped by the Go toolchain, or empty
// for builds without VCS metadata.
func buildInfoSHA() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

func main() {
	if os.Getenv(fixtureModeEnv) != fixtureModeRequired {
		log.Fatalf("%s=%s is required (explicit test-only capability; refusing to run without it)", fixtureModeEnv, fixtureModeRequired)
	}
	runID := strings.TrimSpace(os.Getenv(runIDEnv))
	if runID == "" {
		log.Fatalf("%s is not set (supply the per-run E2E identity; the fixture listener never serves an anonymous assembly)", runIDEnv)
	}
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := config.ValidateJWTSecret(cfg.JWTSecret); err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := disposableDSNAllowed(cfg.DatabaseDSN); err != nil {
		log.Fatalf("fixture boundary: %v", err)
	}

	db, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps, parts := serverboot.Build(db, cfg)

	// The two fixture-only capability flips. Both are construction-time gates
	// on the REAL production services — this binary changes no handler, claim,
	// finalize, guard, or disclosure mechanic:
	//   1. WithPGHTTPEntries opens the T12 three-route PostgreSQL dispatch;
	//   2. WithConnectionLister wires the composite-connection read model so
	//      GET /query-targets enumerates (target, database) connections with
	//      per-connection resolver-verified runtime status.
	parts.QueryExecutionService.WithPGHTTPEntries()
	deps.QueryTargetService = parts.QueryTargetService.WithConnectionLister(parts.QueryExecutions)

	// The run-identity document lets Playwright refuse to reuse a stale or
	// foreign listener on this port — reuse is never inferred from "something
	// is listening".
	assembly := map[string]any{
		"kind":       "controlhub-e2e-pg-fixture",
		"runId":      runID,
		"backendSha": buildInfoSHA(),
		"pgEntries":  true,
		"listenAddr": cfg.HTTPAddress(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/e2e/assembly", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(assembly)
	})
	mux.Handle("/", api.NewRouter(deps))

	ln, err := net.Listen("tcp", cfg.HTTPAddress())
	if err != nil {
		log.Fatalf("fixture listen: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("controlhub-e2e-pg-fixture ready run=%s sha=%s addr=%s", runID, buildInfoSHA(), cfg.HTTPAddress())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("fixture serve: %v", err)
	}
}
