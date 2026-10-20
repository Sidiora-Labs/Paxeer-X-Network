package config

import (
	"encoding/base64"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"strings"
	"testing"
	"time"
)

// optionalKeys are every env var Config reads beyond the two required URLs.
// They are unset before each test so the struct-tag defaults apply
// deterministically regardless of the ambient environment.
var optionalKeys = []string{
	"PORT", "LOG_LEVEL", "NODE_ENV", "CORS_ALLOWED_ORIGINS",
	"API_RATE_LIMIT_MAX", "API_RATE_LIMIT_WINDOW_SEC", "API_MAX_INFLIGHT_REQUESTS",
	"WS_MAX_CONNECTIONS", "WS_MAX_PER_IP", "SSE_MAX_CONNECTIONS", "SSE_MAX_PER_IP",
	"CLIENT_SEND_BUFFER", "COALESCE_FLUSH_MS", "ADMIN_API_KEY",
}

// setBase sets the minimal required env for a valid Config and clears the
// optional tuning vars so each test starts from the struct-tag defaults.
func setBase(t *testing.T) {
	t.Helper()
	for _, k := range optionalKeys {
		unsetEnv(t, k)
	}
	t.Setenv("DATABASE_URL", "postgres://kl:kl@localhost:5432/kl")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
}

// unsetEnv removes key for the duration of the test, restoring it on cleanup.
// t.Setenv can only set values (not clear them), so this is needed to let
// caarlos0/env apply the struct-tag defaults for otherwise-unset integer vars.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, orig)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestLoad_DefaultsHappyPath(t *testing.T) {
	setBase(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.NodeEnv != "production" {
		t.Errorf("NodeEnv = %q, want production", cfg.NodeEnv)
	}
	if cfg.RateLimitMax != 100 {
		t.Errorf("RateLimitMax = %d, want 100", cfg.RateLimitMax)
	}
	if cfg.RateLimitWindowSec != 60 {
		t.Errorf("RateLimitWindowSec = %d, want 60", cfg.RateLimitWindowSec)
	}
	if cfg.WSMaxConnections != 50000 {
		t.Errorf("WSMaxConnections = %d, want 50000", cfg.WSMaxConnections)
	}
	if cfg.WSMaxPerIP != 20 {
		t.Errorf("WSMaxPerIP = %d, want 20", cfg.WSMaxPerIP)
	}
	if cfg.ClientSendBuffer != 256 {
		t.Errorf("ClientSendBuffer = %d, want 256", cfg.ClientSendBuffer)
	}
	if cfg.CoalesceFlushMS != 100 {
		t.Errorf("CoalesceFlushMS = %d, want 100", cfg.CoalesceFlushMS)
	}
	if cfg.MaxInFlightRequests != 10000 {
		t.Errorf("MaxInFlightRequests = %d, want 10000", cfg.MaxInFlightRequests)
	}
	if cfg.RateLimitWindow() != 60*time.Second {
		t.Errorf("RateLimitWindow = %v, want 60s", cfg.RateLimitWindow())
	}
	if cfg.CoalesceFlush() != 100*time.Millisecond {
		t.Errorf("CoalesceFlush = %v, want 100ms", cfg.CoalesceFlush())
	}
}

func TestLoad_OverridesAndRedissScheme(t *testing.T) {
	setBase(t)
	t.Setenv("REDIS_URL", "rediss://user:pass@redis.example:6380/1")
	t.Setenv("PORT", "8080")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("NODE_ENV", "development")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.sidiora.fun,https://sidiora.fun")
	t.Setenv("WS_MAX_CONNECTIONS", "250000")
	t.Setenv("ADMIN_API_KEY", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.WSMaxConnections != 250000 {
		t.Errorf("WSMaxConnections = %d, want 250000", cfg.WSMaxConnections)
	}
	if cfg.AdminAPIKey != "secret" {
		t.Errorf("AdminAPIKey = %q, want secret", cfg.AdminAPIKey)
	}
	if cfg.CORSAllowedOrigins == "" {
		t.Error("CORSAllowedOrigins should be set")
	}
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T)
	}{
		{"missing DATABASE_URL", func(t *testing.T) { unsetEnv(t, "DATABASE_URL") }},
		{"missing REDIS_URL", func(t *testing.T) { unsetEnv(t, "REDIS_URL") }},
		{"bad DATABASE_URL", func(t *testing.T) { t.Setenv("DATABASE_URL", "not-a-url") }},
		{"bad REDIS_URL scheme", func(t *testing.T) { t.Setenv("REDIS_URL", "http://localhost:6379") }},
		{"bad LOG_LEVEL", func(t *testing.T) { t.Setenv("LOG_LEVEL", "verbose") }},
		{"bad NODE_ENV", func(t *testing.T) { t.Setenv("NODE_ENV", "staging") }},
		{"bad PORT", func(t *testing.T) { t.Setenv("PORT", "70000") }},
		{"non-positive rate max", func(t *testing.T) { t.Setenv("API_RATE_LIMIT_MAX", "0") }},
		{"non-positive send buffer", func(t *testing.T) { t.Setenv("CLIENT_SEND_BUFFER", "0") }},
		{"non-positive coalesce", func(t *testing.T) { t.Setenv("COALESCE_FLUSH_MS", "-1") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBase(t)
			tt.mutate(t)
			if _, err := Load(); err == nil {
				t.Fatalf("Load: expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestValidate_DirectBoundary(t *testing.T) {
	base := Config{
		DatabaseURL: "postgres://localhost/kl", RedisURL: "redis://localhost:6379",
		Port: 3000, LogLevel: "info", NodeEnv: "production",
		RateLimitMax: 1, RateLimitWindowSec: 1, MaxInFlightRequests: 1,
		WSMaxConnections: 1, WSMaxPerIP: 1, SSEMaxConnections: 1, SSEMaxPerIP: 1,
		ClientSendBuffer: 1, CoalesceFlushMS: 1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("minimal positive config should validate: %v", err)
	}

	bad := base
	bad.Port = 0
	if err := bad.Validate(); err == nil {
		t.Error("Port=0 should fail validation")
	}
}

func TestLoad_InlineMaterial(t *testing.T) {
	pairs := []struct{ file, inline string }{
		{"IDENTITY_CA_FILE", "IDENTITY_CA_PEM"},
		{"IDENTITY_CLIENT_CERT_FILE", "IDENTITY_CLIENT_CERT_PEM"},
		{"IDENTITY_CLIENT_KEY_FILE", "IDENTITY_CLIENT_KEY_PEM"},
		{"NATS_CREDENTIALS", "NATS_CREDENTIALS_CREDS"},
	}
	for _, p := range pairs {
		t.Run(p.inline, func(t *testing.T) {
			setBase(t)
			for _, other := range pairs {
				if other != p {
					t.Setenv(other.inline, "inline")
				}
			}
			t.Setenv(p.inline, "-----BEGIN MATERIAL-----\n"+p.inline+"\n-----END MATERIAL-----\n")
			if _, err := Load(); err != nil {
				t.Fatalf("inline %s rejected: %v", p.inline, err)
			}
			t.Setenv(p.file, "/etc/identity/material.pem")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), p.file+" and "+p.inline+" are both set") {
				t.Fatalf("both %s and %s accepted: %v", p.file, p.inline, err)
			}
			t.Setenv(p.file, "")
			t.Setenv(p.inline, "")
			t.Setenv("RUNTIME_PROFILE", "container")
			t.Setenv("API_ROLE", "sockets")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), p.file+" or "+p.inline+" is required") {
				t.Fatalf("missing %s accepted in a production profile: %v", p.inline, err)
			}
		})
	}

	cfg := Config{IdentityCAPEM: "ca", IdentityClientCertPEM: "cert", IdentityClientKeyPEM: "key"}
	if ca, cert, key, err := cfg.IdentityTLSMaterial(); err != nil || string(ca) != "ca" || string(cert) != "cert" || string(key) != "key" {
		t.Fatalf("inline identity material = %q %q %q, %v", ca, cert, key, err)
	}
	userJWT, creds := inlineNATSCreds(t)
	option, err := (&Config{NATSCredentialsCreds: creds}).NATSCredentialsOption()
	if err != nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS option: %v", err)
	}
	var applied nats.Options
	if err = option(&applied); err != nil || applied.UserJWT == nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS set no user JWT: %v", err)
	}
	if got, err := applied.UserJWT(); err != nil || got != userJWT {
		t.Fatalf("user JWT = %q, %v", got, err)
	}
}

// inlineNATSCreds returns a user JWT signed by a fresh account nkey and the
// .creds content carrying it with the user seed.
func inlineNATSCreds(t *testing.T) (string, string) {
	t.Helper()
	account, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	user, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	accountPub, _ := account.PublicKey()
	userPub, _ := user.PublicKey()
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc([]byte(`{"typ":"JWT","alg":"ed25519-nkey"}`)) + "." + enc([]byte(`{"sub":"`+userPub+`","iss":"`+accountPub+`","nats":{"type":"user","version":2}}`))
	signature, err := account.Sign([]byte(signing))
	if err != nil {
		t.Fatal(err)
	}
	userJWT := signing + "." + enc(signature)
	seed, err := user.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return userJWT, "-----BEGIN NATS USER JWT-----\n" + userJWT + "\n------END NATS USER JWT------\n\n-----BEGIN USER NKEY SEED-----\n" + string(seed) + "\n------END USER NKEY SEED------\n"
}
