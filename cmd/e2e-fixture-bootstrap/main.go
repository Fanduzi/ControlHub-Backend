// Package main is an explicit TEST/CI-ONLY fixture provisioning command.
//
// input: os, strings, errors, context, database/sql, crypto/sha256, encoding/hex, fmt, io, log, internal/config, go-sql-driver/mysql
// output: main() — explicit one-shot binary (go run ./cmd/e2e-fixture-bootstrap), runFixtureBootstrap seam, resolveFixtureConfig, parseDisposableDSN, verifyFixtureDatabase, hashPassword, printReport
// pos: Creates or reactivates admin AND editor fixture identities for isolated
// E2E runs, gated by an explicit test-mode capability, a dedicated disposable
// metadata DSN (loopback host + *_e2e database name), migration-00016
// verification with retired seeds inactive, .invalid fixture emails (RFC
// 2606), and refusal of the published 0002 seed identities. Never invoked at
// server startup, never logs passwords.
// note: if this file changes, update header and README.md
//
// SAFETY BOUNDARY (the primary production guard):
//
// This command cannot mutate a production database by accident. It requires
// ALL of the following before any SQL runs:
//
//  1. CONTROLHUB_E2E_FIXTURE_MODE=1 — an explicit test-only capability;
//     missing or any other value fails loudly.
//  2. E2E_FIXTURE_DATABASE_DSN — a DEDICATED E2E metadata DSN. The generic
//     DATABASE_DSN is never read. The DSN must parse (driver errors are never
//     echoed), its host must be a literal loopback address (127.0.0.1 / ::1 —
//     hostnames such as `localhost` are refused because their resolution
//     cannot be verified locally), and its database name must match the
//     disposable naming rule ^controlhub_[a-z0-9_]*e2e$ (the default
//     `controlhub` database and production-like names are rejected).
//  3. The database must be migrated to at least 00016 (applied rows only)
//     AND the retired 0002 seed accounts (admin@example.com /
//     editor@example.com) must both exist and be inactive; otherwise
//     provisioning refuses to run.
//  4. Fixture emails must end with `.invalid` (RFC 2606 reserved TLD), the
//     retired seed identities are refused, and the admin and editor fixture
//     emails must be distinct.
//
// The `.invalid` email rule is an ADDITIONAL guard — it is NOT the primary
// production-safety boundary. The gates above protect against accidental
// misconfiguration: a production DSN cannot be accepted by the dedicated-DSN
// gate (literal loopback + disposable name) and the migration/seed
// verification runs before any mutation. Provisioning is transactional: a
// partial failure never leaves a usable fixture administrator behind.
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
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"

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

const (
	// fixtureModeEnv is the explicit test-only capability. Any value other
	// than "1" refuses to run.
	fixtureModeEnv = "CONTROLHUB_E2E_FIXTURE_MODE"
	// fixtureModeRequired is the only accepted capability value.
	fixtureModeRequired = "1"
	// fixtureDSNEnv is the DEDICATED E2E metadata DSN. The generic
	// DATABASE_DSN is intentionally never read by this command.
	fixtureDSNEnv = "E2E_FIXTURE_DATABASE_DSN"
)

// disposableDatabaseNameRE is the strict disposable-database naming rule:
// the ControlHub project prefix and an `e2e` ending (e.g.
// controlhub_e2e, controlhub_issue15_e2e). The default `controlhub`
// database and production-like names never match.
var disposableDatabaseNameRE = regexp.MustCompile(`^controlhub_[a-z0-9_]*e2e$`)

// loopbackHosts are the only acceptable metadata database hosts: literal
// loopback addresses (including Testcontainers' host-port mappings, which
// surface on 127.0.0.1). Hostnames (including `localhost`) are NOT accepted:
// a name's resolution cannot be verified locally, so a tunneled
// production-like target can never sneak in under a hostname.
var loopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
}

// legacySeedEmails are the 0002-published accounts migration 00016 disabled.
// This seam must never recreate them — E2E must use explicit per-run fixtures.
var legacySeedEmails = map[string]bool{
	"admin@example.com":  true,
	"editor@example.com": true,
}

// legacySeedPassword is the 0002-published shared password.
const legacySeedPassword = "secret123"

// migration00016 is the seed-credential remediation migration that must be
// applied before fixtures can be provisioned.
const migration00016 = 16

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := resolveFixtureConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		log.Fatalf("open controlhub e2e db: %v", err)
	}
	defer db.Close()

	if err := verifyFixtureDatabase(context.Background(), dbAdapter{db}); err != nil {
		log.Fatalf("fixture database verification failed: %v", err)
	}

	outcomes, err := runFixtureBootstrap(context.Background(), dbTxStarter{db}, cfg.Fixtures)
	if err != nil {
		log.Fatalf("fixture bootstrap: %v", err)
	}
	printReport(os.Stdout, cfg.Fixtures, outcomes)
}

// fixtureConfig is the fully validated command configuration.
type fixtureConfig struct {
	DSN      string
	Fixtures fixtureSet
}

// resolveFixtureConfig requires the explicit test-mode capability, the
// dedicated disposable E2E DSN, and ALL FOUR fixture credentials from the
// environment. Missing or blank values are hard errors — the command never
// invents a default identity, password, or database. Every gate runs before
// any database connection or mutation.
func resolveFixtureConfig() (fixtureConfig, error) {
	if os.Getenv(fixtureModeEnv) != fixtureModeRequired {
		return fixtureConfig{}, fmt.Errorf("%s=%s is required (explicit test-only capability; refusing to run without it)", fixtureModeEnv, fixtureModeRequired)
	}

	dsn := strings.TrimSpace(os.Getenv(fixtureDSNEnv))
	if dsn == "" {
		return fixtureConfig{}, fmt.Errorf("%s is not set (supply the dedicated disposable E2E metadata DSN; the generic DATABASE_DSN is never read)", fixtureDSNEnv)
	}
	if _, err := parseDisposableDSN(dsn); err != nil {
		return fixtureConfig{}, err
	}

	admin, err := resolveCredential("E2E_FIXTURE_ADMIN", "admin")
	if err != nil {
		return fixtureConfig{}, err
	}
	editor, err := resolveCredential("E2E_FIXTURE_EDITOR", "editor")
	if err != nil {
		return fixtureConfig{}, err
	}
	if admin.Email == editor.Email {
		return fixtureConfig{}, fmt.Errorf("E2E fixture admin and editor emails must be distinct (both resolved to %q); identical identities would silently drop the administrator", admin.Email)
	}
	return fixtureConfig{DSN: dsn, Fixtures: fixtureSet{Admin: admin, Editor: editor}}, nil
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
		// RFC 2606 reserved TLD: an additional guard so a fixture identity can
		// never collide with a real operator account. This is NOT the primary
		// production-safety boundary — the dedicated-DSN gate is.
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

// disposableDSN is a parsed, validated E2E metadata DSN.
type disposableDSN struct {
	// Addr is the canonical loopback host:port (e.g. "127.0.0.1:3306").
	Addr string
	// DBName is the validated disposable database name (matches *_e2e).
	DBName string
}

// parseDisposableDSN validates the dedicated E2E metadata DSN:
//   - parses with the MySQL driver's parser (rejects malformed DSNs; the
//     driver's raw error is never echoed — it can carry secret-bearing
//     parameter values);
//   - host must be a literal loopback address (127.0.0.1 / ::1); hostnames
//     such as `localhost` are refused because their resolution cannot be
//     verified locally;
//   - database name must match the disposable naming rule
//     ^controlhub_[a-z0-9_]*e2e$;
//   - the default `controlhub` database and empty names are rejected.
//
// This is the PRIMARY production-safety boundary: a misconfigured production
// DSN (remote host or production database name) is refused before any
// connection or mutation.
func parseDisposableDSN(dsn string) (disposableDSN, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return disposableDSN{}, errors.New("E2E fixture DSN is malformed (rejected by the MySQL driver parser)")
	}
	if cfg.Net != "tcp" {
		return disposableDSN{}, fmt.Errorf("E2E fixture DSN must use tcp (got %q)", cfg.Net)
	}
	host, _, err := splitHostPort(cfg.Addr)
	if err != nil || !loopbackHosts[host] {
		return disposableDSN{}, fmt.Errorf("E2E fixture DSN host %q is not a literal loopback address (only 127.0.0.1 / ::1 are accepted; hostnames such as localhost are refused because their resolution cannot be verified locally)", cfg.Addr)
	}
	if cfg.DBName == "" {
		return disposableDSN{}, fmt.Errorf("E2E fixture DSN has no database name")
	}
	if cfg.DBName == "controlhub" {
		return disposableDSN{}, fmt.Errorf("E2E fixture DSN must not use the default %q database; provision a dedicated disposable database", cfg.DBName)
	}
	if !disposableDatabaseNameRE.MatchString(cfg.DBName) {
		return disposableDSN{}, fmt.Errorf("E2E fixture DSN database name %q does not match the disposable naming rule ^controlhub_[a-z0-9_]*e2e$", cfg.DBName)
	}
	return disposableDSN{Addr: cfg.Addr, DBName: cfg.DBName}, nil
}

// splitHostPort splits "host" or "host:port" (bracketed IPv6 allowed).
func splitHostPort(addr string) (host string, port string, err error) {
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end == -1 {
			return "", "", errors.New("unterminated IPv6 bracket")
		}
		host = addr[1:end]
		rest := addr[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", "", errors.New("malformed address")
			}
			port = rest[1:]
		}
		return host, port, nil
	}
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx], addr[idx+1:], nil
	}
	return addr, "", nil
}

// rowScanner is the minimal scan surface verifyFixtureDatabase needs, so the
// migration/seed verification is unit-testable without a live database.
type rowScanner interface {
	Scan(dest ...any) error
}

// fixtureProbe is the database surface used by verifyFixtureDatabase.
// dbAdapter bridges *sql.DB (whose QueryRowContext returns *sql.Row).
type fixtureProbe interface {
	QueryRowContext(ctx context.Context, query string, args ...any) rowScanner
}

// dbAdapter adapts *sql.DB to fixtureProbe for unit-testability.
type dbAdapter struct{ db *sql.DB }

func (a dbAdapter) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	return a.db.QueryRowContext(ctx, query, args...)
}

// verifyFixtureDatabase refuses to provision unless the E2E metadata
// database has an APPLIED row for migration 00016 (version 16 itself, not a
// later version) AND both retired 0002 seed accounts exist and are inactive.
// Runs before any mutation.
func verifyFixtureDatabase(ctx context.Context, db fixtureProbe) error {
	var applied int
	err := db.QueryRowContext(ctx, `select count(*) from goose_db_version where version_id = ? and is_applied = 1`, migration00016).Scan(&applied)
	if err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if applied == 0 {
		return fmt.Errorf("E2E fixture database has no applied row for migration 00016 (retired seed remediation); provisioning refuses until it is applied")
	}

	for _, email := range []string{"admin@example.com", "editor@example.com"} {
		var active int
		err := db.QueryRowContext(ctx, `select is_active from users where email = ?`, email).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("retired seed account %s is missing from the E2E fixture database; it must be seeded (migration 0002) and disabled (migration 00016)", email)
		}
		if err != nil {
			return fmt.Errorf("read retired seed account %s: %w", email, err)
		}
		if active != 0 {
			return fmt.Errorf("retired seed account %s is active; migration 00016 must leave the published seeds disabled", email)
		}
	}
	return nil
}

// hashPassword hashes with the exact SHA-256 hex scheme internal/service uses
// for password authentication, so a fixture-created credential signs in
// without any change to existing hashes.
func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// dmlExecutor is the write surface used by the upsert path; txAdapter
// bridges *sql.Tx (whose QueryRowContext returns *sql.Row).
type dmlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) rowScanner
}

// fixtureTransaction is the transaction surface runFixtureBootstrap needs:
// commit/rollback plus the upsert executor.
type fixtureTransaction interface {
	Commit() error
	Rollback() error
	dmlExecutor
}

// fixtureTxStarter begins a fixture transaction; dbTxStarter bridges *sql.DB.
type fixtureTxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (fixtureTransaction, error)
}

// dbTxStarter adapts *sql.DB to fixtureTxStarter.
type dbTxStarter struct{ db *sql.DB }

func (s dbTxStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (fixtureTransaction, error) {
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return txWrapper{tx}, nil
}

// txWrapper adapts *sql.Tx to fixtureTransaction (rowScanner return).
type txWrapper struct{ tx *sql.Tx }

func (w txWrapper) Commit() error   { return w.tx.Commit() }
func (w txWrapper) Rollback() error { return w.tx.Rollback() }
func (w txWrapper) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return w.tx.ExecContext(ctx, query, args...)
}
func (w txWrapper) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	return w.tx.QueryRowContext(ctx, query, args...)
}

// runFixtureBootstrap upserts both fixture identities (admin / editor) in ONE
// transaction: either both persist or neither does, so a partial failure can
// never leave a usable fixture administrator behind. Reactivation rotates
// authorization_version so previously issued Bearer Credentials die.
func runFixtureBootstrap(ctx context.Context, starter fixtureTxStarter, set fixtureSet) (map[string]bootstrapOutcome, error) {
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin fixture transaction: %w", err)
	}
	outcomes, err := upsertFixtures(ctx, tx, set)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit fixture transaction: %w", err)
	}
	return outcomes, nil
}

// upsertFixtures performs the per-identity upserts against an executor
// (inside the caller's transaction).
func upsertFixtures(ctx context.Context, exec dmlExecutor, set fixtureSet) (map[string]bootstrapOutcome, error) {
	outcomes := make(map[string]bootstrapOutcome, 2)
	for _, cred := range []fixtureCredential{set.Admin, set.Editor} {
		outcome, err := upsertFixtureUser(ctx, exec, cred)
		if err != nil {
			return nil, err
		}
		outcomes[cred.Email] = outcome
	}
	return outcomes, nil
}

func upsertFixtureUser(ctx context.Context, exec dmlExecutor, cred fixtureCredential) (bootstrapOutcome, error) {
	var roleID uint64
	err := exec.QueryRowContext(ctx, `select id from roles where name = ? limit 1`, cred.Role).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("role %q not found in roles table", cred.Role)
	}
	if err != nil {
		return "", fmt.Errorf("look up role %q: %w", cred.Role, err)
	}

	res, err := exec.ExecContext(ctx, `
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

// printReport prints ONLY identities, roles, and outcomes. Passwords, hashes,
// and the DSN are never part of the report.
func printReport(w io.Writer, set fixtureSet, outcomes map[string]bootstrapOutcome) {
	fmt.Fprintln(w, "e2e-fixture-bootstrap (test/CI-only provisioning)")
	for _, cred := range []fixtureCredential{set.Admin, set.Editor} {
		fmt.Fprintf(w, "  email:   %s\n", cred.Email)
		fmt.Fprintf(w, "  role:    %s\n", cred.Role)
		fmt.Fprintf(w, "  outcome: %s\n", outcomes[cred.Email])
	}
}
