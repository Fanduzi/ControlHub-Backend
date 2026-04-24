// Package model provides domain entities for the resource management system.
// input: no external dependencies
// output: PageInfo, ResourceListQuery, AuditListQuery, pagination constants, ComputeTotalPages, NormalizePagination
// pos: Shared pagination and filtering types for all list endpoints
// note: if this file changes, update header and README.md
package model

const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 500
)

// PageInfo carries pagination metadata in list responses.
type PageInfo struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

// ResourceListQuery holds all query parameters for GET /resources.
// Repeated query parameters are collected into slices for multi-select filtering.
// Within a filter family, values combine with logical OR. Across families, filters combine with AND.
type ResourceListQuery struct {
	ResourceTypes    []string // repeated ?resourceType= values
	ResourceSubtypes []string // repeated ?resourceSubtype= values
	EnvironmentIDs   []uint64 // repeated ?environmentId= values
	LifecycleStatus  []string // repeated ?lifecycleStatus= values
	HealthStatuses   []string // repeated ?healthStatus= values
	Query            string   // free-text search over name, display_name, external_id
	IncludeArchived  bool
	ArchivedOnly     bool // when true, return only archived resources (takes precedence over IncludeArchived)
	Page             int
	PageSize         int
}

// AuditListQuery holds all query parameters for GET /audit-events.
// Repeated query parameters are collected into slices for multi-select filtering.
type AuditListQuery struct {
	TargetResourceID *uint64  // kept single-value — naturally unique filter
	EventTypes       []string // repeated ?eventType= values
	Results          []string // repeated ?result= values
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

// MaxPage prevents integer overflow in offset calculation: (page-1)*pageSize must not overflow int.
const MaxPage = 1_000_000_000

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
	if page > MaxPage {
		page = MaxPage
	}
	return page, pageSize
}

// DedupStrings returns a deduplicated copy of s, preserving first-occurrence order.
// Empty strings are removed.
func DedupStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
