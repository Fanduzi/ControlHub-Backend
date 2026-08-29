// Package model provides domain entities for the resource management system.
// input: encoding/json, fmt, strings, time, unicode/utf8
// output: NamedInventoryView scope, state, requests, persisted record, and validation
// pos: Minimal JSON contract for reusable inventory filters, sort, and columns
// note: if this file changes, update this header and module README.md.
package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type NamedInventoryViewScope string

const (
	NamedInventoryViewPersonal NamedInventoryViewScope = "personal"
	NamedInventoryViewShared   NamedInventoryViewScope = "shared"
	MaxNamedInventoryViewState                         = 16 * 1024
)

type NamedInventoryViewFilters struct {
	Query            string   `json:"q,omitempty"`
	ResourceTypes    []string `json:"resourceType,omitempty"`
	ResourceSubtypes []string `json:"resourceSubtype,omitempty"`
	EnvironmentIDs   []uint64 `json:"environmentId,omitempty"`
	LifecycleStatus  []string `json:"lifecycleStatus,omitempty"`
	HealthStatuses   []string `json:"healthStatus,omitempty"`
	OwnerID          *uint64  `json:"ownerId,omitempty"`
	Labels           []string `json:"label,omitempty"`
	IncludeArchived  bool     `json:"includeArchived,omitempty"`
	ArchivedOnly     bool     `json:"archivedOnly,omitempty"`
}

type NamedInventoryViewSort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type NamedInventoryViewState struct {
	Filters NamedInventoryViewFilters `json:"filters"`
	Sort    NamedInventoryViewSort    `json:"sort"`
	Columns []string                  `json:"columns"`
}

type NamedInventoryView struct {
	ID          uint64                  `json:"id"`
	OwnerUserID uint64                  `json:"-"`
	Name        string                  `json:"name"`
	Scope       NamedInventoryViewScope `json:"scope"`
	State       NamedInventoryViewState `json:"state"`
	CreatedAt   time.Time               `json:"createdAt"`
	UpdatedAt   time.Time               `json:"updatedAt"`
}

type NamedInventoryViewCreateRequest struct {
	Name  string                  `json:"name"`
	Scope NamedInventoryViewScope `json:"scope"`
	State NamedInventoryViewState `json:"state"`
}

type NamedInventoryViewUpdateRequest struct {
	Name  string                  `json:"name"`
	State NamedInventoryViewState `json:"state"`
}

func (r NamedInventoryViewCreateRequest) Validate() error {
	if err := validateNamedInventoryViewName(r.Name); err != nil {
		return err
	}
	if r.Scope != NamedInventoryViewPersonal && r.Scope != NamedInventoryViewShared {
		return fmt.Errorf("invalid scope")
	}
	return r.State.Validate()
}

func (r NamedInventoryViewUpdateRequest) Validate() error {
	if err := validateNamedInventoryViewName(r.Name); err != nil {
		return err
	}
	return r.State.Validate()
}

func (s NamedInventoryViewState) Validate() error {
	for _, id := range s.Filters.EnvironmentIDs {
		if id == 0 {
			return fmt.Errorf("environment ids must be positive")
		}
	}
	if s.Filters.OwnerID != nil && *s.Filters.OwnerID == 0 {
		return fmt.Errorf("owner id must be positive")
	}
	if s.Sort.Field == "" || (s.Sort.Direction != "asc" && s.Sort.Direction != "desc") {
		return fmt.Errorf("sort field and asc or desc direction are required")
	}
	if len(s.Columns) == 0 || len(s.Columns) > 64 {
		return fmt.Errorf("columns must contain 1 to 64 values")
	}
	seen := make(map[string]struct{}, len(s.Columns))
	for _, column := range s.Columns {
		if column == "" || utf8.RuneCountInString(column) > 64 {
			return fmt.Errorf("invalid column")
		}
		if _, exists := seen[column]; exists {
			return fmt.Errorf("duplicate column")
		}
		seen[column] = struct{}{}
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > MaxNamedInventoryViewState {
		return fmt.Errorf("view state exceeds %d bytes", MaxNamedInventoryViewState)
	}
	return nil
}

func validateNamedInventoryViewName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return fmt.Errorf("name must contain 1 to 120 characters")
	}
	for _, r := range name {
		if r < 32 {
			return fmt.Errorf("name contains control characters")
		}
	}
	return nil
}
