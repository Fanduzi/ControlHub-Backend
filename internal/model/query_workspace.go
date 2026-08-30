// Package model provides domain entities for the resource management system.
// input: encoding/json, fmt, strings, time, unicode/utf8, MaxSavedStatementSize
// output: bounded QueryWorkspaceWorksheet, QueryWorkspace, and QueryWorkspacePutRequest contracts
// pos: Opaque one-row-per-owner query worksheet aggregate without target resolution or SQL guarding
// note: if this file changes, update this header and module README.md.
package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxQueryWorkspaceWorksheets          = 32
	MaxQueryWorkspaceWorksheetIDLength   = 128
	MaxQueryWorkspaceWorksheetNameLength = 120
	MaxQueryWorkspaceDatabaseNameLength  = 128
	MaxQueryWorkspaceJSONSize            = 256 * 1024
)

type QueryWorkspaceWorksheet struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	TargetResourceID uint64  `json:"targetResourceId"`
	Statement        string  `json:"statement"`
	ActiveDatabase   *string `json:"activeDatabase"`
}

type QueryWorkspace struct {
	OwnerUserID uint64                    `json:"-"`
	Worksheets  []QueryWorkspaceWorksheet `json:"worksheets"`
	Version     uint64                    `json:"version"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
}

type QueryWorkspacePutRequest struct {
	ExpectedVersion uint64                    `json:"expectedVersion"`
	Worksheets      []QueryWorkspaceWorksheet `json:"worksheets"`
}

// Validate bounds only the persisted workspace shape. Statement text remains
// opaque: empty, incomplete, invalid, and non-SELECT SQL are all valid here.
func (r QueryWorkspacePutRequest) Validate() error {
	if len(r.Worksheets) > MaxQueryWorkspaceWorksheets {
		return fmt.Errorf("worksheets exceed %d items", MaxQueryWorkspaceWorksheets)
	}
	seen := make(map[string]struct{}, len(r.Worksheets))
	for _, worksheet := range r.Worksheets {
		if strings.TrimSpace(worksheet.ID) == "" || utf8.RuneCountInString(worksheet.ID) > MaxQueryWorkspaceWorksheetIDLength {
			return fmt.Errorf("worksheet id must contain 1 to %d characters", MaxQueryWorkspaceWorksheetIDLength)
		}
		if _, exists := seen[worksheet.ID]; exists {
			return fmt.Errorf("worksheet ids must be unique")
		}
		seen[worksheet.ID] = struct{}{}
		if strings.TrimSpace(worksheet.Name) == "" || utf8.RuneCountInString(worksheet.Name) > MaxQueryWorkspaceWorksheetNameLength {
			return fmt.Errorf("worksheet name must contain 1 to %d characters", MaxQueryWorkspaceWorksheetNameLength)
		}
		if worksheet.TargetResourceID == 0 {
			return fmt.Errorf("worksheet targetResourceId must be positive")
		}
		if len(worksheet.Statement) > MaxSavedStatementSize {
			return fmt.Errorf("worksheet statement exceeds %d bytes", MaxSavedStatementSize)
		}
		if worksheet.ActiveDatabase != nil && (strings.TrimSpace(*worksheet.ActiveDatabase) == "" || utf8.RuneCountInString(*worksheet.ActiveDatabase) > MaxQueryWorkspaceDatabaseNameLength) {
			return fmt.Errorf("worksheet activeDatabase must contain 1 to %d characters", MaxQueryWorkspaceDatabaseNameLength)
		}
	}
	raw, err := json.Marshal(r.Worksheets)
	if err != nil {
		return fmt.Errorf("marshal query workspace: %w", err)
	}
	if len(raw) > MaxQueryWorkspaceJSONSize {
		return fmt.Errorf("query workspace exceeds %d bytes", MaxQueryWorkspaceJSONSize)
	}
	return nil
}
