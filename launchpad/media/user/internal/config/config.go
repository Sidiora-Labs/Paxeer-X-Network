// Package config loads and validates the media/user environment, mirroring the
// TS usersEnvSchema (src/config.ts) env var names (invariant i8 — matches
// docker-envs/users.env) and defaults.
//
// Storage model is BUCKET-PRIMARY (2026-06-26 decision): the S3_* vars are now
// REQUIRED (the TS volume-as-source-of-truth + optional-fallback model is gone).
// Avatars/banners are written straight to R2 and served back from it. media/user
// owns its OWN bucket, separate from media/metadata.
package config

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// Config is the parsed user-service environment.
type Config struct {
	AccountServiceSigningSeed string `env:"ACCOUNT_SERVICE_SIGNING_SEED"`
	IdentityCertSHA256        string `env:"IDENTITY_CERT_SHA256"`
	MetadataServiceURL        string `env:"METADATA_SERVICE_URL"`
	MetadataCertSHA256        string `env:"METADATA_CERT_SHA256"`
	AccountEnableExtendedV3   bool   `env:"ACCOUNT_ENABLE_EXTENDED_V3"`

	AccountExportKey                string `env:"ACCOUNT_EXPORT_KEY"`
	AccountEndpointKey              string `env:"ACCOUNT_ENDPOINT_KEY"`
	AccountEndpointFingerprintKey   string `env:"ACCOUNT_ENDPOINT_FINGERPRINT_KEY"`
	AccountAuditKey                 string `env:"ACCOUNT_AUDIT_KEY"`
	AccountEndpointProofPublicKey   string `env:"ACCOUNT_ENDPOINT_PROOF_PUBLIC_KEY"`
	AccountGatewayPublicKey         string `env:"ACCOUNT_GATEWAY_PUBLIC_KEY"`
	AccountGatewayCertSHA256        string `env:"ACCOUNT_GATEWAY_CERT_SHA256"`
	AccountAssertionHeader          string `env:"ACCOUNT_ASSERTION_HEADER" envDefault:"X-Kindle-Service-Assertion"`
	AccountAssertionSignatureHeader string `env:"ACCOUNT_ASSERTION_SIGNATURE_HEADER" envDefault:"X-Kindle-Service-Signature"`
	AccountAssertionIssuer          string `env:"ACCOUNT_ASSERTION_ISSUER" envDefault:"gateway"`
	AccountAssertionAudience        string `env:"ACCOUNT_ASSERTION_AUDIENCE" envDefault:"account"`
	AccountEnvironment              string `env:"ACCOUNT_OPERATION_ENVIRONMENT" envDefault:"production"`
	AccountRPCURL                   string `env:"ACCOUNT_CHAIN_RPC_URL"`
	AccountDownloadBaseURL          string `env:"ACCOUNT_DOWNLOAD_BASE_URL"`
	AccountAllowMetadataAvatars     bool   `env:"ACCOUNT_ALLOW_METADATA_AVATARS"`
	AccountUseReadSwitch            bool   `env:"ACCOUNT_USE_READ_SWITCH"`
	AccountDeliverySchema           string `env:"ACCOUNT_DELIVERY_SCHEMA" envDefault:"users"`
	AccountSubjectsFile             string `env:"ACCOUNT_SUBJECTS_FILE"`
	AccountSubjectsSHA256           string `env:"ACCOUNT_SUBJECTS_SHA256"`
	AccountSubjectsDocument         string `env:"ACCOUNT_SUBJECTS_DOCUMENT"`
	AccountExportBucket             string `env:"ACCOUNT_EXPORT_BUCKET"`
	AccountNATSURL                  string `env:"ACCOUNT_NATS_URL"`
	AccountNATSCredentialsFile      string `env:"ACCOUNT_NATS_CREDENTIALS_FILE"`
	AccountNATSStream               string `env:"ACCOUNT_NATS_STREAM"`
	AccountNATSCAFile               string `env:"ACCOUNT_NATS_CA_FILE"`
	AccountNATSCertFile             string `env:"ACCOUNT_NATS_CERT_FILE"`
	AccountNATSKeyFile              string `env:"ACCOUNT_NATS_KEY_FILE"`
	AccountTLSCertFile              string `env:"ACCOUNT_TLS_CERT_FILE"`
	AccountTLSKeyFile               string `env:"ACCOUNT_TLS_KEY_FILE"`
	AccountTLSCAFile                string `env:"ACCOUNT_TLS_CA_FILE"`
	AccountNATSCreds                string `env:"ACCOUNT_NATS_CREDS"`
	AccountNATSCAPEM                string `env:"ACCOUNT_NATS_CA_PEM"`
	AccountNATSCertPEM              string `env:"ACCOUNT_NATS_CERT_PEM"`
	AccountNATSKeyPEM               string `env:"ACCOUNT_NATS_KEY_PEM"`
	AccountTLSCertPEM               string `env:"ACCOUNT_TLS_CERT_PEM"`
	AccountTLSKeyPEM                string `env:"ACCOUNT_TLS_KEY_PEM"`
	AccountTLSCAPEM                 string `env:"ACCOUNT_TLS_CA_PEM"`
	NotificationServiceURL          string `env:"NOTIFICATION_SERVICE_URL"`
	NotificationServiceToken        string `env:"NOTIFICATION_SERVICE_TOKEN"`
	NotificationCertSHA256          string `env:"NOTIFICATION_CERT_SHA256"`
	NotificationInboundToken        string `env:"NOTIFICATION_D11_TOKEN"`

	RuntimeZone               string `env:"ZONE"`
	RuntimeMetricsEnvironment string `env:"METRICS_ENVIRONMENT" envDefault:"production"`
	AccountCursorKey          string `env:"ACCOUNT_CURSOR_KEY"`

	RuntimeProfile         string `env:"RUNTIME_PROFILE"`
	BindAddress            string `env:"BIND_ADDRESS"`
	RuntimeBudgetFile      string `env:"DB_BUDGET_FILE"`
	RuntimeBudgetSHA256    string `env:"DB_BUDGET_SHA256"`
	RuntimeBudgetDocument  string `env:"DB_BUDGET_DOCUMENT"`
	RuntimePoolID          string `env:"DB_POOL_ID"`
	RuntimeInstanceID      string `env:"INSTANCE_ID"`
	ExpectedSchemaFile     string `env:"EXPECTED_SCHEMA_FILE"`
	ExpectedSchemaSHA256   string `env:"EXPECTED_SCHEMA_SHA256"`
	ExpectedSchemaDocument string `env:"EXPECTED_SCHEMA_DOCUMENT"`
	IdentityURL            string `env:"IDENTITY_SERVICE_URL"`
	IdentityServiceToken   string `env:"IDENTITY_SERVICE_TOKEN"`

	SecretsProvider       string `env:"SECRETS_PROVIDER" envDefault:"env"`
	SecretsRefreshSeconds int    `env:"SECRETS_REFRESH_SECONDS" envDefault:"30"`
	NodeEnv               string `env:"NODE_ENV" envDefault:"production"`

	DatabaseURL string `env:"DATABASE_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	Port     int    `env:"PORT" envDefault:"3000"`

	// PublicURL builds absolute avatar/banner URLs. When empty, URLs are built
	// from the incoming request host (parity with the TS fallback).
	PublicURL string `env:"PUBLIC_URL"`

	// Object storage (Cloudflare R2 / S3-compatible) — bucket-primary, required.
	S3Endpoint        string `env:"S3_ENDPOINT,required"`
	S3Bucket          string `env:"S3_BUCKET,required"`
	S3AccessKeyID     string `env:"S3_ACCESS_KEY_ID,required"`
	S3SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY,required"`
	S3Region          string `env:"S3_REGION" envDefault:"auto"`

	MaxAvatarSizeBytes int64 `env:"MAX_AVATAR_SIZE_BYTES" envDefault:"2097152"`
	MaxBannerSizeBytes int64 `env:"MAX_BANNER_SIZE_BYTES" envDefault:"5242880"`

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
	cfg.registerInlineDocuments()
	return cfg, nil
}

// Validate enforces field-level rules beyond presence.
func (c *Config) Validate() error {
	var errs []error
	for _, d := range c.boundDocuments() {
		if d[0] != "" && d[1] != "" {
			errs = append(errs, fmt.Errorf("runtime_document_source_ambiguous: %s_FILE and %s_DOCUMENT are both set", d[3], d[3]))
		}
	}

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
	if c.MaxAvatarSizeBytes <= 0 {
		errs = append(errs, fmt.Errorf("MAX_AVATAR_SIZE_BYTES %d must be positive", c.MaxAvatarSizeBytes))
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

	bindings["ACCOUNT_CURSOR_KEY"] = &c.AccountCursorKey
	bindings["ACCOUNT_SERVICE_SIGNING_SEED"] = &c.AccountServiceSigningSeed
	bindings["ACCOUNT_EXPORT_KEY"] = &c.AccountExportKey
	bindings["ACCOUNT_ENDPOINT_KEY"] = &c.AccountEndpointKey
	bindings["ACCOUNT_ENDPOINT_FINGERPRINT_KEY"] = &c.AccountEndpointFingerprintKey
	bindings["ACCOUNT_AUDIT_KEY"] = &c.AccountAuditKey
	bindings["NOTIFICATION_SERVICE_TOKEN"] = &c.NotificationServiceToken
	bindings["NOTIFICATION_D11_TOKEN"] = &c.NotificationInboundToken
	if c.AccountNATSCredentialsFile == "" {
		bindings["ACCOUNT_NATS_CREDS"] = &c.AccountNATSCreds
	}
	if c.AccountNATSKeyFile == "" {
		bindings["ACCOUNT_NATS_KEY_PEM"] = &c.AccountNATSKeyPEM
	}
	if c.AccountTLSKeyFile == "" {
		bindings["ACCOUNT_TLS_KEY_PEM"] = &c.AccountTLSKeyPEM
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
	if want.Service != "media/user" {
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
		{c.AccountSubjectsFile, c.AccountSubjectsDocument, c.AccountSubjectsSHA256, "ACCOUNT_SUBJECTS"},
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

func DecodeAccountKey(raw string) ([]byte, error) {
	key, e := base64.StdEncoding.Strict().DecodeString(raw)
	if e != nil || len(key) != 32 {
		return nil, fmt.Errorf("account_key_invalid")
	}
	return key, nil
}
func (c Config) ValidateAccountRuntime() error {
	for _, raw := range []string{c.AccountCursorKey, c.AccountExportKey, c.AccountEndpointKey, c.AccountEndpointFingerprintKey, c.AccountAuditKey, c.AccountEndpointProofPublicKey, c.AccountGatewayPublicKey, c.AccountServiceSigningSeed} {
		key, e := DecodeAccountKey(raw)
		if e != nil || len(key) != ed25519.PublicKeySize {
			return fmt.Errorf("account_crypto_configuration_invalid")
		}
	}
	for _, raw := range []string{c.AccountGatewayCertSHA256, c.NotificationCertSHA256, c.IdentityCertSHA256, c.MetadataCertSHA256} {
		digest, e := hex.DecodeString(raw)
		if e != nil || len(digest) != 32 || strings.ToLower(raw) != raw {
			return fmt.Errorf("account_peer_pin_invalid")
		}
	}
	for _, raw := range []string{c.AccountDownloadBaseURL, c.NotificationServiceURL, c.IdentityURL, c.MetadataServiceURL, c.S3Endpoint} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("account_https_endpoint_invalid")
		}
	}
	if c.AccountExportBucket == "" || c.AccountExportBucket == c.S3Bucket || len(c.NotificationServiceToken) < 32 || len(c.NotificationInboundToken) < 32 || c.AccountNATSStream == "" || (c.AccountSubjectsFile == "" && c.AccountSubjectsDocument == "") || len(c.AccountSubjectsSHA256) != 64 || c.AccountEnvironment == "" {
		return fmt.Errorf("account_runtime_configuration_missing")
	}
	for _, m := range c.accountMaterial() {
		if (m[2] == "") == (m[3] == "") {
			return fmt.Errorf("exactly one of %s and %s is required", m[0], m[1])
		}
	}
	n, e := url.Parse(c.AccountNATSURL)
	if e != nil || n.Scheme != "tls" || n.Host == "" || n.User != nil {
		return fmt.Errorf("account_broker_tls_required")
	}
	if e = requireURL("ACCOUNT_CHAIN_RPC_URL", c.AccountRPCURL); e != nil {
		return e
	}
	return nil
}

// accountMaterial pairs each account TLS and NATS file input with its inline
// sibling: {file variable, inline variable, file path, inline value}.
func (c Config) accountMaterial() [][4]string {
	return [][4]string{
		{"ACCOUNT_TLS_CERT_FILE", "ACCOUNT_TLS_CERT_PEM", c.AccountTLSCertFile, c.AccountTLSCertPEM},
		{"ACCOUNT_TLS_KEY_FILE", "ACCOUNT_TLS_KEY_PEM", c.AccountTLSKeyFile, c.AccountTLSKeyPEM},
		{"ACCOUNT_TLS_CA_FILE", "ACCOUNT_TLS_CA_PEM", c.AccountTLSCAFile, c.AccountTLSCAPEM},
		{"ACCOUNT_NATS_CREDENTIALS_FILE", "ACCOUNT_NATS_CREDS", c.AccountNATSCredentialsFile, c.AccountNATSCreds},
		{"ACCOUNT_NATS_CA_FILE", "ACCOUNT_NATS_CA_PEM", c.AccountNATSCAFile, c.AccountNATSCAPEM},
		{"ACCOUNT_NATS_CERT_FILE", "ACCOUNT_NATS_CERT_PEM", c.AccountNATSCertFile, c.AccountNATSCertPEM},
		{"ACCOUNT_NATS_KEY_FILE", "ACCOUNT_NATS_KEY_PEM", c.AccountNATSKeyFile, c.AccountNATSKeyPEM},
	}
}

// Material returns the inline credential or PEM value when it is set, else the bytes of
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
