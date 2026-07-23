// Package service provides tests for server-side query result disclosure masking.
package service

import (
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestApplyDisclosureMask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		mode  model.ResultDisclosureMode
		want  any
	}{
		{name: "masked nil remains nil", value: nil, mode: model.ResultDisclosureMaskedNoCopy, want: nil},
		{name: "masked string is replaced", value: "sensitive", mode: model.ResultDisclosureMaskedNoCopy, want: maskedReplacement},
		{name: "masked integer is replaced", value: 42, mode: model.ResultDisclosureMaskedNoCopy, want: maskedReplacement},
		{name: "masked boolean is replaced", value: true, mode: model.ResultDisclosureMaskedNoCopy, want: maskedReplacement},
		{name: "raw string is preserved", value: "value", mode: model.ResultDisclosureRawCopyAllowed, want: "value"},
		{name: "raw nil is preserved", value: nil, mode: model.ResultDisclosureRawCopyAllowed, want: nil},
		{name: "blocked value is preserved", value: "unreachable", mode: model.ResultDisclosureBlocked, want: "unreachable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Given: a scanned value and its server-owned disclosure mode.
			// When: disclosure masking is applied before serialization.
			got := applyDisclosureMask(tt.value, tt.mode)
			// Then: only non-null masked values are redacted.
			if got != tt.want {
				t.Fatalf("applyDisclosureMask(%#v, %q) = %#v, want %#v", tt.value, tt.mode, got, tt.want)
			}
		})
	}
}
