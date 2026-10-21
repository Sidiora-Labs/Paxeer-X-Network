package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
)

// validEnv returns the full set of env vars required for a successful Load.
// Tests mutate a copy to exercise individual failure modes.
func validEnv() map[string]string {
	// Contract addresses must equal the reviewed chain-125 deployment manifest
	// (shared/config.BaseEnv.Validate rejects any other address).
	roles := deployment.Current().Roles
	return map[string]string{
		"DATABASE_URL":            "postgres://u:p@localhost:5432/ranking",
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
	}
}

func applyEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	applyEnv(t, validEnv())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.RankingTickIntervalMS != 30000 {
		t.Errorf("RankingTickIntervalMS default = %d, want 30000", cfg.RankingTickIntervalMS)
	}
	if cfg.RankingNewTickIntervalMS != 10000 {
		t.Errorf("RankingNewTickIntervalMS default = %d, want 10000", cfg.RankingNewTickIntervalMS)
	}
	if cfg.RankingMaxEntries != 200 {
		t.Errorf("RankingMaxEntries default = %d, want 200", cfg.RankingMaxEntries)
	}
	if cfg.TickInterval() != 30*time.Second {
		t.Errorf("TickInterval() = %v, want 30s", cfg.TickInterval())
	}
	if cfg.NewTickInterval() != 10*time.Second {
		t.Errorf("NewTickInterval() = %v, want 10s", cfg.NewTickInterval())
	}
}

func TestLoadOverrides(t *testing.T) {
	env := validEnv()
	env["RANKING_TICK_INTERVAL_MS"] = "5000"
	env["RANKING_NEW_TICK_INTERVAL_MS"] = "2000"
	env["RANKING_MAX_ENTRIES"] = "50"
	env["CORS_ALLOWED_ORIGINS"] = "https://app.sidiora.fun"
	applyEnv(t, env)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RankingTickIntervalMS != 5000 || cfg.RankingNewTickIntervalMS != 2000 {
		t.Errorf("intervals = %d/%d, want 5000/2000", cfg.RankingTickIntervalMS, cfg.RankingNewTickIntervalMS)
	}
	if cfg.RankingMaxEntries != 50 {
		t.Errorf("RankingMaxEntries = %d, want 50", cfg.RankingMaxEntries)
	}
	if cfg.CORSAllowedOrigins != "https://app.sidiora.fun" {
		t.Errorf("CORS = %q", cfg.CORSAllowedOrigins)
	}
}

func TestLoadInvalidIntervals(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{"tick", "RANKING_TICK_INTERVAL_MS", "0", "RANKING_TICK_INTERVAL_MS"},
		{"newtick", "RANKING_NEW_TICK_INTERVAL_MS", "-1", "RANKING_NEW_TICK_INTERVAL_MS"},
		{"maxentries", "RANKING_MAX_ENTRIES", "0", "RANKING_MAX_ENTRIES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.val
			applyEnv(t, env)

			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load: expected error mentioning %q, got %v", tc.want, err)
			}
		})
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
