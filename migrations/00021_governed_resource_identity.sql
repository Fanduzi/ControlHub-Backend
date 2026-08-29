-- input: resources identity columns and legacy external_id values at schema version 18
-- output: immutable origin plus normalized aliases and system/value external identifiers
-- pos: forward-only CMDB identity migration with fail-loud legacy external-id preflight
-- note: if this file changes, update header and internal/repository/mysql/README.md

-- +goose Up
-- +goose StatementBegin
create procedure migrate_resource_identity_76()
begin
  if exists (
    select external_id
    from resources
    where external_id <> ''
    group by external_id
    having count(*) > 1
  ) then
    signal sqlstate '45000'
      set message_text = 'cannot migrate duplicate resources.external_id values';
  end if;
  if exists (
    select 1 from resources
    where source not in ('manual', 'import', 'imported', 'terraform', 'discovery', 'discovered')
  ) then
    signal sqlstate '45000'
      set message_text = 'cannot migrate unsupported resources.source values';
  end if;
end;
-- +goose StatementEnd

call migrate_resource_identity_76();

drop procedure migrate_resource_identity_76;

-- +goose StatementBegin
alter table resources
  drop index uq_resource_name_env,
  change column source origin varchar(64) not null default 'manual',
  add unique key uq_resource_name_env_type (name, environment_id, resource_type);
-- +goose StatementEnd

update resources set origin = 'imported' where origin = 'import';
update resources set origin = 'imported' where origin = 'terraform';
update resources set origin = 'discovered' where origin = 'discovery';

alter table resources
  add constraint chk_resource_origin check (origin in ('manual', 'imported', 'discovered'));

-- +goose StatementBegin
create table resource_aliases (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  environment_id bigint unsigned not null,
  alias varchar(255) not null,
  created_at datetime not null default current_timestamp,
  constraint chk_resource_alias_normalized check (binary alias = binary lower(trim(alias)) and alias <> ''),
  unique key uq_resource_alias_env (environment_id, alias),
  unique key uq_resource_alias_resource (resource_id, alias)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_external_identifiers (
  id bigint unsigned not null auto_increment primary key,
  resource_id bigint unsigned not null,
  external_system varchar(128) not null,
  external_value varchar(255) collate utf8mb4_0900_bin not null,
  created_at datetime not null default current_timestamp,
  constraint chk_resource_external_identifier check (
    binary external_system = binary lower(trim(external_system))
    and trim(external_value) <> ''
  ),
  unique key uq_resource_external_identifier (external_system, external_value),
  unique key uq_resource_external_identifier_resource (resource_id, external_system, external_value)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose StatementBegin
insert into resource_external_identifiers (resource_id, external_system, external_value)
select id, 'legacy', external_id
from resources
where external_id <> '';
-- +goose StatementEnd

-- +goose StatementBegin
alter table resources drop column external_id;
-- +goose StatementEnd

-- +goose StatementBegin
create trigger resources_identity_immutable
before update on resources
for each row
begin
  if new.id <> old.id then
    signal sqlstate '45000' set message_text = 'resource id is immutable';
  end if;
  if new.origin <> old.origin then
    signal sqlstate '45000' set message_text = 'resource origin is immutable';
  end if;
end;
-- +goose StatementEnd

-- +goose Down
drop trigger resources_identity_immutable;

alter table resources add column external_id varchar(255) not null default '' after origin;

update resources r
join resource_external_identifiers rei
  on rei.resource_id = r.id and rei.external_system = 'legacy'
set r.external_id = rei.external_value;

drop table resource_external_identifiers;
drop table resource_aliases;

alter table resources
  drop index uq_resource_name_env_type,
  drop constraint chk_resource_origin,
  change column origin source varchar(64) not null default 'manual',
  add unique key uq_resource_name_env (name, environment_id);
