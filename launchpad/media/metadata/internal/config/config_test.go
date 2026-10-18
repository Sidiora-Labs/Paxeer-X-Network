package config_test

import (
	"os"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
)

// allKeys is the full env universe the config reads. apply() sets every key
// present in the map and truly UNSETS the rest (so required/default behaviour is
// deterministic regardless of the host environment). t.Setenv captures the
// original for restore; os.Unsetenv then removes it for the test's duration.
var allKeys = []string{
	"DATABASE_URL", "REDIS_URL", "S3_ENDPOINT", "S3_BUCKET",
	"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_REGION",
	"LOG_LEVEL", "PORT", "PUBLIC_URL",
	"MAX_LOGO_SIZE_BYTES", "MAX_BANNER_SIZE_BYTES", "CORS_ALLOWED_ORIGINS",
}

func apply(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range allKeys {
		if v, ok := env[k]; ok {
			t.Setenv(k, v)
			continue
		}
		t.Setenv(k, "") // capture original for restore
		os.Unsetenv(k)  // then unset for this test
	}
}

// baseEnv is a complete, valid environment; individual tests mutate one key.
func baseEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":          "postgresql://u:p@localhost:5432/db",
		"REDIS_URL":             "redis://localhost:6379",
		"S3_ENDPOINT":           "https://acct.r2.cloudflarestorage.com",
		"S3_BUCKET":             "token-assets",
		"S3_ACCESS_KEY_ID":      "ak",
		"S3_SECRET_ACCESS_KEY":  "sk",
		"LOG_LEVEL":             "info",
		"PORT":                  "5056",
		"MAX_LOGO_SIZE_BYTES":   "2097152",
		"MAX_BANNER_SIZE_BYTES": "5242880",
	}
}

func TestLoad_Valid(t *testing.T) {
	apply(t, baseEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3Bucket != "token-assets" {
		t.Errorf("S3Bucket = %q, want token-assets", cfg.S3Bucket)
	}
	if cfg.S3Region != "auto" {
		t.Errorf("S3Region default = %q, want auto", cfg.S3Region)
	}
	if cfg.Port != 5056 {
		t.Errorf("Port = %d, want 5056", cfg.Port)
	}
	if cfg.MaxLogoSizeBytes != 2097152 {
		t.Errorf("MaxLogoSizeBytes = %d", cfg.MaxLogoSizeBytes)
	}
}

func TestLoad_Defaults(t *testing.T) {
	env := baseEnv()
	delete(env, "PORT")
	delete(env, "LOG_LEVEL")
	delete(env, "MAX_LOGO_SIZE_BYTES")
	delete(env, "MAX_BANNER_SIZE_BYTES")
	apply(t, env)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("default Port = %d, want 3000", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.MaxLogoSizeBytes != 2097152 || cfg.MaxBannerSizeBytes != 5242880 {
		t.Errorf("default size caps wrong: logo=%d banner=%d", cfg.MaxLogoSizeBytes, cfg.MaxBannerSizeBytes)
	}
}

func TestLoad_Invalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing database url", func(m map[string]string) { delete(m, "DATABASE_URL") }},
		{"missing redis url", func(m map[string]string) { delete(m, "REDIS_URL") }},
		{"missing s3 endpoint", func(m map[string]string) { delete(m, "S3_ENDPOINT") }},
		{"missing s3 bucket", func(m map[string]string) { delete(m, "S3_BUCKET") }},
		{"missing s3 access key", func(m map[string]string) { delete(m, "S3_ACCESS_KEY_ID") }},
		{"missing s3 secret", func(m map[string]string) { delete(m, "S3_SECRET_ACCESS_KEY") }},
		{"bad database url", func(m map[string]string) { m["DATABASE_URL"] = "not-a-url" }},
		{"bad s3 endpoint", func(m map[string]string) { m["S3_ENDPOINT"] = "nope" }},
		{"bad log level", func(m map[string]string) { m["LOG_LEVEL"] = "verbose" }},
		{"bad port", func(m map[string]string) { m["PORT"] = "70000" }},
		{"zero logo size", func(m map[string]string) { m["MAX_LOGO_SIZE_BYTES"] = "0" }},
		{"zero banner size", func(m map[string]string) { m["MAX_BANNER_SIZE_BYTES"] = "0" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			tc.mutate(env)
			apply(t, env)
			if _, err := config.Load(); err == nil {
				t.Error("Load() err = nil, want validation error")
			}
		})
	}
}
