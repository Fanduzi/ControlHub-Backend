-- Add index for lifecycle_status filtering on resources table.
-- Supports efficient pagination queries filtering by lifecycleStatus.

-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_resources_lifecycle ON resources(lifecycle_status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_resources_lifecycle ON resources;
-- +goose StatementEnd
