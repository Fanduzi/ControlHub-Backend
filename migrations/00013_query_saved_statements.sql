-- +goose Up
-- Phase 38R governed saved statements: target-scoped query library with
-- personal and shared_template scopes.
--
-- Follows the repo convention: BIGINT UNSIGNED keys and NO foreign-key
-- constraints (referential integrity is enforced in application code).

CREATE TABLE query_saved_statements (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  target_resource_id BIGINT UNSIGNED NOT NULL,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(120) NOT NULL,
  statement TEXT NOT NULL,
  scope VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_saved_statement_target_scope (target_resource_id, scope, updated_at),
  KEY idx_saved_statement_target_owner (target_resource_id, owner_user_id, updated_at),
  CONSTRAINT chk_saved_statement_scope CHECK (scope IN ('personal', 'shared_template'))
);

-- +goose Down

DROP TABLE IF EXISTS query_saved_statements;
