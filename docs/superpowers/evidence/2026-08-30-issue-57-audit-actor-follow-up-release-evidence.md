# 2026-08-30 Issue 57 Audit Actor Follow-up Release Evidence

Date: 2026-08-30

## Scope

Frontend issue `Fanduzi/ControlHub-Frontend#57` required the audit API to expose
the verified User or Machine Principal as a privacy-safe label. The backend now
joins the identity dictionaries for global and per-resource audit reads, uses a
User email only when its display name is blank, preserves fixed unknown labels
for deleted identities, and returns `actor: null` for unauthenticated events.

## Refs

| Item | Value |
|------|-------|
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and prior `origin/main` | `32a4d7bac098ed7e79dbaaa736c488d80722b10a` |
| Implementation and pushed `main` SHA | `12d3907d96307f8eb9def0ea14a45c20c37a507d` |
| Push | Normal fast-forward `32a4d7b..12d3907`; no force push |
| Implementation CI | [33304565515](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/33304565515) — successful |
| Required CI jobs | `release-local-gates` (`99238682632`) and `release-docker-gates` (`99238682762`) — successful |
| Candidate worktree | `/var/folders/f1/vlfk2v8112qgypfdl_s75bz00000gn/T/tmp.Pd6VTrXYiR/backend` |

## Verification

- Red regression: the focused repository tests initially failed to compile
  because `model.AuditEvent` had no actor projection.
- `go test ./internal/repository/mysql ./internal/api ./internal/openapi -count=1` — passed.
- `make release-local-gates` — passed: uncached full tests, vet, build, and
  OpenAPI validation.
- `golangci-lint run ./... --new-from-rev=HEAD` — 0 new issues.
- `go mod tidy` plus `git diff --exit-code -- go.mod go.sum` — clean.
- Three-level documentation checker, staged mode — passed.
- CI MySQL integration and Schemathesis — 72/72 operations and 2941/2941
  generated cases passed; 0 failed.
- Read-only specification/standards review — PASS, P1=0, P2=0.

The repository-wide optional coverage report remains at the existing 62.5%
baseline, below the review skill's advisory 80% threshold. The repository-wide
lint baseline still contains 47 unrelated findings; the changed-lines lint gate
reported zero. Neither baseline was hidden or modified in this delivery.

## Preservation

The dirty root worktree was not used for implementation. Its existing modified
paths (`CLAUDE.md`, `CONTEXT.md`, `advisor-plans/README.md`) and existing
untracked backup/domain/specification files were preserved. No reset, clean,
stash, rebase, worktree removal, or unrelated-file edit was performed.
