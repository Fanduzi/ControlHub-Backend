//go:build integration

// Package service proves the T7-S2 transaction-internal binding gate and
// resolver against a disposable PostgreSQL server.
// input: context, errors, fmt, strings, sync, testing, time, jackc/pgx/v5,
// jackc/pgx/v5/pgxpool, internal/pgsql
// output: acceptance tests A–G for the frozen contract — touch-confirmed
// bindings and RefID preservation, controlled rejections, FD self-proof
// against same-name replacement, lock-wait timeout under the remaining
// budget, post-touch DDL blocking, view dependency boundary, recursive
// pg_inherits collection, native CommonType verdicts, cancellation, and the
// real resolver wired into RewriteFromGuardPaginated
// pos: T7-S2 real-PostgreSQL acceptance — production OpenPostgresPool,
// test-owned BEGIN READ ONLY + G4 session settings, production PGBind, then
// the S1 rewrite seam; transport SQL is never executed in this slice
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/pgsql"
)

const pgBindRolePassword = "ro-bind-pw-do-not-leak"

var (
	pgBindOnce sync.Once
	pgBindErr  error
)

// pgBindFixture creates the schemas, relations, views, partitions and the
// restricted ro_bind role the gate tests bind against. It runs once per test
// process on the shared disposable container; admin work always uses the
// lab superuser.
func pgBindFixture(t *testing.T) pgPoolLab {
	t.Helper()
	lab := postgresPoolLab(t)
	pgBindOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		admin, err := pgx.Connect(ctx, fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t7s2-fixture",
			lab.host, lab.port, lab.db, lab.user, pgPoolFactoryPassword))
		if err != nil {
			pgBindErr = err
			return
		}
		defer admin.Close(ctx)
		stmts := []string{
			`CREATE SCHEMA IF NOT EXISTS app`,
			`CREATE SCHEMA IF NOT EXISTS analytics`,
			`CREATE SCHEMA IF NOT EXISTS secret`,
			`CREATE SCHEMA IF NOT EXISTS nogranted`,
			`CREATE TABLE IF NOT EXISTS app.orders (
			    id bigint PRIMARY KEY, item text, qty integer,
			    note varchar(20), amount numeric(10,2), ts timestamptz)`,
			`CREATE TABLE IF NOT EXISTS app.users (id bigint PRIMARY KEY, name text)`,
			`CREATE TABLE IF NOT EXISTS app."Order Details" ("Line No" integer, info text)`,
			`CREATE TABLE IF NOT EXISTS app.smallvals (id integer PRIMARY KEY, tag text)`,
			`CREATE TABLE IF NOT EXISTS analytics.widevals (id bigint PRIMARY KEY, total numeric)`,
			`CREATE TABLE IF NOT EXISTS analytics.orders (id bigint, total numeric)`,
			`CREATE TABLE IF NOT EXISTS secret.payroll (id bigint, amount numeric)`,
			`CREATE TABLE IF NOT EXISTS nogranted.hidden (id bigint)`,
			`CREATE OR REPLACE VIEW app.order_view AS SELECT o.id, o.item FROM app.orders o`,
			`CREATE OR REPLACE VIEW app.nested_view AS SELECT v.id, v.item FROM app.order_view v`,
			`CREATE OR REPLACE VIEW app.cross_analytics AS SELECT a.id FROM analytics.orders a`,
			`CREATE OR REPLACE VIEW app.leak_view AS SELECT p.id FROM secret.payroll p`,
			`CREATE MATERIALIZED VIEW IF NOT EXISTS app.order_mv AS SELECT o.id FROM app.orders o`,
			`CREATE TABLE IF NOT EXISTS app.events (id bigint, day date) PARTITION BY RANGE (day)`,
			`CREATE TABLE IF NOT EXISTS app.events_2025 PARTITION OF app.events
			    FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`,
			`CREATE TABLE IF NOT EXISTS app.events_2026 PARTITION OF app.events
			    FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')`,
			`CREATE TABLE IF NOT EXISTS app.swapme (id bigint, val integer)`,
			`CREATE TABLE IF NOT EXISTS app.swapme_b (other text, num numeric)`,
			`CREATE TABLE IF NOT EXISTS app.lockme (id integer)`,
			`DO $$ BEGIN
			    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'ro_bind') THEN
			        CREATE ROLE ro_bind LOGIN PASSWORD '` + pgBindRolePassword + `';
			    END IF;
			END $$`,
			`GRANT CONNECT ON DATABASE ` + lab.db + ` TO ro_bind`,
			`GRANT USAGE ON SCHEMA app TO ro_bind`,
			`GRANT USAGE ON SCHEMA analytics TO ro_bind`,
			`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ro_bind`,
			`GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO ro_bind`,
			`GRANT SELECT ON nogranted.hidden TO ro_bind`,
		}
		for _, stmt := range stmts {
			if _, err := admin.Exec(ctx, stmt); err != nil {
				pgBindErr = fmt.Errorf("fixture %q: %w", stmt, err)
				return
			}
		}
	})
	if pgBindErr != nil {
		t.Fatalf("pg bind fixture: %v", pgBindErr)
	}
	return lab
}

func (l pgPoolLab) adminConn(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t7s2-admin",
		l.host, l.port, l.db, l.user, pgPoolFactoryPassword))
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// pgAttempt is one test-owned attempt: a MaxConns=1 pool for the restricted
// role, a caller-owned BEGIN READ ONLY transaction, and the frozen G4 session
// settings (pinned search_path, UTC, initial statement_timeout). The gate
// itself never begins or ends this transaction.
type pgAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
	pool   *pgxpool.Pool
	pconn  *pgxpool.Conn
	tx     pgx.Tx
}

func beginPGAttempt(t *testing.T, lab pgPoolLab, pinned string, budget time.Duration) *pgAttempt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	dsn := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=ro_bind password=%s sslmode=disable connect_timeout=5 application_name=ch-t7s2-attempt",
		lab.host, lab.port, lab.db, pgBindRolePassword)
	cfg, err := validatePGDSNBinding(dsn, lab.host, lab.port, lab.db)
	if err != nil {
		cancel()
		t.Fatalf("validate dsn: %v", err)
	}
	pool, err := OpenPostgresPool(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatalf("OpenPostgresPool: %v", err)
	}
	pconn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("acquire: %v", err)
	}
	tx, err := pconn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		pconn.Release()
		pool.Close()
		cancel()
		t.Fatalf("begin read only: %v", err)
	}
	setup := []struct{ sql string }{
		{`SELECT pg_catalog.set_config('search_path', $1, true)`},
		{`SELECT pg_catalog.set_config('TimeZone', 'UTC', true)`},
		{`SELECT pg_catalog.set_config('statement_timeout', '10000ms', true)`},
	}
	for i, s := range setup {
		var execErr error
		if i == 0 {
			_, execErr = tx.Exec(ctx, s.sql, pgQuoteIdent(pinned)+",pg_catalog")
		} else {
			_, execErr = tx.Exec(ctx, s.sql)
		}
		if execErr != nil {
			tx.Rollback(ctx)
			pconn.Release()
			pool.Close()
			cancel()
			t.Fatalf("G4 session setup: %v", execErr)
		}
	}
	a := &pgAttempt{ctx: ctx, cancel: cancel, pool: pool, pconn: pconn, tx: tx}
	t.Cleanup(func() {
		_ = a.tx.Rollback(context.Background())
		a.pconn.Release()
		a.pool.Close()
		a.cancel()
	})
	return a
}

func (a *pgAttempt) bind(t *testing.T, stmt string) *PGBindResult {
	t.Helper()
	res, err := PGBind(a.ctx, PGBindInput{
		Tx: a.tx, Database: "pool_lab", PinnedSchema: "app", Guard: mustGuard(t, stmt),
	})
	if err != nil {
		t.Fatalf("PGBind(%q): %v", stmt, err)
	}
	return res
}

func rejectCodeOf(t *testing.T, err error) string {
	t.Helper()
	var re *pgsql.RejectError
	if !errors.As(err, &re) {
		t.Fatalf("error %v is not a controlled rejection", err)
	}
	return re.Code
}

// A — entities, double alias, quoted names: metadata arrives by confirmed
// OID in attnum order, repeated references stay distinct RefID records, and
// every lock the gate holds is inside the recorded approved/probe sets.
func TestPGBind_BindingsAndMetadata(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	a := beginPGAttempt(t, lab, "app", 30*time.Second)

	stmt := `SELECT a1.id, a2.item FROM orders a1 JOIN app.orders a2 ON a1.id = a2.id`
	res := a.bind(t, stmt)

	var wantOID, wantRelType uint32
	if err := admin.QueryRow(a.ctx,
		`SELECT c.oid::pg_catalog.oid, c.reltype::pg_catalog.oid
		   FROM pg_catalog.pg_class c
		  WHERE c.oid = 'app.orders'::pg_catalog.regclass`).Scan(&wantOID, &wantRelType); err != nil {
		t.Fatalf("fixture oid: %v", err)
	}
	if len(res.Refs) != 2 {
		t.Fatalf("bound refs = %d, want 2 distinct occurrences", len(res.Refs))
	}
	seen := map[uint32]int{}
	for id, b := range res.Refs {
		if !b.TouchConfirmed {
			t.Fatalf("ref %d lacks touch confirmation", id)
		}
		if b.Identity.RelationOID != wantOID || b.RelTypeOID != wantRelType {
			t.Fatalf("ref %d identity = (%d,%d), want (%d,%d)", id, b.Identity.RelationOID, b.RelTypeOID, wantOID, wantRelType)
		}
		if b.Identity.DatabaseOID != res.DatabaseOID || res.DatabaseOID == 0 {
			t.Fatalf("ref %d database identity = %d", id, b.Identity.DatabaseOID)
		}
		if b.Schema != "app" || b.Name != "orders" {
			t.Fatalf("ref %d canonical = %q.%q", id, b.Schema, b.Name)
		}
		seen[b.Identity.RelationOID]++
	}
	if seen[wantOID] != 2 {
		t.Fatal("two occurrences of orders must share the confirmed OID without merging RefIDs")
	}
	var names []string
	for _, b := range res.Refs {
		got := make([]string, len(b.Columns))
		for i, c := range b.Columns {
			got[i] = c.Name
			if c.RelationOID != wantOID {
				t.Fatalf("column %s relation oid = %d", c.Name, c.RelationOID)
			}
		}
		names = got
	}
	wantCols := []string{"id", "item", "qty", "note", "amount", "ts"}
	if fmt.Sprint(names) != fmt.Sprint(wantCols) {
		t.Fatalf("attnum-ordered columns = %v, want %v", names, wantCols)
	}
	resolver := res.Resolver.(pgsql.ColumnMetadataResolver)
	md, err := resolver.ColumnMetadata("app", "orders")
	if err != nil {
		t.Fatalf("ColumnMetadata: %v", err)
	}
	byName := map[string]pgsql.ColumnMetadata{}
	for _, c := range md {
		byName[c.Name] = c
	}
	if byName["id"].Type.OID != 20 || byName["id"].AttributeNumber != 1 {
		t.Fatalf("id metadata = %+v", byName["id"])
	}
	if byName["note"].Type.OID != 1043 || byName["note"].Type.Typmod != 24 {
		t.Fatalf("note varchar(20) metadata = %+v, want oid 1043 typmod 24", byName["note"])
	}
	cols, err := res.Resolver.Columns("app", "orders")
	if err != nil || fmt.Sprint(cols) != fmt.Sprint(wantCols) {
		t.Fatalf("Columns = %v err %v", cols, err)
	}

	// Quoted and catalog-qualified references stay exact.
	stmt2 := `SELECT d."Line No" FROM "Order Details" d`
	res2 := a.bind(t, stmt2)
	for _, b := range res2.Refs {
		if b.Name != "Order Details" || len(b.Columns) != 2 {
			t.Fatalf("quoted binding = %+v", b)
		}
	}
	stmt3 := fmt.Sprintf(`SELECT o.id FROM %s.app.orders o`, lab.db)
	if _, err := PGBind(a.ctx, PGBindInput{Tx: a.tx, Database: lab.db, PinnedSchema: "app", Guard: mustGuard(t, stmt3)}); err != nil {
		t.Fatalf("same-database catalog-qualified ref must bind: %v", err)
	}

	// Every relation lock the gate backend holds must be inside
	// approved ∪ probe whitelist — the empirical whitelist consistency proof.
	var pid int32
	if err := a.tx.QueryRow(a.ctx, `SELECT pg_catalog.pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("backend pid: %v", err)
	}
	rows, err := admin.Query(a.ctx,
		`SELECT DISTINCT COALESCE(l.database, 0)::pg_catalog.oid, l.relation::pg_catalog.oid
		   FROM pg_locks l
		  WHERE l.pid = $1 AND l.locktype = 'relation'`, pid)
	if err != nil {
		t.Fatalf("lock dump: %v", err)
	}
	allowed := map[PGRelationIdentity]bool{}
	for _, id := range res.Approved {
		allowed[id] = true
	}
	for _, id := range res.Probes {
		allowed[id] = true
	}
	for _, id := range res2.Approved {
		allowed[id] = true
	}
	var held []PGRelationIdentity
	for rows.Next() {
		var id PGRelationIdentity
		if err := rows.Scan(&id.DatabaseOID, &id.RelationOID); err != nil {
			rows.Close()
			t.Fatalf("lock scan: %v", err)
		}
		held = append(held, id)
	}
	rows.Close()
	for _, id := range held {
		if !allowed[id] {
			var name string
			_ = admin.QueryRow(a.ctx,
				`SELECT n.nspname || '.' || c.relname
				   FROM pg_catalog.pg_class c
				   JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
				  WHERE c.oid = $1`, id.RelationOID).Scan(&name)
			t.Fatalf("gate holds unexplained AccessShare lock %v (%s) outside approved∪probes", id, name)
		}
	}
	if len(held) == 0 {
		t.Fatal("lock dump empty — the proof found nothing to check")
	}
}

func mustGuard(t *testing.T, stmt string) *pgsql.GuardResult {
	t.Helper()
	gr, err := pgsql.GuardPG(stmt)
	if err != nil {
		t.Fatalf("GuardPG(%q): %v", stmt, err)
	}
	return gr
}

// B — controlled rejections: missing object, pinned schema without USAGE,
// missing pinned schema, inaccessible qualified object, cross-database
// catalog, and no search_path fallback into pg_catalog.
func TestPGBind_ControlledRejections(t *testing.T) {
	lab := pgBindFixture(t)

	cases := []struct {
		name   string
		stmt   string
		pinned string
		want   string
	}{
		{"missing object", `SELECT * FROM nosuch_table`, "app", "query_object_not_found"},
		{"no pg_catalog fallback", `SELECT * FROM pg_locks`, "app", "query_object_not_found"},
		{"pinned without usage", `SELECT * FROM hidden`, "nogranted", "query_schema_not_usable"},
		{"pinned missing", `SELECT * FROM anything`, "schema_never_made", "schema_not_found"},
		{"inaccessible qualified", `SELECT * FROM secret.payroll`, "app", "query_object_not_found"},
		{"cross database", `SELECT * FROM other_db.app.orders`, "app", "query_object_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			att := beginPGAttempt(t, lab, tc.pinned, 30*time.Second)
			gr := mustGuard(t, tc.stmt)
			_, err := PGBind(att.ctx, PGBindInput{Tx: att.tx, Database: lab.db, PinnedSchema: tc.pinned, Guard: gr})
			if err == nil {
				t.Fatalf("%q bound without error", tc.stmt)
			}
			if code := rejectCodeOf(t, err); code != tc.want {
				t.Fatalf("%q code = %s, want %s (err %v)", tc.stmt, code, tc.want, err)
			}
		})
	}
}

// C — the touch's own FieldDescription is the identity proof: when a
// same-name replacement occupies the canonical name between resolution and
// touch, the FD DataTypeOID mismatch must reject — name re-resolution alone
// can be stale or correct-by-accident.
func TestPGBind_TouchDetectsReplacement(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	a := beginPGAttempt(t, lab, "app", 30*time.Second)

	gr := mustGuard(t, `SELECT s.id FROM swapme s`)
	budget, err := pgAttemptBudgetFromContext(a.ctx)
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	var once sync.Once
	var swapErr error
	var swapped bool
	g := newPGBindingGate(a.ctx, PGBindInput{
		Tx: a.tx, Database: lab.db, PinnedSchema: "app", Guard: gr,
	}, budget)
	g.afterEntityResolve = func() {
		// Deterministic interleave on the admin connection: after the gate
		// resolved "app"."swapme" to the original OID, swap a differently-
		// shaped table into that name before the self-proof touch runs.
		once.Do(func() {
			if _, swapErr = admin.Exec(a.ctx, `ALTER TABLE app.swapme RENAME TO swapme_x`); swapErr != nil {
				return
			}
			_, swapErr = admin.Exec(a.ctx, `ALTER TABLE app.swapme_b RENAME TO swapme`)
			if swapErr == nil {
				swapped = true
			}
		})
	}
	_, err = g.run()
	defer func() {
		// Restore fixture names for other tests on the shared container.
		// The gate transaction still holds ACCESS SHARE on the replacement
		// object — release it first or the rename would deadlock: defers run
		// before the attempt's own t.Cleanup rollback.
		if !swapped {
			return
		}
		_ = a.tx.Rollback(context.Background())
		admin.Exec(context.Background(), `ALTER TABLE app.swapme RENAME TO swapme_b`)
		admin.Exec(context.Background(), `ALTER TABLE app.swapme_x RENAME TO swapme`)
	}()
	if swapErr != nil {
		t.Fatalf("swap ddl: %v", swapErr)
	}
	if err == nil {
		t.Fatal("touch bound a replacement object without detection")
	}
	if code := rejectCodeOf(t, err); code != "query_binding_mismatch" {
		t.Fatalf("code = %s, want query_binding_mismatch (err %v)", code, err)
	}
}

// D — the touch's lock wait is bounded by the remaining statement_timeout:
// an ACCESS EXCLUSIVE holder blocks the touch until the armed remaining
// budget kills it with 57014, classified timeout — never a fresh budget, and
// the context itself is still alive.
func TestPGBind_LockWaitTimeout(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	a := beginPGAttempt(t, lab, "app", 30*time.Second)

	locked := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		ctx := context.Background()
		tx, err := admin.Begin(ctx)
		if err != nil {
			locked <- err
			return
		}
		if _, err := tx.Exec(ctx, `LOCK TABLE app.lockme IN ACCESS EXCLUSIVE MODE`); err != nil {
			tx.Rollback(ctx)
			locked <- err
			return
		}
		locked <- nil
		<-release
		tx.Rollback(ctx)
	}()
	if err := <-locked; err != nil {
		t.Fatalf("admin lock: %v", err)
	}
	defer close(release)

	gr := mustGuard(t, `SELECT l.id FROM lockme l`)
	// A 1.2s attempt budget inside a generous context: the lock wait must die
	// at statement_timeout (57014), not at the context deadline.
	g := newPGBindingGate(a.ctx, PGBindInput{
		Tx: a.tx, Database: lab.db, PinnedSchema: "app", Guard: gr,
	}, pgAttemptBudget{deadline: time.Now().Add(1200 * time.Millisecond)})
	started := time.Now()
	_, err := g.run()
	if err == nil {
		t.Fatal("blocked touch succeeded behind an ACCESS EXCLUSIVE holder")
	}
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("lock wait error = %v, want ErrQueryTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("lock wait ran %s — the remaining budget did not bound it", elapsed)
	}
}

// D2 — the confirmed ACCESS SHARE lock is held until rollback: a concurrent
// column rename waits on it and dies under its own statement_timeout rather
// than mutating the bound layout mid-attempt.
func TestPGBind_PostBindDDLBlocked(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	a := beginPGAttempt(t, lab, "app", 30*time.Second)
	_ = a.bind(t, `SELECT o.id FROM orders o`)

	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		tx, err := admin.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '1500ms'`); err != nil {
			done <- err
			return
		}
		_, err = tx.Exec(ctx, `ALTER TABLE app.orders RENAME COLUMN item TO item_x`)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("concurrent RENAME COLUMN succeeded while the bound relation was locked")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
			t.Fatalf("rename error = %v, want SQLSTATE 57014 (blocked by held ACCESS SHARE)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("rename neither succeeded nor timed out — indeterminate interleave")
	}
}

// E — views expand to their relation closure inside {pinned ∪ view schema};
// out-of-boundary deps reject; materialized views behave like views; and
// pg_inherits descendants are collected with ownership edges.
func TestPGBind_ViewDepsAndInheritance(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	oidOf := func(reg string) uint32 {
		t.Helper()
		var oid uint32
		if err := admin.QueryRow(context.Background(),
			`SELECT ($1::pg_catalog.regclass)::pg_catalog.oid`, reg).Scan(&oid); err != nil {
			t.Fatalf("regclass %q: %v", reg, err)
		}
		return oid
	}
	ordersOID := oidOf("app.orders")
	viewOID := oidOf("app.order_view")
	mvOID := oidOf("app.order_mv")
	eventsOID := oidOf("app.events")
	child25 := oidOf("app.events_2025")
	child26 := oidOf("app.events_2026")

	a := beginPGAttempt(t, lab, "app", 30*time.Second)

	res := a.bind(t, `SELECT v.id, v.item FROM app.order_view v`)
	depOIDs := map[uint32]bool{}
	for _, d := range res.Dependencies {
		if d.ViewOID != viewOID {
			t.Fatalf("dep recorded under view %d, want %d", d.ViewOID, viewOID)
		}
		depOIDs[d.RelationOID] = true
	}
	if len(depOIDs) != 1 || !depOIDs[ordersOID] {
		t.Fatalf("view deps = %v, want {orders %d}", depOIDs, ordersOID)
	}
	if !inApproved(res, ordersOID) || !inApproved(res, viewOID) {
		t.Fatal("view and base table must both be approved")
	}

	res2 := a.bind(t, `SELECT n.id FROM app.nested_view n`)
	gotDeps := map[uint32]bool{}
	for _, d := range res2.Dependencies {
		gotDeps[d.RelationOID] = true
	}
	if !gotDeps[viewOID] || !gotDeps[ordersOID] {
		t.Fatalf("nested view closure = %v, want order_view + orders", gotDeps)
	}

	res3 := a.bind(t, `SELECT m.id FROM app.order_mv m`)
	found := false
	for _, d := range res3.Dependencies {
		if d.ViewOID == mvOID && d.RelationOID == ordersOID {
			found = true
		}
	}
	if !found {
		t.Fatalf("matview dep closure = %+v, want order_mv→orders", res3.Dependencies)
	}

	for _, tc := range []struct {
		name string
		stmt string
	}{
		{"cross-schema dep", `SELECT c.id FROM app.cross_analytics c`},
		{"secret dep", `SELECT l.id FROM app.leak_view l`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PGBind(a.ctx, PGBindInput{Tx: a.tx, Database: lab.db, PinnedSchema: "app", Guard: mustGuard(t, tc.stmt)})
			if err == nil {
				t.Fatalf("%q bound without rejection", tc.stmt)
			}
			if code := rejectCodeOf(t, err); code != "query_dependency_not_approved" {
				t.Fatalf("%q code = %s, want query_dependency_not_approved", tc.stmt, code)
			}
		})
	}

	res4 := a.bind(t, `SELECT e.id, e.day FROM app.events e`)
	edges := map[[2]uint32]bool{}
	for _, e := range res4.Inherits {
		edges[[2]uint32{e.ParentOID, e.ChildOID}] = true
	}
	if !edges[[2]uint32{eventsOID, child25}] || !edges[[2]uint32{eventsOID, child26}] {
		t.Fatalf("inherit edges = %v, want events→{2025,2026}", edges)
	}
	if !inApproved(res4, eventsOID) || !inApproved(res4, child25) || !inApproved(res4, child26) {
		t.Fatal("partition children must be in the approved set")
	}
	if len(res4.Refs) != 1 {
		t.Fatalf("events bound refs = %d, want exactly the parent occurrence", len(res4.Refs))
	}
}

func inApproved(res *PGBindResult, oid uint32) bool {
	for _, id := range res.Approved {
		if id.RelationOID == oid && id.DatabaseOID == res.DatabaseOID {
			return true
		}
	}
	return false
}

// F — CommonType is PostgreSQL's own verdict, identity-checked on both
// sides; cancellation and budget exhaustion classify correctly; and no
// statement leaves the single connection busy.
func TestPGBind_CommonTypeAndBudget(t *testing.T) {
	lab := pgBindFixture(t)
	a := beginPGAttempt(t, lab, "app", 30*time.Second)
	res := a.bind(t, `SELECT s.id, w.total FROM app.smallvals s JOIN analytics.widevals w ON s.id = w.id`)
	resolver := res.Resolver.(pgsql.ColumnMetadataResolver)

	verdict, err := resolver.CommonType(23, 20)
	if err != nil || verdict != 20 {
		t.Fatalf("CommonType(int4,int8) = %d, %v; want 20", verdict, err)
	}
	if v, err := resolver.CommonType(25, 1043); err != nil || v != 25 {
		t.Fatalf("CommonType(text,varchar) = %d, %v; want 25", v, err)
	}
	if v, err := resolver.CommonType(23, 23); err != nil || v != 23 {
		t.Fatalf("CommonType(int4,int4) = %d, %v; want 23", v, err)
	}

	// The connection is not left busy after drained probes.
	var one int
	if err := a.tx.QueryRow(a.ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("post-probe query: one=%d err=%v — cursor left the connection busy", one, err)
	}

	// Spent budget: zero statements may be sent once the deadline passed.
	dead := &pgTxResolver{
		ctx:    a.ctx,
		tx:     a.tx,
		budget: pgAttemptBudget{deadline: time.Now().Add(-time.Second)},
		byName: map[pgRelKey]*pgBoundRelation{},
		common: map[[2]uint32]uint32{},
		fail:   (&pgBindingGate{ctx: a.ctx}).fail,
	}
	if _, err := dead.CommonType(23, 20); !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("spent budget CommonType = %v, want ErrQueryTimeout", err)
	}

	// Caller cancellation is cancellation — never a claim the remote stopped.
	canceledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	canceledGate := &pgBindingGate{ctx: canceledCtx}
	canceled := &pgTxResolver{
		ctx:    canceledCtx,
		tx:     a.tx,
		budget: pgAttemptBudget{deadline: time.Now().Add(time.Minute)},
		byName: map[pgRelKey]*pgBoundRelation{},
		common: map[[2]uint32]uint32{},
		fail:   canceledGate.fail,
	}
	if _, err := canceled.CommonType(23, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled CommonType = %v, want context.Canceled", err)
	}

	// A probe for types with no common verdict errors on the server — which
	// aborts the attempt transaction exactly like any other attempt failure.
	// Run it last on a dedicated attempt.
	att := beginPGAttempt(t, lab, "app", 30*time.Second)
	bad := att.bind(t, `SELECT o.id FROM orders o`)
	badRes := bad.Resolver.(pgsql.ColumnMetadataResolver)
	if _, err := badRes.CommonType(1007, 23); err == nil {
		t.Fatal("CommonType(int[],int4) must fail — no common verdict exists")
	} else if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("no-verdict CommonType = %v, want internal failure", err)
	}
}

// G — the production resolver feeds the frozen S1 rewrite seam with the same
// GuardResult the gate inspected: PublicProofs, CoveredSources and BoundRefs
// align by original RefID — no renumbering, no name matching.
func TestPGBind_RewriteIntegration(t *testing.T) {
	lab := pgBindFixture(t)
	admin := lab.adminConn(t)
	oidOf := func(reg string) uint32 {
		var oid uint32
		if err := admin.QueryRow(context.Background(),
			`SELECT ($1::pg_catalog.regclass)::pg_catalog.oid`, reg).Scan(&oid); err != nil {
			t.Fatalf("regclass: %v", err)
		}
		return oid
	}
	ordersOID := oidOf("app.orders")
	smallOID := oidOf("app.smallvals")
	wideOID := oidOf("analytics.widevals")

	a := beginPGAttempt(t, lab, "app", 60*time.Second)
	opts := pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100}

	pipeline := func(stmt string) (*PGBindResult, *pgsql.RewriteResult) {
		t.Helper()
		gr := mustGuard(t, stmt)
		bind, err := PGBind(a.ctx, PGBindInput{Tx: a.tx, Database: lab.db, PinnedSchema: "app", Guard: gr})
		if err != nil {
			t.Fatalf("PGBind(%q): %v", stmt, err)
		}
		rw, _, err := pgsql.RewriteFromGuardPaginated(stmt, "app", gr, bind.Resolver, opts)
		if err != nil {
			t.Fatalf("RewriteFromGuardPaginated(%q): %v", stmt, err)
		}
		if len(rw.Refs) != len(gr.Refs) {
			t.Fatalf("%q refs = %d, want guard's %d", stmt, len(rw.Refs), len(gr.Refs))
		}
		return bind, rw
	}

	// Every occurrence a witness covers or a proof depends on must land on a
	// bound RefID — the identity bridge is the original RefID, never order.
	checkAlignment := func(stmt string, bind *PGBindResult, rw *pgsql.RewriteResult) {
		t.Helper()
		for i, w := range rw.Witnesses {
			if len(w.CoveredSources) == 0 {
				t.Fatalf("%q witness %d covers nothing", stmt, i)
			}
			for _, occID := range w.CoveredSources {
				occ, ok := rw.SourceCatalog[occID]
				if !ok {
					t.Fatalf("%q witness covers unknown occurrence %d", stmt, occID)
				}
				if occ.Kind == pgsql.SourceEntityRangeVar {
					if _, bound := bind.Refs[occ.RefID]; !bound {
						t.Fatalf("%q witness covers unbound entity RefID %d", stmt, occ.RefID)
					}
				}
			}
		}
		for i, p := range rw.PublicProofs {
			if p.Resolution != pgsql.ProofComplete {
				t.Fatalf("%q public[%d] proof incomplete: %+v", stmt, i, p)
			}
			if p.FDOrigin.Kind == pgsql.FDOriginKnownColumn {
				occ, ok := rw.SourceCatalog[p.FDOrigin.Source.Occurrence]
				if !ok {
					t.Fatalf("%q proof %d FD source occurrence %d unknown", stmt, i, p.FDOrigin.Source.Occurrence)
				}
				if occ.Kind != pgsql.SourceEntityRangeVar || !occ.HasRefID {
					t.Fatalf("%q proof %d FD source is not a bound entity occurrence", stmt, i)
				}
				b := bind.Refs[occ.RefID]
				if b == nil || b.Identity.RelationOID != p.FDOrigin.RelationOID {
					t.Fatalf("%q proof %d FD origin rel %d mismatches bound %+v", stmt, i, p.FDOrigin.RelationOID, b)
				}
				live := false
				for _, c := range b.Columns {
					if c.AttributeNumber == p.FDOrigin.AttributeNumber {
						live = true
					}
				}
				if !live {
					t.Fatalf("%q proof %d FD attnum %d is not a live bound column", stmt, i, p.FDOrigin.AttributeNumber)
				}
			}
			for _, dep := range p.Dependencies {
				if occ, ok := rw.SourceCatalog[dep.Occurrence]; ok && occ.Kind == pgsql.SourceEntityRangeVar {
					if _, bound := bind.Refs[occ.RefID]; !bound {
						t.Fatalf("%q proof dep lands on unbound RefID %d", stmt, occ.RefID)
					}
				}
			}
		}
	}

	t.Run("direct columns", func(t *testing.T) {
		bind, rw := pipeline(`SELECT o.id, o.item FROM orders o`)
		if fmt.Sprint(rw.Public) != "[id item]" {
			t.Fatalf("public = %v", rw.Public)
		}
		var fdOIDs []uint32
		var fdAttnums []int16
		for _, p := range rw.PublicProofs {
			if p.FDOrigin.Kind != pgsql.FDOriginKnownColumn {
				t.Fatalf("direct column lacks FD origin: %+v", p)
			}
			fdOIDs = append(fdOIDs, p.FDOrigin.RelationOID)
			fdAttnums = append(fdAttnums, p.FDOrigin.AttributeNumber)
		}
		if len(fdOIDs) != 2 || fdOIDs[0] != ordersOID || fdOIDs[1] != ordersOID {
			t.Fatalf("FD origins = %v, want two hits on orders %d", fdOIDs, ordersOID)
		}
		if fmt.Sprint(fdAttnums) != "[1 2]" {
			t.Fatalf("FD attnums = %v, want orders id=1 item=2", fdAttnums)
		}
		checkAlignment("direct", bind, rw)
	})

	t.Run("expression", func(t *testing.T) {
		bind, rw := pipeline(`SELECT o.qty + 1 AS plus FROM orders o`)
		if len(rw.PublicProofs) != 1 || rw.PublicProofs[0].Resolution != pgsql.ProofComplete {
			t.Fatalf("expression proof = %+v", rw.PublicProofs)
		}
		checkAlignment("expr", bind, rw)
	})

	t.Run("cte use site", func(t *testing.T) {
		bind, rw := pipeline(`WITH d AS (SELECT id FROM app.orders) SELECT d.id FROM d`)
		for _, ref := range rw.Refs {
			if ref.Kind == pgsql.RefCTE {
				if _, bound := bind.Refs[ref.RefID]; bound {
					t.Fatal("a CTE use site must never become a BoundRef")
				}
			}
		}
		checkAlignment("cte", bind, rw)
	})

	t.Run("same entity twice", func(t *testing.T) {
		bind, rw := pipeline(`SELECT x.id, y.item FROM app.orders x JOIN app.orders y ON x.id = y.id`)
		if len(bind.Refs) != 2 {
			t.Fatalf("bound refs = %d, want 2", len(bind.Refs))
		}
		checkAlignment("twice", bind, rw)
	})

	t.Run("common-type USING join", func(t *testing.T) {
		// int4 USING int8 projected as the merged id — the rewrite only
		// succeeds if the in-transaction resolver returns PostgreSQL's own
		// common-type verdict (int8) for the merged column.
		bind, rw := pipeline(`SELECT id, s.tag, w.total FROM app.smallvals s JOIN analytics.widevals w USING (id)`)
		for _, want := range []uint32{smallOID, wideOID} {
			if !inApproved(bind, want) {
				t.Fatalf("approved set %v missing %d", bind.Approved, want)
			}
		}
		checkAlignment("using", bind, rw)
	})

	t.Run("quoted entity", func(t *testing.T) {
		bind, rw := pipeline(`SELECT d."Line No" FROM "Order Details" d`)
		if fmt.Sprint(rw.Public) != `[Line No]` {
			t.Fatalf("public = %v", rw.Public)
		}
		checkAlignment("quoted", bind, rw)
	})

	// No cursor may leave the connection busy after the whole pipeline.
	var two int
	if err := a.tx.QueryRow(a.ctx, `SELECT 2`).Scan(&two); err != nil || two != 2 {
		t.Fatalf("post-pipeline query: %d %v", two, err)
	}
}
