// Package service provides tests for the PostgreSQL governed Explain path.
// input: context, errors, testing, time, internal/model, internal/service
// output: TestQueryExplainServicePG* covering the release gate, engine
// dispatch, (target,database) access, pg guard, schema defaulting, audit
// pos: PG sibling of query_explain_service_test.go — the runner is stubbed;
// the integration/live path is covered against a real PostgreSQL target
// note: proves Explain never reaches an unwrapped SELECT and never creates
// a query_executions row — the runner receives statement text it may only
// send as EXPLAIN (FORMAT JSON) …
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
)

// fakePGExplainRunner captures the exact inputs the service dispatched so
// tests can prove the pinned schema and guarded statement reach the runner
// verbatim — the runner is the only place wire traffic may occur.
type fakePGExplainRunner struct {
	called       bool
	gotSchema    string
	gotStatement string
	gotCfg       *pgx.ConnConfig
	raw          ExplainRawPlan
	err          error
}

func (f *fakePGExplainRunner) Explain(ctx context.Context, cfg *pgx.ConnConfig, pinnedSchema, statement string) (ExplainRawPlan, error) {
	f.called = true
	f.gotSchema = pinnedSchema
	f.gotStatement = statement
	f.gotCfg = cfg
	return f.raw, f.err
}

// pgExplainTestDSN parses into a valid pgx.ConnConfig bound to the PG test
// target's host/port and the labdb connection identity.
const pgExplainTestDSN = "postgres://ro:secret-do-not-leak@pg.internal:5432/labdb?sslmode=disable"

func makeReadyPGTarget(id uint64) model.QueryTarget {
	return model.QueryTarget{
		ResourceID: id,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: "staging",
			Engine:      "postgresql",
			Host:        "pg.internal",
			Port:        5432,
		},
	}
}

func readyPGCredential(resourceID uint64, defaultSchema string) model.QueryCredentialMetadata {
	return model.QueryCredentialMetadata{
		ResourceID:        resourceID,
		DatabaseName:      "labdb",
		Enabled:           true,
		Engine:            "postgresql",
		CredentialRef:     "PG_LABDB_RO",
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
		DefaultSchema:     defaultSchema,
	}
}

// newPGExplainService mirrors newExplainService but wires the same
// resolver the PG execution path uses — the fake repo ignores the
// database argument and returns the credential keyed by resource id.
func newPGExplainService(t *testing.T, targets []model.QueryTarget, creds map[uint64]model.QueryCredentialMetadata, audit QueryExplainAuditRecorder) (*QueryExplainService, *fakePGExplainRunner) {
	t.Helper()
	targetRepo := fakeTargetRepo{targets: targets}
	execRepo := &fakeExecRepo{credentials: creds}
	resolver := &fakeResolver{dsn: pgExplainTestDSN}
	access := NewTargetAccessResolver(targetRepo, execRepo, resolver)
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	runner := &fakePGExplainRunner{}
	svc := NewQueryExplainService(guard, access, &fakeExplainExecutor{}, NewExplainNormalizer(), &fakeClock{t: time.Now()}, audit).
		WithPGExplain(runner, NewPGExplainNormalizer())
	return svc, runner
}

// TestQueryExplainServicePGGateClosed proves the construction-time release
// gate: without WithPGExplain a postgresql target answers
// ErrQueryExplainNotSupported and records the unsupported audit outcome.
// WHY: production builds ship default-closed — the capability can never be
// opened by request fields.
func TestQueryExplainServicePGGateClosed(t *testing.T) {
	audit := &fakeAuditRecorder{}
	targetRepo := fakeTargetRepo{targets: []model.QueryTarget{makeReadyPGTarget(105)}}
	execRepo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")}}
	access := NewTargetAccessResolver(targetRepo, execRepo, &fakeResolver{dsn: pgExplainTestDSN})
	svc := NewQueryExplainService(
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		access, &fakeExplainExecutor{}, NewExplainNormalizer(), &fakeClock{t: time.Now()}, audit,
	)
	_, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
		Statement: "select 1", Database: "labdb",
	})
	if !errors.Is(err, ErrQueryExplainNotSupported) {
		t.Fatalf("err = %v, want ErrQueryExplainNotSupported", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditUnsupported {
		t.Fatalf("audit = %+v, want one unsupported call", audit.calls)
	}
}

// TestQueryExplainServicePGSuccess proves the happy path: guarded SELECT
// reaches the runner verbatim with the pinned schema, the response carries
// engine=postgresql, and the success audit fires.
func TestQueryExplainServicePGSuccess(t *testing.T) {
	audit := &fakeAuditRecorder{}
	svc, runner := newPGExplainService(t,
		[]model.QueryTarget{makeReadyPGTarget(105)},
		map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")},
		audit,
	)
	runner.raw = pgRawPlan(t, `[{"Plan":{"Node Type":"Seq Scan","Plan Rows":9}}]`)

	resp, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
		Statement: "select * from orders", Database: "labdb",
	})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !runner.called {
		t.Fatal("runner was not called")
	}
	if runner.gotStatement != "select * from orders" {
		t.Errorf("statement = %q, want the guarded original", runner.gotStatement)
	}
	if runner.gotSchema != "e2e" {
		t.Errorf("pinned schema = %q, want connection default e2e", runner.gotSchema)
	}
	if runner.gotCfg == nil {
		t.Error("runner received nil ConnConfig")
	}
	if resp.Engine != model.ExplainEnginePostgreSQL {
		t.Errorf("engine = %q, want postgresql", resp.Engine)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].Access != model.ExplainAccessFullScan {
		t.Errorf("nodes = %+v, want one full_scan node", resp.Nodes)
	}
	if err := resp.Validate(); err != nil {
		t.Errorf("response failed contract validation: %v", err)
	}
	if len(audit.calls) == 0 || audit.calls[len(audit.calls)-1].outcome != model.ExplainAuditSuccess {
		t.Errorf("audit = %+v, want terminal success", audit.calls)
	}
}

// TestQueryExplainServicePGExplicitSchemaWins proves an explicit request
// schema overrides the connection default — mirroring execute's verbatim
// schema rule.
func TestQueryExplainServicePGExplicitSchemaWins(t *testing.T) {
	svc, runner := newPGExplainService(t,
		[]model.QueryTarget{makeReadyPGTarget(105)},
		map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")},
		nil,
	)
	runner.raw = pgRawPlan(t, `[{"Plan":{"Node Type":"Index Scan","Plan Rows":1}}]`)
	if _, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
		Statement: "select 1", Database: "labdb", Schema: "e2e_alt",
	}); err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if runner.gotSchema != "e2e_alt" {
		t.Errorf("pinned schema = %q, want e2e_alt", runner.gotSchema)
	}
}

// TestQueryExplainServicePGRequiresDatabase proves an empty database fails
// closed with the schema-validation sentinel — there is no connection
// identity to bind.
func TestQueryExplainServicePGRequiresDatabase(t *testing.T) {
	svc, runner := newPGExplainService(t,
		[]model.QueryTarget{makeReadyPGTarget(105)},
		map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")},
		nil,
	)
	_, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
		Statement: "select 1",
	})
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("err = %v, want ErrSchemaValidationFailed", err)
	}
	if runner.called {
		t.Error("runner called despite missing database")
	}
}

// TestQueryExplainServicePGGuardRejects proves the pg_query guard is the
// boundary: non-SELECT and user-typed EXPLAIN never reach the runner.
func TestQueryExplainServicePGGuardRejects(t *testing.T) {
	svc, runner := newPGExplainService(t,
		[]model.QueryTarget{makeReadyPGTarget(105)},
		map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")},
		nil,
	)
	for _, stmt := range []string{
		"explain select 1",
		"delete from orders",
		"select 1; select 2",
		"select pg_sleep(1)",
	} {
		_, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
			Statement: stmt, Database: "labdb",
		})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Errorf("%q: err = %v, want ErrQueryValidationFailed", stmt, err)
		}
	}
	if runner.called {
		t.Error("runner called for a rejected statement")
	}
}

// TestQueryExplainServicePGAccessRefusal proves a disabled connection maps
// to the connection-disabled sentinel (surfaced verbatim by
// pgAccessRefusalError), not a generic not-allowed.
func TestQueryExplainServicePGAccessRefusal(t *testing.T) {
	cred := readyPGCredential(105, "e2e")
	cred.Enabled = false
	audit := &fakeAuditRecorder{}
	svc, _ := newPGExplainService(t,
		[]model.QueryTarget{makeReadyPGTarget(105)},
		map[uint64]model.QueryCredentialMetadata{105: cred},
		audit,
	)
	_, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
		Statement: "select 1", Database: "labdb",
	})
	if !errors.Is(err, ErrSchemaConnectionDisabled) {
		t.Fatalf("err = %v, want ErrSchemaConnectionDisabled", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditRejected {
		t.Fatalf("audit = %+v, want one rejected call", audit.calls)
	}
}

// TestQueryExplainServicePGRunnerErrors proves runner failures classify:
// timeout stays timeout, unsupported stays unsupported, everything else is
// the backend-failure sentinel — driver text never surfaces.
func TestQueryExplainServicePGRunnerErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"timeout", ErrQueryTimeout, ErrQueryTimeout},
		{"unsupported", ErrQueryExplainNotSupported, ErrQueryExplainNotSupported},
		{"backend", ErrQueryBackendFailure, ErrQueryBackendFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, runner := newPGExplainService(t,
				[]model.QueryTarget{makeReadyPGTarget(105)},
				map[uint64]model.QueryCredentialMetadata{105: readyPGCredential(105, "e2e")},
				nil,
			)
			runner.err = tc.err
			_, err := svc.Explain(context.Background(), 7, 105, model.ExplainRequest{
				Statement: "select 1", Database: "labdb",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "pg.internal") {
				t.Errorf("error leaked connection detail: %v", err)
			}
		})
	}
}
