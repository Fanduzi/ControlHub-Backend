package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestDecodeSavedStatementJSONBodyRejectsDuplicateFieldsAtAnyDepth(t *testing.T) {
	// Given: a declaration request with a duplicate nested parameter field.
	request := httptest.NewRequest("POST", "/query-targets/22/saved-statements", strings.NewReader(`{"name":"Test","statement":"SELECT 1 WHERE status = :status","scope":"personal","parameters":[{"name":"status","type":"string","type":"integer"}]}`))

	// When: the handler decodes the body.
	var decoded model.QuerySavedStatementCreateRequest
	err := decodeSavedStatementJSONBody(request, &decoded)

	// Then: duplicate rejection remains independent of typed decoding.
	if err == nil {
		t.Fatal("expected duplicate field error")
	}
}
