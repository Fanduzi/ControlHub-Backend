// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewRelationRepository, RelationRepository struct
// pos: MySQL data access for resource_relations table
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"

	"github.com/fan/controlhub/internal/model"
)

type RelationRepository struct {
	db *sql.DB
}

func NewRelationRepository(db *sql.DB) *RelationRepository {
	return &RelationRepository{db: db}
}

func (r *RelationRepository) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	query := `
	select id, from_resource_id, to_resource_id, relation_type, created_at
	from resource_relations
	where from_resource_id = ? or to_resource_id = ?
	order by created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query, resourceID, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ResourceRelation, 0)
	for rows.Next() {
		var item model.ResourceRelation
		if err := rows.Scan(
			&item.ID,
			&item.FromResourceID,
			&item.ToResourceID,
			&item.RelationType,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
