# Scripts

Repository verification scripts.

## Files
| File | Responsibility |
|------|---------------|
| openapi-fuzz.sh | Runs authenticated Schemathesis OpenAPI fuzzing; exits 2 when prerequisites (including Bearer token) are missing and excludes executeSavedStatement without a stable fixture |
| query-e2e-mysql.sh | Owns the Query E2E MySQL lifecycle and per-user mode-0600 credential state; migrates a legacy worktree env file once and exposes its path without printing secrets |
| run-query-dev.sh | Dev-only launcher for `make run-query-dev`: ensures the Query E2E fixture, sources its quoted shared credential state (never parses), seeds the Local MySQL Query Dev target metadata, then runs the server on APP_PORT with an ephemeral JWT_SECRET |

## OpenAPI Fuzz Exclusion Contract

Schemathesis exclusions are a governed, audited set. An exclusion may only be a
single-operation `--exclude-operation-id` flag in `openapi-fuzz.sh`; broad
path/method/tag/wildcard exclusions are forbidden. `schemathesis.toml` carries
only valid-parameter overrides (`createResourceRelation`, `patchResource`) —
those are not exclusions.

Mechanically enforced by `TestOpenAPIFuzzExclusionContract`
(`internal/integration/openapi_fuzz_contract_test.go`, runs in every
`go test ./...`): every exclusion is a single-operation `--exclude-operation-id`
flag within the canonical set below, no broad path/method/tag flags appear, no
exclusion directives live in `schemathesis.toml`, and this contract section is
present. The per-row reason / fixture-gap / dedicated-test / scope fields are
the audited governance record and are verified by review at delivery.

Two deliberate run-scoping choices predate and are out of scope of this
operation-exclusion contract: `--phases examples,fuzzing` bounds the run to
bounded examples+fuzzing phases (stateful/coverage disabled) for agents and
CI, and `[warnings] fail-on = []` keeps missing-auth / missing-test-data /
validation-mismatch warnings advisory rather than CI-failing. Neither is an
operation exclusion; both are recorded here for audit completeness.

| Operation (id) | Path / method | Reason | Stable-fixture gap | Dedicated coverage | Allowed scope |
|---|---|---|---|---|---|
| `executeSavedStatement` | `POST /query-targets/{id}/saved-statements/{statementId}/execute` | Executing a saved statement needs the full governed chain against a real reachable query target (env-resolved credential + matching schema) plus a stored statement with typed parameter declarations | The disposable fuzz DB seeds no query target whose DSN/credential resolves in the fuzz server env, so generated requests deterministically fail pre-execution (404/403), exercising the error envelope rather than the contract; a request that did reach a target would execute arbitrary generated SQL against it, which the disposable harness cannot admit | Service: `internal/service/query_template_execution_service_test.go` (`TestExecuteSavedStatementRunsPersonalTemplateThroughGovernedChain`, `TestExecuteSavedStatementBindsTypedValuesInSourceOrder`, `TestExecuteSavedStatementRejectsForeignPersonalTemplate`, `TestExecuteSavedStatementAllowsSharedTemplateForAnyActor`, `TestExecuteSavedStatementRejectsMissingStatement`, `TestExecuteSavedStatementValidatesTypedValuesWithFieldErrors`, `TestExecuteSavedStatementRejectsOversizedStringValue`, `TestExecuteSavedStatementStaticStatementExecutesWithNoValues`, `TestExecuteSavedStatementRereadsLatestStatementEveryExecution`, `TestExecuteSavedStatementPagesThroughTemplateRouteWithFreshHistory`, `TestExecuteSavedStatementDisclosureChangeAffectsLaterPage`, `TestExecuteSavedStatementRecordsRejectedAttemptForAccessDenial`); integration (real MySQL): `internal/integration/query_template_execution_test.go` (`TestExecuteSavedStatementIntegrationBindsValuesAgainstRealMySQL`, `TestExecuteSavedStatementIntegrationAuthorizationMatrix`, `TestExecuteSavedStatementIntegrationTemplatePagination`, `TestExecuteSavedStatementIntegrationStaleDefinitionsFailClosed`, `TestExecuteSavedStatementIntegrationDisclosureChangeAffectsLaterPage`, `TestExecuteSavedStatementIntegrationHistoryKeepsPlaceholderSQL`); API handler: `internal/api/query_saved_statement_execution_handler_test.go` (`TestTemplateExecute_RequiresBearer`, `TestTemplateExecute_Success`, `TestTemplateExecute_RejectsUnknownAndForbiddenFields`, `TestTemplateExecute_RejectsDuplicateKeysAndMalformedJSON`, `TestTemplateExecute_RejectsOversizedValuesObject`, `TestTemplateExecute_RejectsInvalidPaginationAndMaxRows`, `TestTemplateExecute_ControlledFieldErrorsNeverEchoValues`, `TestTemplateExecute_ErrorMapping`) | Single operation only |

## Update Rule
Update this file when a script contract changes.
