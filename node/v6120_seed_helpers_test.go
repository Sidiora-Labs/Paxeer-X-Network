package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// v6120HaltExportPath is the state paxd export wrote at the halt height of a
// test chain that applied the activation and v6.10 plans.
var v6120HaltExportPath = filepath.Join("testdata", "v6120-halt-export.json")

// v6120HaltHeightsFile names the record, beside a halt export, of the done
// heights the source chain's live upgrade store held at export. The upgrade
// module exports "{}", so the export itself cannot carry them.
const v6120HaltHeightsFile = "v6120-halt-heights.json"

// readV6120HaltExport reads the genesis document paxd export writes.
func readV6120HaltExport(t *testing.T, path string) *tmtypes.GenesisDoc {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	doc, err := tmtypes.GenesisDocFromJSON(raw)
	require.NoError(t, err)
	require.NotNil(t, doc.ConsensusParams)
	return doc
}

// readV6120DoneHeights reads the activation and v6.10 done heights recorded
// beside the halt export at path.
func readV6120DoneHeights(t *testing.T, path string) map[string]int64 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), v6120HaltHeightsFile))
	require.NoError(t, err)
	var heights map[string]int64
	require.NoError(t, json.Unmarshal(raw, &heights))
	require.Len(t, heights, 2)
	require.Positive(t, heights[ActivationUpgrade])
	require.Positive(t, heights[V610Upgrade])
	return heights
}

// v6120HaltHeight is the height H the export at path was taken at. paxd export
// writes initial_height as the next block, H+1, so a test that applies the
// v6.12.0 plan to the export writes the upgrade-info document for H+1.
func v6120HaltHeight(t *testing.T, path string) int64 {
	t.Helper()
	doc := readV6120HaltExport(t, path)
	require.Greater(t, doc.InitialHeight, int64(1))
	return doc.InitialHeight - 1
}

// seedV6120FromExport builds a test application, initialises it from the
// genesis exported at path exactly as a node starting from that document
// would, at initial height H+1, and writes the recorded activation and v6.10
// done heights into its live upgrade store. The wrapper's context stands at
// the halt height H.
func seedV6120FromExport(t *testing.T, path string) *TestWrapper {
	t.Helper()
	doc := readV6120HaltExport(t, path)
	halt := v6120HaltHeight(t, path)
	heights := readV6120DoneHeights(t, path)

	w := NewTestWrapper(t, doc.GenesisTime, secp256k1.GenPrivKey().PubKey(), true)
	a := w.App
	params := doc.ConsensusParams.ToProto()
	_, err := a.InitChain(context.Background(), &abci.RequestInitChain{
		Time:            doc.GenesisTime,
		ChainId:         doc.ChainID,
		ConsensusParams: &params,
		AppStateBytes:   doc.AppState,
		InitialHeight:   doc.InitialHeight,
	})
	require.NoError(t, err)
	w.Ctx = a.NewContext(false, tmproto.Header{Height: halt, ChainID: doc.ChainID, Time: doc.GenesisTime})
	for _, name := range []string{ActivationUpgrade, V610Upgrade} {
		require.LessOrEqual(t, heights[name], halt, name)
		a.UpgradeKeeper.SetDone(w.Ctx.WithBlockHeight(heights[name]), name)
	}
	return w
}

func TestV6120HaltExportLoads(t *testing.T) {
	doc := readV6120HaltExport(t, v6120HaltExportPath)
	heights := readV6120DoneHeights(t, v6120HaltExportPath)
	halt := v6120HaltHeight(t, v6120HaltExportPath)
	require.Equal(t, doc.InitialHeight-1, halt)
	require.Less(t, heights[ActivationUpgrade], heights[V610Upgrade])

	w := seedV6120FromExport(t, v6120HaltExportPath)
	a, ctx := w.App, w.Ctx
	require.Equal(t, halt, ctx.BlockHeight())
	require.Equal(t, doc.ChainID, a.ChainID)

	// The live upgrade store reports the done heights the source chain held,
	// and the v6.12.0 plan has not run.
	require.Equal(t, heights[ActivationUpgrade], a.UpgradeKeeper.GetDoneHeight(ctx, ActivationUpgrade))
	require.Equal(t, heights[V610Upgrade], a.UpgradeKeeper.GetDoneHeight(ctx, V610Upgrade))
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, V6120Upgrade))

	// The launchpad state is the exported one, and the export carries no
	// native market, so the v6.12.0 plan can apply to it.
	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc.AppState, &appState))
	exported, ok := appState[launchpadtypes.ModuleName]
	require.True(t, ok)
	var want launchpadtypes.GenesisState
	require.NoError(t, json.Unmarshal(exported, &want))
	require.NoError(t, want.Validate())
	require.Empty(t, want.Markets)
	got, err := json.Marshal(a.LaunchpadKeeper.ExportGenesis(ctx))
	require.NoError(t, err)
	require.JSONEq(t, string(exported), string(got))
	require.Zero(t, a.LaunchpadKeeper.GetMarketCount(ctx))

	// The SID proxy slots the handler reads hold the snapshot balances and a
	// legacy supply above their sum, and no migration is recorded yet.
	holders, listed, err := parseSIDHolders(sidHoldersJSON)
	require.NoError(t, err)
	sum := sdk.ZeroInt()
	for i, holder := range holders {
		slot := a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidLegacyBalanceSlot(holder))
		require.Equal(t, common.BigToHash(listed[i].BigInt()), slot, holder.Hex())
		sum = sum.Add(listed[i])
	}
	supply := a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidLegacyTotalSupplySlot()).Big()
	require.Positive(t, supply.Cmp(sum.BigInt()))
	require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()))
}
