// Package app wires the core/trading-charts service together (config, migrations,
// pgxpool, Redis, candle builder, swap consumer, gap-fill timer, UDF + webhook +
// WS HTTP, graceful shutdown) and owns its lifecycle.
package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	sharedchain "github.com/Sidiora-Technologies/KindleLaunch/shared/chain"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	sharedprocess "github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

// App is a fully-wired, ready-to-run trading-charts service.
type App struct {
	accounting  *consumer.AccountingConsumer
	transport   *natsdriver.Consumer
	nats        *nats.Conn
	rpc         *ethclient.Client
	devRPC      *gethrpc.Client
	verifiedRPC *sharedchain.Client
	exact       *store.ExactStore
	features    config.ChartRuntimeFeatures
	secretStop  context.CancelFunc
	tracker     *sharedprocess.Tracker
	Router      http.Handler
	Builder     *engine.Builder
	Consumer    *consumer.SwapConsumer

	pool  *pgxpool.Pool
	redis *goredis.Client
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
		metrics, e = telemetry.New("chartsd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
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

	pool, err := cfg.OpenRuntimePool(ctx, "chartsd", shareddb.PoolOptions{
		MaxConns:         30,
		MaxConnIdleTime:  10 * time.Second,
		ConnectTimeout:   3 * time.Second,
		StatementTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, err
	}

	defer func() {
		if !secretOwned {
			pool.Close()
			_ = rdb.Close()
		}
	}()
	st := store.New(pool)
	builder := engine.New(pool, rdb, st, logger)

	swapConsumer, err := consumer.New(builder, cfg.RedisURL, logger)
	if err != nil {
		pool.Close()
		_ = rdb.Close()
		return nil, err
	}

	defer func() {
		if !secretOwned {
			_ = swapConsumer.Close()
		}
	}()
	var verifiedRPC *sharedchain.Client
	var exactStore *store.ExactStore
	var features config.ChartRuntimeFeatures
	var archive *store.ArchiveReader
	var accountingConsumer *consumer.AccountingConsumer
	var transport *natsdriver.Consumer
	var nc *nats.Conn
	var rpc *ethclient.Client
	var devRPC *gethrpc.Client
	if cfg.NodeEnv == "production" {
		var d chartRuntimeDocument
		if err = config.ReadBoundDocument(cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, &d); err != nil {
			return nil, err
		}
		if d.Schema != "candles" || d.SourceStream == "" || d.Owner == "" || d.ConsumerID == "" || d.Generation == "" || d.ProjectionVersion == "" {
			return nil, errors.New("charts: incomplete runtime configuration")
		}
		features, err = cfg.RuntimeFeatures()
		if err != nil {
			return nil, err
		}
		verifiedRPC, err = sharedchain.NewVerifiedClient(ctx, cfg.RPCURL, cfg.RPCURLFallback, sharedchain.VerifiedConfig{Profile: profile.Paxeer(features.EndpointSetVersion, features.DeploymentVersion), AnchorHeight: features.AnchorHeight, AnchorHash: common.HexToHash(features.AnchorHash)})
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				verifiedRPC.Close()
			}
		}()
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
		exact, e := store.NewExact(pool, d.Schema)
		if e != nil {
			return nil, e
		}
		exactStore = exact
		if features.Archive != nil {
			if len(cfg.OperatorToken) < 32 {
				return nil, errors.New("charts: archive operator token required")
			}
			objects, archiveErr := storage.New(ctx, storage.Config{Endpoint: features.Archive.Endpoint, Region: features.Archive.Region, Bucket: features.Archive.Bucket, AccessKeyID: cfg.ArchiveAccessKeyID, SecretAccessKey: cfg.ArchiveSecretAccessKey})
			if archiveErr != nil {
				return nil, archiveErr
			}
			archive = &store.ArchiveReader{Objects: objects, Bucket: features.Archive.Bucket, Exact: exact, BlockTime: func(c context.Context, height int64, expected string) (int64, error) {
				if height < 0 {
					return 0, errors.New("charts: invalid archive height")
				}
				block, e := (chainread.Reader{RPC: rpc}).Block(c, height)
				if e != nil {
					return 0, errors.New("charts: archive block timestamp unavailable")
				}
				if !strings.EqualFold(block.Hash, expected) {
					return 0, errors.New("charts: archive block authority mismatch")
				}
				return block.Time, nil
			}}
		}
		accountingConsumer, e = consumer.NewSourceVerifiedAccountingConsumer(consumer.SourceVerifiedAccountingConfig{Pool: pool, Deliveries: durable, Partitions: partitions, Exact: exact, ConsumerID: d.ConsumerID, Generation: d.Generation, ProjectionVersion: d.ProjectionVersion, Owner: d.Owner, SourceStream: d.SourceStream, SourcePartitionSchema: "indexer", RPC: rpc, MaxReplayBlocks: d.MaxReplayBlocks, MaxReplayFills: d.MaxReplayFills, SeriesV2: true, StreamWriter: &delivery.StreamWriter{Schema: "api_stream"}})
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
		secretChecks["verified_rpc"] = func(c context.Context) error {
			pin, e := verifiedRPC.Pin(c, features.AnchorHeight)
			if e != nil {
				return e
			}
			if pin.Hash != common.HexToHash(features.AnchorHash) {
				return errors.New("charts: verified RPC anchor changed")
			}
			return nil
		}
		transport, e = natsdriver.NewConsumer(ctx, nc, d.Consumer)
		if e != nil {
			return nil, e
		}
	}
	trustedProxies, err := cfg.TrustedProxies()
	if err != nil {
		return nil, err
	}
	router := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins:    cfg.CORSAllowedOrigins,
		TrustedProxies: trustedProxies,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})

	// Register routes (NewRouter returns *chi.Mux, which satisfies chi.Router).
	httpapi.RegisterUDF(router, st)
	if cfg.NodeEnv == "production" {
		historyCache, e := httpapi.NewHistoryCache(rdb, httpapi.PostgresHistoryRevision{Pool: pool, Schema: "candles"}, features.MaxHistoryOrigins)
		if e != nil {
			return nil, e
		}
		httpapi.RegisterProductionUDF(router, httpapi.SymbolsHandler{Canonical: &httpapi.CanonicalPoolMetadataSource{Pool: pool, Chain: &chainread.Reader{RPC: rpc}}, DisplayPrecision: features.DisplayPrecision}, httpapi.ProductionHistoryHandler{Source: httpapi.CanonicalCandleClient{Endpoint: features.CanonicalAPIOrigin}, Supply: &httpapi.RPCSupplySource{Chain: &chainread.Reader{RPC: rpc}}, Cache: historyCache, Algorithm: features.CanonicalSeriesModel})
		if e = httpapi.RegisterReplayGateway(router, features.ReplayGatewayOrigin); e != nil {
			return nil, e
		}
		registerSeriesRuntime(router, exactStore, archive, cfg.OperatorToken)
	}
	if cfg.NodeEnv != "production" {
		// Versioned UDF over the development stores: history from
		// candles.candles, symbols from indexer.pools + the chain RPC.
		// Without RPC_URL symbols answer 503; history needs only Postgres.
		if cfg.RPCURL != "" {
			chain, e := developmentChainClient(ctx, cfg.RPCURL)
			if e != nil {
				return nil, e
			}
			defer func() {
				if !secretOwned {
					chain.Close()
				}
			}()
			devRPC = chain
		}
		httpapi.RegisterDevelopmentUDF(router, httpapi.SymbolsHandler{Development: &httpapi.DevelopmentPoolMetadataSource{Pool: pool, Chain: devRPC}}, httpapi.DevelopmentHistoryHandler{Source: httpapi.PostgresHistorySource{Pool: pool}})
		httpapi.RegisterWebhook(router, httpapi.WebhookDeps{
			Builder: builder,
			Logger:  logger,
			Secret:  cfg.WebhookHMACSecret,
		})
	}
	httpapi.RegisterWS(ctx, router, httpapi.WSDeps{
		RedisURL:       cfg.RedisURL,
		Logger:         logger,
		MaxConnections: cfg.WSMaxConnections,
		MaxPerIP:       cfg.WSMaxPerIP,
	})

	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{secretStop: secretStop, tracker: tracker,
		accounting: accountingConsumer, transport: transport, nats: nc, rpc: rpc, devRPC: devRPC, verifiedRPC: verifiedRPC, exact: exactStore, features: features,
		Router:   runtimeHandler(tracker, router, metrics),
		Builder:  builder,
		Consumer: swapConsumer,
		pool:     pool,
		redis:    rdb,
	}, nil
}

// Close releases all resources.
func (a *App) Close() {
	if a.transport != nil {
		a.transport.Close()
	}
	if a.nats != nil {
		a.nats.Close()
	}
	if a.verifiedRPC != nil {
		a.verifiedRPC.Close()
	}
	if a.rpc != nil {
		a.rpc.Close()
	}
	if a.devRPC != nil {
		a.devRPC.Close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	if a.pool != nil {
		a.pool.Close()
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
	if a.Consumer != nil {
		_ = a.Consumer.Close()
	}
}

// Run loads config, builds the App, serves HTTP, starts the swap consumer and
// gap-fill timer, and blocks until a shutdown signal arrives.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("candles", cfg.LogLevel)
	logger.Info("starting candles service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	// Start Redis pub/sub swap consumer.
	if app.accounting != nil {
		go func() {
			release, e := app.tracker.Acquire()
			if e != nil {
				return
			}
			defer release()
			if e := app.accounting.Run(ctx, app.transport); e != nil && !errors.Is(e, context.Canceled) {
				logger.Error("accounting consumer failed", slog.Any("err", e))
				app.tracker.BeginDrain()
				cancel()
			}
		}()
	} else if err := app.Consumer.Start(ctx); err != nil {
		return fmt.Errorf("app: start consumer: %w", err)
	}

	if app.exact != nil {
		go func() {
			release, e := app.tracker.Acquire()
			if e != nil {
				return
			}
			defer release()
			ticker := time.NewTicker(time.Duration(app.features.RollupIntervalMillis) * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					bounded, stop := context.WithTimeout(ctx, 10*time.Second)
					_, e := engine.RecomputeRollups(bounded, app.exact, app.features.RollupBatch)
					stop()
					if e != nil && !errors.Is(e, context.Canceled) {
						logger.Error("chart rollup failed", slog.Any("err", e))
						app.tracker.BeginDrain()
						cancel()
						return
					}
				}
			}
		}()
	}
	// Candles are trade-only: the builder opens each new candle at the prior
	// close via the candle cursor, so empty (zero-trade) buckets produce no
	// candle at all. No gap-fill scheduling (Bug 6b).

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ConnState:         app.tracker.ConnState,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
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

	select {
	case err := <-serveErr:
		return err
	default:
		return shutErr
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

type chartRuntimeDocument struct {
	Schema            string                    `json:"schema"`
	SourceStream      string                    `json:"sourceStream"`
	ConsumerID        string                    `json:"consumerId"`
	Generation        string                    `json:"generation"`
	Owner             string                    `json:"owner"`
	ProjectionVersion string                    `json:"projectionVersion"`
	Epoch             int64                     `json:"epoch"`
	Region            string                    `json:"region"`
	MaxReplayBlocks   int                       `json:"maxReplayBlocks"`
	MaxReplayFills    int                       `json:"maxReplayFills"`
	Policy            delivery.Policy           `json:"policy"`
	Consumer          natsdriver.ConsumerConfig `json:"consumer"`
}

func registerSeriesRuntime(router chi.Router, exact *store.ExactStore, archive *store.ArchiveReader, operatorToken string) {
	addressPattern := regexp.MustCompile(`^0x[0-9a-f]{40}$`)
	router.Get("/v2/series/{pool}/candles", func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.RawQuery) > 16384 {
			http.Error(w, "series query too large", 422)
			return
		}
		pool := strings.ToLower(chi.URLParam(r, "pool"))
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "invalid query", 422)
			return
		}
		for key, values := range query {
			if len(values) != 1 || (key != "interval" && key != "from" && key != "to" && key != "dense") {
				http.Error(w, "invalid series query", 422)
				return
			}
		}
		from, e1 := strconv.ParseInt(query.Get("from"), 10, 64)
		to, e2 := strconv.ParseInt(query.Get("to"), 10, 64)
		interval := query.Get("interval")
		duration, e3 := engine.IntervalSeconds(interval)
		dense := query.Get("dense") == "true"
		if !addressPattern.MatchString(pool) || e1 != nil || e2 != nil || e3 != nil || from < 0 || to <= from || (to-from-1)/duration+1 > 10000 || (query.Has("dense") && query.Get("dense") != "true" && query.Get("dense") != "false") {
			http.Error(w, "invalid series scope", 422)
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		bars, e := engine.ReadSeries(bounded, exact, pool, interval, from, to, dense)
		if e != nil && !errors.Is(e, engine.ErrNoData) {
			http.Error(w, "canonical series unavailable", 503)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		sharedhttp.WriteJSON(w, 200, map[string]any{"schemaVersion": 2, "algorithmVersion": engine.SeriesAlgorithm, "items": bars, "noData": len(bars) == 0})
	})
	router.Post("/internal/v2/charts/retention:apply", func(w http.ResponseWriter, r *http.Request) {
		if archive == nil || operatorToken == "" {
			http.Error(w, "archive authority unavailable", 503)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, "Bearer ")), []byte(operatorToken)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		var request struct {
			Pool     string `json:"pool"`
			Interval string `json:"interval"`
			From     int64  `json:"from"`
			To       int64  `json:"to"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || !addressPattern.MatchString(request.Pool) {
			http.Error(w, "invalid retention range", 422)
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		deleted, err := exact.DeleteRetainedRange(bounded, request.Pool, request.Interval, request.From, request.To)
		if err != nil {
			http.Error(w, "retention authority rejected range", 409)
			return
		}
		sharedhttp.WriteJSON(w, 200, map[string]any{"deletedRows": deleted})
	})
	jobs := make(chan struct{}, 1)
	router.Post("/internal/v2/charts/archive:rebuild", func(w http.ResponseWriter, r *http.Request) {
		if archive == nil || operatorToken == "" {
			http.Error(w, "archive authority unavailable", 503)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			http.Error(w, "unauthorized", 401)
			return
		}
		supplied := strings.TrimPrefix(authorization, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(operatorToken)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		select {
		case jobs <- struct{}{}:
			defer func() { <-jobs }()
		default:
			http.Error(w, "archive rebuild busy", 429)
			return
		}
		var request struct {
			RunID     string                  `json:"runId"`
			Pool      string                  `json:"pool"`
			From      int64                   `json:"from"`
			To        int64                   `json:"to"`
			Manifests []store.ArchiveManifest `json:"manifests"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || !addressPattern.MatchString(request.Pool) || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(request.RunID) {
			http.Error(w, "invalid archive rebuild", 422)
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		report, err := engine.ReconstructArchive(bounded, exact, *archive, request.RunID, request.Pool, request.From, request.To, request.Manifests)
		if err != nil {
			http.Error(w, "archive reconstruction failed", 503)
			return
		}
		sharedhttp.WriteJSON(w, 200, report)
	})
}

// developmentChainClient dials the chain RPC for the development symbols
// resolver with bounded HTTP timeouts and no redirects.
func developmentChainClient(ctx context.Context, rawURL string) (*gethrpc.Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("charts: development chain RPC URL invalid")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 3 * time.Second, MaxIdleConnsPerHost: 8}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return gethrpc.DialOptions(ctx, rawURL, gethrpc.WithHTTPClient(client))
}
