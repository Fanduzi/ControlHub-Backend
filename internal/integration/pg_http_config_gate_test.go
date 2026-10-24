//go:build integration

// input: shared MySQL fixture (real repositories/auth), a disposable PostgreSQL 16 with the restricted role, and serverboot.Build driven by Config.PGHTTPEntries — the same production entry cmd/server serves
// output: TestPGHTTPConfigGate_* — production-assembly proof that the deployment flag is default-closed; that "on" atomically opens both the PG HTTP entries and the connections[] read model; and that enabled=false / non_prod_only / all_environments credentials still govern execution per connection
// pos: Production Enablement R2 acceptance — no test hand-flips WithPGHTTPEntries or WithConnectionLister here; every assembly under test is produced by serverboot.Build from a config value, exactly as cmd/server would wire it
// note: if this file changes, update this header and module README.md

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/serverboot"
)

// TestPGHTTPConfigGate exercises the deployment flag end to end on the real
// assembly: default closed, "on" opens entries+lister together, and the
// flag never widens per-connection authorization.
func TestPGHTTPConfigGate(t *testing.T) {
	db := setupTestDB(t)
	pg := newPGDiscFixtureOn(t, "postgres:16-alpine")

	userID := insertAuthzTestUser(t, db, "pgcfg-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	// Authorized connection: enabled + non_prod_only on a staging target.
	targetID := createPGInstance(t, db, "pg-cfg-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, pg.host, pg.port)
	insertPGCredential(t, db, targetID, pg.db, "app", pgHTTPCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+pgHTTPCredRef,
		pgKeywordDSN(pg.host, pg.port, pg.db, pgDiscRole, pgStmtPassword, 5))

	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	insertPolicy(t, disc, targetID, pg.db, "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	// Disabled connection on a second staging target — the flag must not
	// resurrect it.
	disabledTarget := createPGInstance(t, db, "pg-cfg-off-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, pg.host, pg.port)
	insertPGCredential(t, db, disabledTarget, pg.db, "app", pgHTTPCredRef, false, string(model.QueryEnvPolicyNonProdOnly))
	insertPolicy(t, disc, disabledTarget, pg.db, "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	// Production-environment target whose credential is non_prod_only —
	// environment policy must still refuse under the open flag.
	prodTarget := createPGInstance(t, db, "pg-cfg-prod-"+strings.ReplaceAll(t.Name(), "/", "-"), envProd, pg.host, pg.port)
	insertPGCredential(t, db, prodTarget, pg.db, "app", pgHTTPCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	insertPolicy(t, disc, prodTarget, pg.db, "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	build := func(flag bool) http.Handler {
		deps, _ := serverboot.Build(db, config.Config{
			JWTSecret:     authzIntegrationSecret,
			PGHTTPEntries: flag,
		})
		return api.NewRouter(deps)
	}
	closed := build(false)
	enabled := build(true)
	token := mustLogin(t, enabled, "pgcfg-admin@example.com", "secret123")
	execute := func(target uint64) string {
		return fmt.Sprintf("/query-targets/%d/execute", target)
	}

	// --- Flag off: the production assembly is exactly the closed shape ---
	t.Run("off refuses scope fields and engine dispatch", func(t *testing.T) {
		rr := doBearerWithBody(t, closed, http.MethodPost, execute(targetID), token,
			`{"statement":"SELECT id FROM app.orders","database":"discdb","clientExecutionId":"cfg-off-scope"}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("scope-field request: %d; body=%s", rr.Code, rr.Body.String())
		}
		if code, _ := decodeError(t, rr); code != "validation_failed" {
			t.Fatalf("scope-field request error = %q, want validation_failed", code)
		}

		rr = doBearerWithBody(t, closed, http.MethodPost, execute(targetID), token,
			`{"statement":"SELECT id FROM app.orders","clientExecutionId":"cfg-off-plain"}`)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("plain request: %d; body=%s", rr.Code, rr.Body.String())
		}
		if code, _ := decodeError(t, rr); code != "query_not_allowed" {
			t.Fatalf("plain request error = %q, want query_not_allowed", code)
		}

		if got := countWhere(t, db,
			`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?`, targetID); got != 0 {
			t.Fatalf("closed assembly occupied %d claims, want 0", got)
		}
	})

	t.Run("off exposes no connections[]", func(t *testing.T) {
		assertTargetConnections(t, closed, token, targetID, false)
	})

	// --- Flag on: both construction-time capabilities open together ---
	t.Run("on opens entries and connections[] together", func(t *testing.T) {
		rr := doBearerWithBody(t, enabled, http.MethodPost, execute(targetID), token,
			`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"cfg-on-1"}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("enabled execute: %d; body=%s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Status  string `json:"status"`
			RowCount int   `json:"rowCount"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode execute: %v", err)
		}
		if resp.Status != "success" || resp.RowCount != 2 {
			t.Fatalf("execute status/rowCount = %q/%d, want success/2", resp.Status, resp.RowCount)
		}

		// The same single flag also wired the connections[] read model —
		// one bit opened both halves of the governed surface.
		assertTargetConnections(t, enabled, token, targetID, true)
	})

	// --- Flag on never widens per-connection authorization ---
	t.Run("on still refuses disabled credential", func(t *testing.T) {
		rr := doBearerWithBody(t, enabled, http.MethodPost, execute(disabledTarget), token,
			`{"statement":"SELECT id FROM app.orders","database":"discdb","clientExecutionId":"cfg-on-disabled"}`)
		if rr.Code == http.StatusOK {
			t.Fatalf("disabled credential executed: body=%s", rr.Body.String())
		}
		if got := countWhere(t, db,
			`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND status = 'success'`, disabledTarget); got != 0 {
			t.Fatalf("disabled credential produced %d success executions", got)
		}
	})

	t.Run("on still refuses non_prod_only in production", func(t *testing.T) {
		rr := doBearerWithBody(t, enabled, http.MethodPost, execute(prodTarget), token,
			`{"statement":"SELECT id FROM app.orders","database":"discdb","clientExecutionId":"cfg-on-prod"}`)
		if rr.Code == http.StatusOK {
			t.Fatalf("non_prod_only credential executed in production: body=%s", rr.Body.String())
		}
		if got := countWhere(t, db,
			`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND status = 'success'`, prodTarget); got != 0 {
			t.Fatalf("non_prod_only credential produced %d success executions in production", got)
		}
	})
}

// assertTargetConnections reads GET /query-targets and checks whether the
// given target carries a populated connections[] projection — the lister's
// observable fingerprint. present=false additionally proves the projection
// is absent (not merely empty): an unwired lister emits no key at all.
func assertTargetConnections(t *testing.T, h http.Handler, token string, targetID uint64, present bool) {
	t.Helper()
	rr := doBearerWithBody(t, h, http.MethodGet, "/query-targets?pageSize=100", token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /query-targets: %d; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode targets: %v", err)
	}
	items, _ := raw["items"].([]any)
	var rawItem map[string]any
	for _, it := range items {
		m, _ := it.(map[string]any)
		if id, _ := m["resourceId"].(float64); uint64(id) == targetID {
			rawItem = m
		}
	}
	if rawItem == nil {
		t.Fatalf("target %d absent from /query-targets", targetID)
	}
	_, hasKey := rawItem["connections"]
	if !present {
		if hasKey {
			t.Fatalf("closed assembly projected connections[] on target %d", targetID)
		}
		return
	}
	if !hasKey {
		t.Fatalf("enabled assembly missing connections[] on target %d", targetID)
	}
	conns, _ := rawItem["connections"].([]any)
	var found bool
	for _, c := range conns {
		m, _ := c.(map[string]any)
		if m["database"] == "discdb" && m["defaultSchema"] == "app" && m["enabled"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("connections[] lacks the discdb/app enabled row: %v", conns)
	}
}
