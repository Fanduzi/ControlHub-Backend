// Package model provides tests for resource health observations.
// input: HealthObservation freshness calculation and time.Duration thresholds
// output: freshness boundary, per-type fallback, and worst-fresh effective-health tests
// pos: Pure domain contract for Issue 81 resource health calculation
// note: if this file changes, update this header and module README.md.
package model

import (
	"testing"
	"time"
)

func TestHealthObservationFreshnessBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	threshold := 24 * time.Hour

	tests := []struct {
		name       string
		observedAt time.Time
		want       HealthFreshness
	}{
		{name: "exactly at threshold is fresh", observedAt: now.Add(-threshold), want: HealthFreshnessFresh},
		{name: "older than threshold is stale", observedAt: now.Add(-threshold - time.Nanosecond), want: HealthFreshnessStale},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observation := HealthObservation{ObservedAt: tt.observedAt}
			if got := observation.FreshnessAt(now, threshold); got != tt.want {
				t.Fatalf("FreshnessAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHealthFreshnessThresholdsUseTypeOrDefault(t *testing.T) {
	thresholds := HealthFreshnessThresholds{ResourceTypeHost: 5 * time.Minute}
	if got := thresholds.For(ResourceTypeHost); got != 5*time.Minute {
		t.Fatalf("host threshold = %s, want 5m", got)
	}
	if got := thresholds.For(ResourceTypeService); got != 24*time.Hour {
		t.Fatalf("service fallback threshold = %s, want 24h", got)
	}
}

func TestResolveHealthUsesWorstFreshObservation(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-2 * time.Hour)
	status, freshness, gotObservedAt, observer := ResolveHealth(now, 24*time.Hour, nil, []HealthObservation{
		{Status: HealthStatusHealthy, ObservedAt: now.Add(-time.Hour), Observer: "prometheus"},
		{Status: HealthStatusCritical, ObservedAt: observedAt, Observer: "synthetic-check"},
		{Status: HealthStatusCritical, ObservedAt: now.Add(-25 * time.Hour), Observer: "stale-check"},
	})
	if status != HealthStatusCritical || freshness != HealthFreshnessFresh || gotObservedAt == nil || !gotObservedAt.Equal(observedAt) || observer != "synthetic-check" {
		t.Fatalf("resolved health = (%q, %q, %v, %q)", status, freshness, gotObservedAt, observer)
	}
}

func TestResolveHealthNeverLetsMissingEvidenceAppearHealthy(t *testing.T) {
	healthy := HealthStatusHealthy
	status, freshness, observedAt, observer := ResolveHealth(time.Now(), 24*time.Hour, &healthy, nil)
	if status == HealthStatusHealthy || freshness != HealthFreshnessNever || observedAt != nil || observer != "" {
		t.Fatalf("missing evidence = (%q, %q, %v, %q), want non-healthy/never", status, freshness, observedAt, observer)
	}
}
