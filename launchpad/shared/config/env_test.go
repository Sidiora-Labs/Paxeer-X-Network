package config

import (
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"strings"
	"testing"
)

// validEnv sets a complete, valid base environment via t.Setenv.
func validEnv(t *testing.T) {
	t.Helper()
	m := deployment.Current()
	names := map[string]string{"EVENT_EMITTER_ADDRESS": "EventEmitter", "POOL_REGISTRY_ADDRESS": "PoolRegistry", "ROUTER_ADDRESS": "Router", "FACTORY_ADDRESS": "SidioraFactory", "QUOTER_ADDRESS": "Quoter", "PROTOCOL_CONFIG_ADDRESS": "ProtocolConfig", "FEE_ACCUMULATOR_ADDRESS": "FeeAccumulator", "SIDIORA_NFT_ADDRESS": "SidioraNFT", "FEES_ROUTER_ADDRESS": "FeesRouter", "POOL_BEACON_ADDRESS": "PoolBeacon"}
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("REDIS_BULL_URL", "redis://localhost:6379/1")
	t.Setenv("RPC_URL", "https://mainnet-beta.rpc.hyperpaxeer.com/rpc")
	for _, k := range []string{
		"EVENT_EMITTER_ADDRESS", "POOL_REGISTRY_ADDRESS", "ROUTER_ADDRESS",
		"FACTORY_ADDRESS", "QUOTER_ADDRESS", "PROTOCOL_CONFIG_ADDRESS",
		"FEE_ACCUMULATOR_ADDRESS", "SIDIORA_NFT_ADDRESS", "FEES_ROUTER_ADDRESS",
		"POOL_BEACON_ADDRESS",
	} {
		t.Setenv(k, m.Roles[names[k]].Address)
	}
}

func TestLoadValidWithDefaults(t *testing.T) {
	validEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ChainID != 125 {
		t.Errorf("ChainID default = %d, want 125", cfg.ChainID)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
	if cfg.NodeEnv != "production" {
		t.Errorf("NodeEnv default = %q, want production", cfg.NodeEnv)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port default = %d, want 3000", cfg.Port)
	}
	if cfg.RPCURLFallback != "" {
		t.Errorf("RPCURLFallback = %q, want empty (optional)", cfg.RPCURLFallback)
	}
}

func TestLoadCoercesAndOverrides(t *testing.T) {
	validEnv(t)
	t.Setenv("CHAIN_ID", "125")
	t.Setenv("PORT", "8080")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("NODE_ENV", "development")
	t.Setenv("RPC_URL_FALLBACK", "https://fallback.example.com/rpc")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ChainID != 125 || cfg.Port != 8080 {
		t.Errorf("coercion failed: ChainID=%d Port=%d", cfg.ChainID, cfg.Port)
	}
	if cfg.LogLevel != "debug" || cfg.NodeEnv != "development" {
		t.Errorf("override failed: LogLevel=%q NodeEnv=%q", cfg.LogLevel, cfg.NodeEnv)
	}
	if cfg.RPCURLFallback == "" {
		t.Error("RPCURLFallback should be set")
	}
}

func TestLoadMissingRequired(t *testing.T) {
	validEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestValidateRejectsBadAddress(t *testing.T) {
	validEnv(t)
	t.Setenv("ROUTER_ADDRESS", "0xnothex")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed ROUTER_ADDRESS")
	}
}

func TestValidateRejectsBadURL(t *testing.T) {
	validEnv(t)
	t.Setenv("RPC_URL", "not-a-url")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed RPC_URL")
	}
}

func TestValidateRejectsBadFallbackURL(t *testing.T) {
	validEnv(t)
	t.Setenv("RPC_URL_FALLBACK", "::::bad")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed RPC_URL_FALLBACK")
	}
}

func TestValidateRejectsBadEnums(t *testing.T) {
	validEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL")
	}
}

func TestValidateRejectsBadNodeEnv(t *testing.T) {
	validEnv(t)
	t.Setenv("NODE_ENV", "staging")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid NODE_ENV")
	}
}

func TestRejectsWrongChainStaleAddressesAndBounds(t *testing.T) {
	for _, tc := range []struct{ name, key, value, want string }{
		{"chain", "CHAIN_ID", "999", "wrong_chain"}, {"June router", "ROUTER_ADDRESS", "0x6679aF411d534de222C32ed0AF94C3BD67090672", "Router: address_mismatch"}, {"port low", "PORT", "0", "out_of_range"}, {"port high", "PORT", "65536", "out_of_range"}, {"redis scheme", "REDIS_URL", "https://localhost/", "url_scheme_invalid"}, {"database scheme", "DATABASE_URL", "https://localhost/", "url_scheme_invalid"}, {"rpc scheme", "RPC_URL", "file://localhost/etc/passwd", "url_scheme_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, err)
			}
		})
	}
}
func TestPrivateEnvironmentRedaction(t *testing.T) {
	validEnv(t)
	t.Setenv("RPC_URL", "https://operator:super-secret@rpc.example/private-key")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	for _, out := range []string{string(b), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(out, "super-secret") || strings.Contains(out, "operator") || strings.Contains(out, "private-key") {
			t.Fatal("secret leaked")
		}
	}
	t.Setenv("RPC_URL", "https://operator:super-secret@%%/")
	_, err = Load()
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("bad redaction: %v", err)
	}
}

func TestConflictingPrivateConfigNeverExports(t *testing.T) {
	validEnv(t)
	t.Setenv("RPC_URL", "https://operator:super-secret@private.example/rpc")
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	c.RouterAddress = "0x6679aF411d534de222C32ed0AF94C3BD67090672"
	out, e := c.ExportPublic(deployment.Current(), deployment.ActivationAttestation{}, deployment.Policy{}, deployment.PublicEndpoints{})
	if e == nil || out.ChainID != 0 || !strings.Contains(e.Error(), "Router") || strings.Contains(e.Error(), "super-secret") {
		t.Fatalf("bad export redaction: %+v %v", out, e)
	}
}

func TestEnvironmentParserAndEnumsDoNotEchoValues(t *testing.T) {
	for _, name := range []string{"PORT", "CHAIN_ID", "SECRETS_REFRESH_SECONDS", "LOG_LEVEL", "NODE_ENV"} {
		t.Run(name, func(t *testing.T) {
			validEnv(t)
			t.Setenv(name, "operator:private-secret@private.example")
			_, err := Load()
			if err == nil || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "operator") {
				t.Fatalf("unredacted validation: %v", err)
			}
		})
	}
	validEnv(t)
	t.Setenv("QUOTER_ADDRESS", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "QUOTER_ADDRESS") {
		t.Fatalf("missing role not actionable: %v", err)
	}
}
