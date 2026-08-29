//go:build integration

// Package integration provides real-MySQL coverage for named inventory views.
// input: context, encoding/json, errors, testing, internal/model, internal/repository/mysql, internal/service
// output: TestNamedInventoryViewContract
// pos: Real-MySQL contract test for named-view authorization and lossless search-state serialization
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestNamedInventoryViewContract proves that personal views remain owner-only,
// shared views remain readable by every user but mutable only by admins, and
// saved JSON contains only reusable inventory search state. Unknown/stale
// filter values deliberately round-trip so applying a view reruns today's
// controlled inventory search instead of persisting or reconstructing results.
func TestNamedInventoryViewContract(t *testing.T) {
	db := setupTestDB(t)
	views := service.NewNamedInventoryViewService(mysql.NewNamedInventoryViewRepository(db))
	ctx := context.Background()
	owner := service.AuthenticatedUser{ID: 840001, Role: "editor"}
	other := service.AuthenticatedUser{ID: 840002, Role: "editor"}
	admin := service.AuthenticatedUser{ID: 840003, Role: "admin"}

	state := model.NamedInventoryViewState{
		Filters: model.NamedInventoryViewFilters{
			Query:            "payments",
			ResourceTypes:    []string{"service", "stale-type"},
			ResourceSubtypes: []string{"stale-subtype"},
			EnvironmentIDs:   []uint64{999999999},
			LifecycleStatus:  []string{"retired-status"},
			HealthStatuses:   []string{"unknown-health"},
			OwnerID:          uint64Pointer(999999998),
			Labels:           []string{"team:payments", "stale:value"},
			IncludeArchived:  true,
			ArchivedOnly:     false,
		},
		Sort:    model.NamedInventoryViewSort{Field: "name", Direction: "asc"},
		Columns: []string{"name", "resourceType", "environment", "healthStatus"},
	}

	personal, err := views.Create(ctx, owner, model.NamedInventoryViewCreateRequest{
		Name: "My payment inventory", Scope: model.NamedInventoryViewPersonal, State: state,
	})
	if err != nil {
		t.Fatalf("create personal view: %v", err)
	}
	if _, err := views.Create(ctx, other, model.NamedInventoryViewCreateRequest{
		Name: "Forbidden shared view", Scope: model.NamedInventoryViewShared, State: state,
	}); !errors.Is(err, service.ErrNamedInventoryViewForbidden) {
		t.Fatalf("editor create shared error = %v, want forbidden", err)
	}
	shared, err := views.Create(ctx, admin, model.NamedInventoryViewCreateRequest{
		Name: "Team payment inventory", Scope: model.NamedInventoryViewShared, State: state,
	})
	if err != nil {
		t.Fatalf("admin create shared view: %v", err)
	}

	ownerVisible, err := views.List(ctx, owner)
	if err != nil {
		t.Fatalf("list owner views: %v", err)
	}
	if len(ownerVisible) != 2 || ownerVisible[0].ID != personal.ID || ownerVisible[1].ID != shared.ID {
		t.Fatalf("owner views = %+v, want own personal and shared", ownerVisible)
	}
	otherVisible, err := views.List(ctx, other)
	if err != nil {
		t.Fatalf("list other views: %v", err)
	}
	if len(otherVisible) != 1 || otherVisible[0].ID != shared.ID {
		t.Fatalf("other views = %+v, want shared only", otherVisible)
	}
	if got, want := mustJSON(t, ownerVisible[0].State), mustJSON(t, state); got != want {
		t.Fatalf("state round-trip = %s, want %s", got, want)
	}
	encoded := mustJSON(t, ownerVisible[0])
	for _, forbidden := range []string{`"items"`, `"results"`, `"page"`, `"pageSize"`} {
		if containsJSONField(encoded, forbidden) {
			t.Fatalf("serialized view contains result/page snapshot field %s: %s", forbidden, encoded)
		}
	}

	if err := views.Update(ctx, other, personal.ID, model.NamedInventoryViewUpdateRequest{Name: "stolen", State: state}); !errors.Is(err, service.ErrNamedInventoryViewNotFound) {
		t.Fatalf("non-owner update personal error = %v, want not found", err)
	}
	if err := views.Delete(ctx, other, personal.ID); !errors.Is(err, service.ErrNamedInventoryViewNotFound) {
		t.Fatalf("non-owner delete personal error = %v, want not found", err)
	}
	if err := views.Update(ctx, other, shared.ID, model.NamedInventoryViewUpdateRequest{Name: "forbidden", State: state}); !errors.Is(err, service.ErrNamedInventoryViewForbidden) {
		t.Fatalf("editor update shared error = %v, want forbidden", err)
	}
	if err := views.Delete(ctx, other, shared.ID); !errors.Is(err, service.ErrNamedInventoryViewForbidden) {
		t.Fatalf("editor delete shared error = %v, want forbidden", err)
	}
	if err := views.Update(ctx, owner, personal.ID, model.NamedInventoryViewUpdateRequest{Name: "Renamed personal", State: state}); err != nil {
		t.Fatalf("owner update personal: %v", err)
	}
	if err := views.Update(ctx, admin, shared.ID, model.NamedInventoryViewUpdateRequest{Name: "Renamed shared", State: state}); err != nil {
		t.Fatalf("admin update shared: %v", err)
	}
	if err := views.Delete(ctx, owner, personal.ID); err != nil {
		t.Fatalf("owner delete personal: %v", err)
	}
	if err := views.Delete(ctx, admin, shared.ID); err != nil {
		t.Fatalf("admin delete shared: %v", err)
	}
}

func uint64Pointer(value uint64) *uint64 { return &value }

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(raw)
}

func containsJSONField(encoded, field string) bool {
	for index := 0; index+len(field) <= len(encoded); index++ {
		if encoded[index:index+len(field)] == field {
			return true
		}
	}
	return false
}
