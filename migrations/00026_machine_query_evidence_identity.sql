-- input: user-attributed query_executions and nullable-user audit_events at schema version 25
-- output: XOR query evidence identity and at-most-one user/machine audit identity, indexed without foreign keys
-- pos: truthful Machine Principal attribution for governed-select execution evidence
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

ALTER TABLE query_executions
  MODIFY actor_user_id BIGINT UNSIGNED NULL,
  ADD COLUMN actor_machine_principal_id BIGINT UNSIGNED NULL COMMENT 'Verified Machine Principal ID; NULL for User-attributed evidence' AFTER actor_user_id,
  ADD KEY idx_query_executions_machine_actor_created (actor_machine_principal_id, created_at),
  ADD CONSTRAINT chk_query_executions_exactly_one_actor CHECK (
    (actor_user_id IS NULL) <> (actor_machine_principal_id IS NULL)
  );

ALTER TABLE audit_events
  ADD COLUMN actor_machine_principal_id BIGINT UNSIGNED NULL COMMENT 'Verified Machine Principal ID; NULL for User or unauthenticated evidence' AFTER actor_user_id,
  ADD KEY idx_audit_events_machine_actor_created (actor_machine_principal_id, created_at),
  ADD CONSTRAINT chk_audit_events_at_most_one_actor CHECK (
    actor_user_id IS NULL OR actor_machine_principal_id IS NULL
  );

-- +goose Down
-- Refuse to erase truthful machine attribution. Remove or export machine
-- evidence explicitly before retrying the rollback.
DROP PROCEDURE IF EXISTS guard_machine_query_evidence_down_86;
-- +goose StatementBegin
CREATE PROCEDURE guard_machine_query_evidence_down_86()
BEGIN
  IF EXISTS (SELECT 1 FROM query_executions WHERE actor_machine_principal_id IS NOT NULL LIMIT 1)
     OR EXISTS (SELECT 1 FROM audit_events WHERE actor_machine_principal_id IS NOT NULL LIMIT 1) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00026 while machine-attributed evidence exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_machine_query_evidence_down_86();
DROP PROCEDURE guard_machine_query_evidence_down_86;

ALTER TABLE audit_events
  DROP CHECK chk_audit_events_at_most_one_actor,
  DROP INDEX idx_audit_events_machine_actor_created,
  DROP COLUMN actor_machine_principal_id;

ALTER TABLE query_executions
  DROP CHECK chk_query_executions_exactly_one_actor,
  DROP INDEX idx_query_executions_machine_actor_created,
  DROP COLUMN actor_machine_principal_id,
  MODIFY actor_user_id BIGINT UNSIGNED NOT NULL;
