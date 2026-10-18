package config_test

import (
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
)

// baseEnv returns a complete, valid environment map.
func baseEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":         "postgres://kl:kl@localhost:5432/user_test?sslmode=disable",
		"REDIS_URL":            "redis://localhost:6379/0",
		"S3_ENDPOINT":          "https://r2.example.com",
		"S3_BUCKET":            "user-assets",
		"S3_ACCESS_KEY_ID":     "ak",
		"S3_SECRET_ACCESS_KEY": "sk",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, baseEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.S3Region != "auto" {
		t.Errorf("S3Region = %q, want auto", cfg.S3Region)
	}
	if cfg.MaxAvatarSizeBytes != 2097152 {
		t.Errorf("MaxAvatarSizeBytes = %d, want 2097152", cfg.MaxAvatarSizeBytes)
	}
	if cfg.MaxBannerSizeBytes != 5242880 {
		t.Errorf("MaxBannerSizeBytes = %d, want 5242880", cfg.MaxBannerSizeBytes)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	// Omit each required S3 var in turn; Load must fail.
	required := []string{"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"}
	for _, miss := range required {
		t.Run("missing_"+miss, func(t *testing.T) {
			env := baseEnv()
			delete(env, miss)
			setEnv(t, env)
			// Ensure the omitted var is truly empty for this subtest.
			t.Setenv(miss, "")
			if _, err := config.Load(); err == nil {
				t.Errorf("expected error when %s missing", miss)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr bool
	}{
		{"valid", func(c *config.Config) {}, false},
		{"bad db url", func(c *config.Config) { c.DatabaseURL = "not-a-url" }, true},
		{"empty redis", func(c *config.Config) { c.RedisURL = "  " }, true},
		{"bad s3 endpoint", func(c *config.Config) { c.S3Endpoint = "nope" }, true},
		{"empty bucket", func(c *config.Config) { c.S3Bucket = "" }, true},
		{"empty access key", func(c *config.Config) { c.S3AccessKeyID = "" }, true},
		{"empty secret", func(c *config.Config) { c.S3SecretAccessKey = "" }, true},
		{"bad log level", func(c *config.Config) { c.LogLevel = "trace" }, true},
		{"bad port low", func(c *config.Config) { c.Port = 0 }, true},
		{"bad port high", func(c *config.Config) { c.Port = 70000 }, true},
		{"bad avatar size", func(c *config.Config) { c.MaxAvatarSizeBytes = 0 }, true},
		{"bad banner size", func(c *config.Config) { c.MaxBannerSizeBytes = -1 }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{
				DatabaseURL:        "postgres://localhost:5432/db",
				RedisURL:           "redis://localhost:6379",
				S3Endpoint:         "https://r2.example.com",
				S3Bucket:           "b",
				S3AccessKeyID:      "ak",
				S3SecretAccessKey:  "sk",
				S3Region:           "auto",
				LogLevel:           "info",
				Port:               3000,
				MaxAvatarSizeBytes: 2097152,
				MaxBannerSizeBytes: 5242880,
			}
			c.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}
