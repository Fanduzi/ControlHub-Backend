//go:build integration

// Package integration — real PG16 + real MySQL vertical for the T8-C
// per-cell truncation matrix and the post-finalize delivery gate (issue
// #116): production ExecutePGClaimed → real ExecParams scan → positional
// projection carrying value+flag → real disclosure preflight/transform →
// the one keyed finalize → the shared DecideResultDelivery gate.
// input: shared MySQL fixture (real target/credential/claim/execution/
// disclosure repos), a disposable PostgreSQL 16 with a restricted role,
// the production ExecutePGClaimed entry
// output: TestPGClaimedDelivery* — byte-boundary + native-type matrix,
// positional coordinates, sentinel/zero-row pages, disclosure-preserved
// flags, delivery-refusal semantics, same-key capability retries, and
// finalize-failure priority
// pos: T8-C production-delivery proof — every result travels the real
// production entry; no test hand-composes a PGVerifiedResult for the
// covered paths. PG HTTP POST stays closed; this is the internal entry.
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// pgDeliver runs one real keyed execution and returns its outcome.
func pgDeliver(t *testing.T, svc *service.QueryExecutionService, targetID uint64, req service.PGClaimedExecuteRequest) (service.PGClaimedOutcome, error) {
	t.Helper()
	return svc.ExecutePGClaimed(context.Background(), userIdentity(claimUserActor), targetID, req)
}

func capsRequest(key, stmt string, caps ...string) service.PGClaimedExecuteRequest {
	r := claimedRequest(key, stmt)
	r.Capabilities = caps
	return r
}

// cellText unwraps a delivered native-text cell: (text,false) for a value,
// ("",true) for SQL NULL.
func cellText(row []*string, i int) (string, bool) {
	if row[i] == nil {
		return "", true
	}
	return *row[i], false
}

// ---------------------------------------------------------------------------
// A. Byte boundary + native type matrix — one real row exercising every
// boundary arm: ASCII 8191/8192/8193, multibyte cut points, JSON/bytea/
// array/bigint/numeric/time verbatim, and NULL/empty/"NULL" distinctness.
// Derived columns carry no governed source, so no policy rows are needed;
// capability is declared so the truncated cells are delivered.
// ---------------------------------------------------------------------------

func TestPGClaimedDelivery_ByteBoundaryMatrix(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	targetID := setupPGClaimTarget(t, db, fix, "t8c-bytes-"+strings.ReplaceAll(t.Name(), "/", "-"))

	out, err := pgDeliver(t, svc, targetID, capsRequest("t8c-bytes", `
		SELECT
			repeat('a', 8191)                                   AS a8191,
			repeat('b', 8192)                                   AS a8192,
			repeat('c', 8193)                                   AS a8193,
			repeat('中', 2730)                                   AS zh8190,
			repeat('中', 2731)                                   AS zh8193,
			repeat('🙂', 2048)                                   AS em8192,
			repeat('🙂', 2049)                                   AS em8196,
			('{"k":"' || repeat('x', 9000) || '"}')::json        AS j,
			('\x' || repeat('41', 9000))::bytea                  AS bin,
			ARRAY[1,2,3]::text                                   AS arr,
			9223372036854775807::bigint::text                    AS bi,
			123.456789012345678901234567890::numeric::text       AS num,
			'2026-01-02 03:04:05+00'::timestamptz::text          AS ts,
			NULL::text                                           AS nul,
			''::text                                             AS empty,
			'NULL'::text                                         AS nulstr`,
		service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("ExecutePGClaimed: %v", err)
	}
	if out.Result == nil || len(out.Result.Rows) != 1 || len(out.Result.Rows[0]) != 16 {
		t.Fatalf("result = %+v, want one 16-column row", out.Result)
	}
	row, marks := out.Result.Rows[0], out.Result.CellTruncated[0]
	if len(marks) != 16 {
		t.Fatalf("matrix row = %v, want 16 flags", marks)
	}

	check := func(i int, name string, wantLen int, wantTrunc bool) string {
		t.Helper()
		v, isNull := cellText(row, i)
		if isNull {
			t.Fatalf("%s: cell is NULL", name)
		}
		if len(v) != wantLen {
			t.Fatalf("%s: len = %d, want %d", name, len(v), wantLen)
		}
		if !utf8.ValidString(v) {
			t.Fatalf("%s: retained prefix is not valid UTF-8", name)
		}
		if marks[i] != wantTrunc {
			t.Fatalf("%s: flag = %v, want %v", name, marks[i], wantTrunc)
		}
		return v
	}

	const B = service.ResultCellMaxBytes
	check(0, "a8191", 8191, false)
	check(1, "a8192", B, false)
	check(2, "a8193", B, true)
	check(3, "zh8190", 8190, false)
	// 2731×3-byte chars = 8193 bytes — the retained prefix must be the
	// longest complete rune sequence ≤ 8192, i.e. 2730 chars = 8190 bytes.
	if v := check(4, "zh8193", 8190, true); v != strings.Repeat("中", 2730) {
		t.Fatalf("zh8193 prefix mismatch")
	}
	check(5, "em8192", B, false) // 2048 four-byte chars exactly at budget
	check(6, "em8196", B, true)  // 2049th emoji does not fit — cut at 8192
	// A truncated JSON value is a raw prefix — never completed or re-parsed.
	if v := check(7, "j", B, true); !strings.HasPrefix(v, `{"k":"x`) || json.Valid([]byte(v)) {
		t.Fatalf("j: truncated JSON must stay an incomplete prefix")
	}
	if v := check(8, "bin", B, true); !strings.HasPrefix(v, `\x41`) {
		t.Fatalf("bin: bytea text must keep its native \\x prefix")
	}
	if v := check(9, "arr", 7, false); v != "{1,2,3}" {
		t.Fatalf("arr = %q, want native array text", v)
	}
	if v := check(10, "bi", 19, false); v != "9223372036854775807" {
		t.Fatalf("bi = %q — bigint must stay exact text", v)
	}
	if v := check(11, "num", 31, false); v != "123.456789012345678901234567890" {
		t.Fatalf("num = %q — numeric must stay exact text", v)
	}
	if v := check(12, "ts", 22, false); !strings.HasPrefix(v, "2026-01-02") {
		t.Fatalf("ts = %q — time text must be native", v)
	}
	// NULL, empty string, and the literal "NULL" stay three different cells.
	if _, isNull := cellText(row, 13); !isNull || marks[13] {
		t.Fatalf("nul: NULL must stay NULL with flag false")
	}
	if v := check(14, "empty", 0, false); v != "" {
		t.Fatalf("empty = %q", v)
	}
	if v := check(15, "nulstr", 4, false); v != "NULL" {
		t.Fatalf("nulstr = %q — the literal survives verbatim", v)
	}
	// Exactly one keyed finalize — a real execution id + persisted success.
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t8c-bytes"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}
}

// ---------------------------------------------------------------------------
// B. Delivery gate on a real oversized page: refusal is post-finalize —
// the persisted outcome is success with the real execution id, history/
// audit/link land exactly once, and no rows escape. The same key with
// changed capabilities cannot re-execute (digest excludes capabilities).
// ---------------------------------------------------------------------------

func TestPGClaimedDelivery_DeliveryGateReal(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t8c-gate-"+strings.ReplaceAll(t.Name(), "/", "-"))

	stmt := `SELECT repeat('z', 9000) AS v`

	// No capability: delivery refusal, but the terminal fact is success.
	out, err := pgDeliver(t, svc, targetID, claimedRequest("t8c-gate-nocap", stmt))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("no-cap err = %v, want result_contract_upgrade_required", err)
	}
	if out.Result != nil || out.ExecutionID == 0 ||
		out.Status != model.QueryExecutionSuccess || out.Evidence.Status != model.QueryExecutionSuccess {
		t.Fatalf("outcome = %+v, want success-without-rows", out)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t8c-gate-nocap"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}
	rec, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "t8c-gate-nocap", userIdentity(claimUserActor))
	if err != nil || rec.Execution == nil || rec.Execution.Status != model.QueryExecutionSuccess {
		t.Fatalf("persisted outcome must stay success: %v %+v", err, rec.Execution)
	}
	if rec.Execution.RemoteState != model.QueryExecutionRemoteCompleted {
		t.Fatalf("remote_state = %q, want completed", rec.Execution.RemoteState)
	}

	// Same key, capabilities now declared — the claim protocol refuses a
	// second execution; capabilities never enter the digest.
	if _, err := pgDeliver(t, svc, targetID,
		capsRequest("t8c-gate-nocap", stmt, service.CapabilityCellTruncated)); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key capability retry = %v, want already_exists", err)
	}
	if execs, _, _ := claimExecCounts(t, db, targetID, "t8c-gate-nocap"); execs != 1 {
		t.Fatalf("capability retry executed again: execs = %d", execs)
	}

	// Exact capability on a fresh key delivers the safe prefix + matrix.
	out, err = pgDeliver(t, svc, targetID,
		capsRequest("t8c-gate-cap", stmt, service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("capable execute: %v", err)
	}
	v, _ := cellText(out.Result.Rows[0], 0)
	if len(v) != service.ResultCellMaxBytes || v != strings.Repeat("z", service.ResultCellMaxBytes) {
		t.Fatalf("delivered prefix len = %d", len(v))
	}
	if !out.Result.CellTruncated[0][0] {
		t.Fatal("truncated cell not marked")
	}

	// Unknown capabilities never satisfy the exact-match rule.
	if _, err := pgDeliver(t, svc, targetID,
		capsRequest("t8c-gate-unknown", stmt, "bogus", "celltruncated", "bogus")); !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("unknown-cap err = %v, want upgrade required", err)
	}
}

// ---------------------------------------------------------------------------
// C. Coordinates and pages: duplicate public names, a JOIN+CTE with a
// governed source, a LIMIT-0 page, an oversized sentinel row that never
// reaches the delivered matrix, and the unchanged total-budget rejection.
// ---------------------------------------------------------------------------

func TestPGClaimedDelivery_CoordinatesAndPages(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	targetID := setupPGClaimTarget(t, db, fix, "t8c-coord-"+strings.ReplaceAll(t.Name(), "/", "-"))

	// Duplicate public column names — flags follow position, never name.
	out, err := pgDeliver(t, svc, targetID, capsRequest("t8c-dup", `
		SELECT 1 AS x, repeat('d', 9000) AS x`,
		service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	if out.Result.Columns[0].Name != "x" || out.Result.Columns[1].Name != "x" ||
		out.Result.CellTruncated[0][0] || !out.Result.CellTruncated[0][1] {
		t.Fatalf("duplicate-name page = %+v / %v", out.Result.Columns, out.Result.CellTruncated)
	}

	// CTE + JOIN: a governed column and a derived oversized column keep
	// their own positions across the multi-source layout.
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	out, err = pgDeliver(t, svc, targetID, capsRequest("t8c-cte", `
		WITH q AS (SELECT repeat('e', 9000) AS big)
		SELECT o.id, q.big FROM app.orders o CROSS JOIN q ORDER BY o.id`,
		service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("cte: %v", err)
	}
	if len(out.Result.Rows) != 2 || *out.Result.Rows[0][0] != "1" || *out.Result.Rows[1][0] != "2" {
		t.Fatalf("cte rows = %+v", out.Result.Rows)
	}
	for r, marks := range out.Result.CellTruncated {
		if marks[0] || !marks[1] {
			t.Fatalf("cte row %d flags = %v, want [false true]", r, marks)
		}
		if len(*out.Result.Rows[r][1]) != service.ResultCellMaxBytes {
			t.Fatalf("cte big len = %d", len(*out.Result.Rows[r][1]))
		}
	}

	// LIMIT 0 — a zero-row page delivers with no residual matrix and no
	// capability requirement.
	out, err = pgDeliver(t, svc, targetID, claimedRequest("t8c-limit0",
		`SELECT repeat('f', 9000) AS v FROM app.orders LIMIT 0`))
	if err != nil {
		t.Fatalf("limit0: %v", err)
	}
	if len(out.Result.Rows) != 0 || len(out.Result.CellTruncated) != 0 {
		t.Fatalf("zero-row page carried rows/matrix: %+v", out.Result)
	}

	// Only the undelivered sentinel row is oversized — the clean page must
	// not inherit its flag: 25 delivered rows, all flags false, page
	// boundary still detected (hasMore comes from the sentinel).
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "tag", model.ResultDisclosureRawCopyAllowed)
	req := claimedRequest("t8c-sentinel", `
		SELECT CASE WHEN id <= 25 THEN tag ELSE repeat('z', 9000) END AS v
		FROM app.smallvals ORDER BY id`)
	req.Page, req.PageSize = 1, 25
	out, err = pgDeliver(t, svc, targetID, req)
	if err != nil {
		t.Fatalf("sentinel: %v", err)
	}
	if len(out.Result.Rows) != 25 || !out.Result.Page.HasMore {
		t.Fatalf("sentinel page = %d rows hasMore=%v, want 25/true",
			len(out.Result.Rows), out.Result.Page.HasMore)
	}
	for r, marks := range out.Result.CellTruncated {
		if marks[0] {
			t.Fatalf("sentinel row %d polluted the delivered matrix", r)
		}
	}

	// The aggregate result budget is unchanged: ~2 MB of raw page still
	// refuses the whole result — per-cell truncation never relaxes it.
	big := claimedRequest("t8c-total", `SELECT repeat('q', 20000) FROM generate_series(1, 110)`)
	big.Page, big.PageSize, big.MaxRows = 1, 100, 200
	if _, err := pgDeliver(t, svc, targetID, big); !errors.Is(err, service.ErrQueryResultTooLarge) {
		t.Fatalf("over-total-budget err = %v, want query_result_too_large", err)
	}
}

// ---------------------------------------------------------------------------
// D. Disclosure propagation: a truncated raw cell keeps its flag through
// masking; a masked NULL stays NULL with flag false; a missing policy still
// refuses before the user statement is sent.
// ---------------------------------------------------------------------------

func TestPGClaimedDelivery_DisclosureFlags(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	targetID := setupPGClaimTarget(t, db, fix, "t8c-disc-"+strings.ReplaceAll(t.Name(), "/", "-"))

	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "note", model.ResultDisclosureMaskedNoCopy)

	// Masked + truncated: the raw concatenated value was cut → flag true
	// survives even though the delivered text is the short fixed mask.
	out, err := pgDeliver(t, svc, targetID, capsRequest("t8c-mask", `
		SELECT id, note || repeat('y', 9000) AS v FROM app.orders WHERE id = 1`,
		service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("mask: %v", err)
	}
	if v, _ := cellText(out.Result.Rows[0], 1); v != "[MASKED]" {
		t.Fatalf("masked cell = %q", v)
	}
	if !out.Result.CellTruncated[0][1] {
		t.Fatal("masked truncated cell lost its flag")
	}
	if out.Result.Columns[1].CopyAllowed {
		t.Fatal("masked column became copyable")
	}

	// Masked NULL: stays NULL, flag false — never fabricated.
	out, err = pgDeliver(t, svc, targetID, capsRequest("t8c-masknull", `
		SELECT note FROM app.orders WHERE id = 2`, service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("mask null: %v", err)
	}
	if out.Result.Rows[0][0] != nil || out.Result.CellTruncated[0][0] {
		t.Fatalf("masked NULL = %+v flag=%v", out.Result.Rows[0][0], out.Result.CellTruncated[0][0])
	}

	// Missing policy on a governed column still refuses before user SQL —
	// capabilities never widen disclosure authorization.
	if _, err := pgDeliver(t, svc, targetID, capsRequest("t8c-blocked", `
		SELECT qty FROM app.orders`, service.CapabilityCellTruncated)); !errors.Is(err, service.ErrQueryDisclosureBlocked) {
		t.Fatalf("missing policy = %v, want disclosure blocked", err)
	}
	if execs, _, _ := claimExecCounts(t, db, targetID, "t8c-blocked"); execs != 1 {
		t.Fatalf("blocked attempt execs = %d, want the one keyed rejected row", execs)
	}

	// CSV eligibility reuses the shared gate on the real produced page:
	// a masked and/or truncated page is denied; a clean fully-copyable
	// page passes. No CSV endpoint exists — this judges eligibility only.
	cols, rows, matrix := csvArgs(out.Result)
	if err := service.DecideCSVExport(cols, rows, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("csv on masked page = %v, want not permitted", err)
	}
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "tag", model.ResultDisclosureRawCopyAllowed)
	out, err = pgDeliver(t, svc, targetID, capsRequest("t8c-csv",
		`SELECT tag FROM app.smallvals WHERE id <= 3 ORDER BY id`, service.CapabilityCellTruncated))
	if err != nil {
		t.Fatalf("csv page: %v", err)
	}
	cols, rows, matrix = csvArgs(out.Result)
	if err := service.DecideCSVExport(cols, rows, matrix); err != nil {
		t.Fatalf("clean copyable page denied csv: %v", err)
	}
}

// csvArgs adapts a disclosed page to the shared CSV eligibility gate's
// inputs — the same [][]any/string shape the delivery boundary uses.
func csvArgs(res *service.PGDisclosedResult) ([]model.QueryResultColumn, [][]any, [][]bool) {
	cols := make([]model.QueryResultColumn, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = model.QueryResultColumn{
			Name: c.Name, DatabaseType: c.DatabaseType,
			DisplayMode: c.DisplayMode, CopyAllowed: c.CopyAllowed,
		}
	}
	rows := make([][]any, len(res.Rows))
	for r, row := range res.Rows {
		v := make([]any, len(row))
		for c, cell := range row {
			if cell != nil {
				v[c] = *cell
			}
		}
		rows[r] = v
	}
	return cols, rows, res.CellTruncated
}

// ---------------------------------------------------------------------------
// E. Failure priority on real stores: a finalize/audit write failure beats
// the capability refusal — no rows, no execution id, no partial pair, and
// the claim stays occupied without a second execution.
// ---------------------------------------------------------------------------

func TestPGClaimedDelivery_FinalizeFailurePriority(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t8c-fin-"+strings.ReplaceAll(t.Name(), "/", "-"))

	mustExec(t, db, `CREATE TRIGGER t8c_fail_link BEFORE UPDATE ON query_execution_claims
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject link failure'`)
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP TRIGGER IF EXISTS t8c_fail_link`); err != nil {
			t.Logf("drop trigger: %v", err)
		}
	})

	// Truncated page WITHOUT capability: the persistence failure must win —
	// the backend error, not the upgrade refusal, and nothing is delivered.
	out, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t8c-fin-key", `SELECT repeat('z', 9000) AS v`))
	if err == nil || errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("err = %v, want the persistence failure to outrank the capability refusal", err)
	}
	if out.Result != nil || out.ExecutionID != 0 {
		t.Fatalf("finalize failure outcome = %+v, want empty", out)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t8c-fin-key"); execs != 0 || audits != 0 || linked != 0 {
		t.Fatalf("rolled-back finalize left execs=%d audits=%d linked=%d", execs, audits, linked)
	}
	if !claimOccupied(t, db, targetID, "t8c-fin-key") {
		t.Fatal("claim must stay discoverable after finalize failure")
	}
}
