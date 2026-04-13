package model

import "time"

type ResourceType string

// ResourceProfileResponse is the read-only projection returned by
// GET /resources/{id}/profile. Profile keys vary by resource type.
type ResourceProfileResponse struct {
	ResourceID      string         `json:"resourceId"`
	ResourceType    ResourceType   `json:"resourceType"`
	ResourceSubtype string         `json:"resourceSubtype"`
	Profile         map[string]any `json:"profile"`
}

type Resource struct {
	ID              string            `json:"id"`
	ResourceType    ResourceType      `json:"resourceType"`
	ResourceSubtype string            `json:"resourceSubtype"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"displayName"`
	EnvironmentID   string            `json:"environmentId"`
	OwnerID         string            `json:"ownerId"`
	LifecycleStatus string            `json:"lifecycleStatus"`
	HealthStatus    string            `json:"healthStatus"`
	Source          string            `json:"source"`
	ExternalID      string            `json:"externalId"`
	Labels          map[string]string `json:"labels"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}
