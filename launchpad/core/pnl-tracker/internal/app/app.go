// Package app wires the core/pnl-tracker service together (config, migrations,
// pgxpool, Redis, the swap-fold consumer behind an HMAC webhook, the portfolio /
// card / referral / status read API, the OG image renderer, the background
// idempotent reconciler loop, rate limiting, graceful shutdown) and owns its
// lifecycle. Ports @analytics_microservices/pnl server + workers.
package app

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender/fonts"
	inventory "github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l16"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l18"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/card"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/devreads"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/position"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/reconcile"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/referral"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const (
	// rateLimitMax / rateLimitWindow mirror the shared global rate limit
	// (100 req / 60s per client).
	rateLimitMax    = 100
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-run pnl service.
type App struct {
	journeys *pnlJourneys

	fixtureAckBarrier string
	baseline          *inventory.BaselineAuthority
	baselineAhead     *inventory.BaselineAhead
	archiveRPC        *ethclient.Client
	accounting        *inventory.AccountingConsumer
	transport         *natsdriver.Consumer
	nats              *nats.Conn
	rpc               *ethclient.Client
	secretStop        context.CancelFunc
	tracker           *sharedprocess.Tracker
	Router            http.Handler

	logger      *slog.Logger
	pool        *pgxpool.Pool
	redis       *goredis.Client
	reconciler  *reconcile.Reconciler
	reconcileIn time.Duration
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
		metrics, e = telemetry.New("pnld", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
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

	pool, err := cfg.OpenRuntimePool(ctx, "pnld", shareddb.PoolOptions{
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
	var baselineAuthority *inventory.BaselineAuthority
	var baselineAhead *inventory.BaselineAhead
	var archiveRPC *ethclient.Client
	var accountingConsumer *inventory.AccountingConsumer
	var transport *natsdriver.Consumer
	var nc *nats.Conn
	var rpc *ethclient.Client
	if cfg.NodeEnv == "production" {
		if cfg.FixtureCommitAckBarrier != "" {
			if cfg.RuntimeProfile != "owned_local" {
				return nil, errors.New("pnl: fixture barrier forbidden")
			}
			if _, e := cfg.PrivateFixtureEvidence(); e != nil {
				return nil, e
			}
		}
		var d inventoryRuntimeDocument
		if err = config.ReadBoundDocument(cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, &d); err != nil {
			return nil, err
		}
		if d.Schema != "pnl" || d.SourceStream == "" || d.Owner == "" || d.ConsumerID == "" || d.Generation == "" || d.ProjectionVersion == "" {
			return nil, errors.New("pnl: incomplete inventory runtime configuration")
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
		authority, e := inventory.NewBaselineAuthorityWithEpochGuard(pool, rpc, d.Schema, d.SourceStream, d.ConsumerID, d.Generation, d.Owner, d.ProjectionVersion, guard.Assert)
		if e != nil {
			return nil, e
		}
		baselineAuthority = authority
		// Baselines are issued ahead of each wallet's first fill on the
		// primary node, falling back to the archive node (RPC_URL_FALLBACK)
		// when the primary has pruned the creation-block state; both are
		// required, distinct endpoints.
		if cfg.RPCURLFallback == "" || cfg.RPCURLFallback == cfg.RPCURL {
			return nil, errors.New("pnl: distinct archive RPC_URL_FALLBACK required for inventory baselines")
		}
		archiveRPC, e = ethclient.DialContext(ctx, cfg.RPCURLFallback)
		if e != nil {
			return nil, e
		}
		defer func() {
			if !secretOwned {
				archiveRPC.Close()
			}
		}()
		archiveAuthority, e := inventory.NewBaselineAuthorityWithEpochGuard(pool, archiveRPC, d.Schema, d.SourceStream, d.ConsumerID, d.Generation, d.Owner, d.ProjectionVersion, guard.Assert)
		if e != nil {
			return nil, e
		}
		baselineAhead, e = inventory.NewBaselineAhead(authority, archiveAuthority, 32)
		if e != nil {
			return nil, e
		}
		accountingConsumer, e = inventory.NewAccountingConsumerWithRuntime(pool, d.Schema, d.ConsumerID, d.Generation, d.Owner, d.ProjectionVersion, authority, guard.Assert, delivery.StreamWriter{Schema: "api_stream"})
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
		transport, e = natsdriver.NewConsumer(ctx, nc, d.Consumer)
		if e != nil {
			return nil, e
		}
	}
	var journeys *pnlJourneys
	if cfg.JourneyConfigFile != "" {
		if cfg.NodeEnv != "production" || baselineAuthority == nil || accountingConsumer == nil || nc == nil || rpc == nil {
			return nil, errors.New("pnl: production inventory runtime required for journeys")
		}
		journeys, err = newPnLJourneys(ctx, cfg, pool, rdb, rpc, nc, baselineAuthority, accountingConsumer)
		if err != nil {
			return nil, err
		}
		accountingConsumer = journeys.accounting
		defer func() {
			if !secretOwned {
				journeys.second.Close()
			}
		}()
	}
	st := store.New(pool)
	now := shareddb.NowSeconds

	swapConsumer := position.NewConsumer(st, rdb, logger)
	cardSvc := card.New(st, cfg.ShareOrigin(), cfg.OGOrigin(), now)
	referralSvc := referral.New(st, logger, cfg.RewardPerConversion, now)
	renderer := ogrender.New(cfg.FontDir)
	reconciler := reconcile.New(st, rdb, logger, cfg.ReconcileBatchSize)

	trustedProxies, err := sharedhttp.ParseTrustedProxies(cfg.TrustedProxyCIDRs)
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

	if journeys != nil {
		router.Group(func(router chi.Router) {
			router.Use(func(next http.Handler) http.Handler {
				enrolled := journeys.live.EnrollmentMiddleware(next)
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "OPTIONS" && (strings.HasPrefix(r.URL.Path, "/v2/me/portfolio") || strings.HasPrefix(r.URL.Path, "/v3/me/portfolio") || r.URL.Path == "/v3/me/pnl/history") && r.URL.Path != "/v3/me/portfolio/live" {
						enrolled.ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			journeys.shares.RegisterHTTP(router)
			router.Post("/v3/me/portfolio/live", journeys.enrollHTTP)
			router.Delete("/v3/me/portfolio/live", journeys.cancelLiveHTTP)
			journeys.portfolio.RegisterHTTP(router)
			router.Get("/v3/me/portfolio", journeys.portfolio.PortfolioV3HTTP)
			router.Get("/v3/wallets/{wallet}/portfolio", journeys.portfolio.PublicPortfolioV3HTTP)
			router.Get("/v3/me/portfolio/history", journeys.intervalHTTP)
			router.Get("/v2/me/rewards", journeys.rewardsHTTP.Rewards)
			router.Post("/v2/me/rewards/claim", journeys.rewardsHTTP.PrepareClaim)
			router.Post("/v2/me/portfolio/rebuild", journeys.rebuildHTTP)
			router.Get("/v3/me/pnl/history", journeys.intervalHTTP)
			router.Get("/v2/me/referrals", journeys.referralHTTP.Dashboard)
			router.Post("/v2/me/referrals:issue-code", journeys.referralHTTP.IssueCode)
			router.Post("/v2/me/referrals:click", journeys.referralHTTP.Click)
			router.Post("/v2/me/referrals:bind", journeys.referralHTTP.Bind)
			router.Post("/v2/me/referrals:consent", journeys.referralHTTP.Consent)
		})
	}
	shareOrigin := cfg.ShareOrigin()
	httpapi.RegisterReads(router, st, rdb)
	httpapi.RegisterCards(router, httpapi.CardDeps{
		Cards:      cardSvc,
		Renderer:   renderer,
		Redis:      rdb,
		ShareLabel: func(code string) string { return shareLabel(shareOrigin, code) },
	})
	if cfg.NodeEnv != "production" {
		httpapi.RegisterReferrals(router, referralSvc)
		devreads.Mount(ctx, router, cfg, pool, st, logger)
	}
	httpapi.RegisterStatus(router, httpapi.StatusDeps{Store: st, StartedAt: time.Now()})
	if cfg.NodeEnv != "production" {
		httpapi.RegisterWebhook(router, httpapi.WebhookDeps{
			Swap:   swapConsumer,
			Logger: logger,
			Secret: cfg.WebhookHMACSecret,
		})
	}

	handler := sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
		Max:    rateLimitMax,
		Window: rateLimitWindow,
		Redis:  rdb,
		// The limiter wraps the router, so it resolves the client IP itself
		// under the router's trusted-proxy rule.
		KeyFunc: func(r *http.Request) string { return sharedhttp.TrustedClientIP(r, trustedProxies) },
	})(router)

	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{secretStop: secretStop, tracker: tracker, journeys: journeys,
		fixtureAckBarrier: cfg.FixtureCommitAckBarrier, baseline: baselineAuthority, baselineAhead: baselineAhead, archiveRPC: archiveRPC, accounting: accountingConsumer, transport: transport, nats: nc, rpc: rpc,
		Router:      runtimeHandler(tracker, handler, metrics),
		logger:      logger,
		pool:        pool,
		redis:       rdb,
		reconciler:  reconciler,
		reconcileIn: cfg.ReconcileInterval(),
	}, nil
}

// shareLabel renders the human footer drawn on the OG image (origin host without
// scheme, e.g. "sidiora.fun/pnl/abc123").
func shareLabel(shareOrigin, code string) string {
	host := shareOrigin
	for _, prefix := range []string{"https://", "http://"} {
		if len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			host = host[len(prefix):]
		}
	}
	return host + "/pnl/" + code
}

// Close releases all resources.
func (a *App) Close() {
	if a.journeys != nil && a.journeys.second != nil {
		a.journeys.second.Close()
	}
	if a.transport != nil {
		a.transport.Close()
	}
	if a.nats != nil {
		a.nats.Close()
	}
	if a.rpc != nil {
		a.rpc.Close()
	}
	if a.archiveRPC != nil {
		a.archiveRPC.Close()
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
}

// startBackgroundJobs launches the reconciler loop, draining backlog each tick
// until ctx is cancelled.
func (a *App) startBackgroundJobs(ctx context.Context) {
	go a.loop(ctx, a.reconcileIn, "pnl reconciler", func(c context.Context) error {
		// Drain until a tick yields nothing, so a backlog catches up promptly.
		for {
			n, err := a.reconciler.RunOnce(c)
			if err != nil {
				return err
			}
			if n == 0 {
				return nil
			}
			if c.Err() != nil {
				return c.Err()
			}
		}
	})
}

// loop runs fn on a ticker until ctx is cancelled, logging (not propagating)
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

// Run loads config, builds the App, serves HTTP, starts the reconciler, and
// blocks until a shutdown signal arrives.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("pnl", cfg.LogLevel)
	logger.Info("starting pnl service")

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	if cfg.RuntimeAction != "" {
		if cfg.RuntimeAction != "inventory_baseline" || cfg.NodeEnv != "production" || cfg.RuntimeProfile != "owned_local" || app.baseline == nil {
			return errors.New("pnl: unsupported runtime action")
		}
		if _, e := cfg.PrivateFixtureEvidence(); e != nil {
			return e
		}
		bounded, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		digest, e := app.baseline.Capture(bounded, cfg.BaselineWallet, cfg.BaselineToken)
		if e != nil {
			return e
		}
		logger.Info("inventory baseline captured", slog.String("checkpointDigest", string(digest)))
		return nil
	}
	if app.journeys != nil {
		app.startJourneyJobs(ctx)
	}
	if app.baselineAhead != nil {
		go func() {
			release, e := app.tracker.Acquire()
			if e != nil {
				return
			}
			defer release()
			app.loop(ctx, 250*time.Millisecond, "inventory baseline ahead", app.baselineAhead.Drain)
		}()
	}
	if app.accounting != nil {
		go func() {
			release, e := app.tracker.Acquire()
			if e != nil {
				return
			}
			defer release()
			if e := app.runAccounting(ctx); e != nil && !errors.Is(e, context.Canceled) {
				logger.Error("inventory consumer failed", slog.Any("err", e))
				app.tracker.BeginDrain()
				cancel()
			}
		}()
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

type inventoryRuntimeDocument struct {
	Schema            string                    `json:"schema"`
	SourceStream      string                    `json:"sourceStream"`
	ConsumerID        string                    `json:"consumerId"`
	Generation        string                    `json:"generation"`
	Owner             string                    `json:"owner"`
	ProjectionVersion string                    `json:"projectionVersion"`
	Epoch             int64                     `json:"epoch"`
	Region            string                    `json:"region"`
	Consumer          natsdriver.ConsumerConfig `json:"consumer"`
}

func (a *App) runAccounting(ctx context.Context) error {
	if a.fixtureAckBarrier == "" {
		return a.accounting.Run(ctx, a.transport)
	}
	return a.transport.Run(ctx, func(ctx context.Context, message *natsdriver.Message) error {
		return a.accounting.PrepareAndConsume(ctx, message.Subject, message.Wire, fixtureCommitAck{Message: message, Path: a.fixtureAckBarrier})
	})
}

type fixtureCommitAck struct {
	Message *natsdriver.Message
	Path    string
}

func (b fixtureCommitAck) Ack(ctx context.Context) error {
	marker, err := os.OpenFile(b.Path+".reached", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(marker, "%s %s %d\n", b.Message.EventID, b.Message.Stream, b.Message.Sequence)
	closeErr := marker.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, e := os.Lstat(b.Path + ".release"); e == nil {
			if !info.Mode().IsRegular() {
				return errors.New("fixture barrier release invalid")
			}
			return b.Message.Ack(ctx)
		} else if !os.IsNotExist(e) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type pnlJourneyDocument struct {
	Version                  int               `json:"version"`
	Realm                    string            `json:"realm"`
	FontDirectory            string            `json:"fontDirectory"`
	FontRegularSHA256        string            `json:"fontRegularSha256"`
	FontBoldSHA256           string            `json:"fontBoldSha256"`
	FontLicenseSHA256        string            `json:"fontLicenseSha256"`
	IdentityEndpoint         string            `json:"identityEndpoint"`
	IdentityIssuer           string            `json:"identityIssuer"`
	IdentityAudience         string            `json:"identityAudience"`
	IdentityPeerCertSHA256   string            `json:"identityPeerCertSha256"`
	IdentityCAFile           string            `json:"identityCaFile"`
	IdentityCASHA256         string            `json:"identityCaSha256"`
	IdentityClientCertFile   string            `json:"identityClientCertFile"`
	IdentityClientCertSHA256 string            `json:"identityClientCertSha256"`
	IdentityClientKeyFile    string            `json:"identityClientKeyFile"`
	IdentityClientKeySHA256  string            `json:"identityClientKeySha256"`
	LiveBatch                int               `json:"liveBatch"`
	ReleaseDigest            string            `json:"releaseDigest"`
	InterfaceDigest          string            `json:"interfaceDigest"`
	NodeIDs                  [2]string         `json:"nodeIds"`
	ExportsDirectory         string            `json:"exportsDirectory"`
	BrokerStream             string            `json:"brokerStream"`
	CDNZone                  string            `json:"cdnZone"`
	CDNOrigin                string            `json:"cdnOrigin"`
	RewardManifestDigest     string            `json:"rewardManifestDigest"`
	RewardRuntimeHashes      map[string]string `json:"rewardRuntimeHashes"`
	Programs                 []l18.Program     `json:"programs"`
	ApprovedCommunityWallets []string          `json:"approvedCommunityWallets"`
}
type pnlJourneys struct {
	accounting   *inventory.AccountingConsumer
	inventory    *inventory.Service
	live         *inventory.LiveTransferWorker
	shares       *l17.SharesV3
	shareWorker  l17.ShareWorkerV3
	second       *ethclient.Client
	portfolio    l17.Service
	rewardsHTTP  l18.HTTP
	rewards      *l18.JourneyService
	rewardLedger *l18.Ledger
	settlement   *l18.SettlementService
	referralHTTP referral.HTTP
	referrals    []*referral.ProductionService
	community    *l18.CommunityWatcher
	exports      l17.ExportWorker
	cards        l17.CardWorker
	relay        delivery.Relay
	document     pnlJourneyDocument
	sourceStream string
	generation   string
	pool         *pgxpool.Pool
	guard        func(context.Context, pgx.Tx) error
	schema       string
}

func newPnLJourneys(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, rdb *goredis.Client, rpc *ethclient.Client, nc *nats.Conn, baseline *inventory.BaselineAuthority, accounting *inventory.AccountingConsumer) (*pnlJourneys, error) {
	var d pnlJourneyDocument
	if err := config.ReadBoundDocument(cfg.JourneyConfigFile, cfg.JourneyConfigSHA256, &d); err != nil {
		return nil, err
	}
	if d.Version != 3 || d.Realm != cfg.DeploymentRealm || (d.Realm != "production" && d.Realm != "private_fixture") || len(d.ReleaseDigest) != 64 || len(d.InterfaceDigest) != 64 || d.BrokerStream == "" || d.IdentityEndpoint != cfg.IdentityURL || d.IdentityIssuer != "pnld" || d.IdentityAudience != "identity" || d.FontDirectory != cfg.FontDir || d.FontRegularSHA256 != fonts.RegularSHA256 || d.FontBoldSHA256 != fonts.BoldSHA256 || d.FontLicenseSHA256 != fonts.LicenseSHA256 || cfg.RPCURLFallback == "" || cfg.RPCURLFallback == cfg.RPCURL || d.ExportsDirectory == "" || !filepath.IsAbs(d.ExportsDirectory) || d.CDNOrigin == "" || (d.CDNZone == "") != (cfg.PortfolioCDNToken == "") {
		return nil, errors.New("pnl: complete bound journey authority configuration required")
	}
	for _, digest := range []string{d.ReleaseDigest, d.InterfaceDigest} {
		decoded, e := hex.DecodeString(digest)
		if e != nil || len(decoded) != 32 || strings.ToLower(digest) != digest {
			return nil, errors.New("pnl: journey release tuple invalid")
		}
	}
	origin, e := url.Parse(d.CDNOrigin)
	if e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("pnl: CDN authority invalid")
	}
	// cdnZone + PNL_CDN_TOKEN name a Cloudflare zone fronting cdnOrigin. With
	// both empty the public origin is served directly (no caching CDN), and a
	// purge completes when the origin itself answers 410 Gone.
	var cardPurger l17.CardPurger = l17.OriginPurger{PublicOrigin: d.CDNOrigin}
	var sharePurger l17.SharePurgerV3 = l17.OriginPurger{PublicOrigin: d.CDNOrigin}
	if d.CDNZone != "" {
		zone, eZone := hex.DecodeString(d.CDNZone)
		if eZone != nil || len(zone) != 16 || strings.ToLower(d.CDNZone) != d.CDNZone {
			return nil, errors.New("pnl: CDN authority invalid")
		}
		cardPurger = l17.CloudflarePurger{ZoneID: d.CDNZone, Token: cfg.PortfolioCDNToken, PublicOrigin: d.CDNOrigin}
		sharePurger = l17.CloudflareSharePurgerV3{ZoneID: d.CDNZone, Token: cfg.PortfolioCDNToken, PublicOrigin: d.CDNOrigin}
	}
	if d.FontDirectory == "" || !filepath.IsAbs(d.FontDirectory) || filepath.Clean(d.FontDirectory) != d.FontDirectory {
		return nil, errors.New("pnl: bound font directory required")
	}
	if e = fonts.Materialize(d.FontDirectory); e != nil {
		return nil, e
	}
	key, err := base64.RawURLEncoding.DecodeString(cfg.PortfolioCursorKey)
	if err != nil || len(key) < 32 {
		return nil, errors.New("pnl: cursor signing secret invalid")
	}
	var runtime inventoryRuntimeDocument
	if err = config.ReadBoundDocument(cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, &runtime); err != nil {
		return nil, err
	}
	guard, err := queue.NewEpochGuard("recovery", runtime.Epoch, runtime.Region)
	if err != nil {
		return nil, err
	}
	second, err := ethclient.DialContext(ctx, cfg.RPCURLFallback)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			second.Close()
		}
	}()
	policy := delivery.Policy{Subjects: map[string]bool{inventory.RebuildSubject: true, "portfolio.snapshot.v2": true}, EventTypes: map[string]bool{inventory.RebuildSubject: true, "portfolio.snapshot.v2": true}}
	outbox, err := delivery.NewStoreWithEpochGuard(pool, runtime.Schema, policy, guard.Assert)
	if err != nil {
		return nil, err
	}
	inventoryService, err := inventory.NewService(baseline, second, d.NodeIDs, d.ReleaseDigest, outbox)
	if err != nil {
		return nil, err
	}
	admission, err := l17.NewAdmission(32, 2)
	if err != nil {
		return nil, err
	}
	repository := l17.Repository{Pool: pool, ReadAdmission: admission}
	accounting, err = accounting.WithFullPortfolio(repository, outbox, "portfolio.snapshot.v2")
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(d.ExportsDirectory, 0700); err != nil {
		return nil, err
	}
	directory, err := os.Lstat(d.ExportsDirectory)
	if err != nil || !directory.IsDir() || directory.Mode().Perm()&0077 != 0 {
		return nil, errors.New("pnl: private export directory required")
	}
	identity, err := pnlIdentityAuthority(cfg, d)
	if err != nil {
		return nil, err
	}
	live, err := inventory.NewLiveTransferWorker(inventoryService, repository, identity, d.LiveBatch)
	if err != nil {
		return nil, err
	}
	shares, err := l17.NewSharesV3(repository, identity, key, d.Realm, d.ReleaseDigest, d.InterfaceDigest, time.Now)
	if err != nil {
		return nil, err
	}
	portfolio := l17.Service{Repository: repository, Authority: identity, Cache: &l17.RevisionCache{Client: rdb, TTL: 5 * time.Second}, CursorKey: key, ExportsDir: d.ExportsDirectory}
	creator, err := l18.NewIndexedCreator(ctx, rpc.Client(), pool, runtime.SourceStream, time.Now, d.RewardManifestDigest, d.RewardRuntimeHashes)
	if err != nil {
		return nil, err
	}
	rewardAuthority, err := l18.NewCanonicalAuthority(pool, runtime.Schema, creator, d.Programs)
	if err != nil {
		return nil, err
	}
	ledger, err := l18.NewRuntimeLedger(pool, runtime.Schema, "pnld", rewardAuthority, guard.Assert)
	if err != nil {
		return nil, err
	}
	rewards, err := l18.NewJourneyService(ledger, creator, key, time.Now, d.ReleaseDigest, d.InterfaceDigest)
	if err != nil {
		return nil, err
	}
	rewards, err = rewards.WithSourceStream(runtime.SourceStream)
	if err != nil {
		return nil, err
	}
	settlement, err := l18.NewSettlementService(ledger, rewardAuthority)
	if err != nil {
		return nil, err
	}
	referralMAC := hmac.New(sha256.New, key)
	referralMAC.Write([]byte("pnl-referral-attribution-v1"))
	referralKey := referralMAC.Sum(nil)
	attribution := map[string]*l18.Attribution{}
	for _, program := range d.Programs {
		binding, e := l18.NewAttribution(ledger, referralKey, time.Now, program.ID, program.Version)
		if e != nil {
			return nil, e
		}
		attribution[program.ID+":"+program.Version] = binding
	}
	community, err := l18.NewCommunityWatcher(rewards, d.ApprovedCommunityWallets)
	if err != nil {
		return nil, err
	}
	rewardsHTTP := l18.HTTP{Service: rewards, Authenticate: func(request *http.Request) (l18.Actor, error) {
		principal, e := identity.AuthenticateRequest(request.Context(), request)
		if e != nil {
			return l18.Actor{}, e
		}
		return l18.Actor{Wallet: principal.Wallet, Roles: map[string]bool{}}, nil
	}}
	referralServices := make([]*referral.ProductionService, 0, len(d.Programs))
	for _, program := range d.Programs {
		service, e := referral.NewProductionService(pool, ledger, rewardAuthority, attribution[program.ID+":"+program.Version], program.ID, program.Version)
		if e != nil {
			return nil, e
		}
		referralServices = append(referralServices, service)
	}
	referralHTTP, err := referral.NewHTTP(referralServices, rewardsHTTP.Authenticate)
	if err != nil {
		return nil, err
	}
	publisher, err := natsdriver.NewPublisher(nc, d.BrokerStream)
	if err != nil {
		return nil, err
	}
	relay := delivery.Relay{Store: outbox, Publisher: publisher, Owner: runtime.Owner, Batch: 16, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 8}
	owned = true
	return &pnlJourneys{accounting: accounting, inventory: inventoryService, live: live, shares: shares, shareWorker: l17.ShareWorkerV3{Repository: repository, Owner: runtime.Owner, FontDir: d.FontDirectory, Purger: sharePurger}, second: second, portfolio: portfolio, rewardsHTTP: rewardsHTTP, rewards: rewards, rewardLedger: ledger, settlement: settlement, community: community, referralHTTP: referralHTTP, referrals: referralServices, exports: l17.ExportWorker{Repository: repository, Directory: d.ExportsDirectory, Owner: runtime.Owner}, cards: l17.CardWorker{Repository: repository, Owner: runtime.Owner, FontDir: cfg.FontDir, Purger: cardPurger}, relay: relay, document: d, sourceStream: runtime.SourceStream, generation: runtime.Generation, pool: pool, guard: guard.Assert, schema: runtime.Schema}, nil
}

func (a *App) startJourneyJobs(ctx context.Context) {
	jobs := []struct {
		name     string
		interval time.Duration
		run      func(context.Context) error
	}{
		{"portfolio outbox", 100 * time.Millisecond, a.journeys.relay.RunOnce},
		{"portfolio exports", time.Second, a.journeys.exports.RunOnce},
		{"portfolio export cleanup", time.Minute, a.journeys.exports.CleanupOnce},
		{"portfolio cards", time.Second, a.journeys.cards.RunOnce},
		{"live canonical wallet transfers", time.Second, a.journeys.rebuildOnce},
		{"selected portfolio share render", time.Second, a.journeys.shareWorker.RunOnce},
		{"selected portfolio share expiry", time.Second, a.journeys.shareWorker.ExpireOnce},
		{"reward settlement", 10 * time.Second, a.journeys.settleOnce},
		{"canonical referral accrual", time.Second, a.journeys.referralOnce},
		{"canonical cashback accrual", time.Second, a.journeys.cashbackOnce},
		{"community fee ownership", time.Second, a.journeys.community.RunOnce},
	}
	for _, job := range jobs {
		job := job
		go func() {
			release, err := a.tracker.Acquire()
			if err != nil {
				return
			}
			defer release()
			a.loop(ctx, job.interval, job.name, job.run)
		}()
	}
}

func (j *pnlJourneys) rebuildOnce(ctx context.Context) error {
	_, err := j.live.RunOnce(ctx)
	return err
}
func (j *pnlJourneys) settleOnce(ctx context.Context) error {
	rows, err := j.pool.Query(ctx, `SELECT DISTINCT award_id FROM pnl.l18_payout_attempts WHERE state IN ('prepared','pending_investigation') ORDER BY award_id LIMIT 32`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = j.settlement.Reconcile(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (j *pnlJourneys) rebuildHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	principal, err := j.portfolio.Authority.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body struct{}
	if err = decoder.Decode(&body); err != nil {
		http.Error(w, "Full wallet rebuild requires an empty object", 400)
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		http.Error(w, "Invalid request body", 400)
		return
	}
	summary, err := j.live.RebuildOwned(r.Context(), principal)
	if err != nil {
		http.Error(w, "Verified full wallet replay unavailable", 503)
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Version int         `json:"version"`
		Summary l17.Summary `json:"summary"`
	}{3, summary})
}

func (j *pnlJourneys) intervalHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	principal, err := j.portfolio.Authority.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	for key, values := range q {
		if (key != "from" && key != "to" && key != "interval" && key != "limit") || len(values) != 1 {
			http.Error(w, "Invalid interval query", 422)
			return
		}
	}
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		http.Error(w, "Invalid UTC interval start", 422)
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		http.Error(w, "Invalid UTC interval end", 422)
		return
	}
	_, fromOffset := from.Zone()
	_, toOffset := to.Zone()
	if fromOffset != 0 || toOffset != 0 {
		http.Error(w, "UTC boundaries required", 422)
		return
	}
	limit := 50
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			http.Error(w, "Invalid interval limit", 422)
			return
		}
	}
	interval := q.Get("interval")
	if interval == "" {
		interval = "1d"
	}
	result, err := j.inventory.IntervalHistory(r.Context(), principal.Wallet, from, to, interval, limit)
	if err != nil {
		status := 503
		if errors.Is(err, l17.ErrInvalid) {
			status = 422
		}
		http.Error(w, "Verified interval history unavailable", status)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (j *pnlJourneys) referralOnce(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, service := range j.referrals {
		if err := service.RunOnce(bounded); err != nil {
			return err
		}
	}
	return nil
}

func pnlBoundAuthorityFile(path, digest string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(digest) != 64 {
		return nil, errors.New("pnl: identity file binding missing")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1048576 {
		return nil, errors.New("pnl: identity file unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return nil, errors.New("pnl: identity file binding mismatch")
	}
	return raw, nil
}
func pnlIdentityAuthority(cfg config.Config, d pnlJourneyDocument) (auth.MTLSIdentityAuthority, error) {
	rootsPEM, err := pnlBoundAuthorityFile(d.IdentityCAFile, d.IdentityCASHA256)
	if err != nil {
		return auth.MTLSIdentityAuthority{}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootsPEM) {
		return auth.MTLSIdentityAuthority{}, errors.New("pnl: identity trust roots invalid")
	}
	certPEM, err := pnlBoundAuthorityFile(d.IdentityClientCertFile, d.IdentityClientCertSHA256)
	if err != nil {
		return auth.MTLSIdentityAuthority{}, err
	}
	keyPEM, err := pnlBoundAuthorityFile(d.IdentityClientKeyFile, d.IdentityClientKeySHA256)
	if err != nil {
		return auth.MTLSIdentityAuthority{}, err
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return auth.MTLSIdentityAuthority{}, errors.New("pnl: identity client certificate invalid")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(cfg.IdentitySigningKey)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return auth.MTLSIdentityAuthority{}, errors.New("pnl: identity signing key invalid")
	}
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !hmac.Equal(key, derived) {
		return auth.MTLSIdentityAuthority{}, errors.New("pnl: identity signing key invalid")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, MaxIdleConns: 16, MaxIdleConnsPerHost: 16, ResponseHeaderTimeout: time.Second}
	signer := auth.ServiceRequestIssuer{Issuer: d.IdentityIssuer, Audience: d.IdentityAudience, Key: ed25519.PrivateKey(key), Transport: auth.AssertionTransport{AssertionHeader: auth.IdentityAssertionHeader, SignatureHeader: auth.IdentitySignatureHeader}}
	return auth.NewMTLSIdentityAuthority(d.IdentityEndpoint, d.IdentityPeerCertSHA256, &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, signer)
}
func (j *pnlJourneys) enrollHTTP(w http.ResponseWriter, r *http.Request) {
	principal, err := j.portfolio.Authority.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.Error(w, "Authentication required", 401)
		return
	}
	if err = j.live.EnqueueOwned(r.Context(), principal); err != nil {
		http.Error(w, "Verified enrollment unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusAccepted)
}
func (j *pnlJourneys) cancelLiveHTTP(w http.ResponseWriter, r *http.Request) {
	principal, err := j.portfolio.Authority.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.Error(w, "Authentication required", 401)
		return
	}
	if err = j.live.CancelOwned(r.Context(), principal); err != nil {
		http.Error(w, "Verified cancellation unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (j *pnlJourneys) cashbackOnce(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, service := range j.referrals {
		if err := service.CashbackRunOnce(bounded); err != nil {
			return err
		}
	}
	return nil
}
