package config

import (
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://kl:kl@localhost:5432/social?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
}

func TestLoadDefaults(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.MaxMessageLength != 500 {
		t.Errorf("MaxMessageLength = %d, want 500", cfg.MaxMessageLength)
	}
	if cfg.RateWindowSeconds != 10 || cfg.MaxPoolMsgsPerWin != 5 || cfg.MaxDmMsgsPerWindow != 5 {
		t.Errorf("rate defaults = %d/%d/%d", cfg.RateWindowSeconds, cfg.MaxPoolMsgsPerWin, cfg.MaxDmMsgsPerWindow)
	}
	if cfg.WSSendBuffer != 64 || cfg.WSWriteTimeoutSecs != 10 || cfg.WSReadLimitBytes != 4096 {
		t.Errorf("ws defaults = %d/%d/%d", cfg.WSSendBuffer, cfg.WSWriteTimeoutSecs, cfg.WSReadLimitBytes)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	baseEnv(t)
	t.Setenv("PORT", "8088")
	t.Setenv("MAX_MESSAGE_LENGTH", "1000")
	t.Setenv("WS_SEND_BUFFER", "128")
	t.Setenv("CHAT_ADMIN_API_KEY", "secret")
	t.Setenv("LOG_LEVEL", "debug")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8088 || cfg.MaxMessageLength != 1000 || cfg.WSSendBuffer != 128 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.ChatAdminAPIKey != "secret" {
		t.Errorf("admin key = %q", cfg.ChatAdminAPIKey)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	// No DATABASE_URL / REDIS_URL set.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing required vars")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"bad db url", func(c *Config) { c.DatabaseURL = "not a url" }, true},
		{"empty redis", func(c *Config) { c.RedisURL = "" }, true},
		{"bad log level", func(c *Config) { c.LogLevel = "trace" }, true},
		{"bad port", func(c *Config) { c.Port = 0 }, true},
		{"bad port high", func(c *Config) { c.Port = 70000 }, true},
		{"zero msg len", func(c *Config) { c.MaxMessageLength = 0 }, true},
		{"zero rate window", func(c *Config) { c.RateWindowSeconds = 0 }, true},
		{"zero pool max", func(c *Config) { c.MaxPoolMsgsPerWin = 0 }, true},
		{"zero dm max", func(c *Config) { c.MaxDmMsgsPerWindow = 0 }, true},
		{"zero send buffer", func(c *Config) { c.WSSendBuffer = 0 }, true},
		{"zero write timeout", func(c *Config) { c.WSWriteTimeoutSecs = 0 }, true},
		{"zero read limit", func(c *Config) { c.WSReadLimitBytes = 0 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{
				DatabaseURL:        "postgres://kl:kl@localhost:5432/social",
				RedisURL:           "redis://localhost:6379",
				LogLevel:           "info",
				Port:               3000,
				MaxMessageLength:   500,
				RateWindowSeconds:  10,
				MaxPoolMsgsPerWin:  5,
				MaxDmMsgsPerWindow: 5,
				WSSendBuffer:       64,
				WSWriteTimeoutSecs: 10,
				WSReadLimitBytes:   4096,
			}
			tc.mutate(c)
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
