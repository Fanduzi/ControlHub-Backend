-- +goose Up

ALTER TABLE resources
    ADD COLUMN archived_at DATETIME(6) NULL,
    ADD COLUMN archived_by VARCHAR(128) NULL,
    ADD COLUMN archive_reason VARCHAR(512) NULL;

CREATE INDEX idx_resources_archived_at ON resources (archived_at);

-- +goose Down

DROP INDEX idx_resources_archived_at ON resources;

ALTER TABLE resources
    DROP COLUMN archive_reason,
    DROP COLUMN archived_by,
    DROP COLUMN archived_at;
