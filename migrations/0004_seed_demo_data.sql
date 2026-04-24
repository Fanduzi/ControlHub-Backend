-- ControlHub comprehensive demo data for frontend integration testing.

-- +goose Up
-- ============================================================
-- Section 1: Reference Data Expansion
-- ============================================================

-- +goose StatementBegin
insert ignore into environments (name, slug, description) values
  ('Development', 'dev', 'Development and testing environment');
-- +goose StatementEnd

-- +goose StatementBegin
insert ignore into owners (name, email) values
  ('Order Team', 'order-team@example.com'),
  ('Payment Team', 'payment-team@example.com'),
  ('Analytics Team', 'analytics-team@example.com');
-- +goose StatementEnd

-- ============================================================
-- Section 2: Resources
-- ============================================================

-- +goose StatementBegin
insert into resources (
  resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
)
select
  seed.resource_type,
  seed.resource_subtype,
  seed.name,
  seed.display_name,
  environments.id,
  owners.id,
  seed.lifecycle_status,
  seed.health_status,
  seed.labels,
  seed.source,
  seed.external_id
from (
  select 'host' as resource_type, 'vm' as resource_subtype, 'prod-db-host-02' as name, 'Production DB Host 02' as display_name, 'prod' as environment_slug, 'platform@example.com' as owner_email, 'running' as lifecycle_status, 'healthy' as health_status, '{"team":"platform","tier":"infra","rack":"B12"}' as labels, 'manual' as source, 'vmware-prod-db-host-02' as external_id
  union all select 'host', 'vm', 'prod-db-host-03', 'Production DB Host 03', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra","rack":"B13"}', 'manual', 'vmware-prod-db-host-03'
  union all select 'host', 'vm', 'prod-db-host-04', 'Production DB Host 04', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra","rack":"B14"}', 'manual', ''
  union all select 'host', 'vm', 'prod-app-host-01', 'Production Application Host 01', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"compute"}', 'manual', ''
  union all select 'host', 'vm', 'prod-app-host-02', 'Production Application Host 02', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"compute"}', 'terraform', ''
  union all select 'host', 'physical', 'prod-ch-host-01', 'ClickHouse Host 01 (Production)', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra","purpose":"analytics","disk_type":"nvme"}', 'discovery', 'bare-prod-ch-host-01'
  union all select 'host', 'physical', 'prod-ch-host-02', 'ClickHouse Host 02 (Production)', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra","purpose":"analytics","disk_type":"nvme"}', 'discovery', 'bare-prod-ch-host-02'
  union all select 'database_cluster', 'mysql', 'payment-mysql-cluster-prod', 'Payment MySQL Cluster Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"payment","tier":"data","critical":"true","pci_scope":"yes"}', 'manual', 'dbaas-payment-mysql-cluster-prod'
  union all select 'database_cluster', 'redis', 'user-redis-cluster-prod', 'User Session Redis Cluster Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"cache"}', 'manual', 'dbaas-user-redis-cluster-prod'
  union all select 'database_cluster', 'clickhouse', 'analytics-ch-cluster-prod', 'Analytics ClickHouse Cluster Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"analytics","tier":"data","engine":"clickhouse"}', 'manual', 'dbaas-analytics-ch-cluster-prod'
  union all select 'database_cluster', 'mysql', 'config-mysql-cluster-prod', 'Platform Config Service MySQL Cluster Production', 'prod', 'dba@example.com', 'degraded', 'warning', '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-mysql-replica-01-prod', 'Order MySQL Replica 01 Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"data","replication_lag_seconds":"0"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-mysql-replica-02-prod', 'Order MySQL Replica 02 Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'payment-mysql-primary-prod', 'Payment MySQL Primary Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"payment","tier":"data","critical":"true"}', 'manual', 'dbaas-payment-mysql-primary-prod-inst'
  union all select 'database_instance', 'mysql', 'payment-mysql-replica-01-prod', 'Payment MySQL Replica 01', 'prod', 'dba@example.com', 'running', 'warning', '{"team":"payment","tier":"data","replication_lag_seconds":"45","alert":"replication-lag"}', 'manual', ''
  union all select 'database_instance', 'redis', 'user-redis-primary-prod', 'User Redis Primary Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"cache","memory_max_gb":"32"}', 'manual', ''
  union all select 'database_instance', 'redis', 'user-redis-replica-01-prod', 'User Redis Replica 01 Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"cache"}', 'manual', ''
  union all select 'database_instance', 'clickhouse', 'analytics-ch-node-01-prod', 'Analytics ClickHouse Node 01 Production', 'prod', 'dba@example.com', 'running', 'healthy', '{"team":"analytics","tier":"data","clickhouse_role":"replica"}', 'manual', ''
  union all select 'database_instance', 'clickhouse', 'analytics-ch-node-02-prod', 'Analytics ClickHouse Node 02', 'prod', 'dba@example.com', 'running', 'critical', '{"team":"analytics","tier":"data","clickhouse_role":"replica","disk_usage_pct":"94","alert":"disk-pressure"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'config-mysql-primary-prod', 'Config Service MySQL Primary Production', 'prod', 'dba@example.com', 'running', 'warning', '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'config-mysql-replica-01-prod', 'Config Service MySQL Replica 01 Production', 'prod', 'dba@example.com', 'running', 'warning', '{"team":"platform","tier":"data","replication_lag_seconds":"12"}', 'manual', ''
  union all select 'service', 'api', 'payment-api-prod', 'Payment Processing API Service Production', 'prod', 'payment-team@example.com', 'running', 'healthy', '{"team":"payment","tier":"app","runtime":"go","port":"8080"}', 'manual', ''
  union all select 'service', 'api', 'user-api-prod', 'User Management API Service Production', 'prod', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"app","runtime":"java","port":"8080"}', 'manual', ''
  union all select 'service', 'worker', 'analytics-service-prod', 'Analytics Data Pipeline Worker Service Production', 'prod', 'analytics-team@example.com', 'running', 'healthy', '{"team":"analytics","tier":"app","runtime":"python"}', 'manual', ''
  union all select 'service', 'api', 'config-service-prod', 'Platform Configuration Service Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"app"}', 'manual', ''
  union all select 'service', 'worker', 'notification-service-prod', 'Notification Delivery Service', 'prod', 'order-team@example.com', 'stopped', 'unknown', '{"team":"order","tier":"app","runtime":"node","disabled_reason":"kafka-migration"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'order-proxysql-prod', 'Order MySQL ProxySQL Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"proxy","backend":"mysql"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'payment-proxysql-prod', 'Payment MySQL ProxySQL Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"proxy","backend":"mysql","pci_scope":"yes"}', 'manual', ''
  union all select 'database_proxy', 'chproxy', 'analytics-ch-proxy-prod', 'Analytics ClickHouse Proxy Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"proxy","backend":"clickhouse"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'config-proxysql-prod', 'Config Service ProxySQL Production', 'prod', 'platform@example.com', 'degraded', 'warning', '{"team":"platform","tier":"proxy","backend":"mysql"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'payment-proxysql-02-prod', 'Payment ProxySQL Standby', 'prod', 'platform@example.com', 'stopped', 'unknown', '{"team":"platform","tier":"proxy","backend":"mysql","pci_scope":"yes","role":"standby"}', 'manual', ''
  union all select 'virtual_ip', 'floating', 'order-vip-prod', 'Order Service Virtual IP Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"network","ip":"10.0.10.100"}', 'manual', ''
  union all select 'virtual_ip', 'floating', 'payment-vip-prod', 'Payment Service Virtual IP Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"network","ip":"10.0.10.101"}', 'manual', ''
  union all select 'virtual_ip', 'floating', 'analytics-vip-prod', 'Analytics Service Virtual IP Production', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"network","ip":"10.0.10.102"}', 'manual', ''
  union all select 'domain_name', 'dns', 'api.order.internal', 'Order Service API Endpoint', 'prod', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"network","zone":"internal"}', 'manual', ''
  union all select 'domain_name', 'dns', 'api.payment.internal', 'Payment Service API Endpoint', 'prod', 'payment-team@example.com', 'running', 'healthy', '{"team":"payment","tier":"network","zone":"internal"}', 'manual', ''
  union all select 'domain_name', 'dns', 'api.user.internal', 'User Service Internal API Domain', 'prod', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"network"}', 'manual', ''
  union all select 'domain_name', 'dns', 'analytics.internal', 'Analytics Platform Internal Domain', 'prod', 'analytics-team@example.com', 'running', 'healthy', '{"team":"analytics","tier":"network"}', 'manual', ''
  union all select 'control_plane_component', 'orchestrator', 'db-orchestrator-prod', 'Database Orchestrator - Production Control Plane', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"control-plane","managed_clusters":"4"}', 'manual', ''
  union all select 'control_plane_component', 'ha', 'ha-manager-prod', 'High Availability Manager - Production Control Plane', 'prod', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"control-plane"}', 'manual', ''
  union all select 'host', 'vm', 'staging-db-host-01', 'Staging DB Host 01', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra"}', 'manual', ''
  union all select 'host', 'vm', 'staging-db-host-02', 'Staging DB Host 02', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra"}', 'manual', ''
  union all select 'host', 'vm', 'staging-app-host-01', 'Staging Application Host 01', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"compute"}', 'manual', ''
  union all select 'database_cluster', 'mysql', 'order-mysql-cluster-staging', 'Order MySQL Cluster Staging', 'staging', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_cluster', 'mysql', 'payment-mysql-cluster-staging', 'Payment MySQL Cluster Staging', 'staging', 'dba@example.com', 'running', 'healthy', '{"team":"payment","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-mysql-primary-staging', 'Order MySQL Primary Staging', 'staging', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-mysql-replica-staging', 'Order MySQL Replica Staging', 'staging', 'dba@example.com', 'running', 'healthy', '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'payment-mysql-primary-staging', 'Payment MySQL Primary Staging', 'staging', 'dba@example.com', 'provisioning', 'healthy', '{"team":"payment","tier":"data"}', 'manual', ''
  union all select 'service', 'api', 'order-api-staging', 'Order API Staging', 'staging', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"app"}', 'manual', ''
  union all select 'service', 'api', 'payment-api-staging', 'Payment API Staging', 'staging', 'payment-team@example.com', 'running', 'healthy', '{"team":"payment","tier":"app"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'order-proxysql-staging', 'Order ProxySQL Staging', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"proxy"}', 'manual', ''
  union all select 'virtual_ip', 'floating', 'order-vip-staging', 'Order Service Virtual IP Staging', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"network","ip":"10.1.10.100"}', 'manual', ''
  union all select 'domain_name', 'dns', 'staging.order.internal', 'Order Service Staging Internal Domain', 'staging', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"network"}', 'manual', ''
  union all select 'control_plane_component', 'orchestrator', 'db-orchestrator-staging', 'Database Orchestrator - Staging Control Plane', 'staging', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"control-plane"}', 'manual', ''
  union all select 'host', 'vm', 'dev-db-host-01', 'Development DB Host 01', 'dev', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"infra"}', 'manual', ''
  union all select 'database_cluster', 'mysql', 'dev-mysql-cluster', 'Development MySQL Cluster', 'dev', 'dba@example.com', 'running', 'healthy', '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'dev-mysql-primary', 'Development MySQL Primary Instance', 'dev', 'dba@example.com', 'running', 'healthy', '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'service', 'api', 'order-api-dev', 'Order API Development', 'dev', 'order-team@example.com', 'running', 'healthy', '{"team":"order","tier":"app"}', 'manual', ''
  union all select 'database_proxy', 'proxysql', 'dev-proxysql', 'Development ProxySQL', 'dev', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"proxy"}', 'manual', ''
  union all select 'virtual_ip', 'floating', 'dev-vip', 'Development Virtual IP', 'dev', 'platform@example.com', 'running', 'healthy', '{"team":"platform","tier":"network","ip":"10.2.10.100"}', 'manual', ''
  union all select 'domain_name', 'dns', 'dev.order.internal', 'Order Service Development Internal Domain', 'dev', 'order-team@example.com', 'running', 'healthy', '{}', 'manual', ''
) seed
join environments on environments.slug = seed.environment_slug
join owners on owners.email = seed.owner_email;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec)
select resources.id, seed.hostname, seed.ip_address, seed.os_name, seed.spec
from (
  select 'prod-db-host-02' as resource_name, 'prod-db-host-02.internal' as hostname, '10.0.10.22' as ip_address, 'Ubuntu 24.04' as os_name, '{"provider":"vmware","cpu":16,"memory_gb":128}' as spec
  union all select 'prod-db-host-03', 'prod-db-host-03.internal', '10.0.10.23', 'Ubuntu 24.04', '{"provider":"vmware","cpu":16,"memory_gb":128}'
  union all select 'prod-db-host-04', 'prod-db-host-04.internal', '10.0.10.24', 'CentOS 9 Stream', '{"provider":"vmware","cpu":8,"memory_gb":64}'
  union all select 'prod-app-host-01', 'prod-app-host-01.internal', '10.0.20.10', 'Ubuntu 22.04', '{"provider":"vmware","cpu":8,"memory_gb":32,"kubernetes_node":"true"}'
  union all select 'prod-app-host-02', 'prod-app-host-02.internal', '10.0.20.11', 'Ubuntu 22.04', '{"provider":"vmware","cpu":8,"memory_gb":32,"kubernetes_node":"true"}'
  union all select 'prod-ch-host-01', 'prod-ch-host-01.internal', '10.0.30.10', 'Ubuntu 24.04', '{"provider":"bare-metal","cpu":32,"memory_gb":256,"disk_tb":8,"disk_type":"nvme"}'
  union all select 'prod-ch-host-02', 'prod-ch-host-02.internal', '10.0.30.11', 'Ubuntu 24.04', '{"provider":"bare-metal","cpu":32,"memory_gb":256,"disk_tb":8,"disk_type":"nvme"}'
  union all select 'staging-db-host-01', 'staging-db-host-01.internal', '10.1.10.20', 'Ubuntu 24.04', '{"provider":"vmware","cpu":4,"memory_gb":32}'
  union all select 'staging-db-host-02', 'staging-db-host-02.internal', '10.1.10.21', 'Ubuntu 24.04', '{"provider":"vmware","cpu":4,"memory_gb":32}'
  union all select 'staging-app-host-01', 'staging-app-host-01.internal', '10.1.20.10', 'Ubuntu 22.04', '{"cpu":4,"memory_gb":16}'
  union all select 'dev-db-host-01', 'dev-db-host-01.internal', '10.2.10.20', 'Ubuntu 24.04', '{"cpu":2,"memory_gb":8}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
select resources.id, seed.engine, seed.topology_mode, seed.primary_endpoint, seed.spec
from (
  select 'payment-mysql-cluster-prod' as resource_name, 'mysql' as engine, 'primary-replica' as topology_mode, 'payment-mysql-cluster-prod.internal:3306' as primary_endpoint, '{"replicas":1,"storage_class":"ssd","backup_enabled":true}' as spec
  union all select 'user-redis-cluster-prod', 'redis', 'cluster', 'user-redis-cluster-prod.internal:6379', '{"shards":3,"replicas_per_shard":1,"memory_max_gb":"32"}'
  union all select 'analytics-ch-cluster-prod', 'clickhouse', 'replicated', 'analytics-ch-cluster-prod.internal:8123', '{"replicas":2,"storage_gb":8000,"compression":"lz4"}'
  union all select 'config-mysql-cluster-prod', 'mysql', 'primary-replica', 'config-mysql-cluster-prod.internal:3306', '{}'
  union all select 'order-mysql-cluster-staging', 'mysql', 'primary-replica', 'order-mysql-cluster-staging.internal:3306', '{"replicas":1}'
  union all select 'payment-mysql-cluster-staging', 'mysql', 'primary-replica', 'payment-mysql-cluster-staging.internal:3306', '{}'
  union all select 'dev-mysql-cluster', 'mysql', 'single', 'dev-mysql-cluster.internal:3306', '{}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
select resources.id, seed.engine, seed.version, seed.host, seed.port, seed.role, seed.spec
from (
  select 'order-mysql-replica-01-prod' as resource_name, 'mysql' as engine, '8.0.36' as version, 'prod-db-host-02.internal' as host, 3306 as port, 'replica' as role, '{"storage_class":"ssd","delayed":false}' as spec
  union all select 'payment-mysql-primary-prod', 'mysql', '8.0.36', 'prod-db-host-02.internal', 3307, 'primary', '{"storage_class":"ssd","innodb_buffer_pool_gb":"64"}'
  union all select 'payment-mysql-replica-01-prod', 'mysql', '8.0.36', 'prod-db-host-03.internal', 3306, 'replica', '{"storage_class":"ssd"}'
  union all select 'user-redis-primary-prod', 'redis', '7.2.4', 'prod-db-host-03.internal', 6379, 'primary', '{"maxmemory_policy":"allkeys-lru"}'
  union all select 'user-redis-replica-01-prod', 'redis', '7.2.4', 'prod-db-host-04.internal', 6379, 'replica', '{}'
  union all select 'analytics-ch-node-01-prod', 'clickhouse', '24.3', 'prod-ch-host-01.internal', 8123, 'replica', '{"clickhouse_servers_version":"24.3.5.37"}'
  union all select 'analytics-ch-node-02-prod', 'clickhouse', '24.3', 'prod-ch-host-02.internal', 8123, 'replica', '{"clickhouse_servers_version":"24.3.5.37","disk_usage_pct":"94"}'
  union all select 'config-mysql-primary-prod', 'mysql', '8.0.36', 'prod-db-host-04.internal', 3306, 'primary', '{"storage_class":"hdd"}'
  union all select 'config-mysql-replica-01-prod', 'mysql', '8.0.36', 'prod-db-host-04.internal', 3307, 'replica', '{}'
  union all select 'order-mysql-primary-staging', 'mysql', '8.0.36', 'staging-db-host-01.internal', 3306, 'primary', '{}'
  union all select 'order-mysql-replica-staging', 'mysql', '8.0.36', 'staging-db-host-01.internal', 3307, 'replica', '{}'
  union all select 'dev-mysql-primary', 'mysql', '8.0.36', 'dev-db-host-01.internal', 3306, 'primary', '{}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
select resources.id, seed.system_name, seed.repository_url, seed.runtime_env, seed.spec
from (
  select 'payment-api-prod' as resource_name, 'payment-api' as system_name, 'https://git.internal/payment/api' as repository_url, 'kubernetes' as runtime_env, '{"language":"go","framework":"gin","replicas":3}' as spec
  union all select 'user-api-prod', 'user-api', 'https://git.internal/user/api', 'kubernetes', '{"language":"java","framework":"spring-boot","replicas":2}'
  union all select 'analytics-service-prod', 'analytics-pipeline', 'https://git.internal/analytics/pipeline', 'kubernetes', '{"language":"python","framework":"celery"}'
  union all select 'config-service-prod', 'config-service', 'https://git.internal/platform/config', 'vm', '{}'
  union all select 'order-api-staging', 'order-api', 'https://git.internal/order/api', 'kubernetes', '{"language":"go"}'
  union all select 'payment-api-staging', 'payment-api', 'https://git.internal/payment/api', 'kubernetes', '{"language":"go"}'
  union all select 'order-api-dev', 'order-api', 'https://git.internal/order/api', 'docker-compose', '{}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_relations (from_resource_id, to_resource_id, relation_type)
select src.id, dst.id, seed.relation_type
from (
  select 'order-mysql-replica-01-prod' as from_name, 'order-mysql-cluster-prod' as to_name, 'member_of' as relation_type
  union all select 'order-mysql-replica-02-prod', 'order-mysql-cluster-prod', 'member_of'
  union all select 'payment-mysql-primary-prod', 'payment-mysql-cluster-prod', 'member_of'
  union all select 'payment-mysql-replica-01-prod', 'payment-mysql-cluster-prod', 'member_of'
  union all select 'user-redis-primary-prod', 'user-redis-cluster-prod', 'member_of'
  union all select 'user-redis-replica-01-prod', 'user-redis-cluster-prod', 'member_of'
  union all select 'analytics-ch-node-01-prod', 'analytics-ch-cluster-prod', 'member_of'
  union all select 'analytics-ch-node-02-prod', 'analytics-ch-cluster-prod', 'member_of'
  union all select 'config-mysql-primary-prod', 'config-mysql-cluster-prod', 'member_of'
  union all select 'config-mysql-replica-01-prod', 'config-mysql-cluster-prod', 'member_of'
  union all select 'order-mysql-primary-staging', 'order-mysql-cluster-staging', 'member_of'
  union all select 'order-mysql-replica-staging', 'order-mysql-cluster-staging', 'member_of'
  union all select 'payment-mysql-primary-staging', 'payment-mysql-cluster-staging', 'member_of'
  union all select 'dev-mysql-primary', 'dev-mysql-cluster', 'member_of'
  union all select 'order-mysql-replica-01-prod', 'prod-db-host-02', 'runs_on'
  union all select 'order-mysql-replica-02-prod', 'prod-db-host-03', 'runs_on'
  union all select 'payment-mysql-primary-prod', 'prod-db-host-03', 'runs_on'
  union all select 'payment-mysql-replica-01-prod', 'prod-db-host-04', 'runs_on'
  union all select 'user-redis-primary-prod', 'prod-db-host-03', 'runs_on'
  union all select 'user-redis-replica-01-prod', 'prod-db-host-04', 'runs_on'
  union all select 'analytics-ch-node-01-prod', 'prod-ch-host-01', 'runs_on'
  union all select 'analytics-ch-node-02-prod', 'prod-ch-host-02', 'runs_on'
  union all select 'config-mysql-primary-prod', 'prod-db-host-04', 'runs_on'
  union all select 'config-mysql-replica-01-prod', 'prod-db-host-04', 'runs_on'
  union all select 'order-mysql-primary-staging', 'staging-db-host-01', 'runs_on'
  union all select 'order-mysql-replica-staging', 'staging-db-host-01', 'runs_on'
  union all select 'dev-mysql-primary', 'dev-db-host-01', 'runs_on'
  union all select 'payment-api-prod', 'payment-mysql-cluster-prod', 'depends_on'
  union all select 'payment-api-prod', 'payment-proxysql-prod', 'depends_on'
  union all select 'user-api-prod', 'user-redis-cluster-prod', 'depends_on'
  union all select 'analytics-service-prod', 'analytics-ch-proxy-prod', 'depends_on'
  union all select 'config-service-prod', 'config-mysql-cluster-prod', 'depends_on'
  union all select 'notification-service-prod', 'config-mysql-cluster-prod', 'depends_on'
  union all select 'order-api-prod', 'order-proxysql-prod', 'depends_on'
  union all select 'order-api-staging', 'order-mysql-cluster-staging', 'depends_on'
  union all select 'payment-api-staging', 'payment-mysql-cluster-staging', 'depends_on'
  union all select 'order-proxysql-prod', 'order-mysql-cluster-prod', 'fronts'
  union all select 'payment-proxysql-prod', 'payment-mysql-cluster-prod', 'fronts'
  union all select 'analytics-ch-proxy-prod', 'analytics-ch-cluster-prod', 'fronts'
  union all select 'config-proxysql-prod', 'config-mysql-cluster-prod', 'fronts'
  union all select 'order-proxysql-staging', 'order-mysql-cluster-staging', 'fronts'
  union all select 'dev-proxysql', 'dev-mysql-cluster', 'fronts'
  union all select 'payment-proxysql-02-prod', 'payment-mysql-cluster-prod', 'fronts'
  union all select 'order-vip-prod', 'order-proxysql-prod', 'fronts'
  union all select 'payment-vip-prod', 'payment-proxysql-prod', 'fronts'
  union all select 'analytics-vip-prod', 'analytics-ch-proxy-prod', 'fronts'
  union all select 'order-vip-staging', 'order-proxysql-staging', 'fronts'
  union all select 'dev-vip', 'dev-proxysql', 'fronts'
  union all select 'api.order.internal', 'order-vip-prod', 'points_to'
  union all select 'api.payment.internal', 'payment-vip-prod', 'points_to'
  union all select 'api.user.internal', 'user-api-prod', 'points_to'
  union all select 'analytics.internal', 'analytics-vip-prod', 'points_to'
  union all select 'staging.order.internal', 'order-vip-staging', 'points_to'
  union all select 'dev.order.internal', 'dev-vip', 'points_to'
  union all select 'db-orchestrator-prod', 'order-mysql-cluster-prod', 'manages'
  union all select 'db-orchestrator-prod', 'payment-mysql-cluster-prod', 'manages'
  union all select 'ha-manager-prod', 'config-mysql-cluster-prod', 'manages'
  union all select 'ha-manager-prod', 'user-redis-cluster-prod', 'manages'
  union all select 'db-orchestrator-staging', 'order-mysql-cluster-staging', 'manages'
  union all select 'payment-mysql-primary-prod', 'payment-mysql-replica-01-prod', 'replicates_to'
) seed
join resources src on src.name = seed.from_name
join resources dst on dst.name = seed.to_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into audit_events (actor_user_id, target_resource_id, event_type, result, created_at)
select users.id, resources.id, seed.event_type, seed.result, seed.created_at
from (
  select 'admin@example.com' as actor_email, 'prod-db-host-02' as resource_name, 'resource.created' as event_type, 'success' as result, '2026-04-10 09:00:00' as created_at
  union all select 'admin@example.com', 'prod-db-host-03', 'resource.created', 'success', '2026-04-10 09:02:00'
  union all select 'admin@example.com', 'prod-db-host-04', 'resource.created', 'success', '2026-04-10 09:05:00'
  union all select 'admin@example.com', 'payment-mysql-cluster-prod', 'resource.created', 'success', '2026-04-10 09:30:00'
  union all select 'editor@example.com', 'order-mysql-replica-01-prod', 'resource.created', 'success', '2026-04-10 10:00:00'
  union all select 'admin@example.com', 'order-mysql-replica-01-prod', 'relation.created', 'success', '2026-04-10 10:15:00'
  union all select 'admin@example.com', 'payment-mysql-primary-prod', 'resource.created', 'success', '2026-04-10 10:30:00'
  union all select 'admin@example.com', 'payment-mysql-replica-01-prod', 'resource.created', 'success', '2026-04-10 10:45:00'
  union all select 'admin@example.com', 'user-redis-cluster-prod', 'resource.created', 'success', '2026-04-10 11:00:00'
  union all select 'editor@example.com', 'payment-api-prod', 'resource.created', 'success', '2026-04-10 14:00:00'
  union all select 'editor@example.com', 'user-api-prod', 'resource.created', 'success', '2026-04-10 14:10:00'
  union all select 'editor@example.com', 'order-proxysql-prod', 'resource.created', 'success', '2026-04-10 14:30:00'
  union all select 'editor@example.com', 'payment-proxysql-prod', 'resource.created', 'success', '2026-04-10 14:35:00'
  union all select 'admin@example.com', 'config-service-prod', 'resource.created', 'success', '2026-04-11 08:00:00'
  union all select 'admin@example.com', 'config-service-prod', 'resource.updated', 'success', '2026-04-11 08:30:00'
  union all select 'admin@example.com', 'notification-service-prod', 'resource.created', 'success', '2026-04-11 09:00:00'
  union all select 'editor@example.com', 'notification-service-prod', 'resource.lifecycle_changed', 'success', '2026-04-11 10:00:00'
  union all select 'admin@example.com', 'payment-mysql-replica-01-prod', 'resource.health_changed', 'success', '2026-04-11 15:00:00'
  union all select 'admin@example.com', 'analytics-ch-node-02-prod', 'resource.health_changed', 'success', '2026-04-11 16:00:00'
  union all select 'editor@example.com', 'config-mysql-cluster-prod', 'resource.health_changed', 'partial', '2026-04-11 16:30:00'
  union all select 'editor@example.com', 'config-proxysql-prod', 'resource.updated', 'failure', '2026-04-12 09:00:00'
  union all select 'editor@example.com', 'config-proxysql-prod', 'resource.updated', 'success', '2026-04-12 09:15:00'
  union all select 'admin@example.com', 'order-vip-prod', 'relation.created', 'success', '2026-04-12 10:00:00'
  union all select 'admin@example.com', 'api.order.internal', 'relation.created', 'failure', '2026-04-12 10:05:00'
  union all select 'admin@example.com', 'api.order.internal', 'relation.created', 'success', '2026-04-12 10:10:00'
) seed
join users on users.email = seed.actor_email
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
delete from audit_events
where (actor_user_id, target_resource_id, event_type, result, created_at) in (
  select users.id, resources.id, seed.event_type, seed.result, seed.created_at
  from (
    select 'admin@example.com' as actor_email, 'prod-db-host-02' as resource_name, 'resource.created' as event_type, 'success' as result, '2026-04-10 09:00:00' as created_at
    union all select 'admin@example.com', 'prod-db-host-03', 'resource.created', 'success', '2026-04-10 09:02:00'
    union all select 'admin@example.com', 'prod-db-host-04', 'resource.created', 'success', '2026-04-10 09:05:00'
    union all select 'admin@example.com', 'payment-mysql-cluster-prod', 'resource.created', 'success', '2026-04-10 09:30:00'
    union all select 'editor@example.com', 'order-mysql-replica-01-prod', 'resource.created', 'success', '2026-04-10 10:00:00'
    union all select 'admin@example.com', 'order-mysql-replica-01-prod', 'relation.created', 'success', '2026-04-10 10:15:00'
    union all select 'admin@example.com', 'payment-mysql-primary-prod', 'resource.created', 'success', '2026-04-10 10:30:00'
    union all select 'admin@example.com', 'payment-mysql-replica-01-prod', 'resource.created', 'success', '2026-04-10 10:45:00'
    union all select 'admin@example.com', 'user-redis-cluster-prod', 'resource.created', 'success', '2026-04-10 11:00:00'
    union all select 'editor@example.com', 'payment-api-prod', 'resource.created', 'success', '2026-04-10 14:00:00'
    union all select 'editor@example.com', 'user-api-prod', 'resource.created', 'success', '2026-04-10 14:10:00'
    union all select 'editor@example.com', 'order-proxysql-prod', 'resource.created', 'success', '2026-04-10 14:30:00'
    union all select 'editor@example.com', 'payment-proxysql-prod', 'resource.created', 'success', '2026-04-10 14:35:00'
    union all select 'admin@example.com', 'config-service-prod', 'resource.created', 'success', '2026-04-11 08:00:00'
    union all select 'admin@example.com', 'config-service-prod', 'resource.updated', 'success', '2026-04-11 08:30:00'
    union all select 'admin@example.com', 'notification-service-prod', 'resource.created', 'success', '2026-04-11 09:00:00'
    union all select 'editor@example.com', 'notification-service-prod', 'resource.lifecycle_changed', 'success', '2026-04-11 10:00:00'
    union all select 'admin@example.com', 'payment-mysql-replica-01-prod', 'resource.health_changed', 'success', '2026-04-11 15:00:00'
    union all select 'admin@example.com', 'analytics-ch-node-02-prod', 'resource.health_changed', 'success', '2026-04-11 16:00:00'
    union all select 'editor@example.com', 'config-mysql-cluster-prod', 'resource.health_changed', 'partial', '2026-04-11 16:30:00'
    union all select 'editor@example.com', 'config-proxysql-prod', 'resource.updated', 'failure', '2026-04-12 09:00:00'
    union all select 'editor@example.com', 'config-proxysql-prod', 'resource.updated', 'success', '2026-04-12 09:15:00'
    union all select 'admin@example.com', 'order-vip-prod', 'relation.created', 'success', '2026-04-12 10:00:00'
    union all select 'admin@example.com', 'api.order.internal', 'relation.created', 'failure', '2026-04-12 10:05:00'
    union all select 'admin@example.com', 'api.order.internal', 'relation.created', 'success', '2026-04-12 10:10:00'
  ) seed
  join users on users.email = seed.actor_email
  join resources on resources.name = seed.resource_name
);
-- +goose StatementEnd
-- +goose StatementBegin
delete rel
from resource_relations rel
join resources src on src.id = rel.from_resource_id
join resources dst on dst.id = rel.to_resource_id
where (src.name, dst.name, rel.relation_type) in (
  ('order-mysql-replica-01-prod', 'order-mysql-cluster-prod', 'member_of'),
  ('order-mysql-replica-02-prod', 'order-mysql-cluster-prod', 'member_of'),
  ('payment-mysql-primary-prod', 'payment-mysql-cluster-prod', 'member_of'),
  ('payment-mysql-replica-01-prod', 'payment-mysql-cluster-prod', 'member_of'),
  ('user-redis-primary-prod', 'user-redis-cluster-prod', 'member_of'),
  ('user-redis-replica-01-prod', 'user-redis-cluster-prod', 'member_of'),
  ('analytics-ch-node-01-prod', 'analytics-ch-cluster-prod', 'member_of'),
  ('analytics-ch-node-02-prod', 'analytics-ch-cluster-prod', 'member_of'),
  ('config-mysql-primary-prod', 'config-mysql-cluster-prod', 'member_of'),
  ('config-mysql-replica-01-prod', 'config-mysql-cluster-prod', 'member_of'),
  ('order-mysql-primary-staging', 'order-mysql-cluster-staging', 'member_of'),
  ('order-mysql-replica-staging', 'order-mysql-cluster-staging', 'member_of'),
  ('payment-mysql-primary-staging', 'payment-mysql-cluster-staging', 'member_of'),
  ('dev-mysql-primary', 'dev-mysql-cluster', 'member_of'),
  ('order-mysql-replica-01-prod', 'prod-db-host-02', 'runs_on'),
  ('order-mysql-replica-02-prod', 'prod-db-host-03', 'runs_on'),
  ('payment-mysql-primary-prod', 'prod-db-host-03', 'runs_on'),
  ('payment-mysql-replica-01-prod', 'prod-db-host-04', 'runs_on'),
  ('user-redis-primary-prod', 'prod-db-host-03', 'runs_on'),
  ('user-redis-replica-01-prod', 'prod-db-host-04', 'runs_on'),
  ('analytics-ch-node-01-prod', 'prod-ch-host-01', 'runs_on'),
  ('analytics-ch-node-02-prod', 'prod-ch-host-02', 'runs_on'),
  ('config-mysql-primary-prod', 'prod-db-host-04', 'runs_on'),
  ('config-mysql-replica-01-prod', 'prod-db-host-04', 'runs_on'),
  ('order-mysql-primary-staging', 'staging-db-host-01', 'runs_on'),
  ('order-mysql-replica-staging', 'staging-db-host-01', 'runs_on'),
  ('dev-mysql-primary', 'dev-db-host-01', 'runs_on'),
  ('payment-api-prod', 'payment-mysql-cluster-prod', 'depends_on'),
  ('payment-api-prod', 'payment-proxysql-prod', 'depends_on'),
  ('user-api-prod', 'user-redis-cluster-prod', 'depends_on'),
  ('analytics-service-prod', 'analytics-ch-proxy-prod', 'depends_on'),
  ('config-service-prod', 'config-mysql-cluster-prod', 'depends_on'),
  ('notification-service-prod', 'config-mysql-cluster-prod', 'depends_on'),
  ('order-api-prod', 'order-proxysql-prod', 'depends_on'),
  ('order-api-staging', 'order-mysql-cluster-staging', 'depends_on'),
  ('payment-api-staging', 'payment-mysql-cluster-staging', 'depends_on'),
  ('order-proxysql-prod', 'order-mysql-cluster-prod', 'fronts'),
  ('payment-proxysql-prod', 'payment-mysql-cluster-prod', 'fronts'),
  ('analytics-ch-proxy-prod', 'analytics-ch-cluster-prod', 'fronts'),
  ('config-proxysql-prod', 'config-mysql-cluster-prod', 'fronts'),
  ('order-proxysql-staging', 'order-mysql-cluster-staging', 'fronts'),
  ('dev-proxysql', 'dev-mysql-cluster', 'fronts'),
  ('payment-proxysql-02-prod', 'payment-mysql-cluster-prod', 'fronts'),
  ('order-vip-prod', 'order-proxysql-prod', 'fronts'),
  ('payment-vip-prod', 'payment-proxysql-prod', 'fronts'),
  ('analytics-vip-prod', 'analytics-ch-proxy-prod', 'fronts'),
  ('order-vip-staging', 'order-proxysql-staging', 'fronts'),
  ('dev-vip', 'dev-proxysql', 'fronts'),
  ('api.order.internal', 'order-vip-prod', 'points_to'),
  ('api.payment.internal', 'payment-vip-prod', 'points_to'),
  ('api.user.internal', 'user-api-prod', 'points_to'),
  ('analytics.internal', 'analytics-vip-prod', 'points_to'),
  ('staging.order.internal', 'order-vip-staging', 'points_to'),
  ('dev.order.internal', 'dev-vip', 'points_to'),
  ('db-orchestrator-prod', 'order-mysql-cluster-prod', 'manages'),
  ('db-orchestrator-prod', 'payment-mysql-cluster-prod', 'manages'),
  ('ha-manager-prod', 'config-mysql-cluster-prod', 'manages'),
  ('ha-manager-prod', 'user-redis-cluster-prod', 'manages'),
  ('db-orchestrator-staging', 'order-mysql-cluster-staging', 'manages'),
  ('payment-mysql-primary-prod', 'payment-mysql-replica-01-prod', 'replicates_to')
);
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_service prof
join resources on resources.id = prof.resource_id
where resources.name in ('payment-api-prod', 'user-api-prod', 'analytics-service-prod', 'config-service-prod', 'order-api-staging', 'payment-api-staging', 'order-api-dev');
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_database_instance prof
join resources on resources.id = prof.resource_id
where resources.name in ('order-mysql-replica-01-prod', 'payment-mysql-primary-prod', 'payment-mysql-replica-01-prod', 'user-redis-primary-prod', 'user-redis-replica-01-prod', 'analytics-ch-node-01-prod', 'analytics-ch-node-02-prod', 'config-mysql-primary-prod', 'config-mysql-replica-01-prod', 'order-mysql-primary-staging', 'order-mysql-replica-staging', 'dev-mysql-primary');
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_database_cluster prof
join resources on resources.id = prof.resource_id
where resources.name in ('payment-mysql-cluster-prod', 'user-redis-cluster-prod', 'analytics-ch-cluster-prod', 'config-mysql-cluster-prod', 'order-mysql-cluster-staging', 'payment-mysql-cluster-staging', 'dev-mysql-cluster');
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_host prof
join resources on resources.id = prof.resource_id
where resources.name in ('prod-db-host-02', 'prod-db-host-03', 'prod-db-host-04', 'prod-app-host-01', 'prod-app-host-02', 'prod-ch-host-01', 'prod-ch-host-02', 'staging-db-host-01', 'staging-db-host-02', 'staging-app-host-01', 'dev-db-host-01');
-- +goose StatementEnd
-- +goose StatementBegin
delete from resources where name in (
  'prod-db-host-02', 'prod-db-host-03', 'prod-db-host-04', 'prod-app-host-01', 'prod-app-host-02', 'prod-ch-host-01', 'prod-ch-host-02',
  'payment-mysql-cluster-prod', 'user-redis-cluster-prod', 'analytics-ch-cluster-prod', 'config-mysql-cluster-prod',
  'order-mysql-replica-01-prod', 'order-mysql-replica-02-prod', 'payment-mysql-primary-prod', 'payment-mysql-replica-01-prod', 'user-redis-primary-prod', 'user-redis-replica-01-prod', 'analytics-ch-node-01-prod', 'analytics-ch-node-02-prod', 'config-mysql-primary-prod', 'config-mysql-replica-01-prod',
  'payment-api-prod', 'user-api-prod', 'analytics-service-prod', 'config-service-prod', 'notification-service-prod',
  'order-proxysql-prod', 'payment-proxysql-prod', 'analytics-ch-proxy-prod', 'config-proxysql-prod', 'payment-proxysql-02-prod',
  'order-vip-prod', 'payment-vip-prod', 'analytics-vip-prod',
  'api.order.internal', 'api.payment.internal', 'api.user.internal', 'analytics.internal',
  'db-orchestrator-prod', 'ha-manager-prod',
  'staging-db-host-01', 'staging-db-host-02', 'staging-app-host-01',
  'order-mysql-cluster-staging', 'payment-mysql-cluster-staging', 'order-mysql-primary-staging', 'order-mysql-replica-staging', 'payment-mysql-primary-staging',
  'order-api-staging', 'payment-api-staging', 'order-proxysql-staging', 'order-vip-staging', 'staging.order.internal', 'db-orchestrator-staging',
  'dev-db-host-01', 'dev-mysql-cluster', 'dev-mysql-primary', 'order-api-dev', 'dev-proxysql', 'dev-vip', 'dev.order.internal'
);
-- +goose StatementEnd
-- +goose StatementBegin
delete from owners where email in ('order-team@example.com', 'payment-team@example.com', 'analytics-team@example.com');
-- +goose StatementEnd
-- +goose StatementBegin
delete from environments where slug = 'dev';
-- +goose StatementEnd
