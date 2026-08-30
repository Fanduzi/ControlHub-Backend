// Package service provides business logic for actor-owned query workspaces.
// input: context, errors, internal/model
// output: QueryWorkspaceService Get/validated OCC Put and ErrQueryWorkspaceConflict
// pos: Thin owner-only service boundary over the one-row query workspace aggregate
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"

	"github.com/fan/controlhub/internal/model"
)

var ErrQueryWorkspaceConflict = errors.New("query workspace version conflict")

type QueryWorkspaceRepository interface {
	Get(ctx context.Context, ownerUserID uint64) (model.QueryWorkspace, error)
	Put(ctx context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (uint64, error)
}

type QueryWorkspaceService struct{ repo QueryWorkspaceRepository }

func NewQueryWorkspaceService(repo QueryWorkspaceRepository) *QueryWorkspaceService {
	return &QueryWorkspaceService{repo: repo}
}

func (s *QueryWorkspaceService) Get(ctx context.Context, ownerUserID uint64) (model.QueryWorkspace, error) {
	return s.repo.Get(ctx, ownerUserID)
}

func (s *QueryWorkspaceService) Put(ctx context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (model.QueryWorkspace, error) {
	if err := req.Validate(); err != nil {
		return model.QueryWorkspace{}, err
	}
	if _, err := s.repo.Put(ctx, ownerUserID, req); err != nil {
		return model.QueryWorkspace{}, err
	}
	return s.repo.Get(ctx, ownerUserID)
}
