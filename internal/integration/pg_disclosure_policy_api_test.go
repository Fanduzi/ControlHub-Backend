//go:build integration

// Package integration provides real-MySQL HTTP proofs for the T9-A disclosure policy management surface.
// input: shared MySQL/auth/query fixtures, api router, disclosure repository and service
// output: TestDisclosurePolicyHTTP_* integration tests
// pos: Real-DB HTTP acceptance boundary for the T9-A schema-scope slice of issue #117
// note: if this file changes, update this header and module README.md.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// createDisclosureMySQLInstance provisions a database_instance resource whose
// profile reports engine 'mysql' — the non-schema engine branch of the
// disclosure scope gate.
func createDisclosureMySQLInstance(t *testing.T, db *sql.DB, name string) uint64 {
	t.Helper()
	repo := mysql.NewResourceRepository(db)
	res, err := repo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            name,
		DisplayName:     name,
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create mysql instance %s: %v", name, err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', '127.0.0.1', 3306, 'primary', '{}')`, res.ID)
	return res.ID
}

func disclosureHTTPRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	repo := mysql.NewQueryDisclosureRepository(db)
	return api.NewRouter(api.Dependencies{
		AuthService: service.NewAuthService(mysql.NewUserRepository(db), "t9a-disclosure-secret"),
		QueryDisclosureService: service.NewQueryDisclosureService(
			repo, repo, nil, mysql.NewQueryTargetRepository(db),
		),
		QueryExecutionAuth: api.QueryExecutionAuthConfig{Clock: time.Now},
	})
}

func disclosureReq(t *testing.T, router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
}

func mustErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	mustStatus(t, rec, wantStatus)
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v; raw=%s", err, rec.Body.String())
	}
	if resp.Error != wantCode {
		t.Fatalf("error code = %q, want %q; body=%s", resp.Error, wantCode, rec.Body.String())
	}
}

func mustPolicyList(t *testing.T, router http.Handler, token string, target uint64) []model.ResultDisclosurePolicy {
	t.Helper()
	rec := disclosureReq(t, router, http.MethodGet, fmt.Sprintf("/query-disclosure-policies?targetResourceId=%d", target), "", token)
	mustStatus(t, rec, http.StatusOK)
	var resp struct {
		Items []model.ResultDisclosurePolicy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list body: %v; raw=%s", err, rec.Body.String())
	}
	return resp.Items
}

// TestDisclosurePolicyHTTP_FivePartScopeManagement is the end-to-end control
// plane proof (acceptance A/B/C/F/G): schema-aware create/list/update/delete
// over the real router → service → repository → MySQL chain, with the
// engine-conditional schema contract, exact-key isolation, conflict/not-found/
// idempotent semantics, admin-only enforcement, and strict JSON decoding.
func TestDisclosurePolicyHTTP_FivePartScopeManagement(t *testing.T) {
	db := setupTestDB(t)
	router := disclosureHTTPRouter(t, db)
	pgTarget := createPGInstance(t, db, "t9a-pg-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, "127.0.0.1", 1)
	myTarget := createDisclosureMySQLInstance(t, db, "t9a-my-"+strings.ReplaceAll(t.Name(), "/", "-"))

	insertAuthzTestUser(t, db, "t9a-admin@example.com", "admin")
	insertAuthzTestUser(t, db, "t9a-editor@example.com", "editor")
	adminToken := mustLogin(t, router, "t9a-admin@example.com", "secret123")
	editorToken := mustLogin(t, router, "t9a-editor@example.com", "secret123")

	create := func(target uint64, database, schema, object, column, mode string) *httptest.ResponseRecorder {
		return disclosureReq(t, router, http.MethodPost, "/query-disclosure-policies",
			fmt.Sprintf(`{"targetResourceId":%d,"databaseName":%q,"schemaName":%q,"objectName":%q,"columnName":%q,"mode":%q}`,
				target, database, schema, object, column, mode), adminToken)
	}

	// Acceptance A: same target+database, app.orders.id and analytics.orders.id
	// coexist with different modes; response preserves schema.
	rec := create(pgTarget, "sales_db", "app", "orders", "id", "raw_copy_allowed")
	mustStatus(t, rec, http.StatusCreated)
	var created model.ResultDisclosurePolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if created.SchemaName != "app" || created.DatabaseName != "sales_db" {
		t.Fatalf("create response scope = %q.%q, want sales_db.app", created.DatabaseName, created.SchemaName)
	}
	mustStatus(t, create(pgTarget, "sales_db", "analytics", "orders", "id", "masked_no_copy"), http.StatusCreated)

	// Duplicate five-part key → existing conflict behavior.
	mustErrorCode(t, create(pgTarget, "sales_db", "app", "orders", "id", "masked_no_copy"), http.StatusConflict, "disclosure_policy_conflict")

	items := mustPolicyList(t, router, adminToken, pgTarget)
	if len(items) != 2 {
		t.Fatalf("list = %d items, want 2", len(items))
	}
	seen := map[string]model.ResultDisclosureMode{}
	for _, it := range items {
		seen[it.SchemaName] = it.Mode
	}
	if seen["app"] != model.ResultDisclosureRawCopyAllowed || seen["analytics"] != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("list modes = %v, want app=raw_copy_allowed analytics=masked_no_copy", seen)
	}

	// Acceptance B: updating 'app' must not touch 'analytics'; updating a
	// missing key is not-found, not silent success or sibling hit.
	put := fmt.Sprintf(`{"targetResourceId":%d,"databaseName":"sales_db","schemaName":"app","objectName":"orders","columnName":"id","mode":"masked_no_copy"}`, pgTarget)
	mustStatus(t, disclosureReq(t, router, http.MethodPut, "/query-disclosure-policies", put, adminToken), http.StatusNoContent)
	miss := fmt.Sprintf(`{"targetResourceId":%d,"databaseName":"sales_db","schemaName":"reporting","objectName":"orders","columnName":"id","mode":"masked_no_copy"}`, pgTarget)
	mustErrorCode(t, disclosureReq(t, router, http.MethodPut, "/query-disclosure-policies", miss, adminToken), http.StatusNotFound, "disclosure_policy_not_found")
	items = mustPolicyList(t, router, adminToken, pgTarget)
	for _, it := range items {
		if it.SchemaName == "app" && it.Mode != model.ResultDisclosureMaskedNoCopy {
			t.Fatalf("app mode after update = %q, want masked_no_copy", it.Mode)
		}
		if it.SchemaName == "analytics" && it.Mode != model.ResultDisclosureMaskedNoCopy {
			t.Fatalf("analytics must be untouched by the app update: %q", it.Mode)
		}
	}

	// Deleting 'analytics' leaves 'app' intact (exact-key delete, no fallback).
	mustStatus(t, disclosureReq(t, router, http.MethodDelete,
		fmt.Sprintf("/query-disclosure-policies?targetResourceId=%d&databaseName=sales_db&schemaName=analytics&objectName=orders&columnName=id", pgTarget),
		"", adminToken), http.StatusNoContent)
	items = mustPolicyList(t, router, adminToken, pgTarget)
	if len(items) != 1 || items[0].SchemaName != "app" {
		t.Fatalf("after analytics delete: %+v, want only app row", items)
	}
	// Idempotent re-delete of the gone scope stays 204.
	mustStatus(t, disclosureReq(t, router, http.MethodDelete,
		fmt.Sprintf("/query-disclosure-policies?targetResourceId=%d&databaseName=sales_db&schemaName=analytics&objectName=orders&columnName=id", pgTarget),
		"", adminToken), http.StatusNoContent)

	// Acceptance F/I: PostgreSQL scope without schemaName is a controlled 400 —
	// never defaulted to public; a MySQL scope with schemaName is equally a
	// controlled 400; the MySQL ''-schema path still writes.
	mustErrorCode(t, create(pgTarget, "sales_db", "", "orders", "id", "raw_copy_allowed"), http.StatusBadRequest, "validation_failed")
	mustErrorCode(t, create(myTarget, "orders_db", "app", "customers", "email", "raw_copy_allowed"), http.StatusBadRequest, "validation_failed")
	mustStatus(t, create(myTarget, "orders_db", "", "customers", "email", "raw_copy_allowed"), http.StatusCreated)
	mustErrorCode(t, create(myTarget, "orders; DROP TABLE", "", "customers", "email", "raw_copy_allowed"), http.StatusBadRequest, "validation_failed")
	// The MySQL row kept its real database name (never emptied by schema work).
	myItems := mustPolicyList(t, router, adminToken, myTarget)
	if len(myItems) != 1 || myItems[0].DatabaseName != "orders_db" || myItems[0].SchemaName != "" {
		t.Fatalf("mysql list = %+v, want one orders_db '' row", myItems)
	}

	// Acceptance E/G: canonical name fidelity through HTTP — a quoted-style
	// name and a Unicode name travel verbatim request → SQL → response.
	rec = create(pgTarget, "销售库", "Data Layer", `"Quoted Table"`, "café", "masked_no_copy")
	mustStatus(t, rec, http.StatusCreated)
	var wide model.ResultDisclosurePolicy
	if err := json.Unmarshal(rec.Body.Bytes(), &wide); err != nil {
		t.Fatalf("decode wide-name body: %v", err)
	}
	if wide.DatabaseName != "销售库" || wide.SchemaName != "Data Layer" || wide.ObjectName != `"Quoted Table"` || wide.ColumnName != "café" {
		t.Fatalf("wide-name fidelity = %+v", wide)
	}
	items = mustPolicyList(t, router, adminToken, pgTarget)
	found := false
	for _, it := range items {
		if it.SchemaName == "Data Layer" && it.ObjectName == `"Quoted Table"` && it.ColumnName == "café" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list must contain the verbatim wide-name row: %+v", items)
	}

	// Acceptance F: non-admin never reaches management; strict JSON rejects
	// unknown fields.
	mustErrorCode(t, disclosureReq(t, router, http.MethodPost, "/query-disclosure-policies",
		fmt.Sprintf(`{"targetResourceId":%d,"databaseName":"sales_db","schemaName":"app","objectName":"x","columnName":"y","mode":"raw_copy_allowed"}`, pgTarget),
		editorToken), http.StatusForbidden, "forbidden")
	mustErrorCode(t, disclosureReq(t, router, http.MethodPost, "/query-disclosure-policies",
		fmt.Sprintf(`{"targetResourceId":%d,"databaseName":"sales_db","schemaName":"app","objectName":"x","columnName":"y","mode":"raw_copy_allowed","extra":1}`, pgTarget),
		adminToken), http.StatusBadRequest, "validation_failed")
}

// TestDisclosurePolicyHTTP_TrailingSpaceScopes proves trailing-space canonical
// names stay distinct end to end over the real HTTP chain (acceptance R1):
// JSON bodies and url.Values-encoded DELETE queries carry the bytes verbatim,
// sibling keys coexist and never alias, and operations address exactly one
// row. WHY: a PAD SPACE collation or a transport-layer trim would silently
// merge "orders" and "orders " into one policy scope.
func TestDisclosurePolicyHTTP_TrailingSpaceScopes(t *testing.T) {
	db := setupTestDB(t)
	router := disclosureHTTPRouter(t, db)
	pgTarget := createPGInstance(t, db, "t9a-ts-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, "127.0.0.1", 1)
	insertAuthzTestUser(t, db, "t9a-ts-admin@example.com", "admin")
	adminToken := mustLogin(t, router, "t9a-ts-admin@example.com", "secret123")

	create := func(schema, object string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{
			"targetResourceId": pgTarget,
			"databaseName":     "sales_db",
			"schemaName":       schema,
			"objectName":       object,
			"columnName":       "id",
			"mode":             "masked_no_copy",
		})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		return disclosureReq(t, router, http.MethodPost, "/query-disclosure-policies", string(body), adminToken)
	}
	del := func(schema, object string) *httptest.ResponseRecorder {
		v := url.Values{
			"targetResourceId": {fmt.Sprintf("%d", pgTarget)},
			"databaseName":     {"sales_db"},
			"schemaName":       {schema},
			"objectName":       {object},
			"columnName":       {"id"},
		}
		return disclosureReq(t, router, http.MethodDelete, "/query-disclosure-policies?"+v.Encode(), "", adminToken)
	}

	// 'orders' and 'orders ' are two distinct canonical identities.
	mustStatus(t, create("app", "orders"), http.StatusCreated)
	mustStatus(t, create("app", "orders "), http.StatusCreated)

	items := mustPolicyList(t, router, adminToken, pgTarget)
	if len(items) != 2 {
		t.Fatalf("trailing-space siblings must coexist: %+v", items)
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.ObjectName] = true
	}
	if !seen["orders"] || !seen["orders "] {
		t.Fatalf("list must round-trip verbatim names incl. trailing space: %+v", seen)
	}

	// PUT addresses exactly the trailing-space row; the plain row is untouched.
	putBody, err := json.Marshal(map[string]any{
		"targetResourceId": pgTarget,
		"databaseName":     "sales_db",
		"schemaName":       "app",
		"objectName":       "orders ",
		"columnName":       "id",
		"mode":             "raw_copy_allowed",
	})
	if err != nil {
		t.Fatalf("marshal put: %v", err)
	}
	mustStatus(t, disclosureReq(t, router, http.MethodPut, "/query-disclosure-policies", string(putBody), adminToken), http.StatusNoContent)
	items = mustPolicyList(t, router, adminToken, pgTarget)
	for _, it := range items {
		switch it.ObjectName {
		case "orders ":
			if it.Mode != model.ResultDisclosureRawCopyAllowed {
				t.Fatalf("'orders ' mode = %q, want raw_copy_allowed", it.Mode)
			}
		case "orders":
			if it.Mode != model.ResultDisclosureMaskedNoCopy {
				t.Fatalf("'orders' mode = %q, want masked_no_copy (sibling must be untouched)", it.Mode)
			}
		}
	}

	// DELETE with the url-encoded trailing-space scope removes exactly that
	// row; the sibling survives, and re-deleting stays idempotent 204.
	mustStatus(t, del("app", "orders "), http.StatusNoContent)
	items = mustPolicyList(t, router, adminToken, pgTarget)
	if len(items) != 1 || items[0].ObjectName != "orders" {
		t.Fatalf("after sibling delete: %+v, want only 'orders'", items)
	}
	mustStatus(t, del("app", "orders "), http.StatusNoContent)

	// Invalid delete scopes are controlled 400s — never silently applied.
	v := url.Values{
		"targetResourceId": {fmt.Sprintf("%d", pgTarget)},
		"databaseName":     {"sales_db"},
		"schemaName":       {"app"},
		"objectName":       {strings.Repeat("x", model.MaxIdentifierLength+1)},
		"columnName":       {"id"},
	}
	mustErrorCode(t, disclosureReq(t, router, http.MethodDelete,
		"/query-disclosure-policies?"+v.Encode(), "", adminToken), http.StatusBadRequest, "validation_failed")
	// A valid but absent scope is still the idempotent 204.
	mustStatus(t, del("app", "never_existed"), http.StatusNoContent)
}
