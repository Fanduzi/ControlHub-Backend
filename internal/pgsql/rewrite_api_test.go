// Package pgsql_test exercises the exported PostgreSQL rewrite API from outside the package.
// input: GuardPG results, exact original SQL, pinned schema, stub resolver, PageOptions
// output: cross-package acceptance for RewriteFromGuardPaginated and same-analysis rejection checks
// pos: API boundary test for the narrow T7-S1 seam; verifies callers can reuse a GuardResult without constructing internal state
// note: if this file changes, update header and README.md
package pgsql_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fan/controlhub/internal/pgsql"
)

type rewriteResolver map[string][]string

func (r rewriteResolver) Columns(schema, name string) ([]string, error) {
	key := schema + "." + name
	if schema == "" {
		key = name
	}
	if cols, ok := r[key]; ok {
		return cols, nil
	}
	return nil, fmt.Errorf("%w: %s", pgsql.ErrRelationNotFound, key)
}

func TestRewriteFromGuardPaginated_CrossPackage(t *testing.T) {
	statement := `SELECT id FROM orders`
	gr, err := pgsql.GuardPG(statement)
	if err != nil {
		t.Fatalf("GuardPG: %v", err)
	}
	res := rewriteResolver{"app.orders": {"id", "amt"}}
	got, win, err := pgsql.RewriteFromGuardPaginated(statement, "app", gr, res,
		pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100})
	if err != nil {
		t.Fatalf("RewriteFromGuardPaginated: %v", err)
	}
	if got.SQL == "" || len(got.Public) != 1 || got.Public[0] != "id" || len(got.PublicProofs) != 1 || len(got.Refs) != 1 {
		t.Fatalf("rewrite result = %+v", got)
	}
	if win.Page != 1 || win.PageSize != 10 || win.FetchCount != 11 {
		t.Fatalf("page window = %+v", win)
	}
	if got.Refs[0].RefID != 0 || len(got.Witnesses) != 1 || len(got.Witnesses[0].CoveredSources) != 1 {
		t.Fatalf("source records = refs %+v witnesses %+v", got.Refs, got.Witnesses)
	}
}

func TestRewriteFromGuardPaginated_RejectsMixedAnalysis(t *testing.T) {
	gr, err := pgsql.GuardPG(`SELECT id FROM orders`)
	if err != nil {
		t.Fatalf("GuardPG: %v", err)
	}
	res := rewriteResolver{"app.orders": {"id", "amt"}}
	_, _, err = pgsql.RewriteFromGuardPaginated(`SELECT amt FROM orders`, "app", gr, res,
		pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100})
	if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
		t.Fatalf("mixed statement error = %v, want ErrEvidenceUnavailable", err)
	}
	gr.Refs = nil
	_, _, err = pgsql.RewriteFromGuardPaginated(`SELECT id FROM orders`, "app", gr, res,
		pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100})
	if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
		t.Fatalf("mutated refs error = %v, want ErrEvidenceUnavailable", err)
	}
}
