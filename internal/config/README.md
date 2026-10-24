# Config Module

Environment variable and .env file loading with sensible defaults.

## Files
| File | Responsibility |
|------|---------------|
| config.go | LoadDotEnv (.env loader), Load (reads env vars into Config), Config struct with HTTPAddress helper, ValidateJWTSecret (rejects blank/whitespace/short/placeholder signing secrets), parsePGHTTPEntries (strict release-gate parsing) |
| config_test.go | Config loading and signing-secret validation tests |

## Exports
- `LoadDotEnv() error` — loads .env (graceful if missing)
- `Load() (Config, error)` — reads APP_PORT, DATABASE_DSN, JWT_SECRET, CONTROLHUB_PG_HTTP_ENTRIES from environment; returns `ErrQueryExecutionTokenMaxAgeRejected` if the removed QUERY_EXECUTION_TOKEN_MAX_AGE env var is present (even if empty), and `ErrInvalidPGHTTPEntries` when the release gate holds any value outside the exact accepted set
- `Config` struct with `HTTPAddress()` method
- `ValidateJWTSecret(string) error` — rejects blank, whitespace-only, secrets shorter than 32 UTF-8 bytes, and known placeholders (change-me, changeme, secret, your-secret-key, override-secret, <generated-hex-value>); fixed error text never echoes the secret
- `ErrInvalidJWTSecret` — sentinel error returned by ValidateJWTSecret
- `ErrQueryExecutionTokenMaxAgeRejected` — sentinel error returned by Load when the removed QUERY_EXECUTION_TOKEN_MAX_AGE env var is supplied; the eight-hour freshness bound is a fixed backend contract (Issue #21)
- `ErrInvalidPGHTTPEntries` — sentinel error returned by Load when CONTROLHUB_PG_HTTP_ENTRIES is misspelled or uses a loose form; the production PostgreSQL HTTP gate fails closed at startup rather than guessing at a typo

## CONTROLHUB_PG_HTTP_ENTRIES

Deployment-time release gate for PostgreSQL HTTP execution. Exact-match
semantics (no trimming, no case folding):

| Value | Result |
|-------|--------|
| unset, empty, `off` | `Config.PGHTTPEntries=false` — default-closed production graph |
| `on` | `Config.PGHTTPEntries=true` — Build opens both PG construction-time capabilities |
| anything else (`true`, `1`, `ON`, padded variants, typos) | `ErrInvalidPGHTTPEntries` — startup fails before any listener opens |

The flag is consumed once at assembly (`serverboot.Build`); it is never an
HTTP/runtime switch, and it grants no connection any privilege — `enabled`,
`environment_policy`, and DSN binding still govern every request.

## Dependencies
- Upstream: `github.com/joho/godotenv`, `os`
- Downstream: `cmd/server`

## Update Rule
If config variables change, update this file and .env.example.
