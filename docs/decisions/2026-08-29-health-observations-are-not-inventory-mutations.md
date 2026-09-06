# Health Observations Are Not Inventory Mutations

Status: accepted

Collector health reports are operational Health Observations, not Inventory
Mutations, and therefore do not generate field-level CMDB mutation audit on
every probe. ControlHub retains the latest observation per observer and derives
effective health from the worst fresh result. A Manual Health Override remains
an audited Inventory Mutation. This avoids turning CMDB audit into probe
telemetry while preserving operator-governed changes as durable history.
