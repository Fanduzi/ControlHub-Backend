// Package service tests resource mutation preview contracts.
// input: testing package and the service preview contract
// output: runnable coverage for bulk preview semantics and fingerprints
// pos: service-layer contract verification
// note: if this file changes, update this header and module README.md.
package service

import "testing"

func TestBulkResourcePreviewLabelSemantics(t *testing.T) {
	preview, err := PreviewBulkResourceMutation(BulkResourceMutationRequest{
		Targets: []BulkResourceMutationTarget{{ResourceID: 1, ExpectedVersion: "v1"}},
		Labels: LabelOperations{
			Add:    map[string]string{"new": "yes"},
			Update: map[string]string{"old": "updated"},
			Remove: []string{"gone"},
		},
	}, []ResourceMutationSnapshot{{ID: 1, Version: "v1", Labels: map[string]string{"old": "before", "gone": "remove"}}})
	if err != nil || len(preview.Items[0].Errors) != 0 || len(preview.Items[0].LabelDiffs) != 3 {
		t.Fatalf("preview = %#v, err = %v", preview, err)
	}
}

func TestBulkResourcePreviewRejectsDuplicateAndOverlappingInputs(t *testing.T) {
	_, err := PreviewBulkResourceMutation(BulkResourceMutationRequest{}, nil)
	if err == nil {
		t.Fatal("expected empty target error")
	}
	_, err = PreviewBulkResourceMutation(BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 0}}}, nil)
	if err == nil {
		t.Fatal("expected zero ID error")
	}
	_, err = PreviewBulkResourceMutation(BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 1}, {ResourceID: 1}}}, nil)
	if err == nil {
		t.Fatal("expected duplicate ID error")
	}
	_, err = PreviewBulkResourceMutation(BulkResourceMutationRequest{Labels: LabelOperations{Add: map[string]string{"x": "1"}, Remove: []string{"x"}}}, nil)
	if err == nil {
		t.Fatal("expected overlapping label operation error")
	}
	_, err = PreviewBulkResourceMutation(BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 1}}, Labels: LabelOperations{Remove: []string{"x", "x"}}}, nil)
	if err == nil {
		t.Fatal("expected duplicate remove error")
	}
}

func TestBulkResourcePreviewIsSideEffectFree(t *testing.T) {
	request := BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 1, ExpectedVersion: "v1"}}, FieldPatch: map[string]any{"name": "after"}, Labels: LabelOperations{Add: map[string]string{"new": "yes"}}}
	snapshots := []ResourceMutationSnapshot{{ID: 1, Version: "v1", Fields: map[string]any{"name": "before"}, Labels: map[string]string{"old": "keep"}}}
	_, err := PreviewBulkResourceMutation(request, snapshots)
	if err != nil || snapshots[0].Fields["name"] != "before" || len(snapshots[0].Labels) != 1 || request.Labels.Add["new"] != "yes" {
		t.Fatalf("inputs changed or preview failed: snapshots=%#v request=%#v err=%v", snapshots, request, err)
	}
}

func TestBulkResourcePreviewFingerprintDetectsDrift(t *testing.T) {
	request := BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 1, ExpectedVersion: "v1"}}, FieldPatch: map[string]any{"name": "after"}}
	snapshot := ResourceMutationSnapshot{ID: 1, Version: "v1", Fields: map[string]any{"name": "before"}}
	first, err := PreviewBulkResourceMutation(request, []ResourceMutationSnapshot{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := PreviewBulkResourceMutation(request, []ResourceMutationSnapshot{snapshot})
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("fingerprint is not stable")
	}
	versionDrift := snapshot
	versionDrift.Version = "v2"
	contentDrift := snapshot
	contentDrift.Fields = map[string]any{"name": "changed"}
	requestDrift := request
	requestDrift.FieldPatch = map[string]any{"name": "different"}
	for _, drift := range []struct {
		request  BulkResourceMutationRequest
		snapshot ResourceMutationSnapshot
	}{{request, versionDrift}, {request, contentDrift}, {requestDrift, snapshot}} {
		preview, err := PreviewBulkResourceMutation(drift.request, []ResourceMutationSnapshot{drift.snapshot})
		if err != nil || preview.Fingerprint == first.Fingerprint {
			t.Fatalf("drift was not fingerprinted: preview=%#v err=%v", preview, err)
		}
	}
}

func TestBulkResourcePreviewPreservesTargetOrderAndComparesCompositeFields(t *testing.T) {
	request := BulkResourceMutationRequest{Targets: []BulkResourceMutationTarget{{ResourceID: 2, ExpectedVersion: "v2"}, {ResourceID: 1, ExpectedVersion: "v0"}}, FieldPatch: map[string]any{"metadata": map[string]any{"tags": []string{"a"}}}}
	snapshots := []ResourceMutationSnapshot{{ID: 1, Version: "v1", Fields: map[string]any{"metadata": map[string]any{"tags": []string{"a"}}}}, {ID: 2, Version: "v2"}}
	preview, err := PreviewBulkResourceMutation(request, snapshots)
	if err != nil || preview.Items[0].ResourceID != 2 || preview.Items[0].Conflict || preview.Items[1].ResourceID != 1 || !preview.Items[1].Conflict || len(preview.Items[1].FieldDiffs) != 0 {
		t.Fatalf("preview = %#v, err = %v", preview, err)
	}
}
