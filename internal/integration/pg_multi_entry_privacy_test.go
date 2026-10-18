//go:build integration

// Package integration — privacy proofs for the PG-MULTI-ENTRY claimed
// entries.
// input: disposable PG16 fixture + real MySQL production dependencies; saved
// statements exercising CTE/JOIN/DISTINCT-Q+D shapes and a text-keyed
// navigation FK, each carrying a distinct synthetic marker (template value,
// locator value, stored-record comment)
// output: TestPGMultiEntryPrivacy_* — transient values and generated SQL
// never appear in error unwrap chains, history preview/full/error columns,
// audit fields, claim columns, ListHistory, or the claim point-read JSON;
// the authored comment stays only inside the authorized saved record
// pos: stage 4 packet item 5 — storage privacy across every evidence
// surface for both new entries
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// Distinct synthetic markers — none is a legal value for the column it is
// bound to, so a leak into any evidence surface is unambiguous.
const (
	privParamMarker   = `param-MRK-0417'"; DROP TABLE x; --`
	privLocatorMarker = `locator-MRK-0417"`
	privCommentMarker = `C0MT-MRK-0417`
)

var hexDigestRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// assertNoMarker fails when any marker string appears in the blob.
func assertNoMarker(t *testing.T, where, blob string, markers ...string) {
	t.Helper()
	for _, m := range markers {
		if strings.Contains(blob, m) {
			t.Fatalf("%s leaked marker %q", where, m)
		}
	}
}

// unwrapChain concatenates every message in the error's unwrap chain — the
// scan covers what callers could observe by unwrapping, not just Error().
// Both unwrap shapes are followed recursively: the single-error
// Unwrap() error and the multi-error Unwrap() []error (errors.Join and
// friends), so no descendant message escapes the scan.
func unwrapChain(err error) string {
	var sb strings.Builder
	var walk func(e error)
	walk = func(e error) {
		if e == nil {
			return
		}
		sb.WriteString(e.Error())
		sb.WriteByte('\n')
		if multi, ok := e.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				walk(child)
			}
			return
		}
		walk(errors.Unwrap(e))
	}
	walk(err)
	return sb.String()
}

// scanExecutionRows reads every text column of the target's persisted
// execution rows — privacy must hold on all of them.
func scanExecutionRows(t *testing.T, db *sql.DB, targetID uint64) []string {
	t.Helper()
	rows, err := db.Query(
		`SELECT statement_digest, statement_preview, COALESCE(full_statement,''),
		        status, error_code, error_message, database_name, schema_name
		 FROM query_executions WHERE target_resource_id = ?`, targetID)
	if err != nil {
		t.Fatalf("scan executions: %v", err)
	}
	defer rows.Close()
	var blobs []string
	for rows.Next() {
		var digest, preview, full, status, code, msg, dbName, schema string
		if err := rows.Scan(&digest, &preview, &full, &status, &code, &msg, &dbName, &schema); err != nil {
			t.Fatalf("scan executions row: %v", err)
		}
		blobs = append(blobs, digest+preview+full+status+code+msg+dbName+schema)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scan executions rows: %v", err)
	}
	return blobs
}

// scanClaimRows reads the claim bookkeeping columns for a target — digest
// must be hex-only and nothing else may carry markers.
func scanClaimRows(t *testing.T, db *sql.DB, targetID uint64) {
	t.Helper()
	rows, err := db.Query(
		`SELECT client_execution_id, database_name, schema_name, request_digest
		 FROM query_execution_claims WHERE target_resource_id = ?`, targetID)
	if err != nil {
		t.Fatalf("scan claims: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, dbName, schema, digest string
		if err := rows.Scan(&key, &dbName, &schema, &digest); err != nil {
			t.Fatalf("scan claims row: %v", err)
		}
		if !hexDigestRe.MatchString(digest) {
			t.Fatalf("claim digest %q is not hex-only", digest)
		}
		assertNoMarker(t, "claim row", key+dbName+schema+digest,
			privParamMarker, privLocatorMarker, privCommentMarker)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scan claims rows: %v", err)
	}
}

// scanAuditRows concatenates this target's audit event fields.
func scanAuditRows(t *testing.T, db *sql.DB, targetID uint64) {
	t.Helper()
	rows, err := db.Query(
		`SELECT event_type, result FROM audit_events WHERE target_resource_id = ?`, targetID)
	if err != nil {
		t.Fatalf("scan audit: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ, res string
		if err := rows.Scan(&typ, &res); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		assertNoMarker(t, "audit row", typ+res,
			privParamMarker, privLocatorMarker, privCommentMarker)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scan audit rows: %v", err)
	}
}

// The real saved entry over a CTE + JOIN + DISTINCT (Q+D) template: the
// authored comment lives in the saved record only; the parameter marker
// value is a transient input that must never reach an evidence surface. A
// second saved statement produces a real parameter rejection so the error
// unwrap chain and persisted error columns are scanned too.
func TestPGMultiEntryPrivacy_Saved(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGNavFixtureData(t, fix)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-priv-s-"+strings.ReplaceAll(t.Name(), "/", "-"))
	for _, col := range []string{"id", "qty", "note"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "orders", col, model.ResultDisclosureRawCopyAllowed)
	}
	for _, col := range []string{"id", "order_id"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "order_items", col, model.ResultDisclosureRawCopyAllowed)
	}

	// CTE body + JOIN condition + DISTINCT over the CTE — the Q+D rewrite
	// duplicates the predicate legally; the marker rides as a bound value.
	stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "joined filtered",
		Statement: `WITH j AS (
  SELECT o.id FROM app.orders o
  JOIN app.order_items i ON i.order_id = o.id
  WHERE o.note <> :v::text
)
SELECT DISTINCT j.id FROM j ORDER BY 1 /* ` + privCommentMarker + ` */`,
		Scope: model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "v", Type: model.QuerySavedStatementParameterString},
		},
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
		savedRequest("pgmulti-priv-s1", map[string]json.RawMessage{
			"v": privMarkerJSON(t, privParamMarker),
		}))
	if err != nil {
		t.Fatalf("saved execute: %v", err)
	}
	if out.ExecutionID == 0 || out.Evidence == nil || !out.Evidence.UserSQLSent {
		t.Fatalf("saved outcome = %+v, want sent", out)
	}
	assertNoMarker(t, "saved result", mustJSON(t, out.Result), privParamMarker)

	// The authored comment legitimately remains in the authorized saved
	// record — and nowhere else.
	stored, err := mysqlrepo.NewQuerySavedStatementRepository(db).GetByID(ctx, targetID, stmt.ID)
	if err != nil || !strings.Contains(stored.Statement, privCommentMarker) {
		t.Fatalf("saved record lost its authored comment: %v %q", err, stored.Statement)
	}

	// A real parameter rejection — scan the whole unwrap chain.
	bad := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "bad cast",
		Statement: `SELECT :x::bigint`,
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "x", Type: model.QuerySavedStatementParameterString},
		},
	})
	_, perr := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, bad.ID,
		savedRequest("pgmulti-priv-s2", map[string]json.RawMessage{
			"x": privMarkerJSON(t, privParamMarker),
		}))
	if perr == nil {
		t.Fatal("bad-cast call must fail")
	}
	assertNoMarker(t, "error unwrap chain", unwrapChain(perr), privParamMarker, privCommentMarker)

	// Persisted evidence surfaces.
	for _, blob := range scanExecutionRows(t, db, targetID) {
		assertNoMarker(t, "execution row", blob, privParamMarker, privLocatorMarker, privCommentMarker)
	}
	scanClaimRows(t, db, targetID)
	scanAuditRows(t, db, targetID)

	// ListHistory JSON and the claim point-read JSON.
	page, err := svc.ListHistory(ctx, userIdentity(pgSavedOwner), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	assertNoMarker(t, "ListHistory json", mustJSON(t, page.Items),
		privParamMarker, privLocatorMarker, privCommentMarker)
	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "pgmulti-priv-s1", userIdentity(pgSavedOwner))
	if err != nil || view.Execution == nil {
		t.Fatalf("point read: %v %+v", err, view)
	}
	assertNoMarker(t, "claim view json", mustJSON(t, view),
		privParamMarker, privLocatorMarker, privCommentMarker)
	if view.Execution.ActorUserID != pgSavedOwner {
		t.Fatalf("actor = %d, want user %d", view.Execution.ActorUserID, pgSavedOwner)
	}
	for _, it := range page.Items {
		if it.ID != nil && *it.ID == view.Execution.ID &&
			it.Actor.Kind != model.QueryExecutionActorUser {
			t.Fatalf("list item actor = %+v, want user kind", it.Actor)
		}
	}
	// The point view doesn't project the digest — assert the stored column
	// is hex-only directly.
	var digest string
	if err := db.QueryRow(
		`SELECT statement_digest FROM query_executions WHERE id = ?`, view.Execution.ID).Scan(&digest); err != nil {
		t.Fatalf("digest read: %v", err)
	}
	if !hexDigestRe.MatchString(digest) {
		t.Fatalf("execution digest %q is not hex-only", digest)
	}

	// Same statement name stored under an authorized alternate database —
	// the request never carries database/schema; the record pins them.
	createPGSecondDB(t, fix, "privaltdb")
	admin2 := connectPG(t, fix.host, fix.port, "privaltdb", pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, admin2, `CREATE TABLE app.alt_t (id bigint PRIMARY KEY, v text)`)
	execPG(t, admin2, `INSERT INTO app.alt_t VALUES (1, 'alt-row')`)
	execPG(t, admin2, `GRANT SELECT ON app.alt_t TO `+pgDiscRole)
	_ = admin2.Close(context.Background())
	insertPGCredential(t, db, targetID, "privaltdb", "app", "PG_PRIV_ALT", true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_PRIV_ALT",
		pgKeywordDSN(fix.host, fix.port, "privaltdb", pgDiscRole, pgStmtPassword, 5))
	insertPolicy(t, disc, targetID, "privaltdb", "app", "alt_t", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "privaltdb", "app", "alt_t", "v", model.ResultDisclosureRawCopyAllowed)
	alt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "joined filtered", // same name, alternate authorized database
		Statement: `SELECT id, v FROM app.alt_t ORDER BY id`,
		Scope:     model.QuerySavedStatementPersonal,
		Database:  "privaltdb",
	})
	altOut, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, alt.ID,
		savedRequest("pgmulti-priv-s3", nil))
	if err != nil {
		t.Fatalf("alternate-db saved execute: %v", err)
	}
	if len(altOut.Result.Rows) != 1 || *altOut.Result.Rows[0][1] != "alt-row" {
		t.Fatalf("alternate-db rows = %+v, want the alt database's row", altOut.Result.Rows)
	}
}

// The navigation entry: the locator marker is a real request value (legal
// no-match) that must never persist in evidence surfaces, and the generated
// SELECT/WHERE text never appears anywhere either.
func TestPGMultiEntryPrivacy_Navigation(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-priv-n-"+strings.ReplaceAll(t.Name(), "/", "-"))
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.priv_target (k text PRIMARY KEY, note text)`,
		`INSERT INTO app.priv_target VALUES ('k1', 'n1')`,
		`CREATE TABLE app.priv_src (id bigint PRIMARY KEY, k text,
		    CONSTRAINT fk_priv FOREIGN KEY (k) REFERENCES app.priv_target(k))`,
		`GRANT SELECT ON app.priv_target TO ` + pgDiscRole,
		`GRANT SELECT ON app.priv_src TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	_ = admin.Close(context.Background())
	navPolicy(t, disc, targetID, "discdb", "app", "priv_target", "k", "note")

	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-priv-n1", navSource("discdb", "app", "priv_src", "fk_priv"), []string{privLocatorMarker}))
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if out.ExecutionID == 0 || out.Evidence == nil || !out.Evidence.UserSQLSent {
		t.Fatalf("nav outcome = %+v, want sent", out)
	}
	assertNoMarker(t, "nav result", mustJSON(t, out.Result), privLocatorMarker)

	for _, blob := range scanExecutionRows(t, db, targetID) {
		assertNoMarker(t, "execution row", blob, privLocatorMarker)
	}
	scanClaimRows(t, db, targetID)
	scanAuditRows(t, db, targetID)
	page, err := svc.ListHistory(ctx, userIdentity(claimUserActor), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	assertNoMarker(t, "ListHistory json", mustJSON(t, page.Items), privLocatorMarker)
	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "pgmulti-priv-n1", userIdentity(claimUserActor))
	if err != nil || view.Execution == nil {
		t.Fatalf("point read: %v %+v", err, view)
	}
	assertNoMarker(t, "claim view json", mustJSON(t, view), privLocatorMarker)
	if view.Execution.ActorUserID != claimUserActor {
		t.Fatalf("actor = %d, want user %d", view.Execution.ActorUserID, claimUserActor)
	}
	for _, it := range page.Items {
		if it.ID != nil && *it.ID == view.Execution.ID &&
			it.Actor.Kind != model.QueryExecutionActorUser {
			t.Fatalf("list item actor = %+v, want user kind", it.Actor)
		}
	}
}

// The real HTTP read path over the same evidence: GET the execution list
// and the owner-only statement detail through the production router/auth
// chain. Neither response may carry transient markers, compiled or
// generated SQL, or a restorable statement — saved/navigation rows persist
// the fixed withheld preview and an empty full statement.
func TestPGMultiEntryPrivacy_HTTPRead(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGNavFixtureData(t, fix)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-priv-http-"+strings.ReplaceAll(t.Name(), "/", "-"))
	for _, col := range []string{"id", "qty", "note"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "orders", col, model.ResultDisclosureRawCopyAllowed)
	}
	navPolicy(t, disc, targetID, "discdb", "app", "tag_target", "tag")

	// A real owner account: saved/navigation executions run under its
	// identity so the statement-detail endpoint sees it as the actor.
	ownerID := insertAuthzTestUser(t, db, "pgmulti-priv-http-owner@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, ownerID) })
	peerID := insertAuthzTestUser(t, db, "pgmulti-priv-http-peer@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, peerID) })

	stmt := createSavedPG(t, db, ownerID, targetID, model.QuerySavedStatementCreateRequest{
		Name: "http marked",
		Statement: `SELECT upper(:v::text) AS out FROM app.orders WHERE id = 1
			/* ` + privCommentMarker + ` */`,
		Scope: model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "v", Type: model.QuerySavedStatementParameterString},
		},
	})
	savedOut, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(ownerID), targetID, stmt.ID,
		savedRequest("pgmulti-priv-http-s", map[string]json.RawMessage{
			"v": privMarkerJSON(t, privParamMarker),
		}))
	if err != nil || savedOut.ExecutionID == 0 {
		t.Fatalf("saved execute: %v %+v", err, savedOut)
	}
	navOut, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(ownerID), targetID,
		navRequest("pgmulti-priv-http-n", navSource("discdb", "app", "tag_refs", "fk_tag"),
			[]string{privLocatorMarker}))
	if err != nil || navOut.ExecutionID == 0 {
		t.Fatalf("nav execute: %v %+v", err, navOut)
	}

	router := api.NewRouter(api.Dependencies{
		AuthService:                    service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuthAuditEmitter:               mysqlrepo.NewAuthAuditEmitter(db),
		QueryExecutionAuth:             api.QueryExecutionAuthConfig{Clock: time.Now},
		QueryExecutionService:          svc,
		QueryExecutionStatementService: svc,
	})
	ownerToken := mustLogin(t, router, "pgmulti-priv-http-owner@example.com", "secret123")
	peerToken := mustLogin(t, router, "pgmulti-priv-http-peer@example.com", "secret123")

	// GET the execution list through the real router/auth chain.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/query-targets/%d/executions?database=discdb", targetID), nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoMarker(t, "GET executions body", body, privParamMarker, privLocatorMarker, privCommentMarker)
	for _, frag := range []string{"__chub_", "SELECT", "WHERE", "JOIN", "upper(", "repeat(", "fullStatement"} {
		if strings.Contains(body, frag) {
			t.Fatalf("GET executions body leaked generated/compiled SQL fragment %q", frag)
		}
	}
	var page struct {
		Items []struct {
			ClientExecutionID *string `json:"clientExecutionId"`
			StatementPreview  string  `json:"statementPreview"`
			CanRestore        bool    `json:"canRestore"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	seen := map[string]bool{}
	for _, it := range page.Items {
		if it.ClientExecutionID == nil {
			continue
		}
		key := *it.ClientExecutionID
		if key == "pgmulti-priv-http-s" || key == "pgmulti-priv-http-n" {
			seen[key] = true
			if it.StatementPreview != "[statement withheld]" {
				t.Fatalf("%s preview = %q, want the fixed withheld preview", key, it.StatementPreview)
			}
			if it.CanRestore {
				t.Fatalf("%s canRestore = true — a withheld statement must not be restorable", key)
			}
		}
	}
	if !seen["pgmulti-priv-http-s"] || !seen["pgmulti-priv-http-n"] {
		t.Fatalf("list missing keyed rows: %+v", seen)
	}

	// Owner-only statement detail: a withheld row persists no full
	// statement, so the detail endpoint cannot restore one — the owner AND
	// the peer both get the fixed not-found envelope, never statement text,
	// markers, or generated SQL.
	get := func(token string, execID uint64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/query-targets/%d/executions/%d/statement", targetID, execID), nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		name string
		id   uint64
	}{
		{"saved", savedOut.ExecutionID},
		{"navigation", navOut.ExecutionID},
	} {
		for who, token := range map[string]string{"owner": ownerToken, "peer": peerToken} {
			w := get(token, tc.id)
			if w.Code != http.StatusNotFound {
				t.Fatalf("%s statement detail for %s = %d %s — withheld statements must not restore",
					tc.name, who, w.Code, w.Body.String())
			}
			assertNoMarker(t, tc.name+" "+who+" statement body", w.Body.String(),
				privParamMarker, privLocatorMarker, privCommentMarker)
			for _, frag := range []string{"__chub_", "SELECT", "WHERE", "upper("} {
				if strings.Contains(w.Body.String(), frag) {
					t.Fatalf("%s %s statement body leaked generated/compiled SQL fragment %q", tc.name, who, frag)
				}
			}
		}
	}
}

// privMarkerJSON encodes the marker as a JSON string — the markers carry
// quotes deliberately, so literal interpolation would be invalid.
func privMarkerJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}
	return b
}
