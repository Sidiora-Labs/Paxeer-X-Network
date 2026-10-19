package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	testDatabaseURL = "postgres://perps:p4ss@db/perps"
	testNATSURL     = "nats://indexer:n4ts@nats:4222"
	testSecret32    = "0123456789abcdef0123456789abcdef"
)

var allVars = []string{
	envChainID, envRPCURLs, envCometURLs, envDatabaseURL, envNATSURL, envPort,
	envAuthSecret, envExplorerTx, envGatewayURL, envStartHeight,
}

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range allVars {
		t.Setenv(k, vars[k])
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		envChainID:     "125",
		envRPCURLs:     "https://rpc-a.example, http://rpc-b.example",
		envCometURLs:   "https://comet.example",
		envDatabaseURL: testDatabaseURL,
		envNATSURL:     testNATSURL,
		envPort:        "8080",
		envAuthSecret:  testSecret32,
	}
}

func with(base map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for bk, bv := range base {
		out[bk] = bv
	}
	out[k] = v
	return out
}

func refuse(t *testing.T, service Service, want string) {
	t.Helper()
	cfg, err := Load(service)
	if err == nil {
		t.Fatalf("Load(%s) = %v, want error %q", service, cfg, want)
	}
	if err.Error() != want {
		t.Fatalf("Load(%s) error = %q, want %q", service, err.Error(), want)
	}
	if cfg != nil {
		t.Fatalf("Load(%s) returned a Config with error %q", service, err)
	}
}

func TestConfig(t *testing.T) {
	t.Run("loads_all", func(t *testing.T) {
		setEnv(t, with(with(baseEnv(), envStartHeight, "42"), envExplorerTx, "https://explorer.example/tx/"))
		cfg, err := Load(Indexer)
		if err != nil {
			t.Fatalf("Load(indexer): %v", err)
		}
		if cfg.ChainID != 125 || cfg.Port != 8080 || cfg.StartHeight != 42 {
			t.Fatalf("ChainID=%d Port=%d StartHeight=%d, want 125 8080 42", cfg.ChainID, cfg.Port, cfg.StartHeight)
		}
		if strings.Join(cfg.RPCURLs, " ") != "https://rpc-a.example http://rpc-b.example" {
			t.Fatalf("RPCURLs = %q", cfg.RPCURLs)
		}
		if len(cfg.CometURLs) != 1 || cfg.CometURLs[0] != "https://comet.example" {
			t.Fatalf("CometURLs = %q", cfg.CometURLs)
		}
		if cfg.DatabaseURL != testDatabaseURL || cfg.NATSURL != testNATSURL {
			t.Fatalf("DatabaseURL or NATSURL not loaded")
		}
		if cfg.AuthSecret != "" || cfg.ExplorerTxBase != "" || cfg.GatewayURL != "" {
			t.Fatalf("indexer read variables it does not use")
		}

		setEnv(t, map[string]string{
			envChainID:     "125",
			envRPCURLs:     "https://rpc.example",
			envDatabaseURL: testDatabaseURL,
			envNATSURL:     testNATSURL,
			envPort:        "8080",
			envAuthSecret:  testSecret32,
		})
		gw, err := Load(Gateway)
		if err != nil {
			t.Fatalf("Load(gateway): %v", err)
		}
		if gw.ChainID != 125 || gw.Port != 8080 || gw.AuthSecret != testSecret32 || gw.CometURLs != nil {
			t.Fatalf("gateway Config = %v", gw)
		}

		setEnv(t, map[string]string{
			envChainID:    "125",
			envPort:       "3000",
			envGatewayURL: "wss://gateway.example/ws",
			envExplorerTx: "https://explorer.example/tx/",
		})
		portal, err := Load(Portal)
		if err != nil {
			t.Fatalf("Load(portal): %v", err)
		}
		if portal.GatewayURL != "wss://gateway.example/ws" || portal.ExplorerTxBase != "https://explorer.example/tx/" || portal.Port != 3000 {
			t.Fatalf("portal Config = %v", portal)
		}

		setEnv(t, map[string]string{
			envChainID:     "125",
			envRPCURLs:     "https://rpc.example",
			envCometURLs:   "https://comet.example",
			envDatabaseURL: testDatabaseURL,
		})
		replay, err := Load(Replay)
		if err != nil {
			t.Fatalf("Load(replay): %v", err)
		}
		if replay.Port != 0 || replay.NATSURL != "" {
			t.Fatalf("replay Config = %v", replay)
		}
	})

	t.Run("refuses_missing_chain", func(t *testing.T) {
		setEnv(t, with(baseEnv(), envChainID, ""))
		refuse(t, Indexer, "PERPS_CHAIN_ID is required")
		setEnv(t, with(baseEnv(), envChainID, "paxeer"))
		refuse(t, Indexer, "PERPS_CHAIN_ID must be numeric")
		setEnv(t, with(baseEnv(), envChainID, "0"))
		refuse(t, Gateway, "PERPS_CHAIN_ID is required")
	})

	t.Run("refuses_bad_rpc", func(t *testing.T) {
		setEnv(t, with(baseEnv(), envRPCURLs, "ftp://node"))
		refuse(t, Indexer, "PERPS_RPC_URLS needs http or https")
		setEnv(t, with(baseEnv(), envRPCURLs, "https://rpc.example,ws://node"))
		refuse(t, Gateway, "PERPS_RPC_URLS needs http or https")
		setEnv(t, with(baseEnv(), envRPCURLs, " , "))
		refuse(t, Replay, "PERPS_RPC_URLS needs http or https")
		setEnv(t, with(baseEnv(), envRPCURLs, "https://"))
		refuse(t, Indexer, "PERPS_RPC_URLS needs http or https")
	})

	t.Run("refuses_bad_comet", func(t *testing.T) {
		setEnv(t, with(baseEnv(), envCometURLs, "ws://node"))
		refuse(t, Indexer, "PERPS_COMET_URLS needs http or https")
		setEnv(t, with(baseEnv(), envCometURLs, ""))
		refuse(t, Replay, "PERPS_COMET_URLS needs http or https")
		setEnv(t, with(baseEnv(), envCometURLs, "file:///var/comet"))
		refuse(t, Indexer, "PERPS_COMET_URLS needs http or https")
	})

	t.Run("refuses_bad_port", func(t *testing.T) {
		setEnv(t, with(baseEnv(), envPort, "eighty"))
		refuse(t, Indexer, "PORT must be numeric")
		setEnv(t, with(baseEnv(), envPort, "-1"))
		refuse(t, Gateway, "PORT must be numeric")
		setEnv(t, with(baseEnv(), envPort, "65536"))
		refuse(t, Gateway, "PORT must be at most 65535")
		setEnv(t, with(baseEnv(), envPort, ""))
		refuse(t, Indexer, "PORT is required")
		setEnv(t, with(baseEnv(), envStartHeight, "-1"))
		refuse(t, Indexer, "PERPS_START_HEIGHT must not be below zero")
		setEnv(t, with(baseEnv(), envStartHeight, "tip"))
		refuse(t, Indexer, "PERPS_START_HEIGHT must be numeric")
	})

	t.Run("auth_secret_rule", func(t *testing.T) {
		setEnv(t, with(baseEnv(), envAuthSecret, testSecret32[:31]))
		refuse(t, Gateway, "PERPS_AUTH_SECRET must be at least 32 bytes")
		setEnv(t, with(baseEnv(), envAuthSecret, ""))
		refuse(t, Gateway, "PERPS_AUTH_SECRET must be at least 32 bytes")
		if _, err := Load(Indexer); err != nil {
			t.Fatalf("indexer needs no PERPS_AUTH_SECRET: %v", err)
		}
		setEnv(t, with(baseEnv(), envAuthSecret, testSecret32))
		cfg, err := Load(Gateway)
		if err != nil {
			t.Fatalf("Load(gateway) with 32 bytes: %v", err)
		}
		out := cfg.String()
		if !strings.Contains(out, "PERPS_AUTH_SECRET=redacted") || strings.Contains(out, testSecret32) {
			t.Fatalf("String() = %q, want the secret redacted", out)
		}
	})

	t.Run("redacts_secrets", func(t *testing.T) {
		setEnv(t, baseEnv())
		cfg, err := Load(Gateway)
		if err != nil {
			t.Fatalf("Load(gateway): %v", err)
		}
		js, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		for _, out := range []string{fmt.Sprint(cfg), fmt.Sprint(*cfg), fmt.Sprintf("%+v", *cfg), fmt.Sprintf("%#v", *cfg), cfg.LogValue().String(), string(js)} {
			for _, leak := range []string{"p4ss", "n4ts", testSecret32} {
				if strings.Contains(out, leak) {
					t.Fatalf("output %q leaks %q", out, leak)
				}
			}
		}
		out := fmt.Sprint(cfg)
		for _, want := range []string{"DATABASE_URL=redacted", "NATS_URL=redacted", "PERPS_AUTH_SECRET=redacted", "PERPS_CHAIN_ID=125", "PORT=8080"} {
			if !strings.Contains(out, want) {
				t.Fatalf("String() = %q, missing %q", out, want)
			}
		}
	})
}
