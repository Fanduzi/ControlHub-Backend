// Package model provides domain entities for resource health observations.
// input: time.Time observation timestamps and caller-supplied freshness thresholds
// output: HealthObservation, HealthFreshness, threshold lookup, and conservative effective health
// pos: Pure domain contract for observed resource health, freshness, and effective status
// note: if this file changes, update this header and module README.md.
package model

import "time"

type HealthFreshness string

const (
	HealthFreshnessFresh            HealthFreshness = "fresh"
	HealthFreshnessStale            HealthFreshness = "stale"
	HealthFreshnessNever            HealthFreshness = "never"
	DefaultHealthFreshnessThreshold                 = 24 * time.Hour
)

type HealthFreshnessThresholds map[ResourceType]time.Duration

type HealthObservation struct {
	Status     HealthStatus `json:"status"`
	ObservedAt time.Time    `json:"observedAt"`
	Observer   string       `json:"observer"`
}

func (o HealthObservation) FreshnessAt(now time.Time, threshold time.Duration) HealthFreshness {
	if o.ObservedAt.IsZero() {
		return HealthFreshnessNever
	}
	if o.ObservedAt.Before(now.Add(-threshold)) {
		return HealthFreshnessStale
	}
	return HealthFreshnessFresh
}

func (t HealthFreshnessThresholds) For(resourceType ResourceType) time.Duration {
	if threshold := t[resourceType]; threshold > 0 {
		return threshold
	}
	return DefaultHealthFreshnessThreshold
}

// ResolveHealth selects the worst fresh observation. With no fresh
// observation it fails closed to unknown; an override can only make the
// effective status worse, never hide stale or missing evidence as healthy.
func ResolveHealth(now time.Time, threshold time.Duration, manualOverride *HealthStatus, observations []HealthObservation) (HealthStatus, HealthFreshness, *time.Time, string) {
	var selected *HealthObservation
	for i := range observations {
		observation := &observations[i]
		if observation.FreshnessAt(now, threshold) != HealthFreshnessFresh {
			continue
		}
		if selected == nil || healthStatusRank(observation.Status) > healthStatusRank(selected.Status) ||
			(healthStatusRank(observation.Status) == healthStatusRank(selected.Status) && observation.ObservedAt.After(selected.ObservedAt)) {
			selected = observation
		}
	}

	status := HealthStatusUnknown
	freshness := HealthFreshnessNever
	if selected != nil {
		status = selected.Status
		freshness = HealthFreshnessFresh
	} else if len(observations) > 0 {
		freshness = HealthFreshnessStale
		for i := range observations {
			if selected == nil || observations[i].ObservedAt.After(selected.ObservedAt) {
				selected = &observations[i]
			}
		}
	}
	if manualOverride != nil && healthStatusRank(*manualOverride) > healthStatusRank(status) {
		status = *manualOverride
	}
	if selected == nil {
		return status, freshness, nil, ""
	}
	observedAt := selected.ObservedAt
	return status, freshness, &observedAt, selected.Observer
}

func healthStatusRank(status HealthStatus) int {
	switch status {
	case HealthStatusCritical:
		return 3
	case HealthStatusWarning:
		return 2
	case HealthStatusUnknown:
		return 1
	default:
		return 0
	}
}
