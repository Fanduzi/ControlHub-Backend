create table roles (
  id uuid primary key,
  name text not null unique,
  description text not null,
  created_at timestamptz not null default now()
);

create table users (
  id uuid primary key,
  email text not null unique,
  password_hash text not null,
  display_name text not null,
  role_id uuid not null references roles(id),
  created_at timestamptz not null default now()
);

create table environments (
  id uuid primary key,
  name text not null unique,
  slug text not null unique,
  description text not null,
  created_at timestamptz not null default now()
);

create table owners (
  id uuid primary key,
  name text not null,
  email text not null unique,
  created_at timestamptz not null default now()
);

create table resources (
  id uuid primary key,
  resource_type text not null,
  resource_subtype text not null default '',
  name text not null unique,
  display_name text not null,
  environment_id uuid not null references environments(id),
  owner_id uuid not null references owners(id),
  lifecycle_status text not null,
  health_status text not null,
  labels jsonb not null default '{}'::jsonb,
  source text not null default 'manual',
  external_id text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint resources_resource_type_check check (
    resource_type in ('host', 'database_instance', 'database_cluster', 'service')
  )
);

create table resource_relations (
  id uuid primary key,
  from_resource_id uuid not null references resources(id) on delete cascade,
  to_resource_id uuid not null references resources(id) on delete cascade,
  relation_type text not null,
  created_at timestamptz not null default now(),
  constraint resource_relations_no_self_link check (from_resource_id <> to_resource_id),
  constraint resource_relations_unique unique (from_resource_id, to_resource_id, relation_type)
);

create table resource_profiles_host (
  resource_id uuid primary key references resources(id) on delete cascade,
  hostname text not null,
  ip_address text not null,
  os_name text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_database_instance (
  resource_id uuid primary key references resources(id) on delete cascade,
  engine text not null,
  version text not null,
  host text not null,
  port integer not null,
  role text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_database_cluster (
  resource_id uuid primary key references resources(id) on delete cascade,
  engine text not null,
  topology_mode text not null,
  primary_endpoint text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_service (
  resource_id uuid primary key references resources(id) on delete cascade,
  system_name text not null,
  repository_url text not null,
  runtime_env text not null,
  spec jsonb not null default '{}'::jsonb
);

create table audit_events (
  id uuid primary key,
  actor_user_id uuid not null references users(id),
  target_resource_id uuid references resources(id),
  event_type text not null,
  result text not null,
  detail jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index idx_resources_type on resources(resource_type);
create index idx_resources_environment on resources(environment_id);
create index idx_resources_health on resources(health_status);
create index idx_resources_labels_gin on resources using gin(labels);
create index idx_relations_from on resource_relations(from_resource_id);
create index idx_relations_to on resource_relations(to_resource_id);
create index idx_audit_target on audit_events(target_resource_id);
