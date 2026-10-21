// Package config loads and validates the media/metadata environment, mirroring
// the TS metadataEnvSchema (src/config.ts) env var names (invariant i8 — matches
// docker-envs/metadata.env) and defaults.
//
// Storage model is BUCKET-PRIMARY (2026-06-26 decision): the S3_* vars are now
// REQUIRED (the TS volume-as-source-of-truth + optional-fallback model is gone).
// Objects are written straight to R2 and served back from it.
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
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the parsed metadata environment.
type Config struct {
	PublicationConfigFile          string `env:"METADATA_PUBLICATION_CONFIG_FILE"`
	PublicationConfigSHA256        string `env:"METADATA_PUBLICATION_CONFIG_SHA256"`
	PublicationAuditKey            string `env:"METADATA_PUBLICATION_AUDIT_KEY"`
	ModerationPeerCertSHA256       string `env:"MODERATION_PEER_CERT_SHA256"`
	IdentityPeerCertSHA256         string `env:"IDENTITY_PEER_CERT_SHA256"`
	MetadataAssertionPrivateKeyHex string `env:"METADATA_ASSERTION_PRIVATE_KEY_HEX"`
	UserPeerCertSHA256             string `env:"USER_PEER_CERT_SHA256"`
	UserAssertionPublicKeyHex      string `env:"USER_ASSERTION_PUBLIC_KEY_HEX"`
	TLSCertFile                    string `env:"METADATA_TLS_CERT_FILE"`
	TLSKeyFile                     string `env:"METADATA_TLS_KEY_FILE"`
	TLSClientCAFile                string `env:"METADATA_TLS_CLIENT_CA_FILE"`
	GatewayPublicKeyHex            string `env:"GATEWAY_ASSERTION_PUBLIC_KEY_HEX"`
	GatewayPeerCertSHA256          string `env:"GATEWAY_PEER_CERT_SHA256"`
	MetadataSchema                 string `env:"METADATA_SCHEMA" envDefault:"metadata"`
	CanonicalSchema                string `env:"CANONICAL_SCHEMA" envDefault:"indexer"`
	QuarantineBucket               string `env:"S3_QUARANTINE_BUCKET"`
	ApprovedBucket                 string `env:"S3_APPROVED_BUCKET"`
	RasterExecutable               string `env:"RASTER_EXECUTABLE"`
	RasterCgroupParent             string `env:"RASTER_CGROUP_PARENT"`
	ClamAVHost                     string `env:"CLAMAV_HOST"`
	ClamAVPort                     int    `env:"CLAMAV_PORT" envDefault:"3310"`
	IdentityEndpoint               string `env:"IDENTITY_ENDPOINT"`
	IdentityServiceToken           string `env:"IDENTITY_SERVICE_TOKEN"`
	ModerationEndpoint             string `env:"MODERATION_ENDPOINT"`
	ModerationBearerKey            string `env:"MODERATION_BEARER_KEY"`
	MediaScanIntervalSeconds       int    `env:"MEDIA_SCAN_INTERVAL_SECONDS" envDefault:"5"`
	MediaExpiryIntervalSeconds     int    `env:"MEDIA_EXPIRY_INTERVAL_SECONDS" envDefault:"300"`

	// Ingress selects how production requests reach the service:
	//   "mtls" (default): the private channel only — TLS 1.3 with a verified
	//     gateway client certificate and a gateway-signed service assertion.
	//   "edge": a platform edge (e.g. Railway) terminates public TLS and
	//     forwards plain HTTP; every write and every asset read is
	//     authenticated against the identity service's session introspection
	//     (IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN).
	Ingress string `env:"METADATA_INGRESS" envDefault:"mtls"`
	// RasterIsolation selects the untrusted-raster worker sandbox: "cgroup"
	// (default; a delegated cgroup v2 group per decode) or "rlimit" (a
	// separate process that applies hard kernel resource limits to itself
	// before reading input), for platforms without cgroup delegation.
	RasterIsolation string `env:"RASTER_ISOLATION" envDefault:"cgroup"`
	// ModerationSource selects the visibility authority: "service" (default;
	// media/social over pinned mTLS) or "database" (media/social's
	// column-scoped safety_visibility_v2 view in MODERATION_SCHEMA, read on
	// the shared database).
	ModerationSource string `env:"MODERATION_SOURCE" envDefault:"service"`
	ModerationSchema string `env:"MODERATION_SCHEMA" envDefault:"chat"`
	// TrustedProxyCIDRs lists the platform edge proxies whose forwarded
	// client address is honoured by the per-IP rate limit (edge ingress).
	TrustedProxyCIDRs string `env:"TRUSTED_PROXY_CIDRS"`
	// Inline, digest-bound alternatives to the runtime document files, for
	// platforms that cannot mount files: standard base64 of the exact file
	// bytes. The *_SHA256 binding is enforced identically. Set either the
	// file or the document, never both.
	RuntimeBudgetDocument     string `env:"DB_BUDGET_DOCUMENT"`
	ExpectedSchemaDocument    string `env:"EXPECTED_SCHEMA_DOCUMENT"`
	PublicationConfigDocument string `env:"METADATA_PUBLICATION_CONFIG_DOCUMENT"`

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
	IdentityAudience          string `env:"IDENTITY_AUDIENCE"`
	CanonicalStreamID         string `env:"CANONICAL_STREAM_ID"`
	RPCURL                    string `env:"RPC_URL"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
	NodeEnv               string `env:"NODE_ENV" envDefault:"production"`

	DatabaseURL string `env:"DATABASE_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	Port     int    `env:"PORT" envDefault:"3000"`

	// PublicURL builds absolute image/metadata URLs. When empty, URLs are built
	// from the incoming request host (parity with the TS fallback).
	PublicURL string `env:"PUBLIC_URL"`

	// Object storage (Cloudflare R2 / S3-compatible) — bucket-primary, required.
	S3Endpoint        string `env:"S3_ENDPOINT,required"`
	S3Bucket          string `env:"S3_BUCKET,required"`
	S3AccessKeyID     string `env:"S3_ACCESS_KEY_ID,required"`
	S3SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY,required"`
	S3Region          string `env:"S3_REGION" envDefault:"auto"`

	MaxLogoSizeBytes   int64 `env:"MAX_LOGO_SIZE_BYTES" envDefault:"2097152"`
	MaxBannerSizeBytes int64 `env:"MAX_BANNER_SIZE_BYTES" envDefault:"5242880"`

	// Optional CORS allowlist (comma-separated). Empty/"*" allows all (dev).
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// IdentityServiceURL is the gateway's private v2 identity introspection
	// base URL (development: http://gateway.railway.internal:8080), used with
	// IDENTITY_SERVICE_TOKEN to authenticate creator uploads outside
	// production. Unset (or a token shorter than 32 chars) keeps the legacy
	// upload route a 503.
	IdentityServiceURL string `env:"IDENTITY_SERVICE_URL"`
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

// Validate enforces field-level rules beyond presence.
func (c *Config) Validate() error {
	var errs []error

	if err := requireURL("DATABASE_URL", c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(c.RedisURL) == "" {
		errs = append(errs, errors.New("REDIS_URL is required"))
	}
	if err := requireURL("S3_ENDPOINT", c.S3Endpoint); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(c.S3Bucket) == "" {
		errs = append(errs, errors.New("S3_BUCKET is required"))
	}
	if strings.TrimSpace(c.S3AccessKeyID) == "" {
		errs = append(errs, errors.New("S3_ACCESS_KEY_ID is required"))
	}
	if strings.TrimSpace(c.S3SecretAccessKey) == "" {
		errs = append(errs, errors.New("S3_SECRET_ACCESS_KEY is required"))
	}
	if _, ok := validLevels[c.LogLevel]; !ok {
		errs = append(errs, fmt.Errorf("LOG_LEVEL %q must be one of debug|info|warn|error", c.LogLevel))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d must be in 1..65535", c.Port))
	}
	if c.MaxLogoSizeBytes <= 0 {
		errs = append(errs, fmt.Errorf("MAX_LOGO_SIZE_BYTES %d must be positive", c.MaxLogoSizeBytes))
	}
	if c.MaxBannerSizeBytes <= 0 {
		errs = append(errs, fmt.Errorf("MAX_BANNER_SIZE_BYTES %d must be positive", c.MaxBannerSizeBytes))
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
	bindings["S3_ACCESS_KEY_ID"] = &c.S3AccessKeyID
	bindings["S3_SECRET_ACCESS_KEY"] = &c.S3SecretAccessKey

	bindings["RPC_URL"] = &c.RPCURL
	bindings["METADATA_PUBLICATION_AUDIT_KEY"] = &c.PublicationAuditKey
	if c.Ingress == IngressEdge {
		bindings["IDENTITY_SERVICE_TOKEN"] = &c.IdentityServiceToken
	} else {
		bindings["METADATA_ASSERTION_PRIVATE_KEY_HEX"] = &c.MetadataAssertionPrivateKeyHex
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

	moderationBearer := c.ModerationSource != ModerationDatabase
	if moderationBearer {
		if c.ModerationBearerKey == "" {
			return nil, nil, fmt.Errorf("moderation_bearer_key_missing")
		}
		if _, err := provider.GetRequired(ctx, c.ModerationBearerKey); err != nil {
			return nil, nil, fmt.Errorf("moderation_bearer_unavailable")
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
		if moderationBearer {
			if _, err := provider.GetRequired(checkCtx, c.ModerationBearerKey); err != nil {
				failure = fmt.Errorf("moderation_bearer_unavailable")
				return failure
			}
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
	ownedURL, err := c.ownedDatabaseURL()
	if err != nil {
		return nil, err
	}
	pool, err := runtimedb.NewPool(ctx, ownedURL, options)
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
	if want.Service != "media/metadata" {
		return fmt.Errorf("runtime_schema_service_mismatch")
	}
	return dbproduction.CheckReadiness(ctx, pool, want)
}

func ReadBoundDocument(path, digest string, out any) error {
	return decodeBoundDocument(path, "", digest, out)
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

// Production ingress, raster isolation and moderation source selectors.
const (
	IngressMTLS        = "mtls"
	IngressEdge        = "edge"
	IsolationCgroup    = "cgroup"
	IsolationRlimit    = "rlimit"
	ModerationService  = "service"
	ModerationDatabase = "database"
)

// PrivateServiceOrigin reports whether raw is an origin (no path, query,
// fragment or userinfo) that is either https or plain http on a platform
// private-network host (*.railway.internal), which the platform encrypts
// between services and never exposes publicly.
func PrivateServiceOrigin(raw string) bool {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, " \r\n\t") {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := strings.ToLower(u.Hostname())
		return strings.HasSuffix(host, ".railway.internal") && len(host) > len(".railway.internal")
	}
	return false
}

func (c Config) ValidateMediaRuntime() error {
	if c.NodeEnv != "production" {
		return nil
	}
	hex64 := regexp.MustCompile(`^[a-f0-9]{64}$`)
	schema := regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	if !schema.MatchString(c.MetadataSchema) || !schema.MatchString(c.CanonicalSchema) || c.MetadataSchema == "public" || c.MetadataSchema == "pg_catalog" {
		return fmt.Errorf("metadata_schema_invalid")
	}
	if c.Ingress != IngressMTLS && c.Ingress != IngressEdge {
		return fmt.Errorf("metadata_ingress_invalid")
	}
	if c.RasterIsolation != IsolationCgroup && c.RasterIsolation != IsolationRlimit {
		return fmt.Errorf("metadata_worker_isolation_invalid")
	}
	if c.ModerationSource != ModerationService && c.ModerationSource != ModerationDatabase {
		return fmt.Errorf("metadata_moderation_source_invalid")
	}
	if c.Ingress == IngressMTLS {
		for _, path := range []string{c.TLSCertFile, c.TLSKeyFile, c.TLSClientCAFile} {
			if !filepath.IsAbs(path) {
				return fmt.Errorf("metadata_mtls_configuration_missing")
			}
		}
		if !hex64.MatchString(c.GatewayPublicKeyHex) || !hex64.MatchString(c.GatewayPeerCertSHA256) {
			return fmt.Errorf("metadata_gateway_authority_missing")
		}
		if !regexp.MustCompile(`^[a-f0-9]{128}$`).MatchString(c.MetadataAssertionPrivateKeyHex) || !hex64.MatchString(c.IdentityPeerCertSHA256) || !hex64.MatchString(c.UserPeerCertSHA256) || !hex64.MatchString(c.UserAssertionPublicKeyHex) {
			return fmt.Errorf("metadata_identity_authority_missing")
		}
		if u, e := url.Parse(c.IdentityEndpoint); e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("metadata_identity_https_origin_required")
		}
	} else {
		// Edge ingress: the session introspection authority replaces the
		// gateway-signed private channel and the mTLS identity transport.
		if !PrivateServiceOrigin(c.IdentityServiceURL) || len(c.IdentityServiceToken) < 32 || strings.ContainsAny(c.IdentityServiceToken, "\r\n") {
			return fmt.Errorf("metadata_identity_authority_missing")
		}
		if c.ModerationSource == ModerationService {
			// The social visibility authority is served only on its mTLS
			// private channel, which edge ingress has no client identity for.
			return fmt.Errorf("metadata_edge_moderation_requires_database_source")
		}
		if c.TrustedProxyCIDRs != "" {
			if _, e := c.TrustedProxies(); e != nil {
				return e
			}
		}
	}
	if c.IdentityAudience == "" || strings.ContainsAny(c.IdentityAudience, "\r\n") {
		return fmt.Errorf("metadata_identity_authority_missing")
	}
	if c.QuarantineBucket == "" || c.ApprovedBucket == "" || c.QuarantineBucket == c.ApprovedBucket {
		return fmt.Errorf("metadata_distinct_buckets_required")
	}
	if !filepath.IsAbs(c.RasterExecutable) || (c.RasterIsolation == IsolationCgroup && !filepath.IsAbs(c.RasterCgroupParent)) {
		return fmt.Errorf("metadata_worker_authority_missing")
	}
	if c.ClamAVHost == "" || c.ClamAVPort < 1 || c.ClamAVPort > 65535 {
		return fmt.Errorf("metadata_scanner_configuration_missing")
	}
	if c.ModerationSource == ModerationService {
		if !hex64.MatchString(c.ModerationPeerCertSHA256) {
			return fmt.Errorf("metadata_identity_authority_missing")
		}
		if u, e := url.Parse(c.ModerationEndpoint); e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("metadata_moderation_https_origin_required")
		}
		if c.ModerationBearerKey == "" {
			return fmt.Errorf("metadata_media_worker_configuration_invalid")
		}
	} else if !schema.MatchString(c.ModerationSchema) || c.ModerationSchema == "public" || c.ModerationSchema == "pg_catalog" {
		return fmt.Errorf("metadata_moderation_schema_invalid")
	}
	if u, e := url.Parse(c.PublicURL); e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("metadata_public_https_origin_required")
	}
	if (c.PublicationConfigFile == "") == (c.PublicationConfigDocument == "") || (c.PublicationConfigFile != "" && !filepath.IsAbs(c.PublicationConfigFile)) || !hex64.MatchString(c.PublicationConfigSHA256) {
		return fmt.Errorf("metadata_publication_configuration_missing")
	}
	if key, e := base64.StdEncoding.Strict().DecodeString(c.PublicationAuditKey); e != nil || len(key) != 32 {
		return fmt.Errorf("metadata_publication_audit_key_invalid")
	}
	if c.MediaScanIntervalSeconds < 1 || c.MediaScanIntervalSeconds > 60 || c.MediaExpiryIntervalSeconds < 30 || c.MediaExpiryIntervalSeconds > 3600 {
		return fmt.Errorf("metadata_media_worker_configuration_invalid")
	}
	return nil
}

// TrustedProxies parses TRUSTED_PROXY_CIDRS (comma-separated prefixes).
// Prefixes wider than /8 are refused: trusting (nearly) the whole address
// space would let any client spoof its rate-limit identity.
func (c Config) TrustedProxies() ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(c.TrustedProxyCIDRs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prefix, e := netip.ParsePrefix(part)
		if e != nil || prefix != prefix.Masked() || (prefix.Addr().Is4() && prefix.Bits() < 8) || (prefix.Addr().Is6() && prefix.Bits() < 8) {
			return nil, fmt.Errorf("metadata_trusted_proxy_invalid")
		}
		out = append(out, prefix)
	}
	return out, nil
}

type PublicationRuntime struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Environment     string `json:"environment"`
	EnvelopeHeader  string `json:"envelopeHeader"`
	SignatureHeader string `json:"signatureHeader"`
}

func (c Config) ReadPublicationRuntime() (PublicationRuntime, []byte, error) {
	var runtime PublicationRuntime
	if e := decodeBoundDocument(c.PublicationConfigFile, c.PublicationConfigDocument, c.PublicationConfigSHA256, &runtime); e != nil {
		return runtime, nil, e
	}
	environment := regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)
	header := regexp.MustCompile(`^X-[A-Za-z0-9-]{1,60}$`)
	if runtime.SchemaVersion != 3 || !environment.MatchString(runtime.Environment) || !header.MatchString(runtime.EnvelopeHeader) || !header.MatchString(runtime.SignatureHeader) {
		return runtime, nil, fmt.Errorf("metadata_publication_configuration_invalid")
	}
	for _, name := range []string{runtime.EnvelopeHeader, runtime.SignatureHeader} {
		for _, reserved := range []string{"X-Request-ID", "X-Internal-Assertion", "X-Internal-Signature", "X-Kindle-Identity-Assertion", "X-Kindle-Identity-Signature", "X-Kindle-Service-Assertion", "X-Kindle-Service-Signature"} {
			if strings.EqualFold(name, reserved) {
				return runtime, nil, fmt.Errorf("metadata_publication_header_collision")
			}
		}
	}
	if strings.EqualFold(runtime.EnvelopeHeader, runtime.SignatureHeader) {
		return runtime, nil, fmt.Errorf("metadata_publication_header_collision")
	}
	key, e := base64.StdEncoding.Strict().DecodeString(c.PublicationAuditKey)
	if e != nil || len(key) != 32 {
		return runtime, nil, fmt.Errorf("metadata_publication_audit_key_invalid")
	}
	return runtime, key, nil
}

func (c Config) ownedDatabaseURL() (string, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(c.MetadataSchema) || c.MetadataSchema == "public" || c.MetadataSchema == "pg_catalog" {
		return "", fmt.Errorf("metadata_schema_invalid")
	}
	parsed, e := url.Parse(c.DatabaseURL)
	if e != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", fmt.Errorf("metadata_database_route_invalid")
	}
	parameters := parsed.Query()
	parameters.Set("search_path", pgx.Identifier{c.MetadataSchema}.Sanitize()+",pg_catalog")
	parsed.RawQuery = parameters.Encode()
	return parsed.String(), nil
}
