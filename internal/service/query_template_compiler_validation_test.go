package service

import (
	"strings"
	"testing"
)

func TestTemplateStatementCompilerValidateCompilesDeclarationsWithoutRuntimeValues(t *testing.T) {
	// Given: a saved statement declaration, which intentionally has no execution values.
	compiler := NewTemplateStatementCompiler()

	// When: the save path validates its declaration.
	compiled, err := compiler.validateDeclarations("select id from orders where status = :status and enabled = :enabled", []TemplateParameterDefinition{
		{Name: "status", Type: TemplateParameterString},
		{Name: "enabled", Type: TemplateParameterBoolean},
	})

	// Then: declaration validation succeeds and returns placeholder-safe SQL for guarding.
	if err != nil {
		t.Fatalf("validateDeclarations: %v", err)
	}
	if got, want := strings.Count(compiled.Statement, "?"), 2; got != want {
		t.Fatalf("compiled placeholder count = %d, want %d in %q", got, want, compiled.Statement)
	}
}
