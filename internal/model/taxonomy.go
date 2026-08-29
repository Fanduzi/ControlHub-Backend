// Package model provides domain entities for the resource management system.
// input: fmt package
// output: All type constants, XxxDictionary() functions, Validate() methods
// pos: Central taxonomy registry — single source of truth for all enum-like types
// note: if this file changes, update header and README.md
package model

import "fmt"

type LifecycleStatus string

type HealthStatus string

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
	LifecycleStatusProvisioning    LifecycleStatus = "provisioning"
	LifecycleStatusRunning         LifecycleStatus = "running"
	LifecycleStatusStopped         LifecycleStatus = "stopped"
	LifecycleStatusDegraded        LifecycleStatus = "degraded"
	LifecycleStatusDecommissioning LifecycleStatus = "decommissioning"
)

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusWarning  HealthStatus = "warning"
	HealthStatusCritical HealthStatus = "critical"
	HealthStatusUnknown  HealthStatus = "unknown"
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

var lifecycleStatusDictionaryItems = []DictionaryItem{
	{Key: string(LifecycleStatusProvisioning), Label: "Provisioning", Description: "Resource is being created or configured."},
	{Key: string(LifecycleStatusRunning), Label: "Running", Description: "Resource is active and serving expected workload."},
	{Key: string(LifecycleStatusStopped), Label: "Stopped", Description: "Resource is provisioned but currently inactive."},
	{Key: string(LifecycleStatusDegraded), Label: "Degraded", Description: "Resource is operating below expected capacity."},
	{Key: string(LifecycleStatusDecommissioning), Label: "Decommissioning", Description: "Resource is being retired or removed."},
}

var healthStatusDictionaryItems = []DictionaryItem{
	{Key: string(HealthStatusHealthy), Label: "Healthy", Description: "Resource is functioning normally with no issues."},
	{Key: string(HealthStatusWarning), Label: "Warning", Description: "Resource has minor issues that may need attention."},
	{Key: string(HealthStatusCritical), Label: "Critical", Description: "Resource has significant issues requiring immediate action."},
	{Key: string(HealthStatusUnknown), Label: "Unknown", Description: "Health status cannot be determined."},
}

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

// resourceSubtypeMap defines valid subtypes per resource type.
// Types not present have no subtypes (subtype field is ignored).
var resourceSubtypeMap = map[string][]DictionaryItem{
	string(ResourceTypeDatabaseInstance): {
		{Key: "mysql", Label: "MySQL"},
		{Key: "postgresql", Label: "PostgreSQL"},
		{Key: "redis", Label: "Redis"},
		{Key: "clickhouse", Label: "ClickHouse"},
		{Key: "mongodb", Label: "MongoDB"},
		{Key: "tidb", Label: "TiDB"},
	},
	string(ResourceTypeDatabaseCluster): {
		{Key: "mysql", Label: "MySQL"},
		{Key: "postgresql", Label: "PostgreSQL"},
		{Key: "redis", Label: "Redis"},
		{Key: "clickhouse", Label: "ClickHouse"},
		{Key: "mongodb", Label: "MongoDB"},
		{Key: "tidb", Label: "TiDB"},
	},
	string(ResourceTypeDatabaseProxy): {
		{Key: "proxysql", Label: "ProxySQL"},
		{Key: "chproxy", Label: "CHProxy"},
		{Key: "haproxy", Label: "HAProxy"},
		{Key: "maxscale", Label: "MaxScale"},
	},
	string(ResourceTypeHost): {
		{Key: "vm", Label: "Virtual Machine"},
		{Key: "physical", Label: "Physical Server"},
		{Key: "container", Label: "Container"},
	},
	string(ResourceTypeService): {
		{Key: "api", Label: "API Service"},
		{Key: "web", Label: "Web Application"},
		{Key: "job", Label: "Batch Job"},
		{Key: "cron", Label: "Cron Job"},
	},
	string(ResourceTypeControlPlaneComponent): {
		{Key: "orchestrator", Label: "Orchestrator"},
		{Key: "ha_monitor", Label: "HA Monitor"},
		{Key: "backup_manager", Label: "Backup Manager"},
	},
	string(ResourceTypeDomainName): {
		{Key: "dns", Label: "DNS"},
	},
	string(ResourceTypeVirtualIP): {
		{Key: "floating", Label: "Floating"},
	},
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

func LifecycleStatusDictionary() []DictionaryItem {
	return cloneDictionaryItems(lifecycleStatusDictionaryItems)
}

func HealthStatusDictionary() []DictionaryItem {
	return cloneDictionaryItems(healthStatusDictionaryItems)
}

func ResourceTypeDictionary() []DictionaryItem {
	return cloneDictionaryItems(resourceTypeDictionaryItems)
}

// ResourceSubtypeDictionary returns the valid subtypes for a given resource type.
func ResourceSubtypeDictionary(resourceType string) []DictionaryItem {
	items, ok := resourceSubtypeMap[resourceType]
	if !ok {
		return nil
	}
	return cloneDictionaryItems(items)
}

// ValidateResourceSubtype validates that subtype is valid for the given resource type.
// If the resource type has no subtypes defined, any subtype is silently accepted (ignored).
func ValidateResourceSubtype(resourceType, subtype string) error {
	items, ok := resourceSubtypeMap[resourceType]
	if !ok {
		return nil // no subtypes defined for this type — ignore
	}
	if subtype == "" {
		return fmt.Errorf("resourceSubtype is required for %s", resourceType)
	}
	for _, item := range items {
		if item.Key == subtype {
			return nil
		}
	}
	return fmt.Errorf("invalid resourceSubtype %q for type %s", subtype, resourceType)
}

func RelationTypeDictionary() []DictionaryItem {
	return cloneDictionaryItems(relationTypeDictionaryItems)
}

func (l LifecycleStatus) Validate() error {
	for _, item := range lifecycleStatusDictionaryItems {
		if item.Key == string(l) {
			return nil
		}
	}
	return fmt.Errorf("invalid lifecycle status: %s", l)
}

func (h HealthStatus) Validate() error {
	for _, item := range healthStatusDictionaryItems {
		if item.Key == string(h) {
			return nil
		}
	}
	return fmt.Errorf("invalid health status: %s", h)
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
