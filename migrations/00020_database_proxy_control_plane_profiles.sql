-- input: resources table and existing typed profile tables at schema version 18
-- output: resource_profiles_database_proxy, resource_profiles_control_plane_component, ha -> ha_monitor remap
-- pos: typed profiles for Database Proxy and Control Plane Component; fail loud on ambiguous ha coexistence
-- note: if this file changes, update header and internal/repository/mysql/README.md

-- +goose Up
-- +goose StatementBegin
create table resource_profiles_database_proxy (
  id bigint unsigned not null auto_increment primary key comment 'Surrogate profile row id',
  resource_id bigint unsigned not null comment 'Owning Database Proxy resource id',
  technology_subtype varchar(64) not null comment 'Proxy technology: proxysql, chproxy, haproxy, maxscale',
  host varchar(255) not null comment 'Proxy listen host',
  port int not null comment 'Proxy listen port 1-65535',
  role varchar(64) not null comment 'active or standby',
  version varchar(64) not null comment 'Optional proxy version; empty when unknown',
  spec json not null comment 'Reserved typed-profile extension payload',
  unique key uq_resource_profiles_database_proxy_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci comment='Typed profile for database_proxy resources';
-- +goose StatementEnd

-- +goose StatementBegin
create table resource_profiles_control_plane_component (
  id bigint unsigned not null auto_increment primary key comment 'Surrogate profile row id',
  resource_id bigint unsigned not null comment 'Owning Control Plane Component resource id',
  component_subtype varchar(64) not null comment 'orchestrator, ha_monitor, backup_manager',
  endpoint varchar(255) not null comment 'Control-plane endpoint URL or address',
  version varchar(64) not null comment 'Optional component version; empty when unknown',
  role varchar(64) not null comment 'active or standby',
  spec json not null comment 'Reserved typed-profile extension payload',
  unique key uq_resource_profiles_control_plane_component_resource_id (resource_id)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci comment='Typed profile for control_plane_component resources';
-- +goose StatementEnd

-- +goose StatementBegin
DROP PROCEDURE IF EXISTS migrate_control_plane_ha_subtype;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE PROCEDURE migrate_control_plane_ha_subtype()
BEGIN
  DECLARE ha_n INT;
  DECLARE ham_n INT;
  SELECT COUNT(*) INTO ha_n FROM resources WHERE resource_type = 'control_plane_component' AND resource_subtype = 'ha';
  SELECT COUNT(*) INTO ham_n FROM resources WHERE resource_type = 'control_plane_component' AND resource_subtype = 'ha_monitor';
  IF ha_n > 0 AND ham_n > 0 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ambiguous control_plane_component subtype ha coexists with ha_monitor';
  END IF;
  UPDATE resources
  SET resource_subtype = 'ha_monitor'
  WHERE resource_type = 'control_plane_component' AND resource_subtype = 'ha';
END;
-- +goose StatementEnd

-- +goose StatementBegin
CALL migrate_control_plane_ha_subtype();
-- +goose StatementEnd

-- +goose StatementBegin
DROP PROCEDURE IF EXISTS migrate_control_plane_ha_subtype;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE resources
SET resource_subtype = 'ha'
WHERE resource_type = 'control_plane_component' AND resource_subtype = 'ha_monitor' AND name = 'ha-manager-prod';
-- +goose StatementEnd

-- +goose StatementBegin
drop table if exists resource_profiles_control_plane_component;
-- +goose StatementEnd

-- +goose StatementBegin
drop table if exists resource_profiles_database_proxy;
-- +goose StatementEnd
