// Package service provides tests for profile write operations.
// input: internal/service ProfileService, internal/model, testing
// output: TestProfileService* functions
// pos: Validates profile write business rules: strict field validation, PUT full replacement, PATCH partial merge, Database Proxy/Control Plane identity
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// fakeProfileRepo records profile upsert and delete calls for assertions.
type fakeProfileRepo struct {
	upsertedType   string
	upsertedFields map[string]interface{}
	patchedFields  map[string]interface{}
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

func (f *fakeProfileRepo) UpsertDomainNameProfile(_ context.Context, resourceID uint64, fqdn string) error {
	f.upsertedType = "domain_name"
	f.upsertedType = "domain_name"
	f.upsertedFields = map[string]interface{}{
		"resourceID": resourceID,
		"fqdn":       fqdn,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertDatabaseProxyProfile(_ context.Context, resourceID uint64, technologySubtype, host string, port int, role, version string) error {
	f.upsertedType = "database_proxy"
	f.upsertedFields = map[string]interface{}{
		"resourceID":        resourceID,
		"technologySubtype": technologySubtype,
		"host":              host,
		"port":              port,
		"role":              role,
		"version":           version,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertVirtualIPProfile(_ context.Context, resourceID uint64, ipAddress string) error {
	f.upsertedType = "virtual_ip"
	f.upsertedFields = map[string]interface{}{
		"resourceID": resourceID,
		"ipAddress":  ipAddress,
	}
	return nil
}

func (f *fakeProfileRepo) UpsertControlPlaneComponentProfile(_ context.Context, resourceID uint64, componentSubtype, endpoint, version, role string) error {
	f.upsertedType = "control_plane_component"
	f.upsertedFields = map[string]interface{}{
		"resourceID":       resourceID,
		"componentSubtype": componentSubtype,
		"endpoint":         endpoint,
		"version":          version,
		"role":             role,
	}
	return nil
}

func (f *fakeProfileRepo) PatchProfile(_ context.Context, _ uint64, _ model.ResourceType, fields map[string]interface{}) error {
	f.patchedFields = fields
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

func TestProfileServicePutProfileDatabaseProxyAcceptsActiveStandbyRole(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseProxy),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"technologySubtype": "proxysql",
		"host":              "proxy-prod-01",
		"port":              6033,
		"role":              "active",
		"version":           "2.5.5",
	})
	if err != nil {
		t.Fatalf("expected database_proxy profile to be accepted, got %v", err)
	}
	if profileRepo.upsertedType != "database_proxy" {
		t.Fatalf("expected upsertedType database_proxy, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["technologySubtype"] != "proxysql" {
		t.Fatalf("expected technologySubtype proxysql, got %#v", profileRepo.upsertedFields["technologySubtype"])
	}
	if profileRepo.upsertedFields["host"] != "proxy-prod-01" {
		t.Fatalf("expected host proxy-prod-01, got %#v", profileRepo.upsertedFields["host"])
	}
	if profileRepo.upsertedFields["port"] != 6033 {
		t.Fatalf("expected port 6033, got %#v", profileRepo.upsertedFields["port"])
	}
	if profileRepo.upsertedFields["role"] != "active" {
		t.Fatalf("expected role active, got %#v", profileRepo.upsertedFields["role"])
	}
	if profileRepo.upsertedFields["version"] != "2.5.5" {
		t.Fatalf("expected version 2.5.5, got %#v", profileRepo.upsertedFields["version"])
	}

	err = svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"technologySubtype": "proxysql",
		"host":              "proxy-prod-01",
		"port":              6033,
		"role":              "primary",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for non active/standby role, got %v", err)
	}
	if ve.Fields["role"] == "" {
		t.Fatalf("expected field-level detail for role, got %#v", ve.Fields)
	}
}

func TestProfileServicePutProfileControlPlaneComponentRejectsAmbiguousHA(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeControlPlaneComponent),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"componentSubtype": "ha_monitor",
		"endpoint":         "http://ha-monitor:10008",
		"role":             "standby",
		"version":          "3.2.1",
	})
	if err != nil {
		t.Fatalf("expected control_plane_component profile to be accepted, got %v", err)
	}
	if profileRepo.upsertedType != "control_plane_component" {
		t.Fatalf("expected upsertedType control_plane_component, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["componentSubtype"] != "ha_monitor" {
		t.Fatalf("expected componentSubtype ha_monitor, got %#v", profileRepo.upsertedFields["componentSubtype"])
	}
	if profileRepo.upsertedFields["endpoint"] != "http://ha-monitor:10008" {
		t.Fatalf("expected endpoint http://ha-monitor:10008, got %#v", profileRepo.upsertedFields["endpoint"])
	}
	if profileRepo.upsertedFields["role"] != "standby" {
		t.Fatalf("expected role standby, got %#v", profileRepo.upsertedFields["role"])
	}

	err = svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"componentSubtype": "ha",
		"endpoint":         "http://ha-monitor:10008",
		"role":             "active",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for ambiguous ha componentSubtype, got %v", err)
	}
	if ve.Fields["componentSubtype"] == "" {
		t.Fatalf("expected field-level detail for componentSubtype, got %#v", ve.Fields)
	}
	if profileRepo.upsertedType != "control_plane_component" {
		t.Fatalf("expected no additional upsert after ambiguous ha, got %q", profileRepo.upsertedType)
	}
}

func TestProfileServicePutProfileUnsupportedType(t *testing.T) {
	unsupportedTypes := []model.ResourceType{
		model.ResourceType("unsupported"),
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
	if v, ok := profileRepo.patchedFields["hostname"]; !ok || v != "web-02" {
		t.Fatalf("expected hostname web-02 delegated for patching, got %#v", profileRepo.patchedFields)
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
			testResource1ID: makeActiveResource(model.ResourceType("unsupported")),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{})
	if !errors.Is(err, ErrProfileNotSupported) {
		t.Fatalf("expected ErrProfileNotSupported, got %v", err)
	}
}

func TestProfileServicePutProfileDomainNameNormalizesFQDN(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDomainName),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"fqdn": "Example.COM.",
	})
	if err != nil {
		t.Fatalf("expected normalized FQDN to be accepted, got %v", err)
	}
	if profileRepo.upsertedType != "domain_name" {
		t.Fatalf("expected upsertedType domain_name, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["fqdn"] != "example.com" {
		t.Fatalf("expected normalized fqdn example.com, got %#v", profileRepo.upsertedFields["fqdn"])
	}
}

func TestProfileServicePutProfileDomainNameRejectsMissingFQDN(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDomainName),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for missing fqdn, got %v", err)
	}
	if ve.Fields["fqdn"] == "" {
		t.Fatalf("expected field-level detail for fqdn, got %#v", ve.Fields)
	}
	if profileRepo.upsertedType != "" {
		t.Fatalf("expected no upsert after missing fqdn, got %q", profileRepo.upsertedType)
	}
}

func TestProfileServicePutProfileDomainNameRejectsResolutionTarget(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDomainName),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"fqdn":             "orders.example.com",
		"resolutionTarget": "10.0.0.10",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for resolution target profile text, got %v", err)
	}
	if ve.Fields["resolutionTarget"] == "" {
		t.Fatalf("expected field-level detail for resolutionTarget, got %#v", ve.Fields)
	}
	if profileRepo.upsertedType != "" {
		t.Fatalf("expected no upsert when resolution target is submitted as profile text, got %q", profileRepo.upsertedType)
	}
}

func TestProfileServicePutProfileVirtualIPAcceptsSingleAddress(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeVirtualIP),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"ipAddress": "2001:db8::1",
	})
	if err != nil {
		t.Fatalf("expected single IPv6 address to be accepted, got %v", err)
	}
	if profileRepo.upsertedType != "virtual_ip" {
		t.Fatalf("expected upsertedType virtual_ip, got %q", profileRepo.upsertedType)
	}
	if profileRepo.upsertedFields["ipAddress"] != "2001:db8::1" {
		t.Fatalf("expected ipAddress 2001:db8::1, got %#v", profileRepo.upsertedFields["ipAddress"])
	}
}

func TestProfileServicePutProfileVirtualIPRejectsNonSingleAddress(t *testing.T) {
	invalid := []string{"10.0.0.1/24", "10.0.0.1:80", "10.0.0.1,10.0.0.2", ""}
	for _, ip := range invalid {
		profileRepo := &fakeProfileRepo{}
		resourceRepo := &fakeResourceWriteRepo{
			resources: map[uint64]model.Resource{
				testResource1ID: makeActiveResource(model.ResourceTypeVirtualIP),
			},
		}
		svc := NewProfileService(profileRepo, resourceRepo)

		fields := map[string]interface{}{}
		if ip != "" {
			fields["ipAddress"] = ip
		}
		err := svc.PutProfile(context.Background(), testResource1ID, fields)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("ipAddress %q: expected ValidationError, got %v", ip, err)
		}
		if ve.Fields["ipAddress"] == "" {
			t.Fatalf("ipAddress %q: expected field-level detail, got %#v", ip, ve.Fields)
		}
		if profileRepo.upsertedType != "" {
			t.Fatalf("ipAddress %q: expected no upsert, got %q", ip, profileRepo.upsertedType)
		}
	}
}

func TestProfileServicePatchProfileDomainNameNormalizesSubmittedFQDN(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDomainName),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{
		"fqdn": "Orders.Example.COM.",
	})
	if err != nil {
		t.Fatalf("expected patched FQDN to be accepted, got %v", err)
	}
	if profileRepo.patchedFields["fqdn"] != "orders.example.com" {
		t.Fatalf("expected normalized patched fqdn orders.example.com, got %#v", profileRepo.patchedFields)
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

// ---------- PATCH: partial semantics ----------

func TestProfileServicePatchProfileDelegatesSubmittedFields(t *testing.T) {
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
	if len(profileRepo.patchedFields) != 1 || profileRepo.patchedFields["hostname"] != "web-02" {
		t.Fatalf("expected only submitted fields delegated to the repository, got %#v", profileRepo.patchedFields)
	}
}

func TestProfileServicePatchProfileEmptyBodyIsNoOp(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedType != "" || profileRepo.patchedFields != nil {
		t.Fatalf("expected empty PATCH to be a no-op with no profile write, got upsertedType %q patchedFields %#v", profileRepo.upsertedType, profileRepo.patchedFields)
	}
}

func TestProfileServicePatchProfileAllowsExplicitEmptyValue(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "",
	})
	if err != nil {
		t.Fatalf("expected explicit empty string to be allowed, got %v", err)
	}
	if v, ok := profileRepo.patchedFields["hostname"]; !ok || v != "" {
		t.Fatalf("expected explicit empty hostname delegated, got %#v", profileRepo.patchedFields)
	}
}

func TestProfileServicePatchProfileRejectsInvalidFieldBeforeWrite(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PatchProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "x",
		"bogus":    "y",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for unknown field, got %v", err)
	}
	if profileRepo.patchedFields != nil {
		t.Fatalf("expected no repository write after validation failure, got %#v", profileRepo.patchedFields)
	}
}

// ---------- Profile field validation (PUT and PATCH) ----------

func TestProfileServicePutProfileRejectsUnknownField(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "web-01",
		"bogus":    "x",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for unknown field, got %v", err)
	}
	if ve.Fields["bogus"] == "" {
		t.Fatalf("expected field-level detail for bogus, got %#v", ve.Fields)
	}
}

func TestProfileServicePutProfileRejectsNonStringValue(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": 42,
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for non-string value, got %v", err)
	}
	if ve.Fields["hostname"] == "" {
		t.Fatalf("expected field-level detail for hostname, got %#v", ve.Fields)
	}
}

func TestProfileServicePutProfileRejectsFractionalPort(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseInstance),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"port": 3306.5,
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for fractional port, got %v", err)
	}
	if ve.Fields["port"] == "" {
		t.Fatalf("expected field-level detail for port, got %#v", ve.Fields)
	}
}

func TestProfileServicePutProfileRejectsPortOutOfRange(t *testing.T) {
	for _, port := range []interface{}{0, 65536} {
		profileRepo := &fakeProfileRepo{}
		resourceRepo := &fakeResourceWriteRepo{
			resources: map[uint64]model.Resource{
				testResource1ID: makeActiveResource(model.ResourceTypeDatabaseInstance),
			},
		}
		svc := NewProfileService(profileRepo, resourceRepo)

		err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{"port": port})
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("port %v: expected ValidationError, got %v", port, err)
		}
		if ve.Fields["port"] == "" {
			t.Fatalf("port %v: expected field-level detail for port, got %#v", port, ve.Fields)
		}
	}
}

func TestProfileServicePutProfileRejectsOverlongString(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": strings.Repeat("h", 256),
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for overlong hostname, got %v", err)
	}
	if ve.Fields["hostname"] == "" {
		t.Fatalf("expected field-level detail for hostname, got %#v", ve.Fields)
	}
}

func TestProfileServicePutProfileAcceptsExplicitEmptyStrings(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeHost),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"hostname": "",
	})
	if err != nil {
		t.Fatalf("expected explicit empty string to be allowed, got %v", err)
	}
}

func TestProfileServicePutProfileInt64PortPersisted(t *testing.T) {
	profileRepo := &fakeProfileRepo{}
	resourceRepo := &fakeResourceWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: makeActiveResource(model.ResourceTypeDatabaseInstance),
		},
	}
	svc := NewProfileService(profileRepo, resourceRepo)

	err := svc.PutProfile(context.Background(), testResource1ID, map[string]interface{}{
		"port": int64(3306),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if profileRepo.upsertedFields["port"] != 3306 {
		t.Fatalf("expected int64 port 3306 persisted as 3306, got %v", profileRepo.upsertedFields["port"])
	}
}
