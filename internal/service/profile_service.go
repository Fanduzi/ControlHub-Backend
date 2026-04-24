// Package service provides business logic for typed profile writes.
// input: internal/model (ResourceType, Resource), ProfileRepository interface
// output: NewProfileService, ProfileService.PutProfile/PatchProfile, ProfileRepository interface
// pos: Business logic for resource profile upserts with archived-resource guard
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// ProfileRepository defines the data access interface for typed profile upserts and deletes.
// The concrete MySQL implementation lives in internal/repository/mysql/resource_repository.go.
type ProfileRepository interface {
	UpsertHostProfile(ctx context.Context, resourceID uint64, hostname, ipAddress, osName string) error
	UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error
	UpsertDatabaseClusterProfile(ctx context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error
	UpsertServiceProfile(ctx context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error
	DeleteProfile(ctx context.Context, resourceID uint64, resourceType string) error
}

// ProfileService handles profile write operations for resources.
type ProfileService struct {
	profileRepo  ProfileRepository
	resourceRepo ResourceRepository
}

// NewProfileService creates a ProfileService with separate profile and resource repositories.
func NewProfileService(profileRepo ProfileRepository, resourceRepo ResourceRepository) *ProfileService {
	return &ProfileService{profileRepo: profileRepo, resourceRepo: resourceRepo}
}

// PutProfile replaces (upserts) the full profile for a resource.
// Returns ErrResourceNotFound if the resource does not exist,
// or ErrResourceArchived if the resource is archived.
func (s *ProfileService) PutProfile(ctx context.Context, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	return s.writeProfile(ctx, resourceID, res.ResourceType, fields)
}

// PatchProfile partially updates the profile for a resource.
// Currently behaves identically to PutProfile since all profile fields are
// replaced via ON DUPLICATE KEY UPDATE semantics.
func (s *ProfileService) PatchProfile(ctx context.Context, resourceID uint64, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	return s.writeProfile(ctx, resourceID, res.ResourceType, fields)
}

// DeleteProfile removes the profile row for a resource.
// Returns ErrResourceNotFound if the resource does not exist,
// or ErrResourceArchived if the resource is archived.
func (s *ProfileService) DeleteProfile(ctx context.Context, resourceID uint64) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return ErrResourceArchived
	}
	return s.profileRepo.DeleteProfile(ctx, resourceID, string(res.ResourceType))
}

func (s *ProfileService) writeProfile(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]interface{}) error {
	switch resourceType {
	case model.ResourceTypeHost:
		return s.profileRepo.UpsertHostProfile(ctx, resourceID,
			getStringField(fields, "hostname"),
			getStringField(fields, "ipAddress"),
			getStringField(fields, "osName"),
		)
	case model.ResourceTypeDatabaseInstance:
		return s.profileRepo.UpsertDatabaseInstanceProfile(ctx, resourceID,
			getStringField(fields, "engine"),
			getStringField(fields, "version"),
			getStringField(fields, "host"),
			getIntField(fields, "port"),
			getStringField(fields, "role"),
		)
	case model.ResourceTypeDatabaseCluster:
		return s.profileRepo.UpsertDatabaseClusterProfile(ctx, resourceID,
			getStringField(fields, "engine"),
			getStringField(fields, "topologyMode"),
			getStringField(fields, "primaryEndpoint"),
		)
	case model.ResourceTypeService:
		return s.profileRepo.UpsertServiceProfile(ctx, resourceID,
			getStringField(fields, "systemName"),
			getStringField(fields, "repositoryUrl"),
			getStringField(fields, "runtimeEnv"),
		)
	default:
		return fmt.Errorf("resource type %s has no profile table", resourceType)
	}
}

func getStringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func getIntField(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
