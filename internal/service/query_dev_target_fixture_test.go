// Package service validates the disposable query target fixture service.
// input: fake dictionaries/resource store and query fixture configuration
// output: validation, identity ownership, idempotency, and fail-closed unit tests
// pos: unit contract coverage for query dev target orchestration
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// --- fakes ---

type fakeFixtureDictionary struct {
	envs   []model.Environment
	owners []model.Owner
	envErr error
	ownErr error
}

func (f *fakeFixtureDictionary) ListEnvironments() ([]model.Environment, error) {
	return f.envs, f.envErr
}
func (f *fakeFixtureDictionary) ListOwners() ([]model.Owner, error) {
	return f.owners, f.ownErr
}

type fixtureUpsertArgs struct {
	resourceID                  uint64
	engine, version, host, role string
	port                        int
}

type fakeFixtureResourceStore struct {
	listFn   func() ([]model.Resource, error)
	createFn func(model.ResourceCreateInput) (*model.Resource, error)
	upsertFn func(uint64, string, string, string, int, string) error

	createCalls []model.ResourceCreateInput
	upsertCalls []fixtureUpsertArgs
}

func (f *fakeFixtureResourceStore) ListResources(_ context.Context, _ model.ResourceListQuery) ([]model.Resource, int, error) {
	res, err := f.listFn()
	if err != nil {
		return nil, 0, err
	}
	return res, len(res), nil
}
func (f *fakeFixtureResourceStore) CreateResource(_ context.Context, in model.ResourceCreateInput) (*model.Resource, error) {
	f.createCalls = append(f.createCalls, in)
	return f.createFn(in)
}
func (f *fakeFixtureResourceStore) UpsertDatabaseInstanceProfile(_ context.Context, id uint64, engine, version, host string, port int, role string) error {
	f.upsertCalls = append(f.upsertCalls, fixtureUpsertArgs{id, engine, version, host, role, port})
	return f.upsertFn(id, engine, version, host, port, role)
}

func validFixtureCfg() QueryDevTargetFixtureConfig {
	return QueryDevTargetFixtureConfig{
		EnvironmentSlug: "dev",
		OwnerEmail:      "dba@example.com",
		ResourceName:    "local-mysql-query-dev",
		DisplayName:     "Local MySQL Query Dev",
		Engine:          "mysql",
		Version:         "8.0",
		Role:            "primary",
		Host:            "127.0.0.1",
		Port:            3306,
	}
}

func fixtureSvc(dict *fakeFixtureDictionary, store *fakeFixtureResourceStore) *QueryDevTargetFixture {
	return NewQueryDevTargetFixture(dict, store)
}

// --- config validation ---

func TestQueryDevTargetFixtureConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*QueryDevTargetFixtureConfig)
		wantErr error
	}{
		{"valid", func(*QueryDevTargetFixtureConfig) {}, nil},
		{"missing env slug", func(c *QueryDevTargetFixtureConfig) { c.EnvironmentSlug = "" }, errFixtureMissingEnvSlug},
		{"missing owner email", func(c *QueryDevTargetFixtureConfig) { c.OwnerEmail = "" }, errFixtureMissingOwnerEmail},
		{"missing resource name", func(c *QueryDevTargetFixtureConfig) { c.ResourceName = "" }, errFixtureMissingResourceName},
		{"missing host", func(c *QueryDevTargetFixtureConfig) { c.Host = "" }, errFixtureInvalidHostPort},
		{"zero port", func(c *QueryDevTargetFixtureConfig) { c.Port = 0 }, errFixtureInvalidHostPort},
		{"negative port", func(c *QueryDevTargetFixtureConfig) { c.Port = -1 }, errFixtureInvalidHostPort},
		{"unsupported engine", func(c *QueryDevTargetFixtureConfig) { c.Engine = "postgres" }, errFixtureUnsupportedEngine},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validFixtureCfg()
			tc.mutate(&c)
			err := c.validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// --- DSN host:port parser ---

func TestParseMySQLDSNHostPort_Valid(t *testing.T) {
	host, port, err := ParseMySQLDSNHostPort("user:secret@tcp(127.0.0.1:3306)/controlhub")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if host != "127.0.0.1" || port != 3306 {
		t.Fatalf("got host=%q port=%d", host, port)
	}
}

func TestParseMySQLDSNHostPort_Rejects(t *testing.T) {
	cases := []string{
		"",                              // empty
		"user:secret@tcp(127.0.0.1)/db", // portless raw address
		"not a dsn at all",              // unparseable
	}
	for _, dsn := range cases {
		_, _, err := ParseMySQLDSNHostPort(dsn)
		if err == nil {
			t.Fatalf("expected error for %q", dsn)
		}
		// DSN/password must never appear in the error.
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "tcp(") || strings.Contains(err.Error(), "@") {
			t.Fatalf("error leaks DSN fragment: %q", err.Error())
		}
	}
}

// --- EnsureLocalQueryTarget ---

func TestEnsureLocalQueryTarget_ReusesExistingTarget(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	existing := []model.Resource{
		{ID: 42, Name: "local-mysql-query-dev", ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: 7, ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: devFixtureExternalSystem, Value: "local-mysql-query-dev"}}},
		{ID: 99, Name: "unrelated-target", ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: 7},
	}
	created := false
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) { return existing, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) {
			created = true
			return &model.Resource{ID: 1}, nil
		},
		upsertFn: func(uint64, string, string, string, int, string) error { return nil },
	}
	id, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42 (existing)", id)
	}
	if created {
		t.Fatal("CreateResource must not be called when an existing target is reused")
	}
	if len(store.upsertCalls) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(store.upsertCalls))
	}
}

func TestEnsureLocalQueryTarget_CreatesWhenMissing(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) { return nil, nil },
		createFn: func(in model.ResourceCreateInput) (*model.Resource, error) {
			return &model.Resource{ID: 55, Name: in.Name, ResourceType: in.ResourceType, Origin: in.Origin, ExternalIdentifiers: in.ExternalIdentifiers}, nil
		},
		upsertFn: func(uint64, string, string, string, int, string) error { return nil },
	}
	id, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != 55 {
		t.Fatalf("id = %d, want 55 (created)", id)
	}
	if len(store.createCalls) != 1 {
		t.Fatalf("create calls = %d, want 1", len(store.createCalls))
	}
	in := store.createCalls[0]
	if in.Origin != model.ResourceOriginImported {
		t.Fatalf("origin = %q, want imported", in.Origin)
	}
	if len(in.ExternalIdentifiers) != 1 || in.ExternalIdentifiers[0].System != devFixtureExternalSystem || in.ExternalIdentifiers[0].Value != in.Name {
		t.Fatalf("external identifiers = %#v, want fixture marker", in.ExternalIdentifiers)
	}
	if in.ResourceSubtype != "mysql" {
		t.Fatalf("subtype = %q, want mysql", in.ResourceSubtype)
	}
	if in.EnvironmentID != 7 || in.OwnerID != 9 {
		t.Fatalf("env/owner = %d/%d, want 7/9", in.EnvironmentID, in.OwnerID)
	}
}

func TestEnsureLocalQueryTarget_CreateConflictThenRefetch(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	calls := 0
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) {
			calls++
			if calls == 1 {
				return nil, nil // not present yet
			}
			return []model.Resource{{ID: 77, Name: "local-mysql-query-dev", ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: 7, ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: devFixtureExternalSystem, Value: "local-mysql-query-dev"}}}}, nil
		},
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) { return nil, ErrResourceConflict },
		upsertFn: func(uint64, string, string, string, int, string) error { return nil },
	}
	id, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != 77 {
		t.Fatalf("id = %d, want 77 (refetched after conflict)", id)
	}
	if len(store.createCalls) != 1 {
		t.Fatalf("create calls = %d, want 1 (single create attempt)", len(store.createCalls))
	}
}

func TestEnsureLocalQueryTarget_ExistingNonFixtureSameNameRejectsWithoutProfileUpsert(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	// A real, non-fixture resource with the same name/env/type already exists.
	existing := []model.Resource{
		{ID: 42, Name: "local-mysql-query-dev", ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: 7, Source: "manual"},
	}
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) { return existing, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) {
			t.Fatal("CreateResource must not be called for a non-fixture same-name resource")
			return nil, nil
		},
		upsertFn: func(uint64, string, string, string, int, string) error {
			t.Fatal("UpsertDatabaseInstanceProfile must not be called for a non-fixture resource")
			return nil
		},
	}
	_, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if !errors.Is(err, errFixtureExistingResourceNotFixture) {
		t.Fatalf("err = %v, want errFixtureExistingResourceNotFixture", err)
	}
	if strings.Contains(err.Error(), "tcp(") || strings.Contains(err.Error(), "@") || strings.Contains(err.Error(), "://") {
		t.Fatalf("error leaks a DSN fragment: %q", err.Error())
	}
}

func TestEnsureLocalQueryTarget_CreateConflictThenRefetchNonFixtureRejects(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	calls := 0
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) {
			calls++
			if calls == 1 {
				return nil, nil // not present yet
			}
			// The conflict was caused by a real, non-fixture same-name resource.
			return []model.Resource{{ID: 77, Name: "local-mysql-query-dev", ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: 7, Source: "manual"}}, nil
		},
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) { return nil, ErrResourceConflict },
		upsertFn: func(uint64, string, string, string, int, string) error {
			t.Fatal("UpsertDatabaseInstanceProfile must not be called when the conflict is a non-fixture resource")
			return nil
		},
	}
	_, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if !errors.Is(err, errFixtureExistingResourceNotFixture) {
		t.Fatalf("err = %v, want errFixtureExistingResourceNotFixture", err)
	}
}

func TestEnsureLocalQueryTarget_EnvironmentSlugNotFound_Rejects(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "prod"}}, // no dev
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) { t.Fatal("ListResources must not be called"); return nil, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) {
			t.Fatal("CreateResource must not be called")
			return nil, nil
		},
		upsertFn: func(uint64, string, string, string, int, string) error {
			t.Fatal("Upsert must not be called")
			return nil
		},
	}
	_, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if !errors.Is(err, errFixtureEnvSlugNotFound) {
		t.Fatalf("err = %v, want errFixtureEnvSlugNotFound", err)
	}
}

func TestEnsureLocalQueryTarget_OwnerEmailNotFound_Rejects(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "sre@example.com"}}, // no dba
	}
	store := &fakeFixtureResourceStore{
		listFn: func() ([]model.Resource, error) { t.Fatal("ListResources must not be called"); return nil, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) {
			t.Fatal("CreateResource must not be called")
			return nil, nil
		},
		upsertFn: func(uint64, string, string, string, int, string) error {
			t.Fatal("Upsert must not be called")
			return nil
		},
	}
	_, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if !errors.Is(err, errFixtureOwnerEmailNotFound) {
		t.Fatalf("err = %v, want errFixtureOwnerEmailNotFound", err)
	}
}

func TestEnsureLocalQueryTarget_ProfileUpsertUsesHostPortOnly(t *testing.T) {
	dict := &fakeFixtureDictionary{
		envs:   []model.Environment{{ID: 7, Slug: "dev"}},
		owners: []model.Owner{{ID: 9, Email: "dba@example.com"}},
	}
	store := &fakeFixtureResourceStore{
		listFn:   func() ([]model.Resource, error) { return nil, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) { return &model.Resource{ID: 5}, nil },
		upsertFn: func(uint64, string, string, string, int, string) error { return nil },
	}
	cfg := validFixtureCfg()
	if _, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), cfg); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(store.upsertCalls) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(store.upsertCalls))
	}
	u := store.upsertCalls[0]
	if u.host != "127.0.0.1" || u.port != 3306 || u.engine != "mysql" || u.version != "8.0" || u.role != "primary" || u.resourceID != 5 {
		t.Fatalf("upsert args = %+v", u)
	}
	// No upsert arg may carry a DSN fragment (the fixture must forward host/port only).
	for _, v := range []string{u.engine, u.version, u.host, u.role} {
		if strings.Contains(v, "tcp(") || strings.Contains(v, "@") || strings.Contains(v, "://") {
			t.Fatalf("upsert arg leaks a DSN fragment: %q", v)
		}
	}
}

func TestEnsureLocalQueryTarget_NoDSNInError(t *testing.T) {
	dict := &fakeFixtureDictionary{envs: []model.Environment{{ID: 7, Slug: "prod"}}} // env not found
	store := &fakeFixtureResourceStore{
		listFn:   func() ([]model.Resource, error) { return nil, nil },
		createFn: func(model.ResourceCreateInput) (*model.Resource, error) { return nil, nil },
		upsertFn: func(uint64, string, string, string, int, string) error { return nil },
	}
	_, err := fixtureSvc(dict, store).EnsureLocalQueryTarget(context.Background(), validFixtureCfg())
	if err == nil {
		t.Fatal("expected error")
	}
	// The fixture never sees the DSN, so no error path may leak one.
	if strings.Contains(err.Error(), "tcp(") || strings.Contains(err.Error(), "@") || strings.Contains(err.Error(), "://") {
		t.Fatalf("error leaks a DSN fragment: %q", err.Error())
	}
}
