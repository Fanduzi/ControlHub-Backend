package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestLoadFixtureConfig_FlagAbsent(t *testing.T) {
	t.Setenv("QUERY_DEV_ALLOW_TARGET_FIXTURE", "")
	_, allow, err := loadFixtureConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if allow {
		t.Fatal("allowFixture = true, want false when flag absent")
	}
}

func TestLoadFixtureConfig_FlagPresent_AppliesDefaults(t *testing.T) {
	t.Setenv("QUERY_DEV_ALLOW_TARGET_FIXTURE", "true")
	// Force overrides empty so defaults apply regardless of ambient env.
	t.Setenv("QUERY_DEV_TARGET_ENV_SLUG", "")
	t.Setenv("QUERY_DEV_TARGET_OWNER_EMAIL", "")
	t.Setenv("QUERY_DEV_TARGET_NAME", "")
	t.Setenv("QUERY_DEV_TARGET_DISPLAY_NAME", "")
	cfg, allow, err := loadFixtureConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !allow {
		t.Fatal("allowFixture = false, want true")
	}
	if cfg.EnvironmentSlug != "dev" || cfg.OwnerEmail != "dba@example.com" ||
		cfg.ResourceName != "local-mysql-query-dev" || cfg.DisplayName != "Local MySQL Query Dev" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.Engine != "mysql" || cfg.Version != "8.0" || cfg.Role != "primary" {
		t.Fatalf("engine defaults wrong: %+v", cfg)
	}
	// Host/port are filled by the DSN parser in main, not here.
	if cfg.Host != "" || cfg.Port != 0 {
		t.Fatalf("host/port must be unset, got host=%q port=%d", cfg.Host, cfg.Port)
	}
}

func TestLoadFixtureConfig_OverridesApplied(t *testing.T) {
	t.Setenv("QUERY_DEV_ALLOW_TARGET_FIXTURE", "true")
	t.Setenv("QUERY_DEV_TARGET_ENV_SLUG", "staging")
	t.Setenv("QUERY_DEV_TARGET_OWNER_EMAIL", "sre@example.com")
	t.Setenv("QUERY_DEV_TARGET_NAME", "custom-target")
	t.Setenv("QUERY_DEV_TARGET_DISPLAY_NAME", "Custom Target")
	cfg, _, err := loadFixtureConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.EnvironmentSlug != "staging" || cfg.OwnerEmail != "sre@example.com" ||
		cfg.ResourceName != "custom-target" || cfg.DisplayName != "Custom Target" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadFixtureConfig_BadFlagValue(t *testing.T) {
	t.Setenv("QUERY_DEV_ALLOW_TARGET_FIXTURE", "notabool")
	_, allow, err := loadFixtureConfig()
	if err == nil {
		t.Fatal("expected error for bad bool flag")
	}
	if allow {
		t.Fatal("allowFixture must be false on bad flag")
	}
	if strings.Contains(err.Error(), "tcp(") || strings.Contains(err.Error(), "@") {
		t.Fatalf("error leaks fragment: %q", err.Error())
	}
}

func TestPrintReport_NoDSN(t *testing.T) {
	var buf bytes.Buffer
	meta := model.QueryCredentialMetadata{
		ResourceID:        42,
		Engine:            "mysql",
		CredentialRef:     "LOCAL_QUERY_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	printReport(&buf, meta, model.ReadinessReady, true)
	out := buf.String()
	for _, needle := range []string{"42", "LOCAL_QUERY_RO", "mysql", "readiness", "run"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("output missing %q:\n%s", needle, out)
		}
	}
	for _, bad := range []string{"tcp(", "@", "://", "secret", "password"} {
		if strings.Contains(out, bad) {
			t.Fatalf("output leaks %q:\n%s", bad, out)
		}
	}
}

func TestFixtureModePreflight_RejectsExplicitResourceID(t *testing.T) {
	if err := fixtureModePreflight("123"); !errors.Is(err, errFixtureExplicitResourceIDForbidden) {
		t.Fatalf("err = %v, want errFixtureExplicitResourceIDForbidden", err)
	}
	if err := fixtureModePreflight(""); err != nil {
		t.Fatalf("empty explicit id must be allowed, got %v", err)
	}
	if err := fixtureModePreflight("123"); strings.Contains(err.Error(), "tcp(") || strings.Contains(err.Error(), "@") {
		t.Fatalf("error leaks fragment: %q", err.Error())
	}
}
