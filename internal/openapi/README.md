# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source OpenAPI contract, including Operator Access Boundary security and schema-policy responses |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema, public exceptions, authenticated reads, and admin authorization responses |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Dependencies
- Upstream: `github.com/getkin/kin-openapi` for validation tests
- Downstream: `internal/api` serves the embedded contract

## Update Rule
If API route security or the OpenAPI contract changes, update this file in the same change.
