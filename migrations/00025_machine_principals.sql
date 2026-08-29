-- input: application-owned administrator IDs and validated machine-principal credential metadata
-- output: machine_principals and machine_principal_credentials tables at schema version 25 with no foreign keys
-- pos: forward-only persistence for independent machine identities and hashed scoped credentials
-- note: if this file changes, update this header and internal/repository/mysql/README.md.

-- +goose Up

CREATE TABLE machine_principals (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Stable internal machine-principal identifier',
  name VARCHAR(120) NOT NULL COMMENT 'Administrator-assigned machine-principal display name',
  created_by_user_id BIGINT UNSIGNED NOT NULL COMMENT 'Administrator user ID that created the machine principal; application-owned integrity',
  created_at DATETIME(6) NOT NULL COMMENT 'UTC machine-principal creation time',
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Non-user identities for scoped automation access';

CREATE TABLE machine_principal_credentials (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Stable internal credential identifier',
  machine_principal_id BIGINT UNSIGNED NOT NULL COMMENT 'Owning machine-principal ID; application-owned integrity',
  lookup_id CHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'Public random credential lookup identifier; not secret material',
  secret_hash BINARY(32) NOT NULL COMMENT 'SHA-256 digest of a high-entropy random credential; plaintext is never stored',
  scopes JSON NOT NULL COMMENT 'Validated closed array of granted machine scopes',
  expires_at DATETIME(6) NOT NULL COMMENT 'UTC credential expiry time, no more than 90 days after creation',
  last_used_at DATETIME(6) NULL COMMENT 'UTC time of the latest successful authenticated use; NULL before first use',
  revoked_at DATETIME(6) NULL COMMENT 'UTC independent revocation time; NULL while not revoked',
  created_by_user_id BIGINT UNSIGNED NOT NULL COMMENT 'Administrator user ID that created or rotated this credential; application-owned integrity',
  rotated_from_credential_id BIGINT UNSIGNED NULL COMMENT 'Prior credential ID for rotation lineage; NULL for initial issue and intentionally no foreign key',
  created_at DATETIME(6) NOT NULL COMMENT 'UTC credential creation time',
  PRIMARY KEY (id),
  UNIQUE KEY uq_machine_principal_credentials_lookup_id (lookup_id),
  KEY idx_machine_principal_credentials_principal (machine_principal_id, created_at),
  CONSTRAINT chk_machine_principal_credentials_lifetime CHECK (expires_at > created_at AND expires_at <= DATE_ADD(created_at, INTERVAL 90 DAY)),
  CONSTRAINT chk_machine_principal_credentials_scopes CHECK (JSON_TYPE(scopes) = 'ARRAY' AND JSON_LENGTH(scopes) BETWEEN 1 AND 5),
  CONSTRAINT chk_machine_principal_credentials_last_used CHECK (last_used_at IS NULL OR last_used_at >= created_at),
  CONSTRAINT chk_machine_principal_credentials_revoked CHECK (revoked_at IS NULL OR revoked_at >= created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Individually expiring and revocable hash-only credentials for machine principals';

-- +goose Down

DROP TABLE IF EXISTS machine_principal_credentials;
DROP TABLE IF EXISTS machine_principals;
