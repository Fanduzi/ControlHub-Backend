-- Phase 12.3 follow-up patch:
-- keep the historical migration slot while the clean bigint baseline now ships the corrected demo seed data in 0004.

-- +goose Up
SELECT 1;

-- +goose Down
SELECT 1;
