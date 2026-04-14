-- Phase 10: change resources.name from global unique to (name, environment_id) composite unique.
-- This allows the same name in different environments (e.g. "order-mysql" in prod and staging).
-- The existing global `name` unique constraint is auto-named `name` by MySQL.
-- Seed data (0002, 0004) already has distinct names globally, so no data conflicts.

ALTER TABLE resources
  DROP INDEX name,
  ADD UNIQUE KEY uq_resource_name_env (name, environment_id);
