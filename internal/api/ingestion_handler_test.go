// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http/httptest multipart requests and the API test server
// output: ingestion preview and confirm HTTP contract tests
// pos: Verifies the admin-only ingestion upload boundary and controlled error mapping
// note: if this file changes, update this header and module README.md.
package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIngestionPreviewAndConfirmHTTP(t *testing.T) {
	server := NewTestServer()
	payload := `[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`

	preview := ingestRequest(t, server.Router, "/admin/ingestions/preview", "json", payload, "")
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200: %s", preview.Code, preview.Body.String())
	}
	var reviewed struct {
		Confirmable bool   `json:"confirmable"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&reviewed); err != nil {
		t.Fatal(err)
	}
	if !reviewed.Confirmable || reviewed.Fingerprint == "" {
		t.Fatalf("preview = %+v, want confirmable fingerprint", reviewed)
	}

	confirmed := ingestRequest(t, server.Router, "/admin/ingestions/confirm", "json", payload, reviewed.Fingerprint)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200: %s", confirmed.Code, confirmed.Body.String())
	}
}

func TestIngestionHTTPControlledErrors(t *testing.T) {
	server := NewTestServer()
	for name, tc := range map[string]struct {
		path, format, payload, fingerprint string
		want                               int
		code                               string
	}{
		"invalid format": {"/admin/ingestions/preview", "xml", `[]`, "", http.StatusBadRequest, "validation_failed"},
		"validation":     {"/admin/ingestions/preview", "json", `[{"environmentId":0}]`, "", http.StatusBadRequest, "validation_failed"},
		"stale":          {"/admin/ingestions/confirm", "json", `[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`, "stale", http.StatusConflict, "ingestion_preview_stale"},
		"conflict":       {"/admin/ingestions/confirm", "json", `[{"environmentId":1,"ciType":"host","name":"forced-conflict"}]`, "f2f791ab271177970fa0cb0205a6f63d56b2ea27ba4e40dd91c839235a4348c0", http.StatusConflict, "ingestion_conflict"},
	} {
		t.Run(name, func(t *testing.T) {
			response := ingestRequest(t, server.Router, tc.path, tc.format, tc.payload, tc.fingerprint)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			var got errorResponse
			if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Error != tc.code {
				t.Fatalf("error = %q, want %q", got.Error, tc.code)
			}
		})
	}
}

func ingestRequest(t *testing.T, router http.Handler, path, format, payload, fingerprint string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("format", format); err != nil {
		t.Fatal(err)
	}
	if fingerprint != "" {
		if err := form.WriteField("fingerprint", fingerprint); err != nil {
			t.Fatal(err)
		}
	}
	file, err := form.CreateFormFile("file", "ingestion."+format)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}
