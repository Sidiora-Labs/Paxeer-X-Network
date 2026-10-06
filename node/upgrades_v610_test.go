package app

import (
	"bytes"
	"testing"
	"time"

	evmconfig "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	layerxcustodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	appparams "github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// writeV610UpgradeInfo writes the upgrade info file an operator leaves for the
// v6.10 plan, through the keeper that writes it and into the home the
// application reads, so the begin blocker reads exactly what a node reads.
func writeV610UpgradeInfo(t *testing.T, a *App, height int64) {
	t.Helper()
	require.NoError(t, a.UpgradeKeeper.DumpUpgradeInfoToDisk(height, V610Upgrade))
	info, err := a.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Equal(t, V610Upgrade, info.Name)
	require.Equal(t, height, info.Height)
}

func TestV610IsRegisteredBesideThePlansThatPrecedeIt(t *testing.T) {
	tags, err := f.ReadFile("tags")
	require.NoError(t, err)
	names := parseUpgradesList(string(tags))
	require.NotContains(t, names, V610Upgrade)
	require.NotContains(t, names, ActivationUpgrade)
	require.Equal(t, xwebUpgrade, LatestUpgrade)
	require.Equal(t, 1, semver.Compare(V610Upgrade, ActivationUpgrade))

	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	require.True(t, a.UpgradeKeeper.HasHandler(V610Upgrade))

	// The plans that precede it keep their names, their handlers and their store
	// lists, and the new plan adds, deletes and renames no store.
	require.True(t, a.UpgradeKeeper.HasHandler(ActivationUpgrade))
	require.True(t, a.UpgradeKeeper.HasHandler(xwebUpgrade))
	require.True(t, a.UpgradeKeeper.HasHandler(sidioraFeeTokenUpgrade))
	_, ok := layerxStoreUpgrades(V610Upgrade)
	require.False(t, ok)
	require.Equal(t, []string{layerxcustodytypes.StoreKey}, v65StoreUpgrades().Added)
	activation, ok := layerxStoreUpgrades(ActivationUpgrade)
	require.True(t, ok)
	require.Equal(t, activationStoreUpgrades(), activation)
	require.Equal(t, append(v66StoreUpgrades().Added, v68StoreUpgrades().Added...), activation.Added)
	require.Empty(t, activation.Deleted)
	require.Empty(t, activation.Renamed)

	require.Equal(t, []string{ActivationUpgrade, V610Upgrade, V611Upgrade}, knownUpgradePlans())
}

func TestV610AttestorsAreTheSuppliedSetInAscendingOrder(t *testing.T) {
	require.Len(t, XWebAttestors, 4)
	require.Equal(t, uint32(3), XWebThreshold)
	for i, attestor := range XWebAttestors {
		require.NoError(t, attestor.Validate(), attestor.Signer.Hex())
		require.Len(t, attestor.PublicKey, xwebtypes.EnvelopeKeyLength)
		if i > 0 {
			require.Negative(t, bytes.Compare(XWebAttestors[i-1].Signer[:], attestor.Signer[:]))
		}
	}
	set := xwebtypes.AttestorSet{Attestors: XWebAttestors, Threshold: XWebThreshold}
	require.NoError(t, set.Validate())
	require.Equal(t, XWebThreshold, xwebtypes.Majority(len(XWebAttestors)))
}

func TestV610WritesTheOperatingValuesInTheBlockTheUpgradeInfoNames(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx

	const height = int64(42)
	writeV610UpgradeInfo(t, a, height)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)

	anchorBefore := a.LayerXAnchorKeeper.GetParams(at)
	custodyBefore := a.LayerXCustodyKeeper.GetParams(at)
	launchpadBefore := a.LaunchpadKeeper.GetParams(at)
	depositBefore := a.GovKeeper.GetDepositParams(at)
	tallyBefore := a.GovKeeper.GetTallyParams(at)
	versionsBefore := a.UpgradeKeeper.GetModuleVersionMap(at)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, V610Upgrade))
	require.True(t, a.XWebKeeper.IsPaused(at))
	require.Empty(t, a.XWebKeeper.GetAttestorSet(at).Attestors)

	a.BeginBlock(at, height, nil, nil, false)

	// The plan ran on the standard path: the keeper recorded it done in the block
	// the file named and left no plan behind. It adds no store and no module, so
	// the module version map is the one the block started with.
	require.Equal(t, height, a.UpgradeKeeper.GetDoneHeight(at, V610Upgrade))
	require.Equal(t, versionsBefore, a.UpgradeKeeper.GetModuleVersionMap(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)

	// The anchor and custody identifiers are the EVM chain identifier the
	// application derives for its own chain id; every other field is as it was.
	chainID := evmconfig.GetEVMChainID(a.ChainID).Uint64()
	require.NotZero(t, chainID)
	anchorWant := anchorBefore
	anchorWant.PaxeerChainID = chainID
	anchorWant.NetworkID = uint32(chainID)
	require.Equal(t, anchorWant, a.LayerXAnchorKeeper.GetParams(at))
	custodyWant := custodyBefore
	custodyWant.NetworkId = uint32(chainID)
	require.Equal(t, custodyWant, a.LayerXCustodyKeeper.GetParams(at))

	// The launchpad quote denomination is the base coin unit; every other field is
	// as it was.
	launchpadWant := launchpadBefore
	launchpadWant.QuoteDenom = appparams.BaseCoinUnit
	require.Equal(t, launchpadWant, a.LaunchpadKeeper.GetParams(at))

	// The voting periods are the shortened ones and the deposit and tally
	// parameters are untouched.
	voting := a.GovKeeper.GetVotingParams(at)
	require.Equal(t, time.Hour, voting.VotingPeriod)
	require.Equal(t, 20*time.Minute, voting.ExpeditedVotingPeriod)
	require.Equal(t, time.Hour, voting.GetVotingPeriod(false))
	require.Equal(t, 20*time.Minute, voting.GetVotingPeriod(true))
	require.Equal(t, depositBefore, a.GovKeeper.GetDepositParams(at))
	require.Equal(t, tallyBefore, a.GovKeeper.GetTallyParams(at))

	// The web-search module carries the supplied signers in ascending order, a
	// threshold of three, and is no longer paused.
	set := a.XWebKeeper.GetAttestorSet(at)
	require.Equal(t, XWebAttestors, set.Attestors)
	require.Equal(t, XWebThreshold, set.Threshold)
	require.Equal(t, XWebThreshold, a.XWebKeeper.Threshold(at))
	require.NoError(t, set.Validate())
	require.False(t, a.XWebKeeper.IsPaused(at))

	// The file is still on disk in the blocks that follow, and none of them applies
	// the plan a second time or writes the set again.
	next := ctx.WithBlockHeight(height + 1)
	recordPreviousProposer(a, next, valPub)
	a.BeginBlock(next, height+1, nil, nil, false)
	require.Equal(t, height, a.UpgradeKeeper.GetDoneHeight(next, V610Upgrade))
	require.Equal(t, set, a.XWebKeeper.GetAttestorSet(next))
	_, found = a.UpgradeKeeper.GetUpgradePlan(next)
	require.False(t, found)
}

func TestV610WritesNoWebSearchStateFromAnEmptyAttestorList(t *testing.T) {
	supplied := XWebAttestors
	XWebAttestors = nil
	defer func() { XWebAttestors = supplied }()

	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx

	const height = int64(42)
	writeV610UpgradeInfo(t, a, height)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)

	a.BeginBlock(at, height, nil, nil, false)

	// The plan applied and wrote the chain's values, and the web-search module is
	// as its own genesis left it: no attestor, no threshold and still paused.
	require.Equal(t, height, a.UpgradeKeeper.GetDoneHeight(at, V610Upgrade))
	require.Equal(t, appparams.BaseCoinUnit, a.LaunchpadKeeper.GetParams(at).QuoteDenom)
	require.Equal(t, time.Hour, a.GovKeeper.GetVotingParams(at).VotingPeriod)
	require.Empty(t, a.XWebKeeper.GetAttestorSet(at).Attestors)
	require.Zero(t, a.XWebKeeper.Threshold(at))
	require.True(t, a.XWebKeeper.IsPaused(at))
}

func TestV610IgnoresAnUpgradeInfoThatNamesAnotherHeight(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx

	const height = int64(42)
	writeV610UpgradeInfo(t, a, height+5)
	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)

	anchorBefore := a.LayerXAnchorKeeper.GetParams(at)
	custodyBefore := a.LayerXCustodyKeeper.GetParams(at)
	launchpadBefore := a.LaunchpadKeeper.GetParams(at)
	votingBefore := a.GovKeeper.GetVotingParams(at)

	a.BeginBlock(at, height, nil, nil, false)

	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, V610Upgrade))
	require.Equal(t, anchorBefore, a.LayerXAnchorKeeper.GetParams(at))
	require.Equal(t, custodyBefore, a.LayerXCustodyKeeper.GetParams(at))
	require.Equal(t, launchpadBefore, a.LaunchpadKeeper.GetParams(at))
	require.Equal(t, votingBefore, a.GovKeeper.GetVotingParams(at))
	require.Empty(t, a.XWebKeeper.GetAttestorSet(at).Attestors)
	require.True(t, a.XWebKeeper.IsPaused(at))
	_, found := a.UpgradeKeeper.GetUpgradePlan(at)
	require.False(t, found)
}
