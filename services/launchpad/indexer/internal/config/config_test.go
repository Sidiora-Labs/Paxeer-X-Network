package config

import (
	"strings"
	"testing"

	"encoding/base64"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// setBaseEnv populates every required base + indexer env var for a valid paxscan
// config, then lets the caller override individual vars per case. Uses t.Setenv
// (so these tests are not parallel — the process env is global).
func setBaseEnv(t *testing.T) {
	t.Helper()
	// Contract addresses must equal the reviewed chain-125 deployment manifest
	// (shared/config.BaseEnv.Validate rejects any other address).
	roles := deployment.Current().Roles
	vars := map[string]string{
		"DATABASE_URL":            "postgres://u:p@localhost:5432/db",
		"REDIS_URL":               "redis://localhost:6379",
		"REDIS_BULL_URL":          "redis://localhost:6379/1",
		"RPC_URL":                 "http://localhost:8545",
		"RPC_URL_FALLBACK":        "",
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
		"LOG_LEVEL":               "info",
		"NODE_ENV":                "test",
		"PORT":                    "3000",

		// Indexer-specific: default-friendly paxscan config. Clear every
		// optional var so the host environment can't leak into assertions.
		"PAXSCAN_DATABASE_URL":     "postgres://u:p@localhost:5432/blockscout",
		"INDEXER_LOG_SOURCE":       "paxscan",
		"INDEXER_HEAD_SOURCE":      "",
		"INDEXER_MODE":             "live",
		"INDEXER_START_BLOCK":      "",
		"INDEXER_BATCH_SIZE":       "",
		"INDEXER_POLL_INTERVAL_MS": "",
		"INDEXER_CONCURRENCY":      "",
		"WEBHOOK_URLS":             "",
		"WEBHOOK_HMAC_SECRET":      "",
		"WEBHOOK_HMAC_SECRETS":     "",
		"BACKFILL_FROM_BLOCK":      "",
		"BACKFILL_BATCH_SIZE":      "",
		"BACKFILL_ONLY":            "",
		"RPC_URLS":                 "",
		"CORS_ALLOWED_ORIGINS":     "",
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IndexerBatchSize != 50 {
		t.Errorf("IndexerBatchSize = %d, want 50", cfg.IndexerBatchSize)
	}
	if cfg.IndexerPollIntervalMS != 100 {
		t.Errorf("IndexerPollIntervalMS = %d, want 100", cfg.IndexerPollIntervalMS)
	}
	if cfg.IndexerConcurrency != 25 {
		t.Errorf("IndexerConcurrency = %d, want 25", cfg.IndexerConcurrency)
	}
	if cfg.IndexerMode != ModeLive {
		t.Errorf("IndexerMode = %q, want live", cfg.IndexerMode)
	}
	if cfg.BackfillBatchSize != 2000 {
		t.Errorf("BackfillBatchSize = %d, want 2000", cfg.BackfillBatchSize)
	}
	if cfg.RPCGetLogsTimeoutMS != 5000 || cfg.RPCGetLogsStaleThreshold != 50 ||
		cfg.RPCGetLogsHealthIntervalMS != 30000 || cfg.RPCGetLogsReceiptConcurrency != 10 {
		t.Errorf("rpc-getlogs defaults wrong: %+v", cfg)
	}
	if cfg.BackfillFromBlock != nil {
		t.Errorf("BackfillFromBlock = %v, want nil", *cfg.BackfillFromBlock)
	}
	if cfg.ChainID != 125 {
		t.Errorf("ChainID = %d, want 125", cfg.ChainID)
	}
	if got := cfg.HeadSource(); got != SourcePaxscan {
		t.Errorf("HeadSource() = %q, want paxscan (falls back to log source)", got)
	}
}

func TestLoadHeadSourceOverride(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("INDEXER_HEAD_SOURCE", "rpc-getlogs")
	t.Setenv("RPC_URLS", "http://a:8545,http://b:8545")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.HeadSource(); got != SourceRPCGetLogs {
		t.Errorf("HeadSource() = %q, want rpc-getlogs", got)
	}
	if len(cfg.RPCURLs) != 2 {
		t.Errorf("RPCURLs = %v, want 2 entries", cfg.RPCURLs)
	}
}

func TestLoadBackfillFromBlock(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("BACKFILL_FROM_BLOCK", "12345")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BackfillFromBlock == nil || *cfg.BackfillFromBlock != 12345 {
		t.Errorf("BackfillFromBlock = %v, want 12345", cfg.BackfillFromBlock)
	}
}

func TestLoadCleanListTrims(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("INDEXER_LOG_SOURCE", "rpc-getlogs")
	t.Setenv("RPC_URLS", " http://a:8545 , , http://b:8545 ")
	t.Setenv("WEBHOOK_URLS", " https://x/hook , ")
	t.Setenv("WEBHOOK_HMAC_SECRET", "s3cret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.RPCURLs) != 2 || cfg.RPCURLs[0] != "http://a:8545" || cfg.RPCURLs[1] != "http://b:8545" {
		t.Errorf("RPCURLs not trimmed/filtered: %v", cfg.RPCURLs)
	}
	if len(cfg.WebhookURLs) != 1 || cfg.WebhookURLs[0] != "https://x/hook" {
		t.Errorf("WebhookURLs not trimmed/filtered: %v", cfg.WebhookURLs)
	}
}

func TestLoadWebhookMatchedSecrets(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("WEBHOOK_URLS", "https://a/hook,https://b/hook")
	t.Setenv("WEBHOOK_HMAC_SECRETS", "s1,s2")
	if _, err := Load(); err != nil {
		t.Fatalf("Load with matched secrets: %v", err)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T)
		want   string
	}{
		{"bad mode", func(t *testing.T) { t.Setenv("INDEXER_MODE", "sideways") }, "INDEXER_MODE"},
		{"bad log source", func(t *testing.T) { t.Setenv("INDEXER_LOG_SOURCE", "carrier-pigeon") }, "INDEXER_LOG_SOURCE"},
		{"bad head source", func(t *testing.T) { t.Setenv("INDEXER_HEAD_SOURCE", "smoke-signal") }, "INDEXER_HEAD_SOURCE"},
		{"paxscan needs dsn", func(t *testing.T) { t.Setenv("PAXSCAN_DATABASE_URL", "") }, "PAXSCAN_DATABASE_URL is required"},
		{"bad paxscan url", func(t *testing.T) {
			t.Setenv("INDEXER_LOG_SOURCE", "rpc")
			t.Setenv("PAXSCAN_DATABASE_URL", "not a url")
		}, "not a valid URL"},
		{"rpc-getlogs needs urls", func(t *testing.T) {
			t.Setenv("INDEXER_LOG_SOURCE", "rpc-getlogs")
			t.Setenv("RPC_URLS", "")
		}, "RPC_URLS"},
		{"head rpc-getlogs needs urls", func(t *testing.T) {
			t.Setenv("INDEXER_HEAD_SOURCE", "rpc-getlogs")
			t.Setenv("RPC_URLS", "")
		}, "INDEXER_HEAD_SOURCE=rpc-getlogs"},
		{"webhook needs secret", func(t *testing.T) {
			t.Setenv("WEBHOOK_URLS", "https://a/hook")
		}, "WEBHOOK_HMAC_SECRET"},
		{"webhook secrets length mismatch", func(t *testing.T) {
			t.Setenv("WEBHOOK_URLS", "https://a/hook,https://b/hook")
			t.Setenv("WEBHOOK_HMAC_SECRETS", "only-one")
		}, "must equal WEBHOOK_URLS length"},
		{"bad base address", func(t *testing.T) { t.Setenv("ROUTER_ADDRESS", "0xnothex") }, "ROUTER_ADDRESS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBaseEnv(t)
			tc.mutate(t)
			_, err := Load()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestLoadParseError(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("INDEXER_BATCH_SIZE", "not-an-int")
	if _, err := Load(); err == nil {
		t.Fatal("want parse error for non-integer INDEXER_BATCH_SIZE")
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
	cfg, err := Load()
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
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS and NATS_CREDENTIALS_CREDS are both set") {
		t.Fatalf("both NATS credential sources accepted: %v", err)
	}
	t.Setenv("NATS_CREDENTIALS", "")
	t.Setenv("NATS_CREDENTIALS_CREDS", "")
	t.Setenv("NODE_ENV", "production")
	t.Setenv("RUNTIME_PROFILE", "container")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS or NATS_CREDENTIALS_CREDS is required") {
		t.Fatalf("missing NATS credentials accepted in a production profile: %v", err)
	}
}
