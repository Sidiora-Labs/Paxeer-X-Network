// Package config loads and validates the media/social environment, mirroring the
// TS chatEnvSchema (src/config.ts) env var names (invariant i8 — matches
// docker-envs/chat.env) and defaults, plus the knobs the Go realtime hub needs
// to stay bounded under the 500K-concurrency bar (i11).
//
// Identity model (2026-06-26 decision): social writes are SIGN-FREE. There is no
// per-action EIP-191 verification; the actor wallet is injected by media/gateway
// (the sole public ingress) via the X-Actor-Wallet header, which this service
// trusts. Admin/moderation routes are still gated by CHAT_ADMIN_API_KEY.
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	runtimedb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	dbproduction "github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the parsed social-service environment.
type Config struct {
	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	RuntimeProfile            string `env:"RUNTIME_PROFILE"`
	BindAddress               string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile         string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetSHA256       string `env:"DB_BUDGET_SHA256"`
	RuntimeBudgetDocument     string `env:"DB_BUDGET_DOCUMENT"`
	RuntimePoolID             string `env:"DB_POOL_ID"`
	RuntimeInstanceID         string `env:"INSTANCE_ID"`
	ExpectedSchemaFile        string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaSHA256      string `env:"EXPECTED_SCHEMA_SHA256"`
	ExpectedSchemaDocument    string `env:"EXPECTED_SCHEMA_DOCUMENT"`
	SocialRuntimeFile         string `env:"SOCIAL_RUNTIME_FILE"`
	SocialRuntimeSHA256       string `env:"SOCIAL_RUNTIME_SHA256"`
	SocialRuntimeDocument     string `env:"SOCIAL_RUNTIME_DOCUMENT"`
	IdentityURL               string `env:"IDENTITY_SERVICE_URL"`
	IdentityServiceToken      string `env:"IDENTITY_SERVICE_TOKEN"`

	// Platform runtime private identity channel to the gateway identity
	// authority (its mTLS listener, IDENTITY_SERVICE_URL=https://gateway.
	// railway.internal:8443): the private CA, this service's client
	// certificate and key (a file, or the PEM itself as a secret), the pinned
	// gateway server certificate and the ed25519 request-assertion key whose
	// public half is the gateway's identityPeers entry for issuer "social".
	IdentityMTLSCAFile         string `env:"IDENTITY_MTLS_CA_FILE"`
	IdentityMTLSClientCertFile string `env:"IDENTITY_MTLS_CLIENT_CERT_FILE"`
	IdentityMTLSClientKeyFile  string `env:"IDENTITY_MTLS_CLIENT_KEY_FILE"`
	IdentityMTLSClientKeyPEM   string `env:"IDENTITY_MTLS_CLIENT_KEY_PEM"`
	IdentityMTLSPeerCertSHA256 string `env:"IDENTITY_MTLS_PEER_CERT_SHA256"`
	IdentitySigningKey         string `env:"IDENTITY_ASSERTION_SIGNING_KEY"`

	// Platform runtime private ingress: a TLS 1.3 listener that requires a
	// client certificate from the private CA. The gateway (pinned by
	// GATEWAY_PEER_CERT_SHA256) reaches the social API there, and the service
	// authorities in SOCIAL_AUTHORITY_PEERS ("dns=sha256,...", e.g. ranking)
	// reach /internal/v2/social/ranking-visibility. The plain PORT listener
	// then serves only health and metrics.
	PrivateListenAddress   string `env:"SOCIAL_PRIVATE_LISTEN_ADDRESS"`
	PrivateTLSCertFile     string `env:"SOCIAL_TLS_CERT_FILE"`
	PrivateTLSKeyFile      string `env:"SOCIAL_TLS_KEY_FILE"`
	PrivateTLSKeyPEM       string `env:"SOCIAL_TLS_KEY_PEM"`
	PrivateTLSClientCAFile string `env:"SOCIAL_TLS_CLIENT_CA_FILE"`
	GatewayPeerCertSHA256  string `env:"GATEWAY_PEER_CERT_SHA256"`
	AuthorityPeers         string `env:"SOCIAL_AUTHORITY_PEERS"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
	NodeEnv               string `env:"NODE_ENV" envDefault:"production"`

	DatabaseURL string `env:"DATABASE_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	Port     int    `env:"PORT" envDefault:"3000"`

	// MaxMessageLength caps pool/dm/comment content length (parity with TS
	// MAX_MESSAGE_LENGTH).
	MaxMessageLength int `env:"MAX_MESSAGE_LENGTH" envDefault:"500"`

	// Per-actor sliding-window rate limit for realtime sends (parity with the
	// TS ws handler's 10s/5-message window).
	RateWindowSeconds  int `env:"RATE_WINDOW_SECONDS" envDefault:"10"`
	MaxPoolMsgsPerWin  int `env:"MAX_POOL_MSGS_PER_WINDOW" envDefault:"5"`
	MaxDmMsgsPerWindow int `env:"MAX_DM_MSGS_PER_WINDOW" envDefault:"5"`

	// Realtime hub bounds (i11 backpressure). WSSendBuffer is the per-connection
	// outbound queue depth; a client that can't keep up past it is evicted rather
	// than allowed to grow memory without bound.
	WSSendBuffer       int   `env:"WS_SEND_BUFFER" envDefault:"64"`
	WSWriteTimeoutSecs int   `env:"WS_WRITE_TIMEOUT_SECONDS" envDefault:"10"`
	WSReadLimitBytes   int64 `env:"WS_READ_LIMIT_BYTES" envDefault:"4096"`

	// ChatAdminAPIKey gates the /admin/* moderation routes. Empty => admin API
	// is disabled (routes return 503), parity with the TS service.
	ChatAdminAPIKey string `env:"CHAT_ADMIN_API_KEY"`

	// Optional CORS allowlist (comma-separated). Empty/"*" allows all (dev).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// SocialCursorKey is the chat replay-cursor HMAC key of the production
	// platform runtime (unpadded base64url, >= 32 decoded bytes). It is a
	// secret resolved through SECRETS_PROVIDER.
	SocialCursorKey string `env:"SOCIAL_CURSOR_KEY"`
	// DBMaxConns bounds the platform runtime's Postgres pool.
	DBMaxConns int `env:"DB_MAX_CONNS" envDefault:"20"`
}

// PlatformProfile is the RUNTIME_PROFILE of the production runtime for a
// managed container platform's private network (Railway): Postgres and Redis
// from DATABASE_URL/REDIS_URL, the database schema owned by the migration
// coordinator (spec/production-system/integration/migrations, applied by
// kindle-migrate) and verified at boot and on every readiness probe against
// the coordinator's ExpectedSchema document for chat_runtime
// (EXPECTED_SCHEMA_FILE + EXPECTED_SCHEMA_SHA256, dbproduction.CheckReadiness;
// the runtime never migrates), and identity verified per request at the gateway identity authority over its
// private mTLS listener (IDENTITY_SERVICE_URL + IDENTITY_MTLS_* +
// IDENTITY_ASSERTION_SIGNING_KEY, issuer "social"), with the social API served
// on a private mTLS listener (SOCIAL_PRIVATE_LISTEN_ADDRESS + SOCIAL_TLS_*),
// instead of the bound mTLS/NATS runtime document of the container and
// owned_local profiles.
const PlatformProfile = "platform"

// Platform reports whether c selects the production platform runtime.
func (c *Config) Platform() bool {
	return c.NodeEnv == "production" && c.RuntimeProfile == PlatformProfile
}

// PlatformIdentityEndpoint returns the gateway identity authority origin:
// https only (the gateway serves /internal/v2/identity/* solely on its mTLS
// listener), with no path, credentials, query or fragment. A trailing "/" is
// removed.
func (c *Config) PlatformIdentityEndpoint() (string, error) {
	raw := strings.TrimRight(strings.TrimSpace(c.IdentityURL), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return "", errors.New("IDENTITY_SERVICE_URL must be an https origin without path, credentials, query or fragment (the gateway identity mTLS listener)")
	}
	return raw, nil
}

// PlatformIdentityClient describes the platform runtime's private identity
// channel for sharedauth.NewMTLSIdentityClient.
func (c *Config) PlatformIdentityClient() (sharedauth.MTLSIdentityClientConfig, error) {
	endpoint, err := c.PlatformIdentityEndpoint()
	if err != nil {
		return sharedauth.MTLSIdentityClientConfig{}, err
	}
	return sharedauth.MTLSIdentityClientConfig{Endpoint: endpoint, PeerCertSHA256: c.IdentityMTLSPeerCertSHA256, CAFile: c.IdentityMTLSCAFile, ClientCertFile: c.IdentityMTLSClientCertFile, ClientKeyFile: c.IdentityMTLSClientKeyFile, ClientKeyPEM: c.IdentityMTLSClientKeyPEM, Issuer: "social", SigningKey: c.IdentitySigningKey}, nil
}

// PlatformAuthorityPeers parses SOCIAL_AUTHORITY_PEERS: comma-separated
// "dns=sha256" pairs naming the service authorities (their client
// certificate's DNS SAN and DER sha256) admitted to /internal/v2/social/*.
func (c *Config) PlatformAuthorityPeers() (map[string]string, error) {
	peers := map[string]string{}
	for _, entry := range strings.Split(c.AuthorityPeers, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		dns, pin, ok := strings.Cut(entry, "=")
		dns = strings.ToLower(strings.TrimSpace(dns))
		pin = strings.TrimSpace(pin)
		if !ok || dns == "" || strings.ContainsAny(dns, " /:*") || !lowerHex64(pin) || peers[dns] != "" {
			return nil, errors.New("SOCIAL_AUTHORITY_PEERS entries must be unique dns=sha256 pairs")
		}
		peers[dns] = pin
	}
	if len(peers) > 16 {
		return nil, errors.New("SOCIAL_AUTHORITY_PEERS allows at most 16 peers")
	}
	return peers, nil
}

func lowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// PlatformCursorKey decodes SOCIAL_CURSOR_KEY.
func (c *Config) PlatformCursorKey() ([]byte, error) {
	key, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(c.SocialCursorKey))
	if err != nil || len(key) < 32 {
		return nil, errors.New("SOCIAL_CURSOR_KEY must be unpadded base64url of at least 32 bytes")
	}
	return key, nil
}

// ValidatePlatform enforces the production platform runtime's configuration
// after its secrets are resolved.
func (c *Config) ValidatePlatform() error {
	var errs []error
	if c.BindAddress != "" && c.BindAddress != "0.0.0.0" && c.BindAddress != "::" {
		errs = append(errs, errors.New("BIND_ADDRESS must be empty, 0.0.0.0 or ::"))
	}
	if _, err := c.PlatformIdentityEndpoint(); err != nil {
		errs = append(errs, err)
	}
	if c.IdentityMTLSCAFile == "" || c.IdentityMTLSClientCertFile == "" {
		errs = append(errs, errors.New("IDENTITY_MTLS_CA_FILE and IDENTITY_MTLS_CLIENT_CERT_FILE are required"))
	}
	if (c.IdentityMTLSClientKeyFile == "") == (c.IdentityMTLSClientKeyPEM == "") {
		errs = append(errs, errors.New("exactly one of IDENTITY_MTLS_CLIENT_KEY_FILE and IDENTITY_MTLS_CLIENT_KEY_PEM is required"))
	}
	if !lowerHex64(c.IdentityMTLSPeerCertSHA256) {
		errs = append(errs, errors.New("IDENTITY_MTLS_PEER_CERT_SHA256 must be the lowercase hex sha256 of the gateway server certificate"))
	}
	if _, err := sharedauth.DecodeIdentitySigningKey(c.IdentitySigningKey); err != nil {
		errs = append(errs, errors.New("IDENTITY_ASSERTION_SIGNING_KEY: "+err.Error()))
	}
	if host, port, err := net.SplitHostPort(c.PrivateListenAddress); err != nil || port == "" || net.ParseIP(host) == nil || port == strconv.Itoa(c.Port) {
		errs = append(errs, errors.New("SOCIAL_PRIVATE_LISTEN_ADDRESS must be an IP:port distinct from PORT (e.g. [::]:8443)"))
	}
	if c.PrivateTLSCertFile == "" || c.PrivateTLSClientCAFile == "" {
		errs = append(errs, errors.New("SOCIAL_TLS_CERT_FILE and SOCIAL_TLS_CLIENT_CA_FILE are required"))
	}
	if (c.PrivateTLSKeyFile == "") == (c.PrivateTLSKeyPEM == "") {
		errs = append(errs, errors.New("exactly one of SOCIAL_TLS_KEY_FILE and SOCIAL_TLS_KEY_PEM is required"))
	}
	if !lowerHex64(c.GatewayPeerCertSHA256) {
		errs = append(errs, errors.New("GATEWAY_PEER_CERT_SHA256 must be the lowercase hex sha256 of the gateway client certificate"))
	}
	if _, err := c.PlatformAuthorityPeers(); err != nil {
		errs = append(errs, err)
	}
	if _, err := c.PlatformCursorKey(); err != nil {
		errs = append(errs, err)
	}
	if c.ChatAdminAPIKey != "" && len(c.ChatAdminAPIKey) < 32 {
		errs = append(errs, errors.New("CHAT_ADMIN_API_KEY must be empty or at least 32 characters"))
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > 200 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be in 1..200"))
	}
	if (c.ExpectedSchemaFile == "" && c.ExpectedSchemaDocument == "") || !lowerHex64(c.ExpectedSchemaSHA256) {
		errs = append(errs, errors.New("EXPECTED_SCHEMA_FILE and EXPECTED_SCHEMA_SHA256 (lowercase hex sha256 of the file) are required: the coordinator's ExpectedSchema document for chat_runtime"))
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

// Validate enforces field-level rules beyond presence.
func (c *Config) Validate() error {
	var errs []error
	for _, d := range c.boundDocuments() {
		if d[0] != "" && d[1] != "" {
			errs = append(errs, errors.New("runtime_document_source_ambiguous"))
			break
		}
	}

	if err := requireURL("DATABASE_URL", c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(c.RedisURL) == "" {
		errs = append(errs, errors.New("REDIS_URL is required"))
	}
	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, fmt.Errorf("LOG_LEVEL %q must be one of debug|info|warn|error", c.LogLevel))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d must be in 1..65535", c.Port))
	}
	if c.MaxMessageLength <= 0 {
		errs = append(errs, fmt.Errorf("MAX_MESSAGE_LENGTH %d must be positive", c.MaxMessageLength))
	}
	if c.RateWindowSeconds <= 0 {
		errs = append(errs, fmt.Errorf("RATE_WINDOW_SECONDS %d must be positive", c.RateWindowSeconds))
	}
	if c.MaxPoolMsgsPerWin <= 0 {
		errs = append(errs, fmt.Errorf("MAX_POOL_MSGS_PER_WINDOW %d must be positive", c.MaxPoolMsgsPerWin))
	}
	if c.MaxDmMsgsPerWindow <= 0 {
		errs = append(errs, fmt.Errorf("MAX_DM_MSGS_PER_WINDOW %d must be positive", c.MaxDmMsgsPerWindow))
	}
	if c.WSSendBuffer <= 0 {
		errs = append(errs, fmt.Errorf("WS_SEND_BUFFER %d must be positive", c.WSSendBuffer))
	}
	if c.WSWriteTimeoutSecs <= 0 {
		errs = append(errs, fmt.Errorf("WS_WRITE_TIMEOUT_SECONDS %d must be positive", c.WSWriteTimeoutSecs))
	}
	if c.WSReadLimitBytes <= 0 {
		errs = append(errs, fmt.Errorf("WS_READ_LIMIT_BYTES %d must be positive", c.WSReadLimitBytes))
	}

	return errors.Join(errs...)
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
	if c.ChatAdminAPIKey != "" {
		bindings["CHAT_ADMIN_API_KEY"] = &c.ChatAdminAPIKey
	}
	if c.Platform() {
		bindings["IDENTITY_ASSERTION_SIGNING_KEY"] = &c.IdentitySigningKey
		bindings["SOCIAL_CURSOR_KEY"] = &c.SocialCursorKey
		if c.IdentityMTLSClientKeyFile == "" {
			bindings["IDENTITY_MTLS_CLIENT_KEY_PEM"] = &c.IdentityMTLSClientKeyPEM
		}
		if c.PrivateTLSKeyFile == "" {
			bindings["SOCIAL_TLS_KEY_PEM"] = &c.PrivateTLSKeyPEM
		}
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
	if want.Service != "media/social" {
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

func (c *Config) boundDocuments() [][3]string {
	return [][3]string{
		{c.RuntimeBudgetFile, c.RuntimeBudgetDocument, c.RuntimeBudgetSHA256},
		{c.ExpectedSchemaFile, c.ExpectedSchemaDocument, c.ExpectedSchemaSHA256},
		{c.SocialRuntimeFile, c.SocialRuntimeDocument, c.SocialRuntimeSHA256},
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

type SocialRuntimeBinding struct {
	Version                      int                  `json:"version"`
	AuthorityPeers               map[string]string    `json:"authorityPeers"`
	IdentityEndpoint             string               `json:"identityEndpoint"`
	IdentityServerPeerCertSHA256 string               `json:"identityServerPeerCertSHA256"`
	IdentitySigningKeyReference  string               `json:"identitySigningKeyReference"`
	GatewayDNS                   string               `json:"gatewayDNS"`
	GatewayCertSHA256            string               `json:"gatewayCertSHA256"`
	CAFile                       string               `json:"caFile"`
	CASHA256                     string               `json:"caSHA256"`
	ServerCertFile               string               `json:"serverCertFile"`
	ServerCertSHA256             string               `json:"serverCertSHA256"`
	ServerKeyReference           string               `json:"serverKeyReference"`
	ClientCertFile               string               `json:"clientCertFile"`
	ClientCertSHA256             string               `json:"clientCertSHA256"`
	ClientKeyReference           string               `json:"clientKeyReference"`
	NATSCredentialsReference     string               `json:"natsCredentialsReference"`
	NATSURL                      string               `json:"natsURL"`
	ChatStream                   string               `json:"chatStream"`
	ChatSubject                  string               `json:"chatSubject"`
	ChatDurable                  string               `json:"chatDurable"`
	EventStream                  string               `json:"eventStream"`
	DeliverySchema               string               `json:"deliverySchema"`
	ConsumerGeneration           string               `json:"consumerGeneration"`
	ErasureSubject               string               `json:"erasureSubject"`
	ErasureStream                string               `json:"erasureStream"`
	ErasureDurable               string               `json:"erasureDurable"`
	PolicySubject                string               `json:"policySubject"`
	PolicyStream                 string               `json:"policyStream"`
	PolicyDurable                string               `json:"policyDurable"`
	RecoverySchema               string               `json:"recoverySchema"`
	RecoveryEpoch                int64                `json:"recoveryEpoch"`
	RecoveryRegion               string               `json:"recoveryRegion"`
	CursorKeyReference           string               `json:"cursorKeyReference"`
	IdentitySchema               string               `json:"identitySchema"`
	AccountSchema                string               `json:"accountSchema"`
	IndexerSchema                string               `json:"indexerSchema"`
	SocialSchema                 string               `json:"socialSchema"`
	InteractionSubjects          map[string]string    `json:"interactionSubjects"`
	MentionSubject               string               `json:"mentionSubject"`
	NotificationEndpoint         string               `json:"notificationEndpoint"`
	NotificationTokenReference   string               `json:"notificationTokenReference"`
	PendingOutboxLimit           int                  `json:"pendingOutboxLimit"`
	MaxOutboxAgeSeconds          int                  `json:"maxOutboxAgeSeconds"`
	Voice                        *VoiceRuntimeBinding `json:"voice"`
}
type VoiceRuntimeBinding struct {
	Endpoint                    string `json:"endpoint"`
	APIKeyReference             string `json:"apiKeyReference"`
	APISecretReference          string `json:"apiSecretReference"`
	QualificationFile           string `json:"qualificationFile"`
	QualificationSHA256         string `json:"qualificationSHA256"`
	PolicyVersion               string `json:"policyVersion"`
	RecordingEnabled            bool   `json:"recordingEnabled"`
	RecordingPolicyVersion      string `json:"recordingPolicyVersion"`
	RecordingPurpose            string `json:"recordingPurpose"`
	RecordingRetentionSeconds   int    `json:"recordingRetentionSeconds"`
	RecordingEndpoint           string `json:"recordingEndpoint"`
	RecordingRegion             string `json:"recordingRegion"`
	RecordingBucket             string `json:"recordingBucket"`
	RecordingAccessKeyReference string `json:"recordingAccessKeyReference"`
	RecordingSecretReference    string `json:"recordingSecretReference"`
}
