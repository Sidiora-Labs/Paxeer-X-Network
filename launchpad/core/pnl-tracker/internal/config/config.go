// Package config loads and validates the core/pnl-tracker environment, mirroring
// the TS pnlEnvSchema (pnl/src/config.ts): identical env var names (invariant
// i8), identical defaults, extending the shared base env (shared/config.BaseEnv).
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	runtimedb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	dbproduction "github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
)

// Config is the parsed pnl environment.
type Config struct {
	JourneyConfigFile     string `env:"PNL_JOURNEY_CONFIG_FILE"`
	JourneyConfigDocument string `env:"PNL_JOURNEY_CONFIG_DOCUMENT"`
	JourneyConfigSHA256   string `env:"PNL_JOURNEY_CONFIG_SHA256"`
	IdentityURL           string `env:"IDENTITY_SERVICE_URL"`
	IdentitySigningKey    string `env:"IDENTITY_SIGNING_KEY"`
	IdentityServiceToken  string `env:"IDENTITY_SERVICE_TOKEN"`
	PortfolioCursorKey    string `env:"PNL_CURSOR_KEY"`
	PortfolioCDNToken     string `env:"PNL_CDN_TOKEN"`

	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	FixtureCommitAckBarrier   string `env:"FIXTURE_COMMIT_ACK_BARRIER"`
	NATSUsername              string `env:"NATS_USERNAME"`
	NATSPassword              string `env:"NATS_PASSWORD"`
	RuntimeAction             string `env:"RUNTIME_ACTION"`
	BaselineWallet            string `env:"BASELINE_WALLET"`
	BaselineToken             string `env:"BASELINE_TOKEN"`
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
	DeliveryConfigFile        string `env:"DELIVERY_CONFIG_FILE"`
	DeliveryConfigDocument    string `env:"DELIVERY_CONFIG_DOCUMENT"`
	DeliveryConfigSHA256      string `env:"DELIVERY_CONFIG_SHA256"`
	NATSURL                   string `env:"NATS_URL"`
	NATSCredentials           string `env:"NATS_CREDENTIALS"`

	sharedconfig.BaseEnv

	// ReconcileIntervalMS is how often the idempotent backfill reconciler scans
	// indexer.swaps for swaps not yet folded into positions. Mirrors the TS
	// PNL_RECONCILE_INTERVAL_MS (default 30000).
	ReconcileIntervalMS int `env:"PNL_RECONCILE_INTERVAL_MS" envDefault:"30000"`

	// ReconcileBatchSize bounds the number of swaps reconciled per tick.
	ReconcileBatchSize int `env:"PNL_RECONCILE_BATCH_SIZE" envDefault:"500"`

	// PublicBaseURL is the origin used to build a card's shareUrl
	// (PublicBaseURL + "/pnl/" + shortCode) — the referral landing page.
	PublicBaseURL string `env:"PNL_PUBLIC_BASE_URL" envDefault:"https://sidiora.fun"`

	// OGBaseURL is the origin that serves rendered OG images. Empty falls back to
	// PublicBaseURL. The ogUrl is OGBaseURL + "/pnl/cards/" + cardId + "/og.png".
	OGBaseURL string `env:"PNL_OG_BASE_URL"`

	// FontDir is an optional directory holding the Inter *.ttf font files used by
	// the OG renderer. When empty (or a face is missing) the renderer falls back
	// to the built-in basic font so rendering never fails.
	FontDir string `env:"PNL_FONT_DIR"`

	// RewardPerConversion is the (integer) reward units credited to a sharer per
	// referral conversion, surfaced in the sharer dashboard.
	RewardPerConversion int `env:"PNL_REWARD_PER_CONVERSION" envDefault:"1"`

	// WebhookHMACSecret is the HMAC-SHA256 secret shared with the indexer fanout
	// publisher. Required, min 32 chars (zod parity).
	WebhookHMACSecret string `env:"WEBHOOK_HMAC_SECRET,required"`

	// TrustedProxyCIDRs lists the L7 proxies (comma-separated CIDRs) whose
	// X-Forwarded-For is honoured for the client IP; the per-client rate limit
	// keys on that IP. Empty trusts no proxy.
	TrustedProxyCIDRs string `env:"TRUSTED_PROXY_CIDRS"`

	// CORSAllowedOrigins is the HTTP CORS allowlist (comma-separated; empty/"*"
	// allows all).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`
}

// Load parses the process environment into a validated Config.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err = cfg.registerInlineDocuments(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if len(cfg.WebhookHMACSecret) < 32 {
		return Config{}, fmt.Errorf("config: WEBHOOK_HMAC_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

// ReconcileInterval returns the reconciler scan interval as a Duration.
func (c Config) ReconcileInterval() time.Duration {
	return time.Duration(c.ReconcileIntervalMS) * time.Millisecond
}

// OGOrigin returns the origin used to build OG image URLs, falling back to the
// public base URL when PNL_OG_BASE_URL is unset.
func (c Config) OGOrigin() string {
	if s := strings.TrimSpace(c.OGBaseURL); s != "" {
		return strings.TrimRight(s, "/")
	}
	return strings.TrimRight(c.PublicBaseURL, "/")
}

// ShareOrigin returns the trimmed public base URL used to build share URLs.
func (c Config) ShareOrigin() string {
	return strings.TrimRight(c.PublicBaseURL, "/")
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
	bindings["WEBHOOK_HMAC_SECRET"] = &c.WebhookHMACSecret
	if c.RPCURLFallback != "" {
		bindings["RPC_URL_FALLBACK"] = &c.RPCURLFallback
	}
	bindings["NATS_URL"] = &c.NATSURL
	if c.JourneyConfigFile != "" || c.JourneyConfigDocument != "" {
		bindings["IDENTITY_SIGNING_KEY"] = &c.IdentitySigningKey
		bindings["PNL_CURSOR_KEY"] = &c.PortfolioCursorKey
		// PNL_CDN_TOKEN is bound only with a Cloudflare zone in front of the
		// public origin; an origin served directly has no CDN credential.
		if c.PortfolioCDNToken != "" {
			bindings["PNL_CDN_TOKEN"] = &c.PortfolioCDNToken
		}
	}
	if c.DeploymentRealm == "private_fixture" {
		if c.RuntimeProfile != "owned_local" {
			return nil, nil, fmt.Errorf("fixture_nats_profile_mismatch")
		}
		if _, e := c.PrivateFixtureEvidence(); e != nil {
			return nil, nil, e
		}
		bindings["NATS_USERNAME"] = &c.NATSUsername
		bindings["NATS_PASSWORD"] = &c.NATSPassword
	} else {
		bindings["NATS_CREDENTIALS"] = &c.NATSCredentials
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
	dsn := c.DatabaseURL
	if c.JourneyConfigFile != "" || c.JourneyConfigDocument != "" {
		parsed, e := url.Parse(dsn)
		if e != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			return nil, fmt.Errorf("pnl_journey_database_route_invalid")
		}
		query := parsed.Query()
		if value := query.Get("search_path"); value != "" && value != "pnl,pg_catalog" {
			return nil, fmt.Errorf("pnl_journey_search_path_conflict")
		}
		query.Set("search_path", "pnl,pg_catalog")
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	}
	pool, err := runtimedb.NewPool(ctx, dsn, options)
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
	if want.Service != "core/pnl-tracker" {
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
	pairs := []struct{ name, file, inline, digest string }{
		{"DB_BUDGET", c.RuntimeBudgetFile, c.RuntimeBudgetDocument, c.RuntimeBudgetSHA256},
		{"EXPECTED_SCHEMA", c.ExpectedSchemaFile, c.ExpectedSchemaDocument, c.ExpectedSchemaSHA256},
		{"DELIVERY_CONFIG", c.DeliveryConfigFile, c.DeliveryConfigDocument, c.DeliveryConfigSHA256},
		{"PNL_JOURNEY_CONFIG", c.JourneyConfigFile, c.JourneyConfigDocument, c.JourneyConfigSHA256},
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
	if c.NATSCredentials == "" {
		return nil, fmt.Errorf("broker_credentials_missing")
	}
	return append(options, nats.UserCredentials(c.NATSCredentials)), nil
}
