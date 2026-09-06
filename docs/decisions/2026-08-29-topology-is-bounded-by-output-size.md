# Topology Traversal Is Bounded by Output Size

Status: accepted

Topology Traversal defaults to depth two but has no product-level maximum depth
of three. Hop count is capped at 32 as a request safety bound. The backend also
protects traversal with node and edge limits and returns an explicit truncated
result when those limits stop expansion. A fixed shallow depth was rejected
because it can hide a valid Database Estate path even when the returned graph
would remain small.

The Topology Workspace requires an operator-selected root CI for traversal.
Before a root is selected it presents relevant starting CIs from the current
Environment; it does not imply that an arbitrary or truncated graph is a
complete Environment-wide map.
