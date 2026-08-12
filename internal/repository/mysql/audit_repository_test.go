package mysql

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

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
