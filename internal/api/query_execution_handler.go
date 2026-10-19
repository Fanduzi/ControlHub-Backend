// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: bytes, context, encoding/json, errors, fmt, io, net/http, strconv, strings, time, chi, internal/model, internal/service
// output: user-or-machine ordinary execute identity, route-local strict execute decoder, user-only sibling query and owner-only statement handlers, controlled error mapping with claim 409 sentinels, queryExecutionAPI/queryExecutionStatementAPI interfaces
// pos: HTTP handlers for governed execute/history/owner-only statement retrieval, saved-statement execution, and related-record navigation. All three execution entries accept an optional verbatim clientExecutionId claim key through route-local strict decoders with exact case-sensitive field allowlists and explicit-null rejection on every typed scalar/member (nested source members included); ordinary database/schema and navigation source.schema stay fail-closed for MySQL/TiDB. The history list mixes terminal rows with claim-derived entries and accepts database/clientExecutionId filters plus the v2 mixed-ordering cursor (T10-C). Execute/related disclosure blocks publish query_result_disclosure_blocked (Issue #48); target-not-enabled remains query_not_allowed; claim replay/conflict publishes execution_already_exists/client_execution_id_conflict and post-finalize result-contract refusals publish result_contract_upgrade_required as 409 on all three result routes (T8-B).
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// queryExecutionAPI is the handler-level interface the query execution service
// satisfies. Depending on the small interface (not the concrete struct) keeps
// the handlers thin and lets handler tests substitute a stub. The actor is never
// accepted from the request body — it is read from the auth middleware context.
type queryExecutionAPI interface {
	Execute(ctx context.Context, identity model.QueryExecutionIdentity, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error)
	ExecuteSavedStatement(ctx context.Context, actorUserID, targetID, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error)
	ListHistory(ctx context.Context, identity model.QueryExecutionIdentity, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error)
	QueryEvidencePersistenceFailures() int64
	NavigateRelatedRecords(ctx context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error)
}

type queryExecutionStatementAPI interface {
	GetExecutionStatement(ctx context.Context, actorUserID, targetID, executionID uint64) (model.QueryExecutionStatementResponse, error)
}

// mysqlExecuteMaxBodyBytes bounds the ordinary execute body: a worst-case
// escaped MaxStatementLength-rune statement plus the surrounding fields. The
// statement bound counts runes but the body cap counts wire bytes; a
// supplementary-plane rune may legally arrive as a XXXXXXXX surrogate
// pair — 12 ASCII bytes — so the cap derives from 12 bytes per rune, not
// from any UTF-8 byte count. Clients are never required to send raw UTF-8.
const mysqlExecuteMaxBodyBytes = 12*model.MaxStatementLength + 8192

// mysqlNavigationMaxBodyBytes bounds the navigation body: the model caps
// localValues at 16 entries of MaxLocalValueLength decoded UTF-8 bytes each,
// and one decoded byte can arrive as at most 6 bytes of XXXX escape —
// 16*6*MaxLocalValueLength plus the surrounding fields is the exact
// worst-case legal encoding, so no supplementary-plane factor applies.
const mysqlNavigationMaxBodyBytes = 16*6*model.MaxLocalValueLength + 8192

// readStrictBody is the shared bounded-read step for the three governed
// execution decoders: the body is capped at maxBytes (one byte past the
// limit is an error, never a truncated suffix), must be valid UTF-8 before
// encoding/json can replace invalid bytes, must not contain an unpaired
// UTF-16 surrogate escape that encoding/json would lossily map to U+FFFD,
// and carries no duplicate field names.
func readStrictBody(r *http.Request, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errors.New("request body too large")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("request body must be valid UTF-8")
	}
	if err := rejectUnpairedSurrogateEscapes(body); err != nil {
		return nil, err
	}
	if err := rejectDuplicateJSONFields(body); err != nil {
		return nil, err
	}
	return body, nil
}

// rejectUnpairedSurrogateEscapes scans the raw body before encoding/json can
// lossily rewrite it: inside a JSON string, a \uXXXX escape in the high
// surrogate range D800–DBFF must be immediately followed by a \uXXXX escape
// in the low range DC00–DFFF, and a low surrogate escape may never appear
// without its high partner. encoding/json substitutes U+FFFD for each
// unpaired escape, so without this boundary two different malformed inputs
// would decode to one identical value and field names, keys, statements,
// and locator values could not stay byte-faithful. Escaped backslashes and
// quotes are skipped so a literal backslash-u text is not a surrogate
// escape; malformed non-surrogate syntax is left for the decoder to reject.
// The error is a fixed message that never echoes body bytes.
func rejectUnpairedSurrogateEscapes(body []byte) error {
	inString := false
	for i := 0; i < len(body); i++ {
		if !inString {
			if body[i] == '"' {
				inString = true
			}
			continue
		}
		switch body[i] {
		case '"':
			inString = false
		case '\\':
			if i+1 >= len(body) {
				return nil
			}
			i++
			if body[i] != 'u' {
				continue
			}
			if i+4 >= len(body) {
				return nil
			}
			code, ok := jsonHex4(body[i+1 : i+5])
			if !ok {
				continue
			}
			i += 4
			if code >= 0xD800 && code <= 0xDBFF {
				if i+6 < len(body) && body[i+1] == '\\' && body[i+2] == 'u' {
					if low, ok := jsonHex4(body[i+3 : i+7]); ok && low >= 0xDC00 && low <= 0xDFFF {
						i += 6
						continue
					}
				}
				return errors.New("request body contains an unpaired surrogate escape")
			}
			if code >= 0xDC00 && code <= 0xDFFF {
				return errors.New("request body contains an unpaired surrogate escape")
			}
		}
	}
	return nil
}

// jsonHex4 parses exactly four ASCII hex digits into a UTF-16 code unit.
func jsonHex4(b []byte) (uint16, bool) {
	var code uint16
	for _, c := range b {
		code <<= 4
		switch {
		case c >= '0' && c <= '9':
			code |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			code |= uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			code |= uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return code, true
}

// strictJSONObjectFields returns the raw top-level members after enforcing
// the route's exact field allowlist: every top-level name must be spelled
// exactly as allowed (encoding/json matches struct fields case-insensitively
// even under DisallowUnknownFields, so a case-variant alias would otherwise
// land in the typed field) and a member with a nested allowlist must be an
// object whose names are exact members of that nested set. Messages are
// fixed and never echo body bytes.
func strictJSONObjectFields(body []byte, allowed map[string][]string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	for name, raw := range fields {
		nested, ok := allowed[name]
		if !ok {
			return nil, fmt.Errorf("unknown field %s", name)
		}
		if nested == nil {
			continue
		}
		var sub map[string]json.RawMessage
		if err := json.Unmarshal(raw, &sub); err != nil || sub == nil {
			return nil, fmt.Errorf("%s must be an object", name)
		}
		for subName := range sub {
			found := false
			for _, allowedName := range nested {
				if subName == allowedName {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("unknown field %s.%s", name, subName)
			}
		}
	}
	return fields, nil
}

// requireStringMembers validates that raw is a non-null JSON array whose
// members are all non-null JSON strings.
func requireStringMembers(raw json.RawMessage, field string) error {
	if string(bytes.TrimSpace(raw)) == "null" {
		return fmt.Errorf("%s must be an array of strings", field)
	}
	var members []json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return fmt.Errorf("%s must be an array of strings", field)
	}
	for _, member := range members {
		if string(bytes.TrimSpace(member)) == "null" {
			return fmt.Errorf("%s members must be strings", field)
		}
		var s string
		if err := json.Unmarshal(member, &s); err != nil {
			return fmt.Errorf("%s members must be strings", field)
		}
	}
	return nil
}

// nullScalarField names one typed field whose present literal-null value is a
// malformed type; kind is the expected JSON type for the fixed message.
type nullScalarField struct {
	name string
	kind string
}

// rejectNullScalars rejects a present JSON null on each named member of one
// raw field map — an explicit null where the contract types a scalar is a
// malformed type, never a silent zero or empty value. Absent optional fields
// and empty strings keep their existing semantics. prefix qualifies member
// names in the fixed message ("" for top-level fields, "source." for nested
// source members).
func rejectNullScalars(members map[string]json.RawMessage, prefix string, scalars ...nullScalarField) error {
	for _, scalar := range scalars {
		if raw, ok := members[scalar.name]; ok && string(bytes.TrimSpace(raw)) == "null" {
			return fmt.Errorf("%s%s must be a %s", prefix, scalar.name, scalar.kind)
		}
	}
	return nil
}

// rejectNestedNullScalars applies rejectNullScalars to one nested object
// member of the raw field map. strictJSONObjectFields has already proved a
// present member is a non-null object matching the nested allowlist, so the
// unmarshal cannot fail.
func rejectNestedNullScalars(members map[string]json.RawMessage, field string, scalars ...nullScalarField) error {
	raw, ok := members[field]
	if !ok {
		return nil
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil || nested == nil {
		return fmt.Errorf("%s must be an object", field)
	}
	return rejectNullScalars(nested, field+".", scalars...)
}

// executeAllowedFields is the exact case-sensitive field set for the ordinary
// execute route; pagination carries the nested exact set.
var executeAllowedFields = map[string][]string{
	"statement":         nil,
	"database":          nil,
	"schema":            nil,
	"maxRows":           nil,
	"pagination":        {"page", "pageSize"},
	"capabilities":      nil,
	"clientExecutionId": nil,
}

// decodeExecuteJSONBody is the route-local strict decoder for ordinary
// execution: bounded body, valid UTF-8, no duplicate fields, exact
// case-sensitive field allowlist (nested pagination included), exactly one
// JSON value, and the top-level value must be an object. An explicit JSON
// null where the contract types a field — statement, database, schema,
// clientExecutionId, maxRows, pagination, capabilities, or a member of
// capabilities — is a malformed type, never a silent zero or empty value.
// Error messages are fixed and never echo body bytes.
func decodeExecuteJSONBody(r *http.Request, target *model.QueryExecuteRequest) error {
	body, err := readStrictBody(r, mysqlExecuteMaxBodyBytes)
	if err != nil {
		return err
	}
	fields, err := strictJSONObjectFields(body, executeAllowedFields)
	if err != nil {
		return err
	}
	if err := rejectNullScalars(fields, "",
		nullScalarField{"statement", "string"},
		nullScalarField{"database", "string"},
		nullScalarField{"schema", "string"},
		nullScalarField{"maxRows", "number"},
		nullScalarField{"clientExecutionId", "string"}); err != nil {
		return err
	}
	if raw, ok := fields["capabilities"]; ok {
		if err := requireStringMembers(raw, "capabilities"); err != nil {
			return err
		}
	}
	return decodeJSON(bytes.NewReader(body), target)
}

func handleExecuteQuery(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.QueryExecuteRequest
		if err := decodeExecuteJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		identity, ok := queryExecutionIdentityFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.Execute(r.Context(), identity, targetID, req)
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func queryExecutionIdentityFromContext(ctx context.Context) (model.QueryExecutionIdentity, bool) {
	actorUserID, userOK := actorUserIDFromContext(ctx)
	machine, machineOK := machinePrincipalFromContext(ctx)
	if userOK == machineOK {
		return model.QueryExecutionIdentity{}, false
	}
	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorUserID}
	if machineOK {
		identity = model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: machine.ID}
	}
	return identity, identity.Validate() == nil
}

// savedExecuteAllowedFields is the exact case-sensitive field set for the
// saved-statement execute route; pagination carries the nested exact set and
// values keys are author-declared parameter names validated by the model.
var savedExecuteAllowedFields = map[string][]string{
	"values":            nil,
	"maxRows":           nil,
	"pagination":        {"page", "pageSize"},
	"capabilities":      nil,
	"clientExecutionId": nil,
}

// decodeTemplateExecuteJSONBody strictly decodes a template-execution request:
// only values, maxRows, pagination, capabilities, and clientExecutionId
// survive; duplicate keys, case-variant aliases, and unknown fields (SQL
// text, declarations, identities, roles, credentials, DSNs, policy and audit
// payloads) are rejected. The body is bounded so the 16 KiB values limit is
// enforced by the model Validate. An explicit JSON null on a typed field —
// values, maxRows, pagination, capabilities, clientExecutionId, or a member
// of capabilities — is a malformed type; a missing values object or {} is
// valid for static statements.
func decodeTemplateExecuteJSONBody(r *http.Request, target any) error {
	body, err := readStrictBody(r, int64(model.MaxQuerySavedStatementExecuteValuesSize+2*1024))
	if err != nil {
		return err
	}
	fields, err := strictJSONObjectFields(body, savedExecuteAllowedFields)
	if err != nil {
		return err
	}
	if raw, ok := fields["values"]; ok {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil || values == nil {
			return errors.New("values must be an object")
		}
	}
	if err := rejectNullScalars(fields, "",
		nullScalarField{"maxRows", "number"},
		nullScalarField{"clientExecutionId", "string"}); err != nil {
		return err
	}
	if raw, ok := fields["capabilities"]; ok {
		if err := requireStringMembers(raw, "capabilities"); err != nil {
			return err
		}
	}
	return decodeJSON(bytes.NewReader(body), target)
}

// handleExecuteSavedStatement handles
// POST /query-targets/{id}/saved-statements/{statementId}/execute. The actor
// comes from the verified token; the statement ID and values only from the
// body/path. The server re-reads and authorizes the latest saved statement.
func handleExecuteSavedStatement(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statement id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.QuerySavedStatementExecuteRequest
		if err := decodeTemplateExecuteJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.ExecuteSavedStatement(r.Context(), actorUserID, targetID, statementID, req)
		if err != nil {
			writeTemplateExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeJSONFieldError writes the error envelope with controlled per-field
// details. Field keys are author-declared parameter names and values are fixed
// codes; supplied parameter values are never included.
func writeJSONFieldError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{
		"error":   code,
		"message": message,
		"details": fields,
	})
}

// writeTemplateExecutionError maps a template-execution service sentinel to a
// controlled HTTP response. The common execute sentinels reuse the ordinary
// execute mapper; only the template-specific cases (field codes and the
// saved-statement 404) are handled here. Parameter-name field codes carry no
// values.
func writeTemplateExecutionError(w http.ResponseWriter, err error) {
	var valueErr *service.TemplateValueValidationError
	if errors.As(err, &valueErr) {
		writeJSONFieldError(w, http.StatusBadRequest, "validation_failed", valueErr.Error(), valueErr.Fields)
		return
	}
	if errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		writeJSONError(w, http.StatusNotFound, "saved_statement_not_found", err.Error())
		return
	}
	writeQueryExecutionError(w, err)
}

func handleListQueryExecutions(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		actorRole, _ := actorRoleFromContext(r.Context())
		status, from, to, cursor, database, clientExecutionID, err := parseExecutionFilters(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		// WHY: parseExecutionFilters is parsed first so the page+cursor conflict
		// message takes precedence over page-value validation — the conflict is a
		// more specific diagnosis than "page is not a positive integer" and keeps
		// the existing 400 contract stable for clients that already special-case it.
		pg, err := parseExecutionPagination(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorUserID}
		result, err := svc.ListHistory(r.Context(), identity, actorRole, targetID, model.QueryExecutionListQuery{
			TargetResourceID: targetID, Page: pg.Page, PageSize: pg.PageSize,
			Database: database, ClientExecutionID: clientExecutionID,
			Status: status, From: from, To: to, Cursor: cursor,
		})
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetQueryExecutionStatement(svc queryExecutionStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		executionID, err := parseUint64IDParam(chi.URLParam(r, "executionId"), "execution id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		response, err := svc.GetExecutionStatement(r.Context(), actorUserID, targetID, executionID)
		if errors.Is(err, service.ErrQueryExecutionNotFound) {
			writeJSONError(w, http.StatusNotFound, "query_execution_not_found", err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func parseExecutionFilters(r *http.Request) (status *model.QueryExecutionStatus, from, to *time.Time, cursor *string, database, clientExecutionID string, err error) {
	q := r.URL.Query()

	if q.Has("page") && q.Has("cursor") {
		return nil, nil, nil, nil, "", "", fmt.Errorf("cannot use both page and cursor parameters")
	}

	if raw, present := q["status"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("status parameter must be a single non-empty value")
		}
		if err := model.ValidateStatus(raw[0]); err != nil {
			return nil, nil, nil, nil, "", "", err
		}
		s := model.QueryExecutionStatus(raw[0])
		status = &s
	}

	if raw, present := q["database"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("database parameter must be a single non-empty value")
		}
		if utf8.RuneCountInString(raw[0]) > model.MaxIdentifierLength {
			return nil, nil, nil, nil, "", "", fmt.Errorf("database exceeds %d characters", model.MaxIdentifierLength)
		}
		database = raw[0]
	}

	if raw, present := q["clientExecutionId"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("clientExecutionId parameter must be a single non-empty value")
		}
		if err := model.ValidateClientExecutionID(raw[0]); err != nil {
			return nil, nil, nil, nil, "", "", err
		}
		if strings.IndexByte(raw[0], 0) >= 0 {
			return nil, nil, nil, nil, "", "", fmt.Errorf("clientExecutionId must not contain NUL bytes")
		}
		clientExecutionID = raw[0]
	}

	if raw, present := q["from"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("from parameter must be a single non-empty value")
		}
		t, err := model.ParseTimestamp(raw[0])
		if err != nil {
			return nil, nil, nil, nil, "", "", err
		}
		from = &t
	}

	if raw, present := q["to"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("to parameter must be a single non-empty value")
		}
		t, err := model.ParseTimestamp(raw[0])
		if err != nil {
			return nil, nil, nil, nil, "", "", err
		}
		to = &t
	}

	if err := model.ValidateTimeWindow(from, to); err != nil {
		return nil, nil, nil, nil, "", "", err
	}

	if raw, present := q["cursor"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return nil, nil, nil, nil, "", "", fmt.Errorf("cursor parameter must be a single non-empty value")
		}
		cursor = &raw[0]
	}

	return status, from, to, cursor, database, clientExecutionID, nil
}

// writeQueryExecutionError maps a service sentinel to a controlled HTTP response.
// No guard, policy, credential, or target-database validation issue becomes a 500.
// ErrQueryDisclosureBlocked is matched before ErrQueryNotAllowed because execute
// rejections wrap both; matching the other way published query_not_allowed.
func writeQueryExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryDisclosureBlocked):
		writeJSONError(w, http.StatusForbidden, "query_result_disclosure_blocked", err.Error())
	case errors.Is(err, service.ErrQueryNotAllowed):
		writeJSONError(w, http.StatusForbidden, "query_not_allowed", err.Error())
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQueryTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "query_timeout", err.Error())
	case errors.Is(err, service.ErrQueryBackendFailure):
		writeJSONError(w, http.StatusBadGateway, "query_backend_error", err.Error())
	case errors.Is(err, service.ErrQueryExecutionAlreadyExists):
		// Claim occupancy sentinels: safe fixed codes with no row, occupant,
		// or claim detail — the client learns only the dedup verdict.
		writeJSONError(w, http.StatusConflict, "execution_already_exists", err.Error())
	case errors.Is(err, service.ErrQueryExecutionConflict):
		writeJSONError(w, http.StatusConflict, "client_execution_id_conflict", err.Error())
	case errors.Is(err, service.ErrResultContractUpgradeRequired):
		// T8-B wiring convention: the capability refusal is a post-finalize
		// delivery refusal — the attempt already recorded success — so the
		// response is a controlled 409 with no result payload.
		writeJSONError(w, http.StatusConflict, "result_contract_upgrade_required", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}

// executionPagination is the validated pagination boundary for the history
// list endpoint. Page == 0 means "page parameter absent" → cursor-initial mode.
// PageSize is always in [1, MaxPageSize] when this struct is returned without
// error; an absent pageSize parameter resolves to DefaultPageSize.
type executionPagination struct {
	Page     int
	PageSize int
}

// parseExecutionPagination is the single HTTP contract parser for the page and
// pageSize query parameters on GET /query-targets/{id}/executions. It
// distinguishes "parameter absent" from "parameter supplied but invalid":
//
//   - absent page        → cursor-initial mode (Page == 0)
//   - valid page (>=1)   → legacy offset mode (Page == N)
//   - invalid page       → 400 validation_failed (empty, zero, negative,
//     non-integer, >1 value, or out of int range)
//   - absent pageSize    → DefaultPageSize (20)
//   - valid pageSize     → clamped to [1, MaxPageSize]
//   - invalid pageSize   → 400 validation_failed (empty, zero, negative,
//     non-integer, >1 value, >MaxPageSize, or out of int
//     range)
//
// Repeated query parameters (?page=1&page=2) are rejected because the choice
// between two valid values is ambiguous and not part of the public contract.
// The service-layer NormalizePagination remains as defense-in-depth for any
// internal caller that bypasses this handler, but it is no longer the HTTP
// contract parser.
func parseExecutionPagination(r *http.Request) (executionPagination, error) {
	q := r.URL.Query()
	var pg executionPagination
	pg.PageSize = model.DefaultPageSize

	if raw, present := q["page"]; present {
		// q["page"] returns []string{} for "?page=" and a single-element slice
		// for "?page=N". Get() collapses both to "", which would silently treat
		// an explicit empty value as absent — that is the regression being fixed.
		if len(raw) != 1 || raw[0] == "" {
			return executionPagination{}, fmt.Errorf("page parameter must be a positive integer")
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 {
			return executionPagination{}, fmt.Errorf("page parameter must be a positive integer")
		}
		pg.Page = n
	}

	if raw, present := q["pageSize"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return executionPagination{}, fmt.Errorf("pageSize parameter must be an integer in 1..500")
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > model.MaxPageSize {
			return executionPagination{}, fmt.Errorf("pageSize parameter must be an integer in 1..500")
		}
		pg.PageSize = n
	}

	return pg, nil
}

// navigationAllowedFields is the exact case-sensitive field set for the
// related-records route; source carries the nested exact set.
var navigationAllowedFields = map[string][]string{
	"source":            {"database", "schema", "object", "kind", "foreignKey"},
	"localValues":       nil,
	"maxRows":           nil,
	"capabilities":      nil,
	"clientExecutionId": nil,
}

// decodeNavigationJSONBody is the route-local strict decoder for related
// navigation: bounded body, valid UTF-8, no duplicate fields, exact
// case-sensitive field allowlist (nested source included), one JSON value, a
// non-null top-level object. Explicit JSON null on a typed field — source,
// any nested source member, localValues, maxRows, capabilities,
// clientExecutionId, or a member of localValues/capabilities — is a
// malformed type; an empty-string localValues member stays valid
// (byte-equality binding), and the closed source.schema selector keeps its
// absent-or-empty-string shape. Messages are fixed.
func decodeNavigationJSONBody(r *http.Request, target *model.RelatedRecordNavigationRequest) error {
	body, err := readStrictBody(r, mysqlNavigationMaxBodyBytes)
	if err != nil {
		return err
	}
	fields, err := strictJSONObjectFields(body, navigationAllowedFields)
	if err != nil {
		return err
	}
	if err := rejectNullScalars(fields, "",
		nullScalarField{"maxRows", "number"},
		nullScalarField{"clientExecutionId", "string"}); err != nil {
		return err
	}
	if err := rejectNestedNullScalars(fields, "source",
		nullScalarField{"database", "string"},
		nullScalarField{"schema", "string"},
		nullScalarField{"object", "string"},
		nullScalarField{"kind", "string"},
		nullScalarField{"foreignKey", "string"}); err != nil {
		return err
	}
	if raw, ok := fields["localValues"]; ok {
		if err := requireStringMembers(raw, "localValues"); err != nil {
			return err
		}
	}
	if raw, ok := fields["capabilities"]; ok {
		if err := requireStringMembers(raw, "capabilities"); err != nil {
			return err
		}
	}
	return decodeJSON(bytes.NewReader(body), target)
}

// handleNavigateRelatedRecords handles POST /query-targets/{id}/related-records.
// It validates the request body, extracts the actor from the auth context, and
// delegates to the service. The browser never supplies referenced identifiers,
// SQL, credentials, or actor identity.
func handleNavigateRelatedRecords(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.RelatedRecordNavigationRequest
		if err := decodeNavigationJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		// source.schema (pinned-schema selector) stays fail-closed for the
		// MySQL/TiDB navigation path; source.database keeps its existing
		// browse-scope meaning.
		if req.Source.Schema != "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed",
				"source.schema is not accepted until the postgresql execution path is enabled")
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.NavigateRelatedRecords(r.Context(), actorUserID, targetID, req)
		if err != nil {
			writeNavigationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeNavigationError maps a service sentinel to a controlled HTTP response.
// It reuses the same error mapping as writeQueryExecutionError since navigation
// shares the same governance and sentinel vocabulary. Disclosure is matched
// before not-allowed for the same wrap-order reason as writeQueryExecutionError.
func writeNavigationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed),
		errors.Is(err, service.ErrNavigationSourceNotFound),
		errors.Is(err, service.ErrNavigationValueMismatch):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryDisclosureBlocked):
		writeJSONError(w, http.StatusForbidden, "query_result_disclosure_blocked", err.Error())
	case errors.Is(err, service.ErrQueryNotAllowed):
		writeJSONError(w, http.StatusForbidden, "query_not_allowed", err.Error())
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQueryTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "query_timeout", err.Error())
	case errors.Is(err, service.ErrQueryBackendFailure):
		writeJSONError(w, http.StatusBadGateway, "query_backend_error", err.Error())
	case errors.Is(err, service.ErrQueryExecutionAlreadyExists):
		writeJSONError(w, http.StatusConflict, "execution_already_exists", err.Error())
	case errors.Is(err, service.ErrQueryExecutionConflict):
		writeJSONError(w, http.StatusConflict, "client_execution_id_conflict", err.Error())
	case errors.Is(err, service.ErrResultContractUpgradeRequired):
		writeJSONError(w, http.StatusConflict, "result_contract_upgrade_required", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}
