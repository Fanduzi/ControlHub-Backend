// Package model provides write-side request and repository input types for resources.
// input: bytes, encoding/json, and time packages
// output: resource create/update inputs with governed identity and explicit nullable manual health override writes
// pos: Shared request contracts for resource write APIs
// note: if this file changes, update this header and module README.md.
package model

import (
	"bytes"
	"encoding/json"
	"time"
)

type ResourceCreateInput struct {
	ResourceType        ResourceType                 `json:"resourceType"`
	ResourceSubtype     string                       `json:"resourceSubtype"`
	Name                string                       `json:"name"`
	DisplayName         string                       `json:"displayName"`
	EnvironmentID       uint64                       `json:"environmentId"`
	OwnerID             uint64                       `json:"ownerId"`
	LifecycleStatus     LifecycleStatus              `json:"lifecycleStatus"`
	HealthStatus        HealthStatus                 `json:"healthStatus"`
	Origin              ResourceOrigin               `json:"origin"`
	Aliases             []string                     `json:"aliases,omitempty"`
	ExternalIdentifiers []ResourceExternalIdentifier `json:"externalIdentifiers,omitempty"`
	Source              string                       `json:"source"`
	ExternalID          string                       `json:"externalId"`
	Labels              map[string]string            `json:"labels"`
	Profile             map[string]interface{}       `json:"profile,omitempty"`
}

type ResourcePatchRequest struct {
	ID                  *uint64                       `json:"id,omitempty"`
	ResourceType        *ResourceType                 `json:"resourceType,omitempty"`
	Name                *string                       `json:"name,omitempty"`
	CreatedAt           *time.Time                    `json:"createdAt,omitempty"`
	ResourceSubtype     *string                       `json:"resourceSubtype,omitempty"`
	DisplayName         *string                       `json:"displayName,omitempty"`
	EnvironmentID       *uint64                       `json:"environmentId,omitempty"`
	OwnerID             *uint64                       `json:"ownerId,omitempty"`
	LifecycleStatus     *LifecycleStatus              `json:"lifecycleStatus,omitempty"`
	HealthStatus        *HealthStatus                 `json:"healthStatus,omitempty"`
	ClearHealthStatus   bool                          `json:"-"`
	Origin              *ResourceOrigin               `json:"origin,omitempty"`
	Aliases             *[]string                     `json:"aliases,omitempty"`
	ExternalIdentifiers *[]ResourceExternalIdentifier `json:"externalIdentifiers,omitempty"`
	Source              *string                       `json:"source,omitempty"`
	ExternalID          *string                       `json:"externalId,omitempty"`
	Labels              *map[string]string            `json:"labels,omitempty"`
}

type ResourceUpdateInput struct {
	Name                *string
	ResourceSubtype     *string
	DisplayName         *string
	EnvironmentID       *uint64
	OwnerID             *uint64
	LifecycleStatus     *LifecycleStatus
	HealthStatus        *HealthStatus
	ClearHealthStatus   bool
	Aliases             *[]string
	ExternalIdentifiers *[]ResourceExternalIdentifier
	Source              *string
	ExternalID          *string
	Labels              *map[string]string
}

func (r ResourcePatchRequest) HasImmutableFields() bool {
	return r.ID != nil || r.ResourceType != nil || r.CreatedAt != nil || r.Origin != nil || r.Source != nil
}

func (r ResourcePatchRequest) HasMutableFields() bool {
	return r.Name != nil || r.ResourceSubtype != nil || r.DisplayName != nil || r.EnvironmentID != nil ||
		r.OwnerID != nil || r.LifecycleStatus != nil || r.HealthStatus != nil || r.ClearHealthStatus ||
		r.Aliases != nil || r.ExternalIdentifiers != nil || r.ExternalID != nil || r.Labels != nil
}

func (r ResourcePatchRequest) ToUpdateInput() ResourceUpdateInput {
	return ResourceUpdateInput{
		Name:                r.Name,
		ResourceSubtype:     r.ResourceSubtype,
		DisplayName:         r.DisplayName,
		EnvironmentID:       r.EnvironmentID,
		OwnerID:             r.OwnerID,
		LifecycleStatus:     r.LifecycleStatus,
		HealthStatus:        r.HealthStatus,
		ClearHealthStatus:   r.ClearHealthStatus,
		Aliases:             r.Aliases,
		ExternalIdentifiers: r.ExternalIdentifiers,
		ExternalID:          r.ExternalID,
		Labels:              r.Labels,
	}
}

func (r *ResourcePatchRequest) UnmarshalJSON(data []byte) error {
	type plain ResourcePatchRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["healthStatus"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		decoded.ClearHealthStatus = true
	}
	*r = ResourcePatchRequest(decoded)
	return nil
}

const MaxArchiveReasonLength = 512

// ArchiveRequest is the request body for POST /resources/{id}/archive.
type ArchiveRequest struct {
	Reason *string `json:"reason,omitempty"`
}
