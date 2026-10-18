//go:build integration

// Package integration — real PG16 + real MySQL vertical for the keyed
// saved-statement entry (PG-MULTI-ENTRY stage 2, issue #118 + #119).
// input: shared MySQL fixture (real target/credential/claim/execution/saved
// repositories), a disposable PostgreSQL 16 with the restricted ro_disc
// role, and the production ExecutePGSavedStatementClaimed entry
// output: TestPGMultiEntrySaved_* — vertical flow over static and
// parameterized templates, scope/ownership denials, per-call re-read,
// pre-claim value rejections, native-$n and LIMIT :n refusal, pinned-schema
// and connection-context enforcement, withheld statement persistence, the
// post-finalize delivery gate, finalize-failure precedence, and the
// update-after-claim compiled-snapshot race control
// pos: the saved entry converges on the same claim/finalize/delivery runner
// as the ordinary entry; these tests drive the production chain end to end —
// no manual claim/finalize composition, no fake outcome function
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// pgSavedOwner/pgSavedPeer are caller identities — owner_user_id has no FK,
// so arbitrary ids exercise the ownership rule under the real store.
const (
	pgSavedOwner uint64 = 770001
	pgSavedPeer  uint64 = 770002
)

// newPGSavedClaimedService wires the production keyed-PG service with the
// saved-statement reader and compiler — the same setters cmd/server chains.
func newPGSavedClaimedService(db *sql.DB) *service.QueryExecutionService {
	return newPGClaimedService(db).
		WithTemplateExecution(mysqlrepo.NewQuerySavedStatementRepository(db), service.NewTemplateStatementCompiler())
}

func createSavedPG(t *testing.T, db *sql.DB, ownerID, targetID uint64, req model.QuerySavedStatementCreateRequest) model.QuerySavedStatement {
	t.Helper()
	if req.Database == "" {
		req.Database = "discdb"
	}
	saved, err := mysqlrepo.NewQuerySavedStatementRepository(db).
		CreateWithAudit(context.Background(), ownerID, targetID, req)
	if err != nil {
		t.Fatalf("create saved statement: %v", err)
	}
	return saved
}

func savedRequest(key string, values map[string]json.RawMessage) model.QuerySavedStatementExecuteRequest {
	return model.QuerySavedStatementExecuteRequest{ClientExecutionID: key, Values: values}
}

// keyedSavedRow returns the persisted terminal row for a key — status and
// error_code — or empty strings when nothing landed.
func keyedSavedRow(t *testing.T, db *sql.DB, targetID uint64, key string) (status, code string) {
	t.Helper()
	err := db.QueryRow(
		`SELECT status, COALESCE(error_code, '') FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`,
		targetID, key).Scan(&status, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ""
	}
	if err != nil {
		t.Fatalf("keyed row read: %v", err)
	}
	return status, code
}

// ---------------------------------------------------------------------------
// Vertical: real saved statement → authorized read → compile → claim →
// governed attempt → keyed finalize → linked point read. Static and
// parameterized templates; parameter values never reach history.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_Vertical(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-vert-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "note", model.ResultDisclosureRawCopyAllowed)

	// Static template, blank schema → the credential's default_schema "app".
	static := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "orders page",
		Statement: `SELECT id, note FROM app.orders ORDER BY id`,
		Scope:     model.QuerySavedStatementPersonal,
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, static.ID,
		savedRequest("pgmulti-saved-vert-static", nil))
	if err != nil {
		t.Fatalf("static execute: %v", err)
	}
	if out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess || out.Result == nil {
		t.Fatalf("static outcome = %+v", out)
	}
	if len(out.Result.Rows) != 2 || *out.Result.Rows[0][0] != "1" || *out.Result.Rows[0][1] != "n1" ||
		out.Result.Rows[1][1] != nil {
		t.Fatalf("static rows = %+v", out.Result.Rows)
	}
	if out.Evidence == nil || out.Evidence.BackendPID == 0 || !out.Evidence.UserSQLSent {
		t.Fatalf("static evidence = %+v, want real backend pid + sent", out.Evidence)
	}

	// Saved rows never carry statement content: withheld preview, no full
	// statement — success included. The point view doesn't project the
	// statement columns, so they are read off the history table directly.
	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "pgmulti-saved-vert-static", userIdentity(pgSavedOwner))
	if err != nil || view.Execution == nil {
		t.Fatalf("point read: %v %+v", err, view)
	}
	if view.Execution.ID != out.ExecutionID || view.Claim.ExecutionID != out.ExecutionID {
		t.Fatalf("link mismatch: claim=%+v exec=%+v", view.Claim, view.Execution)
	}
	var preview, full, digest string
	if err := db.QueryRow(
		`SELECT statement_preview, COALESCE(full_statement,''), statement_digest FROM query_executions WHERE id = ?`,
		out.ExecutionID).Scan(&preview, &full, &digest); err != nil {
		t.Fatalf("keyed row read: %v", err)
	}
	if preview != "[statement withheld]" || full != "" || len(digest) != 64 {
		t.Fatalf("saved keyed row preview=%q full=%q digest=%q — nothing statement-derived may persist",
			preview, full, digest)
	}
	if view.Execution.Status != model.QueryExecutionSuccess {
		t.Fatalf("saved keyed row status = %v, want success", view.Execution.Status)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "pgmulti-saved-vert-static"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("static history/audit/link = %d/%d/%d", execs, audits, linked)
	}

	// Parameterized template — declaration order deliberately differs from
	// occurrence order; the four types ride one execution.
	typed := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "typed pick",
		Statement: `SELECT :txt::text AS txt, :big::bigint AS big, :num::numeric AS num, :flag::boolean AS flag`,
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "flag", Type: model.QuerySavedStatementParameterBoolean},
			{Name: "txt", Type: model.QuerySavedStatementParameterString},
			{Name: "num", Type: model.QuerySavedStatementParameterDecimal},
			{Name: "big", Type: model.QuerySavedStatementParameterInteger},
		},
	})

	// Quotes, backslashes, CJK, and SQL-looking text all travel as data.
	hostile := `it's a "quoted" \ path — 中文 — '; DROP TABLE app.orders; --`
	values := func(big int64, flag bool) map[string]json.RawMessage {
		return map[string]json.RawMessage{
			"txt":  json.RawMessage(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(hostile) + `"`),
			"big":  json.RawMessage(strconv.FormatInt(big, 10)),
			"num":  json.RawMessage(`"12345.67890123456789"`),
			"flag": json.RawMessage(boolText(flag)),
		}
	}

	checkTyped := func(key string, big int64, flag bool, wantBig, wantFlag string) {
		t.Helper()
		out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, typed.ID,
			savedRequest(key, values(big, flag)))
		if err != nil {
			t.Fatalf("typed execute %s: %v", key, err)
		}
		row := out.Result.Rows[0]
		if *row[0] != hostile {
			t.Fatalf("txt round trip = %q", *row[0])
		}
		if *row[1] != wantBig || *row[2] != "12345.67890123456789" || *row[3] != wantFlag {
			t.Fatalf("typed row = %q/%q/%q", *row[1], *row[2], *row[3])
		}
		if !out.Evidence.UserSQLSent || out.Evidence.BackendPID == 0 {
			t.Fatalf("typed evidence = %+v", out.Evidence)
		}
		v, rerr := repo.GetClaimWithExecution(ctx, targetID, "discdb", key, userIdentity(pgSavedOwner))
		if rerr != nil || v.Execution == nil || v.Execution.ID != out.ExecutionID {
			t.Fatalf("typed keyed row = %+v err=%v", v, rerr)
		}
		var preview, full string
		if err := db.QueryRow(
			`SELECT statement_preview, COALESCE(full_statement,'') FROM query_executions WHERE id = ?`,
			out.ExecutionID).Scan(&preview, &full); err != nil {
			t.Fatalf("keyed row read: %v", err)
		}
		if preview != "[statement withheld]" || full != "" {
			t.Fatalf("typed keyed row preview=%q full=%q, want withheld + empty", preview, full)
		}
	}
	checkTyped("pgmulti-saved-vert-i64min", -9223372036854775808, true, "-9223372036854775808", "t")
	checkTyped("pgmulti-saved-vert-i64max", 9223372036854775807, false, "9223372036854775807", "f")
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// Ownership and scope: a personal statement is invisible to every other
// user — identity carries no admin bypass — while a shared template executes
// under the caller's own identity.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_ScopeOwnership(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-scope-"+strings.ReplaceAll(t.Name(), "/", "-"))

	personal := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "mine only", Statement: `SELECT 1 AS x`, Scope: model.QuerySavedStatementPersonal,
	})
	// Another user is denied. The entry sees only a user identity — it
	// resolves no roles, so the denial is identical for a caller that would
	// hold admin privileges elsewhere; nothing here authenticates an admin.
	_, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedPeer), targetID, personal.ID,
		savedRequest("pgmulti-saved-scope-peer", nil))
	if !errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		t.Fatalf("non-owner execute = %v, want saved_statement_not_found", err)
	}
	if claimOccupied(t, db, targetID, "pgmulti-saved-scope-peer") {
		t.Fatal("denial occupied the key")
	}
	nullDenied := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL AND status = 'rejected' AND error_code = 'saved_statement_not_found'`,
		targetID)
	if nullDenied != 1 {
		t.Fatalf("NULL-key denied rows = %d, want 1", nullDenied)
	}

	// A shared template runs for a non-owner — attributed to the caller.
	shared := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "shared pick", Statement: `SELECT 7 AS x`, Scope: model.QuerySavedStatementSharedTemplate,
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedPeer), targetID, shared.ID,
		savedRequest("pgmulti-saved-scope-shared", nil))
	if err != nil || *out.Result.Rows[0][0] != "7" {
		t.Fatalf("shared execute: out=%+v err=%v", out, err)
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-saved-scope-shared' AND actor_user_id = ?`,
		targetID, pgSavedPeer); got != 1 {
		t.Fatalf("shared row actor = %d, want caller attribution", got)
	}
}

// ---------------------------------------------------------------------------
// Per-call re-read: the statement is re-authorized and re-read for every
// execution, so an update is visible to the next key and a delete denies.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_ReRead(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	repo := mysqlrepo.NewQuerySavedStatementRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-reread-"+strings.ReplaceAll(t.Name(), "/", "-"))

	saved := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "evolving", Statement: `SELECT 1 AS v`, Scope: model.QuerySavedStatementPersonal,
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-rr-one", nil))
	if err != nil || *out.Result.Rows[0][0] != "1" {
		t.Fatalf("first execute: out=%+v err=%v", out, err)
	}

	if err := repo.UpdateWithAudit(ctx, pgSavedOwner, targetID, saved.ID, model.QuerySavedStatementUpdateRequest{
		Name: "evolving", Statement: `SELECT 2 AS v`, Database: "discdb",
	}, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-rr-two", nil))
	if err != nil || *out.Result.Rows[0][0] != "2" {
		t.Fatalf("post-update execute: out=%+v err=%v", out, err)
	}

	if err := repo.DeleteWithAudit(ctx, pgSavedOwner, targetID, saved.ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-rr-three", nil))
	if !errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		t.Fatalf("post-delete execute = %v, want saved_statement_not_found", err)
	}
}

// ---------------------------------------------------------------------------
// Typed-value refusals land before claim: the key never occupies, the
// NULL-key rejected pair writes, and raw values appear nowhere.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_PreClaimValues(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-values-"+strings.ReplaceAll(t.Name(), "/", "-"))

	saved := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "typed gate",
		Statement: `SELECT :n::bigint AS n, :s::text AS s`,
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "n", Type: model.QuerySavedStatementParameterInteger},
			{Name: "s", Type: model.QuerySavedStatementParameterString},
		},
	})

	cases := map[string]map[string]json.RawMessage{
		"missing":    {"s": json.RawMessage(`"x"`)},
		"unknown":    {"n": json.RawMessage(`1`), "s": json.RawMessage(`"x"`), "z": json.RawMessage(`1`)},
		"json null":  {"n": json.RawMessage(`1`), "s": json.RawMessage(`null`)},
		"wrong type": {"n": json.RawMessage(`"not-int"`), "s": json.RawMessage(`"x"`)},
		"oversized":  {"n": json.RawMessage(`1`), "s": json.RawMessage(`"` + strings.Repeat("x", 5000) + `"`)},
	}
	for name, values := range cases {
		key := "pgmulti-saved-val-" + name
		_, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
			savedRequest(key, values))
		var valueErr *service.TemplateValueValidationError
		if !errors.As(err, &valueErr) {
			t.Fatalf("%s = %v, want TemplateValueValidationError", name, err)
		}
		if claimOccupied(t, db, targetID, key) {
			t.Fatalf("%s occupied the key", name)
		}
	}
	// The refused attempts wrote exactly one NULL-key rejected row each.
	nullRejections := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL AND status = 'rejected' AND error_code = 'validation_failed'`,
		targetID)
	if nullRejections != len(cases) {
		t.Fatalf("NULL-key validation rejections = %d, want %d", nullRejections, len(cases))
	}
}

// ---------------------------------------------------------------------------
// Source restrictions: a stored native $n is a pre-claim refusal; a stored
// `LIMIT :n` compiles but the keyed pagination gate rejects it inside the
// attempt — after claim, before any user byte reaches the wire.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_NativeAndLimit(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-native-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	native := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "native", Statement: `SELECT $1::text`, Scope: model.QuerySavedStatementPersonal,
	})
	_, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, native.ID,
		savedRequest("pgmulti-saved-nat", nil))
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("native $n execute = %v, want validation_failed", err)
	}
	if claimOccupied(t, db, targetID, "pgmulti-saved-nat") {
		t.Fatal("native $n occupied the key")
	}

	limited := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "bound limit",
		Statement: `SELECT o.id FROM app.orders o ORDER BY o.id LIMIT :n`,
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "n", Type: model.QuerySavedStatementParameterInteger},
		},
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, limited.ID,
		savedRequest("pgmulti-saved-lim", map[string]json.RawMessage{"n": json.RawMessage(`5`)}))
	if err == nil {
		t.Fatal("LIMIT :n must be refused")
	}
	if !claimOccupied(t, db, targetID, "pgmulti-saved-lim") {
		t.Fatal("LIMIT :n refusal is post-claim — the key must stay occupied")
	}
	status, code := keyedSavedRow(t, db, targetID, "pgmulti-saved-lim")
	if status != string(model.QueryExecutionRejected) || code != "unsupported_limit_offset_form" {
		t.Fatalf("keyed row = %s/%s, want rejected/unsupported_limit_offset_form", status, code)
	}
	if out.Evidence != nil && out.Evidence.UserSQLSent {
		t.Fatal("user SQL must never reach the wire on the pagination refusal")
	}
}

// ---------------------------------------------------------------------------
// Execution context: blank schema resolves the connection default, an
// explicit schema is used verbatim, and the saved entry always proves pinned
// existence/USAGE in-transaction — a missing or unusable pinned schema is a
// keyed rejection even for a constant statement. Disabled, deleted, or
// rebound connections deny before claim with no fallback.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_SchemaAndConnection(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-schema-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "analytics", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	// Explicit schema "analytics" — the unqualified reference resolves there.
	explicit := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "analytics pick", Statement: `SELECT id FROM orders`, Schema: "analytics",
		Scope: model.QuerySavedStatementPersonal,
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, explicit.ID,
		savedRequest("pgmulti-saved-sc-explicit", nil))
	if err != nil || *out.Result.Rows[0][0] != "1" {
		t.Fatalf("explicit schema execute: out=%+v err=%v", out, err)
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pgmulti-saved-sc-explicit' AND schema_name = 'analytics'`,
		targetID); got != 1 {
		t.Fatalf("explicit schema rows = %d, want 1 under analytics", got)
	}

	// A pinned schema that does not exist is a keyed rejection — the saved
	// entry proves it even though the statement is constant.
	missing := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "ghost schema", Statement: `SELECT 1 AS x`, Schema: "ghost",
		Scope: model.QuerySavedStatementPersonal,
	})
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, missing.ID,
		savedRequest("pgmulti-saved-sc-ghost", nil))
	if err == nil {
		t.Fatal("missing pinned schema must refuse")
	}
	status, code := keyedSavedRow(t, db, targetID, "pgmulti-saved-sc-ghost")
	if status != string(model.QueryExecutionRejected) || code != "schema_not_found" {
		t.Fatalf("ghost keyed row = %s/%s, want rejected/schema_not_found", status, code)
	}

	// noaccess exists in pg_namespace but the role holds no USAGE.
	noused := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "no usage", Statement: `SELECT 1 AS x`, Schema: "noaccess",
		Scope: model.QuerySavedStatementPersonal,
	})
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, noused.ID,
		savedRequest("pgmulti-saved-sc-nousage", nil))
	if err == nil {
		t.Fatal("unusable pinned schema must refuse")
	}
	status, code = keyedSavedRow(t, db, targetID, "pgmulti-saved-sc-nousage")
	if status != string(model.QueryExecutionRejected) || code != "query_schema_not_usable" {
		t.Fatalf("noaccess keyed row = %s/%s, want rejected/query_schema_not_usable", status, code)
	}

	// Disabled connection row: pre-claim NULL-key rejection, key free.
	disabledTarget := createPGInstance(t, db, "pgmulti-saved-dis-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	insertPGCredential(t, db, disabledTarget, "discdb", "app", pgClaimCredRef, false, string(model.QueryEnvPolicyNonProdOnly))
	stmt := createSavedPG(t, db, pgSavedOwner, disabledTarget, model.QuerySavedStatementCreateRequest{
		Name: "dead conn", Statement: `SELECT 1`, Scope: model.QuerySavedStatementPersonal,
	})
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), disabledTarget, stmt.ID,
		savedRequest("pgmulti-saved-sc-disabled", nil))
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("disabled connection = %v, want not_allowed", err)
	}
	if claimOccupied(t, db, disabledTarget, "pgmulti-saved-sc-disabled") {
		t.Fatal("disabled connection occupied the key")
	}

	// Deleted connection row: no (target, database) credential — fail closed,
	// never a fallback to another database.
	deletedTarget := createPGInstance(t, db, "pgmulti-saved-del-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	stmt = createSavedPG(t, db, pgSavedOwner, deletedTarget, model.QuerySavedStatementCreateRequest{
		Name: "no conn", Statement: `SELECT 1`, Scope: model.QuerySavedStatementPersonal,
	})
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), deletedTarget, stmt.ID,
		savedRequest("pgmulti-saved-sc-deleted", nil))
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("deleted connection = %v, want not_allowed", err)
	}
	if claimOccupied(t, db, deletedTarget, "pgmulti-saved-sc-deleted") {
		t.Fatal("deleted connection occupied the key")
	}

	// Rebound: the credential's DSN points at a different port — binding
	// validation refuses, no fallback.
	reboundTarget := createPGInstance(t, db, "pgmulti-saved-reb-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	insertPGCredential(t, db, reboundTarget, "discdb", "app", "PG_PGMULTI_SAVED_REBOUND", true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_PGMULTI_SAVED_REBOUND",
		pgKeywordDSN(fix.host, fix.port+1, fix.db, pgDiscRole, pgStmtPassword, 5))
	stmt = createSavedPG(t, db, pgSavedOwner, reboundTarget, model.QuerySavedStatementCreateRequest{
		Name: "rebound", Statement: `SELECT 1`, Scope: model.QuerySavedStatementPersonal,
	})
	_, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), reboundTarget, stmt.ID,
		savedRequest("pgmulti-saved-sc-rebound", nil))
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("rebound connection = %v, want not_allowed", err)
	}
	if claimOccupied(t, db, reboundTarget, "pgmulti-saved-sc-rebound") {
		t.Fatal("rebound connection occupied the key")
	}
}

// ---------------------------------------------------------------------------
// Delivery gate through the saved entry: an over-width cell persists the
// success pair but returns no rows without the capability; a capable fresh
// key delivers the prefix + matrix; a same-key retry is already-exists.
// ---------------------------------------------------------------------------

func TestPGMultiEntrySaved_DeliveryGate(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-gate-"+strings.ReplaceAll(t.Name(), "/", "-"))

	saved := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "wide cell", Statement: `SELECT repeat('z', 9000) AS v`, Scope: model.QuerySavedStatementPersonal,
	})

	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-g-nocap", nil))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("no-cap err = %v, want result_contract_upgrade_required", err)
	}
	if out.Result != nil || out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess {
		t.Fatalf("no-cap outcome = %+v, want success-without-rows", out)
	}
	view, rerr := repo.GetClaimWithExecution(ctx, targetID, "discdb", "pgmulti-saved-g-nocap", userIdentity(pgSavedOwner))
	if rerr != nil || view.Execution == nil || view.Execution.Status != model.QueryExecutionSuccess {
		t.Fatalf("persisted outcome must stay success: %v %+v", rerr, view.Execution)
	}

	capable := savedRequest("pgmulti-saved-g-nocap", nil)
	capable.Capabilities = []string{service.CapabilityCellTruncated}
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID, capable); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key capability retry = %v, want already_exists", err)
	}

	fresh := savedRequest("pgmulti-saved-g-cap", nil)
	fresh.Capabilities = []string{service.CapabilityCellTruncated}
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID, fresh)
	if err != nil {
		t.Fatalf("capable execute: %v", err)
	}
	if v := *out.Result.Rows[0][0]; len(v) != service.ResultCellMaxBytes || v != strings.Repeat("z", service.ResultCellMaxBytes) {
		t.Fatalf("delivered prefix len = %d", len(v))
	}
	if !out.Result.CellTruncated[0][0] {
		t.Fatal("truncated cell not marked")
	}
}

// ---------------------------------------------------------------------------
// A finalize failure keeps the claim discoverable but unlinked, writes no
// history/audit rows, and outranks every later verdict — including the
// delivery-gate refusal.
// ---------------------------------------------------------------------------

// finalizeFailRepo wraps the real repository: claim admission is real, the
// keyed finalize fails — the only seam that can exercise the boundary.
type finalizeFailRepo struct {
	*mysqlrepo.QueryExecutionRepository
}

func (finalizeFailRepo) FinalizeClaimWithAudit(_ context.Context, _ model.QueryExecutionRecord, _, _ string) (uint64, error) {
	return 0, errors.New("forced finalize failure")
}

func TestPGMultiEntrySaved_FinalizeFailure(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)

	realExec := mysqlrepo.NewQueryExecutionRepository(db)
	svc := service.NewQueryExecutionService(
		mysqlrepo.NewQueryTargetRepository(db),
		finalizeFailRepo{realExec},
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
	).WithPGClaimOrchestration(mysqlrepo.NewQueryDisclosureRepository(db)).
		WithTemplateExecution(mysqlrepo.NewQuerySavedStatementRepository(db), service.NewTemplateStatementCompiler())

	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-ffin-"+strings.ReplaceAll(t.Name(), "/", "-"))

	// Over-width result + no capability: the persistence failure still wins
	// over the delivery gate.
	saved := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "finalize boundary", Statement: `SELECT repeat('z', 9000) AS v`, Scope: model.QuerySavedStatementPersonal,
	})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-ff", nil))
	if !errors.Is(err, service.ErrQueryBackendFailure) {
		t.Fatalf("finalize failure = %v, want backend failure (persistence precedence)", err)
	}
	if out.Result != nil || out.ExecutionID != 0 {
		t.Fatalf("finalize failure outcome = %+v, want nil Result and zero ExecutionID", out)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "pgmulti-saved-ff"); execs != 0 || audits != 0 || linked != 0 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 0/0/0 — no rows on finalize failure", execs, audits, linked)
	}
	if !claimOccupied(t, db, targetID, "pgmulti-saved-ff") {
		t.Fatal("claim must remain discoverable and non-retryable after finalize failure")
	}
}

// ---------------------------------------------------------------------------
// Post-claim statement mutation (wrapper injection): a wrapper over the REAL
// claim repository forwards the actual claim INSERT, then runs the REAL
// UpdateWithAudit — the saved SQL and declarations change between this
// request's pre-claim read and its attempt. The in-flight call already owns
// the compiled snapshot of the version it read, so it must deliver the OLD
// shape; the next request re-reads and compiles the NEW version. This is an
// authorized-write race control, not a client-reachable path.
// ---------------------------------------------------------------------------

// savedUpdateExecRepo wraps the real claim repository: the claim row lands
// for real, then the injected update runs before TryClaimExecution returns.
type savedUpdateExecRepo struct {
	*mysqlrepo.QueryExecutionRepository
	update func() error
	err    error
}

func (r *savedUpdateExecRepo) TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error {
	if err := r.QueryExecutionRepository.TryClaimExecution(ctx, in); err != nil {
		return err
	}
	if r.update != nil {
		r.err = r.update()
	}
	return nil
}

func TestPGMultiEntrySaved_UpdateAfterClaim(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	ctx := context.Background()

	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	wrapped := &savedUpdateExecRepo{QueryExecutionRepository: mysqlrepo.NewQueryExecutionRepository(db)}
	svc := service.NewQueryExecutionService(
		mysqlrepo.NewQueryTargetRepository(db),
		wrapped,
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
	).WithPGClaimOrchestration(mysqlrepo.NewQueryDisclosureRepository(db)).
		WithTemplateExecution(savedRepo, service.NewTemplateStatementCompiler())

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-saved-upd-"+strings.ReplaceAll(t.Name(), "/", "-"))
	params := []model.QuerySavedStatementParameterDefinition{
		{Name: "v", Type: model.QuerySavedStatementParameterString},
	}
	saved := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "versioned pick", Statement: `SELECT :v::text AS out`,
		Scope: model.QuerySavedStatementPersonal, Parameters: params,
	})

	// Between the claim INSERT and the attempt, a REAL authorized update
	// rewrites BOTH the SQL and the declaration surface (:v → upper(:v2),
	// declaration v → v2). The injected failure is recorded — the claim
	// itself still succeeds.
	paramsV2 := []model.QuerySavedStatementParameterDefinition{
		{Name: "v2", Type: model.QuerySavedStatementParameterString},
	}
	wrapped.update = func() error {
		return savedRepo.UpdateWithAudit(ctx, pgSavedOwner, targetID, saved.ID,
			model.QuerySavedStatementUpdateRequest{
				Name: "versioned pick", Statement: `SELECT upper(:v2::text) AS out`,
				Database: "discdb", Parameters: paramsV2,
			}, false)
	}
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-upd1", map[string]json.RawMessage{"v": json.RawMessage(`"alpha"`)}))
	if err != nil {
		t.Fatalf("in-flight execute: %v", err)
	}
	if wrapped.err != nil {
		t.Fatalf("injected update: %v", wrapped.err)
	}
	// The claim was made against the pre-update compiled snapshot: the
	// attempt runs the old authorized version — plain :v → 'alpha', never
	// the committed upper(:v2) rewrite.
	if out.Result == nil || len(out.Result.Rows) != 1 ||
		out.Result.Rows[0][0] == nil || *out.Result.Rows[0][0] != "alpha" {
		t.Fatalf("in-flight rows = %+v, want the pre-update snapshot 'alpha'", out.Result)
	}
	if status, _ := keyedSavedRow(t, db, targetID, "pgmulti-saved-upd1"); status != "success" {
		t.Fatalf("keyed row status = %q, want success", status)
	}

	// A fresh key re-reads the row and compiles the NEW version — the
	// authorized update governs every subsequent execution: v2 + upper().
	wrapped.update = nil
	out2, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-upd2", map[string]json.RawMessage{"v2": json.RawMessage(`"alpha"`)}))
	if err != nil {
		t.Fatalf("post-update execute: %v", err)
	}
	if out2.Result == nil || len(out2.Result.Rows) != 1 ||
		out2.Result.Rows[0][0] == nil || *out2.Result.Rows[0][0] != "ALPHA" {
		t.Fatalf("post-update rows = %+v, want the new compiled version 'ALPHA'", out2.Result)
	}

	// Declaration change control: the rename has COMMITTED, so a request
	// carrying the old declaration's values refuses pre-claim — the fresh
	// re-read sees only the current version.
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, saved.ID,
		savedRequest("pgmulti-saved-upd3", map[string]json.RawMessage{"v": json.RawMessage(`"alpha"`)})); err == nil {
		t.Fatal("request with stale declaration values must refuse pre-claim")
	}
	// Pre-claim refusals write the NULL-key pair — nothing lands under the
	// request key itself.
	if execs, _, _ := claimExecCounts(t, db, targetID, "pgmulti-saved-upd3"); execs != 0 {
		t.Fatalf("keyed rows for stale-declaration key = %d, want 0 (refusal is NULL-keyed)", execs)
	}
	if n := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL`,
		targetID); n != 1 {
		t.Fatalf("NULL-key rows = %d, want the single pre-claim refusal pair", n)
	}
}
