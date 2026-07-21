//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// TestRelationshipMap_SchemaParentInbound verifies that rooting on schema_parent
// returns an inbound edge from schema_child (FK: schema_child.parent_id →
// schema_parent.id).
func TestRelationshipMap_SchemaParentInbound(t *testing.T) {
	// Given
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	resp, err := svc.GetRelationshipMap(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", false)

	// Then
	if err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if resp.Root.Name != "schema_parent" {
		t.Fatalf("root name = %q, want schema_parent", resp.Root.Name)
	}
	if resp.Root.Role != model.RelationshipMapRoleRoot {
		t.Fatalf("root role = %q, want root", resp.Root.Role)
	}
	if resp.TargetResourceID != int64(targetID) {
		t.Fatalf("targetResourceId = %d, want %d", resp.TargetResourceID, targetID)
	}

	// Find the inbound edge from schema_child.
	var foundInbound bool
	for _, e := range resp.Edges {
		if e.Direction == model.RelationshipMapDirectionInbound {
			foundInbound = true
			if e.TargetID != resp.Root.ID {
				t.Fatalf("inbound edge targetID = %q, want root ID %q", e.TargetID, resp.Root.ID)
			}
			if len(e.Columns) != 1 || e.Columns[0] != "parent_id" {
				t.Fatalf("inbound edge columns = %v, want [parent_id]", e.Columns)
			}
			if len(e.ReferencedColumns) != 1 || e.ReferencedColumns[0] != "id" {
				t.Fatalf("inbound edge referencedColumns = %v, want [id]", e.ReferencedColumns)
			}
			break
		}
	}
	if !foundInbound {
		t.Fatal("expected inbound edge from schema_child to schema_parent")
	}

	// The related node should be schema_child.
	var relatedNode *model.RelationshipMapNode
	for i, n := range resp.Nodes {
		if n.Role == model.RelationshipMapRoleRelated {
			relatedNode = &resp.Nodes[i]
			break
		}
	}
	if relatedNode == nil {
		t.Fatal("expected a related node for schema_child")
	}
	if relatedNode.Name != "schema_child" {
		t.Fatalf("related node name = %q, want schema_child", relatedNode.Name)
	}
}

// TestRelationshipMap_SchemaChildOutbound verifies that rooting on schema_child
// returns an outbound edge to schema_parent (FK: schema_child.parent_id →
// schema_parent.id).
func TestRelationshipMap_SchemaChildOutbound(t *testing.T) {
	// Given
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	resp, err := svc.GetRelationshipMap(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_child", false)

	// Then
	if err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if resp.Root.Name != "schema_child" {
		t.Fatalf("root name = %q, want schema_child", resp.Root.Name)
	}
	if resp.Root.Role != model.RelationshipMapRoleRoot {
		t.Fatalf("root role = %q, want root", resp.Root.Role)
	}

	// Find the outbound edge to schema_parent.
	var foundOutbound bool
	for _, e := range resp.Edges {
		if e.Direction == model.RelationshipMapDirectionOutbound {
			foundOutbound = true
			if e.SourceID != resp.Root.ID {
				t.Fatalf("outbound edge sourceID = %q, want root ID %q", e.SourceID, resp.Root.ID)
			}
			if len(e.Columns) != 1 || e.Columns[0] != "parent_id" {
				t.Fatalf("outbound edge columns = %v, want [parent_id]", e.Columns)
			}
			if len(e.ReferencedColumns) != 1 || e.ReferencedColumns[0] != "id" {
				t.Fatalf("outbound edge referencedColumns = %v, want [id]", e.ReferencedColumns)
			}
			if e.OnUpdate != "CASCADE" {
				t.Fatalf("outbound edge onUpdate = %q, want CASCADE", e.OnUpdate)
			}
			if e.OnDelete != "RESTRICT" {
				t.Fatalf("outbound edge onDelete = %q, want RESTRICT", e.OnDelete)
			}
			break
		}
	}
	if !foundOutbound {
		t.Fatal("expected outbound edge from schema_child to schema_parent")
	}

	// The related node should be schema_parent.
	var relatedNode *model.RelationshipMapNode
	for i, n := range resp.Nodes {
		if n.Role == model.RelationshipMapRoleRelated {
			relatedNode = &resp.Nodes[i]
			break
		}
	}
	if relatedNode == nil {
		t.Fatal("expected a related node for schema_parent")
	}
	if relatedNode.Name != "schema_parent" {
		t.Fatalf("related node name = %q, want schema_parent", relatedNode.Name)
	}
}

// TestRelationshipMap_EmptyMap verifies that a table with no foreign-key
// relationships returns only the root node and zero edges.
func TestRelationshipMap_EmptyMap(t *testing.T) {
	// Given: query_e2e_items has no FK relationships.
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	resp, err := svc.GetRelationshipMap(ctx, ownerDBA, targetID, "query_e2e", "query_e2e_items", false)

	// Then
	if err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if resp.Root.Name != "query_e2e_items" {
		t.Fatalf("root name = %q, want query_e2e_items", resp.Root.Name)
	}
	if resp.Root.Role != model.RelationshipMapRoleRoot {
		t.Fatalf("root role = %q, want root", resp.Root.Role)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes count = %d, want 1 (root only)", len(resp.Nodes))
	}
	if len(resp.Edges) != 0 {
		t.Fatalf("edges count = %d, want 0 for table with no FKs", len(resp.Edges))
	}
	if resp.Truncated {
		t.Fatal("truncated = true for empty map, want false")
	}
}

// TestRelationshipMap_NoQueryExecutionsRow verifies that a relationship-map
// read does NOT create query_executions rows. It counts rows before and after
// the call and asserts they are equal.
func TestRelationshipMap_NoQueryExecutionsRow(t *testing.T) {
	// Given
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	var beforeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&beforeCount); err != nil {
		t.Fatalf("count query_executions before: %v", err)
	}

	// When
	if _, err := svc.GetRelationshipMap(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", false); err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}

	// Then: no new query_executions rows.
	var afterCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&afterCount); err != nil {
		t.Fatalf("count query_executions after: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("query_executions rows changed: before=%d, after=%d (relationship map must not create executions)", beforeCount, afterCount)
	}
}

// TestRelationshipMap_AuditEvent verifies that a successful relationship-map
// read creates exactly one audit_events row with event type
// "query.schema.relationship_map.read" and result "success".
func TestRelationshipMap_AuditEvent(t *testing.T) {
	// Given
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	if _, err := svc.GetRelationshipMap(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", false); err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}

	// Then: exactly one audit_events row with the expected event type.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.schema.relationship_map.read'`, targetID).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_events count = %d, want exactly 1", count)
	}

	// Verify the row content.
	rows, err := db.Query(`SELECT event_type, result FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.schema.relationship_map.read' ORDER BY id DESC LIMIT 1`, targetID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected audit event for relationship map read")
	}
	var eventType, result string
	if err := rows.Scan(&eventType, &result); err != nil {
		t.Fatalf("scan audit: %v", err)
	}
	if eventType != "query.schema.relationship_map.read" {
		t.Fatalf("audit event type = %q, want %q", eventType, "query.schema.relationship_map.read")
	}
	if result != "success" {
		t.Fatalf("audit result = %q, want success", result)
	}

	// Verify no DSN leakage in audit row.
	for _, val := range []string{eventType, result} {
		if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
			t.Fatalf("audit column %q looks like a DSN fragment", val)
		}
	}
}
