# Health

Unauthenticated liveness for the ControlHub API process. Confirms the server this run started (or an agreed shared instance) is accepting HTTP on `APP_PORT`.

## Sub-features

- `health-ok` — `GET /health` returns JSON `{"status":"ok"}`.
- `startup-log` — process log contains `ControlHub starting on :` + port (when this run owns the PID).

## How to get to it (user POV)

- With the API listening (default `http://127.0.0.1:8080`), request `/health` with any HTTP client. No auth header required.

## Driving it with curl

Preconditions: baseline doctor OK or at least a listening server on `${APP_PORT:-8080}`.

```bash
PORT="${APP_PORT:-8080}"
curl -fsS "http://127.0.0.1:${PORT}/health" | tee "${EVIDENCE_DIR}/health.json"
# expect: {"status":"ok"}
curl -sS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:${PORT}/health" \
  | tee "${EVIDENCE_DIR}/health.http_code"
```

## Gotchas

- Health does not prove migrations or MySQL connectivity beyond "process is up"; a misconfigured DSN may still fail other routes.
- Do not treat OpenAPI `/docs` as the health gate; use `/health` specifically.
