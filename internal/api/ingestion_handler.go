// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: multipart uploads, authenticated User/machine context, internal/model scan values, and internal/service ingestion seams
// output: non-empty admin User previews, collector previews including canonical empty fingerprints, and confirmation responses with controlled stale/scan-conflict mappings
// pos: Bounded HTTP transport for issue #83 CSV/JSON ingestion and issue #87 reachable collector preview/confirm protocol
// note: if this file changes, update this header and module README.md.
package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

const maxIngestionMultipartBytes = service.MaxIngestionBytes + (1 << 20)

func handlePreviewIngestion(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, _, _, err := parseIngestionUpload(w, r, false, false)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var preview *service.IngestionPreview
		if _, collector := machinePrincipalFromContext(r.Context()); collector {
			preview, err = resourceService.PreviewCollectorIngestion(r.Context(), rows)
		} else {
			preview, err = resourceService.PreviewIngestion(r.Context(), rows)
		}
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	}
}

func handleConfirmIngestion(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machine, collector := machinePrincipalFromContext(r.Context())
		rows, fingerprint, metadata, err := parseIngestionUpload(w, r, true, collector)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var preview *service.IngestionPreview
		if collector {
			preview, err = resourceService.ConfirmCollectorIngestion(r.Context(), machine.ID, rows, fingerprint, metadata)
		} else {
			actorUserID, ok := actorUserIDFromContext(r.Context())
			if !ok {
				writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
				return
			}
			preview, err = resourceService.ConfirmIngestion(r.Context(), actorUserID, rows, fingerprint)
		}
		if errors.Is(err, service.ErrIngestionConflict) {
			writeJSON(w, http.StatusConflict, ingestionConfirmError{"ingestion_conflict", "ingestion preview has conflicts", preview})
			return
		}
		if errors.Is(err, service.ErrIngestionFingerprintMismatch) {
			writeJSON(w, http.StatusConflict, ingestionConfirmError{"ingestion_preview_stale", "ingestion preview is stale; preview again", preview})
			return
		}
		if errors.Is(err, service.ErrCollectorScanConflict) {
			writeJSON(w, http.StatusConflict, ingestionConfirmError{"collector_scan_conflict", "collector scan ID was already used with different content", preview})
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

func parseIngestionUpload(w http.ResponseWriter, r *http.Request, confirm, collector bool) ([]service.IngestionRow, string, service.CollectorIngestionMetadata, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIngestionMultipartBytes)
	if err := r.ParseMultipartForm(service.MaxIngestionBytes); err != nil {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion request must be a bounded multipart form")
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion request requires exactly one file")
	}
	allowed := map[string]bool{"format": true}
	if confirm {
		allowed["fingerprint"] = true
	}
	if collector {
		allowed["collectorScanId"] = true
		allowed["collectorScanResult"] = true
	}
	for name, values := range r.MultipartForm.Value {
		if !allowed[name] || len(values) != 1 {
			return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion form fields are invalid")
		}
	}
	format := r.FormValue("format")
	if format == "" {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion format is required")
	}
	fingerprint := r.FormValue("fingerprint")
	if confirm && strings.TrimSpace(fingerprint) == "" {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("reviewed fingerprint is required")
	}
	metadata := service.CollectorIngestionMetadata{}
	if collector {
		metadata.ScanID = strings.TrimSpace(r.FormValue("collectorScanId"))
		metadata.ScanResult = model.CollectorScanResult(strings.TrimSpace(r.FormValue("collectorScanResult")))
		if len(r.MultipartForm.Value["collectorScanId"]) != 1 || len(r.MultipartForm.Value["collectorScanResult"]) != 1 || service.ValidateCollectorIngestionMetadata(metadata) != nil {
			return nil, "", service.CollectorIngestionMetadata{}, errors.New("collector scan metadata is invalid")
		}
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion file is required")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, service.MaxIngestionBytes+1))
	if err != nil || len(payload) > service.MaxIngestionBytes {
		return nil, "", service.CollectorIngestionMetadata{}, errors.New("ingestion payload size is invalid")
	}
	rows, err := service.ParseIngestion(format, payload)
	if err != nil {
		return nil, "", service.CollectorIngestionMetadata{}, fmt.Errorf("invalid ingestion: %w", err)
	}
	return rows, fingerprint, metadata, nil
}
