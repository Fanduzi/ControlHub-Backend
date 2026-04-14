-- ControlHub seed reference data.
-- Down migration deletes by fixed IDs. This is dev-oriented and NOT a production rollback strategy.

-- +goose Up
-- +goose StatementBegin
insert into roles (id, name, description) values
  ('00000000-0000-0000-0000-000000000001', 'admin', 'Full platform access'),
  ('00000000-0000-0000-0000-000000000002', 'editor', 'Can manage assets and relations');
-- +goose StatementEnd

-- +goose StatementBegin
insert into environments (id, name, slug, description) values
  ('10000000-0000-0000-0000-000000000001', 'Production', 'prod', 'Production environment'),
  ('10000000-0000-0000-0000-000000000002', 'Staging', 'staging', 'Staging environment');
-- +goose StatementEnd

-- +goose StatementBegin
insert into owners (id, name, email) values
  ('20000000-0000-0000-0000-000000000001', 'Platform Team', 'platform@example.com'),
  ('20000000-0000-0000-0000-000000000002', 'DBA Team', 'dba@example.com');
-- +goose StatementEnd

-- +goose StatementBegin
insert into users (id, email, password_hash, display_name, role_id) values
  (
    '30000000-0000-0000-0000-000000000001',
    'admin@example.com',
    'fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4',
    'ControlHub Admin',
    '00000000-0000-0000-0000-000000000001'
  ),
  (
    '30000000-0000-0000-0000-000000000002',
    'editor@example.com',
    'fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4',
    'ControlHub Editor',
    '00000000-0000-0000-0000-000000000002'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resources (
  id,
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
) values
  (
    '40000000-0000-0000-0000-000000000001',
    'database_cluster',
    'mysql',
    'order-mysql-cluster-prod',
    'Order MySQL Cluster Prod',
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000002',
    'running',
    'healthy',
    '{"team":"order","tier":"data"}',
    'manual',
    ''
  ),
  (
    '40000000-0000-0000-0000-000000000002',
    'database_instance',
    'mysql',
    'order-mysql-01-prod',
    'Order MySQL 01 Prod',
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000002',
    'running',
    'healthy',
    '{"team":"order","tier":"data"}',
    'manual',
    ''
  ),
  (
    '40000000-0000-0000-0000-000000000003',
    'service',
    'api',
    'order-api-prod',
    'Order API Prod',
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    'running',
    'healthy',
    '{"team":"order","tier":"app"}',
    'manual',
    ''
  ),
  (
    '40000000-0000-0000-0000-000000000004',
    'host',
    'vm',
    'prod-db-host-01',
    'Prod DB Host 01',
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    'running',
    'healthy',
    '{"team":"platform","tier":"infra"}',
    'manual',
    ''
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec) values
  (
    '40000000-0000-0000-0000-000000000001',
    'mysql',
    'primary-replica',
    'order-mysql-cluster-prod.internal:3306',
    '{"replicas":2}'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values
  (
    '40000000-0000-0000-0000-000000000002',
    'mysql',
    '8.0.36',
    'prod-db-host-01.internal',
    3306,
    'primary',
    '{"storageClass":"ssd"}'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec) values
  (
    '40000000-0000-0000-0000-000000000003',
    'order-api',
    'https://example.com/repos/order-api',
    'kubernetes',
    '{"language":"go"}'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec) values
  (
    '40000000-0000-0000-0000-000000000004',
    'prod-db-host-01.internal',
    '10.0.10.21',
    'Ubuntu 24.04',
    '{"provider":"vmware"}'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_relations (id, from_resource_id, to_resource_id, relation_type) values
  (
    '50000000-0000-0000-0000-000000000001',
    '40000000-0000-0000-0000-000000000003',
    '40000000-0000-0000-0000-000000000002',
    'depends_on'
  ),
  (
    '50000000-0000-0000-0000-000000000002',
    '40000000-0000-0000-0000-000000000002',
    '40000000-0000-0000-0000-000000000001',
    'member_of'
  ),
  (
    '50000000-0000-0000-0000-000000000003',
    '40000000-0000-0000-0000-000000000002',
    '40000000-0000-0000-0000-000000000004',
    'runs_on'
  );
-- +goose StatementEnd

-- +goose StatementBegin
insert into audit_events (id, actor_user_id, target_resource_id, event_type, result) values
  (
    '60000000-0000-0000-0000-000000000001',
    '30000000-0000-0000-0000-000000000001',
    '40000000-0000-0000-0000-000000000002',
    'resource.created',
    'success'
  ),
  (
    '60000000-0000-0000-0000-000000000002',
    '30000000-0000-0000-0000-000000000001',
    '40000000-0000-0000-0000-000000000003',
    'relation.created',
    'success'
  );
-- +goose StatementEnd

-- +goose Down
-- WARNING: Deletes seed data by fixed IDs. Dev-oriented, NOT a production rollback strategy.
-- +goose StatementBegin
delete from audit_events where id like '60000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resource_relations where id like '50000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resource_profiles_host where resource_id like '40000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resource_profiles_service where resource_id like '40000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resource_profiles_database_instance where resource_id like '40000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resource_profiles_database_cluster where resource_id like '40000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from resources where id like '40000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from users where id like '30000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from owners where id like '20000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from environments where id like '10000000-0000-0000-0000-%';
-- +goose StatementEnd
-- +goose StatementBegin
delete from roles where id like '00000000-0000-0000-0000-%';
-- +goose StatementEnd
