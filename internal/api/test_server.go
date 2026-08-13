// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, internal/service, net/http
// output: TestServer struct, NewTestServer
// pos: Test infrastructure — fake repos and pre-wired router for handler tests
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type TestServer struct {
	Router http.Handler
	deps   Dependencies
}

type fakeResourceRepo struct {
	resources map[uint64]model.Resource
	listOrder []uint64
	profiles  map[uint64]*model.ResourceProfileResponse
	nextID    uint64
	now       time.Time
}

func (f *fakeResourceRepo) GetResourceProfile(id uint64) (*model.ResourceProfileResponse, error) {
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

func (f *fakeResourceRepo) UpsertHostProfile(_ context.Context, resourceID uint64, hostname, ipAddress, osName string) error {
	res := f.resources[resourceID]
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: res.ResourceSubtype,
		Profile: map[string]any{
			"hostname":  hostname,
			"ipAddress": ipAddress,
			"osName":    osName,
		},
	}
	return nil
}

func (f *fakeResourceRepo) UpsertDatabaseInstanceProfile(_ context.Context, resourceID uint64, engine, version, host string, port int, role string) error {
	res := f.resources[resourceID]
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: res.ResourceSubtype,
		Profile: map[string]any{
			"engine":  engine,
			"version": version,
			"host":    host,
			"port":    port,
			"role":    role,
		},
	}
	return nil
}

func (f *fakeResourceRepo) UpsertDatabaseClusterProfile(_ context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error {
	res := f.resources[resourceID]
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    model.ResourceTypeDatabaseCluster,
		ResourceSubtype: res.ResourceSubtype,
		Profile: map[string]any{
			"engine":          engine,
			"topologyMode":    topologyMode,
			"primaryEndpoint": primaryEndpoint,
		},
	}
	return nil
}

func (f *fakeResourceRepo) UpsertServiceProfile(_ context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error {
	res := f.resources[resourceID]
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: res.ResourceSubtype,
		Profile: map[string]any{
			"systemName":    systemName,
			"repositoryUrl": repositoryUrl,
			"runtimeEnv":    runtimeEnv,
		},
	}
	return nil
}

func (f *fakeResourceRepo) DeleteProfile(_ context.Context, resourceID uint64, _ string) error {
	delete(f.profiles, resourceID)
	return nil
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
		if len(q.EnvironmentIDs) > 0 && !containsUint64(q.EnvironmentIDs, item.EnvironmentID) {
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

func (f *fakeResourceRepo) GetResource(id uint64) (*model.Resource, error) {
	res, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}
	cloned := cloneResource(res)
	if profile, ok := f.profiles[id]; ok {
		cloned.ProfileSummary = profileToSummary(cloned.ResourceType, profile.Profile)
	}
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
		ID:              10000 + f.nextID,
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

func (f *fakeResourceRepo) UpdateResource(_ context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error) {
	existing, ok := f.resources[id]
	if !ok {
		return nil, service.ErrResourceNotFound
	}

	updated := cloneResource(existing)
	if input.Name != nil {
		updated.Name = *input.Name
	}
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

func (f *fakeResourceRepo) ArchiveResource(_ context.Context, id uint64, reason string) (*model.Resource, error) {
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

func (f *fakeResourceRepo) UnarchiveResource(_ context.Context, id uint64) (*model.Resource, error) {
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
	relations map[uint64]model.ResourceRelation
	order     []uint64
	nextID    uint64
	now       time.Time
}

func (f *fakeRelationRepo) ListByResourceID(resourceID uint64) ([]model.ResourceRelation, error) {
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

func (f *fakeRelationRepo) ListRelationViewsByResourceID(resourceID uint64) ([]model.ResourceRelationView, error) {
	items := make([]model.ResourceRelationView, 0)
	for _, id := range f.order {
		relation, ok := f.relations[id]
		if !ok {
			continue
		}
		if relation.FromResourceID != resourceID && relation.ToResourceID != resourceID {
			continue
		}
		var relatedID uint64
		if relation.FromResourceID == resourceID {
			relatedID = relation.ToResourceID
		} else {
			relatedID = relation.FromResourceID
		}
		related, err := f.resources.GetResource(relatedID)
		if err != nil {
			continue
		}
		direction := "outgoing"
		if relation.ToResourceID == resourceID {
			direction = "incoming"
		}
		items = append(items, model.ResourceRelationView{
			ID:                           relation.ID,
			FromResourceID:               relation.FromResourceID,
			ToResourceID:                 relation.ToResourceID,
			RelationType:                 relation.RelationType,
			Direction:                    direction,
			CreatedAt:                    relation.CreatedAt,
			RelatedResourceID:            related.ID,
			RelatedResourceName:          related.Name,
			RelatedResourceDisplayName:   related.DisplayName,
			RelatedResourceType:          string(related.ResourceType),
			RelatedResourceSubtype:       related.ResourceSubtype,
			RelatedResourceHealthStatus:  related.HealthStatus,
			RelatedResourceLifecycleStat: related.LifecycleStatus,
		})
	}
	return items, nil
}

func (f *fakeRelationRepo) ListClusterMembers(clusterID uint64) ([]model.ClusterMemberView, error) {
	items := make([]model.ClusterMemberView, 0)
	for _, id := range f.order {
		relation, ok := f.relations[id]
		if !ok {
			continue
		}
		if relation.ToResourceID != clusterID || relation.RelationType != model.RelationTypeMemberOf {
			continue
		}
		member, err := f.resources.GetResource(relation.FromResourceID)
		if err != nil {
			continue
		}
		var summary *model.ProfileSummary
		if profile, ok := f.resources.profiles[member.ID]; ok {
			summary = profileToSummary(member.ResourceType, profile.Profile)
		}
		items = append(items, model.ClusterMemberView{
			ResourceID:      member.ID,
			Name:            member.Name,
			DisplayName:     member.DisplayName,
			ResourceType:    string(member.ResourceType),
			ResourceSubtype: member.ResourceSubtype,
			LifecycleStatus: member.LifecycleStatus,
			HealthStatus:    member.HealthStatus,
			ProfileSummary:  summary,
		})
	}
	return items, nil
}

func profileToSummary(resourceType model.ResourceType, profile map[string]any) *model.ProfileSummary {
	switch resourceType {
	case model.ResourceTypeDatabaseInstance:
		s := &model.ProfileSummary{}
		if v, ok := profile["hostname"]; !ok {
			if v, ok := profile["host"]; ok {
				s.Hostname = fmt.Sprintf("%v", v)
			}
		} else {
			s.Hostname = fmt.Sprintf("%v", v)
		}
		if v, ok := profile["port"]; ok {
			switch p := v.(type) {
			case float64:
				s.Port = int(p)
			case int:
				s.Port = p
			}
		}
		if v, ok := profile["engine"]; ok {
			s.Engine = fmt.Sprintf("%v", v)
		}
		if v, ok := profile["version"]; ok {
			s.Version = fmt.Sprintf("%v", v)
		}
		if v, ok := profile["role"]; ok {
			s.Role = fmt.Sprintf("%v", v)
		}
		return s
	case model.ResourceTypeHost:
		s := &model.ProfileSummary{}
		if v, ok := profile["hostname"]; ok {
			s.Hostname = fmt.Sprintf("%v", v)
		}
		if v, ok := profile["ipAddress"]; ok {
			s.IP = fmt.Sprintf("%v", v)
		}
		return s
	default:
		return nil
	}
}

func (f *fakeRelationRepo) GetResource(id uint64) (*model.Resource, error) {
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
		ID:             20000 + f.nextID,
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      f.now.Add(time.Duration(f.nextID) * time.Minute),
	}
	f.relations[relation.ID] = relation
	f.order = append(f.order, relation.ID)

	return &relation, nil
}

func (f *fakeRelationRepo) DeleteRelation(_ context.Context, relationID uint64) error {
	if _, ok := f.relations[relationID]; !ok {
		return service.ErrRelationNotFound
	}
	delete(f.relations, relationID)
	filtered := make([]uint64, 0, len(f.order))
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

func (f *fakeTopologyRepo) GetResource(id uint64) (*model.Resource, error) {
	return f.resources.GetResource(id)
}

func (f *fakeTopologyRepo) ListRelationsByResourceIDs(ids []uint64) ([]model.ResourceRelation, error) {
	idSet := make(map[uint64]bool, len(ids))
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
	targetResourceID := uint64(1)
	actor1 := uint64(1)
	actor2 := uint64(2)
	all := []model.AuditEvent{
		{ID: 1, ActorUserID: &actor1, TargetResourceID: &targetResourceID, EventType: "resource.updated", Result: "success", CreatedAt: time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC)},
		{ID: 2, ActorUserID: &actor2, TargetResourceID: nil, EventType: "resource.created", Result: "failure", CreatedAt: time.Date(2026, 4, 11, 22, 0, 0, 0, time.UTC)},
	}

	filtered := make([]model.AuditEvent, 0)
	for _, item := range all {
		if q.TargetResourceID != nil {
			if item.TargetResourceID == nil || *item.TargetResourceID != *q.TargetResourceID {
				continue
			}
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

func (fakeAuditRepo) ListByResourceID(resourceID uint64) ([]model.AuditEvent, error) {
	actor := uint64(1)
	return []model.AuditEvent{{ID: 1, ActorUserID: &actor, TargetResourceID: &resourceID, EventType: "resource.updated", Result: "success", CreatedAt: time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC)}}, nil
}

// fakeQueryTargetRow pairs a raw query target with the environment id used for
// server-side filtering in the fake.
type fakeQueryTargetRow struct {
	target        model.QueryTarget
	environmentID uint64
}

// fakeQueryTargetRepo returns curated raw query targets (identity + connection
// context only). The service derives capability/readiness/governance from
// these, so the fake exercises the full derivation path through the handler.
type fakeQueryTargetRepo struct {
	targets []fakeQueryTargetRow
}

func (f fakeQueryTargetRepo) ListQueryTargets(_ context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, int, error) {
	out := make([]model.QueryTarget, 0, len(f.targets))
	for _, row := range f.targets {
		if q.TargetID != 0 && row.target.ResourceID != q.TargetID {
			continue
		}
		if q.Engine != "" && !strings.EqualFold(row.target.ConnectionContext.Engine, q.Engine) {
			continue
		}
		if q.EnvironmentID != 0 && row.environmentID != q.EnvironmentID {
			continue
		}
		if q.Q != "" {
			lq := strings.ToLower(q.Q)
			portStr := fmt.Sprintf("%d", row.target.ConnectionContext.Port)
			if !strings.Contains(strings.ToLower(row.target.ResourceName), lq) &&
				!strings.Contains(strings.ToLower(row.target.DisplayName), lq) &&
				!strings.Contains(strings.ToLower(row.target.ConnectionContext.Engine), lq) &&
				!strings.Contains(strings.ToLower(row.target.ConnectionContext.Host), lq) &&
				!strings.Contains(strings.ToLower(row.target.ConnectionContext.Environment), lq) &&
				!strings.Contains(strings.ToLower(row.target.ConnectionContext.Owner), lq) &&
				!strings.Contains(strings.ToLower(row.target.ConnectionContext.ClusterName), lq) &&
				!strings.Contains(portStr, q.Q) {
				continue
			}
		}
		out = append(out, row.target)
	}

	total := len(out)
	if q.Page > 0 && q.PageSize > 0 {
		offset := (q.Page - 1) * q.PageSize
		if offset >= total {
			return []model.QueryTarget{}, total, nil
		}
		end := offset + q.PageSize
		if end > total {
			end = total
		}
		return out[offset:end], total, nil
	}
	return out, total, nil
}

func queryTargetSeed() []fakeQueryTargetRow {
	return []fakeQueryTargetRow{
		{
			environmentID: 1,
			target: model.QueryTarget{
				ResourceID: 22, ResourceName: "analytics-ch-node-01-prod", DisplayName: "Analytics ClickHouse Node 01 Production", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Production", Owner: "DBA Team", Engine: "clickhouse", Host: "prod-ch-host-01.internal", Port: 8123, ClusterID: 14, ClusterName: "Analytics ClickHouse Cluster Production"},
			},
		},
		{
			environmentID: 1,
			target: model.QueryTarget{
				ResourceID: 23, ResourceName: "order-mysql-node-01-prod", DisplayName: "Order MySQL Node 01 Production", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Production", Owner: "DBA Team", Engine: "mysql", Host: "prod-mysql-host-01.internal", Port: 3306, ClusterID: 4, ClusterName: "Order MySQL Cluster Production"},
			},
		},
		{
			environmentID: 2,
			target: model.QueryTarget{
				ResourceID: 24, ResourceName: "session-redis-staging", DisplayName: "Session Redis Staging", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Staging", Owner: "Platform Team", Engine: "redis", Host: "staging-redis.internal", Port: 6379},
			},
		},
		{
			environmentID: 2,
			target: model.QueryTarget{
				ResourceID: 25, ResourceName: "profile-mongodb-staging", DisplayName: "Profile MongoDB Staging", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Staging", Owner: "Platform Team", Engine: "mongodb", Host: "staging-mongo.internal", Port: 27017},
			},
		},
		{
			environmentID: 1,
			target: model.QueryTarget{
				ResourceID: 26, ResourceName: "legacy-oracle-node-prod", DisplayName: "Legacy Oracle Node Production", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Production", Owner: "DBA Team", Engine: "oracle", Host: "prod-oracle.internal", Port: 1521},
			},
		},
		{
			environmentID: 1,
			target: model.QueryTarget{
				ResourceID: 27, ResourceName: "order-mysql-node-02-prod", DisplayName: "Order MySQL Node 02 Production", ResourceType: model.ResourceTypeDatabaseInstance,
				ConnectionContext: model.QueryTargetConnectionContext{Environment: "Production", Owner: "DBA Team", Engine: "mysql", Host: "", Port: 3306, ClusterID: 4, ClusterName: "Order MySQL Cluster Production"},
			},
		},
	}
}

// fakeCredentialMetadataStore is an in-memory QueryCredentialMetadataStore for
// the generic test server. It stores metadata only — never a DSN — and records
// audit calls so the Phase 38A credential routes are exercisable in test
// contexts. A not-found returns sql.ErrNoRows so the service classifies it as
// missing_metadata (mirroring the real repository); the write/delete methods
// apply metadata + audit together (atomic from the service's perspective).
type fakeCredentialMetadataStore struct {
	metadata map[uint64]model.QueryCredentialMetadata
	audits   int
}

func (f *fakeCredentialMetadataStore) GetCredentialByResourceID(_ context.Context, rid uint64) (model.QueryCredentialMetadata, error) {
	if m, ok := f.metadata[rid]; ok {
		return m, nil
	}
	return model.QueryCredentialMetadata{}, sql.ErrNoRows
}

func (f *fakeCredentialMetadataStore) UpsertCredentialMetadataWithAudit(_ context.Context, m model.QueryCredentialMetadata, _ uint64, _, _ string) error {
	if f.metadata == nil {
		f.metadata = map[uint64]model.QueryCredentialMetadata{}
	}
	f.metadata[m.ResourceID] = m
	f.audits++
	return nil
}

func (f *fakeCredentialMetadataStore) DeleteCredentialMetadataWithAudit(_ context.Context, rid uint64, _ uint64, _, _ string) error {
	delete(f.metadata, rid)
	f.audits++
	return nil
}

type fakeQuerySchema struct{}

func (f *fakeQuerySchema) ListDatabases(_ context.Context, _, targetID uint64, _ string, _, _ int, _, _ bool) (model.DatabaseListResponse, error) {
	return model.DatabaseListResponse{
		TargetResourceID: int64(targetID),
		Items:            []model.DatabaseSummary{{Name: "testdb"}},
		PageInfo:         model.NewPageInfo(1, 50, 1),
	}, nil
}

func (f *fakeQuerySchema) ListObjects(_ context.Context, _, targetID uint64, database, _, _ string, _, _ int, _ bool) (model.ObjectListResponse, error) {
	return model.ObjectListResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Items:            []model.ObjectSummary{{Database: database, Name: "users", Kind: model.ObjectKindTable}},
		PageInfo:         model.NewPageInfo(1, 50, 1),
	}, nil
}

func (f *fakeQuerySchema) GetObjectDetails(_ context.Context, _, targetID uint64, database, name, kind string, _ bool) (model.ObjectDetailResponse, error) {
	return model.ObjectDetailResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Name:             name,
		Kind:             model.ObjectKind(kind),
		Columns:          []model.ColumnDetail{{Name: "id", DatabaseType: "BIGINT", OrdinalPosition: 1, PrimaryKey: true}},
	}, nil
}

func (f *fakeQuerySchema) GetTableDefinition(_ context.Context, _, targetID uint64, database, name string) (model.TableDefinitionResponse, error) {
	return model.TableDefinitionResponse{
		TargetResourceID: int64(targetID),
		Database:         database,
		Name:             name,
		Kind:             model.ObjectKindTable,
		Dialect:          "mysql",
		Definition:       "CREATE TABLE `" + name + "` (id BIGINT PRIMARY KEY)",
		Truncated:        false,
	}, nil
}

func (f *fakeQuerySchema) GetRelationshipMap(_ context.Context, _, targetID uint64, database, name string, _ bool) (model.RelationshipMapResponse, error) {
	return model.RelationshipMapResponse{
		TargetResourceID: int64(targetID),
		Root: model.RelationshipMapNode{
			ID:       "n0",
			Database: database,
			Name:     name,
			Kind:     model.ObjectKindTable,
			Role:     model.RelationshipMapRoleRoot,
		},
		Nodes: []model.RelationshipMapNode{{
			ID:       "n0",
			Database: database,
			Name:     name,
			Kind:     model.ObjectKindTable,
			Role:     model.RelationshipMapRoleRoot,
		}},
		Edges:     []model.RelationshipMapEdge{},
		Truncated: false,
	}, nil
}

type fakeQueryDisclosure struct{}

func (f *fakeQueryDisclosure) ListPolicies(_ context.Context, _ uint64) ([]model.ResultDisclosurePolicy, error) {
	return []model.ResultDisclosurePolicy{}, nil
}

func (f *fakeQueryDisclosure) CreatePolicy(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	return 1, nil
}

func (f *fakeQueryDisclosure) UpdatePolicy(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) error {
	return nil
}

func (f *fakeQueryDisclosure) DeletePolicy(_ context.Context, _ uint64, _, _, _ string) error {
	return nil
}

type fakeSavedStatementService struct {
	listResp   model.QuerySavedStatementListResponse
	createResp model.QuerySavedStatement
}

func (f *fakeSavedStatementService) List(_ context.Context, _ service.AuthenticatedUser, _ uint64, _ string, _, _ int) (model.QuerySavedStatementListResponse, error) {
	return f.listResp, nil
}

func (f *fakeSavedStatementService) Create(_ context.Context, _ service.AuthenticatedUser, _ uint64, _ model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	return f.createResp, nil
}

func (f *fakeSavedStatementService) Update(_ context.Context, _ service.AuthenticatedUser, _, _ uint64, _ model.QuerySavedStatementUpdateRequest) error {
	return nil
}

func (f *fakeSavedStatementService) Delete(_ context.Context, _ service.AuthenticatedUser, _, _ uint64) error {
	return nil
}

func NewTestServer() *TestServer {
	archivedAt := time.Date(2026, 4, 11, 22, 0, 0, 0, time.UTC)
	archiveReason := "retired"
	resourceRepo := &fakeResourceRepo{
		resources: map[uint64]model.Resource{
			1: {ID: 1, ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql", Name: "order-mysql-prod", DisplayName: "Order MySQL Prod", EnvironmentID: 1, OwnerID: 2, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", ExternalID: "ext-order-mysql", Labels: map[string]string{"team": "order"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			2: {ID: 2, ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Name: "prod-host-01", DisplayName: "Prod Host 01", EnvironmentID: 2, OwnerID: 3, LifecycleStatus: "degraded", HealthStatus: "warning", Source: "manual", ExternalID: "ext-prod-host", Labels: map[string]string{"team": "platform"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			3: {ID: 3, ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql", Name: "order-mysql-prod", DisplayName: "Order MySQL Prod", EnvironmentID: 1, OwnerID: 2, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{"team": "order"}, ClusterId: ptrUint64(4), CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			4: {ID: 4, ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "mysql", Name: "order-mysql-cluster-prod", DisplayName: "Order MySQL Cluster Prod", EnvironmentID: 1, OwnerID: 2, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{"team": "order"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			5: {ID: 5, ResourceType: model.ResourceTypeService, ResourceSubtype: "api", Name: "order-api-prod", DisplayName: "Order API Prod", EnvironmentID: 1, OwnerID: 3, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{"team": "order"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			6: {ID: 6, ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Name: "prod-db-host-01", DisplayName: "Prod DB Host 01", EnvironmentID: 1, OwnerID: 3, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{"team": "platform"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			7: {ID: 7, ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Name: "bare-host", DisplayName: "Bare Host", EnvironmentID: 1, OwnerID: 3, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
			8: {ID: 8, ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Name: "archived-host", DisplayName: "Archived Host", EnvironmentID: 1, OwnerID: 3, LifecycleStatus: "decommissioned", HealthStatus: "unknown", Source: "manual", Labels: map[string]string{}, CreatedAt: time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC), ArchivedAt: &archivedAt, ArchiveReason: &archiveReason},
			9: {ID: 9, ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "clickhouse", Name: "analytics-ch-cluster-prod", DisplayName: "Analytics ClickHouse Cluster Production", EnvironmentID: 1, OwnerID: 2, LifecycleStatus: "running", HealthStatus: "healthy", Source: "manual", Labels: map[string]string{"team": "analytics"}, DatabaseOperationalSummary: &model.DatabaseOperationalSummary{MemberCount: 2, CriticalMemberCount: 1, WarningMemberCount: 0, StoppedMemberCount: 0, DegradedMemberCount: 0, UnknownRoleCount: 0, PrimaryMemberCount: 0, ReplicaMemberCount: 2, WorstMemberID: ptrInt64(10), WorstMemberName: "Analytics ClickHouse Node 02", WorstMemberStatus: "critical"}, CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)},
		},
		listOrder: []uint64{1, 2, 8, 9},
		profiles: map[uint64]*model.ResourceProfileResponse{
			3: {ResourceID: 3, ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql", Profile: map[string]any{"engine": "mysql", "version": "8.0.36", "host": "prod-db-host-01.internal", "port": 3306, "role": "primary"}},
			4: {ResourceID: 4, ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "mysql", Profile: map[string]any{"engine": "mysql", "topologyMode": "primary-replica", "primaryEndpoint": "order-mysql-cluster-prod.internal:3306"}},
			5: {ResourceID: 5, ResourceType: model.ResourceTypeService, ResourceSubtype: "api", Profile: map[string]any{"systemName": "order-api", "repositoryUrl": "https://example.com/repos/order-api", "runtimeEnv": "kubernetes"}},
			6: {ResourceID: 6, ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Profile: map[string]any{"hostname": "prod-db-host-01.internal", "ipAddress": "10.0.10.21", "osName": "Ubuntu 24.04"}},
		},
		now: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC),
	}

	relationRepo := &fakeRelationRepo{
		resources: resourceRepo,
		relations: map[uint64]model.ResourceRelation{
			1: {ID: 1, FromResourceID: 5, ToResourceID: 3, RelationType: model.RelationTypeDependsOn, CreatedAt: time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC)},
			2: {ID: 2, FromResourceID: 3, ToResourceID: 4, RelationType: model.RelationTypeMemberOf, CreatedAt: time.Date(2026, 4, 11, 21, 1, 0, 0, time.UTC)},
			3: {ID: 3, FromResourceID: 3, ToResourceID: 6, RelationType: model.RelationTypeRunsOn, CreatedAt: time.Date(2026, 4, 11, 21, 2, 0, 0, time.UTC)},
		},
		order: []uint64{1, 2, 3},
		now:   time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
	}

	topologyRepo := &fakeTopologyRepo{resources: resourceRepo, relations: relationRepo}
	profileSvc := service.NewProfileService(resourceRepo, resourceRepo)

	queryTargetRepo := fakeQueryTargetRepo{targets: queryTargetSeed()}
	credentialStore := &fakeCredentialMetadataStore{}

	deps := Dependencies{
		ResourceService: service.NewResourceService(resourceRepo, profileSvc),
		RelationService: service.NewRelationService(relationRepo),
		TopologyService: service.NewTopologyService(topologyRepo),
		AuditService:    service.NewAuditService(fakeAuditRepo{}),
		AuthService: service.NewAuthService(service.NewMemoryUserStore(
			model.UserCredential{
				ID: 1, Email: "admin@example.com", RoleName: "admin",
				PasswordHash: "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4",
				IsActive:     true, AuthorizationVersion: 1,
			},
			model.UserCredential{ID: 7, RoleName: "editor", IsActive: true, AuthorizationVersion: 1},
			model.UserCredential{ID: 42, RoleName: "admin", IsActive: true, AuthorizationVersion: 1},
			model.UserCredential{ID: 43, RoleName: "viewer", IsActive: true, AuthorizationVersion: 1},
		), "test-secret"),
		EnvironmentService:         service.NewEnvironmentService(fakeEnvironmentRepo{}),
		OwnerService:               service.NewOwnerService(fakeOwnerRepo{}),
		RoleService:                service.NewRoleService(fakeRoleRepo{}),
		ResourceTypeService:        service.NewResourceTypeService(fakeResourceTypeRepo{}),
		RelationTypeService:        service.NewRelationTypeService(fakeRelationTypeRepo{}),
		LifecycleStatusService:     service.NewLifecycleStatusService(fakeLifecycleStatusRepo{}),
		HealthStatusService:        service.NewHealthStatusService(fakeHealthStatusRepo{}),
		ResourceSubtypeService:     service.NewResourceSubtypeService(),
		ProfileService:             profileSvc,
		QueryTargetService:         service.NewQueryTargetService(queryTargetRepo),
		QueryCredentialService:     service.NewQueryCredentialService(queryTargetRepo, credentialStore, service.NewEnvCredentialResolver()),
		QuerySchemaService:         &fakeQuerySchema{},
		QueryDisclosureService:     &fakeQueryDisclosure{},
		QuerySavedStatementService: &fakeSavedStatementService{},
		QueryExecutionAuth: QueryExecutionAuthConfig{
			Clock: func() time.Time { return time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC) },
		},
	}

	login, err := deps.AuthService.Login("admin@example.com", "secret123")
	if err != nil {
		panic(fmt.Sprintf("test server admin login: %v", err))
	}
	return &TestServer{Router: authenticatedTestRouter(NewRouter(deps), login.Token), deps: deps}
}

func authenticatedTestRouter(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" && testRouteNeedsDefaultActor(r.URL.Path) {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		next.ServeHTTP(w, r)
	})
}

func testRouteNeedsDefaultActor(path string) bool {
	if path == "/resources" || strings.HasPrefix(path, "/resources/") || path == "/resource-relations" || strings.HasPrefix(path, "/resource-relations/") {
		return true
	}
	switch path {
	case "/audit-events", "/environments", "/owners", "/roles", "/resource-types", "/relation-types", "/lifecycle-statuses", "/health-statuses", "/resource-subtypes", "/query-targets":
		return true
	default:
		return false
	}
}

type fakeEnvironmentRepo struct{}

func (fakeEnvironmentRepo) ListEnvironments() ([]model.Environment, error) {
	return []model.Environment{{ID: 1, Name: "Production", Slug: "prod", Description: "Production environment", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}, {ID: 2, Name: "Staging", Slug: "staging", Description: "Staging environment", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}}, nil
}

type fakeOwnerRepo struct{}

func (fakeOwnerRepo) ListOwners() ([]model.Owner, error) {
	return []model.Owner{{ID: 1, Name: "Platform Team", Email: "platform@example.com", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}, {ID: 2, Name: "DBA Team", Email: "dba@example.com", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}}, nil
}

type fakeRoleRepo struct{}

func (fakeRoleRepo) ListRoles() ([]model.Role, error) {
	return []model.Role{{ID: 1, Name: "admin", Description: "Full platform access", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}, {ID: 2, Name: "editor", Description: "Can manage assets and relations", CreatedAt: time.Date(2026, 4, 11, 20, 0, 0, 0, time.UTC)}}, nil
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

func containsUint64(slice []uint64, val uint64) bool {
	for _, v := range slice {
		if v == val {
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
	cloned := &model.ResourceProfileResponse{ResourceID: profile.ResourceID, ResourceType: profile.ResourceType, ResourceSubtype: profile.ResourceSubtype, Profile: make(map[string]any, len(profile.Profile))}
	for key, value := range profile.Profile {
		cloned.Profile[key] = value
	}
	return cloned
}

func ptrUint64(v uint64) *uint64 { return &v }

func ptrInt64(v int64) *int64 { return &v }
