// Package model provides domain entities for the resource management system.
// input: internal/model
// output: TestComputeTotalPages, TestNormalizePagination
// pos: Unit tests for pagination helpers
// note: if this file changes, update header and README.md
package model

import "testing"

func TestComputeTotalPages(t *testing.T) {
	tests := []struct {
		totalItems int
		pageSize   int
		want       int
	}{
		{0, 20, 0},
		{1, 20, 1},
		{19, 20, 1},
		{20, 20, 1},
		{21, 20, 2},
		{40, 20, 2},
		{64, 20, 4},
		{100, 20, 5},
		{5, 0, 0},
		{5, -1, 0},
		{0, 0, 0},
	}

	for _, tt := range tests {
		got := ComputeTotalPages(tt.totalItems, tt.pageSize)
		if got != tt.want {
			t.Errorf("ComputeTotalPages(%d, %d) = %d, want %d", tt.totalItems, tt.pageSize, got, tt.want)
		}
	}
}

func TestDedupStrings(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil input", nil, nil},
		{"empty input", []string{}, nil},
		{"single value", []string{"a"}, []string{"a"}},
		{"deduplicates", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"removes empty strings", []string{"a", "", "b", ""}, []string{"a", "b"}},
		{"all empty", []string{"", "", ""}, nil},
		{"preserves order", []string{"c", "a", "b", "a"}, []string{"c", "a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DedupStrings(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("DedupStrings(%v) = %v, want %v", tt.in, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("DedupStrings(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNewPageInfo(t *testing.T) {
	tests := []struct {
		name             string
		page             int
		pageSize         int
		totalItems       int
		wantHasNext      bool
		wantHasPrevious  bool
		wantTotalPages   int
	}{
		{
			name:            "first page of many",
			page:            1,
			pageSize:        20,
			totalItems:      64,
			wantHasNext:     true,
			wantHasPrevious: false,
			wantTotalPages:  4,
		},
		{
			name:            "middle page",
			page:            2,
			pageSize:        20,
			totalItems:      64,
			wantHasNext:     true,
			wantHasPrevious: true,
			wantTotalPages:  4,
		},
		{
			name:            "last page",
			page:            4,
			pageSize:        20,
			totalItems:      64,
			wantHasNext:     false,
			wantHasPrevious: true,
			wantTotalPages:  4,
		},
		{
			name:            "single page fits all",
			page:            1,
			pageSize:        20,
			totalItems:      5,
			wantHasNext:     false,
			wantHasPrevious: false,
			wantTotalPages:  1,
		},
		{
			name:            "empty result set",
			page:            1,
			pageSize:        20,
			totalItems:      0,
			wantHasNext:     false,
			wantHasPrevious: false,
			wantTotalPages:  0,
		},
		{
			name:            "page beyond data",
			page:            5,
			pageSize:        20,
			totalItems:      64,
			wantHasNext:     false,
			wantHasPrevious: true,
			wantTotalPages:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPageInfo(tt.page, tt.pageSize, tt.totalItems)
			if got.Page != tt.page {
				t.Errorf("Page = %d, want %d", got.Page, tt.page)
			}
			if got.PageSize != tt.pageSize {
				t.Errorf("PageSize = %d, want %d", got.PageSize, tt.pageSize)
			}
			if got.TotalItems != tt.totalItems {
				t.Errorf("TotalItems = %d, want %d", got.TotalItems, tt.totalItems)
			}
			if got.TotalPages != tt.wantTotalPages {
				t.Errorf("TotalPages = %d, want %d", got.TotalPages, tt.wantTotalPages)
			}
			if got.HasNextPage != tt.wantHasNext {
				t.Errorf("HasNextPage = %v, want %v", got.HasNextPage, tt.wantHasNext)
			}
			if got.HasPreviousPage != tt.wantHasPrevious {
				t.Errorf("HasPreviousPage = %v, want %v", got.HasPreviousPage, tt.wantHasPrevious)
			}
		})
	}
}

func TestNormalizePagination(t *testing.T) {
tests := []struct {
	name         string
	page         int
	pageSize     int
	wantPage     int
	wantPageSize int
}{
	{"defaults for zero values", 0, 0, DefaultPage, DefaultPageSize},
	{"negative page becomes default", -1, 20, DefaultPage, 20},
	{"negative pageSize becomes default", 1, -5, 1, DefaultPageSize},
	{"valid values pass through", 3, 50, 3, 50},
	{"pageSize capped at max", 1, 500, 1, MaxPageSize},
	{"pageSize exactly at max", 1, MaxPageSize, 1, MaxPageSize},
	{"page 1 with default pageSize", 1, DefaultPageSize, 1, DefaultPageSize},
	{"page_capped_at_max", MaxPage + 1, 20, MaxPage, 20},
	{"page_exactly_at_max", MaxPage, 20, MaxPage, 20},
	{"huge_page_capped", 3945752125071476982, 64, MaxPage, 64},
}

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		gotPage, gotPageSize := NormalizePagination(tt.page, tt.pageSize)
		if gotPage != tt.wantPage || gotPageSize != tt.wantPageSize {
			t.Errorf("NormalizePagination(%d, %d) = (%d, %d), want (%d, %d)",
				tt.page, tt.pageSize, gotPage, gotPageSize, tt.wantPage, tt.wantPageSize)
		}
	})
}
}
