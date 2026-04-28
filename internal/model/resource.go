// Package model provides domain entities for the resource management system.
// input: time package
// output: Resource struct, ResourceProfileResponse struct, ResourceType type
// pos: Core domain entity for the resource management system
// note: if this file changes, update header and README.md
package model

import "time"

type ResourceType string

// ResourceProfileResponse is the read-only projection returned by
// GET /resources/{id}/profile. Profile keys vary by resource type.
type ResourceProfileResponse struct {
	ResourceID      uint64         `json:"resourceId"`
	ResourceType    ResourceType   `json:"resourceType"`
	ResourceSubtype string         `json:"resourceSubtype"`
	Profile         map[string]any `json:"profile"`
}

type Resource struct {
	ID              uint64            `json:"id"`
	ResourceType    ResourceType      `json:"resourceType"`
	ResourceSubtype string            `json:"resourceSubtype"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"displayName"`
	EnvironmentID   uint64            `json:"environmentId"`
	OwnerID         uint64            `json:"ownerId"`
	LifecycleStatus string            `json:"lifecycleStatus"`
	HealthStatus    string            `json:"healthStatus"`
	Source          string            `json:"source"`
	ExternalID      string            `json:"externalId"`
	Labels          map[string]string `json:"labels"`
	ProfileSummary  *ProfileSummary   `json:"profileSummary,omitempty"`
	ClusterId       *uint64           `json:"clusterId,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	ArchivedAt      *time.Time        `json:"archivedAt,omitempty"`
	ArchivedBy      *uint64           `json:"archivedBy,omitempty"`
	ArchiveReason   *string           `json:"archiveReason,omitempty"`
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
