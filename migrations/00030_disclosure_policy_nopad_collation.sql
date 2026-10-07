-- input: query_result_disclosure_policies name columns still collated utf8mb4_bin (PAD SPACE) at version 29
-- output: four canonical name segments re-collated to utf8mb4_0900_bin (NO PAD) with a guarded rollback
-- pos: Makes the five-part disclosure key byte-exact under MySQL comparison and uniqueness semantics
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

-- utf8mb4_bin is PAD SPACE: 'orders' and 'orders ' compare equal under both
-- the five-part unique key and ordinary equality, so two canonical names that
-- differ only in trailing spaces would silently alias the same policy scope.
-- utf8mb4_0900_bin is binary, case-sensitive, and NO PAD, preserving the
-- exact canonical bytes required for policy identity. The five-part unique
-- key, column lengths, nullability, defaults, and existing values are kept
-- verbatim; only the comparison semantics change.
ALTER TABLE query_result_disclosure_policies
  MODIFY database_name VARCHAR(128) NOT NULL COLLATE utf8mb4_0900_bin,
  MODIFY schema_name VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_0900_bin
    COMMENT 'Schema segment of the canonical policy key; empty for non-schema engines',
  MODIFY object_name VARCHAR(128) NOT NULL COLLATE utf8mb4_0900_bin,
  MODIFY column_name VARCHAR(128) NOT NULL COLLATE utf8mb4_0900_bin;

-- +goose Down

-- Refuse to revert to PAD SPACE while any rows exist whose five-part keys are
-- distinct under NO PAD semantics but equal under PAD SPACE: the old unique
-- key would collide on them and the downgrade would have to merge or drop
-- canonical identities. Export or reconcile such siblings explicitly first.
DROP PROCEDURE IF EXISTS guard_disclosure_nopad_down_30;
-- +goose StatementBegin
CREATE PROCEDURE guard_disclosure_nopad_down_30()
BEGIN
  IF EXISTS (
    SELECT 1
      FROM (
        SELECT target_resource_id,
               TRIM(TRAILING ' ' FROM database_name) AS database_name,
               TRIM(TRAILING ' ' FROM schema_name)   AS schema_name,
               TRIM(TRAILING ' ' FROM object_name)   AS object_name,
               TRIM(TRAILING ' ' FROM column_name)   AS column_name
          FROM query_result_disclosure_policies
      ) pad_key
     GROUP BY target_resource_id, database_name, schema_name, object_name, column_name
    HAVING COUNT(*) > 1
     LIMIT 1
  ) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00030 while disclosure policies distinguish names only by trailing spaces';
  END IF;
END;
-- +goose StatementEnd

CALL guard_disclosure_nopad_down_30();
DROP PROCEDURE guard_disclosure_nopad_down_30;

ALTER TABLE query_result_disclosure_policies
  MODIFY database_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin,
  MODIFY schema_name VARCHAR(128) NOT NULL DEFAULT '' COLLATE utf8mb4_bin
    COMMENT 'Schema segment of the canonical policy key; empty for non-schema engines',
  MODIFY object_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin,
  MODIFY column_name VARCHAR(128) NOT NULL COLLATE utf8mb4_bin;
