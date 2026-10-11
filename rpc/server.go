package evmrpc

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"

	tmutils "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	evmCfg "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	evmrpcconfig "github.com/Sidiora-Labs/Paxeer-X-Network/rpc/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/rpc/stats"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/types"
)

type ConnectionType string

var ConnectionTypeWS ConnectionType = "websocket"
var ConnectionTypeHTTP ConnectionType = "http"

const LocalAddress = "0.0.0.0"
const DefaultWebsocketMaxMessageSize = 10 * 1024 * 1024

type EVMServer interface {
	Start() error
	Stop()
}

// ServerOption configures NewEVMHTTPServer (a TraceContextProvider, a SequencerReceiptOption or a
// SequencerOption) and NewEVMWebSocketServer (a SequencerOption).
type ServerOption interface{ serverOption() }

func (TraceContextProvider) serverOption()   {}
func (SequencerReceiptOption) serverOption() {}
func (SequencerOption) serverOption()        {}

// SequencerOption runs the sequencer's HTTP and WebSocket servers: raw transactions are accepted
// through the accepter, and pending logs, filters and subscriptions are served from the receipt log
// feed. Build it once with NewSequencerOption and hand the same option to both servers, so that a
// hash receipted through either server reaches every newPendingTransactions subscriber.
type SequencerOption struct {
	accepter    SequencerAccepter
	receiptLogs *ReceiptLogFeed
	pendingTxs  *pendingTxFeed1315
}

// NewSequencerOption builds the option holding the one pending-hash feed both servers share.
func NewSequencerOption(accepter SequencerAccepter, feed *ReceiptLogFeed) SequencerOption {
	return SequencerOption{
		accepter:    accepter,
		receiptLogs: feed,
		pendingTxs:  &pendingTxFeed1315{listeners: make(map[rpc.ID]*pendingTxListener1315)},
	}
}

// sendConfig is the SendConfig of a server: the sequencer's when o is set, otherwise the
// mempool path's.
func (o *SequencerOption) sendConfig(config evmrpcconfig.Config) *SendConfig {
	sendConfig := &SendConfig{slow: config.Slow, keyringEnabled: config.EnableUnsafeKeyringRPC}
	if o != nil {
		sendConfig.Sequencer = o.accepter
		sendConfig.pendingTxs = o.pendingTxs
		sendConfig.receiptLogs = o.receiptLogs
	}
	return sendConfig
}

// sequencerOption takes opt as a server's SequencerOption; current is the one already taken, if
// any, since a server takes at most one.
func sequencerOption(opt SequencerOption, current *SequencerOption) (*SequencerOption, error) {
	if current != nil {
		return nil, errors.New("more than one sequencer option")
	}
	if opt.accepter == nil || opt.receiptLogs == nil || opt.pendingTxs == nil {
		return nil, errors.New("sequencer option needs an accepter and a receipt log feed; build it with NewSequencerOption")
	}
	return &opt, nil
}

func NewEVMHTTPServer(
	config evmrpcconfig.Config,
	tmClient client.LocalClient,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	stateStore types.StateStore,
	opts ...ServerOption,
) (EVMServer, error) {
	if tmClient == nil || k == nil || ctxProvider == nil || txConfigProvider == nil || app == nil {
		return nil, errors.New("EVM HTTP server dependencies are not configured")
	}
	ctx := ctxProvider(LatestCtxHeight)
	if config.EnableUnsafeKeyringRPC && evmCfg.IsLiveChainID(ctx) {
		return nil, errors.New("unsafe keyring RPC cannot be enabled on a live chain")
	}

	// Initialize global worker pool with configuration (metrics are embedded in pool)
	InitGlobalWorkerPool(config.WorkerPoolSize, config.WorkerQueueSize)

	// Get pool for logging and DB semaphore setup
	pool := GetGlobalWorkerPool()
	workerCount := pool.WorkerCount()
	queueSize := pool.QueueSize()

	// Set DB semaphore capacity in metrics (aligned with worker count)
	// Only set once to avoid races when multiple test servers start in parallel.
	pool.Metrics.DBSemaphoreCapacity.CompareAndSwap(0, int32(workerCount)) //nolint:gosec // G115: safe, max is 64

	debugEnabled := IsDebugMetricsEnabled()
	logger.Info("Started EVM RPC metrics exporter (interval: 5s)", "workers", workerCount, "queue", queueSize, "db_semaphore", workerCount, "debug_stdout", debugEnabled)
	if !debugEnabled {
		logger.Info("To enable debug metrics output to stdout, set EVM_DEBUG_METRICS=true")
	}

	// Initialize RPC tracker
	stats.InitRPCTracker(ctxProvider(LatestCtxHeight).Context(), config.RPCStatsInterval)

	httpServer := NewHTTPServer(rpc.HTTPTimeouts{
		ReadTimeout:       config.ReadTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
	})
	methodTimeout := tmutils.Some(httpServer.timeouts.WriteTimeout)
	if err := httpServer.SetListenAddr(config.HTTPAddress, config.HTTPPort); err != nil {
		return nil, err
	}
	simulateConfig := &SimulateConfig{
		GasCap:                       config.SimulationGasLimit,
		EVMTimeout:                   config.SimulationEVMTimeout,
		MaxConcurrentSimulationCalls: config.MaxConcurrentSimulationCalls,
	}
	watermarks := NewWatermarkManager(tmClient, ctxProvider, stateStore, k.ReceiptStore())

	traceCtxProvider := defaultTraceContextProvider(ctxProvider)
	var sequencerReceipts SequencerReceiptOption
	var seq *SequencerOption
	for _, opt := range opts {
		switch o := opt.(type) {
		case TraceContextProvider:
			if o != nil {
				traceCtxProvider = o
			}
		case SequencerReceiptOption:
			sequencerReceipts = o
		case SequencerOption:
			var err error
			if seq, err = sequencerOption(o, seq); err != nil {
				return nil, err
			}
		}
	}

	globalBlockCache := NewBlockCache(3000)
	cacheCreationMutex := &sync.Mutex{}
	sendAPI := NewSendAPI(tmClient, txConfigProvider, seq.sendConfig(config), k, beginBlockKeepers, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeHTTP, methodTimeout, globalBlockCache, cacheCreationMutex, watermarks)
	sequencerReceiptAPI, err := newSequencerReceiptAPI(tmClient, ctxProvider, sequencerReceipts, ConnectionTypeHTTP)
	if err != nil {
		return nil, err
	}
	txAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP, methodTimeout, watermarks, globalBlockCache, cacheCreationMutex, config.EnableUnsafeKeyringRPC)
	debugAPI := NewDebugAPI(tmClient, k, beginBlockKeepers, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex, watermarks)
	debugAPI.backend.SetTraceContextProvider(traceCtxProvider)
	if config.TraceBakeEnabled {
		StartTraceBakerForDebugAPI(debugAPI, TraceBakerConfig{
			Workers:      config.TraceBakeWorkers,
			QueueSize:    config.TraceBakeQueueSize,
			Tracers:      config.TraceBakeTracers,
			WindowBlocks: config.TraceBakeWindowBlocks,
			TipFn:        func() int64 { return ctxProvider(LatestCtxHeight).BlockHeight() },
		})
	}
	isPanicOrSyntheticTxFunc := debugAPI.isPanicOrSyntheticTx
	paxLegacyAllowlist := BuildPaxLegacyEnabledSet(config.EnabledLegacyPaxApis)

	paxTxAPI := NewPaxTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP, methodTimeout, isPanicOrSyntheticTxFunc, watermarks, globalBlockCache, cacheCreationMutex, config.EnableUnsafeKeyringRPC)
	paxDebugAPI := NewPaxDebugAPI(tmClient, k, beginBlockKeepers, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex, watermarks)
	paxDebugAPI.backend.SetTraceContextProvider(traceCtxProvider)

	// DB semaphore aligned with worker count
	dbReadSemaphore := make(chan struct{}, workerCount)
	globalLogSlicePool := NewLogSlicePool()
	filterAPI := func(namespace string) *FilterAPI {
		api := NewFilterAPI(
			tmClient,
			k,
			ctxProvider,
			txConfigProvider,
			&FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog},
			ConnectionTypeHTTP,
			namespace,
			dbReadSemaphore,
			globalBlockCache,
			cacheCreationMutex,
			globalLogSlicePool,
			watermarks,
		)
		if seq != nil {
			api.SetReceiptLogFeed(seq.receiptLogs)
		}
		return api
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "pax",
			Service:   NewPaxBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "pax2",
			Service:   NewPax2BlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   txAPI,
		},
		{
			Namespace: "pax",
			Service:   paxTxAPI,
		},
		{
			Namespace: "pax",
			Service:   sequencerReceiptAPI,
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeHTTP, txConfigProvider(LatestCtxHeight).TxDecoder(), watermarks, config.EnableUnsafeKeyringRPC),
		},
		{
			Namespace: "eth",
			Service:   sendAPI,
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   filterAPI("eth"),
		},
		{
			Namespace: "pax",
			Service:   filterAPI("pax"),
		},
		{
			Namespace: "pax",
			Service:   NewAssociationAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, watermarks),
		},
		{
			Namespace: "txpool",
			Service:   NewTxPoolAPI(tmClient, k, ctxProvider, txConfigProvider, &TxPoolConfig{maxNumTxs: int(config.MaxTxPoolTxs)}, ConnectionTypeHTTP), //nolint:gosec
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
		{
			Namespace: "debug",
			Service:   debugAPI,
		},
		{
			Namespace: "pax",
			Service:   paxDebugAPI,
		},
	}
	// Test API can only exist on non-live chain IDs.  These APIs instrument certain overrides.
	if config.EnableTestAPI && !evmCfg.IsLiveChainID(ctx) {
		logger.Info("Enabling Test EVM APIs")
		apis = append(apis, rpc.API{
			Namespace: "test",
			Service:   NewTestAPI(),
		})
	} else {
		logger.Info("Disabling Test EVM APIs", "liveChainID", evmCfg.IsLiveChainID(ctx), "enableTestAPI", config.EnableTestAPI)
	}

	if err := httpServer.EnableRPC(apis, HTTPConfig{
		CorsAllowedOrigins: strings.Split(config.CORSOrigins, ","),
		Vhosts:             []string{"*"},
		DenyList:           config.DenyList,
		PaxLegacyAllowlist: paxLegacyAllowlist,
	}); err != nil {
		return nil, err
	}

	return httpServer, nil
}

func NewEVMWebSocketServer(
	config evmrpcconfig.Config,
	tmClient client.LocalClient,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	stateStore types.StateStore,
	blockHeaderNotifier *BlockHeaderNotifier,
	opts ...ServerOption,
) (EVMServer, error) {
	if tmClient == nil || k == nil || ctxProvider == nil || txConfigProvider == nil || app == nil {
		return nil, errors.New("EVM WebSocket server dependencies are not configured")
	}
	var seq *SequencerOption
	for _, opt := range opts {
		o, ok := opt.(SequencerOption)
		if !ok {
			return nil, fmt.Errorf("EVM WebSocket server takes no %T option", opt)
		}
		var err error
		if seq, err = sequencerOption(o, seq); err != nil {
			return nil, err
		}
	}
	ctx := ctxProvider(LatestCtxHeight)
	if config.EnableUnsafeKeyringRPC && evmCfg.IsLiveChainID(ctx) {
		return nil, errors.New("unsafe keyring RPC cannot be enabled on a live chain")
	}
	// Initialize global worker pool with configuration (metrics are embedded in pool)
	// This is idempotent - if HTTP server already initialized it, this is a no-op
	InitGlobalWorkerPool(config.WorkerPoolSize, config.WorkerQueueSize)

	// Initialize WebSocket tracker.
	stats.InitWSTracker(ctxProvider(LatestCtxHeight).Context(), config.RPCStatsInterval)

	httpServer := NewHTTPServer(rpc.HTTPTimeouts{
		ReadTimeout:       config.ReadTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
	})
	methodTimeout := tmutils.Some(httpServer.timeouts.WriteTimeout)
	if err := httpServer.SetListenAddr(LocalAddress, config.WSPort); err != nil {
		return nil, err
	}
	simulateConfig := &SimulateConfig{
		GasCap:                       config.SimulationGasLimit,
		EVMTimeout:                   config.SimulationEVMTimeout,
		MaxConcurrentSimulationCalls: config.MaxConcurrentSimulationCalls,
	}
	watermarks := NewWatermarkManager(tmClient, ctxProvider, stateStore, k.ReceiptStore())
	// DB semaphore aligned with worker count
	dbReadSemaphore := make(chan struct{}, GetGlobalWorkerPool().WorkerCount())
	globalBlockCache := NewBlockCache(3000)
	cacheCreationMutex := &sync.Mutex{}
	globalLogSlicePool := NewLogSlicePool()
	subscriptionAPI, err := NewSubscriptionAPI(tmClient, k, ctxProvider, &LogFetcher{
		tmClient:           tmClient,
		k:                  k,
		ctxProvider:        ctxProvider,
		txConfigProvider:   txConfigProvider,
		dbReadSemaphore:    dbReadSemaphore,
		globalBlockCache:   globalBlockCache,
		cacheCreationMutex: cacheCreationMutex,
		globalLogSlicePool: globalLogSlicePool,
		watermarks:         watermarks,
	}, &SubscriptionConfig{subscriptionCapacity: 100, newHeadLimit: config.MaxSubscriptionsNewHead}, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeWS, blockHeaderNotifier)
	if err != nil {
		return nil, err
	}
	if seq != nil {
		subscriptionAPI.receiptLogs = seq.receiptLogs
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeWS, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeWS, methodTimeout, watermarks, globalBlockCache, cacheCreationMutex, config.EnableUnsafeKeyringRPC),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeWS, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeWS, txConfigProvider(LatestCtxHeight).TxDecoder(), watermarks, config.EnableUnsafeKeyringRPC),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfigProvider, seq.sendConfig(config), k, beginBlockKeepers, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeWS, methodTimeout, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeWS, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   subscriptionAPI,
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
	}

	wsConfig := WsConfig{Origins: strings.Split(config.WSOrigins, ",")}
	wsConfig.readLimit = DefaultWebsocketMaxMessageSize
	if err := httpServer.EnableWS(apis, wsConfig); err != nil {
		return nil, err
	}

	return httpServer, nil
}
