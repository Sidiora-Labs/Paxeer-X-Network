package config_test

import (
	"testing"
	"time"

	"encoding/base64"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"strings"
)

// setBaseEnv sets every required base + pnl env var to a valid value, so a test
// can then mutate one to exercise a specific validation path.
func setBaseEnv(t *testing.T) {
	t.Helper()
	// Contract addresses must equal the active chain-125 deployment manifest
	// (shared/config.BaseEnv.Validate rejects any other address).
	roles := deployment.Current().Roles
	vars := map[string]string{
		"DATABASE_URL":            "postgres://kl:kl@localhost:5432/pnl",
		"REDIS_URL":               "redis://localhost:6379",
		"REDIS_BULL_URL":          "redis://localhost:6379/1",
		"RPC_URL":                 "http://localhost:8545",
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
		"WEBHOOK_HMAC_SECRET":     "0123456789abcdef0123456789abcdef",
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReconcileIntervalMS != 30000 {
		t.Errorf("ReconcileIntervalMS = %d, want 30000", cfg.ReconcileIntervalMS)
	}
	if cfg.ReconcileInterval() != 30*time.Second {
		t.Errorf("ReconcileInterval = %s, want 30s", cfg.ReconcileInterval())
	}
	if cfg.ReconcileBatchSize != 500 {
		t.Errorf("ReconcileBatchSize = %d, want 500", cfg.ReconcileBatchSize)
	}
	if cfg.RewardPerConversion != 1 {
		t.Errorf("RewardPerConversion = %d, want 1", cfg.RewardPerConversion)
	}
	if cfg.ShareOrigin() != "https://sidiora.fun" {
		t.Errorf("ShareOrigin = %q", cfg.ShareOrigin())
	}
	// OGOrigin falls back to the public base URL when PNL_OG_BASE_URL is unset.
	if cfg.OGOrigin() != "https://sidiora.fun" {
		t.Errorf("OGOrigin = %q, want public fallback", cfg.OGOrigin())
	}
}

func TestLoadOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("PNL_RECONCILE_INTERVAL_MS", "5000")
	t.Setenv("PNL_PUBLIC_BASE_URL", "https://sidiora.fun/")
	t.Setenv("PNL_OG_BASE_URL", "https://og.sidiora.fun/")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReconcileInterval() != 5*time.Second {
		t.Errorf("ReconcileInterval = %s, want 5s", cfg.ReconcileInterval())
	}
	// Trailing slashes are trimmed from both origins.
	if cfg.ShareOrigin() != "https://sidiora.fun" {
		t.Errorf("ShareOrigin = %q", cfg.ShareOrigin())
	}
	if cfg.OGOrigin() != "https://og.sidiora.fun" {
		t.Errorf("OGOrigin = %q", cfg.OGOrigin())
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("WEBHOOK_HMAC_SECRET", "too-short")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for short WEBHOOK_HMAC_SECRET")
	}
}

func TestLoadRejectsBadAddress(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ROUTER_ADDRESS", "not-an-address")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for invalid ROUTER_ADDRESS")
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
	setBaseEnv(t)
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
