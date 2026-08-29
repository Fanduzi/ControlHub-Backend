// Package mysql provides MySQL-backed repository implementations.
// input: context, database/sql, encoding/json, internal/model
// output: NamedInventoryViewRepository CRUD and shared-only read seam
// pos: Persistence for personal and shared named inventory views
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fan/controlhub/internal/model"
)

type NamedInventoryViewRepository struct{ db *sql.DB }

func NewNamedInventoryViewRepository(db *sql.DB) *NamedInventoryViewRepository {
	return &NamedInventoryViewRepository{db: db}
}

func (r *NamedInventoryViewRepository) ListVisible(ctx context.Context, ownerUserID uint64) ([]model.NamedInventoryView, error) {
	return r.list(ctx, "scope = 'shared' OR (scope = 'personal' AND owner_user_id = ?)", ownerUserID)
}

// ListShared is intentionally actor-free so a future machine principal can
// reuse the shared-only read boundary without gaining personal-view access.
func (r *NamedInventoryViewRepository) ListShared(ctx context.Context) ([]model.NamedInventoryView, error) {
	return r.list(ctx, "scope = 'shared'")
}

func (r *NamedInventoryViewRepository) list(ctx context.Context, where string, args ...any) ([]model.NamedInventoryView, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, owner_user_id, name, scope, state, created_at, updated_at
FROM named_inventory_views WHERE `+where+` ORDER BY scope = 'personal' DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := []model.NamedInventoryView{}
	for rows.Next() {
		view, err := scanNamedInventoryView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func (r *NamedInventoryViewRepository) Get(ctx context.Context, id uint64) (model.NamedInventoryView, error) {
	return scanNamedInventoryView(r.db.QueryRowContext(ctx, `SELECT id, owner_user_id, name, scope, state, created_at, updated_at FROM named_inventory_views WHERE id = ?`, id))
}

func (r *NamedInventoryViewRepository) Create(ctx context.Context, ownerUserID uint64, req model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error) {
	state, err := json.Marshal(req.State)
	if err != nil {
		return model.NamedInventoryView{}, err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO named_inventory_views (owner_user_id, name, scope, state) VALUES (?, ?, ?, ?)`, ownerUserID, req.Name, req.Scope, state)
	if err != nil {
		return model.NamedInventoryView{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.NamedInventoryView{}, err
	}
	return r.Get(ctx, uint64(id))
}

func (r *NamedInventoryViewRepository) Update(ctx context.Context, id uint64, req model.NamedInventoryViewUpdateRequest) error {
	state, err := json.Marshal(req.State)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE named_inventory_views SET name = ?, state = ? WHERE id = ?`, req.Name, state, id)
	return requireNamedInventoryViewRow(result, err)
}

func (r *NamedInventoryViewRepository) Delete(ctx context.Context, id uint64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM named_inventory_views WHERE id = ?`, id)
	return requireNamedInventoryViewRow(result, err)
}

type namedInventoryViewScanner interface{ Scan(...any) error }

func scanNamedInventoryView(row namedInventoryViewScanner) (model.NamedInventoryView, error) {
	var view model.NamedInventoryView
	var state []byte
	err := row.Scan(&view.ID, &view.OwnerUserID, &view.Name, &view.Scope, &state, &view.CreatedAt, &view.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(state, &view.State)
	}
	return view, err
}

func requireNamedInventoryViewRow(result sql.Result, err error) error {
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
