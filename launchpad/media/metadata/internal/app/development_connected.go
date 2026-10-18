package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/metadata"
)

// Development publication transport. The environment is what the browser
// signs into the operation envelope (it reads it from
// GET /v3/metadata-publication-authority); the headers match the user
// service's operation transport.
const (
	developmentPublicationEnvironment = "development"
	developmentEnvelopeHeader         = "X-Kindle-Operation"
	developmentSignatureHeader        = "X-Kindle-Operation-Signature"
	developmentIdentityAudience       = "metadata-development"
)

// developmentMedia is the production creator-media surface (ConnectedMedia
// token assets + PublicationService) wired with development dependencies.
type developmentMedia struct {
	connected   *metadata.ConnectedMedia
	publication *metadata.PublicationService
	pool        *pgxpool.Pool
}

// newDevelopmentMedia builds the development creator-media surface. It
// returns (nil, nil), with a warning, when a required input is missing:
// IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN (session identity), RPC_URL
// (creation receipts and wallet-signature verification) and an https
// PUBLIC_URL origin (approved variant URLs). The routes then answer 503.
func newDevelopmentMedia(ctx context.Context, cfg config.Config, logger *slog.Logger, reads *metadata.DevelopmentV2Reads, chain *gethrpc.Client) (*developmentMedia, error) {
	missing := []string{}
	if strings.TrimSpace(cfg.IdentityServiceURL) == "" || len(cfg.IdentityServiceToken) < 32 {
		missing = append(missing, "IDENTITY_SERVICE_URL/IDENTITY_SERVICE_TOKEN")
	}
	if chain == nil {
		missing = append(missing, "RPC_URL")
	}
	publicOrigin := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	if u, e := url.Parse(publicOrigin); e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		missing = append(missing, "PUBLIC_URL (https origin)")
	}
	if len(missing) > 0 {
		if logger != nil {
			logger.Warn("development token image upload and metadata publication disabled", slog.String("missing", strings.Join(missing, ", ")))
		}
		return nil, nil
	}
	schema := metadata.DevelopmentConnectedSchema
	poolConfig, e := pgxpool.ParseConfig(cfg.DatabaseURL)
	if e != nil {
		return nil, e
	}
	// Same pinning as the production runtime role: unqualified ledger tables
	// resolve to this schema only.
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",pg_catalog"
	if poolConfig.MaxConns < 4 {
		poolConfig.MaxConns = 4
	}
	pool, e := pgxpool.NewWithConfig(ctx, poolConfig)
	if e != nil {
		return nil, e
	}
	built := false
	defer func() {
		if !built {
			pool.Close()
		}
	}()
	if e = metadata.EnsureDevelopmentConnectedSchema(ctx, pool, schema); e != nil {
		return nil, e
	}
	backend := sharedauth.NewReportedHashBackend(chain)
	audience := cfg.IdentityAudience
	if audience == "" {
		audience = developmentIdentityAudience
	}
	v2, e := metadata.NewV2ProductionHandlers(pool, schema, audience, backend, metadata.DevelopmentCanonicalSource{Reads: reads}, time.Now)
	if e != nil {
		return nil, e
	}
	awsCfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.S3Region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, "")))
	if e != nil {
		return nil, e
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.S3Endpoint); o.UsePathStyle = true })
	objects := metadata.S3AssetObjects{Client: client, QuarantineBucket: firstSet(cfg.QuarantineBucket, cfg.S3Bucket), PublicBucket: firstSet(cfg.ApprovedBucket, cfg.S3Bucket), PublicOrigin: publicOrigin}
	objects.Readiness = metadata.DevelopmentObjectsReadiness(objects)
	if objects.QuarantineBucket == objects.PublicBucket && logger != nil {
		logger.Warn("development media quarantine shares the public bucket; set S3_QUARANTINE_BUCKET to a private bucket", slog.String("bucket", objects.PublicBucket))
	}
	identity := sharedauth.HTTPIdentityAuthority{Endpoint: strings.TrimRight(strings.TrimSpace(cfg.IdentityServiceURL), "/"), ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: true}
	instance := metadata.ConnectedMedia{
		Metadata:   v2,
		Identity:   identity,
		Objects:    objects,
		Scanner:    sharedhttp.ScanOptions{Host: cfg.ClamAVHost, Port: cfg.ClamAVPort, Timeout: 5 * time.Second},
		Worker:     raster.InProcessWorker{},
		Moderation: metadata.DevelopmentVisibility{},
	}
	operations := sharedauth.OperationAuthorizer{
		Ledger:    sharedauth.Ledger{Pool: pool, Environment: developmentPublicationEnvironment, AuditKey: developmentAuditKey(cfg)},
		Verifier:  sharedauth.WalletOperationVerifier{Backend: backend},
		Transport: sharedauth.OperationTransport{EnvelopeHeader: developmentEnvelopeHeader, SignatureHeader: developmentSignatureHeader},
	}
	publication, e := metadata.NewDevelopmentPublicationService(instance, operations, metadata.SessionCheckIdentities{Authority: sharedauth.HTTPStreamAuthority(identity)})
	if e != nil {
		return nil, e
	}
	built = true
	return &developmentMedia{connected: &instance, publication: &publication, pool: pool}, nil
}

// developmentAuditKey is METADATA_PUBLICATION_AUDIT_KEY (base64, 32 bytes)
// when set, else a key derived from IDENTITY_SERVICE_TOKEN. It only
// pseudonymises actors in security_audit_outbox.
func developmentAuditKey(cfg config.Config) []byte {
	if key, e := base64.StdEncoding.Strict().DecodeString(cfg.PublicationAuditKey); e == nil && len(key) == 32 {
		return key
	}
	mac := hmac.New(sha256.New, []byte(cfg.IdentityServiceToken))
	mac.Write([]byte("metadata-publication-audit-v1"))
	return mac.Sum(nil)
}

func firstSet(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// registerDevelopmentMedia mounts the creator token-image routes and the /v3
// metadata-publication routes. Without the development media surface they
// answer the production unavailable shape (503) instead of 404.
func registerDevelopmentMedia(r chi.Router, media *developmentMedia) {
	if media == nil {
		unavailable := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, `{"requestId":%q,"error":{"code":"unavailable"}}`+"\n", uuid.NewString())
		}
		r.Put("/v2/tokens/{tokenAddress}/assets/{assetId}", unavailable)
		r.Get("/v2/tokens/{tokenAddress}/assets/{assetId}", unavailable)
		r.Get("/v3/metadata-publication-authority", unavailable)
		r.Post(metadata.PublicationRoute, unavailable)
		r.Get(metadata.PublicationRoute+"/{operationId}", unavailable)
		return
	}
	r.Group(func(cr chi.Router) {
		cr.Use(metadata.DevelopmentSessionChannel(media.connected.Identity))
		media.connected.RegisterTokenAssetRoutes(cr)
		media.publication.RegisterRoutes(cr)
	})
}
