// Package api provides HTTP handlers for profile write endpoints.
// input: internal/service (ProfileService), encoding/json, net/http
// output: handlePutResourceProfile, handlePatchResourceProfile, handleDeleteResourceProfile
// pos: HTTP handlers for PUT/PATCH/DELETE /resources/{id}/profile
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"

	"github.com/fan/controlhub/internal/service"
)

func handlePutResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing resource id", http.StatusBadRequest)
			return
		}

		var fields map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := profileService.PutProfile(r.Context(), id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePatchResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing resource id", http.StatusBadRequest)
			return
		}

		var fields map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := profileService.PatchProfile(r.Context(), id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing resource id", http.StatusBadRequest)
			return
		}

		if err := profileService.DeleteProfile(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
