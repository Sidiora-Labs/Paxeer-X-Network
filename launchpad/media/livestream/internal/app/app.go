// Package app wires the media/livestream service together (config, migrations,
// pgxpool, redis, Livepeer client, HTTP router, health, graceful shutdown) and
// owns its lifecycle. Keeping the wiring here (rather than in main) makes the
// whole service testable end-to-end against external test servers.
package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/streams"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimitMax / rateLimitWindow mirror the TS registerRateLimit(app, {max:60,
// windowSeconds:60}) configuration.
const (
	rateLimitMax    = 60
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-serve livestream service.
type App struct {
	secretStop context.CancelFunc
	tracker    *process.Tracker
	Router     http.Handler
	// PrivateRouter serves the platform runtime's private mTLS listener (nil
	// for the other runtimes).
	PrivateRouter http.Handler
	private       *platformPrivate
	pool          *pgxpool.Pool
	redis         *goredis.Client
	rpc           *ethclient.Client
	logger        *slog.Logger
	workerStop    context.CancelFunc
	workerDone    chan struct{}
	failure       chan error
	workerMu      sync.Mutex
	workerError   error
	closeOnce     sync.Once
}

const LivepeerWebhookSourceRevision = "72187ec428cdd41c81ff75556d77a609b2990695"

func decodeStudioWebhook(raw []byte) (streams.ProviderEvent, error) {
	if _, err := sharedauth.BodyDigest(raw); err != nil {
		return streams.ProviderEvent{}, streams.ErrInvalid
	}
	var event struct {
		ID        string `json:"id"`
		WebhookID string `json:"webhookId"`
		CreatedAt int64  `json:"createdAt"`
		Timestamp int64  `json:"timestamp"`
		Event     string `json:"event"`
		Stream    struct {
			ID string `json:"id"`
		} `json:"stream"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ID == "" || event.WebhookID == "" || event.Stream.ID == "" || event.CreatedAt < 1 || event.Timestamp < event.CreatedAt {
		return streams.ProviderEvent{}, streams.ErrInvalid
	}
	state := ""
	switch event.Event {
	case "stream.started":
		state = "LIVE"
	case "stream.idle":
		state = "ENDED"
	default:
		return streams.ProviderEvent{}, streams.ErrInvalid
	}
	return streams.ProviderEvent{EventID: event.ID, ProviderID: event.Stream.ID, State: state, ObservedAt: time.UnixMilli(event.CreatedAt).UTC()}, nil
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	secretCheck, secretStop, err := cfg.ResolveProductionSecrets(ctx)
	if err != nil {
		return nil, err
	}
	a := &App{secretStop: secretStop, tracker: process.NewTracker(), logger: logger, failure: make(chan error, 1)}
	owned := false
	defer func() {
		if !owned {
			a.Close()
		}
	}()
	var metrics *telemetry.Metrics
	if cfg.NodeEnv == "production" {
		metrics, err = telemetry.New("livestreamd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if err != nil {
			return nil, err
		}
	}
	checks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		checks["secrets"] = secretCheck
	}
	platform := cfg.Platform()
	if cfg.NodeEnv != "production" {
		// Outside production the embedded migrations are applied at boot. In
		// production the migration coordinator owns the schema
		// (spec/production-system/integration/migrations): the runtime never
		// migrates and OpenRuntimePool verifies the coordinator's
		// ExpectedSchema document before serving.
		if err = migrate.Up(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}
	a.pool, err = cfg.OpenRuntimePool(ctx, "livestreamd", shareddb.PoolOptions{})
	if err != nil {
		return nil, err
	}
	a.redis, err = sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	deps := streams.Deps{Queries: sqlcdb.New(a.pool), Redis: a.redis, Livepeer: livepeer.New(cfg.LivepeerAPIKey, cfg.LivepeerAPIURL, logger), MaxStreamsPerWallet: cfg.MaxStreamsPerWallet, WebhookSecret: cfg.LivepeerWebhookSecret, Logger: logger}
	var connected *streams.ConnectedRuntime
	if cfg.ConnectedEnabled {
		if cfg.WebhookTimestampUnit != "milliseconds" {
			return nil, errors.New("livepeer_webhook_source_requires_milliseconds")
		}
		if err = cfg.ValidateConnected(); err != nil {
			return nil, err
		}
		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		a.rpc, err = ethclient.DialContext(dialCtx, cfg.RPCURL)
		cancel()
		if err != nil {
			return nil, errors.New("livestream_rpc_unavailable")
		}
		connected, err = streams.NewConnectedRuntime(ctx, cfg, a.pool, a.redis, sharedauth.NewReportedHashBackend(a.rpc.Client()), decodeStudioWebhook)
		if err != nil {
			return nil, err
		}
		deps = connected.Deps
		deps.Logger = logger
		checks["stream_provider_authority"] = connected.Ready
		checks["stream_action_chain"] = func(c context.Context) error {
			bounded, cancel := context.WithTimeout(c, 2*time.Second)
			defer cancel()
			chain, e := a.rpc.ChainID(bounded)
			if e != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
				return streams.ErrUnavailable
			}
			return nil
		}
		checks["stream_worker_schema"] = func(c context.Context) error {
			var present bool
			e := a.pool.QueryRow(c, `SELECT to_regclass('livestream.stream_operations') IS NOT NULL AND to_regclass('livestream.stream_credentials') IS NOT NULL AND to_regclass('livestream.stream_worker_attempts') IS NOT NULL AND to_regclass('livestream.stream_key_rotations') IS NOT NULL AND to_regclass('livestream.stream_usage') IS NOT NULL AND to_regclass('livestream.stream_budget_alerts') IS NOT NULL AND to_regclass('operation_ledger') IS NOT NULL AND to_regclass('security_audit_outbox') IS NOT NULL AND to_regclass('chat.safety_bans') IS NOT NULL AND to_regclass('indexer.indexer_canonical_creations_v1') IS NOT NULL`).Scan(&present)
			if e != nil || !present {
				return streams.ErrUnavailable
			}
			return nil
		}
		checks["stream_workers"] = a.workerReady
	} else if platform {
		// Production platform runtime: the public read surface over the real
		// lifecycle store, Redis viewer counter and moderation bans, with
		// viewer sessions signed by LIVESTREAM_VIEWER_TOKEN_KEY. There is no
		// broadcast provider, so provider-bound routes answer
		// BROADCAST_NOT_PROVISIONED.
		deps, err = streams.NewPublicReads(streams.PublicReadOptions{Pool: a.pool, Redis: a.redis, WalletLimit: cfg.MaxStreamsPerWallet, GlobalLimit: cfg.MaxGlobalStreams, ViewerTokenKey: []byte(cfg.ViewerTokenKey)})
		if err != nil {
			return nil, err
		}
		deps.Queries = sqlcdb.New(a.pool)
		deps.Logger = logger
		checks["stream_read_schema"] = func(c context.Context) error { return streams.ReadSchemaReady(c, a.pool, "livestream") }
	} else if cfg.NodeEnv == "production" {
		checks["stream_provider_authority"] = func(context.Context) error { return streams.ErrUnavailable }
	} else {
		// Outside production there is no provider/identity authority: serve the
		// public read surface from the real lifecycle store and answer every
		// broadcast action as BROADCAST_NOT_PROVISIONED.
		deps, err = streams.NewDevelopmentReads(streams.DevelopmentReadOptions{Pool: a.pool, Redis: a.redis, WalletLimit: cfg.MaxStreamsPerWallet, GlobalLimit: cfg.MaxGlobalStreams, ViewerTokenKey: []byte(cfg.ViewerTokenKey)})
		if err != nil {
			return nil, err
		}
		deps.Queries = sqlcdb.New(a.pool)
		deps.Logger = logger
		checks["stream_read_schema"] = func(c context.Context) error { return streams.ReadSchemaReady(c, a.pool, "livestream") }
	}
	trustedProxies, err := cfg.TrustedProxies()
	if err != nil {
		return nil, err
	}
	handlers := streams.New(deps)
	mux := sharedhttp.NewRouter(sharedhttp.ServerOptions{TrustedProxies: trustedProxies, CORSOrigins: cfg.CORSAllowedOrigins, Health: sharedhttp.HealthDeps{Custom: checks, State: a.tracker.Ready, DB: func(c context.Context) error { return cfg.CheckRuntimeSchema(c, a.pool) }, Redis: func(c context.Context) error { return a.redis.Ping(c).Err() }}})
	mux.Group(func(gr chi.Router) {
		limiter := sharedhttp.RateLimit(sharedhttp.RateLimitOptions{Max: rateLimitMax, Window: rateLimitWindow, Redis: a.redis})
		if platform {
			limiter = gatewayIngressExempt(limiter)
		}
		gr.Use(limiter)
		handlers.RegisterRoutes(gr)
	})
	if metrics != nil || connected != nil {
		mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if metrics != nil {
				metrics.ServeHTTP(w, r)
			} else {
				w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			}
			if connected != nil {
				_ = connected.Metrics.WritePrometheus(w)
			}
		}))
	}
	a.Router = runtimeHandler(a.tracker, mux, metrics)
	if platform {
		// Production platform runtime: the API is served on the private mTLS
		// listener to the gateway; the plain listener keeps health/metrics.
		a.private, err = newPlatformPrivate(cfg)
		if err != nil {
			return nil, err
		}
		a.PrivateRouter = runtimeHandler(a.tracker, a.private.handler(mux), metrics)
		a.Router = platformPublicOnly(a.Router)
	}
	if connected != nil {
		workerCtx, cancel := context.WithCancel(ctx)
		a.workerStop = cancel
		a.workerDone = make(chan struct{})
		go func() {
			var workerErr error
			defer func() {
				if recover() != nil {
					workerErr = errors.New("livestream_worker_panicked")
				}
				if workerCtx.Err() == nil {
					if workerErr == nil {
						workerErr = errors.New("livestream_workers_stopped")
					}
					a.workerMu.Lock()
					a.workerError = workerErr
					a.workerMu.Unlock()
					a.tracker.BeginDrain()
					select {
					case a.failure <- errors.New("livestream_worker_failure"):
					default:
					}
				}
				close(a.workerDone)
			}()
			workerErr = connected.Run(workerCtx)
		}()
	}
	if err = a.tracker.SetReady(); err != nil {
		return nil, err
	}
	owned = true
	return a, nil
}
func (a *App) workerReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	if a.workerError != nil || a.workerDone == nil {
		return streams.ErrUnavailable
	}
	select {
	case <-a.workerDone:
		return streams.ErrUnavailable
	default:
		return nil
	}
}
func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		if a.tracker != nil {
			a.tracker.BeginDrain()
		}
		if a.workerStop != nil {
			a.workerStop()
		}
		if a.workerDone != nil {
			<-a.workerDone
		}
		if a.secretStop != nil {
			a.secretStop()
		}
		if a.rpc != nil {
			a.rpc.Close()
		}
		if a.pool != nil {
			a.pool.Close()
		}
		if a.redis != nil {
			if err := a.redis.Close(); err != nil && a.logger != nil {
				a.logger.Warn("redis close", slog.String("err", "redis_close_failed"))
			}
		}
	})
}

func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("livestream", cfg.LogLevel)
	logger.Info("starting livestream service")
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer a.Close()
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port), Handler: a.Router, ConnState: a.tracker.ConnState, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	serveErr := make(chan error, 2)
	var privateSrv *http.Server
	if a.private != nil {
		privateSrv = &http.Server{Addr: a.private.address, Handler: a.PrivateRouter, ConnState: a.tracker.ConnState, TLSConfig: a.private.tls, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		go func() {
			logger.Info("private mTLS listener", slog.String("address", a.private.address))
			listener, e := net.Listen("tcp", privateSrv.Addr)
			if e == nil {
				e = privateSrv.Serve(tls.NewListener(listener, a.private.tls))
			}
			if e != nil && !errors.Is(e, http.ErrServerClosed) {
				select {
				case serveErr <- e:
				default:
				}
				cancel()
			}
		}()
	}
	monitorDone := make(chan struct{})
	defer close(monitorDone)
	go func() {
		select {
		case e := <-a.failure:
			serveErr <- e
			cancel()
		case <-monitorDone:
		case <-ctx.Done():
		}
	}()
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		if e := srv.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			select {
			case serveErr <- e:
			default:
			}
			cancel()
		}
	}()
	shutErr := process.Run(ctx, process.Options{Tracker: a.tracker, Logger: logger, OnShutdown: func(c context.Context) error {
		if a.workerStop != nil {
			a.workerStop()
		}
		if privateSrv != nil {
			if e := privateSrv.Shutdown(c); e != nil {
				return e
			}
		}
		return srv.Shutdown(c)
	}})
	cancel()
	select {
	case e := <-serveErr:
		return e
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
