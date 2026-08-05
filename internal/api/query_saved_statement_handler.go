// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, internal/model, internal/service
// output: querySavedStatementAPI interface, handleListSavedStatements, handleCreateSavedStatement, handleUpdateSavedStatement, handleDeleteSavedStatement, writeSavedStatementError
// pos: HTTP handlers for CRUD /query-targets/{id}/saved-statements (Phase 38R governed saved statements)
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func parseOptionalInt(raw string, defaultVal int) (int, error) {
	if raw == "" {
		return defaultVal, nil
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return val, nil
}

// querySavedStatementAPI is the handler-level interface the saved statement
// service satisfies. Actor is taken from the verified token, never from body.
type querySavedStatementAPI interface {
	List(ctx context.Context, actor service.AuthenticatedUser, targetResourceID uint64, q string, page, pageSize int) (model.QuerySavedStatementListResponse, error)
	Create(ctx context.Context, actor service.AuthenticatedUser, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
	Update(ctx context.Context, actor service.AuthenticatedUser, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error
	Delete(ctx context.Context, actor service.AuthenticatedUser, targetResourceID, statementID uint64) error
}

// handleListSavedStatements handles GET /query-targets/{id}/saved-statements.
func handleListSavedStatements(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		q := r.URL.Query().Get("q")
		page, err := parseOptionalInt(r.URL.Query().Get("page"), 1)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid page parameter")
			return
		}
		pageSize, err := parseOptionalInt(r.URL.Query().Get("pageSize"), 20)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid pageSize parameter")
			return
		}

		resp, err := svc.List(r.Context(), actor, targetResourceID, q, page, pageSize)
		if err != nil {
			writeSavedStatementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleCreateSavedStatement handles POST /query-targets/{id}/saved-statements.
func handleCreateSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var req model.QuerySavedStatementCreateRequest
		if err := decodeSavedStatementJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}

		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		result, err := svc.Create(r.Context(), actor, targetResourceID, req)
		if err != nil {
			writeSavedStatementError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

// handleUpdateSavedStatement handles PUT /query-targets/{id}/saved-statements/{statementId}.
func handleUpdateSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statementId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var req model.QuerySavedStatementUpdateRequest
		if err := decodeSavedStatementJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}

		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		if err := svc.Update(r.Context(), actor, targetResourceID, statementID, req); err != nil {
			writeSavedStatementError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func decodeSavedStatementJSONBody(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(model.MaxSavedStatementSize+16*1024)))
	if err != nil {
		return err
	}
	if err := rejectDuplicateJSONFields(body); err != nil {
		return err
	}
	return decodeJSON(bytes.NewReader(body), target)
}

func rejectDuplicateJSONFields(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object field")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate JSON field")
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return nil
	}
}

// handleDeleteSavedStatement handles DELETE /query-targets/{id}/saved-statements/{statementId}.
func handleDeleteSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statementId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		if err := svc.Delete(r.Context(), actor, targetResourceID, statementID); err != nil {
			writeSavedStatementError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeSavedStatementError maps a service sentinel to a controlled HTTP response.
func writeSavedStatementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQuerySavedStatementNotFound):
		writeJSONError(w, http.StatusNotFound, "saved_statement_not_found", err.Error())
	case errors.Is(err, service.ErrQueryForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
