-- input: claim rows carry no record of the attempt budget the claimant resolved at admission at version 31
-- output: nullable attempt_timeout_ms on query_execution_claims with a guarded rollback at version 32
-- pos: per-attempt budget evidence for the running/unknown derivation threshold (issue #118, T10-C)
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up

-- The claim occupancy row records the statement-timeout budget the claimant
-- resolved when it consumed the key. The read path derives 'unknown' only
-- from claimed_at + attempt_timeout_ms + grace: the threshold must reflect
-- THIS attempt's resolved budget, not a mutable environment value read later.
-- Nullable, never backfilled: rows claimed before this column existed keep
-- NULL and derive 'unknown' — the missing budget is an evidence gap, and
-- inventing one would fabricate an expiry that never existed.
ALTER TABLE query_execution_claims
  ADD COLUMN attempt_timeout_ms BIGINT UNSIGNED NULL
    COMMENT 'Statement-timeout budget (ms) resolved at claim time; NULL = pre-T10-C claim, budget evidence missing'
    AFTER request_digest;

-- +goose Down

-- Dropping the column destroys per-attempt budget evidence. Refuse while any
-- claim records one — a stored budget cannot be reconstructed after the drop.
DROP PROCEDURE IF EXISTS guard_claim_attempt_timeout_down_32;
-- +goose StatementBegin
CREATE PROCEDURE guard_claim_attempt_timeout_down_32()
BEGIN
  IF EXISTS (SELECT 1 FROM query_execution_claims
             WHERE attempt_timeout_ms IS NOT NULL LIMIT 1)
  THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00032 while claim attempt-timeout evidence exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_claim_attempt_timeout_down_32();
DROP PROCEDURE guard_claim_attempt_timeout_down_32;

ALTER TABLE query_execution_claims
  DROP COLUMN attempt_timeout_ms;
