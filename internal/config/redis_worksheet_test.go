package config

import "testing"

func TestRedisWorksheetConfigRequiresExplicitNonProduction(t *testing.T) {
	for _, tc := range []struct {
		environment, path string
		valid             bool
	}{
		{"production", "", true}, {"production", "/redis/active.json", false}, {"", "/redis/active.json", false},
		{"nonproduction", "relative.json", false}, {"nonproduction", "/redis/active.json", true},
	} {
		t.Run(tc.environment+tc.path, func(t *testing.T) {
			t.Setenv("CONTROLHUB_ENVIRONMENT", tc.environment)
			t.Setenv("CONTROLHUB_CH_PREVIEW_DIR", "")
			t.Setenv("CONTROLHUB_REDIS_WORKSHEET_CONFIG", tc.path)
			cfg, err := Load()
			if (err == nil) != tc.valid || (tc.valid && cfg.RedisWorksheetPath != tc.path) {
				t.Fatalf("path=%q err=%v", cfg.RedisWorksheetPath, err)
			}
		})
	}
}
