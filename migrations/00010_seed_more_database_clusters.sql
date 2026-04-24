-- Add more database clusters and instances for tree-view testing (at least 15 clusters total).
-- Existing clusters: 8 (order-mysql-cluster-prod, payment-mysql-cluster-prod, user-redis-cluster-prod,
--   analytics-ch-cluster-prod, config-mysql-cluster-prod, order-mysql-cluster-staging,
--   payment-mysql-cluster-staging, dev-mysql-cluster)
-- New clusters: 8 more = 16 total

-- +goose Up

-- ============================================================
-- Section 1: New resources (clusters + instances)
-- ============================================================

-- +goose StatementBegin
insert into resources (
  resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
)
select
  seed.resource_type, seed.resource_subtype, seed.name, seed.display_name,
  environments.id, owners.id, seed.lifecycle_status, seed.health_status,
  seed.labels, seed.source, seed.external_id
from (
  -- Cluster 9: User MySQL Cluster Production
  select 'database_cluster' as resource_type, 'mysql' as resource_subtype,
         'user-mysql-cluster-prod' as name, 'User Service MySQL Cluster Production' as display_name,
         'prod' as environment_slug, 'dba@example.com' as owner_email,
         'running' as lifecycle_status, 'healthy' as health_status,
         '{"team":"user","tier":"data"}' as labels, 'manual' as source, 'dbaas-user-mysql-cluster-prod' as external_id
  union all select 'database_instance', 'mysql', 'user-mysql-primary-prod', 'User MySQL Primary Production',
         'prod', 'dba@example.com', 'running', 'healthy',
         '{"team":"user","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'user-mysql-replica-prod', 'User MySQL Replica Production',
         'prod', 'dba@example.com', 'running', 'healthy',
         '{"team":"user","tier":"data","replication_lag_seconds":"2"}', 'manual', ''

  -- Cluster 10: TiDB Analytics Cluster Production
  union all select 'database_cluster', 'tidb', 'analytics-tidb-cluster-prod', 'Analytics TiDB Cluster Production',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","tier":"data","engine":"tidb"}', 'manual', 'dbaas-analytics-tidb-prod'
  union all select 'database_instance', 'tidb', 'analytics-tidb-pd-01-prod', 'Analytics TiDB PD Node 01',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","tidb_role":"pd"}', 'manual', ''
  union all select 'database_instance', 'tidb', 'analytics-tidb-tikv-01-prod', 'Analytics TiDB TiKV Node 01',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","tidb_role":"tikv"}', 'manual', ''
  union all select 'database_instance', 'tidb', 'analytics-tidb-tidb-01-prod', 'Analytics TiDB Compute Node 01',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","tidb_role":"compute"}', 'manual', ''

  -- Cluster 11: MongoDB User Data Production
  union all select 'database_cluster', 'mongodb', 'user-mongodb-cluster-prod', 'User Data MongoDB Cluster Production',
         'prod', 'dba@example.com', 'running', 'healthy',
         '{"team":"user","tier":"data","engine":"mongodb"}', 'manual', 'dbaas-user-mongodb-prod'
  union all select 'database_instance', 'mongodb', 'user-mongo-primary-prod', 'User MongoDB Primary Production',
         'prod', 'dba@example.com', 'running', 'healthy',
         '{"team":"user","mongodb_role":"primary"}', 'manual', ''
  union all select 'database_instance', 'mongodb', 'user-mongo-secondary-01-prod', 'User MongoDB Secondary 01 Production',
         'prod', 'dba@example.com', 'running', 'healthy',
         '{"team":"user","mongodb_role":"secondary"}', 'manual', ''
  union all select 'database_instance', 'mongodb', 'user-mongo-secondary-02-prod', 'User MongoDB Secondary 02 Production',
         'prod', 'dba@example.com', 'running', 'warning',
         '{"team":"user","mongodb_role":"secondary","oplog_lag_seconds":"120","alert":"oplog-lag"}', 'manual', ''

  -- Cluster 12: Redis Cache Cluster Production (payment team)
  union all select 'database_cluster', 'redis', 'payment-redis-cluster-prod', 'Payment Redis Cache Cluster Production',
         'prod', 'payment-team@example.com', 'running', 'healthy',
         '{"team":"payment","tier":"cache"}', 'manual', 'dbaas-payment-redis-prod'
  union all select 'database_instance', 'redis', 'payment-redis-primary-prod', 'Payment Redis Primary Production',
         'prod', 'payment-team@example.com', 'running', 'healthy',
         '{"team":"payment","tier":"cache","memory_max_gb":"64"}', 'manual', ''
  union all select 'database_instance', 'redis', 'payment-redis-replica-prod', 'Payment Redis Replica Production',
         'prod', 'payment-team@example.com', 'running', 'healthy',
         '{"team":"payment","tier":"cache"}', 'manual', ''

  -- Cluster 13: MySQL Order Reporting Cluster Production
  union all select 'database_cluster', 'mysql', 'order-reporting-cluster-prod', 'Order Reporting MySQL Cluster Production',
         'prod', 'order-team@example.com', 'running', 'healthy',
         '{"team":"order","tier":"data","purpose":"reporting"}', 'manual', 'dbaas-order-reporting-prod'
  union all select 'database_instance', 'mysql', 'order-reporting-primary-prod', 'Order Reporting Primary Production',
         'prod', 'order-team@example.com', 'running', 'healthy',
         '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-reporting-replica-01-prod', 'Order Reporting Replica 01 Production',
         'prod', 'order-team@example.com', 'running', 'healthy',
         '{"team":"order","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'order-reporting-replica-02-prod', 'Order Reporting Replica 02 Production',
         'prod', 'order-team@example.com', 'stopped', 'healthy',
         '{"team":"order","tier":"data","stopped_reason":"maintenance"}', 'manual', ''

  -- Cluster 14: ClickHouse Event Tracking Cluster Production
  union all select 'database_cluster', 'clickhouse', 'event-ch-cluster-prod', 'Event Tracking ClickHouse Cluster Production',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","tier":"data","engine":"clickhouse"}', 'manual', 'dbaas-event-ch-prod'
  union all select 'database_instance', 'clickhouse', 'event-ch-node-01-prod', 'Event CH Node 01 Production',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","clickhouse_role":"replica"}', 'manual', ''
  union all select 'database_instance', 'clickhouse', 'event-ch-node-02-prod', 'Event CH Node 02 Production',
         'prod', 'analytics-team@example.com', 'running', 'healthy',
         '{"team":"analytics","clickhouse_role":"replica"}', 'manual', ''

  -- Cluster 15: MySQL Config Cluster Staging
  union all select 'database_cluster', 'mysql', 'config-mysql-cluster-staging', 'Platform Config MySQL Cluster Staging',
         'staging', 'dba@example.com', 'running', 'healthy',
         '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'config-mysql-primary-staging', 'Config MySQL Primary Staging',
         'staging', 'dba@example.com', 'running', 'healthy',
         '{"team":"platform","tier":"data"}', 'manual', ''
  union all select 'database_instance', 'mysql', 'config-mysql-replica-staging', 'Config MySQL Replica Staging',
         'staging', 'dba@example.com', 'running', 'healthy',
         '{"team":"platform","tier":"data"}', 'manual', ''

  -- Cluster 16: Redis Session Cluster Staging
  union all select 'database_cluster', 'redis', 'session-redis-cluster-staging', 'Session Redis Cluster Staging',
         'staging', 'order-team@example.com', 'provisioning', 'healthy',
         '{"team":"platform","tier":"cache"}', 'manual', ''
  union all select 'database_instance', 'redis', 'session-redis-primary-staging', 'Session Redis Primary Staging',
         'staging', 'order-team@example.com', 'provisioning', 'healthy',
         '{"team":"platform","tier":"cache"}', 'manual', ''
) seed
join environments on environments.slug = seed.environment_slug
join owners on owners.email = seed.owner_email;
-- +goose StatementEnd

-- ============================================================
-- Section 2: Cluster profiles
-- ============================================================

-- +goose StatementBegin
insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
select resources.id, seed.engine, seed.topology_mode, seed.primary_endpoint, seed.spec
from (
  select 'user-mysql-cluster-prod' as resource_name, 'mysql' as engine, 'primary-replica' as topology_mode, 'user-mysql.internal:3306' as primary_endpoint, '{"replicas":1,"storage_class":"ssd","storage_gb":500}' as spec
  union all select 'analytics-tidb-cluster-prod', 'tidb', 'distributed', 'analytics-tidb.internal:4000', '{"pd_nodes":1,"tikv_nodes":1,"compute_nodes":1}'
  union all select 'user-mongodb-cluster-prod', 'mongodb', 'replica_set', 'user-mongo.internal:27017', '{"replicas":2,"storage_class":"ssd","sharding_enabled":false}'
  union all select 'payment-redis-cluster-prod', 'redis', 'sentinel', 'payment-redis.internal:6379', '{"replicas":1,"memory_max_gb":"64","eviction_policy":"allkeys-lru"}'
  union all select 'order-reporting-cluster-prod', 'mysql', 'primary-replica', 'order-reporting.internal:3306', '{"replicas":2,"storage_class":"hdd","storage_gb":2000}'
  union all select 'event-ch-cluster-prod', 'clickhouse', 'cluster', 'event-ch.internal:8123', '{"replicas":2,"shards":1,"compression":"lz4"}'
  union all select 'config-mysql-cluster-staging', 'mysql', 'primary-replica', 'config-mysql-staging.internal:3306', '{"replicas":1,"storage_class":"ssd"}'
  union all select 'session-redis-cluster-staging', 'redis', 'standalone', 'session-redis-staging.internal:6379', '{"memory_max_gb":"16"}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- ============================================================
-- Section 3: Instance profiles
-- ============================================================

-- +goose StatementBegin
insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
select resources.id, seed.engine, seed.version, seed.host, seed.port, seed.role, seed.spec
from (
  select 'user-mysql-primary-prod' as resource_name, 'mysql' as engine, '8.0.36' as version, 'user-mysql-01.internal' as host, 3306 as port, 'primary' as role, '{"innodb_buffer_pool_gb":8}' as spec
  union all select 'user-mysql-replica-prod', 'mysql', '8.0.36', 'user-mysql-02.internal', 3306, 'replica', '{"innodb_buffer_pool_gb":8}'
  union all select 'analytics-tidb-pd-01-prod', 'tidb', '7.5.0', 'tidb-pd-01.internal', 2379, 'pd', '{}'
  union all select 'analytics-tidb-tikv-01-prod', 'tidb', '7.5.0', 'tidb-tikv-01.internal', 20160, 'tikv', '{"storage_gb":1000}'
  union all select 'analytics-tidb-tidb-01-prod', 'tidb', '7.5.0', 'tidb-compute-01.internal', 4000, 'compute', '{}'
  union all select 'user-mongo-primary-prod', 'mongodb', '7.0.5', 'user-mongo-01.internal', 27017, 'primary', '{"wiredtiger_cache_gb":16}'
  union all select 'user-mongo-secondary-01-prod', 'mongodb', '7.0.5', 'user-mongo-02.internal', 27017, 'secondary', '{"wiredtiger_cache_gb":16}'
  union all select 'user-mongo-secondary-02-prod', 'mongodb', '7.0.5', 'user-mongo-03.internal', 27017, 'secondary', '{"wiredtiger_cache_gb":16}'
  union all select 'payment-redis-primary-prod', 'redis', '7.2.4', 'payment-redis-01.internal', 6379, 'primary', '{"maxmemory_policy":"allkeys-lru"}'
  union all select 'payment-redis-replica-prod', 'redis', '7.2.4', 'payment-redis-02.internal', 6379, 'replica', '{}'
  union all select 'order-reporting-primary-prod', 'mysql', '8.0.36', 'order-rpt-01.internal', 3306, 'primary', '{"innodb_buffer_pool_gb":32,"read_only":false}'
  union all select 'order-reporting-replica-01-prod', 'mysql', '8.0.36', 'order-rpt-02.internal', 3306, 'replica', '{"innodb_buffer_pool_gb":32}'
  union all select 'order-reporting-replica-02-prod', 'mysql', '8.0.36', 'order-rpt-03.internal', 3306, 'replica', '{"innodb_buffer_pool_gb":32}'
  union all select 'event-ch-node-01-prod', 'clickhouse', '24.3', 'event-ch-01.internal', 8123, 'replica', '{}'
  union all select 'event-ch-node-02-prod', 'clickhouse', '24.3', 'event-ch-02.internal', 8123, 'replica', '{}'
  union all select 'config-mysql-primary-staging', 'mysql', '8.0.36', 'config-mysql-s-01.internal', 3306, 'primary', '{}'
  union all select 'config-mysql-replica-staging', 'mysql', '8.0.36', 'config-mysql-s-02.internal', 3306, 'replica', '{}'
  union all select 'session-redis-primary-staging', 'redis', '7.2.4', 'session-redis-s-01.internal', 6379, 'primary', '{}'
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- ============================================================
-- Section 4: Cluster-Instance relations (member_of)
-- ============================================================

-- +goose StatementBegin
insert into resource_relations (from_resource_id, to_resource_id, relation_type)
select src.id, dst.id, 'member_of'
from (
  select 'user-mysql-primary-prod' as from_name, 'user-mysql-cluster-prod' as to_name
  union all select 'user-mysql-replica-prod', 'user-mysql-cluster-prod'
  union all select 'analytics-tidb-pd-01-prod', 'analytics-tidb-cluster-prod'
  union all select 'analytics-tidb-tikv-01-prod', 'analytics-tidb-cluster-prod'
  union all select 'analytics-tidb-tidb-01-prod', 'analytics-tidb-cluster-prod'
  union all select 'user-mongo-primary-prod', 'user-mongodb-cluster-prod'
  union all select 'user-mongo-secondary-01-prod', 'user-mongodb-cluster-prod'
  union all select 'user-mongo-secondary-02-prod', 'user-mongodb-cluster-prod'
  union all select 'payment-redis-primary-prod', 'payment-redis-cluster-prod'
  union all select 'payment-redis-replica-prod', 'payment-redis-cluster-prod'
  union all select 'order-reporting-primary-prod', 'order-reporting-cluster-prod'
  union all select 'order-reporting-replica-01-prod', 'order-reporting-cluster-prod'
  union all select 'order-reporting-replica-02-prod', 'order-reporting-cluster-prod'
  union all select 'event-ch-node-01-prod', 'event-ch-cluster-prod'
  union all select 'event-ch-node-02-prod', 'event-ch-cluster-prod'
  union all select 'config-mysql-primary-staging', 'config-mysql-cluster-staging'
  union all select 'config-mysql-replica-staging', 'config-mysql-cluster-staging'
  union all select 'session-redis-primary-staging', 'session-redis-cluster-staging'
) seed
join resources src on src.name = seed.from_name
join resources dst on dst.name = seed.to_name;
-- +goose StatementEnd

-- +goose Down

-- Reverse relations
-- +goose StatementBegin
delete rel
from resource_relations rel
join resources src on src.id = rel.from_resource_id
join resources dst on dst.id = rel.to_resource_id
where (src.name, dst.name, rel.relation_type) in (
  ('user-mysql-primary-prod', 'user-mysql-cluster-prod', 'member_of'),
  ('user-mysql-replica-prod', 'user-mysql-cluster-prod', 'member_of'),
  ('analytics-tidb-pd-01-prod', 'analytics-tidb-cluster-prod', 'member_of'),
  ('analytics-tidb-tikv-01-prod', 'analytics-tidb-cluster-prod', 'member_of'),
  ('analytics-tidb-tidb-01-prod', 'analytics-tidb-cluster-prod', 'member_of'),
  ('user-mongo-primary-prod', 'user-mongodb-cluster-prod', 'member_of'),
  ('user-mongo-secondary-01-prod', 'user-mongodb-cluster-prod', 'member_of'),
  ('user-mongo-secondary-02-prod', 'user-mongodb-cluster-prod', 'member_of'),
  ('payment-redis-primary-prod', 'payment-redis-cluster-prod', 'member_of'),
  ('payment-redis-replica-prod', 'payment-redis-cluster-prod', 'member_of'),
  ('order-reporting-primary-prod', 'order-reporting-cluster-prod', 'member_of'),
  ('order-reporting-replica-01-prod', 'order-reporting-cluster-prod', 'member_of'),
  ('order-reporting-replica-02-prod', 'order-reporting-cluster-prod', 'member_of'),
  ('event-ch-node-01-prod', 'event-ch-cluster-prod', 'member_of'),
  ('event-ch-node-02-prod', 'event-ch-cluster-prod', 'member_of'),
  ('config-mysql-primary-staging', 'config-mysql-cluster-staging', 'member_of'),
  ('config-mysql-replica-staging', 'config-mysql-cluster-staging', 'member_of'),
  ('session-redis-primary-staging', 'session-redis-cluster-staging', 'member_of')
);
-- +goose StatementEnd

-- Reverse instance profiles
-- +goose StatementBegin
delete prof
from resource_profiles_database_instance prof
join resources on resources.id = prof.resource_id
where resources.name in (
  'user-mysql-primary-prod', 'user-mysql-replica-prod',
  'analytics-tidb-pd-01-prod', 'analytics-tidb-tikv-01-prod', 'analytics-tidb-tidb-01-prod',
  'user-mongo-primary-prod', 'user-mongo-secondary-01-prod', 'user-mongo-secondary-02-prod',
  'payment-redis-primary-prod', 'payment-redis-replica-prod',
  'order-reporting-primary-prod', 'order-reporting-replica-01-prod', 'order-reporting-replica-02-prod',
  'event-ch-node-01-prod', 'event-ch-node-02-prod',
  'config-mysql-primary-staging', 'config-mysql-replica-staging',
  'session-redis-primary-staging'
);
-- +goose StatementEnd

-- Reverse cluster profiles
-- +goose StatementBegin
delete prof
from resource_profiles_database_cluster prof
join resources on resources.id = prof.resource_id
where resources.name in (
  'user-mysql-cluster-prod', 'analytics-tidb-cluster-prod', 'user-mongodb-cluster-prod',
  'payment-redis-cluster-prod', 'order-reporting-cluster-prod', 'event-ch-cluster-prod',
  'config-mysql-cluster-staging', 'session-redis-cluster-staging'
);
-- +goose StatementEnd

-- Reverse resources
-- +goose StatementBegin
delete from resources where name in (
  'user-mysql-cluster-prod', 'user-mysql-primary-prod', 'user-mysql-replica-prod',
  'analytics-tidb-cluster-prod', 'analytics-tidb-pd-01-prod', 'analytics-tidb-tikv-01-prod', 'analytics-tidb-tidb-01-prod',
  'user-mongodb-cluster-prod', 'user-mongo-primary-prod', 'user-mongo-secondary-01-prod', 'user-mongo-secondary-02-prod',
  'payment-redis-cluster-prod', 'payment-redis-primary-prod', 'payment-redis-replica-prod',
  'order-reporting-cluster-prod', 'order-reporting-primary-prod', 'order-reporting-replica-01-prod', 'order-reporting-replica-02-prod',
  'event-ch-cluster-prod', 'event-ch-node-01-prod', 'event-ch-node-02-prod',
  'config-mysql-cluster-staging', 'config-mysql-primary-staging', 'config-mysql-replica-staging',
  'session-redis-cluster-staging', 'session-redis-primary-staging'
);
-- +goose StatementEnd
