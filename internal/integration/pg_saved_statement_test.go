//go:build integration

// Package integration proves PostgreSQL saved-statement context persistence and the G12 restore revalidation against real state.
// input: disposable PostgreSQL 16, disposable MySQL control plane, production repositories, TargetAccessResolver, livePGSchemaCatalog, pgx native binds
// output: TestPostgreSQLSavedStatementContext* — context round trip, restore failure matrix (disabled/deleted connection, rebound DSN, dropped schema, revoked USAGE, no cross-database fallback), and a component-level proof that compiled $k statements bind through the real driver
// pos: T11 vertical evidence (issue #119) — restore probes the LIVE server, so fixture mutations must run through the admin connection, never the restricted role; PG user/execute endpoints stay closed and are not exercised here beyond the fail-closed assertion
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	pgc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const (
	pgStmtPassword = "pw-do-not-log-t11"
	pgStmtRole     = "ro_stmt"
)

type pgStmtFixture struct {
	db     *sql.DB
	svc    *service.QuerySavedStatementService
	repo   *mysql.MySQLQuerySavedStatementRepository
	target uint64
	host   string
	port   int
	adminA *pgx.Conn // admin connection on db_a — fixture changes only
	adminB *pgx.Conn // admin connection on db_b — fixture changes only
}

// newPGStmtFixture starts a disposable PostgreSQL 16, creates the restricted
// role plus two databases carrying same-named schemas, registers the target
// and its (target, database) connection rows in the control plane, and wires
// the production saved-statement service exactly like cmd/server/main.go.
func newPGStmtFixture(t *testing.T) pgStmtFixture {
	t.Helper()
	db := setupTestDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := pgc.Run(ctx, "postgres:16-alpine",
		pgc.WithDatabase("postgres"),
		pgc.WithUsername(pgMetaAdminUser),
		pgc.WithPassword(pgMetaAdminPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
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

	admin := connectPG(t, host, port, "postgres", pgMetaAdminUser, pgMetaAdminPassword)
	var quoted string
	if err := admin.QueryRow(ctx, `SELECT quote_literal($1::text)`, pgStmtPassword).Scan(&quoted); err != nil {
		t.Fatalf("quote role password: %s", redactPGMeta(err.Error()))
	}
	execPG(t, admin, `CREATE ROLE `+pgStmtRole+` NOSUPERUSER NOCREATEDB NOCREATEROLE LOGIN PASSWORD `+quoted)
	execPG(t, admin, `CREATE DATABASE db_a`)
	execPG(t, admin, `CREATE DATABASE db_b`)
	execPG(t, admin, `GRANT CONNECT ON DATABASE db_a TO `+pgStmtRole)
	execPG(t, admin, `GRANT CONNECT ON DATABASE db_b TO `+pgStmtRole)

	adminA := connectPG(t, host, port, "db_a", pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE SCHEMA app`,
		`CREATE SCHEMA analytics`,
		`REVOKE ALL ON SCHEMA app FROM PUBLIC`,
		`REVOKE ALL ON SCHEMA analytics FROM PUBLIC`,
		`GRANT USAGE ON SCHEMA app TO ` + pgStmtRole,
		`GRANT USAGE ON SCHEMA analytics TO ` + pgStmtRole,
		`CREATE TABLE app.orders (id bigint PRIMARY KEY, status text NOT NULL)`,
		`CREATE TABLE analytics.orders (id bigint PRIMARY KEY)`,
		`INSERT INTO app.orders VALUES (1, 'paid'), (2, 'open')`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgStmtRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO ` + pgStmtRole,
	} {
		execPG(t, adminA, stmt)
	}
	adminB := connectPG(t, host, port, "db_b", pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE SCHEMA app`,
		`REVOKE ALL ON SCHEMA app FROM PUBLIC`,
		`GRANT USAGE ON SCHEMA app TO ` + pgStmtRole,
		`CREATE TABLE app.only_b (id bigint PRIMARY KEY)`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgStmtRole,
	} {
		execPG(t, adminB, stmt)
	}

	target := createPGInstance(t, db, "t11-pg-stmt-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, host, port)
	insertPGCredential(t, db, target, "db_a", "app", "PG_T11_DBA", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, target, "db_b", "app", "PG_T11_DBB", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, target, "db_mismatch", "app", "PG_T11_MIS", true, string(model.QueryEnvPolicyNonProdOnly))

	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T11_DBA", pgKeywordDSN(host, port, "db_a", pgStmtRole, pgStmtPassword, 5))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T11_DBB", pgKeywordDSN(host, port, "db_b", pgStmtRole, pgStmtPassword, 5))
	// The mismatch row claims db_mismatch but resolves to a DSN bound to db_b.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T11_MIS", pgKeywordDSN(host, port, "db_b", pgStmtRole, pgStmtPassword, 5))

	targetRepo := mysql.NewQueryTargetRepository(db)
	executionRepo := mysql.NewQueryExecutionRepository(db)
	stmtRepo := mysql.NewQuerySavedStatementRepository(db)
	svc := service.NewQuerySavedStatementService(
		stmtRepo,
		stmtRepo,
		targetRepo,
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
	).WithPGContext(executionRepo, service.NewTargetAccessResolver(targetRepo, executionRepo, service.NewEnvCredentialResolver()))
	return pgStmtFixture{db: db, svc: svc, repo: stmtRepo, target: target, host: host, port: port, adminA: adminA, adminB: adminB}
}

func (f pgStmtFixture) createStatement(t *testing.T, database, schema, statement string, params []model.QuerySavedStatementParameterDefinition) uint64 {
	t.Helper()
	created, err := f.svc.Create(context.Background(), service.AuthenticatedUser{ID: ownerDBA, Role: "editor"}, f.target, model.QuerySavedStatementCreateRequest{
		Name:       "t11 " + database + "/" + schema,
		Statement:  statement,
		Scope:      model.QuerySavedStatementPersonal,
		Database:   database,
		Schema:     schema,
		Parameters: params,
	})
	if err != nil {
		t.Fatalf("create %s/%s: %v", database, schema, err)
	}
	return created.ID
}

func (f pgStmtFixture) restore(t *testing.T, statementID uint64) (model.QuerySavedStatement, error) {
	t.Helper()
	return f.svc.RestoreContext(context.Background(), service.AuthenticatedUser{ID: ownerDBA, Role: "editor"}, f.target, statementID)
}

func TestPostgreSQLSavedStatementContextRoundTrip(t *testing.T) {
	f := newPGStmtFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, service.AuthenticatedUser{ID: ownerDBA, Role: "editor"}, f.target, model.QuerySavedStatementCreateRequest{
		Name:      "paid orders",
		Statement: `SELECT id FROM app.orders WHERE status = :status AND note = ':quoted'`,
		Scope:     model.QuerySavedStatementPersonal,
		Database:  "db_a",
		Schema:    "app",
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	loaded, err := f.repo.GetByID(ctx, f.target, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.DatabaseName != "db_a" || loaded.SchemaName != "app" {
		t.Fatalf("persisted context = %q/%q, want db_a/app", loaded.DatabaseName, loaded.SchemaName)
	}
	// The stored statement is the user's original text — never a compiled,
	// canonical, witness-injected, or paginated transport form.
	if want := `SELECT id FROM app.orders WHERE status = :status AND note = ':quoted'`; loaded.Statement != want {
		t.Fatalf("persisted statement = %q, want original %q", loaded.Statement, want)
	}
	if len(loaded.Parameters) != 1 || loaded.Parameters[0].Name != "status" || loaded.Parameters[0].Type != model.QuerySavedStatementParameterString {
		t.Fatalf("persisted parameters = %+v", loaded.Parameters)
	}

	// The parameter table stores declarations only — the schema carries
	// name/type/ordinal and has no column that could hold a bound value.
	var stmt, dbName, schemaName string
	if err := f.db.QueryRowContext(ctx,
		`SELECT statement, database_name, schema_name FROM query_saved_statements WHERE id = ?`, created.ID).
		Scan(&stmt, &dbName, &schemaName); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if strings.Contains(stmt, "$1") || strings.Contains(stmt, "paid") {
		t.Fatalf("persisted statement carries compiled markers or values: %q", stmt)
	}
	colRows, err := f.db.QueryContext(ctx, `SHOW COLUMNS FROM query_saved_statement_parameters`)
	if err != nil {
		t.Fatalf("show columns: %v", err)
	}
	var cols []string
	for colRows.Next() {
		var col, typ, null, key, def, extra2 any
		if err := colRows.Scan(&col, &typ, &null, &key, &def, &extra2); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols = append(cols, fmt.Sprint(col))
	}
	colRows.Close()
	for _, c := range cols {
		if strings.Contains(c, "value") {
			t.Fatalf("parameter table has a value-bearing column %q — bound values must never persist", c)
		}
	}
	var pName, pType string
	if err := f.db.QueryRowContext(ctx,
		`SELECT name, type FROM query_saved_statement_parameters WHERE statement_id = ?`, created.ID).
		Scan(&pName, &pType); err != nil {
		t.Fatalf("raw parameter read: %v", err)
	}
	if pName != "status" || pType != "string" {
		t.Fatalf("persisted parameter = %q/%q", pName, pType)
	}
	var extra int
	if err := f.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM query_saved_statement_parameters WHERE statement_id = ?`, created.ID).Scan(&extra); err != nil {
		t.Fatalf("parameter count: %v", err)
	}
	if extra != 1 {
		t.Fatalf("parameter rows = %d, want exactly the declaration", extra)
	}
}

func TestPostgreSQLSavedStatementRestoreRevalidatesLiveState(t *testing.T) {
	f := newPGStmtFixture(t)

	stmtID := f.createStatement(t, "db_a", "app", `SELECT id FROM app.orders WHERE status = :status`,
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})

	t.Run("happy path restores the saved statement", func(t *testing.T) {
		got, err := f.restore(t, stmtID)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if got.ID != stmtID || got.DatabaseName != "db_a" || got.SchemaName != "app" {
			t.Fatalf("restored = %+v", got)
		}
	})

	t.Run("connection disabled fails closed", func(t *testing.T) {
		mustExec(t, f.db, `UPDATE query_target_credentials SET enabled = 0 WHERE resource_id = ? AND database_name = 'db_a'`, f.target)
		t.Cleanup(func() {
			mustExec(t, f.db, `UPDATE query_target_credentials SET enabled = 1 WHERE resource_id = ? AND database_name = 'db_a'`, f.target)
		})
		_, err := f.restore(t, stmtID)
		if !errors.Is(err, service.ErrSchemaConnectionDisabled) {
			t.Fatalf("error = %v, want ErrSchemaConnectionDisabled", err)
		}
	})

	t.Run("connection row deleted fails closed", func(t *testing.T) {
		id := f.createStatement(t, "db_b", "app", `SELECT id FROM app.only_b`, nil)
		mustExec(t, f.db, `DELETE FROM query_target_credentials WHERE resource_id = ? AND database_name = 'db_b'`, f.target)
		t.Cleanup(func() {
			insertPGCredential(t, f.db, f.target, "db_b", "app", "PG_T11_DBB", true, string(model.QueryEnvPolicyNonProdOnly))
		})
		_, err := f.restore(t, id)
		// db_a/app still exists and is healthy — restore must never hop to it.
		if !errors.Is(err, service.ErrSchemaConnectionNotFound) {
			t.Fatalf("error = %v, want ErrSchemaConnectionNotFound", err)
		}
	})

	t.Run("DSN rebound to another database fails closed", func(t *testing.T) {
		id := f.createStatement(t, "db_mismatch", "app", `SELECT 1`, nil)
		_, err := f.restore(t, id)
		if !errors.Is(err, service.ErrSchemaBindingMismatch) {
			t.Fatalf("error = %v, want ErrSchemaBindingMismatch", err)
		}
	})

	t.Run("dropped schema fails closed without falling back to a same-named object", func(t *testing.T) {
		id := f.createStatement(t, "db_a", "analytics", `SELECT id FROM analytics.orders`, nil)
		execPG(t, f.adminA, `DROP SCHEMA analytics CASCADE`)
		t.Cleanup(func() {
			execPG(t, f.adminA, `CREATE SCHEMA analytics`)
			execPG(t, f.adminA, `REVOKE ALL ON SCHEMA analytics FROM PUBLIC`)
			execPG(t, f.adminA, `GRANT USAGE ON SCHEMA analytics TO `+pgStmtRole)
			execPG(t, f.adminA, `CREATE TABLE analytics.orders (id bigint PRIMARY KEY)`)
			execPG(t, f.adminA, `GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO `+pgStmtRole)
		})
		// app.orders still exists with the same table name — restore of the
		// pinned analytics context must fail, not silently re-pin to app.
		_, err := f.restore(t, id)
		if !errors.Is(err, service.ErrSchemaNotFound) {
			t.Fatalf("error = %v, want ErrSchemaNotFound", err)
		}
	})

	t.Run("revoked USAGE fails closed", func(t *testing.T) {
		execPG(t, f.adminA, `REVOKE USAGE ON SCHEMA app FROM `+pgStmtRole)
		t.Cleanup(func() {
			execPG(t, f.adminA, `GRANT USAGE ON SCHEMA app TO `+pgStmtRole)
		})
		_, err := f.restore(t, stmtID)
		if !errors.Is(err, service.ErrSchemaNotUsable) {
			t.Fatalf("error = %v, want ErrSchemaNotUsable", err)
		}
	})

	t.Run("default-schema pin resolves through the connection row", func(t *testing.T) {
		id := f.createStatement(t, "db_a", "", `SELECT id FROM app.orders`, nil)
		got, err := f.restore(t, id)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if got.SchemaName != "" {
			t.Fatalf("restored schema = %q, want empty pin (default_schema resolves at restore)", got.SchemaName)
		}
	})
}

// TestPostgreSQLTemplateCompilationNativeBinding is a component proof only:
// it executes the compiler's $k output through pgx's native parameter binding
// on a disposable server to show the markers are real driver binds, not text
// interpolation. It is NOT evidence of the governed execution chain — the
// production PG execute path stays closed until the full T7+ chain lands.
func TestPostgreSQLTemplateCompilationNativeBinding(t *testing.T) {
	f := newPGStmtFixture(t)

	compiled, err := service.NewTemplateStatementCompiler().CompilePG(service.TemplateStatementInput{
		Statement:   `SELECT id FROM app.orders WHERE status = :status`,
		Definitions: []service.TemplateParameterDefinition{{Name: "status", Type: service.TemplateParameterString}},
		Values:      map[string]any{"status": "paid"},
	})
	if err != nil {
		t.Fatalf("CompilePG: %v", err)
	}
	if strings.Contains(compiled.Statement, "paid") || !strings.Contains(compiled.Statement, "$1") {
		t.Fatalf("compiled statement = %q, want a $1 bind with no value text", compiled.Statement)
	}

	conn := connectPG(t, f.host, f.port, "db_a", pgStmtRole, pgStmtPassword)
	var ids []int64
	rows, err := conn.Query(context.Background(), compiled.Statement, compiled.Args...)
	if err != nil {
		t.Fatalf("native bind query: %s", redactPGMeta(err.Error()))
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %s", redactPGMeta(err.Error()))
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %s", redactPGMeta(err.Error()))
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("ids = %v, want [1] — the bound 'paid' value must match only the paid row", ids)
	}
}

func TestPostgreSQLSavedStatementExecuteStaysClosed(t *testing.T) {
	f := newPGStmtFixture(t)
	stmtID := f.createStatement(t, "db_a", "app", `SELECT id FROM app.orders`, nil)

	// The saved-statement execute route resolves through access.Resolve, which
	// still rejects postgresql at the engine gate (isExecutableEngine is
	// unchanged) — the attempt is recorded and rejected as not allowed.
	_, err := newTemplateExecutionIntegrationService(f.db).ExecuteSavedStatement(
		context.Background(), ownerDBA, f.target, stmtID, model.QuerySavedStatementExecuteRequest{})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("ExecuteSavedStatement error = %v, want ErrQueryNotAllowed", err)
	}
}
