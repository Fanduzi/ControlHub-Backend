# Test Support Module

Shared metadata and fixtures used only by tests; production packages must not
import this module.

## Files
| File | Responsibility |
|------|---------------|
| operatoraccess/policy.go | Exhaustive protected-operation table with authorization classes, including topology workspace reads, canonical OpenAPI paths, and concrete request paths |

## Exports
- `operatoraccess.All()` — returns a fresh protected-operation slice
- `operatoraccess.Class.RequiredOpenAPIResponses()` — derives mandatory OpenAPI response statuses

## Update Rule
If a protected route or its authorization class changes, update the policy and
all consumers in `internal/api`, `internal/openapi`, and `internal/integration`.
