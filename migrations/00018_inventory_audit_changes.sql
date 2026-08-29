-- input: existing audit_events table at schema version 17
-- output: nullable JSON field-change evidence for governed inventory events
-- pos: forward-only schema support for atomic CMDB mutation audit reads
-- note: if this file changes, update header and internal/repository/mysql/README.md

-- +goose Up
-- +goose StatementBegin
alter table audit_events
  add column changes json null comment 'Server-owned field changes for governed inventory mutations; NULL for non-inventory events' after result;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table audit_events drop column changes;
-- +goose StatementEnd
