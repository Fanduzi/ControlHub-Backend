//go:build integration

// Package integration — real PG16 + real MySQL vertical for the keyed related-record navigation entry (PG-MULTI-ENTRY stage 3).
// input: shared MySQL fixture (real target/credential/claim/execution/ disclosure repos), a disposable PostgreSQL 16 with a restricted role and FK mappings, and the production NavigatePGRelatedRecordsClaimed entry
// output: TestPGMultiEntryNavigation* — vertical lifecycle with trusted, plus newPGNavFixtureOn (image-parametrized catalog for T12-R1 versions) relation metadata; composite/same-name/cross-schema/self/quoted mappings; locator edge values; pre-claim refusal pairs; disclosure blocking before the wire; delivery gate; real constraint-drift and lock-protection proofs via labeled injection wrappers
// pos: stage-3 production-orchestration proof — requests travel the real service entry; no test hand-composes claim/finalize calls. Drift and lock proofs use wrappers over the REAL repositories (forwarding real work) to schedule admin DDL — labeled injection, never a pretend client.
// note: if this file changes, update this header and module README.md.

package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// newPGNavFixture extends the shared disclosure fixture with the navigation
// catalog: single/composite/cross-schema/self/quoted/trailing-space FK
// mappings, a same-name constraint on a second source, an over-width target
// for the delivery gate, and a no-USAGE-schema source.
func newPGNavFixture(t *testing.T) pgDiscFixture {
	t.Helper()
	return newPGNavFixtureOn(t, "postgres:16-alpine")
}

func newPGNavFixtureOn(t *testing.T, image string) pgDiscFixture {
	t.Helper()
	fix := newPGDiscFixtureOn(t, image)
	setupPGNavFixtureData(t, fix)
	return fix
}

// setupPGNavFixtureData applies the navigation catalog to an existing
// fixture — shared by the plain navigation fixture and the isolated
// pg_stat_statements container.
func setupPGNavFixtureData(t *testing.T, fix pgDiscFixture) {
	t.Helper()
	ctx := context.Background()
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(ctx) }()
	for _, stmt := range []string{
		// Single-column FK → app.orders.
		`CREATE TABLE app.order_items (id bigint PRIMARY KEY, order_id bigint,
		    CONSTRAINT fk_orders FOREIGN KEY (order_id) REFERENCES app.orders(id))`,
		`INSERT INTO app.order_items VALUES (1, 1), (2, 2), (3, 1)`,
		// Same constraint name on a different source — the mapping keys by
		// (source table, conname), never by FK name alone.
		`CREATE TABLE app.order_items_alt (id bigint PRIMARY KEY, order_id bigint,
		    CONSTRAINT fk_orders FOREIGN KEY (order_id) REFERENCES app.orders(id))`,
		`INSERT INTO app.order_items_alt VALUES (1, 2)`,
		// Composite FK declared (zz, aa) → (z, a): the definition order is
		// neither alphabetical nor attnum order.
		`CREATE TABLE app.comp_target (a bigint, z bigint, PRIMARY KEY (a, z))`,
		`INSERT INTO app.comp_target VALUES (11, 22), (33, 44)`,
		`CREATE TABLE app.comp_src (id bigint PRIMARY KEY, aa bigint, zz bigint,
		    CONSTRAINT fk_comp FOREIGN KEY (zz, aa) REFERENCES app.comp_target (z, a))`,
		`INSERT INTO app.comp_src VALUES (1, 11, 22)`,
		// Cross-schema referenced relation.
		`CREATE TABLE analytics.metrics (id bigint PRIMARY KEY, label text)`,
		`INSERT INTO analytics.metrics VALUES (7, 'm7')`,
		`CREATE TABLE app.metric_refs (id bigint PRIMARY KEY, m_id bigint,
		    CONSTRAINT fk_metric FOREIGN KEY (m_id) REFERENCES analytics.metrics(id))`,
		`INSERT INTO app.metric_refs VALUES (1, 7)`,
		// Self-referencing FK.
		`CREATE TABLE app.tree (id bigint PRIMARY KEY, parent bigint,
		    CONSTRAINT fk_parent FOREIGN KEY (parent) REFERENCES app.tree(id))`,
		`INSERT INTO app.tree VALUES (1, NULL), (2, 1), (3, 1)`,
		// Quoted mixed-case names and a quoted FK name.
		`CREATE TABLE app."Camel Case" (id bigint PRIMARY KEY, "v x" text)`,
		`INSERT INTO app."Camel Case" VALUES (5, 'cv')`,
		`CREATE TABLE app."ref tab" (id bigint PRIMARY KEY, fk_id bigint,
		    CONSTRAINT "fk Weird" FOREIGN KEY (fk_id) REFERENCES app."Camel Case"(id))`,
		`INSERT INTO app."ref tab" VALUES (1, 5)`,
		// Trailing-space referenced relation (the disc fixture already
		// creates app."trail ").
		`CREATE TABLE app.reftrail (id bigint PRIMARY KEY, fk_id bigint,
		    CONSTRAINT fk_trail FOREIGN KEY (fk_id) REFERENCES app."trail "(id))`,
		`INSERT INTO app.reftrail VALUES (1, 1)`,
		// Text-typed locator target: proves empty string, literal "NULL",
		// and SQL-looking text bind as exact values.
		`CREATE TABLE app.tag_target (tag text PRIMARY KEY)`,
		`INSERT INTO app.tag_target VALUES (''), ('safe'), ('''; DROP TABLE app.orders; --x')`,
		`CREATE TABLE app.tag_refs (id bigint PRIMARY KEY, t text,
		    CONSTRAINT fk_tag FOREIGN KEY (t) REFERENCES app.tag_target(tag))`,
		`INSERT INTO app.tag_refs VALUES (1, 'safe')`,
		// Over-width payload for the delivery gate.
		`CREATE TABLE app.blob_target (id bigint PRIMARY KEY, payload text)`,
		`INSERT INTO app.blob_target VALUES (9, repeat('x', 9000))`,
		`CREATE TABLE app.blob_refs (id bigint PRIMARY KEY, b_id bigint,
		    CONSTRAINT fk_blob FOREIGN KEY (b_id) REFERENCES app.blob_target(id))`,
		`INSERT INTO app.blob_refs VALUES (1, 9)`,
		// Alternate physical referenced table — the target-swap drift case
		// re-points the same-named FK here between preparation and attempt.
		`CREATE TABLE app.orders_alt (id bigint PRIMARY KEY, qty integer, note text)`,
		`INSERT INTO app.orders_alt VALUES (1, 5, 'alt'), (2, 5, 'alt2')`,
		// Unicode identifiers end to end: quoted Unicode source, target,
		// FK name, and column names.
		`CREATE TABLE app."注文" ("注文番号" text PRIMARY KEY, "内容" text)`,
		`INSERT INTO app."注文" VALUES ('注文-001', '中身')`,
		`CREATE TABLE app."注文明細" (id bigint PRIMARY KEY, "番号" text,
		    CONSTRAINT "fk_注文" FOREIGN KEY ("番号") REFERENCES app."注文"("注文番号"))`,
		`INSERT INTO app."注文明細" VALUES (1, '注文-001')`,
		// Source-side index and inheritance child: both enter the audit's
		// explainable set through the private navigation source identity.
		`CREATE INDEX idx_tag_refs_t ON app.tag_refs(t)`,
		`CREATE TABLE app.tag_refs_child () INHERITS (app.tag_refs)`,
		// A source the restricted role cannot see (no schema USAGE) — the
		// metadata probe's privilege filter makes it invisible, i.e. not
		// found, never an internal error.
		`CREATE TABLE noaccess.hidden (id bigint PRIMARY KEY)`,
		`GRANT SELECT ON noaccess.hidden TO ` + pgDiscRole,
		// Tables created after the fixture's GRANT need the grant re-issued.
		`GRANT SELECT ON ALL TABLES IN SCHEMA app TO ` + pgDiscRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
}

func navSource(database, schema, object, fk string) model.RelatedRecordNavigationSource {
	return model.RelatedRecordNavigationSource{
		Kind: string(model.ObjectKindTable), Database: database,
		Schema: schema, Object: object, ForeignKey: fk,
	}
}

func navRequest(key string, source model.RelatedRecordNavigationSource, values []string) service.PGClaimedNavigationRequest {
	return service.PGClaimedNavigationRequest{ClientExecutionID: key, Source: source, LocalValues: values}
}

// navPolicy inserts disclosure policies for every public column of a
// navigation target — the generated SELECT * exposes them all.
func navPolicy(t *testing.T, disc *mysqlrepo.MySQLQueryDisclosureRepository, targetID uint64, db, schema, object string, columns ...string) {
	t.Helper()
	for _, col := range columns {
		insertPolicy(t, disc, targetID, db, schema, object, col, model.ResultDisclosureRawCopyAllowed)
	}
}

// navAuditEvents counts related_record_navigation audit rows for a target —
// the navigation family is the only audit evidence this entry may emit.
func navAuditEvents(t *testing.T, db *sql.DB, targetID uint64) int {
	t.Helper()
	return countWhere(t, db,
		`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'related_record_navigation'`,
		targetID)
}

// navKeyedRow reads the keyed terminal row's withheld-contract columns.
func navKeyedRow(t *testing.T, db *sql.DB, executionID uint64) (preview, full, digest string) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT statement_preview, COALESCE(full_statement,''), statement_digest FROM query_executions WHERE id = ?`,
		executionID).Scan(&preview, &full, &digest); err != nil {
		t.Fatalf("keyed row read: %v", err)
	}
	return preview, full, digest
}

// ---------------------------------------------------------------------------
// Vertical: real navigation request → access → metadata prep → claim →
// governed locator execution → keyed finalize → trusted relation metadata.
// ---------------------------------------------------------------------------

func TestPGMultiEntryNavigation_Vertical(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-vert-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")

	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-nav-vert", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess || out.Result == nil {
		t.Fatalf("outcome = %+v, want real execution id + delivered result", out)
	}
	if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "1" ||
		*out.Result.Rows[0][1] != "10" || *out.Result.Rows[0][2] != "n1" {
		t.Fatalf("rows = %+v, want the single referenced orders row", out.Result.Rows)
	}
	if out.Evidence == nil || out.Evidence.BackendPID == 0 || !out.Evidence.UserSQLSent {
		t.Fatalf("evidence = %+v, want real backend pid + sent", out.Evidence)
	}
	rel := out.Relation
	if rel == nil || rel.Database != "discdb" || rel.SourceSchema != "app" || rel.SourceObject != "order_items" ||
		rel.ForeignKey != "fk_orders" || rel.ReferencedSchema != "app" || rel.ReferencedObject != "orders" ||
		len(rel.SourceColumns) != 1 || rel.SourceColumns[0] != "order_id" ||
		len(rel.ReferencedColumns) != 1 || rel.ReferencedColumns[0] != "id" {
		t.Fatalf("relation = %+v, want canonical mapping metadata", rel)
	}

	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "pgmulti-nav-vert", userIdentity(claimUserActor))
	if err != nil || view.Execution == nil || view.Execution.ID != out.ExecutionID || view.Claim.ExecutionID != out.ExecutionID {
		t.Fatalf("linked view mismatch: claim=%+v exec=%+v err=%v", view.Claim, view.Execution, err)
	}
	preview, full, digest := navKeyedRow(t, db, out.ExecutionID)
	if preview != "[statement withheld]" || full != "" || len(digest) != 64 {
		t.Fatalf("nav keyed row preview=%q full=%q digest=%q — no generated SQL or locator may persist",
			preview, full, digest)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "pgmulti-nav-vert"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}
	if navAuditEvents(t, db, targetID) != 1 {
		t.Fatalf("navigation audit events = %d, want exactly 1 related_record_navigation", navAuditEvents(t, db, targetID))
	}
}

// ---------------------------------------------------------------------------
// Mapping shapes: composite definition order, shared FK name, cross-schema,
// self FK, quoted/trailing-space names, and locator edge values — all through
// the real entry.
// ---------------------------------------------------------------------------

func TestPGMultiEntryNavigation_Shapes(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-shape-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")
	navPolicy(t, disc, targetID, "discdb", "app", "comp_target", "a", "z")
	navPolicy(t, disc, targetID, "discdb", "analytics", "metrics", "id", "label")
	navPolicy(t, disc, targetID, "discdb", "app", "tree", "id", "parent")
	navPolicy(t, disc, targetID, "discdb", "app", "Camel Case", "id", "v x")
	navPolicy(t, disc, targetID, "discdb", "app", "trail ", "id", "v ")
	navPolicy(t, disc, targetID, "discdb", "app", "tag_target", "tag")
	navPolicy(t, disc, targetID, "discdb", "app", "注文", "注文番号", "内容")

	t.Run("composite fk order", func(t *testing.T) {
		// conkey order is (zz, aa) → confkey (z, a) — neither alphabetical
		// nor attnum order; locator values bind in constraint order.
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-comp", navSource("discdb", "app", "comp_src", "fk_comp"), []string{"22", "11"}))
		if err != nil {
			t.Fatalf("composite navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "11" || *out.Result.Rows[0][1] != "22" {
			t.Fatalf("composite rows = %+v, want comp_target (a=11, z=22)", out.Result.Rows)
		}
		rel := out.Relation
		if rel == nil || len(rel.SourceColumns) != 2 || rel.SourceColumns[0] != "zz" || rel.SourceColumns[1] != "aa" ||
			len(rel.ReferencedColumns) != 2 || rel.ReferencedColumns[0] != "z" || rel.ReferencedColumns[1] != "a" {
			t.Fatalf("relation = %+v, want definition-order columns zz,aa → z,a", rel)
		}
	})

	t.Run("same fk name on another source", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-alt", navSource("discdb", "app", "order_items_alt", "fk_orders"), []string{"2"}))
		if err != nil {
			t.Fatalf("alt-source navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "2" {
			t.Fatalf("alt rows = %+v, want orders id=2", out.Result.Rows)
		}
	})

	t.Run("cross-schema target", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-xschema", navSource("discdb", "app", "metric_refs", "fk_metric"), []string{"7"}))
		if err != nil {
			t.Fatalf("cross-schema navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "7" || *out.Result.Rows[0][1] != "m7" {
			t.Fatalf("cross-schema rows = %+v", out.Result.Rows)
		}
		if out.Relation == nil || out.Relation.ReferencedSchema != "analytics" || out.Relation.ReferencedObject != "metrics" {
			t.Fatalf("relation = %+v, want analytics.metrics", out.Relation)
		}
	})

	t.Run("self fk", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-self", navSource("discdb", "app", "tree", "fk_parent"), []string{"1"}))
		if err != nil {
			t.Fatalf("self-fk navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "1" || out.Result.Rows[0][1] != nil {
			t.Fatalf("self-fk rows = %+v, want tree (1, NULL)", out.Result.Rows)
		}
	})

	t.Run("quoted mixedcase names", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-quoted", navSource("discdb", "app", "ref tab", "fk Weird"), []string{"5"}))
		if err != nil {
			t.Fatalf("quoted navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "5" || *out.Result.Rows[0][1] != "cv" {
			t.Fatalf("quoted rows = %+v", out.Result.Rows)
		}
	})

	t.Run("trailing space target", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-trail", navSource("discdb", "app", "reftrail", "fk_trail"), []string{"1"}))
		if err != nil {
			t.Fatalf("trailing-space navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "1" || *out.Result.Rows[0][1] != "space" {
			t.Fatalf("trailing-space rows = %+v", out.Result.Rows)
		}
	})

	t.Run("unicode identifiers", func(t *testing.T) {
		// Quoted Unicode source/target/FK/column names resolve verbatim —
		// the request names land in the bound metadata probe, and the
		// Unicode locator value binds as data.
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-uni", navSource("discdb", "app", "注文明細", "fk_注文"), []string{"注文-001"}))
		if err != nil {
			t.Fatalf("unicode navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "注文-001" || *out.Result.Rows[0][1] != "中身" {
			t.Fatalf("unicode rows = %+v, want (注文-001, 中身)", out.Result.Rows)
		}
		rel := out.Relation
		if rel == nil || rel.SourceObject != "注文明細" || rel.ForeignKey != "fk_注文" ||
			rel.SourceColumns[0] != "番号" || rel.ReferencedObject != "注文" || rel.ReferencedColumns[0] != "注文番号" {
			t.Fatalf("unicode relation = %+v, want canonical Unicode mapping names", rel)
		}
	})

	t.Run("source index and inheritance audit", func(t *testing.T) {
		// tag_refs carries a secondary index and an inheritance child —
		// both enter the explainable set through the private navigation
		// source; the real lock audit must verify the attempt anyway.
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-ownaudit", navSource("discdb", "app", "tag_refs", "fk_tag"), []string{"safe"}))
		if err != nil {
			t.Fatalf("indexed/inherited source navigate: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != "safe" {
			t.Fatalf("audit rows = %+v, want the 'safe' tag", out.Result.Rows)
		}
	})

	t.Run("locator text edge values", func(t *testing.T) {
		// Empty string is a zero-length text value — it binds and matches
		// the '' row exactly; it never collapses to NULL.
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-empty", navSource("discdb", "app", "tag_refs", "fk_tag"), []string{""}))
		if err != nil {
			t.Fatalf("empty locator: %v", err)
		}
		if len(out.Result.Rows) != 1 || out.Result.Rows[0][0] == nil || *out.Result.Rows[0][0] != "" {
			t.Fatalf("empty-locator rows = %+v, want the '' row as empty text", out.Result.Rows)
		}
		// SQL-looking text is data — exact match against the stored row.
		sqlLooking := `'; DROP TABLE app.orders; --x`
		out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-sqltxt", navSource("discdb", "app", "tag_refs", "fk_tag"), []string{sqlLooking}))
		if err != nil {
			t.Fatalf("sql-looking locator: %v", err)
		}
		if len(out.Result.Rows) != 1 || *out.Result.Rows[0][0] != sqlLooking {
			t.Fatalf("sql-looking rows = %+v, want exact text round trip", out.Result.Rows)
		}
		// Literal "NULL" text matches nothing here — still a clean success.
		out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-nulltxt", navSource("discdb", "app", "tag_refs", "fk_tag"), []string{"NULL"}))
		if err != nil {
			t.Fatalf("NULL-text locator: %v", err)
		}
		if len(out.Result.Rows) != 0 {
			t.Fatalf("NULL-text rows = %+v, want empty page", out.Result.Rows)
		}
		// A locator with no match is a successful empty page, not an error.
		out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-nomatch", navSource("discdb", "app", "order_items", "fk_orders"), []string{"999"}))
		if err != nil {
			t.Fatalf("no-match locator: %v", err)
		}
		if len(out.Result.Rows) != 0 || out.Relation == nil {
			t.Fatalf("no-match rows = %+v relation = %+v, want empty page + relation", out.Result.Rows, out.Relation)
		}
	})

	t.Run("bigint cast failure is a keyed rejection", func(t *testing.T) {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-badcast", navSource("discdb", "app", "order_items", "fk_orders"), []string{"not-a-number"}))
		if err == nil {
			t.Fatal("invalid bigint locator must surface a controlled error")
		}
		if strings.Contains(err.Error(), "not-a-number") || strings.Contains(err.Error(), "22P02") {
			t.Fatalf("raw server detail leaked: %v", err)
		}
		rejected := countWhere(t, db,
			`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-nav-badcast' AND status = 'rejected'`,
			targetID)
		if rejected != 1 {
			t.Fatalf("keyed rejected rows = %d, want 1", rejected)
		}
		_ = out
	})
}

// ---------------------------------------------------------------------------
// Pre-claim refusals and the post-claim disclosure boundary: invisible
// source/FK and locator-count mismatches write one NULL-key rejected pair;
// a missing policy stops the keyed attempt before the user statement sends.
// ---------------------------------------------------------------------------

func TestPGMultiEntryNavigation_PreClaim(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-pre-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")
	nullKeyNav := func(key string, code string, wantTotal int) {
		t.Helper()
		if claimOccupied(t, db, targetID, key) {
			t.Fatalf("refusal occupied key %q", key)
		}
		n := countWhere(t, db,
			`SELECT COUNT(*) FROM query_executions e
			  WHERE e.target_resource_id = ? AND e.client_execution_id IS NULL AND e.status = 'rejected' AND e.error_code = ?`,
			targetID, code)
		if n != wantTotal {
			t.Fatalf("NULL-key %s rows = %d, want %d cumulative", code, n, wantTotal)
		}
	}

	// Missing object, missing FK, and invisible-schema source are the same
	// controlled not-found refusal — one NULL-key rejected pair each.
	for i, tc := range []struct {
		key    string
		source model.RelatedRecordNavigationSource
	}{
		{"pgmulti-nav-noobj", navSource("discdb", "app", "nope", "fk_orders")},
		{"pgmulti-nav-nofk", navSource("discdb", "app", "order_items", "nope")},
		{"pgmulti-nav-hidden", navSource("discdb", "noaccess", "hidden", "nope")},
	} {
		_, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest(tc.key, tc.source, []string{"1"}))
		if !errors.Is(err, service.ErrNavigationSourceNotFound) {
			t.Fatalf("%s = %v, want navigation source not found", tc.key, err)
		}
		nullKeyNav(tc.key, "navigation_source_not_found", i+1)
	}

	// Locator count must match the FK column count before claim.
	_, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-nav-vcount", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1", "2"}))
	if !errors.Is(err, service.ErrNavigationValueMismatch) {
		t.Fatalf("value count = %v, want navigation value mismatch", err)
	}
	nullKeyNav("pgmulti-nav-vcount", "navigation_value_mismatch", 1)

	// A target column without a policy stops the claimed attempt at the
	// disclosure preflight — the locator statement is never sent.
	blockedTarget := createPGInstance(t, db, "pgmulti-nav-blocked-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	insertPGCredential(t, db, blockedTarget, "discdb", "app", pgClaimCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	navPolicy(t, disc, blockedTarget, "discdb", "app", "orders", "id", "qty") // note uncovered
	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), blockedTarget,
		navRequest("pgmulti-nav-blocked", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
	if err == nil {
		t.Fatal("missing policy must block the attempt")
	}
	if out.ExecutionID == 0 || out.Evidence == nil || out.Evidence.UserSQLSent {
		t.Fatalf("blocked outcome = %+v, want keyed refusal with UserSQLSent false", out)
	}
	rejected := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-nav-blocked' AND status = 'rejected'`,
		blockedTarget)
	if rejected != 1 {
		t.Fatalf("blocked keyed rows = %d, want 1", rejected)
	}
}

// ---------------------------------------------------------------------------
// Delivery gate: an over-width referenced cell persists success with no
// Result for a capability-less client (Relation still populated); a capable
// new key delivers the bounded prefix.
// ---------------------------------------------------------------------------

func TestPGMultiEntryNavigation_DeliveryGate(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-gate-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "blob_target", "id", "payload")

	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-nav-g-nocap", navSource("discdb", "app", "blob_refs", "fk_blob"), []string{"9"}))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("no-cap navigate = %v, want ErrResultContractUpgradeRequired", err)
	}
	if out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess || out.Result != nil || out.Relation == nil {
		t.Fatalf("refused outcome = %+v, want persisted success + relation, no Result", out)
	}
	status := ""
	if err := db.QueryRow(`SELECT status FROM query_executions WHERE id = ?`, out.ExecutionID).Scan(&status); err != nil {
		t.Fatalf("keyed row read: %v", err)
	}
	if status != string(model.QueryExecutionSuccess) {
		t.Fatalf("persisted status = %q, want success — delivery refusal is not an execution failure", status)
	}

	capReq := navRequest("pgmulti-nav-g-cap", navSource("discdb", "app", "blob_refs", "fk_blob"), []string{"9"})
	capReq.Capabilities = []string{service.CapabilityCellTruncated}
	out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, capReq)
	if err != nil {
		t.Fatalf("capable navigate: %v", err)
	}
	if len(out.Result.Rows) != 1 || out.Result.Rows[0][1] == nil || len(*out.Result.Rows[0][1]) != 8192 {
		t.Fatalf("capable rows = %+v, want the 8192-byte bounded prefix", out.Result.Rows)
	}
}

// ---------------------------------------------------------------------------
// Drift injection: a wrapper over the REAL claim repository forwards the
// actual claim insert, then runs controlled admin DDL that drops and
// recreates the FK with a new constraint OID before the attempt binds. The
// in-transaction verify must detect the changed mapping and keyed-reject —
// the locator statement is never sent.
// ---------------------------------------------------------------------------

// navDriftControl carries the injected DDL — the test swaps it between
// calls while the service keeps one wrapper instance.
type navDriftControl struct{ fn func() }

type navDriftExecRepo struct {
	*mysqlrepo.QueryExecutionRepository
	drift *navDriftControl
}

func (r navDriftExecRepo) TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error {
	if err := r.QueryExecutionRepository.TryClaimExecution(ctx, in); err != nil {
		return err
	}
	if r.drift != nil && r.drift.fn != nil {
		r.drift.fn()
	}
	return nil
}

func TestPGMultiEntryNavigation_DriftConstraint(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	ctx := context.Background()

	// Admin DDL runs after the real claim INSERT — between metadata
	// preparation and the attempt's locked recheck.
	driftAdmin := func(stmts ...string) {
		conn, err := pgx.Connect(ctx, pgKeywordDSN(fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword, 5))
		if err != nil {
			t.Errorf("drift admin connect: %s", redactPGMeta(err.Error()))
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		for _, stmt := range stmts {
			if _, err := conn.Exec(ctx, stmt); err != nil {
				t.Errorf("drift ddl %q: %s", stmt, redactPGMeta(err.Error()))
			}
		}
	}
	drift := &navDriftControl{fn: func() {
		// Same target, new constraint OID — the mapping's identity changed.
		driftAdmin(
			`ALTER TABLE app.order_items DROP CONSTRAINT fk_orders`,
			`ALTER TABLE app.order_items ADD CONSTRAINT fk_orders FOREIGN KEY (order_id) REFERENCES app.orders(id)`)
	}}

	realExec := mysqlrepo.NewQueryExecutionRepository(db)
	svc := service.NewQueryExecutionService(
		mysqlrepo.NewQueryTargetRepository(db),
		navDriftExecRepo{QueryExecutionRepository: realExec, drift: drift},
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysqlrepo.NewQueryDisclosureRepository(db),
			mysqlrepo.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			mysqlrepo.NewQueryTargetRepository(db),
		),
	).WithPGClaimOrchestration(mysqlrepo.NewQueryDisclosureRepository(db))
	disc := mysqlrepo.NewQueryDisclosureRepository(db)

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-drift-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")

	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-nav-drift", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
	if err == nil {
		t.Fatal("constraint drift must keyed-reject the attempt")
	}
	if out.ExecutionID == 0 || out.Evidence == nil || out.Evidence.UserSQLSent {
		t.Fatalf("drift outcome = %+v, want keyed rejection with UserSQLSent false", out)
	}
	rejected := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-nav-drift'
		  AND status = 'rejected' AND error_code = 'query_binding_mismatch'`,
		targetID)
	if rejected != 1 {
		t.Fatalf("drift keyed rows = %d, want 1 query_binding_mismatch", rejected)
	}

	// Target swap: the same-named FK is re-pointed at a different physical
	// referenced relation after preparation — the locked recheck compares
	// the whole mapping and must reject before the locator statement sends.
	drift.fn = func() {
		driftAdmin(
			`ALTER TABLE app.order_items DROP CONSTRAINT fk_orders`,
			`ALTER TABLE app.order_items ADD CONSTRAINT fk_orders FOREIGN KEY (order_id) REFERENCES app.orders_alt(id)`)
	}
	out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-nav-drift2", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
	if err == nil {
		t.Fatal("target swap must keyed-reject the attempt")
	}
	if out.ExecutionID == 0 || out.Evidence == nil || out.Evidence.UserSQLSent {
		t.Fatalf("swap outcome = %+v, want keyed rejection with UserSQLSent false", out)
	}
	rejected = countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-nav-drift2'
		  AND status = 'rejected' AND error_code = 'query_binding_mismatch'`,
		targetID)
	if rejected != 1 {
		t.Fatalf("swap keyed rows = %d, want 1 query_binding_mismatch", rejected)
	}
	drift.fn = nil
}

// ---------------------------------------------------------------------------
// Lock protection: a wrapper over the REAL disclosure reader signals on the
// first preflight lookup — at that point the attempt holds ACCESS SHARE on
// both source and target. Admin ALTER TABLE ... DROP CONSTRAINT must wait
// behind that lock (pg_locks shows the ungranted AEL request), and only
// completes after the attempt releases it at cleanup.
// ---------------------------------------------------------------------------

type navBarrierPolicies struct {
	inner      service.QueryDisclosureReader
	first      chan struct{}
	release    chan struct{}
	signalOnce sync.Once
}

func (r *navBarrierPolicies) ListByTarget(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error) {
	return r.inner.ListByTarget(ctx, targetResourceID)
}

func (r *navBarrierPolicies) GetByScope(ctx context.Context, targetResourceID uint64, database, schema, object, column string) (model.ResultDisclosurePolicy, error) {
	r.signalOnce.Do(func() { close(r.first) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return model.ResultDisclosurePolicy{}, ctx.Err()
	}
	return r.inner.GetByScope(ctx, targetResourceID, database, schema, object, column)
}

func TestPGMultiEntryNavigation_LockProtection(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGNavFixture(t)
	setupPGClaimEnv(t, fix)
	ctx := context.Background()

	barrier := &navBarrierPolicies{
		inner:   mysqlrepo.NewQueryDisclosureRepository(db),
		first:   make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := service.NewQueryExecutionService(
		mysqlrepo.NewQueryTargetRepository(db),
		mysqlrepo.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysqlrepo.NewQueryDisclosureRepository(db),
			mysqlrepo.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			mysqlrepo.NewQueryTargetRepository(db),
		),
	).WithPGClaimOrchestration(barrier)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-nav-lock-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")

	type outcome struct {
		out service.PGClaimedNavigationOutcome
		err error
	}
	done := make(chan outcome, 1)
	ddlErr := make(chan error, 1)
	var attemptReturned, ddlStarted, ddlReturned atomic.Bool
	go func() {
		o, e := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-nav-lock", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
		attemptReturned.Store(true)
		done <- outcome{o, e}
	}()
	// Robust cleanup: on any early failure the barrier releases exactly once
	// and every outstanding goroutine is awaited before fixture teardown —
	// nothing is dropped while the attempt still holds locks.
	var releaseOnce sync.Once
	releaseBarrier := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	t.Cleanup(func() {
		releaseBarrier()
		if !attemptReturned.Load() {
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("navigation attempt goroutine never returned during cleanup")
			}
		}
		if ddlStarted.Load() && !ddlReturned.Load() {
			select {
			case <-ddlErr:
			case <-time.After(15 * time.Second):
				t.Error("DDL goroutine never returned during cleanup")
			}
		}
	})

	select {
	case <-barrier.first:
	case <-time.After(30 * time.Second):
		t.Fatal("disclosure preflight never reached — locks not held")
	}

	// Admin DDL on a second connection: ACCESS EXCLUSIVE conflicts with the
	// attempt's ACCESS SHARE — it must wait, then lose to lock_timeout.
	ddlStarted.Store(true)
	go func() {
		defer ddlReturned.Store(true)
		conn, err := pgx.Connect(ctx, pgKeywordDSN(fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword, 5))
		if err != nil {
			ddlErr <- err
			return
		}
		defer func() { _ = conn.Close(context.Background()) }()
		if _, err := conn.Exec(ctx, `SET lock_timeout = '2000ms'`); err != nil {
			ddlErr <- err
			return
		}
		_, err = conn.Exec(ctx, `ALTER TABLE app.order_items DROP CONSTRAINT fk_orders`)
		ddlErr <- err
	}()

	// Prove the DDL is genuinely waiting behind the attempt's lock.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	waited := false
	for !waited && waitCtx.Err() == nil {
		var n int
		if err := admin.QueryRow(waitCtx,
			`SELECT count(*) FROM pg_catalog.pg_locks
			  WHERE relation = (SELECT c.oid FROM pg_catalog.pg_class c
			                     JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			                    WHERE n.nspname = 'app' AND c.relname = 'order_items')
			    AND mode = 'AccessExclusiveLock' AND NOT granted`).Scan(&n); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("pg_locks probe: %v", err)
		}
		waited = n > 0
		if !waited {
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitCancel()
	if !waited {
		t.Fatal("DROP CONSTRAINT never queued behind the attempt's ACCESS SHARE — lock protection absent")
	}

	// Hold the barrier until the bounded DDL actually loses to lock_timeout —
	// the failure lands while the attempt still holds ACCESS SHARE. Only then
	// release the attempt to finish. The error must be PostgreSQL's own
	// lock-not-available code, not an incidental failure.
	select {
	case err := <-ddlErr:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("bounded DDL error = %v, want PgError 55P03 lock_not_available", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("bounded DDL never returned")
	}
	releaseBarrier()
	var res outcome
	select {
	case res = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("navigation attempt did not return")
	}
	if res.err != nil {
		t.Fatalf("navigate: %v", res.err)
	}
	if res.out.Status != model.QueryExecutionSuccess || res.out.Result == nil || len(res.out.Result.Rows) != 1 {
		t.Fatalf("protected outcome = %+v, want successful delivery under held locks", res.out)
	}
	// Locks released at cleanup: the same DROP now completes immediately.
	ddl2, err := pgx.Connect(ctx, pgKeywordDSN(fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword, 5))
	if err != nil {
		t.Fatalf("post-attempt connect: %s", redactPGMeta(err.Error()))
	}
	defer func() { _ = ddl2.Close(context.Background()) }()
	if _, err := ddl2.Exec(ctx, `ALTER TABLE app.order_items DROP CONSTRAINT fk_orders`); err != nil {
		t.Fatalf("post-attempt DROP: %s — attempt locks must be released", redactPGMeta(err.Error()))
	}
}
