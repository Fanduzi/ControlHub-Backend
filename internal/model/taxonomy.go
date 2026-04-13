package model

import "fmt"

type RelationType string

const (
	ResourceTypeHost                  ResourceType = "host"
	ResourceTypeDatabaseInstance      ResourceType = "database_instance"
	ResourceTypeDatabaseCluster       ResourceType = "database_cluster"
	ResourceTypeService               ResourceType = "service"
	ResourceTypeDomainName            ResourceType = "domain_name"
	ResourceTypeVirtualIP             ResourceType = "virtual_ip"
	ResourceTypeDatabaseProxy         ResourceType = "database_proxy"
	ResourceTypeControlPlaneComponent ResourceType = "control_plane_component"
)

const (
	RelationTypeDependsOn    RelationType = "depends_on"
	RelationTypeMemberOf     RelationType = "member_of"
	RelationTypeRunsOn       RelationType = "runs_on"
	RelationTypePointsTo     RelationType = "points_to"
	RelationTypeFronts       RelationType = "fronts"
	RelationTypeManages      RelationType = "manages"
	RelationTypeReplicatesTo RelationType = "replicates_to"
)

var resourceTypeDictionaryItems = []DictionaryItem{
	{Key: string(ResourceTypeHost), Label: "Host", Description: "Infrastructure carrier resource for workloads or data services."},
	{Key: string(ResourceTypeDatabaseInstance), Label: "Database Instance", Description: "Running database node or instance with engine-specific identity."},
	{Key: string(ResourceTypeDatabaseCluster), Label: "Database Cluster", Description: "Logical database service boundary that groups related instances."},
	{Key: string(ResourceTypeService), Label: "Service", Description: "Application or platform service that depends on infrastructure or data resources."},
	{Key: string(ResourceTypeDomainName), Label: "Domain Name", Description: "DNS name resource used as a routable or discoverable entry point."},
	{Key: string(ResourceTypeVirtualIP), Label: "Virtual IP", Description: "Virtual or floating IP resource exposed independently from a host."},
	{Key: string(ResourceTypeDatabaseProxy), Label: "Database Proxy", Description: "Database-facing proxy layer such as ProxySQL or similar routing components."},
	{Key: string(ResourceTypeControlPlaneComponent), Label: "Control Plane Component", Description: "Control or HA management component such as orchestrators or schedulers."},
}

var relationTypeDictionaryItems = []DictionaryItem{
	{Key: string(RelationTypeDependsOn), Label: "Depends On", Description: "The source resource functionally depends on the target resource."},
	{Key: string(RelationTypeMemberOf), Label: "Member Of", Description: "The source resource belongs to the target logical grouping."},
	{Key: string(RelationTypeRunsOn), Label: "Runs On", Description: "The source resource is carried or hosted by the target resource."},
	{Key: string(RelationTypePointsTo), Label: "Points To", Description: "The source resource resolves or routes traffic to the target resource."},
	{Key: string(RelationTypeFronts), Label: "Fronts", Description: "The source resource fronts the target resource as an entry or proxy layer."},
	{Key: string(RelationTypeManages), Label: "Manages", Description: "The source resource manages or orchestrates the target resource."},
	{Key: string(RelationTypeReplicatesTo), Label: "Replicates To", Description: "The source resource replicates data or state to the target resource."},
}

func ResourceTypeDictionary() []DictionaryItem {
	return cloneDictionaryItems(resourceTypeDictionaryItems)
}

func RelationTypeDictionary() []DictionaryItem {
	return cloneDictionaryItems(relationTypeDictionaryItems)
}

func (r ResourceType) Validate() error {
	for _, item := range resourceTypeDictionaryItems {
		if item.Key == string(r) {
			return nil
		}
	}
	return fmt.Errorf("invalid resource type: %s", r)
}

func (r RelationType) Validate() error {
	for _, item := range relationTypeDictionaryItems {
		if item.Key == string(r) {
			return nil
		}
	}
	return fmt.Errorf("invalid relation type: %s", r)
}

func cloneDictionaryItems(items []DictionaryItem) []DictionaryItem {
	cloned := make([]DictionaryItem, len(items))
	copy(cloned, items)
	return cloned
}
