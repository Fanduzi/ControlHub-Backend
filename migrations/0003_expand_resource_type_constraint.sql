-- Expand resource_type CHECK constraint from 4 to 8 types.

-- +goose Up
-- +goose StatementBegin
alter table resources
  drop check chk_resource_type,
  add constraint chk_resource_type check (
    resource_type in (
      'host',
      'database_instance',
      'database_cluster',
      'service',
      'domain_name',
      'virtual_ip',
      'database_proxy',
      'control_plane_component'
    )
  );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table resources
  drop check chk_resource_type,
  add constraint chk_resource_type check (
    resource_type in ('host', 'database_instance', 'database_cluster', 'service')
  );
-- +goose StatementEnd
