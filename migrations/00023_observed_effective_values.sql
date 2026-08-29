-- input: governed resources and audit actors
-- output: per-source observations and versioned manual overrides
-- pos: minimal persistence for CMDB effective-value projection
-- note: if this file changes, update this header and internal/repository/mysql/README.md.

-- +goose Up
create table resource_observed_values (
  resource_id bigint unsigned not null,
  source varchar(128) not null,
  field_name varchar(64) not null,
  field_value json not null,
  observed_at datetime(6) not null default current_timestamp(6),
  primary key (resource_id, source, field_name)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;

create table resource_manual_overrides (
  resource_id bigint unsigned not null,
  field_name varchar(64) not null,
  field_value json not null,
  version bigint unsigned not null,
  updated_by bigint unsigned not null,
  updated_at datetime(6) not null default current_timestamp(6),
  primary key (resource_id, field_name)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci;

-- +goose Down
drop table resource_manual_overrides;
drop table resource_observed_values;
