// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewAuthAuditEmitter, AuthAuditEmitter
// pos: MySQL data access for auth audit event persistence with fail-open semantics
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"log"
)

// AuthAuditEmitter writes authentication and authorization audit events to MySQL.
// Every method is fail-open: persistence errors are logged at a fixed category
// and never propagated to callers so the security decision is unchanged.
type AuthAuditEmitter struct {
	db *sql.DB
}

// NewAuthAuditEmitter returns an emitter backed by the given database.
func NewAuthAuditEmitter(db *sql.DB) *AuthAuditEmitter {
	return &AuthAuditEmitter{db: db}
}

// EmitAuthAudit inserts one auth audit row. actorUserID and targetResourceID
// may be nil. Fail-open: errors are logged, never returned.
func (e *AuthAuditEmitter) EmitAuthAudit(eventType, result string, actorUserID *uint64, targetResourceID *uint64) error {
	_, err := e.db.ExecContext(context.Background(),
		`insert into audit_events (actor_user_id, target_resource_id, event_type, result)
		 values (?, ?, ?, ?)`,
		actorUserID, targetResourceID, eventType, result,
	)
	if err != nil {
		// Fixed-category operational signal: no identity, no request values, no secrets.
		log.Printf("auth_audit_emit_fail event=%s result=%s err=%v", eventType, result, err)
	}
	return nil // fail-open: never return error
}
