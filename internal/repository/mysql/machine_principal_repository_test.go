// Package mysql provides MySQL-backed repository implementations.
// input: crypto/sha256, database/sql, database/sql/driver, encoding/json, strings, testing, time, sqlmock, internal/model, internal/service
// output: machine-principal persistence, audit safety, idempotent last-use, and rollback tests
// pos: SQL transaction contract coverage for the machine-principal security boundary
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func TestMachinePrincipalRepositoryCreateCommitsHashAndSafeAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(30 * 24 * time.Hour)
	plaintext := "chmp_abcdefghijklmnop.this-plaintext-must-never-persist"
	hash := sha256.Sum256([]byte(plaintext))

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO machine_principals").
		WithArgs("inventory agent", uint64(7), now).
		WillReturnResult(sqlmock.NewResult(10, 1))
	mock.ExpectExec("INSERT INTO machine_principal_credentials").
		WithArgs(uint64(10), "abcdefghijklmnop", hash[:], `["inventory:read"]`, expiresAt, nil, uint64(7), now).
		WillReturnResult(sqlmock.NewResult(20, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(7), "machine_principal.created", "success", auditJSONWithout(plaintext)).
		WillReturnResult(sqlmock.NewResult(30, 1))
	mock.ExpectCommit()

	principal, credential, err := repo.Create(t.Context(), 7, "inventory agent", service.MachineCredentialInsert{
		LookupID: "abcdefghijklmnop", SecretHash: hash,
		Scopes: []model.MachineScope{model.MachineScopeInventoryRead}, ExpiresAt: expiresAt, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if principal.ID != 10 || credential.ID != 20 || credential.MachinePrincipalID != 10 {
		t.Fatalf("created principal/credential = %+v / %+v", principal, credential)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryListAggregatesSafeLifecycleMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	createdAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	lastUsedAt := createdAt.Add(time.Hour)
	revokedAt := createdAt.Add(2 * time.Hour)
	maxCredentialID := ^uint64(0)
	mock.ExpectQuery("SELECT p.id, p.name, p.created_by_user_id, p.created_at").WillReturnRows(sqlmock.NewRows([]string{
		"principal_id", "principal_name", "created_by_user_id", "principal_created_at", "credential_id", "credential_created_at", "expires_at", "last_used_at", "revoked_at",
	}).
		AddRow(10, "inventory agent", 7, createdAt, 20, createdAt, createdAt.Add(30*24*time.Hour), lastUsedAt, nil).
		AddRow(10, "inventory agent", 7, createdAt, 21, createdAt.Add(time.Minute), createdAt.Add(30*24*time.Hour), nil, revokedAt).
		AddRow(11, "max-id agent", 7, createdAt, "18446744073709551615", createdAt, createdAt.Add(30*24*time.Hour), nil, nil))

	items, err := NewMachinePrincipalRepository(db).List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 || len(items[0].Credentials) != 2 || items[0].Credentials[0].ID != 20 || items[0].Credentials[0].LastUsedAt == nil || items[0].Credentials[1].RevokedAt == nil || items[1].Credentials[0].ID != maxCredentialID {
		t.Fatalf("aggregated lifecycle list = %#v", items)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal lifecycle list: %v", err)
	}
	for _, forbidden := range []string{"secret", "hash", "lookup", "scope", "machinePrincipalId", "rotatedFromCredentialId"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Fatalf("list exposed %q: %s", forbidden, raw)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryCreateRollsBackWhenAuditFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	hash := sha256.Sum256([]byte("synthetic credential"))

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO machine_principals").WillReturnResult(sqlmock.NewResult(10, 1))
	mock.ExpectExec("INSERT INTO machine_principal_credentials").WillReturnResult(sqlmock.NewResult(20, 1))
	mock.ExpectExec("INSERT INTO audit_events").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	_, _, err = repo.Create(t.Context(), 7, "inventory agent", service.MachineCredentialInsert{
		LookupID: "abcdefghijklmnop", SecretHash: hash,
		Scopes: []model.MachineScope{model.MachineScopeInventoryRead}, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	})
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryRotateKeepsOldCredentialAndAudits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	hash := sha256.Sum256([]byte("rotated credential"))

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT p.id, p.name, p.created_by_user_id, p.created_at").
		WithArgs(uint64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_by_user_id", "created_at"}).AddRow(10, "inventory agent", 7, now.Add(-24*time.Hour)))
	mock.ExpectExec("INSERT INTO machine_principal_credentials").
		WithArgs(uint64(10), "ponmlkjihgfedcba", hash[:], `["inventory:read"]`, now.Add(30*24*time.Hour), uint64(20), uint64(7), now).
		WillReturnResult(sqlmock.NewResult(21, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(7), "machine_credential.rotated", "success", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(31, 1))
	mock.ExpectCommit()

	oldID := uint64(20)
	principal, credential, err := repo.Rotate(t.Context(), 7, oldID, service.MachineCredentialInsert{
		LookupID: "ponmlkjihgfedcba", SecretHash: hash,
		Scopes: []model.MachineScope{model.MachineScopeInventoryRead}, ExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt: now, RotatedFromCredentialID: &oldID,
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if principal.ID != 10 || credential.ID != 21 || credential.RotatedFromCredentialID == nil || *credential.RotatedFromCredentialID != 20 {
		t.Fatalf("rotation result = %+v / %+v", principal, credential)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryRevokeAndMarkUsedAreStateBounded(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE machine_principal_credentials SET revoked_at").
		WithArgs(now, uint64(20)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(7), "machine_credential.revoked", "success", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(32, 1))
	mock.ExpectCommit()
	if err := repo.Revoke(t.Context(), 7, 20, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	mock.ExpectExec("UPDATE machine_principal_credentials SET last_used_at").
		WithArgs(now, uint64(21), now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.MarkUsed(t.Context(), 21, now); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryMarkUsedAcceptsSameTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE machine_principal_credentials SET last_used_at").
		WithArgs(now, uint64(21), now).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(uint64(21), now).
		WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(true))

	if err := repo.MarkUsed(t.Context(), 21, now); err != nil {
		t.Fatalf("MarkUsed at unchanged timestamp: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMachinePrincipalRepositoryFindCredentialReturnsHashAndMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	hash := sha256.Sum256([]byte("credential"))
	mock.ExpectQuery("SELECT p.id, p.name, p.created_by_user_id, p.created_at").
		WithArgs("abcdefghijklmnop").
		WillReturnRows(sqlmock.NewRows([]string{
			"principal_id", "principal_name", "created_by_user_id", "principal_created_at",
			"credential_id", "machine_principal_id", "lookup_id", "scopes", "expires_at",
			"last_used_at", "revoked_at", "rotated_from_credential_id", "credential_created_at", "secret_hash",
		}).AddRow(10, "inventory agent", 7, now.Add(-time.Hour), 20, 10, "abcdefghijklmnop", `["inventory:read"]`, now.Add(time.Hour), nil, nil, nil, now, hash[:]))

	auth, err := repo.FindCredential(t.Context(), "abcdefghijklmnop")
	if err != nil {
		t.Fatalf("FindCredential: %v", err)
	}
	if auth.Principal.ID != 10 || auth.Credential.ID != 20 || auth.SecretHash != hash || len(auth.Credential.Scopes) != 1 {
		t.Fatalf("authentication record = %+v", auth)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

type auditJSONWithout string

func (forbidden auditJSONWithout) Match(value driver.Value) bool {
	var raw []byte
	switch value := value.(type) {
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		return false
	}
	return json.Valid(raw) && !strings.Contains(string(raw), string(forbidden))
}
