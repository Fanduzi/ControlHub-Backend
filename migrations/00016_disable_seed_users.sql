-- Phase 38X-1B: forward-only seed credential remediation (Issue #13).
--
-- Migration 0002 published two working accounts (admin@example.com and
-- editor@example.com) sharing the password secret123. 0002 has shipped and
-- must not be rewritten, so this migration disables both users and increments
-- their authorization_version: any Backend Bearer Credential minted before
-- this migration applied dies immediately, and the published passwords stop
-- signing in. The rows are preserved so audit_events attribution stays intact.

-- +goose Up
-- +goose StatementBegin
UPDATE users
SET is_active = 0,
    authorization_version = authorization_version + 1
WHERE email IN ('admin@example.com', 'editor@example.com');
-- +goose StatementEnd

-- +goose Down
-- Intentionally empty: a rollback must never re-enable the published seed
-- credentials. Re-enabling is an explicit operator decision via the
-- bootstrap-admin command.
