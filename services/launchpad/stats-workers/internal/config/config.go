// Package config loads and validates the core/stats-workers environment,
// mirroring the TS statsEnvSchema (stats/src/config.ts): identical env var names
// (invariant i8), identical defaults, extending the shared base env
// (shared/config.BaseEnv).
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
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

// Config is the parsed stats environment.
type Config struct {
	StatsCursorKey            string `env:"STATS_CURSOR_KEY"`
	StatsAuditKey             string `env:"STATS_AUDIT_KEY"`
	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
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
	DeliveryConfigFile        string `env:"DELIVERY_CONFIG_FILE"`
	DeliveryConfigDocument    string `env:"DELIVERY_CONFIG_DOCUMENT"`
	DeliveryConfigSHA256      string `env:"DELIVERY_CONFIG_SHA256"`
	NATSURL                   string `env:"NATS_URL"`
	NATSCredentials           string `env:"NATS_CREDENTIALS"`
	NATSCredentialsCreds      string `env:"NATS_CREDENTIALS_CREDS"`

	// Production identity channel to the gateway identity authority's private
	// mTLS listener (IDENTITY_SERVICE_URL=https://gateway.railway.internal:8443):
	// the private CA, statsd's client certificate and key (a file, or the PEM
	// itself as a secret), the pinned gateway server certificate and the
	// ed25519 request-assertion key whose public half is the gateway's
	// identityPeers entry for issuer "stats". The gateway never accepts the
	// IDENTITY_SERVICE_TOKEN bearer in production.
	IdentityURL                string `env:"IDENTITY_SERVICE_URL"`
	IdentityMTLSCAFile         string `env:"IDENTITY_MTLS_CA_FILE"`
	IdentityMTLSClientCertFile string `env:"IDENTITY_MTLS_CLIENT_CERT_FILE"`
	IdentityMTLSCAPEM          string `env:"IDENTITY_MTLS_CA_PEM"`
	IdentityMTLSClientCertPEM  string `env:"IDENTITY_MTLS_CLIENT_CERT_PEM"`
	IdentityMTLSClientKeyFile  string `env:"IDENTITY_MTLS_CLIENT_KEY_FILE"`
	IdentityMTLSClientKeyPEM   string `env:"IDENTITY_MTLS_CLIENT_KEY_PEM"`
	IdentityMTLSPeerCertSHA256 string `env:"IDENTITY_MTLS_PEER_CERT_SHA256"`
	IdentitySigningKey         string `env:"IDENTITY_ASSERTION_SIGNING_KEY"`

	sharedconfig.BaseEnv

	// HolderRefreshIntervalMS / ActivePoolWindowHours mirror the TS
	// STATS_HOLDER_REFRESH_INTERVAL_MS (default 60000) and
	// STATS_ACTIVE_POOL_WINDOW_HOURS (default 24).
	HolderRefreshIntervalMS int `env:"STATS_HOLDER_REFRESH_INTERVAL_MS" envDefault:"60000"`
	ActivePoolWindowHours   int `env:"STATS_ACTIVE_POOL_WINDOW_HOURS" envDefault:"24"`

	// WebhookHMACSecret is the HMAC-SHA256 secret shared with the indexer fanout
	// publisher (matched-index CSV contract). Required, min 32 chars (zod parity).
	WebhookHMACSecret string `env:"WEBHOOK_HMAC_SECRET,required"`

	// Production canonical read-model workers. PublicStats* drive the
	// market.stats.<pool> producer that core-api's /v2 pool statistics,
	// stats:batch, pool detail and snapshot reads serve from the shared stream
	// projection schema; LegacyBridge* drive the canonical replay into the legacy
	// stats tables behind /stats/*, /bff/token and the platform metrics.
	PublicStatsIntervalMS  int    `env:"STATS_PUBLIC_STATS_INTERVAL_MS" envDefault:"2000"`
	PublicStatsBatch       int    `env:"STATS_PUBLIC_STATS_BATCH" envDefault:"50"`
	PublicStreamSchema     string `env:"STATS_PUBLIC_STREAM_SCHEMA" envDefault:"api_stream"`
	LegacyBridgeIntervalMS int    `env:"STATS_LEGACY_BRIDGE_INTERVAL_MS" envDefault:"1000"`
	LegacyBridgeBatch      int    `env:"STATS_LEGACY_BRIDGE_BATCH" envDefault:"100"`

	// CORSAllowedOrigins is the HTTP CORS allowlist (comma-separated; empty/"*"
	// allows all). Parity with the shared createServer CORS handling.
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

// ValidateProductionWorkers bounds the canonical read-model worker settings.
func (c Config) ValidateProductionWorkers() error {
	if c.PublicStatsIntervalMS < 250 || c.PublicStatsIntervalMS > 60000 || c.PublicStatsBatch < 1 || c.PublicStatsBatch > 100 || c.LegacyBridgeIntervalMS < 100 || c.LegacyBridgeIntervalMS > 60000 || c.LegacyBridgeBatch < 1 || c.LegacyBridgeBatch > 1000 {
		return fmt.Errorf("stats_worker_bounds_invalid")
	}
	if len(c.PublicStreamSchema) == 0 || len(c.PublicStreamSchema) > 63 || c.PublicStreamSchema[0] < 'a' || c.PublicStreamSchema[0] > 'z' {
		return fmt.Errorf("stats_public_stream_schema_invalid")
	}
	for _, r := range c.PublicStreamSchema {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return fmt.Errorf("stats_public_stream_schema_invalid")
		}
	}
	return nil
}

// IdentityClient describes statsd's private identity channel (issuer
// "stats") for sharedauth.NewMTLSIdentityClient.
func (c *Config) IdentityClient() sharedauth.MTLSIdentityClientConfig {
	return sharedauth.MTLSIdentityClientConfig{Endpoint: strings.TrimRight(strings.TrimSpace(c.IdentityURL), "/"), PeerCertSHA256: c.IdentityMTLSPeerCertSHA256, CAFile: c.IdentityMTLSCAFile, CAPEM: c.IdentityMTLSCAPEM, ClientCertFile: c.IdentityMTLSClientCertFile, ClientCertPEM: c.IdentityMTLSClientCertPEM, ClientKeyFile: c.IdentityMTLSClientKeyFile, ClientKeyPEM: c.IdentityMTLSClientKeyPEM, Issuer: "stats", SigningKey: c.IdentitySigningKey}
}

// ValidateIdentityClient checks the production identity channel settings:
// an https origin without path (the gateway serves /internal/v2/identity/*
// only on its mTLS listener), exactly one source each for the CA, the client
// certificate and the client key, the lowercase gateway server pin and a well-formed
// ed25519 assertion key.
func (c *Config) ValidateIdentityClient() error {
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.IdentityURL), "/"))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return fmt.Errorf("identity_authority_endpoint_invalid")
	}
	if (c.IdentityMTLSCAFile == "") == (c.IdentityMTLSCAPEM == "") || (c.IdentityMTLSClientCertFile == "") == (c.IdentityMTLSClientCertPEM == "") || (c.IdentityMTLSClientKeyFile == "") == (c.IdentityMTLSClientKeyPEM == "") {
		return fmt.Errorf("identity_authority_mtls_missing")
	}
	if pin, e := hex.DecodeString(c.IdentityMTLSPeerCertSHA256); e != nil || len(pin) != 32 || strings.ToLower(c.IdentityMTLSPeerCertSHA256) != c.IdentityMTLSPeerCertSHA256 {
		return fmt.Errorf("identity_authority_peer_pin_invalid")
	}
	if _, e := sharedauth.DecodeIdentitySigningKey(c.IdentitySigningKey); e != nil {
		return fmt.Errorf("identity_assertion_signing_key_invalid")
	}
	return nil
}

// HolderRefreshInterval returns the holder-stats refresh interval as a Duration.
func (c Config) HolderRefreshInterval() time.Duration {
	return time.Duration(c.HolderRefreshIntervalMS) * time.Millisecond
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
	bindings["STATS_CURSOR_KEY"] = &c.StatsCursorKey
	bindings["STATS_AUDIT_KEY"] = &c.StatsAuditKey
	bindings["IDENTITY_ASSERTION_SIGNING_KEY"] = &c.IdentitySigningKey
	if c.IdentityMTLSClientKeyFile == "" {
		bindings["IDENTITY_MTLS_CLIENT_KEY_PEM"] = &c.IdentityMTLSClientKeyPEM
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

	if _, _, err = c.Phase2Keys(); err != nil {
		return nil, nil, err
	}
	if err = c.ValidateIdentityClient(); err != nil {
		return nil, nil, err
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
	if want.Service != "core/stats-workers" {
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
	for _, p := range []struct{ fileName, inlineName, file, inline string }{
		{"IDENTITY_MTLS_CA_FILE", "IDENTITY_MTLS_CA_PEM", c.IdentityMTLSCAFile, c.IdentityMTLSCAPEM},
		{"IDENTITY_MTLS_CLIENT_CERT_FILE", "IDENTITY_MTLS_CLIENT_CERT_PEM", c.IdentityMTLSClientCertFile, c.IdentityMTLSClientCertPEM},
		{"IDENTITY_MTLS_CLIENT_KEY_FILE", "IDENTITY_MTLS_CLIENT_KEY_PEM", c.IdentityMTLSClientKeyFile, c.IdentityMTLSClientKeyPEM},
	} {
		if p.file != "" && p.inline != "" {
			return fmt.Errorf("%s and %s are both set", p.fileName, p.inlineName)
		}
		if c.NodeEnv == "production" && c.RuntimeProfile != "" && p.file == "" && p.inline == "" {
			return fmt.Errorf("%s or %s is required", p.fileName, p.inlineName)
		}
	}
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

func (c Config) Phase2Keys() (cursor, audit []byte, err error) {
	cursor, err = hex.DecodeString(c.StatsCursorKey)
	if err != nil || len(cursor) < 32 || len(cursor) > 64 {
		return nil, nil, fmt.Errorf("stats_cursor_key_invalid")
	}
	audit, err = hex.DecodeString(c.StatsAuditKey)
	if err != nil || len(audit) < 32 || len(audit) > 64 {
		return nil, nil, fmt.Errorf("stats_audit_key_invalid")
	}
	return cursor, audit, nil
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
