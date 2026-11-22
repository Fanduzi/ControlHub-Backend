package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func redisApprovalTarget() model.QueryTarget {
	return model.QueryTarget{
		ResourceID: 42,
		ConnectionContext: model.QueryTargetConnectionContext{
			Engine: "redis", Environment: "Staging", Host: "redis.example.test", Port: 6379,
		},
	}
}

func writeRedisApproval(t *testing.T, manifest map[string]any) *RedisWorksheet {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return &RedisWorksheet{path: path}
}

func baseRedisManifest() map[string]any {
	return map[string]any{
		"targetId": 42, "environment": "Staging", "address": "redis.example.test:6379",
		"user": "reader", "credentialRef": "REDIS_WORKSHEET_RO", "caPem": "pem", "users": []uint64{7},
		"keys":       map[string]any{"demo:order:note:101": "raw_copy_allowed"},
		"hashFields": map[string]any{"demo:order:101": map[string]any{"status": "raw_copy_allowed"}},
	}
}

// The approval file is the operator's only authorization boundary: key lists,
// field lists and disclosure modes must validate strictly so malformed policy
// fails closed instead of widening reads.
func TestRedisWorksheetApproval(t *testing.T) {
	mutate := func(f func(map[string]any)) map[string]any {
		m := baseRedisManifest()
		f(m)
		return m
	}
	for _, tc := range []struct {
		name     string
		manifest map[string]any
		wantErr  bool
	}{
		{name: "keys and hashFields", manifest: baseRedisManifest()},
		{name: "hashFields only", manifest: mutate(func(m map[string]any) { delete(m, "keys") })},
		{name: "keys only", manifest: mutate(func(m map[string]any) { delete(m, "hashFields") })},
		{name: "no approvals fails closed", manifest: mutate(func(m map[string]any) { delete(m, "keys"); delete(m, "hashFields") }), wantErr: true},
		{name: "empty inner field map", manifest: mutate(func(m map[string]any) {
			m["hashFields"] = map[string]any{"demo:order:101": map[string]any{}}
		}), wantErr: true},
		{name: "bad hash key", manifest: mutate(func(m map[string]any) {
			m["hashFields"] = map[string]any{"demo:order:101;x": map[string]any{"status": "raw_copy_allowed"}}
		}), wantErr: true},
		{name: "bad field name", manifest: mutate(func(m map[string]any) {
			m["hashFields"] = map[string]any{"demo:order:101": map[string]any{"bad field": "raw_copy_allowed"}}
		}), wantErr: true},
		{name: "overlong field name", manifest: mutate(func(m map[string]any) {
			m["hashFields"] = map[string]any{"demo:order:101": map[string]any{strings.Repeat("f", 129): "raw_copy_allowed"}}
		}), wantErr: true},
		{name: "unknown field mode", manifest: mutate(func(m map[string]any) {
			m["hashFields"] = map[string]any{"demo:order:101": map[string]any{"status": "not_a_mode"}}
		}), wantErr: true},
		{name: "unknown key mode", manifest: mutate(func(m map[string]any) {
			m["keys"] = map[string]any{"demo:order:note:101": "not_a_mode"}
		}), wantErr: true},
		{name: "bad key name", manifest: mutate(func(m map[string]any) {
			m["keys"] = map[string]any{"demo:order:101;x": "raw_copy_allowed"}
		}), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := writeRedisApproval(t, tc.manifest)
			a, err := s.approval(redisApprovalTarget(), 7)
			if tc.wantErr {
				if !errors.Is(err, ErrQueryNotAllowed) {
					t.Fatalf("approval err=%v, want ErrQueryNotAllowed", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("approval err=%v", err)
			}
			if len(a.Keys)+len(a.HashFields) == 0 {
				t.Fatal("approval accepted but empty")
			}
		})
	}
}

// Statement parsing is the second boundary: only the exact fixed GET and HGET
// forms may reach Redis; every other shape is a validation refusal before any
// approval or disclosure lookup.
func TestRedisWorksheetStatementShape(t *testing.T) {
	for _, tc := range []struct {
		statement string
		get       bool
		hget      bool
	}{
		{"GET demo:order:101", true, false},
		{"get\tdemo:order:101", true, false},
		{"HGET demo:order:101 status", false, true},
		{"hget demo:order:101 status", false, true},
		{"HGET demo:order:101 field-name_2", false, true},
		{"GET demo:order:101 status", false, false},
		{"HGET demo:order:101", false, false},
		{"HGET demo:order:101 status extra", false, false},
		{"HSET demo:order:101 status x", false, false},
		{"HGETALL demo:order:101", false, false},
		{"HGET demo:order:101 *", false, false},
		{"GET demo:order:101\nGET demo:order:102", false, false},
		{"HGET demo:order:101 status\nHGET demo:order:101 region", false, false},
		{"", false, false},
	} {
		t.Run(strings.ReplaceAll(tc.statement, "\n", "|"), func(t *testing.T) {
			trimmed := strings.Trim(tc.statement, " \t\r\n")
			if got := redisGet.FindStringSubmatch(trimmed) != nil; got != tc.get {
				t.Fatalf("GET match=%v", got)
			}
			if got := redisHGet.FindStringSubmatch(trimmed) != nil; got != tc.hget {
				t.Fatalf("HGET match=%v", got)
			}
		})
	}
}

// The evidence preview is a fixed label so history never carries keys,
// fields or values.
func TestRedisStatementPreview(t *testing.T) {
	for _, tc := range []struct{ statement, want string }{
		{"GET demo:order:101", "Redis GET"},
		{"HGET demo:order:101 status", "Redis HGET"},
		{"  hget\tdemo:order:101\tstatus  ", "Redis HGET"},
		{"HSET demo:order:101 status x", "Redis GET"},
		{"", "Redis GET"},
	} {
		if got := redisStatementPreview(tc.statement); got != tc.want {
			t.Fatalf("preview(%q)=%q, want %q", tc.statement, got, tc.want)
		}
	}
}
