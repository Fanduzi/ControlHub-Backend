# Scripts

Repository verification scripts.

## Files
| File | Responsibility |
|------|---------------|
| openapi-fuzz.sh | Runs authenticated Schemathesis OpenAPI fuzzing; exits 2 when prerequisites (including Bearer token) are missing and excludes executeSavedStatement without a stable fixture |

## OpenAPI Fuzz Exclusion Contract

Schemathesis exclusions are a governed, audited set. An exclusion may only be a
single-operation `--exclude-operation-id` flag in `openapi-fuzz.sh`; broad
path/method/tag/wildcard exclusions are forbidden. Any change to the set must
update this section and `internal/integration/openapi_fuzz_contract_test.go`
in the same change (`TestOpenAPIFuzzExclusionContract` enforces the contract
mechanically). `schemathesis.toml` carries only valid-parameter overrides
(`createResourceRelation`, `patchResource`) — those are not exclusions.

| Operation (id) | Path / method | Reason | Stable-fixture gap | Dedicated coverage | Allowed scope |
|---|---|---|---|---|---|
| `executeSavedStatement` | `POST /query-targets/{id}/saved-statements/{statementId}/execute` | Executing a saved statement needs the full governed chain against a real reachable query target (env-resolved credential + matching schema) plus a stored statement with typed parameter declarations | The disposable fuzz DB seeds no query target whose DSN/credential resolves in the fuzz server env, so generated requests deterministically fail pre-execution (404/403), exercising the error envelope rather than the contract; a request that did reach a target would execute arbitrary generated SQL against it, which the disposable harness cannot admit | Service: `internal/service/query_template_execution_service_test.go` (`TestExecuteSavedStatementRunsPersonalTemplateThroughGovernedChain`, `TestExecuteSavedStatementBindsTypedValuesInSourceOrder`, `TestExecuteSavedStatementRejectsForeignPersonalTemplate`, `TestExecuteSavedStatementAllowsSharedTemplateForAnyActor`, `TestExecuteSavedStatementRejectsMissingStatement`, `TestExecuteSavedStatementValidatesTypedValuesWithFieldErrors`, `TestExecuteSavedStatementRejectsOversizedStringValue`, `TestExecuteSavedStatementStaticStatementExecutesWithNoValues`, `TestExecuteSavedStatementRereadsLatestStatementEveryExecution`, `TestExecuteSavedStatementPagesThroughTemplateRouteWithFreshHistory`, `TestExecuteSavedStatementDisclosureChangeAffectsLaterPage`, `TestExecuteSavedStatementRecordsRejectedAttemptForAccessDenial`); integration (real MySQL): `internal/integration/query_template_execution_test.go` (`TestExecuteSavedStatementIntegrationBindsValuesAgainstRealMySQL`, `TestExecuteSavedStatementIntegrationAuthorizationMatrix`, `TestExecuteSavedStatementIntegrationTemplatePagination`, `TestExecuteSavedStatementIntegrationStaleDefinitionsFailClosed`, `TestExecuteSavedStatementIntegrationDisclosureChangeAffectsLaterPage`, `TestExecuteSavedStatementIntegrationHistoryKeepsPlaceholderSQL`); API handler: `internal/api/query_saved_statement_execution_handler_test.go` (`TestTemplateExecute_RequiresBearer`, `TestTemplateExecute_Success`, `TestTemplateExecute_RejectsUnknownAndForbiddenFields`, `TestTemplateExecute_RejectsDuplicateKeysAndMalformedJSON`, `TestTemplateExecute_RejectsOversizedValuesObject`, `TestTemplateExecute_RejectsInvalidPaginationAndMaxRows`, `TestTemplateExecute_ControlledFieldErrorsNeverEchoValues`, `TestTemplateExecute_ErrorMapping`) | Single operation only |

## Update Rule
Update this file when a script contract changes.
