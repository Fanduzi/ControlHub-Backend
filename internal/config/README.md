# Config Module

Environment variable and .env file loading with sensible defaults.

## Files
| File | Responsibility |
|------|---------------|
| config.go | LoadDotEnv (.env loader), Load (reads env vars into Config), Config struct with HTTPAddress helper |
| config_test.go | Config loading tests |

## Exports
- `LoadDotEnv() error` — loads .env (graceful if missing)
- `Load() Config` — reads APP_PORT, DATABASE_DSN, JWT_SECRET from environment
- `Config` struct with `HTTPAddress()` method

## Dependencies
- Upstream: `github.com/joho/godotenv`, `os`
- Downstream: `cmd/server`

## Update Rule
If config variables change, update this file and .env.example.
