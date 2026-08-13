# Evidence: 38X-2 Authentication Hardening Release (Issues #21-#24)

This release delivers fixed query-execution freshness, authentication and
authorization audit telemetry, hardened Console BFF configuration, and
Argon2id password migration with the protected bootstrap-admin count
endpoint.

## Verified candidate facts

| Repository | Base | Candidate and merged product SHA |
| --- | --- | --- |
| `ControlHub-Backend` | `1713d8efa48478284d046e279bf9962153349607` | `5b45966a234fbb1d3b431dbf1361e32f160f9074` |
| `ControlHub-Frontend` | `1c5ad1f1663322501ed60616b37d460379083b73` | `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` |

Both product branches were fast-forward merged and normally pushed. The
backend push range was `1713d8e..5b45966`; the frontend push range was
`1c5ad1f..d6bc752`. No force push, rebase, amend, tag, or deployment was
used.

## Delivered contracts

- Query-execution credentials have a fixed eight-hour freshness ceiling.
- Authentication and authorization decisions emit fixed-metadata audit events
  without credential, password, token, DSN, or parameter-value disclosure.
- Production Console BFF configuration rejects weak session keys and unsafe
  origins; browser authentication remains sealed and server-held.
- Password verification supports legacy SHA-256 records only as a migration
  path, upgrades successful logins to Argon2id, and protects bootstrap-admin
  user counts behind current admin authorization.

## Gates

Backend candidate and merged-root checks passed:

- `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, and
  `make openapi-validate`
- Testcontainers integration coverage
- `make test-openapi-fuzz`: 2040 generated cases passed

Frontend candidate and merged-root checks passed under Node `22.22.0`:

- runtime check, TypeScript, build, E2E preflight, and E2E governance
- lint: zero errors; six existing warnings in untouched legacy files
- unit tests: 1494 passed
- real Chromium release E2E: 176 passed, zero failed, zero skipped

The browser run used the merged backend product SHA on an isolated loopback
port, a disposable metadata database migrated through version 17, and the
test-only fixture provisioner. Existing root services and fixtures were not
used for that run.

## Independent review

A fresh read-only review of the combined backend and frontend product ranges
reported P1=0 and P2=0. The remaining P3 note is that
`extractResourceIDFromPath` has no direct unit test; its behavior is covered
by the authorization integration matrix.

## CI

- Backend product CI:
  [run 31716085951](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31716085951)
  at `5b45966a234fbb1d3b431dbf1361e32f160f9074`; both
  `release-local-gates` and `release-docker-gates` succeeded.
- Frontend product CI:
  [run 31716088251](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31716088251)
  at `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3`; both `release-local` and
  `release-e2e` succeeded.

## Worktree preservation and cleanup

The backend and frontend root dirty-path manifests were captured before the
release and rechecked after product merges. Their user-owned WIP remained
unchanged. Task-owned temporary servers, credentials, and disposable database
state from browser verification were removed; the existing root services and
fixtures were preserved. Candidate worktrees and branches remain until final
verification of this evidence commit.
