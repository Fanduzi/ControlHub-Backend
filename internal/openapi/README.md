# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source OpenAPI contract, including the Operator Access Boundary security default |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema and authorization declarations |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Dependencies
- Upstream: `github.com/getkin/kin-openapi` for validation tests
- Downstream: `internal/api` serves the embedded contract

## Update Rule
If API route security or the OpenAPI contract changes, update this file in the same change.
