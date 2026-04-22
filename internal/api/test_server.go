// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, internal/service, net/http/httptest
// output: TestServer struct, NewTestServer
// pos: Test infrastructure — fake repos and pre-wired router for handler tests
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type TestServer struct {
	Router *chi.Mux
}

type fakeResourceRepo struct {
	resources map[string]model.Resource
	listOrder []string
	profiles  map[string]*model.ResourceProfileResponse
	nextID    int
	now       time.Time
}

func (f *fakeResourceRepo) GetResourceProfile(id string) (*model.ResourceProfileResponse, error) {
	res, err := f.GetResource(id)
	if err != nil {
		return nil, err
	}
	if p, ok := f.profiles[id]; ok {
		return cloneProfileResponse(p), nil
	}
	return &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    res.ResourceType,
		ResourceSubtype: res.ResourceSubtype,
		Profile:         map[string]any{},
	}, nil
}

func (f *fakeResourceRepo) ListResources(_ context.Context, q model.ResourceListQuery) ([]model.Resource, int, error) {
	filtered := make([]model.Resource, 0, len(f.listOrder))
	for _, id := range f.listOrder {
		item, ok := f.resources[id]
		if !ok {
			continue
		}
		if len(q.ResourceTypes) > 0 && !containsString(q.ResourceTypes, string(item.ResourceType)) {
			continue
		}
		if len(q.EnvironmentIDs) > 0 && !containsString(q.EnvironmentIDs, item.EnvironmentID) {
			continue
		}
		if len(q.LifecycleStatus) > 0 && !containsString(q.LifecycleStatus, item.LifecycleStatus) {
			continue
		}
		if len(q.HealthStatuses) > 0 && !containsString(q.HealthStatuses, item.HealthStatus) {
			continue
		}
			if len(q.ResourceSubtypes) > 0 && !containsString(q.ResourceSubtypes, item.ResourceSubtype) {
				continue
			}
		if q.Query != "" {
			lq := strings.ToLower(q.Query)
			labelMatch := false
			for _, v := range item.Labels {
				if strings.Contains(strings.ToLower(v), lq) {
					labelMatch = true
					break
				}
			}
			if !strings.Contains(strings.ToLower(item.Name), lq) &&
				!strings.Contains(strings.ToLower(item.DisplayName), lq) &&
				!strings.Contains(strings.ToLower(item.ExternalID), lq) &&
				!labelMatch {
				continue
			}
		}
			if q.ArchivedOnly && !item.IsArchived() {
					continue
				}
				if !q.ArchivedOnly && !q.IncludeArchived && item.IsArchived() {
				continue
			}
		filtered = append(filtered, cloneResource(item))
	}

	total := len(filtered)
	offset := (q.Page - 1) * q.PageSize
	if offset >= total {
		return []model.Resource{}, total, nil
	}
	end := offset + q.PageSize
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

func (f *fakeResourceRepo) GetResource(id string) (*model.Resource, error) {
	res, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}
	cloned := cloneResource(res)
	return &cloned, nil
}

func (f *fakeResourceRepo) CreateResource(_ context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	for _, existing := range f.resources {
		if existing.EnvironmentID == input.EnvironmentID && existing.Name == input.Name {
			return nil, service.ErrResourceConflict
		}
	}

	f.nextID++
	createdAt := f.now.Add(time.Duration(f.nextID) * time.Minute)
	created := model.Resource{
		ID:              fmt.Sprintf("res-created-%d", f.nextID),
		ResourceType:    input.ResourceType,
		ResourceSubtype: input.ResourceSubtype,
		Name:            input.Name,
		DisplayName:     input.DisplayName,
		EnvironmentID:   input.EnvironmentID,
		OwnerID:         input.OwnerID,
		LifecycleStatus: string(input.LifecycleStatus),
		HealthStatus:    string(input.HealthStatus),
		Source:          input.Source,
		ExternalID:      input.ExternalID,
		Labels:          cloneLabels(input.Labels),
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}
	f.resources[created.ID] = created
	f.listOrder = append(f.listOrder, created.ID)

	cloned := cloneResource(created)
	return &cloned, nil
}

func (f *fakeResourceRepo) UpdateResource(_ context.Context, id string, input model.ResourceUpdateInput) (*model.Resource, error) {
	existing, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}

	updated := cloneResource(existing)
	if input.ResourceSubtype != nil {
		updated.ResourceSubtype = *input.ResourceSubtype
	}
	if input.DisplayName != nil {
		updated.DisplayName = *input.DisplayName
	}
	if input.EnvironmentID != nil {
		updated.EnvironmentID = *input.EnvironmentID
	}
	if input.OwnerID != nil {
		updated.OwnerID = *input.OwnerID
	}
	if input.LifecycleStatus != nil {
		updated.LifecycleStatus = string(*input.LifecycleStatus)
	}
	if input.HealthStatus != nil {
		updated.HealthStatus = string(*input.HealthStatus)
	}
	if input.Source != nil {
		updated.Source = *input.Source
	}
	if input.ExternalID != nil {
		updated.ExternalID = *input.ExternalID
	}
	if input.Labels != nil {
		updated.Labels = cloneLabels(*input.Labels)
	}
	updated.UpdatedAt = existing.UpdatedAt.Add(time.Minute)

	f.resources[id] = updated
	cloned := cloneResource(updated)
	return &cloned, nil
}

func (f *fakeResourceRepo) ArchiveResource(_ context.Context, id string, reason string) (*model.Resource, error) {
	res, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}
	now := f.now.Add(time.Duration(f.nextID+100) * time.Minute)
	res.ArchivedAt = &now
	res.ArchiveReason = &reason
	f.resources[id] = res
	cloned := cloneResource(res)
	return &cloned, nil
}

func (f *fakeResourceRepo) UnarchiveResource(_ context.Context, id string) (*model.Resource, error) {
	res, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}
	res.ArchivedAt = nil
	res.ArchivedBy = nil
	res.ArchiveReason = nil
	f.resources[id] = res
	cloned := cloneResource(res)
	return &cloned, nil
}

type fakeRelationRepo struct {
	resources *fakeResourceRepo
	relations map[string]model.ResourceRelation
	order     []string
	nextID    int
	now       time.Time
}

func (f *fakeRelationRepo) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	items := make([]model.ResourceRelation, 0)
	for _, id := range f.order {
		relation, ok := f.relations[id]
		if !ok {
			continue
		}
		if relation.FromResourceID == resourceID || relation.ToResourceID == resourceID {
			items = append(items, relation)
		}
	}
	return items, nil
}

func (f *fakeRelationRepo) GetResource(id string) (*model.Resource, error) {
	return f.resources.GetResource(id)
}

func (f *fakeRelationRepo) CreateRelation(_ context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	for _, existing := range f.relations {
		if existing.FromResourceID == input.FromResourceID && existing.ToResourceID == input.ToResourceID && existing.RelationType == input.RelationType {
			return nil, service.ErrRelationConflict
		}
	}

	f.nextID++
	relation := model.ResourceRelation{
		ID:             fmt.Sprintf("rel-created-%d", f.nextID),
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      f.now.Add(time.Duration(f.nextID) * time.Minute),
	}
	f.relations[relation.ID] = relation
	f.order = append(f.order, relation.ID)

	return &relation, nil
}

func (f *fakeRelationRepo) DeleteRelation(_ context.Context, relationID string) error {
	if _, ok := f.relations[relationID]; !ok {
		return service.ErrRelationNotFound
	}
	delete(f.relations, relationID)
	filtered := make([]string, 0, len(f.order))
	for _, id := range f.order {
		if id != relationID {
			filtered = append(filtered, id)
		}
	}
	f.order = filtered
	return nil
}

type fakeTopologyRepo struct {
	resources *fakeResourceRepo
	relations *fakeRelationRepo
}

func (f *fakeTopologyRepo) GetResource(id string) (*model.Resource, error) {
	return f.resources.GetResource(id)
}

func (f *fakeTopologyRepo) ListRelationsByResourceIDs(ids []string) ([]model.ResourceRelation, error) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var result []model.ResourceRelation
	for _, id := range f.relations.order {
		rel, ok := f.relations.relations[id]
		if !ok {
			continue
		}
		if idSet[rel.FromResourceID] || idSet[rel.ToResourceID] {
			result = append(result, rel)
		}
	}
	return result, nil
}

type fakeAuditRepo struct{}

func (fakeAuditRepo) ListAuditEvents(_ context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error) {
	all := []model.AuditEvent{
		{
			ID:               "audit-1",
			ActorUserID:      "user-1",
			TargetResourceID: "res-1",
			EventType:        "resource.updated",
			Result:           "success",
			CreatedAt:        time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
		},
		{
			ID:               "audit-2",
			ActorUserID:      "user-2",
			TargetResourceID: "res-2",
			EventType:        "resource.created",
			Result:           "failure",
			CreatedAt:        time.Date(2026, 4, 11, 22, 0, 0, 0, time.UTC),
		},
	}

	filtered := make([]model.AuditEvent, 0)
	for _, item := range all {
		if q.TargetResourceID != "" && item.TargetResourceID != q.TargetResourceID {
			continue
		}
		if len(q.EventTypes) > 0 && !containsString(q.EventTypes, item.EventType) {
			continue
		}
		if len(q.Results) > 0 && !containsString(q.Results, item.Result) {
			continue
		}
		filtered = append(filtered, item)
	}

	total := len(filtered)
	offset := (q.Page - 1) * q.PageSize
	if offset >= total {
		return []model.AuditEvent{}, total, nil
	}
	end := offset + q.PageSize
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
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
	archivedAt := time.Date(2026, 4, 11, 22, 0, 0, 0, time.UTC)
	archiveReason := "retired"
	resourceRepo := &fakeResourceRepo{
		resources: map[string]model.Resource{
			"res-1": {
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
				ExternalID:      "ext-order-mysql",
				Labels:          map[string]string{"team": "order"},
				CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
				UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			},
			"res-2": {
				ID:              "res-2",
				ResourceType:    model.ResourceTypeHost,
				ResourceSubtype: "vm",
				Name:            "prod-host-01",
				DisplayName:     "Prod Host 01",
				EnvironmentID:   "env-staging",
				OwnerID:         "owner-platform",
				LifecycleStatus: "degraded",
				HealthStatus:    "warning",
				Source:          "manual",
				ExternalID:      "ext-prod-host",
				Labels:          map[string]string{"team": "platform"},
				CreatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
				UpdatedAt:       time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
			},
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
				"res-archived": {
					ID:              "res-archived",
					ResourceType:    model.ResourceTypeHost,
					ResourceSubtype: "vm",
					Name:            "archived-host",
					DisplayName:     "Archived Host",
					EnvironmentID:   "env-prod",
					OwnerID:         "owner-platform",
					LifecycleStatus: "decommissioned",
					HealthStatus:    "unknown",
					Source:          "manual",
					Labels:          map[string]string{},
					CreatedAt:       time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC),
					UpdatedAt:       time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC),
					ArchivedAt:      &archivedAt,
					ArchiveReason:   &archiveReason,
				},
			},
			listOrder: []string{"res-1", "res-2", "res-archived"},
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
		now: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
	}

	relationRepo := &fakeRelationRepo{
		resources: resourceRepo,
		relations: map[string]model.ResourceRelation{
			"rel-1": {
				ID:             "rel-1",
				FromResourceID: "res-service",
				ToResourceID:   "res-db-instance",
				RelationType:   model.RelationTypeDependsOn,
				CreatedAt:      time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
			},
			"rel-2": {
				ID:             "rel-2",
				FromResourceID: "res-db-instance",
				ToResourceID:   "res-db-cluster",
				RelationType:   model.RelationTypeMemberOf,
				CreatedAt:      time.Date(2026, 4, 11, 21, 1, 0, 0, time.UTC),
			},
			"rel-3": {
				ID:             "rel-3",
				FromResourceID: "res-db-instance",
				ToResourceID:   "res-host",
				RelationType:   model.RelationTypeRunsOn,
				CreatedAt:      time.Date(2026, 4, 11, 21, 2, 0, 0, time.UTC),
			},
		},
		order: []string{"rel-1", "rel-2", "rel-3"},
		now:   time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
	}

	topologyRepo := &fakeTopologyRepo{
		resources: resourceRepo,
		relations: relationRepo,
	}

	deps := Dependencies{
		ResourceService:         service.NewResourceService(resourceRepo),
		RelationService:         service.NewRelationService(relationRepo),
		TopologyService:         service.NewTopologyService(topologyRepo),
		AuditService:            service.NewAuditService(fakeAuditRepo{}),
		AuthService:             service.NewAuthService(fakeUserCredentialRepo{}, "test-secret"),
		EnvironmentService:      service.NewEnvironmentService(fakeEnvironmentRepo{}),
		OwnerService:            service.NewOwnerService(fakeOwnerRepo{}),
		RoleService:             service.NewRoleService(fakeRoleRepo{}),
		ResourceTypeService:     service.NewResourceTypeService(fakeResourceTypeRepo{}),
		RelationTypeService:     service.NewRelationTypeService(fakeRelationTypeRepo{}),
		LifecycleStatusService:  service.NewLifecycleStatusService(fakeLifecycleStatusRepo{}),
		HealthStatusService:     service.NewHealthStatusService(fakeHealthStatusRepo{}),
		ResourceSubtypeService:  service.NewResourceSubtypeService(),
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

type fakeProfileRepo struct{}

func (fakeProfileRepo) UpsertHostProfile(_ context.Context, _ string, _, _, _ string) error {
	return nil
}
func (fakeProfileRepo) UpsertDatabaseInstanceProfile(_ context.Context, _ string, _, _, _ string, _ int, _ string) error {
	return nil
}
func (fakeProfileRepo) UpsertDatabaseClusterProfile(_ context.Context, _ string, _, _, _ string) error {
	return nil
}
func (fakeProfileRepo) UpsertServiceProfile(_ context.Context, _ string, _, _, _ string) error {
	return nil
}
func (fakeProfileRepo) DeleteProfile(_ context.Context, _, _ string) error {
	return nil
}

func cloneResource(resource model.Resource) model.Resource {
	resource.Labels = cloneLabels(resource.Labels)
	return resource
}

func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func cloneProfileResponse(profile *model.ResourceProfileResponse) *model.ResourceProfileResponse {
	if profile == nil {
		return nil
	}
	cloned := &model.ResourceProfileResponse{
		ResourceID:      profile.ResourceID,
		ResourceType:    profile.ResourceType,
		ResourceSubtype: profile.ResourceSubtype,
		Profile:         make(map[string]any, len(profile.Profile)),
	}
	for key, value := range profile.Profile {
		cloned.Profile[key] = value
	}
	return cloned
}
