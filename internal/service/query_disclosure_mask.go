// Package service provides server-side query result disclosure controls.
package service

import "github.com/fan/controlhub/internal/model"

const maskedReplacement = "[MASKED]"

// applyDisclosureMask replaces non-null masked values without changing other modes.
func applyDisclosureMask(value any, mode model.ResultDisclosureMode) any {
	if mode == model.ResultDisclosureMaskedNoCopy {
		if value == nil {
			return nil
		}
		return maskedReplacement
	}
	return value
}
