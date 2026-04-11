package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository struct {
	db *pgxpool.Pool
}

func NewAuditRepository(db *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) ListAll() ([]model.AuditEvent, error) {
	query := `
select id::text, actor_user_id::text, coalesce(target_resource_id::text, ''), event_type, result, created_at
from audit_events
order by created_at desc`

	rows, err := r.db.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAuditEvents(rows)
}

func (r *AuditRepository) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	query := `
select id::text, actor_user_id::text, coalesce(target_resource_id::text, ''), event_type, result, created_at
from audit_events
where target_resource_id::text = $1
order by created_at desc`

	rows, err := r.db.Query(context.Background(), query, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAuditEvents(rows)
}

func scanAuditEvents(rows pgxRows) ([]model.AuditEvent, error) {
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

type pgxRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}
