-- +goose Up
-- +goose StatementBegin
alter table audit_events
  add column changes json null comment 'Server-owned field changes for governed inventory mutations; NULL for non-inventory events' after result;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table audit_events drop column changes;
-- +goose StatementEnd
