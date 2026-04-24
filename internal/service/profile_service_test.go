// Package service provides tests for profile write operations.
// input: internal/service ProfileService, internal/model, testing
// output: TestProfileService* functions
// pos: Validates profile write business rules
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// fakeProfileRepo records profile upsert and delete calls for assertions.
type fakeProfileRepo struct {
	upsertedType   string
	upsertedFields map[string]interface{}
	deletedType    string
	deletedID      uint64
	deleteErr      error
}

func (f *fakeProfileRepo) UpsertHostProfile(_ context.Context, resourceID uint64, hostname, ipAddress, osName string) error {
	f.upsertedType = "host"
	f.upsertedFields = map[string]interface{}{
		"resourceID": resourceID,
		"hostname":   hostname,
		"ipAddress":  ipAddress,
		"osName":     osName,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertDatabaseInstanceProfile(_ context.Context, resourceID uint64, engine, version, host string, port int, role string) error {
	f.upsertedType = "database_instance"
	f.upsertedFields = map[string]interface{}{
		"resourceID": resourceID,
		"engine":     engine,
		"version":    version,
		"host":       host,
		"port":       port,
		"role":       role,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertDatabaseClusterProfile(_ context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error {
	f.upsertedType = "database_cluster"
	f.upsertedFields = map[string]interface{}{
		"resourceID":      resourceID,
		"engine":          engine,
		"topologyMode":    topologyMode,
		"primaryEndpoint": primaryEndpoint,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertServiceProfile(_ context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error {
	f.upsertedType = "service"
	f.upsertedFields = map[string]interface{}{
		"resourceID":    resourceID,
		"systemName":    systemName,
		"repositoryUrl": repositoryUrl,
		"runtimeEnv":    runtimeEnv,
	}
	return nil
}

func (f *fakeProfileRepo) DeleteProfile(_ context.Context, resourceID uint64, resourceType string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedType = resourceType
	f.deletedID = resourceID
	return nil
}

// helper to build a non-archived resource with a given type.
func makeActiveResource(resourceType model.ResourceType) model.Resource {
	return model.Resource{
		ID:            testResource1ID,
		ResourceType:  resourceType,
		Name:          "test-resource",
		DisplayName:   "Test Resource",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
	}
}

// helper to build an archived resource with a given type.
func makeArchivedResource(resourceType model.ResourceType) model.Resource {
	now := time.Now().UTC()
	r := makeActiveResource(resourceType)
	r.ArchivedAt = &now
	return r
}

// ---------- PutProfile: success for each supported type ----------

func TestProfileServicePutProfileHost(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname":  "web-01",
		"ipAddress": "10.0.0.1",
		"osName":    "ubuntu-22.04",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "host" {
		t.Fatalf("expected upsertedType host, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["hostname"] != "web-01" {
		t.Fatalf("expected hostname web-01, got %v", profileRepo.upsertedFields["hostname"])
	}
	if profileRepo.upsertedFields["ipAddress"] != "10.0.0.1" {
		t.Fatalf("expected ipAddress 10.0.0.1, got %v", profileRepo.upsertedFields["ipAddress"])
	}
	if profileRepo.upsertedFields["osName"] != "ubuntu-22.04" {
		t.Fatalf("expected osName ubuntu-22.04, got %v", profileRepo.upsertedFields["osName"])
	}
}

func TestProfileServicePutProfileDatabaseInstance(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseInstance),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"engine":  "mysql",
		"version": "8.0",
		"host":    "db-01.internal",
		"port":    3306,
		"role":    "primary",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "database_instance" {
		t.Fatalf("expected upsertedType database_instance, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["engine"] != "mysql" {
		t.Fatalf("expected engine mysql, got %v", profileRepo.upsertedFields["engine"])
	}
	if profileRepo.upsertedFields["port"] != 3306 {
		t.Fatalf("expected port 3306, got %v", profileRepo.upsertedFields["port"])
	}
}

func TestProfileServicePutProfileDatabaseCluster(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseCluster),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"engine":          "mysql",
		"topologyMode":    "master-slave",
		"primaryEndpoint": "mysql-cluster.internal:3306",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "database_cluster" {
		t.Fatalf("expected upsertedType database_cluster, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["topologyMode"] != "master-slave" {
		t.Fatalf("expected topologyMode master-slave, got %v", profileRepo.upsertedFields["topologyMode"])
	}
}

func TestProfileServicePutProfileService(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeService),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"systemName":    "order-api",
		"repositoryUrl": "https://github.com/example/order-api",
		"runtimeEnv":    "production",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "service" {
		t.Fatalf("expected upsertedType service, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["systemName"] != "order-api" {
		t.Fatalf("expected systemName order-api, got %v", profileRepo.upsertedFields["systemName"])
	}
}

// ---------- PutProfile: not-found guard ----------

func TestProfileServicePutProfileNotFound(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testMissingID, map[string]interface{}{})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
	if profileRepo.upsertedType != "" {
		t.Fatalf("expected no upsert call, but upsertedType = %q", profileRepo.upsertedType)
	}
}

// ---------- PutProfile: archived guard ----------

func TestProfileServicePutProfileArchived(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeArchivedResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "web-01",
	})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
	if profileRepo.upsertedType != "" {
		t.Fatalf("expected no upsert call, but upsertedType = %q", profileRepo.upsertedType)
	}
}

// ---------- PutProfile: unsupported type ----------

func TestProfileServicePutProfileUnsupportedType(t *testing.T) {
	unsupportedTypes := []model.ResourceType{
		model.ResourceTypeDomainName,
		model.ResourceTypeVirtualIP,
		model.ResourceTypeDatabaseProxy,
		model.ResourceTypeControlPlaneComponent,
	}

	for _, rt := range unsupportedTypes {
		profileRepo := &fakeProfileRepo{}
		resourceRepo := &fakeResourceWriteRepo{
			resources: map[uint64]model.Resource{
				testResource1ID: makeActiveResource(rt),
			},
		}
		svc := NewProfileService(profileRepo, resourceRepo)

		err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{})
		if !errors.Is(err, ErrProfileNotSupported) {
			t.Fatalf("resource type %q: expected ErrProfileNotSupported, got %v", rt, err)
		}
		if profileRepo.upsertedType != "" {
			t.Fatalf("resource type %q: expected no upsert call, but upsertedType = %q", rt, profileRepo.upsertedType)
		}
	}
}

// ---------- PatchProfile: same guards as PutProfile ----------

func TestProfileServicePatchProfileHost(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "web-02",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "host" {
		t.Fatalf("expected upsertedType host, got %q", profileRepo.upsertedType)
	}
}

func TestProfileServicePatchProfileNotFound(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testMissingID, map[string]interface{}{})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestProfileServicePatchProfileArchived(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeArchivedResource(model.ResourceTypeDatabaseInstance),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{
		"engine": "mysql",
	})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}

func TestProfileServicePatchProfileUnsupportedType(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDomainName),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{})
	if !errors.Is(err, ErrProfileNotSupported) {
		t.Fatalf("expected ErrProfileNotSupported, got %v", err)
	}
}

// ---------- DeleteProfile: success, not-found, archived ----------

func TestProfileServiceDeleteProfile(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.DeleteProfile(context.Background(), testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.deletedID != testResource1ID {
		t.Fatalf("expected deletedID %d, got %d", testResource1ID, profileRepo.deletedID)
	}
	if profileRepo.deletedType != "host" {
		t.Fatalf("expected deletedType host, got %q", profileRepo.deletedType)
	}
}

func TestProfileServiceDeleteProfileNotFound(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.DeleteProfile(context.Background(), testMissingID)
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
	if profileRepo.deletedID != 0 {
		t.Fatalf("expected no delete call, but deletedID = %d", profileRepo.deletedID)
	}
}

func TestProfileServiceDeleteProfileArchived(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeArchivedResource(model.ResourceTypeService),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.DeleteProfile(context.Background(), testResource1ID)
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
	if profileRepo.deletedID != 0 {
		t.Fatalf("expected no delete call, but deletedID = %d", profileRepo.deletedID)
	}
}

// ---------- getStringField / getIntField: exercised via missing fields ----------

func TestProfileServicePutProfileDefaultsMissingFields(t *testing.T) {
	// Passing an empty fields map should still succeed — helpers return zero values.
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseInstance),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "database_instance" {
		t.Fatalf("expected upsertedType database_instance, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["port"] != 0 {
		t.Fatalf("expected default port 0, got %v", profileRepo.upsertedFields["port"])
	}
	if profileRepo.upsertedFields["engine"] != "" {
		t.Fatalf("expected default engine empty, got %q", profileRepo.upsertedFields["engine"])
	}
}
