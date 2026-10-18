package keeper_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/artifacts/native"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// initTokenCreation is a token whose name() and symbol() are "USD Ledger"
// and "USDL" and whose decimals() is the immutable constructor argument.
const initTokenCreation = "0x60a0604052348015600e575f5ffd5b5060405161019f38038061019f833981016040819052602b916035565b60ff16608052605a565b5f602082840312156044575f5ffd5b815160ff811681146053575f5ffd5b9392505050565b60805161012e6100715f395f607b015261012e5ff3fe6080604052348015600e575f5ffd5b5060043610603a575f3560e01c806306fdde0314603e578063313ce56714607457806395d89b411460a4575b5f5ffd5b60408051808201909152600a8152692aa9a2102632b233b2b960b11b60208201525b604051606b919060c3565b60405180910390f35b60405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152602001606b565b6040805180820190915260048152631554d11360e21b60208201526060565b602081525f82518060208401528060208501604085015e5f604082850101526040601f19601f8301168401019150509291505056fea2646970667358221220068b7ac52323f118a0c55bc91191aa6297234996f3db0e3c6095d37fc40f9e8164736f6c634300081e0033"

const initHeight = int64(12)

var (
	initUSDL     = common.HexToAddress(types.USDLAddress)
	initPECOR    = common.HexToAddress(types.PECORAddress)
	initOrders   = common.HexToAddress(types.PECOROrdersAddress)
	initStops    = common.HexToAddress(types.PECORStopOrdersAddress)
	initQuoter   = common.HexToAddress(types.PECORQuoterAddress)
	initVault    = common.HexToAddress(types.PECORVaultAddress)
	initDeployer = common.HexToAddress("0x00000000000000000000000000000000000de901")
)

// initEnv is the spot keeper on the bank and EVM keepers of a fresh test
// application, with its own spot and pricefeed stores.
type initEnv struct {
	t   *testing.T
	a   *app.App
	ctx sdk.Context
	key sdk.StoreKey
	k   *keeper.Keeper
}

func newInitEnv(t *testing.T) *initEnv {
	t.Helper()
	a := app.Setup(t, false, true, false)
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(initHeight).WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	a.EvmKeeper.BeginBlock(ctx)
	pricefeed, ctx := pricefeedtestutil.NewKeeper(a, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, a.BankKeeper, &a.EvmKeeper, pricefeed)
	return &initEnv{t: t, a: a, ctx: launchpadtest.WithStore(ctx, key), key: key, k: k}
}

func (e *initEnv) init() error { return keeper.InitV6120(e.ctx, *e.k) }

// installUSDL places a token with the given decimals() at the USDL address.
func (e *initEnv) installUSDL(decimals uint8) {
	e.t.Helper()
	uint8Type, err := abi.NewType("uint8", "", nil)
	require.NoError(e.t, err)
	args, err := abi.Arguments{{Type: uint8Type}}.Pack(decimals)
	require.NoError(e.t, err)
	nonce := e.a.EvmKeeper.GetNonce(e.ctx, initDeployer)
	_, err = e.a.EvmKeeper.CallEVM(e.ctx, initDeployer, nil, nil, append(common.FromHex(initTokenCreation), args...))
	require.NoError(e.t, err)
	code := e.a.EvmKeeper.GetCode(e.ctx, crypto.CreateAddress(initDeployer, nonce))
	require.NotEmpty(e.t, code)
	e.a.EvmKeeper.SetCode(e.ctx, initUSDL, code)
}

func (e *initEnv) setMetadata(denom, display string, exponent uint32) {
	e.a.BankKeeper.SetDenomMetaData(e.ctx, banktypes.Metadata{
		DenomUnits: []*banktypes.DenomUnit{{Denom: denom, Exponent: 0}, {Denom: display, Exponent: exponent}},
		Base:       denom,
		Display:    display,
		Name:       display,
		Symbol:     display,
	})
}

// createPointer deploys the native pointer of denom as the pointers hook of
// the plan does.
func (e *initEnv) createPointer(denom, symbol string) common.Address {
	e.t.Helper()
	err := e.a.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		_, upsertErr := e.a.EvmKeeper.UpsertERCNativePointer(e.ctx, evm, denom,
			utils.ERCMetadata{Name: symbol, Symbol: symbol, Decimals: types.OpeningTokenDecimals})
		return upsertErr
	}, func(string, string) {})
	require.NoError(e.t, err)
	pointer, _, exists := e.a.EvmKeeper.GetERC20NativePointer(e.ctx, denom)
	require.True(e.t, exists)
	return pointer
}

// ready installs USDL, the uhpx and SID metadata and both native pointers,
// the state the plan reaches before the spot hook.
func (e *initEnv) ready() (common.Address, common.Address) {
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	return e.createPointer(evmkeeper.BaseDenom, "PAX"), e.createPointer(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol)
}

func shareDenom(token common.Address) string {
	return "spot/share/" + strings.ToLower(token.Hex())
}

func (e *initEnv) sharePointer(token common.Address) (common.Address, bool) {
	pointer, _, exists := e.a.EvmKeeper.GetERC20NativePointer(e.ctx, shareDenom(token))
	return pointer, exists
}

func (e *initEnv) activationReceipt() (*evmtypes.Receipt, error) {
	kind := evmkeeper.SyntheticReceiptActivation
	return e.a.EvmKeeper.GetTransientReceipt(e.ctx, evmkeeper.SyntheticReceiptHash(kind, initHeight),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind)))
}

// requireUntouched checks that no spot state, share token or activation log
// was written.
func (e *initEnv) requireUntouched(tokens ...common.Address) {
	e.t.Helper()
	iter := e.ctx.KVStore(e.key).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	require.False(e.t, iter.Valid(), "spot store holds key %x", func() []byte {
		if iter.Valid() {
			return iter.Key()
		}
		return nil
	}())
	for _, token := range append([]common.Address{initUSDL}, tokens...) {
		_, exists := e.sharePointer(token)
		require.False(e.t, exists, token.Hex())
		_, found := e.a.BankKeeper.GetDenomMetaData(e.ctx, shareDenom(token))
		require.False(e.t, found, token.Hex())
	}
	_, err := e.activationReceipt()
	require.Error(e.t, err)
}

func (e *initEnv) staticCall(to common.Address, method string) interface{} {
	e.t.Helper()
	input, err := native.GetParsedABI().Pack(method)
	require.NoError(e.t, err)
	output, err := e.a.EvmKeeper.StaticCallEVM(e.ctx, e.a.AccountKeeper.GetModuleAddress(evmtypes.ModuleName), &to, input)
	require.NoError(e.t, err, method)
	values, err := native.GetParsedABI().Unpack(method, output)
	require.NoError(e.t, err, method)
	require.Len(e.t, values, 1, method)
	return values[0]
}

func TestInitV6120Order(t *testing.T) {
	// Before the pointers hook has run, the spot hook refuses and writes nothing.
	e := newInitEnv(t)
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	require.EqualError(t, e.init(), "spot: uhpx has no native pointer")
	e.requireUntouched()

	// With only the uhpx pointer the SID pointer is still missing.
	pax := e.createPointer(evmkeeper.BaseDenom, "PAX")
	require.EqualError(t, e.init(), "spot: "+layerxbridgetypes.SidioraDenom()+" has no native pointer")
	e.requireUntouched(pax)

	// Once the pointers hook has created both pointers the spot hook runs.
	sid := e.createPointer(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol)
	require.NoError(t, e.init())
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	require.Equal(t, types.OpeningTokens(pax, sid), params.Tokens)
}

func TestInitV6120RegistersOpeningPairs(t *testing.T) {
	e := newInitEnv(t)
	pax, sid := e.ready()
	require.NoError(t, e.init())

	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	want := types.DefaultParams()
	want.Tokens = []types.TokenParams{
		{Address: initUSDL, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: pax, Kind: types.TokenKindNative, Decimals: 6, MarketID: 11, MaxUtilizationBps: 9000},
		{Address: sid, Kind: types.TokenKindNative, Decimals: 6, MarketID: 12, MaxUtilizationBps: 9000},
	}
	require.Equal(t, want, params)
	require.NoError(t, params.Validate())

	// USDL keeps no pointer and no bank denom of its own.
	_, _, exists := e.a.EvmKeeper.GetERC20NativePointer(e.ctx, initUSDL.Hex())
	require.False(t, exists)

	// Every token has its share denom with metadata and an ERC20 native
	// pointer of the token decimals and an empty supply.
	shares := map[common.Address]struct{}{}
	for _, token := range []common.Address{initUSDL, pax, sid} {
		share, exists := e.sharePointer(token)
		require.True(t, exists, token.Hex())
		require.NotEqual(t, common.Address{}, share)
		shares[share] = struct{}{}
		_, found := e.a.BankKeeper.GetDenomMetaData(e.ctx, shareDenom(token))
		require.True(t, found, token.Hex())
		denom, _, ok := e.a.EvmKeeper.GetNativePointee(e.ctx, share.Hex())
		require.True(t, ok)
		require.Equal(t, shareDenom(token), denom)
		require.Equal(t, uint8(6), e.staticCall(share, "decimals"))
		require.Equal(t, 0, big.NewInt(0).Cmp(e.staticCall(share, "totalSupply").(*big.Int)))
		require.True(t, e.a.BankKeeper.GetSupply(e.ctx, shareDenom(token)).Amount.IsZero())
	}
	require.Len(t, shares, 3)
}

type initLog struct {
	Address common.Address
	Topics  []common.Hash
	Data    []byte
}

func initWord(v int64) []byte { return common.LeftPadBytes(big.NewInt(v).Bytes(), 32) }

func initTopic(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

func TestInitV6120Logs(t *testing.T) {
	e := newInitEnv(t)
	pax, sid := e.ready()
	e.a.EvmKeeper.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	require.NoError(t, e.init())

	ownership := crypto.Keccak256Hash([]byte("OwnershipTransferred(address,address)"))
	operator := crypto.Keccak256Hash([]byte("OperatorUpdated(address,bool)"))
	registered := crypto.Keccak256Hash([]byte("TokenRegistered(address,uint8,bool)"))
	shareCreated := crypto.Keccak256Hash([]byte("ShareTokenCreated(address,address)"))
	capUpdated := crypto.Keccak256Hash([]byte("UtilizationCapUpdated(address,uint256)"))
	tracker := crypto.Keccak256Hash([]byte("TransactionTrackerUpdated(address)"))
	require.Equal(t, common.HexToHash("0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0"), ownership)
	require.Equal(t, common.HexToHash("0x966c160e1c4dbc7df8d69af4ace01e9297c3cf016397b7914971f2fbfa32672d"), operator)
	require.Equal(t, common.HexToHash("0x3247537f6efe5e5d3f25242f42522e1bfbabeea906fc7005a128b11bcdb3f99b"), registered)
	require.Equal(t, common.HexToHash("0x6f64ce3fea7909e3e9ebc85ea8450670af3fe5d23143325c4f1ebd38c5343256"), shareCreated)
	require.Equal(t, common.HexToHash("0xbcfb6f6e36e3fb105ffb3042d755b572cec796e9c4c634f0bb7ef3c8967331ac"), capUpdated)
	require.Equal(t, common.HexToHash("0x3dd2b7e16ddea78bec31b5b95991ba3d03fda27bc5c8a8441775b76c8f88033e"), tracker)

	governance := initTopic(types.GovernanceEVMAddress())
	want := []initLog{
		{initPECOR, []common.Hash{ownership, {}, governance}, []byte{}},
		{initOrders, []common.Hash{ownership, {}, governance}, []byte{}},
		{initStops, []common.Hash{ownership, {}, governance}, []byte{}},
		{initVault, []common.Hash{ownership, {}, governance}, []byte{}},
		{initVault, []common.Hash{operator, initTopic(initPECOR)}, initWord(1)},
		{initVault, []common.Hash{operator, initTopic(initOrders)}, initWord(1)},
		{initVault, []common.Hash{operator, initTopic(initStops)}, initWord(1)},
	}
	for _, token := range []struct {
		address common.Address
		stable  int64
	}{{initUSDL, 1}, {pax, 0}, {sid, 0}} {
		share, exists := e.sharePointer(token.address)
		require.True(t, exists)
		want = append(want,
			initLog{initVault, []common.Hash{registered, initTopic(token.address)}, append(initWord(6), initWord(token.stable)...)},
			initLog{initVault, []common.Hash{capUpdated, initTopic(token.address)}, initWord(9000)},
			initLog{initVault, []common.Hash{shareCreated, initTopic(token.address), initTopic(share)}, []byte{}},
		)
	}
	want = append(want, initLog{initVault, []common.Hash{tracker, {}}, []byte{}})

	collect := func(r *evmtypes.Receipt) []initLog {
		got := make([]initLog, 0, len(r.Logs))
		for i, l := range evmkeeper.GetLogsForTx(r, 0) {
			require.True(t, r.Logs[i].Synthetic)
			data := l.Data
			if data == nil {
				data = []byte{}
			}
			got = append(got, initLog{l.Address, l.Topics, data})
		}
		return got
	}
	r, err := e.activationReceipt()
	require.NoError(t, err)
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptActivation, initHeight)
	require.Equal(t, hash.Hex(), r.TxHashHex)
	require.Equal(t, uint64(initHeight), r.BlockNumber)
	require.Equal(t, want, collect(r))
	for _, l := range r.Logs {
		require.NotEqual(t, initQuoter, common.HexToAddress(l.Address))
		for _, token := range []common.Address{initUSDL, pax, sid} {
			share, _ := e.sharePointer(token)
			require.NotEqual(t, share, common.HexToAddress(l.Address), "no log at a share pointer")
		}
	}

	// The flush moves the receipt to the highest real index + 3.
	require.NoError(t, e.a.EvmKeeper.FlushTransientReceipts(e.ctx))
	var stored *evmtypes.Receipt
	require.Eventually(t, func() bool {
		got, err := e.a.EvmKeeper.GetReceipt(e.ctx, hash)
		if err != nil {
			return false
		}
		stored = got
		return true
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, uint32(5), stored.TransactionIndex)
	require.Equal(t, want, collect(stored))
}

func TestInitV6120RefusesDenomWithoutPointer(t *testing.T) {
	sidDenom := layerxbridgetypes.SidioraDenom()

	// The SID denom has metadata but no pointer.
	e := newInitEnv(t)
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(sidDenom, layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	pax := e.createPointer(evmkeeper.BaseDenom, "PAX")
	require.EqualError(t, e.init(), "spot: "+sidDenom+" has no native pointer")
	e.requireUntouched(pax)

	// The SID denom has a pointer but no metadata.
	e = newInitEnv(t)
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	pax = e.createPointer(evmkeeper.BaseDenom, "PAX")
	sid := e.createPointer(sidDenom, layerxbridgetypes.SidioraSymbol)
	require.EqualError(t, e.init(), "spot: "+sidDenom+" metadata is absent")
	e.requireUntouched(pax, sid)

	// USDL without code is refused as well.
	e = newInitEnv(t)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(sidDenom, layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	pax = e.createPointer(evmkeeper.BaseDenom, "PAX")
	sid = e.createPointer(sidDenom, layerxbridgetypes.SidioraSymbol)
	require.EqualError(t, e.init(), "spot: USDL "+initUSDL.Hex()+" has no code")
	e.requireUntouched(pax, sid)
}

func TestInitV6120RefusesWrongDecimals(t *testing.T) {
	sidDenom := layerxbridgetypes.SidioraDenom()

	// A native denom whose metadata exponent is not 6.
	e := newInitEnv(t)
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(sidDenom, layerxbridgetypes.SidioraSymbol, 8)
	pax := e.createPointer(evmkeeper.BaseDenom, "PAX")
	sid := e.createPointer(sidDenom, layerxbridgetypes.SidioraSymbol)
	require.EqualError(t, e.init(), "spot: "+sidDenom+" decimals 8, want 6")
	e.requireUntouched(pax, sid)

	e = newInitEnv(t)
	e.installUSDL(types.OpeningTokenDecimals)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", 18)
	e.setMetadata(sidDenom, layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	pax = e.createPointer(evmkeeper.BaseDenom, "PAX")
	sid = e.createPointer(sidDenom, layerxbridgetypes.SidioraSymbol)
	require.EqualError(t, e.init(), "spot: uhpx decimals 18, want 6")
	e.requireUntouched(pax, sid)

	// USDL whose decimals() is not 6.
	e = newInitEnv(t)
	e.installUSDL(18)
	e.setMetadata(evmkeeper.BaseDenom, "PAX", types.OpeningTokenDecimals)
	e.setMetadata(sidDenom, layerxbridgetypes.SidioraSymbol, types.OpeningTokenDecimals)
	pax = e.createPointer(evmkeeper.BaseDenom, "PAX")
	sid = e.createPointer(sidDenom, layerxbridgetypes.SidioraSymbol)
	require.EqualError(t, e.init(), "spot: USDL decimals 18, want 6")
	e.requireUntouched(pax, sid)
}

func TestInitV6120Once(t *testing.T) {
	e := newInitEnv(t)
	pax, sid := e.ready()
	require.NoError(t, e.init())
	params := e.ctx.KVStore(e.key).Get(types.ParamsKey)
	require.NotNil(t, params)
	before, err := e.activationReceipt()
	require.NoError(t, err)
	shares := map[common.Address]common.Address{}
	for _, token := range []common.Address{initUSDL, pax, sid} {
		share, exists := e.sharePointer(token)
		require.True(t, exists)
		shares[token] = share
	}

	err = e.init()
	require.ErrorIs(t, err, keeper.ErrAlreadyInitialised)
	require.EqualError(t, err, "spot: already initialised")
	require.Equal(t, params, e.ctx.KVStore(e.key).Get(types.ParamsKey))
	after, err := e.activationReceipt()
	require.NoError(t, err)
	require.Equal(t, before, after)
	for token, share := range shares {
		again, exists := e.sharePointer(token)
		require.True(t, exists)
		require.Equal(t, share, again)
	}
}
