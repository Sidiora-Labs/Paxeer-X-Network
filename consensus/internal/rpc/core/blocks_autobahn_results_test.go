package core

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/example/code"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/example/kvstore"
	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	atypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/autobahn/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/merkle"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/autobahn/consensus"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/autobahn/producer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/p2p"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/p2p/conn"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/proxy"
	sm "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils/scope"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils/tcp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

func TestAutobahnBlockResultsCarryTxResults(t *testing.T) {
	const maxTxsPerBlock = 3
	const maxGasPerBlock = 1_000

	ctx := t.Context()
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 1)
	validatorKey := validatorKeys[0]
	nodeKey := p2p.NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
	addr := tcp.TestReserveAddr()
	genDoc := &types.GenesisDoc{
		ChainID:       "autobahn-block-results-test",
		InitialHeight: 1,
		AppState:      json.RawMessage(`{}`),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := proxy.New(kvstore.NewApplication(), proxy.NopMetrics())
	stateStore := sm.NewStore(dbm.NewMemDB())
	endpoint := p2p.Endpoint{AddrPort: addr}
	nodeInfo := types.NodeInfo{
		NodeID:     nodeKey.Public().NodeID(),
		ListenAddr: addr.String(),
		Network:    genDoc.ChainID,
		Moniker:    "autobahn-block-results",
		Channels:   []byte{},
	}
	router, err := p2p.NewRouter(
		p2p.NopMetrics(),
		nodeKey,
		func() *types.NodeInfo { return &nodeInfo },
		dbm.NewMemDB(),
		&p2p.RouterOptions{
			SelfAddress:              utils.Some(endpoint.NodeAddress(nodeKey.Public().NodeID())),
			Endpoint:                 endpoint,
			Connection:               conn.DefaultMConnConfig(),
			IncomingConnectionWindow: utils.Some(time.Duration(0)),
			MaxAcceptRate:            utils.Some(rate.Inf),
			MaxDialRate:              utils.Some(rate.Limit(30)),
			Giga: utils.Some(&p2p.GigaRouterConfig{
				DialInterval: time.Second,
				ValidatorAddrs: map[atypes.PublicKey]p2p.GigaNodeAddr{
					validatorKey.Public(): {
						Key:      nodeKey.Public(),
						HostPort: tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()},
					},
				},
				Consensus: &consensus.Config{
					Key:                validatorKey,
					ViewTimeout:        func(atypes.View) time.Duration { return time.Hour },
					PersistentStateDir: utils.None[string](),
				},
				Producer: &producer.Config{
					App:                     app,
					MaxGasWantedPerBlock:    maxGasPerBlock,
					MaxGasEstimatedPerBlock: maxGasPerBlock,
					MaxTxsPerBlock:          maxTxsPerBlock,
					MaxTxsPerSecond:         utils.None[uint64](),
					// The block holding the three transactions is sealed by the
					// fourth insertion, never by the interval.
					BlockInterval: time.Hour,
				},
				GenDoc:     genDoc,
				StateStore: stateStore,
			}),
		},
	)
	require.NoError(t, err)
	giga, ok := router.Giga().Get()
	require.True(t, ok, "router has no GigaRouter")
	// Under Autobahn the node's Comet block store stays empty.
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	env := &Environment{App: app, StateStore: stateStore, BlockStore: blockStore, Router: router}

	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("router", func() error { return utils.IgnoreCancel(router.Run(ctx)) })
		var txs []types.Tx
		for i := range maxTxsPerBlock + 1 {
			tx := types.Tx(fmt.Sprintf("key%d=value%d", i, i))
			if _, err := giga.InsertTx(ctx, tx); err != nil {
				return fmt.Errorf("giga.InsertTx(): %w", err)
			}
			txs = append(txs, tx)
		}
		blockTxs := types.Txs(txs[:maxTxsPerBlock])

		// Find the global block holding the three transactions and wait until
		// the router has executed it and persisted its FinalizeBlock response.
		var height int64
		require.Eventually(t, func() bool {
			for h := int64(1); h <= giga.LastCommittedBlockNumber(); h++ {
				rb, err := giga.BlockByNumber(ctx, atypes.GlobalBlockNumber(h)) //nolint:gosec // h is positive
				if err != nil || len(rb.Block.Data.Txs) == 0 {
					continue
				}
				height = h
				_, err = stateStore.LoadFinalizeBlockResponses(h)
				return err == nil
			}
			return false
		}, time.Minute, 10*time.Millisecond, "block of three transactions was not executed")
		rb, err := giga.BlockByNumber(ctx, atypes.GlobalBlockNumber(height)) //nolint:gosec // height is positive
		require.NoError(t, err)
		require.Equal(t, blockTxs, rb.Block.Data.Txs)

		stored, err := stateStore.LoadFinalizeBlockResponses(height)
		require.NoError(t, err)
		res, err := env.BlockResults(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(&height)})
		require.NoError(t, err)
		require.Equal(t, height, res.Height)
		require.Len(t, res.TxsResults, maxTxsPerBlock)
		require.Equal(t, stored.TxResults, res.TxsResults)
		for i, txResult := range res.TxsResults {
			require.Equal(t, code.CodeTypeOK, txResult.Code, "TxsResults[%d].Code", i)
		}
		require.Equal(t, stored.Events, res.FinalizeBlockEvents)
		require.Equal(t, stored.ValidatorUpdates, res.ValidatorUpdates)
		require.Equal(t, int64(0), res.TotalGasUsed)
		require.NotNil(t, res.ConsensusParamUpdates)
		require.NotNil(t, res.ConsensusParamUpdates.Block)
		require.Equal(t, int64(maxGasPerBlock), res.ConsensusParamUpdates.Block.MaxGas)

		// The receiptsRoot served for the block is the Comet results root over
		// these three results, not the empty-tree root.
		leaves, err := abci.MarshalTxResults(res.TxsResults)
		require.NoError(t, err)
		wantLeaves, err := abci.MarshalTxResults([]*abci.ExecTxResult{
			{Code: code.CodeTypeOK},
			{Code: code.CodeTypeOK},
			{Code: code.CodeTypeOK},
		})
		require.NoError(t, err)
		receiptsRoot := merkle.HashFromByteSlices(leaves)
		require.Equal(t, merkle.HashFromByteSlices(wantLeaves), receiptsRoot)
		require.NotEqual(t, merkle.HashFromByteSlices(nil), receiptsRoot)

		// A node whose state store never recorded the execution of this height
		// refuses it with the load error instead of serving an empty result.
		unexecuted := &Environment{App: app, StateStore: sm.NewStore(dbm.NewMemDB()), BlockStore: blockStore, Router: router}
		_, err = unexecuted.BlockResults(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(&height)})
		require.ErrorIs(t, err, sm.ErrNoFinalizeBlockResponsesForHeight{Height: height})
		return nil
	})
	require.NoError(t, err)
}
