// Package config loads and validates the core/indexer environment, mirroring the
// TS indexerEnvSchema (indexer/src/config.ts) one-to-one: identical env var
// names (invariant i8 — matches indexer/ecosystem.config.cjs / deploy env),
// defaults, and cross-field validation. It extends the shared base env
// (shared/config.BaseEnv) exactly as the TS schema extends baseEnvSchema.
//
// Meta-AG contract addresses are intentionally NOT modelled here: meta-ag is
// excluded from the new chain (L4), so the optional TRANSACTION_TRACKER_ADDRESS
// etc. fields are dropped.
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	runtimedb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	dbproduction "github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
)

// Log-source modes (INDEXER_LOG_SOURCE / INDEXER_HEAD_SOURCE).
const (
	SourcePaxscan    = "paxscan"
	SourceRPC        = "rpc"
	SourceRPCGetLogs = "rpc-getlogs"
)

// Indexer run modes (INDEXER_MODE).
const (
	ModeLive     = "live"
	ModeBackfill = "backfill"
)

// Config is the parsed indexer environment. The embedded BaseEnv carries the
// fields shared by every service (DATABASE_URL, REDIS_URL, RPC_URL, CHAIN_ID,
// the contract address book, LOG_LEVEL, PORT, ...). Field order follows
// config.ts.
type Config struct {
	LifecycleConfigFile     string `env:"INDEXER_LIFECYCLE_CONFIG_FILE"`
	LifecycleConfigDocument string `env:"INDEXER_LIFECYCLE_CONFIG_DOCUMENT"`
	LifecycleConfigSHA256   string `env:"INDEXER_LIFECYCLE_CONFIG_SHA256"`

	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	FixturePubAckBarrier      string `env:"FIXTURE_PUBACK_BARRIER"`
	NATSUsername              string `env:"NATS_USERNAME"`
	NATSPassword              string `env:"NATS_PASSWORD"`
	RuntimeProfile            string `env:"RUNTIME_PROFILE"`
	BindAddress               string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile         string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetDocument     string `env:"DB_BUDGET_DOCUMENT"`
	RuntimeBudgetSHA256       string `env:"DB_BUDGET_SHA256"`
	RuntimePoolID             string `env:"DB_POOL_ID"`
	RuntimeInstanceID         string `env:"INSTANCE_ID"`
	ExpectedSchemaFile        string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaDocument    string `env:"EXPECTED_SCHEMA_DOCUMENT"`
	ExpectedSchemaSHA256      string `env:"EXPECTED_SCHEMA_SHA256"`
	ReceiptConfigFile         string `env:"INDEXER_RECEIPT_CONFIG_FILE"`
	ReceiptConfigDocument     string `env:"INDEXER_RECEIPT_CONFIG_DOCUMENT"`
	ReceiptConfigSHA256       string `env:"INDEXER_RECEIPT_CONFIG_SHA256"`
	DeliveryConfigFile        string `env:"DELIVERY_CONFIG_FILE"`
	DeliveryConfigDocument    string `env:"DELIVERY_CONFIG_DOCUMENT"`
	DeliveryConfigSHA256      string `env:"DELIVERY_CONFIG_SHA256"`
	NATSURL                   string `env:"NATS_URL"`
	NATSCredentials           string `env:"NATS_CREDENTIALS"`
	NATSCredentialsCreds      string `env:"NATS_CREDENTIALS_CREDS"`

	sharedconfig.BaseEnv

	IndexerStartBlock     int64 `env:"INDEXER_START_BLOCK" envDefault:"0"`
	IndexerBatchSize      int   `env:"INDEXER_BATCH_SIZE" envDefault:"50"`
	IndexerPollIntervalMS int   `env:"INDEXER_POLL_INTERVAL_MS" envDefault:"100"`
	IndexerConcurrency    int   `env:"INDEXER_CONCURRENCY" envDefault:"25"`

	// Webhook fan-out targets + signing (matched-index CSV arrays).
	WebhookURLs        []string `env:"WEBHOOK_URLS" envSeparator:","`
	WebhookHMACSecret  string   `env:"WEBHOOK_HMAC_SECRET"`
	WebhookHMACSecrets []string `env:"WEBHOOK_HMAC_SECRETS" envSeparator:","`

	// Backfill configuration.
	IndexerMode         string `env:"INDEXER_MODE" envDefault:"live"`
	BackfillFromBlock   *int64 `env:"BACKFILL_FROM_BLOCK"`
	BackfillBatchSize   int    `env:"BACKFILL_BATCH_SIZE" envDefault:"2000"`
	BackfillConcurrency int    `env:"BACKFILL_CONCURRENCY" envDefault:"20"`
	BackfillOnly        bool   `env:"BACKFILL_ONLY" envDefault:"false"`

	// Log-source selection.
	PaxscanDatabaseURL string `env:"PAXSCAN_DATABASE_URL"`
	IndexerLogSource   string `env:"INDEXER_LOG_SOURCE" envDefault:"paxscan"`
	IndexerHeadSource  string `env:"INDEXER_HEAD_SOURCE"`

	// HistoricalRPCURL is an optional archive JSON-RPC endpoint for chain 125.
	// The primary RPC_URL may be a pruned node (Paxeer keeps ~100k blocks);
	// production then serves current blocks from RPC_URL and asks this endpoint
	// only for reads the primary reports as pruned (anchor/checkpoint
	// verification after downtime, replay/backfill below the pruning floor).
	HistoricalRPCURL string `env:"INDEXER_HISTORICAL_RPC_URL"`
	// Archive-rate backfill tuning (ranges below the primary's pruning floor):
	// requests in flight per archive client (the committed source and the
	// receipt authority each hold this many), blocks read ahead of the range
	// being qualified (0 disables readahead), and the per-attempt archive
	// timeout after which an unanswered request is retried.
	HistoricalConcurrency      int `env:"INDEXER_HISTORICAL_CONCURRENCY" envDefault:"48"`
	HistoricalReadahead        int `env:"INDEXER_HISTORICAL_READAHEAD" envDefault:"256"`
	HistoricalAttemptTimeoutMS int `env:"INDEXER_HISTORICAL_ATTEMPT_TIMEOUT_MS" envDefault:"8000"`

	// rpc-getlogs configuration.
	RPCURLs                      []string `env:"RPC_URLS" envSeparator:","`
	RPCGetLogsTimeoutMS          int      `env:"RPC_GETLOGS_TIMEOUT_MS" envDefault:"5000"`
	RPCGetLogsStaleThreshold     int      `env:"RPC_GETLOGS_STALE_THRESHOLD" envDefault:"50"`
	RPCGetLogsHealthIntervalMS   int      `env:"RPC_GETLOGS_HEALTH_INTERVAL_MS" envDefault:"30000"`
	RPCGetLogsReceiptConcurrency int      `env:"RPC_GETLOGS_RECEIPT_CONCURRENCY" envDefault:"10"`

	// Optional CORS allowlist for the status server (comma-separated; empty/"*"
	// allows all). Not in the TS indexer schema; added for the Go status edge.
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`
}

var (
	validModes   = map[string]struct{}{ModeLive: {}, ModeBackfill: {}}
	validSources = map[string]struct{}{SourcePaxscan: {}, SourceRPC: {}, SourceRPCGetLogs: {}}
)

// Load parses the process environment into a validated Config.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err = cfg.registerInlineDocuments(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.WebhookURLs = cleanList(cfg.WebhookURLs)
	cfg.WebhookHMACSecrets = cleanList(cfg.WebhookHMACSecrets)
	cfg.RPCURLs = cleanList(cfg.RPCURLs)
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// HeadSource resolves the effective chain-head source: INDEXER_HEAD_SOURCE when
// set, otherwise INDEXER_LOG_SOURCE (parity with config.ts).
// ArchiveTuning is the validated archive-rate backfill tuning. Readahead 0
// (disabled) is reported as -1, the sources' "disabled" value.
type ArchiveTuning struct {
	Concurrency, Readahead int
	AttemptTimeout         time.Duration
}

func (c *Config) ArchiveTuning() (ArchiveTuning, error) {
	if c.HistoricalConcurrency == 0 {
		c.HistoricalConcurrency = 48
	}
	if c.HistoricalAttemptTimeoutMS == 0 {
		c.HistoricalAttemptTimeoutMS = 8000
	}
	if c.HistoricalConcurrency < 1 || c.HistoricalConcurrency > 96 {
		return ArchiveTuning{}, errors.New("config: INDEXER_HISTORICAL_CONCURRENCY must be 1..96")
	}
	if c.HistoricalReadahead < 0 || c.HistoricalReadahead > 1024 {
		return ArchiveTuning{}, errors.New("config: INDEXER_HISTORICAL_READAHEAD must be 0..1024")
	}
	if c.HistoricalAttemptTimeoutMS < 500 || c.HistoricalAttemptTimeoutMS > 15000 {
		return ArchiveTuning{}, errors.New("config: INDEXER_HISTORICAL_ATTEMPT_TIMEOUT_MS must be 500..15000")
	}
	t := ArchiveTuning{Concurrency: c.HistoricalConcurrency, Readahead: c.HistoricalReadahead, AttemptTimeout: time.Duration(c.HistoricalAttemptTimeoutMS) * time.Millisecond}
	if t.Readahead == 0 {
		t.Readahead = -1
	}
	return t, nil
}

func (c *Config) HeadSource() string {
	if c.IndexerHeadSource != "" {
		return c.IndexerHeadSource
	}
	return c.IndexerLogSource
}

// Validate enforces the same field-level + cross-field rules as the zod
// indexerEnvSchema (including its superRefine block).
func (c *Config) Validate() error {
	if c.NodeEnv == "production" && ((c.LifecycleConfigFile == "" && c.LifecycleConfigDocument == "") || len(c.LifecycleConfigSHA256) != 64) {
		return errors.New("production indexer lifecycle document binding required")
	}

	var errs []error
	if err := c.BaseEnv.Validate(); err != nil {
		errs = append(errs, err)
	}

	if _, ok := validModes[c.IndexerMode]; !ok {
		errs = append(errs, fmt.Errorf("INDEXER_MODE %q must be one of live|backfill", c.IndexerMode))
	}
	if _, ok := validSources[c.IndexerLogSource]; !ok {
		errs = append(errs, fmt.Errorf("INDEXER_LOG_SOURCE %q must be one of paxscan|rpc|rpc-getlogs", c.IndexerLogSource))
	}
	if c.IndexerHeadSource != "" {
		if _, ok := validSources[c.IndexerHeadSource]; !ok {
			errs = append(errs, fmt.Errorf("INDEXER_HEAD_SOURCE %q must be one of paxscan|rpc|rpc-getlogs", c.IndexerHeadSource))
		}
	}
	if c.PaxscanDatabaseURL != "" {
		if err := requireURL("PAXSCAN_DATABASE_URL", c.PaxscanDatabaseURL); err != nil {
			errs = append(errs, err)
		}
	}
	if c.HistoricalRPCURL != "" {
		if err := requireHTTPURL("INDEXER_HISTORICAL_RPC_URL", c.HistoricalRPCURL); err != nil {
			errs = append(errs, err)
		}
	}

	// superRefine: log + head source dependencies.
	if c.NodeEnv != "production" && c.IndexerLogSource == SourcePaxscan && c.PaxscanDatabaseURL == "" {
		errs = append(errs, errors.New("PAXSCAN_DATABASE_URL is required when INDEXER_LOG_SOURCE=paxscan"))
	}
	if c.NodeEnv != "production" && c.IndexerLogSource == SourceRPCGetLogs && len(c.RPCURLs) == 0 {
		errs = append(errs, errors.New("RPC_URLS (comma-separated) is required when INDEXER_LOG_SOURCE=rpc-getlogs"))
	}
	head := c.HeadSource()
	if c.NodeEnv != "production" && head == SourcePaxscan && c.PaxscanDatabaseURL == "" {
		errs = append(errs, errors.New("PAXSCAN_DATABASE_URL is required when INDEXER_HEAD_SOURCE=paxscan"))
	}
	if c.NodeEnv != "production" && head == SourceRPCGetLogs && len(c.RPCURLs) == 0 {
		errs = append(errs, errors.New("RPC_URLS (comma-separated) is required when INDEXER_HEAD_SOURCE=rpc-getlogs"))
	}

	// superRefine: webhook signing validation.
	if len(c.WebhookURLs) > 0 {
		if len(c.WebhookHMACSecrets) == 0 && c.WebhookHMACSecret == "" {
			errs = append(errs, errors.New("WEBHOOK_HMAC_SECRET (or WEBHOOK_HMAC_SECRETS) is required when WEBHOOK_URLS is non-empty"))
		}
		if len(c.WebhookHMACSecrets) > 0 && len(c.WebhookHMACSecrets) != len(c.WebhookURLs) {
			errs = append(errs, fmt.Errorf("WEBHOOK_HMAC_SECRETS length (%d) must equal WEBHOOK_URLS length (%d)", len(c.WebhookHMACSecrets), len(c.WebhookURLs)))
		}
	}

	return errors.Join(errs...)
}

// cleanList trims whitespace and drops empty entries, mirroring the TS
// `.split(',').map(trim).filter(len>0)` transforms.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func requireHTTPURL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Fragment != "" {
		return fmt.Errorf("%s is not a valid http(s) URL", name)
	}
	return nil
}

func requireURL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s %q is not a valid URL", name, raw)
	}
	return nil
}

func (c *Config) ResolveProductionSecrets(ctx context.Context) (func(context.Context) error, context.CancelFunc, error) {
	if c.NodeEnv != "production" {
		if c.NodeEnv != "" && c.NodeEnv != "test" && c.NodeEnv != "development" {
			return nil, nil, fmt.Errorf("environment_invalid")
		}
		return nil, func() {}, nil
	}
	if c.SecretsRefreshSeconds < 1 || c.SecretsRefreshSeconds > 60 {
		return nil, nil, fmt.Errorf("secret_refresh_bound_invalid")
	}
	interval := time.Duration(c.SecretsRefreshSeconds) * time.Second

	provider, err := sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(c.SecretsProvider), CacheTTL: 0})
	if err != nil {
		return nil, nil, err
	}
	bindings := map[string]*string{}
	bindings["DATABASE_URL"] = &c.DatabaseURL
	bindings["REDIS_URL"] = &c.RedisURL
	bindings["RPC_URL"] = &c.RPCURL
	if c.RPCURLFallback != "" {
		bindings["RPC_URL_FALLBACK"] = &c.RPCURLFallback
	}
	if c.HistoricalRPCURL != "" {
		bindings["INDEXER_HISTORICAL_RPC_URL"] = &c.HistoricalRPCURL
	}
	if c.WebhookHMACSecret != "" {
		bindings["WEBHOOK_HMAC_SECRET"] = &c.WebhookHMACSecret
	}
	var webhookSecrets string
	if len(c.WebhookHMACSecrets) > 0 {
		bindings["WEBHOOK_HMAC_SECRETS"] = &webhookSecrets
	}
	bindings["NATS_URL"] = &c.NATSURL
	if c.DeploymentRealm == "private_fixture" {
		if c.RuntimeProfile != "owned_local" {
			return nil, nil, fmt.Errorf("fixture_nats_profile_mismatch")
		}
		if _, e := c.PrivateFixtureEvidence(); e != nil {
			return nil, nil, e
		}
		bindings["NATS_USERNAME"] = &c.NATSUsername
		bindings["NATS_PASSWORD"] = &c.NATSPassword
	} else if c.NATSCredentials != "" {
		bindings["NATS_CREDENTIALS"] = &c.NATSCredentials
	} else if c.NATSCredentialsCreds != "" {
		bindings["NATS_CREDENTIALS_CREDS"] = &c.NATSCredentialsCreds
	} else {
		return nil, nil, fmt.Errorf("required_secret_unavailable: NATS_CREDENTIALS or NATS_CREDENTIALS_CREDS")
	}
	consumed := map[string]string{}
	for key, target := range bindings {
		value, err := provider.GetRequired(ctx, key)
		if err != nil {
			return nil, nil, fmt.Errorf("required_secret_unavailable: %s", key)
		}
		*target = value
		consumed[key] = value
	}

	if webhookSecrets != "" {
		c.WebhookHMACSecrets = cleanList(strings.Split(webhookSecrets, ","))
		if len(c.WebhookHMACSecrets) != len(c.WebhookURLs) {
			return nil, nil, fmt.Errorf("webhook_secret_count_mismatch")
		}
	}
	var mu sync.Mutex
	var failure error
	check := func(checkCtx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if failure != nil {
			return failure
		}
		for key, old := range consumed {
			value, err := provider.GetRequired(checkCtx, key)
			if err != nil {
				failure = fmt.Errorf("required_secret_unavailable: %s", key)
				return failure
			}
			if value != old {
				failure = fmt.Errorf("required_secret_rotated_restart_required: %s", key)
				return failure
			}
		}
		return nil
	}
	refreshCtx, stop := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-ticker.C:
				probe, cancel := context.WithTimeout(refreshCtx, 2*time.Second)
				err := check(probe)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()
	return check, stop, nil
}

func (c *Config) OpenRuntimePool(ctx context.Context, role string, legacy runtimedb.PoolOptions) (*pgxpool.Pool, error) {
	if c.NodeEnv != "production" {
		return runtimedb.NewPool(ctx, c.DatabaseURL, legacy)
	}
	if c.RuntimeProfile == "container" {
		if c.Port != 8080 || c.BindAddress != "0.0.0.0" {
			return nil, fmt.Errorf("container_bind_mismatch")
		}
	} else if c.RuntimeProfile == "owned_local" {
		if c.BindAddress != "127.0.0.1" {
			return nil, fmt.Errorf("owned_loopback_required")
		}
	} else {
		return nil, fmt.Errorf("runtime_profile_required")
	}
	if (c.RuntimeBudgetFile == "" && c.RuntimeBudgetDocument == "") || c.RuntimePoolID == "" || c.RuntimeInstanceID == "" {
		return nil, fmt.Errorf("runtime_pool_configuration_missing")
	}
	data, err := boundBytes(c.RuntimeBudgetFile, c.RuntimeBudgetDocument, c.RuntimeBudgetSHA256)
	if err != nil {
		return nil, fmt.Errorf("runtime_budget_binding_mismatch")
	}
	budget, err := dbproduction.ParseConnectionBudget(data)
	if err != nil {
		return nil, err
	}
	options, err := budget.BudgetPoolOptions(role, c.RuntimePoolID, c.RuntimeInstanceID)
	if err != nil {
		return nil, err
	}
	if options.Transport != runtimedb.TransportDirect {
		return nil, fmt.Errorf("runtime_transaction_pooler_seam_unavailable")
	}
	pool, err := runtimedb.NewPool(ctx, c.DatabaseURL, options)
	if err != nil {
		return nil, err
	}
	if err = c.CheckRuntimeSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
func (c *Config) CheckRuntimeSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if c.NodeEnv != "production" {
		return pool.Ping(ctx)
	}
	if (c.ExpectedSchemaFile == "" && c.ExpectedSchemaDocument == "") || len(c.ExpectedSchemaSHA256) != 64 {
		return fmt.Errorf("runtime_schema_configuration_missing")
	}
	data, err := boundBytes(c.ExpectedSchemaFile, c.ExpectedSchemaDocument, c.ExpectedSchemaSHA256)
	if err != nil {
		return err
	}
	want, err := dbproduction.LoadExpectedSchema(data, c.ExpectedSchemaSHA256)
	if err != nil {
		return err
	}
	if want.Service != "core/indexer" {
		return fmt.Errorf("runtime_schema_service_mismatch")
	}
	return dbproduction.CheckReadiness(ctx, pool, want)
}

// inlineDocuments holds the inline *_DOCUMENT values of the last Load, keyed
// by their declared sha256, so ReadBoundDocument callers that pass only the
// file and digest read the inline document when no file is set.
var (
	inlineDocumentsMu sync.RWMutex
	inlineDocuments   = map[string]string{}
)

func (c *Config) registerInlineDocuments() error {
	if c.NATSCredentials != "" && c.NATSCredentialsCreds != "" {
		return fmt.Errorf("NATS_CREDENTIALS and NATS_CREDENTIALS_CREDS are both set")
	}
	if c.NodeEnv == "production" && c.RuntimeProfile != "" && c.DeploymentRealm != "private_fixture" && c.NATSCredentials == "" && c.NATSCredentialsCreds == "" {
		return fmt.Errorf("NATS_CREDENTIALS or NATS_CREDENTIALS_CREDS is required")
	}
	pairs := []struct{ name, file, inline, digest string }{
		{"DB_BUDGET", c.RuntimeBudgetFile, c.RuntimeBudgetDocument, c.RuntimeBudgetSHA256},
		{"EXPECTED_SCHEMA", c.ExpectedSchemaFile, c.ExpectedSchemaDocument, c.ExpectedSchemaSHA256},
		{"DELIVERY_CONFIG", c.DeliveryConfigFile, c.DeliveryConfigDocument, c.DeliveryConfigSHA256},
		{"INDEXER_RECEIPT_CONFIG", c.ReceiptConfigFile, c.ReceiptConfigDocument, c.ReceiptConfigSHA256},
		{"INDEXER_LIFECYCLE_CONFIG", c.LifecycleConfigFile, c.LifecycleConfigDocument, c.LifecycleConfigSHA256},
	}
	documents := map[string]string{}
	for _, p := range pairs {
		if p.file != "" && p.inline != "" {
			return fmt.Errorf("%s_FILE and %s_DOCUMENT are both set", p.name, p.name)
		}
		if p.inline != "" {
			documents[p.digest] = p.inline
		}
	}
	inlineDocumentsMu.Lock()
	inlineDocuments = documents
	inlineDocumentsMu.Unlock()
	return nil
}

func ReadBoundDocument(path, digest string, out any) error {
	inline := ""
	if path == "" {
		inlineDocumentsMu.RLock()
		inline = inlineDocuments[digest]
		inlineDocumentsMu.RUnlock()
	}
	return decodeBoundDocument(path, inline, digest, out)
}

// boundBytes returns the exact bytes of a digest-bound runtime document,
// read from the file at path or decoded from inline (standard base64), and
// verifies them against the sha256 hex digest. Exactly one source may be set.
func boundBytes(path, inline, digest string) ([]byte, error) {
	if (path == "") == (inline == "") || len(digest) != 64 {
		return nil, fmt.Errorf("runtime_document_binding_missing")
	}
	var raw []byte
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1048576 {
			return nil, fmt.Errorf("runtime_document_invalid")
		}
		if raw, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	} else {
		if len(inline) > 1398104 {
			return nil, fmt.Errorf("runtime_document_invalid")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(inline)
		if err != nil {
			return nil, fmt.Errorf("runtime_document_invalid")
		}
		raw = decoded
	}
	if len(raw) == 0 || len(raw) > 1048576 {
		return nil, fmt.Errorf("runtime_document_invalid")
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return nil, fmt.Errorf("runtime_document_binding_mismatch")
	}
	return raw, nil
}

func decodeBoundDocument(path, inline, digest string, out any) error {
	raw, err := boundBytes(path, inline, digest)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return fmt.Errorf("runtime_document_trailing_data")
	}
	return nil
}

func (c *Config) RuntimeNATSOptions() ([]nats.Option, error) {
	options := []nats.Option{nats.Timeout(2 * time.Second), nats.Name(c.RuntimeInstanceID)}
	if c.DeploymentRealm == "private_fixture" {
		if c.RuntimeProfile != "owned_local" || c.NATSUsername == "" || c.NATSPassword == "" {
			return nil, fmt.Errorf("fixture_broker_credentials_missing")
		}
		if _, err := c.PrivateFixtureEvidence(); err != nil {
			return nil, err
		}
		return append(options, nats.UserInfo(c.NATSUsername, c.NATSPassword)), nil
	}
	if c.NATSCredentialsCreds != "" {
		credentials, err := inlineNATSCredentials(c.NATSCredentialsCreds)
		if err != nil {
			return nil, err
		}
		return append(options, credentials), nil
	}
	if c.NATSCredentials == "" {
		return nil, fmt.Errorf("broker_credentials_missing")
	}
	return append(options, nats.UserCredentials(c.NATSCredentials)), nil
}

// inlineNATSCredentials turns the content of a NATS .creds file
// (NATS_CREDENTIALS_CREDS) into the same user JWT and seed option
// nats.UserCredentials builds from the file form.
func inlineNATSCredentials(creds string) (nats.Option, error) {
	if len(creds) > 1048576 {
		return nil, fmt.Errorf("broker_credentials_invalid: NATS_CREDENTIALS_CREDS")
	}
	userJWT, err := nkeys.ParseDecoratedJWT([]byte(creds))
	if err != nil || strings.Count(userJWT, ".") != 2 || strings.ContainsAny(userJWT, " \t\r\n") {
		return nil, fmt.Errorf("broker_credentials_invalid: NATS_CREDENTIALS_CREDS")
	}
	pair, err := nkeys.ParseDecoratedNKey([]byte(creds))
	if err != nil {
		return nil, fmt.Errorf("broker_credentials_invalid: NATS_CREDENTIALS_CREDS")
	}
	seed, err := pair.Seed()
	if err != nil {
		return nil, fmt.Errorf("broker_credentials_invalid: NATS_CREDENTIALS_CREDS")
	}
	return nats.UserJWTAndSeed(userJWT, string(seed)), nil
}
