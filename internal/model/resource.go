// Package model provides domain entities for the resource management system.
// input: errors, time packages
// output: Resource struct with governed identity and health evidence, Completeness, ResourceProfileResponse, ResourceType and identity types
// pos: Core domain entity for the resource management system
// note: if this file changes, update this header and module README.md.
package model

import (
	"errors"
	"time"
)

type ResourceType string

type ResourceOrigin string

const (
	ResourceOriginManual     ResourceOrigin = "manual"
	ResourceOriginImported   ResourceOrigin = "imported"
	ResourceOriginDiscovered ResourceOrigin = "discovered"
)

func (o ResourceOrigin) Validate() error {
	switch o {
	case ResourceOriginManual, ResourceOriginImported, ResourceOriginDiscovered:
		return nil
	default:
		return errors.New("origin is not supported")
	}
}

type ResourceExternalIdentifier struct {
	System string `json:"system"`
	Value  string `json:"value"`
}

// Completeness is a server-derived read-only inventory-quality projection.
type Completeness struct {
	Score               int      `json:"score"`
	Status              string   `json:"status"`
	MissingRequirements []string `json:"missingRequirements"`
}

// ResourceProfileResponse is the read-only projection returned by
// GET /resources/{id}/profile. Profile keys vary by resource type.
type ResourceProfileResponse struct {
	ResourceID      uint64         `json:"resourceId"`
	ResourceType    ResourceType   `json:"resourceType"`
	ResourceSubtype string         `json:"resourceSubtype"`
	Profile         map[string]any `json:"profile"`
}

type Resource struct {
	ID                   uint64                       `json:"id"`
	ResourceType         ResourceType                 `json:"resourceType"`
	ResourceSubtype      string                       `json:"resourceSubtype"`
	Name                 string                       `json:"name"`
	DisplayName          string                       `json:"displayName"`
	EnvironmentID        uint64                       `json:"environmentId"`
	OwnerID              uint64                       `json:"ownerId"`
	LifecycleStatus      string                       `json:"lifecycleStatus"`
	HealthStatus         string                       `json:"healthStatus"`
	HealthFreshness      HealthFreshness              `json:"healthFreshness"`
	HealthObservedAt     *time.Time                   `json:"healthObservedAt"`
	HealthObserver       string                       `json:"healthObserver"`
	ManualHealthOverride *HealthStatus                `json:"manualHealthOverride"`
	Origin               ResourceOrigin               `json:"origin"`
	Aliases              []string                     `json:"aliases"`
	ExternalIdentifiers  []ResourceExternalIdentifier `json:"externalIdentifiers"`
	// Source and ExternalID keep internal callers compiling during the API
	// transition; new clients use Origin and ExternalIdentifiers.
	Source                     string                      `json:"source,omitempty"`
	ExternalID                 string                      `json:"externalId,omitempty"`
	Labels                     map[string]string           `json:"labels"`
	ProfileSummary             *ProfileSummary             `json:"profileSummary,omitempty"`
	DatabaseOperationalSummary *DatabaseOperationalSummary `json:"databaseOperationalSummary,omitempty"`
	ClusterId                  *uint64                     `json:"clusterId,omitempty"`
	CreatedAt                  time.Time                   `json:"createdAt"`
	UpdatedAt                  time.Time                   `json:"updatedAt"`
	ArchivedAt                 *time.Time                  `json:"archivedAt,omitempty"`
	ArchivedBy                 *uint64                     `json:"archivedBy,omitempty"`
	ArchiveReason              *string                     `json:"archiveReason,omitempty"`
}

type ProfileSummary struct {
	Hostname  string `json:"hostname,omitempty"`
	IP        string `json:"ip,omitempty"`
	Port      int    `json:"port,omitempty"`
	NodeCount int    `json:"nodeCount,omitempty"`
	Engine    string `json:"engine,omitempty"`
	Version   string `json:"version,omitempty"`
	Role      string `json:"role,omitempty"`
}

// DatabaseOperationalSummary is a derived read-only rollup of database cluster
// member health for operator list views. Populated for database_cluster resources.
type DatabaseOperationalSummary struct {
	MemberCount         int64  `json:"memberCount"`
	CriticalMemberCount int64  `json:"criticalMemberCount"`
	WarningMemberCount  int64  `json:"warningMemberCount"`
	StoppedMemberCount  int64  `json:"stoppedMemberCount"`
	DegradedMemberCount int64  `json:"degradedMemberCount"`
	UnknownRoleCount    int64  `json:"unknownRoleCount"`
	PrimaryMemberCount  int64  `json:"primaryMemberCount"`
	ReplicaMemberCount  int64  `json:"replicaMemberCount"`
	WorstMemberID       *int64 `json:"worstMemberId,omitempty"`
	WorstMemberName     string `json:"worstMemberName,omitempty"`
	WorstMemberStatus   string `json:"worstMemberStatus,omitempty"`
}

// ResourceRelationView extends ResourceRelation with resolved related resource metadata.
type ResourceRelationView struct {
	ID                           uint64       `json:"id"`
	FromResourceID               uint64       `json:"fromResourceId"`
	ToResourceID                 uint64       `json:"toResourceId"`
	RelationType                 RelationType `json:"relationType"`
	Direction                    string       `json:"direction"`
	CreatedAt                    time.Time    `json:"createdAt"`
	RelatedResourceID            uint64       `json:"relatedResourceId"`
	RelatedResourceName          string       `json:"relatedResourceName"`
	RelatedResourceDisplayName   string       `json:"relatedResourceDisplayName"`
	RelatedResourceType          string       `json:"relatedResourceType"`
	RelatedResourceSubtype       string       `json:"relatedResourceSubtype"`
	RelatedResourceHealthStatus  string       `json:"relatedResourceHealthStatus"`
	RelatedResourceLifecycleStat string       `json:"relatedResourceLifecycleStatus"`
}

// ClusterMemberView represents a database instance that is a member of a database cluster.
type ClusterMemberView struct {
	ResourceID      uint64          `json:"resourceId"`
	Name            string          `json:"name"`
	DisplayName     string          `json:"displayName"`
	ResourceType    string          `json:"resourceType"`
	ResourceSubtype string          `json:"resourceSubtype"`
	LifecycleStatus string          `json:"lifecycleStatus"`
	HealthStatus    string          `json:"healthStatus"`
	ProfileSummary  *ProfileSummary `json:"profileSummary,omitempty"`
}

// IsArchived returns true if the resource has been archived.
func (r *Resource) IsArchived() bool {
	return r.ArchivedAt != nil
}
