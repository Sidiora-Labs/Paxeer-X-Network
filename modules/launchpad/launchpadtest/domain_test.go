package launchpadtest_test

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeperv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	typesv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	domainCallGas    uint64 = 30_000_000
	domainUSDLSupply int64  = 30_000_000_000_000
	domainChainID           = "hyperpax_125-1"
	domainERC20Role         = "SidioraERC20"
	domainABIDir            = "../../../precompiles/launchpad/abi"
	domainMarketPath        = "../keeper/market.go"
	domainGoldenPath        = "../keeper/testdata/v6120/golden.json"
	domainExportPath        = "../../../node/testdata/v6120-halt-export.json"
)

var domainGenesisTime = time.Unix(1_800_000_000, 0).UTC()

// domainGolden is the part of golden.json the domain test reads: the halt
// height and time, the pools of each generation and the getters recorded at
// every contract.
type domainGolden struct {
	Height      uint64 `json:"height"`
	Timestamp   uint64 `json:"timestamp"`
	Generations []struct {
		Generation int              `json:"generation"`
		Pools      []common.Address `json:"pools"`
	} `json:"generations"`
	Contracts []struct {
		Address common.Address `json:"address"`
		Calls   []struct {
			Signature string          `json:"signature"`
			From      *common.Address `json:"from,omitempty"`
			Input     hexutil.Bytes   `json:"input"`
			Reverted  bool            `json:"reverted"`
			Output    hexutil.Bytes   `json:"output"`
		} `json:"calls"`
	} `json:"contracts"`
}

// output is the recorded answer of signature at address, sent with no
// arguments and no sender.
func (g *domainGolden) output(t *testing.T, address common.Address, signature string) []byte {
	t.Helper()
	for _, c := range g.Contracts {
		if c.Address != address {
			continue
		}
		for _, call := range c.Calls {
			if call.From == nil && call.Signature == signature && len(call.Input) == 4 {
				require.False(t, call.Reverted, "golden %s at %s reverted", signature, address.Hex())
				return call.Output
			}
		}
	}
	t.Fatalf("golden.json records no %s at %s", signature, address.Hex())
	return nil
}

// migrationEVM is the EVM keeper with the retirement runtime of the
// launchpad precompile, as the v6.12.0 upgrade handler passes it.
type migrationEVM struct{ *evmkeeper.Keeper }

func (m migrationEVM) InstallRetired(ctx sdk.Context, retired, replacement common.Address) error {
	return launchpad.InstallRetired(ctx, m.Keeper, retired, replacement)
}

// domain is a launchpad keeper built by launchpadtest.NewKeeper over the
// test application and an EVM whose 0x1017 is the precompile built over that
// keeper with NewPrecompileWithKeeper. base is the context the state DB was
// opened on.
type domain struct {
	t       *testing.T
	keeper  *launchpadkeeper.Keeper
	base    sdk.Context
	stateDB *state.DBImpl
	evm     *vm.EVM
	abis    map[string]abi.ABI
	usdl    common.Address
}

func newDomain(t *testing.T, k *launchpadkeeper.Keeper, ctx sdk.Context) *domain {
	t.Helper()
	app := testkeeper.EVMTestApp
	p, err := launchpad.NewPrecompileWithKeeper(k, app.GetPrecompileKeepers().EVMK())
	require.NoError(t, err)
	d := &domain{t: t, keeper: k, base: ctx, stateDB: state.NewDBImpl(ctx, &app.EvmKeeper, false),
		abis: map[string]abi.ABI{}}
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, contract := range app.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = contract
	}
	precompiles[common.HexToAddress(types.LaunchpadAddress)] = p
	d.evm = vm.NewEVM(*blockCtx, d.stateDB, evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx)),
		vm.Config{}, precompiles)
	files, err := filepath.Glob(filepath.Join(domainABIDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		parsed, err := abi.JSON(bytes.NewReader(raw))
		require.NoError(t, err, file)
		d.abis[strings.TrimSuffix(filepath.Base(file), ".json")] = parsed
	}
	return d
}

// freshDomain is a domain on a fresh launchpad store at domainGenesisTime
// with the default genesis, USDL deployed in the test EVM with its supply
// held by the returned account and named by Params.QuoteToken, and the
// generation 2 role surfaces installed.
func freshDomain(t *testing.T) (*domain, common.Address) {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(domainGenesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	d := newDomain(t, k, ctx)
	holder := d.account()
	d.usdl = d.deployERC20("USD Ledger", "USDL", big.NewInt(domainUSDLSupply), holder)
	params := k.GetParams(d.ctx())
	params.QuoteToken = d.usdl.Hex()
	require.NoError(t, k.UpdateParams(d.ctx(), k.Authority(), params))
	require.NoError(t, launchpad.InstallRoleSurfaces(d.ctx(), &app.EvmKeeper, k))
	require.Equal(t, big.NewInt(domainUSDLSupply), d.balanceOf(d.usdl, holder))
	return d, holder
}

// migratedDomain is a domain on the test application branch seeded with the
// EVM state of the halt-height export at H+1, with the v6.10 launchpad store
// and MigrateV6120 run on it.
func migratedDomain(t *testing.T, g *domainGolden) *domain {
	t.Helper()
	app := testkeeper.EVMTestApp
	raw, err := os.ReadFile(domainExportPath)
	require.NoError(t, err, "halt-height export %s", domainExportPath)
	var export struct {
		InitialHeight json.Number                `json:"initial_height"`
		AppState      map[string]json.RawMessage `json:"app_state"`
	}
	require.NoError(t, json.Unmarshal(raw, &export))
	initial, err := export.InitialHeight.Int64()
	require.NoError(t, err)
	require.Equal(t, int64(g.Height)+1, initial, "halt-height export is not the export of golden height %d", g.Height)
	var genesis evmtypes.GenesisState
	require.NoError(t, app.AppCodec().UnmarshalAsJSON(export.AppState["evm"], &genesis))

	ctx, _ := app.GetContextForDeliverTx([]byte{}).CacheContext()
	ctx = ctx.WithChainID(domainChainID).WithBlockHeight(initial).
		WithBlockTime(time.Unix(int64(g.Timestamp)+1, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	for _, code := range genesis.Codes {
		app.EvmKeeper.SetCode(ctx, common.HexToAddress(code.Address), code.Code)
	}
	for _, s := range genesis.States {
		app.EvmKeeper.SetState(ctx, common.HexToAddress(s.Address), common.BytesToHash(s.Key),
			common.BytesToHash(s.Value))
	}
	for _, nonce := range genesis.Nonces {
		app.EvmKeeper.SetNonce(ctx, common.HexToAddress(nonce.Address), nonce.Nonce)
	}

	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	// The overlay of launchpadtest serves the launchpad store by key name, so
	// the v6.10 keeper writes into the store k reads.
	key := sdk.NewKVStoreKey(types.StoreKey)
	legacy := keeperv610.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper),
		&app.EvmKeeper)
	params := typesv610.DefaultParams()
	params.QuoteDenom = "uhpx"
	require.NoError(t, legacy.UpdateParams(ctx, legacy.Authority(), params))
	pending, err := sdk.ZeroInt().Marshal()
	require.NoError(t, err)
	ctx.KVStore(key).Set(types.ProtocolFeesPrefix, pending)
	require.NoError(t, launchpadkeeper.MigrateV6120(ctx, *k, migrationEVM{&app.EvmKeeper}))
	d := newDomain(t, k, ctx)
	d.usdl = common.HexToAddress(k.GetParams(d.ctx()).QuoteToken)
	return d
}

func (d *domain) ctx() sdk.Context { return d.stateDB.Ctx() }

// account is a fresh EVM address associated with its Pax account.
func (d *domain) account() common.Address {
	acc, address := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(d.ctx(), acc, address)
	return address
}

// deployERC20 deploys SidioraERC20(name, symbol, supply, holder), the
// creation code the launchpad keeper deploys pool tokens with, from a fresh
// deployer.
func (d *domain) deployERC20(name, symbol string, supply *big.Int, holder common.Address) common.Address {
	d.t.Helper()
	source, err := os.ReadFile(domainMarketPath)
	require.NoError(d.t, err)
	match := regexp.MustCompile(`var sidioraERC20Creation = common\.FromHex\("(0x[0-9a-f]+)"\)`).FindSubmatch(source)
	require.Len(d.t, match, 2)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(d.t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(d.t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(d.t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack(name, symbol, supply, holder)
	require.NoError(d.t, err)
	ret, address, _, err := d.evm.Create(d.account(), append(common.FromHex(string(match[1])), args...),
		domainCallGas, uint256.NewInt(0))
	require.NoError(d.t, err, "%x", ret)
	require.NotEmpty(d.t, d.stateDB.GetCode(address))
	return address
}

func (d *domain) method(role, name string) abi.Method {
	d.t.Helper()
	m, ok := d.abis[role].Methods[name]
	require.True(d.t, ok, "%s.%s", role, name)
	return m
}

func (d *domain) pack(role, name string, args ...interface{}) []byte {
	d.t.Helper()
	m := d.method(role, name)
	packed, err := m.Inputs.Pack(args...)
	require.NoError(d.t, err, "%s.%s", role, name)
	return append(append([]byte(nil), m.ID...), packed...)
}

// do calls role method name at to from from, requires success and returns
// the unpacked outputs.
func (d *domain) do(from, to common.Address, role, name string, args ...interface{}) []interface{} {
	d.t.Helper()
	ret, _, err := d.evm.Call(from, to, d.pack(role, name, args...), domainCallGas, uint256.NewInt(0))
	require.NoError(d.t, err, "%s.%s at %s: %x", role, name, to.Hex(), ret)
	out, err := d.method(role, name).Outputs.Unpack(ret)
	require.NoError(d.t, err, "%s.%s", role, name)
	return out
}

// raw is the static call of input at to.
func (d *domain) raw(to common.Address, input []byte) []byte {
	d.t.Helper()
	ret, _, err := d.evm.StaticCall(common.Address{}, to, input, domainCallGas)
	require.NoError(d.t, err, "%x at %s: %x", input, to.Hex(), ret)
	return ret
}

func (d *domain) view(to common.Address, role, name string, args ...interface{}) []interface{} {
	d.t.Helper()
	out, err := d.method(role, name).Outputs.Unpack(d.raw(to, d.pack(role, name, args...)))
	require.NoError(d.t, err, "%s.%s", role, name)
	return out
}

func (d *domain) balanceOf(token, holder common.Address) *big.Int {
	d.t.Helper()
	return d.view(token, domainERC20Role, "balanceOf", holder)[0].(*big.Int)
}

// approve grants amount of token to 0x1017.
func (d *domain) approve(token, owner common.Address, amount *big.Int) {
	d.t.Helper()
	require.Equal(d.t, true, d.do(owner, token, domainERC20Role, "approve",
		common.HexToAddress(types.LaunchpadAddress), amount)[0])
}

func (d *domain) ledger(ledger launchpadkeeper.QuoteLedger) *big.Int {
	d.t.Helper()
	amount, err := d.keeper.GetQuoteLedger(d.ctx(), ledger)
	require.NoError(d.t, err)
	return amount.BigInt()
}

func (d *domain) accumulated(pool common.Address) *big.Int {
	d.t.Helper()
	return d.view(launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator], types.RoleFeeAccumulator,
		"getAccumulatedFees", pool)[0].(*big.Int)
}

func (d *domain) quote(pool common.Address, in *big.Int, buy bool) (amountOut, fee *big.Int) {
	d.t.Helper()
	v := reflect.ValueOf(d.view(launchpadkeeper.Generation2Surfaces[types.RoleQuoter], types.RoleQuoter,
		"quoteExactInput", pool, in, buy)[0])
	return v.FieldByName("AmountOut").Interface().(*big.Int), v.FieldByName("FeeAmount").Interface().(*big.Int)
}

// market is the keeper record of the market of pool.
func (d *domain) market(pool common.Address) types.Market {
	d.t.Helper()
	m, found := d.keeper.GetMarket(d.ctx(), types.ModuleName+"/"+pool.Hex())
	require.True(d.t, found, pool.Hex())
	return m
}

// requireInvariants writes every snapshot layer of the state DB through to
// the context it was opened on and requires the launchpad custody and
// solvency invariants there, off the layered cache stack of the EVM calls.
func (d *domain) requireInvariants() {
	d.t.Helper()
	_, err := d.stateDB.Finalize()
	require.NoError(d.t, err)
	for _, invariant := range []sdk.Invariant{launchpadkeeper.CustodyInvariant(d.keeper),
		launchpadkeeper.SolvencyInvariant(d.keeper)} {
		message, broken := invariant(d.base)
		require.False(d.t, broken, message)
	}
}

func TestLaunchpadDomainWiring(t *testing.T) {
	t.Run("trade", func(t *testing.T) {
		d, creator := freshDomain(t)
		router := launchpadkeeper.Generation2Surfaces[types.RoleRouter]
		feeAccumulator := launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator]
		custody := d.keeper.ModuleEVMAddress(d.ctx())
		deadline := big.NewInt(domainGenesisTime.Add(10_000 * time.Hour).Unix())

		// createMarket pulls the creation fee by allowance to 0x1017.
		fee := d.keeper.GetParams(d.ctx()).CreationFee.BigInt()
		require.Equal(t, big.NewInt(1_000_000), fee)
		d.approve(d.usdl, creator, fee)
		out := d.do(creator, router, types.RoleRouter, "createMarket", "Domain Token", "DOMN",
			uint8(types.FeeStrategyClaim), common.Address{})
		token, pool, nftID := out[0].(common.Address), out[1].(common.Address), out[2].(*big.Int)
		require.Equal(t, new(big.Int).Sub(big.NewInt(domainUSDLSupply), fee), d.balanceOf(d.usdl, creator))
		require.Equal(t, fee, d.balanceOf(d.usdl, custody))
		require.Equal(t, fee, d.ledger(launchpadkeeper.TreasuryHeldLedger()))
		market := d.market(pool)
		require.Equal(t, token.Hex(), market.TokenAddress)
		require.Equal(t, types.Generation2, market.Generation)
		require.Equal(t, nftID.Uint64(), market.FeeRightID)
		require.Equal(t, uint64(1), d.keeper.GetMarketCount(d.ctx()))
		require.Equal(t, creator, d.view(launchpadkeeper.Generation2Surfaces[types.RoleFeeRights],
			types.RoleFeeRights, "ownerOf", nftID)[0])
		supply := big.NewInt(1_000_000_000_000_000)
		require.Equal(t, supply, d.balanceOf(token, pool))

		// buy pulls USDL by allowance and splits the fee into the protocol
		// and pool cuts.
		in := big.NewInt(100_000_000)
		bought, buyFee := big.NewInt(9_606_813_905_120), big.NewInt(3_000_000)
		quoted, quotedFee := d.quote(pool, in, true)
		require.Equal(t, bought, quoted)
		require.Equal(t, buyFee, quotedFee)
		d.approve(d.usdl, creator, in)
		require.Equal(t, bought, d.do(creator, router, types.RoleRouter, "buy", pool, in, bought, deadline)[0])
		require.Equal(t, bought, d.balanceOf(token, creator))
		require.Equal(t, new(big.Int).Sub(supply, bought), d.balanceOf(token, pool))
		require.Equal(t, new(big.Int).Add(fee, in), d.balanceOf(d.usdl, custody))
		require.Equal(t, big.NewInt(97_000_000), d.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
		protocolCut := big.NewInt(300_000)
		poolCut := new(big.Int).Sub(buyFee, protocolCut)
		require.Equal(t, poolCut, d.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)))
		require.Equal(t, new(big.Int).Add(fee, protocolCut), d.ledger(launchpadkeeper.TreasuryHeldLedger()))
		require.Equal(t, poolCut, d.accumulated(pool))

		// sell pulls the pool token by allowance and pays USDL from custody.
		sellIn := big.NewInt(4_803_406_952_560)
		sold := big.NewInt(47_278_912)
		quoted, _ = d.quote(pool, sellIn, false)
		require.Equal(t, sold, quoted)
		usdlBefore := d.balanceOf(d.usdl, creator)
		d.approve(token, creator, sellIn)
		require.Equal(t, sold, d.do(creator, router, types.RoleRouter, "sell", pool, sellIn, sold, deadline)[0])
		require.Equal(t, new(big.Int).Add(usdlBefore, sold), d.balanceOf(d.usdl, creator))
		require.Equal(t, new(big.Int).Sub(bought, sellIn), d.balanceOf(token, creator))
		require.Equal(t, big.NewInt(49_721_088), d.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
		require.Equal(t, poolCut, d.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)), "a sell moves no USDL fee")

		// claimFees pays the fee-right holder the pool cut from custody.
		usdlBefore = d.balanceOf(d.usdl, creator)
		custodyBefore := d.balanceOf(d.usdl, custody)
		require.Equal(t, poolCut, d.do(creator, launchpadkeeper.Generation2Surfaces[types.RoleFeesRouter],
			types.RoleFeesRouter, "claimFees", nftID)[0])
		require.Equal(t, new(big.Int).Add(usdlBefore, poolCut), d.balanceOf(d.usdl, creator))
		require.Equal(t, new(big.Int).Sub(custodyBefore, poolCut), d.balanceOf(d.usdl, custody))
		require.Zero(t, d.accumulated(pool).Sign())
		require.Zero(t, d.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)).Sign())

		d.requireInvariants()
	})

	t.Run("migrated", func(t *testing.T) {
		raw, err := os.ReadFile(domainGoldenPath)
		require.NoError(t, err, "golden fixture %s", domainGoldenPath)
		var g domainGolden
		require.NoError(t, json.Unmarshal(raw, &g), domainGoldenPath)
		require.NotZero(t, g.Height)
		d := migratedDomain(t, &g)

		var pools []common.Address
		var migrated common.Address
		for _, gen := range g.Generations {
			pools = append(pools, gen.Pools...)
			if gen.Generation == 2 {
				require.NotEmpty(t, gen.Pools)
				migrated = gen.Pools[0]
			}
		}
		require.NotEqual(t, common.Address{}, migrated)
		require.Equal(t, uint64(len(pools)), d.keeper.GetMarketCount(d.ctx()))
		require.Equal(t, types.Generation2, d.market(migrated).Generation)
		require.Equal(t, common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479"), d.usdl)

		// The migrated pool reads through its surface as the contract read
		// at H.
		for _, signature := range []string{"tokenAddress()", "usdlAddress()", "getReserves()", "realUsdlBalance()",
			"tokenReserve()", "virtualUsdlReserve()"} {
			got := d.raw(migrated, d.pack(types.RolePool, signature[:strings.Index(signature, "(")]))
			require.Equal(t, hexutil.Bytes(g.output(t, migrated, signature)).String(), hexutil.Bytes(got).String(),
				"%s at migrated pool %s", signature, migrated.Hex())
		}

		d.requireInvariants()
	})
}
