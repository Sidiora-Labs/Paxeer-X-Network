// Package config loads and validates the media/gateway environment. The gateway
// is the PUBLIC media edge (SECTION 15): it authenticates users once (EIP-191 ->
// JWT session), fronts media/social over a REST + WS tunnel injecting the trusted
// X-Actor-Wallet header, serves media bytes from R2 with CDN caching, and guards
// the token-create upload before forwarding it to media/metadata.
//
// It is STATELESS apart from Redis (auth nonces, hot object cache, rate limiter):
// it owns no Postgres schema and runs no migrations. Env var names follow the
// shared S3_* convention (invariant i8) so deploy configs port across unchanged.
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// Config is the parsed gateway environment.
type Config struct {
	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	RuntimeProfile            string `env:"RUNTIME_PROFILE"`
	BindAddress               string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile         string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetSHA256       string `env:"DB_BUDGET_SHA256"`
	RuntimePoolID             string `env:"DB_POOL_ID"`
	RuntimeInstanceID         string `env:"INSTANCE_ID"`
	ExpectedSchemaFile        string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaSHA256      string `env:"EXPECTED_SCHEMA_SHA256"`
	DatabaseURL               string `env:"DATABASE_URL"`
	IdentityConfigSHA256      string `env:"IDENTITY_CONFIG_SHA256"`
	IdentityConfigFile        string `env:"IDENTITY_CONFIG_FILE"`
	GatewayRuntimeFile        string `env:"GATEWAY_RUNTIME_FILE"`
	GatewayRuntimeSHA256      string `env:"GATEWAY_RUNTIME_SHA256"`
	IdentityServiceToken      string `env:"IDENTITY_SERVICE_TOKEN"`
	IdentityLimiterKey        string `env:"IDENTITY_LIMITER_KEY"`
	IdentitySigningSecret     string `env:"IDENTITY_SIGNING_SECRET"`
	RPCURL                    string `env:"RPC_URL"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
	NodeEnv               string `env:"NODE_ENV" envDefault:"production"`

	RedisURL string `env:"REDIS_URL,required"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	Port     int    `env:"PORT" envDefault:"3000"`

	// PublicURL is the externally-reachable base of this gateway (used to build
	// absolute media URLs). Empty falls back to the incoming request host.
	PublicURL string `env:"PUBLIC_URL"`

	// ── Session auth (EIP-191 SIWE -> JWT) ──────────────────────────────────
	// JWTSecret signs session tokens (HMAC-SHA256). Required and must be strong.
	JWTSecret string `env:"GATEWAY_JWT_SECRET,required"`
	// JWTTTLSeconds is the session lifetime (default 24h).
	JWTTTLSeconds int `env:"GATEWAY_JWT_TTL_SECONDS" envDefault:"86400"`
	// NonceTTLSeconds bounds how long a login nonce is valid (default 5m).
	NonceTTLSeconds int `env:"GATEWAY_NONCE_TTL_SECONDS" envDefault:"300"`
	// AppDomain is embedded in the SIWE sign-in message for user clarity.
	AppDomain string `env:"GATEWAY_APP_DOMAIN" envDefault:"kindlelaunch"`

	// ── Upstreams the edge fronts ───────────────────────────────────────────
	// SocialHTTPURL / SocialWSURL are the internal base URLs of media/social.
	SocialHTTPURL string `env:"SOCIAL_HTTP_URL,required"`
	SocialWSURL   string `env:"SOCIAL_WS_URL,required"`
	// MetadataUploadURL is the internal base URL of media/metadata (the
	// authoritative upload writer the create-wizard upload is forwarded to).
	MetadataUploadURL string `env:"METADATA_UPLOAD_URL,required"`
	// UserHTTPURL is the internal base URL of media/user (e.g.
	// http://user.railway.internal:8080). Outside production the gateway
	// forwards the development account owner writes there; unset, those
	// routes answer 503 DEPENDENCY_UNAVAILABLE.
	UserHTTPURL string `env:"USER_HTTP_URL"`
	// UpstreamTimeoutSeconds caps proxied REST/upload calls (default 30s).
	UpstreamTimeoutSeconds int `env:"UPSTREAM_TIMEOUT_SECONDS" envDefault:"30"`

	// ── Object storage (Cloudflare R2 / S3-compatible) ──────────────────────
	S3Endpoint        string `env:"S3_ENDPOINT,required"`
	S3AccessKeyID     string `env:"S3_ACCESS_KEY_ID,required"`
	S3SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY,required"`
	S3Region          string `env:"S3_REGION" envDefault:"auto"`
	// Logical bucket map. At least one must be set; an unset bucket makes its
	// logical name 404 at the edge. token=metadata assets, user=profile assets,
	// og=pnl share cards.
	TokenBucket string `env:"METADATA_BUCKET"`
	UserBucket  string `env:"USER_BUCKET"`
	OGBucket    string `env:"OG_BUCKET"`
	// ObjectCacheMaxBytes is the largest object body cached in Redis (default
	// 256KiB). Larger objects stream straight from R2 (no cache).
	ObjectCacheMaxBytes int64 `env:"OBJECT_CACHE_MAX_BYTES" envDefault:"262144"`
	// ObjectCacheTTLSeconds is the Redis hot-cache TTL for objects (default 5m).
	ObjectCacheTTLSeconds int `env:"OBJECT_CACHE_TTL_SECONDS" envDefault:"300"`

	// ── Upload edge guard ───────────────────────────────────────────────────
	MaxUploadBytes int64 `env:"MAX_UPLOAD_BYTES" envDefault:"6291456"` // 6 MiB

	// ── Public ingress rate limiting (i12) ──────────────────────────────────
	RateLimitMax           int `env:"RATE_LIMIT_MAX" envDefault:"120"`
	RateLimitWindowSeconds int `env:"RATE_LIMIT_WINDOW_SECONDS" envDefault:"60"`

	// Optional CORS allowlist (comma-separated). Empty/"*" allows all (dev).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`
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
	return cfg, nil
}

// Buckets returns the configured logical-name -> bucket map, omitting unset ones.
func (c *Config) Buckets() map[string]string {
	m := make(map[string]string, 3)
	if s := strings.TrimSpace(c.TokenBucket); s != "" {
		m["token"] = s
	}
	if s := strings.TrimSpace(c.UserBucket); s != "" {
		m["user"] = s
	}
	if s := strings.TrimSpace(c.OGBucket); s != "" {
		m["og"] = s
	}
	return m
}

// Validate enforces field-level rules beyond presence.
func (c *Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.RedisURL) == "" {
		errs = append(errs, errors.New("REDIS_URL is required"))
	}
	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, fmt.Errorf("LOG_LEVEL %q must be one of debug|info|warn|error", c.LogLevel))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d must be in 1..65535", c.Port))
	}
	if len(strings.TrimSpace(c.JWTSecret)) < 16 {
		errs = append(errs, errors.New("GATEWAY_JWT_SECRET must be at least 16 characters"))
	}
	if c.JWTTTLSeconds <= 0 {
		errs = append(errs, fmt.Errorf("GATEWAY_JWT_TTL_SECONDS %d must be positive", c.JWTTTLSeconds))
	}
	if c.NonceTTLSeconds <= 0 {
		errs = append(errs, fmt.Errorf("GATEWAY_NONCE_TTL_SECONDS %d must be positive", c.NonceTTLSeconds))
	}
	for name, raw := range map[string]string{
		"SOCIAL_HTTP_URL":     c.SocialHTTPURL,
		"SOCIAL_WS_URL":       c.SocialWSURL,
		"METADATA_UPLOAD_URL": c.MetadataUploadURL,
		"S3_ENDPOINT":         c.S3Endpoint,
	} {
		if err := requireURL(name, raw); err != nil {
			errs = append(errs, err)
		}
	}
	if strings.TrimSpace(c.UserHTTPURL) != "" {
		if err := requireURL("USER_HTTP_URL", c.UserHTTPURL); err != nil {
			errs = append(errs, err)
		}
	}
	if c.UpstreamTimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("UPSTREAM_TIMEOUT_SECONDS %d must be positive", c.UpstreamTimeoutSeconds))
	}
	if strings.TrimSpace(c.S3AccessKeyID) == "" {
		errs = append(errs, errors.New("S3_ACCESS_KEY_ID is required"))
	}
	if strings.TrimSpace(c.S3SecretAccessKey) == "" {
		errs = append(errs, errors.New("S3_SECRET_ACCESS_KEY is required"))
	}
	if len(c.Buckets()) == 0 {
		errs = append(errs, errors.New("at least one of METADATA_BUCKET/USER_BUCKET/OG_BUCKET is required"))
	}
	if c.ObjectCacheMaxBytes < 0 {
		errs = append(errs, fmt.Errorf("OBJECT_CACHE_MAX_BYTES %d must be non-negative", c.ObjectCacheMaxBytes))
	}
	if c.ObjectCacheTTLSeconds <= 0 {
		errs = append(errs, fmt.Errorf("OBJECT_CACHE_TTL_SECONDS %d must be positive", c.ObjectCacheTTLSeconds))
	}
	if c.MaxUploadBytes <= 0 {
		errs = append(errs, fmt.Errorf("MAX_UPLOAD_BYTES %d must be positive", c.MaxUploadBytes))
	}
	if c.RateLimitMax <= 0 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_MAX %d must be positive", c.RateLimitMax))
	}
	if c.RateLimitWindowSeconds <= 0 {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_WINDOW_SECONDS %d must be positive", c.RateLimitWindowSeconds))
	}

	if c.NodeEnv == "production" {
		if c.GatewayRuntimeFile == "" || len(c.GatewayRuntimeSHA256) != 64 {
			errs = append(errs, errors.New("gateway_runtime_binding_required"))
		}
		origin, err := url.Parse(c.PublicURL)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			errs = append(errs, errors.New("public_https_origin_required"))
		}
		seen := map[string]bool{}
		for _, raw := range strings.Split(c.CORSAllowedOrigins, ",") {
			u, e := url.Parse(raw)
			if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || seen[raw] {
				errs = append(errs, errors.New("exact_https_cors_origins_required"))
				break
			}
			seen[raw] = true
		}
		if !seen[c.PublicURL] {
			errs = append(errs, errors.New("public_origin_not_allowed"))
		}
		if c.ObjectCacheTTLSeconds > 86400 || c.ObjectCacheMaxBytes > 1048576 {
			errs = append(errs, errors.New("media_cache_bound_invalid"))
		}
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
	bindings["REDIS_URL"] = &c.RedisURL
	bindings["GATEWAY_JWT_SECRET"] = &c.JWTSecret
	bindings["S3_ACCESS_KEY_ID"] = &c.S3AccessKeyID
	bindings["S3_SECRET_ACCESS_KEY"] = &c.S3SecretAccessKey

	bindings["DATABASE_URL"] = &c.DatabaseURL
	bindings["IDENTITY_SERVICE_TOKEN"] = &c.IdentityServiceToken
	bindings["IDENTITY_LIMITER_KEY"] = &c.IdentityLimiterKey
	bindings["IDENTITY_SIGNING_SECRET"] = &c.IdentitySigningSecret
	bindings["RPC_URL"] = &c.RPCURL
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
	if want.Service != "media/gateway" {
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

type RuntimePoolSplit struct {
	IdentityPoolID             string `json:"identityPoolId"`
	IdentityMaxConnections     int32  `json:"identityMaxConnections"`
	ControlPoolID              string `json:"controlPoolId"`
	ControlMaxConnections      int32  `json:"controlMaxConnections"`
	ControlCredentialReference string `json:"controlCredentialReference"`
}

func (c *Config) OpenSplitRuntimePools(ctx context.Context, split RuntimePoolSplit) (*pgxpool.Pool, *pgxpool.Pool, error) {
	if c.NodeEnv != "production" || split.IdentityPoolID != c.RuntimePoolID || split.ControlPoolID == "" || split.ControlPoolID == split.IdentityPoolID || split.IdentityMaxConnections < 1 || split.ControlMaxConnections < 1 || split.ControlCredentialReference == "" {
		return nil, nil, fmt.Errorf("explicit_runtime_pool_split_required")
	}
	if c.RuntimeProfile == "container" {
		if c.Port != 8080 || c.BindAddress != "0.0.0.0" {
			return nil, nil, fmt.Errorf("container_bind_mismatch")
		}
	} else if c.RuntimeProfile != "owned_local" || c.BindAddress != "127.0.0.1" {
		return nil, nil, fmt.Errorf("runtime_profile_required")
	}
	data, err := os.ReadFile(c.RuntimeBudgetFile)
	if err != nil || len(data) > 1048576 || len(c.RuntimeBudgetSHA256) != 64 || fmt.Sprintf("%x", sha256.Sum256(data)) != c.RuntimeBudgetSHA256 {
		return nil, nil, fmt.Errorf("runtime_budget_binding_mismatch")
	}
	budget, err := dbproduction.ParseConnectionBudget(data)
	if err != nil {
		return nil, nil, err
	}
	options, err := budget.BudgetPoolOptions("mediagatewayd", c.RuntimePoolID, c.RuntimeInstanceID)
	if err != nil {
		return nil, nil, err
	}
	if options.Transport != runtimedb.TransportDirect || options.DatabaseUser != "identity_runtime" || int64(split.IdentityMaxConnections)+int64(split.ControlMaxConnections) > int64(options.MaxConns) {
		return nil, nil, fmt.Errorf("runtime_pool_split_exceeds_reviewed_allocation")
	}
	provider, err := sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(c.SecretsProvider), CacheTTL: 0})
	if err != nil {
		return nil, nil, err
	}
	credential, err := provider.GetRequired(ctx, split.ControlCredentialReference)
	if err != nil {
		return nil, nil, fmt.Errorf("control_database_credential_unavailable")
	}
	identityOptions := options
	identityOptions.MaxConns = split.IdentityMaxConnections
	controlOptions := options
	controlOptions.MaxConns = split.ControlMaxConnections
	controlOptions.PoolID = split.ControlPoolID
	controlOptions.DatabaseUser = "control_runtime"
	identity, err := runtimedb.NewPool(ctx, c.DatabaseURL, identityOptions)
	if err != nil {
		return nil, nil, err
	}
	control, err := runtimedb.NewPool(ctx, credential, controlOptions)
	if err != nil {
		identity.Close()
		return nil, nil, err
	}
	if err = c.CheckRuntimeSchema(ctx, identity); err != nil {
		identity.Close()
		control.Close()
		return nil, nil, err
	}
	return identity, control, nil
}
