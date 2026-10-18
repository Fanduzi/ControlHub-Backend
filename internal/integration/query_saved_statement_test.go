//go:build integration

// Package integration provides Testcontainers-backed tests for governed saved statement persistence.
// input: shared MySQL fixture, saved-statement repository, target fixtures
// output: owner-scoped CRUD/visibility lifecycle, the legacy empty-context regression anchor (T11), and TestSavedStatementSnapshot_ConsistentAcrossConcurrentUpdate — GetByID must return one coherent statement/declaration version under concurrent UpdateWithAudit
// pos: Real-MySQL persistence boundary for Phase 38R saved statements
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

func TestQuerySavedStatementRepositoryPersonalParameterizedLifecycle(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQuerySavedStatementRepository(db)
	ctx := context.Background()
	const targetID uint64 = 8800000001
	const ownerID uint64 = 8800000002

	created, err := repo.CreateWithAudit(ctx, ownerID, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "Status query",
		Statement: "SELECT 1 WHERE status = :status",
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	visible, err := repo.ListVisible(ctx, model.QuerySavedStatementListQuery{
		TargetResourceID: targetID,
		OwnerUserID:      ownerID,
		Page:             1,
		PageSize:         20,
	})
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if len(visible.Items) != 1 || len(visible.Items[0].Parameters) != 1 {
		t.Fatalf("owner list = %+v, want one definition", visible.Items)
	}
	// Legacy MySQL/TiDB rows carry the empty composite context — T11 must not
	// change that shape.
	if visible.Items[0].DatabaseName != "" || visible.Items[0].SchemaName != "" {
		t.Fatalf("legacy context = %q/%q, want empty", visible.Items[0].DatabaseName, visible.Items[0].SchemaName)
	}

	nonOwner, err := repo.ListVisible(ctx, model.QuerySavedStatementListQuery{
		TargetResourceID: targetID,
		OwnerUserID:      1,
		Page:             1,
		PageSize:         20,
	})
	if err != nil {
		t.Fatalf("non-owner list: %v", err)
	}
	if len(nonOwner.Items) != 0 {
		t.Fatalf("non-owner list = %+v, want no personal records", nonOwner.Items)
	}

	if err := repo.UpdateWithAudit(ctx, ownerID, targetID, created.ID, model.QuerySavedStatementUpdateRequest{
		Name:      "Amount query",
		Statement: "SELECT 1 WHERE amount >= :minimum_total",
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "minimum_total", Type: model.QuerySavedStatementParameterDecimal},
		},
	}, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := repo.GetByID(ctx, targetID, created.ID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if len(updated.Parameters) != 1 || updated.Parameters[0].Name != "minimum_total" {
		t.Fatalf("updated parameters = %+v, want replacement", updated.Parameters)
	}

	static, err := repo.CreateWithAudit(ctx, ownerID, targetID+1, model.QuerySavedStatementCreateRequest{
		Name:      "Static query",
		Statement: "SELECT 1",
		Scope:     model.QuerySavedStatementPersonal,
	})
	if err != nil {
		t.Fatalf("create static: %v", err)
	}
	if static.Parameters == nil {
		t.Fatal("static create returned nil parameters; API compatibility requires []")
	}

	if err := repo.DeleteWithAudit(ctx, ownerID, targetID, created.ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var parentCount, parameterCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_saved_statements WHERE id = ?`, created.ID).Scan(&parentCount); err != nil {
		t.Fatalf("count parent: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_saved_statement_parameters WHERE statement_id = ?`, created.ID).Scan(&parameterCount); err != nil {
		t.Fatalf("count parameters: %v", err)
	}
	if parentCount != 0 || parameterCount != 0 {
		t.Fatalf("deleted counts = parent:%d parameters:%d, want 0/0", parentCount, parameterCount)
	}
	var executionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&executionCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if executionCount != 0 {
		t.Fatalf("saved statement CRUD created %d execution rows", executionCount)
	}
}

func TestQuerySavedStatementRepositoryCreateRollsBackOnParameterFailure(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQuerySavedStatementRepository(db)
	ctx := context.Background()
	const targetID uint64 = 8800000011
	const ownerID uint64 = 8800000012

	_, err := repo.CreateWithAudit(ctx, ownerID, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "Invalid type",
		Statement: "SELECT 1",
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "value", Type: model.QuerySavedStatementParameterType("unsupported")},
		},
	})
	if err == nil {
		t.Fatal("invalid parameter type should fail the database write")
	}
	var parentCount, auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_saved_statements WHERE target_resource_id = ?`, targetID).Scan(&parentCount); err != nil {
		t.Fatalf("count parent: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.saved_statement.created'`, targetID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if parentCount != 0 || auditCount != 0 {
		t.Fatalf("failed create committed parent:%d audit:%d", parentCount, auditCount)
	}
}

func TestQuerySavedStatementRepositoryGetNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQuerySavedStatementRepository(db)
	_, err := repo.GetByID(context.Background(), 8800000021, 8800000022)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetByID error = %v, want sql.ErrNoRows", err)
	}
}

// GetByID returns one statement-level snapshot: a concurrent UpdateWithAudit
// swaps statement text and declarations atomically, so every observed read
// must be exactly version A or exactly version B — never a mixed pair. A
// two-query header+parameters read could observe a header committed after
// the update together with declarations deleted/inserted by it.
func TestSavedStatementSnapshot_ConsistentAcrossConcurrentUpdate(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQuerySavedStatementRepository(db)
	ctx := context.Background()
	const targetID uint64 = 8800000031
	const ownerID uint64 = 8800000032

	created, err := repo.CreateWithAudit(ctx, ownerID, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "Snapshot probe",
		Statement: "SELECT 1 WHERE tag = :v1",
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "v1", Type: model.QuerySavedStatementParameterString},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	versionA := model.QuerySavedStatementUpdateRequest{
		Name:      "Snapshot probe",
		Statement: "SELECT 1 WHERE tag = :v1",
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "v1", Type: model.QuerySavedStatementParameterString},
		},
	}
	versionB := model.QuerySavedStatementUpdateRequest{
		Name:      "Snapshot probe",
		Statement: "SELECT 2 WHERE tag = :v2",
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "v2", Type: model.QuerySavedStatementParameterInteger},
		},
	}

	isCoherent := func(s model.QuerySavedStatement) bool {
		a := strings.Contains(s.Statement, ":v1") &&
			len(s.Parameters) == 1 && s.Parameters[0].Name == "v1" &&
			s.Parameters[0].Type == model.QuerySavedStatementParameterString
		b := strings.Contains(s.Statement, ":v2") &&
			len(s.Parameters) == 1 && s.Parameters[0].Name == "v2" &&
			s.Parameters[0].Type == model.QuerySavedStatementParameterInteger
		return a || b
	}

	// Bounded work: the updater applies 40 alternating versions while each
	// of 40 readers takes a fixed 20 reads — a start barrier maximizes
	// overlap and an updater error can never leave readers waiting on a
	// stop signal that never closes. Every goroutine reports through errCh
	// (capacity covers all writers); nothing calls t.Fatal off the main
	// goroutine.
	const updates, readers, readsEach = 40, 40, 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	var observed atomic.Int64
	errCh := make(chan error, readers+1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < updates; i++ {
			version := versionA
			if i%2 == 1 {
				version = versionB
			}
			if err := repo.UpdateWithAudit(ctx, ownerID, targetID, created.ID, version, false); err != nil {
				errCh <- fmt.Errorf("update %d: %w", i, err)
				return
			}
		}
	}()
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < readsEach; i++ {
				s, err := repo.GetByID(ctx, targetID, created.ID)
				if err != nil {
					errCh <- fmt.Errorf("get: %w", err)
					return
				}
				observed.Add(1)
				if !isCoherent(s) {
					errCh <- fmt.Errorf("mixed snapshot: statement=%q parameters=%+v", s.Statement, s.Parameters)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("snapshot consistency: %v", err)
		}
	}
	if observed.Load() == 0 {
		t.Fatal("no snapshots observed — coherence was never exercised")
	}
}
