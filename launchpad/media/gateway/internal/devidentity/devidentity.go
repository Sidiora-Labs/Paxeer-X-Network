// Package devidentity builds the gateway's real v2 identity authority
// (*auth.Identity) outside production, where the sha256-bound identity
// document, the split runtime pools and the mTLS mesh do not exist.
//
// Only the configuration source differs from production: the same Identity
// type, SQL, HS256 access-token format, SIWE builder and chain-125 proof
// verifier run, configured from environment variables instead of the bound
// document. The identity tables live in their own schema (gateway_identity),
// created and migrated at boot from the embedded goose migrations, which copy
// the identity DDL of production migrations 330000-330002.
package devidentity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// Schema holds the development identity tables and goose's version table.
const Schema = "gateway_identity"

// Issuer and Audience are the JWT iss/aud of development access tokens.
const (
	Issuer   = "gateway"
	Audience = "kindlelaunch-web"
)

// SigningSecretReference is the environment variable the HS256 access-token
// key is resolved from (auth.EnvironmentIdentitySecrets), as in production
// where the identity document's keys[].secretReference names it.
const SigningSecretReference = "IDENTITY_SIGNING_SECRET"

//go:embed migrations/*.sql
var migrationFS embed.FS

var gooseMu sync.Mutex

// Options is the environment-derived configuration.
type Options struct {
	DatabaseURL string
	RPCURL      string
	// PublicURL is the https origin the browser uses (the web origin that
	// rewrites /v2/auth to the gateway); SIWE domain = its host, uri = it.
	PublicURL string
	// CORSAllowedOrigins is the comma-separated https allowlist; it must
	// contain PublicURL and becomes the identity's allowed origins.
	CORSAllowedOrigins string
	// SigningSecret is the value of IDENTITY_SIGNING_SECRET (>= 32 bytes).
	SigningSecret string
	LimiterKey    string
	ServiceToken  string
	Observe       func(string)
}

// Runtime owns the identity and the resources behind it.
type Runtime struct {
	Identity *auth.Identity
	Pool     *pgxpool.Pool
	// RPC is the chain-125 proof backend (the same one production uses). It
	// identifies blocks by the node-reported hash, which differs from
	// go-ethereum's recomputed header hash on Paxeer (Cosmos-EVM); see
	// sharedauth.ReportedHashBackend.
	RPC *sharedauth.ReportedHashBackend
}

// Close releases the pool and the RPC client.
func (r *Runtime) Close() {
	if r.Pool != nil {
		r.Pool.Close()
	}
	if r.RPC != nil {
		r.RPC.Close()
	}
}

// Validate checks the options without touching the network.
func (o Options) Validate() ([]string, string, error) {
	var errs []error
	if strings.TrimSpace(o.DatabaseURL) == "" {
		errs = append(errs, errors.New("DATABASE_URL is required for development identity"))
	}
	if strings.TrimSpace(o.RPCURL) == "" {
		errs = append(errs, errors.New("RPC_URL is required for development identity"))
	}
	if len(o.SigningSecret) < 32 {
		errs = append(errs, errors.New("IDENTITY_SIGNING_SECRET must be at least 32 bytes"))
	}
	if len(o.LimiterKey) < 32 {
		errs = append(errs, errors.New("IDENTITY_LIMITER_KEY must be at least 32 bytes"))
	}
	if len(o.ServiceToken) < 32 {
		errs = append(errs, errors.New("IDENTITY_SERVICE_TOKEN must be at least 32 characters"))
	}
	public, err := url.Parse(o.PublicURL)
	if err != nil || public.Scheme != "https" || public.Host == "" || public.User != nil || public.Path != "" || public.RawQuery != "" || public.Fragment != "" {
		errs = append(errs, errors.New("PUBLIC_URL must be a bare https origin (the browser's application origin)"))
	}
	origins := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(o.CORSAllowedOrigins, ",") {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || seen[raw] {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS must be distinct bare https origins for development identity"))
			break
		}
		seen[raw] = true
		origins = append(origins, raw)
	}
	if !seen[o.PublicURL] {
		errs = append(errs, errors.New("PUBLIC_URL must be one of CORS_ALLOWED_ORIGINS"))
	}
	if len(errs) > 0 {
		return nil, "", errors.Join(errs...)
	}
	return origins, public.Host, nil
}

// KeyID derives the access-token key id from the secret, so rotating
// IDENTITY_SIGNING_SECRET retires every token minted under the old one.
func KeyID(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return "dev-" + hex.EncodeToString(digest[:8])
}

// Open migrates the identity schema and builds the real identity authority.
func Open(ctx context.Context, o Options) (*Runtime, error) {
	origins, domain, err := o.Validate()
	if err != nil {
		return nil, err
	}
	if err = Migrate(ctx, o.DatabaseURL); err != nil {
		return nil, err
	}
	pool, err := NewPool(ctx, o.DatabaseURL)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{Pool: pool}
	kid := KeyID(o.SigningSecret)
	tokens, err := auth.NewIdentityTokens(Issuer, Audience, kid, []auth.SigningKey{{
		ID:              kid,
		SecretReference: SigningSecretReference,
		ActiveAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RetireAt:        time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
	}}, auth.EnvironmentIdentitySecrets{}, time.Now)
	if err != nil {
		runtime.Close()
		return nil, fmt.Errorf("development identity tokens: %w", err)
	}
	if o.Observe != nil {
		tokens.SetObserver(o.Observe)
	}
	if _, err = (auth.EnvironmentIdentitySecrets{}).ResolveIdentityKey(ctx, SigningSecretReference); err != nil {
		runtime.Close()
		return nil, errors.New("IDENTITY_SIGNING_SECRET must be set in the environment (>= 32 bytes)")
	}
	client, err := rpc.DialContext(ctx, o.RPCURL)
	if err != nil {
		runtime.Close()
		return nil, fmt.Errorf("development identity rpc: %w", err)
	}
	runtime.RPC = sharedauth.NewReportedHashBackend(client)
	runtime.Identity, err = auth.NewIdentity(auth.IdentityConfig{
		Pool:           pool,
		Domain:         domain,
		URI:            o.PublicURL,
		AllowedOrigins: origins,
		Verifier:       auth.Chain125ProofVerifier{Backend: runtime.RPC},
		LimiterKey:     []byte(o.LimiterKey),
		Tokens:         tokens,
		Observe:        o.Observe,
	})
	if err != nil {
		runtime.Close()
		return nil, fmt.Errorf("development identity: %w", err)
	}
	return runtime, nil
}

// poolConfig parses the DSN and pins search_path to the identity schema,
// whatever search_path the shared DATABASE_URL carries.
func poolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("DATABASE_URL is not a valid Postgres DSN")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = Schema
	cfg.MaxConns = 8
	return cfg, nil
}

// NewPool opens the identity pool (search_path = gateway_identity).
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("development identity pool: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("development identity database: %w", err)
	}
	return pool, nil
}

// Migrate creates the identity schema when absent and applies the embedded
// goose migrations inside it (goose_db_version lives in the same schema).
func Migrate(ctx context.Context, dsn string) error {
	cfg, err := poolConfig(dsn)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		return fmt.Errorf("development identity database: %w", err)
	}
	_, err = conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{Schema}.Sanitize())
	closeErr := conn.Close(ctx)
	if err != nil {
		return fmt.Errorf("development identity schema: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	gooseMu.Lock()
	defer gooseMu.Unlock()
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer func(db *sql.DB) { _ = db.Close() }(db)
	goose.SetBaseFS(migrationFS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("development identity migrate: %w", err)
	}
	if err = goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("development identity migrate: %w", err)
	}
	return nil
}
