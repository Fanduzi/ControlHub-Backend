// Package api provides HTTP handlers for profile write endpoints.
// input: internal/service (ProfileService), internal/api (decodeJSONBody), encoding/json, net/http
// output: handlePutResourceProfile, handlePatchResourceProfile, handleDeleteResourceProfile
// pos: HTTP handlers for PUT/PATCH/DELETE /resources/{id}/profile with strict single-JSON-object decoding
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/fan/controlhub/internal/service"
)

func handlePutResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(r.PathValue("id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var fields map[string]interface{}
		if err := decodeJSONBody(r, &fields); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid JSON")
			return
		}
		if fields == nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "profile must be a JSON object")
			return
		}

		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if err := profileService.PutProfileInventory(r.Context(), actorUserID, id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePatchResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(r.PathValue("id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var fields map[string]interface{}
		if err := decodeJSONBody(r, &fields); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid JSON")
			return
		}
		if fields == nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "profile must be a JSON object")
			return
		}

		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if err := profileService.PatchProfileInventory(r.Context(), actorUserID, id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		// An empty PATCH body is a no-op and reports 200 per the approved
		// design spec; a real partial update reports 204.
		if len(fields) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(r.PathValue("id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if err := profileService.DeleteProfileInventory(r.Context(), actorUserID, id); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
