package keeper_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"os/exec"
	"path/filepath"
	"sort"

	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	tmtypes "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
	"github.com/sidiora-labs/paxeer-network/layerxproof/codec"
	"github.com/sidiora-labs/paxeer-network/layerxproof/testvectors"
	custodykeeper "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/keeper"
	custodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	"github.com/sidiora-labs/paxeer-network/modules/layerxexchange/keeper"
	"github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	"github.com/sidiora-labs/paxeer-network/sdk/store"
	"github.com/sidiora-labs/paxeer-network/sdk/store/cachemulti"
	"github.com/sidiora-labs/paxeer-network/sdk/store/dbadapter"
	storetypes "github.com/sidiora-labs/paxeer-network/sdk/store/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	authtypes "github.com/sidiora-labs/paxeer-network/sdk/x/auth/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	testkeeper "github.com/sidiora-labs/paxeer-network/testutil/keeper"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

const (
	tokenDenom     = "ufoo"
	marginBatch    = uint64(40)
	perpsBatch     = uint64(41)
	promotedBatch  = uint64(42)
	mutatedBatch   = uint64(43)
	unknownBatch   = uint64(99)
	eventNamespace = "paxprotocol.paxchain.layerxexchange."
)

var (
	tokenPointer = common.HexToAddress("0x00000000000000000000000000000000000f00f0")
	genesisTime  = time.Unix(1_800_000_000, 0).UTC()
	account      = [32]byte{0xbe, 0xef, 0x01}
	marketID     = [32]byte{0x6d, 0x01}
	orderID      = [32]byte{0x0d, 0x02}
	positionID   = [32]byte{0x0e, 0x03}
)

// withStore adds the exchange store to ctx's multistore. The application
// mounts it only once the v6.5 store upgrade wires the module; until then the
// keeper runs over the app's real stores plus its own.
func withStore(t *testing.T, ctx sdk.Context, key storetypes.StoreKey) sdk.Context {
	t.Helper()
	parent := ctx.MultiStore()
	stores := map[storetypes.StoreKey]storetypes.CacheWrapper{}
	keys := map[string]storetypes.StoreKey{}
	for _, existing := range parent.StoreKeys() {
		if store, ok := mounted(parent, existing); ok {
			stores[existing] = store
			keys[existing.Name()] = existing
		}
	}
	stores[key] = dbadapter.Store{DB: tmdb.NewMemDB()}
	keys[key.Name()] = key
	return ctx.WithMultiStore(cachemulti.NewStore(tmdb.NewMemDB(), stores, keys, nil, nil, nil, 0))
}

func mounted(parent storetypes.MultiStore, key storetypes.StoreKey) (store storetypes.KVStore, ok bool) {
	defer func() {
		if recover() != nil {
			store, ok = nil, false
		}
	}()
	return parent.GetKVStore(key), true
}

// encodeWitness writes a version-2 native state witness for a non-account key
// whose module subtree holds one leaf.
func encodeWitness(moduleID uint16, key, value []byte, moduleSiblings [][32]byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, 2)
	out = binary.BigEndian.AppendUint16(out, moduleID)
	out = binary.BigEndian.AppendUint32(out, uint32(len(key))) //nolint:gosec
	out = append(out, key...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(value))) //nolint:gosec
	out = append(out, value...)
	out = binary.BigEndian.AppendUint32(out, 0)
	out = binary.BigEndian.AppendUint32(out, 1)
	out = append(out, 0)
	out = binary.BigEndian.AppendUint32(out, 9)
	out = append(out, byte(len(moduleSiblings)))
	for _, sibling := range moduleSiblings {
		out = append(out, sibling[:]...)
	}
	return out
}

// perpsWitness proves key/value in the perps module and returns its root.
func perpsWitness(t *testing.T, key, value []byte) ([]byte, [32]byte) {
	t.Helper()
	witness := encodeWitness(types.PerpsModuleID, key, value, [][32]byte{{0x71}, {0x72}, {0x73}, {0x74}})
	decoded, err := codec.DecodeStateWitness(witness)
	require.NoError(t, err)
	root, err := decoded.Root()
	require.NoError(t, err)
	return witness, root
}

type fixture struct {
	t         *testing.T
	ctx       sdk.Context
	keeper    *keeper.Keeper
	custody   *custodykeeper.Keeper
	vectors   testvectors.Fixture
	caller    common.Address
	callerAcc sdk.AccAddress
	native    [32]byte
	token     [32]byte
}

func vectorBytes(t *testing.T, v testvectors.Vector, key string) []byte {
	t.Helper()
	out, err := v.Bytes(key)
	require.NoError(t, err)
	return out
}

func vectorArray(t *testing.T, v testvectors.Vector, key string) [32]byte {
	t.Helper()
	out, err := v.Array32(key)
	require.NoError(t, err)
	return out
}

func vector(t *testing.T, fixture testvectors.Fixture, section, name string) testvectors.Vector {
	t.Helper()
	for _, candidate := range fixture[section] {
		if candidate.Name == name {
			return candidate
		}
	}
	t.Fatalf("no %s vector %s", section, name)
	return testvectors.Vector{}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := testkeeper.EVMTestApp
	base, _ := app.NewContext(false, tmtypes.Header{}).WithBlockHeight(11).WithBlockTime(genesisTime).CacheContext()
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := withStore(t, base, key)
	vectors, err := testvectors.Load()
	require.NoError(t, err)
	f := &fixture{t: t, ctx: ctx, custody: app.LayerXCustodyKeeper, vectors: vectors,
		native: vectorArray(t, vectors["withdrawal"][0], "asset"), token: vectorArray(t, vectors["exit"][0], "asset")}
	f.keeper = keeper.NewKeeper(app.AppCodec(), key, app.LayerXCustodyKeeper, &app.EvmKeeper)
	// Finalized roots come from custody's authority-registered checkpoints;
	// the app wires the anchor module's reader in its place.
	f.custody.SetAnchorReader(nil)
	f.callerAcc, f.caller = testkeeper.MockAddressPair()
	app.EvmKeeper.SetAddressMapping(ctx, f.callerAcc, f.caller)
	for _, denom := range []string{sdk.MustGetBaseDenom(), tokenDenom} {
		coins := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(50_000_000)))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", f.callerAcc, coins))
	}
	f.custody.InitGenesis(ctx, *custodytypes.DefaultGenesis())
	require.NoError(t, f.custody.SetAsset(ctx, custodytypes.AssetMapping{AssetId: custodytypes.Hash32(f.native),
		Denom: sdk.MustGetBaseDenom(), Enabled: true}))
	require.NoError(t, f.custody.SetAsset(ctx, custodytypes.AssetMapping{AssetId: custodytypes.Hash32(f.token),
		Denom: tokenDenom, Pointer: tokenPointer.Hex(), Enabled: true}))
	f.keeper.InitGenesis(ctx, *types.DefaultGenesis())
	require.NoError(t, f.keeper.SetMarket(ctx, types.Market{MarketId: custodytypes.Hash32(marketID),
		MarginAssetId: custodytypes.Hash32(f.native), Enabled: true}))
	return f
}

func (f *fixture) balance(address sdk.AccAddress, denom string) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(f.ctx, address, denom).Amount
}

func (f *fixture) typedEvents(ctx sdk.Context, name string) int {
	count := 0
	for _, event := range ctx.EventManager().Events() {
		if event.Type == name {
			count++
		}
	}
	return count
}

func (f *fixture) fresh() sdk.Context { return f.ctx.WithEventManager(sdk.NewEventManager()) }

func TestDepositMarginMovesFundsOnlyIntoCustody(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	before := f.balance(f.callerAcc, sdk.MustGetBaseDenom())
	intent, err := f.keeper.DepositMargin(ctx, f.caller, f.callerAcc, f.native, account, sdk.NewInt(2_500))
	require.NoError(t, err)

	require.Equal(t, sdk.NewInt(2_500), f.balance(f.custody.ModuleAddress(), sdk.MustGetBaseDenom()))
	require.Equal(t, before.SubRaw(2_500), f.balance(f.callerAcc, sdk.MustGetBaseDenom()))
	chainID := testkeeper.EVMTestApp.EvmKeeper.ChainID(ctx)
	require.Equal(t, custodytypes.Hash32(types.IntentID(chainID, f.caller, types.IntentKind_INTENT_KIND_DEPOSIT, 1)), intent.IntentId)
	depositID := custodytypes.DepositID(chainID, f.caller, f.native, account, big.NewInt(2_500), 1)
	require.Equal(t, custodytypes.Hash32(depositID), intent.DepositId)
	deposit, found := f.custody.GetDeposit(ctx, depositID)
	require.True(t, found)
	require.Equal(t, custodytypes.Hash32(account), deposit.Beneficiary)
	require.Equal(t, 1, f.typedEvents(ctx, eventNamespace+"EventMarginDeposited"))
	require.Equal(t, 1, f.typedEvents(ctx, "paxprotocol.paxchain.layerxcustody.EventCustodyDeposit"))

	stored, found := f.keeper.GetIntent(ctx, types.IntentID(chainID, f.caller, types.IntentKind_INTENT_KIND_DEPOSIT, 1))
	require.True(t, found)
	require.Equal(t, intent, stored)
	require.Equal(t, types.IntentStatus_INTENT_STATUS_PENDING, stored.Status)
	require.Equal(t, "2500", stored.Amount)
	require.Equal(t, int64(11), stored.Height)
	require.Equal(t, uint64(1), f.keeper.GetOwnerNonce(ctx, f.caller))
	message, broken := custodykeeper.SolvencyInvariant(f.custody)(ctx)
	require.False(t, broken, message)

	// Refusals leave no intent, no nonce and no custody movement behind.
	_, err = f.keeper.DepositMargin(ctx, f.caller, f.callerAcc, f.token, account, sdk.NewInt(10))
	require.ErrorIs(t, err, types.ErrNotMarginAsset)
	_, err = f.keeper.DepositMargin(ctx, f.caller, f.callerAcc, f.native, [32]byte{}, sdk.NewInt(10))
	require.Error(t, err)
	_, err = f.keeper.DepositMargin(ctx, f.caller, f.callerAcc, f.native, account, sdk.NewInt(60_000_000))
	require.Error(t, err)
	require.Equal(t, uint64(1), f.keeper.GetOwnerNonce(ctx, f.caller))
	require.Equal(t, uint64(1), f.keeper.GetIntentCount(ctx))
	require.Equal(t, sdk.NewInt(2_500), f.balance(f.custody.ModuleAddress(), sdk.MustGetBaseDenom()))
	require.Equal(t, 1, f.typedEvents(ctx, eventNamespace+"EventMarginDeposited"))
}

func TestIntentsAreRecordedWithoutMovingFunds(t *testing.T) {
	f := newFixture(t)
	f.enableNativeTIF()
	ctx := f.fresh()
	chainID := testkeeper.EVMTestApp.EvmKeeper.ChainID(ctx)
	custodyBefore := f.balance(f.custody.ModuleAddress(), sdk.MustGetBaseDenom())
	callerBefore := f.balance(f.callerAcc, sdk.MustGetBaseDenom())

	withdraw, err := f.keeper.WithdrawMargin(ctx, f.caller, account, f.native, sdk.NewInt(700))
	require.NoError(t, err)
	place, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideSell, sdk.NewInt(31_000), sdk.NewInt(4), types.TimeInForcePostOnly)
	require.NoError(t, err)
	cancel, err := f.keeper.CancelOrder(ctx, f.caller, orderID)
	require.NoError(t, err)
	settle, err := f.keeper.RequestSettlement(ctx, f.caller, positionID)
	require.NoError(t, err)

	for nonce, intent := range []types.Intent{withdraw, place, cancel, settle} {
		require.Equal(t, uint64(nonce+1), intent.Nonce)
		require.Equal(t, custodytypes.Hash32(types.IntentID(chainID, f.caller, intent.Kind, intent.Nonce)), intent.IntentId)
		require.Equal(t, custodytypes.Address(f.caller), intent.Owner)
	}
	require.Equal(t, types.IntentKind_INTENT_KIND_WITHDRAW, withdraw.Kind)
	require.Equal(t, sdk.MustGetBaseDenom(), withdraw.Denom)
	require.Equal(t, custodytypes.Hash32(marketID), place.MarketId)
	require.Equal(t, uint32(types.SideSell), place.Side)
	require.Equal(t, "31000", place.Price)
	require.Equal(t, "4", place.Quantity)
	require.Equal(t, uint32(types.TimeInForcePostOnly), place.TimeInForce)
	require.Equal(t, custodytypes.Hash32(orderID), cancel.OrderId)
	require.Equal(t, custodytypes.Hash32(positionID), settle.PositionId)
	for _, name := range []string{"EventMarginWithdrawalRequested", "EventOrderPlaced", "EventOrderCancelRequested",
		"EventSettlementRequested"} {
		require.Equal(t, 1, f.typedEvents(ctx, eventNamespace+name), name)
	}
	require.Equal(t, custodyBefore, f.balance(f.custody.ModuleAddress(), sdk.MustGetBaseDenom()))
	require.Equal(t, callerBefore, f.balance(f.callerAcc, sdk.MustGetBaseDenom()))

	tooLarge := sdk.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
	refusals := map[string]error{}
	_, refusals["unlisted market"] = f.keeper.PlaceOrder(ctx, f.caller, [32]byte{9}, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 0)
	_, refusals["side"] = f.keeper.PlaceOrder(ctx, f.caller, marketID, 3, sdk.NewInt(1), sdk.NewInt(1), 0)
	_, refusals["time in force"] = f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 4)
	_, refusals["zero price"] = f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.ZeroInt(), sdk.NewInt(1), 0)
	_, refusals["u128 quantity"] = f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), tooLarge, 0)
	_, refusals["zero order"] = f.keeper.CancelOrder(ctx, f.caller, [32]byte{})
	_, refusals["zero position"] = f.keeper.RequestSettlement(ctx, f.caller, [32]byte{})
	_, refusals["non-margin asset"] = f.keeper.WithdrawMargin(ctx, f.caller, account, f.token, sdk.NewInt(1))
	_, refusals["zero amount"] = f.keeper.WithdrawMargin(ctx, f.caller, account, f.native, sdk.ZeroInt())
	_, refusals["zero account"] = f.keeper.WithdrawMargin(ctx, f.caller, [32]byte{}, f.native, sdk.NewInt(1))
	_, refusals["zero owner"] = f.keeper.CancelOrder(ctx, common.Address{}, orderID)
	for name, err := range refusals {
		require.Error(t, err, name)
	}
	require.NoError(t, f.keeper.SetMarket(ctx, types.Market{MarketId: custodytypes.Hash32(marketID),
		MarginAssetId: custodytypes.Hash32(f.native), Enabled: false}))
	_, err = f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 0)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
	require.Equal(t, uint64(4), f.keeper.GetOwnerNonce(ctx, f.caller))
	require.Equal(t, uint64(4), f.keeper.GetIntentCount(ctx))
}

func TestOrderTimeInForceIsRecordedDistinctlyBeforeAnyNonce(t *testing.T) {
	f := newFixture(t)
	f.enableNativeTIF()
	ctx := f.fresh()
	chainID := testkeeper.EVMTestApp.EvmKeeper.ChainID(ctx)
	_, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 4)
	require.ErrorIs(t, err, types.ErrInvalidIntent)
	require.Equal(t, uint64(0), f.keeper.GetOwnerNonce(ctx, f.caller))
	require.Equal(t, uint64(0), f.keeper.GetIntentCount(ctx))
	require.Equal(t, 0, f.typedEvents(ctx, eventNamespace+"EventOrderPlaced"))
	tifs := []uint8{types.TimeInForceGoodTillCancelled, types.TimeInForceImmediateOrCancel,
		types.TimeInForceFillOrKill, types.TimeInForcePostOnly}
	for i, tif := range tifs {
		placed, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(30_000), sdk.NewInt(2), tif)
		require.NoError(t, err)
		require.Equal(t, uint64(i+1), placed.Nonce)
		stored, found := f.keeper.GetIntent(ctx, types.IntentID(chainID, f.caller, types.IntentKind_INTENT_KIND_PLACE, placed.Nonce))
		require.True(t, found)
		require.Equal(t, uint32(tif), stored.TimeInForce)
	}
	_, err = f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 0xff)
	require.ErrorIs(t, err, types.ErrInvalidIntent)
	require.Equal(t, uint64(4), f.keeper.GetOwnerNonce(ctx, f.caller))
	require.Equal(t, uint64(4), f.keeper.GetIntentCount(ctx))
	require.Equal(t, 4, f.typedEvents(ctx, eventNamespace+"EventOrderPlaced"))
}

func TestViewsProveFinalizedState(t *testing.T) {
	f := newFixture(t)
	exit := f.vectors["exit"][0]
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, marginBatch, vectorArray(t, exit, "state_root"), [32]byte{1}))
	marketValue := append(append([]byte(nil), marketID[:]...), 0x01, 0x02)
	marketWitness, perpsRoot := perpsWitness(t, types.MarketStateKey(marketID), marketValue)
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, perpsBatch, perpsRoot, [32]byte{1}))
	promoted := vector(t, f.vectors, "state", "valid-module-promoted")
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, promotedBatch, vectorArray(t, promoted, "state_root"), [32]byte{1}))
	mutated := vector(t, f.vectors, "state", "mutated-value")
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, mutatedBatch, vectorArray(t, mutated, "state_root"), [32]byte{1}))

	margin, err := f.keeper.ProveMargin(f.ctx, vectorArray(t, exit, "account"), f.token, marginBatch, vectorBytes(t, exit, "witness"))
	require.NoError(t, err)
	require.Equal(t, vectorArray(t, exit, "state_root"), margin.StateRoot)
	balance := margin.Account.Balance.Bytes()
	require.Equal(t, exit.Fields["balance"], new(big.Int).SetBytes(balance[:]).String())
	_, err = f.keeper.ProveMargin(f.ctx, vectorArray(t, exit, "account"), f.native, marginBatch, vectorBytes(t, exit, "witness"))
	require.ErrorIs(t, err, types.ErrInvalidProof, "another asset")
	_, err = f.keeper.ProveMargin(f.ctx, vectorArray(t, exit, "account"), f.token, unknownBatch, vectorBytes(t, exit, "witness"))
	require.ErrorIs(t, err, types.ErrNotFinalized)

	market, err := f.keeper.ProveMarket(f.ctx, marketID, perpsBatch, marketWitness)
	require.NoError(t, err)
	require.Equal(t, keeper.ProvenState{BatchNumber: perpsBatch, StateRoot: perpsRoot, ModuleID: types.PerpsModuleID,
		Key: types.MarketStateKey(marketID), Value: marketValue}, market)
	orderWitness, orderRoot := perpsWitness(t, types.OrderStateKey(marketID, orderID), []byte{0x0d})
	positionWitness, positionRoot := perpsWitness(t, types.PositionStateKey(marketID, positionID), []byte{0x0e})
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, perpsBatch+10, orderRoot, [32]byte{1}))
	require.NoError(t, f.custody.RegisterCheckpoint(f.ctx, perpsBatch+11, positionRoot, [32]byte{1}))
	order, err := f.keeper.ProveOrder(f.ctx, marketID, orderID, perpsBatch+10, orderWitness)
	require.NoError(t, err)
	require.Equal(t, []byte{0x0d}, order.Value)
	position, err := f.keeper.ProvePosition(f.ctx, marketID, positionID, perpsBatch+11, positionWitness)
	require.NoError(t, err)
	require.Equal(t, []byte{0x0e}, position.Value)

	_, err = f.keeper.ProveMarket(f.ctx, [32]byte{0x55}, perpsBatch, marketWitness)
	require.ErrorIs(t, err, types.ErrStateMismatch, "another market's key")
	_, err = f.keeper.ProveOrder(f.ctx, marketID, orderID, perpsBatch, marketWitness)
	require.ErrorIs(t, err, types.ErrStateMismatch, "a market entry is not an order")
	_, err = f.keeper.ProveMarket(f.ctx, marketID, marginBatch, marketWitness)
	require.ErrorIs(t, err, types.ErrInvalidProof, "another batch's root")
	_, err = f.keeper.ProveMarket(f.ctx, marketID, unknownBatch, marketWitness)
	require.ErrorIs(t, err, types.ErrNotFinalized)
	_, err = f.keeper.ProveMarket(f.ctx, marketID, promotedBatch, vectorBytes(t, promoted, "witness"))
	require.ErrorIs(t, err, types.ErrStateMismatch, "a module 3 entry")
	_, err = f.keeper.ProveMarket(f.ctx, marketID, mutatedBatch, vectorBytes(t, mutated, "witness"))
	require.ErrorIs(t, err, types.ErrInvalidProof, "a mutated value")
	_, err = f.keeper.ProveMarket(f.ctx, marketID, perpsBatch, make([]byte, types.MaxWitnessBytes+1))
	require.ErrorIs(t, err, types.ErrEvidenceTooLong)
}

func TestGenesisAndAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	_, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(5), sdk.NewInt(6), 0)
	require.NoError(t, err)
	_, err = f.keeper.CancelOrder(ctx, f.caller, orderID)
	require.NoError(t, err)
	exported := f.keeper.ExportGenesis(ctx)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Intents, 2)
	require.Equal(t, []types.OwnerNonce{{Owner: custodytypes.Address(f.caller), Nonce: 2}}, exported.OwnerNonces)

	app := testkeeper.EVMTestApp
	key := sdk.NewKVStoreKey(types.StoreKey)
	other := keeper.NewKeeper(app.AppCodec(), key, app.LayerXCustodyKeeper, &app.EvmKeeper)
	otherCtx := withStore(t, ctx, key)
	other.InitGenesis(otherCtx, *exported)
	require.Equal(t, exported, other.ExportGenesis(otherCtx))
	next, err := other.RequestSettlement(otherCtx, f.caller, positionID)
	require.NoError(t, err)
	require.Equal(t, uint64(3), next.Nonce)

	duplicate := *exported
	duplicate.Intents = append(duplicate.Intents, exported.Intents[0])
	duplicate.IntentCount = 3
	require.Error(t, duplicate.Validate())

	server := keeper.NewMsgServerImpl(f.keeper)
	listing := types.Market{MarketId: custodytypes.Hash32([32]byte{0x77}), MarginAssetId: custodytypes.Hash32(f.token), Enabled: true}
	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: f.callerAcc.String(), Market: listing})
	require.Error(t, err, "not the authority")
	governance := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governance, Market: listing})
	require.NoError(t, err)
	require.True(t, f.keeper.GetParams(ctx).MarginAsset(custodytypes.Hash32(f.token)))
	_, err = server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: governance,
		Params: types.Params{Markets: []types.Market{listing, listing}}})
	require.Error(t, err, "a market listed twice")
}

func signedNativeGenesis(t *testing.T, tif, oracle bool) []byte {
	t.Helper()
	blob := func(out, value []byte) []byte {
		out = binary.BigEndian.AppendUint32(out, uint32(len(value)))
		return append(out, value...)
	}
	key := func(name string) []byte { out := make([]byte, 32); copy(out, name); return out }
	content := binary.BigEndian.AppendUint16(nil, 3)
	content = binary.BigEndian.AppendUint32(content, 77)
	content = binary.BigEndian.AppendUint64(content, uint64(genesisTime.UnixMilli()))
	parameters := []string{"module-enable:perps", "parameter-version"}
	if oracle {
		parameters = append(parameters, "perps-oracle-transport")
	}
	if tif {
		parameters = append(parameters, "perps-order-tif")
	}
	sort.Strings(parameters)
	content = binary.BigEndian.AppendUint32(content, uint32(len(parameters)))
	for _, name := range parameters {
		content = binary.BigEndian.AppendUint16(content, types.GovernanceModuleID)
		content = blob(content, key(name))
		value := make([]byte, 32)
		value[31] = 1
		content = blob(content, value)
	}
	content = binary.BigEndian.AppendUint32(content, 1)
	content = blob(content, bytes.Repeat([]byte{1}, 32))
	content = blob(content, append([]byte{2}, bytes.Repeat([]byte{3}, 32)...))
	content = append(content, make([]byte, 16)...)
	type accountEntry struct {
		id   [32]byte
		kind uint16
	}
	accounts := []accountEntry{}
	for kind, name := range map[uint16]string{9: "system:insurance", 10: "system:fees", 11: "system:paxeer-reserve", 12: "system:paxeer-withdrawals"} {
		id, err := codec.DeriveAccountID([]byte(name))
		require.NoError(t, err)
		accounts = append(accounts, accountEntry{id, kind})
	}
	sort.Slice(accounts, func(i, j int) bool { return bytes.Compare(accounts[i].id[:], accounts[j].id[:]) < 0 })
	content = binary.BigEndian.AppendUint32(content, uint32(len(accounts)))
	for _, account := range accounts {
		content = blob(content, account.id[:])
		content = blob(content, bytes.Repeat([]byte{4}, 32))
		content = append(content, make([]byte, 17)...)
		content = binary.BigEndian.AppendUint16(content, account.kind)
		content = blob(content, make([]byte, 32))
	}
	content = binary.BigEndian.AppendUint32(content, 0)
	encoded := binary.BigEndian.AppendUint16(nil, 3)
	encoded = binary.BigEndian.AppendUint16(encoded, 0x4701)
	encoded = append(encoded, content...)
	stateRoot := sha256.Sum256([]byte("genesis capability codec fixture"))
	encoded = blob(encoded, stateRoot[:])
	receiptInput := binary.BigEndian.AppendUint32(nil, 77)
	receiptInput = append(receiptInput, stateRoot[:]...)
	receiptRoot := sha256.Sum256(append([]byte("LXP/v1/genesis-receipt-root\x00"), receiptInput...))
	encoded = blob(encoded, receiptRoot[:])
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	encoded = blob(encoded, private.Public().(ed25519.PublicKey))
	return blob(encoded, ed25519.Sign(private, encoded))
}

func (f *fixture) nativeMarket(encoded []byte, batch uint64) types.Market {
	f.t.Helper()
	commitment, _, err := types.NativeGenesisCapability(encoded)
	require.NoError(f.t, err)
	witness := encodeWitness(types.GovernanceModuleID, types.GenesisManifestStateKey(), commitment[:],
		[][32]byte{{0x81}, {0x82}, {0x83}, {0x84}})
	decoded, err := codec.DecodeStateWitness(witness)
	require.NoError(f.t, err)
	root, err := decoded.Root()
	require.NoError(f.t, err)
	require.NoError(f.t, f.custody.RegisterCheckpoint(f.ctx, batch, root, [32]byte{1}))
	return types.Market{MarketId: custodytypes.Hash32(marketID), MarginAssetId: custodytypes.Hash32(f.native),
		Enabled: true, NativeGenesis: encoded, CapabilityBatch: batch, CapabilityWitness: witness}
}

func (f *fixture) enableNativeTIF() {
	f.t.Helper()
	genesis := signedNativeGenesis(f.t, true, true)
	path := os.Getenv("PAXEER_X_TIF_NATIVE_GENESIS_FILE")
	if os.Getenv("PAXEER_X_TIF_INGRESS_FILE") != "" {
		require.NotEmpty(f.t, path, "cross-language producer requires C-produced native genesis")
	}
	if path != "" {
		var err error
		genesis, err = os.ReadFile(path)
		require.NoError(f.t, err)
		_, enabled, err := types.NativeGenesisCapability(genesis)
		require.NoError(f.t, err)
		require.True(f.t, enabled)
	}
	require.NoError(f.t, f.keeper.SetMarket(f.ctx, f.nativeMarket(genesis, 60)))
}

func TestNativeTimeInForceCapabilityRefusesBeforeNonce(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	for _, tif := range []uint8{1, 2, 3} {
		_, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), tif)
		require.ErrorIs(t, err, types.ErrInvalidIntent)
	}
	require.Zero(t, f.keeper.GetOwnerNonce(ctx, f.caller))
	require.Zero(t, f.keeper.GetIntentCount(ctx))
	require.Empty(t, ctx.EventManager().Events())
	valid := f.nativeMarket(signedNativeGenesis(t, true, true), 60)
	legacy := f.nativeMarket(signedNativeGenesis(t, false, false), 61)
	oracleOnly := f.nativeMarket(signedNativeGenesis(t, false, true), 62)
	otherSigner := append([]byte(nil), valid.NativeGenesis...)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x43}, ed25519.SeedSize))
	copy(otherSigner[len(otherSigner)-100:len(otherSigner)-68], private.Public().(ed25519.PublicKey))
	copy(otherSigner[len(otherSigner)-64:], ed25519.Sign(private, otherSigner[:len(otherSigner)-68]))
	wrongKey := valid
	commitment, _, err := types.NativeGenesisCapability(valid.NativeGenesis)
	require.NoError(t, err)
	wrongKey.CapabilityWitness = encodeWitness(types.GovernanceModuleID, []byte("perps-order-tif"), commitment[:],
		[][32]byte{{0x81}, {0x82}, {0x83}, {0x84}})
	decodedWitness, err := codec.DecodeStateWitness(wrongKey.CapabilityWitness)
	require.NoError(t, err)
	wrongRoot, err := decodedWitness.Root()
	require.NoError(t, err)
	wrongKey.CapabilityBatch = 63
	require.NoError(t, f.custody.RegisterCheckpoint(ctx, 63, wrongRoot, [32]byte{1}))
	for name, listing := range map[string]types.Market{
		"legacy": legacy, "oracle only": oracleOnly, "wrong state key": wrongKey,
		"substituted signer":    func() types.Market { x := valid; x.NativeGenesis = otherSigner; return x }(),
		"unfinalized":           func() types.Market { x := valid; x.CapabilityBatch = unknownBatch; return x }(),
		"other batch root":      func() types.Market { x := valid; x.CapabilityBatch = 61; return x }(),
		"other signed manifest": func() types.Market { x := valid; x.NativeGenesis = legacy.NativeGenesis; return x }(),
		"tampered witness": func() types.Market {
			x := valid
			x.CapabilityWitness = append([]byte(nil), valid.CapabilityWitness...)
			x.CapabilityWitness[len(x.CapabilityWitness)-1] ^= 1
			return x
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, f.keeper.SetMarket(ctx, listing))
			for _, tif := range []uint8{1, 2, 3} {
				_, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), tif)
				require.Error(t, err)
			}
			require.Zero(t, f.keeper.GetOwnerNonce(ctx, f.caller))
			require.Zero(t, f.keeper.GetIntentCount(ctx))
			require.Empty(t, ctx.EventManager().Events())
		})
	}
	for name, encoded := range map[string][]byte{
		"truncated":          valid.NativeGenesis[:len(valid.NativeGenesis)-1],
		"trailing":           append(append([]byte(nil), valid.NativeGenesis...), 0),
		"signature":          func() []byte { x := append([]byte(nil), valid.NativeGenesis...); x[len(x)-1] ^= 1; return x }(),
		"fixed length":       func() []byte { x := append([]byte(nil), valid.NativeGenesis...); x[27] = 31; return x }(),
		"tif without oracle": signedNativeGenesis(t, true, false),
	} {
		t.Run(name, func(t *testing.T) {
			listing := valid
			listing.NativeGenesis = encoded
			require.Error(t, f.keeper.SetMarket(ctx, listing))
			require.Zero(t, f.keeper.GetOwnerNonce(ctx, f.caller))
			require.Zero(t, f.keeper.GetIntentCount(ctx))
		})
	}
	for _, listing := range []types.Market{legacy, oracleOnly} {
		require.NoError(t, f.keeper.SetMarket(ctx, listing))
		_, err := f.keeper.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(1), sdk.NewInt(1), 0)
		require.NoError(t, err)
	}
}

func TestNativeCapabilityTruncationAndEvidenceBounds(t *testing.T) {
	f := newFixture(t)
	valid := f.nativeMarket(signedNativeGenesis(t, true, true), 60)
	for n := 0; n < len(valid.NativeGenesis); n++ {
		_, _, err := types.NativeGenesisCapability(valid.NativeGenesis[:n])
		require.Error(t, err, "prefix length %d", n)
	}
	for _, listing := range []types.Market{
		func() types.Market { x := valid; x.NativeGenesis = nil; return x }(),
		func() types.Market { x := valid; x.CapabilityWitness = nil; return x }(),
		func() types.Market { x := valid; x.CapabilityWitness = make([]byte, types.MaxWitnessBytes+1); return x }(),
		func() types.Market {
			x := valid
			x.NativeGenesis = make([]byte, types.MaxNativeGenesisBytes+1)
			return x
		}(),
	} {
		require.Error(t, f.keeper.SetMarket(f.ctx, listing))
	}
	require.Zero(t, f.keeper.GetOwnerNonce(f.ctx, f.caller))
	require.Zero(t, f.keeper.GetIntentCount(f.ctx))
}

func TestNativeCapabilityMarketWireRoundTrip(t *testing.T) {
	f := newFixture(t)
	listing := f.nativeMarket(signedNativeGenesis(t, true, true), 60)
	wire, err := listing.Marshal()
	require.NoError(t, err)
	require.Len(t, wire, listing.Size())
	var decoded types.Market
	require.NoError(t, decoded.Unmarshal(wire))
	require.True(t, listing.Equal(decoded))
	params := types.Params{Markets: []types.Market{listing}}
	encoded, err := params.Marshal()
	require.NoError(t, err)
	var restored types.Params
	require.NoError(t, restored.Unmarshal(encoded))
	require.Equal(t, params, restored)
	legacy := types.Market{MarketId: listing.MarketId, MarginAssetId: listing.MarginAssetId, Enabled: true}
	legacyWire, err := legacy.Marshal()
	require.NoError(t, err)
	var legacyRestored types.Market
	require.NoError(t, legacyRestored.Unmarshal(legacyWire))
	require.Equal(t, legacy, legacyRestored)
}

func TestOrderIntentSurvivesCommittedKeeperReopen(t *testing.T) {
	phase := os.Getenv("PAXEER_X_TIF_KEEPER_PHASE")
	if phase == "" {
		executable, err := os.Executable()
		require.NoError(t, err)
		dir := t.TempDir()
		for _, childPhase := range []string{"writer", "recover"} {
			deadline, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			command := exec.CommandContext(deadline, executable, "-test.run=^TestOrderIntentSurvivesCommittedKeeperReopen$", "-test.count=1")
			command.Env = append(os.Environ(), "PAXEER_X_TIF_KEEPER_PHASE="+childPhase, "PAXEER_X_TIF_KEEPER_DB="+dir)
			output, err := command.CombinedOutput()
			cancel()
			require.NoError(t, err, "%s subprocess: %s", childPhase, output)
		}
		return
	}
	require.Contains(t, []string{"writer", "recover"}, phase)
	f := newFixture(t)
	f.enableNativeTIF()
	app := testkeeper.EVMTestApp
	dir := os.Getenv("PAXEER_X_TIF_KEEPER_DB")
	require.NotEmpty(t, dir)
	type checkpoint struct {
		Intents   []types.Intent
		Canonical [][]byte
		Events    sdk.Events
		Commit    storetypes.CommitID
		Params    []byte
		Owner     string
	}
	var expected checkpoint
	evidencePath := filepath.Join(dir, "expected.json")
	open := func() (storetypes.CommitMultiStore, sdk.Context, *keeper.Keeper) {
		database, err := tmdb.NewGoLevelDB("exchange", dir)
		require.NoError(t, err)
		key := sdk.NewKVStoreKey(types.StoreKey)
		committed := store.NewCommitMultiStore(database)
		committed.MountStoreWithDB(key, sdk.StoreTypeIAVL, database)
		require.NoError(t, committed.LoadLatestVersion())
		stores := map[storetypes.StoreKey]storetypes.CacheWrapper{}
		keys := map[string]storetypes.StoreKey{}
		for _, existing := range f.ctx.MultiStore().StoreKeys() {
			if existing.Name() == types.StoreKey {
				continue
			}
			if mountedStore, ok := mounted(f.ctx.MultiStore(), existing); ok {
				stores[existing], keys[existing.Name()] = mountedStore, existing
			}
		}
		stores[key], keys[key.Name()] = committed.GetKVStore(key), key
		ctx := f.ctx.WithMultiStore(cachemulti.NewStore(tmdb.NewMemDB(), stores, keys, nil, nil, nil, 0)).WithEventManager(sdk.NewEventManager())
		return committed, ctx, keeper.NewKeeper(app.AppCodec(), key, f.custody, &app.EvmKeeper)
	}
	if phase == "writer" {
		committed, ctx, first := open()
		first.InitGenesis(ctx, *types.DefaultGenesis())
		require.NoError(t, first.SetParams(ctx, f.keeper.GetParams(f.ctx)))
		for _, tif := range []uint8{0, 1, 2, 3} {
			intent, err := first.PlaceOrder(ctx, f.caller, marketID, types.SideBuy, sdk.NewInt(17), sdk.NewInt(2), tif)
			require.NoError(t, err)
			expected.Intents = append(expected.Intents, intent)
			encoded, err := intent.Marshal()
			require.NoError(t, err)
			expected.Canonical = append(expected.Canonical, encoded)
		}
		require.Equal(t, 4, f.typedEvents(ctx, eventNamespace+"EventOrderPlaced"))
		expected.Events = append(sdk.Events(nil), ctx.EventManager().Events()...)
		params := first.GetParams(ctx)
		var err error
		expected.Params, err = params.Marshal()
		require.NoError(t, err)
		expected.Owner = f.caller.Hex()
		ctx.MultiStore().(storetypes.CacheMultiStore).Write()
		expected.Commit = committed.Commit(true)
		require.NoError(t, committed.Close())
		encoded, err := json.Marshal(expected)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(evidencePath, encoded, 0600))
		return
	}
	encodedExpected, err := os.ReadFile(evidencePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encodedExpected, &expected))
	require.Len(t, expected.Intents, 4)
	require.Len(t, expected.Canonical, 4)
	require.Len(t, expected.Events, 4)
	f.caller = common.HexToAddress(expected.Owner)
	require.NotEqual(t, common.Address{}, f.caller)
	intents, canonical, events := expected.Intents, expected.Canonical, expected.Events
	reopened, restoredCtx, restored := open()
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, expected.Commit, reopened.LastCommitID())
	params := restored.GetParams(restoredCtx)
	paramsBytes, err := params.Marshal()
	require.NoError(t, err)
	require.Equal(t, expected.Params, paramsBytes)
	require.Empty(t, restoredCtx.EventManager().Events())
	rows := []map[string]interface{}{}
	projectedLogs := []byte{}
	abiBytes, err := os.ReadFile("../../../precompiles/layerxexchange/abi.json")
	require.NoError(t, err)
	contractABI, err := abi.JSON(bytes.NewReader(abiBytes))
	require.NoError(t, err)
	placedABI := contractABI.Events["OrderPlaced"]
	for i, intent := range intents {
		id, err := custodytypes.ParseNonZeroHash32(intent.IntentId)
		require.NoError(t, err)
		stored, found := restored.GetIntent(restoredCtx, id)
		require.True(t, found)
		encoded, err := stored.Marshal()
		require.NoError(t, err)
		require.Equal(t, canonical[i], encoded)
		expectedEvent, err := sdk.TypedEventToEvent(&types.EventOrderPlaced{IntentId: stored.IntentId,
			Owner: stored.Owner, MarketId: stored.MarketId, Side: stored.Side, Price: stored.Price,
			Quantity: stored.Quantity, TimeInForce: stored.TimeInForce, Nonce: stored.Nonce})
		require.NoError(t, err)
		require.Equal(t, expectedEvent, events[i])
		price, ok := new(big.Int).SetString(stored.Price, 10)
		require.True(t, ok)
		quantity, ok := new(big.Int).SetString(stored.Quantity, 10)
		require.True(t, ok)
		data, err := placedABI.Inputs.NonIndexed().Pack(uint8(stored.Side), price, quantity, uint8(stored.TimeInForce), stored.Nonce)
		require.NoError(t, err)
		require.Len(t, data, 160)
		market, err := custodytypes.ParseNonZeroHash32(stored.MarketId)
		require.NoError(t, err)
		projectedLogs = append(projectedLogs, placedABI.ID.Bytes()...)
		projectedLogs = append(projectedLogs, id[:]...)
		projectedLogs = append(projectedLogs, market[:]...)
		projectedLogs = append(projectedLogs, common.LeftPadBytes(common.HexToAddress(stored.Owner).Bytes(), 32)...)
		projectedLogs = append(projectedLogs, data...)
		rows = append(rows, map[string]interface{}{"intent_id": stored.IntentId, "owner": stored.Owner,
			"market_id": stored.MarketId, "side": stored.Side, "price": stored.Price,
			"quantity": stored.Quantity, "time_in_force": stored.TimeInForce, "nonce": stored.Nonce,
			"canonical_intent": hex.EncodeToString(encoded), "sdk_event": events[i]})
	}
	require.Len(t, projectedLogs, 1152)
	if output := os.Getenv("PAXEER_X_TIF_INGRESS_FILE"); output != "" {
		encoded, err := json.Marshal(rows)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, encoded, 0600))
		require.NoError(t, os.WriteFile(output+".events", projectedLogs, 0600))
	}
	require.Equal(t, uint64(4), restored.GetOwnerNonce(restoredCtx, f.caller))
	require.Equal(t, uint64(4), restored.GetIntentCount(restoredCtx))
	next, err := restored.PlaceOrder(restoredCtx, f.caller, marketID, types.SideBuy, sdk.NewInt(17), sdk.NewInt(2), 3)
	require.NoError(t, err)
	require.Equal(t, uint64(5), next.Nonce)
	for _, intent := range intents {
		require.NotEqual(t, intent.IntentId, next.IntentId)
	}
	require.Equal(t, 1, f.typedEvents(restoredCtx, eventNamespace+"EventOrderPlaced"))
}
