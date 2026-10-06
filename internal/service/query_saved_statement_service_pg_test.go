// Package service provides PostgreSQL tests for QuerySavedStatementService.
// input: context, errors, testing, internal/model
// output: TestQuerySavedStatementServicePG* — engine-aware context validation, PG declaration validation through the real guard/pagination path, and the G12 RestoreContext revalidation matrix
// pos: T11 service-boundary evidence for saved-statement context persistence and restore; unit-level seams only — the live-PostgreSQL restore proof lives in the integration file
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// newPGSavedStatementService wires a saved-statement service for a PostgreSQL
// target with the same store/resolver/catalog seams the schema tests use.
func newPGSavedStatementService(reader QuerySavedStatementReader, writer QuerySavedStatementWriter, target model.QueryTarget, store *pgCredStore, resolver *pgRefResolver) (*QuerySavedStatementService, *fakePGCatalog) {
	targets := fakeTargetRepo{targets: []model.QueryTarget{target}}
	cat := &fakePGCatalog{}
	svc := NewQuerySavedStatementService(reader, writer, targets, &fakeSavedStatementGuard{}).
		WithPGContext(store, NewTargetAccessResolver(targets, store, resolver))
	svc.pgCatalog = cat
	return svc, cat
}

func TestQuerySavedStatementServicePG_CreateValidatesConnectionContext(t *testing.T) {
	t.Parallel()
	target := pgTarget("Staging")
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: pgRows()}
	resolver := &pgRefResolver{dsns: pgDSNs()}

	t.Run("requires a database segment", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ctx",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("empty database error = %v, want validation failure", err)
		}
		if writer.createReq.Name != "" {
			t.Fatal("repository mutation occurred without a connection context")
		}
	})

	t.Run("rejects an unknown connection", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ctx",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_missing",
			Schema:    "app",
		})
		if !errors.Is(err, ErrSchemaConnectionNotFound) {
			t.Fatalf("unknown connection error = %v, want ErrSchemaConnectionNotFound", err)
		}
		if writer.createReq.Name != "" {
			t.Fatal("repository mutation occurred for an unknown connection")
		}
	})

	t.Run("persists database, schema, and declarations", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{createResp: model.QuerySavedStatement{ID: 7}}
		svc, cat := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ctx",
			Statement: "SELECT id FROM orders WHERE status = :status",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
			Schema:    "app",
			Parameters: []model.QuerySavedStatementParameterDefinition{
				{Name: "status", Type: model.QuerySavedStatementParameterString},
			},
		})
		if err != nil {
			t.Fatalf("create error: %v", err)
		}
		if writer.createReq.Database != "db_a" || writer.createReq.Schema != "app" {
			t.Fatalf("persisted context = %q/%q, want db_a/app", writer.createReq.Database, writer.createReq.Schema)
		}
		// Save-time validation must not connect to PostgreSQL: no resolver or
		// catalog traffic is part of the write path.
		if len(resolver.resolveCalls()) != 0 || cat.callCount() != 0 {
			t.Fatal("save-time validation reached the resolver or catalog")
		}
	})

	t.Run("empty schema is legal and resolves to the connection default at restore", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{createResp: model.QuerySavedStatement{ID: 8}}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ctx",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
		})
		if err != nil {
			t.Fatalf("create error: %v", err)
		}
		if writer.createReq.Schema != "" {
			t.Fatalf("persisted schema = %q, want empty (default_schema pin)", writer.createReq.Schema)
		}
	})
}

func TestQuerySavedStatementServicePG_CreateRunsTheRealGuardAndPaginationGate(t *testing.T) {
	t.Parallel()
	target := pgTarget("Staging")
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: pgRows()}
	resolver := &pgRefResolver{dsns: pgDSNs()}

	t.Run("LIMIT placeholder is rejected by the real pagination gate", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "paged",
			Statement: "SELECT id FROM orders LIMIT :n",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
			Parameters: []model.QuerySavedStatementParameterDefinition{
				{Name: "n", Type: model.QuerySavedStatementParameterInteger},
			},
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("LIMIT :n error = %v, want validation failure", err)
		}
		var reject *pgsql.RejectError
		if !errors.As(err, &reject) || reject.Code != "unsupported_limit_offset_form" {
			t.Fatalf("LIMIT :n error = %v, want unsupported_limit_offset_form", err)
		}
		if writer.createReq.Name != "" {
			t.Fatal("repository mutation occurred for a pagination-illegal template")
		}
	})

	t.Run("native $n input is rejected at save time", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "native",
			Statement: "SELECT * FROM orders WHERE id = $1",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("native $1 error = %v, want validation failure", err)
		}
	})

	t.Run("non-SELECT is rejected by the real guard", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ddl",
			Statement: "DROP TABLE orders",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("DDL error = %v, want validation failure", err)
		}
	})
}

func TestQuerySavedStatementServicePG_UpdateValidatesContext(t *testing.T) {
	t.Parallel()
	target := pgTarget("Staging")
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: pgRows()}
	resolver := &pgRefResolver{dsns: pgDSNs()}
	reader := &fakeSavedStatementReader{getResp: model.QuerySavedStatement{
		ID: 1, OwnerUserID: 1, Scope: model.QuerySavedStatementPersonal,
	}}

	t.Run("rejects a moved connection on update", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(reader, writer, target, store, resolver)
		err := svc.Update(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 1, model.QuerySavedStatementUpdateRequest{
			Name:      "moved",
			Statement: "SELECT 1",
			Database:  "db_missing",
		})
		if !errors.Is(err, ErrSchemaConnectionNotFound) {
			t.Fatalf("update error = %v, want ErrSchemaConnectionNotFound", err)
		}
	})
}

func TestQuerySavedStatementServicePG_RestoreContext(t *testing.T) {
	t.Parallel()
	target := pgTarget("Staging")
	saved := model.QuerySavedStatement{
		ID:           5,
		OwnerUserID:  1,
		Scope:        model.QuerySavedStatementPersonal,
		DatabaseName: "db_a",
		SchemaName:   "app",
	}

	newSvc := func(rows map[string]model.QueryCredentialMetadata, dsns map[string]string, stmt model.QuerySavedStatement) (*QuerySavedStatementService, *fakePGCatalog, *pgCredStore) {
		store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: rows}
		resolver := &pgRefResolver{dsns: dsns}
		svc, cat := newPGSavedStatementService(&fakeSavedStatementReader{getResp: stmt}, &fakeSavedStatementWriter{}, target, store, resolver)
		return svc, cat, store
	}

	t.Run("restores a live context and returns the persisted statement", func(t *testing.T) {
		svc, cat, _ := newSvc(pgRows(), pgDSNs(), saved)
		got, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if err != nil {
			assertNoUnitSecret(t, err.Error())
			t.Fatalf("restore error: %v", err)
		}
		if got.ID != 5 || got.DatabaseName != "db_a" || got.SchemaName != "app" {
			t.Fatalf("restored statement = %+v", got)
		}
		if cat.callCount() != 1 || cat.calls[0].method != "probe" || cat.calls[0].database != "db_a" || cat.calls[0].schema != "app" {
			t.Fatalf("catalog calls = %+v, want one probe of db_a/app", cat.calls)
		}
		if !cat.calls[0].passA || cat.calls[0].passB {
			t.Fatal("probe did not run on the db_a connection credential")
		}
	})

	t.Run("empty schema restores through the connection default", func(t *testing.T) {
		stmt := saved
		stmt.SchemaName = ""
		svc, cat, _ := newSvc(pgRows(), pgDSNs(), stmt)
		if _, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5); err != nil {
			t.Fatalf("restore error: %v", err)
		}
		if cat.calls[0].schema != "" || cat.calls[0].fallback != "app" {
			t.Fatalf("probe = %+v, want empty pin with app fallback", cat.calls[0])
		}
	})

	t.Run("connection disabled fails closed", func(t *testing.T) {
		stmt := saved
		stmt.DatabaseName = "db_off"
		svc, cat, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaConnectionDisabled) {
			t.Fatalf("disabled error = %v, want ErrSchemaConnectionDisabled", err)
		}
		if cat.callCount() != 0 {
			t.Fatal("disabled connection reached the catalog")
		}
	})

	t.Run("connection deleted fails closed", func(t *testing.T) {
		stmt := saved
		stmt.DatabaseName = "db_missing"
		svc, cat, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaConnectionNotFound) {
			t.Fatalf("deleted error = %v, want ErrSchemaConnectionNotFound", err)
		}
		if cat.callCount() != 0 {
			t.Fatal("deleted connection reached the catalog")
		}
	})

	t.Run("DSN rebound to another database fails closed", func(t *testing.T) {
		// PG_T6_BAD resolves to a DSN whose dbname is db_a while the row claims
		// db_wrong — the binding check must catch the drift.
		stmt := saved
		stmt.DatabaseName = "db_wrong"
		svc, cat, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaBindingMismatch) {
			t.Fatalf("rebound error = %v, want ErrSchemaBindingMismatch", err)
		}
		if cat.callCount() != 0 {
			t.Fatal("rebound connection reached the catalog")
		}
	})

	t.Run("deleted schema fails closed", func(t *testing.T) {
		stmt := saved
		stmt.SchemaName = "missing"
		svc, _, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaNotFound) {
			t.Fatalf("deleted schema error = %v, want ErrSchemaNotFound", err)
		}
	})

	t.Run("revoked USAGE fails closed", func(t *testing.T) {
		stmt := saved
		stmt.SchemaName = "secret"
		svc, _, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaNotUsable) {
			t.Fatalf("revoked USAGE error = %v, want ErrSchemaNotUsable", err)
		}
	})

	t.Run("a same-named schema on another connection is never used", func(t *testing.T) {
		// The statement pins db_b. Only db_a/app exists in the catalog view;
		// restoring db_b must resolve db_b's own credential and probe there —
		// never hop to db_a's same-named schema.
		stmt := saved
		stmt.DatabaseName = "db_b"
		svc, cat, store := newSvc(pgRows(), pgDSNs(), stmt)
		if _, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5); err != nil {
			t.Fatalf("restore error: %v", err)
		}
		if cat.calls[0].database != "db_b" || !cat.calls[0].passB || cat.calls[0].passA {
			t.Fatalf("probe = %+v, want the db_b connection's own credential", cat.calls[0])
		}
		if lookups := store.credentialLookups(); len(lookups) == 0 || lookups[len(lookups)-1] != "db_b" {
			t.Fatalf("credential lookups = %v, want db_b", lookups)
		}
	})

	t.Run("statement on a deleted database does not fall back", func(t *testing.T) {
		// db_c was deleted entirely; a statement that pinned it must fail even
		// though db_a/app would satisfy the same schema name.
		stmt := saved
		stmt.DatabaseName = "db_c"
		svc, _, _ := newSvc(pgRows(), pgDSNs(), stmt)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrSchemaConnectionNotFound) {
			t.Fatalf("deleted database error = %v, want ErrSchemaConnectionNotFound", err)
		}
	})

	t.Run("target deleted fails closed", func(t *testing.T) {
		svc, _, _ := newSvc(pgRows(), pgDSNs(), saved)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 4242, 5)
		if !errors.Is(err, ErrQueryTargetNotFound) {
			t.Fatalf("missing target error = %v, want ErrQueryTargetNotFound", err)
		}
	})

	t.Run("unwired resolver fails loud", func(t *testing.T) {
		svc := NewQuerySavedStatementService(
			&fakeSavedStatementReader{getResp: saved},
			&fakeSavedStatementWriter{},
			fakeTargetRepo{targets: []model.QueryTarget{target}},
			&fakeSavedStatementGuard{},
		)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrQueryBackendFailure) {
			t.Fatalf("unwired error = %v, want ErrQueryBackendFailure", err)
		}
	})

	t.Run("non-owner cannot restore a personal statement", func(t *testing.T) {
		svc, _, _ := newSvc(pgRows(), pgDSNs(), saved)
		_, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 9, Role: "editor"}, 9001, 5)
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Fatalf("non-owner error = %v, want ErrQuerySavedStatementNotFound", err)
		}
	})
}

func TestQuerySavedStatementServicePG_MySQLKeepsLegacySemantics(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: map[string]model.QueryCredentialMetadata{}}
	resolver := &pgRefResolver{dsns: map[string]string{}}

	t.Run("non-empty context is rejected for mysql", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "ctx",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
			Database:  "db_a",
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("mysql context error = %v, want validation failure", err)
		}
	})

	t.Run("empty context still saves without touching PG wiring", func(t *testing.T) {
		writer := &fakeSavedStatementWriter{createResp: model.QuerySavedStatement{ID: 3}}
		svc, _ := newPGSavedStatementService(&fakeSavedStatementReader{}, writer, target, store, resolver)
		_, err := svc.Create(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, model.QuerySavedStatementCreateRequest{
			Name:      "legacy",
			Statement: "SELECT 1",
			Scope:     model.QuerySavedStatementPersonal,
		})
		if err != nil {
			t.Fatalf("create error: %v", err)
		}
	})

	t.Run("restore is an authorized read only", func(t *testing.T) {
		svc, _ := newPGSavedStatementService(
			&fakeSavedStatementReader{getResp: model.QuerySavedStatement{ID: 4, OwnerUserID: 1, Scope: model.QuerySavedStatementPersonal}},
			&fakeSavedStatementWriter{}, target, store, resolver)
		got, err := svc.RestoreContext(context.Background(), AuthenticatedUser{ID: 1, Role: "editor"}, 9001, 4)
		if err != nil {
			t.Fatalf("restore error: %v", err)
		}
		if got.ID != 4 {
			t.Fatalf("restored statement = %+v", got)
		}
		if len(store.credentialLookups()) != 0 {
			t.Fatal("mysql restore touched credential metadata")
		}
	})
}
