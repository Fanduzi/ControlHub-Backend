// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: chi/v5, internal/model, internal/service
// output: TestServer struct, NewTestServer
// pos: Test infrastructure — fake repos and pre-wired router for handler tests
// note: if this file changes, update header and README.md
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

type fakeResourceRepo struct {
	profiles map[string]*model.ResourceProfileResponse
}

func (f fakeResourceRepo) GetResourceProfile(id string) (*model.ResourceProfileResponse, error) {
	res, err := f.GetResource(id)
	if err != nil {
		return nil, err
	}
	if p, ok := f.profiles[id]; ok {
		return p, nil
	}
	return &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    res.ResourceType,
		ResourceSubtype: res.ResourceSubtype,
		Profile:         map[string]any{},
	}, nil
}

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
	resources := map[string]model.Resource{
		"res-db-instance": {
			ID:              "res-db-instance",
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
		"res-db-cluster": {
			ID:              "res-db-cluster",
			ResourceType:    model.ResourceTypeDatabaseCluster,
			ResourceSubtype: "mysql",
			Name:            "order-mysql-cluster-prod",
			DisplayName:     "Order MySQL Cluster Prod",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-dba",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Source:          "manual",
			Labels:          map[string]string{"team": "order"},
			CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		"res-service": {
			ID:              "res-service",
			ResourceType:    model.ResourceTypeService,
			ResourceSubtype: "api",
			Name:            "order-api-prod",
			DisplayName:     "Order API Prod",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-platform",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Source:          "manual",
			Labels:          map[string]string{"team": "order"},
			CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		"res-host": {
			ID:              "res-host",
			ResourceType:    model.ResourceTypeHost,
			ResourceSubtype: "vm",
			Name:            "prod-db-host-01",
			DisplayName:     "Prod DB Host 01",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-platform",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Source:          "manual",
			Labels:          map[string]string{"team": "platform"},
			CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
		"res-no-profile": {
			ID:              "res-no-profile",
			ResourceType:    model.ResourceTypeHost,
			ResourceSubtype: "vm",
			Name:            "bare-host",
			DisplayName:     "Bare Host",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-platform",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Source:          "manual",
			Labels:          map[string]string{},
			CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
		},
	}

	if res, ok := resources[id]; ok {
		return &res, nil
	}
	return nil, service.ErrResourceNotFound
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
	resourceRepo := fakeResourceRepo{
		profiles: map[string]*model.ResourceProfileResponse{
			"res-db-instance": {
				ResourceID:      "res-db-instance",
				ResourceType:    model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql",
				Profile: map[string]any{
					"engine":  "mysql",
					"version": "8.0.36",
					"host":    "prod-db-host-01.internal",
					"port":    3306,
					"role":    "primary",
				},
			},
			"res-db-cluster": {
				ResourceID:      "res-db-cluster",
				ResourceType:    model.ResourceTypeDatabaseCluster,
				ResourceSubtype: "mysql",
				Profile: map[string]any{
					"engine":          "mysql",
					"topologyMode":    "primary-replica",
					"primaryEndpoint": "order-mysql-cluster-prod.internal:3306",
				},
			},
			"res-service": {
				ResourceID:      "res-service",
				ResourceType:    model.ResourceTypeService,
				ResourceSubtype: "api",
				Profile: map[string]any{
					"systemName":    "order-api",
					"repositoryUrl": "https://example.com/repos/order-api",
					"runtimeEnv":    "kubernetes",
				},
			},
			"res-host": {
				ResourceID:      "res-host",
				ResourceType:    model.ResourceTypeHost,
				ResourceSubtype: "vm",
				Profile: map[string]any{
					"hostname":  "prod-db-host-01.internal",
					"ipAddress": "10.0.10.21",
					"osName":    "Ubuntu 24.04",
				},
			},
		},
	}

	deps := Dependencies{
		ResourceService:     service.NewResourceService(resourceRepo),
		RelationService:     service.NewRelationService(fakeRelationRepo{}),
		AuditService:        service.NewAuditService(fakeAuditRepo{}),
		AuthService:         service.NewAuthService(fakeUserCredentialRepo{}, "test-secret"),
		EnvironmentService:  service.NewEnvironmentService(fakeEnvironmentRepo{}),
		OwnerService:        service.NewOwnerService(fakeOwnerRepo{}),
		RoleService:         service.NewRoleService(fakeRoleRepo{}),
		ResourceTypeService:     service.NewResourceTypeService(fakeResourceTypeRepo{}),
		RelationTypeService:     service.NewRelationTypeService(fakeRelationTypeRepo{}),
		LifecycleStatusService:  service.NewLifecycleStatusService(fakeLifecycleStatusRepo{}),
		HealthStatusService:     service.NewHealthStatusService(fakeHealthStatusRepo{}),
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

type fakeResourceTypeRepo struct{}

func (fakeResourceTypeRepo) ListResourceTypes() ([]model.DictionaryItem, error) {
	return model.ResourceTypeDictionary(), nil
}

type fakeRelationTypeRepo struct{}

func (fakeRelationTypeRepo) ListRelationTypes() ([]model.DictionaryItem, error) {
	return model.RelationTypeDictionary(), nil
}

type fakeLifecycleStatusRepo struct{}

func (fakeLifecycleStatusRepo) ListLifecycleStatuses() ([]model.DictionaryItem, error) {
	return model.LifecycleStatusDictionary(), nil
}

type fakeHealthStatusRepo struct{}

func (fakeHealthStatusRepo) ListHealthStatuses() ([]model.DictionaryItem, error) {
	return model.HealthStatusDictionary(), nil
}
