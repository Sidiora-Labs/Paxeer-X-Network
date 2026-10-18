// Package app wires the media/social service together (config, migrations,
// pgxpool, Redis pub/sub, HTTP+WS router, health, graceful shutdown) and owns
// its lifecycle. Keeping the wiring here (rather than in main) makes the whole
// service testable end-to-end against external test servers.
package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/notifications"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/devsocial"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/voice"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/social"
)

// rateLimitMax / rateLimitWindow mirror the TS registerRateLimit(app, {max:60,
// windowSeconds:60}) configuration for the chat service's REST surface.
const (
	rateLimitMax    = 60
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-serve social service.
type App struct {
	platform *platformIdentity
	private  *platformPrivate
	// PrivateRouter is the handler of the platform runtime's private mTLS
	// listener (nil for the other runtimes).
	PrivateRouter http.Handler
	connected     *socialConnectedRuntime
	dev           *devsocial.Runtime
	serverTLS     *tls.Config
	secretStop    context.CancelFunc
	tracker       *process.Tracker
	Router        http.Handler
	Hub           *fanout.Hub

	pool   *pgxpool.Pool
	redis  *goredis.Client
	sub    *goredis.Client
	logger *slog.Logger
}

// New runs migrations, opens the DB + Redis (publisher + dedicated subscriber)
// pools, and builds the HTTP+WS router with health endpoints, the rate-limited
// REST routes, and the realtime /ws endpoint.
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
		metrics, e = telemetry.New("sociald", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if e != nil {
			return nil, e
		}
	}
	secretChecks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		secretChecks["secrets"] = secretCheck
	}

	platform := cfg.Platform()
	if cfg.NodeEnv != "production" {
		// Outside production the embedded migrations are applied at boot. In
		// production the migration coordinator owns the schema
		// (spec/production-system/integration/migrations): the runtime never
		// migrates and OpenRuntimePool verifies the coordinator's
		// ExpectedSchema document before serving.
		if err := migrate.Up(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}
	if cfg.NodeEnv != "production" || platform {
		// The v2 stores address their tables unqualified in the "chat" schema
		// (as the bound runtime's pool does), so the pool resolves it first.
		dsn, err := devsocial.RuntimeDSN(cfg.DatabaseURL)
		if err != nil {
			return nil, err
		}
		cfg.DatabaseURL = dsn
	}

	pool, err := cfg.OpenRuntimePool(ctx, "sociald", shareddb.PoolOptions{})
	if err != nil {
		return nil, err
	}

	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, err
	}
	sub, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		pool.Close()
		_ = rdb.Close()
		return nil, err
	}

	var connected *socialConnectedRuntime
	var identity *platformIdentity
	var private *platformPrivate
	if platform {
		identity, err = newPlatformIdentity(cfg)
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
		secretChecks["identity_authority"] = identity.ready
		private, err = newPlatformPrivate(cfg, pool)
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
	} else if cfg.NodeEnv == "production" {
		connected, err = newSocialConnectedRuntime(ctx, cfg, pool, rdb, logger)
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
		secretChecks["social_runtime"] = connected.ready
	}
	runtimeOwned := false
	defer func() {
		if connected != nil && !runtimeOwned {
			connected.close()
		}
	}()
	queries := sqlcdb.New(pool)

	socialDeps := social.Deps{
		Queries:          queries,
		MaxMessageLength: cfg.MaxMessageLength,
		AdminAPIKey:      cfg.ChatAdminAPIKey,
		Logger:           logger,
	}
	if platform {
		// Pool creators come from the production indexer's canonical
		// creations interface, the indexer relation chat_runtime is granted.
		socialDeps.PoolCreator = func(c context.Context, address string) (string, error) {
			_, creator, found, e := devsocial.LookupPool(c, pool, devsocial.CanonicalPools, address)
			if e == nil && !found {
				e = pgx.ErrNoRows
			}
			return creator, e
		}
	}
	handlers := social.New(socialDeps)

	hubDeps := fanout.Deps{
		Queries:          queries,
		Pub:              rdb,
		Sub:              sub,
		MaxMessageLength: cfg.MaxMessageLength,
		RateWindow:       time.Duration(cfg.RateWindowSeconds) * time.Second,
		MaxPoolMsgs:      cfg.MaxPoolMsgsPerWin,
		MaxDmMsgs:        cfg.MaxDmMsgsPerWindow,
		SendBuffer:       cfg.WSSendBuffer,
		WriteWait:        time.Duration(cfg.WSWriteTimeoutSecs) * time.Second,
		ReadLimit:        cfg.WSReadLimitBytes,
		Logger:           logger,
	}
	var hub *fanout.Hub
	var dev *devsocial.Runtime
	if connected != nil {
		hubDeps.Durable = connected.chat
		hubDeps.Authenticate = connected.ticket
		hub, err = fanout.NewConnected(hubDeps)
		if err != nil {
			return nil, err
		}
	} else if identity != nil {
		// Production platform runtime: the durable chat store over the
		// coordinator-owned chat tables, identity verified at the gateway identity
		// authority, and the v2 chat stream protocol (sidiora.v2 subprotocol,
		// single-use topic-bound ticket, session re-checked while connected).
		key, keyErr := cfg.PlatformCursorKey()
		if keyErr != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, keyErr
		}
		dev, err = devsocial.New(ctx, devsocial.Deps{Pool: pool, Redis: rdb, Logger: logger, Authenticate: contextPrincipal, CursorKey: key, Pools: devsocial.CanonicalPools})
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
		dev.Chat.Projection = platformMessageProjection(pool)
		hubDeps.Durable = dev.Chat
		hubDeps.Authenticate = identity.ticket
		hubDeps.ProtocolVersion = 2
		hub = fanout.New(hubDeps)
	} else {
		// Development: the realtime hub persists through the durable chat
		// store and admits the gateway-attested actor (X-Actor-Wallet).
		dev, err = devsocial.New(ctx, devsocial.Deps{Pool: pool, Redis: rdb, Logger: logger})
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
		hubDeps.Durable = dev.Chat
		hubDeps.Authenticate = devsocial.Authenticate
		hub = fanout.New(hubDeps)
	}

	mux := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins: cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})

	// Realtime WS endpoint: its own per-message rate limiting lives in the hub,
	// so it is not behind the REST token bucket.
	mux.Get("/ws", hub.ServeWS)
	if identity != nil {
		mux.Get("/v2/stream", hub.ServeWS)
	}
	if connected != nil {
		mux.Get("/v2/stream", hub.ServeWS)
		if err = fanout.RegisterConnectedRoutes(mux, connected.chat, hub, connected.request); err != nil {
			return nil, err
		}
		mux.Mount("/v2/social", http.StripPrefix("/v2/social", connected.interactions.Routes()))
		connected.interactions.RegisterContractRoutes(mux)
		mux.Group(func(gr chi.Router) {
			gr.Use(connected.safetyMiddleware)
			social.RegisterSafetyRoutes(gr, connected.safety, connected.voice)
		})
	}

	if dev != nil {
		// The v2 social surface the gateway forwards. Like the bound
		// production v2 routes it sits outside the legacy per-peer token
		// bucket (every request arrives from the gateway's address).
		if err = dev.Register(mux); err != nil {
			pool.Close()
			_ = rdb.Close()
			_ = sub.Close()
			return nil, err
		}
	}

	var platformSafety *safety.Store
	if identity != nil {
		// Moderation: reports, cases, bans, appeals and voice-room control over
		// the safety tables (coordinator migrations 380000/380001), with the moderation principal
		// resolved from the verified session's moderator grant. Voice rooms
		// have no media provider in this runtime, so provider-bound voice
		// actions answer unavailable.
		platformSafety = &safety.Store{DB: pool, Targets: safety.VoiceTargets{DB: pool, Next: socialTargetResolver{pool: pool}}}
		control := &voice.Control{DB: pool, Policy: platformSafety, Pools: platformPools{db: pool}}
		mux.Group(func(gr chi.Router) {
			gr.Use(identity.moderation(platformSafety, true))
			social.RegisterSafetyRoutes(gr, platformSafety, control)
		})
	}

	// Rate-limit the REST routes (health probes + WS stay un-throttled here, i12).
	mux.Group(func(gr chi.Router) {
		limiter := sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
			Max:    rateLimitMax,
			Window: rateLimitWindow,
			Redis:  rdb,
		})
		if identity != nil {
			limiter = gatewayIngressExempt(limiter)
		}
		gr.Use(limiter)
		if platformSafety != nil {
			// Legacy report and admin moderation routes act through the safety
			// store; CHAT_ADMIN_API_KEY stays an outer gate only.
			gr.Use(identity.moderation(platformSafety, false))
		}
		handlers.RegisterRoutes(gr)
		if connected == nil {
			handlers.RegisterContractRoutes(gr)
		}
	})

	var domainRouter http.Handler = mux
	if connected != nil {
		domainRouter = connected.ingress(mux)
	} else if identity != nil {
		domainRouter = identity.ingress(mux)
	}

	if metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	runtimeOwned = true
	var serverTLS *tls.Config
	if connected != nil {
		serverTLS = connected.serverTLS
	}
	result := &App{platform: identity, connected: connected, dev: dev, serverTLS: serverTLS, secretStop: secretStop, tracker: tracker, Router: runtimeHandler(tracker, domainRouter, metrics), Hub: hub, pool: pool, redis: rdb, sub: sub, logger: logger}
	if identity != nil {
		// Production platform runtime: the social API and the ranking
		// visibility authority are served on the private mTLS listener; the
		// plain listener keeps only health and metrics.
		result.private = private
		result.PrivateRouter = runtimeHandler(tracker, private.handler(domainRouter), metrics)
		result.Router = platformPublicOnly(result.Router)
	}
	return result, nil
}

// Close releases the DB and Redis pools.
func (a *App) Close() {
	if a.connected != nil {
		a.connected.close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	a.pool.Close()
	if err := a.redis.Close(); err != nil && a.logger != nil {
		a.logger.Warn("redis close", slog.String("err", err.Error()))
	}
	if err := a.sub.Close(); err != nil && a.logger != nil {
		a.logger.Warn("redis sub close", slog.String("err", err.Error()))
	}
}

// Run loads config, builds the App, serves HTTP+WS, runs the realtime hub, and
// blocks until ctx is cancelled or a signal arrives, then drains gracefully.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("social", cfg.LogLevel)
	logger.Info("starting social service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	if app.connected != nil {
		app.connected.start(app.Hub)
	} else {
		go func() {
			if err := app.Hub.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("hub run", slog.String("err", err.Error()))
				cancel()
			}
		}()
		if app.dev != nil {
			go func() { _ = app.dev.Run(ctx) }()
		}
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 2)
	var privateSrv *http.Server
	if app.private != nil {
		privateSrv = &http.Server{
			Addr:              app.private.address,
			Handler:           app.PrivateRouter,
			ConnState:         app.tracker.ConnState,
			ReadHeaderTimeout: 10 * time.Second,
			TLSConfig:         app.private.tls,
		}
		go func() {
			logger.Info("private mTLS listener", slog.String("address", app.private.address))
			listener, err := net.Listen("tcp", privateSrv.Addr)
			if err == nil {
				err = privateSrv.Serve(tls.NewListener(listener, app.private.tls))
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				serveErr <- err
				cancel()
			}
		}()
	}
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		var err error
		if app.serverTLS != nil {
			listener, e := net.Listen("tcp", srv.Addr)
			if e != nil {
				err = e
			} else {
				err = srv.Serve(tls.NewListener(listener, app.serverTLS))
			}
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
			if privateSrv != nil {
				if err := privateSrv.Shutdown(sctx); err != nil {
					return err
				}
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

type socialConnectedRuntime struct {
	binding                                       config.SocialRuntimeBinding
	ctx                                           context.Context
	stop                                          context.CancelFunc
	wg                                            sync.WaitGroup
	failure                                       atomic.Pointer[error]
	pool                                          *pgxpool.Pool
	identity                                      sharedauth.RequestIdentityAuthority
	chatIdentity                                  fanout.ChatTicketAuthority
	ticket                                        fanout.AuthenticateUpgrade
	request                                       fanout.AuthenticateUpgrade
	serverTLS                                     *tls.Config
	chat                                          *fanout.DurableStore
	interactions                                  *interactions.HTTP
	store                                         interactions.Store
	safety                                        *safety.Store
	voice                                         *voice.Control
	broker                                        fanout.SocialBroker
	nc                                            *nats.Conn
	chatConsumer, erasureConsumer, policyConsumer *natsdriver.Consumer
	delivery                                      *delivery.Store
	publisher                                     *natsdriver.Publisher
	notificationCredentials                       *notifications.Credentials
	notification                                  *notifications.ChatClient
	redis                                         *goredis.Client
	limiter                                       *safety.AbuseLimiter
	sourceOrder                                   interactions.CanonicalSourceOrder
	voiceFailure                                  error
	provider                                      sharedsecrets.Provider
	secrets                                       map[string]string
	logger                                        *slog.Logger
}

func socialBoundBytes(path, digest string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1048576 || len(digest) != 64 {
		return nil, errors.New("social bound file unavailable")
	}
	raw, e := os.ReadFile(path)
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return nil, errors.New("social bound file mismatch")
	}
	return raw, nil
}
func socialIdentifier(schema, table string) (pgx.Identifier, error) {
	for _, value := range []string{schema, table} {
		if len(value) < 1 || len(value) > 63 {
			return nil, errors.New("social SQL identifier unavailable")
		}
		for _, c := range value {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				return nil, errors.New("social SQL identifier invalid")
			}
		}
	}
	return pgx.Identifier{schema, table}, nil
}
func newSocialConnectedRuntime(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, redis *goredis.Client, logger *slog.Logger) (*socialConnectedRuntime, error) {
	lifecycle, stop := context.WithCancel(ctx)
	r := &socialConnectedRuntime{ctx: lifecycle, stop: stop, pool: pool, redis: redis, secrets: map[string]string{}, logger: logger}
	owned := false
	defer func() {
		if !owned {
			r.close()
		}
	}()
	if e := config.ReadBoundDocument(cfg.SocialRuntimeFile, cfg.SocialRuntimeSHA256, &r.binding); e != nil {
		return nil, e
	}
	b := r.binding
	if b.SocialSchema != "chat" || b.IdentitySchema != "users" || b.AccountSchema != "users" || b.IndexerSchema != "indexer" {
		return nil, errors.New("social registered owner schema binding mismatch")
	}
	if b.Version != 2 || b.GatewayDNS == "" || len(b.GatewayCertSHA256) != 64 || b.ConsumerGeneration == "" || b.PendingOutboxLimit < 1 || b.MaxOutboxAgeSeconds < 1 || b.MaxOutboxAgeSeconds > 86400 {
		return nil, errors.New("social runtime binding incomplete")
	}
	r.provider, _ = sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
	if r.provider == nil {
		return nil, errors.New("social secret authority unavailable")
	}
	secret := func(reference string) (string, error) {
		if reference == "" {
			return "", errors.New("social secret reference required")
		}
		value, e := r.provider.GetRequired(ctx, reference)
		if e != nil {
			return "", errors.New("social runtime secret unavailable")
		}
		r.secrets[reference] = value
		return value, nil
	}
	ca, e := socialBoundBytes(b.CAFile, b.CASHA256)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("social private CA invalid")
	}
	cert, e := socialBoundBytes(b.ServerCertFile, b.ServerCertSHA256)
	if e != nil {
		return nil, e
	}
	key, e := secret(b.ServerKeyReference)
	if e != nil {
		return nil, e
	}
	pair, e := tls.X509KeyPair(cert, []byte(key))
	if e != nil {
		return nil, e
	}
	r.serverTLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{pair}}
	cert, e = socialBoundBytes(b.ClientCertFile, b.ClientCertSHA256)
	if e != nil {
		return nil, e
	}
	key, e = secret(b.ClientKeyReference)
	if e != nil {
		return nil, e
	}
	pair, e = tls.X509KeyPair(cert, []byte(key))
	if e != nil {
		return nil, e
	}
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}
	endpoint, e := url.Parse(b.IdentityEndpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("social private identity endpoint required")
	}
	transport := &http.Transport{TLSClientConfig: clientTLS.Clone(), Proxy: nil, MaxIdleConns: 128, MaxIdleConnsPerHost: 32, ResponseHeaderTimeout: 2 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	signing, e := secret(b.IdentitySigningKeyReference)
	if e != nil {
		return nil, e
	}
	signingKey, e := base64.RawURLEncoding.Strict().DecodeString(signing)
	if e != nil || len(signingKey) != ed25519.PrivateKeySize || !bytes.Equal(signingKey, ed25519.NewKeyFromSeed(signingKey[:ed25519.SeedSize])) {
		return nil, errors.New("social identity signing key invalid")
	}
	signer := sharedauth.ServiceRequestIssuer{Issuer: "social", Audience: "identity", Key: ed25519.PrivateKey(signingKey), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
	identityAuthority, e := sharedauth.NewMTLSIdentityAuthority(b.IdentityEndpoint, b.IdentityServerPeerCertSHA256, client, signer)
	if e != nil {
		return nil, e
	}
	r.identity = identityAuthority
	r.chatIdentity = identityAuthority
	r.request = r.authenticateRequest
	r.ticket = r.authenticateTicket
	guard, e := queue.NewEpochGuard(b.RecoverySchema, b.RecoveryEpoch, b.RecoveryRegion)
	if e != nil {
		return nil, e
	}
	subjects := map[string]bool{}
	events := map[string]bool{"account.social.erasure.v1": true, "social.policy.changed.v2": true, "social.mention.intent.v1": true}
	for typ, subject := range b.InteractionSubjects {
		if typ != "social.edge.changed" && typ != "social.comment.changed" && typ != "social.callout.changed" {
			return nil, errors.New("social event type invalid")
		}
		subjects[subject] = true
		events[typ] = true
	}
	if len(b.InteractionSubjects) != 3 || b.MentionSubject == "" || b.ErasureSubject == "" || b.PolicySubject == "" {
		return nil, errors.New("social subjects missing")
	}
	subjects[b.MentionSubject] = true
	subjects[b.ErasureSubject] = true
	subjects[b.PolicySubject] = true
	r.delivery, e = delivery.NewStoreWithEpochGuard(pool, b.DeliverySchema, delivery.Policy{Subjects: subjects, EventTypes: events}, guard.Assert)
	if e != nil {
		return nil, e
	}
	accounts, e := socialIdentifier(b.AccountSchema, "account_state")
	if e != nil {
		return nil, e
	}
	pools, e := socialIdentifier(b.IndexerSchema, "pools")
	if e != nil {
		return nil, e
	}
	sessions, e := socialIdentifier(b.IdentitySchema, "identity_sessions")
	if e != nil {
		return nil, e
	}
	identityAccounts, e := socialIdentifier(b.IdentitySchema, "identity_accounts")
	if e != nil {
		return nil, e
	}
	wallets, e := socialIdentifier(b.IdentitySchema, "identity_wallet_links")
	if e != nil {
		return nil, e
	}
	positions, e := socialIdentifier(b.IndexerSchema, "indexer_raw_logs_v1")
	if e != nil {
		return nil, e
	}
	sessionGuard, e := socialIdentifier(b.SocialSchema, "interaction_session_valid")
	if e != nil {
		return nil, e
	}
	accountGuard, e := socialIdentifier(b.SocialSchema, "interaction_account_active")
	if e != nil {
		return nil, e
	}
	poolGuard, e := socialIdentifier(b.SocialSchema, "interaction_pool_creator")
	if e != nil {
		return nil, e
	}
	policy := &interactions.PGPolicy{DB: pool, Accounts: accounts, Pools: pools, Sessions: sessions, IdentityAccounts: identityAccounts, WalletLinks: wallets, SessionGuard: sessionGuard, AccountGuard: accountGuard, PoolGuard: poolGuard, Publisher: &interactions.Publisher{Delivery: r.delivery, Subjects: b.InteractionSubjects, NotificationSubject: b.MentionSubject, Watermark: interactions.CanonicalSourceOrder{Positions: positions}.Read}}
	r.store = interactions.Store{DB: pool, Authority: policy}
	transactions, e := socialIdentifier(b.IndexerSchema, "indexer_raw_logs_v1")
	if e != nil {
		return nil, e
	}
	blocks, e := socialIdentifier(b.IndexerSchema, "indexer_block_completeness_v1")
	if e != nil {
		return nil, e
	}
	r.interactions, e = interactions.NewHTTP(r.store, policy, interactions.NewViewerCache(redis), interactions.CommittedReceiptVerifier{DB: pool, Transactions: transactions, Blocks: blocks})
	if e != nil {
		return nil, e
	}
	cursor, e := secret(b.CursorKeyReference)
	if e != nil {
		return nil, e
	}
	cursorKey, e := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if e != nil || len(cursorKey) < 32 {
		return nil, errors.New("social cursor key invalid")
	}
	targets, e := fanout.NewPostgresNotificationTargets(b.IdentitySchema)
	if e != nil {
		return nil, e
	}
	writer := fanout.NotificationIntentWriter{Targets: targets, MaximumRecipients: 1000}
	r.limiter = &safety.AbuseLimiter{Redis: redis, Namespace: "social:" + b.ConsumerGeneration}
	r.sourceOrder = interactions.CanonicalSourceOrder{Positions: positions}
	r.chat = &fanout.DurableStore{Pool: pool, Projection: r.projectMessage, ReadPolicy: policy.ReadChatPolicy, Policy: func(ctx context.Context, tx pgx.Tx, in fanout.SendInput) error {
		if e := policy.SendPolicy(ctx, tx, in); e != nil {
			return e
		}
		if in.ClientID == "" {
			return nil
		}
		kind := in.Kind
		if kind == "dm" {
			var exists bool
			if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_members m JOIN chat_conversations c ON c.id=m.conversation_id WHERE c.kind='dm' AND m.wallet=$1 AND EXISTS(SELECT 1 FROM chat_members peer WHERE peer.conversation_id=c.id AND peer.wallet=$2))`, in.Actor, in.Target).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				kind = "dm_new"
			}
		}
		return r.limiter.Take(ctx, in.Actor, kind, in.Target, 0)
	}, CursorKey: cursorKey, NotificationQueue: writer.Queue, PendingOutboxLimit: b.PendingOutboxLimit, MaxOutboxAge: time.Duration(b.MaxOutboxAgeSeconds) * time.Second}
	r.notificationCredentials, e = notifications.NewCredentials(ctx, sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider)}, b.NotificationTokenReference)
	if e != nil {
		return nil, e
	}
	r.notification, e = notifications.NewChatClient(b.NotificationEndpoint, client, r.notificationCredentials, b.NotificationTokenReference)
	if e != nil {
		return nil, e
	}
	r.safety = &safety.Store{DB: pool, Targets: safety.VoiceTargets{DB: pool, Next: socialTargetResolver{pool: pool}}}
	r.voice = &voice.Control{DB: pool, Policy: r.safety, Pools: socialPools{DB: pool, Guard: poolGuard}}
	if b.Voice != nil {
		r.voice, e = r.newVoice(ctx, *b.Voice, secret, poolGuard)
		if e != nil {
			return nil, e
		}
	} else {
		r.voiceFailure = voice.ErrUnavailable
	}
	brokerURL, e := url.Parse(b.NATSURL)
	if e != nil || brokerURL.Scheme != "tls" || brokerURL.Host == "" || brokerURL.User != nil || brokerURL.RawQuery != "" || brokerURL.Fragment != "" {
		return nil, errors.New("social private TLS broker required")
	}
	credentials, e := secret(b.NATSCredentialsReference)
	if e != nil {
		return nil, e
	}
	r.nc, e = nats.Connect(b.NATSURL, nats.UserCredentialBytes([]byte(credentials)), nats.Secure(clientTLS.Clone()), nats.Timeout(3*time.Second), nats.MaxReconnects(3), nats.ReconnectWait(time.Second), nats.Name("social-"+cfg.RuntimeInstanceID), nats.DisconnectErrHandler(func(_ *nats.Conn, e error) { r.fail(errors.New("social broker disconnected")) }))
	if e != nil {
		return nil, e
	}
	js, e := jetstream.New(r.nc)
	if e != nil {
		return nil, e
	}
	stream, e := js.Stream(ctx, b.ChatStream)
	if e != nil {
		return nil, e
	}
	info, e := stream.Info(ctx)
	if e != nil {
		return nil, e
	}
	if info.Config.Replicas != 3 || info.Config.Storage != jetstream.FileStorage || info.Config.MaxBytes < 1 || info.Config.MaxMsgs < 1 || info.Config.MaxAge <= 0 || info.Config.Discard != jetstream.DiscardNew {
		return nil, errors.New("social bounded R3 file stream required")
	}
	publisher, e := natsdriver.NewPublisher(r.nc, b.ChatStream)
	if e != nil {
		return nil, e
	}
	r.broker = fanout.SocialBroker{Publisher: publisher, Stream: b.ChatStream, Subject: b.ChatSubject}
	consumer := func(stream, durable, subject string) (*natsdriver.Consumer, error) {
		return natsdriver.NewConsumer(ctx, r.nc, natsdriver.ConsumerConfig{Stream: stream, Durable: durable, Subject: subject, Batch: 32, MaxAckPending: 128, MaxWaiting: 4, MaxDeliver: 10, FetchWait: 500 * time.Millisecond, AckWait: 30 * time.Second})
	}
	r.chatConsumer, e = consumer(b.ChatStream, b.ChatDurable, b.ChatSubject)
	if e != nil {
		return nil, e
	}
	r.erasureConsumer, e = consumer(b.ErasureStream, b.ErasureDurable, b.ErasureSubject)
	if e != nil {
		return nil, e
	}
	r.policyConsumer, e = consumer(b.PolicyStream, b.PolicyDurable, b.PolicySubject)
	if e != nil {
		return nil, e
	}
	r.publisher, e = natsdriver.NewPublisher(r.nc, b.EventStream)
	if e != nil {
		return nil, e
	}
	owned = true
	return r, nil
}
func (r *socialConnectedRuntime) authenticateRequest(req *http.Request) (fanout.Principal, error) {
	identity, e := r.identity.AuthenticateRequest(req.Context(), req)
	if e != nil {
		return fanout.Principal{}, e
	}
	return fanout.Principal{Wallet: identity.Wallet, SessionID: identity.SessionID, ExpiresAt: identity.AccessExpiresAt, Validate: func(ctx context.Context) error {
		return r.chatIdentity.CheckStreamSession(ctx, identity.StreamPrincipal())
	}}, nil
}
func (r *socialConnectedRuntime) authenticateTicket(req *http.Request) (fanout.Principal, error) {
	var ticket string
	for _, protocol := range websocket.Subprotocols(req) {
		if strings.HasPrefix(protocol, "ticket.") {
			if ticket != "" {
				return fanout.Principal{}, fanout.ErrInvalid
			}
			ticket = strings.TrimPrefix(protocol, "ticket.")
		}
	}
	topics := strings.Split(req.URL.Query().Get("topics"), ",")
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: req.Header.Get("Origin"), Endpoint: "/v2/stream"}
	principal, e := r.chatIdentity.ConsumeChatStreamTicket(req.Context(), ticket, binding)
	if e != nil {
		return fanout.Principal{}, e
	}
	identity, e := r.identity.AuthenticateRequest(req.Context(), req)
	if e != nil {
		return fanout.Principal{}, e
	}
	if identity.SessionID != principal.SessionID || identity.AccountID != principal.AccountID || identity.Wallet != principal.Wallet || identity.AuthEpoch != principal.AuthEpoch {
		return fanout.Principal{}, fanout.ErrNotFound
	}
	return fanout.Principal{Topics: append([]string(nil), topics...), Wallet: principal.Wallet, SessionID: principal.SessionID, ExpiresAt: identity.AccessExpiresAt, Validate: func(ctx context.Context) error {
		return r.chatIdentity.CheckStreamSession(ctx, principal)
	}}, nil
}
func (r *socialConnectedRuntime) safetyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.safety == nil {
			sharedhttp.WriteError(w, safety.HTTPStatus(safety.ErrUnavailable), "Moderation rejected", safety.ErrUnavailable.Error())
			return
		}
		identity, e := r.identity.AuthenticateRequest(req.Context(), req)
		if e != nil {
			sharedhttp.WriteError(w, safety.HTTPStatus(safety.ErrForbidden), "Moderation rejected", safety.ErrForbidden.Error())
			return
		}
		principal, e := r.safety.ResolvePrincipal(req.Context(), identity.Wallet, identity.AccountID, identity.SessionID, identity.AccessExpiresAt)
		if e != nil {
			sharedhttp.WriteError(w, safety.HTTPStatus(e), "Moderation rejected", e.Error())
			return
		}
		ctx := safety.WithPrincipal(social.WithModeration(req.Context(), r.safety), principal)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
func (r *socialConnectedRuntime) ingress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for _, name := range []string{"X-Actor-Wallet", "X-Actor-Role", "X-Roles", "X-Admin", "X-Admin-Key", "X-Admin-Role"} {
			req.Header.Del(name)
		}
		if req.URL.Path == "/health" || strings.HasPrefix(req.URL.Path, "/health/") || req.URL.Path == "/metrics" {
			next.ServeHTTP(w, req)
			return
		}
		if strings.HasPrefix(req.URL.Path, "/internal/v2/social/") {
			r.authorityHTTP(w, req)
			return
		}
		if req.TLS == nil || len(req.TLS.VerifiedChains) == 0 || len(req.TLS.PeerCertificates) == 0 || req.TLS.PeerCertificates[0].VerifyHostname(r.binding.GatewayDNS) != nil || fmt.Sprintf("%x", sha256.Sum256(req.TLS.PeerCertificates[0].Raw)) != r.binding.GatewayCertSHA256 {
			sharedhttp.WriteJSON(w, 403, map[string]string{"code": "TRUSTED_INGRESS_REQUIRED"})
			return
		}
		if req.URL.Path == "/v2/stream" || req.URL.Path == "/ws" {
			next.ServeHTTP(w, req)
			return
		}
		// The gateway signs an actor assertion only when it authenticated a
		// session; a viewer-aware contract read without one is anonymous.
		if req.Header.Get(sharedauth.ActorAssertionHeader) == "" && interactions.PublicContractRead(req) {
			next.ServeHTTP(w, req.WithContext(interactions.WithHTTP(req.Context(), r.interactions)))
			return
		}
		principal, e := r.request(req)
		if e != nil {
			sharedhttp.WriteJSON(w, 401, map[string]string{"code": "AUTH_EXPIRED"})
			return
		}
		req.Header.Set("X-Actor-Wallet", principal.Wallet)
		ctx := fanout.WithPrincipal(req.Context(), principal)
		ctx = interactions.WithIdentity(ctx, principal)
		ctx = interactions.WithHTTP(ctx, r.interactions)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
func (r *socialConnectedRuntime) fail(e error) {
	if e != nil {
		r.failure.CompareAndSwap(nil, &e)
	}
}
func (r *socialConnectedRuntime) ready(ctx context.Context) error {
	if failure := r.failure.Load(); failure != nil {
		return *failure
	}
	if r.voiceFailure != nil {
		return r.voiceFailure
	}
	if r.notificationCredentials != nil {
		if e := r.notificationCredentials.Ready(ctx); e != nil {
			return e
		}
	}
	if r.nc == nil || !r.nc.IsConnected() {
		return errors.New("social broker unavailable")
	}
	for reference, consumed := range r.secrets {
		value, e := r.provider.GetRequired(ctx, reference)
		if e != nil || value != consumed {
			return errors.New("social secret rotated or unavailable")
		}
	}
	return nil
}
func (r *socialConnectedRuntime) close() {
	r.stop()
	for _, consumer := range []*natsdriver.Consumer{r.chatConsumer, r.erasureConsumer, r.policyConsumer} {
		if consumer != nil {
			consumer.Close()
		}
	}
	r.wg.Wait()
	if r.nc != nil {
		r.nc.Close()
	}
}

type socialPools struct {
	DB    *pgxpool.Pool
	Guard pgx.Identifier
}

func (p socialPools) Exists(ctx context.Context, pool string) (bool, error) {
	if p.DB == nil || len(p.Guard) != 2 {
		return false, voice.ErrUnavailable
	}
	var creator *string
	e := p.DB.QueryRow(ctx, `SELECT `+p.Guard.Sanitize()+`($1::text)`, pool).Scan(&creator)
	return e == nil && creator != nil && *creator != "", e
}

type socialTargetResolver struct{ pool *pgxpool.Pool }

func (t socialTargetResolver) Resolve(ctx context.Context, kind, id string) (string, string, error) {
	if t.pool == nil {
		return "", "", safety.ErrUnavailable
	}
	var pool, actor string
	switch kind {
	case "message":
		e := t.pool.QueryRow(ctx, `SELECT CASE WHEN c.kind='pool' THEN substring(c.id from 6) ELSE '' END,m.sender FROM chat_messages m JOIN chat_conversations c ON c.id=m.conversation_id WHERE m.id=$1`, id).Scan(&pool, &actor)
		return pool, actor, e
	case "comment":
		e := t.pool.QueryRow(ctx, `SELECT pool,author FROM interaction_comments WHERE id=$1`, id).Scan(&pool, &actor)
		return pool, actor, e
	default:
		return "", "", safety.ErrUnavailable
	}
}

type voiceQualification struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	Endpoint  string    `json:"endpoint"`
	ExpiresAt time.Time `json:"expiresAt"`
	Gates     map[string]struct {
		Command   string `json:"command"`
		ExitCode  int    `json:"exitCode"`
		LogFile   string `json:"logFile"`
		LogSHA256 string `json:"logSHA256"`
	} `json:"gates"`
}

func (r *socialConnectedRuntime) newVoice(ctx context.Context, b config.VoiceRuntimeBinding, secret func(string) (string, error), poolGuard pgx.Identifier) (*voice.Control, error) {
	var proof voiceQualification
	if e := config.ReadBoundDocument(b.QualificationFile, b.QualificationSHA256, &proof); e != nil {
		return nil, voice.ErrUnavailable
	}
	if proof.Version != 1 || proof.Provider != "livekit" || proof.Endpoint != b.Endpoint || !proof.ExpiresAt.After(time.Now()) {
		return nil, voice.ErrUnavailable
	}
	for _, gate := range []string{"provider", "ejection", "acl", "retention"} {
		value, ok := proof.Gates[gate]
		if !ok || value.Command == "" || value.ExitCode != 0 {
			return nil, voice.ErrUnavailable
		}
		if _, e := socialBoundBytes(value.LogFile, value.LogSHA256); e != nil {
			return nil, voice.ErrUnavailable
		}
	}
	key, e := secret(b.APIKeyReference)
	if e != nil {
		return nil, e
	}
	apiSecret, e := secret(b.APISecretReference)
	if e != nil {
		return nil, e
	}
	var output voice.S3RecordingOutput
	var recordingStorage *storage.Client
	policy := voice.RecordingPolicy{Enabled: b.RecordingEnabled, Version: b.RecordingPolicyVersion, Purpose: b.RecordingPurpose, Retention: time.Duration(b.RecordingRetentionSeconds) * time.Second, RestrictedStorageQualified: proof.Gates["retention"].Command != ""}
	if b.RecordingEnabled {
		access, e := secret(b.RecordingAccessKeyReference)
		if e != nil {
			return nil, e
		}
		credential, e := secret(b.RecordingSecretReference)
		if e != nil {
			return nil, e
		}
		endpoint, e := url.Parse(b.RecordingEndpoint)
		if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return nil, voice.ErrUnavailable
		}
		output = voice.S3RecordingOutput{Endpoint: b.RecordingEndpoint, ForcePathStyle: true, Bucket: b.RecordingBucket, Region: b.RecordingRegion, AccessKey: access, Secret: credential}
		recordingStorage, e = storage.New(ctx, storage.Config{Endpoint: b.RecordingEndpoint, Region: b.RecordingRegion, AccessKeyID: access, SecretAccessKey: credential, Bucket: b.RecordingBucket})
		if e != nil {
			return nil, e
		}
	}
	provider, e := voice.NewLiveKit(b.Endpoint, key, apiSecret, output)
	if e != nil {
		return nil, e
	}
	return voice.NewProductionControl(r.pool, r.safety, socialPools{DB: r.pool, Guard: poolGuard}, provider, voice.Gates{ProviderQualified: true, EjectionQualified: true, ACLQualified: true, RetentionQualified: true}, b.PolicyVersion, policy, recordingStorage, b.RecordingBucket)
}
func (r *socialConnectedRuntime) worker(run func(context.Context) error) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if e := run(r.ctx); e != nil && r.ctx.Err() == nil {
			r.fail(e)
			if r.logger != nil {
				r.logger.Error("social worker stopped", slog.String("error", e.Error()))
			}
		}
	}()
}
func (r *socialConnectedRuntime) start(hub *fanout.Hub) {
	r.worker(func(ctx context.Context) error { return hub.RunConnected(ctx, r.chatConsumer, r.broker) })
	r.worker(r.notificationCredentials.Run)
	r.worker(func(ctx context.Context) error {
		return r.erasureConsumer.Run(ctx, func(ctx context.Context, m *natsdriver.Message) error {
			return r.store.ConsumeErasure(ctx, r.delivery, r.binding.ConsumerGeneration, r.binding.ErasureSubject, m.Wire, m)
		})
	})
	r.worker(func(ctx context.Context) error {
		return r.policyConsumer.Run(ctx, func(ctx context.Context, m *natsdriver.Message) error { return r.consumePolicy(ctx, m) })
	})
	if r.voice.Provider != nil {
		r.worker(func(ctx context.Context) error {
			return r.voice.RunReconciler(ctx, func(result voice.ReconcileResult, e error) {
				if e != nil || result.Orphans > 0 {
					if r.logger != nil {
						r.logger.Error("voice reconciliation requires attention", slog.Int("orphans", result.Orphans), slog.Any("error", e))
					}
				}
			})
		})
	}
	r.worker(func(ctx context.Context) error {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		relay := delivery.Relay{Store: r.delivery, Publisher: r.publisher, Owner: "social_" + r.binding.ConsumerGeneration, Batch: 32, LeaseDuration: 30 * time.Second, PublishTimeout: 4 * time.Second, RetryDelay: time.Second, MaxAttempts: 10}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
				_, a := r.chat.DrainNotificationIntents(bounded, 32, func(ctx context.Context, v fanout.NotificationIntent) error {
					_, _, e := r.notification.EnqueueChat(ctx, v.EventID, v.AccountID, v.ConversationID, v.CreatedAt)
					return e
				})
				b := r.stageSafetyOutbox(bounded)
				c := relay.RunOnce(bounded)
				d := r.eraseOnce(bounded)
				_, e := r.store.PruneReplay(bounded, 1000)
				cancel()
				if failure := errors.Join(a, b, c, d, e); failure != nil && ctx.Err() == nil {
					r.fail(failure)
					if r.logger != nil {
						r.logger.Error("social durable work pending", slog.String("error", failure.Error()))
					}
				}
			}
		}
	})
}

type socialPolicyEvent struct {
	Kind       string             `json:"kind"`
	EntityID   string             `json:"entityId"`
	Revision   int64              `json:"revision"`
	Visibility *safety.Visibility `json:"visibility,omitempty"`
}

func (r *socialConnectedRuntime) stageSafetyOutbox(ctx context.Context) error {
	for i := 0; i < 32; i++ {
		tx, e := r.pool.Begin(ctx)
		if e != nil {
			return e
		}
		var id, kind, entity string
		var revision int64
		var payload []byte
		var at time.Time
		e = tx.QueryRow(ctx, `SELECT id::text,kind,entity_id,revision,payload,created_at FROM safety_outbox WHERE published_at IS NULL ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &kind, &entity, &revision, &payload, &at)
		if errors.Is(e, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return nil
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return e
		}
		value := socialPolicyEvent{Kind: kind, EntityID: entity, Revision: revision}
		if kind == "visibility_changed" {
			var visibility safety.Visibility
			if e = json.Unmarshal(payload, &visibility); e == nil {
				value.Visibility = &visibility
			}
		}
		wire, marshalErr := json.Marshal(value)
		if e == nil {
			e = marshalErr
		}
		position, orderErr := r.sourceOrder.Read(ctx, tx)
		if e == nil {
			e = orderErr
		}
		if e == nil {
			eventID := "social_policy_" + strings.ReplaceAll(id, "-", "")
			e = r.delivery.Enqueue(ctx, tx, r.binding.PolicySubject, delivery.Envelope{EventID: eventID, EventType: "social.policy.changed.v2", SchemaVersion: 2, ChainID: 125, SourceOrder: position, EmittedAt: at.UTC(), TraceID: eventID, PayloadHash: delivery.Hash(wire), Payload: wire})
		}
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE safety_outbox SET published_at=clock_timestamp() WHERE id=$1`, id)
		}
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (r *socialConnectedRuntime) consumePolicy(ctx context.Context, m *natsdriver.Message) error {
	return r.delivery.Consume(ctx, "social_policy", r.binding.ConsumerGeneration, r.binding.PolicySubject, m.Wire, m, func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope) error {
		if envelope.EventType != "social.policy.changed.v2" {
			return safety.ErrInvalid
		}
		var v socialPolicyEvent
		if e := json.Unmarshal(envelope.Payload, &v); e != nil {
			return e
		}
		if v.EntityID == "" || v.Revision < 1 {
			return safety.ErrInvalid
		}
		if v.Kind == "policy_changed" {
			if r.voice.Provider != nil {
				if e := r.voice.ApplyPolicyUpdate(ctx, v.EntityID, v.Revision); e != nil {
					return e
				}
			}
			return nil
		}
		if v.Kind != "visibility_changed" {
			return nil
		}
		if v.Visibility == nil || v.Visibility.Revision != v.Revision {
			return safety.ErrInvalid
		}
		return r.applyVisibility(ctx, tx, *v.Visibility)
	})
}
func (r *socialConnectedRuntime) eraseOnce(ctx context.Context) error {
	rows, e := r.pool.Query(ctx, `SELECT deletion_case_id::text,wallet FROM interaction_erasure_jobs WHERE state<>'completed' AND (hold_until IS NULL OR hold_until<=clock_timestamp()) ORDER BY deletion_case_id LIMIT 32`)
	if e != nil {
		return e
	}
	type job struct{ id, wallet string }
	jobs := []job{}
	for rows.Next() {
		var v job
		if e = rows.Scan(&v.id, &v.wallet); e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range jobs {
		if r.voice.Provider == nil {
			var needs bool
			if e = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM voice_rooms r LEFT JOIN voice_participants p ON p.room_id=r.id WHERE r.state IN ('live','ending') AND (r.host_wallet=$1 OR p.wallet=$1))`, v.wallet).Scan(&needs); e != nil {
				return e
			}
			if needs {
				return voice.ErrUnavailable
			}
		} else if e = r.voice.DeleteAccount(ctx, v.wallet); e != nil {
			return e
		}
		if _, e = r.store.EraseBatch(ctx, v.id); e != nil {
			return e
		}
	}
	return nil
}
func (r *socialConnectedRuntime) applyVisibility(ctx context.Context, tx pgx.Tx, v safety.Visibility) error {
	if v.Status != "removed" && v.Status != "restored" {
		return safety.ErrInvalid
	}
	if v.Status == "restored" {
		return nil
	}
	switch v.TargetType {
	case "message":
		return r.chat.ModerateTx(ctx, tx, v.TargetID)
	case "comment":
		var actor, pool string
		var removed bool
		e := tx.QueryRow(ctx, `SELECT author,pool,removed FROM interaction_comments WHERE id=$1 FOR UPDATE`, v.TargetID).Scan(&actor, &pool, &removed)
		if e != nil {
			return e
		}
		if removed {
			return nil
		}
		if _, e = tx.Exec(ctx, `UPDATE interaction_comments SET body='',removed=true,revision=revision+1 WHERE id=$1`, v.TargetID); e != nil {
			return e
		}
		return r.store.RecordRemovalTx(ctx, tx, actor, pool, v.TargetID)
	default:
		return nil
	}
}

func (r *socialConnectedRuntime) authorityHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	allowed := false
	if req.TLS != nil && len(req.TLS.VerifiedChains) > 0 && len(req.TLS.PeerCertificates) > 0 {
		peer := req.TLS.PeerCertificates[0]
		for dns, digest := range r.binding.AuthorityPeers {
			if len(digest) == 64 && peer.VerifyHostname(dns) == nil && fmt.Sprintf("%x", sha256.Sum256(peer.Raw)) == digest {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		sharedhttp.WriteJSON(w, 403, map[string]string{"code": "SERVICE_AUTHORITY_REQUIRED"})
		return
	}
	path := strings.TrimPrefix(req.URL.Path, "/internal/v2/social/")
	var value any
	var e error
	switch {
	case req.Method == http.MethodGet && strings.HasPrefix(path, "graph/"):
		value, e = r.graphAuthority(req.Context(), strings.TrimPrefix(path, "graph/"))
	case req.Method == http.MethodGet && path == "content-visibility":
		value, e = r.contentVisibility(req.Context(), req.URL.Query().Get("targetType"), req.URL.Query().Get("targetId"))
	case req.Method == http.MethodGet && path == "ranking-visibility":
		value, e = r.rankingVisibility(req.Context())
	case req.Method == http.MethodGet && strings.HasPrefix(path, "visibility/"):
		parts := strings.Split(strings.TrimPrefix(path, "visibility/"), "/")
		if len(parts) != 2 {
			e = safety.ErrInvalid
		} else {
			value, e = r.visibilityAuthority(req.Context(), parts[0], parts[1], req.URL.Query().Get("accountId"))
		}
	case req.Method == http.MethodPost && path == "callouts:review":
		var input struct{ Actor, Pool, Body string }
		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16384))
		decoder.DisallowUnknownFields()
		e = decoder.Decode(&input)
		if e == nil {
			value, e = r.reviewCallout(req.Context(), input.Actor, input.Pool, input.Body)
		}
	case req.Method == http.MethodPost && path == "callout-authority":
		var input struct {
			TargetKind, TargetID, Actor string
			Revision                    int64
			Removed                     bool
			CreatedAt                   time.Time
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		decoder.DisallowUnknownFields()
		e = decoder.Decode(&input)
		if e == nil && input.TargetKind == "callout" && input.Revision > 0 && !input.CreatedAt.IsZero() {
			var wallet string
			e = r.pool.QueryRow(req.Context(), `SELECT chat.social_wallet_for_account($1::uuid)`, input.Actor).Scan(&wallet)
			if e == nil {
				var tx pgx.Tx
				tx, e = r.pool.Begin(req.Context())
				if e == nil {
					e = interactions.ApplyCalloutAuthorityTx(req.Context(), tx, input.TargetID, input.Revision, wallet, input.Removed)
					if e == nil {
						e = tx.Commit(req.Context())
					} else {
						_ = tx.Rollback(req.Context())
					}
				}
			}
		} else {
			e = safety.ErrInvalid
		}
		value = map[string]bool{"committed": e == nil}
	default:
		sharedhttp.WriteJSON(w, 404, map[string]string{"code": "NOT_FOUND"})
		return
	}
	if e != nil {
		sharedhttp.WriteJSON(w, 503, map[string]string{"code": "SOCIAL_AUTHORITY_UNAVAILABLE"})
		return
	}
	sharedhttp.WriteJSON(w, 200, value)
}
func (r *socialConnectedRuntime) graphAuthority(ctx context.Context, account string) (any, error) {
	if _, e := uuid.Parse(account); e != nil {
		return nil, e
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var wallet string
	if e = tx.QueryRow(ctx, `SELECT chat.social_wallet_for_account($1::uuid)`, account).Scan(&wallet); e != nil {
		return nil, e
	}
	var revision int64
	var observed time.Time
	if e = tx.QueryRow(ctx, `SELECT revision,transaction_timestamp() FROM interaction_graph_revisions WHERE wallet=$1 AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1)`, wallet).Scan(&revision, &observed); e != nil {
		return nil, e
	}
	followed, blocked := []string{}, []string{}
	rows, e := tx.Query(ctx, `SELECT chat.social_account_for_wallet(followee)::text,'follow' FROM interaction_follows WHERE follower=$1 AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=followee) UNION ALL SELECT chat.social_account_for_wallet(target)::text,'block' FROM interaction_blocks WHERE owner=$1 ORDER BY 2,1 LIMIT 10001`, wallet)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id *string
		var kind string
		if e = rows.Scan(&id, &kind); e != nil {
			return nil, e
		}
		count++
		if id == nil {
			continue
		}
		if kind == "follow" {
			followed = append(followed, *id)
		} else {
			blocked = append(blocked, *id)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if count > 10000 {
		return nil, safety.ErrUnavailable
	}
	return struct {
		AccountID                         string
		Revision                          uint64
		FollowedActorIDs, BlockedActorIDs []string
		ObservedAt                        time.Time
	}{account, uint64(revision), followed, blocked, observed.UTC()}, nil
}
func (r *socialConnectedRuntime) visibilityAuthority(ctx context.Context, kind, id, account string) (any, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var actor string
	var removed bool
	var revision int64
	switch kind {
	case "callout":
		e = tx.QueryRow(ctx, `SELECT actor,removed,revision FROM interaction_callout_authority WHERE id=$1 ORDER BY revision DESC LIMIT 1`, id).Scan(&actor, &removed, &revision)
	case "comment":
		e = tx.QueryRow(ctx, `SELECT author,removed,revision FROM interaction_comments WHERE id=$1`, id).Scan(&actor, &removed, &revision)
	case "pool", "token":
		e = tx.QueryRow(ctx, `SELECT creator,false,0 FROM chat.social_canonical_pools() WHERE (pool=$1 AND $2='pool') OR (token=$1 AND $2='token')`, id, kind).Scan(&actor, &removed, &revision)
	default:
		return nil, safety.ErrInvalid
	}
	if e != nil {
		return nil, e
	}
	var safetyRevision int64
	var status string
	e = tx.QueryRow(ctx, `SELECT revision,status FROM safety_visibility WHERE target_type=$1 AND target_id=$2`, kind, id).Scan(&safetyRevision, &status)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	if e == nil {
		if safetyRevision < 1 {
			return nil, safety.ErrUnavailable
		}
		removed = removed || status == "removed"
		if safetyRevision > revision {
			revision = safetyRevision
		}
	}
	var denied bool
	var observed time.Time
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1) OR EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND scope='global' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>transaction_timestamp())),transaction_timestamp()`, actor).Scan(&denied, &observed)
	if e != nil {
		return nil, e
	}
	removed = removed || denied
	if account != "" {
		var viewer string
		if e = tx.QueryRow(ctx, `SELECT chat.social_wallet_for_account($1::uuid)`, account).Scan(&viewer); e != nil {
			return nil, e
		}
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_blocks WHERE (owner=$1 AND target=$2) OR (owner=$2 AND target=$1)) OR EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1)`, viewer, actor).Scan(&denied); e != nil {
			return nil, e
		}
		removed = removed || denied
	}
	return struct {
		ResourceID   string
		Revision     uint64
		PublicReason string
		Visible      bool
		ObservedAt   time.Time
	}{id, uint64(revision), "", !removed, observed.UTC()}, nil
}
func (r *socialConnectedRuntime) rankingVisibility(ctx context.Context) (any, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var observed time.Time
	if e = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&observed); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT p.pool,NOT EXISTS(SELECT 1 FROM safety_visibility v WHERE v.status='removed' AND ((v.target_type='token' AND v.target_id=p.token) OR (v.target_type='pool' AND v.target_id=p.pool))) AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=p.creator),COALESCE((SELECT max(v.revision) FROM safety_visibility v WHERE (v.target_type='token' AND v.target_id=p.token) OR (v.target_type='pool' AND v.target_id=p.pool)),0) FROM chat.social_canonical_pools() p ORDER BY p.pool LIMIT 100001`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	pools := map[string]bool{}
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var pool string
		var visible bool
		var revision int64
		if e = rows.Scan(&pool, &visible, &revision); e != nil {
			return nil, e
		}
		count++
		pools[pool] = visible
		fmt.Fprintf(hash, "%s:%t:%d\n", pool, visible, revision)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if count == 0 || count > 100000 {
		return nil, safety.ErrUnavailable
	}
	return struct {
		Revision   string          `json:"revision"`
		ObservedAt time.Time       `json:"observedAt"`
		Complete   bool            `json:"complete"`
		Pools      map[string]bool `json:"pools"`
	}{fmt.Sprintf("%x", hash.Sum(nil)), observed.UTC(), true, pools}, nil
}

func (r *socialConnectedRuntime) reviewCallout(ctx context.Context, account, pool, body string) (any, error) {
	if e := interactions.Text(body); e != nil {
		return nil, e
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var wallet, creator string
	if e = tx.QueryRow(ctx, `SELECT chat.social_wallet_for_account($1::uuid),chat.interaction_pool_creator($2::text)`, account, pool).Scan(&wallet, &creator); e != nil {
		return nil, e
	}
	if creator == "" {
		return nil, safety.ErrUnavailable
	}
	var revision int64
	var observed time.Time
	var denied bool
	e = tx.QueryRow(ctx, `SELECT g.revision,transaction_timestamp(),EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1) OR EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>transaction_timestamp()) AND (scope='global' OR (scope='pool' AND resource=$2))) OR EXISTS(SELECT 1 FROM safety_visibility WHERE target_type IN ('pool','token') AND target_id=$2 AND status='removed') FROM interaction_graph_revisions g WHERE g.wallet=$1`, wallet, pool).Scan(&revision, &observed, &denied)
	if e != nil {
		return nil, e
	}
	return struct {
		ResourceID   string
		Revision     uint64
		PublicReason string
		Visible      bool
		ObservedAt   time.Time
	}{pool, uint64(revision), "", !denied, observed.UTC()}, nil
}
func (r *socialConnectedRuntime) contentVisibility(ctx context.Context, kind, id string) (safety.Visibility, error) {
	result := safety.Visibility{TargetType: kind, TargetID: id}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	var owner string
	var denied bool
	switch kind {
	case "token":
		e = tx.QueryRow(ctx, `SELECT creator FROM chat.social_canonical_pools() WHERE pool=$1`, id).Scan(&owner)
	case "asset":
		e = tx.QueryRow(ctx, `SELECT owner_wallet,state IN ('REJECTED','DELETED') OR blocked FROM chat.social_asset_state($1::uuid)`, id).Scan(&owner, &denied)
	default:
		return result, safety.ErrInvalid
	}
	if e != nil || owner == "" || denied {
		return result, safety.ErrUnavailable
	}
	var policyDenied bool
	if e = tx.QueryRow(ctx, `SELECT NOT chat.interaction_account_active($1::text) OR EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1) OR EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND scope='global' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>transaction_timestamp()))`, owner).Scan(&policyDenied); e != nil || policyDenied {
		return result, safety.ErrUnavailable
	}
	e = tx.QueryRow(ctx, `SELECT revision,status,case_id::text FROM safety_visibility WHERE target_type=$1 AND target_id=$2`, kind, id).Scan(&result.Revision, &result.Status, &result.CaseID)
	if errors.Is(e, pgx.ErrNoRows) {
		result.Status = "allowed"
		return result, nil
	}
	if e != nil || result.Revision < 1 || result.CaseID == "" {
		return result, safety.ErrUnavailable
	}
	return result, nil
}

func (r *socialConnectedRuntime) projectMessage(ctx context.Context, m fanout.DurableMessage) (fanout.DurableMessage, error) {
	var denied bool
	e := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1) OR EXISTS(SELECT 1 FROM safety_visibility WHERE target_type='message' AND target_id=$2 AND status='removed')`, m.Sender, m.ID).Scan(&denied)
	if e != nil {
		return m, e
	}
	if denied && !m.Deleted {
		m.Deleted = true
		m.Content = ""
		m.ReplyTo = ""
		m.Revision++
	}
	return m, nil
}
