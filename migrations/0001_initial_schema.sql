-- ControlHub initial schema for MySQL 8.0+

-- +goose Up
-- +goose StatementBegin
create table roles (
  id bigint unsigned not null auto_increment primary key,
  name varchar(255) not null unique,
  description text not null,
  created_at datetime not null default current_timestamp
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table users (
  id bigint unsigned not null auto_increment primary key,
  email varchar(255) not null unique,
  password_hash varchar(255) not null,
  display_name varchar(255) not null,
  role_id bigint unsigned not null,
  created_at datetime not null default current_timestamp,
  index idx_users_role_id (role_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table environments (
  id bigint unsigned not null auto_increment primary key,
  name varchar(255) not null unique,
  slug varchar(255) not null unique,
  description text not null,
  created_at datetime not null default current_timestamp
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table owners (
  id bigint unsigned not null auto_increment primary key,
  name varchar(255) not null,
  email varchar(255) not null unique,
  created_at datetime not null default current_timestamp
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resources (
  id bigint unsigned not null auto_increment primary key,
  resource_type varchar(64) not null,
  resource_subtype varchar(64) not null default '',
  name varchar(255) not null unique,
  display_name varchar(255) not null,
  environment_id bigint unsigned not null,
  owner_id bigint unsigned not null,
  lifecycle_status varchar(64) not null,
  health_status varchar(64) not null,
  labels json not null,
  source varchar(64) not null default 'manual',
  external_id varchar(255) not null default '',
  created_at datetime not null default current_timestamp,
  updated_at datetime not null default current_timestamp on update current_timestamp,
  constraint chk_resource_type check (
    resource_type in ('host', 'database_instance', 'database_cluster', 'service')
  )
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_relations (
  id bigint unsigned not null auto_increment primary key,
  from_resource_id bigint unsigned not null,
  to_resource_id bigint unsigned not null,
  relation_type varchar(64) not null,
  created_at datetime not null default current_timestamp,
  constraint chk_no_self_link check (from_resource_id <> to_resource_id),
  unique key uq_relation (from_resource_id, to_resource_id, relation_type)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_host (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  hostname varchar(255) not null,
  ip_address varchar(64) not null,
  os_name varchar(255) not null,
  spec json not null,
  unique key uq_resource_profiles_host_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_database_instance (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  engine varchar(64) not null,
  version varchar(64) not null,
  host varchar(255) not null,
  port int not null,
  role varchar(64) not null,
  spec json not null,
  unique key uq_resource_profiles_database_instance_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_database_cluster (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  engine varchar(64) not null,
  topology_mode varchar(64) not null,
  primary_endpoint varchar(255) not null,
  spec json not null,
  unique key uq_resource_profiles_database_cluster_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_service (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  system_name varchar(255) not null,
  repository_url varchar(512) not null,
  runtime_env varchar(64) not null,
  spec json not null,
  unique key uq_resource_profiles_service_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- audit_events: bootstrap/demo placeholder only.
-- Phase-1 stores events in MySQL for local development; the long-term
-- backing store will be ClickHouse. FK constraints are intentionally
-- omitted so that resource write paths do not depend on this table.
-- +goose StatementBegin
create table audit_events (
  id bigint unsigned not null auto_increment primary key,
  actor_user_id bigint unsigned not null,
  target_resource_id bigint unsigned default null,
  event_type varchar(64) not null,
  result varchar(64) not null,
  created_at datetime not null default current_timestamp
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create index idx_resources_type on resources(resource_type);
-- +goose StatementEnd
-- +goose StatementBegin
create index idx_resources_environment on resources(environment_id);
-- +goose StatementEnd
-- +goose StatementBegin
create index idx_resources_health on resources(health_status);
-- +goose StatementEnd
-- +goose StatementBegin
create index idx_relations_from on resource_relations(from_resource_id);
-- +goose StatementEnd
-- +goose StatementBegin
create index idx_relations_to on resource_relations(to_resource_id);
-- +goose StatementEnd
-- +goose StatementBegin
create index idx_audit_target on audit_events(target_resource_id);
-- +goose StatementEnd

-- +goose Down
-- Drop in reverse dependency order.
-- +goose StatementBegin
drop index idx_audit_target on audit_events;
-- +goose StatementEnd
-- +goose StatementBegin
drop index idx_relations_to on resource_relations;
-- +goose StatementEnd
-- +goose StatementBegin
drop index idx_relations_from on resource_relations;
-- +goose StatementEnd
-- +goose StatementBegin
drop index idx_resources_health on resources;
-- +goose StatementEnd
-- +goose StatementBegin
drop index idx_resources_environment on resources;
-- +goose StatementEnd
-- +goose StatementBegin
drop index idx_resources_type on resources;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists audit_events;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resource_profiles_service;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resource_profiles_database_cluster;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resource_profiles_database_instance;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resource_profiles_host;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resource_relations;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists resources;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists owners;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists environments;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists users;
-- +goose StatementEnd
-- +goose StatementBegin
drop table if exists roles;
-- +goose StatementEnd
