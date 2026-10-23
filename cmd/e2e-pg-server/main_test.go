// Package main provides tests for the E2E fixture listener's safety boundary.
// input: disposableDSNAllowed, buildInfoSHA
// output: disposable-DSN acceptance/refusal matrix tests
// pos: proves the fixture listener refuses production-shaped metadata DSNs
// before serving — loopback + controlhub_*_e2e is the primary guard
// note: if this file changes, update header and cmd/e2e-pg-server/README.md
package main

import (
	"strings"
	"testing"
)

func TestDisposableDSNAllowed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{"accepts loopback e2e database", "controlhub:pw@tcp(127.0.0.1:3306)/controlhub_t13_e2e?parseTime=true", false},
		{"accepts controlhub_e2e name", "controlhub:pw@tcp(127.0.0.1:3306)/controlhub_e2e", false},
		{"rejects default controlhub db", "controlhub:pw@tcp(127.0.0.1:3306)/controlhub", true},
		{"rejects production-shaped name", "controlhub:pw@tcp(127.0.0.1:3306)/controlhub_prod", true},
		{"rejects remote host", "controlhub:pw@tcp(10.20.0.5:3306)/controlhub_e2e", true},
		{"rejects hostname localhost", "controlhub:pw@tcp(localhost:3306)/controlhub_e2e", true},
		{"rejects malformed dsn", "not-a-dsn", true},
		{"rejects unix socket", "controlhub:pw@unix(/tmp/mysql.sock)/controlhub_e2e", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := disposableDSNAllowed(tc.dsn)
			if tc.wantErr && err == nil {
				t.Fatalf("DSN %q must be refused", tc.dsn)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("DSN %q must be accepted, got %v", tc.dsn, err)
			}
			if err != nil && strings.Contains(err.Error(), "pw") {
				t.Fatalf("refusal message must never echo DSN secrets: %v", err)
			}
		})
	}
}
