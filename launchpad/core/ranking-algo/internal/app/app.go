// Package app wires the core/ranking-algo service together (config, pgxpool,
// Redis, ranking service, scheduler, rate-limited HTTP API, graceful shutdown)
// and owns its lifecycle. Ports @market-microservices/ranking-algo/src/index.ts.
package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	sharedprocess "github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/store"
)

const (
	// rateLimitMax/Window mirror the TS registerRateLimit(max:100, windowSeconds:60).
	rateLimitMax    = 100
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-run ranking-algo service.
type App struct {
	native     *nativeRankingRuntime
	secretStop context.CancelFunc
	tracker    *sharedprocess.Tracker
	Router     http.Handler
	Service    *ranker.Service
	cfg        config.Config

	pool  *pgxpool.Pool
	redis *goredis.Client
}

// New builds every component (no migrations: ranking-algo reads schemas owned by
// other services — SECTION 9 read-mostly).
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
	tracker := sharedprocess.NewTracker()
	var metrics *telemetry.Metrics
	if cfg.NodeEnv == "production" {
		var e error
		metrics, e = telemetry.New("rankingd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if e != nil {
			return nil, e
		}
	}
	secretChecks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		secretChecks["secrets"] = secretCheck
	}

	pool, err := cfg.OpenRuntimePool(ctx, "rankingd", shareddb.PoolOptions{
		MaxConns:         20,
		MaxConnIdleTime:  10 * time.Second,
		ConnectTimeout:   3 * time.Second,
		StatementTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		shareddb.ClosePool(pool)
		return nil, err
	}

	if cfg.NodeEnv == "production" {
		nativeApp, nativeErr := newNativeApp(ctx, cfg, logger, pool, rdb, tracker, metrics, secretChecks, secretStop)
		secretOwned = nativeErr == nil
		if nativeErr != nil {
			shareddb.ClosePool(pool)
			rdb.Close()
		}
		return nativeApp, nativeErr
	}
	svc := ranker.NewService(store.New(pool), rdb, cfg.RankingMaxEntries, logger)

	router := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins: cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})

	// Rate-limited ranking routes (health stays un-throttled). Middleware must be
	// declared on the group before the routes are registered (chi rule).
	router.Group(func(gr chi.Router) {
		gr.Use(sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
			Max:    rateLimitMax,
			Window: rateLimitWindow,
			Redis:  rdb,
		}))
		httpapi.RegisterRankings(gr, rdb)
	})

	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{secretStop: secretStop, tracker: tracker,
		Router:  runtimeHandler(tracker, router, metrics),
		Service: svc,
		cfg:     cfg,
		pool:    pool,
		redis:   rdb,
	}, nil
}

// Close releases all resources.
func (a *App) Close() {
	if a.native != nil {
		a.native.close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	if a.pool != nil {
		shareddb.ClosePool(a.pool)
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
}

// Run loads config, builds the App, computes the initial rankings, schedules the
// recurring recomputes, serves the HTTP API, and blocks until shutdown.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("ranking-algo", cfg.LogLevel)
	logger.Info("starting ranking-algo service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	if app.native != nil {
		app.native.run(ctx)
	} else {
		// Initial run before scheduling (parity with index.ts await runAll/runNew).
		if err := app.Service.RunAll(ctx); err != nil {
			logger.Error("initial runAll failed", slog.Any("err", err))
		}
		if err := app.Service.RunNew(ctx); err != nil {
			logger.Error("initial runNew failed", slog.Any("err", err))
		}

		go app.scheduleLoop(ctx, cfg.TickInterval(), app.Service.RunAll, "runAll")
		go app.scheduleLoop(ctx, cfg.NewTickInterval(), app.Service.RunNew, "runNew")

	}
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if app.native != nil {
		srv.TLSConfig = app.native.serverTLS.Clone()
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		serve := srv.ListenAndServe
		if app.native != nil {
			serve = func() error { return srv.ListenAndServeTLS("", "") }
		}
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()

	shutErr := sharedprocess.Run(ctx, sharedprocess.Options{Tracker: app.tracker,
		Logger:     logger,
		OnShutdown: func(sctx context.Context) error { cancel(); return srv.Shutdown(sctx) },
	})

	cancel()

	select {
	case err := <-serveErr:
		return err
	default:
		return shutErr
	}
}

// scheduleLoop invokes run on every tick until ctx is cancelled, logging (but
// not propagating) per-cycle errors so one bad cycle never kills the scheduler
// (parity with the try/catch in index.ts runAll/runNew).
func (a *App) scheduleLoop(ctx context.Context, interval time.Duration, run func(context.Context) error, name string) {
	logger := sharedlog.New("ranking-algo", a.cfg.LogLevel)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := run(ctx); err != nil {
				logger.Error("ranking cycle failed", slog.String("cycle", name), slog.Any("err", err))
			}
		}
	}
}

func runtimeHandler(tracker *sharedprocess.Tracker, next http.Handler, metrics *telemetry.Metrics) http.Handler {
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

type nativeRankingRuntime struct {
	binding     config.RankingRuntimeBinding
	serverTLS   *tls.Config
	visibility  *rankingVisibilityClient
	reader      *store.CanonicalInputReader
	coordinator *ranker.Coordinator
	publication *store.PublicationStore
	cache       *store.SnapshotCache
	pool        *pgxpool.Pool
	stream      delivery.StreamWriter
	guard       *queue.EpochGuard
	provider    sharedsecrets.Provider
	secrets     map[string]string
	ctx         context.Context
	stop        context.CancelFunc
	client      *http.Client
	logger      *slog.Logger
	workers     sync.WaitGroup
}
type rankingVisibilityClient struct {
	endpoint string
	client   *http.Client
}

func (c *rankingVisibilityClient) ReadRankingVisibility(ctx context.Context) (store.RankingVisibility, error) {
	var out store.RankingVisibility
	bounded, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return out, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return out, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return out, ranker.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<20+1))
	if err != nil || len(raw) > 64<<20 {
		return out, ranker.ErrUnavailable
	}
	if json.Unmarshal(raw, &out) != nil || !out.Complete || len(out.Revision) != 64 || out.Pools == nil || len(out.Pools) > 1000000 || out.ObservedAt.IsZero() || out.ObservedAt.After(time.Now()) || time.Since(out.ObservedAt) > 60*time.Second {
		return out, ranker.ErrUnavailable
	}
	return out, nil
}
func (c *rankingVisibilityClient) CurrentRankingRevision(ctx context.Context) (string, time.Time, error) {
	snapshot, err := c.ReadRankingVisibility(ctx)
	return snapshot.Revision, snapshot.ObservedAt, err
}
func rankingBoundBytes(path, digest string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 || len(digest) != 64 {
		return nil, ranker.ErrUnavailable
	}
	raw, err := os.ReadFile(path)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return nil, ranker.ErrUnavailable
	}
	return raw, nil
}
func newNativeApp(ctx context.Context, cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool, rdb *goredis.Client, tracker *sharedprocess.Tracker, metrics *telemetry.Metrics, checks map[string]sharedhttp.Check, secretStop context.CancelFunc) (*App, error) {
	lifecycle, stop := context.WithCancel(ctx)
	native := &nativeRankingRuntime{ctx: lifecycle, stop: stop, pool: pool, logger: logger, secrets: map[string]string{}}
	owned := false
	defer func() {
		if !owned {
			native.close()
		}
	}()
	if err := config.ReadBoundDocument(cfg.RankingRuntimeFile, cfg.RankingRuntimeSHA256, &native.binding); err != nil {
		return nil, err
	}
	b := native.binding
	if b.Version != 2 || b.GatewayDNS == "" || len(b.GatewayCertSHA256) != 64 || b.VisibilityDNS == "" || len(b.VisibilityCertSHA256) != 64 || b.MaxAttempts < 1 || b.MaxAttempts > 100 || b.MaximumReads < 1 || b.MaximumReads > 64 || cfg.TickInterval() != 30*time.Second || cfg.NewTickInterval() != 10*time.Second {
		return nil, errors.New("ranking runtime binding unavailable")
	}
	provider, err := sharedsecrets.NewChecked(sharedsecrets.Options{Provider: sharedsecrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
	if err != nil {
		return nil, err
	}
	native.provider = provider
	secret := func(ref string) (string, error) {
		if ref == "" {
			return "", ranker.ErrUnavailable
		}
		value, err := provider.GetRequired(ctx, ref)
		if err != nil {
			return "", errors.New("ranking required secret unavailable")
		}
		native.secrets[ref] = value
		return value, nil
	}
	ca, err := rankingBoundBytes(b.CAFile, b.CASHA256)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, ranker.ErrUnavailable
	}
	certificate, err := rankingBoundBytes(b.ClientCertFile, b.ClientCertSHA256)
	if err != nil {
		return nil, err
	}
	key, err := secret(b.ClientKeyReference)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certificate, []byte(key))
	if err != nil {
		return nil, err
	}
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}
	pinned := clientTLS.Clone()
	pinned.ServerName = b.VisibilityDNS
	pinned.VerifyConnection = func(connection tls.ConnectionState) error {
		if len(connection.PeerCertificates) == 0 || connection.PeerCertificates[0].VerifyHostname(b.VisibilityDNS) != nil || fmt.Sprintf("%x", sha256.Sum256(connection.PeerCertificates[0].Raw)) != b.VisibilityCertSHA256 {
			return ranker.ErrUnavailable
		}
		return nil
	}
	endpoint, err := url.Parse(b.VisibilityEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/internal/v2/social/ranking-visibility" {
		return nil, ranker.ErrUnavailable
	}
	native.client = &http.Client{Transport: &http.Transport{TLSClientConfig: pinned, Proxy: nil, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, ResponseHeaderTimeout: 2 * time.Second}, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	native.visibility = &rankingVisibilityClient{b.VisibilityEndpoint, native.client}
	certificate, err = rankingBoundBytes(b.ServerCertFile, b.ServerCertSHA256)
	if err != nil {
		return nil, err
	}
	key, err = secret(b.ServerKeyReference)
	if err != nil {
		return nil, err
	}
	pair, err = tls.X509KeyPair(certificate, []byte(key))
	if err != nil {
		return nil, err
	}
	native.serverTLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	guard, err := queue.NewEpochGuard(b.RecoverySchema, b.RecoveryEpoch, b.RecoveryRegion)
	if err != nil {
		return nil, err
	}
	native.guard = guard
	native.reader, err = store.NewCanonicalInputReader(b.SourceSchema, b.StatsSchema, b.SourceStream, b.ProjectionVersion, native.visibility)
	if err != nil {
		return nil, err
	}
	source, err := store.NewFrozenInputs(pool, native.reader)
	if err != nil {
		return nil, err
	}
	native.publication, err = store.NewPublicationStore(pool, b.RankingSchema, guard)
	if err != nil {
		return nil, err
	}
	native.coordinator, err = ranker.NewCoordinator(source, native.publication)
	if err != nil {
		return nil, err
	}
	native.cache, err = store.NewSnapshotCache(rdb, b.CacheNamespace)
	if err != nil {
		return nil, err
	}
	cursorRaw, err := secret(b.CursorKeyReference)
	if err != nil {
		return nil, err
	}
	cursorKey, err := base64.RawURLEncoding.Strict().DecodeString(cursorRaw)
	if err != nil || len(cursorKey) > 64 {
		return nil, ranker.ErrUnavailable
	}
	cursor, err := ranker.NewCursorCodec(cursorKey)
	if err != nil {
		return nil, err
	}
	handler, err := httpapi.NewSnapshots(native.publication.SnapshotStore, native.cache, cursor, native.visibility, b.MaximumReads)
	if err != nil {
		return nil, err
	}
	// Category heads are committed to api_stream (append_ranking) and relayed
	// to NATS by the api_stream outbox relay; ranking holds no broker
	// connection, so the binding's NATS fields are not read.
	native.stream = delivery.StreamWriter{Schema: "api_stream"}
	checks["ranking"] = native.ready
	router := sharedhttp.NewRouter(sharedhttp.ServerOptions{CORSOrigins: cfg.CORSAllowedOrigins, Health: sharedhttp.HealthDeps{Custom: checks, State: tracker.Ready, DB: func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) }}})
	router.Group(func(gr chi.Router) { httpapi.RegisterSnapshots(gr, handler) })
	router.Get("/internal/v2/rankings/inputs", func(w http.ResponseWriter, request *http.Request) {
		bounded, cancel := context.WithTimeout(request.Context(), 30*time.Second)
		defer cancel()
		raw, err := native.reader.MarshalUniverse(bounded, pool, uuid.NewString(), time.Now().UTC().Truncate(time.Second))
		if err != nil {
			sharedhttp.WriteJSON(w, 503, map[string]string{"code": "RANKING_INPUT_UNAVAILABLE"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		sharedhttp.WriteJSON(w, 200, raw)
	})
	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err = tracker.SetReady(); err != nil {
		return nil, err
	}
	ingress := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health" || request.URL.Path == "/health/live" || request.URL.Path == "/health/ready" || request.URL.Path == "/metrics" {
			router.ServeHTTP(w, request)
			return
		}
		if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.PeerCertificates) == 0 || request.TLS.PeerCertificates[0].VerifyHostname(b.GatewayDNS) != nil || fmt.Sprintf("%x", sha256.Sum256(request.TLS.PeerCertificates[0].Raw)) != b.GatewayCertSHA256 {
			sharedhttp.WriteJSON(w, 403, map[string]string{"code": "TRUSTED_INGRESS_REQUIRED"})
			return
		}
		router.ServeHTTP(w, request)
	})
	owned = true
	return &App{native: native, secretStop: secretStop, tracker: tracker, Router: runtimeHandler(tracker, ingress, metrics), cfg: cfg, pool: pool, redis: rdb}, nil
}
func (r *nativeRankingRuntime) ready(ctx context.Context) error {
	for ref, old := range r.secrets {
		value, err := r.provider.GetRequired(ctx, ref)
		if err != nil || value != old {
			return errors.New("ranking secret unavailable or rotated")
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	err = r.guard.Assert(ctx, tx)
	tx.Rollback(ctx)
	if err != nil {
		return err
	}
	visibility, err := r.visibility.ReadRankingVisibility(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, category := range append(ranker.HeavyCategoryNames(), "new") {
		h, err := r.publication.Current(ctx, category)
		if err != nil || h.Stale(now) || h.Provenance.ModerationRevision != visibility.Revision {
			return ranker.ErrUnavailable
		}
	}
	health, err := r.publication.PublicationHealth(ctx, now, r.binding.MaxAttempts)
	if err != nil {
		return err
	}
	if health.Exhausted > 0 || health.OldestPendingAgeSeconds > 60 {
		return ranker.ErrUnavailable
	}
	return nil
}
func (r *nativeRankingRuntime) run(parent context.Context) {
	r.workers.Add(3)
	for _, newOnly := range []bool{false, true} {
		go func(newOnly bool) {
			defer r.workers.Done()
			_ = r.coordinator.Run(r.ctx, newOnly, func(err error) {
				r.logger.Error("ranking source refresh failed", slog.String("categoryRun", fmt.Sprint(newOnly)), slog.String("code", "RANKING_INPUT_UNAVAILABLE"))
			})
		}(newOnly)
	}
	go func() {
		defer r.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for tick := 1; ; tick++ {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				// Superseded, published snapshots past the 900 s cursor horizon
				// are deleted once a minute so ranking storage stays bounded.
				if tick%60 == 0 {
					if _, err := r.publication.Prune(r.ctx, time.Now().UTC(), 100); err != nil {
						r.logger.Error("ranking prune failed", slog.String("code", "RANKING_PRUNE_UNAVAILABLE"))
					}
				}
				for i := 0; i < 6; i++ {
					worked, err := r.publication.CommitStreamOnce(r.ctx, r.stream, r.binding.MaxAttempts)
					if err != nil {
						r.logger.Error("ranking publication failed", slog.String("code", "RANKING_PUBLICATION_UNAVAILABLE"))
						break
					}
					if !worked {
						break
					}
				}
				for _, category := range append(ranker.HeavyCategoryNames(), "new") {
					_ = r.cache.Rebuild(r.ctx, r.publication.SnapshotStore, category)
				}
			}
		}
	}()
}
func (r *nativeRankingRuntime) close() {
	if r.stop != nil {
		r.stop()
	}
	r.workers.Wait()
	if r.client != nil {
		r.client.CloseIdleConnections()
	}
}
