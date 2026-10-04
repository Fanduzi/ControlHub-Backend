//go:build integration

// Package service proves OpenPostgresPool against a disposable PostgreSQL server.
// input: context, errors, fmt, strconv, strings, sync, testing, time, jackc/pgx/v5, jackc/pgx/v5/pgxpool, testcontainers postgres
// output: TestPostgresPool_RealServer, TestPostgresPool_ConstructedVersionGate, TestPostgresPool_ReconnectVersionGate, TestPostgresPool_VersionReadFailureCleansUp
// pos: T3 acceptance — production factory dial, real server_version_num, native ExecParams, reconnect version gate, and failure cleanup
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	pgc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const pgPoolFactoryPassword = "pw-do-not-leak-t3"

type pgPoolLab struct {
	host string
	port int
	db   string
	user string
}

var (
	pgPoolLabOnce sync.Once
	pgPoolLabInst pgPoolLab
	pgPoolLabErr  error
)

func postgresPoolLab(t *testing.T) pgPoolLab {
	t.Helper()
	pgPoolLabOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := pgc.Run(ctx, "postgres:16-alpine",
			pgc.WithDatabase("pool_lab"),
			pgc.WithUsername("lab"),
			pgc.WithPassword(pgPoolFactoryPassword),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		)
		if err != nil {
			pgPoolLabErr = fmt.Errorf("start postgres container: %w", err)
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			pgPoolLabErr = fmt.Errorf("postgres host: %w", err)
			return
		}
		mapped, err := c.MappedPort(ctx, "5432/tcp")
		if err != nil {
			pgPoolLabErr = fmt.Errorf("postgres port: %w", err)
			return
		}
		port, err := strconv.Atoi(mapped.Port())
		if err != nil {
			pgPoolLabErr = fmt.Errorf("postgres port parse: %w", err)
			return
		}
		pgPoolLabInst = pgPoolLab{host: host, port: port, db: "pool_lab", user: "lab"}
	})
	if pgPoolLabErr != nil {
		t.Fatalf("postgres fixture: %v", pgPoolLabErr)
	}
	return pgPoolLabInst
}

func (l pgPoolLab) config(t *testing.T, appName string) *pgx.ConnConfig {
	t.Helper()
	dsn := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=%s",
		l.host, l.port, l.db, l.user, pgPoolFactoryPassword, appName,
	)
	cfg, err := validatePGDSNBinding(dsn, l.host, l.port, l.db)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return cfg
}

// TestPostgresPool_RealServer is the production-factory acceptance. The
// version it logs is read from the live server through the factory connection.
// It is not a constructed version number.
func TestPostgresPool_RealServer(t *testing.T) {
	lab := postgresPoolLab(t)
	cfg := lab.config(t, "ch-t3-real")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := OpenPostgresPool(ctx, cfg)
	if err != nil {
		t.Fatalf("OpenPostgresPool: %v", err)
	}
	if pool.Stat().MaxConns() != 1 {
		pool.Close()
		t.Fatalf("MaxConns = %d, want 1", pool.Stat().MaxConns())
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("acquire: %v", err)
	}
	dialed := conn.Conn().Config()
	if dialed.Host != cfg.Host || dialed.Port != cfg.Port || dialed.User != cfg.User || dialed.Database != cfg.Database {
		conn.Release()
		pool.Close()
		t.Fatalf("dialed identity host=%s port=%d user=%s db=%s", dialed.Host, dialed.Port, dialed.User, dialed.Database)
	}
	if dialed.ConnectTimeout != cfg.ConnectTimeout || dialed.TLSConfig != nil {
		conn.Release()
		pool.Close()
		t.Fatalf("dialed timeout=%s tls=%v, want %s and no TLS", dialed.ConnectTimeout, dialed.TLSConfig != nil, cfg.ConnectTimeout)
	}
	if dialed.RuntimeParams["application_name"] != "ch-t3-real" {
		conn.Release()
		pool.Close()
		t.Fatalf("dialed application_name=%q", dialed.RuntimeParams["application_name"])
	}
	version, user, db, app, ssl := readPGSession(t, ctx, conn)
	conn.Release()
	pool.Close()

	t.Logf("live server_version_num=%d (postgres:16-alpine via OpenPostgresPool)", version)
	if version < 160000 || version >= 170000 {
		t.Fatalf("live server_version_num=%d, want PG 16 (160000 <= n < 170000)", version)
	}
	if !pgServerVersionSupported(version) {
		t.Fatalf("live version %d rejected by the gate", version)
	}
	if user != lab.user || db != lab.db || app != "ch-t3-real" {
		t.Fatalf("session identity user=%s db=%s app=%s", user, db, app)
	}
	if ssl != "off" {
		t.Fatalf("ssl = %q, want off for sslmode=disable", ssl)
	}
	if cfg.ConnectTimeout != 5*time.Second || cfg.Host != lab.host || int(cfg.Port) != lab.port {
		t.Fatal("factory dial mutated the validated ConnConfig")
	}

	if _, err := pool.Acquire(ctx); err == nil {
		t.Fatal("Acquire succeeded after the caller closed the pool")
	}
	waitNoPGBackends(t, lab, "ch-t3-real")
}

// TestPostgresPool_ConstructedVersionGate drives the factory's accept/reject
// and cleanup path with constructed version numbers. The server underneath is
// the same live PG 16 container; these numbers are not that server's version.
func TestPostgresPool_ConstructedVersionGate(t *testing.T) {
	lab := postgresPoolLab(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cases := []struct {
		version int
		accept  bool
	}{
		{139999, false},
		{140000, true},
		{179999, true},
		{180000, false},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.version), func(t *testing.T) {
			app := fmt.Sprintf("ch-t3-ver-%d", tc.version)
			cfg := lab.config(t, app)
			pool, err := openPostgresPool(ctx, cfg, func(ctx context.Context, conn *pgx.Conn) (int, error) {
				// Prove the acquired connection can read the live server, then
				// return the constructed gate input. The live number is not the
				// decision input.
				live, liveErr := readPGServerVersionNum(ctx, conn)
				if liveErr != nil {
					return 0, liveErr
				}
				if live < 160000 || live >= 170000 {
					return 0, fmt.Errorf("fixture is not the expected live PG 16 (server_version_num=%d)", live)
				}
				return tc.version, nil
			})
			if tc.accept {
				if err != nil {
					t.Fatalf("constructed %d rejected: %v", tc.version, err)
				}
				if pool == nil {
					t.Fatal("accepted version returned a nil pool")
				}
				one := execParamsText(t, ctx, pool, "SELECT 1")
				if one != "1" {
					pool.Close()
					t.Fatalf("SELECT 1 = %q, want 1", one)
				}
				pool.Close()
			} else {
				if pool != nil {
					pool.Close()
					t.Fatal("rejected version returned a pool")
				}
				if !errors.Is(err, ErrPGVersionUnsupported) {
					t.Fatalf("error = %v, want ErrPGVersionUnsupported", err)
				}
				if PGPoolErrorCode(err) != "pg_version_unsupported" {
					t.Fatalf("code = %q", PGPoolErrorCode(err))
				}
				assertNoSecret(t, err)
			}
			waitNoPGBackends(t, lab, app)
		})
	}
}

// TestPostgresPool_ReconnectVersionGate checks the version gate on a new
// physical connection from a pool the factory already returned. 160015 and
// 180000 are constructed inputs. The live SHOW inside the reader only proves
// the disposable server is PG 16; it is not the gate decision.
func TestPostgresPool_ReconnectVersionGate(t *testing.T) {
	lab := postgresPoolLab(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	t.Run("reject_after_reset", func(t *testing.T) {
		testPostgresPoolReconnect(t, ctx, lab, "ch-t3-reset-reject", []int{160015, 180000}, false)
		waitNoPGBackends(t, lab, "ch-t3-reset-reject")
	})
	t.Run("accept_after_reset", func(t *testing.T) {
		testPostgresPoolReconnect(t, ctx, lab, "ch-t3-reset-accept", []int{160015, 160015}, true)
		waitNoPGBackends(t, lab, "ch-t3-reset-accept")
	})
}

func testPostgresPoolReconnect(t *testing.T, ctx context.Context, lab pgPoolLab, app string, versions []int, acceptSecond bool) {
	t.Helper()
	if len(versions) != 2 {
		t.Fatalf("versions = %v, want two constructed numbers", versions)
	}
	t.Logf("constructed gate inputs %v; live SHOW only checks the PG 16 fixture", versions)
	cfg := lab.config(t, app)
	var pids []int32
	call := 0
	pool, err := openPostgresPool(ctx, cfg, func(ctx context.Context, conn *pgx.Conn) (int, error) {
		live, liveErr := readPGServerVersionNum(ctx, conn)
		if liveErr != nil {
			return 0, liveErr
		}
		if live < 160000 || live >= 170000 {
			return 0, fmt.Errorf("fixture is not the expected live PG 16 (server_version_num=%d)", live)
		}
		var pid int32
		if scanErr := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); scanErr != nil {
			return 0, scanErr
		}
		if call >= len(versions) {
			return 0, fmt.Errorf("version gate ran %d times, want %d", call+1, len(versions))
		}
		version := versions[call]
		call++
		pids = append(pids, pid)
		return version, nil
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	if call != 1 || len(pids) != 1 {
		t.Fatalf("open consumed %d version checks, want the first physical connection only", call)
	}

	// Re-acquiring the idle connection must not dial a new backend.
	same, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("reacquire idle connection: %v", err)
	}
	same.Release()
	if call != 1 {
		t.Fatalf("idle reacquire ran the version gate (%d calls)", call)
	}

	pool.Reset()
	second, err := pool.Acquire(ctx)
	if acceptSecond {
		if err != nil {
			t.Fatalf("supported reconnect rejected: %v", err)
		}
		if second == nil {
			t.Fatal("supported reconnect returned a nil connection")
		}
		defer second.Release()
		if call != 2 || len(pids) != 2 {
			t.Fatalf("reconnect checks = %d, want the new physical connection", call)
		}
		if pids[0] == pids[1] || pids[0] == 0 {
			t.Fatalf("backend pids = %v, want two different live backends", pids)
		}
		if got := execParamsText(t, ctx, second, "SELECT 1"); got != "1" {
			t.Fatalf("SELECT 1 = %q", got)
		}
		return
	}
	if second != nil {
		second.Release()
		t.Fatal("unsupported reconnect delivered a connection")
	}
	if !errors.Is(err, ErrPGVersionUnsupported) {
		t.Fatalf("error = %v, want ErrPGVersionUnsupported", err)
	}
	if PGPoolErrorCode(err) != "pg_version_unsupported" {
		t.Fatalf("code = %q", PGPoolErrorCode(err))
	}
	assertNoSecret(t, err)
	if call != 2 || len(pids) != 2 {
		t.Fatalf("rejected reconnect checks = %d pids %v, want the new backend to be checked before delivery", call, pids)
	}
	if pids[0] == pids[1] || pids[0] == 0 {
		t.Fatalf("backend pids = %v, want the rejected connection to be a different backend", pids)
	}
	if pool.Stat().AcquiredConns() != 0 || pool.Stat().TotalConns() != 0 {
		t.Fatalf("pool still holds acquired=%d total=%d after rejecting the new connection", pool.Stat().AcquiredConns(), pool.Stat().TotalConns())
	}
}

func TestPostgresPool_VersionReadFailureCleansUp(t *testing.T) {
	lab := postgresPoolLab(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"read error", errors.New("injected version read failure " + pgPoolFactoryPassword), ErrPGVersionReadFailed},
		{"deadline", context.DeadlineExceeded, ErrQueryTimeout},
		{"canceled", context.Canceled, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := "ch-t3-read-" + strings.ReplaceAll(tc.name, " ", "-")
			cfg := lab.config(t, app)
			pool, err := openPostgresPool(ctx, cfg, func(context.Context, *pgx.Conn) (int, error) {
				return 0, tc.err
			})
			if pool != nil {
				pool.Close()
				t.Fatal("version read failure returned a pool")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			assertNoSecret(t, err)
			if strings.Contains(err.Error(), "injected") || strings.Contains(err.Error(), pgPoolFactoryPassword) {
				t.Fatalf("internal read error leaked: %q", err.Error())
			}
			waitNoPGBackends(t, lab, app)
		})
	}
}

func readPGSession(t *testing.T, ctx context.Context, conn *pgxpool.Conn) (version int, user, db, app, ssl string) {
	t.Helper()
	raw := execParamsText(t, ctx, conn, "SELECT current_setting('server_version_num') || '|' || current_user || '|' || current_database() || '|' || current_setting('application_name') || '|' || current_setting('ssl')")
	parts := strings.Split(raw, "|")
	if len(parts) != 5 {
		t.Fatalf("session row %q, want 5 fields", raw)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("version %q: %v", parts[0], err)
	}
	return version, parts[1], parts[2], parts[3], parts[4]
}

// execParamsText runs one native PgConn().ExecParams call, the path later
// execution must be able to use. poolOrConn accepts either a pool or an
// already acquired connection.
func execParamsText(t *testing.T, ctx context.Context, target any, sql string) string {
	t.Helper()
	var (
		conn    *pgxpool.Conn
		release bool
	)
	switch v := target.(type) {
	case *pgxpool.Pool:
		var err error
		conn, err = v.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		release = true
	case *pgxpool.Conn:
		conn = v
	default:
		t.Fatalf("unsupported exec target %T", target)
	}
	if release {
		defer conn.Release()
	}
	result := conn.Conn().PgConn().ExecParams(ctx, sql, nil, nil, nil, []int16{0}).Read()
	if result.Err != nil {
		t.Fatalf("ExecParams: %v", result.Err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		t.Fatalf("ExecParams rows = %d", len(result.Rows))
	}
	return string(result.Rows[0][0])
}

func waitNoPGBackends(t *testing.T, lab pgPoolLab, appName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observer, err := OpenPostgresPool(ctx, lab.config(t, "ch-t3-observer"))
	if err != nil {
		t.Fatalf("observer pool: %v", err)
	}
	defer observer.Close()

	deadline := time.Now().Add(3 * time.Second)
	var last int
	for {
		var n int64
		err := observer.QueryRow(ctx,
			"SELECT count(*) FROM pg_stat_activity WHERE application_name = $1", appName,
		).Scan(&n)
		if err != nil {
			t.Fatalf("count backends: %v", err)
		}
		last = int(n)
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("application_name %s still has %d backend(s) after pool close", appName, last)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
