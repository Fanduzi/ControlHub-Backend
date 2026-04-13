-- ControlHub comprehensive demo data for frontend integration testing
-- Adds ~60 resources across 3 environments with realistic relationships
--
-- Scenarios:
--   Order MySQL cluster (prod/staging/dev)
--   Payment MySQL cluster (prod/staging)
--   User Redis cluster (prod)
--   Analytics ClickHouse cluster (prod)
--   Config MySQL cluster (prod)
--   Shared infrastructure (hosts, proxies, VIPs, domains, control plane)
--
-- Status variety:
--   running/healthy   — majority of resources
--   running/warning   — payment-mysql-replica-01, config-mysql-replica-01
--   running/critical  — analytics-ch-node-02 (disk pressure)
--   degraded/warning  — config-mysql-cluster, config-proxysql
--   stopped/unknown   — notification-service (intentionally disabled)
--   provisioning/healthy — payment-mysql-primary-staging

-- ============================================================
-- Section 1: Reference Data Expansion
-- ============================================================

insert ignore into environments (id, name, slug, description) values
  ('10000000-0000-0000-0000-000000000003', 'Development', 'dev', 'Development and testing environment');

insert ignore into owners (id, name, email) values
  ('20000000-0000-0000-0000-000000000003', 'Order Team',     'order-team@example.com'),
  ('20000000-0000-0000-0000-000000000004', 'Payment Team',   'payment-team@example.com'),
  ('20000000-0000-0000-0000-000000000005', 'Analytics Team', 'analytics-team@example.com');

-- ============================================================
-- Section 2: Production Resources (39 new)
-- ============================================================
-- Existing prod resources:
--   40000000-...-0001  order-mysql-cluster-prod  (database_cluster)
--   40000000-...-0002  order-mysql-01-prod       (database_instance, primary)
--   40000000-...-0003  order-api-prod             (service)
--   40000000-...-0004  prod-db-host-01           (host)

-- --- Production Hosts (7) ---
-- ID mapping: ...0001=db-host-02, ...0002=db-host-03, ...0003=db-host-04
--             ...0004=app-host-01, ...0005=app-host-02, ...0006=ch-host-01, ...0007=ch-host-02

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000001', 'host', 'vm',
   'prod-db-host-02', 'Production DB Host 02',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra","rack":"B12"}', 'manual', 'vmware-prod-db-host-02'),

  ('41000000-0000-0000-0000-000000000002', 'host', 'vm',
   'prod-db-host-03', 'Production DB Host 03',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra","rack":"B13"}', 'manual', 'vmware-prod-db-host-03'),

  ('41000000-0000-0000-0000-000000000003', 'host', 'vm',
   'prod-db-host-04', 'Production DB Host 04',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra","rack":"B14"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000004', 'host', 'vm',
   'prod-app-host-01', 'Production Application Host 01',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"compute"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000005', 'host', 'vm',
   'prod-app-host-02', 'Production Application Host 02',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"compute"}', 'terraform', ''),

  ('41000000-0000-0000-0000-000000000006', 'host', 'physical',
   'prod-ch-host-01', 'Production ClickHouse Host 01 - High Storage Density Node',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra","purpose":"analytics","disk_type":"nvme"}', 'discovery', 'bare-prod-ch-host-01'),

  ('41000000-0000-0000-0000-000000000007', 'host', 'physical',
   'prod-ch-host-02', 'Production ClickHouse Host 02 - High Storage Density Node',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra","purpose":"analytics","disk_type":"nvme"}', 'discovery', 'bare-prod-ch-host-02');

-- --- Production Database Clusters (4) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000010', 'database_cluster', 'mysql',
   'payment-mysql-cluster-prod', 'Payment MySQL Cluster Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"payment","tier":"data","critical":"true","pci_scope":"yes"}', 'manual',
   'dbaas-payment-mysql-cluster-prod'),

  ('41000000-0000-0000-0000-000000000011', 'database_cluster', 'redis',
   'user-redis-cluster-prod', 'User Session Redis Cluster Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"cache"}', 'manual',
   'dbaas-user-redis-cluster-prod'),

  ('41000000-0000-0000-0000-000000000012', 'database_cluster', 'clickhouse',
   'analytics-ch-cluster-prod', 'Analytics ClickHouse Cluster Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"analytics","tier":"data","engine":"clickhouse"}', 'manual',
   'dbaas-analytics-ch-cluster-prod'),

  ('41000000-0000-0000-0000-000000000013', 'database_cluster', 'mysql',
   'config-mysql-cluster-prod', 'Platform Config Service MySQL Cluster Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'degraded', 'warning',
   '{"team":"platform","tier":"data"}', 'manual', '');

-- --- Production Database Instances (10) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000020', 'database_instance', 'mysql',
   'order-mysql-replica-01-prod', 'Order MySQL Replica 01 Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"data","replication_lag_seconds":"0"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000021', 'database_instance', 'mysql',
   'order-mysql-replica-02-prod', 'Order MySQL Replica 02 Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000022', 'database_instance', 'mysql',
   'payment-mysql-primary-prod', 'Payment MySQL Primary Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"payment","tier":"data","critical":"true"}', 'manual',
   'dbaas-payment-mysql-primary-prod-inst'),

  ('41000000-0000-0000-0000-000000000023', 'database_instance', 'mysql',
   'payment-mysql-replica-01-prod', 'Payment MySQL Replica 01 Production - Replication Lag Warning',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'warning',
   '{"team":"payment","tier":"data","replication_lag_seconds":"45","alert":"replication-lag"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000024', 'database_instance', 'redis',
   'user-redis-primary-prod', 'User Redis Primary Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"cache","memory_max_gb":"32"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000025', 'database_instance', 'redis',
   'user-redis-replica-01-prod', 'User Redis Replica 01 Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"cache"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000026', 'database_instance', 'clickhouse',
   'analytics-ch-node-01-prod', 'Analytics ClickHouse Node 01 Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"analytics","tier":"data","clickhouse_role":"replica"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000027', 'database_instance', 'clickhouse',
   'analytics-ch-node-02-prod', 'Analytics ClickHouse Node 02 Production - Critical Disk Pressure',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'critical',
   '{"team":"analytics","tier":"data","clickhouse_role":"replica","disk_usage_pct":"94","alert":"disk-pressure"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000028', 'database_instance', 'mysql',
   'config-mysql-primary-prod', 'Config Service MySQL Primary Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'warning',
   '{"team":"platform","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000029', 'database_instance', 'mysql',
   'config-mysql-replica-01-prod', 'Config Service MySQL Replica 01 Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002',
   'running', 'warning',
   '{"team":"platform","tier":"data","replication_lag_seconds":"12"}', 'manual', '');

-- --- Production Services (5) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000030', 'service', 'api',
   'payment-api-prod', 'Payment Processing API Service Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000004',
   'running', 'healthy',
   '{"team":"payment","tier":"app","runtime":"go","port":"8080"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000031', 'service', 'api',
   'user-api-prod', 'User Management API Service Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"app","runtime":"java","port":"8080"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000032', 'service', 'worker',
   'analytics-service-prod', 'Analytics Data Pipeline Worker Service Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000005',
   'running', 'healthy',
   '{"team":"analytics","tier":"app","runtime":"python"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000033', 'service', 'api',
   'config-service-prod', 'Platform Configuration Service Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"app"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000034', 'service', 'worker',
   'notification-service-prod', 'Notification Delivery Service Production - Currently Disabled for Migration',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003',
   'stopped', 'unknown',
   '{"team":"order","tier":"app","runtime":"node","disabled_reason":"kafka-migration"}', 'manual', '');

-- --- Production Database Proxies (4) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000040', 'database_proxy', 'proxysql',
   'order-proxysql-prod', 'Order MySQL ProxySQL Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"proxy","backend":"mysql"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000041', 'database_proxy', 'proxysql',
   'payment-proxysql-prod', 'Payment MySQL ProxySQL Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"proxy","backend":"mysql","pci_scope":"yes"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000042', 'database_proxy', 'chproxy',
   'analytics-ch-proxy-prod', 'Analytics ClickHouse Proxy Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"proxy","backend":"clickhouse"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000043', 'database_proxy', 'proxysql',
   'config-proxysql-prod', 'Config Service ProxySQL Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'degraded', 'warning',
   '{"team":"platform","tier":"proxy","backend":"mysql"}', 'manual', '');

-- --- Production Virtual IPs (3) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000050', 'virtual_ip', 'floating',
   'order-vip-prod', 'Order Service Virtual IP Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"network","ip":"10.0.10.100"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000051', 'virtual_ip', 'floating',
   'payment-vip-prod', 'Payment Service Virtual IP Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"network","ip":"10.0.10.101"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000052', 'virtual_ip', 'floating',
   'analytics-vip-prod', 'Analytics Service Virtual IP Production',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"network","ip":"10.0.10.102"}', 'manual', '');

-- --- Production Domain Names (4) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000060', 'domain_name', 'dns',
   'api.order.internal', 'Order Service Internal API Domain - Production Primary Endpoint',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"network","zone":"internal"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000061', 'domain_name', 'dns',
   'api.payment.internal', 'Payment Service Internal API Domain - Production Primary Endpoint',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000004',
   'running', 'healthy',
   '{"team":"payment","tier":"network","zone":"internal"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000062', 'domain_name', 'dns',
   'api.user.internal', 'User Service Internal API Domain',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"network"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000063', 'domain_name', 'dns',
   'analytics.internal', 'Analytics Platform Internal Domain',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000005',
   'running', 'healthy',
   '{"team":"analytics","tier":"network"}', 'manual', '');

-- --- Production Control Plane Components (2) ---

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000070', 'control_plane_component', 'orchestrator',
   'db-orchestrator-prod', 'Database Orchestrator - Production Control Plane',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"control-plane","managed_clusters":"4"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000071', 'control_plane_component', 'ha',
   'ha-manager-prod', 'High Availability Manager - Production Control Plane',
   '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"control-plane"}', 'manual', '');

-- ============================================================
-- Section 3: Staging Resources (14)
-- ============================================================

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  -- Staging hosts (3)
  ('41000000-0000-0000-0000-000000000080', 'host', 'vm',
   'staging-db-host-01', 'Staging DB Host 01',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000081', 'host', 'vm',
   'staging-db-host-02', 'Staging DB Host 02',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000082', 'host', 'vm',
   'staging-app-host-01', 'Staging Application Host 01',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"compute"}', 'manual', ''),

  -- Staging database clusters (2)
  ('41000000-0000-0000-0000-000000000083', 'database_cluster', 'mysql',
   'order-mysql-cluster-staging', 'Order MySQL Cluster Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000084', 'database_cluster', 'mysql',
   'payment-mysql-cluster-staging', 'Payment MySQL Cluster Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"payment","tier":"data"}', 'manual', ''),

  -- Staging database instances (3)
  ('41000000-0000-0000-0000-000000000085', 'database_instance', 'mysql',
   'order-mysql-primary-staging', 'Order MySQL Primary Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000086', 'database_instance', 'mysql',
   'order-mysql-replica-staging', 'Order MySQL Replica Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"order","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000087', 'database_instance', 'mysql',
   'payment-mysql-primary-staging', 'Payment MySQL Primary Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
   'provisioning', 'healthy',
   '{"team":"payment","tier":"data"}', 'manual', ''),

  -- Staging services (2)
  ('41000000-0000-0000-0000-000000000088', 'service', 'api',
   'order-api-staging', 'Order API Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"app"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000089', 'service', 'api',
   'payment-api-staging', 'Payment API Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000004',
   'running', 'healthy',
   '{"team":"payment","tier":"app"}', 'manual', ''),

  -- Staging proxy (1)
  ('41000000-0000-0000-0000-000000000090', 'database_proxy', 'proxysql',
   'order-proxysql-staging', 'Order ProxySQL Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"proxy"}', 'manual', ''),

  -- Staging VIP (1)
  ('41000000-0000-0000-0000-000000000091', 'virtual_ip', 'floating',
   'order-vip-staging', 'Order Service Virtual IP Staging',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"network","ip":"10.1.10.100"}', 'manual', ''),

  -- Staging domain (1)
  ('41000000-0000-0000-0000-000000000092', 'domain_name', 'dns',
   'staging.order.internal', 'Order Service Staging Internal Domain',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"network"}', 'manual', ''),

  -- Staging control plane (1)
  ('41000000-0000-0000-0000-000000000093', 'control_plane_component', 'orchestrator',
   'db-orchestrator-staging', 'Database Orchestrator - Staging Control Plane',
   '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"control-plane"}', 'manual', '');

-- ============================================================
-- Section 4: Development Resources (7)
-- ============================================================

insert into resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
) values
  ('41000000-0000-0000-0000-000000000094', 'host', 'vm',
   'dev-db-host-01', 'Development DB Host 01',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"infra"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000095', 'database_cluster', 'mysql',
   'dev-mysql-cluster', 'Development MySQL Cluster',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"platform","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000096', 'database_instance', 'mysql',
   'dev-mysql-primary', 'Development MySQL Primary Instance',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000002',
   'running', 'healthy',
   '{"team":"platform","tier":"data"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000097', 'service', 'api',
   'order-api-dev', 'Order API Development',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{"team":"order","tier":"app"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000098', 'database_proxy', 'proxysql',
   'dev-proxysql', 'Development ProxySQL',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"proxy"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000099', 'virtual_ip', 'floating',
   'dev-vip', 'Development Virtual IP',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000001',
   'running', 'healthy',
   '{"team":"platform","tier":"network","ip":"10.2.10.100"}', 'manual', ''),

  ('41000000-0000-0000-0000-000000000100', 'domain_name', 'dns',
   'dev.order.internal', 'Order Service Development Internal Domain',
   '10000000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000003',
   'running', 'healthy',
   '{}', 'manual', '');

-- ============================================================
-- Section 5: Profiles
-- ============================================================

-- --- Host Profiles (7 prod) ---

insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec) values
  ('41000000-0000-0000-0000-000000000001', 'prod-db-host-02.internal', '10.0.10.22', 'Ubuntu 24.04', '{"provider":"vmware","cpu":16,"memory_gb":128}'),
  ('41000000-0000-0000-0000-000000000002', 'prod-db-host-03.internal', '10.0.10.23', 'Ubuntu 24.04', '{"provider":"vmware","cpu":16,"memory_gb":128}'),
  ('41000000-0000-0000-0000-000000000003', 'prod-db-host-04.internal', '10.0.10.24', 'CentOS 9 Stream', '{"provider":"vmware","cpu":8,"memory_gb":64}'),
  ('41000000-0000-0000-0000-000000000004', 'prod-app-host-01.internal', '10.0.20.10', 'Ubuntu 22.04', '{"provider":"vmware","cpu":8,"memory_gb":32,"kubernetes_node":"true"}'),
  ('41000000-0000-0000-0000-000000000005', 'prod-app-host-02.internal', '10.0.20.11', 'Ubuntu 22.04', '{"provider":"vmware","cpu":8,"memory_gb":32,"kubernetes_node":"true"}'),
  ('41000000-0000-0000-0000-000000000006', 'prod-ch-host-01.internal', '10.0.30.10', 'Ubuntu 24.04', '{"provider":"bare-metal","cpu":32,"memory_gb":256,"disk_tb":8,"disk_type":"nvme"}'),
  ('41000000-0000-0000-0000-000000000007', 'prod-ch-host-02.internal', '10.0.30.11', 'Ubuntu 24.04', '{"provider":"bare-metal","cpu":32,"memory_gb":256,"disk_tb":8,"disk_type":"nvme"}');

-- --- Host Profiles (3 staging, 1 dev) ---

insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec) values
  ('41000000-0000-0000-0000-000000000080', 'staging-db-host-01.internal', '10.1.10.20', 'Ubuntu 24.04', '{"provider":"vmware","cpu":4,"memory_gb":32}'),
  ('41000000-0000-0000-0000-000000000081', 'staging-db-host-02.internal', '10.1.10.21', 'Ubuntu 24.04', '{"provider":"vmware","cpu":4,"memory_gb":32}'),
  ('41000000-0000-0000-0000-000000000082', 'staging-app-host-01.internal', '10.1.20.10', 'Ubuntu 22.04', '{"cpu":4,"memory_gb":16}'),
  ('41000000-0000-0000-0000-000000000094', 'dev-db-host-01.internal', '10.2.10.20', 'Ubuntu 24.04', '{"cpu":2,"memory_gb":8}');

-- --- Database Cluster Profiles (4 prod, 2 staging, 1 dev) ---

insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec) values
  ('41000000-0000-0000-0000-000000000010', 'mysql',        'primary-replica',  'payment-mysql-cluster-prod.internal:3306',  '{"replicas":1,"storage_class":"ssd","backup_enabled":true}'),
  ('41000000-0000-0000-0000-000000000011', 'redis',        'cluster',          'user-redis-cluster-prod.internal:6379',     '{"shards":3,"replicas_per_shard":1,"memory_max_gb":"32"}'),
  ('41000000-0000-0000-0000-000000000012', 'clickhouse',   'replicated',       'analytics-ch-cluster-prod.internal:8123',   '{"replicas":2,"storage_gb":8000,"compression":"lz4"}'),
  ('41000000-0000-0000-0000-000000000013', 'mysql',        'primary-replica',  'config-mysql-cluster-prod.internal:3306',   '{}'),
  ('41000000-0000-0000-0000-000000000083', 'mysql',        'primary-replica',  'order-mysql-cluster-staging.internal:3306', '{"replicas":1}'),
  ('41000000-0000-0000-0000-000000000084', 'mysql',        'primary-replica',  'payment-mysql-cluster-staging.internal:3306', '{}'),
  ('41000000-0000-0000-0000-000000000095', 'mysql',        'single',           'dev-mysql-cluster.internal:3306',           '{}');

-- --- Database Instance Profiles (10 prod, 2 staging, 1 dev) ---
-- Note: order-mysql-replica-02-prod gets NO profile row (sparse test case)

insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values
  ('41000000-0000-0000-0000-000000000020', 'mysql',      '8.0.36', 'prod-db-host-02.internal',     3306, 'replica', '{"storage_class":"ssd","delayed":false}'),
  -- 000021 intentionally skipped: order-mysql-replica-02 has no profile
  ('41000000-0000-0000-0000-000000000022', 'mysql',      '8.0.36', 'prod-db-host-02.internal',     3307, 'primary', '{"storage_class":"ssd","innodb_buffer_pool_gb":"64"}'),
  ('41000000-0000-0000-0000-000000000023', 'mysql',      '8.0.36', 'prod-db-host-03.internal',     3306, 'replica', '{"storage_class":"ssd"}'),
  ('41000000-0000-0000-0000-000000000024', 'redis',      '7.2.4',  'prod-db-host-03.internal',     6379, 'primary', '{"maxmemory_policy":"allkeys-lru"}'),
  ('41000000-0000-0000-0000-000000000025', 'redis',      '7.2.4',  'prod-db-host-04.internal',     6379, 'replica', '{}'),
  ('41000000-0000-0000-0000-000000000026', 'clickhouse', '24.3',   'prod-ch-host-01.internal',     8123, 'replica',  '{"clickhouse_servers_version":"24.3.5.37"}'),
  ('41000000-0000-0000-0000-000000000027', 'clickhouse', '24.3',   'prod-ch-host-02.internal',     8123, 'replica',  '{"clickhouse_servers_version":"24.3.5.37","disk_usage_pct":"94"}'),
  ('41000000-0000-0000-0000-000000000028', 'mysql',      '8.0.36', 'prod-db-host-04.internal',     3306, 'primary', '{"storage_class":"hdd"}'),
  ('41000000-0000-0000-0000-000000000029', 'mysql',      '8.0.36', 'prod-db-host-04.internal',     3307, 'replica', '{}'),
  ('41000000-0000-0000-0000-000000000085', 'mysql',      '8.0.36', 'staging-db-host-01.internal',  3306, 'primary', '{}'),
  ('41000000-0000-0000-0000-000000000086', 'mysql',      '8.0.36', 'staging-db-host-01.internal',  3307, 'replica', '{}'),
  ('41000000-0000-0000-0000-000000000096', 'mysql',      '8.0.36', 'dev-db-host-01.internal',      3306, 'primary', '{}');

-- Note: payment-mysql-primary-staging (000087) has no profile — still provisioning

-- --- Service Profiles (5 prod, 2 staging, 1 dev) ---
-- Note: notification-service-prod gets NO profile row (stopped/disabled)

insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec) values
  ('41000000-0000-0000-0000-000000000030', 'payment-api',   'https://git.internal/payment/api',   'kubernetes', '{"language":"go","framework":"gin","replicas":3}'),
  ('41000000-0000-0000-0000-000000000031', 'user-api',      'https://git.internal/user/api',      'kubernetes', '{"language":"java","framework":"spring-boot","replicas":2}'),
  ('41000000-0000-0000-0000-000000000032', 'analytics-pipeline', 'https://git.internal/analytics/pipeline', 'kubernetes', '{"language":"python","framework":"celery"}'),
  ('41000000-0000-0000-0000-000000000033', 'config-service', 'https://git.internal/platform/config', 'vm', '{}'),
  -- 000034 notification-service skipped: no profile (disabled service)
  ('41000000-0000-0000-0000-000000000088', 'order-api',     'https://git.internal/order/api',     'kubernetes', '{"language":"go"}'),
  ('41000000-0000-0000-0000-000000000089', 'payment-api',   'https://git.internal/payment/api',   'kubernetes', '{"language":"go"}'),
  ('41000000-0000-0000-0000-000000000097', 'order-api',     'https://git.internal/order/api',     'docker-compose', '{}');

-- ============================================================
-- Section 6: Relations (~57 new)
-- ============================================================

-- --- member_of: instance -> cluster (14) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production Order replicas -> Order cluster (existing 40000000...0001)
  ('51000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000020', '40000000-0000-0000-0000-000000000001', 'member_of'),
  ('51000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000021', '40000000-0000-0000-0000-000000000001', 'member_of'),
  -- Production Payment
  ('51000000-0000-0000-0000-000000000003', '41000000-0000-0000-0000-000000000022', '41000000-0000-0000-0000-000000000010', 'member_of'),
  ('51000000-0000-0000-0000-000000000004', '41000000-0000-0000-0000-000000000023', '41000000-0000-0000-0000-000000000010', 'member_of'),
  -- Production User Redis
  ('51000000-0000-0000-0000-000000000005', '41000000-0000-0000-0000-000000000024', '41000000-0000-0000-0000-000000000011', 'member_of'),
  ('51000000-0000-0000-0000-000000000006', '41000000-0000-0000-0000-000000000025', '41000000-0000-0000-0000-000000000011', 'member_of'),
  -- Production Analytics CH
  ('51000000-0000-0000-0000-000000000007', '41000000-0000-0000-0000-000000000026', '41000000-0000-0000-0000-000000000012', 'member_of'),
  ('51000000-0000-0000-0000-000000000008', '41000000-0000-0000-0000-000000000027', '41000000-0000-0000-0000-000000000012', 'member_of'),
  -- Production Config
  ('51000000-0000-0000-0000-000000000009', '41000000-0000-0000-0000-000000000028', '41000000-0000-0000-0000-000000000013', 'member_of'),
  ('51000000-0000-0000-0000-000000000010', '41000000-0000-0000-0000-000000000029', '41000000-0000-0000-0000-000000000013', 'member_of'),
  -- Staging Order
  ('51000000-0000-0000-0000-000000000011', '41000000-0000-0000-0000-000000000085', '41000000-0000-0000-0000-000000000083', 'member_of'),
  ('51000000-0000-0000-0000-000000000012', '41000000-0000-0000-0000-000000000086', '41000000-0000-0000-0000-000000000083', 'member_of'),
  -- Staging Payment
  ('51000000-0000-0000-0000-000000000013', '41000000-0000-0000-0000-000000000087', '41000000-0000-0000-0000-000000000084', 'member_of'),
  -- Development
  ('51000000-0000-0000-0000-000000000014', '41000000-0000-0000-0000-000000000096', '41000000-0000-0000-0000-000000000095', 'member_of');

-- --- runs_on: instance -> host (13) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000020', '41000000-0000-0000-0000-000000000020', '41000000-0000-0000-0000-000000000001', 'runs_on'),
  ('51000000-0000-0000-0000-000000000021', '41000000-0000-0000-0000-000000000021', '41000000-0000-0000-0000-000000000002', 'runs_on'),
  ('51000000-0000-0000-0000-000000000022', '41000000-0000-0000-0000-000000000022', '41000000-0000-0000-0000-000000000002', 'runs_on'),
  ('51000000-0000-0000-0000-000000000023', '41000000-0000-0000-0000-000000000023', '41000000-0000-0000-0000-000000000003', 'runs_on'),
  ('51000000-0000-0000-0000-000000000024', '41000000-0000-0000-0000-000000000024', '41000000-0000-0000-0000-000000000003', 'runs_on'),
  ('51000000-0000-0000-0000-000000000025', '41000000-0000-0000-0000-000000000025', '41000000-0000-0000-0000-000000000003', 'runs_on'),
  ('51000000-0000-0000-0000-000000000026', '41000000-0000-0000-0000-000000000026', '41000000-0000-0000-0000-000000000006', 'runs_on'),
  ('51000000-0000-0000-0000-000000000027', '41000000-0000-0000-0000-000000000027', '41000000-0000-0000-0000-000000000007', 'runs_on'),
  ('51000000-0000-0000-0000-000000000028', '41000000-0000-0000-0000-000000000028', '41000000-0000-0000-0000-000000000003', 'runs_on'),
  ('51000000-0000-0000-0000-000000000029', '41000000-0000-0000-0000-000000000029', '41000000-0000-0000-0000-000000000003', 'runs_on'),
  -- Staging
  ('51000000-0000-0000-0000-000000000030', '41000000-0000-0000-0000-000000000085', '41000000-0000-0000-0000-000000000080', 'runs_on'),
  ('51000000-0000-0000-0000-000000000031', '41000000-0000-0000-0000-000000000086', '41000000-0000-0000-0000-000000000080', 'runs_on'),
  -- Development
  ('51000000-0000-0000-0000-000000000032', '41000000-0000-0000-0000-000000000096', '41000000-0000-0000-0000-000000000094', 'runs_on');

-- --- depends_on: service -> cluster/proxy/instance (9) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000040', '41000000-0000-0000-0000-000000000030', '41000000-0000-0000-0000-000000000010', 'depends_on'),
  ('51000000-0000-0000-0000-000000000041', '41000000-0000-0000-0000-000000000030', '41000000-0000-0000-0000-000000000041', 'depends_on'),
  ('51000000-0000-0000-0000-000000000042', '41000000-0000-0000-0000-000000000031', '41000000-0000-0000-0000-000000000011', 'depends_on'),
  ('51000000-0000-0000-0000-000000000043', '41000000-0000-0000-0000-000000000032', '41000000-0000-0000-0000-000000000042', 'depends_on'),
  ('51000000-0000-0000-0000-000000000044', '41000000-0000-0000-0000-000000000033', '41000000-0000-0000-0000-000000000013', 'depends_on'),
  ('51000000-0000-0000-0000-000000000045', '41000000-0000-0000-0000-000000000034', '41000000-0000-0000-0000-000000000013', 'depends_on'),
  -- Also link existing order-api to proxy
  ('51000000-0000-0000-0000-000000000046', '40000000-0000-0000-0000-000000000003', '41000000-0000-0000-0000-000000000040', 'depends_on'),
  -- Staging
  ('51000000-0000-0000-0000-000000000047', '41000000-0000-0000-0000-000000000088', '41000000-0000-0000-0000-000000000083', 'depends_on'),
  ('51000000-0000-0000-0000-000000000048', '41000000-0000-0000-0000-000000000089', '41000000-0000-0000-0000-000000000084', 'depends_on');

-- --- fronts: proxy -> cluster (6) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000050', '41000000-0000-0000-0000-000000000040', '40000000-0000-0000-0000-000000000001', 'fronts'),
  ('51000000-0000-0000-0000-000000000051', '41000000-0000-0000-0000-000000000041', '41000000-0000-0000-0000-000000000010', 'fronts'),
  ('51000000-0000-0000-0000-000000000052', '41000000-0000-0000-0000-000000000042', '41000000-0000-0000-0000-000000000012', 'fronts'),
  ('51000000-0000-0000-0000-000000000053', '41000000-0000-0000-0000-000000000043', '41000000-0000-0000-0000-000000000013', 'fronts'),
  -- Staging
  ('51000000-0000-0000-0000-000000000054', '41000000-0000-0000-0000-000000000090', '41000000-0000-0000-0000-000000000083', 'fronts'),
  -- Development
  ('51000000-0000-0000-0000-000000000055', '41000000-0000-0000-0000-000000000098', '41000000-0000-0000-0000-000000000095', 'fronts');

-- --- fronts: VIP -> proxy (5) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000060', '41000000-0000-0000-0000-000000000050', '41000000-0000-0000-0000-000000000040', 'fronts'),
  ('51000000-0000-0000-0000-000000000061', '41000000-0000-0000-0000-000000000051', '41000000-0000-0000-0000-000000000041', 'fronts'),
  ('51000000-0000-0000-0000-000000000062', '41000000-0000-0000-0000-000000000052', '41000000-0000-0000-0000-000000000042', 'fronts'),
  -- Staging
  ('51000000-0000-0000-0000-000000000063', '41000000-0000-0000-0000-000000000091', '41000000-0000-0000-0000-000000000090', 'fronts'),
  -- Development
  ('51000000-0000-0000-0000-000000000064', '41000000-0000-0000-0000-000000000099', '41000000-0000-0000-0000-000000000098', 'fronts');

-- --- points_to: domain -> VIP/service/proxy (6) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000070', '41000000-0000-0000-0000-000000000060', '41000000-0000-0000-0000-000000000050', 'points_to'),
  ('51000000-0000-0000-0000-000000000071', '41000000-0000-0000-0000-000000000061', '41000000-0000-0000-0000-000000000051', 'points_to'),
  ('51000000-0000-0000-0000-000000000072', '41000000-0000-0000-0000-000000000062', '41000000-0000-0000-0000-000000000031', 'points_to'),
  ('51000000-0000-0000-0000-000000000073', '41000000-0000-0000-0000-000000000063', '41000000-0000-0000-0000-000000000052', 'points_to'),
  -- Staging
  ('51000000-0000-0000-0000-000000000074', '41000000-0000-0000-0000-000000000092', '41000000-0000-0000-0000-000000000091', 'points_to'),
  -- Development
  ('51000000-0000-0000-0000-000000000075', '41000000-0000-0000-0000-000000000100', '41000000-0000-0000-0000-000000000099', 'points_to');

-- --- manages: control_plane -> cluster (4) ---
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  -- Production
  ('51000000-0000-0000-0000-000000000080', '41000000-0000-0000-0000-000000000070', '40000000-0000-0000-0000-000000000001', 'manages'),
  ('51000000-0000-0000-0000-000000000081', '41000000-0000-0000-0000-000000000070', '41000000-0000-0000-0000-000000000010', 'manages'),
  ('51000000-0000-0000-0000-000000000082', '41000000-0000-0000-0000-000000000071', '41000000-0000-0000-0000-000000000013', 'manages'),
  ('51000000-0000-0000-0000-000000000083', '41000000-0000-0000-0000-000000000071', '41000000-0000-0000-0000-000000000011', 'manages'),
  -- Staging
  ('51000000-0000-0000-0000-000000000084', '41000000-0000-0000-0000-000000000093', '41000000-0000-0000-0000-000000000083', 'manages');

-- ============================================================
-- Section 7: Audit Events (25)
-- ============================================================

insert into audit_events (id, actor_user_id, target_resource_id, event_type, result, created_at) values
  -- Day 1: Initial setup
  ('61000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000001', 'resource.created',       'success', '2026-04-10 09:00:00'),
  ('61000000-0000-0000-0000-000000000002', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000002', 'resource.created',       'success', '2026-04-10 09:02:00'),
  ('61000000-0000-0000-0000-000000000003', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000003', 'resource.created',       'success', '2026-04-10 09:05:00'),
  ('61000000-0000-0000-0000-000000000004', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000010', 'resource.created',       'success', '2026-04-10 09:30:00'),
  ('61000000-0000-0000-0000-000000000005', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000020', 'resource.created',       'success', '2026-04-10 10:00:00'),

  -- Day 2: Cluster wiring
  ('61000000-0000-0000-0000-000000000006', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000020', 'relation.created',       'success', '2026-04-10 10:15:00'),
  ('61000000-0000-0000-0000-000000000007', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000022', 'resource.created',       'success', '2026-04-10 10:30:00'),
  ('61000000-0000-0000-0000-000000000008', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000023', 'resource.created',       'success', '2026-04-10 10:45:00'),
  ('61000000-0000-0000-0000-000000000009', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000011', 'resource.created',       'success', '2026-04-10 11:00:00'),

  -- Day 3: Services and proxies
  ('61000000-0000-0000-0000-000000000010', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000030', 'resource.created',       'success', '2026-04-10 14:00:00'),
  ('61000000-0000-0000-0000-000000000011', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000031', 'resource.created',       'success', '2026-04-10 14:10:00'),
  ('61000000-0000-0000-0000-000000000012', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000040', 'resource.created',       'success', '2026-04-10 14:30:00'),
  ('61000000-0000-0000-0000-000000000013', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000041', 'resource.created',       'success', '2026-04-10 14:35:00'),

  -- Day 4: Configuration work + a failure
  ('61000000-0000-0000-0000-000000000014', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000033', 'resource.created',       'success', '2026-04-11 08:00:00'),
  ('61000000-0000-0000-0000-000000000015', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000033', 'resource.updated',       'success', '2026-04-11 08:30:00'),
  ('61000000-0000-0000-0000-000000000016', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000034', 'resource.created',       'success', '2026-04-11 09:00:00'),
  ('61000000-0000-0000-0000-000000000017', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000034', 'resource.lifecycle_changed', 'success', '2026-04-11 10:00:00'),

  -- Day 5: Health incidents
  ('61000000-0000-0000-0000-000000000018', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000023', 'resource.health_changed', 'success', '2026-04-11 15:00:00'),
  ('61000000-0000-0000-0000-000000000019', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000027', 'resource.health_changed', 'success', '2026-04-11 16:00:00'),
  ('61000000-0000-0000-0000-000000000020', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000013', 'resource.health_changed', 'partial', '2026-04-11 16:30:00'),

  -- Day 6: Infrastructure changes with failures
  ('61000000-0000-0000-0000-000000000021', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000043', 'resource.updated',       'failure', '2026-04-12 09:00:00'),
  ('61000000-0000-0000-0000-000000000022', '30000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000043', 'resource.updated',       'success', '2026-04-12 09:15:00'),
  ('61000000-0000-0000-0000-000000000023', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000050', 'relation.created',       'success', '2026-04-12 10:00:00'),
  ('61000000-0000-0000-0000-000000000024', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000060', 'relation.created',       'failure', '2026-04-12 10:05:00'),
  ('61000000-0000-0000-0000-000000000025', '30000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000060', 'relation.created',       'success', '2026-04-12 10:10:00');
