//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const navCredentialRef = "NAV_TARGET"

// setupNavigateFixture provisions a ready mysql/staging query target backed by
// the disposable test MySQL, then creates the schema_child -> schema_parent
// foreign-key fixture. It returns the wired HTTP server base URL, the target
// resource id, and the raw DB handle.
func setupNavigateFixture(t *testing.T) (string, uint64, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	// Create the FK fixture tables.
	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e_aux")
	mustExec(t, db, "DROP VIEW IF EXISTS query_e2e_aux.schema_parent_summary")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.schema_child")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.schema_parent")
	mustExec(t, db, `CREATE TABLE query_e2e_aux.schema_parent (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		parent_code VARCHAR(32) NOT NULL,
		label VARCHAR(128) NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		UNIQUE KEY uq_schema_parent_code (parent_code),
		KEY idx_schema_parent_label (label)
	) ENGINE=InnoDB`)
	mustExec(t, db, `CREATE TABLE query_e2e_aux.schema_child (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		parent_id BIGINT UNSIGNED NOT NULL,
		child_name VARCHAR(64) NOT NULL,
		sort_order INT NOT NULL DEFAULT 0,
		PRIMARY KEY (id),
		KEY idx_schema_child_parent (parent_id, sort_order),
		CONSTRAINT fk_schema_child_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux.schema_parent (id) ON UPDATE CASCADE ON DELETE RESTRICT
	) ENGINE=InnoDB`)
	mustExec(t, db, "INSERT IGNORE INTO query_e2e_aux.schema_parent (id, parent_code, label) VALUES (1,'P_ALPHA','Alpha Parent'),(2,'P_BETA','Beta Parent')")
	mustExec(t, db, "INSERT IGNORE INTO query_e2e_aux.schema_child (id, parent_id, child_name, sort_order) VALUES (1,1,'child_a1',1),(2,1,'child_a2',2),(3,2,'child_b1',1)")
	mustExec(t, db, "GRANT SELECT ON query_e2e_aux.* TO 'root'@'%'")

	// Target resource.
	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-nav-target-" + t.Name(),
		DisplayName:     "Navigate FK Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create nav target resource: %v", err)
	}

	dsnHost, dsnPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn host/port: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, dsnHost, dsnPort)

	seedCredentialRow(t, db, res.ID, "mysql", navCredentialRef, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+navCredentialRef, globalEnv.dsn)

	// Wire the full HTTP server.
	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()

	queryExecutionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		credentialResolver,
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
	)

	dictRepo := mysql.NewDictionaryRepository(db)
	profileSvc := service.NewProfileService(resRepo, resRepo)

	deps := api.Dependencies{
		ResourceService:        service.NewResourceService(resRepo, profileSvc),
		RelationService:        service.NewRelationService(mysql.NewRelationRepository(db)),
		TopologyService:        service.NewTopologyService(mysql.NewRelationRepository(db)),
		AuditService:           service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:            service.NewAuthService(mysql.NewUserRepository(db), "nav-integration-jwt-secret"),
		ProfileService:         profileSvc,
		EnvironmentService:     service.NewEnvironmentService(dictRepo),
		OwnerService:           service.NewOwnerService(dictRepo),
		RoleService:            service.NewRoleService(dictRepo),
		ResourceTypeService:    service.NewResourceTypeService(dictRepo),
		RelationTypeService:    service.NewRelationTypeService(dictRepo),
		LifecycleStatusService: service.NewLifecycleStatusService(dictRepo),
		HealthStatusService:    service.NewHealthStatusService(dictRepo),
		ResourceSubtypeService: service.NewResourceSubtypeService(),
		QueryTargetService:     service.NewQueryTargetService(queryTargetRepo).WithCredentialReader(queryExecutionRepo).WithCredentialResolver(credentialResolver),
		QueryCredentialService: service.NewQueryCredentialService(queryTargetRepo, queryExecutionRepo, credentialResolver),
		QueryExecutionService:  queryExecutionSvc,
		QuerySchemaService: service.NewQuerySchemaService(
			service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver),
			service.NewMySQLSchemaInspector(),
			service.NewQuerySchemaCache(256, wallClock{}),
			queryExecutionRepo,
			wallClock{},
		),
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       time.Now,
		},
	}

	router := api.NewRouter(deps)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find available port: %v", err)
	}
	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())

	server := &http.Server{Handler: router}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()
	t.Cleanup(func() { server.Close() })

	if !waitForServer(t, baseURL, 10*time.Second) {
		t.Fatal("server did not become ready")
	}

	return baseURL, res.ID, db
}

func navLogin(t *testing.T, baseURL string) string {
	t.Helper()
	body := `{"email":"admin@example.com","password":"secret123"}`
	resp, err := http.Post(baseURL+"/auth/login", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status = %d: %s", resp.StatusCode, string(b))
	}
	var lr struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if lr.Token == "" {
		t.Fatal("login returned empty token")
	}
	return lr.Token
}

func TestNavigateRelatedRecords_Integration_Success(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	token := navLogin(t, baseURL)

	body := `{
		"source": {
			"database": "query_e2e_aux",
			"object": "schema_child",
			"kind": "table",
			"foreignKey": "fk_schema_child_parent"
		},
		"localValues": ["1"]
	}`

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/query-targets/%d/related-records", baseURL, targetID), bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("navigate request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("status=%d body=%s", resp.StatusCode, string(respBody))

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, string(respBody))
	}

	var navResp struct {
		Status             string `json:"status"`
		SourceDatabase     string `json:"sourceDatabase"`
		SourceObject       string `json:"sourceObject"`
		ForeignKey         string `json:"foreignKey"`
		ReferencedDatabase string `json:"referencedDatabase"`
		ReferencedObject   string `json:"referencedObject"`
		Columns            []struct {
			Name         string `json:"name"`
			DatabaseType string `json:"databaseType"`
		} `json:"columns"`
		Rows       [][]interface{} `json:"rows"`
		RowCount   int             `json:"rowCount"`
		ExecutedAt string          `json:"executedAt"`
	}
	if err := json.Unmarshal(respBody, &navResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if navResp.Status != string(model.QueryExecutionSuccess) {
		t.Fatalf("status = %q, want success", navResp.Status)
	}
	if navResp.ReferencedDatabase != "query_e2e_aux" {
		t.Fatalf("referencedDatabase = %q, want query_e2e_aux", navResp.ReferencedDatabase)
	}
	if navResp.ReferencedObject != "schema_parent" {
		t.Fatalf("referencedObject = %q, want schema_parent", navResp.ReferencedObject)
	}
	if navResp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", navResp.RowCount)
	}
	if len(navResp.Rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(navResp.Rows))
	}
	if navResp.ExecutedAt == "" {
		t.Fatal("executedAt must not be empty")
	}

	// --- Persistence secrecy assertions (P1) ---
	// Read the query_executions row to prove localValues, SQL, and credentials
	// are never persisted.
	var preview, digest, errorCode, errorMessage string
	err = db.QueryRow(
		`SELECT statement_preview, statement_digest, error_code, error_message
		 FROM query_executions WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`,
		targetID,
	).Scan(&preview, &digest, &errorCode, &errorMessage)
	if err != nil {
		t.Fatalf("read query_executions: %v", err)
	}

	// localValues must never appear in any persisted field.
	for _, forbidden := range []string{"P_ALPHA", "child_a1", "1"} {
		if strings.Contains(preview, forbidden) {
			t.Errorf("statement_preview leaks localValue %q: %s", forbidden, preview)
		}
		if strings.Contains(digest, forbidden) {
			t.Errorf("statement_digest leaks localValue %q: %s", forbidden, digest)
		}
		if strings.Contains(errorMessage, forbidden) {
			t.Errorf("error_message leaks localValue %q: %s", forbidden, errorMessage)
		}
	}

	// SQL text must never appear in preview/digest.
	if strings.Contains(preview, "SELECT") || strings.Contains(preview, "WHERE") {
		t.Errorf("statement_preview leaks SQL: %s", preview)
	}
	if strings.Contains(digest, "SELECT") || strings.Contains(digest, "WHERE") {
		t.Errorf("statement_digest leaks SQL: %s", digest)
	}

	// DSN/credential markers must never appear.
	for _, marker := range []string{"tcp(", "@", "://", "root:", "password", navCredentialRef} {
		if strings.Contains(preview, marker) {
			t.Errorf("statement_preview leaks credential marker %q: %s", marker, preview)
		}
		if strings.Contains(digest, marker) {
			t.Errorf("statement_digest leaks credential marker %q: %s", marker, digest)
		}
		if strings.Contains(errorMessage, marker) {
			t.Errorf("error_message leaks credential marker %q: %s", marker, errorMessage)
		}
	}

	// Read the audit_events row to prove the action is fixed and no sensitive data leaks.
	var auditAction, auditResult string
	err = db.QueryRow(
		`SELECT event_type, result FROM audit_events WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`,
		targetID,
	).Scan(&auditAction, &auditResult)
	if err != nil {
		t.Fatalf("read audit_events: %v", err)
	}
	if auditAction != "related_record_navigation" {
		t.Errorf("audit action = %q, want related_record_navigation", auditAction)
	}
	if auditResult != "success" {
		t.Errorf("audit result = %q, want success", auditResult)
	}
}

func TestNavigateRelatedRecords_Integration_EmptyLocalValue(t *testing.T) {
	baseURL, targetID, _ := setupNavigateFixture(t)
	token := navLogin(t, baseURL)

	body := `{
		"source": {
			"database": "query_e2e_aux",
			"object": "schema_child",
			"kind": "table",
			"foreignKey": "fk_schema_child_parent"
		},
		"localValues": [""]
	}`

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/query-targets/%d/related-records", baseURL, targetID), bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("navigate request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("status=%d body=%s", resp.StatusCode, string(respBody))

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, string(respBody))
	}

	var navResp struct {
		RowCount int `json:"rowCount"`
	}
	if err := json.Unmarshal(respBody, &navResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Empty string value matches no rows, but should succeed.
	if navResp.RowCount != 0 {
		t.Fatalf("rowCount = %d, want 0 for empty value", navResp.RowCount)
	}
}

func TestNavigateRelatedRecords_Integration_MissingBearer(t *testing.T) {
	baseURL, targetID, _ := setupNavigateFixture(t)

	body := `{
		"source": {
			"database": "query_e2e_aux",
			"object": "schema_child",
			"kind": "table",
			"foreignKey": "fk_schema_child_parent"
		},
		"localValues": ["1"]
	}`

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/query-targets/%d/related-records", baseURL, targetID), bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("navigate request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 401: %s", resp.StatusCode, string(respBody))
	}
}
