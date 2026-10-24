// Package main provides the ControlHub application entry point.
// input: config.LoadDotEnv/Load/ValidateJWTSecret, serverboot.Build (shared production assembly incl. pgsql.GuardPG warm-up), api.NewRouter, os/signal Notify (SIGTERM/SIGINT), net.Listen
// output: main() binary entry point; runServer() graceful-drain lifecycle (shutdown.go)
// pos: Application bootstrap and lifecycle: validates the signing secret before opening the database, builds the shared production dependency graph via internal/serverboot, then serves HTTP; SIGTERM/SIGINT stop new traffic and drain in-flight handlers for at most ten seconds (Issue #37). PostgreSQL HTTP execution stays closed unless CONTROLHUB_PG_HTTP_ENTRIES=on — serverboot.Build gates both PG construction-time capabilities on that one flag, which can never be opened by HTTP requests.
// note: if wiring, startup validation, or the shutdown contract changes, update this header and cmd/server/README.md
package main

import (
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/serverboot"
)

func main() {
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

	db, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps, _ := serverboot.Build(db, cfg)

	// Issue #37: SIGTERM/SIGINT begin a bounded graceful drain instead of
	// killing the process mid-query, so in-flight governed queries can finish
	// their existing query deadline and two-second Evidence Persistence Window.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	ln, err := net.Listen("tcp", cfg.HTTPAddress())
	if err != nil {
		log.Fatalf("ControlHub listen: %v", err)
	}

	log.Printf("ControlHub starting on %s", cfg.HTTPAddress())
	os.Exit(runServer(&http.Server{Handler: api.NewRouter(deps)}, ln, sigCh, 0, log.Printf))
}
