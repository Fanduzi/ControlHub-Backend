package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/model"
)

type RelationRepository struct {
	db *pgxpool.Pool
}

func NewRelationRepository(db *pgxpool.Pool) *RelationRepository {
	return &RelationRepository{db: db}
}

func (r *RelationRepository) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	query := `
select id::text, from_resource_id::text, to_resource_id::text, relation_type, created_at
from resource_relations
where from_resource_id::text = $1 or to_resource_id::text = $1
order by created_at desc`

	rows, err := r.db.Query(context.Background(), query, resourceID)
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
