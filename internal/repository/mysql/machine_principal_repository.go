// Package mysql provides MySQL-backed repository implementations.
// input: context, crypto/sha256, database/sql, encoding/json, fmt, internal/model, internal/service
// output: atomic hash-only machine-principal credential persistence with administrator audit
// pos: MySQL repository for the independent machine-principal lifecycle
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type MachinePrincipalRepository struct{ db *sql.DB }

func NewMachinePrincipalRepository(db *sql.DB) *MachinePrincipalRepository {
	return &MachinePrincipalRepository{db: db}
}

func (r *MachinePrincipalRepository) Create(ctx context.Context, actorID uint64, name string, credential service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("begin machine principal create: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO machine_principals (name, created_by_user_id, created_at) VALUES (?, ?, ?)`, name, actorID, credential.CreatedAt)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("insert machine principal: %w", err)
	}
	principalID, err := result.LastInsertId()
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("machine principal last insert id: %w", err)
	}
	stored, err := insertMachineCredential(ctx, tx, uint64(principalID), actorID, credential)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, err
	}
	if err := insertMachinePrincipalAudit(ctx, tx, actorID, "machine_principal.created", []model.AuditChange{
		{Field: "machine_principal_id", Operation: model.AuditChangeAdd, After: uint64(principalID)},
		{Field: "credential_id", Operation: model.AuditChangeAdd, After: stored.ID},
		{Field: "scopes", Operation: model.AuditChangeAdd, After: stored.Scopes},
		{Field: "expires_at", Operation: model.AuditChangeAdd, After: stored.ExpiresAt},
	}); err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("commit machine principal create: %w", err)
	}
	return model.MachinePrincipal{ID: uint64(principalID), Name: name, CreatedByUserID: actorID, CreatedAt: credential.CreatedAt}, stored, nil
}

func (r *MachinePrincipalRepository) Rotate(ctx context.Context, actorID, oldCredentialID uint64, credential service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("begin machine credential rotation: %w", err)
	}
	defer tx.Rollback()

	var principal model.MachinePrincipal
	err = tx.QueryRowContext(ctx, `SELECT p.id, p.name, p.created_by_user_id, p.created_at
		FROM machine_principal_credentials c
		JOIN machine_principals p ON p.id = c.machine_principal_id
		WHERE c.id = ? FOR UPDATE`, oldCredentialID).
		Scan(&principal.ID, &principal.Name, &principal.CreatedByUserID, &principal.CreatedAt)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("find machine credential for rotation: %w", err)
	}
	stored, err := insertMachineCredential(ctx, tx, principal.ID, actorID, credential)
	if err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, err
	}
	if err := insertMachinePrincipalAudit(ctx, tx, actorID, "machine_credential.rotated", []model.AuditChange{
		{Field: "machine_principal_id", Operation: model.AuditChangeUpdate, Before: principal.ID, After: principal.ID},
		{Field: "credential_id", Operation: model.AuditChangeAdd, After: stored.ID},
		{Field: "rotated_from_credential_id", Operation: model.AuditChangeAdd, After: oldCredentialID},
		{Field: "scopes", Operation: model.AuditChangeAdd, After: stored.Scopes},
		{Field: "expires_at", Operation: model.AuditChangeAdd, After: stored.ExpiresAt},
	}); err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, fmt.Errorf("commit machine credential rotation: %w", err)
	}
	return principal, stored, nil
}

func (r *MachinePrincipalRepository) Revoke(ctx context.Context, actorID, credentialID uint64, revokedAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin machine credential revoke: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE machine_principal_credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, revokedAt, credentialID)
	if err := requireMachineCredentialRow(result, err); err != nil {
		return err
	}
	if err := insertMachinePrincipalAudit(ctx, tx, actorID, "machine_credential.revoked", []model.AuditChange{
		{Field: "credential_id", Operation: model.AuditChangeUpdate, Before: credentialID, After: credentialID},
		{Field: "revoked_at", Operation: model.AuditChangeAdd, After: revokedAt},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit machine credential revoke: %w", err)
	}
	return nil
}

func (r *MachinePrincipalRepository) FindCredential(ctx context.Context, lookupID string) (service.MachineCredentialAuthentication, error) {
	var auth service.MachineCredentialAuthentication
	var scopes, hash []byte
	err := r.db.QueryRowContext(ctx, `SELECT p.id, p.name, p.created_by_user_id, p.created_at,
		c.id, c.machine_principal_id, c.lookup_id, c.scopes, c.expires_at,
		c.last_used_at, c.revoked_at, c.rotated_from_credential_id, c.created_at, c.secret_hash
		FROM machine_principal_credentials c
		JOIN machine_principals p ON p.id = c.machine_principal_id
		WHERE c.lookup_id = ?`, lookupID).Scan(
		&auth.Principal.ID, &auth.Principal.Name, &auth.Principal.CreatedByUserID, &auth.Principal.CreatedAt,
		&auth.Credential.ID, &auth.Credential.MachinePrincipalID, &auth.Credential.LookupID, &scopes, &auth.Credential.ExpiresAt,
		&auth.Credential.LastUsedAt, &auth.Credential.RevokedAt, &auth.Credential.RotatedFromCredentialID, &auth.Credential.CreatedAt, &hash,
	)
	if err != nil {
		return service.MachineCredentialAuthentication{}, err
	}
	if len(hash) != sha256.Size {
		return service.MachineCredentialAuthentication{}, fmt.Errorf("invalid stored machine credential hash")
	}
	copy(auth.SecretHash[:], hash)
	if err := json.Unmarshal(scopes, &auth.Credential.Scopes); err != nil {
		return service.MachineCredentialAuthentication{}, fmt.Errorf("decode machine credential scopes: %w", err)
	}
	auth.Credential.Scopes, err = model.NormalizeMachineScopes(auth.Credential.Scopes)
	if err != nil {
		return service.MachineCredentialAuthentication{}, fmt.Errorf("validate stored machine credential scopes: %w", err)
	}
	return auth, nil
}

func (r *MachinePrincipalRepository) MarkUsed(ctx context.Context, credentialID uint64, usedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE machine_principal_credentials SET last_used_at = ?
		WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`, usedAt, credentialID, usedAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 0 {
		return err
	}
	var active bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM machine_principal_credentials
		WHERE id = ? AND revoked_at IS NULL AND expires_at > ?)`, credentialID, usedAt).Scan(&active); err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return nil
}

func insertMachineCredential(ctx context.Context, tx *sql.Tx, principalID, actorID uint64, credential service.MachineCredentialInsert) (model.MachineCredential, error) {
	scopes, err := json.Marshal(credential.Scopes)
	if err != nil {
		return model.MachineCredential{}, fmt.Errorf("marshal machine credential scopes: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO machine_principal_credentials
		(machine_principal_id, lookup_id, secret_hash, scopes, expires_at, rotated_from_credential_id, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, principalID, credential.LookupID, credential.SecretHash[:], string(scopes), credential.ExpiresAt, credential.RotatedFromCredentialID, actorID, credential.CreatedAt)
	if err != nil {
		return model.MachineCredential{}, fmt.Errorf("insert machine credential: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.MachineCredential{}, fmt.Errorf("machine credential last insert id: %w", err)
	}
	return model.MachineCredential{
		ID: uint64(id), MachinePrincipalID: principalID, LookupID: credential.LookupID,
		Scopes: append([]model.MachineScope(nil), credential.Scopes...), ExpiresAt: credential.ExpiresAt,
		RotatedFromCredentialID: credential.RotatedFromCredentialID, CreatedAt: credential.CreatedAt,
	}, nil
}

func insertMachinePrincipalAudit(ctx context.Context, tx *sql.Tx, actorID uint64, eventType string, changes []model.AuditChange) error {
	raw, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("marshal machine principal audit changes: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (actor_user_id, target_resource_id, event_type, result, changes)
		VALUES (?, NULL, ?, ?, ?)`, actorID, eventType, "success", string(raw))
	if err != nil {
		return fmt.Errorf("insert machine principal audit: %w", err)
	}
	return nil
}

func requireMachineCredentialRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

var _ service.MachinePrincipalRepository = (*MachinePrincipalRepository)(nil)
