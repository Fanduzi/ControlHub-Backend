//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func TestInspector_GetTableDefinition_BaseTable(t *testing.T) {
	// Given
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	resp, err := svc.GetTableDefinition(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent")

	// Then
	if err != nil {
		t.Fatalf("GetTableDefinition: %v", err)
	}
	if resp.Kind != model.ObjectKindTable {
		t.Fatalf("kind = %q, want table", resp.Kind)
	}
	if resp.Dialect != "mysql" {
		t.Fatalf("dialect = %q, want mysql", resp.Dialect)
	}
	if resp.Definition == "" {
		t.Fatal("definition must not be empty")
	}
	if !strings.Contains(resp.Definition, "CREATE TABLE") {
		t.Fatal("definition missing CREATE TABLE")
	}
	if !strings.Contains(resp.Definition, "schema_parent") {
		t.Fatal("definition missing table name")
	}
	if !strings.Contains(resp.Definition, "PRIMARY KEY") {
		t.Fatal("definition missing PRIMARY KEY")
	}
	if resp.Truncated {
		t.Fatal("truncated = true for normal table, want false")
	}
}

func TestInspector_GetTableDefinition_ViewRejected(t *testing.T) {
	// Given
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	_, err := svc.GetTableDefinition(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent_summary")

	// Then
	if !errors.Is(err, service.ErrSchemaDefinitionNotSupported) {
		t.Fatalf("view error = %v, want ErrSchemaDefinitionNotSupported", err)
	}
}

func TestInspector_GetTableDefinition_MissingTableRejected(t *testing.T) {
	// Given
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	_, err := svc.GetTableDefinition(ctx, ownerDBA, targetID, "query_e2e_aux", "nonexistent_table_xyz")

	// Then
	if !errors.Is(err, service.ErrSchemaObjectNotFound) {
		t.Fatalf("missing table error = %v, want ErrSchemaObjectNotFound", err)
	}
}

func TestInspector_GetTableDefinition_AuditFixedEvent(t *testing.T) {
	// Given
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	if _, err := svc.GetTableDefinition(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent"); err != nil {
		t.Fatalf("GetTableDefinition: %v", err)
	}

	// Then
	rows, err := db.Query(`SELECT event_type, result FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.schema.table_definition.read' ORDER BY id DESC LIMIT 1`, targetID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected audit event for table definition read")
	}
	var eventType, result string
	if err := rows.Scan(&eventType, &result); err != nil {
		t.Fatalf("scan audit: %v", err)
	}
	if eventType != "query.schema.table_definition.read" {
		t.Fatalf("audit event type = %q, want %q", eventType, "query.schema.table_definition.read")
	}
	if result != "success" {
		t.Fatalf("audit result = %q, want success", result)
	}
}

func TestInspector_GetTableDefinition_NoDefinitionInAuditOrHistory(t *testing.T) {
	// Given
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	// When
	if _, err := svc.GetTableDefinition(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent"); err != nil {
		t.Fatalf("GetTableDefinition: %v", err)
	}

	rows, err := db.Query(`SELECT event_type, result FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.schema.table_definition.read'`, targetID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType, result string
		if err := rows.Scan(&eventType, &result); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if strings.Contains(eventType, "CREATE TABLE") || strings.Contains(result, "CREATE TABLE") {
			t.Fatal("audit row contains definition fragment")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}

	qeRows, err := db.Query(`SELECT engine, statement_digest, statement_preview, status, error_code, error_message FROM query_executions WHERE target_resource_id = ?`, targetID)
	if err != nil {
		t.Fatalf("query executions: %v", err)
	}
	defer qeRows.Close()
	for qeRows.Next() {
		var values [6]sql.NullString
		if err := qeRows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5]); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, value := range values {
			if value.Valid && strings.Contains(value.String, "CREATE TABLE") {
				t.Fatal("query_executions contains definition")
			}
		}
	}
	if err := qeRows.Err(); err != nil {
		t.Fatalf("iterate query executions: %v", err)
	}
}
