# Inventory Mutations and Audit Evidence Commit Together

Status: accepted

Every accepted Inventory Mutation persists its field-level before/after audit
evidence in the same transaction as the Configuration Item, Typed Profile, or
CI Relationship change. If the audit write fails, the mutation rolls back.
This intentionally differs from authentication audit, which remains fail-open:
Inventory history is part of the governed CMDB record itself, while an
authentication audit outage must not change a valid access decision.

The diff covers identity, ownership, Environment, lifecycle, Manual Health
Overrides, CI Labels, External CI Identifiers, Typed Profiles, and CI
Relationships. Credentials, passwords, tokens, DSNs, and secrets are rejected
at the Inventory write boundary and never enter this audit record; query text,
query results, and Machine Credentials remain outside Inventory audit.

Inventory Batch Mutations first produce a preview and then commit all selected
CI changes atomically; any failure rolls back the batch. Each affected CI keeps
its own audit diff, and bulk Label edits use explicit add, update, and remove
operations rather than replacing an unreviewed Label object.
