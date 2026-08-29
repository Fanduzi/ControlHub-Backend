-- input: resources table with health_status used as the legacy manual health override
-- output: nullable manual overrides and one latest health observation per resource and observer
-- pos: durable MySQL storage for Issue 81 health evidence and override clearing
-- note: if this file changes, update this header and internal/repository/mysql/README.md.

-- +goose Up
-- +goose StatementBegin
alter table resources
  modify column health_status varchar(64) null comment 'Optional manual health override; NULL derives effective health from observations';
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_health_observations (
  resource_id bigint unsigned not null comment 'Resource whose health was observed',
  observer varchar(191) not null comment 'Stable identifier of the monitoring source',
  health_status varchar(64) not null comment 'Observed health status: healthy, warning, critical, or unknown',
  observed_at datetime(6) not null comment 'UTC instant supplied by the observer for this evidence',
  primary key (resource_id, observer),
  constraint chk_resource_health_observation_status check (
    health_status in ('healthy', 'warning', 'critical', 'unknown')
  )
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci
  comment='Latest resource health observation from each observer';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table resource_health_observations;
-- +goose StatementEnd

-- +goose StatementBegin
update resources set health_status = 'unknown' where health_status is null;
alter table resources
  modify column health_status varchar(64) not null comment 'Legacy manual health status';
-- +goose StatementEnd
