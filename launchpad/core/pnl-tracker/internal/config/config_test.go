package config_test

import (
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
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
