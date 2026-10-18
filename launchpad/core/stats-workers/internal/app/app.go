// Package app wires the core/stats-workers service together (config, migrations,
// pgxpool, Redis, the five event consumers, HTTP read API + HMAC webhook,
// background platform-metrics precompute + bucket-trader cleanup, rate limiting,
// graceful shutdown) and owns its lifecycle. Ports @analytics_microservices/stats
// server.ts + index.ts.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/holdings"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/projectionstats"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
	canonicalsearch "github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/search"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/ethereum/go-ethereum/ethclient"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	sharedprocess "github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const (
	// rateLimitMax / rateLimitWindow mirror the TS registerRateLimit(app,
	// { max: 100, windowSeconds: 60 }).
	rateLimitMax    = 100
	rateLimitWindow = 60 * time.Second

	// platformPrecomputeInterval / bucketCleanupInterval mirror the TS
	// PRECOMPUTE_INTERVAL_MS (25s) and BUCKET_TRADERS_CLEANUP_INTERVAL_MS (1h).
	platformPrecomputeInterval = 25 * time.Second
	bucketCleanupInterval      = time.Hour

	// bucketRetentionSeconds is the bucket_traders prune cutoff (now-24h), the
	// hardcoded TS cleanup window.
	bucketRetentionSeconds = 86400
)

// App is a fully-wired, ready-to-run stats service.
type App struct {
	jobs       []statsRuntimeJob
	accounting *consumer.SourceVerifiedAccountingConsumer
	transports []*natsdriver.Consumer
	nats       *nats.Conn
	rpc        *ethclient.Client
	secretStop context.CancelFunc
	tracker    *sharedprocess.Tracker
	Router     http.Handler

	logger *slog.Logger
	store  *store.Store
	pool   *pgxpool.Pool
	redis  *goredis.Client
	holder *consumer.HolderTracker
}

// New runs migrations and builds every component.
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
		metrics, e = telemetry.New("statsd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if e != nil {
			return nil, e
		}
	}
	secretChecks := map[string]sharedhttp.Check{}
	if secretCheck != nil {
		secretChecks["secrets"] = secretCheck
	}

	if cfg.NodeEnv != "production" {
		if err := migrate.Up(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}

	pool, err := cfg.OpenRuntimePool(ctx, "statsd", shareddb.PoolOptions{
		MaxConns:         30,
		MaxConnIdleTime:  10 * time.Second,
		ConnectTimeout:   3 * time.Second,
		StatementTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	defer func() {
		if !secretOwned {
			pool.Close()
		}
	}()
	var rdb *goredis.Client
	if cfg.NodeEnv == "production" {
		rdb, err = sharedredis.NewBoundedClient(cfg.RedisURL, 40*time.Millisecond)
	} else {
		rdb, err = sharedredis.NewClient(cfg.RedisURL)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}

	defer func() {
		if !secretOwned {
			_ = rdb.Close()
		}
	}()
	st := store.New(pool)

	// Event consumers (driven synchronously by the webhook dispatch).
	swapConsumer := consumer.NewSwapConsumer(st, rdb, logger)
	marketConsumer, err := consumer.NewMarketConsumer(st, logger)
	if err != nil {
		pool.Close()
		_ = rdb.Close()
		return nil, err
	}
	stateConsumer := consumer.NewStateConsumer(st, logger)
	holderTracker := consumer.NewHolderTracker(st, rdb, logger, 0) // 0 -> 10s debounce default
	multihopConsumer := consumer.NewMultihopConsumer(st, logger)

	var reads *projectionstats.ReadService
	var holders *holdings.Repository
	var reactions analytics.ReactionAuthority
	var searchService *canonicalsearch.Service
	var identity httpapi.IdentityAuthority
	var reactionDeps httpapi.DurableReactionDeps
	var jobs []statsRuntimeJob
	runtimeHealth := &statsRuntimeHealth{failures: map[string]error{}}
	var accountingConsumer *consumer.SourceVerifiedAccountingConsumer
	var transports []*natsdriver.Consumer
	defer func() {
		if !secretOwned {
			for _, transport := range transports {
				transport.Close()
			}
		}
	}()
	var nc *nats.Conn
	var rpc *ethclient.Client
	if cfg.NodeEnv == "production" {
		var d statsRuntimeDocument
		if err = config.ReadBoundDocument(cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, &d); err != nil {
			return nil, err
		}
		if err = d.validate(); err != nil {
			return nil, err
		}
		rpc, err = ethclient.DialContext(ctx, cfg.RPCURL)
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				rpc.Close()
			}
		}()
		guard, e := queue.NewEpochGuard("recovery", d.Epoch, d.Region)
		if e != nil {
			return nil, e
		}
		durable, e := delivery.NewStoreWithEpochGuard(pool, d.Schema, d.Policy, guard.Assert)
		if e != nil {
			return nil, e
		}
		partitions, e := delivery.NewPartitionStore(pool, d.Schema)
		if e != nil {
			return nil, e
		}
		repository, e := projectionstats.NewRepository(pool, d.Schema)
		if e != nil {
			return nil, e
		}
		authority, e := projectionstats.NewAccountingSourceAuthority("indexer")
		if e != nil {
			return nil, e
		}
		cursorKey, auditKey, e := cfg.Phase2Keys()
		if e != nil {
			return nil, e
		}
		reads, e = projectionstats.NewReadService(repository, projectionstats.ReadConfig{SourceSchema: "indexer", SourceStream: d.SourceStream, ProjectionVersion: d.ProjectionVersion, CursorKey: cursorKey, CursorTTL: time.Duration(d.Phase2.CursorTTLSeconds) * time.Second})
		if e != nil {
			return nil, e
		}
		holders, e = holdings.NewRepository(pool, d.Phase2.HolderSchema)
		if e != nil {
			return nil, e
		}
		reactions = analytics.ReactionAuthority{Pool: pool, Schema: d.Schema}
		projector, e := analytics.NewProjector(reactions, risk.ProjectionModelV1{}, analytics.PostgresRiskInputs{Authority: reactions})
		if e != nil {
			return nil, e
		}
		// Reaction writes and identity-aware search verify the forwarded
		// browser credential at the gateway identity authority over its
		// private mTLS listener, as issuer "stats".
		mtlsIdentity, e := auth.NewMTLSIdentityClient(cfg.IdentityClient())
		if e != nil {
			return nil, e
		}
		identity = mtlsIdentity
		policies := auth.PolicyRegistry{Version: d.Phase2.ReactionPolicyVersion, Actions: map[string]auth.Policy{"reaction.replace": {Scope: "wallet", OwnerRequired: true, RouteTemplate: "/v2/stats/{poolAddress}/reactions"}}}
		reactionDeps = httpapi.DurableReactionDeps{Authority: reactions, Identity: identity, Ledger: auth.Ledger{Pool: pool, Environment: cfg.NodeEnv, AuditKey: auditKey}, Verifier: auth.WalletOperationVerifier{Backend: auth.NewReportedHashBackend(rpc.Client())}, Policies: policies, Admission: &httpapi.ReactionAdmission{Redis: rdb, Namespace: d.Phase2.ReactionNamespace, WalletPerMinute: d.Phase2.ReactionWalletPerMinute, IPPerMinute: d.Phase2.ReactionIPPerMinute}}
		searchService, e = canonicalsearch.NewService(canonicalsearch.Store{Pool: pool}, rdb, d.Phase2.SearchMaxFills)
		if e != nil {
			return nil, e
		}
		accountingConsumer, e = consumer.NewSourceVerifiedAccountingConsumer(consumer.SourceVerifiedAccountingConfig{MarketEffect: projector.Effect, Pool: pool, Deliveries: durable, Partitions: partitions, Repository: repository, Authority: authority, ConsumerID: d.ConsumerID, Generation: d.Generation, ProjectionVersion: d.ProjectionVersion, Owner: d.Owner, SourceStream: d.SourceStream, SourcePartitionSchema: "indexer", RPC: rpc, MaxReplayBlocks: d.MaxReplayBlocks, MaxReplayFills: d.MaxReplayFills})
		if e != nil {
			return nil, e
		}
		natsOptions, e := cfg.RuntimeNATSOptions()
		if e != nil {
			return nil, e
		}
		nc, e = nats.Connect(cfg.NATSURL, natsOptions...)
		if e != nil {
			return nil, e
		}
		defer func() {
			if !secretOwned {
				nc.Close()
			}
		}()
		for _, c := range d.Consumers {
			transport, e := natsdriver.NewConsumer(ctx, nc, c)
			if e != nil {
				return nil, e
			}
			transports = append(transports, transport)
			current := transport
			jobs = append(jobs, statsRuntimeJob{name: "market accounting", run: func(ctx context.Context) error { return accountingConsumer.Run(ctx, current) }})
		}
		publisher, e := natsdriver.NewPublisher(nc, d.Phase2.OutputStream)
		if e != nil {
			return nil, e
		}
		durablePublisher := projectionstats.DurablePublisher{Repository: repository, Publisher: publisher, Subject: d.Phase2.StatsSubject, Batch: d.Phase2.PublishBatch, AttemptTimeout: time.Duration(d.Phase2.PublishTimeoutSeconds) * time.Second}
		jobs = append(jobs, statsRuntimeJob{name: "stats durable publisher", run: durablePublisher.Run})
		expiry := projectionstats.ExpiryScheduler{Service: reads, Interval: time.Duration(d.Phase2.ExpiryIntervalSeconds) * time.Second, Windows: d.Phase2.Windows, Clock: time.Now}
		runtimeHealth.set("canonical windows", errors.New("canonical windows awaiting initial refresh"))
		jobs = append(jobs, statsRuntimeJob{name: "canonical window expiry", run: func(ctx context.Context) error {
			return retryStatsJob(ctx, expiry.Interval, runtimeHealth, "canonical windows", func(ctx context.Context) error { return expiry.Refresh(ctx, time.Now().Unix()) })
		}})
		runtimeHealth.set("transfer holders", errors.New("transfer holders awaiting canonical catchup"))
		holderCursor := ""
		jobs = append(jobs, statsRuntimeJob{name: "canonical transfer holders", run: func(ctx context.Context) error {
			return retryStatsJob(ctx, time.Duration(d.Phase2.HolderIntervalMS)*time.Millisecond, runtimeHealth, "transfer holders", func(ctx context.Context) error {
				return catchUpStatsHolders(ctx, pool, holders, d.SourceStream, d.Phase2.HolderTokenBatch, d.Phase2.HolderTransferBatch, &holderCursor)
			})
		}})
		jobs = append(jobs, statsRuntimeJob{name: "analytics expiry", run: func(ctx context.Context) error {
			return projector.RunExpiry(ctx, time.Duration(d.Phase2.AnalyticsExpirySeconds)*time.Second, d.Phase2.AnalyticsBatch)
		}})
		searchProjection := &canonicalsearch.Projection{Delivery: durable, ConsumerID: d.Phase2.SearchConsumerID, InboxGeneration: d.Phase2.SearchGeneration, Sources: d.Phase2.SearchSources, InvalidationSubject: d.Phase2.SearchInvalidationSubject, InvalidationEventType: d.Phase2.SearchInvalidationEventType}
		for _, subscription := range d.Phase2.SearchConsumers {
			sub := subscription
			transport, e := natsdriver.NewConsumer(ctx, nc, sub.Consumer)
			if e != nil {
				return nil, e
			}
			transports = append(transports, transport)
			current := transport
			jobs = append(jobs, statsRuntimeJob{name: "search " + sub.Kind + " " + sub.Consumer.Durable, run: func(ctx context.Context) error {
				return current.Run(ctx, func(ctx context.Context, message *natsdriver.Message) error {
					broker, err := message.BrokerPosition()
					if err != nil {
						return err
					}
					envelope, err := delivery.Decode(message.Wire, d.Policy, message.Subject)
					if err != nil {
						return err
					}
					if envelope.EventType != sub.EventType || broker.EventID != envelope.EventID || broker.WireSHA256 != delivery.Hash(message.Wire) {
						return delivery.ErrIntegrity
					}
					switch sub.Kind {
					case "creation":
						return searchProjection.ConsumeCanonicalCreation(ctx, message.Subject, message.Wire, message)
					case "invalidation":
						return searchService.ConsumeInvalidation(ctx, message.Subject, message.Wire, message, d.Policy)
					case "account_profile":
						return searchProjection.ConsumeAccountProfile(ctx, message.Subject, message.Wire, message)
					default:
						return searchProjection.Consume(ctx, message.Subject, message.Wire, message)
					}
				})
			}})
		}
		if err = cfg.ValidateProductionWorkers(); err != nil {
			return nil, err
		}
		publicStats, e := projectionstats.NewPublicStatsProjector(projectionstats.PublicStatsConfig{Reads: reads, HolderSchema: d.Phase2.HolderSchema, StreamSchema: cfg.PublicStreamSchema, Batch: cfg.PublicStatsBatch})
		if e != nil {
			return nil, e
		}
		runtimeHealth.set("public market stats", errors.New("public market stats awaiting initial refresh"))
		jobs = append(jobs, statsRuntimeJob{name: "public market stats", run: func(ctx context.Context) error {
			return retryStatsJob(ctx, time.Duration(cfg.PublicStatsIntervalMS)*time.Millisecond, runtimeHealth, "public market stats", func(ctx context.Context) error {
				_, err := publicStats.RefreshOnce(ctx, time.Now())
				return err
			})
		}})
		legacy := &consumer.CanonicalLegacyBridge{Pool: pool, ProjectionSchema: d.Schema, SourceSchema: "indexer", SourceStream: d.SourceStream, ProjectionVersion: d.ProjectionVersion, Market: marketConsumer, Swap: swapConsumer, Holder: holderTracker, Batch: cfg.LegacyBridgeBatch}
		runtimeHealth.set("legacy read model", errors.New("legacy read model awaiting canonical replay"))
		jobs = append(jobs, statsRuntimeJob{name: "canonical legacy read model", run: func(ctx context.Context) error {
			return retryStatsJob(ctx, time.Duration(cfg.LegacyBridgeIntervalMS)*time.Millisecond, runtimeHealth, "legacy read model", func(ctx context.Context) error {
				_, err := legacy.RunOnce(ctx)
				return err
			})
		}})
		jobs = append(jobs, statsRuntimeJob{name: "platform precompute", run: func(ctx context.Context) error {
			return periodicStatsJob(ctx, platformPrecomputeInterval, logger, "platform precompute", func(c context.Context) error {
				return httpapi.PrecomputePlatformMetrics(c, st, rdb)
			})
		}}, statsRuntimeJob{name: "bucket-trader cleanup", run: func(ctx context.Context) error {
			return periodicStatsJob(ctx, bucketCleanupInterval, logger, "bucket-trader cleanup", func(c context.Context) error {
				_, err := st.PruneBucketTraders(c, shareddb.NowSeconds()-bucketRetentionSeconds)
				return err
			})
		}})
		relay := delivery.Relay{Store: durable, Publisher: publisher, Owner: d.Owner, Batch: d.Phase2.PublishBatch, LeaseDuration: 30 * time.Second, PublishTimeout: time.Duration(d.Phase2.PublishTimeoutSeconds) * time.Second, RetryDelay: time.Second, MaxAttempts: d.Phase2.PublishMaxAttempts}
		jobs = append(jobs, statsRuntimeJob{name: "search durable outbox", run: func(ctx context.Context) error {
			return retryStatsJob(ctx, 100*time.Millisecond, runtimeHealth, "search outbox", relay.RunOnce)
		}})
		secretChecks["broker"] = func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !nc.IsConnected() {
				return errors.New("stats broker disconnected")
			}
			return nil
		}
		secretChecks["source"] = func(ctx context.Context) error {
			var raw, typed, fence int64
			var owner string
			var alive bool
			err := pool.QueryRow(ctx, `SELECT w.raw_complete_height,w.typed_complete_height,l.fence_token,l.owner_instance,l.expires_at>clock_timestamp() FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1`, d.SourceStream).Scan(&raw, &typed, &fence, &owner, &alive)
			if err != nil {
				return err
			}
			if raw < 0 || raw != typed || fence <= 0 || owner == "" || !alive {
				return consumer.ErrAccountingSourceUnavailable
			}
			return nil
		}
		secretChecks["projections"] = runtimeHealth.check
		secretChecks["search"] = func(ctx context.Context) error {
			var active bool
			err := pool.QueryRow(ctx, "SELECT g.state='active' AND g.validated_at IS NOT NULL FROM stats_search.active_v2 a JOIN stats_search.generations_v2 g USING(generation) WHERE a.singleton").Scan(&active)
			if err != nil {
				return err
			}
			if !active {
				return canonicalsearch.ErrUnavailable
			}
			return nil
		}
	}
	router := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins: cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})

	// Read API + webhook (NewRouter returns *chi.Mux, satisfying chi.Router). The
	// static routes (/stats/platform, /stats/batch, /stats/cross-token-swaps/...)
	// are registered before the /stats/{poolAddress} param route so chi resolves
	// them first.
	httpapi.RegisterPlatform(router, st, rdb)
	httpapi.RegisterCrossTokenSwaps(router, st)
	httpapi.RegisterPoolStats(router, st, rdb)
	httpapi.RegisterPoolHolders(router, st)
	httpapi.RegisterPoolTransactions(router, st)
	httpapi.RegisterPoolAnalytics(router, st)
	httpapi.RegisterSearch(router, st, rdb)
	httpapi.RegisterPressure(router, st, rdb)
	httpapi.RegisterReactions(router, rdb)
	if cfg.NodeEnv == "production" {
		(projectionstats.PublicHandlers{Service: reads, Clock: time.Now}).Register(router)
		httpapi.RegisterTransferHolders(router, st, holders)
		httpapi.RegisterDurableAnalytics(router, reactions)
		if err = httpapi.RegisterDurableReactions(router, reactionDeps); err != nil {
			return nil, err
		}
		httpapi.RegisterIntegratedSearch(router, searchService, identity)
	}
	if cfg.NodeEnv != "production" {
		// The web token page reads IF_HOLDERS from this host whenever a stats
		// URL is configured; dev answers it from the webhook-fed ledger.
		httpapi.RegisterDevPoolHolders(router, st)
		httpapi.RegisterWebhook(router, httpapi.WebhookDeps{
			Swap:     swapConsumer,
			Market:   marketConsumer,
			State:    stateConsumer,
			Holder:   holderTracker,
			Multihop: multihopConsumer,
			Logger:   logger,
			Secret:   cfg.WebhookHMACSecret,
		})
	}

	// Redis-backed global rate limit (100 req / 60s per client), wrapped as the
	// outermost layer so it applies to every route (parity with the TS global
	// registerRateLimit).
	handler := sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
		Max:    rateLimitMax,
		Window: rateLimitWindow,
		Redis:  rdb,
	})(router)

	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{jobs: jobs, secretStop: secretStop, tracker: tracker,
		accounting: accountingConsumer, transports: transports, nats: nc, rpc: rpc,
		Router: runtimeHandler(tracker, handler, metrics),
		logger: logger,
		store:  st,
		pool:   pool,
		redis:  rdb,
		holder: holderTracker,
	}, nil
}

// Close releases all resources and stops pending holder-refresh timers.
func (a *App) Close() {
	for _, t := range a.transports {
		t.Close()
	}
	if a.nats != nil {
		a.nats.Close()
	}
	if a.rpc != nil {
		a.rpc.Close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	if a.holder != nil {
		a.holder.Close()
	}
	if a.pool != nil {
		a.pool.Close()
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
}

// startBackgroundJobs launches the platform-metrics precompute loop and the
// bucket_traders cleanup loop, both stopping when ctx is cancelled (parity with
// the TS setInterval timers registered in registerPlatformRoutes).
func (a *App) startBackgroundJobs(ctx context.Context) {
	go a.loop(ctx, platformPrecomputeInterval, "platform precompute", func(c context.Context) error {
		return httpapi.PrecomputePlatformMetrics(c, a.store, a.redis)
	})
	go a.loop(ctx, bucketCleanupInterval, "bucket-trader cleanup", func(c context.Context) error {
		cutoff := shareddb.NowSeconds() - bucketRetentionSeconds
		removed, err := a.store.PruneBucketTraders(c, cutoff)
		if err == nil && removed > 0 {
			a.logger.Info("pruned stale bucket_traders", slog.Int64("removed", removed), slog.Int64("cutoff", cutoff))
		}
		return err
	})
}

// loop runs fn on a ticker until ctx is cancelled, logging (but not propagating)
// errors so a transient failure never kills the loop.
func (a *App) loop(ctx context.Context, interval time.Duration, name string, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := fn(ctx); err != nil {
				a.logger.Error("background job failed", slog.String("job", name), slog.Any("err", err))
			}
		}
	}
}

// Run loads config, builds the App, serves HTTP, starts the background jobs, and
// blocks until a shutdown signal arrives.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("stats", cfg.LogLevel)
	logger.Info("starting stats service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	workerErrors := make(chan error, len(app.jobs)+1)
	if len(app.jobs) > 0 {
		releases := make([]func(), 0, len(app.jobs))
		for range app.jobs {
			release, acquireErr := app.tracker.Acquire()
			if acquireErr != nil {
				for _, done := range releases {
					done()
				}
				cancel()
				return acquireErr
			}
			releases = append(releases, release)
		}
		for index, job := range app.jobs {
			current := job
			release := releases[index]
			go func() {
				defer release()
				workerErr := current.run(ctx)
				if ctx.Err() != nil && (workerErr == nil || errors.Is(workerErr, context.Canceled) || errors.Is(workerErr, context.DeadlineExceeded)) {
					return
				}
				if workerErr == nil {
					workerErr = errors.New("required stats worker stopped unexpectedly")
				}
				workerErr = fmt.Errorf("stats worker %s: %w", current.name, workerErr)
				logger.Error("stats runtime worker failed", slog.String("worker", current.name), slog.Any("err", workerErr))
				workerErrors <- workerErr
				app.tracker.BeginDrain()
				cancel()
			}()
		}
	} else {
		app.startBackgroundJobs(ctx)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port),
			slog.String("webhook", "POST /webhooks/events"))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()

	shutErr := sharedprocess.Run(ctx, sharedprocess.Options{Tracker: app.tracker,
		Logger:     logger,
		OnShutdown: func(sctx context.Context) error { cancel(); return srv.Shutdown(sctx) },
	})

	cancel()

	var result error = shutErr
	select {
	case err := <-serveErr:
		result = errors.Join(result, err)
	default:
	}
	for {
		select {
		case err := <-workerErrors:
			result = errors.Join(result, err)
		default:
			return result
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

type statsRuntimeDocument struct {
	Phase2            *statsPhase2Document        `json:"phase2"`
	Schema            string                      `json:"schema"`
	SourceStream      string                      `json:"sourceStream"`
	ConsumerID        string                      `json:"consumerId"`
	Generation        string                      `json:"generation"`
	Owner             string                      `json:"owner"`
	ProjectionVersion string                      `json:"projectionVersion"`
	Epoch             int64                       `json:"epoch"`
	Region            string                      `json:"region"`
	MaxReplayBlocks   int                         `json:"maxReplayBlocks"`
	MaxReplayFills    int                         `json:"maxReplayFills"`
	Policy            delivery.Policy             `json:"policy"`
	Consumers         []natsdriver.ConsumerConfig `json:"consumers"`
}

// validate checks the bound DELIVERY_CONFIG document before anything connects.
// NATS stream and durable names are not defaulted in code: they come only from
// this document, which is rendered from the fleet NATS manifest (streams.json),
// because the broker binds them by name (js.Consumer(stream, durable) and
// Publish WithExpectStream(outputStream)).
func (d statsRuntimeDocument) validate() error {
	if d.Phase2 == nil || d.Schema != "stats" || d.SourceStream == "" || d.Owner == "" || d.ConsumerID == "" || d.Generation == "" || d.ProjectionVersion == "" || len(d.Consumers) != 1 {
		return errors.New("stats: incomplete ordered consumer configuration")
	}
	if !d.Policy.Subjects[accounting.MarketSubject] || !d.Policy.EventTypes["accounting.market.v1"] {
		return errors.New("stats: market accounting policy required")
	}
	if err := d.Phase2.validate(d.Policy); err != nil {
		return err
	}
	subjects := map[string]bool{}
	for _, c := range d.Consumers {
		if _, err := c.BrokerConsumerConfig(); err != nil {
			return err
		}
		if c.Subject != accounting.MarketSubject {
			return errors.New("stats: unapproved subject")
		}
		if subjects[c.Subject] {
			return errors.New("stats: duplicate durable subject")
		}
		subjects[c.Subject] = true
	}
	return nil
}

type statsRuntimeJob struct {
	name string
	run  func(context.Context) error
}
type statsRuntimeHealth struct {
	mu       sync.Mutex
	failures map[string]error
}

func (h *statsRuntimeHealth) set(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[name] = err
}
func (h *statsRuntimeHealth) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, err := range h.failures {
		if err != nil {
			return fmt.Errorf("stats %s unavailable: %w", name, err)
		}
	}
	return nil
}

// periodicStatsJob runs fn on a ticker until ctx ends. Failures are logged and
// retried on the next tick (the legacy best-effort maintenance loops).
func periodicStatsJob(ctx context.Context, interval time.Duration, logger *slog.Logger, name string, fn func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := fn(ctx); err != nil {
				logger.Error("background job failed", slog.String("job", name), slog.Any("err", err))
			}
		}
	}
}

func retryStatsJob(ctx context.Context, interval time.Duration, health *statsRuntimeHealth, name string, run func(context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := run(bounded)
		cancel()
		health.set(name, err)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type statsSearchSubscription struct {
	Kind      string                    `json:"kind"`
	EventType string                    `json:"eventType"`
	Consumer  natsdriver.ConsumerConfig `json:"consumer"`
}
type statsPhase2Document struct {
	CursorTTLSeconds            int                                       `json:"cursorTtlSeconds"`
	ExpiryIntervalSeconds       int                                       `json:"expiryIntervalSeconds"`
	Windows                     []int64                                   `json:"windows"`
	OutputStream                string                                    `json:"outputStream"`
	StatsSubject                string                                    `json:"statsSubject"`
	PublishBatch                int                                       `json:"publishBatch"`
	PublishTimeoutSeconds       int                                       `json:"publishTimeoutSeconds"`
	PublishMaxAttempts          int                                       `json:"publishMaxAttempts"`
	HolderSchema                string                                    `json:"holderSchema"`
	HolderIntervalMS            int                                       `json:"holderIntervalMs"`
	HolderTokenBatch            int                                       `json:"holderTokenBatch"`
	HolderTransferBatch         int                                       `json:"holderTransferBatch"`
	AnalyticsExpirySeconds      int                                       `json:"analyticsExpirySeconds"`
	AnalyticsBatch              int                                       `json:"analyticsBatch"`
	ReactionPolicyVersion       string                                    `json:"reactionPolicyVersion"`
	ReactionNamespace           string                                    `json:"reactionNamespace"`
	ReactionWalletPerMinute     int                                       `json:"reactionWalletPerMinute"`
	ReactionIPPerMinute         int                                       `json:"reactionIpPerMinute"`
	SearchMaxFills              int                                       `json:"searchMaxFills"`
	SearchConsumerID            string                                    `json:"searchConsumerId"`
	SearchGeneration            string                                    `json:"searchGeneration"`
	SearchInvalidationSubject   string                                    `json:"searchInvalidationSubject"`
	SearchInvalidationEventType string                                    `json:"searchInvalidationEventType"`
	SearchSources               map[string]canonicalsearch.SourceRelation `json:"searchSources"`
	SearchConsumers             []statsSearchSubscription                 `json:"searchConsumers"`
}

func (d statsPhase2Document) validate(policy delivery.Policy) error {
	token := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	schema := regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	if d.CursorTTLSeconds < 60 || d.CursorTTLSeconds > 3600 || d.ExpiryIntervalSeconds < 1 || d.ExpiryIntervalSeconds > 60 || len(d.Windows) < 1 || len(d.Windows) > 16 || !token.MatchString(d.OutputStream) || !policy.Subjects[d.StatsSubject] || !policy.EventTypes["stats.minute.v2"] || d.PublishBatch < 1 || d.PublishBatch > 100 || d.PublishTimeoutSeconds < 1 || d.PublishTimeoutSeconds > 20 || d.PublishMaxAttempts < 1 || d.PublishMaxAttempts > 100 || !schema.MatchString(d.HolderSchema) || d.HolderIntervalMS < 100 || d.HolderIntervalMS > 60000 || d.HolderTokenBatch < 1 || d.HolderTokenBatch > 100 || d.HolderTransferBatch < 1 || d.HolderTransferBatch > 1000 || d.AnalyticsExpirySeconds < 1 || d.AnalyticsExpirySeconds > 30 || d.AnalyticsBatch < 1 || d.AnalyticsBatch > 100 || !token.MatchString(d.ReactionPolicyVersion) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(d.ReactionNamespace) || d.ReactionWalletPerMinute < 1 || d.ReactionWalletPerMinute > 12 || d.ReactionIPPerMinute < 1 || d.ReactionIPPerMinute > 4000 || d.SearchMaxFills < 1 || d.SearchMaxFills > 128 || !token.MatchString(d.SearchConsumerID) || !token.MatchString(d.SearchGeneration) || !policy.Subjects[d.SearchInvalidationSubject] || !policy.EventTypes[d.SearchInvalidationEventType] || len(d.SearchConsumers) < 2 || len(d.SearchConsumers) > 16 {
		return errors.New("stats: complete bounded phase2 runtime configuration required")
	}
	seenWindows := map[int64]bool{}
	for _, window := range d.Windows {
		if window < 1 || window > 86400 || seenWindows[window] {
			return errors.New("stats: invalid expiry windows")
		}
		seenWindows[window] = true
	}
	creation, invalidation := false, false
	seen := map[string]bool{}
	for _, sub := range d.SearchConsumers {
		if _, err := sub.Consumer.BrokerConsumerConfig(); err != nil {
			return err
		}
		if !policy.Subjects[sub.Consumer.Subject] || !policy.EventTypes[sub.EventType] || seen[sub.Consumer.Stream+":"+sub.Consumer.Durable] {
			return errors.New("stats: unapproved or duplicate search subscription")
		}
		seen[sub.Consumer.Stream+":"+sub.Consumer.Durable] = true
		switch sub.Kind {
		case "creation":
			if sub.Consumer.Subject != "canonical.creation.v1" || sub.EventType != "canonical_creation" {
				return errors.New("stats: canonical creation source required")
			}
			if creation {
				return errors.New("stats: duplicate canonical creation subscription")
			}
			creation = true
		case "invalidation":
			if invalidation || sub.Consumer.Subject != d.SearchInvalidationSubject || sub.EventType != d.SearchInvalidationEventType {
				return errors.New("stats: invalid search invalidation subscription")
			}
			invalidation = true
		case "metadata", "lifecycle", "metrics", "visibility", "profile_deletion", "preferences":
			source, ok := d.SearchSources[sub.Kind]
			if !ok || !schema.MatchString(source.Schema) || !schema.MatchString(source.View) || !strings.HasSuffix(source.View, "_v2") {
				return errors.New("stats: authorized search source relation required")
			}
		case "account_profile":
			if sub.EventType != "profile.updated" && sub.EventType != "account.deletion.requested" && sub.EventType != "account.backfill.completed" {
				return errors.New("stats: authorized account search event required")
			}
		default:
			return errors.New("stats: unsupported search subscription")
		}
	}
	if !creation || !invalidation {
		return errors.New("stats: creation and invalidation consumers required")
	}
	return nil
}
func catchUpStatsHolders(ctx context.Context, pool *pgxpool.Pool, repository *holdings.Repository, stream string, tokenLimit, transferLimit int, after *string) error {
	if ctx == nil || pool == nil || repository == nil || stream == "" || after == nil || tokenLimit < 1 || tokenLimit > 100 || transferLimit < 1 || transferLimit > 1000 {
		return holdings.ErrIntegrity
	}
	var raw, typed, fence int64
	var owner string
	var alive bool
	if err := pool.QueryRow(ctx, `SELECT w.raw_complete_height,w.typed_complete_height,l.fence_token,l.owner_instance,l.expires_at>clock_timestamp() FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1`, stream).Scan(&raw, &typed, &fence, &owner, &alive); err != nil {
		return err
	}
	if raw < 0 || raw != typed || fence <= 0 || owner == "" || !alive {
		return consumer.ErrAccountingSourceUnavailable
	}
	rows, err := pool.Query(ctx, `SELECT k.token FROM indexer.indexer_canonical_creations_v1 k JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=125 AND c.stream_id=$1 AND c.height=k.height AND c.block_hash=k.block_hash WHERE k.token>$2 AND k.height<=$3 AND c.fetch_complete AND c.required_decode_complete AND k.payload->'metadata' IS NOT NULL AND k.payload->'metadata'<>'null'::jsonb ORDER BY k.token LIMIT $4`, stream, *after, typed, tokenLimit)
	if err != nil {
		return err
	}
	tokens := make([]string, 0, tokenLimit)
	for rows.Next() {
		var token string
		if err = rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, token)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(tokens) == 0 {
		*after = ""
	}
	for _, token := range tokens {
		if err = repository.DiscoverCanonical(ctx, token, stream); err != nil {
			return err
		}
		result, catchErr := repository.CatchUpCanonicalAt(ctx, token, stream, strconv.FormatInt(typed, 10), transferLimit)
		if catchErr != nil {
			return catchErr
		}
		if result.Incident || result.Coverage == "quarantined" {
			return holdings.ErrIntegrity
		}
		if result.More || result.Coverage != "complete" {
			return consumer.ErrAccountingSourceUnavailable
		}
		*after = token
	}
	if _, err = repository.DeleteExpiredSnapshots(ctx, 100); err != nil {
		return err
	}
	if len(tokens) == tokenLimit {
		return consumer.ErrAccountingSourceUnavailable
	}
	return nil
}
