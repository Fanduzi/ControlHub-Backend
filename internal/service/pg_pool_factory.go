// Package service provides the PostgreSQL connection factory for the governed query workbench.
// input: context, errors, fmt, strconv, strings, sync, time, jackc/pgx/v5, jackc/pgx/v5/pgxpool
// output: OpenPostgresPool, PGPoolErrorCode, ErrPGVersionUnsupported, ErrPGConnectFailed, ErrPGVersionReadFailed, PGVersionUnsupportedCode
// pos: Builds one native pgxpool from a T2-validated ConnConfig, checks SHOW server_version_num on every new physical connection, and closes the pool on every failure
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL servers accepted by the frozen G4 gate: PG 14 through 17.
// server_version_num is compared numerically; 180000 (PG 18) is excluded.
const (
	pgServerVersionMin = 140000
	pgServerVersionMax = 180000

	// postgresPoolMaxConns matches MySQLQueryExecutor's per-target
	// MaxOpenConns of 1. The other pool periods below are pgxpool v5.11
	// ParseConfig operational defaults, not a product contract (G4).
	postgresPoolMaxConns = int32(1)

	// Shell identity is a public-ParseConfig stand-in. It is never dialed.
	// The validated ConnConfig replaces it before NewWithConfig.
	pgPoolShellHost     = "127.0.0.1"
	pgPoolShellPort     = uint16(1)
	pgPoolShellDatabase = "pg_pool_shell"
	pgPoolShellUser     = "pg_pool_shell"
	pgPoolShellPassword = "pg-pool-shell-unused"
)

// PGVersionUnsupportedCode is the frozen controlled error code for a server
// outside [140000, 180000). HTTP mapping stays on the existing handler table
// when a later ticket surfaces this failure; this factory does not open an
// execution route.
const PGVersionUnsupportedCode = "pg_version_unsupported"

// ErrPGVersionUnsupported is the safe, fixed failure for a server version
// outside the supported range. The message never includes the DSN, password,
// or config.
var ErrPGVersionUnsupported = errors.New("postgresql server version is not supported")

// ErrPGConnectFailed is the safe failure for a dial that did not produce a
// connection. Driver text is discarded so a DSN fragment cannot surface.
var ErrPGConnectFailed = errors.New("postgresql connection failed")

// ErrPGVersionReadFailed is the safe failure for a connection that came up
// but did not yield a numeric server_version_num. It is distinct from
// ErrPGVersionUnsupported, which means the number was read and rejected.
var ErrPGVersionReadFailed = errors.New("postgresql server version could not be read")

// pgVersionReader reads the server version from a native connection that has
// been dialed but not yet added to the pool. Tests replace it with a
// constructed number; production always uses readPGServerVersionNum.
type pgVersionReader func(context.Context, *pgx.Conn) (int, error)

// pgPoolShellFunc builds the legal pgxpool.Config shell. Tests replace it to
// simulate an init failure. The shell's ConnConfig is never dialed.
type pgPoolShellFunc func() (*pgxpool.Config, error)

// OpenPostgresPool dials a native pgx pool from a ConnConfig already returned
// by validatePGDSNBinding. The pool is handed back only after one live
// connection has passed the version gate. The caller owns the returned pool
// and must Close it.
//
// Every new physical connection is checked in AfterConnect, before pgxpool
// accepts it. Pool.Reset, expiry, and disconnect therefore cannot deliver an
// unchecked connection. Creating the pool object is not success. On shell
// init failure, dial failure, context cancel, deadline, version-read failure,
// or an unsupported version, the result is a fixed error and no usable pool.
// cfg is copied; this function does not parse the caller's DSN again.
func OpenPostgresPool(ctx context.Context, cfg *pgx.ConnConfig) (*pgxpool.Pool, error) {
	return openPostgresPool(ctx, cfg, readPGServerVersionNum)
}

func openPostgresPool(ctx context.Context, cfg *pgx.ConnConfig, readVersion pgVersionReader) (*pgxpool.Pool, error) {
	return openPostgresPoolWithShell(ctx, cfg, readVersion, loadPGPoolConfigShell)
}

func openPostgresPoolWithShell(ctx context.Context, cfg *pgx.ConnConfig, readVersion pgVersionReader, shell pgPoolShellFunc) (*pgxpool.Pool, error) {
	if cfg == nil {
		return nil, ErrPGConnectFailed
	}
	if readVersion == nil {
		readVersion = readPGServerVersionNum
	}
	poolCfg, err := postgresPoolConfigFrom(cfg, shell)
	if err != nil {
		return nil, err
	}
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		version, err := readVersion(ctx, conn)
		if err != nil {
			return classifyPGVersionReadError(err)
		}
		if !pgServerVersionSupported(version) {
			return ErrPGVersionUnsupported
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, classifyPGDialError(err)
	}
	accepted := false
	defer func() {
		if !accepted {
			pool.Close()
		}
	}()

	// The first Acquire runs the AfterConnect gate before this function
	// returns. Release is registered after the failure closer, so a rejected
	// first connection is returned before the pool is closed.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, classifyPGDialError(err)
	}
	defer conn.Release()
	accepted = true
	return pool, nil
}

// postgresPoolConfig installs the already-validated ConnConfig on a pgxpool
// Config created by the public ParseConfig API. The shell's own ConnConfig
// is discarded and is not dialed.
func postgresPoolConfig(cfg *pgx.ConnConfig) (*pgxpool.Config, error) {
	return postgresPoolConfigFrom(cfg, loadPGPoolConfigShell)
}

func postgresPoolConfigFrom(cfg *pgx.ConnConfig, shell pgPoolShellFunc) (*pgxpool.Config, error) {
	if cfg == nil {
		return nil, ErrPGConnectFailed
	}
	if shell == nil {
		shell = loadPGPoolConfigShell
	}
	base, err := shell()
	if err != nil || base == nil || base.ConnConfig == nil {
		// shell() may carry a service-file path or the shell conn string.
		// Callers see only the fixed connect error.
		return nil, ErrPGConnectFailed
	}
	// Copy is the public clone. It keeps the constructor flag ParseConfig
	// set; replacing ConnConfig afterwards is what the dial uses.
	poolCfg := base.Copy()
	poolCfg.ConnConfig = cfg.Copy()
	poolCfg.MaxConns = postgresPoolMaxConns
	poolCfg.MinConns = 0
	poolCfg.MinIdleConns = 0
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute
	poolCfg.BeforeConnect = nil
	poolCfg.AfterConnect = nil
	return poolCfg, nil
}

var (
	pgPoolShellMu sync.Mutex
	pgPoolShell   *pgxpool.Config
)

// loadPGPoolConfigShell returns the process-wide shell. A successful shell is
// reused so later requests do not parse again. A failure is not cached: the
// next call tries again. This function does not change the process environment.
func loadPGPoolConfigShell() (*pgxpool.Config, error) {
	pgPoolShellMu.Lock()
	defer pgPoolShellMu.Unlock()
	if pgPoolShell != nil {
		return pgPoolShell, nil
	}
	shell, err := parsePGPoolConfigShell()
	if err != nil {
		return nil, err
	}
	if shell == nil || shell.ConnConfig == nil {
		return nil, errors.New("postgres pool config shell is empty")
	}
	pgPoolShell = shell
	return pgPoolShell, nil
}

// parsePGPoolConfigShell asks pgxpool.ParseConfig for a legal Config shell.
// The keyword string overrides the env keys that would otherwise read a
// passfile or client certificates. It is not the caller's DSN, and its
// ConnConfig is never installed on a pool that dials.
//
// sslrootcert= must be last. pgx treats the next token as the value when an
// empty keyword is followed by another key, so an empty sslrootcert earlier
// in the string would name a file. An empty final value suppresses the
// default and PGSSLROOTCERT file read. sslmode=disable then returns before
// sslcert or sslkey are opened. A non-empty password skips the passfile.
//
// PGSERVICE cannot be removed by a conn string. When that variable is set,
// ParseConfig still reads the service file and this function returns the
// driver error. The factory turns that into ErrPGConnectFailed and does not dial.
func parsePGPoolConfigShell() (*pgxpool.Config, error) {
	connString := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable application_name=pg-pool-shell connect_timeout=1 target_session_attrs=any sslrootcert=",
		pgPoolShellHost, pgPoolShellPort, pgPoolShellDatabase, pgPoolShellUser, pgPoolShellPassword,
	)
	return pgxpool.ParseConfig(connString)
}

func pgServerVersionSupported(versionNum int) bool {
	return versionNum >= pgServerVersionMin && versionNum < pgServerVersionMax
}

func readPGServerVersionNum(ctx context.Context, conn *pgx.Conn) (int, error) {
	var raw string
	if err := conn.QueryRow(ctx, "SHOW server_version_num").Scan(&raw); err != nil {
		return 0, err
	}
	return parsePGServerVersionNum(raw)
}

func parsePGServerVersionNum(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("empty server_version_num")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("negative server_version_num")
	}
	return n, nil
}

// classifyPGDialError maps pool construction and Acquire failures onto a
// fixed error. Version sentinels produced by AfterConnect must survive;
// Acquire returns them directly, and a generic connect failure would hide
// the gate decision. Any other driver text is dropped.
func classifyPGDialError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPGVersionUnsupported):
		return ErrPGVersionUnsupported
	case errors.Is(err, ErrPGVersionReadFailed):
		return ErrPGVersionReadFailed
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrQueryTimeout):
		return ErrQueryTimeout
	default:
		return ErrPGConnectFailed
	}
}

func classifyPGVersionReadError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrQueryTimeout
	default:
		return ErrPGVersionReadFailed
	}
}

// PGPoolErrorCode maps a factory error onto the existing controlled-code
// vocabulary. Version rejection uses the frozen pg_version_unsupported code.
// Deadline, cancellation, and other dial/read failures reuse the codes the
// MySQL execution path already records. The returned string is safe to show;
// it is not the driver error.
func PGPoolErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrPGVersionUnsupported):
		return PGVersionUnsupportedCode
	case errors.Is(err, ErrQueryTimeout):
		return "query_timeout"
	case errors.Is(err, context.Canceled):
		return "query_canceled"
	default:
		return "query_backend_error"
	}
}
