# Cutover Local

Operator-invoked CLI that preserves legacy UUID-backed runtime tables and
rebuilds the schema with bigint identifiers.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Load config, parse flags, run local preserve-then-import cutover |

## Exports
- (binary entry point, no exported Go symbols)

## Dependencies
- Upstream: `internal/config`, `internal/cutover`
- Downstream: none (leaf in dependency tree)

## Update Rule
If cutover logic or config loading changes, update this file and cmd/cutover-local/main.go L3 header.
