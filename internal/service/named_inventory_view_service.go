// Package service manages named inventory views with authorization.
// input: context, database/sql, errors, fmt, strings, internal/model
// output: NamedInventoryViewService, repository interface, controlled errors
// pos: Personal-owner and shared-admin authorization boundary for inventory views
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrNamedInventoryViewNotFound   = errors.New("named inventory view not found")
	ErrNamedInventoryViewForbidden  = errors.New("forbidden")
	ErrNamedInventoryViewValidation = errors.New("named inventory view validation failed")
)

type NamedInventoryViewRepository interface {
	ListVisible(context.Context, uint64) ([]model.NamedInventoryView, error)
	ListShared(context.Context) ([]model.NamedInventoryView, error)
	Get(context.Context, uint64) (model.NamedInventoryView, error)
	Create(context.Context, uint64, model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error)
	Update(context.Context, uint64, model.NamedInventoryViewUpdateRequest) error
	Delete(context.Context, uint64) error
}

type NamedInventoryViewService struct{ repo NamedInventoryViewRepository }

func NewNamedInventoryViewService(repo NamedInventoryViewRepository) *NamedInventoryViewService {
	return &NamedInventoryViewService{repo: repo}
}

func (s *NamedInventoryViewService) List(ctx context.Context, actor AuthenticatedUser) ([]model.NamedInventoryView, error) {
	return s.repo.ListVisible(ctx, actor.ID)
}

func (s *NamedInventoryViewService) ListShared(ctx context.Context) ([]model.NamedInventoryView, error) {
	return s.repo.ListShared(ctx)
}

func (s *NamedInventoryViewService) Create(ctx context.Context, actor AuthenticatedUser, req model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error) {
	if err := req.Validate(); err != nil {
		return model.NamedInventoryView{}, fmt.Errorf("%w: %v", ErrNamedInventoryViewValidation, err)
	}
	if req.Scope == model.NamedInventoryViewShared && actor.Role != "admin" {
		return model.NamedInventoryView{}, ErrNamedInventoryViewForbidden
	}
	req.Name = strings.TrimSpace(req.Name)
	return s.repo.Create(ctx, actor.ID, req)
}

func (s *NamedInventoryViewService) Update(ctx context.Context, actor AuthenticatedUser, id uint64, req model.NamedInventoryViewUpdateRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrNamedInventoryViewValidation, err)
	}
	view, err := s.repo.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNamedInventoryViewNotFound
	}
	if err != nil {
		return err
	}
	if view.Scope == model.NamedInventoryViewPersonal && view.OwnerUserID != actor.ID {
		return ErrNamedInventoryViewNotFound
	}
	if view.Scope == model.NamedInventoryViewShared && actor.Role != "admin" {
		return ErrNamedInventoryViewForbidden
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := s.repo.Update(ctx, id, req); errors.Is(err, sql.ErrNoRows) {
		return ErrNamedInventoryViewNotFound
	} else {
		return err
	}
}

func (s *NamedInventoryViewService) Delete(ctx context.Context, actor AuthenticatedUser, id uint64) error {
	view, err := s.repo.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNamedInventoryViewNotFound
	}
	if err != nil {
		return err
	}
	if view.Scope == model.NamedInventoryViewPersonal && view.OwnerUserID != actor.ID {
		return ErrNamedInventoryViewNotFound
	}
	if view.Scope == model.NamedInventoryViewShared && actor.Role != "admin" {
		return ErrNamedInventoryViewForbidden
	}
	if err := s.repo.Delete(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return ErrNamedInventoryViewNotFound
	} else {
		return err
	}
}
