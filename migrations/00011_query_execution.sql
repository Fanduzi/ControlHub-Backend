-- +goose Up
-- Phase 37 read-only query sandbox: credential metadata + execution history.
--
-- Follows the repo convention: BIGINT UNSIGNED keys and NO foreign-key
-- constraints (referential integrity is enforced in application code, matching
-- every existing table; see TestSchemaUsesBigintPrimaryKeysWithoutForeignKeys).
-- The credential_ref column stores an opaque, validated key ([A-Z0-9_]+) that
-- the server resolves to a DSN from the environment; the DSN/password is never
-- stored here.

CREATE TABLE query_target_credentials (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  resource_id BIGINT UNSIGNED NOT NULL,
  engine VARCHAR(32) NOT NULL,
  credential_ref VARCHAR(128) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  environment_policy VARCHAR(32) NOT NULL DEFAULT 'non_prod_only',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_query_target_credentials_resource (resource_id)
);

CREATE TABLE query_executions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  target_resource_id BIGINT UNSIGNED NOT NULL,
  actor_user_id BIGINT UNSIGNED NOT NULL,
  engine VARCHAR(32) NOT NULL,
  statement_digest VARCHAR(512) NOT NULL,
  statement_preview VARCHAR(512) NOT NULL,
  status VARCHAR(32) NOT NULL,
  row_count INT NOT NULL DEFAULT 0,
  duration_ms INT NOT NULL DEFAULT 0,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  error_message VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_query_executions_target_created (target_resource_id, created_at),
  KEY idx_query_executions_actor_created (actor_user_id, created_at)
);

-- +goose Down

DROP TABLE IF EXISTS query_executions;
DROP TABLE IF EXISTS query_target_credentials;
