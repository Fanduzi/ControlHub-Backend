// Package openapi tests the published API contract.
// input: bytes, testing, embedded OpenAPI YAML
// output: audit environment-filter parameter contract test
// pos: Regression coverage for audit list query documentation
// note: if this file changes, update this header and README.md.
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
