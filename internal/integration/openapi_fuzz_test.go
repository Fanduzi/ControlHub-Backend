//go:build integration

package integration

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestOpenAPIFuzz starts a real ControlHub HTTP server backed by the
// disposable Testcontainers MySQL database, then invokes the
// scripts/openapi-fuzz.sh Schemathesis wrapper against it.
//
// This test validates that:
//   - no endpoint returns unexpected 5xx responses
//   - responses match declared OpenAPI schemas
//   - declared status codes and content types are respected
//   - query/path parameter edge cases do not crash handlers
//
// The database is disposable — write endpoints are exercised freely.
// The daily local controlhub database is never touched.
func TestOpenAPIFuzz(t *testing.T) {
	db := setupTestDB(t)

	// Pick a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find available port: %v", err)
	}
	addr := listener.Addr().String()
	baseURL := fmt.Sprintf("http://%s", addr)

	// Wire real dependencies (same as cmd/server/main.go but with test DB).
	dictRepo := mysql.NewDictionaryRepository(db)
	relationRepo := mysql.NewRelationRepository(db)

	resourceRepo := mysql.NewResourceRepository(db)
	profileSvc := service.NewProfileService(resourceRepo, resourceRepo)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	queryExecutionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
	)

	deps := api.Dependencies{
		ResourceService:        service.NewResourceService(resourceRepo, profileSvc),
		RelationService:        service.NewRelationService(relationRepo),
		TopologyService:        service.NewTopologyService(relationRepo),
		AuditService:           service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:            service.NewAuthService(mysql.NewUserRepository(db), "fuzz-test-jwt-secret"),
		ProfileService:         profileSvc,
		EnvironmentService:     service.NewEnvironmentService(dictRepo),
		OwnerService:           service.NewOwnerService(dictRepo),
		RoleService:            service.NewRoleService(dictRepo),
		ResourceTypeService:    service.NewResourceTypeService(dictRepo),
		RelationTypeService:    service.NewRelationTypeService(dictRepo),
		LifecycleStatusService: service.NewLifecycleStatusService(dictRepo),
		HealthStatusService:    service.NewHealthStatusService(dictRepo),
		ResourceSubtypeService: service.NewResourceSubtypeService(),
		QueryTargetService:     service.NewQueryTargetService(queryTargetRepo).WithCredentialReader(queryExecutionRepo),
		QueryExecutionService:  queryExecutionSvc,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       time.Now,
		},
	}

	router := api.NewRouter(deps)
	server := &http.Server{Handler: router}

	// Start server in background.
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()
	t.Cleanup(func() {
		server.Close()
	})

	// Wait for server to be ready.
	if !waitForServer(t, baseURL, 10*time.Second) {
		t.Fatal("server did not become ready within timeout")
	}

	// Locate the openapi-fuzz.sh script.
	scriptPath := resolveScript(t, "openapi-fuzz.sh")

	// Run Schemathesis via the shell wrapper.
	t.Logf("Invoking Schemathesis against %s", baseURL)
	cmd := exec.Command("/bin/bash", scriptPath, baseURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Schemathesis exits non-zero when it finds violations.
		// Report them as test failures.
		t.Errorf("Schemathesis found contract violations: %v", err)
	}
}

// waitForServer polls the /health endpoint until the server responds.
func waitForServer(t *testing.T, baseURL string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// resolveScript finds a script in the project's scripts/ directory.
func resolveScript(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "scripts", name)
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve script path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("script not found at %s: %v", abs, err)
	}
	return abs
}

// wallClock implements service.Clock with the wall clock for integration tests.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
