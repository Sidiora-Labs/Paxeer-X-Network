// Package config loads and validates the core/api gateway environment.
//
// Unlike the data/processor services, core/api is a pure EDGE: it reads from
// the shared Postgres + Redis (written by the core/* services) and fans data
// out over WSS/SSE, plus a thin rate-limited REST snapshot surface. It needs no
// chain RPC or contract addresses, so it deliberately does NOT embed the full
// shared baseEnvSchema (which marks RPC_URL + the 10 contract addresses as
// required). Env var names that ARE shared (DATABASE_URL, REDIS_URL, PORT,
// LOG_LEVEL, NODE_ENV, CORS_ALLOWED_ORIGINS) keep their canonical names so the
// deploy configs port as-is (invariant i8).
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
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the parsed core/api gateway environment.
type Config struct {
	DiscoveryRuntimeFile     string `env:"DISCOVERY_RUNTIME_CONFIG_FILE"`
	DiscoveryRuntimeDocument string `env:"DISCOVERY_RUNTIME_CONFIG_DOCUMENT"`
	DiscoveryRuntimeSHA256   string `env:"DISCOVERY_RUNTIME_CONFIG_SHA256"`
	NetworkAuthorityFile     string `env:"NETWORK_AUTHORITY_CONFIG_FILE"`
	NetworkAuthorityDocument string `env:"NETWORK_AUTHORITY_CONFIG_DOCUMENT"`
	NetworkAuthoritySHA256   string `env:"NETWORK_AUTHORITY_CONFIG_SHA256"`
	NetworkRPCURL            string `env:"NETWORK_RPC_URL"`
	IdentityPeerCertSHA256   string `env:"IDENTITY_PEER_CERT_SHA256"`
	IdentityCAFile           string `env:"IDENTITY_CA_FILE"`
	IdentityClientCertFile   string `env:"IDENTITY_CLIENT_CERT_FILE"`
	IdentityClientKeyFile    string `env:"IDENTITY_CLIENT_KEY_FILE"`
	IdentitySigningKey       string `env:"IDENTITY_SIGNING_KEY"`
	IdentityIssuer           string `env:"IDENTITY_ASSERTION_ISSUER"`
	IdentityAudience         string `env:"IDENTITY_ASSERTION_AUDIENCE"`
	TrustedProxyCIDRs        string `env:"TRUSTED_PROXY_CIDRS"`
	StreamConsumerFile       string `env:"STREAM_CONSUMER_CONFIG_FILE"`
	StreamConsumerDocument   string `env:"STREAM_CONSUMER_CONFIG_DOCUMENT"`
	StreamConsumerSHA256     string `env:"STREAM_CONSUMER_CONFIG_SHA256"`
	StreamSchema             string `env:"STREAM_SCHEMA" envDefault:"api_stream"`
	// StreamRelayFile/SHA256 bind the API_ROLE=rest api_stream outbox relay
	// document (app/stream_relay.go streamRelayDocument).
	StreamRelayFile     string `env:"STREAM_RELAY_CONFIG_FILE"`
	StreamRelayDocument string `env:"STREAM_RELAY_CONFIG_DOCUMENT"`
	StreamRelaySHA256   string `env:"STREAM_RELAY_CONFIG_SHA256"`
	NATSURL             string `env:"NATS_URL"`
	NATSCredentials     string `env:"NATS_CREDENTIALS"`
	AdmissionCapacity   int    `env:"API_ADMISSION_CAPACITY" envDefault:"1000"`
	AdmissionMaxWaiting int    `env:"API_ADMISSION_MAX_WAITING" envDefault:"1000"`

	// NetworkAuthorityCacheTTLSec bounds how long the non-production
	// GET /v2/network/authority reuses one attestation (success or failure).
	// Values outside 1..30 fall back to the default.
	NetworkAuthorityCacheTTLSec int `env:"NETWORK_AUTHORITY_CACHE_TTL_SEC" envDefault:"20"`

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
	APIRole                   string `env:"API_ROLE"`
	IdentityURL               string `env:"IDENTITY_SERVICE_URL"`
	IdentityServiceToken      string `env:"IDENTITY_SERVICE_TOKEN"`
	StreamSigningKey          string `env:"STREAM_SIGNING_KEY"`
	StatsURL                  string `env:"STATS_SERVICE_URL"`
	AccountingReadSHA256      string `env:"ACCOUNTING_READ_CONFIG_SHA256"`
	AccountingReadFile        string `env:"ACCOUNTING_READ_CONFIG_FILE"`
	AccountingReadDocument    string `env:"ACCOUNTING_READ_CONFIG_DOCUMENT"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`

	// DatabaseURL is the shared Postgres the gateway reads from (snapshot REST).
	DatabaseURL string `env:"DATABASE_URL,required"`
	// RedisURL backs both the pub/sub fan-out and the read-through caches.
	RedisURL string `env:"REDIS_URL,required"`

	Port     int    `env:"PORT" envDefault:"3000"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	NodeEnv  string `env:"NODE_ENV" envDefault:"production"`

	// CORSAllowedOrigins is the comma-separated CORS allowlist (empty/"*" = all).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// --- Rate limiting (public ingress, invariant i12) ---------------------

	// RateLimitMax is the max requests per window per IP/key on the REST surface.
	RateLimitMax int `env:"API_RATE_LIMIT_MAX" envDefault:"100"`
	// RateLimitWindowSec is the rate-limit window in seconds.
	RateLimitWindowSec int `env:"API_RATE_LIMIT_WINDOW_SEC" envDefault:"60"`
	// MaxInFlightRequests caps concurrent in-flight REST requests; over capacity
	// the gateway load-sheds with 503 instead of exhausting memory (i12, no-OOM).
	MaxInFlightRequests int `env:"API_MAX_INFLIGHT_REQUESTS" envDefault:"10000"`

	// --- Realtime connection caps + backpressure (invariant i11, 500K) -----

	// WSMaxConnections caps total concurrent WebSocket connections.
	WSMaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"50000"`
	// WSMaxPerIP caps concurrent WebSocket connections from a single IP.
	WSMaxPerIP int `env:"WS_MAX_PER_IP" envDefault:"20"`
	// SSEMaxConnections caps total concurrent SSE connections.
	SSEMaxConnections int `env:"SSE_MAX_CONNECTIONS" envDefault:"50000"`
	// SSEMaxPerIP caps concurrent SSE connections from a single IP.
	SSEMaxPerIP int `env:"SSE_MAX_PER_IP" envDefault:"20"`
	// ClientSendBuffer bounds each connection's outbound queue; a client whose
	// queue overflows is evicted as a slow consumer rather than blocking fan-out.
	ClientSendBuffer int `env:"CLIENT_SEND_BUFFER" envDefault:"256"`
	// CoalesceFlushMS is the RAF-style coalescing window: high-frequency ticks
	// (swaps, candles, pool state) are collapsed to the latest-per-key and
	// flushed at most once per window, so a burst never floods a client.
	CoalesceFlushMS int `env:"COALESCE_FLUSH_MS" envDefault:"100"`

	// --- Auth -------------------------------------------------------------

	// AdminAPIKey, when set, guards privileged routes (e.g. internal metrics);
	// empty leaves those routes unmounted. Public read routes never require it.
	AdminAPIKey string `env:"ADMIN_API_KEY"`
}

var (
	validLevels = map[string]struct{}{"debug": {}, "info": {}, "warn": {}, "error": {}}
	validNodes  = map[string]struct{}{"development": {}, "production": {}, "test": {}}
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
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// Validate enforces field-level rules (URL shape, enums, positive bounds).
func (c *Config) Validate() error {
	var errs []error

	if err := requireURL("DATABASE_URL", c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}
	if err := requireRedisURL("REDIS_URL", c.RedisURL); err != nil {
		errs = append(errs, err)
	}
	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, fmt.Errorf("LOG_LEVEL %q must be one of debug|info|warn|error", c.LogLevel))
	}
	if _, ok := validNodes[c.NodeEnv]; !ok {
		errs = append(errs, fmt.Errorf("NODE_ENV %q must be one of development|production|test", c.NodeEnv))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d out of range 1-65535", c.Port))
	}
	for _, p := range []struct {
		name string
		val  int
	}{
		{"API_RATE_LIMIT_MAX", c.RateLimitMax},
		{"API_RATE_LIMIT_WINDOW_SEC", c.RateLimitWindowSec},
		{"API_MAX_INFLIGHT_REQUESTS", c.MaxInFlightRequests},
		{"WS_MAX_CONNECTIONS", c.WSMaxConnections},
		{"WS_MAX_PER_IP", c.WSMaxPerIP},
		{"SSE_MAX_CONNECTIONS", c.SSEMaxConnections},
		{"SSE_MAX_PER_IP", c.SSEMaxPerIP},
		{"CLIENT_SEND_BUFFER", c.ClientSendBuffer},
		{"COALESCE_FLUSH_MS", c.CoalesceFlushMS},
	} {
		if p.val <= 0 {
			errs = append(errs, fmt.Errorf("%s must be positive, got %d", p.name, p.val))
		}
	}

	return errors.Join(errs...)
}

// RateLimitWindow returns the rate-limit window as a time.Duration.
func (c *Config) RateLimitWindow() time.Duration {
	return time.Duration(c.RateLimitWindowSec) * time.Second
}

// CoalesceFlush returns the coalescing window as a time.Duration.
func (c *Config) CoalesceFlush() time.Duration {
	return time.Duration(c.CoalesceFlushMS) * time.Millisecond
}

func requireURL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s %q is not a valid URL", name, raw)
	}
	return nil
}

// requireRedisURL accepts redis:// and rediss:// DSNs (host required).
func requireRedisURL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return fmt.Errorf("%s %q is not a valid redis:// URL", name, raw)
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
	if c.AdminAPIKey != "" {
		bindings["ADMIN_API_KEY"] = &c.AdminAPIKey
	}
	bindings["IDENTITY_SIGNING_KEY"] = &c.IdentitySigningKey
	bindings["STREAM_SIGNING_KEY"] = &c.StreamSigningKey
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
	if options.Transport != runtimedb.TransportDirect || options.MaxConns > 20 {
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
	if want.Service != "core/api" {
		return fmt.Errorf("runtime_schema_service_mismatch")
	}
	return dbproduction.CheckReadiness(ctx, pool, want)
}

func (c *Config) RuntimeRole() (string, error) {
	switch c.APIRole {
	case "rest", "sockets":
		return "apid:" + c.APIRole, nil
	case "development", "":
		if c.NodeEnv != "production" {
			return "apid:development", nil
		}
	}
	return "", fmt.Errorf("API_ROLE requires rest or sockets in production")
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
		{"NETWORK_AUTHORITY_CONFIG", c.NetworkAuthorityFile, c.NetworkAuthorityDocument, c.NetworkAuthoritySHA256},
		{"STREAM_RELAY_CONFIG", c.StreamRelayFile, c.StreamRelayDocument, c.StreamRelaySHA256},
		{"STREAM_CONSUMER_CONFIG", c.StreamConsumerFile, c.StreamConsumerDocument, c.StreamConsumerSHA256},
		{"DISCOVERY_RUNTIME_CONFIG", c.DiscoveryRuntimeFile, c.DiscoveryRuntimeDocument, c.DiscoveryRuntimeSHA256},
		{"ACCOUNTING_READ_CONFIG", c.AccountingReadFile, c.AccountingReadDocument, c.AccountingReadSHA256},
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
