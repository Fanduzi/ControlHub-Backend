-- input: claim and keyed-execution identity columns still on the case-insensitive utf8mb4_0900_ai_ci table default at version 30
-- output: claim key/scope/digest and executions.client_execution_id re-collated to utf8mb4_0900_bin (case-sensitive NO PAD) with a guarded rollback
-- pos: byte-exact occupancy identity for the G9 execution-claim protocol (issue #118, T10-A)
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

-- The claim idempotency key is an opaque client identity: it must compare
-- byte-exact. The inherited utf8mb4_0900_ai_ci collation folds case, so 'KeyA'
-- and 'keya' would collide on the primary key and silently admit or conflict
-- the wrong occupancy. utf8mb4_0900_bin is binary, case-sensitive, and NO
-- PAD. Columns keep their types, nullability, defaults, and contents; only
-- comparison semantics change.
ALTER TABLE query_execution_claims
  MODIFY client_execution_id        VARCHAR(64)  NOT NULL COLLATE utf8mb4_0900_bin,
  MODIFY database_name              VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_0900_bin,
  MODIFY schema_name                VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_0900_bin,
  MODIFY request_digest             CHAR(64)     NOT NULL COLLATE utf8mb4_0900_bin
    COMMENT 'Canonical request fingerprint; same key + different digest = conflict';

-- The keyed unique index on executions must likewise never merge keys that
-- differ only by case.
ALTER TABLE query_executions
  MODIFY client_execution_id VARCHAR(64) NULL COLLATE utf8mb4_0900_bin
    COMMENT 'Client-supplied idempotency key; NULL = row carries no claim identity';

-- +goose Down

-- Reverting to utf8mb4_0900_ai_ci widens every stored identity's match class:
-- under ai_ci a claim keyed 'lower-key' also answers 'LOWER-KEY', and accent
-- and other equivalence folds apply on top of case. No "safe characters"
-- list can prove a stored row keeps its identity, so the only provable-safe
-- downgrade is an untouched claim space: refuse while ANY claim row exists
-- or ANY execution row carries a client_execution_id, before any DDL runs.
-- The guard deletes, merges, or rewrites nothing.
DROP PROCEDURE IF EXISTS guard_claim_collation_down_31;
-- +goose StatementBegin
CREATE PROCEDURE guard_claim_collation_down_31()
BEGIN
  IF EXISTS (SELECT 1 FROM query_execution_claims LIMIT 1)
     OR EXISTS (SELECT 1 FROM query_executions WHERE client_execution_id IS NOT NULL LIMIT 1)
  THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00031 while claim or keyed-execution data exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_claim_collation_down_31();
DROP PROCEDURE guard_claim_collation_down_31;

ALTER TABLE query_executions
  MODIFY client_execution_id VARCHAR(64) NULL COLLATE utf8mb4_0900_ai_ci
    COMMENT 'Client-supplied idempotency key; NULL = row carries no claim identity';

ALTER TABLE query_execution_claims
  MODIFY client_execution_id        VARCHAR(64)  NOT NULL COLLATE utf8mb4_0900_ai_ci,
  MODIFY database_name              VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_0900_ai_ci,
  MODIFY schema_name                VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_0900_ai_ci,
  MODIFY request_digest             CHAR(64)     NOT NULL COLLATE utf8mb4_0900_ai_ci
    COMMENT 'Canonical request fingerprint; same key + different digest = conflict';
