-- input: query execution evidence and application-owned user IDs at schema version 27
-- output: one-row-per-owner query workspaces and nullable private execution statements at schema version 28
-- pos: storage foundation for persistent query worksheets and owner-only successful-statement reuse
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

CREATE TABLE query_workspaces (
  owner_user_id BIGINT UNSIGNED NOT NULL,
  worksheets JSON NOT NULL,
  version BIGINT UNSIGNED NOT NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (owner_user_id)
);

ALTER TABLE query_executions
  ADD COLUMN full_statement TEXT NULL COMMENT 'Private full SQL for owner-only successful execution reuse' AFTER statement_preview;

-- +goose Down

ALTER TABLE query_executions
  DROP COLUMN full_statement;

DROP TABLE IF EXISTS query_workspaces;
