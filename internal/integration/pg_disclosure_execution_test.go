//go:build integration

// Package integration — real-MySQL + real-PG16 vertical proof for T9-B.
// input: shared MySQL fixture (real policy repository + real PG target resource), a disposable PostgreSQL 16 with a restricted role, and the production ExecutePGGovernedDisclosed entry
// output: the closed-loop acceptance — canonical policies written through, plus newPGDiscFixtureOn (the image-parametrized fixture for the T12-R1 version-compat matrix) the real T9-A repository drive the real executor's pre-wire preflight and post-cleanup transform
// pos: T9-B acceptance A/B (issue #117) — the end-to-end disclosure layer over two real stores; broader matrix coverage lives in the service package's real-PG tests
// note: if this file changes, update this header and module README.md.

package integration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	pgc "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const pgDiscRole = "ro_disc"

// pgDiscFixture starts a PG16 with a restricted role and two schemas holding
// same-named tables — the five-part isolation stage for real stores — plus a
// noaccess schema that exists but grants no USAGE to the role.
type pgDiscFixture struct {
	host string
	port int
	db   string
}

func newPGDiscFixture(t *testing.T, opts ...testcontainers.ContainerCustomizer) pgDiscFixture {
	t.Helper()
	return newPGDiscFixtureOn(t, "postgres:16-alpine", opts...)
}

// newPGDiscFixtureOn starts a disposable PostgreSQL on an explicit image —
// the version-compat matrix runs the same fixture body on PG14/15/17 while
// the default entry stays pinned to PG16.
func newPGDiscFixtureOn(t *testing.T, image string, opts ...testcontainers.ContainerCustomizer) pgDiscFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := pgc.Run(ctx, image,
		append([]testcontainers.ContainerCustomizer{
			pgc.WithDatabase("discdb"),
			pgc.WithUsername(pgMetaAdminUser),
			pgc.WithPassword(pgMetaAdminPassword),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(90 * time.Second)),
		}, opts...)...,
	)
	if err != nil {
		t.Fatalf("start postgres: %s", redactPGMeta(err.Error()))
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer stopCancel()
		_ = container.Terminate(stopCtx)
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("postgres host: %s", redactPGMeta(err.Error()))
	}
	mapped, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("postgres port: %s", redactPGMeta(err.Error()))
	}
	port, err := strconv.Atoi(mapped.Port())
	if err != nil {
		t.Fatalf("postgres port parse: %v", err)
	}

	admin := connectPG(t, host, port, "discdb", pgMetaAdminUser, pgMetaAdminPassword)
	var quoted string
	if err := admin.QueryRow(ctx, `SELECT quote_literal($1::text)`, pgStmtPassword).Scan(&quoted); err != nil {
		t.Fatalf("quote role password: %s", redactPGMeta(err.Error()))
	}
	for _, stmt := range []string{
		`CREATE ROLE ` + pgDiscRole + ` NOSUPERUSER NOCREATEDB NOCREATEROLE LOGIN PASSWORD ` + quoted,
		`GRANT CONNECT ON DATABASE discdb TO ` + pgDiscRole,
		`CREATE SCHEMA app`,
		`CREATE SCHEMA analytics`,
		// noaccess exists in pg_namespace but the restricted role has no
		// USAGE — the keyed-entry binding-gate verdict stage.
		`CREATE SCHEMA noaccess`,
		`GRANT USAGE ON SCHEMA app TO ` + pgDiscRole,
		`GRANT USAGE ON SCHEMA analytics TO ` + pgDiscRole,
		`CREATE TABLE app.orders (id bigint PRIMARY KEY, qty integer, note text)`,
		// 50-row generator for the cancel test's CPU-bound cartesian count —
		// no SRF in FROM, so nothing spills to pgsql_tmp.
		`CREATE TABLE app.smallvals (id integer PRIMARY KEY, tag text)`,
		`INSERT INTO app.smallvals SELECT g, 'tag-' || g::text FROM generate_series(1, 50) g`,
		`CREATE TABLE analytics.orders (id bigint PRIMARY KEY, total numeric)`,
		`CREATE TABLE app."trail " (id bigint PRIMARY KEY, "v " text)`,
		`INSERT INTO app.orders VALUES (1, 10, 'n1'), (2, 20, NULL)`,
		`INSERT INTO analytics.orders VALUES (1, 99.5)`,
		`INSERT INTO app."trail " VALUES (1, 'space')`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgDiscRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	admin.Close(ctx)
	return pgDiscFixture{host: host, port: port, db: "discdb"}
}

func (f pgDiscFixture) input(t *testing.T, stmt string, target uint64, reader service.QueryDisclosureReader) service.PGDisclosureInput {
	t.Helper()
	cfg, err := pgx.ParseConfig(pgKeywordDSN(f.host, f.port, f.db, pgDiscRole, pgStmtPassword, 5))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	return service.PGDisclosureInput{
		Execute: service.PGExecuteInput{
			ConnConfig:   cfg,
			Database:     f.db,
			PinnedSchema: "app",
			Statement:    stmt,
			Page:         pgsql.PageOptions{Page: 1, PageSize: 25, MaxRows: 100},
			Timeout:      20 * time.Second,
		},
		TargetResourceID: target,
		Policies:         reader,
	}
}

func insertPolicy(t *testing.T, repo *mysqlrepo.MySQLQueryDisclosureRepository, target uint64, db, schema, object, column string, mode model.ResultDisclosureMode) {
	t.Helper()
	if _, err := repo.Insert(context.Background(), model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: target, DatabaseName: db, SchemaName: schema,
		ObjectName: object, ColumnName: column, Mode: mode,
	}); err != nil {
		t.Fatalf("insert policy %s.%s.%s.%s: %v", db, schema, object, column, err)
	}
}

// A: minimal closed loop — the real PG16 executor's disclosed path resolves
// canonical policies through the real MySQL repository.
func TestPGDisclosureExecution_RealStores(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	repo := mysqlrepo.NewQueryDisclosureRepository(db)
	target := createPGInstance(t, db, "t9b-exec-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, "127.0.0.1", 1)

	// Covered: id raw + qty masked + note raw.
	insertPolicy(t, repo, target, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, repo, target, "discdb", "app", "orders", "qty", model.ResultDisclosureMaskedNoCopy)
	insertPolicy(t, repo, target, "discdb", "app", "orders", "note", model.ResultDisclosureRawCopyAllowed)

	res, err := service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id, qty, note FROM app.orders ORDER BY id`, target, repo))
	if err != nil {
		t.Fatalf("covered execute: %v", err)
	}
	if len(res.Columns) != 3 || len(res.Rows) != 2 {
		t.Fatalf("shape = %d cols, %d rows", len(res.Columns), len(res.Rows))
	}
	if *res.Rows[0][0] != "1" || *res.Rows[0][1] != "[MASKED]" || *res.Rows[0][2] != "n1" {
		t.Fatalf("row0 = %q/%q/%q", *res.Rows[0][0], *res.Rows[0][1], *res.Rows[0][2])
	}
	if res.Rows[1][2] != nil {
		t.Fatal("masked-adjacent NULL must stay NULL")
	}
	if res.Columns[1].DisplayMode != model.ResultDisclosureMaskedNoCopy || res.Columns[1].CopyAllowed {
		t.Fatalf("qty mode = %s", res.Columns[1].DisplayMode)
	}

	// Expression column over covered sources resolves by union — the masked
	// member wins.
	res, err = service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id, qty, note, id + qty FROM app.orders`, target, repo))
	if err != nil {
		t.Fatalf("covered expr execute: %v", err)
	}
	if res.Columns[3].DisplayMode != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("expr mode = %s (qty masked must win)", res.Columns[3].DisplayMode)
	}

	// Missing policy refuses — nothing reaches the wire.
	res, err = service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id FROM analytics.orders`, target, repo))
	if !errors.Is(err, service.ErrQueryDisclosureBlocked) || res != nil {
		t.Fatalf("uncovered analytics.orders: res=%v err=%v", res, err)
	}

	// Five-part isolation through the REAL store: the same object name under
	// schema analytics gets its own policy; a wrong-database row is ignored.
	insertPolicy(t, repo, target, "discdb", "analytics", "orders", "id", model.ResultDisclosureMaskedNoCopy)
	insertPolicy(t, repo, target, "otherdb", "analytics", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	res, err = service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id FROM analytics.orders`, target, repo))
	if err != nil {
		t.Fatalf("analytics covered: %v", err)
	}
	if res.Columns[0].DisplayMode != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("analytics.orders.id must be masked by its own scope, got %s", res.Columns[0].DisplayMode)
	}

	// Exact trailing-space identity through the real NO PAD collation:
	// policy for "trail " covers only "trail " — and vice versa.
	insertPolicy(t, repo, target, "discdb", "app", "trail ", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, repo, target, "discdb", "app", "trail ", "v ", model.ResultDisclosureRawCopyAllowed)
	res, err = service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id, "v " FROM app."trail "`, target, repo))
	if err != nil {
		t.Fatalf("trailing-space execute: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][1] != "space" {
		t.Fatalf("trailing-space rows = %v", res.Rows)
	}

	// A second target must not borrow this target's policies.
	other := createPGInstance(t, db, "t9b-exec-other-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, "127.0.0.1", 1)
	res, err = service.ExecutePGGovernedDisclosed(context.Background(),
		fix.input(t, `SELECT id, "v " FROM app."trail "`, other, repo))
	if !errors.Is(err, service.ErrQueryDisclosureBlocked) || res != nil {
		t.Fatalf("cross-target borrow: res=%v err=%v", res, err)
	}
}
