// Package api provides HTTP handlers for actor-owned query workspaces.
// input: bytes, context, encoding/json, errors, io, net/http, internal/model, internal/service
// output: queryWorkspaceAPI and GET/PUT /query-workspace handlers with controlled OCC mapping
// pos: User-only HTTP boundary for the singular query-workspace aggregate
// note: if this file changes, update this header and module README.md.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type queryWorkspaceAPI interface {
	Get(ctx context.Context, ownerUserID uint64) (model.QueryWorkspace, error)
	Put(ctx context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (model.QueryWorkspace, error)
}

func decodeQueryWorkspaceJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, int64(model.MaxQueryWorkspaceJSONSize+1024))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if err := rejectDuplicateJSONFields(body); err != nil {
		return err
	}
	if err := validateQueryWorkspaceRequiredFields(body); err != nil {
		return err
	}
	return decodeJSON(bytes.NewReader(body), target)
}

func validateQueryWorkspaceRequiredFields(body []byte) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	expectedVersion, ok := payload["expectedVersion"]
	if !ok || bytes.Equal(bytes.TrimSpace(expectedVersion), []byte("null")) {
		return errors.New("expectedVersion is required")
	}
	worksheetsJSON, ok := payload["worksheets"]
	if !ok || bytes.Equal(bytes.TrimSpace(worksheetsJSON), []byte("null")) {
		return errors.New("worksheets is required")
	}
	var worksheets []map[string]json.RawMessage
	if err := json.Unmarshal(worksheetsJSON, &worksheets); err != nil {
		return err
	}
	for _, worksheet := range worksheets {
		if _, ok := worksheet["activeDatabase"]; !ok {
			return errors.New("worksheet activeDatabase is required")
		}
	}
	return nil
}

func handlePutQueryWorkspace(svc queryWorkspaceAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		var req model.QueryWorkspacePutRequest
		if err := decodeQueryWorkspaceJSONBody(w, r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		workspace, err := svc.Put(r.Context(), ownerUserID, req)
		if errors.Is(err, service.ErrQueryWorkspaceConflict) {
			writeJSONError(w, http.StatusConflict, "query_workspace_conflict", err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, workspace)
	}
}

func handleGetQueryWorkspace(svc queryWorkspaceAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		workspace, err := svc.Get(r.Context(), ownerUserID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, workspace)
	}
}
