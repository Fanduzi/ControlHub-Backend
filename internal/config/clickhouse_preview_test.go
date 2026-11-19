package config

import "testing"

func TestCHPreviewConfigRequiresExplicitNonProduction(t *testing.T) {
	for _, tc := range []struct {
		environment, dir string
		valid            bool
	}{
		{"production", "", true}, {"production", "/preview", false}, {"", "/preview", false}, {"nonproduction", "relative", false}, {"nonproduction", "/preview", true},
	} {
		t.Run(tc.environment+tc.dir, func(t *testing.T) {
			t.Setenv("CONTROLHUB_ENVIRONMENT", tc.environment)
			t.Setenv("CONTROLHUB_CH_PREVIEW_DIR", tc.dir)
			cfg, err := Load()
			if (err == nil) != tc.valid || (tc.valid && cfg.CHPreviewDir != tc.dir) {
				t.Fatalf("dir=%q err=%v", cfg.CHPreviewDir, err)
			}
		})
	}
}
