// Package main is an explicit TEST/CI-ONLY fixture provisioning command.
//
// input: os, strings, errors, context, database/sql, crypto/sha256, encoding/hex, fmt, io, log, internal/config, go-sql-driver/mysql
// output: main() — explicit one-shot binary (go run ./cmd/e2e-fixture-bootstrap), runFixtureBootstrap seam, resolveFixtureConfig, hashPassword, printReport
// pos: Creates or reactivates admin AND editor fixture identities for isolated
// E2E runs from operator-supplied env; requires .invalid fixture emails (RFC
// 2606) and refuses the published 0002 seed identities; never invoked at
// server startup, never logs passwords.
// note: if this file changes, update header and README.md
//
// This command exists ONLY to provision throwaway operator identities for
// isolated frontend E2E runs (local and CI). It is the counterpart of
// cmd/bootstrap-admin for the editor role and adds a hard guard: it refuses
// to recreate the published seed accounts (admin@example.com / editor@example.com
// / secret123) that migration 00016 disabled. Production authorization
// semantics are untouched — the identities it creates are ordinary users
// subject to the same server-enforced role matrix.
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

// fixtureCredential carries one role's operator-supplied fixture identity.
// The password is presence-validated only; its exact bytes are hashed and
// never echoed.
type fixtureCredential struct {
	Email    string
	Password string
	Role     string
}

// fixtureSet carries both fixture identities required for the full console
// access matrix (admin + editor).
type fixtureSet struct {
	Admin  fixtureCredential
	Editor fixtureCredential
}

// bootstrapOutcome mirrors cmd/bootstrap-admin's outcome vocabulary.
type bootstrapOutcome string

const (
	outcomeCreated     bootstrapOutcome = "created"
	outcomeReactivated bootstrapOutcome = "reactivated"
)

// legacySeedEmails are the 0002-published accounts migration 00016 disabled.
// This seam must never recreate them — E2E must use explicit per-run fixtures.
var legacySeedEmails = map[string]bool{
	"admin@example.com":  true,
	"editor@example.com": true,
}

// legacySeedPassword is the 0002-published shared password.
const legacySeedPassword = "secret123"

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := resolveFixtureConfig()
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

	outcomes, err := runFixtureBootstrap(context.Background(), db, cfg)
	if err != nil {
		log.Fatalf("fixture bootstrap: %v", err)
	}
	printReport(os.Stdout, cfg, outcomes)
}

// resolveFixtureConfig requires ALL FOUR credentials from the environment.
// A missing or blank value is a hard error — the command never invents a
// default identity or password. The published 0002 seed identities are
// refused outright so E2E can never silently regress to them.
func resolveFixtureConfig() (fixtureSet, error) {
	admin, err := resolveCredential("E2E_FIXTURE_ADMIN", "admin")
	if err != nil {
		return fixtureSet{}, err
	}
	editor, err := resolveCredential("E2E_FIXTURE_EDITOR", "editor")
	if err != nil {
		return fixtureSet{}, err
	}
	return fixtureSet{Admin: admin, Editor: editor}, nil
}

func resolveCredential(prefix, role string) (fixtureCredential, error) {
	email := strings.TrimSpace(os.Getenv(prefix + "_EMAIL"))
	if email == "" {
		return fixtureCredential{}, fmt.Errorf("%s_EMAIL is not set (supply the E2E fixture %s identity explicitly)", prefix, role)
	}
	password := os.Getenv(prefix + "_PASSWORD")
	if strings.TrimSpace(password) == "" {
		return fixtureCredential{}, fmt.Errorf("%s_PASSWORD is not set (supply the E2E fixture %s password explicitly)", prefix, role)
	}
	email = strings.ToLower(email)
	if !strings.HasSuffix(email, ".invalid") {
		// RFC 2606 reserved TLD: a fixture identity can never collide with a
		// real operator account, so an accidental production DATABASE_DSN
		// cannot take over an existing operator.
		return fixtureCredential{}, fmt.Errorf("E2E fixture %s email %q must end with .invalid (RFC 2606 reserved TLD; fixtures must never collide with real operator accounts)", role, email)
	}
	if legacySeedEmails[email] {
		return fixtureCredential{}, fmt.Errorf("refusing published seed email %q for the E2E %s fixture: migration 00016 disabled the 0002 accounts; provision an explicit per-run identity", email, role)
	}
	if password == legacySeedPassword {
		return fixtureCredential{}, fmt.Errorf("refusing the published seed password for the E2E %s fixture: migration 00016 disabled the 0002 accounts; provision an explicit per-run password", role)
	}
	return fixtureCredential{Email: email, Password: password, Role: role}, nil
}

// hashPassword hashes with the exact SHA-256 hex scheme internal/service uses
// for password authentication, so a fixture-created credential signs in
// without any change to existing hashes.
func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// runFixtureBootstrap upserts both fixture identities with their server roles
// (admin / editor), reactivating when present while rotating
// authorization_version so previously issued Bearer Credentials die.
func runFixtureBootstrap(ctx context.Context, db *sql.DB, set fixtureSet) (map[string]bootstrapOutcome, error) {
	outcomes := make(map[string]bootstrapOutcome, 2)
	for _, cred := range []fixtureCredential{set.Admin, set.Editor} {
		outcome, err := upsertFixtureUser(ctx, db, cred)
		if err != nil {
			return nil, err
		}
		outcomes[cred.Email] = outcome
	}
	return outcomes, nil
}

func upsertFixtureUser(ctx context.Context, db *sql.DB, cred fixtureCredential) (bootstrapOutcome, error) {
	var roleID uint64
	err := db.QueryRowContext(ctx, `select id from roles where name = ? limit 1`, cred.Role).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("role %q not found in roles table", cred.Role)
	}
	if err != nil {
		return "", fmt.Errorf("look up role %q: %w", cred.Role, err)
	}

	res, err := db.ExecContext(ctx, `
		insert into users (email, password_hash, display_name, role_id)
		values (?, ?, ?, ?)
		on duplicate key update
			password_hash = values(password_hash),
			display_name = values(display_name),
			role_id = values(role_id),
			is_active = 1,
			authorization_version = authorization_version + 1`,
		cred.Email, hashPassword(cred.Password), fmt.Sprintf("E2E %s Fixture", cred.Role), roleID,
	)
	if err != nil {
		return "", fmt.Errorf("upsert %s fixture: %w", cred.Role, err)
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
		return "", fmt.Errorf("upsert %s fixture: unexpected affected rows %d", cred.Role, affected)
	}
}

// printReport prints ONLY identities, roles, and outcomes. Passwords and
// hashes are never part of the report.
func printReport(w io.Writer, set fixtureSet, outcomes map[string]bootstrapOutcome) {
	fmt.Fprintln(w, "e2e-fixture-bootstrap (test/CI-only provisioning)")
	for _, cred := range []fixtureCredential{set.Admin, set.Editor} {
		fmt.Fprintf(w, "  email:   %s\n", cred.Email)
		fmt.Fprintf(w, "  role:    %s\n", cred.Role)
		fmt.Fprintf(w, "  outcome: %s\n", outcomes[cred.Email])
	}
}
