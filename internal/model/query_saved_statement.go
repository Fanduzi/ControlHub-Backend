// Package model provides domain entities for the resource management system.
// input: fmt, time, unicode/utf8 packages
// output: QuerySavedStatementScope, QuerySavedStatement, QuerySavedStatementCreateRequest, QuerySavedStatementUpdateRequest, QuerySavedStatementListQuery, QuerySavedStatementListResponse
// pos: Governed saved statements for target-scoped query library
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// QuerySavedStatementScope is the immutable visibility scope for a saved statement.
type QuerySavedStatementScope string

const (
	QuerySavedStatementPersonal       QuerySavedStatementScope = "personal"
	QuerySavedStatementSharedTemplate QuerySavedStatementScope = "shared_template"
)

// Validate returns nil only for a known scope value.
func (s QuerySavedStatementScope) Validate() error {
	switch s {
	case QuerySavedStatementPersonal, QuerySavedStatementSharedTemplate:
		return nil
	}
	return fmt.Errorf("invalid scope: %s", s)
}

// MaxSavedStatementNameLength bounds the name field.
const MaxSavedStatementNameLength = 120

// MaxSavedStatementSize bounds the statement text at 16 KiB.
const MaxSavedStatementSize = 16 * 1024

// QuerySavedStatement is the persisted saved statement record.
type QuerySavedStatement struct {
	ID               uint64                   `json:"id"`
	TargetResourceID uint64                   `json:"targetResourceId"`
	OwnerUserID      uint64                   `json:"-"` // Never exposed in API
	Name             string                   `json:"name"`
	Statement        string                   `json:"statement"`
	Scope            QuerySavedStatementScope `json:"scope"`
	CreatedAt        time.Time                `json:"createdAt"`
	UpdatedAt        time.Time                `json:"updatedAt"`
}

// QuerySavedStatementCreateRequest is the body for creating a saved statement.
// TargetResourceID comes from the URL path, not the request body.
type QuerySavedStatementCreateRequest struct {
	Name      string                   `json:"name"`
	Statement string                   `json:"statement"`
	Scope     QuerySavedStatementScope `json:"scope"`
}

// Validate checks all required fields and bounds.
func (r QuerySavedStatementCreateRequest) Validate() error {
	if err := validateSavedStatementName(r.Name); err != nil {
		return err
	}
	if r.Statement == "" {
		return fmt.Errorf("statement is required")
	}
	if len(r.Statement) > MaxSavedStatementSize {
		return fmt.Errorf("statement exceeds %d bytes", MaxSavedStatementSize)
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	return nil
}

// QuerySavedStatementUpdateRequest is the body for updating a saved statement.
// Scope is immutable and never accepted on update.
type QuerySavedStatementUpdateRequest struct {
	Name      string `json:"name"`
	Statement string `json:"statement"`
}

// Validate checks all required fields and bounds.
func (r QuerySavedStatementUpdateRequest) Validate() error {
	if err := validateSavedStatementName(r.Name); err != nil {
		return err
	}
	if r.Statement == "" {
		return fmt.Errorf("statement is required")
	}
	if len(r.Statement) > MaxSavedStatementSize {
		return fmt.Errorf("statement exceeds %d bytes", MaxSavedStatementSize)
	}
	return nil
}

// QuerySavedStatementListQuery carries the filter for listing saved statements.
type QuerySavedStatementListQuery struct {
	TargetResourceID uint64
	OwnerUserID      uint64
	Page             int
	PageSize         int
	Search           string
}

// QuerySavedStatementListResponse is the paginated list response.
type QuerySavedStatementListResponse struct {
	Items                    []QuerySavedStatement `json:"items"`
	PageInfo                 PageInfo              `json:"pageInfo"`
	CanManageSharedTemplates bool                  `json:"canManageSharedTemplates"`
}

// validateSavedStatementName rejects empty, over-long, and control-character names.
func validateSavedStatementName(name string) error {
	name = trimWhitespace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > MaxSavedStatementNameLength {
		return fmt.Errorf("name exceeds %d characters", MaxSavedStatementNameLength)
	}
	for _, r := range name {
		if r < 32 {
			return fmt.Errorf("name contains control characters")
		}
	}
	return nil
}

func trimWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}
