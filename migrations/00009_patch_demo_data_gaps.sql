-- Patch demo data gaps: missing profiles and relations from 0004 seed.

-- +goose Up

-- ============================================================
-- Section 1: Missing profiles
-- ============================================================

-- order-mysql-replica-02-prod: database_instance with no profile
-- +goose StatementBegin
insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
select resources.id, seed.engine, seed.version, seed.host, seed.port, seed.role, seed.spec
from (
  select 'order-mysql-replica-02-prod' as resource_name, 'mysql' as engine, '8.0.36' as version, 'prod-db-host-03.internal' as host, 3308 as port, 'replica' as role, '{}' as spec
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- order-mysql-cluster-prod: database_cluster with basic profile from 0002, upgrade to richer spec
-- +goose StatementBegin
insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
select resources.id, seed.engine, seed.topology_mode, seed.primary_endpoint, seed.spec
from (
  select 'order-mysql-cluster-prod' as resource_name, 'mysql' as engine, 'primary-replica' as topology_mode, 'order-mysql-cluster-prod.internal:3306' as primary_endpoint, '{"replicas":2,"storage_class":"ssd"}' as spec
) seed
join resources on resources.name = seed.resource_name
on duplicate key update
  engine = values(engine),
  topology_mode = values(topology_mode),
  primary_endpoint = values(primary_endpoint),
  spec = values(spec);
-- +goose StatementEnd

-- notification-service-prod: service with no profile
-- +goose StatementBegin
insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
select resources.id, seed.system_name, seed.repository_url, seed.runtime_env, seed.spec
from (
  select 'notification-service-prod' as resource_name, 'notification-service' as system_name, 'https://git.internal/order/notification' as repository_url, 'kubernetes' as runtime_env, '{"language":"node","framework":"express","disabled_reason":"kafka-migration"}' as spec
) seed
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- ============================================================
-- Section 2: Missing relations
-- ============================================================

-- +goose StatementBegin
insert into resource_relations (from_resource_id, to_resource_id, relation_type)
select src.id, dst.id, seed.relation_type
from (
  select 'order-mysql-replica-01-prod' as from_name, 'order-mysql-replica-02-prod' as to_name, 'replicates_to' as relation_type
  union all select 'order-api-staging', 'staging-app-host-01', 'runs_on'
  union all select 'payment-api-staging', 'staging-app-host-01', 'runs_on'
  union all select 'order-api-dev', 'dev-db-host-01', 'runs_on'
) seed
join resources src on src.name = seed.from_name
join resources dst on dst.name = seed.to_name;
-- +goose StatementEnd

-- +goose Down

-- Reverse relations (exact match on name-based join)
-- +goose StatementBegin
delete rel
from resource_relations rel
join resources src on src.id = rel.from_resource_id
join resources dst on dst.id = rel.to_resource_id
where (src.name, dst.name, rel.relation_type) in (
  ('order-mysql-replica-01-prod', 'order-mysql-replica-02-prod', 'replicates_to'),
  ('order-api-staging', 'staging-app-host-01', 'runs_on'),
  ('payment-api-staging', 'staging-app-host-01', 'runs_on'),
  ('order-api-dev', 'dev-db-host-01', 'runs_on')
);
-- +goose StatementEnd

-- Reverse notification-service-prod profile
-- +goose StatementBegin
delete prof
from resource_profiles_service prof
join resources on resources.id = prof.resource_id
where resources.name = 'notification-service-prod';
-- +goose StatementEnd

-- Revert order-mysql-cluster-prod profile to original 0002 values
-- +goose StatementBegin
update resource_profiles_database_cluster prof
join resources on resources.id = prof.resource_id
set prof.spec = '{"replicas":2}'
where resources.name = 'order-mysql-cluster-prod';
-- +goose StatementEnd

-- Reverse order-mysql-replica-02-prod profile
-- +goose StatementBegin
delete prof
from resource_profiles_database_instance prof
join resources on resources.id = prof.resource_id
where resources.name = 'order-mysql-replica-02-prod';
-- +goose StatementEnd
