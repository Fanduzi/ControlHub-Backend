-- input: single-connection query targets and name-keyed disclosure/saved-statement/execution evidence at schema version 28
-- output: composite (resource_id, database_name) connection identity, pinned-schema and PostgreSQL evidence columns, guarded downgrade at schema version 29
-- pos: storage foundation for per-database Query Connections and execution claims (spec G1)
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

-- A credential row becomes one Query Connection: (resource_id, database_name)
-- names it. Empty database_name keeps the legacy single-connection identity,
-- so existing MySQL/TiDB rows remain equivalent under the widened key.
ALTER TABLE query_target_credentials
  ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Fixed database of this connection; empty = engine-default single connection' AFTER resource_id,
  ADD COLUMN default_schema VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Pinned default schema; PostgreSQL connections require it' AFTER database_name,
  DROP INDEX uq_query_target_credentials_resource,
  ADD UNIQUE KEY uq_query_target_credentials_resource (resource_id, database_name);

-- Execution evidence gains the resolved connection context, the observed
-- backend handle, and the optional client idempotency key. The unique index
-- tolerates many NULL keys (MySQL semantics), so legacy keyless rows are
-- unaffected.
ALTER TABLE query_executions
  ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Connection database the execution ran against; empty = engine default' AFTER engine,
  ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Pinned schema the execution ran under; empty for non-schema engines' AFTER database_name,
  ADD COLUMN backend_pid INT NULL COMMENT 'Observed backend process ID for cancel/verify evidence' AFTER error_message,
  ADD COLUMN remote_state VARCHAR(16) NOT NULL DEFAULT '' COMMENT 'Backend-side state probe: empty|stopped|unknown|completed' AFTER backend_pid,
  ADD COLUMN client_execution_id VARCHAR(64) NULL COMMENT 'Client-supplied idempotency key; NULL = row carries no claim identity' AFTER remote_state,
  ADD UNIQUE KEY uq_query_executions_client_execution (target_resource_id, client_execution_id);

-- Occupancy records for the idempotent execution-claim protocol: exactly one
-- claimant per (target, client_execution_id), finalized atomically with the
-- terminal execution row. Application-owned actor identity: exactly one of
-- actor_user_id / actor_machine_principal_id is non-NULL (no DB CHECK, matching
-- the FK-free convention; the writer enforces the XOR).
CREATE TABLE query_execution_claims (
  target_resource_id         BIGINT UNSIGNED NOT NULL,
  client_execution_id        VARCHAR(64)     NOT NULL,
  actor_user_id              BIGINT UNSIGNED NULL COMMENT 'User identity; NULL for machine-attributed claims',
  actor_machine_principal_id BIGINT UNSIGNED NULL COMMENT 'Verified Machine Principal ID; NULL for user-attributed claims',
  database_name              VARCHAR(128)    NOT NULL DEFAULT '',
  schema_name                VARCHAR(128)    NOT NULL DEFAULT '',
  request_digest             CHAR(64)        NOT NULL COMMENT 'Canonical request fingerprint; same key + different digest = conflict',
  execution_id               BIGINT UNSIGNED NULL COMMENT 'Terminal execution row linked at finalize; NULL = attempt unfinished',
  claimed_at                 DATETIME(6)     NOT NULL,
  PRIMARY KEY (target_resource_id, client_execution_id),
  KEY ix_claim_actor (actor_user_id, actor_machine_principal_id, claimed_at)
);

ALTER TABLE query_saved_statements
  ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Connection database the statement is bound to; empty = engine default' AFTER target_resource_id,
  ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Pinned schema the statement is bound to; empty for non-schema engines' AFTER database_name;

-- Disclosure scope gains the schema dimension; schema_name = '' keeps legacy
-- MySQL/TiDB policies equivalent under the widened unique key.
ALTER TABLE query_result_disclosure_policies
  ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_bin COMMENT 'Schema segment of the canonical policy key; empty for non-schema engines' AFTER database_name,
  DROP INDEX uq_disclosure_policy_scope,
  ADD UNIQUE KEY uq_disclosure_policy_scope (target_resource_id, database_name, schema_name, object_name, column_name);

-- +goose Down
-- Refuse to erase PostgreSQL connection identity, claims, or pinned-schema
-- evidence. Export or purge the new-dimension data explicitly before retrying
-- the rollback.
DROP PROCEDURE IF EXISTS guard_pg_query_connections_down_29;
-- +goose StatementBegin
CREATE PROCEDURE guard_pg_query_connections_down_29()
BEGIN
  IF EXISTS (SELECT 1 FROM query_execution_claims LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_target_credentials
                 WHERE database_name <> '' OR default_schema <> '' LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_executions
                 WHERE database_name <> '' OR schema_name <> ''
                    OR backend_pid IS NOT NULL OR remote_state <> ''
                    OR client_execution_id IS NOT NULL LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_saved_statements
                 WHERE database_name <> '' OR schema_name <> '' LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_result_disclosure_policies
                 WHERE schema_name <> '' LIMIT 1) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00029 while PostgreSQL connection identity or claim data exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_pg_query_connections_down_29();
DROP PROCEDURE guard_pg_query_connections_down_29;

ALTER TABLE query_result_disclosure_policies
  DROP INDEX uq_disclosure_policy_scope,
  ADD UNIQUE KEY uq_disclosure_policy_scope (target_resource_id, database_name, object_name, column_name),
  DROP COLUMN schema_name;

ALTER TABLE query_saved_statements
  DROP COLUMN database_name,
  DROP COLUMN schema_name;

DROP TABLE IF EXISTS query_execution_claims;

ALTER TABLE query_executions
  DROP INDEX uq_query_executions_client_execution,
  DROP COLUMN database_name,
  DROP COLUMN schema_name,
  DROP COLUMN backend_pid,
  DROP COLUMN remote_state,
  DROP COLUMN client_execution_id;

ALTER TABLE query_target_credentials
  DROP INDEX uq_query_target_credentials_resource,
  ADD UNIQUE KEY uq_query_target_credentials_resource (resource_id),
  DROP COLUMN database_name,
  DROP COLUMN default_schema;
