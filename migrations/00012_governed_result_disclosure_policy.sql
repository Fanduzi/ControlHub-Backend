-- +goose Up
-- Phase 38Q governed result-disclosure policy: per-column disclosure mode
-- for query results. Absence of a matching row means the column is blocked
-- (fail-closed).
--
-- Follows the repo convention: BIGINT UNSIGNED keys and NO foreign-key
-- constraints (referential integrity is enforced in application code).

CREATE TABLE query_result_disclosure_policies (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  target_resource_id BIGINT UNSIGNED NOT NULL,
  database_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin,
  object_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin,
  column_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin,
  mode VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_disclosure_policy_scope (target_resource_id, database_name, object_name, column_name),
  KEY idx_disclosure_policy_target (target_resource_id),
  CONSTRAINT chk_disclosure_mode CHECK (mode IN ('raw_copy_allowed', 'masked_no_copy'))
);

-- +goose Down

DROP TABLE IF EXISTS query_result_disclosure_policies;
