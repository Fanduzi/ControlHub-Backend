// Package service provides tests for QuerySavedStatementService.
package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// fakeSavedStatementReader implements QuerySavedStatementReader for testing.
type fakeSavedStatementReader struct {
	listResp model.QuerySavedStatementListResponse
	listErr  error
	getResp  model.QuerySavedStatement
	getErr   error
}

func (f *fakeSavedStatementReader) ListVisible(_ context.Context, _ model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error) {
	return f.listResp, f.listErr
}

func (f *fakeSavedStatementReader) GetByID(_ context.Context, _, _ uint64) (model.QuerySavedStatement, error) {
	return f.getResp, f.getErr
}

// fakeSavedStatementWriter implements QuerySavedStatementWriter for testing.
type fakeSavedStatementWriter struct {
	createResp model.QuerySavedStatement
	createErr  error
	updateErr  error
	deleteErr  error
}

func (f *fakeSavedStatementWriter) CreateWithAudit(_ context.Context, _, _ uint64, _ model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	return f.createResp, f.createErr
}

func (f *fakeSavedStatementWriter) UpdateWithAudit(_ context.Context, _, _, _ uint64, _ model.QuerySavedStatementUpdateRequest, _ bool) error {
	return f.updateErr
}

func (f *fakeSavedStatementWriter) DeleteWithAudit(_ context.Context, _, _, _ uint64, _ bool) error {
	return f.deleteErr
}

// fakeSavedStatementGuard implements SavedStatementGuard for testing.
type fakeSavedStatementGuard struct {
	err error
}

func (f *fakeSavedStatementGuard) GuardSavedStatement(statement string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return statement, nil
}

func TestQuerySavedStatementServiceList(t *testing.T) {
	t.Run("returns canManageSharedTemplates true for admin", func(t *testing.T) {
		// Given: an admin actor and a target with saved statements.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				listResp: model.QuerySavedStatementListResponse{
					Items: []model.QuerySavedStatement{{ID: 1}},
				},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: admin lists saved statements.
		actor := AuthenticatedUser{ID: 1, Role: "admin"}
		resp, err := svc.List(context.Background(), actor, 22, "", 1, 20)

		// Then: CanManageSharedTemplates is true.
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.CanManageSharedTemplates {
			t.Error("expected CanManageSharedTemplates true for admin")
		}
	})

	t.Run("returns canManageSharedTemplates false for non-admin", func(t *testing.T) {
		// Given: a non-admin actor and a target.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				listResp: model.QuerySavedStatementListResponse{},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: editor lists saved statements.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		resp, err := svc.List(context.Background(), actor, 22, "", 1, 20)

		// Then: CanManageSharedTemplates is false.
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CanManageSharedTemplates {
			t.Error("expected CanManageSharedTemplates false for non-admin")
		}
	})

	t.Run("returns error for missing target", func(t *testing.T) {
		// Given: a target that does not exist.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: nil},
			&fakeSavedStatementGuard{},
		)

		// When: listing saved statements for a nonexistent target.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		_, err := svc.List(context.Background(), actor, 999, "", 1, 20)

		// Then: ErrQueryTargetNotFound is returned.
		if !errors.Is(err, ErrQueryTargetNotFound) {
			t.Errorf("expected ErrQueryTargetNotFound, got %v", err)
		}
	})
}

func TestQuerySavedStatementServiceCreate(t *testing.T) {
	t.Run("creates personal statement for any actor", func(t *testing.T) {
		// Given: a non-admin actor with a valid target.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{
				createResp: model.QuerySavedStatement{ID: 1},
			},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: editor creates a personal statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementCreateRequest{
			Name:      "Test",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
		}
		_, err := svc.Create(context.Background(), actor, 22, req)

		// Then: creation succeeds.
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects shared template for non-admin", func(t *testing.T) {
		// Given: a non-admin actor.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: editor tries to create a shared template.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementCreateRequest{
			Name:      "Test",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementSharedTemplate,
		}
		_, err := svc.Create(context.Background(), actor, 22, req)

		// Then: ErrQueryForbidden is returned.
		if !errors.Is(err, ErrQueryForbidden) {
			t.Errorf("expected ErrQueryForbidden, got %v", err)
		}
	})

	t.Run("rejects invalid SQL", func(t *testing.T) {
		// Given: a guard that rejects the statement.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{err: ErrQueryStatementNotAllowed},
		)

		// When: actor tries to save a disallowed statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementCreateRequest{
			Name:      "Test",
			Statement: "DROP TABLE orders",
			Scope:     model.QuerySavedStatementPersonal,
		}
		_, err := svc.Create(context.Background(), actor, 22, req)

		// Then: ErrQueryValidationFailed wraps the guard error.
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Errorf("expected ErrQueryValidationFailed, got %v", err)
		}
	})

	t.Run("rejects missing target", func(t *testing.T) {
		// Given: a target that does not exist.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: nil},
			&fakeSavedStatementGuard{},
		)

		// When: creating a statement for a nonexistent target.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementCreateRequest{
			Name:      "Test",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
		}
		_, err := svc.Create(context.Background(), actor, 999, req)

		// Then: ErrQueryTargetNotFound is returned.
		if !errors.Is(err, ErrQueryTargetNotFound) {
			t.Errorf("expected ErrQueryTargetNotFound, got %v", err)
		}
	})

	t.Run("creates shared template for admin", func(t *testing.T) {
		// Given: an admin actor with a valid target.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{
				createResp: model.QuerySavedStatement{ID: 2, Scope: model.QuerySavedStatementSharedTemplate},
			},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: admin creates a shared template.
		actor := AuthenticatedUser{ID: 1, Role: "admin"}
		req := model.QuerySavedStatementCreateRequest{
			Name:      "Shared",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementSharedTemplate,
		}
		_, err := svc.Create(context.Background(), actor, 22, req)

		// Then: creation succeeds.
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestQuerySavedStatementServiceUpdate(t *testing.T) {
	t.Run("returns not found for missing statement", func(t *testing.T) {
		// Given: a writer that returns sql.ErrNoRows on update.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{updateErr: sql.ErrNoRows},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: updating a nonexistent statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementUpdateRequest{
			Name:      "Updated",
			Statement: "SELECT 2",
		}
		err := svc.Update(context.Background(), actor, 22, 999, req)

		// Then: ErrQuerySavedStatementNotFound is returned.
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Errorf("expected ErrQuerySavedStatementNotFound, got %v", err)
		}
	})

	t.Run("rejects invalid SQL on update", func(t *testing.T) {
		// Given: a guard that rejects the statement.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{err: ErrQueryStatementNotAllowed},
		)

		// When: updating with a disallowed statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementUpdateRequest{
			Name:      "Bad",
			Statement: "DROP TABLE t",
		}
		err := svc.Update(context.Background(), actor, 22, 1, req)

		// Then: ErrQueryValidationFailed wraps the guard error.
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Errorf("expected ErrQueryValidationFailed, got %v", err)
		}
	})

	t.Run("rejects non-admin updating shared template", func(t *testing.T) {
		// Given: a shared_template statement owned by user 2.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				getResp: model.QuerySavedStatement{
					ID:          1,
					OwnerUserID: 2,
					Scope:       model.QuerySavedStatementSharedTemplate,
				},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: a non-admin actor tries to update a shared template.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementUpdateRequest{
			Name:      "Updated",
			Statement: "SELECT 2",
		}
		err := svc.Update(context.Background(), actor, 22, 1, req)

		// Then: ErrQueryForbidden is returned.
		if !errors.Is(err, ErrQueryForbidden) {
			t.Errorf("expected ErrQueryForbidden, got %v", err)
		}
	})

	t.Run("rejects non-owner updating another user's personal statement", func(t *testing.T) {
		// Given: a personal statement owned by user 2.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				getResp: model.QuerySavedStatement{
					ID:          1,
					OwnerUserID: 2,
					Scope:       model.QuerySavedStatementPersonal,
				},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: a non-owner actor (id=1) tries to update user 2's personal statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementUpdateRequest{
			Name:      "Updated",
			Statement: "SELECT 2",
		}
		err := svc.Update(context.Background(), actor, 22, 1, req)

		// Then: ErrQuerySavedStatementNotFound is returned (owner mismatch).
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Errorf("expected ErrQuerySavedStatementNotFound, got %v", err)
		}
	})

	t.Run("rejects missing target on update", func(t *testing.T) {
		// Given: a target that does not exist.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: nil},
			&fakeSavedStatementGuard{},
		)

		// When: updating a statement for a nonexistent target.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		req := model.QuerySavedStatementUpdateRequest{
			Name:      "Updated",
			Statement: "SELECT 2",
		}
		err := svc.Update(context.Background(), actor, 999, 1, req)

		// Then: ErrQueryTargetNotFound is returned.
		if !errors.Is(err, ErrQueryTargetNotFound) {
			t.Errorf("expected ErrQueryTargetNotFound, got %v", err)
		}
	})
}

func TestQuerySavedStatementServiceDelete(t *testing.T) {
	t.Run("returns not found for missing statement", func(t *testing.T) {
		// Given: a writer that returns sql.ErrNoRows on delete.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{deleteErr: sql.ErrNoRows},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: deleting a nonexistent statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		err := svc.Delete(context.Background(), actor, 22, 999)

		// Then: ErrQuerySavedStatementNotFound is returned.
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Errorf("expected ErrQuerySavedStatementNotFound, got %v", err)
		}
	})

	t.Run("returns error for missing target", func(t *testing.T) {
		// Given: a target that does not exist.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: nil},
			&fakeSavedStatementGuard{},
		)

		// When: deleting from a nonexistent target.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		err := svc.Delete(context.Background(), actor, 999, 1)

		// Then: ErrQueryTargetNotFound is returned.
		if !errors.Is(err, ErrQueryTargetNotFound) {
			t.Errorf("expected ErrQueryTargetNotFound, got %v", err)
		}
	})

	t.Run("rejects non-admin deleting shared template", func(t *testing.T) {
		// Given: a shared_template statement owned by user 2.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				getResp: model.QuerySavedStatement{
					ID:          1,
					OwnerUserID: 2,
					Scope:       model.QuerySavedStatementSharedTemplate,
				},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: a non-admin actor tries to delete a shared template.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		err := svc.Delete(context.Background(), actor, 22, 1)

		// Then: ErrQueryForbidden is returned.
		if !errors.Is(err, ErrQueryForbidden) {
			t.Errorf("expected ErrQueryForbidden, got %v", err)
		}
	})

	t.Run("rejects non-owner deleting another user's personal statement", func(t *testing.T) {
		// Given: a personal statement owned by user 2.
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{
				getResp: model.QuerySavedStatement{
					ID:          1,
					OwnerUserID: 2,
					Scope:       model.QuerySavedStatementPersonal,
				},
			},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 22}}},
			&fakeSavedStatementGuard{},
		)

		// When: a non-owner actor (id=1) tries to delete user 2's personal statement.
		actor := AuthenticatedUser{ID: 1, Role: "editor"}
		err := svc.Delete(context.Background(), actor, 22, 1)

		// Then: ErrQuerySavedStatementNotFound is returned (owner mismatch).
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Errorf("expected ErrQuerySavedStatementNotFound, got %v", err)
		}
	})
}
