// Package config loads and validates the core/trading-charts environment,
// mirroring the TS candlesEnvSchema (candles/src/config.ts): identical env var
// names (invariant i8), extending the shared base env (shared/config.BaseEnv).
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	runtimedb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	dbproduction "github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"io"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
)

// Config is the parsed candles environment.
type Config struct {
	FeaturesConfigFile        string `env:"CHART_FEATURES_CONFIG_FILE"`
	FeaturesConfigSHA256      string `env:"CHART_FEATURES_CONFIG_SHA256"`
	ArchiveAccessKeyID        string `env:"S3_ACCESS_KEY_ID" json:"-"`
	ArchiveSecretAccessKey    string `env:"S3_SECRET_ACCESS_KEY" json:"-"`
	OperatorToken             string `env:"CHART_OPERATOR_TOKEN" json:"-"`
	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	NATSUsername              string `env:"NATS_USERNAME"`
	NATSPassword              string `env:"NATS_PASSWORD"`
	RuntimeProfile            string `env:"RUNTIME_PROFILE"`
	BindAddress               string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile         string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetSHA256       string `env:"DB_BUDGET_SHA256"`
	RuntimePoolID             string `env:"DB_POOL_ID"`
	RuntimeInstanceID         string `env:"INSTANCE_ID"`
	ExpectedSchemaFile        string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaSHA256      string `env:"EXPECTED_SCHEMA_SHA256"`
	DeliveryConfigFile        string `env:"DELIVERY_CONFIG_FILE"`
	DeliveryConfigSHA256      string `env:"DELIVERY_CONFIG_SHA256"`
	NATSURL                   string `env:"NATS_URL"`
	NATSCredentials           string `env:"NATS_CREDENTIALS"`

	sharedconfig.BaseEnv

	WebhookHMACSecret string `env:"WEBHOOK_HMAC_SECRET,required"`

	// WebSocket connection limits (parity with ws-candles.ts env vars).
	WSMaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"10000"`
	WSMaxPerIP       int `env:"WS_MAX_PER_IP" envDefault:"20"`

	// CORS allowlist for the HTTP server.
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// TrustedProxyCIDRs lists the L7 proxies (Railway's edge) whose
	// X-Forwarded-For / X-Real-IP are honoured for the per-IP WebSocket limit.
	// Outside production only; empty trusts no proxy.
	TrustedProxyCIDRs string `env:"TRUSTED_PROXY_CIDRS"`
}

// Load parses the process environment into a validated Config.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if len(cfg.WebhookHMACSecret) < 32 {
		return Config{}, fmt.Errorf("config: WEBHOOK_HMAC_SECRET must be at least 32 characters")
	}
	if _, err := cfg.TrustedProxies(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// TrustedProxies parses TRUSTED_PROXY_CIDRS. Production never trusts forwarded
// client addresses (nil); elsewhere an empty value also trusts none.
func (c *Config) TrustedProxies() ([]netip.Prefix, error) {
	if c.NodeEnv == "production" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, raw := range strings.Split(c.TrustedProxyCIDRs, ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, errors.New("TRUSTED_PROXY_CIDRS entry is not a CIDR")
		}
		out = append(out, prefix.Masked())
	}
	if len(out) > 32 {
		return nil, errors.New("TRUSTED_PROXY_CIDRS allows at most 32 entries")
	}
	return out, nil
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
	features, featuresErr := c.RuntimeFeatures()
	if featuresErr != nil {
		return nil, nil, featuresErr
	}
	bindings := map[string]*string{}
	if features.Archive != nil {
		bindings["S3_ACCESS_KEY_ID"] = &c.ArchiveAccessKeyID
		bindings["S3_SECRET_ACCESS_KEY"] = &c.ArchiveSecretAccessKey
		bindings["CHART_OPERATOR_TOKEN"] = &c.OperatorToken
	}
	bindings["DATABASE_URL"] = &c.DatabaseURL
	bindings["REDIS_URL"] = &c.RedisURL
	bindings["RPC_URL"] = &c.RPCURL
	bindings["WEBHOOK_HMAC_SECRET"] = &c.WebhookHMACSecret
	if c.RPCURLFallback != "" {
		bindings["RPC_URL_FALLBACK"] = &c.RPCURLFallback
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
	if c.RuntimeBudgetFile == "" || c.RuntimePoolID == "" || c.RuntimeInstanceID == "" {
		return nil, fmt.Errorf("runtime_pool_configuration_missing")
	}
	data, err := os.ReadFile(c.RuntimeBudgetFile)
	if err != nil {
		return nil, err
	}
	if len(data) > 1048576 || len(c.RuntimeBudgetSHA256) != 64 || fmt.Sprintf("%x", sha256.Sum256(data)) != c.RuntimeBudgetSHA256 {
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
	if c.ExpectedSchemaFile == "" || len(c.ExpectedSchemaSHA256) != 64 {
		return fmt.Errorf("runtime_schema_configuration_missing")
	}
	data, err := os.ReadFile(c.ExpectedSchemaFile)
	if err != nil {
		return err
	}
	want, err := dbproduction.LoadExpectedSchema(data, c.ExpectedSchemaSHA256)
	if err != nil {
		return err
	}
	if want.Service != "core/trading-charts" {
		return fmt.Errorf("runtime_schema_service_mismatch")
	}
	return dbproduction.CheckReadiness(ctx, pool, want)
}

func ReadBoundDocument(path, digest string, out any) error {
	if path == "" || len(digest) != 64 {
		return fmt.Errorf("runtime_document_binding_missing")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1048576 {
		return fmt.Errorf("runtime_document_invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return fmt.Errorf("runtime_document_binding_mismatch")
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

type ChartArchiveFeatures struct {
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	Bucket   string `json:"bucket"`
}

// ChartRuntimeFeatures is the sha256-bound features document. Its anchorHash
// is the block hash the node reports for anchorHeight, never go-ethereum's
// recomputed header hash; shared/chain rejects the latter with
// ErrRecomputedAnchor.
type ChartRuntimeFeatures struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	EndpointSetVersion   string                `json:"endpointSetVersion"`
	DeploymentVersion    string                `json:"deploymentVersion"`
	AnchorHeight         uint64                `json:"anchorHeight"`
	AnchorHash           string                `json:"anchorHash"`
	CanonicalAPIOrigin   string                `json:"canonicalApiOrigin"`
	ReplayGatewayOrigin  string                `json:"replayGatewayOrigin"`
	CanonicalSeriesModel string                `json:"canonicalSeriesModel"`
	DisplayPrecision     int                   `json:"displayPrecision"`
	MaxHistoryOrigins    int                   `json:"maxHistoryOrigins"`
	RollupBatch          int                   `json:"rollupBatch"`
	RollupIntervalMillis int                   `json:"rollupIntervalMillis"`
	Archive              *ChartArchiveFeatures `json:"archive"`
}

func (c *Config) RuntimeFeatures() (ChartRuntimeFeatures, error) {
	var f ChartRuntimeFeatures
	if err := ReadBoundDocument(c.FeaturesConfigFile, c.FeaturesConfigSHA256, &f); err != nil {
		return f, err
	}
	if f.SchemaVersion != 2 || f.EndpointSetVersion == "" || len(f.EndpointSetVersion) > 128 || f.DeploymentVersion == "" || f.AnchorHeight == 0 || !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(f.AnchorHash) || f.AnchorHash == "0x"+strings.Repeat("0", 64) || f.CanonicalSeriesModel == "" || len(f.CanonicalSeriesModel) > 128 || f.DisplayPrecision < 1 || f.DisplayPrecision > 12 || f.MaxHistoryOrigins < 1 || f.MaxHistoryOrigins > 32 || f.RollupBatch < 1 || f.RollupBatch > 64 || f.RollupIntervalMillis < 100 || f.RollupIntervalMillis > 60000 {
		return f, fmt.Errorf("charts_features_invalid")
	}
	expected := deployment.Current().DeploymentVersion
	if c.DeploymentRealm == "private_fixture" {
		manifest, err := c.PrivateFixtureEvidence()
		if err != nil {
			return f, err
		}
		expected = manifest.Deployment.DeploymentVersion
	}
	if f.DeploymentVersion != expected {
		return f, fmt.Errorf("charts_deployment_version_mismatch")
	}
	for _, origin := range []string{f.CanonicalAPIOrigin, f.ReplayGatewayOrigin} {
		if err := validateChartOrigin(origin); err != nil {
			return f, err
		}
	}
	if f.Archive != nil {
		if f.Archive.Bucket == "" || len(f.Archive.Bucket) > 128 || f.Archive.Endpoint == "" {
			return f, fmt.Errorf("charts_archive_config_invalid")
		}
		if err := validateChartOrigin(f.Archive.Endpoint); err != nil {
			return f, err
		}
	}
	return f, nil
}
func validateChartOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("charts_origin_invalid")
	}
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return fmt.Errorf("charts_origin_requires_https")
	}
	return nil
}
