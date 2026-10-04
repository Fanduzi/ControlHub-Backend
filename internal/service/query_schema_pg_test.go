// Package service proves PostgreSQL schema access without opening a server.
// input: context, database/sql, errors, strings, sync, testing, time, jackc/pgx/v5, jackc/pgx/v5/pgconn, internal/model
// output: TestPostgreSQLSchema_* for connection identity, effective-schema cache identity, singleflight boundaries, and unsupported definitions
// pos: Unit boundary for T6 metadata access; user SQL execution stays outside this file
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
)

const (
	pgUnitPassA = "pw-do-not-log-t6-a"
	pgUnitPassB = "pw-do-not-log-t6-b"
)

func assertNoUnitSecret(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, pgUnitPassA) || strings.Contains(text, pgUnitPassB) {
		t.Fatal("text contains credential material")
	}
}

type pgCredStore struct {
	*fakeExecRepo
	rows    map[string]model.QueryCredentialMetadata
	lookups []string
}

func (s *pgCredStore) GetCredential(_ context.Context, resourceID uint64, databaseName string) (model.QueryCredentialMetadata, error) {
	s.lookups = append(s.lookups, databaseName)
	row, ok := s.rows[fmt.Sprintf("%d\x00%s", resourceID, databaseName)]
	if !ok {
		return model.QueryCredentialMetadata{}, sql.ErrNoRows
	}
	return row, nil
}

type pgRefResolver struct {
	dsns  map[string]string
	calls []string
}

func (r *pgRefResolver) Resolve(_ context.Context, ref string) (string, error) {
	r.calls = append(r.calls, ref)
	if err := model.ValidateCredentialRef(ref); err != nil {
		return "", err
	}
	dsn, ok := r.dsns[ref]
	if !ok || dsn == "" {
		return "", errors.New("credential missing")
	}
	return dsn, nil
}

func pgUnitDSN(database, user, password string) string {
	return fmt.Sprintf(
		"host=db.internal port=5432 dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t6-unit",
		database, user, password,
	)
}

func pgTarget(environment string) model.QueryTarget {
	return model.QueryTarget{
		ResourceID: 9001,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: environment,
			Engine:      "postgresql",
			Host:        "db.internal",
			Port:        5432,
		},
	}
}

func pgRow(database, schema, ref string, enabled bool, policy model.QueryEnvironmentPolicy) model.QueryCredentialMetadata {
	return model.QueryCredentialMetadata{
		ResourceID:        9001,
		DatabaseName:      database,
		DefaultSchema:     schema,
		Engine:            "postgresql",
		CredentialRef:     ref,
		Enabled:           enabled,
		EnvironmentPolicy: policy,
	}
}

type pgCatCall struct {
	method   string
	database string
	schema   string
	fallback string
	name     string
	cfgDB    string
	cfgUser  string
	passA    bool
	passB    bool
}

type fakePGCatalog struct {
	mu       sync.Mutex
	calls    []pgCatCall
	schemas  []model.SchemaSummary
	objects  []ObjectSummary
	probeErr error
	hold     *arriveGate
}

func (f *fakePGCatalog) record(method, database, schema, fallback, name string, cfg *pgx.ConnConfig) {
	call := pgCatCall{method: method, database: database, schema: schema, fallback: fallback, name: name}
	if cfg != nil {
		call.cfgDB = cfg.Database
		call.cfgUser = cfg.User
		call.passA = cfg.Password == pgUnitPassA
		call.passB = cfg.Password == pgUnitPassB
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakePGCatalog) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// pin mirrors the service's effective schema for the fake catalog. missing and
// secret are distinct failures so a stale cache cannot satisfy either one.
func (f *fakePGCatalog) pin(schema, fallback string) (string, error) {
	name := schema
	if name == "" {
		name = fallback
	}
	switch name {
	case "":
		return "", ErrSchemaValidationFailed
	case "missing":
		return "", ErrSchemaNotFound
	case "secret":
		return "", ErrSchemaNotUsable
	default:
		return name, nil
	}
}

func (f *fakePGCatalog) ListFixedDatabase(_ context.Context, cfg *pgx.ConnConfig, database, q string, page, pageSize int) ([]model.DatabaseSummary, model.PageInfo, error) {
	f.record("databases", database, "", "", "", cfg)
	items, info := pageFixedDatabase(database, q, page, pageSize)
	return items, info, nil
}

func (f *fakePGCatalog) ListSchemas(_ context.Context, cfg *pgx.ConnConfig, database, _ string, page, pageSize int) ([]model.SchemaSummary, model.PageInfo, error) {
	f.record("schemas", database, "", "", "", cfg)
	items := f.schemas
	if items == nil {
		items = []model.SchemaSummary{}
	}
	return items, model.NewPageInfo(page, pageSize, len(items)), nil
}

func (f *fakePGCatalog) ListObjects(_ context.Context, cfg *pgx.ConnConfig, database, schema, fallback, _, _ string, _, pageSize int) (string, []ObjectSummary, model.PageInfo, error) {
	f.record("objects", database, schema, fallback, "", cfg)
	pinned, err := f.pin(schema, fallback)
	if err != nil {
		return "", nil, model.PageInfo{}, err
	}
	items := f.objects
	if items == nil {
		items = []ObjectSummary{}
	}
	return pinned, items, model.NewPageInfo(1, pageSize, len(items)), nil
}

func (f *fakePGCatalog) GetObjectDetails(_ context.Context, cfg *pgx.ConnConfig, database, schema, fallback, name, kind string) (model.ObjectDetailResponse, error) {
	f.record("details", database, schema, fallback, name, cfg)
	if f.hold != nil {
		f.hold.arrive()
	}
	pinned, err := f.pin(schema, fallback)
	if err != nil {
		return model.ObjectDetailResponse{}, err
	}
	return model.ObjectDetailResponse{
		Database: database,
		Schema:   pinned,
		Name:     name,
		Kind:     model.ObjectKind(kind),
	}, nil
}

func (f *fakePGCatalog) GetRelationshipMap(_ context.Context, cfg *pgx.ConnConfig, database, schema, fallback, name string) (model.RelationshipMapResponse, error) {
	f.record("relationship", database, schema, fallback, name, cfg)
	pinned, err := f.pin(schema, fallback)
	if err != nil {
		return model.RelationshipMapResponse{}, err
	}
	return model.RelationshipMapResponse{
		Root: model.RelationshipMapNode{Schema: pinned, Name: name},
	}, nil
}

func (f *fakePGCatalog) ProbeDefinition(_ context.Context, cfg *pgx.ConnConfig, database, schema, fallback string) error {
	f.record("probe", database, schema, fallback, "", cfg)
	if f.probeErr != nil {
		return f.probeErr
	}
	_, err := f.pin(schema, fallback)
	return err
}

// arriveGate blocks each arrival until release is closed. A collapsed
// singleflight call never reaches the second arrival.
type arriveGate struct {
	need    int
	arrived chan struct{}
	release chan struct{}
	mu      sync.Mutex
	n       int
}

func newArriveGate(need int) *arriveGate {
	return &arriveGate{
		need:    need,
		arrived: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *arriveGate) arrive() {
	g.mu.Lock()
	g.n++
	if g.n == g.need {
		close(g.arrived)
	}
	g.mu.Unlock()
	<-g.release
}

// lockingPGAudit serializes the shared test audit slice for concurrent schema calls.
type lockingPGAudit struct {
	mu sync.Mutex
	*pgCredStore
}

func (a *lockingPGAudit) InsertAuditEvent(ctx context.Context, actor, target uint64, etype, result string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pgCredStore.InsertAuditEvent(ctx, actor, target, etype, result)
}

type pgUnitEnv struct {
	svc      *QuerySchemaService
	store    *pgCredStore
	resolver *pgRefResolver
	cat      *fakePGCatalog
	insp     *fakeSchemaInspector
}

func newPGUnit(t *testing.T, target model.QueryTarget, rows map[string]model.QueryCredentialMetadata, dsns map[string]string) pgUnitEnv {
	t.Helper()
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: rows}
	resolver := &pgRefResolver{dsns: dsns}
	inspector := &fakeSchemaInspector{}
	cat := &fakePGCatalog{schemas: []model.SchemaSummary{{Name: "app"}}, objects: []ObjectSummary{{Name: "orders", Kind: "table"}}}
	clock := &fakeClock{t: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	svc := NewQuerySchemaService(
		NewTargetAccessResolver(fakeTargetRepo{targets: []model.QueryTarget{target}}, store, resolver),
		inspector,
		NewQuerySchemaCache(100, clock),
		store,
		clock,
	)
	svc.pgCatalog = cat
	return pgUnitEnv{svc: svc, store: store, resolver: resolver, cat: cat, insp: inspector}
}

func pgRows() map[string]model.QueryCredentialMetadata {
	return map[string]model.QueryCredentialMetadata{
		"9001\x00db_a":     pgRow("db_a", "app", "PG_T6_A", true, model.QueryEnvPolicyNonProdOnly),
		"9001\x00db_b":     pgRow("db_b", "app", "PG_T6_B", true, model.QueryEnvPolicyNonProdOnly),
		"9001\x00db_off":   pgRow("db_off", "app", "PG_T6_OFF", false, model.QueryEnvPolicyNonProdOnly),
		"9001\x00db_wrong": pgRow("db_wrong", "app", "PG_T6_BAD", true, model.QueryEnvPolicyNonProdOnly),
	}
}

func pgDSNs() map[string]string {
	return map[string]string{
		"PG_T6_A":   pgUnitDSN("db_a", "ro_a", pgUnitPassA),
		"PG_T6_B":   pgUnitDSN("db_b", "ro_b", pgUnitPassB),
		"PG_T6_BAD": pgUnitDSN("db_a", "ro_a", pgUnitPassA),
	}
}

func TestPostgreSQLSchema_ConnectionsDoNotShareIdentity(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	ctx := context.Background()

	a, err := env.svc.ListSchemas(ctx, 1, 9001, "db_a", "", 1, 20, false)
	if err != nil {
		assertNoUnitSecret(t, err.Error())
		t.Fatalf("db_a: %v", err)
	}
	b, err := env.svc.ListSchemas(ctx, 1, 9001, "db_b", "", 1, 20, false)
	if err != nil {
		assertNoUnitSecret(t, err.Error())
		t.Fatalf("db_b: %v", err)
	}
	if a.Database != "db_a" || b.Database != "db_b" {
		t.Fatalf("databases = %s %s", a.Database, b.Database)
	}
	if len(env.cat.calls) != 2 || env.cat.calls[0].cfgDB != "db_a" || env.cat.calls[1].cfgDB != "db_b" {
		t.Fatalf("catalog calls = %+v", env.cat.calls)
	}
	if !env.cat.calls[0].passA || env.cat.calls[0].passB || env.cat.calls[0].cfgUser != "ro_a" {
		t.Fatal("db_a connection did not keep its own credential")
	}
	if !env.cat.calls[1].passB || env.cat.calls[1].passA || env.cat.calls[1].cfgUser != "ro_b" {
		t.Fatal("db_b connection did not keep its own credential")
	}
	if env.store.lookups[0] != "db_a" || env.store.lookups[1] != "db_b" {
		t.Fatalf("lookups = %v", env.store.lookups)
	}
}

func TestPostgreSQLSchema_DisabledAndMissingStopBeforeConnect(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	ctx := context.Background()

	_, err := env.svc.ListSchemas(ctx, 1, 9001, "db_off", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaConnectionDisabled) {
		t.Fatalf("disabled = %v", err)
	}
	_, err = env.svc.ListSchemas(ctx, 1, 9001, "db_missing", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaConnectionNotFound) {
		t.Fatalf("missing = %v", err)
	}
	before := len(env.store.lookups)
	_, err = env.svc.ListSchemas(ctx, 1, 9001, "", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("empty database = %v", err)
	}
	if len(env.store.lookups) != before {
		t.Fatal("empty database looked up a credential")
	}
	if len(env.resolver.calls) != 0 || len(env.cat.calls) != 0 {
		t.Fatal("denied connections reached the resolver or catalog")
	}
}

func TestPostgreSQLSchema_BindingMismatchIsBare(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	_, err := env.svc.ListSchemas(context.Background(), 1, 9001, "db_wrong", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaBindingMismatch) {
		t.Fatalf("mismatch = %v", err)
	}
	if err.Error() != ErrSchemaBindingMismatch.Error() {
		t.Fatalf("mismatch text = %q", err.Error())
	}
	assertNoUnitSecret(t, err.Error())
	if len(env.cat.calls) != 0 {
		t.Fatal("binding mismatch opened a catalog")
	}
}

func TestPostgreSQLSchema_ProductionPolicyBlocksBeforeSecret(t *testing.T) {
	t.Parallel()
	rows := pgRows()
	rows["9001\x00db_a"] = pgRow("db_a", "app", "PG_T6_A", true, model.QueryEnvPolicyNonProdOnly)
	env := newPGUnit(t, pgTarget("Production"), rows, pgDSNs())
	access := NewTargetAccessResolver(fakeTargetRepo{targets: []model.QueryTarget{pgTarget("Production")}}, env.store, env.resolver)
	_, err := access.ResolveMetadata(context.Background(), 1, 9001, "db_a")
	var denied *TargetAccessError
	if !errors.As(err, &denied) || denied.Status != model.QueryCredentialRuntimePolicyBlocked {
		t.Fatalf("policy = %v", err)
	}
	if len(env.resolver.calls) != 0 {
		t.Fatal("production policy resolved a secret")
	}
	_, err = env.svc.ListSchemas(context.Background(), 1, 9001, "db_a", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaNotAllowed) {
		t.Fatalf("service = %v", err)
	}
	assertNoUnitSecret(t, err.Error())
}

func TestPostgreSQLSchema_MySQLIgnoresSelectorAndRejectsSchema(t *testing.T) {
	t.Parallel()
	store := &pgCredStore{fakeExecRepo: &fakeExecRepo{}, rows: map[string]model.QueryCredentialMetadata{
		"9001\x00": enabledCred(model.QueryEnvPolicyNonProdOnly),
	}}
	resolver := &pgRefResolver{dsns: map[string]string{"ORDER_MYSQL_RO": testResolverDSN}}
	inspector := &fakeSchemaInspector{databases: []DatabaseSummary{{Name: "orders"}}, objects: []ObjectSummary{{Name: "users", Kind: "table"}}}
	clock := &fakeClock{t: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	svc := NewQuerySchemaService(
		NewTargetAccessResolver(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}}, store, resolver),
		inspector,
		NewQuerySchemaCache(100, clock),
		store,
		clock,
	)
	ctx := context.Background()
	if _, err := svc.ListDatabases(ctx, 1, 9001, "db_a", "", 1, 20, false, false); err != nil {
		t.Fatalf("mysql databases: %v", err)
	}
	if len(store.lookups) != 1 || store.lookups[0] != "" {
		t.Fatalf("mysql lookup key = %q", store.lookups)
	}
	if !inspector.called {
		t.Fatal("mysql inspector was not called")
	}
	inspector.called = false
	_, err := svc.ListSchemas(ctx, 1, 9001, "db_a", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("mysql schemas = %v", err)
	}
	_, err = svc.ListObjects(ctx, 1, 9001, "orders", "public", "", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaValidationFailed) || inspector.called {
		t.Fatalf("mysql objects schema err=%v called=%v", err, inspector.called)
	}
	target := mysqlTarget("Staging")
	target.ConnectionContext.Engine = "tidb"
	store.rows["9001\x00"] = model.QueryCredentialMetadata{
		ResourceID: 9001, Enabled: true, Engine: "tidb", CredentialRef: "ORDER_MYSQL_RO", EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	svc = NewQuerySchemaService(
		NewTargetAccessResolver(fakeTargetRepo{targets: []model.QueryTarget{target}}, store, resolver),
		inspector, NewQuerySchemaCache(100, clock), store, clock,
	)
	_, err = svc.GetRelationshipMap(ctx, 1, 9001, "orders", "public", "users", false)
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("tidb schema = %v, want validation", err)
	}
	_, err = svc.GetRelationshipMap(ctx, 1, 9001, "orders", "", "users", false)
	if !errors.Is(err, ErrSchemaRelationshipNotSupported) {
		t.Fatalf("tidb map = %v, want unsupported", err)
	}
}

func TestPostgreSQLSchema_PinAndCacheKeepSchemaIdentity(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	ctx := context.Background()
	resp, err := env.svc.ListObjects(ctx, 1, 9001, "db_a", "", "", "", 1, 20, false)
	if err != nil {
		t.Fatalf("default schema: %v", err)
	}
	if resp.Schema != "app" || env.cat.calls[0].schema != "app" || env.cat.calls[0].fallback != "app" {
		t.Fatalf("default pin resp=%q call=%+v", resp.Schema, env.cat.calls[0])
	}
	_, err = env.svc.ListObjects(ctx, 1, 9001, "db_a", "missing", "", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Fatalf("missing schema = %v", err)
	}
	if env.cat.calls[1].schema != "missing" || env.cat.calls[1].fallback != "app" {
		t.Fatalf("explicit schema was rewritten: %+v", env.cat.calls[1])
	}
	env.cat.calls = nil
	if _, err := env.svc.ListSchemas(ctx, 1, 9001, "db_a", "", 1, 20, false); err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if _, err := env.svc.ListSchemas(ctx, 1, 9001, "db_a", "", 1, 20, false); err != nil {
		t.Fatalf("schemas cached: %v", err)
	}
	if _, err := env.svc.ListSchemas(ctx, 1, 9001, "db_b", "", 1, 20, false); err != nil {
		t.Fatalf("db_b schemas: %v", err)
	}
	if len(env.cat.calls) != 2 || env.cat.calls[0].database != "db_a" || env.cat.calls[1].database != "db_b" {
		t.Fatalf("cache calls = %+v", env.cat.calls)
	}
}

func TestPostgreSQLSchema_DefaultSchemaChangeDoesNotReuseCache(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	ctx := context.Background()
	env.cat.objects = []ObjectSummary{{Name: "orders", Kind: "table"}}

	listed, err := env.svc.ListObjects(ctx, 1, 9001, "db_a", "", "", "", 1, 20, false)
	if err != nil || listed.Schema != "app" || len(listed.Items) != 1 || listed.Items[0].Name != "orders" {
		t.Fatalf("default app = %v %+v", err, listed)
	}
	setPGDefaultSchema(env, "analytics")
	listed, err = env.svc.ListObjects(ctx, 1, 9001, "db_a", "", "", "", 1, 20, false)
	if err != nil || listed.Schema != "analytics" {
		t.Fatalf("default analytics = %v schema=%q calls=%d", err, listed.Schema, env.cat.callCount())
	}
	setPGDefaultSchema(env, "missing")
	if _, err = env.svc.ListObjects(ctx, 1, 9001, "db_a", "", "", "", 1, 20, false); !errors.Is(err, ErrSchemaNotFound) {
		t.Fatalf("missing default = %v", err)
	}
	setPGDefaultSchema(env, "secret")
	if _, err = env.svc.ListObjects(ctx, 1, 9001, "db_a", "", "", "", 1, 20, false); !errors.Is(err, ErrSchemaNotUsable) {
		t.Fatalf("unusable default = %v", err)
	}
	explicit, err := env.svc.ListObjects(ctx, 1, 9001, "db_a", "app", "", "", 1, 20, false)
	if err != nil || explicit.Schema != "app" {
		t.Fatalf("explicit app = %v %+v", err, explicit)
	}

	setPGDefaultSchema(env, "app")
	env.cat.calls = nil
	detail, err := env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "", "orders", "table", false)
	if err != nil || detail.Schema != "app" || detail.Name != "orders" {
		t.Fatalf("detail app = %v %+v", err, detail)
	}
	setPGDefaultSchema(env, "analytics")
	detail, err = env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "", "orders", "table", false)
	if err != nil || detail.Schema != "analytics" || detail.Name != "orders" {
		t.Fatalf("detail analytics = %v %+v", err, detail)
	}
	setPGDefaultSchema(env, "missing")
	if _, err = env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "", "orders", "table", false); !errors.Is(err, ErrSchemaNotFound) {
		t.Fatalf("detail missing = %v", err)
	}
	setPGDefaultSchema(env, "secret")
	if _, err = env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "", "orders", "table", false); !errors.Is(err, ErrSchemaNotUsable) {
		t.Fatalf("detail unusable = %v", err)
	}
	detail, err = env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "app", "orders", "table", false)
	if err != nil || detail.Schema != "app" {
		t.Fatalf("explicit detail = %v %+v", err, detail)
	}

	setPGDefaultSchema(env, "app")
	rel, err := env.svc.GetRelationshipMap(ctx, 1, 9001, "db_a", "", "orders", false)
	if err != nil || rel.Root.Schema != "app" || rel.Root.Name != "orders" {
		t.Fatalf("map app = %v %+v", err, rel.Root)
	}
	setPGDefaultSchema(env, "analytics")
	rel, err = env.svc.GetRelationshipMap(ctx, 1, 9001, "db_a", "", "orders", false)
	if err != nil || rel.Root.Schema != "analytics" {
		t.Fatalf("map analytics = %v %+v", err, rel.Root)
	}
	setPGDefaultSchema(env, "missing")
	if _, err = env.svc.GetRelationshipMap(ctx, 1, 9001, "db_a", "", "orders", false); !errors.Is(err, ErrSchemaNotFound) {
		t.Fatalf("map missing = %v", err)
	}
	setPGDefaultSchema(env, "secret")
	if _, err = env.svc.GetRelationshipMap(ctx, 1, 9001, "db_a", "", "orders", false); !errors.Is(err, ErrSchemaNotUsable) {
		t.Fatalf("map unusable = %v", err)
	}
	rel, err = env.svc.GetRelationshipMap(ctx, 1, 9001, "db_a", "app", "orders", false)
	if err != nil || rel.Root.Schema != "app" {
		t.Fatalf("explicit map = %v %+v", err, rel.Root)
	}
}

func setPGDefaultSchema(env pgUnitEnv, schema string) {
	row := env.store.rows["9001\x00db_a"]
	row.DefaultSchema = schema
	row.CredentialRef = "PG_T6_A"
	env.store.rows["9001\x00db_a"] = row
}

func TestPostgreSQLSchema_SingleflightDoesNotShareDistinctObjects(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	env.svc.audit = &lockingPGAudit{pgCredStore: env.store}
	gate := newArriveGate(2)
	env.cat.hold = gate
	ctx := context.Background()

	type result struct {
		resp model.ObjectDetailResponse
		err  error
	}
	got := make([]result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "app Kind:table Query:x", "orders", "table", false)
		got[0] = result{resp, err}
	}()
	go func() {
		defer wg.Done()
		resp, err := env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "app", "x Kind:table Query:orders", "table", false)
		got[1] = result{resp, err}
	}()
	select {
	case <-gate.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("distinct object requests collapsed onto one catalog call")
	}
	close(gate.release)
	wg.Wait()

	if got[0].err != nil || got[0].resp.Schema != "app Kind:table Query:x" || got[0].resp.Name != "orders" {
		t.Fatalf("request A = %v %+v", got[0].err, got[0].resp)
	}
	if got[1].err != nil || got[1].resp.Schema != "app" || got[1].resp.Name != "x Kind:table Query:orders" {
		t.Fatalf("request B = %v %+v", got[1].err, got[1].resp)
	}
	if env.cat.callCount() != 2 {
		t.Fatalf("catalog calls = %d", env.cat.callCount())
	}

	againA, err := env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "app Kind:table Query:x", "orders", "table", false)
	againB, errB := env.svc.GetObjectDetails(ctx, 1, 9001, "db_a", "app", "x Kind:table Query:orders", "table", false)
	if err != nil || errB != nil || againA.Schema != got[0].resp.Schema || againA.Name != "orders" || againB.Schema != "app" || againB.Name != got[1].resp.Name {
		t.Fatalf("cached identities A=%+v B=%+v err=%v %v", againA, againB, err, errB)
	}
	if env.cat.callCount() != 2 {
		t.Fatalf("cache wrote a shared catalog result, calls=%d", env.cat.callCount())
	}
}

func TestMapPGQueryError_StatementTimeoutSQLSTATE(t *testing.T) {
	t.Parallel()
	canceled := &pgconn.PgError{Code: pgQueryCanceledSQLSTATE, Message: "canceling statement due to statement timeout"}
	got := mapPGQueryError(canceled)
	if got != ErrSchemaTimeout || strings.Contains(got.Error(), "canceling") || strings.Contains(got.Error(), "57014") {
		t.Fatalf("57014 = %v", got)
	}
	other := &pgconn.PgError{Code: "42P01", Message: "relation secret does not exist"}
	got = mapPGQueryError(other)
	if got != ErrSchemaBackendError || strings.Contains(got.Error(), "secret") {
		t.Fatalf("other sqlstate = %v", got)
	}
}

func TestPostgreSQLSchema_DefinitionUnsupportedOnlyAfterProbe(t *testing.T) {
	t.Parallel()
	env := newPGUnit(t, pgTarget("Staging"), pgRows(), pgDSNs())
	ctx := context.Background()
	_, err := env.svc.GetTableDefinition(ctx, 1, 9001, "db_a", "", "orders")
	if !errors.Is(err, ErrSchemaObjectDefinitionUnsupported) {
		t.Fatalf("definition = %v", err)
	}
	_, err = env.svc.GetTableDefinition(ctx, 1, 9001, "db_a", "missing", "orders")
	if !errors.Is(err, ErrSchemaNotFound) || errors.Is(err, ErrSchemaObjectDefinitionUnsupported) {
		t.Fatalf("missing schema definition = %v", err)
	}
	env.cat.probeErr = errors.New("dial failed password=" + pgUnitPassA)
	_, err = env.svc.GetTableDefinition(ctx, 1, 9001, "db_a", "app", "orders")
	if !errors.Is(err, ErrSchemaBackendError) || errors.Is(err, ErrSchemaObjectDefinitionUnsupported) {
		t.Fatalf("probe failure = %v", err)
	}
	assertNoUnitSecret(t, err.Error())
	listed, err := env.svc.ListSchemas(ctx, 1, 9001, "db_a", "", 1, 20, false)
	if err != nil || listed.Database != "db_a" {
		t.Fatalf("schemas after unsupported = %v %+v", err, listed)
	}
}

func TestPageFixedDatabase_OneConnectionDatabase(t *testing.T) {
	t.Parallel()
	items, info := pageFixedDatabase("db_a", "", 1, 50)
	if len(items) != 1 || items[0].Name != "db_a" || !items[0].IsDefault || info.TotalItems != 1 {
		t.Fatalf("page1 = %+v %+v", items, info)
	}
	items, info = pageFixedDatabase("db_a", "", 2, 50)
	if len(items) != 0 || info.TotalItems != 1 {
		t.Fatalf("page2 = %+v %+v", items, info)
	}
	items, info = pageFixedDatabase("db_a", "other", 1, 50)
	if len(items) != 0 || info.TotalItems != 0 {
		t.Fatalf("filter = %+v %+v", items, info)
	}
}

func TestEscapePGLike_DoesNotChangeSQLStructure(t *testing.T) {
	t.Parallel()
	got := pgLikePattern(`a_b%c\`)
	if got != `%a\_b\%c\\%` {
		t.Fatalf("pattern = %q", got)
	}
	// The quote stays inside the bound pattern. Only LIKE metacharacters are
	// escaped, so the payload cannot widen the match or close a SQL literal.
	payload := `%' OR nspname = 'pg_catalog`
	got = pgLikePattern(payload)
	if got != `%`+escapePGLike(payload)+`%` || !strings.Contains(got, `\%' OR `) || strings.HasPrefix(got, `%' OR`) {
		t.Fatalf("pattern = %q", got)
	}
}
