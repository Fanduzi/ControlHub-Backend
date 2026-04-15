// Package model provides domain entities for the resource management system.
// input: no external dependencies
// output: PageInfo, ResourceListQuery, AuditListQuery, pagination constants, ComputeTotalPages, NormalizePagination
// pos: Shared pagination and filtering types for all list endpoints
// note: if this file changes, update header and README.md
package model

const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// PageInfo carries pagination metadata in list responses.
type PageInfo struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

// ResourceListQuery holds all query parameters for GET /resources.
type ResourceListQuery struct {
	ResourceType    string
	EnvironmentID   string
	LifecycleStatus string
	HealthStatus    string
	Query           string // free-text search over name, display_name, external_id
	IncludeArchived bool
	Page            int
	PageSize        int
}

// AuditListQuery holds all query parameters for GET /audit-events.
type AuditListQuery struct {
	TargetResourceID string
	EventType        string
	Result           string
	Page             int
	PageSize         int
}

// ComputeTotalPages returns the number of pages needed for totalItems items at pageSize.
func ComputeTotalPages(totalItems, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	if totalItems <= 0 {
		return 0
	}
	return (totalItems + pageSize - 1) / pageSize
}

// NormalizePagination applies defaults and caps to raw page/pageSize values.
func NormalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = DefaultPage
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}
