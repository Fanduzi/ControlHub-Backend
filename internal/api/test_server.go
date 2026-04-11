package api

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type TestServer struct {
	Router *chi.Mux
}

type fakeResourceRepo struct{}

func (fakeResourceRepo) ListResources(resourceType string, _ string) ([]model.Resource, error) {
	items := []model.Resource{
		{
			ID:              "res-1",
			ResourceType:    model.ResourceTypeDatabaseInstance,
			ResourceSubtype: "mysql",
			Name:            "order-mysql-prod",
			DisplayName:     "Order MySQL Prod",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-dba",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Source:          "manual",
			Labels:          map[string]string{"team": "order"},
			CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
	}

	if resourceType == "" || resourceType == string(model.ResourceTypeDatabaseInstance) {
		return items, nil
	}

	return []model.Resource{}, nil
}

func (fakeResourceRepo) GetResource(id string) (*model.Resource, error) {
	return &model.Resource{
		ID:              id,
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-prod",
		DisplayName:     "Order MySQL Prod",
		EnvironmentID:   "env-prod",
		OwnerID:         "owner-dba",
		LifecycleStatus: "running",
		HealthStatus:    "healthy",
		Source:          "manual",
		Labels:          map[string]string{"team": "order"},
		CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
	}, nil
}

type fakeRelationRepo struct{}

func (fakeRelationRepo) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	return []model.ResourceRelation{
		{
			ID:             "rel-1",
			FromResourceID: "svc-1",
			ToResourceID:   resourceID,
			RelationType:   "depends_on",
			CreatedAt:      time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
		},
	}, nil
}

type fakeAuditRepo struct{}

func (fakeAuditRepo) ListAll() ([]model.AuditEvent, error) {
	return []model.AuditEvent{
		{
			ID:               "audit-1",
			ActorUserID:      "user-1",
			TargetResourceID: "res-1",
			EventType:        "resource.updated",
			Result:           "success",
			CreatedAt:        time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (fakeAuditRepo) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	return []model.AuditEvent{
		{
			ID:               "audit-1",
			ActorUserID:      "user-1",
			TargetResourceID: resourceID,
			EventType:        "resource.updated",
			Result:           "success",
			CreatedAt:        time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
		},
	}, nil
}

func NewTestServer() *TestServer {
	deps := Dependencies{
		ResourceService:    service.NewResourceService(fakeResourceRepo{}),
		RelationService:    service.NewRelationService(fakeRelationRepo{}),
		AuditService:       service.NewAuditService(fakeAuditRepo{}),
		AuthService:        service.NewAuthService(fakeUserCredentialRepo{}, "test-secret"),
		EnvironmentService: service.NewEnvironmentService(fakeEnvironmentRepo{}),
		OwnerService:       service.NewOwnerService(fakeOwnerRepo{}),
		RoleService:        service.NewRoleService(fakeRoleRepo{}),
	}

	return &TestServer{Router: NewRouter(deps)}
}

type fakeUserCredentialRepo struct{}

func (fakeUserCredentialRepo) FindByEmail(email string) (*model.UserCredential, error) {
	if email != "admin@example.com" {
		return nil, nil
	}

	return &model.UserCredential{
		ID:           "user-1",
		Email:        "admin@example.com",
		RoleName:     "admin",
		PasswordHash: "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4",
	}, nil
}

type fakeEnvironmentRepo struct{}

func (fakeEnvironmentRepo) ListEnvironments() ([]model.Environment, error) {
	return []model.Environment{
		{
			ID:          "10000000-0000-0000-0000-000000000001",
			Name:        "Production",
			Slug:        "prod",
			Description: "Production environment",
			CreatedAt:   time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		{
			ID:          "10000000-0000-0000-0000-000000000002",
			Name:        "Staging",
			Slug:        "staging",
			Description: "Staging environment",
			CreatedAt:   time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
	}, nil
}

type fakeOwnerRepo struct{}

func (fakeOwnerRepo) ListOwners() ([]model.Owner, error) {
	return []model.Owner{
		{
			ID:        "20000000-0000-0000-0000-000000000001",
			Name:      "Platform Team",
			Email:     "platform@example.com",
			CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		{
			ID:        "20000000-0000-0000-0000-000000000002",
			Name:      "DBA Team",
			Email:     "dba@example.com",
			CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
	}, nil
}

type fakeRoleRepo struct{}

func (fakeRoleRepo) ListRoles() ([]model.Role, error) {
	return []model.Role{
		{
			ID:          "00000000-0000-0000-0000-000000000001",
			Name:        "admin",
			Description: "Full platform access",
			CreatedAt:   time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		{
			ID:          "00000000-0000-0000-0000-000000000002",
			Name:        "editor",
			Description: "Can manage assets and relations",
			CreatedAt:   time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
	}, nil
}
