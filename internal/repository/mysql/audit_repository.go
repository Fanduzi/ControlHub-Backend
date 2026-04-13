// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewAuditRepository, AuditRepository struct
// pos: MySQL data access for audit_events table
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository struct {
	db *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) ListAll() ([]model.AuditEvent, error) {
	query := `
	select id, actor_user_id, coalesce(target_resource_id, ''), event_type, result, created_at
	from audit_events
	order by created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAuditEvents(rows)
}

func (r *AuditRepository) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	query := `
	select id, actor_user_id, coalesce(target_resource_id, ''), event_type, result, created_at
	from audit_events
	where target_resource_id = ?
	order by created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAuditEvents(rows)
}

func scanAuditEvents(rows *sql.Rows) ([]model.AuditEvent, error) {
	items := make([]model.AuditEvent, 0)
	for rows.Next() {
		var item model.AuditEvent
		if err := rows.Scan(
			&item.ID,
			&item.ActorUserID,
			&item.TargetResourceID,
			&item.EventType,
			&item.Result,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
