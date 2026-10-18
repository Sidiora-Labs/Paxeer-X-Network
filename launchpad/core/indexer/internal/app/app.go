// Package app wires the core/indexer service together (config, migrations,
// pgxpool, Redis, chain client, log sources, decoder, store, webhook publisher,
// block + backfill processors, status HTTP, graceful shutdown) and owns its
// lifecycle. Keeping the wiring here (not in main) makes the service testable
// end-to-end against external test servers + httptest RPC.
package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/telemetry"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedlog "github.com/Sidiora-Technologies/KindleLaunch/shared/log"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/process"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/chainreader"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/processor"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
)

// App is a fully-wired, ready-to-run indexer service.
type App struct {
	runStop    context.CancelFunc
	workers    *runtimeWorkers
	history    *source.HistoricalSource
	webhook    *delivery.WebhookBridge
	archive    *processor.Archive
	rawArchive *delivery.Archive
	replay     *processor.ReplayRunner
	lifecycle  lifecycleRuntimeDocument
	transport  *http.Transport

	runtimeRPC      *ethclient.Client
	runtimeRPCClose func()
	nats            *nats.Conn
	relay           *delivery.Relay
	secretStop      context.CancelFunc
	tracker         *process.Tracker
	Router          http.Handler
	Processor       *processor.Processor
	Backfill        *processor.Backfill
	Publisher       *publisher.Publisher

	cfg     config.Config
	pool    *pgxpool.Pool
	redis   *goredis.Client
	chain   *chain.Client
	sources *source.Bundle
	logger  *slog.Logger
}

func (a *App) SourceCapacityProviders() source.CapacityProviders {
	if a == nil || a.history == nil || a.history.RPC == nil || a.sources == nil || a.sources.Head == nil || a.Processor == nil || a.pool == nil {
		return source.CapacityProviders{}
	}
	return source.CapacityProviders{Live: a.history.RPC, Historical: a.history, Head: a.sources.Head, Processor: a.Processor, Pool: a.pool}
}

// New runs migrations and builds every component for the configured run mode.
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
		metrics, e = telemetry.New("indexerd", cfg.RuntimeMetricsEnvironment, cfg.RuntimeZone)
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

	pool, err := cfg.OpenRuntimePool(ctx, "indexerd", shareddb.PoolOptions{
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
	rdb, err := sharedredis.NewClient(cfg.RedisURL)
	if err != nil {
		return nil, err
	}

	defer func() {
		if !secretOwned {
			_ = rdb.Close()
		}
	}()
	chainClient, err := chain.NewClient(ctx, cfg.RPCURL, cfg.RPCURLFallback)
	if err != nil {
		return nil, err
	}

	defer func() {
		if !secretOwned {
			chainClient.Close()
		}
	}()
	nftReader, err := chainreader.NewNFTReader(chainClient, cfg.PoolRegistryAddress)
	if err != nil {
		return nil, err
	}

	var sources *source.Bundle
	if cfg.NodeEnv == "production" {
		sources = &source.Bundle{}
	} else {
		sources, err = source.Create(ctx, cfg, logger)
	}
	if err != nil {
		return nil, err
	}

	defer func() {
		if !secretOwned {
			_ = sources.Close()
		}
	}()
	targets, err := publisher.BuildTargets(cfg.WebhookURLs, cfg.WebhookHMACSecret, cfg.WebhookHMACSecrets)
	if err != nil {
		return nil, err
	}

	st := store.New(pool)
	pub := publisher.New(publisher.Options{Targets: targets, Logger: logger, Redis: rdb})

	deps := processor.Deps{
		Source:              sources.Log,
		Head:                sources.Head,
		Store:               st,
		Publisher:           pub,
		Decoder:             decode.NewDecoder(),
		NFT:                 nftReader,
		EventEmitterAddress: cfg.EventEmitterAddress,
		ChainID:             int32(cfg.ChainID),
		StartBlock:          cfg.IndexerStartBlock,
		BatchSize:           cfg.IndexerBatchSize,
		Concurrency:         cfg.IndexerConcurrency,
		PollInterval:        time.Duration(cfg.IndexerPollIntervalMS) * time.Millisecond,
		Logger:              logger,
	}
	var runtimeRPC *ethclient.Client
	var runtimeRPCClose func()
	var nc *nats.Conn
	var relay *delivery.Relay
	var lifecycle lifecycleRuntimeDocument
	var history *source.HistoricalSource
	var webhook *delivery.WebhookBridge
	var typedArchive *processor.Archive
	var rawArchive *delivery.Archive
	var replay *processor.ReplayRunner
	var webhookTransport *http.Transport
	workers := newRuntimeWorkers(logger)
	defer func() {
		if !secretOwned {
			workers.Close()
			if history != nil {
				_ = history.Close()
			}
			if webhookTransport != nil {
				webhookTransport.CloseIdleConnections()
			}
		}
	}()
	if cfg.NodeEnv == "production" {
		if cfg.IndexerBatchSize < 1 || cfg.IndexerBatchSize > 64 {
			return nil, errors.New("indexer: qualified batch size outside 1..64")
		}
		var doc receiptRuntimeDocument
		if err = config.ReadBoundDocument(cfg.ReceiptConfigFile, cfg.ReceiptConfigSHA256, &doc); err != nil {
			return nil, err
		}
		var d deliveryRuntimeDocument
		if err = config.ReadBoundDocument(cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, &d); err != nil {
			return nil, err
		}
		if d.Schema != "indexer" || d.Owner == "" || d.CanonicalStreamID == "" || d.ProjectionVersion == "" || d.DeploymentID == "" {
			return nil, errors.New("indexer: incomplete durable source configuration")
		}
		archiveTuning, err := cfg.ArchiveTuning()
		if err != nil {
			return nil, err
		}
		runtimeRPC, runtimeRPCClose, err = receipt.NewBoundedRPCClientWithArchive(ctx, cfg.RPCURL, cfg.HistoricalRPCURL, receipt.ArchiveOptions{Concurrency: archiveTuning.Concurrency, AttemptTimeout: archiveTuning.AttemptTimeout})
		if err != nil {
			return nil, err
		}
		defer func() {
			if !secretOwned {
				runtimeRPCClose()
			}
		}()
		var qualified *receipt.RPCQualification
		switch doc.Realm {
		case "production":
			// A pruned node cannot serve the chain-identity anchor forever; once
			// the anchor leaves every source's window, identity is proven by the
			// durable anchor-descended ingestion instead.
			qualified, err = receipt.NewRPCQualificationWithAttestation(ctx, runtimeRPC, doc.Production, func(c context.Context, anchor receipt.BlockAnchor) error {
				return attestDurableAnchor(c, pool, d.CanonicalStreamID, anchor)
			})
		case "private_fixture":
			if cfg.RuntimeProfile != "owned_local" {
				return nil, errors.New("indexer: private fixture requires owned local profile")
			}
			evidence, e := cfg.PrivateFixtureEvidence()
			if e != nil {
				return nil, e
			}
			if evidence.Deployment.DeploymentVersion != doc.PrivateFixture.DeploymentVersion || evidence.Deployment.ActivationHeight != doc.PrivateFixture.ActivationHeight || evidence.GenesisHash != doc.PrivateFixture.GenesisHash.Hex() || len(evidence.Deployment.Roles) != len(doc.PrivateFixture.Roles) || len(evidence.CreationTransactions) != len(doc.PrivateFixture.CreationTransactions) {
				return nil, errors.New("indexer: fixture receipt manifest binding mismatch")
			}
			for role, id := range evidence.Deployment.Roles {
				if doc.PrivateFixture.Roles[role] != id {
					return nil, errors.New("indexer: fixture role binding mismatch")
				}
			}
			for address, tx := range doc.PrivateFixture.CreationTransactions {
				if evidence.CreationTransactions[strings.ToLower(address.Hex())] != strings.ToLower(tx.Hex()) {
					return nil, errors.New("indexer: fixture creation binding mismatch")
				}
			}
			qualified, err = receipt.NewPrivateFixtureRPCQualification(ctx, runtimeRPC, doc.PrivateFixture)
		default:
			return nil, errors.New("indexer: explicit receipt realm required")
		}
		if err != nil {
			return nil, err
		}
		if err = qualified.ConfigureArchiveReads(archiveTuning.Concurrency, archiveTuning.Readahead); err != nil {
			return nil, err
		}
		guard, e := queue.NewEpochGuard("recovery", d.Epoch, d.Region)
		if e != nil {
			return nil, e
		}
		guardedCanonical, e := store.NewLifecycleStore(pool, guard.Assert)
		if e != nil {
			return nil, e
		}
		st = guardedCanonical
		deps.Store = guardedCanonical
		durable, e := delivery.NewStoreWithEpochGuard(pool, d.Schema, d.Policy, guard.Assert)
		if e != nil {
			return nil, e
		}
		partitions, e := delivery.NewPartitionStore(pool, d.Schema)
		if e != nil {
			return nil, e
		}
		if e = config.ReadBoundDocument(cfg.LifecycleConfigFile, cfg.LifecycleConfigSHA256, &lifecycle); e != nil {
			return nil, e
		}
		if e = lifecycle.validate(d, cfg); e != nil {
			return nil, e
		}
		provider, e := secrets.NewChecked(secrets.Options{Provider: secrets.ProviderType(cfg.SecretsProvider), CacheTTL: 0})
		if e != nil {
			return nil, e
		}
		lifecycleSecrets := map[string]string{}
		for endpoint, reference := range lifecycle.EndpointSecretReferences {
			value, e := provider.GetRequired(ctx, reference)
			if e != nil {
				return nil, fmt.Errorf("indexer: committed endpoint secret unavailable: %s", endpoint)
			}
			lifecycle.Committed.Endpoints[endpoint] = value
			lifecycleSecrets[reference] = value
		}
		secretChecks["lifecycle_secrets"] = func(c context.Context) error {
			for reference, original := range lifecycleSecrets {
				current, e := provider.GetRequired(c, reference)
				if e != nil {
					return errors.New("indexer: lifecycle secret unavailable")
				}
				if current != original {
					return errors.New("indexer: lifecycle secret rotated; restart required")
				}
			}
			return nil
		}
		if lifecycle.Committed.GenesisHash != doc.Production.GenesisHash && doc.Realm == "production" || lifecycle.Committed.GenesisHash != doc.PrivateFixture.GenesisHash && doc.Realm == "private_fixture" {
			return nil, errors.New("indexer: committed provider genesis disagrees with receipt authority")
		}
		if doc.Realm == "production" {
			if e = bindRuntimeAnchor(&lifecycle, doc.Production); e != nil {
				return nil, e
			}
			if lifecycle.Committed.Anchor != nil {
				anchor := receipt.BlockAnchor{Height: lifecycle.Committed.Anchor.Height, Hash: lifecycle.Committed.Anchor.Hash}
				if _, pinned := lifecycle.Committed.Endpoints[historicalEndpointID]; pinned {
					return nil, errors.New("indexer: historical endpoint id is reserved")
				}
				if cfg.HistoricalRPCURL != "" {
					if lifecycle.Committed.HistoricalEndpoints == nil {
						lifecycle.Committed.HistoricalEndpoints = map[string]string{}
					}
					lifecycle.Committed.HistoricalEndpoints[historicalEndpointID] = cfg.HistoricalRPCURL
				}
				lifecycle.Committed.AnchorAttested = attestDurableAnchor(ctx, pool, d.CanonicalStreamID, anchor) == nil
			}
		}
		lifecycle.Committed.HistoricalConcurrency = archiveTuning.Concurrency
		lifecycle.Committed.HistoricalReadahead = archiveTuning.Readahead
		lifecycle.Committed.HistoricalAttemptTimeout = archiveTuning.AttemptTimeout
		history, e = source.CreateHistorical(ctx, lifecycle.Committed, cfg.PaxscanDatabaseURL, lifecycle.Explorer)
		if e != nil {
			return nil, e
		}
		if doc.Realm == "production" {
			markets, e := persistedMarkets(ctx, pool)
			if e != nil {
				return nil, e
			}
			if e = qualified.Rehydrate(ctx, markets); e != nil {
				return nil, fmt.Errorf("indexer: rehydrate qualified markets: %w", e)
			}
		}
		if e = seedCanonicalWatermark(ctx, pool, d.CanonicalStreamID, lifecycle); e != nil {
			return nil, e
		}
		watermark, e := st.GetCanonicalWatermark(ctx, 125, d.CanonicalStreamID)
		if e != nil {
			return nil, fmt.Errorf("indexer: source-issued canonical bootstrap checkpoint unavailable: %w", e)
		}
		if e = verifyCanonicalRuntimeCheckpoint(ctx, pool, history.RPC, d.CanonicalStreamID, watermark, lifecycle); e != nil {
			return nil, e
		}
		var durableHeight atomic.Uint64
		durableHeight.Store(uint64(watermark))
		deps.Head = &canonicalRuntimeHead{source: history.RPC, store: st, stream: d.CanonicalStreamID, height: &durableHeight}
		deps.Source = history.RPC
		sources.Log = history
		sources.Head = deps.Head
		if _, e = deps.Head.Head(ctx); e != nil {
			return nil, e
		}
		historicalQualified := &source.HistoricalQualifiedSource{History: history, Authority: qualified, DurableHeight: durableHeight.Load}
		liveQualified := &source.LiveQualifiedSource{RPC: history.RPC, Authority: qualified, DurableHeight: durableHeight.Load, PriorHash: func(c context.Context, height uint64) (common.Hash, error) {
			return durableBlockHash(c, pool, d.CanonicalStreamID, height, lifecycle)
		}}
		deps.Quarantine = &processor.QuarantinePolicy{Registry: qualified.Registry(), SourceStream: d.CanonicalStreamID, OptionalEmitterTopics: lifecycle.OptionalEmitterTopics}
		authorize := func(c context.Context, action string) error {
			scope, ok := lifecycle.Operations[action]
			if !ok {
				return errors.New("indexer: private operation has no issued authorization")
			}
			return authorizeRuntimeOperation(c, pool, scope)
		}
		var stager *publisher.DurableStager
		if len(lifecycle.Webhooks.Targets) > 0 {
			webhookTransport, e = runtimeWebhookTransport(lifecycle.Webhooks)
			if e != nil {
				return nil, e
			}
			webhook, e = delivery.NewWebhookBridge(durable, lifecycle.Webhooks.Targets, &http.Client{Transport: webhookTransport}, provider, authorize, d.Owner, time.Duration(lifecycle.Webhooks.LeaseMS)*time.Millisecond, time.Duration(lifecycle.Webhooks.TimeoutMS)*time.Millisecond, time.Duration(lifecycle.Webhooks.RetryMS)*time.Millisecond, lifecycle.Webhooks.MaxAttempts)
			if e != nil {
				return nil, e
			}
			stager, e = publisher.NewDurableStagerWithWebhook(durable, d.DeploymentID, d.Subjects, webhook, lifecycle.Webhooks.Generation)
		} else {
			stager, e = publisher.NewDurableStager(durable, d.DeploymentID, d.Subjects)
		}
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
		brokerPublisher, e := natsdriver.NewPublisher(nc, d.Stream)
		if e != nil {
			return nil, e
		}
		var persistentPublisher delivery.PersistentPublisher = brokerPublisher
		if cfg.FixturePubAckBarrier != "" {
			if cfg.RuntimeProfile != "owned_local" || doc.Realm != "private_fixture" {
				return nil, errors.New("indexer: fixture barrier forbidden")
			}
			persistentPublisher = fixturePublishBarrier{Publisher: brokerPublisher, Path: cfg.FixturePubAckBarrier}
		}
		relay = &delivery.Relay{Store: durable, Publisher: persistentPublisher, Owner: d.Owner, Batch: 32, LeaseDuration: 30 * time.Second, PublishTimeout: 5 * time.Second, RetryDelay: time.Second, MaxAttempts: 5}
		replay = &processor.ReplayRunner{Store: st, Source: historicalQualified, Deliveries: durable, Owner: d.Owner, DeliverySchema: d.Schema, BatchSize: lifecycle.ReplayBatchSize}
		if lifecycle.Archive.Enabled {
			catalog := doc.Production.CatalogJSON
			if doc.Realm == "private_fixture" {
				catalog = doc.PrivateFixture.CatalogJSON
			}
			binary, e := runtimeBinaryDigest()
			if e != nil {
				return nil, e
			}
			if lifecycle.Archive.DecoderDigest != binary || lifecycle.Archive.CodeDigest != binary || lifecycle.Archive.ABIDigest != delivery.Hash(catalog) {
				return nil, errors.New("indexer: archive provenance pins do not match actual executable and qualified ABI catalog")
			}
			access, e := provider.GetRequired(ctx, lifecycle.Archive.AccessKeyReference)
			if e != nil {
				return nil, errors.New("indexer: archive access key unavailable")
			}
			secret, e := provider.GetRequired(ctx, lifecycle.Archive.SecretKeyReference)
			if e != nil {
				return nil, errors.New("indexer: archive secret key unavailable")
			}
			lifecycleSecrets[lifecycle.Archive.AccessKeyReference] = access
			lifecycleSecrets[lifecycle.Archive.SecretKeyReference] = secret
			objects, e := storage.New(ctx, storage.Config{Endpoint: lifecycle.Archive.Endpoint, Region: lifecycle.Archive.Region, Bucket: lifecycle.Archive.Bucket, AccessKeyID: access, SecretAccessKey: secret})
			if e != nil {
				return nil, e
			}
			typedArchive = &processor.Archive{Store: st, Objects: objects, Source: historicalQualified}
			replay.Archive = typedArchive
			rawArchive, e = delivery.NewArchive(durable, objects, lifecycle.Archive.Prefix, d.CanonicalStreamID, d.DeploymentID, lifecycle.Archive.DecoderDigest, lifecycle.Archive.ABIDigest, lifecycle.Archive.CodeDigest)
			if e != nil {
				return nil, e
			}
			secretChecks["archive"] = func(c context.Context) error {
				info, e := objects.Head(c, lifecycle.Archive.ReadinessObject)
				if e != nil {
					return e
				}
				if info.Size != lifecycle.Archive.ReadinessObjectBytes {
					return errors.New("indexer: archive readiness object mismatch")
				}
				return nil
			}
		}
		webhookBudget, e := delivery.NewWorkBudget(lifecycle.WorkConcurrency, lifecycle.EventsPerSecond, lifecycle.WorkBurst)
		if e != nil {
			return nil, e
		}
		if webhook != nil {
			workers.AddPeriodic("webhooks", time.Duration(lifecycle.Webhooks.PollMS)*time.Millisecond, false, func(c context.Context) error { return webhook.RunOnceWithBudget(c, webhookBudget) })
		}
		replaySlots := make(chan struct{}, lifecycle.WorkConcurrency)
		for index, request := range lifecycle.ReplayRequests {
			request := request
			workers.AddOnce(fmt.Sprintf("replay_%d", index), func(c context.Context) error {
				return withRuntimeSlot(c, replaySlots, func() error { return replay.Run(c, request) })
			})
		}
		for index, rebuild := range lifecycle.Rebuilds {
			rebuild := rebuild
			projector, e := processor.NewGenerationReplayProjection(replay, rebuild.Request)
			if e != nil {
				return nil, e
			}
			rebuildBudget, e := delivery.NewWorkBudget(lifecycle.WorkConcurrency, lifecycle.EventsPerSecond, lifecycle.WorkBurst)
			if e != nil {
				return nil, e
			}
			engine, e := delivery.NewReplayEngine(rawArchive, rebuild.ConsumerID, rebuild.Request.Generation, d.Owner, projector, authorize, 30*time.Second)
			if e != nil {
				return nil, e
			}
			workers.AddOnce(fmt.Sprintf("rebuild_%d", index), func(c context.Context) error {
				if e := engine.Start(c, rebuild.ManifestDigest); e != nil {
					return e
				}
				for {
					status, e := engine.Status(c, 0)
					if e != nil {
						return e
					}
					if status.Status == "verified" || status.Status == "active" {
						break
					}
					if e = engine.RunBlockWithBudget(c, rebuildBudget); e != nil {
						return e
					}
				}
				if rebuild.Promote {
					return engine.Promote(c, rebuild.ExpectedAliasRevision)
				}
				return nil
			})
		}
		if typedArchive != nil {
			workers.AddPeriodic("archive", time.Duration(lifecycle.Archive.PollMS)*time.Millisecond, true, func(c context.Context) error {
				return captureArchiveBatch(c, pool, st, typedArchive, rawArchive, lifecycle.Archive, d.CanonicalStreamID)
			})
		}
		if cfg.IndexerMode == config.ModeBackfill {
			request := lifecycle.BackfillRequest
			if request == nil {
				return nil, errors.New("indexer: authorized backfill request unavailable")
			}
			deps.HistoricalReplay = replay
			deps.HistoricalRequest = request
		}
		secretChecks["workers"] = workers.Ready
		secretChecks["canonical_source"] = func(c context.Context) error {
			_, e := deps.Head.Head(c)
			if e != nil {
				return e
			}
			watermark, e := st.GetCanonicalWatermark(c, 125, d.CanonicalStreamID)
			if e != nil {
				return e
			}
			return verifyCanonicalRuntimeCheckpoint(c, pool, history.RPC, d.CanonicalStreamID, watermark, lifecycle)
		}
		deps.QualifiedBatchSource = liveQualified
		deps.CanonicalOwner = d.Owner
		deps.CanonicalStreamID = d.CanonicalStreamID
		deps.AccountingPartitions = partitions
		deps.Durable = stager
		deps.AccountingProjectionVersion = d.ProjectionVersion
		deps.InventoryGeneration = d.Generation
		deps.InventoryProjectionVersion = d.InventoryProjectionVersion
		deps.LegacyProjection = doc.Realm == "production"
		deps.Reserves = qualified
	}
	proc := processor.New(deps)
	if cfg.NodeEnv == "production" {
		secretChecks["ingestion"] = proc.Ready
	}
	backfill := processor.NewBackfill(proc, cfg.BackfillFromBlock, cfg.BackfillBatchSize)

	router := sharedhttp.NewRouter(sharedhttp.ServerOptions{
		CORSOrigins: cfg.CORSAllowedOrigins,
		Health: sharedhttp.HealthDeps{
			Custom: secretChecks,
			State:  tracker.Ready,
			DB:     func(c context.Context) error { return cfg.CheckRuntimeSchema(c, pool) },
			Redis:  func(c context.Context) error { return rdb.Ping(c).Err() },
		},
	})
	httpapi.RegisterStatus(router, httpapi.StatusDeps{
		Store:     st,
		ChainID:   int32(cfg.ChainID),
		Publisher: pub,
		StartTime: time.Now(),
	})

	if metrics != nil {
		router.Handle("/metrics", metrics)
	}
	if err := tracker.SetReady(); err != nil {
		return nil, err
	}
	secretOwned = true
	return &App{workers: workers, history: history, webhook: webhook, archive: typedArchive, rawArchive: rawArchive, replay: replay, lifecycle: lifecycle, transport: webhookTransport, secretStop: secretStop, tracker: tracker,
		runtimeRPC: runtimeRPC, runtimeRPCClose: runtimeRPCClose, nats: nc, relay: relay,
		Router:    runtimeHandler(tracker, router, metrics),
		Processor: proc,
		Backfill:  backfill,
		Publisher: pub,
		cfg:       cfg,
		pool:      pool,
		redis:     rdb,
		chain:     chainClient,
		sources:   sources,
		logger:    logger,
	}, nil
}

// Close releases all resources.
func (a *App) Close() {
	drain, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if a.runStop != nil {
		a.runStop()
	}
	if a.workers != nil {
		a.workers.Stop()
	}
	if a.tracker != nil {
		if err := a.tracker.Drain(drain); err != nil && a.logger != nil {
			a.logger.Error("indexer in-flight drain failed", slog.Any("error", err))
		}
	}
	if a.workers != nil {
		a.workers.CloseContext(drain)
	}
	if a.history != nil {
		_ = a.history.Close()
	}
	if a.transport != nil {
		a.transport.CloseIdleConnections()
	}
	if a.nats != nil {
		a.nats.Close()
	}
	if a.runtimeRPCClose != nil {
		a.runtimeRPCClose()
	} else if a.runtimeRPC != nil {
		a.runtimeRPC.Close()
	}
	if a.secretStop != nil {
		a.secretStop()
	}
	_ = a.sources.Close()
	a.chain.Close()
	a.pool.Close()
	if err := a.redis.Close(); err != nil && a.logger != nil {
		a.logger.Warn("redis close", slog.String("err", err.Error()))
	}
}

// Run loads config, builds the App, serves the status HTTP server, runs the
// configured mode (backfill then live, or live directly), and blocks until ctx
// is cancelled or a signal arrives, then drains gracefully.
func Run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := sharedlog.New("indexer", cfg.LogLevel)
	logger.Info("starting indexer service",
		slog.String("mode", cfg.IndexerMode), slog.String("logSource", cfg.IndexerLogSource))

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	app.runStop = cancel
	defer app.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           app.Router,
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

	if err = app.workers.Start(ctx); err != nil {
		return err
	}
	if app.relay != nil {
		go func() {
			release, e := app.tracker.Acquire()
			if e != nil {
				return
			}
			defer release()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if e := app.relay.RunOnce(ctx); e != nil && !errors.Is(e, context.Canceled) {
						logger.Error("durable relay failed", slog.Any("err", e))
						app.tracker.BeginDrain()
						cancel()
						return
					}
				}
			}
		}()
	}
	// Backfill mode: run the historical replay first.
	if cfg.IndexerMode == config.ModeBackfill {
		if err := app.Backfill.Run(ctx); err != nil {
			return err
		}
		if cfg.BackfillOnly {
			logger.Info("backfill complete — BACKFILL_ONLY=true, exiting")
			if err := shutdownServer(ctx, srv); err != nil {
				logger.Warn("graceful shutdown error", slog.Any("err", err))
			}
			return firstNonShutdownErr(serveErr)
		}
		logger.Info("backfill complete — transitioning to live mode")
	}

	procErr := make(chan error, 1)
	go func() {
		release, e := app.tracker.Acquire()
		if e != nil {
			procErr <- e
			return
		}
		defer release()
		e = app.Processor.Run(ctx)
		procErr <- e
		if e != nil && !errors.Is(e, context.Canceled) {
			app.tracker.BeginDrain()
			cancel()
		}
	}()

	shutErr := process.Run(ctx, process.Options{Tracker: app.tracker,
		Logger:     logger,
		OnShutdown: func(sctx context.Context) error { cancel(); return srv.Shutdown(sctx) },
	})

	cancel()
	app.Publisher.Disconnect()

	select {
	case err := <-serveErr:
		return err
	case err := <-procErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return shutErr
	default:
		return shutErr
	}
}

func shutdownServer(ctx context.Context, srv *http.Server) error {
	sctx, scancel := context.WithTimeout(ctx, 10*time.Second)
	defer scancel()
	return srv.Shutdown(sctx)
}

func firstNonShutdownErr(serveErr <-chan error) error {
	select {
	case err := <-serveErr:
		return err
	default:
		return nil
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

type receiptRuntimeDocument struct {
	Realm          string                           `json:"realm"`
	Production     receipt.RPCOptions               `json:"production"`
	PrivateFixture receipt.PrivateFixtureRPCOptions `json:"privateFixture"`
}
type deliveryRuntimeDocument struct {
	Schema                     string            `json:"schema"`
	Owner                      string            `json:"owner"`
	Epoch                      int64             `json:"epoch"`
	Region                     string            `json:"region"`
	Stream                     string            `json:"stream"`
	CanonicalStreamID          string            `json:"canonicalStreamId"`
	DeploymentID               string            `json:"deploymentId"`
	ProjectionVersion          string            `json:"projectionVersion"`
	InventoryProjectionVersion string            `json:"inventoryProjectionVersion"`
	Generation                 string            `json:"generation"`
	Policy                     delivery.Policy   `json:"policy"`
	Subjects                   map[string]string `json:"subjects"`
}

var inventoryToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// validateInventory binds inventory enrichment as one unit: a generation needs
// the inventory projection version and the accounting.inventory.v1 policy and
// subject mapping, and none of them may appear without a generation, so a
// partially enabled document fails at boot instead of at the first fill.
func (d deliveryRuntimeDocument) validateInventory() error {
	subject := accounting.InventorySubject
	approved := d.Policy.Subjects[subject] || d.Policy.EventTypes[subject] || d.Subjects[subject] != ""
	if d.Generation == "" {
		if d.InventoryProjectionVersion != "" || approved {
			return errors.New("indexer: inventory publication requires an inventory generation")
		}
		return nil
	}
	if !inventoryToken.MatchString(d.Generation) || d.InventoryProjectionVersion == "" || len(d.InventoryProjectionVersion) > 128 || d.InventoryProjectionVersion == d.ProjectionVersion {
		return errors.New("indexer: bounded inventory generation and distinct projection version required")
	}
	if !d.Policy.Subjects[subject] || !d.Policy.EventTypes[subject] || d.Subjects[subject] != subject {
		return errors.New("indexer: inventory delivery policy unavailable")
	}
	return nil
}

type fixturePublishBarrier struct {
	Publisher delivery.PersistentPublisher
	Path      string
}

func (b fixturePublishBarrier) Publish(ctx context.Context, subject, eventID string, wire []byte) (delivery.PersistenceAck, error) {
	ack, err := b.Publisher.Publish(ctx, subject, eventID, wire)
	if err != nil {
		return ack, err
	}
	marker, err := os.OpenFile(b.Path+".reached", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ack, err
	}
	_, err = fmt.Fprintf(marker, "%s %s %d\n", eventID, ack.Stream, ack.Sequence)
	closeErr := marker.Close()
	if err != nil {
		return ack, err
	}
	if closeErr != nil {
		return ack, closeErr
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, e := os.Lstat(b.Path + ".release"); e == nil {
			if !info.Mode().IsRegular() {
				return ack, errors.New("fixture barrier release invalid")
			}
			return ack, nil
		} else if !os.IsNotExist(e) {
			return ack, e
		}
		select {
		case <-ctx.Done():
			return ack, ctx.Err()
		case <-ticker.C:
		}
	}
}

type lifecycleWebhookDocument struct {
	Targets                                          []delivery.WebhookTarget `json:"targets"`
	Generation                                       string                   `json:"generation"`
	LeaseMS, TimeoutMS, RetryMS, PollMS, MaxAttempts int
	CAFile, CASHA256                                 string
}
type lifecycleArchiveDocument struct {
	Enabled                                                                  bool `json:"enabled"`
	Endpoint, Region, Bucket, Prefix, AccessKeyReference, SecretKeyReference string
	DecoderDigest, ABIDigest, CodeDigest                                     string
	ReadinessObject                                                          string
	ReadinessObjectBytes                                                     int64
	FromHeight                                                               int64
	BatchBlocks, PollMS                                                      int
}
type lifecycleRebuildDocument struct {
	ConsumerID, ManifestDigest string
	Request                    store.ReplayRequest
	Promote                    bool
	ExpectedAliasRevision      int64
}
type lifecycleRuntimeDocument struct {
	Version                    int                                       `json:"version"`
	Committed                  source.CommittedOptions                   `json:"committed"`
	EndpointSecretReferences   map[string]string                         `json:"endpointSecretReferences"`
	Explorer                   *source.CommittedPaxscanOptions           `json:"explorer,omitempty"`
	OptionalEmitterTopics      map[string]processor.OptionalFamilyPolicy `json:"optionalEmitterTopics"`
	Webhooks                   lifecycleWebhookDocument                  `json:"webhooks"`
	Archive                    lifecycleArchiveDocument                  `json:"archive"`
	ReplayBatchSize            int                                       `json:"replayBatchSize"`
	WorkConcurrency, WorkBurst int
	EventsPerSecond            float64
	ReplayRequests             []store.ReplayRequest           `json:"replayRequests"`
	BackfillRequest            *store.ReplayRequest            `json:"backfillRequest,omitempty"`
	Rebuilds                   []lifecycleRebuildDocument      `json:"rebuilds"`
	Operations                 map[string]store.OperationScope `json:"operations"`
	InitialCheckpointHeight    int64
	InitialCheckpointHash      string
}

func (d *lifecycleRuntimeDocument) validate(deliveryDoc deliveryRuntimeDocument, cfg config.Config) error {
	if cfg.ChainID != 125 {
		return errors.New("indexer: lifecycle chain authority must be 125")
	}
	if d.Version != 2 || d.ReplayBatchSize < 1 || d.ReplayBatchSize > 64 || len(d.ReplayRequests) > 16 || len(d.Rebuilds) > 16 || len(d.OptionalEmitterTopics) > 4096 || len(d.EndpointSecretReferences) > 8 || d.InitialCheckpointHeight < 0 || len(d.InitialCheckpointHash) != 66 {
		return errors.New("indexer: bounded version2 lifecycle configuration required")
	}
	if d.Committed.Endpoints == nil {
		d.Committed.Endpoints = map[string]string{}
	}
	if d.Explorer != nil && d.Explorer.RPC != nil {
		return errors.New("indexer: explorer RPC provider must be constructed locally")
	}
	if deliveryDoc.Subjects[publisher.CanonicalCreationEventType] != publisher.CanonicalCreationSubject || !deliveryDoc.Policy.Subjects[publisher.CanonicalCreationSubject] || !deliveryDoc.Policy.EventTypes[publisher.CanonicalCreationEventType] {
		return errors.New("indexer: canonical creation delivery policy unavailable")
	}
	if !deliveryDoc.Policy.Subjects[processor.ReplaySubjectV2] || !deliveryDoc.Policy.EventTypes[processor.ReplaySubjectV2] {
		return errors.New("indexer: version2 replay subject policy unavailable")
	}
	if err := deliveryDoc.validateInventory(); err != nil {
		return err
	}
	if cfg.IndexerMode == config.ModeBackfill && (d.BackfillRequest == nil || cfg.BackfillBatchSize < 1 || cfg.BackfillBatchSize > 64) {
		return errors.New("indexer: explicit bounded authorized backfill configuration required")
	}
	for _, request := range d.ReplayRequests {
		if err := store.ValidateReplayRequest(request); err != nil {
			return err
		}
		if request.SourceStream != deliveryDoc.CanonicalStreamID || request.Mode == "backfill" {
			return errors.New("indexer: replay request source or mode mismatch")
		}
	}
	if d.BackfillRequest != nil {
		if err := store.ValidateReplayRequest(*d.BackfillRequest); err != nil {
			return err
		}
		if d.BackfillRequest.SourceStream != deliveryDoc.CanonicalStreamID || d.BackfillRequest.Mode != "backfill" {
			return errors.New("indexer: historical backfill source mismatch")
		}
	}
	if len(d.Webhooks.Targets) > 0 && (d.Webhooks.Generation == "" || d.Webhooks.PollMS < 100 || d.Webhooks.PollMS > 60000) {
		return errors.New("indexer: bounded durable webhook configuration required")
	}
	if d.Archive.Enabled {
		a := d.Archive
		if a.Endpoint == "" || a.Bucket == "" || a.AccessKeyReference == "" || a.SecretKeyReference == "" || a.ReadinessObject == "" || a.ReadinessObjectBytes < 0 || a.FromHeight < 0 || a.BatchBlocks < 1 || a.BatchBlocks > 64 || a.PollMS < 1000 || a.PollMS > 3600000 {
			return errors.New("indexer: actual archive credentials, readiness object and bounded policy required")
		}
	} else if len(d.Rebuilds) > 0 {
		return errors.New("indexer: raw archive required for generation reconstruction")
	}
	for _, rebuild := range d.Rebuilds {
		if err := store.ValidateReplayRequest(rebuild.Request); err != nil {
			return err
		}
		if rebuild.Request.Mode != "reconstruct" || rebuild.Request.SourceStream != deliveryDoc.CanonicalStreamID || rebuild.ConsumerID == "" || len(rebuild.ManifestDigest) != 64 || rebuild.ExpectedAliasRevision < 0 {
			return errors.New("indexer: isolated generation reconstruction configuration invalid")
		}
		if rebuild.Promote {
			if _, ok := d.Operations["replay.promote"]; !ok {
				return errors.New("indexer: issued private promotion authorization required")
			}
		}
	}
	return nil
}
func authorizeRuntimeOperation(ctx context.Context, pool *pgxpool.Pool, scope store.OperationScope) error {
	if pool == nil || scope.AuthorizationID == "" || scope.Operator == "" || scope.From < 0 || scope.To < scope.From {
		return errors.New("indexer: authenticated operational scope unavailable")
	}
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT operator_identity=$2 AND action=$3 AND target=$4 AND generation=$5 AND projection_revision=$6 AND from_height<=$7 AND to_height>=$8 AND expires_at>clock_timestamp() AND revoked_at IS NULL AND authorization_evidence~'^[0-9a-f]{64}$' FROM indexer.indexer_replay_authorizations WHERE authorization_id=$1`, scope.AuthorizationID, scope.Operator, scope.Action, scope.Target, scope.Generation, scope.Revision, scope.From, scope.To).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("indexer: operational authorization denied")
	}
	return nil
}
func runtimeWebhookTransport(doc lifecycleWebhookDocument) (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("indexer: verified HTTP transport unavailable")
	}
	transport := base.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if doc.CAFile != "" {
		info, err := os.Lstat(doc.CAFile)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("indexer: webhook CA file invalid")
		}
		pem, err := os.ReadFile(doc.CAFile)
		if err != nil {
			return nil, err
		}
		if len(doc.CASHA256) != 64 || fmt.Sprintf("%x", sha256.Sum256(pem)) != doc.CASHA256 {
			return nil, errors.New("indexer: webhook CA digest mismatch")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("indexer: webhook CA cannot be parsed")
		}
		transport.TLSClientConfig.RootCAs = roots
	} else if doc.CASHA256 != "" {
		return nil, errors.New("indexer: webhook CA path required for configured digest")
	}
	return transport, nil
}

type canonicalRuntimeHead struct {
	source *source.CommittedSource
	store  *store.Store
	stream string
	height *atomic.Uint64
}

func (h *canonicalRuntimeHead) Head(ctx context.Context) (int64, error) {
	durable, err := h.store.GetCanonicalWatermark(ctx, 125, h.stream)
	if err != nil {
		return 0, err
	}
	if durable < 0 {
		return 0, errors.New("indexer: durable checkpoint must be nonnegative")
	}
	h.height.Store(uint64(durable))
	head, err := h.source.ObserveHead(ctx, uint64(durable))
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		return 0, errors.Join(err, h.store.QuarantineCommittedHead(cleanup, h.stream))
	}
	if head.Height > uint64(^uint64(0)>>1) {
		return 0, errors.New("indexer: committed head exceeds durable integer range")
	}
	if err = h.store.RetainCommittedHead(ctx, h.stream, int64(head.Height), strings.ToLower(head.Hash.Hex()), head.SourceID); err != nil {
		return 0, err
	}
	if err = h.source.ReportDurableHeight(time.Now().UTC(), uint64(durable)); err != nil {
		return 0, err
	}
	return int64(head.Height), nil
}
func (h *canonicalRuntimeHead) Name() string { return "durable-pinned-committed-rpc" }
func (h *canonicalRuntimeHead) Close() error { return nil }

type runtimeWorker struct {
	name        string
	interval    time.Duration
	once, fatal bool
	run         func(context.Context) error
}
type runtimeWorkerState struct {
	started, completed bool
	failure            error
}
type runtimeWorkers struct {
	mu      sync.Mutex
	items   []runtimeWorker
	states  map[string]runtimeWorkerState
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	close   sync.Once
	logger  *slog.Logger
	started bool
}

func newRuntimeWorkers(logger *slog.Logger) *runtimeWorkers {
	return &runtimeWorkers{states: map[string]runtimeWorkerState{}, logger: logger}
}
func (w *runtimeWorkers) AddOnce(name string, run func(context.Context) error) {
	w.items = append(w.items, runtimeWorker{name: name, once: true, run: run})
	w.states[name] = runtimeWorkerState{}
}
func (w *runtimeWorkers) AddPeriodic(name string, interval time.Duration, fatal bool, run func(context.Context) error) {
	w.items = append(w.items, runtimeWorker{name: name, interval: interval, fatal: fatal, run: run})
	w.states[name] = runtimeWorkerState{}
}
func (w *runtimeWorkers) Start(parent context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return errors.New("indexer: runtime workers already started")
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.started = true
	w.mu.Unlock()
	for _, item := range w.items {
		item := item
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				err := item.run(ctx)
				w.mu.Lock()
				w.states[item.name] = runtimeWorkerState{started: true, completed: item.once && err == nil, failure: err}
				w.mu.Unlock()
				if err != nil && !errors.Is(err, context.Canceled) && w.logger != nil {
					w.logger.Error("indexer provider worker failed", slog.String("worker", item.name), slog.Any("error", err))
				}
				if item.once || item.fatal && err != nil {
					return
				}
				timer := time.NewTimer(item.interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	return nil
}
func (w *runtimeWorkers) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return errors.New("indexer: provider workers not started")
	}
	for name, state := range w.states {
		if !state.started {
			return fmt.Errorf("indexer: provider worker has no successful run: %s", name)
		}
		if state.failure != nil {
			return fmt.Errorf("indexer: provider worker unavailable: %s", name)
		}
	}
	return nil
}
func (w *runtimeWorkers) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (w *runtimeWorkers) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w.CloseContext(ctx)
}
func (w *runtimeWorkers) CloseContext(ctx context.Context) {
	w.close.Do(func() {
		w.Stop()
		done := make(chan struct{})
		go func() { w.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			if w.logger != nil {
				w.logger.Error("indexer provider worker shutdown deadline exceeded")
			}
		}
	})
}

func captureArchiveBatch(ctx context.Context, pool *pgxpool.Pool, st *store.Store, typed *processor.Archive, raw *delivery.Archive, policy lifecycleArchiveDocument, stream string) error {
	durable, err := st.GetCanonicalWatermark(ctx, 125, stream)
	if err != nil {
		return err
	}
	next := policy.FromHeight
	rows, err := pool.Query(ctx, `SELECT DISTINCT t.from_height,t.to_height FROM indexer.indexer_archive_segments t JOIN indexer.delivery_archive_manifests r ON r.source_stream=t.source_stream AND r.from_height=t.from_height AND r.to_height=t.to_height WHERE t.source_stream=$1 AND t.from_height>=$2 ORDER BY t.from_height,t.to_height`, stream, policy.FromHeight)
	if err != nil {
		return err
	}
	for rows.Next() {
		var from, to int64
		if err = rows.Scan(&from, &to); err != nil {
			rows.Close()
			return err
		}
		if from > next {
			break
		}
		if to >= next {
			next = to + 1
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if next > durable {
		return nil
	}
	to := next + int64(policy.BatchBlocks) - 1
	if to > durable {
		to = durable
	}
	if _, err = typed.Produce(ctx, stream, next, to); err != nil {
		return err
	}
	_, _, err = raw.Capture(ctx, next, to)
	return err
}

func verifyCanonicalRuntimeCheckpoint(ctx context.Context, pool *pgxpool.Pool, provider *source.CommittedSource, stream string, height int64, doc lifecycleRuntimeDocument) error {
	if height < 0 || provider == nil {
		return errors.New("indexer: canonical checkpoint authority unavailable")
	}
	var expected string
	err := pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete`, stream, height).Scan(&expected)
	if errors.Is(err, pgx.ErrNoRows) {
		if height != doc.InitialCheckpointHeight {
			return errors.New("indexer: canonical checkpoint has no complete source block or issued initial checkpoint")
		}
		expected = doc.InitialCheckpointHash
	} else if err != nil {
		return err
	}
	original, err := provider.BlockAt(ctx, uint64(height))
	if err != nil {
		return err
	}
	if strings.ToLower(original.Hash.Hex()) != expected {
		return errors.New("indexer: persisted checkpoint differs from canonical source")
	}
	return nil
}
func runtimeBinaryDigest() (string, error) {
	executable, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", err
	}
	defer executable.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, executable); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func withRuntimeSlot(ctx context.Context, slots chan struct{}, run func() error) error {
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slots }()
	return run()
}
