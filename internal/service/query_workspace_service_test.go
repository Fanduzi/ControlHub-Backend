// Package service provides tests for query-workspace business logic.
// input: context, errors, testing, internal/model
// output: actor-owned missing reads, validated OCC writes, and conflict propagation tests
// pos: Service-seam regression coverage for the query workspace aggregate
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type fakeQueryWorkspaceRepository struct {
	workspace  model.QueryWorkspace
	getOwner   uint64
	putOwner   uint64
	putRequest model.QueryWorkspacePutRequest
	putVersion uint64
	putErr     error
}

func (f *fakeQueryWorkspaceRepository) Get(_ context.Context, ownerUserID uint64) (model.QueryWorkspace, error) {
	f.getOwner = ownerUserID
	return f.workspace, nil
}

func (f *fakeQueryWorkspaceRepository) Put(_ context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (uint64, error) {
	f.putOwner = ownerUserID
	f.putRequest = req
	return f.putVersion, f.putErr
}

func TestQueryWorkspaceServiceGetsOnlyAuthenticatedOwnerWorkspace(t *testing.T) {
	repo := &fakeQueryWorkspaceRepository{workspace: model.QueryWorkspace{OwnerUserID: 41, Version: 0, Worksheets: []model.QueryWorkspaceWorksheet{}}}

	workspace, err := NewQueryWorkspaceService(repo).Get(context.Background(), 41)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if repo.getOwner != 41 || workspace.OwnerUserID != 41 || workspace.Version != 0 || workspace.Worksheets == nil {
		t.Fatalf("Get() = %+v, repository owner = %d", workspace, repo.getOwner)
	}
}

func TestQueryWorkspaceServiceValidatesThenReturnsPersistedAggregate(t *testing.T) {
	req := model.QueryWorkspacePutRequest{ExpectedVersion: 3, Worksheets: []model.QueryWorkspaceWorksheet{{
		ID: "worksheet-1", Name: "Orders", TargetResourceID: 9, Statement: "not sql",
	}}}
	want := model.QueryWorkspace{OwnerUserID: 7, Version: 4, Worksheets: req.Worksheets}
	repo := &fakeQueryWorkspaceRepository{workspace: want, putVersion: 4}

	got, err := NewQueryWorkspaceService(repo).Put(context.Background(), 7, req)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if repo.putOwner != 7 || repo.putRequest.ExpectedVersion != 3 || got.Version != 4 || got.Worksheets[0].Statement != "not sql" {
		t.Fatalf("Put() = %+v, repository owner/request = %d/%+v", got, repo.putOwner, repo.putRequest)
	}
}

func TestQueryWorkspaceServiceRejectsInvalidRequestBeforeRepository(t *testing.T) {
	repo := &fakeQueryWorkspaceRepository{}
	_, err := NewQueryWorkspaceService(repo).Put(context.Background(), 7, model.QueryWorkspacePutRequest{
		Worksheets: []model.QueryWorkspaceWorksheet{{Name: "missing id and target"}},
	})
	if err == nil {
		t.Fatal("Put() error = nil, want validation error")
	}
	if repo.putOwner != 0 {
		t.Fatalf("repository Put owner = %d, want no call", repo.putOwner)
	}
}

func TestQueryWorkspaceServicePreservesOCCConflict(t *testing.T) {
	repo := &fakeQueryWorkspaceRepository{putErr: ErrQueryWorkspaceConflict}
	req := model.QueryWorkspacePutRequest{Worksheets: []model.QueryWorkspaceWorksheet{{
		ID: "worksheet-1", Name: "Orders", TargetResourceID: 9,
	}}}

	_, err := NewQueryWorkspaceService(repo).Put(context.Background(), 7, req)
	if !errors.Is(err, ErrQueryWorkspaceConflict) {
		t.Fatalf("Put() error = %v, want ErrQueryWorkspaceConflict", err)
	}
}
