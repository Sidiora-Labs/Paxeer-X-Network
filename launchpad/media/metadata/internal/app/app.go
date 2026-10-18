// Package app wires the media/metadata service together (config, migrations,
// pgxpool, redis, R2 object store, HTTP router, health, graceful shutdown) and
// owns its lifecycle. Keeping the wiring here (rather than in main) makes the
// whole service testable end-to-end against external test servers + MinIO.
package app

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
	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"image"
	"image/png"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/metadata"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/migrate"
)

// rateLimitMax / rateLimitWindow mirror the TS registerRateLimit(app, {max:100,
// windowSeconds:60}) configuration.
const (
	rateLimitMax    = 100
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-serve metadata service.
type App struct {
	secretStop              context.CancelFunc
	mediaStop               context.CancelFunc
	mediaDone               <-chan struct{}
	tlsConfig               *tls.Config
	tlsCertFile, tlsKeyFile string
	rpc                     *ethclient.Client
	tracker                 *process.Tracker
	Router                  http.Handler

	pool   *pgxpool.Pool
	redis  *goredis.Client
	logger *slog.Logger
	// devMediaPool is the development connected-media pool (schema-pinned
	// search_path); nil in production.
	devMediaPool *pgxpool.Pool
}

// New runs migrations, opens the DB + Redis pools + R2 client, and builds the
// HTTP router with health endpoints and rate-limited metadata routes.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	secretCheck, secretStop, err := cfg.ResolveProductionSecrets(ctx)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			secretStop()
		}
	}()
	if err = cfg.ValidateMediaRuntime(); err != nil {
		return nil, err
	}
	tracker := process.NewTracker()
	var metrics *telemetry.Metrics
	if cfg.NodeEnv == "production" {
		metrics, err = telemetry.New("metadatad", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if err != nil {
			return nil, err
		}
	}
	checks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		checks["secrets"] = secretCheck
	}
	if cfg.NodeEnv != "production" {
		if err = migrate.Up(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}
	pool, err := cfg.OpenRuntimePool(ctx, "metadatad", shareddb.PoolOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if !owned {
			pool.Close()
		}
	}()
	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !owned {
			rdb.Close()
		}
	}()
	var rpc *ethclient.Client
	var connected *metadata.ConnectedMedia
	var publication *metadata.PublicationService
	var serverTLS *tls.Config
	var workerMu sync.Mutex
	var workerError error
	checks["media_retention"] = func(c context.Context) error {
		if err := c.Err(); err != nil {
			return err
		}
		workerMu.Lock()
		defer workerMu.Unlock()
		return workerError
	}
	var legacyReads *metadata.ProductionLegacyReads
	edge := cfg.NodeEnv == "production" && cfg.Ingress == config.IngressEdge
	if cfg.NodeEnv == "production" {
		if !edge {
			serverTLS, err = metadataServerTLS(cfg)
			if err != nil {
				return nil, err
			}
		}
		if pool.Config().MaxConns < 3 {
			return nil, fmt.Errorf("metadata_runtime_pool_requires_three_connections")
		}
		chainClient, e := gethrpc.DialContext(ctx, cfg.RPCURL)
		if e != nil {
			return nil, fmt.Errorf("metadata_rpc_unavailable")
		}
		rpc = ethclient.NewClient(chainClient)
		defer func() {
			if !owned {
				rpc.Close()
			}
		}()
		startup, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		chain, e := rpc.ChainID(startup)
		if e != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
			return nil, fmt.Errorf("metadata_chain_authority_unavailable")
		}
		// Paxeer reports CometBFT-derived block hashes, so every
		// block-pinned read goes through the node-reported hash.
		v2, e := metadata.NewV2ProductionHandlers(pool, cfg.MetadataSchema, cfg.IdentityAudience, sharedauth.NewReportedHashBackend(chainClient), metadata.PostgresCanonicalSource{Schema: cfg.CanonicalSchema, CanonicalStreamID: cfg.CanonicalStreamID}, time.Now)
		if e != nil {
			return nil, e
		}
		provider, e := sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
		if e != nil {
			return nil, e
		}
		awsCfg, e := awsconfig.LoadDefaultConfig(startup, awsconfig.WithRegion(cfg.S3Region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, "")))
		if e != nil {
			return nil, fmt.Errorf("metadata_object_configuration_unavailable")
		}
		client := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.S3Endpoint); o.UsePathStyle = true })
		worker := raster.ProcessWorker{Executable: cfg.RasterExecutable, CgroupParent: cfg.RasterCgroupParent, Isolation: cfg.RasterIsolation}
		var identity metadata.RequestIdentityAuthority
		var identityEndpoint string
		var identityClient *http.Client
		var clientTLS *tls.Config
		if edge {
			identityEndpoint = strings.TrimRight(strings.TrimSpace(cfg.IdentityServiceURL), "/")
			identity = sharedauth.HTTPIdentityAuthority{Endpoint: identityEndpoint, ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: strings.HasPrefix(identityEndpoint, "http://")}
		} else {
			clientTLS = serverTLS.Clone()
			clientTLS.ClientAuth = tls.NoClientCert
			clientTLS.ClientCAs = nil
			clientTLS.RootCAs = serverTLS.ClientCAs
			privateKey, e := hex.DecodeString(cfg.MetadataAssertionPrivateKeyHex)
			if e != nil || len(privateKey) != ed25519.PrivateKeySize {
				return nil, fmt.Errorf("metadata_signer_unavailable")
			}
			mtlsIdentity, e := sharedauth.NewMTLSIdentityAuthority(cfg.IdentityEndpoint, cfg.IdentityPeerCertSHA256, &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: clientTLS}}, sharedauth.ServiceRequestIssuer{Issuer: "metadata", Audience: "identity", Key: ed25519.PrivateKey(privateKey), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}})
			if e != nil {
				return nil, fmt.Errorf("metadata_identity_transport_unavailable")
			}
			identity, identityEndpoint, identityClient = mtlsIdentity, cfg.IdentityEndpoint, mtlsIdentity.Client
		}
		var moderation metadata.VisibilityAuthority
		var moderationReady sharedhttp.Check
		if cfg.ModerationSource == config.ModerationDatabase {
			authority := metadata.PostgresVisibilityAuthority{Pool: pool, Schema: cfg.ModerationSchema}
			moderation, moderationReady = authority, authority.Ready
		} else {
			service := metadata.D12VisibilityClient{Endpoint: cfg.ModerationEndpoint, Secrets: provider, TokenKey: cfg.ModerationBearerKey, Client: metadataPinnedClient(clientTLS, cfg.ModerationPeerCertSHA256)}
			moderation = service
			moderationReady = func(c context.Context) error { return probeServiceReady(c, cfg.ModerationEndpoint, service.Client) }
		}
		instance := metadata.ConnectedMedia{Metadata: v2, Identity: identity, Objects: metadata.S3AssetObjects{Client: client, QuarantineBucket: cfg.QuarantineBucket, PublicBucket: cfg.ApprovedBucket, PublicOrigin: cfg.PublicURL, Endpoint: cfg.S3Endpoint}, Scanner: sharedhttp.ScanOptions{Host: cfg.ClamAVHost, Port: cfg.ClamAVPort, Timeout: 2 * time.Second}, Worker: worker, Moderation: moderation}
		if e = instance.Ready(startup); e != nil {
			return nil, fmt.Errorf("metadata_media_dependencies_unavailable")
		}
		var probe bytes.Buffer
		if e = png.Encode(&probe, image.NewNRGBA(image.Rect(0, 0, 1, 1))); e != nil {
			return nil, e
		}
		if _, e = worker.Decode(startup, probe.Bytes()); e != nil {
			return nil, fmt.Errorf("metadata_worker_os_authority_unavailable")
		}
		if e = moderationReady(startup); e != nil {
			return nil, fmt.Errorf("metadata_moderation_authority_unavailable")
		}
		if cfg.ModerationSource != config.ModerationDatabase {
			moderationToken, e := provider.GetRequired(startup, cfg.ModerationBearerKey)
			if e != nil {
				return nil, fmt.Errorf("metadata_moderation_identity_unavailable")
			}
			request, e := http.NewRequestWithContext(startup, http.MethodGet, cfg.ModerationEndpoint+"/internal/v2/safety/visibility", nil)
			if e != nil {
				return nil, e
			}
			request.Header.Set("Authorization", "Bearer "+moderationToken)
			if _, e = identity.AuthenticateRequest(startup, request); e != nil {
				return nil, fmt.Errorf("metadata_moderation_identity_unavailable")
			}
		}
		checks["media_objects"] = instance.Objects.Ready
		checks["media_scanner"] = func(c context.Context) error { _, e := sharedhttp.ScannerVersion(c, instance.Scanner); return e }
		checks["media_worker"] = func(c context.Context) error {
			if e := c.Err(); e != nil {
				return e
			}
			return worker.Ready()
		}
		checks["media_identity"] = func(c context.Context) error { return probeServiceReady(c, identityEndpoint, identityClient) }
		checks["media_moderation"] = moderationReady
		checks["media_rpc"] = func(c context.Context) error {
			chain, e := rpc.ChainID(c)
			if e != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
				return fmt.Errorf("metadata_chain_authority_unavailable")
			}
			return nil
		}
		publicationRuntime, auditKey, e := cfg.ReadPublicationRuntime()
		if e != nil {
			return nil, e
		}
		committed := metadata.ReportedCommittedProofVerifier{RPC: chainClient, Blocks: sharedauth.PostgresCommittedBlockProvider{Pool: pool}}
		operations := sharedauth.OperationAuthorizer{Ledger: sharedauth.Ledger{Pool: pool, Environment: publicationRuntime.Environment, AuditKey: auditKey}, Verifier: committed, Transport: sharedauth.OperationTransport{EnvelopeHeader: publicationRuntime.EnvelopeHeader, SignatureHeader: publicationRuntime.SignatureHeader}}
		var publishing metadata.PublicationService
		if edge {
			// Edge ingress confirms publication identities with the same
			// identity service that authenticates the requests.
			publishing, e = metadata.NewSessionCheckedPublicationService(instance, operations, metadata.SessionCheckIdentities{Authority: sharedauth.HTTPStreamAuthority(identity.(sharedauth.HTTPIdentityAuthority))})
		} else {
			publishing, e = metadata.NewPublicationService(instance, operations, sharedauth.PostgresIdentityResolver{Pool: pool, GuardFunction: "users.identity_guard_v2"})
		}
		if e != nil {
			return nil, e
		}
		publicationReady := func(c context.Context) error {
			var searchPath string
			if e := pool.QueryRow(c, "SELECT current_setting('search_path')").Scan(&searchPath); e != nil || (searchPath != cfg.MetadataSchema+",pg_catalog" && searchPath != pgx.Identifier{cfg.MetadataSchema}.Sanitize()+",pg_catalog") {
				return fmt.Errorf("metadata_publication_search_path_unavailable")
			}
			if e := committed.CheckCommittedHead(c); e != nil {
				return fmt.Errorf("metadata_publication_committed_authority_unavailable")
			}
			if !edge {
				var guarded bool
				if e := pool.QueryRow(c, "SELECT to_regprocedure('users.identity_guard_v2(uuid,text)') IS NOT NULL AND has_function_privilege('users.identity_guard_v2(uuid,text)','EXECUTE')").Scan(&guarded); e != nil || !guarded {
					return fmt.Errorf("metadata_publication_identity_authority_unavailable")
				}
			}
			for _, table := range []string{"operation_ledger", "security_audit_outbox", "metadata_publication_intents_v3", "metadata_publication_jobs_v3", "metadata_publication_grants_v3"} {
				rows, e := pool.Query(c, "SELECT 1 FROM "+pgx.Identifier{cfg.MetadataSchema, table}.Sanitize()+" LIMIT 0")
				if e != nil {
					return fmt.Errorf("metadata_publication_storage_unavailable")
				}
				rows.Close()
				if rows.Err() != nil {
					return fmt.Errorf("metadata_publication_storage_unavailable")
				}
			}
			return nil
		}
		if e = publicationReady(startup); e != nil {
			return nil, e
		}
		checks["metadata_publication"] = publicationReady
		publication = &publishing
		connected = &instance
		legacyReads = metadata.NewProductionLegacyReads(instance, cfg.PublicURL)
	}
	// Development: the production creator-media surface (token image upload,
	// /v3 metadata publication) with development dependencies, and the v2
	// read adapter that serves its published documents.
	var devReads *metadata.DevelopmentV2Reads
	var devMedia *developmentMedia
	var devStore *storage.Client
	if cfg.NodeEnv != "production" {
		var chain *gethrpc.Client
		var receipts metadata.DevelopmentReceipts
		if cfg.RPCURL != "" {
			chain, err = gethrpc.DialContext(ctx, cfg.RPCURL)
			if err != nil {
				return nil, fmt.Errorf("metadata_development_rpc_invalid")
			}
			rpc = ethclient.NewClient(chain)
			defer func() {
				if !owned {
					rpc.Close()
				}
			}()
			receipts = rpc
		}
		devStore, err = storage.New(ctx, storage.Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, AccessKeyID: cfg.S3AccessKeyID, SecretAccessKey: cfg.S3SecretAccessKey, Bucket: cfg.S3Bucket})
		if err != nil {
			return nil, err
		}
		devReads = metadata.NewDevelopmentV2Reads(pool, receipts, time.Now)
		devMedia, err = newDevelopmentMedia(ctx, cfg, logger, devReads, chain)
		if err != nil {
			return nil, err
		}
		if devMedia != nil {
			defer func() {
				if !owned {
					devMedia.pool.Close()
				}
			}()
			devReads.Connected = devMedia.connected
			connected, publication = devMedia.connected, devMedia.publication
		}
	}
	// Development runs behind Railway's edge, which is the TCP peer of every
	// request: without trusting it the per-IP rate limit would be one bucket
	// shared by every browser (same choice as the development gateway).
	var trustedProxies []netip.Prefix
	if cfg.NodeEnv != "production" {
		trustedProxies = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	} else if edge {
		if trustedProxies, err = cfg.TrustedProxies(); err != nil {
			return nil, err
		}
	}
	mux := sharedhttp.NewRouter(sharedhttp.ServerOptions{TrustedProxies: trustedProxies, CORSOrigins: cfg.CORSAllowedOrigins, Health: sharedhttp.HealthDeps{Custom: checks, State: tracker.Ready, DB: func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) }, Redis: func(c context.Context) error { return rdb.Ping(c).Err() }}})
	mux.Group(func(gr chi.Router) {
		gr.Use(sharedhttp.RateLimit(sharedhttp.RateLimitOptions{Max: rateLimitMax, Window: rateLimitWindow, Redis: rdb}))
		if cfg.NodeEnv == "production" {
			if edge {
				gr.Use(metadata.SessionChannel(connected.Identity))
			} else {
				gr.Use(metadataPrivateChannel(cfg, connected.Identity))
			}
			connected.RegisterRoutes(gr)
			publication.RegisterRoutes(gr)
			legacyReads.RegisterRoutes(gr)
			return
		}
		// Creator uploads authenticate the v2 identity session through the
		// gateway's private introspection; without it the upload stays a 503.
		// The scanner is clamd from CLAMAV_HOST/CLAMAV_PORT and fails closed
		// (503) when unset or unreachable, as in every other upload path.
		var identity metadata.SessionAuthority
		if strings.TrimSpace(cfg.IdentityServiceURL) != "" && len(cfg.IdentityServiceToken) >= 32 {
			identity = sharedauth.HTTPIdentityAuthority{Endpoint: strings.TrimRight(strings.TrimSpace(cfg.IdentityServiceURL), "/"), ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: true}
		} else if logger != nil {
			logger.Warn("development metadata uploads disabled: IDENTITY_SERVICE_URL and IDENTITY_SERVICE_TOKEN (>= 32 chars) are required")
		}
		scanner := sharedhttp.ScanOptions{Host: cfg.ClamAVHost, Port: cfg.ClamAVPort, Timeout: 5 * time.Second}
		metadata.New(metadata.Deps{Queries: sqlcdb.New(pool), Redis: rdb, Store: devStore, PublicURL: cfg.PublicURL, MaxLogoSize: cfg.MaxLogoSizeBytes, MaxBanner: cfg.MaxBannerSizeBytes, Logger: logger, Identity: identity, Scan: func(c context.Context, data []byte) sharedhttp.ScanResult {
			return sharedhttp.ScanBuffer(c, data, scanner)
		}}).RegisterRoutes(gr)
		// Creator token images + /v3 metadata publication: the production
		// handlers on development dependencies (development_connected.go).
		registerDevelopmentMedia(gr, devMedia)
		// Development adapter for the public v2 reads production serves from
		// the canonical stream (V2ContractHandlers/ConnectedMedia): backed by
		// the legacy tables above plus the creation receipt on RPC_URL, and by
		// the production read handlers once a document is published.
		devReads.RegisterRoutes(gr)
	})
	if edge {
		// Immutable approved variants: this service is the public origin
		// (PUBLIC_URL) in front of the approved bucket. The internal account
		// authority (user -> metadata) needs the mTLS private channel and is
		// not served on edge ingress.
		connected.RegisterApprovedObjects(mux)
	} else if cfg.NodeEnv == "production" {
		mux.Group(func(gr chi.Router) { gr.Use(metadataAccountChannel(cfg)); connected.RegisterAccountAuthority(gr) })
	} else if devMedia != nil {
		// Immutable approved variants (the production CDN's role), outside
		// the per-IP rate limit like any static object.
		devMedia.connected.RegisterDevelopmentApprovedObjects(mux)
	}
	// On edge ingress the listener is the public internet: metrics are
	// recorded but not exposed there.
	if metrics != nil && !edge {
		mux.Handle("/metrics", metrics)
	}
	if err = tracker.SetReady(); err != nil {
		return nil, err
	}
	mediaContext, mediaStop := context.WithCancel(ctx)
	mediaDone := make(chan struct{})
	if connected == nil {
		close(mediaDone)
	} else {
		go func() {
			defer close(mediaDone)
			interval := time.Duration(cfg.MediaScanIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = 5 * time.Second
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			lastExpiry := time.Time{}
			for {
				select {
				case <-mediaContext.Done():
					return
				case <-ticker.C:
					release, e := tracker.Acquire()
					if e != nil {
						return
					}
					job, cancel := context.WithTimeout(mediaContext, 20*time.Second)
					expire := time.Since(lastExpiry) >= time.Duration(cfg.MediaExpiryIntervalSeconds)*time.Second
					e = runMediaMaintenance(job, pool, cfg.MetadataSchema, *connected, *publication, expire)
					if expire && e == nil {
						lastExpiry = time.Now()
					}
					if e == nil && devStore != nil {
						e = connected.ProjectDevelopmentLegacy(job, sqlcdb.New(pool), devStore, time.Now)
					}
					cancel()
					release()
					if mediaContext.Err() != nil {
						return
					}
					if cfg.NodeEnv == "production" {
						workerMu.Lock()
						workerError = e
						workerMu.Unlock()
					}
					if e != nil && logger != nil {
						// Development keeps readiness unaffected and logs the cause.
						if cfg.NodeEnv == "production" {
							logger.Error("metadata retention unavailable")
						} else {
							logger.Error("development media maintenance failed", slog.String("err", e.Error()))
						}
					}
				}
			}
		}()
	}
	owned = true
	instance := &App{secretStop: secretStop, mediaStop: mediaStop, mediaDone: mediaDone, tlsConfig: serverTLS, tlsCertFile: cfg.TLSCertFile, tlsKeyFile: cfg.TLSKeyFile, tracker: tracker, Router: runtimeHandler(tracker, mux, metrics), rpc: rpc, pool: pool, redis: rdb, logger: logger}
	if devMedia != nil {
		instance.devMediaPool = devMedia.pool
	}
	return instance, nil
}

func probeServiceReady(ctx context.Context, endpoint string, configured *http.Client) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, e := http.NewRequestWithContext(bounded, http.MethodGet, endpoint+"/health/ready", nil)
	if e != nil {
		return fmt.Errorf("media_authority_unavailable")
	}
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if configured != nil {
		client.Transport = configured.Transport
	}
	response, e := client.Do(request)
	if e != nil {
		return fmt.Errorf("media_authority_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("media_authority_unavailable")
	}
	return nil
}
func runMediaMaintenance(ctx context.Context, pool *pgxpool.Pool, schema string, media metadata.ConnectedMedia, publication metadata.PublicationService, expire bool) error {
	connection, e := pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer connection.Release()
	key := schema + ":metadata-rejected-retention-v2"
	var leader bool
	if e = connection.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&leader); e != nil {
		return e
	}
	if !leader {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := connection.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", key); e != nil {
			connection.Conn().Close(cleanup)
		}
	}()
	if e = scanPendingMedia(ctx, connection, media); e != nil {
		return e
	}
	if e = media.MaintainAccountAssets(ctx, expire); e != nil {
		return e
	}
	if _, e = publication.RunPublicationBatch(ctx, 1); e != nil {
		return e
	}
	if expire {
		return media.ExpireRejected(ctx)
	}
	return nil
}
func (a *App) stopMediaWorkers(ctx context.Context) error {
	if a.mediaStop != nil {
		a.mediaStop()
	}
	if a.mediaDone == nil {
		return nil
	}
	select {
	case <-a.mediaDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases the DB and Redis pools.
func (a *App) Close() {
	closeCtx, cancel := context.WithTimeout(context.Background(), process.DefaultTimeout)
	defer cancel()
	_ = a.stopMediaWorkers(closeCtx)
	if a.secretStop != nil {
		a.secretStop()
	}
	if a.rpc != nil {
		a.rpc.Close()
	}
	if a.devMediaPool != nil {
		a.devMediaPool.Close()
	}
	a.pool.Close()
	if err := a.redis.Close(); err != nil && a.logger != nil {
		a.logger.Warn("redis close", slog.String("err", err.Error()))
	}
}

// Run loads config from the environment, builds the App, serves HTTP, and blocks
// until ctx is cancelled or a shutdown signal arrives, then drains gracefully.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("metadata", cfg.LogLevel)
	logger.Info("starting metadata service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
		TLSConfig:         app.tlsConfig,
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		var err error
		if app.tlsConfig != nil {
			err = srv.ListenAndServeTLS(app.tlsCertFile, app.tlsKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()

	shutErr := process.Run(ctx, process.Options{Tracker: app.tracker,
		Logger: logger,
		OnShutdown: func(sctx context.Context) error {
			return errors.Join(app.stopMediaWorkers(sctx), srv.Shutdown(sctx))
		},
	})

	select {
	case err := <-serveErr:
		return err
	default:
		return shutErr
	}
}

func runtimeHandler(tracker *process.Tracker, next http.Handler, metrics *telemetry.Metrics) http.Handler {
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if metrics == nil || r.Method != http.MethodGet || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		writer := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(writer, r)
		status := writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		exclusion := ""
		if r.Context().Err() != nil {
			exclusion = "client_cancel"
		}
		if status == 400 || status == 422 {
			exclusion = "invalid_request"
		}
		_ = metrics.RecordRead("other", status, time.Since(started), exclusion)
	})
	tracked := tracker.Middleware(observed)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		tracked.ServeHTTP(w, r)
	})
}

func metadataServerTLS(cfg config.Config) (*tls.Config, error) {
	info, e := os.Stat(cfg.TLSKeyFile)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("metadata_tls_key_authority_unavailable")
	}
	pair, e := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if e != nil {
		return nil, fmt.Errorf("metadata_tls_keypair_unavailable")
	}
	roots, e := os.ReadFile(cfg.TLSClientCAFile)
	if e != nil || len(roots) > 1048576 {
		return nil, fmt.Errorf("metadata_tls_client_authority_unavailable")
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(roots) {
		return nil, fmt.Errorf("metadata_tls_client_authority_unavailable")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: ca, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}
func metadataPrivateChannel(cfg config.Config, identity metadata.RequestIdentityAuthority) func(http.Handler) http.Handler {
	key, _ := hex.DecodeString(cfg.GatewayPublicKeyHex)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deny := func(status int) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]string{"code": "media_channel_unavailable"})
			}
			if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
				deny(403)
				return
			}
			peer := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
			if hex.EncodeToString(peer[:]) != cfg.GatewayPeerCertSHA256 {
				deny(403)
				return
			}
			protected := r.Method != http.MethodGet || strings.Contains(r.URL.Path, "/assets/")
			if !protected {
				next.ServeHTTP(w, r)
				return
			}
			limit := int64(32768)
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/metadata-publications") {
				limit = metadata.PublicationBodyLimit
			}
			if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/assets/") {
				limit = 6 << 20
			}
			body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
			if e != nil {
				deny(413)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			values := r.Header.Values("X-Internal-Assertion")
			signatures := r.Header.Values("X-Internal-Signature")
			if len(values) != 1 || len(signatures) != 1 || len(values[0]) > 12000 {
				deny(403)
				return
			}
			raw, e := base64.RawURLEncoding.Strict().DecodeString(values[0])
			if e != nil || len(raw) > 8192 {
				deny(403)
				return
			}
			signature, e := base64.RawURLEncoding.Strict().DecodeString(signatures[0])
			if e != nil || len(signature) != ed25519.SignatureSize {
				deny(403)
				return
			}
			var assertion sharedauth.ServiceAssertion
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&assertion) != nil || decoder.Decode(new(any)) != io.EOF {
				deny(403)
				return
			}
			canonical, e := json.Marshal(assertion)
			if e != nil || !bytes.Equal(canonical, raw) {
				deny(403)
				return
			}
			principal, e := sharedauth.VerifyServiceAssertion(assertion, signature, ed25519.PublicKey(key), r.TLS, cfg.GatewayPeerCertSHA256, "gateway", "metadata", sharedauth.RequestDigest(r.Method, r.URL.RequestURI(), body), time.Now())
			if e != nil {
				deny(403)
				return
			}
			authenticated, e := identity.AuthenticateRequest(r.Context(), r)
			if e != nil {
				status := 503
				if errors.Is(e, sharedauth.ErrScopeDenied) {
					status = 403
				}
				deny(status)
				return
			}
			if authenticated.Wallet != principal.Wallet || authenticated.SessionID != principal.SessionID {
				deny(403)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func scanPendingMedia(ctx context.Context, connection *pgxpool.Conn, media metadata.ConnectedMedia) error {
	store := media.Metadata.Store
	if store == nil {
		return fmt.Errorf("metadata_scan_store_unavailable")
	}
	table := func(name string) string { return pgx.Identifier{store.Schema, name}.Sanitize() }
	rows, e := store.Pool.Query(ctx, "SELECT a.id,a.pool,a.hash,a.size,a.object_key,a.state,d.mime FROM "+table("assets")+" a JOIN "+table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.state IN('UPLOADING','QUARANTINED','VALIDATING') OR(a.state='APPROVED' AND d.variants='{}'::jsonb) ORDER BY d.created_at LIMIT 10")
	if e != nil {
		return e
	}
	type pending struct {
		id, pool, hash, key, state, mime string
		size                             int64
	}
	var items []pending
	for rows.Next() {
		var asset pending
		if e = rows.Scan(&asset.id, &asset.pool, &asset.hash, &asset.size, &asset.key, &asset.state, &asset.mime); e != nil {
			rows.Close()
			return e
		}
		items = append(items, asset)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, asset := range items {
		lock := store.Schema + ":" + asset.id
		var locked bool
		if e = connection.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", lock).Scan(&locked); e != nil {
			return e
		}
		if !locked {
			continue
		}
		e = func() error {
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if _, e := connection.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lock); e != nil {
					connection.Conn().Close(cleanup)
				}
			}()
			visibility, e := media.Moderation.Read(ctx, asset.pool)
			if e != nil {
				return fmt.Errorf("metadata_scan_moderation_unavailable")
			}
			if e = store.Restrict(ctx, asset.pool, strconv.FormatInt(visibility.Revision, 10), visibility.Status == "removed"); e != nil {
				return e
			}
			if visibility.Status == "removed" {
				return nil
			}
			var current string
			if e = store.Pool.QueryRow(ctx, "SELECT state FROM "+table("assets")+" WHERE id=$1", asset.id).Scan(&current); e != nil {
				return e
			}
			if current != "UPLOADING" && current != "QUARANTINED" && current != "VALIDATING" && current != "APPROVED" {
				return nil
			}
			if current == "QUARANTINED" {
				if e = store.TransitionAsset(ctx, asset.id, current, "VALIDATING"); e != nil {
					return e
				}
				current = "VALIDATING"
			}
			key := asset.key
			if current == "APPROVED" {
				key = "quarantine/" + asset.id
			}
			if key != "quarantine/"+asset.id {
				return fmt.Errorf("metadata_quarantine_identity_invalid")
			}
			object, e := media.Objects.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(media.Objects.QuarantineBucket), Key: aws.String(key)})
			if e != nil {
				var api smithy.APIError
				if current == "UPLOADING" && errors.As(e, &api) && api.ErrorCode() == "NoSuchKey" {
					return nil
				}
				return fmt.Errorf("metadata_quarantine_unavailable")
			}
			original, e := raster.ReadBounded(object.Body)
			object.Body.Close()
			if e != nil {
				return e
			}
			digest := sha256.Sum256(original)
			if int64(len(original)) != asset.size || hex.EncodeToString(digest[:]) != asset.hash || http.DetectContentType(original) != asset.mime {
				return fmt.Errorf("metadata_quarantine_integrity_failed")
			}
			decoded, e := media.Worker.Decode(ctx, original)
			if e != nil {
				return e
			}
			if current == "UPLOADING" {
				if e = store.TransitionAsset(ctx, asset.id, "UPLOADING", "QUARANTINED"); e != nil {
					return e
				}
				if e = store.TransitionAsset(ctx, asset.id, "QUARANTINED", "VALIDATING"); e != nil {
					return e
				}
				current = "VALIDATING"
			}
			scan := sharedhttp.ScanVerifiedBuffer(ctx, original, media.Scanner)
			if scan.Verdict == sharedhttp.ScanInfected {
				if current != "VALIDATING" {
					return fmt.Errorf("approved_asset_scan_rejected")
				}
				tag, e := store.Pool.Exec(ctx, "WITH rejected AS(UPDATE "+table("assets")+" SET state='REJECTED',scanner_engine=$2,scanner_revision=$3 WHERE id=$1 AND state='VALIDATING' RETURNING id) UPDATE "+table("asset_details_v2")+" SET rejected_until=clock_timestamp()+interval '24 hours' WHERE asset_id IN(SELECT id FROM rejected)", asset.id, scan.Engine, scan.SignatureRevision)
				if e != nil {
					return e
				}
				if tag.RowsAffected() != 1 {
					return fmt.Errorf("metadata_rejection_conflict")
				}
				return nil
			}
			if scan.Verdict != sharedhttp.ScanClean {
				return fmt.Errorf("metadata_scanner_unavailable")
			}
			production := *store
			production.RasterWorker = media.Worker
			if e = production.Promote(ctx, asset.id, original, decoded.Variants, scan, media.Objects); e != nil {
				return e
			}
			variants := map[string]string{}
			for _, size := range []int{256, 512, 1024} {
				hash := sha256.Sum256(decoded.Variants[size])
				variants[strconv.Itoa(size)] = fmt.Sprintf("approved/%s/%d/%s.png", asset.hash, size, hex.EncodeToString(hash[:]))
			}
			raw, _ := json.Marshal(variants)
			tag, e := store.Pool.Exec(ctx, "UPDATE "+table("asset_details_v2")+" SET variants=$2::jsonb WHERE asset_id=$1 AND mime=$3 AND width=$4 AND height=$5", asset.id, string(raw), asset.mime, decoded.Width, decoded.Height)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("metadata_asset_details_conflict")
			}
			return nil
		}()
		if e != nil {
			return e
		}
	}
	return nil
}

func metadataAccountChannel(cfg config.Config) func(http.Handler) http.Handler {
	key, _ := hex.DecodeString(cfg.UserAssertionPublicKeyHex)
	verify := sharedauth.ServiceRequestVerifier{Issuer: "user", Audience: "metadata", PeerCertSHA256: cfg.UserPeerCertSHA256, Key: ed25519.PublicKey(key), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
			if e != nil {
				http.Error(w, "media_channel_unavailable", 413)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if e = verify.Authenticate(r, body); e != nil {
				http.Error(w, "media_channel_unavailable", 403)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func metadataPinnedClient(config *tls.Config, pin string) *http.Client {
	cloned := config.Clone()
	cloned.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return sharedauth.ErrScopeDenied
		}
		digest := sha256.Sum256(state.PeerCertificates[0].Raw)
		if hex.EncodeToString(digest[:]) != pin {
			return sharedauth.ErrScopeDenied
		}
		return nil
	}
	return &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: cloned}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
