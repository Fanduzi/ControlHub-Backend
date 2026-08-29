-- input: resources table and existing typed profile tables at schema version 18
-- output: resource_profiles_domain_name, resource_profiles_virtual_ip, subtype backfill
-- pos: typed minimum-identity profiles for Domain Name and Virtual IP
-- note: if this file changes, update header and internal/repository/mysql/README.md

-- +goose Up
-- +goose StatementBegin
create table resource_profiles_domain_name (
  id bigint unsigned not null auto_increment primary key comment 'Surrogate profile row id',
  resource_id bigint unsigned not null comment 'Owning Domain Name resource id',
  fqdn varchar(255) not null comment 'Normalized FQDN: lowercase, no trailing dot',
  spec json not null comment 'Reserved typed-profile extension payload',
  unique key uq_resource_profiles_domain_name_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci comment='Typed profile for domain_name resources; FQDN only, no resolution target';
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_virtual_ip (
  id bigint unsigned not null auto_increment primary key comment 'Surrogate profile row id',
  resource_id bigint unsigned not null comment 'Owning Virtual IP resource id',
  ip_address varchar(64) not null comment 'Single IPv4 or IPv6 address; no CIDR, port, or address set',
  spec json not null comment 'Reserved typed-profile extension payload',
  unique key uq_resource_profiles_virtual_ip_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci comment='Typed profile for virtual_ip resources; single address only';
-- +goose StatementEnd

-- +goose StatementBegin
update resources
set resource_subtype = 'dns'
where resource_type = 'domain_name' and resource_subtype = '';
-- +goose StatementEnd

-- +goose StatementBegin
update resources
set resource_subtype = 'floating'
where resource_type = 'virtual_ip' and resource_subtype = '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
update resources
set resource_subtype = ''
where resource_type = 'domain_name' and resource_subtype = 'dns';
-- +goose StatementEnd

-- +goose StatementBegin
update resources
set resource_subtype = ''
where resource_type = 'virtual_ip' and resource_subtype = 'floating';
-- +goose StatementEnd

-- +goose StatementBegin
drop table if exists resource_profiles_virtual_ip;
-- +goose StatementEnd

-- +goose StatementBegin
drop table if exists resource_profiles_domain_name;
-- +goose StatementEnd
