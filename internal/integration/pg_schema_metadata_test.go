//go:build integration

// Package integration proves PostgreSQL schema metadata through the real HTTP handler.
// input: disposable PostgreSQL 16, production OpenPostgresPool, restricted role, httptest router, MySQL control plane
// output: TestPostgreSQLSchemaMetadataHTTP acceptance A-J, INCLUDE primary keys, domain nullability, index budget, and statement_timeout
// pos: T6 vertical proof from fixture through the production pool, metadata service, and schema HTTP routes; user SQL stays rejected
// note: if this file changes, update this header and module README.md.
// Issue #114 names GET /connections/{id}/schemas. Frozen G1/G2 and the existing
// router use GET /query-targets/{id}/schema/schemas?database=. This test follows
// the frozen route. {id} is the target resource. database selects the connection.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	pgc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const (
	pgMetaPassword      = "pw-do-not-log-t6"
	pgMetaAdminPassword = "pw-do-not-log-t6-admin"
	pgMetaRole          = "ro_meta"
	pgMetaAdminUser     = "fixture_admin"
)

func redactPGMeta(text string) string {
	text = strings.ReplaceAll(text, pgMetaPassword, "[redacted]")
	text = strings.ReplaceAll(text, pgMetaAdminPassword, "[redacted]")
	return text
}

func refusePGMetaSecret(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, pgMetaPassword) || strings.Contains(body, pgMetaAdminPassword) {
		t.Fatal("response leaked a credential")
	}
}

type pgHTTPError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func mustPGBody(t *testing.T, rec *httptest.ResponseRecorder, status int, dest any) {
	t.Helper()
	body := rec.Body.String()
	refusePGMetaSecret(t, body)
	if rec.Code != status {
		t.Fatalf("status=%d want %d body=%s", rec.Code, status, redactPGMeta(body))
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("json: %v body=%s", err, redactPGMeta(body))
	}
}

func mustPGError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) pgHTTPError {
	t.Helper()
	var out pgHTTPError
	mustPGBody(t, rec, status, &out)
	if out.Error != code {
		t.Fatalf("error=%s want %s message=%s", out.Error, code, redactPGMeta(out.Message))
	}
	return out
}

func getSchema(t *testing.T, h http.Handler, token string, id uint64, leaf string, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	path := fmt.Sprintf("/query-targets/%d/schema/%s?%s", id, leaf, q.Encode())
	return doBearer(t, h, http.MethodGet, path, token)
}

func pgKeywordDSN(host string, port int, database, user, password string, timeoutSec int) string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=%d application_name=ch-t6",
		host, port, database, user, password, timeoutSec,
	)
}

func execPG(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql); err != nil {
		t.Fatalf("fixture statement failed: %s", redactPGMeta(err.Error()))
	}
}

func connectPG(t *testing.T, host string, port int, database, user, password string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, pgKeywordDSN(host, port, database, user, password, 5))
	if err != nil {
		t.Fatalf("connect %s: %s", database, redactPGMeta(err.Error()))
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func pgOrdersExist(t *testing.T, host string, port int) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, pgKeywordDSN(host, port, "db_a", pgMetaAdminUser, pgMetaAdminPassword, 5))
	if err != nil {
		t.Fatalf("admin connect: %s", redactPGMeta(err.Error()))
	}
	defer conn.Close(ctx)
	var name *string
	if err := conn.QueryRow(ctx, `SELECT to_regclass('app.orders')::text`).Scan(&name); err != nil {
		t.Fatalf("regclass: %s", redactPGMeta(err.Error()))
	}
	return name != nil && *name != ""
}

func createPGInstance(t *testing.T, db *sql.DB, name string, envID uint64, host string, port int) uint64 {
	t.Helper()
	repo := mysql.NewResourceRepository(db)
	res, err := repo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "postgresql",
		Name:            name,
		DisplayName:     name,
		EnvironmentID:   envID,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create pg resource %s: %v", name, err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'postgresql', '16', ?, ?, 'primary', '{}')`, res.ID, host, port)
	return res.ID
}

func insertPGCredential(t *testing.T, db *sql.DB, resourceID uint64, database, schema, ref string, enabled bool, policy string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO query_target_credentials
		(resource_id, database_name, default_schema, engine, credential_ref, enabled, environment_policy)
		VALUES (?, ?, ?, 'postgresql', ?, ?, ?)`,
		resourceID, database, schema, ref, enabled, policy)
}

func columnNamed(t *testing.T, cols []model.ColumnDetail, name string) model.ColumnDetail {
	t.Helper()
	for _, col := range cols {
		if col.Name == name {
			return col
		}
	}
	t.Fatalf("missing column %s", name)
	return model.ColumnDetail{}
}

func indexNamed(t *testing.T, indexes []model.IndexDetail, name string) model.IndexDetail {
	t.Helper()
	for _, idx := range indexes {
		if idx.Name == name {
			return idx
		}
	}
	t.Fatalf("missing index %s", name)
	return model.IndexDetail{}
}

func nodeByID(nodes []model.RelationshipMapNode, id string) (model.RelationshipMapNode, bool) {
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return model.RelationshipMapNode{}, false
}

func schemaNames(items []model.SchemaSummary) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}

// TestPostgreSQLSchemaMetadataHTTP is the T6 vertical path: a restricted
// PostgreSQL role, the production pool factory, the metadata service, and the
// real schema handlers. The admin role only builds the fixture.
func TestPostgreSQLSchemaMetadataHTTP(t *testing.T) {
	mysqlSvc, mysqlID, db := setupSchemaSandboxTarget(t)
	if mysqlSvc == nil || mysqlID == 0 {
		t.Fatal("mysql sandbox target missing")
	}

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
	if err := admin.QueryRow(ctx, `SELECT quote_literal($1::text)`, pgMetaPassword).Scan(&quoted); err != nil {
		t.Fatalf("quote role password: %s", redactPGMeta(err.Error()))
	}
	execPG(t, admin, `CREATE ROLE `+pgMetaRole+` NOSUPERUSER NOCREATEDB NOCREATEROLE LOGIN PASSWORD `+quoted)
	execPG(t, admin, `CREATE DATABASE db_a`)
	execPG(t, admin, `CREATE DATABASE db_b`)
	execPG(t, admin, `GRANT CONNECT ON DATABASE db_a TO `+pgMetaRole)
	execPG(t, admin, `GRANT CONNECT ON DATABASE db_b TO `+pgMetaRole)

	dbA := connectPG(t, host, port, "db_a", pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE SCHEMA app`,
		`CREATE SCHEMA analytics`,
		`CREATE SCHEMA secret`,
		`CREATE SCHEMA "CaseSchema"`,
		`CREATE SCHEMA "Odd Name"`,
		`REVOKE ALL ON SCHEMA app FROM PUBLIC`,
		`REVOKE ALL ON SCHEMA analytics FROM PUBLIC`,
		`REVOKE ALL ON SCHEMA secret FROM PUBLIC`,
		`REVOKE ALL ON SCHEMA "CaseSchema" FROM PUBLIC`,
		`REVOKE ALL ON SCHEMA "Odd Name" FROM PUBLIC`,
		`GRANT USAGE ON SCHEMA app TO ` + pgMetaRole,
		`GRANT USAGE ON SCHEMA analytics TO ` + pgMetaRole,
		`GRANT USAGE ON SCHEMA "CaseSchema" TO ` + pgMetaRole,
		`GRANT USAGE ON SCHEMA "Odd Name" TO ` + pgMetaRole,
		`GRANT USAGE ON SCHEMA public TO ` + pgMetaRole,
		`CREATE TABLE app.orders (
			id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			note text,
			qty integer NOT NULL
		)`,
		`CREATE TABLE analytics.orders (
			id bigint PRIMARY KEY,
			region text NOT NULL
		)`,
		`CREATE TABLE app.events (
			id integer NOT NULL,
			day date NOT NULL,
			PRIMARY KEY (id, day)
		) PARTITION BY RANGE (day)`,
		`CREATE TABLE app.events_2026 PARTITION OF app.events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')`,
		`CREATE VIEW app.orders_v AS SELECT id, qty FROM app.orders`,
		`CREATE TABLE analytics.ref_pair (
			a integer NOT NULL,
			b integer NOT NULL,
			PRIMARY KEY (a, b)
		)`,
		`CREATE TABLE app.child (
			a integer NOT NULL,
			b integer NOT NULL
		)`,
		`CREATE INDEX idx_child_ba ON app.child (b, a)`,
		`ALTER TABLE app.child ADD CONSTRAINT fk_child_pair FOREIGN KEY (a, b) REFERENCES analytics.ref_pair (a, b) ON UPDATE CASCADE ON DELETE RESTRICT`,
		`CREATE TABLE public.decoy (id integer PRIMARY KEY)`,
		`CREATE TABLE "CaseSchema"."Odd Table" (id integer PRIMARY KEY)`,
		`CREATE TABLE "Odd Name".kept (id integer PRIMARY KEY)`,
		`CREATE TABLE secret.hidden (id integer PRIMARY KEY)`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgMetaRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO ` + pgMetaRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "CaseSchema" TO ` + pgMetaRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "Odd Name" TO ` + pgMetaRole,
		`GRANT SELECT ON public.decoy TO ` + pgMetaRole,
	} {
		execPG(t, dbA, stmt)
	}
	dbB := connectPG(t, host, port, "db_b", pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE SCHEMA app`,
		`REVOKE ALL ON SCHEMA app FROM PUBLIC`,
		`GRANT USAGE ON SCHEMA app TO ` + pgMetaRole,
		`CREATE TABLE app.only_b (id integer PRIMARY KEY)`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgMetaRole,
	} {
		execPG(t, dbB, stmt)
	}

	liveID := createPGInstance(t, db, "t6-pg-live", envStaging, host, port)
	prodID := createPGInstance(t, db, "t6-pg-prod", envProd, host, port)
	deadID := createPGInstance(t, db, "t6-pg-dead", envStaging, "127.0.0.1", 1)

	insertPGCredential(t, db, liveID, "db_a", "app", "PG_T6_DBA", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, liveID, "db_b", "app", "PG_T6_DBB", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, liveID, "db_off", "app", "PG_T6_OFF", false, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, liveID, "db_mismatch", "app", "PG_T6_MIS", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, prodID, "db_a", "app", "PG_T6_PROD", true, string(model.QueryEnvPolicyNonProdOnly))
	insertPGCredential(t, db, deadID, "db_a", "app", "PG_T6_DEAD", true, string(model.QueryEnvPolicyNonProdOnly))

	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T6_DBA", pgKeywordDSN(host, port, "db_a", pgMetaRole, pgMetaPassword, 5))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T6_DBB", pgKeywordDSN(host, port, "db_b", pgMetaRole, pgMetaPassword, 5))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T6_MIS", pgKeywordDSN(host, port, "db_a", pgMetaRole, pgMetaPassword, 5))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T6_DEAD", pgKeywordDSN("127.0.0.1", 1, "db_a", pgMetaRole, pgMetaPassword, 2))

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()
	disclosureRepo := mysql.NewQueryDisclosureRepository(db)
	schemaSvc := service.NewQuerySchemaService(
		service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver),
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, wallClock{}),
		queryExecutionRepo,
		wallClock{},
	)
	execSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		credentialResolver,
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(disclosureRepo, disclosureRepo, service.NewMySQLSchemaInspector(), queryTargetRepo),
	)
	handler := api.NewRouter(api.Dependencies{
		AuthService:            service.NewAuthService(mysql.NewUserRepository(db), "t6-schema-http-secret"),
		QuerySchemaService:     schemaSvc,
		QueryExecutionService:  execSvc,
		QueryCredentialService: service.NewQueryCredentialService(queryTargetRepo, queryExecutionRepo, credentialResolver),
		QueryExecutionAuth:     api.QueryExecutionAuthConfig{Clock: time.Now},
	})
	email := "t6-schema-editor@example.com"
	insertAuthzTestUser(t, db, email, "editor")
	token := mustLogin(t, handler, email, "secret123")

	t.Run("A_databases_stay_on_the_selected_connection", func(t *testing.T) {
		var schemas model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}}), http.StatusOK, &schemas)
		if schemas.Database != "db_a" || schemas.TargetResourceID != int64(liveID) {
			t.Fatalf("schema list identity = %+v", schemas)
		}
		if contains(schemaNames(schemas.Items), "secret") {
			t.Fatal("restricted role listed a schema without USAGE")
		}

		var objects model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}}), http.StatusOK, &objects)
		if objects.Schema != "app" || objects.Database != "db_a" {
			t.Fatalf("omitted schema pinned %+v", objects)
		}
		names := objNames(objects.Items)
		if !contains(names, "orders") || contains(names, "only_b") || contains(names, "decoy") {
			t.Fatalf("db_a objects = %v", names)
		}

		var other model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_b"}}), http.StatusOK, &other)
		otherNames := objNames(other.Items)
		if !contains(otherNames, "only_b") || contains(otherNames, "orders") || other.Database != "db_b" {
			t.Fatalf("db_b objects = %+v", other)
		}

		var databases model.DatabaseListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "databases", url.Values{"database": {"db_a"}}), http.StatusOK, &databases)
		if databases.DefaultDatabase == nil || *databases.DefaultDatabase != "db_a" || len(databases.Items) != 1 || databases.Items[0].Name != "db_a" || !databases.Items[0].IsDefault {
			t.Fatalf("databases = %+v", databases)
		}
		var page2 model.DatabaseListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "databases", url.Values{"database": {"db_a"}, "page": {"2"}}), http.StatusOK, &page2)
		if len(page2.Items) != 0 || page2.PageInfo.TotalItems != 1 {
			t.Fatalf("page 2 = %+v", page2)
		}
	})

	t.Run("A_disable_one_database_keeps_the_other", func(t *testing.T) {
		mustExec(t, db, `UPDATE query_target_credentials SET enabled = 0 WHERE resource_id = ? AND database_name = 'db_a'`, liveID)
		t.Cleanup(func() {
			mustExec(t, db, `UPDATE query_target_credentials SET enabled = 1 WHERE resource_id = ? AND database_name = 'db_a'`, liveID)
		})
		mustPGError(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}}), http.StatusForbidden, "query_connection_disabled")
		var other model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_b"}}), http.StatusOK, &other)
		if other.Database != "db_b" {
			t.Fatalf("db_b identity = %+v", other)
		}
	})

	t.Run("C_usage_filter_and_explicit_unusable_schema", func(t *testing.T) {
		var schemas model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}}), http.StatusOK, &schemas)
		names := schemaNames(schemas.Items)
		for _, want := range []string{"app", "analytics", "CaseSchema", "Odd Name", "public"} {
			if !contains(names, want) {
				t.Fatalf("schema list missing %s: %v", want, names)
			}
		}
		for _, banned := range []string{"secret", "pg_catalog", "information_schema", "pg_toast"} {
			if contains(names, banned) {
				t.Fatalf("schema list included %s: %v", banned, names)
			}
		}
		secret := url.Values{"database": {"db_a"}, "schema": {"secret"}}
		mustPGError(t, getSchema(t, handler, token, liveID, "objects", secret), http.StatusForbidden, "query_schema_not_usable")
		mustPGError(t, getSchema(t, handler, token, liveID, "object-details", url.Values{"database": {"db_a"}, "schema": {"secret"}, "name": {"hidden"}, "kind": {"table"}}), http.StatusForbidden, "query_schema_not_usable")
		mustPGError(t, getSchema(t, handler, token, liveID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"secret"}, "name": {"hidden"}}), http.StatusForbidden, "query_schema_not_usable")
		mustPGError(t, getSchema(t, handler, token, liveID, "relationship-map", url.Values{"database": {"db_a"}, "schema": {"secret"}, "name": {"hidden"}}), http.StatusForbidden, "query_schema_not_usable")
	})

	t.Run("B_same_name_objects_do_not_share_metadata", func(t *testing.T) {
		app := objectDetail(t, handler, token, liveID, "app", "orders", "table")
		analytics := objectDetail(t, handler, token, liveID, "analytics", "orders", "table")
		if app.Schema != "app" || analytics.Schema != "analytics" {
			t.Fatalf("schemas = %s %s", app.Schema, analytics.Schema)
		}
		if !contains(columnNames(app.Columns), "note") || contains(columnNames(app.Columns), "region") {
			t.Fatalf("app.orders columns = %+v", app.Columns)
		}
		if !contains(columnNames(analytics.Columns), "region") || contains(columnNames(analytics.Columns), "note") {
			t.Fatalf("analytics.orders columns = %+v", analytics.Columns)
		}
		if len(app.ForeignKeys) != 0 || len(analytics.ForeignKeys) != 0 {
			t.Fatalf("orders foreign keys leaked: app=%+v analytics=%+v", app.ForeignKeys, analytics.ForeignKeys)
		}
	})

	t.Run("D_types_partition_view_and_composite_keys", func(t *testing.T) {
		orders := objectDetail(t, handler, token, liveID, "app", "orders", "table")
		id := columnNamed(t, orders.Columns, "id")
		note := columnNamed(t, orders.Columns, "note")
		qty := columnNamed(t, orders.Columns, "qty")
		if id.DatabaseType != "bigint" || id.Nullable || !id.PrimaryKey || !id.AutoIncrement || id.OrdinalPosition != 1 {
			t.Fatalf("id = %+v", id)
		}
		if note.DatabaseType != "text" || !note.Nullable || note.OrdinalPosition != 2 {
			t.Fatalf("note = %+v", note)
		}
		if qty.DatabaseType != "integer" || qty.Nullable || qty.OrdinalPosition != 3 {
			t.Fatalf("qty = %+v", qty)
		}

		events := objectDetail(t, handler, token, liveID, "app", "events", "table")
		if events.Kind != model.ObjectKindTable {
			t.Fatalf("events kind = %s", events.Kind)
		}
		eventID := columnNamed(t, events.Columns, "id")
		day := columnNamed(t, events.Columns, "day")
		if eventID.DatabaseType != "integer" || !eventID.PrimaryKey || day.DatabaseType != "date" || !day.PrimaryKey || day.OrdinalPosition != 2 {
			t.Fatalf("events columns = %+v", events.Columns)
		}
		var tables model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"app"}, "kind": {"table"}}), http.StatusOK, &tables)
		tableNames := objNames(tables.Items)
		if !contains(tableNames, "events") || !contains(tableNames, "events_2026") || contains(tableNames, "orders_v") {
			t.Fatalf("tables = %v", tableNames)
		}
		childPartition := objectDetail(t, handler, token, liveID, "app", "events_2026", "table")
		if childPartition.Kind != model.ObjectKindTable || columnNamed(t, childPartition.Columns, "day").DatabaseType != "date" {
			t.Fatalf("partition child = %+v", childPartition)
		}

		var views model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"app"}, "kind": {"view"}}), http.StatusOK, &views)
		if objNames(views.Items) == nil || len(views.Items) != 1 || views.Items[0].Name != "orders_v" || views.Items[0].Kind != model.ObjectKindView {
			t.Fatalf("views = %+v", views.Items)
		}
		view := objectDetail(t, handler, token, liveID, "app", "orders_v", "view")
		if view.Kind != model.ObjectKindView || contains(columnNames(view.Columns), "note") {
			t.Fatalf("view = %+v", view)
		}

		child := objectDetail(t, handler, token, liveID, "app", "child", "table")
		if len(child.Indexes) != 1 {
			t.Fatalf("child indexes = %+v", child.Indexes)
		}
		idx := indexNamed(t, child.Indexes, "idx_child_ba")
		if idx.Primary || idx.Unique || !slices.Equal(idx.Columns, []string{"b", "a"}) {
			t.Fatalf("idx_child_ba = %+v", idx)
		}
		if len(child.ForeignKeys) != 1 {
			t.Fatalf("child fks = %+v", child.ForeignKeys)
		}
		fk := child.ForeignKeys[0]
		if fk.Name != "fk_child_pair" || fk.ReferencedDatabase != "db_a" || fk.ReferencedSchema != "analytics" || fk.ReferencedObject != "ref_pair" || fk.OnUpdate != "CASCADE" || fk.OnDelete != "RESTRICT" || !slices.Equal(fk.Columns, []string{"a", "b"}) || !slices.Equal(fk.ReferencedColumns, []string{"a", "b"}) {
			t.Fatalf("fk = %+v", fk)
		}

		outbound := relationship(t, handler, token, liveID, "app", "child")
		if outbound.Root.Schema != "app" || outbound.Root.Name != "child" || len(outbound.Edges) != 1 || outbound.Edges[0].Direction != model.RelationshipMapDirectionOutbound {
			t.Fatalf("outbound = %+v", outbound)
		}
		target, ok := nodeByID(outbound.Nodes, outbound.Edges[0].TargetID)
		if !ok || target.Schema != "analytics" || target.Name != "ref_pair" || !slices.Equal(outbound.Edges[0].Columns, []string{"a", "b"}) || !slices.Equal(outbound.Edges[0].ReferencedColumns, []string{"a", "b"}) {
			t.Fatalf("outbound edge = %+v nodes=%+v", outbound.Edges[0], outbound.Nodes)
		}
		inbound := relationship(t, handler, token, liveID, "analytics", "ref_pair")
		if len(inbound.Edges) != 1 || inbound.Edges[0].Direction != model.RelationshipMapDirectionInbound {
			t.Fatalf("inbound = %+v", inbound)
		}
		source, ok := nodeByID(inbound.Nodes, inbound.Edges[0].SourceID)
		if !ok || source.Schema != "app" || source.Name != "child" {
			t.Fatalf("inbound source = %+v", inbound)
		}
		mustPGError(t, getSchema(t, handler, token, liveID, "relationship-map", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"orders_v"}}), http.StatusConflict, "relationship_map_not_supported")
	})

	t.Run("E_case_quoting_and_input_cannot_change_sql", func(t *testing.T) {
		mustPGError(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"caseschema"}}), http.StatusNotFound, "schema_not_found")
		odd := objectDetail(t, handler, token, liveID, "CaseSchema", "Odd Table", "table")
		if odd.Schema != "CaseSchema" || odd.Name != "Odd Table" || columnNamed(t, odd.Columns, "id").DatabaseType != "integer" {
			t.Fatalf("quoted object = %+v", odd)
		}
		var searched model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}, "q": {"caseschema"}}), http.StatusOK, &searched)
		if schemaNames(searched.Items) == nil || len(searched.Items) != 1 || searched.Items[0].Name != "CaseSchema" {
			t.Fatalf("case-insensitive search = %+v", searched.Items)
		}
		var kept model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"Odd Name"}}), http.StatusOK, &kept)
		if !contains(objNames(kept.Items), "kept") {
			t.Fatalf("Odd Name objects = %+v", kept.Items)
		}

		payload := "app'; DROP TABLE app.orders; --"
		mustPGError(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {payload}}), http.StatusNotFound, "schema_not_found")
		var after model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"app"}}), http.StatusOK, &after)
		if !contains(objNames(after.Items), "orders") || !pgOrdersExist(t, host, port) {
			t.Fatal("schema input changed the stored orders table")
		}
		var filtered model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}, "q": {`%' OR nspname = 'pg_catalog`}}), http.StatusOK, &filtered)
		if len(filtered.Items) != 0 || strings.Contains(redactPGMeta(fmt.Sprint(schemaNames(filtered.Items))), "pg_catalog") {
			t.Fatalf("search changed the catalog filter: %+v", filtered.Items)
		}
		mustPGError(t, getSchema(t, handler, token, liveID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"orders'; DROP TABLE app.orders; --"}}), http.StatusConflict, "query_object_definition_unsupported")
		if !pgOrdersExist(t, host, port) {
			t.Fatal("definition input changed the stored orders table")
		}
	})

	t.Run("F_missing_connection_schema_and_object_do_not_fall_back", func(t *testing.T) {
		mustPGError(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_none"}}), http.StatusNotFound, "query_connection_not_found")
		mustPGError(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"no_such"}}), http.StatusNotFound, "schema_not_found")
		mustPGError(t, getSchema(t, handler, token, liveID, "object-details", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"only_b"}, "kind": {"table"}}), http.StatusNotFound, "query_object_not_found")
		var pinned model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}}), http.StatusOK, &pinned)
		if pinned.Schema != "app" || contains(objNames(pinned.Items), "decoy") {
			t.Fatalf("default pin = %+v", pinned)
		}
		var public model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_a"}, "schema": {"public"}}), http.StatusOK, &public)
		if contains(objNames(public.Items), "orders") || !contains(objNames(public.Items), "decoy") || public.Schema != "public" {
			t.Fatalf("public objects = %+v", public)
		}
	})

	t.Run("G_access_failures_reuse_existing_rules", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/query-targets/%d/schema/schemas?database=db_a", liveID), nil))
		mustPGError(t, rec, http.StatusUnauthorized, "unauthorized")

		mustPGError(t, getSchema(t, handler, token, prodID, "schemas", url.Values{"database": {"db_a"}}), http.StatusForbidden, "schema_not_allowed")
		mismatch := mustPGError(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_mismatch"}}), http.StatusForbidden, "dsn_binding_mismatch")
		if strings.Contains(mismatch.Message, "password") || strings.Contains(mismatch.Message, "dbname=") {
			t.Fatal("binding error included connection material")
		}
		mustPGError(t, getSchema(t, handler, token, liveID, "objects", url.Values{"database": {"db_off"}}), http.StatusForbidden, "query_connection_disabled")
	})

	t.Run("H_definition_unsupported_is_distinct_from_connect_and_schema_errors", func(t *testing.T) {
		mustPGError(t, getSchema(t, handler, token, liveID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"orders"}}), http.StatusConflict, "query_object_definition_unsupported")
		mustPGError(t, getSchema(t, handler, token, liveID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"does_not_exist"}}), http.StatusConflict, "query_object_definition_unsupported")
		var schemas model.SchemaListResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "schemas", url.Values{"database": {"db_a"}}), http.StatusOK, &schemas)
		if !contains(schemaNames(schemas.Items), "app") {
			t.Fatal("unsupported definition blocked schema listing")
		}
		mustPGError(t, getSchema(t, handler, token, liveID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"no_such"}, "name": {"orders"}}), http.StatusNotFound, "schema_not_found")
		dead := getSchema(t, handler, token, deadID, "table-definition", url.Values{"database": {"db_a"}, "schema": {"app"}, "name": {"orders"}})
		refusePGMetaSecret(t, dead.Body.String())
		if dead.Code == http.StatusConflict || dead.Code == http.StatusInternalServerError {
			t.Fatalf("down server disguised as unsupported or internal: %s", redactPGMeta(dead.Body.String()))
		}
		mustPGError(t, dead, http.StatusBadGateway, "schema_backend_error")
	})

	t.Run("I_mysql_schema_behavior_stays", func(t *testing.T) {
		mustPGError(t, getSchema(t, handler, token, mysqlID, "schemas", url.Values{"database": {"query_e2e_aux"}}), http.StatusBadRequest, "schema_validation_failed")
		mustPGError(t, getSchema(t, handler, token, mysqlID, "objects", url.Values{"database": {"query_e2e_aux"}, "schema": {"public"}}), http.StatusBadRequest, "schema_validation_failed")
		var objects model.ObjectListResponse
		mustPGBody(t, getSchema(t, handler, token, mysqlID, "objects", url.Values{"database": {"query_e2e_aux"}}), http.StatusOK, &objects)
		if objects.Schema != "" || !contains(objNames(objects.Items), "schema_parent") {
			t.Fatalf("mysql objects = %+v", objects)
		}
	})

	t.Run("J_metadata_does_not_open_user_sql", func(t *testing.T) {
		rejected := doBearerWithBody(t, handler, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", liveID), token, `{"statement":"select 1"}`)
		errBody := mustPGError(t, rejected, http.StatusForbidden, "query_not_allowed")
		if !strings.Contains(errBody.Message, "engine is not supported for read-only execution") {
			t.Fatalf("execute message = %s", redactPGMeta(errBody.Message))
		}
		closed := doBearerWithBody(t, handler, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", liveID), token, `{"statement":"select 1","database":"db_a","schema":"app"}`)
		closedBody := mustPGError(t, closed, http.StatusBadRequest, "validation_failed")
		if !strings.Contains(closedBody.Message, "not accepted") {
			t.Fatalf("closed message = %s", redactPGMeta(closedBody.Message))
		}
		credRec := doBearer(t, handler, http.MethodGet, fmt.Sprintf("/query-targets/%d/credential?database=db_a", liveID), token)
		var cred model.QueryCredentialStatusResponse
		mustPGBody(t, credRec, http.StatusOK, &cred)
		if cred.ExecutionEligible {
			t.Fatal("postgresql credential became execution eligible")
		}
	})

	t.Run("primary_key_include_and_domain_nullability", func(t *testing.T) {
		for _, stmt := range []string{
			`CREATE TABLE app.pk_include (
				id bigint,
				payload text,
				PRIMARY KEY (id) INCLUDE (payload)
			)`,
			`CREATE DOMAIN app.plain_text AS text`,
			`CREATE DOMAIN app.nn_text AS text NOT NULL`,
			`CREATE TABLE app.domain_probe (
				plain text,
				col_nn text NOT NULL,
				dom_plain app.plain_text,
				dom_nn app.nn_text
			)`,
			`GRANT SELECT ON app.pk_include, app.domain_probe TO ` + pgMetaRole,
		} {
			execPG(t, dbA, stmt)
		}
		included := objectDetail(t, handler, token, liveID, "app", "pk_include", "table")
		id := columnNamed(t, included.Columns, "id")
		payload := columnNamed(t, included.Columns, "payload")
		if !id.PrimaryKey || payload.PrimaryKey || payload.DatabaseType != "text" {
			t.Fatalf("pk include columns = %+v", included.Columns)
		}
		events := objectDetail(t, handler, token, liveID, "app", "events", "table")
		eventID := columnNamed(t, events.Columns, "id")
		day := columnNamed(t, events.Columns, "day")
		if !eventID.PrimaryKey || !day.PrimaryKey || eventID.OrdinalPosition != 1 || day.OrdinalPosition != 2 {
			t.Fatalf("composite primary key = %+v", events.Columns)
		}

		probe := objectDetail(t, handler, token, liveID, "app", "domain_probe", "table")
		plain := columnNamed(t, probe.Columns, "plain")
		colNN := columnNamed(t, probe.Columns, "col_nn")
		domPlain := columnNamed(t, probe.Columns, "dom_plain")
		domNN := columnNamed(t, probe.Columns, "dom_nn")
		plainType := pgFormatType(t, dbA, "app", "domain_probe", "plain")
		domPlainType := pgFormatType(t, dbA, "app", "domain_probe", "dom_plain")
		domNNType := pgFormatType(t, dbA, "app", "domain_probe", "dom_nn")
		if !plain.Nullable || plain.DatabaseType != plainType || colNN.Nullable || colNN.DatabaseType != "text" {
			t.Fatalf("column nullability = %+v", probe.Columns)
		}
		if !domPlain.Nullable || domPlain.DatabaseType != domPlainType || domPlainType == plainType {
			t.Fatalf("nullable domain = %+v type %s", domPlain, domPlainType)
		}
		if domNN.Nullable || domNN.DatabaseType != domNNType || domNNType == plainType {
			t.Fatalf("not-null domain = %+v type %s", domNN, domNNType)
		}
	})

	t.Run("index_column_budget_stops_without_empty_indexes", func(t *testing.T) {
		for _, stmt := range []string{
			`CREATE TABLE app.idx_exact (id integer NOT NULL)`,
			`CREATE TABLE app.idx_over (id integer NOT NULL)`,
			`CREATE TABLE app.idx_split (id integer NOT NULL, c2 integer NOT NULL, c3 integer NOT NULL)`,
			`DO $$
BEGIN
  FOR i IN 0..255 LOOP
    EXECUTE format('CREATE INDEX idx_exact_%s ON app.idx_exact (id)', lpad(i::text, 4, '0'));
    EXECUTE format('CREATE INDEX idx_over_%s ON app.idx_over (id)', lpad(i::text, 4, '0'));
  END LOOP;
  EXECUTE 'CREATE INDEX idx_over_0256 ON app.idx_over (id)';
  FOR i IN 0..254 LOOP
    EXECUTE format('CREATE INDEX idx_split_%s ON app.idx_split (id)', lpad(i::text, 4, '0'));
  END LOOP;
  EXECUTE 'CREATE INDEX idx_split_zzzz ON app.idx_split (id, c2, c3)';
END $$`,
			`GRANT SELECT ON app.idx_exact, app.idx_over, app.idx_split TO ` + pgMetaRole,
		} {
			execPG(t, dbA, stmt)
		}
		exact := objectDetail(t, handler, token, liveID, "app", "idx_exact", "table")
		if exact.Truncated.Indexes || len(exact.Indexes) != 256 {
			t.Fatalf("exact indexes = %d truncated=%v", len(exact.Indexes), exact.Truncated.Indexes)
		}
		assertIndexColumnsPresent(t, exact.Indexes)
		over := objectDetail(t, handler, token, liveID, "app", "idx_over", "table")
		if !over.Truncated.Indexes || len(over.Indexes) != 256 || indexNames(over.Indexes)["idx_over_0256"] {
			t.Fatalf("over indexes = %d truncated=%v has 0256=%v", len(over.Indexes), over.Truncated.Indexes, indexNames(over.Indexes)["idx_over_0256"])
		}
		assertIndexColumnsPresent(t, over.Indexes)
		split := objectDetail(t, handler, token, liveID, "app", "idx_split", "table")
		splitIdx := indexNamed(t, split.Indexes, "idx_split_zzzz")
		if !split.Truncated.Indexes || len(split.Indexes) != 256 || !slices.Equal(splitIdx.Columns, []string{"id"}) {
			t.Fatalf("split indexes=%d truncated=%v zzzz=%+v", len(split.Indexes), split.Truncated.Indexes, splitIdx)
		}
		assertIndexColumnsPresent(t, split.Indexes)
	})

	t.Run("statement_timeout_is_schema_timeout", func(t *testing.T) {
		lockCtx := context.Background()
		tx, err := dbA.Begin(lockCtx)
		if err != nil {
			t.Fatalf("begin lock: %s", redactPGMeta(err.Error()))
		}
		defer func() { _ = tx.Rollback(lockCtx) }()
		// pg_namespace is read while a new session starts, before statement_timeout
		// is set. pg_index is first touched by the column query, after that GUC.
		if _, err := tx.Exec(lockCtx, `LOCK TABLE pg_catalog.pg_index IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatalf("lock pg_index: %s", redactPGMeta(err.Error()))
		}
		started := time.Now()
		recCh := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/query-targets/%d/schema/object-details?database=db_a&schema=app&name=orders&kind=table&refresh=true", liveID), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			handler.ServeHTTP(rec, req)
			recCh <- rec
		}()
		var rec *httptest.ResponseRecorder
		select {
		case rec = <-recCh:
		case <-time.After(12 * time.Second):
			t.Fatal("metadata did not return before the 15s caller budget")
		}
		elapsed := time.Since(started)
		body := mustPGError(t, rec, http.StatusRequestTimeout, "schema_timeout")
		if strings.Contains(strings.ToLower(body.Message), "cancel") || strings.Contains(body.Message, "57014") {
			t.Fatalf("timeout message leaked driver text: %s", redactPGMeta(body.Message))
		}
		if elapsed < 3*time.Second || elapsed > 12*time.Second {
			t.Fatalf("elapsed %s, want the database statement_timeout", elapsed)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			var left int
			if err := admin.QueryRow(lockCtx, `SELECT count(*) FROM pg_stat_activity WHERE usename = $1 AND datname = 'db_a'`, pgMetaRole).Scan(&left); err != nil {
				t.Fatalf("activity: %s", redactPGMeta(err.Error()))
			}
			if left == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("restricted backends still open: %d", left)
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = tx.Rollback(lockCtx)
		var detail model.ObjectDetailResponse
		mustPGBody(t, getSchema(t, handler, token, liveID, "object-details", url.Values{
			"database": {"db_a"},
			"schema":   {"app"},
			"name":     {"orders"},
			"kind":     {"table"},
			"refresh":  {"true"},
		}), http.StatusOK, &detail)
		if columnNamed(t, detail.Columns, "id").DatabaseType != "bigint" {
			t.Fatalf("details after timeout = %+v", detail.Columns)
		}
	})
}

func pgFormatType(t *testing.T, conn *pgx.Conn, schema, table, column string) string {
	t.Helper()
	var typ string
	err := conn.QueryRow(context.Background(), `
		SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attname = $3`, schema, table, column).Scan(&typ)
	if err != nil {
		t.Fatalf("format_type %s.%s: %s", table, column, redactPGMeta(err.Error()))
	}
	return typ
}

func indexNames(indexes []model.IndexDetail) map[string]bool {
	names := make(map[string]bool, len(indexes))
	for _, idx := range indexes {
		names[idx.Name] = true
	}
	return names
}

func assertIndexColumnsPresent(t *testing.T, indexes []model.IndexDetail) {
	t.Helper()
	for _, idx := range indexes {
		if len(idx.Columns) == 0 {
			t.Fatalf("index %s has no columns", idx.Name)
		}
	}
}

func objectDetail(t *testing.T, h http.Handler, token string, id uint64, schema, name, kind string) model.ObjectDetailResponse {
	t.Helper()
	var detail model.ObjectDetailResponse
	mustPGBody(t, getSchema(t, h, token, id, "object-details", url.Values{
		"database": {"db_a"},
		"schema":   {schema},
		"name":     {name},
		"kind":     {kind},
	}), http.StatusOK, &detail)
	return detail
}

func relationship(t *testing.T, h http.Handler, token string, id uint64, schema, name string) model.RelationshipMapResponse {
	t.Helper()
	var rel model.RelationshipMapResponse
	mustPGBody(t, getSchema(t, h, token, id, "relationship-map", url.Values{
		"database": {"db_a"},
		"schema":   {schema},
		"name":     {name},
	}), http.StatusOK, &rel)
	return rel
}

func columnNames(cols []model.ColumnDetail) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}
