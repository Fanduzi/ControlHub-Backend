// Package mysql provides MySQL-backed repository implementations.
// input: context, database/sql, encoding/json, errors, fmt, go-sql-driver/mysql, internal/model, internal/service
// output: QueryWorkspaceRepository Get/OCC Put and service-parity ErrQueryWorkspaceConflict
// pos: One-row-per-owner JSON aggregate persistence for query worksheets
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

var ErrQueryWorkspaceConflict = service.ErrQueryWorkspaceConflict

type QueryWorkspaceRepository struct{ db *sql.DB }

func NewQueryWorkspaceRepository(db *sql.DB) *QueryWorkspaceRepository {
	return &QueryWorkspaceRepository{db: db}
}

func (r *QueryWorkspaceRepository) Get(ctx context.Context, ownerUserID uint64) (model.QueryWorkspace, error) {
	workspace := model.QueryWorkspace{OwnerUserID: ownerUserID, Worksheets: []model.QueryWorkspaceWorksheet{}}
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT worksheets, version, updated_at FROM query_workspaces WHERE owner_user_id = ?`, ownerUserID).
		Scan(&raw, &workspace.Version, &workspace.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace, nil
	}
	if err != nil {
		return model.QueryWorkspace{}, fmt.Errorf("get query workspace: %w", err)
	}
	if err := json.Unmarshal(raw, &workspace.Worksheets); err != nil {
		return model.QueryWorkspace{}, fmt.Errorf("decode query workspace: %w", err)
	}
	if workspace.Worksheets == nil {
		workspace.Worksheets = []model.QueryWorkspaceWorksheet{}
	}
	return workspace, nil
}

func (r *QueryWorkspaceRepository) Put(ctx context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (uint64, error) {
	if err := req.Validate(); err != nil {
		return 0, err
	}
	worksheets := req.Worksheets
	if worksheets == nil {
		worksheets = []model.QueryWorkspaceWorksheet{}
	}
	raw, err := json.Marshal(worksheets)
	if err != nil {
		return 0, fmt.Errorf("encode query workspace: %w", err)
	}
	if req.ExpectedVersion == 0 {
		_, err := r.db.ExecContext(ctx, `INSERT INTO query_workspaces (owner_user_id, worksheets, version) VALUES (?, ?, 1)`, ownerUserID, raw)
		if err != nil {
			var mysqlErr *gomysql.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
				return 0, ErrQueryWorkspaceConflict
			}
			return 0, fmt.Errorf("insert query workspace: %w", err)
		}
		return 1, nil
	}

	result, err := r.db.ExecContext(ctx, `UPDATE query_workspaces SET worksheets = ?, version = version + 1 WHERE owner_user_id = ? AND version = ?`, raw, ownerUserID, req.ExpectedVersion)
	if err != nil {
		return 0, fmt.Errorf("update query workspace: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("update query workspace rows affected: %w", err)
	}
	if rows == 0 {
		return 0, ErrQueryWorkspaceConflict
	}
	return req.ExpectedVersion + 1, nil
}
