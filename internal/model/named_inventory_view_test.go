// Package model tests named inventory view validation.
// input: testing and named inventory view state values
// output: positive-ID filter validation regression tests
// pos: Public named-view state validation contract tests
// note: if this file changes, update this header and README.md.
package model

import "testing"

func TestNamedInventoryViewStateRejectsZeroFilterIDs(t *testing.T) {
	valid := NamedInventoryViewState{
		Sort:    NamedInventoryViewSort{Field: "name", Direction: "asc"},
		Columns: []string{"name"},
	}
	zero := uint64(0)
	for name, state := range map[string]NamedInventoryViewState{
		"environment": {Filters: NamedInventoryViewFilters{EnvironmentIDs: []uint64{0}}, Sort: valid.Sort, Columns: valid.Columns},
		"owner":       {Filters: NamedInventoryViewFilters{OwnerID: &zero}, Sort: valid.Sort, Columns: valid.Columns},
	} {
		t.Run(name, func(t *testing.T) {
			if err := state.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want positive-ID rejection")
			}
		})
	}
}
