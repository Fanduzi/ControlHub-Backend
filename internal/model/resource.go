package model

import (
	"fmt"
	"time"
)

type ResourceType string

const (
	ResourceTypeHost             ResourceType = "host"
	ResourceTypeDatabaseInstance ResourceType = "database_instance"
	ResourceTypeDatabaseCluster  ResourceType = "database_cluster"
	ResourceTypeService          ResourceType = "service"
)

func (r ResourceType) Validate() error {
	switch r {
	case ResourceTypeHost, ResourceTypeDatabaseInstance, ResourceTypeDatabaseCluster, ResourceTypeService:
		return nil
	default:
		return fmt.Errorf("invalid resource type: %s", r)
	}
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
