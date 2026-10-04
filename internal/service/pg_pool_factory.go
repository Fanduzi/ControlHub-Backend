// Package service provides the PostgreSQL connection factory for the governed query workbench.
// input: context, errors, reflect, strconv, strings, time, unsafe, jackc/pgx/v5, jackc/pgx/v5/pgxpool
// output: OpenPostgresPool, PGPoolErrorCode, ErrPGVersionUnsupported, ErrPGConnectFailed, ErrPGVersionReadFailed, PGVersionUnsupportedCode
// pos: Builds one native pgxpool from a T2-validated ConnConfig, checks SHOW server_version_num, and closes the pool on every failure
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

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

// pgVersionReader reads the server version from a connection the factory
// already acquired. Tests replace it with a constructed number; production
// always uses readPGServerVersionNum.
type pgVersionReader func(context.Context, *pgxpool.Conn) (int, error)

// OpenPostgresPool dials a native pgx pool from a ConnConfig already returned
// by validatePGDSNBinding. The pool is handed back only after a live
// connection has been acquired and SHOW server_version_num is inside
// [140000, 180000). The caller owns the returned pool and must Close it.
//
// Creating the pool object is not success. On dial failure, context cancel,
// deadline, version-read failure, or an unsupported version, the pool is
// closed before return and the result is nil. cfg is copied; this function
// does not parse a DSN and does not fill identity, TLS, fallbacks, or
// connect_timeout from the environment.
func OpenPostgresPool(ctx context.Context, cfg *pgx.ConnConfig) (*pgxpool.Pool, error) {
	return openPostgresPool(ctx, cfg, readPGServerVersionNum)
}

func openPostgresPool(ctx context.Context, cfg *pgx.ConnConfig, readVersion pgVersionReader) (*pgxpool.Pool, error) {
	if cfg == nil {
		return nil, ErrPGConnectFailed
	}
	if readVersion == nil {
		readVersion = readPGServerVersionNum
	}

	pool, err := pgxpool.NewWithConfig(ctx, postgresPoolConfig(cfg))
	if err != nil {
		return nil, classifyPGDialError(err)
	}
	accepted := false
	defer func() {
		if !accepted {
			pool.Close()
		}
	}()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, classifyPGDialError(err)
	}
	defer conn.Release()

	version, err := readVersion(ctx, conn)
	if err != nil {
		return nil, classifyPGVersionReadError(err)
	}
	if !pgServerVersionSupported(version) {
		return nil, ErrPGVersionUnsupported
	}
	accepted = true
	return pool, nil
}

// postgresPoolConfig installs the already-validated ConnConfig on a pgxpool
// Config. pgxpool.NewWithConfig panics unless the unexported
// createdByParseConfig flag is set, and the only exported setter is
// ParseConfig, which re-reads the process environment. The flag is set here
// so the validated copy is what dials — no second DSN parse and no env fill.
func postgresPoolConfig(cfg *pgx.ConnConfig) *pgxpool.Config {
	poolCfg := &pgxpool.Config{
		ConnConfig:        cfg.Copy(),
		MaxConns:          postgresPoolMaxConns,
		MinConns:          0,
		MinIdleConns:      0,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
	markPGXPoolConfigParsed(poolCfg)
	return poolCfg
}

func markPGXPoolConfigParsed(cfg *pgxpool.Config) {
	field := reflect.ValueOf(cfg).Elem().FieldByName("createdByParseConfig")
	if !field.IsValid() || field.Kind() != reflect.Bool {
		panic("pgxpool.Config.createdByParseConfig missing; pgx pool constructor contract changed")
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().SetBool(true)
}

func pgServerVersionSupported(versionNum int) bool {
	return versionNum >= pgServerVersionMin && versionNum < pgServerVersionMax
}

func readPGServerVersionNum(ctx context.Context, conn *pgxpool.Conn) (int, error) {
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

func classifyPGDialError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
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
