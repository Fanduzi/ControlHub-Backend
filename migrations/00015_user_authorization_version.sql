-- Phase 38X-1A: durable Authorization Version and active flag for immediate
-- Backend Bearer Credential invalidation on role change, disablement, or
-- password reset. Current server-owned state is authoritative; a role embedded
-- in a signed token is not.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE users
  ADD COLUMN is_active TINYINT(1) NOT NULL DEFAULT 1 AFTER role_id,
  ADD COLUMN authorization_version BIGINT UNSIGNED NOT NULL DEFAULT 1 AFTER is_active;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
  DROP COLUMN authorization_version,
  DROP COLUMN is_active;
-- +goose StatementEnd
