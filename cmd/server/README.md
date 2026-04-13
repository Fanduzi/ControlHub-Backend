# Server Entry Point

Application bootstrap and manual dependency injection.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Load config, open DB, wire all services into api.Dependencies, start HTTP server |

## Exports
- (binary entry point, no exported Go symbols)

## Dependencies
- Upstream: `internal/config`, `internal/api`, `internal/service`, `internal/repository/mysql`
- Downstream: none (leaf in dependency tree)

## Update Rule
If services or wiring change, update this file and the root README.md Architecture section.
