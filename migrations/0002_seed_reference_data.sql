-- ControlHub seed reference data.

-- +goose Up
-- +goose StatementBegin
insert into roles (name, description) values
  ('admin', 'Full platform access'),
  ('editor', 'Can manage assets and relations');
-- +goose StatementEnd

-- +goose StatementBegin
insert into environments (name, slug, description) values
  ('Production', 'prod', 'Production environment'),
  ('Staging', 'staging', 'Staging environment');
-- +goose StatementEnd

-- +goose StatementBegin
insert into owners (name, email) values
  ('Platform Team', 'platform@example.com'),
  ('DBA Team', 'dba@example.com');
-- +goose StatementEnd

-- +goose StatementBegin
insert into users (email, password_hash, display_name, role_id)
select
  seed.email,
  seed.password_hash,
  seed.display_name,
  roles.id
from (
  select 'admin@example.com' as email,
         'fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4' as password_hash,
         'ControlHub Admin' as display_name,
         'admin' as role_name
  union all
  select 'editor@example.com',
         'fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4',
         'ControlHub Editor',
         'editor'
) seed
join roles on roles.name = seed.role_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resources (
  resource_type,
  resource_subtype,
  name,
  display_name,
  environment_id,
  owner_id,
  lifecycle_status,
  health_status,
  labels,
  source,
  external_id
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
  select 'database_cluster' as resource_type, 'mysql' as resource_subtype,
         'order-mysql-cluster-prod' as name, 'Order MySQL Cluster Prod' as display_name,
         'prod' as environment_slug, 'dba@example.com' as owner_email,
         'running' as lifecycle_status, 'healthy' as health_status,
         '{"team":"order","tier":"data"}' as labels, 'manual' as source, '' as external_id
  union all
  select 'database_instance', 'mysql',
         'order-mysql-01-prod', 'Order MySQL 01 Prod',
         'prod', 'dba@example.com',
         'running', 'healthy',
         '{"team":"order","tier":"data"}', 'manual', ''
  union all
  select 'service', 'api',
         'order-api-prod', 'Order API Prod',
         'prod', 'platform@example.com',
         'running', 'healthy',
         '{"team":"order","tier":"app"}', 'manual', ''
  union all
  select 'host', 'vm',
         'prod-db-host-01', 'Prod DB Host 01',
         'prod', 'platform@example.com',
         'running', 'healthy',
         '{"team":"platform","tier":"infra"}', 'manual', ''
) seed
join environments on environments.slug = seed.environment_slug
join owners on owners.email = seed.owner_email;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
select resources.id, 'mysql', 'primary-replica', 'order-mysql-cluster-prod.internal:3306', '{"replicas":2}'
from resources
where resources.name = 'order-mysql-cluster-prod';
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
select resources.id, 'mysql', '8.0.36', 'prod-db-host-01.internal', 3306, 'primary', '{"storageClass":"ssd"}'
from resources
where resources.name = 'order-mysql-01-prod';
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
select resources.id, 'order-api', 'https://example.com/repos/order-api', 'kubernetes', '{"language":"go"}'
from resources
where resources.name = 'order-api-prod';
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec)
select resources.id, 'prod-db-host-01.internal', '10.0.10.21', 'Ubuntu 24.04', '{"provider":"vmware"}'
from resources
where resources.name = 'prod-db-host-01';
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_relations (from_resource_id, to_resource_id, relation_type)
select src.id, dst.id, seed.relation_type
from (
  select 'order-api-prod' as from_name, 'order-mysql-01-prod' as to_name, 'depends_on' as relation_type
  union all
  select 'order-mysql-01-prod', 'order-mysql-cluster-prod', 'member_of'
  union all
  select 'order-mysql-01-prod', 'prod-db-host-01', 'runs_on'
) seed
join resources src on src.name = seed.from_name
join resources dst on dst.name = seed.to_name;
-- +goose StatementEnd

-- +goose StatementBegin
insert into audit_events (actor_user_id, target_resource_id, event_type, result)
select users.id, resources.id, seed.event_type, seed.result
from (
  select 'admin@example.com' as actor_email, 'order-mysql-01-prod' as resource_name, 'resource.created' as event_type, 'success' as result
  union all
  select 'admin@example.com', 'order-api-prod', 'relation.created', 'success'
) seed
join users on users.email = seed.actor_email
join resources on resources.name = seed.resource_name;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
delete from audit_events
where (actor_user_id, target_resource_id, event_type, result) in (
  select users.id, resources.id, seed.event_type, seed.result
  from (
    select 'admin@example.com' as actor_email, 'order-mysql-01-prod' as resource_name, 'resource.created' as event_type, 'success' as result
    union all
    select 'admin@example.com', 'order-api-prod', 'relation.created', 'success'
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
  ('order-api-prod', 'order-mysql-01-prod', 'depends_on'),
  ('order-mysql-01-prod', 'order-mysql-cluster-prod', 'member_of'),
  ('order-mysql-01-prod', 'prod-db-host-01', 'runs_on')
);
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_host prof
join resources on resources.id = prof.resource_id
where resources.name = 'prod-db-host-01';
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_service prof
join resources on resources.id = prof.resource_id
where resources.name = 'order-api-prod';
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_database_instance prof
join resources on resources.id = prof.resource_id
where resources.name = 'order-mysql-01-prod';
-- +goose StatementEnd
-- +goose StatementBegin
delete prof
from resource_profiles_database_cluster prof
join resources on resources.id = prof.resource_id
where resources.name = 'order-mysql-cluster-prod';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resources where name in (
  'order-mysql-cluster-prod',
  'order-mysql-01-prod',
  'order-api-prod',
  'prod-db-host-01'
);
-- +goose StatementEnd
-- +goose StatementBegin
delete from users where email in ('admin@example.com', 'editor@example.com');
-- +goose StatementEnd
-- +goose StatementBegin
delete from owners where email in ('platform@example.com', 'dba@example.com');
-- +goose StatementEnd
-- +goose StatementBegin
delete from environments where slug in ('prod', 'staging');
-- +goose StatementEnd
-- +goose StatementBegin
delete from roles where name in ('admin', 'editor');
-- +goose StatementEnd
