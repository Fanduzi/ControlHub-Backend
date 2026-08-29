// Package service provides resource mutation preview contracts.
// input: standard-library hashing, JSON, reflection, sorting, and formatting packages
// output: pure bulk resource mutation preview request, result, validation, and fingerprinting
// pos: service-layer preview contract with no persistence dependency
// note: if this file changes, update this header and module README.md.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// LabelOperations applies adds, updates, and removals to a resource's labels.
type LabelOperations struct {
	Add    map[string]string `json:"add,omitempty"`
	Update map[string]string `json:"update,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

type BulkResourceMutationTarget struct {
	ResourceID      uint64 `json:"resourceId"`
	ExpectedVersion string `json:"expectedVersion"`
}

// BulkResourceMutationRequest describes one bulk update to preview.
type BulkResourceMutationRequest struct {
	Targets    []BulkResourceMutationTarget `json:"targets"`
	FieldPatch map[string]any               `json:"fieldPatch,omitempty"`
	Labels     LabelOperations              `json:"labels,omitempty"`
}

// ResourceMutationSnapshot is the current state used to calculate a preview.
type ResourceMutationSnapshot struct {
	ID      uint64            `json:"id"`
	Version string            `json:"version"`
	Fields  map[string]any    `json:"fields,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type FieldDiff struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

type LabelDiff struct {
	Key    string  `json:"key"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

type BulkResourcePreviewItem struct {
	ResourceID uint64      `json:"resourceId"`
	Conflict   bool        `json:"conflict"`
	FieldDiffs []FieldDiff `json:"fieldDiffs,omitempty"`
	LabelDiffs []LabelDiff `json:"labelDiffs,omitempty"`
	Errors     []string    `json:"errors,omitempty"`
}

type BulkResourcePreview struct {
	Items       []BulkResourcePreviewItem `json:"items"`
	Fingerprint string                    `json:"fingerprint"`
	Confirmable bool                      `json:"confirmable"`
}

// PreviewBulkResourceMutation calculates a mutation preview without changing its inputs.
func PreviewBulkResourceMutation(request BulkResourceMutationRequest, snapshots []ResourceMutationSnapshot) (BulkResourcePreview, error) {
	if err := ValidateBulkResourceMutation(request); err != nil {
		return BulkResourcePreview{}, err
	}

	byID := make(map[uint64]ResourceMutationSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byID[snapshot.ID] = snapshot
	}
	preview := BulkResourcePreview{Items: make([]BulkResourcePreviewItem, 0, len(request.Targets))}
	for _, target := range request.Targets {
		item := BulkResourcePreviewItem{ResourceID: target.ResourceID}
		snapshot, ok := byID[target.ResourceID]
		if !ok {
			item.Errors = []string{"current snapshot is missing"}
			preview.Items = append(preview.Items, item)
			continue
		}
		if snapshot.Version != target.ExpectedVersion {
			item.Conflict = true
			item.Errors = append(item.Errors, "expected version does not match current version")
		}
		item.FieldDiffs = fieldDiffs(snapshot.Fields, request.FieldPatch)
		item.LabelDiffs, item.Errors = labelDiffs(snapshot.Labels, request.Labels, item.Errors)
		preview.Items = append(preview.Items, item)
	}
	fingerprint, err := previewFingerprint(request, snapshots)
	if err != nil {
		return BulkResourcePreview{}, err
	}
	preview.Fingerprint = fingerprint
	preview.Confirmable = true
	for _, item := range preview.Items {
		if item.Conflict || len(item.Errors) != 0 {
			preview.Confirmable = false
			break
		}
	}
	return preview, nil
}

// ValidateBulkResourceMutation checks request-wide invariants before a repository reads targets.
func ValidateBulkResourceMutation(request BulkResourceMutationRequest) error {
	if len(request.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	ids := make(map[uint64]struct{}, len(request.Targets))
	for _, target := range request.Targets {
		if target.ResourceID == 0 {
			return fmt.Errorf("resource ID must not be zero")
		}
		if _, ok := ids[target.ResourceID]; ok {
			return fmt.Errorf("duplicate resource ID %d", target.ResourceID)
		}
		ids[target.ResourceID] = struct{}{}
		if target.ExpectedVersion == "" {
			return fmt.Errorf("expected version is required for resource %d", target.ResourceID)
		}
	}
	ops := make(map[string]string, len(request.Labels.Add)+len(request.Labels.Update)+len(request.Labels.Remove))
	for operation, labels := range map[string]map[string]string{"add": request.Labels.Add, "update": request.Labels.Update} {
		for key := range labels {
			if previous, exists := ops[key]; exists {
				return fmt.Errorf("label %q appears in both %s and %s", key, previous, operation)
			}
			ops[key] = operation
		}
	}
	for _, key := range request.Labels.Remove {
		if previous, exists := ops[key]; exists {
			return fmt.Errorf("label %q appears in both %s and remove", key, previous)
		}
		ops[key] = "remove"
	}
	return nil
}

func fieldDiffs(before, patch map[string]any) []FieldDiff {
	keys := sortedAnyKeys(patch)
	diffs := make([]FieldDiff, 0, len(keys))
	for _, field := range keys {
		if !reflect.DeepEqual(before[field], patch[field]) {
			diffs = append(diffs, FieldDiff{Field: field, Before: before[field], After: patch[field]})
		}
	}
	return diffs
}

func labelDiffs(before map[string]string, operations LabelOperations, errors []string) ([]LabelDiff, []string) {
	labels := clonePreviewLabels(before)
	var diffs []LabelDiff
	for _, key := range sortedStringKeys(operations.Add) {
		if _, exists := labels[key]; exists {
			errors = append(errors, fmt.Sprintf("label %q already exists", key))
			continue
		}
		value := operations.Add[key]
		labels[key] = value
		diffs = append(diffs, LabelDiff{Key: key, After: &value})
	}
	for _, key := range sortedStringKeys(operations.Update) {
		old, exists := labels[key]
		if !exists {
			errors = append(errors, fmt.Sprintf("label %q does not exist", key))
			continue
		}
		value := operations.Update[key]
		labels[key] = value
		if old != value {
			diffs = append(diffs, LabelDiff{Key: key, Before: &old, After: &value})
		}
	}
	for _, key := range sortedStrings(operations.Remove) {
		old, exists := labels[key]
		if !exists {
			errors = append(errors, fmt.Sprintf("label %q does not exist", key))
			continue
		}
		delete(labels, key)
		diffs = append(diffs, LabelDiff{Key: key, Before: &old})
	}
	return diffs, errors
}

func previewFingerprint(request BulkResourceMutationRequest, snapshots []ResourceMutationSnapshot) (string, error) {
	cloned := append([]ResourceMutationSnapshot(nil), snapshots...)
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].ID < cloned[j].ID })
	encoded, err := json.Marshal(struct {
		Request   BulkResourceMutationRequest `json:"request"`
		Snapshots []ResourceMutationSnapshot  `json:"snapshots"`
	}{request, cloned})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func clonePreviewLabels(labels map[string]string) map[string]string {
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func sortedAnyKeys(values map[string]any) []string { return sortedStringKeys(values) }

func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrings(values []string) []string {
	cloned := append([]string(nil), values...)
	sort.Strings(cloned)
	return cloned
}
