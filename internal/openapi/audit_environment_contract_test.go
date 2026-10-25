// Package openapi tests the published API contract.
package openapi

import (
	"bytes"
	"testing"
)

func TestAuditEventsEnvironmentIDParameterIsDocumented(t *testing.T) {
	want := []byte("name: environmentId\n          schema:\n            type: integer\n            format: int64\n            minimum: 1")
	if !bytes.Contains(YAML, want) {
		t.Fatal("GET /audit-events must document positive environmentId")
	}
}
