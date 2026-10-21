// Package app wires the media/gateway edge together (config, Redis, per-bucket
// R2 clients, EIP-191->JWT auth, the media/social REST + WS tunnel, the R2 serve
// edge, and the guarded create-wizard upload) and owns its lifecycle. Keeping
// the wiring here (rather than in main) makes the whole edge testable end-to-end
// against external test servers + MinIO + httptest upstreams.
package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	mediastore "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
	control "github.com/Sidiora-Technologies/KindleLaunch/security/control"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethereum/go-ethereum/ethclient"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/devidentity"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/proxy"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/serve"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/upload"
)

// App is a fully-wired, ready-to-serve gateway edge.
type App struct {
	secretStop     context.CancelFunc
	tracker        *process.Tracker
	Router         http.Handler
	privateRouter  http.Handler
	privateAddress string
	privateTLS     *tls.Config
	runtimeStop    context.CancelFunc
	runtimeWG      *sync.WaitGroup
	broker         *nats.Conn
	consumer       *natsdriver.Consumer
	auditConsumer  *natsdriver.Consumer

	identity    *auth.Identity
	rpc         *ethclient.Client
	devIdentity *devidentity.Runtime
	controlPool *pgxpool.Pool
	pool        *pgxpool.Pool
	redis       *goredis.Client
	logger      *slog.Logger
}

// New opens the Redis pool + per-bucket R2 clients and builds the HTTP router
// with health endpoints, rate-limited public routes, the auth + social tunnel,
// the media serve edge, and the guarded upload.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	secretCheck, secretStop, secretErr := cfg.ResolveProductionSecrets(ctx)
	if secretErr != nil {
		return nil, secretErr
	}
	secretOwned := false
	defer func() {
		if !secretOwned {
			secretStop()
		}
	}()
	tracker := process.NewTracker()
	var metrics *telemetry.Metrics
	if cfg.NodeEnv == "production" {
		var e error
		metrics, e = telemetry.New("mediagatewayd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if e != nil {
			return nil, e
		}
	}
	secretChecks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		secretChecks["secrets"] = secretCheck
	}

	var pool, controlPool *pgxpool.Pool
	var runtimeBinding gatewayRuntimeBinding
	if cfg.NodeEnv == "production" {
		var err error
		if err = config.ReadBoundDocument(cfg.GatewayRuntimeFile, cfg.GatewayRuntimeSHA256, &runtimeBinding); err != nil {
			return nil, err
		}
		pool, controlPool, err = cfg.OpenSplitRuntimePools(ctx, runtimeBinding.Pools)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				pool.Close()
				controlPool.Close()
			}
		}()
	}
	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		return nil, err
	}

	defer func() {
		if !secretOwned {
			_ = rdb.Close()
		}
	}()
	readers, err := buildBuckets(ctx, cfg)
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}

	authH := auth.New(auth.Deps{
		Redis:     rdb,
		JWTSecret: cfg.JWTSecret,
		JWTTTL:    time.Duration(cfg.JWTTTLSeconds) * time.Second,
		NonceTTL:  time.Duration(cfg.NonceTTLSeconds) * time.Second,
		AppDomain: cfg.AppDomain,
		Logger:    logger,
	})

	upstreamTimeout := time.Duration(cfg.UpstreamTimeoutSeconds) * time.Second
	restProxy, err := proxy.NewREST(proxy.RESTDeps{
		TargetBaseURL: cfg.SocialHTTPURL,
		Prefix:        "/social",
		Timeout:       upstreamTimeout,
		Logger:        logger,
	})
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}
	// Frozen v2 follow-graph paths are forwarded verbatim (no prefix strip) to
	// the social upstream outside production; production mounts them through
	// the connected social allowlist instead.
	followProxy, err := proxy.NewREST(proxy.RESTDeps{
		TargetBaseURL: cfg.SocialHTTPURL,
		Prefix:        "",
		Timeout:       upstreamTimeout,
		Logger:        logger,
	})
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}
	// Development account owner writes media/user serves outside production
	// (USER_HTTP_URL). The caller's bearer is forwarded so media/user can
	// re-verify the session with the identity authority itself.
	var userProxy http.Handler
	if cfg.NodeEnv != "production" && strings.TrimSpace(cfg.UserHTTPURL) != "" {
		up, e := proxy.NewREST(proxy.RESTDeps{
			TargetBaseURL:        cfg.UserHTTPURL,
			Prefix:               "",
			Timeout:              upstreamTimeout,
			Logger:               logger,
			ForwardAuthorization: true,
			Upstream:             "user",
		})
		if e != nil {
			_ = rdb.Close()
			return nil, e
		}
		userProxy = up
	}
	wsProxy, err := proxy.NewWS(proxy.WSDeps{
		TargetBaseURL: cfg.SocialWSURL,
		Logger:        logger,
	})
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}

	objCache := cache.New(rdb, time.Duration(cfg.ObjectCacheTTLSeconds)*time.Second)
	serveH := serve.New(serve.Deps{
		Buckets:      readers,
		Cache:        objCache,
		CacheMaxSize: cfg.ObjectCacheMaxBytes,
		Logger:       logger,
	})

	uploadH := upload.New(upload.Deps{
		MetadataBaseURL: cfg.MetadataUploadURL,
		MaxBytes:        cfg.MaxUploadBytes,
		Timeout:         upstreamTimeout,
		Logger:          logger,
	})

	var connected *gatewayConnectedRuntime
	if cfg.NodeEnv == "production" {
		connected, err = newGatewayConnectedRuntime(ctx, cfg, pool, controlPool, runtimeBinding, rdb, objCache, readers, logger)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				connected.close()
			}
		}()
		secretChecks["connected_runtime"] = connected.ready
	}
	identityMetrics := newIdentityRuntimeMetrics()
	var identity *auth.Identity
	var identityRPC *ethclient.Client
	if cfg.NodeEnv == "production" {
		var identityConfig struct {
			Issuer         string            `json:"issuer"`
			Audience       string            `json:"audience"`
			ActiveKey      string            `json:"activeKey"`
			Keys           []auth.SigningKey `json:"keys"`
			Domain         string            `json:"domain"`
			URI            string            `json:"uri"`
			AllowedOrigins []string          `json:"allowedOrigins"`
		}
		if err := config.ReadBoundDocument(cfg.IdentityConfigFile, cfg.IdentityConfigSHA256, &identityConfig); err != nil {
			return nil, err
		}
		originSet := map[string]bool{}
		for _, origin := range strings.Split(cfg.CORSAllowedOrigins, ",") {
			originSet[origin] = true
		}
		if len(identityConfig.AllowedOrigins) != len(originSet) {
			return nil, errors.New("identity and CORS origin bindings disagree")
		}
		for _, origin := range identityConfig.AllowedOrigins {
			if !originSet[origin] {
				return nil, errors.New("identity and CORS origin bindings disagree")
			}
			delete(originSet, origin)
		}
		provider, err := sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
		if err != nil {
			return nil, err
		}
		identityKeyValues := map[string]string{}
		for _, key := range identityConfig.Keys {
			value, e := provider.GetRequired(ctx, key.SecretReference)
			if e != nil {
				return nil, fmt.Errorf("identity_signing_secret_unavailable")
			}
			identityKeyValues[key.SecretReference] = value
		}
		secretChecks["identity_signing_keys"] = func(c context.Context) error {
			for reference, consumed := range identityKeyValues {
				value, e := provider.GetRequired(c, reference)
				if e != nil {
					return e
				}
				if value != consumed {
					return errors.New("identity signing key changed; restart required")
				}
			}
			return nil
		}
		tokens, err := auth.NewIdentityTokens(identityConfig.Issuer, identityConfig.Audience, identityConfig.ActiveKey, identityConfig.Keys, identityKeyResolver{provider}, time.Now)
		if err != nil {
			return nil, err
		}
		tokens.SetObserver(identityMetrics.observe)
		identityRPC, err = ethclient.DialContext(ctx, cfg.RPCURL)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				identityRPC.Close()
			}
		}()
		identity, err = auth.NewIdentity(auth.IdentityConfig{Pool: pool, Domain: identityConfig.Domain, URI: identityConfig.URI, AllowedOrigins: identityConfig.AllowedOrigins, Verifier: auth.Chain125ProofVerifier{Backend: sharedauth.NewReportedHashBackend(identityRPC.Client())}, LimiterKey: []byte(cfg.IdentityLimiterKey), Tokens: tokens, Observe: identityMetrics.observe, SecurityAuditKey: connected.auditKey, Events: &auth.IdentityEventDelivery{Store: connected.store, Subject: auth.IdentityRevisionSubject, EventType: auth.IdentityRevisionEventType}})
		if err != nil {
			return nil, err
		}
	}
	var devIdentity *devidentity.Runtime
	if cfg.NodeEnv != "production" {
		devIdentity, err = openDevelopmentIdentity(ctx, cfg, identityMetrics.observe, logger)
		if err != nil {
			return nil, err
		}
		if devIdentity != nil {
			defer func() {
				if !secretOwned {
					devIdentity.Close()
				}
			}()
			identity = devIdentity.Identity
			pool = devIdentity.Pool
		}
	}
	var trustedProxies []netip.Prefix
	if connected != nil {
		trustedProxies = connected.trustedProxies
	} else if cfg.NodeEnv != "production" {
		trustedProxies = developmentTrustedProxies()
	}
	mux := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		TrustedProxies: trustedProxies,
		CORSOrigins:    cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB: func(c context.Context) error {
				if pool == nil {
					return fmt.Errorf("identity_database_missing")
				}
				if cfg.NodeEnv != "production" {
					return pool.Ping(c)
				}
				return cfg.CheckRuntimeSchema(c, pool)
			},
			Redis: func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})

	// Rate-limit all public ingress per IP (health probes stay un-throttled, i12).
	mux.Group(func(gr chi.Router) {
		gr.Use(sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
			Max:    cfg.RateLimitMax,
			Window: time.Duration(cfg.RateLimitWindowSeconds) * time.Second,
			Redis:  rdb,
		}))

		if cfg.NodeEnv != "production" {
			authH.RegisterRoutes(gr)
		}
		if connected == nil {
			serveH.RegisterRoutes(gr)
		} else {
			if e := connected.approved.RegisterConnectedRoutes(gr, connected.binding.ApprovedBucket); e != nil {
				connected.fail(e)
			}
			for _, upstream := range connected.upstreams {
				for _, route := range upstream.routes {
					handler := http.Handler(upstream.handler)
					if route.Upload && route.Method == http.MethodPut {
						handler = upstream.upload
					}
					gr.Method(route.Method, route.Template, handler)
				}
			}
			socketHandler, e := connected.control.RequireCapability("socket", connected.socket)
			if e != nil {
				connected.fail(e)
			} else {
				gr.Method(http.MethodGet, "/v2/stream", socketHandler)
			}
			for _, route := range gatewayControlRoutes() {
				gr.Method(route.Method, route.Template, connected.controlHandler)
			}
		}
		if cfg.NodeEnv != "production" {
			uploadH.RegisterRoutes(gr)
		}

		// Development sessions: the legacy HS256 bearer from /auth/login or a
		// v2 identity session (bearer or __Host-kindle_access cookie) when
		// development identity is enabled.
		sessions := auth.DevSessions{Legacy: authH, Identity: identity}
		// Social realtime tunnel: WS requires a session (exact path beats the
		// REST wildcard below in chi's trie).
		if cfg.NodeEnv != "production" {
			gr.With(sessions.Require).Method(http.MethodGet, "/social/ws", wsProxy)
		}
		// Social REST: reads are public; writes need a session, which social
		// itself enforces once the gateway injects X-Actor-Wallet.
		if cfg.NodeEnv != "production" {
			gr.With(sessions.Optional).Handle("/social/*", restProxy)
			// Frozen v2 social paths the web calls (follow graph, /v2/social,
			// /v2/chat, comments, callouts, bounties, rooms) are forwarded
			// verbatim to the social upstream, as production's connected
			// social allowlist does. Reads attach the actor when present;
			// writes require a session.
			for _, route := range proxy.DevelopmentSocialRoutes() {
				guard := sessions.Optional
				if route.Authenticate {
					guard = sessions.Require
				}
				gr.With(guard).Method(route.Method, route.Template, followProxy)
			}
		}
		// Account owner routes whose service (media/user's connected
		// account runtime) is production-only answer a JSON 503 here.
		if cfg.NodeEnv != "production" {
			registerDevelopmentAccountRoutes(gr)
			// Owner writes media/user serves in development. Without
			// USER_HTTP_URL they answer the same frozen 503.
			for _, route := range proxy.DevelopmentUserRoutes() {
				if userProxy == nil {
					gr.Method(route.Method, route.Template, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { auth.WriteNotServed(w) }))
					continue
				}
				gr.With(sessions.Require).Method(route.Method, route.Template, userProxy)
			}
		}

	})

	var privateRouter http.Handler
	if devIdentity != nil {
		// Outside production the real v2 identity surface is served on the
		// main listener. /internal/v2/identity/* is admitted by
		// publicGatewayHandler only for private-network callers and is
		// authenticated with the IDENTITY_SERVICE_TOKEN bearer.
		if err := identity.RegisterV2Routes(mux, auth.IdentityHTTPConfig{ServiceToken: cfg.IdentityServiceToken, DevelopmentOwnerReads: true}); err != nil {
			return nil, fmt.Errorf("development identity routes: %w", err)
		}
	} else if identity != nil {
		if err := identity.RegisterV2Routes(mux, auth.IdentityHTTPConfig{ServiceToken: cfg.IdentityServiceToken, ServiceVerifiers: connected.identityVerifiers}); err != nil {
			return nil, err
		}
		privateRouter = privateIdentityHandler(mux)
		connected.start(identity)
	}
	if metrics != nil {
		mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.ServeHTTP(w, r)
			if r.Method == http.MethodGet {
				identityMetrics.write(w)
				if connected != nil {
					connected.writeMediaMetrics(w)
				}
			}
		}))
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	result := &App{secretStop: secretStop, tracker: tracker, Router: runtimeHandler(tracker, publicGatewayHandler(mux, cfg), metrics), identity: identity, rpc: identityRPC, pool: pool, controlPool: controlPool, redis: rdb, logger: logger}
	result.devIdentity = devIdentity
	if connected != nil {
		result.privateRouter = privateRouter
		result.privateAddress = connected.binding.IdentityListenAddress
		result.privateTLS = connected.serverTLS
		result.runtimeStop = connected.stop
		result.runtimeWG = &connected.wg
		result.broker = connected.nc
		result.consumer = connected.consumer
		result.auditConsumer = connected.auditConsumer
	}
	return result, nil
}

// buildBuckets constructs one R2 client per configured logical bucket, all on
// the same endpoint/credentials.
func buildBuckets(ctx context.Context, cfg config.Config) (map[string]serve.Reader, error) {
	readers := make(map[string]serve.Reader)
	for name, bucket := range cfg.Buckets() {
		client, err := storage.New(ctx, storage.Config{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			Bucket:          bucket,
		})
		if err != nil {
			return nil, fmt.Errorf("bucket %q: %w", name, err)
		}
		readers[name] = client
	}
	return readers, nil
}

// Close releases the Redis pool.
func (a *App) Close() {
	if a.runtimeStop != nil {
		a.runtimeStop()
	}
	if a.consumer != nil {
		a.consumer.Close()
	}
	if a.auditConsumer != nil {
		a.auditConsumer.Close()
	}
	if a.runtimeWG != nil {
		a.runtimeWG.Wait()
	}
	if a.broker != nil {
		a.broker.Close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	if a.controlPool != nil {
		a.controlPool.Close()
	}
	if a.pool != nil {
		a.pool.Close()
	}
	if a.rpc != nil {
		a.rpc.Close()
	}
	if a.devIdentity != nil {
		// Closes the identity pool (also held in a.pool, closed above; pgxpool
		// Close is idempotent) and the chain RPC client.
		a.devIdentity.Close()
	}
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
	logger := sharedlog.New("gateway", cfg.LogLevel)
	logger.Info("starting media gateway")

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
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 2)
	var privateServer *http.Server
	if app.privateRouter != nil {
		privateServer = &http.Server{Addr: app.privateAddress, Handler: app.privateRouter, TLSConfig: app.privateTLS, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
		listener, e := net.Listen("tcp", app.privateAddress)
		if e != nil {
			return e
		}
		go func() {
			if e := privateServer.Serve(tls.NewListener(listener, app.privateTLS)); e != nil && !errors.Is(e, http.ErrServerClosed) {
				serveErr <- e
				cancel()
			}
		}()
	}
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()

	shutErr := process.Run(ctx, process.Options{Tracker: app.tracker,
		Logger: logger,
		OnShutdown: func(sctx context.Context) error {
			if privateServer != nil {
				return errors.Join(srv.Shutdown(sctx), privateServer.Shutdown(sctx))
			}
			return srv.Shutdown(sctx)
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

type identityKeyResolver struct{ provider sharedsecrets.Provider }

func (r identityKeyResolver) ResolveIdentityKey(ctx context.Context, reference string) ([]byte, error) {
	value, err := r.provider.GetRequired(ctx, reference)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

type gatewayRuntimeBinding struct {
	Pools                        config.RuntimePoolSplit      `json:"pools"`
	Control                      gatewayControlBinding        `json:"control"`
	ObjectWorkers                []gatewayObjectWorkerBinding `json:"objectWorkers"`
	TrustedProxies               []string                     `json:"trustedProxies"`
	IdentityEndpoint             string                       `json:"identityEndpoint"`
	IdentityServerPeerCertSHA256 string                       `json:"identityServerPeerCertSHA256"`
	IdentityPeers                []gatewayIdentityPeer        `json:"identityPeers"`
	IdentityListenAddress        string                       `json:"identityListenAddress"`
	CAFile                       string                       `json:"caFile"`
	CASHA256                     string                       `json:"caSHA256"`
	ClientCertFile               string                       `json:"clientCertFile"`
	ClientCertSHA256             string                       `json:"clientCertSHA256"`
	ClientKeyReference           string                       `json:"clientKeyReference"`
	ServerCertFile               string                       `json:"serverCertFile"`
	ServerCertSHA256             string                       `json:"serverCertSHA256"`
	ServerKeyReference           string                       `json:"serverKeyReference"`
	AssertionKeyReference        string                       `json:"assertionKeyReference"`
	NATSURL                      string                       `json:"natsURL"`
	NATSCredentialsReference     string                       `json:"natsCredentialsReference"`
	Stream                       string                       `json:"stream"`
	Durable                      string                       `json:"durable"`
	ConsumerGeneration           string                       `json:"consumerGeneration"`
	RecoverySchema               string                       `json:"recoverySchema"`
	RecoveryEpoch                int64                        `json:"recoveryEpoch"`
	RecoveryRegion               string                       `json:"recoveryRegion"`
	MediaSchema                  string                       `json:"mediaSchema"`
	MetadataSchema               string                       `json:"metadataSchema"`
	SafetySchema                 string                       `json:"safetySchema"`
	ApprovedBucket               string                       `json:"approvedBucket"`
	UploadNamespace              string                       `json:"uploadNamespace"`
	UploadConcurrent             int                          `json:"uploadConcurrent"`
	UploadBytesPerSecond         int64                        `json:"uploadBytesPerSecond"`
	// PurgeMode selects how approved-media purges complete: "http" (the
	// default when empty) submits them to the CDN purge API at
	// purgeEndpoint; "origin_cache" is for a deployment with no CDN in front
	// of the gateway (approved media is served no-store), where the gateway's
	// own object cache is the only retained copy and is evicted directly.
	PurgeMode           string                   `json:"purgeMode"`
	PurgeEndpoint       string                   `json:"purgeEndpoint"`
	PurgeTokenReference string                   `json:"purgeTokenReference"`
	Audit               gatewayAuditBinding      `json:"audit"`
	Upstreams           []gatewayUpstreamBinding `json:"upstreams"`
}
type gatewayAuditBinding struct {
	KeyReference       string `json:"keyReference"`
	DeliverySchema     string `json:"deliverySchema"`
	SinkDeliverySchema string `json:"sinkDeliverySchema"`
	SinkSchema         string `json:"sinkSchema"`
	Subject            string `json:"subject"`
	EventType          string `json:"eventType"`
	Stream             string `json:"stream"`
	Durable            string `json:"durable"`
}
type gatewayIdentityPeer struct {
	Issuer         string `json:"issuer"`
	PeerCertSHA256 string `json:"peerCertSHA256"`
	PublicKey      string `json:"publicKey"`
}
type gatewayUpstreamBinding struct {
	Name   string                 `json:"name"`
	URL    string                 `json:"url"`
	Routes []proxy.ConnectedRoute `json:"routes"`
}
type gatewayUpstream struct {
	routes  []proxy.ConnectedRoute
	handler *proxy.Connected
	upload  http.Handler
}
type gatewayConnectedRuntime struct {
	binding           gatewayRuntimeBinding
	trustedProxies    []netip.Prefix
	store             *delivery.Store
	nc                *nats.Conn
	publisher         *natsdriver.Publisher
	consumer          *natsdriver.Consumer
	auditKey          []byte
	auditBridge       sharedauth.AuditBridge
	auditSink         sharedauth.AuditSink
	auditPublisher    *natsdriver.Publisher
	auditConsumer     *natsdriver.Consumer
	identityVerifiers []sharedauth.ServiceRequestVerifier
	socket            *proxy.ConnectedWS
	approved          *serve.Approved
	control           *control.Runtime
	controlHandler    http.Handler
	mediaAuthority    *mediastore.Authority
	objectCache       *cache.Cache
	objects           []gatewayObjectWorker
	purge             mediastore.PurgeWorker
	upstreams         []gatewayUpstream
	serverTLS         *tls.Config
	stop              context.CancelFunc
	ctx               context.Context
	wg                sync.WaitGroup
	purgeAge          atomic.Int64
	purgeErrors       atomic.Uint64
	failure           atomic.Pointer[error]
	provider          sharedsecrets.Provider
	secrets           map[string]string
	logger            *slog.Logger
}

// privateListenerTLS is the TLS policy of the private identity listener
// (identityListenAddress): TLS 1.3 only, and a client certificate that chains
// to the private CA is required on every connection.
func privateListenerTLS(roots *x509.CertPool, server tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{server}}
}

// privateIdentityHandler serves the private identity listener: only
// /internal/v2/identity/*, and only to a verified client chain. The identity
// routes then authenticate the calling service with the verifiers from
// identityServiceVerifiers; the IDENTITY_SERVICE_TOKEN bearer is never
// accepted there.
func privateIdentityHandler(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/internal/v2/identity/") || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// identityServiceVerifiers turns the runtime document's identityPeers into the
// request verifiers of the identity authority: the gateway itself (issuer
// "gateway", its own client certificate and assertion key) plus one verifier
// per peer, each bound to its issuer, its client-certificate DER sha256 and
// its ed25519 public key (unpadded base64url). Issuers are free-form tokens
// (user, metadata, social, livestream, pnld, apid, stats, ...), unique and
// never "gateway"; at most 16 peers.
func identityServiceVerifiers(peers []gatewayIdentityPeer, ownPin string, ownKey ed25519.PublicKey) ([]sharedauth.ServiceRequestVerifier, error) {
	identityTransport := sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}
	verifiers := []sharedauth.ServiceRequestVerifier{{Transport: identityTransport, Issuer: "gateway", Audience: "identity", PeerCertSHA256: ownPin, Key: ownKey}}
	peerIssuers := map[string]bool{"gateway": true}
	if len(peers) == 0 || len(peers) > 16 {
		return nil, errors.New("identity service peers required")
	}
	for _, peer := range peers {
		if peerIssuers[peer.Issuer] || peer.Issuer == "" || len(peer.PeerCertSHA256) != 64 {
			return nil, errors.New("identity service peer binding invalid")
		}
		public, e := base64.RawURLEncoding.Strict().DecodeString(peer.PublicKey)
		if e != nil || len(public) != ed25519.PublicKeySize {
			return nil, errors.New("identity service peer key invalid")
		}
		verifiers = append(verifiers, sharedauth.ServiceRequestVerifier{Transport: identityTransport, Issuer: peer.Issuer, Audience: "identity", PeerCertSHA256: peer.PeerCertSHA256, Key: ed25519.PublicKey(public)})
		peerIssuers[peer.Issuer] = true
	}
	return verifiers, nil
}

func gatewayBoundBytes(path, digest string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1048576 || len(digest) != 64 {
		return nil, errors.New("gateway TLS file binding unavailable")
	}
	raw, e := os.ReadFile(path)
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return nil, errors.New("gateway TLS file binding mismatch")
	}
	return raw, nil
}
func newGatewayConnectedRuntime(ctx context.Context, cfg config.Config, pool, controlPool *pgxpool.Pool, binding gatewayRuntimeBinding, rdb *goredis.Client, objectCache *cache.Cache, readers map[string]serve.Reader, logger *slog.Logger) (*gatewayConnectedRuntime, error) {
	lifecycle, stop := context.WithCancel(ctx)
	runtime := &gatewayConnectedRuntime{ctx: lifecycle, stop: stop, secrets: map[string]string{}, logger: logger}
	owned := false
	defer func() {
		if !owned {
			runtime.close()
		}
	}()
	runtime.binding = binding
	b := runtime.binding
	if len(b.TrustedProxies) > 16 {
		return nil, errors.New("gateway proxy trust bound exceeded")
	}
	for _, raw := range b.TrustedProxies {
		prefix, e := netip.ParsePrefix(raw)
		if e != nil || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, errors.New("explicit ingress proxy CIDR required")
		}
		runtime.trustedProxies = append(runtime.trustedProxies, prefix)
	}
	endpoint, e := url.Parse(b.IdentityEndpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("private identity HTTPS authority required")
	}
	host, port, e := net.SplitHostPort(b.IdentityListenAddress)
	if e != nil || port == "" || (net.ParseIP(host) == nil) || b.IdentityListenAddress == fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port) {
		return nil, errors.New("distinct explicit private identity listener required")
	}
	if len(b.Upstreams) != 4 || b.ConsumerGeneration == "" || len(b.ConsumerGeneration) > 128 || readers[b.ApprovedBucket] == nil {
		return nil, errors.New("gateway upstream and durable bindings incomplete")
	}
	runtime.provider, e = sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
	if e != nil {
		return nil, e
	}
	secret := func(reference string) (string, error) {
		if reference == "" {
			return "", errors.New("gateway secret reference required")
		}
		value, e := runtime.provider.GetRequired(ctx, reference)
		if e != nil {
			return "", errors.New("gateway runtime secret unavailable")
		}
		runtime.secrets[reference] = value
		return value, nil
	}
	ca, e := gatewayBoundBytes(b.CAFile, b.CASHA256)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("gateway private CA invalid")
	}
	cert, e := gatewayBoundBytes(b.ClientCertFile, b.ClientCertSHA256)
	if e != nil {
		return nil, e
	}
	key, e := secret(b.ClientKeyReference)
	if e != nil {
		return nil, e
	}
	pair, e := tls.X509KeyPair(cert, []byte(key))
	if e != nil {
		return nil, errors.New("gateway private client identity invalid")
	}
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}
	cert, e = gatewayBoundBytes(b.ServerCertFile, b.ServerCertSHA256)
	if e != nil {
		return nil, e
	}
	key, e = secret(b.ServerKeyReference)
	if e != nil {
		return nil, e
	}
	serverPair, e := tls.X509KeyPair(cert, []byte(key))
	if e != nil {
		return nil, errors.New("gateway private server identity invalid")
	}
	runtime.serverTLS = privateListenerTLS(roots, serverPair)
	signing, e := secret(b.AssertionKeyReference)
	if e != nil {
		return nil, e
	}
	signingKey, e := base64.RawURLEncoding.Strict().DecodeString(signing)
	if e != nil || len(signingKey) != ed25519.PrivateKeySize {
		return nil, errors.New("gateway assertion signing key invalid")
	}
	authorityTransport := &http.Transport{TLSClientConfig: clientTLS.Clone(), Proxy: nil, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 2 * time.Second}
	identityTransport := sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}
	authority, e := sharedauth.NewMTLSIdentityAuthority(b.IdentityEndpoint, b.IdentityServerPeerCertSHA256, &http.Client{Transport: authorityTransport, Timeout: 2 * time.Second}, sharedauth.ServiceRequestIssuer{Issuer: "gateway", Audience: "identity", Key: ed25519.PrivateKey(signingKey), Transport: identityTransport})
	if e != nil {
		return nil, e
	}
	ownPin := fmt.Sprintf("%x", sha256.Sum256(pair.Certificate[0]))
	runtime.identityVerifiers, e = identityServiceVerifiers(b.IdentityPeers, ownPin, ed25519.PrivateKey(signingKey).Public().(ed25519.PublicKey))
	if e != nil {
		return nil, e
	}

	seen := map[string]bool{}
	for _, upstream := range b.Upstreams {
		if upstream.Name == "user" {
			existing := map[string]bool{}
			for _, route := range upstream.Routes {
				existing[route.Method+" "+route.Template] = true
			}
			for _, route := range proxy.UserConnectedRoutes() {
				if !existing[route.Method+" "+route.Template] {
					upstream.Routes = append(upstream.Routes, route)
				}
			}
		}
		if upstream.Name == "metadata" {
			existing := map[string]bool{}
			for _, route := range upstream.Routes {
				existing[route.Method+" "+route.Template] = true
			}
			for _, route := range proxy.MetadataConnectedRoutes() {
				if !existing[route.Method+" "+route.Template] {
					upstream.Routes = append(upstream.Routes, route)
				}
			}
		}
		if upstream.Name == "livestream" {
			existing := map[string]bool{}
			for _, route := range upstream.Routes {
				existing[route.Method+" "+route.Template] = true
			}
			for _, route := range proxy.LivestreamConnectedRoutes() {
				if !existing[route.Method+" "+route.Template] {
					upstream.Routes = append(upstream.Routes, route)
				}
			}
		}
		if upstream.Name == "social" {
			existing := map[string]bool{}
			for _, route := range upstream.Routes {
				existing[route.Method+" "+route.Template] = true
			}
			for _, route := range proxy.SocialConnectedRoutes() {
				if !existing[route.Method+" "+route.Template] {
					upstream.Routes = append(upstream.Routes, route)
				}
			}
			runtime.socket, e = proxy.NewConnectedWS(proxy.ConnectedWSDeps{URL: upstream.URL, Identity: authority, SigningKey: ed25519.PrivateKey(signingKey), TLS: clientTLS, Origins: strings.Split(cfg.CORSAllowedOrigins, ",")})
			if e != nil {
				return nil, e
			}
		}
		if seen[upstream.Name] {
			return nil, errors.New("gateway duplicate upstream binding")
		}
		seen[upstream.Name] = true
		handler, e := proxy.NewConnected(proxy.ConnectedDeps{Name: upstream.Name, URL: upstream.URL, Routes: upstream.Routes, Identity: authority, SigningKey: ed25519.PrivateKey(signingKey), TLS: clientTLS})
		if e != nil {
			return nil, e
		}
		var uploadHandler http.Handler
		for _, route := range upstream.Routes {
			if route.Upload && route.Method == http.MethodPut {
				admission, e := upload.NewAdmission(rdb, b.UploadNamespace, b.UploadConcurrent)
				if e != nil {
					return nil, e
				}
				uploadHandler, e = upload.NewConnected(upload.ConnectedDeps{Upstream: handler, Identity: authority, Admission: admission, IngressBytesPerSecond: b.UploadBytesPerSecond})
				if e != nil {
					return nil, e
				}
				break
			}
		}
		runtime.upstreams = append(runtime.upstreams, gatewayUpstream{routes: upstream.Routes, handler: handler, upload: uploadHandler})
	}
	mediaAuthority, e := mediastore.NewConnectedAuthority(pool, b.MediaSchema, b.MetadataSchema, b.SafetySchema)
	if e != nil {
		return nil, e
	}
	runtime.approved, e = serve.NewConnectedApproved(serve.Deps{Buckets: readers, Cache: objectCache, CacheMaxSize: cfg.ObjectCacheMaxBytes, Logger: logger}, mediaAuthority)
	if e != nil {
		return nil, e
	}
	purgeProvider, e := gatewayPurgeProvider(b, mediaAuthority, objectCache, clientTLS, secret)
	if e != nil {
		return nil, e
	}
	runtime.purge = mediastore.PurgeWorker{Authority: mediaAuthority, Provider: purgeProvider}
	runtime.mediaAuthority = mediaAuthority
	runtime.objectCache = objectCache
	if e = mediaAuthority.ReplayTombstones(ctx); e != nil {
		return nil, e
	}
	if e = runtime.configureControl(ctx, cfg, pool, controlPool, authority, clientTLS, secret); e != nil {
		return nil, e
	}
	if e = runtime.configureObjects(ctx, cfg, secret); e != nil {
		return nil, e
	}
	guard, e := queue.NewEpochGuard(b.RecoverySchema, b.RecoveryEpoch, b.RecoveryRegion)
	if e != nil {
		return nil, e
	}
	runtime.store, e = delivery.NewStoreWithEpochGuard(pool, "users", delivery.Policy{Subjects: map[string]bool{auth.IdentityRevisionSubject: true}, EventTypes: map[string]bool{auth.IdentityRevisionEventType: true}}, guard.Assert)
	if e != nil {
		return nil, e
	}
	natsURL, e := url.Parse(b.NATSURL)
	if e != nil || natsURL.Scheme != "tls" || natsURL.Host == "" || natsURL.User != nil || natsURL.Path != "" || natsURL.RawQuery != "" || natsURL.Fragment != "" {
		return nil, errors.New("gateway private TLS broker required")
	}
	credentials, e := secret(b.NATSCredentialsReference)
	if e != nil {
		return nil, e
	}
	runtime.nc, e = nats.Connect(b.NATSURL, nats.UserCredentialBytes([]byte(credentials)), nats.Secure(clientTLS.Clone()), nats.Timeout(3*time.Second), nats.MaxReconnects(3), nats.ReconnectWait(time.Second), nats.Name("gateway-"+cfg.RuntimeInstanceID), nats.DisconnectErrHandler(func(_ *nats.Conn, e error) { runtime.fail(errors.New("identity durable broker disconnected")) }))
	if e != nil {
		return nil, e
	}
	runtime.publisher, e = natsdriver.NewPublisher(runtime.nc, b.Stream)
	if e != nil {
		return nil, e
	}
	runtime.consumer, e = natsdriver.NewConsumer(ctx, runtime.nc, natsdriver.ConsumerConfig{Stream: b.Stream, Durable: b.Durable, Subject: auth.IdentityRevisionSubject, Batch: 32, MaxAckPending: 128, MaxWaiting: 4, MaxDeliver: 10, FetchWait: 500 * time.Millisecond, AckWait: 30 * time.Second})
	if e != nil {
		return nil, e
	}
	audit := b.Audit
	if audit.DeliverySchema == "users" || audit.DeliverySchema == audit.SinkDeliverySchema || audit.SinkSchema == "users" || audit.Subject == auth.IdentityRevisionSubject || audit.EventType == auth.IdentityRevisionEventType || audit.Durable == b.Durable {
		return nil, errors.New("distinct security audit authority required")
	}
	auditSecret, e := secret(audit.KeyReference)
	if e != nil {
		return nil, e
	}
	runtime.auditKey = []byte(auditSecret)
	if len(runtime.auditKey) < 32 {
		return nil, errors.New("audit privacy key invalid")
	}
	auditPolicy := delivery.Policy{Subjects: map[string]bool{audit.Subject: true}, EventTypes: map[string]bool{audit.EventType: true}}
	sourceStore, e := delivery.NewStoreWithEpochGuard(pool, audit.DeliverySchema, auditPolicy, guard.Assert)
	if e != nil {
		return nil, e
	}
	sinkStore, e := delivery.NewStoreWithEpochGuard(pool, audit.SinkDeliverySchema, auditPolicy, guard.Assert)
	if e != nil {
		return nil, e
	}
	if audit.SinkSchema == "" || audit.Subject == "" || audit.EventType == "" {
		return nil, errors.New("audit sink authority required")
	}
	runtime.auditBridge = sharedauth.AuditBridge{Pool: pool, Delivery: sourceStore, Subject: audit.Subject, EventType: audit.EventType}
	runtime.auditSink = sharedauth.AuditSink{Delivery: sinkStore, Consumer: audit.Durable, Generation: b.ConsumerGeneration, Subject: audit.Subject, EventType: audit.EventType, SinkSchema: audit.SinkSchema}
	runtime.auditPublisher, e = natsdriver.NewPublisher(runtime.nc, audit.Stream)
	if e != nil {
		return nil, e
	}
	runtime.auditConsumer, e = natsdriver.NewConsumer(ctx, runtime.nc, natsdriver.ConsumerConfig{Stream: audit.Stream, Durable: audit.Durable, Subject: audit.Subject, Batch: 32, MaxAckPending: 128, MaxWaiting: 4, MaxDeliver: 10, FetchWait: 500 * time.Millisecond, AckWait: 30 * time.Second})
	if e != nil {
		return nil, e
	}
	owned = true
	return runtime, nil
}
func (r *gatewayConnectedRuntime) fail(e error) {
	if e != nil {
		r.failure.CompareAndSwap(nil, &e)
	}
}
func (r *gatewayConnectedRuntime) ready(ctx context.Context) error {
	if failure := r.failure.Load(); failure != nil {
		return *failure
	}
	if e := r.control.Ready(ctx); e != nil {
		return e
	}
	if r.nc == nil || !r.nc.IsConnected() {
		return errors.New("identity broker unavailable")
	}
	for reference, consumed := range r.secrets {
		value, e := r.provider.GetRequired(ctx, reference)
		if e != nil || value != consumed {
			return errors.New("gateway runtime secret changed or unavailable")
		}
	}
	return nil
}
func (r *gatewayConnectedRuntime) start(identity *auth.Identity) {
	r.wg.Add(8)
	go func() {
		defer r.wg.Done()
		if e := r.control.Run(r.ctx); e != nil && r.ctx.Err() == nil {
			r.fail(e)
		}
	}()
	go func() { defer r.wg.Done(); r.runRestrictionIntake() }()
	go func() { defer r.wg.Done(); r.runObjectWorkers() }()
	go func() {
		defer r.wg.Done()
		e := r.consumer.Run(r.ctx, func(ctx context.Context, message *natsdriver.Message) error {
			return identity.ConsumeIdentityRevision(ctx, r.binding.Durable, r.binding.ConsumerGeneration, message.Subject, message.Wire, message)
		})
		if r.ctx.Err() == nil {
			r.fail(e)
		}
	}()
	go func() {
		defer r.wg.Done()
		e := r.auditConsumer.Run(r.ctx, func(ctx context.Context, message *natsdriver.Message) error {
			return r.auditSink.Consume(ctx, message.Wire, message)
		})
		if r.ctx.Err() == nil {
			r.fail(e)
		}
	}()
	auditRelay := delivery.Relay{Store: r.auditBridge.Delivery, Publisher: r.auditPublisher, Owner: r.binding.Audit.Durable, Batch: 32, LeaseDuration: 15 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 10}
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				if _, e := r.auditBridge.Stage(r.ctx, 32); e != nil {
					r.fail(e)
					return
				}
				if e := auditRelay.RunOnce(r.ctx); e != nil {
					r.fail(e)
					return
				}
			}
		}
	}()
	relay := delivery.Relay{Store: r.store, Publisher: r.publisher, Owner: r.binding.Durable, Batch: 32, LeaseDuration: 15 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 10}
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				if e := relay.RunOnce(r.ctx); e != nil && r.ctx.Err() == nil {
					r.fail(e)
					return
				}
			}
		}
	}()
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				if _, e := r.purge.RunOne(r.ctx); e != nil && r.ctx.Err() == nil {
					r.purgeErrors.Add(1)
					if r.logger != nil {
						r.logger.Warn("media purge pending", slog.String("reason", "provider_or_authority_unavailable"))
					}
				}
				probe, cancel := context.WithTimeout(r.ctx, 2*time.Second)
				age, e := r.mediaAuthority.PurgeBacklogAge(probe)
				cancel()
				if e == nil {
					previous := r.purgeAge.Swap(age.Milliseconds())
					if previous <= 30000 && age > 30*time.Second && r.logger != nil {
						r.logger.Warn("media purge backlog alert", slog.Int64("age_seconds", int64(age.Seconds())))
					}
				}
			}
		}
	}()
}
func (r *gatewayConnectedRuntime) close() {
	r.stop()
	if r.consumer != nil {
		r.consumer.Close()
	}
	if r.auditConsumer != nil {
		r.auditConsumer.Close()
	}
	r.wg.Wait()
	if r.nc != nil {
		r.nc.Close()
	}
}

func publicGatewayHandler(next http.Handler, cfg config.Config) http.Handler {
	origins := map[string]bool{}
	for _, origin := range strings.Split(cfg.CORSAllowedOrigins, ",") {
		origins[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL == nil || strings.HasPrefix(r.URL.Path, "/internal/") && !developmentInternalIdentity(cfg, r) {
			http.NotFound(w, r)
			return
		}
		if cfg.NodeEnv == "production" {
			if origin := r.Header.Get("Origin"); origin != "" {
				if !origins[origin] {
					sharedhttp.WriteJSON(w, http.StatusForbidden, map[string]string{"code": "SCOPE_DENIED"})
					return
				}
				w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token, X-Request-ID, ETag, Retry-After, X-Recovery-Receipt")
			}
		}
		next.ServeHTTP(w, r)
	})
}

type identityRuntimeMetrics struct {
	mu     sync.Mutex
	counts map[string]uint64
}

func newIdentityRuntimeMetrics() *identityRuntimeMetrics {
	counts := map[string]uint64{}
	for _, name := range []string{"issued", "verified", "consumed", "unknown_or_used", "challenge_expired", "challenge_domain", "challenge_uri", "challenge_wallet", "challenge_purpose", "challenge_binding", "challenge_digest", "throttled", "refresh_reuse", "ticket_issued", "ticket_consumed", "unknown_kid", "key_not_active", "key_retired", "claims_rejected", "key_dependency_unavailable", "recovery_guidance", "last_wallet_unlink_denied", "private_socket_close_failed", "private_socket_authority_closed", "saturated", "AUTH_EXPIRED", "SCOPE_DENIED", "DEPENDENCY_UNAVAILABLE", "FIELD_INVALID", "STATE_CONFLICT", "FIELD_UNKNOWN"} {
		counts[name] = 0
	}
	return &identityRuntimeMetrics{counts: counts}
}
func (m *identityRuntimeMetrics) observe(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, allowed := m.counts[name]; allowed {
		m.counts[name]++
	}
}
func (m *identityRuntimeMetrics) write(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.counts))
	for name := range m.counts {
		names = append(names, name)
	}
	sort.Strings(names)
	_, _ = fmt.Fprintln(w, "# TYPE identity_outcomes_total counter")
	for _, name := range names {
		_, _ = fmt.Fprintf(w, "identity_outcomes_total{outcome=%q} %d\n", name, m.counts[name])
	}
}

type gatewayControlBinding struct {
	Schema                      string `json:"schema"`
	AuthoritySchema             string `json:"authoritySchema"`
	IncidentCredentialReference string `json:"incidentCredentialReference"`
	RevocationEndpoint          string `json:"revocationEndpoint"`
	RevocationTokenReference    string `json:"revocationTokenReference"`
}
type gatewayObjectWorkerBinding struct {
	Kind                        string `json:"kind"`
	Bucket                      string `json:"bucket"`
	Prefix                      string `json:"prefix"`
	DeleteAccessKeyReference    string `json:"deleteAccessKeyReference"`
	DeleteSecretKeyReference    string `json:"deleteSecretKeyReference"`
	InventoryAccessKeyReference string `json:"inventoryAccessKeyReference"`
	InventorySecretKeyReference string `json:"inventorySecretKeyReference"`
	InventoryLimit              int    `json:"inventoryLimit"`
}
type gatewayObjectWorker struct {
	binding     gatewayObjectWorkerBinding
	deleteStore *storage.Client
	inventory   *s3.Client
}

func (r *gatewayConnectedRuntime) configureControl(ctx context.Context, cfg config.Config, identityPool, controlPool *pgxpool.Pool, authority proxy.RequestIdentityAuthority, clientTLS *tls.Config, secret func(string) (string, error)) error {
	binding := r.binding.Control
	if binding.Schema != "pnl" || binding.AuthoritySchema != "pnl" {
		return errors.New("exact control authority schemas required")
	}
	consumed, e := secret(r.binding.Pools.ControlCredentialReference)
	if e != nil {
		return e
	}
	_ = consumed
	configuration := control.RuntimeConfig{ControlPool: controlPool, IdentityPool: identityPool, ControlSchema: binding.Schema, AuthoritySchema: binding.AuthoritySchema, Observe: func(_ string, e error) {
		if e != nil && r.logger != nil {
			r.logger.Warn("control worker pending", slog.String("reason", "control_dependency_or_authority_unavailable"))
		}
	}}
	if binding.IncidentCredentialReference != "" {
		configuration.IncidentCredential, e = secret(binding.IncidentCredentialReference)
		if e != nil {
			return e
		}
	}
	if binding.RevocationEndpoint != "" || binding.RevocationTokenReference != "" {
		token, e := secret(binding.RevocationTokenReference)
		if e != nil {
			return e
		}
		configuration.Revocation, e = control.NewRevocationProvider(binding.RevocationEndpoint, token, &http.Transport{TLSClientConfig: clientTLS.Clone(), Proxy: nil})
		if e != nil {
			return e
		}
	}
	r.control, e = control.NewRuntime(configuration)
	if e != nil {
		return e
	}
	if e = r.control.Ready(ctx); e != nil {
		return e
	}
	handler := r.control.Handler()
	r.controlHandler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		public := request.Method == http.MethodGet && (strings.HasPrefix(request.URL.Path, "/v2/control/caps/") || request.URL.Path == "/v2/control/settlement-capability")
		if !public {
			if len(request.Header.Values("Authorization")) > 1 {
				sharedhttp.WriteJSON(w, http.StatusBadRequest, map[string]string{"code": "FIELD_INVALID"})
				return
			}
			if request.Header.Get("Authorization") == "" {
				if _, e := authority.AuthenticateRequest(request.Context(), request); e != nil {
					sharedhttp.WriteJSON(w, http.StatusUnauthorized, map[string]string{"code": "AUTH_REQUIRED"})
					return
				}
			}
			copy := request.Clone(request.Context())
			copy.Header = request.Header.Clone()
			if copy.Header.Get("Authorization") == "" {
				if cookie, e := request.Cookie("__Host-kindle_access"); e == nil {
					copy.Header.Set("Authorization", "Bearer "+cookie.Value)
				}
			}
			for _, name := range []string{"capability", "policy", "key"} {
				copy.SetPathValue(name, chi.URLParam(request, name))
			}
			handler.ServeHTTP(w, copy)
			return
		}
		handler.ServeHTTP(w, request)
	})
	return nil
}
func gatewayControlRoutes() []proxy.ConnectedRoute {
	routes := []proxy.ConnectedRoute{{Method: http.MethodGet, Template: "/v2/control/caps/{capability}"}, {Method: http.MethodGet, Template: "/v2/control/campaigns/{policy}"}, {Method: http.MethodGet, Template: "/v2/control/recommendations/{key}"}, {Method: http.MethodGet, Template: "/v2/control/settlement-capability"}}
	for _, operation := range []string{"replays", "actions", "complete-action", "recover-incident", "bounty-submissions", "transition", "caps", "budget", "policies", "policy-state", "recommendations", "bounties", "bounty-state", "inventory", "access", "incidents", "settlements"} {
		routes = append(routes, proxy.ConnectedRoute{Method: http.MethodPost, Template: "/v2/control/" + operation, Authenticate: true})
	}
	return routes
}
func (r *gatewayConnectedRuntime) configureObjects(ctx context.Context, cfg config.Config, secret func(string) (string, error)) error {
	if len(r.binding.ObjectWorkers) < 1 || len(r.binding.ObjectWorkers) > 4 {
		return errors.New("explicit scoped object workers required")
	}
	seen := map[string]bool{}
	references := map[string]bool{"S3_ACCESS_KEY_ID": true, "S3_SECRET_ACCESS_KEY": true}
	for _, binding := range r.binding.ObjectWorkers {
		prefix := map[string]string{"variant": "approved/", "original": "quarantine/", "temporary": "temporary/", "recording": "private/"}[binding.Kind]
		if prefix == "" || binding.Prefix != prefix || binding.Bucket == "" || seen[binding.Kind] || binding.InventoryLimit < 1 || binding.InventoryLimit > 100000 {
			return errors.New("object worker authority binding invalid")
		}
		seen[binding.Kind] = true
		for _, ref := range []string{binding.DeleteAccessKeyReference, binding.DeleteSecretKeyReference, binding.InventoryAccessKeyReference, binding.InventorySecretKeyReference} {
			if ref == "" || references[ref] {
				return errors.New("distinct object IAM role references required")
			}
			references[ref] = true
		}
		deleteKey, e := secret(binding.DeleteAccessKeyReference)
		if e != nil {
			return e
		}
		deleteSecret, e := secret(binding.DeleteSecretKeyReference)
		if e != nil {
			return e
		}
		inventoryKey, e := secret(binding.InventoryAccessKeyReference)
		if e != nil {
			return e
		}
		inventorySecret, e := secret(binding.InventorySecretKeyReference)
		if e != nil {
			return e
		}
		if deleteKey == inventoryKey || deleteKey == cfg.S3AccessKeyID || inventoryKey == cfg.S3AccessKeyID {
			return errors.New("object IAM roles must be independently provisioned")
		}
		deletion, _, e := mediastore.NewScopedObjectClient(ctx, storage.Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: binding.Bucket, AccessKeyID: deleteKey, SecretAccessKey: deleteSecret})
		if e != nil {
			return e
		}
		_, inventory, e := mediastore.NewScopedObjectClient(ctx, storage.Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: binding.Bucket, AccessKeyID: inventoryKey, SecretAccessKey: inventorySecret})
		if e != nil {
			return e
		}
		r.objects = append(r.objects, gatewayObjectWorker{binding, deletion, inventory})
	}
	if !seen["variant"] || !seen["original"] || !seen["temporary"] {
		return errors.New("complete asset lineage stores required")
	}
	return nil
}
func (r *gatewayConnectedRuntime) runRestrictionIntake() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			_, e := r.mediaAuthority.IntakeRestrictions(r.ctx, func(ctx context.Context, key string) error {
				return r.objectCache.Evict(ctx, r.binding.ApprovedBucket, key)
			})
			if e != nil && r.ctx.Err() == nil && r.logger != nil {
				r.logger.Warn("media restriction intake pending", slog.String("reason", "restriction_dependency_or_integrity_failure"))
			}
		}
	}
}
func (r *gatewayConnectedRuntime) runObjectWorkers() {
	stores := map[string]*storage.Client{}
	for _, object := range r.objects {
		stores[object.binding.Kind] = object.deleteStore
	}
	worker := mediastore.DeleteWorker{Authority: r.mediaAuthority, Stores: stores}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextInventory := time.Time{}
	for {
		bounded, cancel := context.WithTimeout(r.ctx, 30*time.Second)
		_, deletionIntakeError := r.mediaAuthority.IntakeDeletedAssets(bounded)
		_, importError := r.mediaAuthority.ImportApprovedBatch(bounded)
		_, deleteError := worker.RunOne(bounded)
		_, retentionError := worker.ExpireOriginal(bounded)
		if (deletionIntakeError != nil || importError != nil || deleteError != nil || retentionError != nil) && r.ctx.Err() == nil && r.logger != nil {
			r.logger.Warn("media object deletion pending", slog.String("reason", "object_dependency_or_retention_authority_failure"))
		}
		if !time.Now().Before(nextInventory) {
			for _, object := range r.objects {
				report, e := r.mediaAuthority.ReconcileInventory(bounded, object.inventory, object.binding.Bucket, object.binding.Prefix, object.binding.InventoryLimit)
				if e == nil {
					e = r.mediaAuthority.PersistInventory(bounded, object.binding.Bucket, object.binding.Prefix, report)
				}
				if e != nil && r.ctx.Err() == nil && r.logger != nil {
					r.logger.Warn("media inventory pending", slog.String("reason", "inventory_dependency_unavailable"))
				}
			}
			nextInventory = time.Now().Add(time.Hour)
		}
		cancel()
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *gatewayConnectedRuntime) writeMediaMetrics(w http.ResponseWriter) {
	age := r.purgeAge.Load()
	alert := 0
	if age > 30000 {
		alert = 1
	}
	_, _ = fmt.Fprintln(w, "# TYPE media_purge_backlog_age_seconds gauge")
	_, _ = fmt.Fprintf(w, "media_purge_backlog_age_seconds %.3f\n", float64(age)/1000)
	_, _ = fmt.Fprintln(w, "# TYPE media_purge_backlog_alert gauge")
	_, _ = fmt.Fprintf(w, "media_purge_backlog_alert %d\n", alert)
	_, _ = fmt.Fprintln(w, "# TYPE media_purge_provider_errors_total counter")
	_, _ = fmt.Fprintf(w, "media_purge_provider_errors_total %d\n", r.purgeErrors.Load())
}

// gatewayPurgeProvider builds the approved-media purge provider the runtime
// binding selects: the CDN purge API ("http", the default) or, when no CDN
// fronts the gateway, eviction of the gateway's own approved-object cache
// ("origin_cache").
func gatewayPurgeProvider(b gatewayRuntimeBinding, authority *mediastore.Authority, objects *cache.Cache, clientTLS *tls.Config, secret func(string) (string, error)) (mediastore.PurgeProvider, error) {
	switch b.PurgeMode {
	case "", "http":
		token, e := secret(b.PurgeTokenReference)
		if e != nil {
			return nil, e
		}
		provider, e := mediastore.NewHTTPPurgeProvider(b.PurgeEndpoint, token, &http.Transport{TLSClientConfig: clientTLS.Clone(), Proxy: nil})
		if e != nil {
			return nil, e
		}
		return provider, nil
	case "origin_cache":
		if b.PurgeEndpoint != "" || b.PurgeTokenReference != "" {
			return nil, errors.New("origin cache purge takes no CDN purge endpoint")
		}
		if objects == nil || b.ApprovedBucket == "" {
			return nil, errors.New("origin cache purge requires the approved object cache")
		}
		bucket := b.ApprovedBucket
		provider, e := mediastore.NewOriginCachePurgeProvider(authority, func(ctx context.Context, key string) error {
			return objects.Evict(ctx, bucket, key)
		})
		if e != nil {
			return nil, e
		}
		return provider, nil
	default:
		return nil, errors.New("gateway purge mode invalid")
	}
}
