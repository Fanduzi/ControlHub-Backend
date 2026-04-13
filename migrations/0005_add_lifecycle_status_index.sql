-- Add index for lifecycle_status filtering on resources table.
-- Supports efficient pagination queries filtering by lifecycleStatus.

CREATE INDEX idx_resources_lifecycle ON resources(lifecycle_status);
