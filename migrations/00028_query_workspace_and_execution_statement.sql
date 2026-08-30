-- input: query execution evidence and application-owned user IDs at schema version 27
-- output: one-row-per-owner query workspaces, nullable private execution statements, and fail-loud data-preserving rollback at schema version 28
-- pos: storage foundation and guarded downgrade for persistent query worksheets and owner-only successful-statement reuse
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
-- Refuse to erase query drafts or reusable execution SQL. Export or purge both
-- data sets explicitly before retrying the rollback.
DROP PROCEDURE IF EXISTS guard_query_workspace_statement_down_28;
-- +goose StatementBegin
CREATE PROCEDURE guard_query_workspace_statement_down_28()
BEGIN
  IF EXISTS (SELECT 1 FROM query_workspaces LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_executions WHERE full_statement IS NOT NULL LIMIT 1) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00028 while query workspace or full statement data exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_query_workspace_statement_down_28();
DROP PROCEDURE guard_query_workspace_statement_down_28;

ALTER TABLE query_executions
  DROP COLUMN full_statement;

DROP TABLE IF EXISTS query_workspaces;
