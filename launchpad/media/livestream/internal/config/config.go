// Package config loads and validates the media/livestream environment, mirroring
// the TS livestreamEnvSchema (src/config.ts) one-to-one: identical env var names
// (invariant i8 — matches docker-envs/livestream.env), defaults, and validation.
//
// Unlike most services, livestream does NOT inherit the chain-heavy base env
// (no RPC_URL / contract addresses): the TS service defines its own schema, and
// docker-envs/livestream.env carries none of those vars.
package config

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
	runtimedb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	dbproduction "github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the parsed livestream environment. Field order follows config.ts.
type Config struct {
	PlaybackWebhookID            string `env:"LIVEPEER_PLAYBACK_WEBHOOK_ID"`
	PlaybackWebhookURL           string `env:"LIVEPEER_PLAYBACK_WEBHOOK_URL"`
	PlaybackWebhookSecret        string `env:"LIVEPEER_PLAYBACK_WEBHOOK_SECRET"`
	BudgetAlertURL               string `env:"LIVESTREAM_BUDGET_ALERT_URL"`
	BudgetAlertKey               string `env:"LIVESTREAM_BUDGET_ALERT_KEY"`
	IdentityCAFile               string `env:"IDENTITY_MTLS_CA_FILE"`
	IdentityClientCertFile       string `env:"IDENTITY_MTLS_CLIENT_CERT_FILE"`
	IdentityClientKeyFile        string `env:"IDENTITY_MTLS_CLIENT_KEY_FILE"`
	IdentityPeerCertSHA256       string `env:"IDENTITY_MTLS_PEER_CERT_SHA256"`
	IdentitySigningKey           string `env:"IDENTITY_ASSERTION_SIGNING_KEY"`
	BudgetPolicyFile             string `env:"LIVESTREAM_BUDGET_POLICY_FILE"`
	BudgetPolicySHA256           string `env:"LIVESTREAM_BUDGET_POLICY_SHA256"`
	BudgetPolicyDocument         string `env:"LIVESTREAM_BUDGET_POLICY_DOCUMENT"`
	WebhookTimestampUnit         string `env:"LIVEPEER_WEBHOOK_TIMESTAMP_UNIT"`
	ConnectedEnabled             bool   `env:"LIVESTREAM_CONNECTED_ENABLED"`
	MaxGlobalStreams             int    `env:"MAX_GLOBAL_STREAMS"`
	ActionEnvironment            string `env:"ACTION_ENVIRONMENT"`
	ActionAuditKey               string `env:"ACTION_AUDIT_KEY"`
	ViewerTokenKey               string `env:"LIVESTREAM_VIEWER_TOKEN_KEY"`
	VaultTransitURL              string `env:"LIVESTREAM_VAULT_TRANSIT_URL"`
	VaultTransitToken            string `env:"LIVESTREAM_VAULT_TRANSIT_TOKEN"`
	VaultTransitKey              string `env:"LIVESTREAM_VAULT_TRANSIT_KEY"`
	IngestEndpoint               string `env:"LIVESTREAM_INGEST_ENDPOINT"`
	ProviderMode                 string `env:"LIVEPEER_PROVIDER_MODE"`
	DeploymentRealm              string `env:"DEPLOYMENT_REALM"`
	PrivateFixtureManifestFile   string `env:"PRIVATE_FIXTURE_MANIFEST_FILE"`
	PrivateFixtureManifestSHA256 string `env:"PRIVATE_FIXTURE_MANIFEST_SHA256"`
	RPCURL                       string `env:"RPC_URL"`
	RuntimeZone                  string `env:"ZONE"`
	RuntimeMetricsEnvironment    string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	RuntimeProfile               string `env:"RUNTIME_PROFILE"`
	BindAddress                  string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile            string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetSHA256          string `env:"DB_BUDGET_SHA256"`
	RuntimeBudgetDocument        string `env:"DB_BUDGET_DOCUMENT"`
	RuntimePoolID                string `env:"DB_POOL_ID"`
	RuntimeInstanceID            string `env:"INSTANCE_ID"`
	ExpectedSchemaFile           string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaSHA256         string `env:"EXPECTED_SCHEMA_SHA256"`
	ExpectedSchemaDocument       string `env:"EXPECTED_SCHEMA_DOCUMENT"`
	IdentityURL                  string `env:"IDENTITY_SERVICE_URL"`
	IdentityServiceToken         string `env:"IDENTITY_SERVICE_TOKEN"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
	NodeEnv               string `env:"NODE_ENV" envDefault:"production"`

	DatabaseURL string `env:"DATABASE_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	Port     int    `env:"PORT" envDefault:"3000"`

	LivepeerAPIKey string `env:"LIVEPEER_API_KEY"`
	LivepeerAPIURL string `env:"LIVEPEER_API_URL" envDefault:"https://livepeer.studio/api"`

	MaxStreamsPerWallet int `env:"MAX_STREAMS_PER_WALLET" envDefault:"3"`

	// Required for readiness; callbacks always require authenticated raw bodies.
	LivepeerWebhookSecret string `env:"LIVEPEER_WEBHOOK_SECRET"`

	// Optional CORS allowlist (comma-separated). Empty/"*" allows all (dev).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// TrustedProxyCIDRs (comma-separated, same name as core/api) lists the L7
	// proxies whose X-Forwarded-For / X-Real-IP are honoured for the client IP
	// and rate-limit key. Honoured outside production and in the production
	// platform runtime (behind the platform edge); empty trusts none.
	TrustedProxyCIDRs string `env:"TRUSTED_PROXY_CIDRS"`

	// DBMaxConns bounds the platform runtime's Postgres pool.
	DBMaxConns int `env:"DB_MAX_CONNS" envDefault:"20"`

	// Platform runtime private ingress: a TLS 1.3 listener requiring a
	// client certificate of the private CA, serving the livestream API only
	// to the gateway (its client certificate pinned by
	// GATEWAY_PEER_CERT_SHA256), which proxies /v2/streams* there. The plain
	// PORT listener then serves only health and metrics.
	PrivateListenAddress   string `env:"LIVESTREAM_PRIVATE_LISTEN_ADDRESS"`
	PrivateTLSCertFile     string `env:"LIVESTREAM_TLS_CERT_FILE"`
	PrivateTLSCertPEM      string `env:"LIVESTREAM_TLS_CERT_PEM"`
	PrivateTLSKeyFile      string `env:"LIVESTREAM_TLS_KEY_FILE"`
	PrivateTLSKeyPEM       string `env:"LIVESTREAM_TLS_KEY_PEM"`
	PrivateTLSClientCAFile string `env:"LIVESTREAM_TLS_CLIENT_CA_FILE"`
	PrivateTLSClientCAPEM  string `env:"LIVESTREAM_TLS_CLIENT_CA_PEM"`
	GatewayPeerCertSHA256  string `env:"GATEWAY_PEER_CERT_SHA256"`
}

// PlatformProfile is the RUNTIME_PROFILE of the production runtime for a
// managed container platform (Railway): Postgres and Redis from
// DATABASE_URL/REDIS_URL, the database schema owned by the migration
// coordinator (spec/production-system/integration/migrations, applied by
// kindle-migrate) and verified at boot and on every readiness probe against
// the coordinator's ExpectedSchema document for livestream_runtime
// (EXPECTED_SCHEMA_FILE + EXPECTED_SCHEMA_SHA256, dbproduction.CheckReadiness;
// the runtime never migrates), and the public read surface (directory, pool listings, stream documents, viewer
// sessions and heartbeats) over the real lifecycle store. It carries no
// broadcast provider (LIVEPEER_PROVIDER_MODE=unavailable): provider-bound
// routes answer BROADCAST_NOT_PROVISIONED. Broadcasting is served only by the
// connected runtime (LIVESTREAM_CONNECTED_ENABLED with the container or
// owned_local profile).
const PlatformProfile = "platform"

// Platform reports whether c selects the production platform runtime.
func (c *Config) Platform() bool {
	return c.NodeEnv == "production" && c.RuntimeProfile == PlatformProfile
}

// ValidatePlatform enforces the production platform runtime's configuration
// after its secrets are resolved.
func (c *Config) ValidatePlatform() error {
	var errs []error
	if c.ConnectedEnabled {
		errs = append(errs, errors.New("LIVESTREAM_CONNECTED_ENABLED requires the container or owned_local runtime profile"))
	}
	if c.ProviderMode != "unavailable" {
		errs = append(errs, errors.New("the platform runtime has no broadcast provider: LIVEPEER_PROVIDER_MODE=unavailable required"))
	}
	if c.BindAddress != "" && c.BindAddress != "0.0.0.0" && c.BindAddress != "::" {
		errs = append(errs, errors.New("BIND_ADDRESS must be empty, 0.0.0.0 or ::"))
	}
	if host, port, err := net.SplitHostPort(c.PrivateListenAddress); err != nil || port == "" || net.ParseIP(host) == nil || port == strconv.Itoa(c.Port) {
		errs = append(errs, errors.New("LIVESTREAM_PRIVATE_LISTEN_ADDRESS must be an IP:port distinct from PORT (e.g. [::]:8443)"))
	}
	if (c.PrivateTLSCertFile == "") == (c.PrivateTLSCertPEM == "") {
		errs = append(errs, errors.New("exactly one of LIVESTREAM_TLS_CERT_FILE and LIVESTREAM_TLS_CERT_PEM is required"))
	}
	if (c.PrivateTLSClientCAFile == "") == (c.PrivateTLSClientCAPEM == "") {
		errs = append(errs, errors.New("exactly one of LIVESTREAM_TLS_CLIENT_CA_FILE and LIVESTREAM_TLS_CLIENT_CA_PEM is required"))
	}
	if (c.PrivateTLSKeyFile == "") == (c.PrivateTLSKeyPEM == "") {
		errs = append(errs, errors.New("exactly one of LIVESTREAM_TLS_KEY_FILE and LIVESTREAM_TLS_KEY_PEM is required"))
	}
	if pin, err := hex.DecodeString(c.GatewayPeerCertSHA256); err != nil || len(pin) != 32 || strings.ToLower(c.GatewayPeerCertSHA256) != c.GatewayPeerCertSHA256 {
		errs = append(errs, errors.New("GATEWAY_PEER_CERT_SHA256 must be the lowercase hex sha256 of the gateway client certificate"))
	}
	if len(c.ViewerTokenKey) < 32 {
		errs = append(errs, errors.New("LIVESTREAM_VIEWER_TOKEN_KEY must be at least 32 bytes"))
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > 200 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be in 1..200"))
	}
	if sum, err := hex.DecodeString(c.ExpectedSchemaSHA256); (c.ExpectedSchemaFile == "" && c.ExpectedSchemaDocument == "") || err != nil || len(sum) != 32 || strings.ToLower(c.ExpectedSchemaSHA256) != c.ExpectedSchemaSHA256 {
		errs = append(errs, errors.New("EXPECTED_SCHEMA_FILE and EXPECTED_SCHEMA_SHA256 (lowercase hex sha256 of the file) are required: the coordinator's ExpectedSchema document for livestream_runtime"))
	}
	origins := 0
	for _, raw := range strings.Split(c.CORSAllowedOrigins, ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS entries must be https origins"))
			break
		}
		origins++
	}
	if origins == 0 {
		errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS must list the web origin(s)"))
	}
	return errors.Join(errs...)
}

var validLevels = map[string]struct{}{"debug": {}, "info": {}, "warn": {}, "error": {}}

// Load parses the process environment into a validated Config.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.registerInlineDocuments()
	return cfg, nil
}

// Validate enforces the same field-level rules as the zod livestreamEnvSchema.
func (c *Config) Validate() error {
	var errs []error
	for _, d := range c.boundDocuments() {
		if d[0] != "" && d[1] != "" {
			errs = append(errs, fmt.Errorf("runtime_document_source_ambiguous: %s_FILE and %s_DOCUMENT are both set", d[3], d[3]))
		}
	}
	unavailable := c.ProviderMode == "unavailable"
	if unavailable {
		if err := c.ValidateUnavailableProvider(); err != nil {
			errs = append(errs, err)
		}
	} else if c.ProviderMode != "" && c.ProviderMode != "available" {
		errs = append(errs, errors.New("LIVEPEER_PROVIDER_MODE invalid"))
	}
	if !unavailable && len(strings.TrimSpace(c.LivepeerWebhookSecret)) < 32 {
		errs = append(errs, errors.New("LIVEPEER_WEBHOOK_SECRET requires at least32 bytes"))
	}

	if err := requireURL("DATABASE_URL", c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(c.RedisURL) == "" {
		errs = append(errs, errors.New("REDIS_URL is required"))
	}
	if err := requireURL("LIVEPEER_API_URL", c.LivepeerAPIURL); err != nil {
		errs = append(errs, err)
	}
	u, e := url.Parse(c.LivepeerAPIURL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		errs = append(errs, errors.New("LIVEPEER_API_URL requires HTTPS without credentials,query or fragment"))
	}
	if !unavailable && strings.TrimSpace(c.LivepeerAPIKey) == "" {
		errs = append(errs, errors.New("LIVEPEER_API_KEY is required (min 1 char)"))
	}
	if _, err := c.TrustedProxies(); err != nil {
		errs = append(errs, err)
	}
	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, fmt.Errorf("LOG_LEVEL %q must be one of debug|info|warn|error", c.LogLevel))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d must be in 1..65535", c.Port))
	}
	if c.MaxStreamsPerWallet <= 0 {
		errs = append(errs, fmt.Errorf("MAX_STREAMS_PER_WALLET %d must be positive", c.MaxStreamsPerWallet))
	}

	return errors.Join(errs...)
}

func requireURL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s is not a valid URL", name)
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
	if c.ProviderMode == "unavailable" {
		if e := c.ValidateUnavailableProvider(); e != nil {
			return nil, nil, e
		}
	} else {
		bindings["LIVEPEER_API_KEY"] = &c.LivepeerAPIKey
		bindings["LIVEPEER_WEBHOOK_SECRET"] = &c.LivepeerWebhookSecret
	}

	if c.Platform() {
		bindings["LIVESTREAM_VIEWER_TOKEN_KEY"] = &c.ViewerTokenKey
		if c.PrivateTLSKeyFile == "" {
			bindings["LIVESTREAM_TLS_KEY_PEM"] = &c.PrivateTLSKeyPEM
		}
	} else if !c.ConnectedEnabled {
		bindings["IDENTITY_SERVICE_TOKEN"] = &c.IdentityServiceToken
	} else {
		bindings["IDENTITY_ASSERTION_SIGNING_KEY"] = &c.IdentitySigningKey
	}
	if c.ConnectedEnabled && !c.Platform() {
		bindings["ACTION_AUDIT_KEY"] = &c.ActionAuditKey
		bindings["LIVESTREAM_VIEWER_TOKEN_KEY"] = &c.ViewerTokenKey
		bindings["LIVESTREAM_VAULT_TRANSIT_TOKEN"] = &c.VaultTransitToken
		bindings["LIVEPEER_PLAYBACK_WEBHOOK_SECRET"] = &c.PlaybackWebhookSecret
		bindings["LIVESTREAM_BUDGET_ALERT_KEY"] = &c.BudgetAlertKey
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
	if c.Platform() {
		if err := c.ValidatePlatform(); err != nil {
			return nil, nil, err
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
	if c.Platform() {
		pool, err := runtimedb.NewPool(ctx, c.DatabaseURL, runtimedb.PoolOptions{MaxConns: int32(c.DBMaxConns), StatementTimeout: 10 * time.Second, LockTimeout: 5 * time.Second})
		if err != nil {
			return nil, err
		}
		if err = c.CheckRuntimeSchema(ctx, pool); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
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
	if want.Service != "media/livestream" {
		return fmt.Errorf("runtime_schema_service_mismatch")
	}
	return dbproduction.CheckReadiness(ctx, pool, want)
}

func ReadBoundDocument(path, digest string, out any) error {
	inline := ""
	if path == "" {
		if value, ok := inlineDocuments.Load(digest); ok {
			inline = value.(string)
		}
	}
	return decodeBoundDocument(path, inline, digest, out)
}

// inlineDocuments maps a pinned sha256 digest to the inline *_DOCUMENT value
// Load registered for it, so ReadBoundDocument callers holding an empty *_FILE
// path resolve the inline sibling; boundBytes still checks the digest.
var inlineDocuments sync.Map

func (c *Config) boundDocuments() [][4]string {
	return [][4]string{
		{c.RuntimeBudgetFile, c.RuntimeBudgetDocument, c.RuntimeBudgetSHA256, "DB_BUDGET"},
		{c.ExpectedSchemaFile, c.ExpectedSchemaDocument, c.ExpectedSchemaSHA256, "EXPECTED_SCHEMA"},
		{c.BudgetPolicyFile, c.BudgetPolicyDocument, c.BudgetPolicySHA256, "LIVESTREAM_BUDGET_POLICY"},
	}
}

func (c *Config) registerInlineDocuments() {
	for _, d := range c.boundDocuments() {
		if d[1] != "" {
			inlineDocuments.Store(d[2], d[1])
		}
	}
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

// TrustedProxies parses TRUSTED_PROXY_CIDRS. The bound production runtimes
// ignore the value so their client-IP behaviour stays exactly as reviewed; the
// platform runtime honours it because every client reaches it through the
// platform edge proxy.
func (c *Config) TrustedProxies() ([]netip.Prefix, error) {
	if c.NodeEnv == "production" && !c.Platform() {
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

// ValidateUnavailableProvider admits LIVEPEER_PROVIDER_MODE=unavailable. Outside
// production and in the production platform runtime no provider is reachable
// from the read surface, so the mode only waives the unused Livepeer key and
// webhook secret.
func (c *Config) ValidateUnavailableProvider() error {
	if (c.NodeEnv != "production" || c.Platform()) && c.ProviderMode == "unavailable" {
		return nil
	}
	if c.NodeEnv != "production" || c.RuntimeProfile != "owned_local" || c.ProviderMode != "unavailable" {
		return errors.New("unavailable_provider_requires_explicit_private_fixture")
	}
	base := sharedconfig.BaseEnv{DeploymentRealm: c.DeploymentRealm, PrivateFixtureManifestFile: c.PrivateFixtureManifestFile, PrivateFixtureManifestSHA256: c.PrivateFixtureManifestSHA256, RPCURL: c.RPCURL}
	_, err := base.PrivateFixtureEvidence()
	return err
}

func (c *Config) ValidateConnected() error {
	provider, err := url.Parse(c.LivepeerAPIURL)
	if err != nil || provider.Scheme != "https" || provider.Host != "livepeer.studio" || provider.Path != "/api" || provider.RawPath != "" || provider.User != nil || provider.RawQuery != "" || provider.Fragment != "" {
		return errors.New("connected_livestream_provider_endpoint_invalid")
	}
	if err = c.Validate(); err != nil {
		return err
	}
	if len(c.PlaybackWebhookSecret) < 32 || len(c.BudgetAlertKey) < 32 || c.PlaybackWebhookID == "" || c.BudgetAlertURL == "" || c.PlaybackWebhookURL == "" {
		return errors.New("livestream_usage_or_playback_authority_missing")
	}
	for _, endpoint := range []string{c.PlaybackWebhookURL, c.BudgetAlertURL} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("livestream_callback_endpoint_invalid")
		}
	}
	if (c.BudgetPolicyFile == "" && c.BudgetPolicyDocument == "") || len(c.BudgetPolicySHA256) != 64 || (c.WebhookTimestampUnit != "seconds" && c.WebhookTimestampUnit != "milliseconds") || c.ProviderMode != "available" || c.NodeEnv != "production" || c.MaxGlobalStreams < 1 || c.MaxGlobalStreams > 100000 || c.MaxStreamsPerWallet > c.MaxGlobalStreams || c.ActionEnvironment == "" || len(c.ActionAuditKey) < 32 || len(c.ViewerTokenKey) < 32 || len(c.VaultTransitToken) < 16 || c.VaultTransitKey == "" {
		return errors.New("connected_livestream_authority_missing")
	}
	for _, raw := range []string{c.VaultTransitURL, c.IdentityURL} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return errors.New("connected_livestream_authority_endpoint_invalid")
		}
	}
	ingest, e := url.Parse(c.IngestEndpoint)
	if e != nil || (ingest.Scheme != "https" && ingest.Scheme != "rtmps") || ingest.Host == "" || ingest.User != nil || ingest.RawQuery != "" || ingest.Fragment != "" {
		return errors.New("connected_livestream_ingest_endpoint_invalid")
	}
	return nil
}

func (c *Config) IdentityAuthority() (sharedauth.RequestIdentityAuthority, error) {
	if c == nil || c.IdentityCAFile == "" || c.IdentityClientCertFile == "" || c.IdentityClientKeyFile == "" {
		return nil, errors.New("livestream_identity_mtls_missing")
	}
	rootPEM, err := os.ReadFile(c.IdentityCAFile)
	if err != nil || len(rootPEM) > 1048576 {
		return nil, errors.New("livestream_identity_ca_unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errors.New("livestream_identity_ca_invalid")
	}
	certificate, err := tls.LoadX509KeyPair(c.IdentityClientCertFile, c.IdentityClientKeyFile)
	if err != nil {
		return nil, errors.New("livestream_identity_client_certificate_invalid")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(c.IdentitySigningKey)
	if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
		return nil, errors.New("livestream_identity_signing_key_invalid")
	}
	key := ed25519.PrivateKey(keyBytes)
	seedKey := ed25519.NewKeyFromSeed(key.Seed())
	if !bytes.Equal(seedKey, key) {
		return nil, errors.New("livestream_identity_signing_key_invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	signer := sharedauth.ServiceRequestIssuer{Issuer: "livestream", Audience: "identity", Key: key, Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
	authority, err := sharedauth.NewMTLSIdentityAuthority(c.IdentityURL, c.IdentityPeerCertSHA256, client, signer)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("livestream_identity_authority_invalid")
	}
	return authority, nil
}

// Material returns the inline PEM value when it is set, else the bytes of
// the regular file at path. Either source must hold 1 byte to 1 MiB.
func Material(path, inline string) ([]byte, error) {
	if inline != "" {
		if len(inline) > 1<<20 {
			return nil, errors.New("inline material too large")
		}
		return []byte(inline), nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
		return nil, errors.New("material file unavailable")
	}
	return os.ReadFile(path)
}
