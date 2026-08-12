# Config Module

Environment variable and .env file loading with sensible defaults.

## Files
| File | Responsibility |
|------|---------------|
| config.go | LoadDotEnv (.env loader), Load (reads env vars into Config), Config struct with HTTPAddress helper, ValidateJWTSecret (rejects blank/whitespace/short/placeholder signing secrets) |
| config_test.go | Config loading and signing-secret validation tests |

## Exports
- `LoadDotEnv() error` — loads .env (graceful if missing)
- `Load() (Config, error)` — reads APP_PORT, DATABASE_DSN, JWT_SECRET from environment; returns `ErrQueryExecutionTokenMaxAgeRejected` if the removed QUERY_EXECUTION_TOKEN_MAX_AGE env var is set
- `Config` struct with `HTTPAddress()` method
- `ValidateJWTSecret(string) error` — rejects blank, whitespace-only, secrets shorter than 32 UTF-8 bytes, and known placeholders (change-me, changeme, secret, your-secret-key, override-secret, <generated-hex-value>); fixed error text never echoes the secret
- `ErrInvalidJWTSecret` — sentinel error returned by ValidateJWTSecret
- `ErrQueryExecutionTokenMaxAgeRejected` — sentinel error returned by Load when the removed QUERY_EXECUTION_TOKEN_MAX_AGE env var is supplied; the eight-hour freshness bound is a fixed backend contract (Issue #21)

## Dependencies
- Upstream: `github.com/joho/godotenv`, `os`
- Downstream: `cmd/server`

## Update Rule
If config variables change, update this file and .env.example.
