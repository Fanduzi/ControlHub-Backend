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
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
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

func TestCollectorConfirmRequiresScanMetadata(t *testing.T) {
	server := NewTestServer()
	machine := &scopeMatrixMachineService{granted: model.MachineScopeInventoryIngest}
	server.deps.MachineCredentialService = machine
	rows, err := service.ParseIngestion("json", []byte(`[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`))
	if err != nil {
		t.Fatal(err)
	}

	response := ingestRequestWithAuthorization(t, NewRouter(server.deps), "/admin/ingestions/confirm", "json", `[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`, service.PreviewIngestion(rows, nil).Fingerprint, "Bearer chmp_route-test.secret")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestCollectorConfirmPropagatesNormalizedScanMetadata(t *testing.T) {
	server := NewTestServer()
	server.deps.MachineCredentialService = &scopeMatrixMachineService{granted: model.MachineScopeInventoryIngest}
	payload := `[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`
	rows, err := service.ParseIngestion("json", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}

	response := ingestRequestWithFields(t, NewRouter(server.deps), "/admin/ingestions/confirm", payload, []ingestionFormField{
		{"format", "json"},
		{"fingerprint", service.PreviewIngestion(rows, nil).Fingerprint},
		{"collectorScanId", " scan-123 "},
		{"collectorScanResult", " complete "},
	}, "Bearer chmp_route-test.secret")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if server.resourceRepo.collectorConfirmCalls != 1 || server.resourceRepo.collectorPrincipalID != 91 {
		t.Fatalf("collector calls/principal = %d/%d, want 1/91", server.resourceRepo.collectorConfirmCalls, server.resourceRepo.collectorPrincipalID)
	}
	if got, want := server.resourceRepo.collectorMetadata, (service.CollectorIngestionMetadata{ScanID: "scan-123", ScanResult: model.CollectorScanResultComplete}); got != want {
		t.Fatalf("collector metadata = %+v, want %+v", got, want)
	}
}

func TestIngestionScanFieldsStayMachineConfirmOnly(t *testing.T) {
	payload := `[{"environmentId":1,"ciType":"host","name":"ingested-host"}]`
	rows, err := service.ParseIngestion("json", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := service.PreviewIngestion(rows, nil).Fingerprint

	for name, tc := range map[string]struct {
		path, authorization string
		fields              []ingestionFormField
	}{
		"machine duplicate scan ID":     {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", "a"}, {"collectorScanId", "b"}, {"collectorScanResult", "complete"}}},
		"machine duplicate scan result": {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", "scan-1"}, {"collectorScanResult", "complete"}, {"collectorScanResult", "failed"}}},
		"machine missing scan result":   {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", "scan-1"}}},
		"machine blank scan ID":         {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", " "}, {"collectorScanResult", "complete"}}},
		"machine oversized scan ID":     {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", strings.Repeat("x", service.MaxCollectorScanIDBytes+1)}, {"collectorScanResult", "complete"}}},
		"machine invalid result":        {"/admin/ingestions/confirm", "Bearer chmp_route-test.secret", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", "scan-1"}, {"collectorScanResult", "unknown"}}},
		"User confirm scan fields":      {"/admin/ingestions/confirm", "", []ingestionFormField{{"format", "json"}, {"fingerprint", fingerprint}, {"collectorScanId", "scan-1"}, {"collectorScanResult", "complete"}}},
		"preview scan fields":           {"/admin/ingestions/preview", "", []ingestionFormField{{"format", "json"}, {"collectorScanId", "scan-1"}, {"collectorScanResult", "complete"}}},
	} {
		t.Run(name, func(t *testing.T) {
			server := NewTestServer()
			if tc.authorization != "" {
				server.deps.MachineCredentialService = &scopeMatrixMachineService{granted: model.MachineScopeInventoryIngest}
			}
			router := http.Handler(server.Router)
			if tc.authorization != "" {
				router = NewRouter(server.deps)
			}
			response := ingestRequestWithFields(t, router, tc.path, payload, tc.fields, tc.authorization)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}
}

func ingestRequest(t *testing.T, router http.Handler, path, format, payload, fingerprint string) *httptest.ResponseRecorder {
	return ingestRequestWithAuthorization(t, router, path, format, payload, fingerprint, "")
}

func ingestRequestWithAuthorization(t *testing.T, router http.Handler, path, format, payload, fingerprint, authorization string) *httptest.ResponseRecorder {
	fields := []ingestionFormField{{"format", format}}
	if fingerprint != "" {
		fields = append(fields, ingestionFormField{"fingerprint", fingerprint})
	}
	return ingestRequestWithFields(t, router, path, payload, fields, authorization)
}

type ingestionFormField struct{ name, value string }

func ingestRequestWithFields(t *testing.T, router http.Handler, path, payload string, fields []ingestionFormField, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range fields {
		if err := form.WriteField(field.name, field.value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := form.CreateFormFile("file", "ingestion.json")
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
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}
