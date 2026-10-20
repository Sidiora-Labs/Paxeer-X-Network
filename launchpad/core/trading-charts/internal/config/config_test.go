package config_test

import (
	"strings"
	"testing"

	"encoding/base64"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// validEnv returns the full set of env vars required for a successful Load.
// Tests mutate a copy to exercise individual failure modes.
func validEnv() map[string]string {
	// Contract addresses must equal the reviewed chain-125 deployment manifest
	// (shared/config.BaseEnv.Validate rejects any other address).
	roles := deployment.Current().Roles
	return map[string]string{
		"DATABASE_URL":            "postgres://u:p@localhost:5432/charts",
		"REDIS_URL":               "redis://localhost:6379",
		"REDIS_BULL_URL":          "redis://localhost:6379/1",
		"RPC_URL":                 "https://rpc.example.com/rpc",
		"EVENT_EMITTER_ADDRESS":   roles["EventEmitter"].Address,
		"POOL_REGISTRY_ADDRESS":   roles["PoolRegistry"].Address,
		"ROUTER_ADDRESS":          roles["Router"].Address,
		"FACTORY_ADDRESS":         roles["SidioraFactory"].Address,
		"QUOTER_ADDRESS":          roles["Quoter"].Address,
		"PROTOCOL_CONFIG_ADDRESS": roles["ProtocolConfig"].Address,
		"FEE_ACCUMULATOR_ADDRESS": roles["FeeAccumulator"].Address,
		"SIDIORA_NFT_ADDRESS":     roles["SidioraNFT"].Address,
		"FEES_ROUTER_ADDRESS":     roles["FeesRouter"].Address,
		"POOL_BEACON_ADDRESS":     roles["PoolBeacon"].Address,
		"WEBHOOK_HMAC_SECRET":     strings.Repeat("s", 32),
	}
}

func applyEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoadHappyPath(t *testing.T) {
	applyEnv(t, validEnv())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.WebhookHMACSecret != strings.Repeat("s", 32) {
		t.Errorf("WebhookHMACSecret = %q", cfg.WebhookHMACSecret)
	}
	if cfg.WSMaxConnections != 10000 {
		t.Errorf("WSMaxConnections default = %d, want 10000", cfg.WSMaxConnections)
	}
	if cfg.WSMaxPerIP != 20 {
		t.Errorf("WSMaxPerIP default = %d, want 20", cfg.WSMaxPerIP)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port default = %d, want 3000", cfg.Port)
	}
}

func TestLoadOverridesWSLimits(t *testing.T) {
	env := validEnv()
	env["WS_MAX_CONNECTIONS"] = "500"
	env["WS_MAX_PER_IP"] = "5"
	env["CORS_ALLOWED_ORIGINS"] = "https://app.sidiora.fun"
	applyEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WSMaxConnections != 500 || cfg.WSMaxPerIP != 5 {
		t.Errorf("WS limits = %d/%d, want 500/5", cfg.WSMaxConnections, cfg.WSMaxPerIP)
	}
	if cfg.CORSAllowedOrigins != "https://app.sidiora.fun" {
		t.Errorf("CORS = %q", cfg.CORSAllowedOrigins)
	}
}

func TestLoadMissingSecret(t *testing.T) {
	env := validEnv()
	delete(env, "WEBHOOK_HMAC_SECRET")
	applyEnv(t, env)

	if _, err := config.Load(); err == nil {
		t.Fatal("Load: expected error for missing WEBHOOK_HMAC_SECRET")
	}
}

func TestLoadShortSecret(t *testing.T) {
	env := validEnv()
	env["WEBHOOK_HMAC_SECRET"] = "tooshort"
	applyEnv(t, env)

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "at least 32") {
		t.Fatalf("Load: expected short-secret error, got %v", err)
	}
}

func TestLoadInvalidBaseEnv(t *testing.T) {
	env := validEnv()
	env["EVENT_EMITTER_ADDRESS"] = "not-an-address"
	applyEnv(t, env)

	if _, err := config.Load(); err == nil {
		t.Fatal("Load: expected base-env validation error for bad address")
	}
}

func TestLoadMissingRequiredBaseVar(t *testing.T) {
	env := validEnv()
	delete(env, "DATABASE_URL")
	applyEnv(t, env)

	if _, err := config.Load(); err == nil {
		t.Fatal("Load: expected error for missing DATABASE_URL")
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

func TestInlineNATSCredentials(t *testing.T) {
	userJWT, creds := inlineNATSCreds(t)
	applyEnv(t, validEnv())
	t.Setenv("NATS_CREDENTIALS_CREDS", creds)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS rejected: %v", err)
	}
	options, err := cfg.RuntimeNATSOptions()
	if err != nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS options: %v", err)
	}
	var applied nats.Options
	for _, option := range options {
		if err := option(&applied); err != nil {
			t.Fatal(err)
		}
	}
	if applied.UserJWT == nil || applied.SignatureCB == nil {
		t.Fatal("inline NATS_CREDENTIALS_CREDS set no user JWT handler")
	}
	if got, err := applied.UserJWT(); err != nil || got != userJWT {
		t.Fatalf("user JWT = %q, %v", got, err)
	}
	if _, err := applied.SignatureCB([]byte("nonce")); err != nil {
		t.Fatalf("inline seed cannot sign: %v", err)
	}
	t.Setenv("NATS_CREDENTIALS", "/etc/nats/user.creds")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS and NATS_CREDENTIALS_CREDS are both set") {
		t.Fatalf("both NATS credential sources accepted: %v", err)
	}
	t.Setenv("NATS_CREDENTIALS", "")
	t.Setenv("NATS_CREDENTIALS_CREDS", "")
	t.Setenv("NODE_ENV", "production")
	t.Setenv("RUNTIME_PROFILE", "container")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS or NATS_CREDENTIALS_CREDS is required") {
		t.Fatalf("missing NATS credentials accepted in a production profile: %v", err)
	}
}
