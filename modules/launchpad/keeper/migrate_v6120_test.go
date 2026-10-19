package keeper_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmgenesis "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeperv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	typesv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	v6120GoldenFile     = "golden.json"
	v6120HaltExportPath = "../../../node/testdata/v6120-halt-export.json"
	v6120SurfaceRuntime = "0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3"
)

var (
	v6120Generation2Factory        = common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830")
	v6120Generation2FeeAccumulator = common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")
	v6120Generation2ProtocolConfig = common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6")
	v6120Generation2EventEmitter   = common.HexToAddress("0x622E61E4E4dA55E7d2D7D6E2E2B1c64a0C7c3EF6")
	v6120Generation2Collection     = common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B")
	v6120Generation1Pool           = common.HexToAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9")
	v6120Generation2Pool           = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
)

type v6120GoldenCall struct {
	Key       string          `json:"key"`
	Signature string          `json:"signature"`
	Selector  hexutil.Bytes   `json:"selector"`
	To        common.Address  `json:"to"`
	From      *common.Address `json:"from,omitempty"`
	Input     hexutil.Bytes   `json:"input"`
	Reverted  bool            `json:"reverted"`
	Output    hexutil.Bytes   `json:"output"`
}

type v6120GoldenContract struct {
	Address    common.Address     `json:"address"`
	Role       string             `json:"role"`
	Generation int                `json:"generation"`
	CodeHash   common.Hash        `json:"codeHash"`
	CodeSize   int                `json:"codeSize"`
	Calls      []*v6120GoldenCall `json:"calls"`
}

type v6120GoldenGeneration struct {
	Generation int                       `json:"generation"`
	Roles      map[string]common.Address `json:"roles"`
	Pools      []common.Address          `json:"pools"`
}

type v6120GoldenSupply struct {
	Token       common.Address `json:"token"`
	Pool        common.Address `json:"pool"`
	TotalSupply *hexutil.Big   `json:"totalSupply"`
	HolderSum   *hexutil.Big   `json:"holderSum"`
	Holders     int            `json:"holders"`
}

// v6120Golden is golden.json as the recorder of
// testdata/v6120/record writes it: every getter of every pool, token,
// collection and role address read by eth_call at the halt height H.
type v6120Golden struct {
	Height      uint64                  `json:"height"`
	BlockHash   common.Hash             `json:"blockHash"`
	Timestamp   uint64                  `json:"timestamp"`
	Generations []v6120GoldenGeneration `json:"generations"`
	Contracts   []*v6120GoldenContract  `json:"contracts"`
	Refusals    []*v6120GoldenCall      `json:"refusals"`
	Traces      []json.RawMessage       `json:"traces"`
	Supplies    []v6120GoldenSupply     `json:"supplies"`

	byAddress map[common.Address]*v6120GoldenContract
}

func loadV6120Golden(t *testing.T) *v6120Golden {
	t.Helper()
	path := filepath.Join(v6120FixtureDir, v6120GoldenFile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Fatalf("golden fixture %s is absent: record it at the halt height H with "+
			"`go run ./modules/launchpad/keeper/testdata/v6120/record` against a node serving H (task 2.2.15)", path)
	}
	var g v6120Golden
	decodeV6120Fixture(t, v6120GoldenFile, &g)
	g.byAddress = make(map[common.Address]*v6120GoldenContract, len(g.Contracts))
	for _, c := range g.Contracts {
		_, dup := g.byAddress[c.Address]
		require.False(t, dup, "golden contract %s recorded twice", c.Address.Hex())
		g.byAddress[c.Address] = c
	}
	return &g
}

func (g *v6120Golden) generation(t *testing.T, n int) v6120GoldenGeneration {
	t.Helper()
	for _, gen := range g.Generations {
		if gen.Generation == n {
			return gen
		}
	}
	t.Fatalf("golden.json has no generation %d", n)
	return v6120GoldenGeneration{}
}

func (g *v6120Golden) pools(t *testing.T) []common.Address {
	t.Helper()
	return append(append([]common.Address{}, g.generation(t, 1).Pools...), g.generation(t, 2).Pools...)
}

func (g *v6120Golden) contract(t *testing.T, address common.Address) *v6120GoldenContract {
	t.Helper()
	c, ok := g.byAddress[address]
	require.True(t, ok, "golden.json records no contract at %s", address.Hex())
	return c
}

// call is the golden record of signature with words sent to address.
func (g *v6120Golden) call(t *testing.T, address common.Address, signature string, words ...[]byte) *v6120GoldenCall {
	t.Helper()
	input := calldata(signature, words...)
	for _, c := range g.contract(t, address).Calls {
		if c.From == nil && bytes.Equal(c.Input, input) {
			return c
		}
	}
	t.Fatalf("golden.json records no %s at %s with input %x", signature, address.Hex(), input)
	return nil
}

// calls are the golden records of signature at address.
func (g *v6120Golden) calls(t *testing.T, address common.Address, signature string) []*v6120GoldenCall {
	t.Helper()
	var out []*v6120GoldenCall
	for _, c := range g.contract(t, address).Calls {
		if c.From == nil && c.Signature == signature {
			out = append(out, c)
		}
	}
	return out
}

func (g *v6120Golden) output(t *testing.T, address common.Address, signature string, words ...[]byte) []byte {
	t.Helper()
	c := g.call(t, address, signature, words...)
	require.False(t, c.Reverted, "golden %s at %s reverted", signature, address.Hex())
	return c.Output
}

func (g *v6120Golden) uint(t *testing.T, address common.Address, signature string, words ...[]byte) sdk.Int {
	t.Helper()
	out := g.output(t, address, signature, words...)
	require.Len(t, out, 32, "golden %s at %s", signature, address.Hex())
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(out))
}

func (g *v6120Golden) uint64(t *testing.T, address common.Address, signature string, words ...[]byte) uint64 {
	t.Helper()
	v := g.uint(t, address, signature, words...)
	require.True(t, v.IsUint64(), "golden %s at %s exceeds uint64", signature, address.Hex())
	return v.Uint64()
}

func (g *v6120Golden) address(t *testing.T, address common.Address, signature string, words ...[]byte) common.Address {
	t.Helper()
	out := g.output(t, address, signature, words...)
	require.Len(t, out, 32, "golden %s at %s", signature, address.Hex())
	require.Zero(t, new(big.Int).Rsh(new(big.Int).SetBytes(out), 160).Sign())
	return common.BytesToAddress(out)
}

func (g *v6120Golden) flag(t *testing.T, address common.Address, signature string, words ...[]byte) bool {
	t.Helper()
	v := g.uint64(t, address, signature, words...)
	require.LessOrEqual(t, v, uint64(1), "golden %s at %s is not a bool", signature, address.Hex())
	return v == 1
}

// token is the pool token of pool as golden.json records it.
func (g *v6120Golden) token(t *testing.T, pool common.Address) common.Address {
	t.Helper()
	return g.address(t, pool, "tokenAddress()")
}

func (g *v6120Golden) accumulators(t *testing.T) []common.Address {
	t.Helper()
	return []common.Address{g.generation(t, 1).Roles[types.RoleFeeAccumulator],
		g.generation(t, 2).Roles[types.RoleFeeAccumulator]}
}

func uintArg(n uint64) []byte { return word(new(big.Int).SetUint64(n)) }

// v6120Run is a test application branch seeded from the halt-height export,
// with a fresh launchpad store at H+1.
type v6120Run struct {
	t       *testing.T
	ctx     sdk.Context
	key     sdk.StoreKey
	k       *keeper.Keeper
	golden  *v6120Golden
	usdl    v6120USDLFixture
	layout  v6120LayoutFixture
	holders []v6120TokenHolders
	pools   v6120PoolsFixture
}

type v6120HaltExport struct {
	InitialHeight json.Number                `json:"initial_height"`
	AppState      map[string]json.RawMessage `json:"app_state"`
}

// seedV6120 loads golden.json and the halt-height export, requires the export
// to carry the Launch-x contracts of both generations, and writes its EVM
// code, storage and nonces into a branch of the test application at H+1.
func seedV6120(t *testing.T) *v6120Run {
	t.Helper()
	app := testkeeper.EVMTestApp
	r := &v6120Run{t: t, golden: loadV6120Golden(t)}
	decodeV6120Fixture(t, "usdl.json", &r.usdl)
	decodeV6120Fixture(t, "layout.json", &r.layout)
	decodeV6120Fixture(t, "holders.json", &r.holders)
	decodeV6120Fixture(t, "pools.json", &r.pools)

	raw, err := os.ReadFile(v6120HaltExportPath)
	require.NoError(t, err, "halt-height export %s", v6120HaltExportPath)
	var export v6120HaltExport
	require.NoError(t, json.Unmarshal(raw, &export))
	initial, err := export.InitialHeight.Int64()
	require.NoError(t, err)
	require.Equal(t, int64(r.golden.Height)+1, initial,
		"halt-height export %s is not the export of golden height %d", v6120HaltExportPath, r.golden.Height)
	evmState, ok := export.AppState["evm"]
	require.True(t, ok, "halt-height export %s has no evm state", v6120HaltExportPath)
	var genesis evmgenesis.GenesisState
	require.NoError(t, app.AppCodec().UnmarshalAsJSON(evmState, &genesis))
	codes := map[common.Address]bool{}
	for _, code := range genesis.Codes {
		codes[common.HexToAddress(code.Address)] = len(code.Code) > 0
	}
	for _, g := range r.layout.Generations {
		for _, a := range []string{g.PoolRegistry, g.SidioraFactory, g.Treasury} {
			if !codes[common.HexToAddress(a)] {
				t.Fatalf("halt-height export %s carries no code at generation %d contract %s: it must be taken "+
					"at H from a chain holding the Launch-x contracts of both generations", v6120HaltExportPath,
					g.Generation, a)
			}
		}
	}
	for _, pool := range r.golden.pools(t) {
		if !codes[pool] {
			t.Fatalf("halt-height export %s carries no code at golden pool %s", v6120HaltExportPath, pool.Hex())
		}
	}

	ctx := app.GetContextForDeliverTx([]byte{})
	ctx, _ = ctx.CacheContext()
	ctx = ctx.WithBlockHeight(initial).WithBlockTime(time.Unix(int64(r.golden.Timestamp)+1, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	for _, code := range genesis.Codes {
		app.EvmKeeper.SetCode(ctx, common.HexToAddress(code.Address), code.Code)
	}
	for _, state := range genesis.States {
		app.EvmKeeper.SetState(ctx, common.HexToAddress(state.Address), common.BytesToHash(state.Key),
			common.BytesToHash(state.Value))
	}
	for _, nonce := range genesis.Nonces {
		app.EvmKeeper.SetNonce(ctx, common.HexToAddress(nonce.Address), nonce.Nonce)
	}
	r.key = sdk.NewKVStoreKey(types.StoreKey)
	r.ctx = launchpadtest.WithStore(ctx, r.key)
	r.k = keeper.NewKeeper(r.key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper),
		&app.EvmKeeper)
	r.k.InitGenesis(r.ctx, *types.DefaultGenesis())
	legacy := keeperv610.NewKeeper(r.key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper),
		&app.EvmKeeper)
	params := typesv610.DefaultParams()
	params.QuoteDenom = "uhpx"
	require.NoError(t, legacy.UpdateParams(r.ctx, legacy.Authority(), params))
	pending, err := sdk.ZeroInt().Marshal()
	require.NoError(t, err)
	r.ctx.KVStore(r.key).Set(types.ProtocolFeesPrefix, pending)
	return r
}

func (r *v6120Run) migrate() error {
	return keeper.MigrateV6120(r.ctx, *r.k, migrationEVM{&testkeeper.EVMTestApp.EvmKeeper})
}

// migratedV6120 is the seeded branch after MigrateV6120 succeeded.
func migratedV6120(t *testing.T) *v6120Run {
	t.Helper()
	r := seedV6120(t)
	require.NoError(t, r.migrate())
	return r
}

func (r *v6120Run) view(to common.Address, signature string, words ...[]byte) []byte {
	r.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(r.ctx, r.k.ModuleAddress(), &to,
		calldata(signature, words...))
	require.NoError(r.t, err, "%s at %s", signature, to.Hex())
	return ret
}

func (r *v6120Run) viewInt(to common.Address, signature string, words ...[]byte) sdk.Int {
	r.t.Helper()
	ret := r.view(to, signature, words...)
	require.Len(r.t, ret, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret))
}

func (r *v6120Run) market(pool common.Address) types.Market {
	r.t.Helper()
	market, ok := r.k.GetMarket(r.ctx, types.ModuleName+"/"+pool.Hex())
	require.True(r.t, ok, "no market for pool %s", pool.Hex())
	return market
}

func (r *v6120Run) account(a common.Address) string {
	return testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(r.ctx, a).String()
}

func (r *v6120Run) optionalAccount(a common.Address) string {
	if a == (common.Address{}) {
		return ""
	}
	return r.account(a)
}

func (r *v6120Run) usdlBalance(holder common.Address) sdk.Int {
	r.t.Helper()
	for _, b := range r.usdl.Balances {
		if common.HexToAddress(b.Address) == holder {
			return sdk.NewIntFromBigInt(requireAmount(r.t, b.Balance, false))
		}
	}
	r.t.Fatalf("usdl.json lists no balance of %s", holder.Hex())
	return sdk.Int{}
}

// generation2Of is the generation 2 address serving the role of a
// generation 1 or generation 2 role contract recorded in golden.json.
func (r *v6120Run) generation2Of(c *v6120GoldenContract) common.Address {
	r.t.Helper()
	if c.Role == types.RoleTreasury {
		return common.HexToAddress(r.layout.Treasury)
	}
	a, ok := r.golden.generation(r.t, 2).Roles[c.Role]
	require.True(r.t, ok, "golden generation 2 has no %s", c.Role)
	return a
}

func (r *v6120Run) generation1FeeRights() map[uint64]uint64 {
	r.t.Helper()
	ids, err := r.k.FeeRightIDMap(r.ctx)
	require.NoError(r.t, err)
	out := map[uint64]uint64{}
	for _, m := range ids {
		out[m.Generation1ID] = m.Generation2ID
	}
	return out
}

func TestEveryPoolMigrates(t *testing.T) {
	r := migratedV6120(t)
	pools := r.golden.pools(t)
	require.Equal(t, uint64(len(pools)), r.k.GetMarketCount(r.ctx))
	runtime := common.FromHex(v6120SurfaceRuntime)
	require.Len(t, runtime, 41)
	for i, pool := range pools {
		market, ok := r.k.GetMarketByIndex(r.ctx, uint64(i+1))
		require.True(t, ok, "no market at index %d", i+1)
		require.Equal(t, pool.Hex(), market.PoolAddress)
		require.Equal(t, uint64(i+1), market.Index)
		require.Equal(t, market, r.market(pool))
		surface, found, err := r.k.GetSurface(r.ctx, pool)
		require.NoError(t, err)
		require.True(t, found, "pool %s is not a registered surface", pool.Hex())
		want := types.Generation1
		if i >= len(r.golden.generation(t, 1).Pools) {
			want = types.Generation2
		}
		require.Equal(t, keeper.Surface{Role: types.RolePool, Generation: want, Market: market.Denom}, surface)
		require.Equal(t, runtime, testkeeper.EVMTestApp.EvmKeeper.GetCode(r.ctx, pool))
	}
}

func TestPoolFieldsMatchGolden(t *testing.T) {
	r := migratedV6120(t)
	for _, pool := range r.golden.pools(t) {
		market := r.market(pool)
		g := r.golden
		require.Equal(t, g.token(t, pool).Hex(), market.TokenAddress, pool.Hex())
		require.Equal(t, g.address(t, pool, "opticalAddress()").Hex(), market.Optical, pool.Hex())
		require.Equal(t, r.account(g.address(t, pool, "guardian()")), market.Guardian, pool.Hex())
		require.Equal(t, g.uint(t, pool, "virtualUsdlReserve()"), market.VirtualQuoteReserve, pool.Hex())
		require.Equal(t, g.uint(t, pool, "creationTimestamp()").Int64(), market.CreationTime, pool.Hex())
		require.Equal(t, g.uint(t, pool, "cumulativeVolume()"), market.CumulativeVolume, pool.Hex())
		require.Equal(t, g.uint64(t, pool, "snapshotIndex()"), market.SnapshotIndex, pool.Hex())
		require.Equal(t, g.uint64(t, pool, "snapshotCount()"), market.SnapshotCount, pool.Hex())
		require.Equal(t, g.uint(t, pool, "accumulatedUsdlFees()"), market.AccumulatedQuoteFees, pool.Hex())
		require.Equal(t, g.uint(t, pool, "accumulatedTokenFees()"), market.AccumulatedTokenFees, pool.Hex())
		require.Len(t, market.PriceSnapshots, types.SnapshotSlots)
		for i := range market.PriceSnapshots {
			require.Equal(t, g.uint(t, pool, "priceSnapshots(uint256)", uintArg(uint64(i))), market.PriceSnapshots[i],
				"%s snapshot %d", pool.Hex(), i)
		}
		// Slots 24 to 28 are absent from the generation 1 implementation;
		// their getters revert there and the market carries zero.
		for _, f := range []struct {
			signature string
			got       sdk.Int
		}{
			{"lastSnapshotTimestamp()", sdk.NewInt(market.LastSnapshotTimestamp)},
			{"cumulativeBuyUsdlVolume()", market.CumulativeBuyQuoteVolume},
			{"cumulativeSellUsdlVolume()", market.CumulativeSellQuoteVolume},
		} {
			c := g.call(t, pool, f.signature)
			if c.Reverted {
				require.True(t, f.got.IsZero(), "%s %s", pool.Hex(), f.signature)
				continue
			}
			require.Equal(t, g.uint(t, pool, f.signature), f.got, "%s %s", pool.Hex(), f.signature)
		}
		if c := g.call(t, pool, "pendingGuardian()"); c.Reverted {
			require.Empty(t, market.PendingGuardian, pool.Hex())
		} else {
			require.Equal(t, r.optionalAccount(g.address(t, pool, "pendingGuardian()")), market.PendingGuardian, pool.Hex())
		}
	}
}

func TestPoolTokensStayContracts(t *testing.T) {
	r := migratedV6120(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	for _, pool := range r.golden.pools(t) {
		token := r.golden.token(t, pool)
		recorded := r.golden.contract(t, token)
		require.Equal(t, types.RolePool, r.golden.contract(t, pool).Role)
		require.Equal(t, "SidioraERC20", recorded.Role)
		require.Equal(t, recorded.CodeHash, evm.GetCodeHash(r.ctx, token), "token %s code changed", token.Hex())
		require.Len(t, evm.GetCode(r.ctx, token), recorded.CodeSize)
		_, found, err := r.k.GetSurface(r.ctx, token)
		require.NoError(t, err)
		require.False(t, found, "token %s became a surface", token.Hex())
		supply := r.golden.uint(t, token, "totalSupply()")
		require.Equal(t, supply, r.viewInt(token, "totalSupply()"))
		require.Equal(t, supply, r.market(pool).TotalSupply)
		require.Equal(t, r.golden.output(t, token, "name()"), r.view(token, "name()"))
		require.Equal(t, r.golden.output(t, token, "symbol()"), r.view(token, "symbol()"))
		require.Equal(t, pool, r.golden.address(t, token, "pool()"))
	}
}

func TestPoolTokenBalancesMatchGolden(t *testing.T) {
	r := migratedV6120(t)
	supplies := map[common.Address]v6120GoldenSupply{}
	for _, s := range r.golden.Supplies {
		supplies[s.Token] = s
	}
	listed := map[common.Address][]string{}
	for _, h := range r.holders {
		listed[common.HexToAddress(h.Token)] = h.Holders
	}
	for _, pool := range r.golden.pools(t) {
		token := r.golden.token(t, pool)
		supply, ok := supplies[token]
		require.True(t, ok, "golden.json has no supply record of %s", token.Hex())
		require.Equal(t, pool, supply.Pool)
		holders := listed[token]
		require.Len(t, holders, supply.Holders)
		sum := sdk.ZeroInt()
		for _, h := range holders {
			holder := common.HexToAddress(h)
			balance := r.viewInt(token, "balanceOf(address)", addressWord(holder))
			require.Equal(t, r.golden.uint(t, token, "balanceOf(address)", addressWord(holder)), balance,
				"%s balance of %s", token.Hex(), h)
			sum = sum.Add(balance)
		}
		require.Equal(t, sdk.NewIntFromBigInt(supply.TotalSupply.ToInt()), sum, token.Hex())
		require.Equal(t, sdk.NewIntFromBigInt(supply.HolderSum.ToInt()), sum, token.Hex())
	}
}

func TestReservesAreMinOfBookAndHeld(t *testing.T) {
	r := migratedV6120(t)
	quote := common.HexToAddress(r.usdl.Address)
	var want []types.BookHeldDelta
	for _, pool := range r.golden.pools(t) {
		market := r.market(pool)
		token := r.golden.token(t, pool)
		bookQuote, heldQuote := r.golden.uint(t, pool, "realUsdlBalance()"), r.usdlBalance(pool)
		bookToken := r.golden.uint(t, pool, "tokenReserve()")
		heldToken := r.golden.uint(t, token, "balanceOf(address)", addressWord(pool))
		require.Equal(t, sdk.MinInt(bookQuote, heldQuote), market.RealQuoteBalance, pool.Hex())
		require.Equal(t, sdk.MinInt(bookToken, heldToken), market.TokenReserve, pool.Hex())
		ledger, err := r.k.GetQuoteLedger(r.ctx, keeper.HeldQuoteLedger(pool))
		require.NoError(t, err)
		require.Equal(t, heldQuote, ledger, pool.Hex())
		require.True(t, ledger.GTE(market.RealQuoteBalance))
		require.True(t, r.viewInt(token, "balanceOf(address)", addressWord(pool)).GTE(market.TokenReserve))
		for _, pair := range []struct {
			asset      common.Address
			book, held sdk.Int
		}{{quote, bookQuote, heldQuote}, {token, bookToken, heldToken}} {
			got, found, err := r.k.GetBookHeldDelta(r.ctx, pool, pair.asset)
			require.NoError(t, err)
			if pair.book.Equal(pair.held) {
				require.False(t, found, "%s %s", pool.Hex(), pair.asset.Hex())
				continue
			}
			d := types.BookHeldDelta{Pool: pool.Hex(), Asset: pair.asset.Hex(), Book: pair.book, Held: pair.held,
				Delta: pair.held.Sub(pair.book)}
			require.True(t, found, "%s %s", pool.Hex(), pair.asset.Hex())
			require.Equal(t, d, got)
			want = append(want, d)
		}
	}
	deltas, err := r.k.BookHeldDeltas(r.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, want, deltas)
	for _, known := range r.pools.Deltas {
		got, found, err := r.k.GetBookHeldDelta(r.ctx, common.HexToAddress(known.Pool),
			common.HexToAddress(known.AssetAddress))
		require.NoError(t, err)
		require.True(t, found, "known delta %s %s", known.Pool, known.Asset)
		require.Equal(t, known.Delta, got.Delta.String())
	}
}

func TestMigratedPoolsBecomeGeneration2(t *testing.T) {
	r := migratedV6120(t)
	for _, pool := range r.golden.pools(t) {
		market := r.market(pool)
		require.Equal(t, types.Generation2, market.Generation, pool.Hex())
		require.Equal(t, v6120Generation2Factory.Hex(), market.Factory)
		require.Equal(t, v6120Generation2FeeAccumulator.Hex(), market.FeeAccumulator)
		require.Equal(t, v6120Generation2ProtocolConfig.Hex(), market.ProtocolConfig)
		require.Equal(t, v6120Generation2EventEmitter.Hex(), market.EventEmitter)
		require.Equal(t, v6120Generation2Collection.Hex(), market.FeeRightCollection)
		require.True(t, r.k.HasRole(r.ctx, v6120Generation2FeeAccumulator, keeper.PoolRole, pool), pool.Hex())
		registered, ok := r.k.PoolByToken(r.ctx, r.golden.token(t, pool))
		require.True(t, ok)
		require.Equal(t, pool, registered)
	}
	g1 := r.golden.generation(t, 1).Roles[types.RoleProtocolConfig]
	g2 := r.golden.generation(t, 2).Roles[types.RoleProtocolConfig]
	require.Equal(t, v6120Generation2ProtocolConfig, g2)
	var want []map[string]string
	if a, b := r.golden.address(t, g1, "usdlAddress()"), r.golden.address(t, g2, "usdlAddress()"); a != b {
		want = append(want, map[string]string{keeper.AttributeKeyField: "usdlAddress",
			keeper.AttributeKeyGeneration1: a.Hex(), keeper.AttributeKeyGeneration2: b.Hex()})
	}
	for _, field := range []string{"virtualUsdlDefault", "virtualTokenDefault", "minFeeBps", "maxFeeBps",
		"baseFeeBps", "protocolFeeBps", "feeDecayRate", "volatilityWeight", "concentrationWeight", "creationFee"} {
		a, b := r.golden.uint(t, g1, field+"()"), r.golden.uint(t, g2, field+"()")
		if !a.Equal(b) {
			want = append(want, map[string]string{keeper.AttributeKeyField: field,
				keeper.AttributeKeyGeneration1: a.String(), keeper.AttributeKeyGeneration2: b.String()})
		}
	}
	got := eventsOf(r.ctx, keeper.EventTypeMigrationProtocolConfigDiff)
	if want == nil {
		want = []map[string]string{}
	}
	require.Equal(t, want, got)
}

func TestPoolTokenExtrasKept(t *testing.T) {
	r := migratedV6120(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	for _, pool := range r.golden.pools(t) {
		token := r.golden.token(t, pool)
		recorded := r.golden.contract(t, token)
		for _, c := range recorded.Calls {
			if c.From != nil {
				continue
			}
			ret, err := evm.StaticCallEVM(r.ctx, r.k.ModuleAddress(), &token, c.Input)
			if c.Reverted {
				require.Error(t, err, "%s %s", token.Hex(), c.Key)
				continue
			}
			require.NoError(t, err, "%s %s", token.Hex(), c.Key)
			require.Equal(t, []byte(c.Output), ret, "%s %s input %x", token.Hex(), c.Key, []byte(c.Input))
		}
		for _, signature := range []string{"DOMAIN_SEPARATOR()", "PERMIT_TYPEHASH()", "deploymentFactory()",
			"decimals()"} {
			require.NotEmpty(t, r.golden.calls(t, token, signature), "%s %s", token.Hex(), signature)
		}
		for _, signature := range []string{"nonces(address)", "allowance(address,address)",
			"getPastBalance(address,uint256)", "getPastTotalSupply(uint256)", "getPastVotes(address,uint256)"} {
			require.NotEmpty(t, r.golden.calls(t, token, signature), "%s %s", token.Hex(), signature)
		}
		market := r.market(pool)
		require.Equal(t, r.golden.flag(t, pool, "paused()"), market.Paused, pool.Hex())
		require.Equal(t, r.account(r.golden.address(t, pool, "guardian()")), market.Guardian, pool.Hex())
	}
}

func TestFeeRightsMatchGolden(t *testing.T) {
	r := migratedV6120(t)
	g1 := r.golden.generation(t, 1).Roles[types.RoleFeeRights]
	g2 := r.golden.generation(t, 2).Roles[types.RoleFeeRights]
	require.Equal(t, v6120Generation2Collection, g2)
	mapping := r.generation1FeeRights()
	next := r.golden.uint64(t, g2, "nextTokenId()")
	g2Supply := r.golden.uint64(t, g2, "totalSupply()")
	g1Supply := r.golden.uint64(t, g1, "totalSupply()")

	var g1IDs []uint64
	for i := uint64(0); i < g1Supply; i++ {
		g1IDs = append(g1IDs, r.golden.uint64(t, g1, "tokenByIndex(uint256)", uintArg(i)))
	}
	require.Len(t, mapping, len(g1IDs))
	sorted := append([]uint64{}, g1IDs...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	for n, id := range sorted {
		require.Equal(t, next+uint64(n), mapping[id], "generation 1 fee right %d", id)
	}

	balances := map[common.Address]uint64{}
	check := func(collection common.Address, id, newID uint64) {
		owner := r.golden.address(t, collection, "ownerOf(uint256)", uintArg(id))
		got, err := r.k.FeeRightOwnerOf(r.ctx, g2, newID)
		require.NoError(t, err)
		require.Equal(t, owner, got, "fee right %s/%d", collection.Hex(), id)
		right, found, err := r.k.GetFeeRight(r.ctx, g2, newID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, types.FeeStrategy(r.golden.uint64(t, collection, "getFeeStrategy(uint256)", uintArg(id))),
			right.Strategy)
		pool := r.golden.address(t, collection, "getPoolAddress(uint256)", uintArg(id))
		require.Equal(t, pool.Hex(), right.Pool)
		approved, err := r.k.FeeRightGetApproved(r.ctx, g2, newID)
		require.NoError(t, err)
		require.Equal(t, r.golden.address(t, collection, "getApproved(uint256)", uintArg(id)), approved)
		require.Equal(t, newID, r.k.FeeRightPoolTokenID(r.ctx, g2, pool))
		market := r.market(pool)
		require.Equal(t, newID, market.FeeRightID)
		require.Equal(t, r.account(owner), market.FeeRightsHolder)
		require.Equal(t, right.Strategy, market.FeeStrategy)
		balances[owner]++
	}
	for i := uint64(0); i < g2Supply; i++ {
		id := r.golden.uint64(t, g2, "tokenByIndex(uint256)", uintArg(i))
		got, err := r.k.FeeRightByIndex(r.ctx, g2, i)
		require.NoError(t, err)
		require.Equal(t, id, got, "generation 2 fee right index %d keeps its id", i)
		check(g2, id, id)
	}
	for _, id := range g1IDs {
		check(g1, id, mapping[id])
	}
	for owner, n := range balances {
		got, err := r.k.FeeRightBalanceOf(r.ctx, g2, owner)
		require.NoError(t, err)
		require.Equal(t, n, got, owner.Hex())
		for i := uint64(0); i < n; i++ {
			id, err := r.k.FeeRightOfOwnerByIndex(r.ctx, g2, owner, i)
			require.NoError(t, err)
			holder, err := r.k.FeeRightOwnerOf(r.ctx, g2, id)
			require.NoError(t, err)
			require.Equal(t, owner, holder)
		}
	}
	for _, collection := range []common.Address{g1, g2} {
		for _, c := range r.golden.calls(t, collection, "isApprovedForAll(address,address)") {
			require.False(t, c.Reverted)
			owner, operator := common.BytesToAddress(c.Input[4:36]), common.BytesToAddress(c.Input[36:68])
			if new(big.Int).SetBytes(c.Output).Sign() == 1 {
				require.True(t, r.k.FeeRightIsApprovedForAll(r.ctx, g2, owner, operator), "%s %s", owner.Hex(),
					operator.Hex())
			}
		}
	}
	state, err := r.k.GetFeeRightCollection(r.ctx, g2)
	require.NoError(t, err)
	total := g2Supply + uint64(len(g1IDs))
	require.Equal(t, keeper.FeeRightCollectionState{NextTokenID: next + uint64(len(g1IDs)),
		PoolIndexMigrationCursor: next + uint64(len(g1IDs)), PoolIndexReady: true, TotalSupply: total}, state)
	require.Equal(t, uint64(len(r.golden.pools(t))), total)
}

func TestRolesMigrateWithoutAdmin(t *testing.T) {
	r := migratedV6120(t)
	checked := 0
	for _, c := range r.golden.Contracts {
		if c.Role == types.RolePool || c.Role == "SidioraERC20" {
			continue
		}
		calls := r.golden.calls(t, c.Address, "hasRole(bytes32,address)")
		if len(calls) == 0 {
			continue
		}
		target := r.generation2Of(c)
		require.Zero(t, r.k.RegisteredDefaultAdminCount(r.ctx, target), target.Hex())
		for _, call := range calls {
			require.False(t, call.Reverted)
			require.Len(t, call.Input, 68)
			var role [32]byte
			copy(role[:], call.Input[4:36])
			account := common.BytesToAddress(call.Input[36:68])
			if role == keeper.DefaultAdminRole {
				require.False(t, r.k.HasRole(r.ctx, target, role, account), "%s admin %s", target.Hex(), account.Hex())
				continue
			}
			if new(big.Int).SetBytes(call.Output).Sign() == 1 {
				require.True(t, r.k.HasRole(r.ctx, target, role, account), "%s %s role %x of %s", c.Role,
					target.Hex(), role, account.Hex())
				checked++
			}
		}
	}
	require.Positive(t, checked, "golden.json records no role membership")
}

func TestAccountingMatchesGolden(t *testing.T) {
	r := migratedV6120(t)
	accumulators := r.golden.accumulators(t)
	require.Equal(t, v6120Generation2FeeAccumulator, accumulators[1])
	require.True(t, r.golden.flag(t, accumulators[1], "accountingInitialized()"))
	require.True(t, r.golden.uint(t, accumulators[1], "getProtocolFeesPending()").IsZero())
	liabilities := r.golden.uint(t, accumulators[1], "totalLiabilities()")
	for _, pool := range r.golden.pools(t) {
		fees, airdrop, surplus := sdk.ZeroInt(), sdk.ZeroInt(), sdk.ZeroInt()
		for i, a := range accumulators {
			f := r.golden.uint(t, a, "getAccumulatedFees(address)", addressWord(pool))
			d := r.golden.uint(t, a, "getAirdropBalance(address)", addressWord(pool))
			o := r.golden.uint(t, a, "getOpticalSurplus(address)", addressWord(pool))
			fees, airdrop, surplus = fees.Add(f), airdrop.Add(d), surplus.Add(o)
			if i == 0 {
				liabilities = liabilities.Add(f).Add(d).Add(o)
			}
			require.True(t, r.golden.uint(t, a, "getLpRewardsBalance(address)", addressWord(pool)).IsZero())
		}
		market := r.market(pool)
		require.Equal(t, fees, market.AccumulatedFees, pool.Hex())
		require.Equal(t, airdrop, market.AirdropBalance, pool.Hex())
		got, err := r.k.OpticalSurplus(r.ctx, pool)
		require.NoError(t, err)
		require.Equal(t, surplus, got, pool.Hex())
	}
	got, err := r.k.TotalLiabilities(r.ctx)
	require.NoError(t, err)
	require.Equal(t, liabilities, got)
	require.Equal(t, []map[string]string{{keeper.AttributeKeyAddress: accumulators[0].Hex(),
		keeper.AttributeKeyTreasury: r.golden.generation(t, 1).Roles[types.RoleTreasury].Hex(),
		types.AttributeKeyAmount:    r.golden.uint(t, accumulators[0], "getProtocolFeesPending()").String()}},
		eventsOf(r.ctx, keeper.EventTypeMigrationSettledProtocolFees))
	held := sdk.ZeroInt()
	for _, a := range accumulators {
		held = held.Add(r.usdlBalance(a))
	}
	feeHeld, err := r.k.GetQuoteLedger(r.ctx, keeper.FeeHeldLedger(v6120Generation2FeeAccumulator))
	require.NoError(t, err)
	require.Equal(t, held, feeHeld)
	legacy, err := r.k.GetQuoteLedger(r.ctx, keeper.FeeHeldLedger(accumulators[0]))
	require.NoError(t, err)
	require.True(t, legacy.IsZero())
}

func TestOpenAirdropEpochsMigrate(t *testing.T) {
	r := migratedV6120(t)
	holders := map[common.Address][]string{}
	for _, h := range r.holders {
		holders[common.HexToAddress(h.Token)] = h.Holders
	}
	for _, pool := range r.golden.pools(t) {
		total := uint64(0)
		for _, a := range r.golden.accumulators(t) {
			epochs := r.golden.uint64(t, a, "getAirdropEpoch(address)", addressWord(pool))
			total += epochs
			for e := uint64(1); e <= epochs; e++ {
				record, found, err := r.k.GetPoolAirdropEpoch(r.ctx, pool, e)
				require.NoError(t, err)
				require.True(t, found, "%s epoch %d", pool.Hex(), e)
				args := [][]byte{addressWord(pool), uintArg(e)}
				require.Equal(t, r.golden.uint64(t, a, "airdropSnapshotBlock(address,uint256)", args...),
					record.SnapshotBlock)
				require.Equal(t, r.golden.uint(t, a, "airdropEligibleSupply(address,uint256)", args...),
					record.EligibleSupply)
				require.Equal(t, r.golden.uint(t, a, "airdropEpochRemaining(address,uint256)", args...),
					record.Remaining)
				require.Equal(t, r.golden.uint(t, a, "airdropClaimedSupply(address,uint256)", args...),
					record.ClaimedSupply)
				require.Equal(t, r.golden.uint(t, a, "legacyAirdropRemaining(address,uint256)", args...),
					record.LegacyRemaining)
				root := common.BytesToHash(r.golden.output(t, a, "legacyAirdropRoot(address,uint256)", args...))
				if root == (common.Hash{}) {
					require.Empty(t, record.LegacyRoot)
				} else {
					require.Equal(t, root.Hex(), record.LegacyRoot)
				}
				for _, h := range holders[r.golden.token(t, pool)] {
					holder := common.HexToAddress(h)
					claimed := r.golden.flag(t, a, "hasClaimedAirdrop(address,address,uint256)", addressWord(pool),
						addressWord(holder), uintArg(e))
					require.Equal(t, claimed, r.k.HasClaimedPoolAirdrop(r.ctx, pool, holder, e), "%s %s %d",
						pool.Hex(), h, e)
				}
			}
		}
		require.Equal(t, total, r.market(pool).AirdropEpoch, pool.Hex())
		_, found, err := r.k.GetPoolAirdropEpoch(r.ctx, pool, total+1)
		require.NoError(t, err)
		require.False(t, found)
	}
}

func TestRegistryMetadataMatchesGolden(t *testing.T) {
	r := migratedV6120(t)
	mapping := r.generation1FeeRights()
	for _, gen := range []int{1, 2} {
		registry := r.golden.generation(t, gen).Roles[types.RolePoolRegistry]
		for _, pool := range r.golden.generation(t, gen).Pools {
			out := r.golden.output(t, registry, "getPoolMetadata(address)", addressWord(pool))
			require.Len(t, out, 6*32)
			at := func(i int) []byte { return out[i*32 : (i+1)*32] }
			nftID := new(big.Int).SetBytes(at(3)).Uint64()
			if gen == 1 {
				mapped, ok := mapping[nftID]
				require.True(t, ok, "generation 1 fee right %d of %s is not mapped", nftID, pool.Hex())
				nftID = mapped
			}
			want := keeper.PoolMetadata{Creator: common.BytesToAddress(at(0)), Token: common.BytesToAddress(at(1)),
				Optical: common.BytesToAddress(at(2)), NFTID: nftID,
				CreatedAt: new(big.Int).SetBytes(at(4)).Int64(), CreatedBlock: new(big.Int).SetBytes(at(5)).Int64()}
			got, found, err := r.k.GetPoolMetadata(r.ctx, pool)
			require.NoError(t, err)
			require.True(t, found, pool.Hex())
			require.Equal(t, want, got, pool.Hex())
			require.Equal(t, r.golden.address(t, registry, "getPoolByToken(address)", addressWord(want.Token)), pool)
			market := r.market(pool)
			require.Equal(t, want.Optical.Hex(), market.Optical)
			require.Equal(t, r.account(want.Creator), market.Creator)
			if want.Optical != (common.Address{}) {
				recorded, ok := r.golden.byAddress[want.Optical]
				if ok {
					require.Equal(t, recorded.CodeHash, testkeeper.EVMTestApp.EvmKeeper.GetCodeHash(r.ctx, want.Optical))
				}
			}
		}
	}
}

func TestMigrationIsAtomic(t *testing.T) {
	r := seedV6120(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	var pendingSlot uint64
	found := false
	for _, s := range r.layout.Storage[types.RoleFeeAccumulator] {
		if s.Field == "_protocolFeesPending" {
			pendingSlot, found = s.Slot, true
		}
	}
	require.True(t, found)
	evm.SetState(r.ctx, v6120Generation2FeeAccumulator, common.BigToHash(new(big.Int).SetUint64(pendingSlot)),
		common.BigToHash(big.NewInt(1)))

	before := storeEntries(r.ctx, r.key)
	custody := r.viewInt(common.HexToAddress(r.usdl.Address), "balanceOf(address)",
		addressWord(r.k.ModuleEVMAddress(r.ctx)))
	err := r.migrate()
	require.ErrorIs(t, err, keeper.ErrNoKeeperStore)
	require.True(t, strings.HasPrefix(upgradeError(err).Error(), "upgrade v6.12.0: launchpad: "))
	require.Equal(t, before, storeEntries(r.ctx, r.key))
	require.Zero(t, r.k.GetMarketCount(r.ctx))
	quote := common.HexToAddress(r.usdl.Address)
	require.Equal(t, custody, r.viewInt(quote, "balanceOf(address)", addressWord(r.k.ModuleEVMAddress(r.ctx))))
	for _, b := range r.usdl.Balances {
		holder := common.HexToAddress(b.Address)
		require.Equal(t, sdk.NewIntFromBigInt(requireAmount(t, b.Balance, false)),
			r.viewInt(quote, "balanceOf(address)", addressWord(holder)), b.Address)
	}
	for _, pool := range r.golden.pools(t) {
		require.Equal(t, r.golden.contract(t, pool).CodeHash, evm.GetCodeHash(r.ctx, pool), pool.Hex())
	}
	require.Empty(t, eventsOf(r.ctx, keeper.EventTypeMigrationProtocolConfigDiff))
}

func TestMigrationIsAtomicOnLegacyProtocolFees(t *testing.T) {
	r := migratedV6120(t)
	require.False(t, r.ctx.KVStore(r.key).Has(types.ProtocolFeesPrefix))
	require.NotPanics(t, func() { r.k.ExportGenesis(r.ctx) })

	r = seedV6120(t)
	pending, err := sdk.NewInt(1).Marshal()
	require.NoError(t, err)
	r.ctx.KVStore(r.key).Set(types.ProtocolFeesPrefix, pending)
	before := storeEntries(r.ctx, r.key)
	err = r.migrate()
	require.ErrorIs(t, err, keeper.ErrNoKeeperStore)
	require.Equal(t, before, storeEntries(r.ctx, r.key))
	require.Zero(t, r.k.GetMarketCount(r.ctx))
}

func TestQuoteTokenIsUsdl(t *testing.T) {
	r := migratedV6120(t)
	require.Equal(t, "0x85FcD13735F4309833A503EE804ea32395851479", r.k.GetParams(r.ctx).QuoteToken)
	require.Equal(t, r.usdl.Address, r.k.GetParams(r.ctx).QuoteToken)
	quote := common.HexToAddress(r.usdl.Address)
	require.Equal(t, common.HexToHash(r.usdl.CodeHash), testkeeper.EVMTestApp.EvmKeeper.GetCodeHash(r.ctx, quote))
	require.Equal(t, sdk.NewIntFromBigInt(requireAmount(t, r.usdl.TotalSupply, false)), r.viewInt(quote, "totalSupply()"))
	require.Equal(t, word(big.NewInt(6)), r.view(quote, "decimals()"))
	_, found := testkeeper.EVMTestApp.BankKeeper.GetDenomMetaData(r.ctx, "uusdl")
	require.False(t, found, "a uusdl bank denom exists")
	for _, pool := range r.golden.pools(t) {
		require.Equal(t, quote, r.golden.address(t, pool, "usdlAddress()"), pool.Hex())
	}
}

func TestFixturesAreConsistent(t *testing.T) {
	g := loadV6120Golden(t)
	var usdl v6120USDLFixture
	var layout v6120LayoutFixture
	var holders []v6120TokenHolders
	var pools v6120PoolsFixture
	decodeV6120Fixture(t, "usdl.json", &usdl)
	decodeV6120Fixture(t, "layout.json", &layout)
	decodeV6120Fixture(t, "holders.json", &holders)
	decodeV6120Fixture(t, "pools.json", &pools)
	require.Equal(t, layout.Height, g.Height)
	require.Equal(t, usdl.Height, g.Height)
	require.Equal(t, pools.Height, g.Height)
	require.NotEqual(t, common.Hash{}, g.BlockHash)
	require.NotZero(t, g.Timestamp)
	require.Len(t, g.Generations, 2)

	byGen := map[int][]common.Address{}
	for _, b := range usdl.Balances {
		if b.Role == types.RolePool {
			byGen[int(b.Generation)] = append(byGen[int(b.Generation)], common.HexToAddress(b.Address))
		}
	}
	for _, lg := range layout.Generations {
		gen := g.generation(t, int(lg.Generation))
		require.Equal(t, byGen[int(lg.Generation)], gen.Pools, "generation %d pools", lg.Generation)
		require.Equal(t, lg.PoolCount, uint64(len(gen.Pools)))
		require.Equal(t, common.HexToAddress(lg.PoolRegistry), gen.Roles[types.RolePoolRegistry])
		require.Equal(t, common.HexToAddress(lg.SidioraFactory), gen.Roles[types.RoleFactory])
		require.Equal(t, uint64(len(gen.Pools)), g.uint64(t, gen.Roles[types.RolePoolRegistry], "getPoolCount()"))
		for _, pool := range gen.Pools {
			c := g.contract(t, pool)
			require.Equal(t, types.RolePool, c.Role)
			require.Equal(t, int(lg.Generation), c.Generation)
			require.Equal(t, common.HexToHash(lg.PoolProxyCodeHash), c.CodeHash, pool.Hex())
		}
	}
	listed := map[common.Address]int{}
	for _, h := range holders {
		listed[common.HexToAddress(h.Token)] = len(h.Holders)
	}
	require.Len(t, g.Supplies, len(listed))
	for _, s := range g.Supplies {
		require.Equal(t, 0, s.TotalSupply.ToInt().Cmp(s.HolderSum.ToInt()), s.Token.Hex())
		require.Equal(t, listed[s.Token], s.Holders, s.Token.Hex())
		require.Equal(t, s.Token, g.token(t, s.Pool))
	}
	for _, d := range pools.Deltas {
		pool := common.HexToAddress(d.Pool)
		held := requireAmount(t, d.Held, false)
		book := requireAmount(t, d.Book, false)
		switch d.Asset {
		case "usdl":
			require.Equal(t, 0, g.uint(t, pool, "realUsdlBalance()").BigInt().Cmp(book), d.Pool)
		case "token":
			require.Equal(t, 0, g.uint(t, pool, "tokenReserve()").BigInt().Cmp(book), d.Pool)
			require.Equal(t, 0, g.uint(t, common.HexToAddress(d.AssetAddress), "balanceOf(address)",
				addressWord(pool)).BigInt().Cmp(held), d.Pool)
		}
	}
	quote := common.HexToAddress(usdl.Address)
	for _, b := range usdl.Balances {
		if c, ok := g.byAddress[common.HexToAddress(b.Address)]; ok && c.Role == types.RolePool {
			require.Equal(t, quote, g.address(t, c.Address, "usdlAddress()"))
		}
	}
	require.Equal(t, common.HexToHash(usdl.CodeHash), g.contract(t, quote).CodeHash)
}

func TestInvariantsHoldAfterMigration(t *testing.T) {
	r := seedV6120(t)
	quote := common.HexToAddress(r.usdl.Address)
	custody := r.k.ModuleEVMAddress(r.ctx)
	before := r.viewInt(quote, "balanceOf(address)", addressWord(custody))
	moved := sdk.ZeroInt()
	for _, b := range r.usdl.Balances {
		if b.Role == types.RolePool || b.Role == types.RoleFeeAccumulator || b.Role == types.RoleTreasury {
			moved = moved.Add(sdk.NewIntFromBigInt(requireAmount(t, b.Balance, false)))
		}
	}
	require.NoError(t, r.migrate())
	require.Equal(t, before.Add(moved), r.viewInt(quote, "balanceOf(address)", addressWord(custody)))
	for name, invariant := range map[string]sdk.Invariant{"custody": keeper.CustodyInvariant(r.k),
		"solvency": keeper.SolvencyInvariant(r.k), "liabilities": keeper.LiabilitiesInvariant(r.k)} {
		message, broken := invariant(r.ctx)
		require.False(t, broken, "%s invariant: %s", name, message)
	}
	for _, b := range r.usdl.Balances {
		if b.Role == types.RolePool || b.Role == types.RoleFeeAccumulator || b.Role == types.RoleTreasury {
			require.True(t, r.viewInt(quote, "balanceOf(address)", addressWord(common.HexToAddress(b.Address))).IsZero(),
				"%s %s keeps usdl", b.Role, b.Address)
		}
	}
}

func TestTreasuryLedgerMigrates(t *testing.T) {
	r := migratedV6120(t)
	quote := common.HexToAddress(r.usdl.Address)
	treasuries := []common.Address{r.golden.generation(t, 1).Roles[types.RoleTreasury],
		common.HexToAddress(r.layout.Treasury)}
	tokens := []common.Address{quote}
	for _, pool := range r.golden.pools(t) {
		tokens = append(tokens, r.golden.token(t, pool))
	}
	for _, token := range tokens {
		ledger := sdk.ZeroInt()
		for _, treasury := range treasuries {
			ledger = ledger.Add(r.golden.uint(t, treasury, "getBalance(address)", addressWord(token)))
		}
		got, err := r.k.TreasuryGetBalance(r.ctx, token)
		require.NoError(t, err)
		require.Equal(t, ledger, got, token.Hex())
		held, err := r.k.TreasuryHeld(r.ctx, token)
		require.NoError(t, err)
		require.True(t, held.GTE(got), token.Hex())
	}
	usdlHeld := sdk.ZeroInt()
	for _, treasury := range treasuries {
		usdlHeld = usdlHeld.Add(r.usdlBalance(treasury))
	}
	held, err := r.k.TreasuryHeld(r.ctx, quote)
	require.NoError(t, err)
	require.Equal(t, usdlHeld, held)
	members := 0
	for _, treasury := range treasuries {
		for _, c := range r.golden.calls(t, treasury, "hasRole(bytes32,address)") {
			if !bytes.Equal(c.Input[4:36], keeper.TreasuryDepositorRole[:]) || new(big.Int).SetBytes(c.Output).Sign() != 1 {
				continue
			}
			account := common.BytesToAddress(c.Input[36:68])
			require.True(t, r.k.HasRole(r.ctx, common.HexToAddress(r.layout.Treasury), keeper.TreasuryDepositorRole,
				account), account.Hex())
			members++
		}
	}
	require.Positive(t, members, "golden.json records no DEPOSITOR_ROLE member")
}

func TestGoldenPoolsPresent(t *testing.T) {
	g := loadV6120Golden(t)
	g1, g2 := g.generation(t, 1), g.generation(t, 2)
	require.Len(t, g1.Pools, 45)
	require.Len(t, g2.Pools, 4)
	require.Contains(t, g1.Pools, v6120Generation1Pool)
	require.Contains(t, g2.Pools, v6120Generation2Pool)
	for _, pool := range []common.Address{v6120Generation1Pool, v6120Generation2Pool} {
		c := g.contract(t, pool)
		require.NotEmpty(t, c.Calls, pool.Hex())
		require.Equal(t, crypto.Keccak256Hash(), crypto.Keccak256Hash(), "hashing available")
		g.output(t, pool, "getReserves()")
	}
	r := migratedV6120(t)
	for _, pool := range []common.Address{v6120Generation1Pool, v6120Generation2Pool} {
		market := r.market(pool)
		require.Equal(t, r.golden.token(t, pool).Hex(), market.TokenAddress)
	}
	require.Equal(t, uint64(49), r.k.GetMarketCount(r.ctx))
}

func TestPauseStateMigrates(t *testing.T) {
	r := migratedV6120(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	trader := common.HexToAddress("0x0000000000000000000000000000000000c0ffee")
	for _, pool := range r.golden.pools(t) {
		market := r.market(pool)
		paused := r.golden.flag(t, pool, "paused()")
		require.Equal(t, paused, market.Paused, pool.Hex())
		require.Equal(t, paused, r.golden.flag(t, pool, "emergencyPaused()"), pool.Hex())
		require.Equal(t, r.account(r.golden.address(t, pool, "guardian()")), market.Guardian, pool.Hex())
		if !paused {
			continue
		}
		for _, isBuy := range []bool{true, false} {
			_, _, err := r.k.Swap(r.ctx, r.k.ModuleCaller(), evmtypes.NewDBImpl(r.ctx, evm, false), market.Denom, isBuy,
				big.NewInt(1), big.NewInt(0), trader, trader, uint64(r.ctx.BlockTime().Unix())+3600)
			require.ErrorIs(t, err, types.ErrPaused, pool.Hex())
		}
	}
}

func TestMarketNamesTrimmed(t *testing.T) {
	r := migratedV6120(t)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	stored := func(token common.Address, signature string) string {
		out, err := abi.Arguments{{Type: stringType}}.Unpack(r.view(token, signature))
		require.NoError(t, err)
		return out[0].(string)
	}
	want := []map[string]string{}
	for i := uint64(1); i <= r.k.GetMarketCount(r.ctx); i++ {
		market, ok := r.k.GetMarketByIndex(r.ctx, i)
		require.True(t, ok)
		token := common.HexToAddress(market.TokenAddress)
		for _, f := range []struct{ field, signature, got string }{
			{"name", "name()", market.Name}, {"symbol", "symbol()", market.Symbol},
		} {
			value := stored(token, f.signature)
			require.Equal(t, strings.TrimSpace(value), f.got, "%s %s", market.PoolAddress, f.field)
			if value != f.got {
				want = append(want, map[string]string{keeper.AttributeKeyPool: market.PoolAddress,
					keeper.AttributeKeyField: f.field, keeper.AttributeKeyStored: value, keeper.AttributeKeyTrimmed: f.got})
			}
		}
	}
	require.Contains(t, want, map[string]string{keeper.AttributeKeyPool: "0xf5950B58486Df0550cdDD6f6A1671B769F489814",
		keeper.AttributeKeyField: "name", keeper.AttributeKeyStored: "PAXMAN ", keeper.AttributeKeyTrimmed: "PAXMAN"})
	require.ElementsMatch(t, want, eventsOf(r.ctx, keeper.EventTypeMigrationMarketNameTrimmed))
}
