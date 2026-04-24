// Package model provides write-side request and repository input types for resources.
// input: time package
// output: ResourceCreateInput, ResourcePatchRequest, ResourceUpdateInput, ArchiveRequest
// pos: Shared request contracts for resource write APIs
// note: if this file changes, update header and README.md
package model

import "time"

type ResourceCreateInput struct {
	ResourceType    ResourceType           `json:"resourceType"`
	ResourceSubtype string                 `json:"resourceSubtype"`
	Name            string                 `json:"name"`
	DisplayName     string                 `json:"displayName"`
	EnvironmentID   uint64                 `json:"environmentId"`
	OwnerID         uint64                 `json:"ownerId"`
	LifecycleStatus LifecycleStatus        `json:"lifecycleStatus"`
	HealthStatus    HealthStatus           `json:"healthStatus"`
	Source          string                 `json:"source"`
	ExternalID      string                 `json:"externalId"`
	Labels          map[string]string      `json:"labels"`
	Profile         map[string]interface{} `json:"profile,omitempty"`
}

type ResourcePatchRequest struct {
	ID              *uint64           `json:"id,omitempty"`
	ResourceType    *ResourceType     `json:"resourceType,omitempty"`
	Name            *string           `json:"name,omitempty"`
	CreatedAt       *time.Time        `json:"createdAt,omitempty"`
	ResourceSubtype *string           `json:"resourceSubtype,omitempty"`
	DisplayName     *string           `json:"displayName,omitempty"`
	EnvironmentID   *uint64           `json:"environmentId,omitempty"`
	OwnerID         *uint64           `json:"ownerId,omitempty"`
	LifecycleStatus *LifecycleStatus  `json:"lifecycleStatus,omitempty"`
	HealthStatus    *HealthStatus     `json:"healthStatus,omitempty"`
	Source          *string           `json:"source,omitempty"`
	ExternalID      *string           `json:"externalId,omitempty"`
	Labels          *map[string]string `json:"labels,omitempty"`
}

type ResourceUpdateInput struct {
	Name            *string
	ResourceSubtype *string
	DisplayName     *string
	EnvironmentID   *uint64
	OwnerID         *uint64
	LifecycleStatus *LifecycleStatus
	HealthStatus    *HealthStatus
	Source          *string
	ExternalID      *string
	Labels          *map[string]string
}

func (r ResourcePatchRequest) HasImmutableFields() bool {
	return r.ID != nil || r.ResourceType != nil || r.CreatedAt != nil
}

func (r ResourcePatchRequest) HasMutableFields() bool {
	return r.Name != nil || r.ResourceSubtype != nil || r.DisplayName != nil || r.EnvironmentID != nil ||
		r.OwnerID != nil || r.LifecycleStatus != nil || r.HealthStatus != nil ||
		r.Source != nil || r.ExternalID != nil || r.Labels != nil
}

func (r ResourcePatchRequest) ToUpdateInput() ResourceUpdateInput {
	return ResourceUpdateInput{
		Name:            r.Name,
		ResourceSubtype: r.ResourceSubtype,
		DisplayName:     r.DisplayName,
		EnvironmentID:   r.EnvironmentID,
		OwnerID:         r.OwnerID,
		LifecycleStatus: r.LifecycleStatus,
		HealthStatus:    r.HealthStatus,
		Source:          r.Source,
		ExternalID:      r.ExternalID,
		Labels:          r.Labels,
	}
}

// ArchiveRequest is the request body for POST /resources/{id}/archive.
type ArchiveRequest struct {
	Reason *string `json:"reason,omitempty"`
}
