-- Phase 12.3 follow-up patch:
-- apply demo-name and topology seed corrections to databases that already ran the old 0004.
-- 0004 was edited in-place for clean rebuilds, but existing local dev databases need an explicit patch.

-- +goose Up

UPDATE resources
SET display_name = 'ClickHouse Host 01 (Production)'
WHERE id = '41000000-0000-0000-0000-000000000006';

UPDATE resources
SET display_name = 'ClickHouse Host 02 (Production)'
WHERE id = '41000000-0000-0000-0000-000000000007';

UPDATE resources
SET display_name = 'Payment MySQL Replica 01'
WHERE id = '41000000-0000-0000-0000-000000000023';

UPDATE resources
SET display_name = 'Analytics ClickHouse Node 02'
WHERE id = '41000000-0000-0000-0000-000000000027';

UPDATE resources
SET display_name = 'Notification Delivery Service'
WHERE id = '41000000-0000-0000-0000-000000000034';

UPDATE resources
SET display_name = 'Order Service API Endpoint'
WHERE id = '41000000-0000-0000-0000-000000000060';

UPDATE resources
SET display_name = 'Payment Service API Endpoint'
WHERE id = '41000000-0000-0000-0000-000000000061';

INSERT INTO resources (
  id, resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
)
SELECT
  '41000000-0000-0000-0000-000000000044', 'database_proxy', 'proxysql',
  'payment-proxysql-02-prod', 'Payment ProxySQL Standby',
  '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
  'stopped', 'unknown',
  '{"team":"platform","tier":"proxy","backend":"mysql","pci_scope":"yes","role":"standby"}', 'manual', ''
WHERE NOT EXISTS (
  SELECT 1 FROM resources WHERE id = '41000000-0000-0000-0000-000000000044'
);

INSERT INTO resource_relations (id, from_resource_id, to_resource_id, relation_type)
SELECT
  '51000000-0000-0000-0000-000000000090',
  '41000000-0000-0000-0000-000000000044',
  '41000000-0000-0000-0000-000000000010',
  'fronts'
WHERE NOT EXISTS (
  SELECT 1 FROM resource_relations WHERE id = '51000000-0000-0000-0000-000000000090'
);

INSERT INTO resource_relations (id, from_resource_id, to_resource_id, relation_type)
SELECT
  '51000000-0000-0000-0000-000000000091',
  '41000000-0000-0000-0000-000000000022',
  '41000000-0000-0000-0000-000000000023',
  'replicates_to'
WHERE NOT EXISTS (
  SELECT 1 FROM resource_relations WHERE id = '51000000-0000-0000-0000-000000000091'
);

-- +goose Down

-- Irreversible patch migration:
-- existing databases may already have these values or rows from a clean rebuild after 0004 was updated.
SELECT 1;
