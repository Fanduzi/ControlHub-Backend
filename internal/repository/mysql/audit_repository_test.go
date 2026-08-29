// Package mysql provides MySQL-backed repository implementations.
// input: database/sql/driver test doubles, internal/model, encoding/json, testing, time
// output: user/machine actor scan plus operator/resource/principal search SQL contract tests
// pos: Regression coverage for truthful MySQL audit actor projection and filtering
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestListAuditEvents_SearchAndScanMachinePrincipal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	const searchPredicate = `from audit_events .*left join users u on u\.id = audit_events\.actor_user_id .*left join machine_principals mp on mp\.id = audit_events\.actor_machine_principal_id .*left join resources r on r\.id = audit_events\.target_resource_id .*where \(u\.display_name like \? or u\.email like \? or mp\.name like \? or r\.name like \? or r\.display_name like \?\)`
	mock.ExpectQuery(`select count\(\*\) `+searchPredicate).
		WithArgs("%agent%", "%agent%", "%agent%", "%agent%", "%agent%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`select audit_events\.id, audit_events\.actor_user_id, audit_events\.actor_machine_principal_id, audit_events\.target_resource_id, audit_events\.event_type, audit_events\.result, audit_events\.changes, audit_events\.created_at `+searchPredicate+` order by`).
		WithArgs("%agent%", "%agent%", "%agent%", "%agent%", "%agent%", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "actor_user_id", "actor_machine_principal_id", "target_resource_id", "event_type", "result", "changes", "created_at"}).
			AddRow(1, nil, 91, 22, "query.executed", "success", nil, time.Date(2026, 8, 30, 6, 0, 0, 0, time.UTC)))

	items, total, err := NewAuditRepository(db).ListAuditEvents(context.Background(), model.AuditListQuery{
		Query:    "agent",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(items) != 1 || total != 1 {
		t.Fatalf("items=%d total=%d, want one row and total 1", len(items), total)
	}
	if items[0].ActorUserID != nil || items[0].ActorMachinePrincipalID == nil || *items[0].ActorMachinePrincipalID != 91 {
		t.Fatalf("audit identity = user:%v machine:%v, want machine principal 91 only", items[0].ActorUserID, items[0].ActorMachinePrincipalID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestNullableUint64ScanUint64StringAndNil(t *testing.T) {
	var value nullableUint64

	if err := value.Scan(uint64(18446744073709551615)); err != nil {
		t.Fatalf("scan uint64: %v", err)
	}
	if !value.Valid || value.Uint64 != 18446744073709551615 {
		t.Fatalf("expected max uint64 valid value, got valid=%v value=%d", value.Valid, value.Uint64)
	}

	if err := value.Scan("18446744073709551615"); err != nil {
		t.Fatalf("scan string: %v", err)
	}
	if !value.Valid || value.Uint64 != 18446744073709551615 {
		t.Fatalf("expected max uint64 from string, got valid=%v value=%d", value.Valid, value.Uint64)
	}

	if err := value.Scan(nil); err != nil {
		t.Fatalf("scan nil: %v", err)
	}
	if value.Valid {
		t.Fatal("expected null value to be invalid")
	}
}

func TestAuditEventJSON_NullTargetResourceID(t *testing.T) {
	actorID := uint64(2)
	event := model.AuditEvent{
		ID:          1,
		ActorUserID: &actorID,
		EventType:   "resource.updated",
		Result:      "success",
		CreatedAt:   time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit event: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if value, ok := decoded["targetResourceId"]; !ok {
		t.Fatal("expected targetResourceId key in JSON")
	} else if value != nil {
		t.Fatalf("expected targetResourceId to be null, got %v", value)
	}
}
