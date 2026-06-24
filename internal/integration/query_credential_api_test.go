//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 38A
// query credential metadata API path (Task B6): admin upsert/get/delete through
// the real service + repository + resolver, the runtime-gated readiness
// correction, audit recording, and the no-DSN-stored invariant.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// qcCredentialRef is the opaque ref exercised by the credential API tests. It is
// a safe [A-Z0-9_]+ key; the DSN it resolves to lives only in the environment.
const qcCredentialRef = "ORDER_MYSQL_RO"

// qcAdmin is the admin actor the credential service accepts for writes.
func qcAdmin() service.AuthenticatedUser {
	return service.AuthenticatedUser{ID: ownerDBA, Role: "admin"}
}

// newCredentialApiTarget provisions a mysql/staging query target whose profile
// host/port optionally match the disposable test MySQL DSN, and writes NO
// credential row. Returns the target resource id and the db handle.
func newCredentialApiTarget(t *testing.T, matchDSN bool) (uint64, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := mysql.NewResourceRepository(db).CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qc-api-target-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "QC API Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create credential api target resource: %v", err)
	}

	dsnCfg, err := mysqldriver.ParseDSN(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	dsnHost, dsnPortStr, err := net.SplitHostPort(dsnCfg.Addr)
	if err != nil {
		t.Fatalf("split test dsn addr %q: %v", dsnCfg.Addr, err)
	}
	dsnPort, err := strconv.Atoi(dsnPortStr)
	if err != nil {
		t.Fatalf("parse test dsn port %q: %v", dsnPortStr, err)
	}

	host, port := dsnHost, dsnPort
	if !matchDSN {
		host, port = "mismatch.invalid", 9999
	}
	mustExec(t, db, `insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, host, port)
	return res.ID, db
}

func newCredentialApiSvc(db *sql.DB) *service.QueryCredentialService {
	return service.NewQueryCredentialService(
		mysql.NewQueryTargetRepository(db),
		mysql.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
	)
}

// newRuntimeReadinessSvc wires the query target read model with BOTH the
// credential reader and the resolver, mirroring cmd/server/main.go, so the
// Phase 38A readiness correction (ready only on secret_resolved) is exercised.
func newRuntimeReadinessSvc(db *sql.DB) *service.QueryTargetService {
	return service.NewQueryTargetService(mysql.NewQueryTargetRepository(db)).
		WithCredentialReader(mysql.NewQueryExecutionRepository(db)).
		WithCredentialResolver(service.NewEnvCredentialResolver())
}

func qcReadyTarget(t *testing.T, db *sql.DB, ctx context.Context, targetID uint64) model.QueryTarget {
	t.Helper()
	return *findTargetByID(t, mustList(t, newRuntimeReadinessSvc(db), ctx), targetID)
}

// TestQueryCredentialAPI_AdminPutGetDelete_Lifecycle proves the full admin
// lifecycle against real MySQL: PUT persists metadata, GET returns a configured
// status, and DELETE removes it so the target locks as credential_required.
func TestQueryCredentialAPI_AdminPutGetDelete_Lifecycle(t *testing.T) {
	targetID, db := newCredentialApiTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, globalEnv.dsn)
	svc := newCredentialApiSvc(db)

	// Before any metadata, GET reports not configured.
	before, err := svc.GetStatus(ctx, targetID)
	if err != nil {
		t.Fatalf("get before put: %v", err)
	}
	if before.Configured || before.RuntimeStatus != model.QueryCredentialRuntimeMissingMetadata {
		t.Fatalf("before put: %+v", before)
	}

	// Admin PUT with a resolvable, bound secret -> secret_resolved + eligible.
	upserted, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef:     qcCredentialRef,
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !upserted.Configured || upserted.RuntimeStatus != model.QueryCredentialRuntimeSecretResolved || !upserted.ExecutionEligible {
		t.Fatalf("upsert status = %+v, want configured/secret_resolved/eligible", upserted)
	}

	// GET returns the configured status.
	got, err := svc.GetStatus(ctx, targetID)
	if err != nil {
		t.Fatalf("get after put: %v", err)
	}
	if got.CredentialRef != qcCredentialRef || !got.Enabled || got.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly {
		t.Fatalf("get after put = %+v", got)
	}

	// DELETE removes metadata; GET reads not-configured again.
	if err := svc.Delete(ctx, qcAdmin(), targetID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := svc.GetStatus(ctx, targetID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if after.Configured || after.RuntimeStatus != model.QueryCredentialRuntimeMissingMetadata {
		t.Fatalf("after delete: %+v", after)
	}
}

// TestQueryCredentialAPI_SecretResolvedMakesTargetReady is the Phase 38A readiness
// correction proof: with metadata present and a resolvable, bound secret, GET
// /query-targets marks the target ready (run=true) — and NOT before.
func TestQueryCredentialAPI_SecretResolvedMakesTargetReady(t *testing.T) {
	targetID, db := newCredentialApiTarget(t, true)
	ctx := context.Background()
	svc := newCredentialApiSvc(db)

	// No metadata -> locked.
	if tgt := qcReadyTarget(t, db, ctx, targetID); tgt.Readiness == model.ReadinessReady || tgt.AvailableActions.Run {
		t.Fatal("target must not be ready before credential metadata exists")
	}

	// Secret not provisioned yet: PUT succeeds, but secret_missing keeps it locked.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, "")
	if _, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("upsert with unresolved secret: %v", err)
	}
	if tgt := qcReadyTarget(t, db, ctx, targetID); tgt.Readiness == model.ReadinessReady || tgt.AvailableActions.Run {
		t.Fatal("target must NOT be ready when the secret is missing (metadata alone is insufficient)")
	}
	status, _ := svc.GetStatus(ctx, targetID)
	if status.RuntimeStatus != model.QueryCredentialRuntimeSecretMissing {
		t.Fatalf("runtime = %q, want secret_missing", status.RuntimeStatus)
	}

	// Provision the secret with a matching binding -> secret_resolved -> READY.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, globalEnv.dsn)
	if _, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("upsert with resolved secret: %v", err)
	}
	tgt := qcReadyTarget(t, db, ctx, targetID)
	if tgt.Readiness != model.ReadinessReady {
		t.Fatalf("readiness = %q, want ready after secret resolves+binds", tgt.Readiness)
	}
	if !tgt.AvailableActions.Run || !tgt.Governance.ExecutionEnabled {
		t.Fatal("ready target must expose run=true and executionEnabled=true")
	}
}

// TestQueryCredentialAPI_BindingMismatchLocksTarget proves a credential that
// resolves but does not bind to the target keeps the target locked with
// binding_mismatch.
func TestQueryCredentialAPI_BindingMismatchLocksTarget(t *testing.T) {
	targetID, db := newCredentialApiTarget(t, false) // profile host/port != DSN
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, globalEnv.dsn)
	svc := newCredentialApiSvc(db)

	if _, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyAllEnvironments, ConfirmAllEnvironments: true,
	}); err != nil {
		t.Fatalf("upsert mismatched: %v", err)
	}
	status, err := svc.GetStatus(ctx, targetID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if status.RuntimeStatus != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("runtime = %q, want binding_mismatch", status.RuntimeStatus)
	}
	if status.ExecutionEligible {
		t.Fatal("binding_mismatch must not be execution eligible")
	}
	if tgt := qcReadyTarget(t, db, ctx, targetID); tgt.Readiness == model.ReadinessReady || tgt.AvailableActions.Run {
		t.Fatal("mismatched target must not be ready")
	}
}

// TestQueryCredentialAPI_AuditAndNoDSN proves successful PUT and DELETE write
// audit rows, and that no DSN appears in the metadata table, the audit rows, or
// any status response.
func TestQueryCredentialAPI_AuditAndNoDSN(t *testing.T) {
	targetID, db := newCredentialApiTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, globalEnv.dsn)
	svc := newCredentialApiSvc(db)

	resp, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := svc.Delete(ctx, qcAdmin(), targetID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Audit rows for both events.
	if n := qcAuditCount(t, db, targetID, "query.credential.updated"); n != 1 {
		t.Fatalf("updated audit rows = %d, want 1", n)
	}
	if n := qcAuditCount(t, db, targetID, "query.credential.deleted"); n != 1 {
		t.Fatalf("deleted audit rows = %d, want 1", n)
	}

	// No DSN-looking value in the credential metadata columns (now empty after delete,
	// so also assert the prior row, while it existed, stored no DSN via the upsert path —
	// re-upsert to re-create the row and inspect it).
	if _, err := svc.Upsert(ctx, qcAdmin(), targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("re-upsert for DSN inspection: %v", err)
	}
	qcAssertMetadataStoresNoDSN(t, db, targetID, globalEnv.dsn)

	// No audit row column carries a DSN-looking value.
	qcAssertAuditStoresNoDSN(t, db, targetID)

	// No DSN in the status response JSON.
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	qcAssertBodyNoDSN(t, string(raw), globalEnv.dsn)
}

// TestQueryCredentialAPI_NonAdminRejected proves a non-admin actor cannot upsert
// or delete through the real service, and no metadata or audit row is written.
func TestQueryCredentialAPI_NonAdminRejected(t *testing.T) {
	targetID, db := newCredentialApiTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+qcCredentialRef, globalEnv.dsn)
	svc := newCredentialApiSvc(db)
	viewer := service.AuthenticatedUser{ID: ownerDBA + 1, Role: "viewer"}

	if _, err := svc.Upsert(ctx, viewer, targetID, model.QueryCredentialUpsertRequest{
		CredentialRef: qcCredentialRef, Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); !errors.Is(err, service.ErrQueryCredentialForbidden) {
		t.Fatalf("non-admin upsert err = %v, want ErrQueryCredentialForbidden", err)
	}
	if err := svc.Delete(ctx, viewer, targetID); !errors.Is(err, service.ErrQueryCredentialForbidden) {
		t.Fatalf("non-admin delete err = %v, want ErrQueryCredentialForbidden", err)
	}
	if qcAuditCount(t, db, targetID, "query.credential.updated") != 0 || qcAuditCount(t, db, targetID, "query.credential.deleted") != 0 {
		t.Fatal("non-admin operations must write no audit rows")
	}
}

// qcAuditCount returns the number of audit_events rows for a target + event type.
func qcAuditCount(t *testing.T, db *sql.DB, targetID uint64, eventType string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ? and event_type = ?`, targetID, eventType).Scan(&n); err != nil {
		t.Fatalf("count audit %s: %v", eventType, err)
	}
	return n
}

// qcAssertMetadataStoresNoDSN reads the credential metadata row and asserts no
// stored column equals or looks like the DSN.
func qcAssertMetadataStoresNoDSN(t *testing.T, db *sql.DB, resourceID uint64, dsn string) {
	t.Helper()
	rows, err := db.Query(`select engine, credential_ref, environment_policy from query_target_credentials where resource_id = ?`, resourceID)
	if err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var engine, ref, policy string
		if err := rows.Scan(&engine, &ref, &policy); err != nil {
			t.Fatalf("scan metadata: %v", err)
		}
		for _, val := range []string{engine, ref, policy} {
			if val == dsn {
				t.Fatalf("metadata column equals the DSN: %q", val)
			}
			if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
				t.Fatalf("metadata column %q looks like a DSN fragment", val)
			}
		}
	}
}

// qcAssertAuditStoresNoDSN asserts no audit_events row for the target carries a
// DSN-looking value in its scalar columns.
func qcAssertAuditStoresNoDSN(t *testing.T, db *sql.DB, targetID uint64) {
	t.Helper()
	rows, err := db.Query(`select event_type, result from audit_events where target_resource_id = ?`, targetID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType, result string
		if err := rows.Scan(&eventType, &result); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		for _, val := range []string{eventType, result} {
			if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
				t.Fatalf("audit column %q looks like a DSN fragment", val)
			}
		}
	}
}

// qcAssertBodyNoDSN asserts a response body carries no DSN fragment.
func qcAssertBodyNoDSN(t *testing.T, body, dsn string) {
	t.Helper()
	if strings.Contains(body, dsn) {
		t.Fatalf("response body contains the DSN: %s", body)
	}
	for _, marker := range []string{"tcp(", "://", "root:test"} {
		if strings.Contains(body, marker) {
			t.Fatalf("response body contains DSN marker %q: %s", marker, body)
		}
	}
}
