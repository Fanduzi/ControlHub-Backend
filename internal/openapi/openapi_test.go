package openapi_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/fan/controlhub/internal/openapi"
)

func TestOpenAPIYAMLIsValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml validation failed: %v", err)
	}
}
