-- +goose Up
-- Phase 38X-2B: Allow actor_user_id to be NULL for unauthenticated auth outcomes.
-- Failed login and rejected Bearer have no verified actor; the column must be
-- nullable so these events can be recorded without inventing attribution.
-- +goose StatementBegin
alter table audit_events
  modify actor_user_id bigint unsigned null;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table audit_events
  modify actor_user_id bigint unsigned not null default 0;
-- +goose StatementEnd
