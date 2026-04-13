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
