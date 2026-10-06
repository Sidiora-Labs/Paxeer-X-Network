package blocksync

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/mempool"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/consensus"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/eventbus"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/p2p"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/proxy"
	sm "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state"
	sf "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state/test/factory"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/test/factory"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	pb "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/blocksync"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

type reactorTestSuite struct {
	network *p2p.TestNetwork
	nodes   []types.NodeID

	reactors map[types.NodeID]*Reactor
}

func setup(
	ctx context.Context,
	t *testing.T,
	genDoc *types.GenesisDoc,
	privVal types.PrivValidator,
	maxBlockHeights []int64,
) *reactorTestSuite {
	t.Helper()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	numNodes := len(maxBlockHeights)
	require.True(t, numNodes >= 1,
		"must specify at least one block height (nodes)")

	rts := &reactorTestSuite{
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		nodes:    make([]types.NodeID, 0, numNodes),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
	}

	for i, nodeID := range rts.network.NodeIDs() {
		rts.addNode(ctx, t, nodeID, genDoc, privVal, maxBlockHeights[i])
	}

	t.Cleanup(func() {
		cancel()
		for _, nodeID := range rts.nodes {
			if rts.reactors[nodeID].IsRunning() {
				rts.reactors[nodeID].Wait()

				require.False(t, rts.reactors[nodeID].IsRunning())
			}
		}
	})
	t.Cleanup(leaktest.Check(t))

	return rts
}

func makeReactor(
	ctx context.Context,
	t *testing.T,
	genDoc *types.GenesisDoc,
	router *p2p.Router,
	blockSync bool,
	restartEvent func(),
	selfRemediationConfig *config.SelfRemediationConfig,
) *Reactor {

	app := abci.BaseApplication{}

	blockDB := dbm.NewMemDB()
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(blockDB)
	proxyApp := proxy.New(app, proxy.NopMetrics())

	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	require.NoError(t, stateStore.Save(state))
	mp := mempool.NewTxMempool(mempool.TestConfig(), proxyApp, mempool.NopMetrics(), mempool.NopTxConstraintsFetcher)
	bus := eventbus.NewDefault()
	require.NoError(t, bus.Start(ctx))

	blockExec := sm.NewBlockExecutor(
		stateStore,
		proxyApp,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		bus,
		sm.NopMetrics(),
		types.DefaultConsensusPolicy(),
	)

	r, err := NewReactor(
		stateStore,
		blockStore,
		router,
		utils.Some(SyncerConfig{
			BlockExec:             blockExec,
			ConsReactor:           utils.None[ConsensusReactor](),
			BlockSync:             blockSync,
			Metrics:               consensus.NopMetrics(),
			EventBus:              nil, // eventbus can be nil
			RestartEvent:          restartEvent,
			SelfRemediationConfig: selfRemediationConfig,
		}),
	)
	if err != nil {
		t.Fatalf("NewReactor(): %v", err)
	}
	return r
}

func (rts *reactorTestSuite) addNode(
	ctx context.Context,
	t *testing.T,
	nodeID types.NodeID,
	genDoc *types.GenesisDoc,
	privVal types.PrivValidator,
	maxBlockHeight int64,
) {
	t.Helper()

	rts.nodes = append(rts.nodes, nodeID)

	remediationConfig := config.DefaultSelfRemediationConfig()
	remediationConfig.BlocksBehindThreshold = 1000

	reactor := makeReactor(
		ctx,
		t,
		genDoc,
		rts.network.Node(nodeID).Router,
		true,
		func() {},
		remediationConfig,
	)
	lastCommit := &types.Commit{}

	state, err := reactor.stateStore.Load()
	require.NoError(t, err)
	for blockHeight := int64(1); blockHeight <= maxBlockHeight; blockHeight++ {
		block, blockID, partSet, seenCommit := makeNextBlock(ctx, t, state, privVal, blockHeight, lastCommit)

		syncer := reactor.syncer.OrPanic("syncer should be configured in tests")
		state, err = syncer.blockExec.ApplyBlock(ctx, state, blockID, block, nil)
		require.NoError(t, err)

		reactor.store.SaveBlock(block, partSet, seenCommit)
		lastCommit = seenCommit
	}

	rts.reactors[nodeID] = reactor
	require.NoError(t, reactor.Start(ctx))
	require.True(t, reactor.IsRunning())
}

func makeNextBlock(ctx context.Context,
	t *testing.T,
	state sm.State,
	signer types.PrivValidator,
	height int64,
	lc *types.Commit) (*types.Block, types.BlockID, *types.PartSet, *types.Commit) {
	block := sf.MakeBlock(state, height, lc)
	partSet, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: partSet.Header()}

	// Simulate a commit for the current height
	vote, err := factory.MakeVote(
		ctx,
		signer,
		block.Header.ChainID,
		0,
		block.Header.Height,
		0,
		2,
		blockID,
		time.Now(),
	)
	require.NoError(t, err)
	seenCommit := &types.Commit{
		Height:     vote.Height,
		Round:      vote.Round,
		BlockID:    blockID,
		Signatures: []types.CommitSig{vote.CommitSig()},
	}
	return block, blockID, partSet, seenCommit
}

func (rts *reactorTestSuite) start(t *testing.T) {
	t.Helper()
	rts.network.Start(t)
}

func TestReactor_AbruptDisconnect(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(64)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})

	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())

	rts.start(t)

	secondarySyncer := rts.reactors[rts.nodes[1]].syncer.OrPanic("syncer should be configured in tests")

	require.Eventually(
		t,
		func() bool {
			pool := secondarySyncer.pool.Load()
			if pool == nil {
				return false
			}
			height, _, _ := pool.GetStatus()
			return pool.MaxPeerHeight() == maxBlockHeight && height > 0 && height <= maxBlockHeight
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)

	// Remove synced node from the syncing node which should not result in any
	// deadlocks or race conditions within the context of poolRoutine.
	rts.network.Remove(t, rts.nodes[0])
}

// TestReactor_OnStopWaitsForGoroutines is a regression test for the
// "panic: leveldb/table: reader released" shutdown panic seen on v6.4.4
// sentry nodes. Before the fix, blocksync's long-running goroutines
// (Reactor.requestRoutine, Reactor.poolRoutine, Reactor.processBlockSyncCh,
// Reactor.processPeerUpdates, Reactor.autoRestartIfBehind, and
// BlockPool.makeRequestersRoutine) were started with raw `go fn(ctx)` using
// the outer ctx, instead of `Spawn(...)` which would register them with the
// BaseService WaitGroup and bind them to BaseService.inner.ctx. As a result,
// Reactor.Stop() / BlockPool.Stop() — which cancels only the inner ctx —
// did not signal these goroutines to exit, let alone wait for them. The
// node's OnStop then proceeded to n.blockStore.Close() while poolRoutine
// was still mid-SaveBlock -> Base() -> bs.db.Iterator, causing goleveldb to
// panic when the table reader was released underneath the live iterator.
//
// This test asserts the fix: after `reactor.Stop()` returns, the
// blocksync-package goroutines have exited. The outer ctx is still live at
// this point in the test, so the unfixed code keeps them running and the
// assertion fails deterministically. On failure the live goroutine stacks
// are dumped to make the leak obvious.
func TestReactor_OnStopWaitsForGoroutines(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_stop_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())

	rts := setup(ctx, t, genDoc, privVals[0], []int64{0})

	reactor := rts.reactors[rts.nodes[0]]
	require.True(t, reactor.IsRunning())

	dumpBlocksyncGoroutines := func() (string, int) {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		var out strings.Builder
		count := 0
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			if !strings.Contains(g, "/internal/blocksync.") {
				continue
			}
			// The test functions themselves live in the blocksync package, so
			// runtime.Stack reports them as matches. Only count background
			// routines spawned by Reactor.OnStart and BlockPool.OnStart,
			// which are created by libs/service.Spawn, not testing.tRunner.
			if strings.Contains(g, "testing.tRunner") {
				continue
			}
			out.WriteString(g)
			out.WriteString("\n\n")
			count++
		}
		return out.String(), count
	}

	// OnStart Spawns 5 reactor routines and BlockPool.OnStart Spawns 1.
	require.Eventually(t, func() bool {
		_, c := dumpBlocksyncGoroutines()
		return c >= 6
	}, 5*time.Second, 10*time.Millisecond, "blocksync goroutines did not start")

	reactor.Stop()
	require.False(t, reactor.IsRunning())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, c := dumpBlocksyncGoroutines(); c == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	dump, c := dumpBlocksyncGoroutines()
	t.Fatalf("%d blocksync goroutine(s) still alive after Reactor.Stop() returned. "+
		"This means at least one routine was not registered with the "+
		"BaseService WaitGroup via Spawn(), so Stop did not wait for it. "+
		"Live stacks:\n\n%s", c, dump)
}

func TestReactor_SyncTime(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(101)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})
	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())
	rts.start(t)

	require.Eventually(
		t,
		func() bool {
			pool := rts.reactors[rts.nodes[1]].syncer.OrPanic("syncer should be configured in tests").pool.Load()
			if pool == nil {
				return false
			}
			return rts.reactors[rts.nodes[1]].GetRemainingSyncTime() > time.Nanosecond &&
				pool.getLastSyncRate() > 0.001
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)
}

type MockBlockStore struct {
	mock.Mock
	sm.BlockStore
}

func (m *MockBlockStore) Height() int64 {
	args := m.Called()
	return args.Get(0).(int64)
}

func TestAutoRestartIfBehind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		blocksBehindThreshold     uint64
		blocksBehindCheckInterval time.Duration
		selfHeight                int64
		selfHeights               []int64
		progressing               bool
		maxPeerHeight             int64
		isBlockSync               bool
		restartExpected           bool
	}{
		{
			name:                      "Should not restart if blocksBehindThreshold is 0",
			blocksBehindThreshold:     0,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			selfHeight:                100,
			maxPeerHeight:             200,
			isBlockSync:               false,
			restartExpected:           false,
		},
		{
			name:                      "Should not restart if behindHeight is less than threshold",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             140,
			isBlockSync:               false,
			restartExpected:           false,
		},
		{
			name:                      "Should restart if behindHeight is greater than or equal to threshold",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             160,
			isBlockSync:               false,
			restartExpected:           true,
		},
		{
			name:                      "Should not restart if blocksync",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             160,
			isBlockSync:               true,
			restartExpected:           false,
		},
		{
			name:                      "Should not restart if behind but self height advances between checks",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			progressing:               true,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             100000,
			isBlockSync:               false,
			restartExpected:           false,
		},
		{
			name:                      "Should restart if behind and self height is unchanged at two consecutive checks",
			blocksBehindThreshold:     50,
			selfHeight:                110,
			selfHeights:               []int64{100, 105, 110},
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             1000,
			isBlockSync:               false,
			restartExpected:           true,
		},
		{
			name:                      "Should not restart if not behind and self height is unchanged",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             120,
			isBlockSync:               false,
			restartExpected:           false,
		},
	}

	for _, tt := range tests {
		t.Log(tt.name)
		t.Run(tt.name, func(t *testing.T) {
			mockBlockStore := new(MockBlockStore)
			switch {
			case tt.progressing:
				for i := range int64(10000) {
					mockBlockStore.On("Height").Return(tt.selfHeight + i).Once()
				}
			default:
				for _, h := range tt.selfHeights {
					mockBlockStore.On("Height").Return(h).Once()
				}
				mockBlockStore.On("Height").Return(tt.selfHeight)
			}

			blockPool := &BlockPool{
				height:        tt.selfHeight,
				maxPeerHeight: tt.maxPeerHeight,
			}

			restart := utils.NewAtomicSend(false)
			syncer := &syncController{
				store:                     mockBlockStore,
				blocksBehindThreshold:     tt.blocksBehindThreshold,
				blocksBehindCheckInterval: tt.blocksBehindCheckInterval,
				restartEvent:              func() { restart.Store(true) },
			}
			if tt.isBlockSync {
				syncer.blockSync.Store(true)
			}
			r := &Reactor{syncer: utils.Some(syncer)}

			ctx := t.Context()
			if tt.restartExpected {
				r.syncer.OrPanic("syncer").autoRestartIfBehind(ctx, blockPool)
				assert.True(t, restart.Load(), "Expected restart but did not occur")
			} else {
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				r.syncer.OrPanic("syncer").autoRestartIfBehind(ctx, blockPool)
				assert.False(t, restart.Load(), "Unexpected restart")
			}
		})
	}
}

// TestAutoRestartIfBehind_DistanceTrend drives the real syncController over a
// real BlockPool whose single peer's height is set at every check, so the
// restart decision is judged on the distance behind that peer between two
// checks and not on the node's own height advancing: a node that advances at
// chain speed with a constant or growing distance restarts, a node whose
// distance is closing does not, and the block-sync and cooldown skips stay.
func TestAutoRestartIfBehind_DistanceTrend(t *testing.T) {
	t.Parallel()

	type step struct{ self, peer int64 }
	seq := func(n int, self0, selfStep, peer0, peerStep int64) []step {
		steps := make([]step, n)
		for i := range steps {
			steps[i] = step{self: self0 + int64(i)*selfStep, peer: peer0 + int64(i)*peerStep}
		}
		return steps
	}

	tests := []struct {
		name            string
		steps           []step
		isBlockSync     bool
		cooldownSeconds uint64
		restartExpected bool
	}{
		{
			name:            "Should not restart while the distance behind the peer shrinks between checks",
			steps:           seq(200, 100, 2, 1000, 1),
			restartExpected: false,
		},
		{
			name:            "Should restart when the node advances at chain speed and the distance stays constant",
			steps:           seq(10, 100, 1, 93574, 1),
			restartExpected: true,
		},
		{
			name:            "Should restart when the node advances and the distance grows",
			steps:           seq(10, 100, 1, 1000, 2),
			restartExpected: true,
		},
		{
			name:            "Should not restart on a constant distance while already in block sync",
			steps:           seq(200, 100, 1, 1000, 1),
			isBlockSync:     true,
			restartExpected: false,
		},
		{
			name:            "Should not restart on a growing distance before the cooldown has passed",
			steps:           seq(200, 100, 1, 1000, 2),
			cooldownSeconds: 3600,
			restartExpected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := types.NodeID(strings.Repeat("c", 40))
			pool := NewBlockPool(1, makeRouter(testPeers{
				peer: {id: peer, base: 1, height: 1, inputChan: make(chan inputData, 1)},
			}))

			mockBlockStore := new(MockBlockStore)
			for _, st := range tt.steps {
				mockBlockStore.On("Height").Return(st.self).Run(func(mock.Arguments) {
					pool.SetPeerRange(peer, 1, st.peer)
				}).Once()
			}
			last := tt.steps[len(tt.steps)-1]
			mockBlockStore.On("Height").Return(last.self)

			restart := utils.NewAtomicSend(false)
			syncer := &syncController{
				store:                     mockBlockStore,
				blocksBehindThreshold:     50,
				blocksBehindCheckInterval: 10 * time.Millisecond,
				restartCooldownSeconds:    tt.cooldownSeconds,
				restartEvent:              func() { restart.Store(true) },
			}
			if tt.isBlockSync {
				syncer.blockSync.Store(true)
			}

			if tt.restartExpected {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				syncer.autoRestartIfBehind(ctx, pool)
				assert.True(t, restart.Load(), "Expected restart but did not occur")
				assert.True(t, syncer.blockSync.Load(), "Expected block sync to be set on restart")
			} else {
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				syncer.autoRestartIfBehind(ctx, pool)
				assert.False(t, restart.Load(), "Unexpected restart")
			}
		})
	}
}

func makeValidationFailurePair(
	ctx context.Context,
	t *testing.T,
	testRootName string,
) (sm.State, *types.Block, *types.Block) {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), testRootName)
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	initialState, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)

	lastCommit := &types.Commit{}
	block1, _, _, seenCommit1 := makeNextBlock(ctx, t, initialState, privVals[0], 1, lastCommit)
	block2, _, _, _ := makeNextBlock(ctx, t, initialState, privVals[0], 2, seenCommit1)

	badBlock2Proto, err := block2.ToProto()
	require.NoError(t, err)
	badBlock2Proto.LastCommit.Signatures[0].Signature[0] ^= 0xFF
	badCommit, err := types.CommitFromProto(badBlock2Proto.LastCommit)
	require.NoError(t, err)
	badBlock2Proto.Header.LastCommitHash = badCommit.Hash()
	badBlock2, err := types.BlockFromProto(badBlock2Proto)
	require.NoError(t, err)

	return initialState, block1, badBlock2
}

func TestPoolRoutine_DoesNotReturnOnValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_validation_failure_does_not_return")

	badPeer := types.NodeID(strings.Repeat("a", 40))
	goodPeer := types.NodeID(strings.Repeat("b", 40))
	router := makeRouter(testPeers{
		badPeer:  {id: badPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer: {id: goodPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	})
	pool := NewBlockPool(1, router)
	done := make(chan error, 1)
	go func() { done <- pool.run(ctx) }()
	t.Cleanup(func() {
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("pool.run(): %v", err)
		}
	})
	pool.SetPeerRange(badPeer, 1, 2)

	evictNetwork := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})
	syncer := &syncController{
		router:  evictNetwork.Node(evictNetwork.NodeIDs()[0]).Router,
		metrics: consensus.NopMetrics(),
	}

	results := make(chan error, 1)
	go func() {
		_, err := syncer.poolRoutine(ctx, pool, initialState, false)
		results <- err
	}()
	t.Cleanup(func() {
		err := <-results
		require.ErrorIs(t, err, context.Canceled)
	})

	introducedGoodPeer := false
	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned early after validation failure: %v", err)
		case request := <-pool.Requests():
			if request.PeerID == goodPeer {
				return
			}

			switch request.Height {
			case 1:
				_ = pool.AddBlock(request.PeerID, block1, block1.Size())
			case 2:
				_ = pool.AddBlock(request.PeerID, badBlock2, badBlock2.Size())
				if !introducedGoodPeer {
					introducedGoodPeer = true
					pool.SetPeerRange(goodPeer, 1, 2)
				}
			}
		}
	}
}

func TestPoolRoutine_RetriesAfterValidationFailure(t *testing.T) {
	ctx := t.Context()

	initialState, block1, badBlock2 := makeValidationFailurePair(ctx, t, "block_sync_retry_after_validation_failure")
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})

	badPeer := types.NodeID(strings.Repeat("a", 40))
	goodPeer1 := types.NodeID(strings.Repeat("b", 40))
	goodPeer2 := types.NodeID(strings.Repeat("c", 40))
	peers := testPeers{
		badPeer:   {id: badPeer, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer1: {id: goodPeer1, base: 1, height: 2, inputChan: make(chan inputData, 1)},
		goodPeer2: {id: goodPeer2, base: 1, height: 2, inputChan: make(chan inputData, 1)},
	}
	pool := NewBlockPool(1, makeRouter(peers))
	runPoolForTest(t, pool)
	pool.SetPeerRange(badPeer, 1, 2)

	syncer := &syncController{
		router:  network.Node(network.NodeIDs()[0]).Router,
		metrics: consensus.NopMetrics(),
	}

	results := make(chan error, 1)
	go func() {
		_, err := syncer.poolRoutine(ctx, pool, initialState, false)
		results <- err
	}()
	t.Cleanup(func() {
		err := <-results
		require.ErrorIs(t, err, context.Canceled)
	})

	introducedGoodPeers := false
	height1Requests := map[types.NodeID]int{}

	for {
		select {
		case err := <-results:
			t.Fatalf("poolRoutine returned before retry was observed: %v", err)
		case request := <-pool.Requests():
			if request.Height == 1 {
				height1Requests[request.PeerID]++
				if request.PeerID != badPeer && height1Requests[request.PeerID] == 1 {
					return
				}
			}

			if request.PeerID == badPeer && request.Height == 2 && !introducedGoodPeers {
				introducedGoodPeers = true
				pool.SetPeerRange(goodPeer1, 1, 2)
				pool.SetPeerRange(goodPeer2, 1, 2)
			}

			if request.PeerID == badPeer {
				switch request.Height {
				case 1:
					_ = pool.AddBlock(request.PeerID, block1, block1.Size())
				case 2:
					_ = pool.AddBlock(request.PeerID, badBlock2, badBlock2.Size())
				}
			}
		}
	}
}

func TestQueryResponder_ServesBlockRequestsWhenBlockSyncDisabled(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_query_responder_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 2})
	nodeIDs := network.NodeIDs()

	server := makeReactor(
		ctx,
		t,
		genDoc,
		network.Node(nodeIDs[0]).Router,
		false,
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	lastCommit := &types.Commit{}
	state, err := server.stateStore.Load()
	require.NoError(t, err)
	for height := int64(1); height <= 3; height++ {
		block, blockID, partSet, seenCommit := makeNextBlock(ctx, t, state, privVals[0], height, lastCommit)
		state, err = server.syncer.OrPanic("syncer should be configured in tests").blockExec.ApplyBlock(ctx, state, blockID, block, nil)
		require.NoError(t, err)
		server.store.SaveBlock(block, partSet, seenCommit)
		lastCommit = seenCommit
	}
	require.NoError(t, server.Start(ctx))
	t.Cleanup(server.Wait)

	client := p2p.TestMakeChannelNoCleanup(t, network.Node(nodeIDs[1]), GetChannelDescriptor())
	network.Start(t)

	client.Send(wrap(&pb.BlockRequest{Height: 2}), nodeIDs[0])
	for range 2 {
		msg, err := client.Recv(ctx)
		require.NoError(t, err)
		if blockResponse, ok := msg.Message.Sum.(*pb.Message_BlockResponse); ok {
			require.Equal(t, int64(2), blockResponse.BlockResponse.GetBlock().Header.Height)
			require.Equal(t, nodeIDs[0], msg.From)
			return
		}
	}
	t.Fatal("did not receive block response")
}

func TestQueryResponder_ServesStatusRequestsWhenBlockSyncDisabled(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_query_status_test")
	require.NoError(t, err)

	valSet, _ := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 2})
	nodeIDs := network.NodeIDs()

	server := makeReactor(
		ctx,
		t,
		genDoc,
		network.Node(nodeIDs[0]).Router,
		false,
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	require.NoError(t, server.Start(ctx))
	t.Cleanup(server.Wait)

	client := p2p.TestMakeChannelNoCleanup(t, network.Node(nodeIDs[1]), GetChannelDescriptor())
	network.Start(t)

	client.Send(wrap(&pb.StatusRequest{}), nodeIDs[0])
	msg, err := client.Recv(ctx)
	require.NoError(t, err)

	statusResponse, ok := msg.Message.Sum.(*pb.Message_StatusResponse)
	require.True(t, ok)
	require.Equal(t, server.store.Base(), statusResponse.StatusResponse.GetBase())
	require.Equal(t, server.store.Height(), statusResponse.StatusResponse.GetHeight())
	require.Equal(t, nodeIDs[0], msg.From)
}
