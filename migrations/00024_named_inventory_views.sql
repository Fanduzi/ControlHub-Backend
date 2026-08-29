-- input: application-owned user IDs and reusable Inventory search presentation state at schema version 21
-- output: named_inventory_views table at schema version 24 with no foreign keys
-- pos: forward-only persistence for personal and shared named Inventory views
-- note: if this file changes, update this header and internal/repository/mysql/README.md.

-- +goose Up
-- Named inventory views persist reusable search presentation state only.
-- User identity integrity is enforced by the application; no foreign keys.

CREATE TABLE named_inventory_views (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(120) NOT NULL,
  scope VARCHAR(16) NOT NULL,
  state JSON NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_named_inventory_views_owner (owner_user_id, updated_at),
  KEY idx_named_inventory_views_scope (scope, updated_at),
  CONSTRAINT chk_named_inventory_view_scope CHECK (scope IN ('personal', 'shared'))
);

-- +goose Down

DROP TABLE IF EXISTS named_inventory_views;
