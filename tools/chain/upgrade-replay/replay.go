package main

import (
	"bytes"
	"fmt"
	evmconfig "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	layerxgovtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	appparams "github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"reflect"
	"strings"
	"time"

	ecommon "github.com/ethereum/go-ethereum/common"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"

	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	layerxanchortypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// checks records the outcome of the replay's assertions as it prints them.
type checks struct {
	failures int
	passes   int
}

func (c *checks) pass(format string, args ...interface{}) {
	c.passes++
	fmt.Printf("  ok   %s\n", fmt.Sprintf(format, args...))
}

func (c *checks) fail(format string, args ...interface{}) {
	c.failures++
	fmt.Printf("  FAIL %s\n", fmt.Sprintf(format, args...))
}

// assert records one claim about the replayed state.
func (c *checks) assert(held bool, format string, args ...interface{}) bool {
	if held {
		c.pass(format, args...)
	} else {
		c.fail(format, args...)
	}
	return held
}

// note prints an observation that is not an assertion.
func (c *checks) note(format string, args ...interface{}) {
	fmt.Printf("  note %s\n", fmt.Sprintf(format, args...))
}

// guard runs fn and records a failure when it panics, so one panicking read
// does not cost the rest of the report.
func (c *checks) guard(what string, fn func()) bool {
	panicked := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = true
				c.fail("%s panicked: %v", what, recovered)
			}
		}()
		fn()
	}()
	return !panicked
}

// nonZero reports whether value differs from the zero value of its type.
func nonZero(value interface{}) bool {
	return !reflect.DeepEqual(value, reflect.Zero(reflect.TypeOf(value)).Interface())
}

// writeUpgradeInfo writes the upgrade-info.json a node's old binary leaves
// behind when it halts for a plan, through the upgrade keeper that writes it,
// so the application's store loader reads exactly what a node would read.
func writeUpgradeInfo(home string, height int64, plan string) error {
	cdc := app.MakeEncodingConfig().Marshaler
	keeper := upgradekeeper.NewKeeper(map[int64]bool{}, sdk.NewKVStoreKey(upgradetypes.StoreKey), cdc, home, nil)
	return keeper.DumpUpgradeInfoToDisk(height, plan)
}

// replay applies plan to the state under home as the block at the height after
// the committed one, asserts what the fork owes the chain, commits once and
// reopens the store. It returns the number of failed assertions.
func replay(home string, opts appOptions, plan, chainID string, pre *preState) int {
	c := &checks{}
	upgradeHeight := pre.Height + 1
	blockTime := time.Now().UTC()

	fmt.Printf("plan %s replays over the state at height %d, as the block at height %d\n",
		plan, pre.Height, upgradeHeight)

	if err := writeUpgradeInfo(home, upgradeHeight, plan); err != nil {
		c.fail("writing the plan's upgrade-info.json: %v", err)
		return c.failures
	}
	c.pass("the plan is named in upgrade-info.json at height %d, as a halted node leaves it", upgradeHeight)

	fmt.Println("opening the application over the state; a plan that does not declare every store the state lacks ends the process here")
	a := openApp(home, opts)
	opened := true
	defer func() {
		if opened {
			_ = a.Close()
		}
	}()
	c.pass("the application loaded the state with the plan's store upgrades mounted")
	info, err := a.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if !c.assert(err == nil && info.Name == plan && info.Height == upgradeHeight, "the applied plan and height match upgrade-info.json") {
		return c.failures
	}

	c.assert(a.LastBlockHeight() == pre.Height,
		"the loaded state is at height %d (loaded %d)", pre.Height, a.LastBlockHeight())
	if !c.assert(a.UpgradeKeeper.HasHandler(plan), "the application registers a handler for plan %s", plan) {
		c.note("without a handler the upgrade keeper would panic, so the plan is not applied")
		return c.failures
	}

	cms := a.CommitMultiStore()
	mounted := 0
	for _, name := range allStoreKeys() {
		key := a.GetKey(name)
		if key != nil && cms.GetCommitKVStore(key) != nil {
			mounted++
		}
	}
	c.assert(mounted == len(allStoreKeys()),
		"the load mounted all %d module stores (%d mounted)", len(allStoreKeys()), mounted)
	for _, name := range pre.Missing {
		key := a.GetKey(name)
		c.assert(key != nil && cms.GetCommitKVStore(key) != nil,
			"the plan added the store %s the state did not carry", name)
	}

	// A node applies a plan on the cache-wrapped state of the block, where the
	// writes a module makes are visible to the reads that follow them; on the
	// committed store itself a write is only visible after the commit. The replay
	// applies the plan the same way and writes the block through below, so the
	// handler and the assertions read what a node's block would read.
	ctx, writeBlock := blockContext(a, upgradeHeight, chainID, blockTime).CacheContext()
	before := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	fmt.Printf("version map before the plan: %d modules\n", len(before))
	for _, name := range forkModules {
		if version, known := before[name]; known {
			c.note("the state already carries module %s at consensus version %d", name, version)
		}
	}

	var operatingBefore operatingState
	if plan == app.V610Upgrade {
		operatingBefore = readOperatingState(a, ctx)
		c.assert(a.UpgradeKeeper.GetDoneHeight(ctx, app.ActivationUpgrade) > 0, "v6.10 starts from recorded post-activation state")
		c.assert(len(pre.Missing) == 0, "v6.10 adds, deletes and renames no mounted store")
		assertEmptyOwnerList(c, a, ctx, upgradeHeight)
	}
	if !c.guard("applying plan "+plan, func() {
		a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: plan, Height: upgradeHeight})
	}) {
		return c.failures
	}
	c.pass("the plan handler ran over the state without a panic")

	after := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	if plan == app.V610Upgrade {
		c.assert(reflect.DeepEqual(before, after), "v6.10 preserves the post-activation module version map")
	}
	fmt.Printf("version map after the plan: %d modules\n", len(after))
	for _, name := range forkModules {
		if version, known := after[name]; known {
			c.pass("the version map carries module %s at consensus version %d", name, version)
		} else {
			c.fail("the version map carries module %s", name)
		}
	}
	done := a.UpgradeKeeper.GetDoneHeight(ctx, plan)
	c.assert(done == upgradeHeight, "plan %s is recorded done at height %d (recorded %d)", plan, upgradeHeight, done)

	if plan == app.V610Upgrade {
		assertOperatingValues(c, a, ctx, operatingBefore, chainID)
		assertGovernanceRoute(c, a, ctx)
	} else {
		assertForkGenesis(c, a, ctx)
		assertPrecompiles(c, a, ctx, upgradeHeight)
	}
	persistedOperating := readOperatingState(a, ctx)

	writeBlock()
	c.pass("the block wrote the plan's state through to the committed store")
	commitID := cms.Commit(true)
	c.assert(commitID.Version == upgradeHeight,
		"the single commit wrote version %d (wrote %d)", upgradeHeight, commitID.Version)
	// The composite commit store hands every child store out through an adapter
	// whose version is the whole store's version and whose root hash fails
	// closed, so a per-store initial version cannot be read through the
	// application. What the added stores carry at the committed version is read
	// instead, through the same store the modules read.
	c.note("the store backend exposes no per-store version, so each added store is read at the committed version instead")
	committed := blockContext(a, upgradeHeight, chainID, blockTime)
	for _, state := range forkModuleStates {
		key := a.GetKey(state.StoreKey)
		if key == nil {
			c.fail("the added store %s is mounted after the commit", state.StoreKey)
			continue
		}
		c.assert(committed.KVStore(key).Has(state.ParamsKey),
			"the added store %s holds the %s parameters at committed version %d",
			state.StoreKey, state.Module, commitID.Version)
	}

	if err := a.Close(); err != nil {
		c.fail("closing the application after the commit: %v", err)
	}
	opened = false

	fmt.Println("reopening the application over the committed state")
	b := openApp(home, opts)
	defer func() { _ = b.Close() }()
	c.pass("the application reopened the committed state, so the commit left the store loadable")
	c.assert(b.LastBlockHeight() == upgradeHeight,
		"the reopened state is at height %d (loaded %d)", upgradeHeight, b.LastBlockHeight())
	reopened := blockContext(b, upgradeHeight, chainID, blockTime)
	reopenedDone := b.UpgradeKeeper.GetDoneHeight(reopened, plan)
	c.assert(reopenedDone == upgradeHeight,
		"the reopened state records plan %s done at height %d (records %d)", plan, upgradeHeight, reopenedDone)
	persisted := b.UpgradeKeeper.GetModuleVersionMap(reopened)
	kept := 0
	for _, name := range forkModules {
		if _, known := persisted[name]; known {
			kept++
		}
	}
	c.assert(kept == len(forkModules),
		"the reopened version map carries all %d fork modules (carries %d)", len(forkModules), kept)
	assertPrecompilesServed(c, b, reopened, upgradeHeight)
	if plan == app.V610Upgrade {
		c.assert(reflect.DeepEqual(persistedOperating, readOperatingState(b, reopened)), "v6.10 operating values and all six governance effects survive commit and reopen")
	}

	return c.failures
}

// forkModuleState names a module the fork brings, the store it writes and the
// entry its own InitGenesis leaves behind.
type forkModuleState struct {
	Module    string
	StoreKey  string
	ParamsKey []byte
}

// forkModuleStates is the initialisation evidence each fork module owes, taken
// from each module's own types package.
var forkModuleStates = []forkModuleState{
	{layerxcustodytypes.ModuleName, layerxcustodytypes.StoreKey, layerxcustodytypes.ParamsKey},
	{layerxanchortypes.ModuleName, layerxanchortypes.StoreKey, layerxanchortypes.ParamsKey},
	{layerxexchangetypes.ModuleName, layerxexchangetypes.StoreKey, layerxexchangetypes.ParamsKey},
	{layerxbridgetypes.ModuleName, layerxbridgetypes.StoreKey, layerxbridgetypes.ParamsKey},
	{launchpadtypes.ModuleName, launchpadtypes.StoreKey, launchpadtypes.ParamsKey},
	{xwebtypes.ModuleName, xwebtypes.StoreKey, xwebtypes.ParamsKey},
}

// assertForkGenesis reads the genesis state the plan wrote for every module the
// fork brings, through each module's own store entry and keeper.
func assertForkGenesis(c *checks, a *app.App, ctx sdk.Context) {
	for _, state := range forkModuleStates {
		key := a.GetKey(state.StoreKey)
		if key == nil {
			c.fail("the %s store is mounted", state.StoreKey)
			continue
		}
		c.assert(ctx.KVStore(key).Has(state.ParamsKey),
			"the plan initialised the %s module: its store carries the params entry", state.Module)
	}
	c.guard("reading the xweb state", func() {
		c.assert(a.XWebKeeper.IsPaused(ctx), "the xweb module is paused")
		attestors := a.XWebKeeper.GetAttestorSet(ctx).Attestors
		c.assert(len(attestors) == 0, "the xweb attestor set is empty (carries %d)", len(attestors))
		params := a.XWebKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the xweb params are readable and valid")
		c.assert(nonZero(params), "the xweb params are not the zero value")
		c.note("the xweb authority is %s", params.Authority)
	})
	c.guard("reading the layerxcustody params", func() {
		params := a.LayerXCustodyKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxcustody params are readable and valid")
		c.assert(nonZero(params), "the layerxcustody params are not the zero value")
		c.note("the layerxcustody params are the module's own defaults: %t",
			reflect.DeepEqual(params, layerxcustodytypes.DefaultParams()))
	})
	c.guard("reading the layerxanchor params", func() {
		params := a.LayerXAnchorKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxanchor params are readable and valid")
		c.assert(nonZero(params), "the layerxanchor params are not the zero value")
		c.note("the layerxanchor authority is %s", params.Authority)
	})
	c.guard("reading the layerxexchange params", func() {
		params := a.LayerXExchangeKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxexchange params are readable and valid")
		c.note("the layerxexchange module's own DefaultParams is the zero value, so its params carry no non-zero claim; the params entry above is its initialisation evidence")
		c.note("the layerxexchange params are the module's own defaults: %t",
			reflect.DeepEqual(params, layerxexchangetypes.DefaultParams()))
	})
	c.guard("reading the layerxbridge params", func() {
		params := a.LayerXBridgeKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the layerxbridge params are readable and valid")
		c.assert(nonZero(params), "the layerxbridge params are not the zero value")
		c.note("the layerxbridge authority is %s", params.Authority)
	})
	c.guard("reading the launchpad params", func() {
		params := a.LaunchpadKeeper.GetParams(ctx)
		c.assert(params.Validate() == nil, "the launchpad params are readable and valid")
		c.assert(nonZero(params), "the launchpad params are not the zero value")
		c.note("the launchpad params are the module's own defaults: %t",
			reflect.DeepEqual(params, launchpadtypes.DefaultParams()))
	})
	c.guard("reading the fee-token params of the EVM module", func() {
		params := a.EvmKeeper.GetParams(ctx)
		c.assert(params.MaxFeeTokenRateAge > 0,
			"the fee-token rate age bound is set (%d blocks)", params.MaxFeeTokenRateAge)
		c.assert(!params.MaxFeeTokenSpread.IsNil() && params.MaxFeeTokenSpread.IsPositive(),
			"the fee-token spread bound is set (%s)", params.MaxFeeTokenSpread)
	})
}

// assertPrecompiles checks that the fork's precompile addresses are reachable
// through the EVM keeper at the upgrade height and were not reachable at the
// height before it.
func assertPrecompiles(c *checks, a *app.App, ctx sdk.Context, upgradeHeight int64) {
	at := a.EvmKeeper.CustomPrecompiles(ctx)
	earlier := a.EvmKeeper.CustomPrecompiles(ctx.WithBlockHeight(upgradeHeight - 1))
	reached := 0
	for _, precompile := range forkPrecompiles {
		address := ecommon.HexToAddress(precompile.Address)
		_, served := at[address]
		_, servedEarlier := earlier[address]
		now := c.assert(served, "the %s precompile at %s is served at height %d",
			precompile.Name, precompile.Address, upgradeHeight)
		before := c.assert(!servedEarlier, "the %s precompile at %s is not served at height %d",
			precompile.Name, precompile.Address, upgradeHeight-1)
		if now && before {
			reached++
		}
	}
	fmt.Printf("precompile reachability at the fork: %d of %d addresses served at %d and not at %d\n",
		reached, len(forkPrecompiles), upgradeHeight, upgradeHeight-1)
}

// assertPrecompilesServed checks that the fork's precompile addresses are still
// reachable after the state was committed and reopened.
func assertPrecompilesServed(c *checks, a *app.App, ctx sdk.Context, height int64) {
	served := a.EvmKeeper.CustomPrecompiles(ctx)
	count := 0
	for _, precompile := range forkPrecompiles {
		if _, ok := served[ecommon.HexToAddress(precompile.Address)]; ok {
			count++
		}
	}
	c.assert(count == len(forkPrecompiles),
		"the reopened state serves all %d fork precompiles at height %d (serves %d)",
		len(forkPrecompiles), height, count)
}

type operatingState struct {
	Anchor       layerxanchortypes.Params
	Custody      layerxcustodytypes.Params
	Launchpad    launchpadtypes.Params
	Exchange     layerxexchangetypes.Params
	Web          xwebtypes.Params
	Attestors    xwebtypes.AttestorSet
	WebPaused    bool
	BridgePaused bool
	Emergency    bool
	Deposit      govtypes.DepositParams
	Tally        govtypes.TallyParams
	Voting       govtypes.VotingParams
}

func readOperatingState(a *app.App, ctx sdk.Context) operatingState {
	return operatingState{Anchor: a.LayerXAnchorKeeper.GetParams(ctx), Custody: a.LayerXCustodyKeeper.GetParams(ctx), Launchpad: a.LaunchpadKeeper.GetParams(ctx), Exchange: a.LayerXExchangeKeeper.GetParams(ctx), Web: a.XWebKeeper.GetParams(ctx), Attestors: a.XWebKeeper.GetAttestorSet(ctx), WebPaused: a.XWebKeeper.IsPaused(ctx), BridgePaused: a.LayerXBridgeKeeper.IsPaused(ctx), Emergency: a.LayerXCustodyKeeper.GetEmergency(ctx), Deposit: a.GovKeeper.GetDepositParams(ctx), Tally: a.GovKeeper.GetTallyParams(ctx), Voting: a.GovKeeper.GetVotingParams(ctx)}
}
func assertEmptyOwnerList(c *checks, a *app.App, ctx sdk.Context, height int64) {
	before := readOperatingState(a, ctx)
	empty, _ := ctx.CacheContext()
	c.guard("v6.10 with empty owner list", func() {
		supplied := app.XWebAttestors
		app.XWebAttestors = nil
		defer func() { app.XWebAttestors = supplied }()
		a.UpgradeKeeper.ApplyUpgrade(empty, upgradetypes.Plan{Name: app.V610Upgrade, Height: height})
		c.assert(reflect.DeepEqual(before.Attestors, a.XWebKeeper.GetAttestorSet(empty)), "v6.10 empty owner list preserves web-search attestors and threshold")
		c.assert(before.WebPaused == a.XWebKeeper.IsPaused(empty), "v6.10 empty owner list preserves web-search pause")
	})
	c.assert(a.UpgradeKeeper.GetDoneHeight(ctx, app.V610Upgrade) == 0, "the empty-list trial left the replay source unmodified")
}
func assertOperatingValues(c *checks, a *app.App, ctx sdk.Context, before operatingState, chainID string) {
	chain := evmconfig.GetEVMChainID(chainID)
	if !c.assert(chain.IsUint64() && chain.Uint64() > 0 && chain.Uint64() <= uint64(^uint32(0)), "v6.10 derives a nonzero representable EVM network identifier") {
		return
	}
	after := readOperatingState(a, ctx)
	c.assert(after.Voting.VotingPeriod == time.Hour, "v6.10 voting period is one hour")
	c.assert(after.Voting.ExpeditedVotingPeriod == 20*time.Minute, "v6.10 expedited voting period is twenty minutes")
	c.assert(reflect.DeepEqual(before.Deposit, after.Deposit), "v6.10 deposit parameters and minimum deposit remain unchanged")
	c.assert(reflect.DeepEqual(before.Tally, after.Tally), "v6.10 quorum and both thresholds remain unchanged")
	anchor := before.Anchor
	anchor.PaxeerChainID = chain.Uint64()
	anchor.NetworkID = uint32(chain.Uint64())
	c.assert(reflect.DeepEqual(anchor, after.Anchor), "v6.10 anchor chain and network IDs match EVM and other parameters remain unchanged")
	custody := before.Custody
	custody.NetworkId = uint32(chain.Uint64())
	c.assert(reflect.DeepEqual(custody, after.Custody), "v6.10 custody network ID matches EVM and other parameters remain unchanged")
	launchpad := before.Launchpad
	launchpad.QuoteDenom = appparams.BaseCoinUnit
	c.assert(reflect.DeepEqual(launchpad, after.Launchpad), "v6.10 launchpad quote denom is the base coin and other parameters remain unchanged")
	c.assert(len(app.XWebAttestors) == 4 && len(after.Attestors.Attestors) == 4, "v6.10 carries four supplied web-search signers")
	c.assert(reflect.DeepEqual(app.XWebAttestors, after.Attestors.Attestors), "v6.10 web-search signers, payouts and compressed public keys match supplied entries")
	ascending := true
	for i, attestor := range after.Attestors.Attestors {
		if attestor.Validate() != nil || len(attestor.PublicKey) != xwebtypes.EnvelopeKeyLength {
			ascending = false
		}
		if i > 0 && bytes.Compare(after.Attestors.Attestors[i-1].Signer[:], attestor.Signer[:]) >= 0 {
			ascending = false
		}
	}
	c.assert(ascending, "v6.10 web-search signer order and compressed public keys are valid")
	c.assert(after.Attestors.Threshold == 3 && a.XWebKeeper.Threshold(ctx) == 3, "v6.10 web-search threshold is three")
	c.assert(!after.WebPaused, "v6.10 web-search is unpaused")
}
func assertGovernanceRoute(c *checks, a *app.App, ctx sdk.Context) {
	before := readOperatingState(a, ctx)
	authority := layerxgovtypes.GovernanceAuthority()
	anchor := before.Anchor
	anchor.ReporterShare = sdk.NewDecWithPrec(2, 1)
	if anchor.ReporterShare.Equal(before.Anchor.ReporterShare) {
		anchor.ReporterShare = sdk.NewDecWithPrec(3, 1)
	}
	exchange := before.Exchange
	exchange.Markets = append([]layerxexchangetypes.Market(nil), exchange.Markets...)
	if len(exchange.Markets) == 0 {
		exchange.Markets = []layerxexchangetypes.Market{{MarketId: strings.Repeat("01", 32), MarginAssetId: strings.Repeat("02", 32), Enabled: true}}
	} else {
		exchange.Markets[0].Enabled = !exchange.Markets[0].Enabled
	}
	launchpad := before.Launchpad
	launchpad.ProtocolFeeBps = 2000
	if before.Launchpad.ProtocolFeeBps == 2000 {
		launchpad.ProtocolFeeBps = 1000
	}
	web := before.Web
	web.Fee = sdk.NewInt(1)
	if before.Web.Fee.Equal(web.Fee) {
		web.Fee = sdk.NewInt(2)
	}
	var bridge sdk.Msg = &layerxbridgetypes.MsgPause{Authority: authority}
	if before.BridgePaused {
		bridge = &layerxbridgetypes.MsgUnpause{Authority: authority}
	}
	messages := []sdk.Msg{
		&layerxcustodytypes.MsgSetEmergency{Authority: authority, Enabled: !before.Emergency},
		&layerxanchortypes.MsgUpdateParams{Authority: authority, Params: anchor},
		&layerxexchangetypes.MsgUpdateParams{Authority: authority, Params: exchange},
		bridge,
		&launchpadtypes.MsgUpdateParams{Authority: authority, Params: launchpad},
		&xwebtypes.MsgSetParams{Authority: authority, Fee: web.Fee, MaxPayloadBytes: web.MaxPayloadBytes, MaxCallbackGas: web.MaxCallbackGas, TimeoutBlocks: web.TimeoutBlocks},
	}
	proposal, err := layerxgovtypes.NewLayerXProposal("Replay six authority messages", "Execute every fork module through the application router", messages...)
	if !c.assert(err == nil, "six-module proposal construction succeeds: %v", err) {
		return
	}
	if !c.assert(proposal.ValidateBasic() == nil, "six-module proposal validates every authority message") {
		return
	}
	raw, err := a.AppCodec().MarshalInterface(proposal)
	if !c.assert(err == nil, "six-module proposal packs through the application codec: %v", err) {
		return
	}
	var content govtypes.Content
	err = a.AppCodec().UnmarshalInterface(raw, &content)
	if !c.assert(err == nil, "six-module proposal unpacks through the application registry: %v", err) {
		return
	}
	router := a.GovKeeper.Router()
	if !c.assert(router.HasRoute(layerxgovtypes.RouterKey), "the application registers the six-module governance route") {
		return
	}
	handler := router.GetRoute(layerxgovtypes.RouterKey)
	err = handler(ctx, content)
	if !c.assert(err == nil, "governance proposal executed all six authority messages: %v", err) {
		return
	}
	after := readOperatingState(a, ctx)
	c.assert(after.Emergency == !before.Emergency, "governance custody authority message effect is present")
	c.assert(reflect.DeepEqual(anchor, after.Anchor), "governance anchor authority message effect is present")
	c.assert(reflect.DeepEqual(exchange, after.Exchange), "governance exchange authority message effect is present")
	c.assert(after.BridgePaused == !before.BridgePaused, "governance bridge authority message effect is present")
	c.assert(reflect.DeepEqual(launchpad, after.Launchpad), "governance launchpad authority message effect is present")
	c.assert(reflect.DeepEqual(web, after.Web), "governance web-search authority message effect is present")
	foreign := &banktypes.MsgSend{FromAddress: authority, ToAddress: authority, Amount: sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseCoinUnit, 1))}
	_, err = layerxgovtypes.NewLayerXProposal("Foreign message", "Must refuse", foreign)
	c.assert(err != nil, "six-module proposal constructor refuses a foreign message")
	packed, err := cdctypes.NewAnyWithValue(foreign)
	if !c.assert(err == nil, "foreign-message refusal uses a real packed bank message: %v", err) {
		return
	}
	refused := &layerxgovtypes.LayerXProposal{Title: "Foreign message", Description: "Must refuse", Messages: []*cdctypes.Any{packed}}
	c.assert(refused.ValidateBasic() != nil, "six-module proposal validation refuses a foreign message")
	c.assert(handler(ctx, refused) != nil, "six-module governance handler refuses a foreign message")
	c.assert(reflect.DeepEqual(after, readOperatingState(a, ctx)), "foreign-message refusal preserves every replayed effect")
}
