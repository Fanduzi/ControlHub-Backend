// Package service tests the PostgreSQL connection factory without a live server.
// input: context, errors, fmt, net, path/filepath, strings, testing, time, jackc/pgx/v5, jackc/pgx/v5/pgxpool, validatePGDSNBinding
// output: boundary, version-probe budget, config-preservation, shell-init failure, dial-failure, cancel, and timeout tests for OpenPostgresPool
// pos: Unit proof that the factory keeps the T2 config, rejects bad versions by number, and returns no pool on dial or shell-init failure
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGServerVersionSupported_Boundaries(t *testing.T) {
	t.Parallel()
	// These numbers are constructed. They are not readings from a PostgreSQL server.
	cases := []struct {
		version int
		want    bool
	}{
		{139999, false},
		{140000, true},
		{179999, true},
		{180000, false},
	}
	for _, tc := range cases {
		if got := pgServerVersionSupported(tc.version); got != tc.want {
			t.Errorf("pgServerVersionSupported(%d) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestParsePGServerVersionNum(t *testing.T) {
	t.Parallel()
	n, err := parsePGServerVersionNum(" 160004\n")
	if err != nil || n != 160004 {
		t.Fatalf("parse = %d, %v, want 160004", n, err)
	}
	if _, err := parsePGServerVersionNum(""); err == nil {
		t.Fatal("empty version was accepted")
	}
	if _, err := parsePGServerVersionNum("16.4"); err == nil {
		t.Fatal("dotted version was accepted")
	}
	if _, err := parsePGServerVersionNum("-1"); err == nil {
		t.Fatal("negative version was accepted")
	}
}

func TestPGVersionProbeBudget_UsesConnectTimeoutOrFiniteDefault(t *testing.T) {
	t.Parallel()
	// The version read does not inherit a deadline from Acquire. The budget
	// has to be finite even when the validated config omitted connect_timeout.
	if got := pgVersionProbeBudget(7 * time.Second); got != 7*time.Second {
		t.Fatalf("budget = %s, want the validated ConnectTimeout", got)
	}
	if got := pgVersionProbeBudget(0); got != pgVersionProbeDefaultBudget || got <= 0 {
		t.Fatalf("unset ConnectTimeout budget = %s, want a finite default", got)
	}
}

func TestPGPoolErrorCode_UsesFrozenVocabulary(t *testing.T) {
	t.Parallel()
	if PGPoolErrorCode(ErrPGVersionUnsupported) != "pg_version_unsupported" {
		t.Fatalf("version code = %q", PGPoolErrorCode(ErrPGVersionUnsupported))
	}
	if PGPoolErrorCode(ErrQueryTimeout) != "query_timeout" {
		t.Fatalf("timeout code = %q", PGPoolErrorCode(ErrQueryTimeout))
	}
	if PGPoolErrorCode(context.Canceled) != "query_canceled" {
		t.Fatalf("cancel code = %q", PGPoolErrorCode(context.Canceled))
	}
	if PGPoolErrorCode(ErrPGConnectFailed) != "query_backend_error" {
		t.Fatalf("connect code = %q", PGPoolErrorCode(ErrPGConnectFailed))
	}
	if PGPoolErrorCode(ErrPGVersionReadFailed) != "query_backend_error" {
		t.Fatalf("version-read code = %q", PGPoolErrorCode(ErrPGVersionReadFailed))
	}
	for _, err := range []error{ErrPGVersionUnsupported, ErrPGConnectFailed, ErrPGVersionReadFailed, ErrQueryTimeout} {
		if strings.Contains(err.Error(), "pw-do-not-leak") || strings.Contains(err.Error(), "postgres://") {
			t.Fatalf("controlled message leaks material: %q", err.Error())
		}
	}
}

func TestOpenPostgresPool_PreservesValidatedConfig(t *testing.T) {
	// Process environment must not be able to replace the validated config.
	// Not parallel: t.Setenv is process-wide.
	t.Setenv("PGHOST", "evil.example")
	t.Setenv("PGPORT", "1")
	t.Setenv("PGUSER", "envuser")
	t.Setenv("PGPASSWORD", "envpw")
	t.Setenv("PGDATABASE", "envdb")
	t.Setenv("PGSSLMODE", "require")
	t.Setenv("PGAPPNAME", "from-env")

	const dsn = "host=db.internal port=5432 dbname=orders user=ro password=pw-do-not-leak " +
		"sslmode=prefer connect_timeout=7 application_name=ch target_session_attrs=read-write"
	cfg, err := validatePGDSNBinding(dsn, "db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	beforeUser, beforePass, beforeHost := cfg.User, cfg.Password, cfg.Host
	beforeFallbacks := len(cfg.Fallbacks)
	disabled, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password=pw sslmode=disable",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("disable validate: %v", err)
	}

	// These paths do not exist. They are set after T2 validation so they
	// cannot fail the user DSN parse. The shell keyword string must still
	// build without reading them.
	t.Setenv("PGSSLROOTCERT", filepath.Join(t.TempDir(), "missing-root.crt"))
	t.Setenv("PGSSLCERT", filepath.Join(t.TempDir(), "missing.crt"))
	t.Setenv("PGSSLKEY", filepath.Join(t.TempDir(), "missing.key"))
	t.Setenv("PGPASSFILE", filepath.Join(t.TempDir(), "missing.pgpass"))

	poolCfg, err := postgresPoolConfig(cfg)
	if err != nil {
		t.Fatalf("pool config: %v", err)
	}
	shell, err := loadPGPoolConfigShell()
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	got := poolCfg.ConnConfig
	if poolCfg == shell || got == shell.ConnConfig {
		t.Fatal("dial config is the shell or its ConnConfig")
	}
	if shell.ConnConfig.Host != pgPoolShellHost || shell.ConnConfig.User != pgPoolShellUser || shell.ConnConfig.Database != pgPoolShellDatabase {
		t.Fatalf("shell identity host=%s user=%s db=%s", shell.ConnConfig.Host, shell.ConnConfig.User, shell.ConnConfig.Database)
	}
	if got == cfg {
		t.Fatal("pool config aliases the caller's ConnConfig")
	}
	if got.Host != "db.internal" || got.Port != 5432 || got.Database != "orders" || got.User != "ro" || got.Password != "pw-do-not-leak" {
		t.Fatalf("identity changed: host=%s port=%d db=%s user=%s", got.Host, got.Port, got.Database, got.User)
	}
	if got.ConnectTimeout != 7*time.Second {
		t.Fatalf("ConnectTimeout = %s, want 7s", got.ConnectTimeout)
	}
	if got.RuntimeParams["application_name"] != "ch" || len(got.RuntimeParams) != 1 {
		t.Fatalf("RuntimeParams = %v, want only application_name=ch", got.RuntimeParams)
	}
	if (got.TLSConfig == nil) != (cfg.TLSConfig == nil) {
		t.Fatal("TLSConfig nil-ness diverged from the validated config")
	}
	if got.TLSConfig != nil && cfg.TLSConfig != nil && got.TLSConfig.ServerName != cfg.TLSConfig.ServerName {
		t.Fatalf("TLS ServerName = %q, want %q", got.TLSConfig.ServerName, cfg.TLSConfig.ServerName)
	}
	if len(got.Fallbacks) != len(cfg.Fallbacks) || len(got.Fallbacks) == 0 {
		t.Fatalf("fallbacks = %d, validated = %d, want the same non-empty prefer set", len(got.Fallbacks), len(cfg.Fallbacks))
	}
	for i, fb := range got.Fallbacks {
		src := cfg.Fallbacks[i]
		if fb == src {
			t.Fatal("fallback aliases the caller's fallback")
		}
		if fb.Host != src.Host || fb.Port != src.Port || (fb.TLSConfig == nil) != (src.TLSConfig == nil) {
			t.Fatalf("fallback %d diverged", i)
		}
		if fb.Host != cfg.Host || fb.Port != cfg.Port {
			t.Fatalf("fallback %d is not the same endpoint", i)
		}
	}
	if poolCfg.MaxConns != 1 || poolCfg.MinConns != 0 || poolCfg.MinIdleConns != 0 || poolCfg.HealthCheckPeriod <= 0 {
		t.Fatalf("pool limits = max %d min %d idle %d health %s", poolCfg.MaxConns, poolCfg.MinConns, poolCfg.MinIdleConns, poolCfg.HealthCheckPeriod)
	}
	if cfg.User != beforeUser || cfg.Password != beforePass || cfg.Host != beforeHost || len(cfg.Fallbacks) != beforeFallbacks {
		t.Fatal("building the pool config mutated the validated ConnConfig")
	}
	got.RuntimeParams["application_name"] = "mutated"
	if cfg.RuntimeParams["application_name"] != "ch" {
		t.Fatal("pool RuntimeParams aliases the validated map")
	}

	disabledCfg, err := postgresPoolConfig(disabled)
	if err != nil {
		t.Fatalf("disable pool config: %v", err)
	}
	if disabledCfg.ConnConfig.TLSConfig != nil {
		t.Fatal("sslmode=disable gained a TLSConfig")
	}
	if disabledCfg.ConnConfig == shell.ConnConfig {
		t.Fatal("sslmode=disable dials the shell ConnConfig")
	}
}

func TestClassifyPGDialError_PreservesVersionGate(t *testing.T) {
	t.Parallel()
	// AfterConnect's decision comes back through Acquire. Turning it into
	// ErrPGConnectFailed would hide an unsupported server.
	if got := classifyPGDialError(ErrPGVersionUnsupported); !errors.Is(got, ErrPGVersionUnsupported) || got.Error() != ErrPGVersionUnsupported.Error() {
		t.Fatalf("unsupported = %v", got)
	}
	wrappedRead := fmt.Errorf("pool acquire: %w", ErrPGVersionReadFailed)
	if got := classifyPGDialError(wrappedRead); !errors.Is(got, ErrPGVersionReadFailed) || strings.Contains(got.Error(), "pool acquire") {
		t.Fatalf("version read = %v", got)
	}
	wrappedDial := fmt.Errorf("dial pw-do-not-leak: %w", errors.New("connection refused"))
	got := classifyPGDialError(wrappedDial)
	if !errors.Is(got, ErrPGConnectFailed) {
		t.Fatalf("dial = %v, want ErrPGConnectFailed", got)
	}
	assertNoSecret(t, got)
}

func TestOpenPostgresPool_ShellInitFailureIsSafe(t *testing.T) {
	t.Parallel()
	cfg, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password=pw-do-not-leak sslmode=disable",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	const leakedPath = "/tmp/pg-shell-missing-root.crt"
	pool, err := openPostgresPoolWithShell(context.Background(), cfg, nil, func() (*pgxpool.Config, error) {
		return nil, fmt.Errorf("read %s: password=%s", leakedPath, pgPoolShellPassword)
	})
	if pool != nil {
		pool.Close()
		t.Fatal("shell init failure returned a pool")
	}
	if !errors.Is(err, ErrPGConnectFailed) {
		t.Fatalf("error = %v, want ErrPGConnectFailed", err)
	}
	assertNoSecret(t, err)
	if strings.Contains(err.Error(), leakedPath) || strings.Contains(err.Error(), pgPoolShellPassword) || strings.Contains(err.Error(), "pg-shell") {
		t.Fatalf("shell init error leaked: %q", err.Error())
	}
}

func TestParsePGPoolConfigShell_ServiceEnvFailsClosed(t *testing.T) {
	// Not parallel: PGSERVICE is process-wide. The factory must not clear it
	// to make the shell parse, and must not return the driver text.
	cfg, err := validatePGDSNBinding(
		"host=db.internal port=5432 dbname=orders user=ro password=pw-do-not-leak sslmode=disable",
		"db.internal", 5432, "orders")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	serviceFile := filepath.Join(t.TempDir(), "missing-service.conf")
	t.Setenv("PGSERVICE", "no-such-service")
	t.Setenv("PGSERVICEFILE", serviceFile)

	_, parseErr := parsePGPoolConfigShell()
	if parseErr == nil {
		t.Fatal("PGSERVICE shell parse succeeded; the service file should have been read")
	}
	if !strings.Contains(parseErr.Error(), serviceFile) {
		t.Fatalf("parse error %q does not mention the service file, so the redaction test is vacuous", parseErr.Error())
	}
	pool, err := openPostgresPoolWithShell(context.Background(), cfg, nil, func() (*pgxpool.Config, error) {
		return nil, parseErr
	})
	if pool != nil {
		pool.Close()
		t.Fatal("service-file shell failure returned a pool")
	}
	if !errors.Is(err, ErrPGConnectFailed) {
		t.Fatalf("error = %v, want ErrPGConnectFailed", err)
	}
	assertNoSecret(t, err)
	if strings.Contains(err.Error(), serviceFile) || strings.Contains(err.Error(), "no-such-service") || strings.Contains(err.Error(), pgPoolShellPassword) {
		t.Fatalf("service-file failure leaked: %q", err.Error())
	}
}

func TestOpenPostgresPool_NilConfig(t *testing.T) {
	t.Parallel()
	pool, err := OpenPostgresPool(context.Background(), nil)
	if pool != nil {
		pool.Close()
		t.Fatal("nil config returned a pool")
	}
	if !errors.Is(err, ErrPGConnectFailed) {
		t.Fatalf("error = %v, want ErrPGConnectFailed", err)
	}
}

func TestOpenPostgresPool_ConnectFailureCleansUp(t *testing.T) {
	t.Parallel()
	cfg := refusedPGConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := OpenPostgresPool(ctx, cfg)
	if pool != nil {
		pool.Close()
		t.Fatal("connect failure returned a pool")
	}
	if !errors.Is(err, ErrPGConnectFailed) {
		t.Fatalf("error = %v, want ErrPGConnectFailed", err)
	}
	assertNoSecret(t, err)
	if cfg.Password != "pw-do-not-leak" || cfg.Host != "127.0.0.1" {
		t.Fatal("failed dial mutated the validated ConnConfig")
	}
}

func TestOpenPostgresPool_ContextCancelCleansUp(t *testing.T) {
	t.Parallel()
	cfg := hangingPGConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		pool *pgxpool.Pool
		err  error
	}
	done := make(chan result, 1)
	go func() {
		pool, err := OpenPostgresPool(ctx, cfg)
		done <- result{pool, err}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case got := <-done:
		if got.pool != nil {
			got.pool.Close()
			t.Fatal("canceled connect returned a pool")
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", got.err)
		}
		assertNoSecret(t, got.err)
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not unblock the connection factory")
	}
}

func TestOpenPostgresPool_ContextTimeoutCleansUp(t *testing.T) {
	t.Parallel()
	cfg := hangingPGConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	pool, err := OpenPostgresPool(ctx, cfg)
	if pool != nil {
		pool.Close()
		t.Fatal("timed-out connect returned a pool")
	}
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	assertNoSecret(t, err)
}

func refusedPGConfig(t *testing.T) *pgx.ConnConfig {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return pgConfigOn(t, addr.Port)
}

func hangingPGConfig(t *testing.T) *pgx.ConnConfig {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold the accepted socket without answering the startup packet so
			// the client stays inside the connect handshake until its context ends.
			go func() {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
				_ = c.Close()
			}()
		}
	}()
	return pgConfigOn(t, ln.Addr().(*net.TCPAddr).Port)
}

func pgConfigOn(t *testing.T, port int) *pgx.ConnConfig {
	t.Helper()
	dsn := fmt.Sprintf(
		"host=127.0.0.1 port=%d dbname=orders user=ro password=pw-do-not-leak sslmode=disable connect_timeout=2",
		port,
	)
	cfg, err := validatePGDSNBinding(dsn, "127.0.0.1", port, "orders")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return cfg
}

func assertNoSecret(t *testing.T, err error) {
	t.Helper()
	text := err.Error()
	for _, secret := range []string{"pw-do-not-leak", pgPoolShellPassword, "postgres://", "password="} {
		if strings.Contains(text, secret) {
			t.Fatalf("error text contains %q", secret)
		}
	}
}
