// Package integration provides MySQL-backed integration tests.
// input: scripts/openapi-fuzz.sh, scripts/schemathesis.toml, scripts/README.md
// output: OpenAPI fuzz exclusion-contract assertions
// pos: Prevents silent drift of Schemathesis exclusions away from the documented governance contract
// note: if changed, update this header and README.md
package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// allowedFuzzExclusions is the canonical, audited set of operations excluded
// from Schemathesis fuzzing. Every entry MUST be documented in
// scripts/README.md (OpenAPI Fuzz Exclusion Contract) with operation, reason,
// stable-fixture gap, dedicated test pointer, and allowed scope. Any exclusion
// outside this set, or any broad path/method/tag exclusion, fails this test.
var allowedFuzzExclusions = map[string]bool{
	"executeSavedStatement": true,
}

// TestOpenAPIFuzzExclusionContract keeps the Schemathesis exclusion set narrow,
// documented, and centralized: exclusions may only live in openapi-fuzz.sh as
// single-operation --exclude-operation-id flags, must match the canonical set
// above, and must be documented in scripts/README.md. A new exclusion is a
// governance change: document it first (reason, fixture gap, dedicated test,
// scope), then add it here.
func TestOpenAPIFuzzExclusionContract(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	script := readFuzzContractFile(t, filepath.Join(root, "scripts", "openapi-fuzz.sh"))
	toml := readFuzzContractFile(t, filepath.Join(root, "scripts", "schemathesis.toml"))
	readme := readFuzzContractFile(t, filepath.Join(root, "scripts", "README.md"))

	// 1. Every CLI exclusion is in the canonical allowlist, and every canonical
	// exclusion is actually excluded in the script (no dead contract entries).
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`--exclude-operation-id\s+(\S+)`).FindAllStringSubmatch(script, -1) {
		got[m[1]] = true
		if !allowedFuzzExclusions[m[1]] {
			t.Errorf("undocumented fuzz exclusion %q: add it to the exclusion contract first", m[1])
		}
	}
	for op := range allowedFuzzExclusions {
		if !got[op] {
			t.Errorf("canonical exclusion %q missing from openapi-fuzz.sh", op)
		}
	}

	// 4. Exclusions have a single source of truth: the script. Config overrides
	// (include-operation-id) stay in schemathesis.toml but exclusion directives do not.
	if strings.Contains(toml, "exclude-operation-id") {
		t.Error("schemathesis.toml must not carry exclusion directives; they belong in openapi-fuzz.sh")
	}

	// 5. The README carries a dedicated exclusion-contract section that
	// documents every canonical exclusion (heading is the drift marker).
	const contractHeading = "OpenAPI Fuzz Exclusion Contract"
	if !strings.Contains(readme, contractHeading) {
		t.Errorf("scripts/README.md must contain a %q section", contractHeading)
	}
	for op := range allowedFuzzExclusions {
		if !strings.Contains(readme, op) {
			t.Errorf("scripts/README.md exclusion contract must document %q", op)
		}
	}
}

func readFuzzContractFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
