// Package config loads and validates the core/ranking-algo environment,
// mirroring the TS rankingEnvSchema (ranking-algo/src/config.ts): identical env
// var names (invariant i8) and defaults, extending the shared base env
// (shared/config.BaseEnv).
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
	"io"
	"os"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
)

// Config is the parsed ranking-algo environment.
type Config struct {
	RankingRuntimeFile        string `env:"RANKING_RUNTIME_FILE"`
	RankingRuntimeDocument    string `env:"RANKING_RUNTIME_DOCUMENT"`
	RankingRuntimeSHA256      string `env:"RANKING_RUNTIME_SHA256"`
	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
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

	sharedconfig.BaseEnv

	// RankingTickIntervalMS is the cadence for the heavy rankers (trending,
	// breakout, top-volume, movers, unusual). Parity default 30000ms.
	RankingTickIntervalMS int `env:"RANKING_TICK_INTERVAL_MS" envDefault:"30000"`
	// RankingNewTickIntervalMS is the cadence for the new-pools ranker. Parity
	// default 10000ms.
	RankingNewTickIntervalMS int `env:"RANKING_NEW_TICK_INTERVAL_MS" envDefault:"10000"`
	// RankingMaxEntries caps how many entries each ranked Redis ZSET holds.
	// Parity default 200.
	RankingMaxEntries int `env:"RANKING_MAX_ENTRIES" envDefault:"200"`

	// CORSAllowedOrigins is the comma-separated CORS allowlist for the HTTP API.
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
	if cfg.RankingTickIntervalMS <= 0 {
		return Config{}, fmt.Errorf("config: RANKING_TICK_INTERVAL_MS must be positive")
	}
	if cfg.RankingNewTickIntervalMS <= 0 {
		return Config{}, fmt.Errorf("config: RANKING_NEW_TICK_INTERVAL_MS must be positive")
	}
	if cfg.RankingMaxEntries <= 0 {
		return Config{}, fmt.Errorf("config: RANKING_MAX_ENTRIES must be positive")
	}
	return cfg, nil
}

// TickInterval returns the heavy-ranker cadence as a time.Duration.
func (c Config) TickInterval() time.Duration {
	return time.Duration(c.RankingTickIntervalMS) * time.Millisecond
}

// NewTickInterval returns the new-pools ranker cadence as a time.Duration.
func (c Config) NewTickInterval() time.Duration {
	return time.Duration(c.RankingNewTickIntervalMS) * time.Millisecond
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
	pool, err := runtimedb.NewPool(ctx, c.DatabaseURL, options)
	if err != nil {
		return nil, err
	}
	if err = c.CheckRuntimeSchema(ctx, pool); err != nil {
		runtimedb.ClosePool(pool)
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
	if want.Service != "core/ranking-algo" {
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
		{"RANKING_RUNTIME", c.RankingRuntimeFile, c.RankingRuntimeDocument, c.RankingRuntimeSHA256},
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

type RankingRuntimeBinding struct {
	Version                  int    `json:"version"`
	RankingSchema            string `json:"rankingSchema"`
	StatsSchema              string `json:"statsSchema"`
	SourceSchema             string `json:"sourceSchema"`
	SourceStream             string `json:"sourceStream"`
	ProjectionVersion        string `json:"projectionVersion"`
	CacheNamespace           string `json:"cacheNamespace"`
	CursorKeyReference       string `json:"cursorKeyReference"`
	RecoverySchema           string `json:"recoverySchema"`
	RecoveryEpoch            int64  `json:"recoveryEpoch"`
	RecoveryRegion           string `json:"recoveryRegion"`
	VisibilityEndpoint       string `json:"visibilityEndpoint"`
	VisibilityDNS            string `json:"visibilityDNS"`
	VisibilityCertSHA256     string `json:"visibilityCertSHA256"`
	GatewayDNS               string `json:"gatewayDNS"`
	GatewayCertSHA256        string `json:"gatewayCertSHA256"`
	CAFile                   string `json:"caFile"`
	CASHA256                 string `json:"caSha256"`
	ClientCertFile           string `json:"clientCertFile"`
	ClientCertSHA256         string `json:"clientCertSha256"`
	ClientKeyReference       string `json:"clientKeyReference"`
	ServerCertFile           string `json:"serverCertFile"`
	ServerCertSHA256         string `json:"serverCertSha256"`
	ServerKeyReference       string `json:"serverKeyReference"`
	NATSURL                  string `json:"natsURL"`
	NATSCredentialsReference string `json:"natsCredentialsReference"`
	RankingStream            string `json:"rankingStream"`
	MaxAttempts              int    `json:"maxAttempts"`
	MaximumReads             int    `json:"maximumReads"`
}
