// Package app wires the media/user service together (config, migrations,
// pgxpool, redis, R2 object store, HTTP router, health, graceful shutdown) and
// owns its lifecycle. Keeping the wiring here (rather than in main) makes the
// whole service testable end-to-end against external test servers + MinIO.
package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/account"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"io"
	"log/slog"
	"net/http"
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

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/user"
)

// rateLimitMax / rateLimitWindow mirror the TS registerRateLimit(app, {max:60,
// windowSeconds:60}) configuration for the users service.
const (
	rateLimitMax    = 60
	rateLimitWindow = 60 * time.Second
)

// App is a fully-wired, ready-to-serve user service.
type App struct {
	secretStop  context.CancelFunc
	accountStop context.CancelFunc
	accountDone chan struct{}
	accountNATS *nats.Conn
	accountRPC  *sharedauth.ReportedHashBackend
	TLS         *tls.Config
	tracker     *process.Tracker
	Router      http.Handler

	pool   *pgxpool.Pool
	redis  *goredis.Client
	logger *slog.Logger
}

// New runs migrations, opens the DB + Redis pools + R2 client, and builds the
// HTTP router with health endpoints and rate-limited user routes.
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
		metrics, e = telemetry.New("userd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
		if e != nil {
			return nil, e
		}
	}
	accountHealth := &accountRuntimeHealth{}
	secretChecks := map[string]sharedhttp.Check{}
	if cfg.NodeEnv == "production" {
		secretChecks["account_workers"] = accountHealth.Check
	}
	if secretCheck != nil {
		secretChecks["secrets"] = secretCheck
	}

	if cfg.NodeEnv != "production" {
		if err := migrate.Up(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}

	pool, err := cfg.OpenRuntimePool(ctx, "userd", shareddb.PoolOptions{})
	if err != nil {
		return nil, err
	}

	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, err
	}

	store, err := storage.New(ctx, storage.Config{
		Endpoint:        cfg.S3Endpoint,
		Region:          cfg.S3Region,
		AccessKeyID:     cfg.S3AccessKeyID,
		SecretAccessKey: cfg.S3SecretAccessKey,
		Bucket:          cfg.S3Bucket,
	})
	if err != nil {
		pool.Close()
		_ = rdb.Close()
		return nil, err
	}

	handlers := user.New(user.Deps{
		Queries:   sqlcdb.New(pool),
		Redis:     rdb,
		Store:     store,
		PublicURL: cfg.PublicURL,
		MaxAvatar: cfg.MaxAvatarSizeBytes,
		MaxBanner: cfg.MaxBannerSizeBytes,
		Logger:    logger,
	})

	mux := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins: cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})
	// Rate-limit only the user routes (health probes stay un-throttled, i12).
	mux.Group(func(gr chi.Router) {
		gr.Use(sharedhttp.RateLimit(sharedhttp.RateLimitOptions{
			Max:    rateLimitMax,
			Window: rateLimitWindow,
			Redis:  rdb,
		}))
		if cfg.NodeEnv != "production" {
			handlers.RegisterRoutes(gr)
			registerDevelopmentProfileReads(gr, sqlcdb.New(pool))
			registerDevelopmentProfileWrites(gr, cfg, pool, rdb, logger)
		}
	})

	var accountsTLS *tls.Config
	var accountStop context.CancelFunc
	var accountDone chan struct{}
	var accountNATS *nats.Conn
	var accountRPC *sharedauth.ReportedHashBackend
	var committedBlocks sharedauth.CommittedBlockProvider
	if cfg.NodeEnv == "production" {
		if err = cfg.ValidateAccountRuntime(); err != nil {
			pool.Close()
			_ = rdb.Close()
			return nil, err
		}
		accountsTLS, err = accountTLS(cfg)
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			return nil, err
		}
		committedBlocks = sharedauth.PostgresCommittedBlockProvider{Pool: pool}
		accountRPC, err = dialAccountChain(ctx, cfg.AccountRPCURL)
		if err != nil {
			pool.Close()
			_ = rdb.Close()
			return nil, err
		}
		natsAuth, e := accountNATSOptions(cfg)
		if e != nil {
			accountRPC.Close()
			pool.Close()
			_ = rdb.Close()
			return nil, e
		}
		accountNATS, err = nats.Connect(cfg.AccountNATSURL, append(natsAuth, nats.Name("userd-"+cfg.RuntimeInstanceID), nats.Timeout(2*time.Second), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second))...)
		if err != nil {
			accountRPC.Close()
			pool.Close()
			_ = rdb.Close()
			return nil, err
		}
		setupFailure := func(e error) (*App, error) {
			accountNATS.Close()
			accountRPC.Close()
			pool.Close()
			_ = rdb.Close()
			return nil, e
		}
		publisher, e := natsdriver.NewPublisher(accountNATS, cfg.AccountNATSStream)
		if e != nil {
			return setupFailure(e)
		}
		var subjects map[string]string
		if e = config.ReadBoundDocument(cfg.AccountSubjectsFile, cfg.AccountSubjectsSHA256, &subjects); e != nil {
			return setupFailure(e)
		}
		policy := delivery.Policy{Subjects: map[string]bool{}, EventTypes: map[string]bool{}}
		for kind, subject := range subjects {
			if subject == "" || policy.Subjects[subject] {
				return setupFailure(fmt.Errorf("account_subject_configuration_invalid"))
			}
			policy.Subjects[subject] = true
			policy.EventTypes[kind] = true
		}
		durable, e := delivery.NewStore(pool, cfg.AccountDeliverySchema, policy)
		if e != nil {
			return setupFailure(e)
		}
		cursor, e := config.DecodeAccountKey(cfg.AccountCursorKey)
		if e != nil {
			return setupFailure(e)
		}
		exportKey, e := config.DecodeAccountKey(cfg.AccountExportKey)
		if e != nil {
			return setupFailure(e)
		}
		endpointKey, e := config.DecodeAccountKey(cfg.AccountEndpointKey)
		if e != nil {
			return setupFailure(e)
		}
		fingerprintKey, e := config.DecodeAccountKey(cfg.AccountEndpointFingerprintKey)
		if e != nil {
			return setupFailure(e)
		}
		auditKey, e := config.DecodeAccountKey(cfg.AccountAuditKey)
		if e != nil {
			return setupFailure(e)
		}
		signingSeed, e := config.DecodeAccountKey(cfg.AccountServiceSigningSeed)
		if e != nil {
			return setupFailure(e)
		}
		proofKey, e := config.DecodeAccountKey(cfg.AccountEndpointProofPublicKey)
		if e != nil {
			return setupFailure(e)
		}
		gatewayKey, e := config.DecodeAccountKey(cfg.AccountGatewayPublicKey)
		if e != nil {
			return setupFailure(e)
		}
		privateObjects, e := storage.New(ctx, storage.Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, AccessKeyID: cfg.S3AccessKeyID, SecretAccessKey: cfg.S3SecretAccessKey, Bucket: cfg.AccountExportBucket})
		if e != nil {
			return setupFailure(e)
		}
		policies, routes := sharedauth.AccountOwnerPoliciesV2()
		operations := sharedauth.OperationAuthorizer{Ledger: sharedauth.Ledger{Pool: pool, Environment: cfg.AccountEnvironment, AuditKey: auditKey}, Verifier: sharedauth.CommittedProofVerifier{Backend: accountRPC, Blocks: committedBlocks}, Policies: policies, Routes: routes, Transport: sharedauth.OperationTransport{EnvelopeHeader: "X-Kindle-Operation", SignatureHeader: "X-Kindle-Operation-Signature"}}
		userStore := account.Store{DB: pool, Clock: time.Now}
		notificationClient, e := pinnedAccountClient(accountsTLS, cfg.NotificationCertSHA256)
		if e != nil {
			return setupFailure(e)
		}
		signer := sharedauth.ServiceRequestIssuer{Issuer: "user", Audience: "identity", Key: ed25519.NewKeyFromSeed(signingSeed), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
		identityClient, e := pinnedAccountClient(accountsTLS, cfg.IdentityCertSHA256)
		if e != nil {
			return setupFailure(e)
		}
		identityAuthority, e := sharedauth.NewMTLSIdentityAuthority(cfg.IdentityURL, cfg.IdentityCertSHA256, identityClient, signer)
		if e != nil {
			return setupFailure(e)
		}
		metadataClient, e := pinnedAccountClient(accountsTLS, cfg.MetadataCertSHA256)
		if e != nil {
			return setupFailure(e)
		}
		metadataSigner := signer
		metadataSigner.Audience = "metadata"
		assets := accountAssetsAuthority{Store: userStore, Client: metadataClient, Endpoint: cfg.MetadataServiceURL, Signer: metadataSigner}

		recoveryVerifier := sharedauth.ServiceRequestVerifier{Issuer: "gateway", Audience: cfg.AccountAssertionAudience, PeerCertSHA256: cfg.AccountGatewayCertSHA256, Key: ed25519.PublicKey(gatewayKey), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
		runtime := account.Runtime{RecoveryGatewayVerifier: &recoveryVerifier, IdentityAuthority: identityAuthority, Assets: assets, ExtendedV3: cfg.AccountEnableExtendedV3, UseReadSwitch: cfg.AccountUseReadSwitch, AllowMetadataAvatars: cfg.AccountAllowMetadataAvatars, Authorizer: &operations, Objects: privateObjects, ExportKey: exportKey, EndpointKey: endpointKey, EndpointFingerprintKey: fingerprintKey, EndpointVerifier: account.CompactEndpointProofVerifier(ed25519.PublicKey(proofKey)), DeviceVerifier: account.CompactDeviceProofVerifier(ed25519.PublicKey(proofKey)), DownloadBaseURL: cfg.AccountDownloadBaseURL, Delivery: durable, Subjects: subjects, Metrics: &account.Metrics{}, DeletionOwners: map[string]account.DeletionOwner{"images": accountImageDeletionOwner{Redis: rdb, Legacy: account.ImagePurger{Store: userStore, Objects: store, Exports: privateObjects}, Assets: assets}, "notifications": notificationDeletionOwner{Client: notificationClient, Endpoint: cfg.NotificationServiceURL, Token: cfg.NotificationServiceToken, PeerCertSHA256: cfg.NotificationCertSHA256}}}
		accounts, e := account.NewConnectedHTTP(userStore, runtime, cursor)
		if e != nil {
			return setupFailure(e)
		}
		assertions := sharedauth.ServiceAssertionVerifier{Transport: sharedauth.AssertionTransport{AssertionHeader: cfg.AccountAssertionHeader, SignatureHeader: cfg.AccountAssertionSignatureHeader}, Issuer: cfg.AccountAssertionIssuer, Audience: cfg.AccountAssertionAudience, PeerCertSHA256: cfg.AccountGatewayCertSHA256, Key: ed25519.PublicKey(gatewayKey)}
		accountRoutes := accounts.GatewayHandler(assertions, sharedauth.PostgresIdentityResolver{Pool: pool}, operations)
		mux.Handle("/v1/*", accountRoutes)
		mux.Handle("/v2/*", accountRoutes)
		mux.Handle("/v3/*", accountRoutes)
		internalAccounts := accountNotificationAuthority(userStore, accounts.Runtime, cfg.NotificationInboundToken, cfg.NotificationCertSHA256)
		mux.Handle("/internal/v2/accounts/*", internalAccounts)
		mux.Handle("/internal/v2/wallets/*", internalAccounts)
		workerContext, stop := context.WithCancel(ctx)
		accountStop = stop
		accountDone = make(chan struct{})
		worker := account.Worker{Store: userStore, Runtime: accounts.Runtime, Owner: cfg.RuntimeInstanceID, LeaseDuration: time.Minute}
		relay := delivery.Relay{Store: durable, Publisher: publisher, Owner: cfg.RuntimeInstanceID, Batch: 16, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 20}
		go runAccountWorkers(workerContext, worker, relay, accountNATS, accountHealth, accountDone, logger)
	}

	if metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{accountStop: accountStop, accountDone: accountDone, accountNATS: accountNATS, accountRPC: accountRPC, TLS: accountsTLS, secretStop: secretStop, tracker: tracker, Router: runtimeHandler(tracker, mux, metrics), pool: pool, redis: rdb, logger: logger}, nil
}

// registerDevelopmentProfileReads mounts the public profile reads the web
// calls (GET /v1|/v2|/v3/profiles/{wallet}, GET /v2/profiles:batch) outside
// production. Production serves these paths from the account runtime over
// account_state (below); development has no account runtime, so the same
// frozen contracts are projected from the legacy users.user_profiles row that
// POST /users/{wallet} writes. The owner write is registerDevelopmentProfileWrites.
func registerDevelopmentProfileReads(r chi.Router, queries sqlcdb.Querier) {
	reads := account.DevelopmentProfileReads{Queries: queries, Clock: time.Now}
	handler := reads.Handler()
	for _, route := range reads.Routes() {
		r.Method(http.MethodGet, route, handler)
	}
}

// registerDevelopmentProfileWrites mounts the owner profile write the web's
// profile editor issues through the gateway (PATCH /v3/profiles/{wallet})
// outside production, writing the legacy users.user_profiles row the
// development reads project. The caller is authenticated against the
// gateway's v2 identity (IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN, >= 32
// chars); without them the route answers 503 DEPENDENCY_UNAVAILABLE.
func registerDevelopmentProfileWrites(r chi.Router, cfg config.Config, pool *pgxpool.Pool, rdb *goredis.Client, logger *slog.Logger) {
	var identity sharedauth.RequestIdentityAuthority
	if strings.TrimSpace(cfg.IdentityURL) != "" && len(cfg.IdentityServiceToken) >= 32 {
		identity = sharedauth.HTTPIdentityAuthority{Endpoint: strings.TrimRight(strings.TrimSpace(cfg.IdentityURL), "/"), ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: true}
	} else if logger != nil {
		logger.Warn("development profile writes disabled: IDENTITY_SERVICE_URL and IDENTITY_SERVICE_TOKEN (>= 32 chars) are required")
	}
	writes := account.DevelopmentProfileWrites{
		DB:       pool,
		Reads:    account.DevelopmentProfileReads{Queries: sqlcdb.New(pool), Clock: time.Now},
		Identity: identity,
		Invalidate: func(ctx context.Context, wallet string) error {
			return sharedredis.CacheInvalidate(ctx, rdb, "user:json:"+wallet)
		},
		Clock: time.Now,
	}
	handler := writes.Handler()
	for _, route := range writes.Routes() {
		r.Method(http.MethodPatch, route, handler)
	}
}

// Close releases the DB and Redis pools.
func (a *App) Close() {
	if a.accountStop != nil {
		a.accountStop()
	}
	if a.accountDone != nil {
		select {
		case <-a.accountDone:
		case <-time.After(5 * time.Second):
		}
	}
	if a.accountNATS != nil {
		a.accountNATS.Close()
	}
	if a.accountRPC != nil {
		a.accountRPC.Close()
	}
	if a.secretStop != nil {
		a.secretStop()
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
	logger := sharedlog.New("user", cfg.LogLevel)
	logger.Info("starting user service")

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
		TLSConfig:         app.TLS,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.Port))
		serve := func() error {
			if app.TLS != nil {
				return srv.ListenAndServeTLS("", "")
			}
			return srv.ListenAndServe()
		}
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			cancel()
		}
	}()

	shutErr := process.Run(ctx, process.Options{Tracker: app.tracker,
		Logger: logger,
		OnShutdown: func(sctx context.Context) error {
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

type accountRuntimeHealth struct {
	mu      sync.Mutex
	last    time.Time
	failure error
}

func (h *accountRuntimeHealth) record(e error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = time.Now()
	h.failure = e
}
func (h *accountRuntimeHealth) Check(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last.IsZero() || time.Since(h.last) > 30*time.Second {
		return errors.New("account_worker_not_ready")
	}
	return h.failure
}
func runAccountWorkers(ctx context.Context, worker account.Worker, relay delivery.Relay, nc *nats.Conn, health *accountRuntimeHealth, done chan struct{}, logger *slog.Logger) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextPurge := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cycle, cancel := context.WithTimeout(ctx, 20*time.Second)
			e := worker.RunOnce(cycle)
			if errors.Is(e, pgx.ErrNoRows) {
				e = nil
			}
			if relayError := relay.RunOnce(cycle); relayError != nil {
				e = errors.Join(e, relayError)
			}
			if time.Now().After(nextPurge) {
				if purgeError := worker.PurgeExpiredExports(cycle, 16); purgeError != nil {
					e = errors.Join(e, purgeError)
				}
				nextPurge = time.Now().Add(time.Minute)
			}
			if !nc.IsConnected() {
				e = errors.Join(e, errors.New("account_broker_disconnected"))
			}
			cancel()
			health.record(e)
			if e != nil && logger != nil {
				logger.Error("account worker cycle failed", slog.String("class", "account_runtime"))
			}
		}
	}
}
func accountTLS(cfg config.Config) (*tls.Config, error) {
	cert, roots, e := materialTLS(cfg.AccountTLSCertFile, cfg.AccountTLSCertPEM, cfg.AccountTLSKeyFile, cfg.AccountTLSKeyPEM, cfg.AccountTLSCAFile, cfg.AccountTLSCAPEM)
	if e != nil {
		return nil, e
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}
func pinnedAccountClient(config *tls.Config, pin string) (*http.Client, error) {
	if config == nil || len(pin) != 64 || config.RootCAs == nil || len(config.Certificates) == 0 || config.InsecureSkipVerify {
		return nil, errors.New("account_peer_configuration_invalid")
	}
	parsed, e := hex.DecodeString(pin)
	if e != nil || len(parsed) != 32 {
		return nil, errors.New("account_peer_configuration_invalid")
	}
	clientTLS := config.Clone()
	clientTLS.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return sharedauth.ErrScopeDenied
		}
		digest := sha256.Sum256(state.PeerCertificates[0].Raw)
		if subtle.ConstantTimeCompare(digest[:], parsed) != 1 {
			return sharedauth.ErrScopeDenied
		}
		return nil
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: clientTLS, MaxIdleConns: 16, MaxIdleConnsPerHost: 8, ResponseHeaderTimeout: 5 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type accountAssetsAuthority struct {
	Store    account.Store
	Client   *http.Client
	Endpoint string
	Signer   sharedauth.ServiceRequestIssuer
}

func (a accountAssetsAuthority) call(ctx context.Context, method, path string, out any) error {
	return a.callDecoded(ctx, method, path, func(raw []byte) error { return account.Decode(bytes.NewReader(raw), out) })
}

// callDecoded performs one signed, pinned request to the metadata account
// asset authority and hands the bounded 200 body to decode.
func (a accountAssetsAuthority) callDecoded(ctx context.Context, method, path string, decode func([]byte) error) error {
	if a.Client == nil || a.Endpoint == "" {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Endpoint, "/")+path, nil)
	if e != nil {
		return e
	}
	r.Header.Set("X-Request-ID", uuid.NewString())
	if e = a.Signer.SignRequest(r, nil); e != nil {
		return e
	}
	response, e := a.Client.Do(r)
	if e != nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return account.ErrMissing
	}
	if response.StatusCode == 403 {
		return account.ErrDenied
	}
	if response.StatusCode != 200 {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 16385))
	if e != nil || len(raw) > 16384 {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	return decode(raw)
}

// metadataAccountAsset is the metadata account asset authority's wire answer
// (media/metadata WireAsset). publicUrl is null until the asset is approved.
type metadataAccountAsset struct {
	AssetID     string  `json:"assetId"`
	OwnerWallet string  `json:"ownerWallet"`
	SHA256      string  `json:"sha256"`
	MediaType   string  `json:"mediaType"`
	Bytes       int64   `json:"bytes"`
	State       string  `json:"state"`
	PublicURL   *string `json:"publicUrl"`
}

// decodeMetadataAccountAsset strictly decodes one WireAsset: unknown fields,
// duplicate keys and trailing data are refused, while the null publicUrl of a
// not-yet-approved asset is a valid answer (the caller denies it), not the
// FIELD_INVALID that account.Decode reserves for client bodies.
func decodeMetadataAccountAsset(raw []byte, out *metadataAccountAsset) error {
	if _, e := sharedauth.BodyDigest(raw); e != nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(out); e != nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	return nil
}
func (a accountAssetsAuthority) ApprovedAccountAsset(ctx context.Context, wallet, id string) error {
	owner, e := account.Wallet(wallet)
	if e != nil {
		return e
	}
	asset, e := uuid.Parse(id)
	if e != nil || asset == uuid.Nil || asset.String() != id {
		return account.ErrInvalid
	}
	var out metadataAccountAsset
	if e = a.callDecoded(ctx, "GET", "/internal/v2/accounts/"+owner+"/assets/"+id, func(raw []byte) error { return decodeMetadataAccountAsset(raw, &out) }); e != nil {
		return e
	}
	return approvedAccountAsset(out, owner, id)
}

// approvedAccountAsset admits an asset as a profile avatar/banner only when the
// metadata authority reports it approved, owned by the wallet, with an
// immutable public URL and a raster media type.
func approvedAccountAsset(out metadataAccountAsset, owner, id string) error {
	if out.AssetID != id || out.OwnerWallet != owner || out.State != "approved" || len(out.SHA256) != 66 || !strings.HasPrefix(out.SHA256, "0x") || out.Bytes < 1 || out.PublicURL == nil || account.SafeLink(*out.PublicURL) != nil {
		return account.ErrDenied
	}
	switch out.MediaType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return account.ErrDenied
	}
	return nil
}
func (a accountAssetsAuthority) DeleteAccount(ctx context.Context, id, deletion uuid.UUID) error {
	var wallet string
	var deleted bool
	if e := a.Store.DB.QueryRow(ctx, `SELECT wallet,deleted FROM account_state WHERE account_id=$1`, id).Scan(&wallet, &deleted); e != nil {
		return e
	}
	if !deleted {
		return account.ErrState
	}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if e := a.call(ctx, "DELETE", "/internal/v2/accounts/"+wallet+"/assets?deletionId="+deletion.String(), &out); e != nil {
		return e
	}
	if !out.Deleted {
		return account.ErrState
	}
	return nil
}

type accountImageDeletionOwner struct {
	Redis  *goredis.Client
	Legacy account.ImagePurger
	Assets accountAssetsAuthority
}

func (o accountImageDeletionOwner) DeleteAccount(ctx context.Context, id, deletion uuid.UUID) error {
	if e := o.Assets.DeleteAccount(ctx, id, deletion); e != nil {
		return e
	}
	if e := o.Legacy.DeleteAccount(ctx, id, deletion); e != nil {
		return e
	}
	var wallet string
	if e := o.Legacy.Store.DB.QueryRow(ctx, `SELECT wallet FROM account_state WHERE account_id=$1`, id).Scan(&wallet); e != nil {
		return e
	}
	if o.Redis == nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	return o.Redis.Del(ctx, "user:json:"+wallet).Err()
}

type notificationDeletionOwner struct {
	Client                          *http.Client
	Endpoint, Token, PeerCertSHA256 string
}

func (o notificationDeletionOwner) DeleteAccount(ctx context.Context, id, deletion uuid.UUID) error {
	if o.Client == nil || len(o.Token) < 32 || id == uuid.Nil || deletion == uuid.Nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	r, e := http.NewRequestWithContext(ctx, "DELETE", strings.TrimRight(o.Endpoint, "/")+"/internal/v2/accounts/"+id.String()+"/notification-data?deletionId="+deletion.String(), nil)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+o.Token)
	response, e := o.Client.Do(r)
	if e != nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if e = account.Decode(io.LimitReader(response.Body, 16385), &out); e != nil {
		return e
	}
	if !out.Deleted {
		return account.ErrState
	}
	return nil
}

type accountPushTarget struct {
	AccountID, DeviceInstallationID, Provider, EncryptedEndpointRef, Status string
	ConsentRevision, Revision                                               uint64
	LastSeen                                                                time.Time
}
type accountDeliveryPreference struct {
	AccountID, Timezone, Language string
	Revision                      uint64
	Categories, Channels          map[string]bool
	CategoryChannels              map[string]map[string]bool
	QuietHours                    *struct{ StartMinute, EndMinute int }
	Linked                        bool
}

func accountNotificationAuthority(store account.Store, runtime *account.Runtime, token, pin string) http.Handler {
	mux := http.NewServeMux()
	target := func(ctx context.Context, id, device uuid.UUID) (accountPushTarget, []byte, error) {
		var out accountPushTarget
		var raw, fingerprint []byte
		var revoked, installationRevoked bool
		var revision int64
		e := store.DB.QueryRow(ctx, `SELECT e.provider,e.fingerprint,e.ciphertext,e.consent_revision,e.validated_at,e.revoked,a.revision,i.revoked FROM account_endpoint e JOIN account_state a USING(account_id) JOIN account_device_installation i ON i.installation_id=e.device_id AND i.account_id=e.account_id WHERE e.account_id=$1 AND e.device_id=$2 AND NOT a.deleted`, id, device).Scan(&out.Provider, &fingerprint, &raw, &out.ConsentRevision, &out.LastSeen, &revoked, &revision, &installationRevoked)
		out.AccountID = id.String()
		out.DeviceInstallationID = device.String()
		out.EncryptedEndpointRef = hex.EncodeToString(fingerprint)
		out.Revision = uint64(revision)
		out.Status = "active"
		if revoked || installationRevoked || out.LastSeen.Before(time.Now().Add(-30*24*time.Hour)) {
			out.Status = "revoked"
		}
		return out, raw, e
	}
	requestAccount := func(r *http.Request) (uuid.UUID, error) {
		id, e := uuid.Parse(r.PathValue("accountId"))
		if e != nil || id == uuid.Nil {
			return uuid.Nil, account.ErrDenied
		}
		return id, nil
	}
	write := func(w http.ResponseWriter, out any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/delivery-preference", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		preferences, e := store.ReadPreferences(r.Context(), account.Principal{AccountID: id}, id)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		revision, e := strconv.ParseUint(preferences.Revision, 10, 64)
		if e != nil || revision == 0 {
			account.WriteError(w, account.ErrState, uuid.New())
			return
		}
		out := accountDeliveryPreference{AccountID: id.String(), Timezone: preferences.Timezone, Language: preferences.Locale, Revision: revision, Categories: map[string]bool{}, Channels: map[string]bool{}, CategoryChannels: map[string]map[string]bool{}}
		for _, category := range []string{"trade", "watchlist", "creator", "chat", "security", "marketing"} {
			out.CategoryChannels[category] = map[string]bool{"push": false, "inapp": false, "email": false}
		}
		rows, e := store.DB.Query(r.Context(), `SELECT category,channel,enabled FROM account_consent WHERE account_id=$1`, id)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var category, channel string
			var enabled bool
			if e = rows.Scan(&category, &channel, &enabled); e != nil {
				account.WriteError(w, e, uuid.New())
				return
			}
			if _, ok := out.CategoryChannels[category]; !ok {
				account.WriteError(w, account.ErrInvalid, uuid.New())
				return
			}
			out.CategoryChannels[category][channel] = enabled
			out.Categories[category] = out.Categories[category] || enabled
			out.Channels[channel] = out.Channels[channel] || enabled
		}
		if e = rows.Err(); e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		e = store.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity_wallet_links WHERE account_id=$1 AND chain_id=125 AND state='verified')`, id).Scan(&out.Linked)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		minute := func(value string) int {
			parsed, e := time.Parse("15:04", value)
			if e != nil {
				return -1
			}
			return parsed.Hour()*60 + parsed.Minute()
		}
		start, end := minute(preferences.QuietHours.Start), minute(preferences.QuietHours.End)
		if start < 0 || end < 0 {
			account.WriteError(w, account.ErrInvalid, uuid.New())
			return
		}
		out.QuietHours = &struct{ StartMinute, EndMinute int }{start, end}
		write(w, out)
	})
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/targets", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		rows, e := store.DB.Query(r.Context(), `SELECT e.device_id FROM account_endpoint e JOIN account_state a USING(account_id) WHERE e.account_id=$1 AND NOT a.deleted ORDER BY e.device_id LIMIT 33`, id)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		devices := []uuid.UUID{}
		for rows.Next() {
			var device uuid.UUID
			if e = rows.Scan(&device); e != nil {
				rows.Close()
				account.WriteError(w, e, uuid.New())
				return
			}
			devices = append(devices, device)
		}
		e = rows.Err()
		rows.Close()
		if e != nil || len(devices) > 32 {
			account.WriteError(w, account.ErrState, uuid.New())
			return
		}
		out := []accountPushTarget{}
		for _, device := range devices {
			value, _, e := target(r.Context(), id, device)
			if e != nil {
				account.WriteError(w, e, uuid.New())
				return
			}
			out = append(out, value)
		}
		write(w, out)
	})
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/targets/{deviceId}", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		device, de := uuid.Parse(r.PathValue("deviceId"))
		if e != nil || de != nil {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		value, _, e := target(r.Context(), id, device)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		write(w, value)
	})
	mutate := func(w http.ResponseWriter, r *http.Request, disable bool) {
		id, e := requestAccount(r)
		part := r.PathValue("deviceOperation")
		suffix := ":resolve"
		if disable {
			suffix = ":disable"
		}
		if !strings.HasSuffix(part, suffix) {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		device, de := uuid.Parse(strings.TrimSuffix(part, suffix))
		if e != nil || de != nil {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		var requested accountPushTarget
		if disable {
			var input struct {
				Target accountPushTarget
				Reason string
			}
			e = account.Decode(r.Body, &input)
			requested = input.Target
			if input.Reason == "" || len(input.Reason) > 160 {
				e = account.ErrInvalid
			}
		} else {
			e = account.Decode(r.Body, &requested)
		}
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		current, sealed, e := target(r.Context(), id, device)
		if e != nil || requested.AccountID != current.AccountID || requested.DeviceInstallationID != current.DeviceInstallationID || requested.Provider != current.Provider || requested.Revision != current.Revision || requested.ConsentRevision != current.ConsentRevision || requested.EncryptedEndpointRef != current.EncryptedEndpointRef || current.Status != "active" {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		if disable {
			tag, e := store.DB.Exec(r.Context(), `UPDATE account_endpoint SET revoked=true WHERE account_id=$1 AND device_id=$2 AND consent_revision=$3 AND fingerprint=$4`, id, device, current.ConsentRevision, mustAccountFingerprint(current.EncryptedEndpointRef))
			if e != nil || tag.RowsAffected() != 1 {
				account.WriteError(w, account.ErrState, uuid.New())
				return
			}
			w.WriteHeader(204)
			return
		}
		plain, e := account.Open(runtime.EndpointKey, sealed, []byte(id.String()))
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		out := struct{ Token, URL, PublicKey, Auth, Permission string }{Permission: "granted"}
		if current.Provider == "webpush" {
			if e = account.Decode(bytes.NewReader(plain), &out); e != nil || account.SafeLink(out.URL) != nil || out.PublicKey == "" || out.Auth == "" || out.Permission != "granted" {
				account.WriteError(w, account.ErrInvalid, uuid.New())
				return
			}
		} else {
			out.Token = string(plain)
		}
		write(w, out)
	}
	mux.HandleFunc("POST /internal/v2/accounts/{accountId}/targets/{deviceOperation}", func(w http.ResponseWriter, r *http.Request) {
		mutate(w, r, strings.HasSuffix(r.PathValue("deviceOperation"), ":disable"))
	})
	mux.HandleFunc("DELETE /internal/v2/accounts/{accountId}/targets", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e == nil {
			_, e = store.DB.Exec(r.Context(), `UPDATE account_endpoint SET revoked=true WHERE account_id=$1`, id)
		}
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/verified-account", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		var verified bool
		e = store.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity_wallet_links l JOIN account_state a USING(account_id) WHERE l.account_id=$1 AND l.chain_id=125 AND l.state='verified' AND NOT a.deleted)`, id).Scan(&verified)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		write(w, struct{ Verified bool }{verified})
	})
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/watchlist", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		var revision uint64
		e = store.DB.QueryRow(r.Context(), `SELECT revision FROM account_state WHERE account_id=$1 AND NOT deleted`, id).Scan(&revision)
		if e != nil || revision == 0 {
			account.WriteError(w, account.ErrState, uuid.New())
			return
		}
		type entry struct {
			PoolAddress  string
			AlertEnabled bool
			Order        int
			AddedAt      time.Time
		}
		out := struct {
			AccountID string
			Revision  uint64
			Entries   []entry
		}{id.String(), revision, []entry{}}
		rows, e := store.DB.Query(r.Context(), `SELECT pool,alert_enabled,added_at FROM account_watch WHERE account_id=$1 ORDER BY added_at,pool LIMIT 500`, id)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var value entry
			value.Order = len(out.Entries)
			if e = rows.Scan(&value.PoolAddress, &value.AlertEnabled, &value.AddedAt); e != nil {
				account.WriteError(w, e, uuid.New())
				return
			}
			out.Entries = append(out.Entries, value)
		}
		if e = rows.Err(); e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		write(w, out)
	})
	attribution := func(w http.ResponseWriter, r *http.Request, wallet string, owner *uuid.UUID) {
		normalized, e := account.Wallet(wallet)
		if e != nil || normalized != wallet {
			account.WriteError(w, account.ErrInvalid, uuid.New())
			return
		}
		out := struct {
			AccountID, ProfileID string
			Verified, OptedIn    bool
		}{}
		var visibility string
		e = store.DB.QueryRow(r.Context(), `SELECT l.account_id::text,l.visibility FROM identity_wallet_links l JOIN account_state a USING(account_id) WHERE l.chain_id=125 AND l.wallet=$1 AND l.state='verified' AND NOT a.deleted AND a.wallet=l.wallet`, wallet).Scan(&out.AccountID, &visibility)
		if errors.Is(e, pgx.ErrNoRows) {
			write(w, out)
			return
		}
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		if owner != nil && out.AccountID != owner.String() {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		out.Verified = true
		out.OptedIn = visibility == "public_optin"
		if out.OptedIn {
			out.ProfileID = wallet
		}
		write(w, out)
	}
	mux.HandleFunc("GET /internal/v2/wallets/{wallet}/attribution", func(w http.ResponseWriter, r *http.Request) { attribution(w, r, r.PathValue("wallet"), nil) })
	mux.HandleFunc("GET /internal/v2/accounts/{accountId}/attribution", func(w http.ResponseWriter, r *http.Request) {
		id, e := requestAccount(r)
		if e != nil {
			account.WriteError(w, e, uuid.New())
			return
		}
		attribution(w, r, r.URL.Query().Get("wallet"), &id)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if runtime == nil || len(token) < 32 || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		peer := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
		if hex.EncodeToString(peer[:]) != pin || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			account.WriteError(w, account.ErrDenied, uuid.New())
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func mustAccountFingerprint(value string) []byte { raw, _ := hex.DecodeString(value); return raw }

// materialTLS loads a certificate, its key and a CA bundle, each from its
// inline value when set, else from its file.
func materialTLS(certFile, certPEM, keyFile, keyPEM, caFile, caPEM string) (tls.Certificate, *x509.CertPool, error) {
	certBytes, e := config.Material(certFile, certPEM)
	if e != nil {
		return tls.Certificate{}, nil, errors.New("account_tls_certificate_unavailable")
	}
	keyBytes, e := config.Material(keyFile, keyPEM)
	if e != nil {
		return tls.Certificate{}, nil, errors.New("account_tls_key_unavailable")
	}
	cert, e := tls.X509KeyPair(certBytes, keyBytes)
	if e != nil {
		return tls.Certificate{}, nil, e
	}
	caBytes, e := config.Material(caFile, caPEM)
	if e != nil {
		return tls.Certificate{}, nil, errors.New("account_tls_roots_unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caBytes) {
		return tls.Certificate{}, nil, errors.New("account_tls_roots_invalid")
	}
	return cert, roots, nil
}

// accountNATSOptions returns the account broker's credential and TLS options.
// File forms keep nats' own loaders (credentials re-read on reconnect); an
// inline ACCOUNT_NATS_CREDS is parsed once, and any inline TLS material moves
// the whole client identity into an in-memory tls.Config.
func accountNATSOptions(cfg config.Config) ([]nats.Option, error) {
	var options []nats.Option
	if cfg.AccountNATSCreds == "" {
		options = append(options, nats.UserCredentials(cfg.AccountNATSCredentialsFile))
	} else {
		jwt, e := nkeys.ParseDecoratedJWT([]byte(cfg.AccountNATSCreds))
		if e != nil {
			return nil, errors.New("account_nats_credentials_invalid")
		}
		pair, e := nkeys.ParseDecoratedUserNKey([]byte(cfg.AccountNATSCreds))
		if e != nil {
			return nil, errors.New("account_nats_credentials_invalid")
		}
		seed, e := pair.Seed()
		if e != nil || jwt == "" {
			return nil, errors.New("account_nats_credentials_invalid")
		}
		options = append(options, nats.UserJWTAndSeed(jwt, string(seed)))
	}
	if cfg.AccountNATSCAPEM == "" && cfg.AccountNATSCertPEM == "" && cfg.AccountNATSKeyPEM == "" {
		return append(options, nats.RootCAs(cfg.AccountNATSCAFile), nats.ClientCert(cfg.AccountNATSCertFile, cfg.AccountNATSKeyFile)), nil
	}
	cert, roots, e := materialTLS(cfg.AccountNATSCertFile, cfg.AccountNATSCertPEM, cfg.AccountNATSKeyFile, cfg.AccountNATSKeyPEM, cfg.AccountNATSCAFile, cfg.AccountNATSCAPEM)
	if e != nil {
		return nil, errors.New("account_nats_tls_invalid")
	}
	return append(options, nats.Secure(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}})), nil
}
