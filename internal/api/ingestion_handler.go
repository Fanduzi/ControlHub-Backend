// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: multipart uploads, authenticated actor context, and internal/service ingestion seams
// output: admin ingestion preview and atomic confirmation responses
// pos: Bounded HTTP transport for issue #83 CSV/JSON ingestion
// note: if this file changes, update this header and module README.md.
package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fan/controlhub/internal/service"
)

const maxIngestionMultipartBytes = service.MaxIngestionBytes + (1 << 20)

func handlePreviewIngestion(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, _, err := parseIngestionUpload(w, r, false)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		preview, err := resourceService.PreviewIngestion(r.Context(), rows)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	}
}

func handleConfirmIngestion(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, fingerprint, err := parseIngestionUpload(w, r, true)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		preview, err := resourceService.ConfirmIngestion(r.Context(), actorUserID, rows, fingerprint)
		if errors.Is(err, service.ErrIngestionConflict) {
			writeJSON(w, http.StatusConflict, ingestionConfirmError{"ingestion_conflict", "ingestion preview has conflicts", preview})
			return
		}
		if errors.Is(err, service.ErrIngestionFingerprintMismatch) {
			writeJSON(w, http.StatusConflict, ingestionConfirmError{"ingestion_preview_stale", "ingestion preview is stale; preview again", preview})
			return
		}
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	}
}

type ingestionConfirmError struct {
	Error   string                    `json:"error"`
	Message string                    `json:"message"`
	Preview *service.IngestionPreview `json:"preview,omitempty"`
}

func parseIngestionUpload(w http.ResponseWriter, r *http.Request, confirm bool) ([]service.IngestionRow, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIngestionMultipartBytes)
	if err := r.ParseMultipartForm(service.MaxIngestionBytes); err != nil {
		return nil, "", errors.New("ingestion request must be a bounded multipart form")
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		return nil, "", errors.New("ingestion request requires exactly one file")
	}
	allowed := map[string]bool{"format": true}
	if confirm {
		allowed["fingerprint"] = true
	}
	for name, values := range r.MultipartForm.Value {
		if !allowed[name] || len(values) != 1 {
			return nil, "", errors.New("ingestion form fields are invalid")
		}
	}
	format := r.FormValue("format")
	if format == "" {
		return nil, "", errors.New("ingestion format is required")
	}
	fingerprint := r.FormValue("fingerprint")
	if confirm && strings.TrimSpace(fingerprint) == "" {
		return nil, "", errors.New("reviewed fingerprint is required")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "", errors.New("ingestion file is required")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, service.MaxIngestionBytes+1))
	if err != nil || len(payload) > service.MaxIngestionBytes {
		return nil, "", errors.New("ingestion payload size is invalid")
	}
	rows, err := service.ParseIngestion(format, payload)
	if err != nil {
		return nil, "", fmt.Errorf("invalid ingestion: %w", err)
	}
	return rows, fingerprint, nil
}
