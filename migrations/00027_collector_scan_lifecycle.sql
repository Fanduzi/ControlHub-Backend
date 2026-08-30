-- input: verified machine-principal IDs, collector scan receipts, and governed resource IDs at schema version 26
-- output: idempotent collector scan ledger, capped per-principal/per-CI missing state, and fail-loud data-preserving rollback
-- pos: durable persistence contract for collector complete-scan lifecycle transitions
-- note: if this file changes, update this header and migrations/README.md.

-- +goose Up
-- Principal/resource identity integrity remains application-owned, matching the
-- existing machine-principal and inventory persistence conventions.

CREATE TABLE collector_scan_ledger (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Stable internal completed-scan ledger identifier',
  machine_principal_id BIGINT UNSIGNED NOT NULL COMMENT 'Verified collector Machine Principal ID',
  collector_scan_id VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Collector-owned idempotency identifier',
  payload_hash BINARY(32) NOT NULL COMMENT 'SHA-256 digest used to reject a conflicting retry',
  result VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Terminal collector scan result',
  completed_at DATETIME(6) NOT NULL COMMENT 'UTC time the scan reached its terminal result',
  PRIMARY KEY (id),
  UNIQUE KEY uq_collector_scan_ledger_principal_scan (machine_principal_id, collector_scan_id),
  KEY idx_collector_scan_ledger_principal_completed (machine_principal_id, completed_at),
  CONSTRAINT chk_collector_scan_ledger_scan_id CHECK (collector_scan_id <> ''),
  CONSTRAINT chk_collector_scan_ledger_result CHECK (result IN ('complete', 'incomplete', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Completed collector scan idempotency and conflict evidence';

CREATE TABLE collector_ci_scan_states (
  machine_principal_id BIGINT UNSIGNED NOT NULL COMMENT 'Verified collector Machine Principal ID',
  resource_id BIGINT UNSIGNED NOT NULL COMMENT 'Previously seen governed CI resource ID',
  consecutive_complete_scan_omissions TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Capped consecutive successful complete-scan omissions',
  last_seen_collector_scan_id VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Latest collector scan ID in which this collector saw the CI',
  last_completed_collector_scan_id VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT 'Latest successful complete-scan ID applied to this state',
  missing_since DATETIME(6) NULL COMMENT 'Third consecutive complete omission time; NULL while present',
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (machine_principal_id, resource_id),
  KEY idx_collector_ci_scan_states_resource (resource_id, machine_principal_id),
  KEY idx_collector_ci_scan_states_missing (machine_principal_id, missing_since),
  CONSTRAINT chk_collector_ci_scan_states_last_seen CHECK (last_seen_collector_scan_id <> ''),
  CONSTRAINT chk_collector_ci_scan_states_omissions CHECK (consecutive_complete_scan_omissions <= 3),
  CONSTRAINT chk_collector_ci_scan_states_missing CHECK ((consecutive_complete_scan_omissions = 3) = (missing_since IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-collector durable CI presence and complete-scan omission state';

-- +goose Down
-- Refuse to erase collector idempotency or Missing evidence. Export or purge
-- both data sets explicitly before retrying the rollback.
DROP PROCEDURE IF EXISTS guard_collector_scan_lifecycle_down_87;
-- +goose StatementBegin
CREATE PROCEDURE guard_collector_scan_lifecycle_down_87()
BEGIN
  IF EXISTS (SELECT 1 FROM collector_scan_ledger LIMIT 1)
     OR EXISTS (SELECT 1 FROM collector_ci_scan_states LIMIT 1) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'cannot roll back migration 00027 while collector scan lifecycle data exists';
  END IF;
END;
-- +goose StatementEnd

CALL guard_collector_scan_lifecycle_down_87();
DROP PROCEDURE guard_collector_scan_lifecycle_down_87;

DROP TABLE IF EXISTS collector_ci_scan_states;
DROP TABLE IF EXISTS collector_scan_ledger;
