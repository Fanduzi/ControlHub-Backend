// Package main is the explicit operator-invoked bootstrap-admin command.
//
// input: os, strings, errors, context, database/sql, crypto/sha256, encoding/hex, fmt, io, log, internal/config, go-sql-driver/mysql
// output: main() — explicit one-shot binary (go run ./cmd/bootstrap-admin), runBootstrap seam, resolveBootstrapConfig, hashPassword, printReport
// pos: Creates or reactivates the administrator from deployment-supplied inputs (BOOTSTRAP_ADMIN_EMAIL + BOOTSTRAP_ADMIN_PASSWORD) via an idempotent upsert on the unique users.email; never invoked at server startup, never logs passwords.
// note: if this file changes, update header and README.md
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/config"
)

// bootstrapConfig carries the two operator-supplied credentials. The password
// is presence-validated only; its exact bytes are hashed and never echoed.
type bootstrapConfig struct {
	email    string
	password string
}

// bootstrapOutcome reports what the upsert did so the operator can see that an
// existing account had its authorization version rotated.
type bootstrapOutcome string

const (
	outcomeCreated     bootstrapOutcome = "created"
	outcomeReactivated bootstrapOutcome = "reactivated"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := resolveBootstrapConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	dsn := strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	if dsn == "" {
		log.Fatal("DATABASE_DSN is not set (set it in .env or export it)")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open controlhub db: %v", err)
	}
	defer db.Close()

	outcome, err := runBootstrap(context.Background(), db, cfg)
	if err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	printReport(os.Stdout, cfg.email, outcome)
}

// resolveBootstrapConfig requires BOTH credentials from the environment. A
// missing or blank value is a hard error — the command never invents a default
// identity or password. Email is normalized to lowercase to match Login lookup;
// the password is trimmed for the presence check only and otherwise stored as
// supplied.
func resolveBootstrapConfig() (bootstrapConfig, error) {
	email := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	if email == "" {
		return bootstrapConfig{}, errors.New("BOOTSTRAP_ADMIN_EMAIL is not set (supply the administrator email explicitly)")
	}
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if strings.TrimSpace(password) == "" {
		return bootstrapConfig{}, errors.New("BOOTSTRAP_ADMIN_PASSWORD is not set (supply the administrator password explicitly)")
	}
	return bootstrapConfig{email: strings.ToLower(email), password: password}, nil
}

// hashPassword hashes with the exact SHA-256 hex scheme internal/service uses
// for password authentication, so a bootstrap-created credential signs in
// without any change to existing hashes.
func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// runBootstrap creates the administrator, or reactivates an existing account
// while rotating authorization_version so previously issued Bearer Credentials
// die. One idempotent upsert against the unique users.email — safe to run
// repeatedly. MySQL affected rows: 1 = inserted, 2 = existing row updated; the
// update path always bumps authorization_version, so an existing row is always
// reported as 2.
func runBootstrap(ctx context.Context, db *sql.DB, cfg bootstrapConfig) (bootstrapOutcome, error) {
	var adminRoleID uint64
	err := db.QueryRowContext(ctx, `select id from roles where name = 'admin' limit 1`).Scan(&adminRoleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("admin role not found in roles table")
	}
	if err != nil {
		return "", fmt.Errorf("look up admin role: %w", err)
	}

	res, err := db.ExecContext(ctx, `
		insert into users (email, password_hash, display_name, role_id)
		values (?, ?, 'ControlHub Admin', ?)
		on duplicate key update
			password_hash = values(password_hash),
			display_name = values(display_name),
			role_id = values(role_id),
			is_active = 1,
			authorization_version = authorization_version + 1`,
		cfg.email, hashPassword(cfg.password), adminRoleID,
	)
	if err != nil {
		return "", fmt.Errorf("upsert admin: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	switch affected {
	case 1:
		return outcomeCreated, nil
	case 2:
		return outcomeReactivated, nil
	default:
		return "", fmt.Errorf("upsert admin: unexpected affected rows %d", affected)
	}
}

// printReport prints ONLY the identity and outcome. The password and its hash
// are never part of the report.
func printReport(w io.Writer, email string, outcome bootstrapOutcome) {
	fmt.Fprintln(w, "bootstrap-admin")
	fmt.Fprintf(w, "  email:   %s\n", email)
	fmt.Fprintf(w, "  role:    admin\n")
	fmt.Fprintf(w, "  outcome: %s\n", outcome)
	if outcome == outcomeReactivated {
		fmt.Fprintln(w, "  prior credentials invalidated (authorization version rotated)")
	}
}
