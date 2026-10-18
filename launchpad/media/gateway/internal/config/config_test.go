package config

import (
	"testing"
)

// setValid populates the process env with a complete, valid gateway config.
// NODE_ENV defaults to production, so the config carries the production
// bindings Validate checks: the sha256-bound runtime document reference and
// the exact https public origin, which must be one of the CORS origins.
func setValid(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GATEWAY_RUNTIME_FILE":   "/app/prod/gateway-runtime.json",
		"GATEWAY_RUNTIME_SHA256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"PUBLIC_URL":             "https://web.example",
		"CORS_ALLOWED_ORIGINS":   "https://web.example",
		"REDIS_URL":            "redis://localhost:6379",
		"GATEWAY_JWT_SECRET":   "super-secret-signing-key",
		"SOCIAL_HTTP_URL":      "http://social:3000",
		"SOCIAL_WS_URL":        "ws://social:3000",
		"METADATA_UPLOAD_URL":  "http://metadata:3000",
		"S3_ENDPOINT":          "https://acc.r2.cloudflarestorage.com",
		"S3_ACCESS_KEY_ID":     "ak",
		"S3_SECRET_ACCESS_KEY": "sk",
		"METADATA_BUCKET":      "kl-metadata",
	} {
		t.Setenv(k, v)
	}
}

func TestLoad_Valid(t *testing.T) {
	setValid(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("default Port = %d, want 3000", cfg.Port)
	}
	if cfg.JWTTTLSeconds != 86400 {
		t.Errorf("default JWTTTLSeconds = %d, want 86400", cfg.JWTTTLSeconds)
	}
	if got := cfg.Buckets(); len(got) != 1 || got["token"] != "kl-metadata" {
		t.Errorf("Buckets() = %v, want {token: kl-metadata}", got)
	}
}

func TestBuckets_AllThree(t *testing.T) {
	setValid(t)
	t.Setenv("USER_BUCKET", "kl-user")
	t.Setenv("OG_BUCKET", "kl-og")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b := cfg.Buckets()
	if b["token"] != "kl-metadata" || b["user"] != "kl-user" || b["og"] != "kl-og" {
		t.Errorf("Buckets() = %v", b)
	}
}

func TestValidate_Failures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  map[string]string
		wantErr bool
	}{
		{"missing all buckets", map[string]string{"METADATA_BUCKET": ""}, true},
		{"short jwt secret", map[string]string{"GATEWAY_JWT_SECRET": "short"}, true},
		{"bad social url", map[string]string{"SOCIAL_HTTP_URL": "not-a-url"}, true},
		{"bad log level", map[string]string{"LOG_LEVEL": "verbose"}, true},
		{"bad port", map[string]string{"PORT": "70000"}, true},
		{"production without runtime binding", map[string]string{"GATEWAY_RUNTIME_FILE": ""}, true},
		{"production with short runtime digest", map[string]string{"GATEWAY_RUNTIME_SHA256": "abc"}, true},
		{"production plain-http public origin", map[string]string{"PUBLIC_URL": "http://web.example", "CORS_ALLOWED_ORIGINS": "http://web.example"}, true},
		{"production public origin with path", map[string]string{"PUBLIC_URL": "https://web.example/app"}, true},
		{"production public origin outside CORS", map[string]string{"CORS_ALLOWED_ORIGINS": "https://other.example"}, true},
		{"production duplicate CORS origin", map[string]string{"CORS_ALLOWED_ORIGINS": "https://web.example,https://web.example"}, true},
		{"production cache bound", map[string]string{"OBJECT_CACHE_MAX_BYTES": "2097152"}, true},
		{"production two origins", map[string]string{"CORS_ALLOWED_ORIGINS": "https://web.example,https://app.example"}, false},
		{"ok", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setValid(t)
			for k, v := range tc.mutate {
				t.Setenv(k, v)
			}
			_, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
