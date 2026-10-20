package launchpad_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
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
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goldenPath is the halt-height record of every Launch-x contract the
// recorder of modules/launchpad/keeper/testdata/v6120/record writes.
var goldenPath = filepath.Join("..", "..", "modules", "launchpad", "keeper", "testdata", "v6120", "golden.json")

type goldenCall struct {
	Key       string          `json:"key"`
	Signature string          `json:"signature"`
	Selector  hexutil.Bytes   `json:"selector"`
	To        common.Address  `json:"to"`
	From      *common.Address `json:"from,omitempty"`
	Input     hexutil.Bytes   `json:"input"`
	Reverted  bool            `json:"reverted"`
	Output    hexutil.Bytes   `json:"output"`
}

type goldenContract struct {
	Address    common.Address `json:"address"`
	Role       string         `json:"role"`
	Generation int            `json:"generation"`
	Calls      []*goldenCall  `json:"calls"`
}

type goldenFrame struct {
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	Input   hexutil.Bytes  `json:"input"`
	Output  hexutil.Bytes  `json:"output"`
	Error   string         `json:"error"`
	Logs    []goldenLog    `json:"logs"`
	Calls   []goldenFrame  `json:"calls"`
	Revert  string         `json:"revertReason"`
	Type    string         `json:"type"`
	Value   *hexutil.Big   `json:"value"`
	Gas     hexutil.Uint64 `json:"gas"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
}

type goldenLog struct {
	Address common.Address `json:"address"`
	Topics  []common.Hash  `json:"topics"`
	Data    hexutil.Bytes  `json:"data"`
}

type goldenTrace struct {
	Family    string                            `json:"family"`
	Key       string                            `json:"key"`
	Signature string                            `json:"signature"`
	From      common.Address                    `json:"from"`
	To        common.Address                    `json:"to"`
	Input     hexutil.Bytes                     `json:"input"`
	CallTrace goldenFrame                       `json:"callTrace"`
	PreState  map[common.Address]goldenOverride `json:"preState,omitempty"`
}

type goldenRecord struct {
	Height      uint64 `json:"height"`
	Timestamp   uint64 `json:"timestamp"`
	Generations []struct {
		Generation int                       `json:"generation"`
		Roles      map[string]common.Address `json:"roles"`
		Pools      []common.Address          `json:"pools"`
	} `json:"generations"`
	Contracts []*goldenContract `json:"contracts"`
	Refusals  []*goldenCall     `json:"refusals"`
	Traces    []*goldenTrace    `json:"traces"`
}

// loadGolden reads the halt-height record; the replay has nothing to compare
// against without it.
func loadGolden(t *testing.T) goldenRecord {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "the golden record %s is written by go run ./modules/launchpad/keeper/testdata/v6120/record "+
		"-rpc <node> -height <halt height> -trader <account> against a node at the halt height", goldenPath)
	var g goldenRecord
	require.NoError(t, json.Unmarshal(raw, &g), goldenPath)
	require.NotZero(t, g.Height, "golden height")
	require.NotEmpty(t, g.Contracts, "golden contracts")
	return g
}

// haltChainID is the chain the golden record was taken on (EVM chain id 125),
// the chain H+1 runs on.
const haltChainID = "hyperpax_125-1"

// poolsPath is the book-held fixture of requirement 2.38.
var poolsPath = filepath.Join("..", "..", "modules", "launchpad", "keeper", "testdata", "v6120", "pools.json")

// haltExportPath is the genesis export of the chain at the golden height H,
// whose EVM state carries the Launch-x contracts of both generations.
var haltExportPath = filepath.Join("..", "..", "node", "testdata", "v6120-halt-export.json")

var (
	haltOnce    sync.Once
	haltInitial int64
	haltEVM     evmtypes.GenesisState
	haltErr     error
)

// haltState is the initial height and the EVM genesis of the halt-height
// export, read once.
func haltState(t *testing.T) (int64, evmtypes.GenesisState) {
	t.Helper()
	haltOnce.Do(func() {
		raw, err := os.ReadFile(haltExportPath)
		if err != nil {
			haltErr = err
			return
		}
		var export struct {
			InitialHeight json.Number                `json:"initial_height"`
			AppState      map[string]json.RawMessage `json:"app_state"`
		}
		if haltErr = json.Unmarshal(raw, &export); haltErr != nil {
			return
		}
		if haltInitial, haltErr = export.InitialHeight.Int64(); haltErr != nil {
			return
		}
		evmState, ok := export.AppState["evm"]
		if !ok {
			haltErr = fmt.Errorf("%s has no evm state", haltExportPath)
			return
		}
		haltErr = testkeeper.EVMTestApp.AppCodec().UnmarshalAsJSON(evmState, &haltEVM)
	})
	require.NoError(t, haltErr, "halt-height export %s", haltExportPath)
	return haltInitial, haltEVM
}

// migrationEVM is the EVM keeper with the retirement runtime of the
// launchpad precompile, as the v6.12.0 upgrade handler passes it.
type migrationEVM struct{ *evmkeeper.Keeper }

func (m migrationEVM) InstallRetired(ctx sdk.Context, retired, replacement common.Address) error {
	return launchpad.InstallRetired(ctx, m.Keeper, retired, replacement)
}

// goldenOverride is the debug_traceCall state override of one account: the
// storage words that replaced the state at H for one traced call.
type goldenOverride struct {
	StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
}

// migratedHarness is newEnv at the golden halt timestamp and the next height
// with the EVM state of the halt-height export, the pre-state overrides
// written over it, the v6.10 launchpad store and the v6.12.0 launchpad hook
// MigrateV6120 run on it.
func migratedHarness(t *testing.T, g goldenRecord, pre map[common.Address]goldenOverride) *harness {
	t.Helper()
	initial, genesis := haltState(t)
	require.Equal(t, int64(g.Height)+1, initial,
		"halt-height export %s is not the export of golden height %d", haltExportPath, g.Height)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	var storeKey sdk.StoreKey
	h := newEnv(t, haltChainID, time.Unix(int64(g.Timestamp)+1, 0).UTC(), initial, func(ctx sdk.Context, key sdk.StoreKey) {
		storeKey = key
		for _, code := range genesis.Codes {
			evm.SetCode(ctx, common.HexToAddress(code.Address), code.Code)
		}
		for _, state := range genesis.States {
			evm.SetState(ctx, common.HexToAddress(state.Address), common.BytesToHash(state.Key),
				common.BytesToHash(state.Value))
		}
		for _, nonce := range genesis.Nonces {
			evm.SetNonce(ctx, common.HexToAddress(nonce.Address), nonce.Nonce)
		}
		for address, account := range pre {
			for slot, value := range account.StateDiff {
				evm.SetState(ctx, address, slot, value)
			}
		}
	})
	app := testkeeper.EVMTestApp
	legacy := keeperv610.NewKeeper(storeKey, app.BankKeeper,
		tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper), &app.EvmKeeper)
	params := typesv610.DefaultParams()
	params.QuoteDenom = "uhpx"
	require.NoError(t, legacy.UpdateParams(h.ctx(), legacy.Authority(), params))
	pending, err := sdk.ZeroInt().Marshal()
	require.NoError(t, err)
	h.ctx().KVStore(storeKey).Set(types.ProtocolFeesPrefix, pending)
	require.NoError(t, launchpadkeeper.MigrateV6120(h.ctx(), *h.keeper, migrationEVM{evm}))
	h.usdl = common.HexToAddress(h.keeper.GetParams(h.ctx()).QuoteToken)
	return h
}

// directOwner maps each abi.json signature to the role 0x1017 resolves it to:
// the first role of directRoles whose ABI declares it.
func directOwner(t *testing.T) map[string]string {
	t.Helper()
	direct := roleABI(t, directRole)
	owners := map[string]string{}
	for _, role := range directRoles {
		for _, m := range roleABI(t, role).Methods {
			if _, declared := direct.Methods[m.Name]; !declared || direct.Methods[m.Name].Sig != m.Sig {
				continue
			}
			if _, taken := owners[m.Sig]; !taken {
				owners[m.Sig] = role
			}
		}
	}
	return owners
}

// unserved reports whether a call reverted as a selector or role the
// precompile does not serve.
func unserved(t *testing.T, ret []byte, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	if len(ret) == 0 {
		return "empty revert data"
	}
	if reason, unpackErr := abi.UnpackRevert(ret); unpackErr == nil {
		for _, marker := range []string{"serves no handler", "declared by no direct role", "unknown role",
			"does not serve"} {
			if strings.Contains(reason, marker) {
				return reason
			}
		}
	}
	return ""
}

// liveSurface is a harness with one market created through the Router after
// the upgrade, and the address that serves role in it.
func liveSurface(t *testing.T, role string) (*harness, common.Address, common.Address, common.Address) {
	t.Helper()
	h := newHarness(t)
	token, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	h.approve(h.usdl, h.caller, big.NewInt(100_000_000))
	h.do(h.caller, router, types.RoleRouter, "buy", pool, big.NewInt(100_000_000), big.NewInt(0), deadline)
	switch role {
	case types.RolePool:
		return h, pool, token, pool
	case roleERC20:
		return h, token, token, pool
	default:
		at, ok := launchpadkeeper.Generation2Surfaces[role]
		require.True(t, ok, role)
		return h, at, token, pool
	}
}

// requireServed calls every function of role at its surface with zero
// arguments inside a snapshot and requires each to be served: it returns or
// reverts with contract revert data, never as an unknown selector or an
// unserved role. A function 0x1017 resolves to role returns the same bytes and
// the same success when called there directly.
func requireServed(t *testing.T, h *harness, role string, at common.Address) {
	t.Helper()
	owners := directOwner(t)
	methods := roleABI(t, role).Methods
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := methods[name]
		packed, err := m.Inputs.Pack(zeroArgs(m)...)
		require.NoError(t, err, "%s.%s", role, m.Sig)
		input := append(append([]byte(nil), m.ID...), packed...)
		snapshot := h.stateDB.Snapshot()
		ret, err := h.send(h.caller, at, input)
		h.stateDB.RevertToSnapshot(snapshot)
		require.Empty(t, unserved(t, ret, err), "%s.%s at %s is not served", role, m.Sig, at.Hex())
		if owners[m.Sig] != role || at == precompile {
			continue
		}
		snapshot = h.stateDB.Snapshot()
		directRet, directErr := h.send(h.caller, precompile, input)
		h.stateDB.RevertToSnapshot(snapshot)
		require.Equal(t, err == nil, directErr == nil, "%s.%s at 0x1017 and at %s", role, m.Sig, at.Hex())
		require.Equal(t, hexutil.Bytes(ret), hexutil.Bytes(directRet), "%s.%s at 0x1017 and at %s", role, m.Sig,
			at.Hex())
	}
}

// goldenTranslation is how the halt-height record reads at H+1: generation 1
// role addresses and the generation 1 EventEmitter answer for their
// generation 2 replacements in the getters of generation 1 pools (ruling 5),
// EventEmitter events are emitted at 0x1017, every USDL holder the launchpad
// holds for (pools, the FeeAccumulator and Treasury of both generations, the
// module EVM address) is one custody balance, the generation 2 role
// addresses answer with the generation 1 state merged in (requirement 2025),
// the pools of pools.json hold min(book, held) (requirement 2024 ac_4) and
// views read at the H+1 block time.
type goldenTranslation struct {
	quote    common.Address
	moves    map[common.Address]common.Address
	emitters map[common.Address]bool
	tokens   map[common.Address]bool
	internal map[common.Address]bool
	factory  common.Address
	// legacy is the generation 1 answer by role and input.
	legacy map[string]*goldenCall
	// pools is the generation 1 then the generation 2 _allPools array.
	pools []common.Address
	// liabilities is the generation 1 FeeAccumulator liability merged in.
	liabilities *big.Int
	// reserves is the realUsdlBalance and tokenReserve change of each pool
	// of pools.json, and effective its golden getEffectiveReserves words.
	reserves  map[common.Address][2]*big.Int
	effective map[common.Address][2]*big.Int
	// elapsed is the H+1 block time less the golden timestamp.
	elapsed int64
	// snapshots is the golden snapshotIndex and snapshotCount of each pool.
	snapshots map[common.Address][2]uint64
	// nextTokenID is the golden nextTokenId of the generation 2 collection.
	nextTokenID *big.Int
}

func translation(h *harness, g goldenRecord) goldenTranslation {
	tr := goldenTranslation{quote: h.usdl, moves: map[common.Address]common.Address{},
		emitters: map[common.Address]bool{}, tokens: map[common.Address]bool{h.usdl: true},
		internal: map[common.Address]bool{h.custody(): true, h.keeper.TreasuryContract(): true},
		factory:  h.keeper.Generation2().SidioraFactory, legacy: map[string]*goldenCall{},
		liabilities: new(big.Int), reserves: map[common.Address][2]*big.Int{},
		effective: map[common.Address][2]*big.Int{}, elapsed: h.ctx().BlockTime().Unix() - int64(g.Timestamp),
		snapshots: map[common.Address][2]uint64{}, nextTokenID: new(big.Int)}
	roles := map[int]map[string]common.Address{}
	pools := map[int][]common.Address{}
	for _, gen := range g.Generations {
		roles[gen.Generation] = gen.Roles
		pools[gen.Generation] = gen.Pools
		for _, pool := range gen.Pools {
			tr.internal[pool] = true
		}
		for _, role := range []string{types.RoleFeeAccumulator, types.RoleTreasury} {
			if a := gen.Roles[role]; a != (common.Address{}) {
				tr.internal[a] = true
			}
		}
	}
	tr.pools = append(append(tr.pools, pools[1]...), pools[2]...)
	for role, from := range roles[1] {
		if to := roles[2][role]; from != (common.Address{}) && to != (common.Address{}) && from != to {
			tr.moves[from] = to
		}
	}
	emitter := h.keeper.Generation2().EventEmitter
	tr.emitters[emitter] = true
	for _, c := range g.Contracts {
		switch c.Role {
		case roleERC20:
			tr.tokens[c.Address] = true
		case types.RolePool:
			for _, call := range c.Calls {
				if call.Reverted {
					continue
				}
				switch {
				case call.Signature == "eventEmitter()" && len(call.Output) == 32:
					at := common.BytesToAddress(call.Output)
					tr.emitters[at] = true
					if c.Generation == 1 && at != emitter {
						tr.moves[at] = emitter
					}
				case call.Signature == "getEffectiveReserves()" && len(call.Output) == 64:
					tr.effective[c.Address] = [2]*big.Int{new(big.Int).SetBytes(call.Output[:32]),
						new(big.Int).SetBytes(call.Output[32:])}
				case call.Signature == "snapshotIndex()" && len(call.Output) == 32:
					r := tr.snapshots[c.Address]
					r[0] = new(big.Int).SetBytes(call.Output).Uint64()
					tr.snapshots[c.Address] = r
				case call.Signature == "snapshotCount()" && len(call.Output) == 32:
					r := tr.snapshots[c.Address]
					r[1] = new(big.Int).SetBytes(call.Output).Uint64()
					tr.snapshots[c.Address] = r
				}
			}
		default:
			if c.Generation != 1 {
				if c.Role == types.RoleFeeRights {
					for _, call := range c.Calls {
						if call.Signature == "nextTokenId()" && !call.Reverted {
							tr.nextTokenID.SetBytes(call.Output)
						}
					}
				}
				continue
			}
			for _, call := range c.Calls {
				tr.legacy[c.Role+"/"+call.Input.String()] = call
				if c.Role == types.RoleFeeAccumulator && !call.Reverted &&
					(call.Signature == "getAccumulatedFees(address)" || call.Signature == "getAirdropBalance(address)" ||
						call.Signature == "getOpticalSurplus(address)") {
					tr.liabilities.Add(tr.liabilities, new(big.Int).SetBytes(call.Output))
				}
			}
		}
	}
	raw, err := os.ReadFile(poolsPath)
	require.NoError(h.t, err, poolsPath)
	var fixture struct {
		Deltas []struct {
			Pool  common.Address `json:"pool"`
			Asset string         `json:"asset"`
			Book  string         `json:"book"`
			Held  string         `json:"held"`
		} `json:"deltas"`
	}
	require.NoError(h.t, json.Unmarshal(raw, &fixture), poolsPath)
	for _, d := range fixture.Deltas {
		book, ok := new(big.Int).SetString(d.Book, 10)
		require.True(h.t, ok, "%s book %s", poolsPath, d.Book)
		held, ok := new(big.Int).SetString(d.Held, 10)
		require.True(h.t, ok, "%s held %s", poolsPath, d.Held)
		change := new(big.Int)
		if held.Cmp(book) < 0 {
			change.Sub(held, book)
		}
		r, found := tr.reserves[d.Pool]
		if !found {
			r = [2]*big.Int{new(big.Int), new(big.Int)}
		}
		if d.Asset == "usdl" {
			r[0].Add(r[0], change)
		} else {
			r[1].Add(r[1], change)
		}
		tr.reserves[d.Pool] = r
	}
	return tr
}

// shifted is out with its word i increased by delta.
func shifted(out []byte, i int, delta *big.Int) []byte {
	moved := append([]byte(nil), out...)
	word := new(big.Int).Add(new(big.Int).SetBytes(moved[32*i:32*i+32]), delta)
	copy(moved[32*i:32*i+32], common.LeftPadBytes(word.Bytes(), 32))
	return moved
}

func bigWord(v *big.Int) []byte { return common.LeftPadBytes(v.Bytes(), 32) }

// reserved is the answer of a pool getter at a pool of pools.json with
// realUsdlBalance and tokenReserve set to min(book, held) (requirement 2024
// ac_4) and the price taken from the effective reserves.
func (tr goldenTranslation) reserved(pool common.Address, signature string, out []byte) []byte {
	r, ok := tr.reserves[pool]
	if !ok {
		return out
	}
	switch signature {
	case "realUsdlBalance()":
		return shifted(out, 0, r[0])
	case "tokenReserve()":
		return shifted(out, 0, r[1])
	case "getReserves()":
		return shifted(shifted(out, 1, r[0]), 2, r[1])
	case "getEffectiveReserves()":
		return shifted(shifted(out, 0, r[0]), 1, r[1])
	case "getPoolInfo()":
		return shifted(shifted(out, 3, r[0]), 4, r[1])
	case "getPrice()":
		e := tr.effective[pool]
		quote := new(big.Int).Add(e[0], r[0])
		token := new(big.Int).Add(e[1], r[1])
		return bigWord(quote.Div(quote.Mul(quote, big.NewInt(1e18)), token))
	}
	return out
}

// merged is the answer of a generation 2 role address with the generation 1
// state MigrateV6120 merges into it (requirements 2024 and 2025).
func (tr goldenTranslation) merged(role, signature string, input, out []byte) []byte {
	legacy := tr.legacy[role+"/"+hexutil.Bytes(input).String()]
	answered := legacy != nil && !legacy.Reverted
	pools := big.NewInt(int64(len(tr.pools)))
	switch {
	case signature == "hasRole(bytes32,address)":
		// Memberships other than DEFAULT_ADMIN_ROLE merge (2025 ac_2).
		if answered && new(big.Int).SetBytes(input[4:36]).Sign() != 0 &&
			new(big.Int).SetBytes(legacy.Output).Sign() != 0 {
			return amountWord(1)
		}
	case role == types.RoleFeeAccumulator && signature == "getAccumulatedFees(address)" && answered:
		return shifted(out, 0, new(big.Int).SetBytes(legacy.Output))
	case role == types.RoleFeeAccumulator && signature == "totalLiabilities()":
		return shifted(out, 0, tr.liabilities)
	case role == types.RoleFeeRights && (signature == "nextTokenId()" || signature == "totalSupply()"):
		// Generation 1 fee rights are minted again at nextTokenId (2025 ac_1).
		supply := tr.legacy[role+"/"+hexutil.Bytes(crypto.Keccak256([]byte("totalSupply()"))[:4]).String()]
		if supply != nil && !supply.Reverted {
			return shifted(out, 0, new(big.Int).SetBytes(supply.Output))
		}
	case role == types.RoleFeeRights && signature == "poolIndexMigrationCursor()":
		// The cursor is rebuilt over the merged ids (2025 ac_1): it stands at
		// the merged nextTokenId, past every generation 1 id minted again.
		supply := tr.legacy[role+"/"+hexutil.Bytes(crypto.Keccak256([]byte("totalSupply()"))[:4]).String()]
		if supply != nil && !supply.Reverted {
			return bigWord(new(big.Int).Add(tr.nextTokenID, new(big.Int).SetBytes(supply.Output)))
		}
	case role == types.RolePoolRegistry && signature == "getPoolCount()":
		return bigWord(pools)
	case (role == types.RolePoolRegistry || role == types.RoleQuoter) && signature == "getAllPools(uint256,uint256)":
		offset := new(big.Int).SetBytes(input[4:36])
		end := new(big.Int).Add(offset, new(big.Int).SetBytes(input[36:68]))
		if end.Cmp(pools) > 0 {
			end = pools
		}
		var page []common.Address
		if offset.Cmp(end) < 0 {
			page = tr.pools[offset.Int64():end.Int64()]
		}
		encoded := append(amountWord(32), amountWord(int64(len(page)))...)
		for _, pool := range page {
			encoded = append(encoded, addressWord(pool)...)
		}
		return encoded
	case role == types.RoleQuoter && signature == "getPoolStats(address)" && len(out) == 256:
		// poolAge is block.timestamp less the creation time (Quoter.sol:100).
		return shifted(out, 5, big.NewInt(tr.elapsed))
	}
	return out
}

// movedWords is out with every 32-byte word holding a moved address
// replaced by its generation 2 address.
func (tr goldenTranslation) movedWords(out []byte) []byte {
	moved := append([]byte(nil), out...)
	for i := 0; i+32 <= len(moved); i += 32 {
		word := moved[i : i+32]
		if new(big.Int).Rsh(new(big.Int).SetBytes(word), 160).Sign() != 0 {
			continue
		}
		if to, ok := tr.moves[common.BytesToAddress(word)]; ok {
			copy(word, common.LeftPadBytes(to.Bytes(), 32))
		}
	}
	return moved
}

// expected is the answer at H+1 to a call with signature and input the
// golden record answered at H with reverted and out at to, a contract of role
// and generation: a retired generation 1 address reverts
// Retired(replacement), every served role address counts no default admin
// (requirement 2025 ac_2), a generation 1 pool answers with the generation 2
// role addresses and factory (ruling 5), a pool of pools.json with the
// reserves of requirement 2024 ac_4, a generation 2 role address with the
// generation 1 state merged in, and every other contract as it did.
func (h *harness) expected(tr goldenTranslation, to common.Address, role string, generation int, reverted bool,
	signature string, input, out []byte) (retired, wantReverted bool, want hexutil.Bytes) {
	h.t.Helper()
	entry, found, err := h.keeper.GetRetired(h.ctx(), to)
	require.NoError(h.t, err)
	if found {
		return true, true, errorData("Retired(address)",
			common.LeftPadBytes(common.HexToAddress(entry.Replacement).Bytes(), 32))
	}
	switch {
	case signature == "registeredDefaultAdminCount()":
		return false, false, make(hexutil.Bytes, 32)
	case role == types.RolePool:
		if generation == 1 {
			if signature == "factory()" {
				return false, false, common.LeftPadBytes(tr.factory.Bytes(), 32)
			}
			if reverted && len(out) == 0 {
				if served, ok := h.generation1Served(to, signature); ok {
					return false, false, served
				}
			}
			out = tr.movedWords(out)
		}
		if !reverted && signature == "getPriceSnapshots()" && len(out) == 32*types.SnapshotSlots {
			out = tr.ordered(to, out)
		}
		if !reverted {
			out = tr.reserved(to, signature, out)
		}
	case role != roleERC20 && generation == 2 && !reverted:
		out = tr.merged(role, signature, input, out)
	}
	return false, reverted, out
}

// generation1Served is the answer of a generation 1 pool to a SidioraPool
// getter its contract did not declare, which every pool serves (requirement
// 2003): the contract constants, and the migrated launchpad keeper state for
// the state getters.
func (h *harness) generation1Served(pool common.Address, signature string) (hexutil.Bytes, bool) {
	h.t.Helper()
	switch signature {
	case "PAUSER_ROLE()":
		return crypto.Keccak256([]byte("PAUSER_ROLE")), true
	case "SNAPSHOT_INTERVAL()":
		return amountWord(60), true
	case "SNAPSHOT_MIN_VOLUME_BPS()":
		return amountWord(1), true
	}
	m := h.market(pool)
	switch signature {
	case "cumulativeBuyUsdlVolume()":
		return bigWord(m.CumulativeBuyQuoteVolume.BigInt()), true
	case "cumulativeSellUsdlVolume()":
		return bigWord(m.CumulativeSellQuoteVolume.BigInt()), true
	case "lastSnapshotTimestamp()":
		return amountWord(m.LastSnapshotTimestamp), true
	case "emergencyPaused()":
		if m.Paused {
			return amountWord(1), true
		}
		return amountWord(0), true
	case "pendingGuardian()":
		if m.PendingGuardian == "" {
			return addressWord(common.Address{}), true
		}
		return addressWord(testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(h.ctx(),
			sdk.MustAccAddressFromBech32(m.PendingGuardian))), true
	case "currentFeeBps()":
		fee, err := h.keeper.FeeBps(h.ctx(), m)
		require.NoError(h.t, err, pool.Hex())
		return bigWord(fee), true
	}
	return nil, false
}

// ordered is the golden raw snapshot ring of pool as getPriceSnapshots
// returns it (requirement 2003 ac_19, SidioraPool._orderedSnapshots): the
// snapshotCount prices oldest first, from snapshotIndex once the ring is
// full, zero after.
func (tr goldenTranslation) ordered(pool common.Address, ring []byte) []byte {
	r := tr.snapshots[pool]
	var start uint64
	if r[1] == types.SnapshotSlots {
		start = r[0]
	}
	out := make([]byte, len(ring))
	for i := uint64(0); i < types.SnapshotSlots && i < r[1]; i++ {
		j := (start + i) % types.SnapshotSlots
		copy(out[32*i:32*i+32], ring[32*j:32*j+32])
	}
	return out
}

// unpaidSwap reports whether trace is a generation 1 pool swap whose
// transaction paid no input into the pool: the swap reverts
// InsufficientInput() there (requirement 2003 ac_34, the pay-first check of
// SidioraPool.sol:147).
func (tr goldenTranslation) unpaidSwap(generation int, trace *goldenTrace) bool {
	if generation != 1 || trace.Signature != "swap(uint256,uint256,bool,address,uint256)" {
		return false
	}
	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	var logs []goldenLog
	collectLogs(trace.CallTrace, &logs)
	for _, log := range logs {
		if tr.tokens[log.Address] && len(log.Topics) == 3 && log.Topics[0] == transfer &&
			common.BytesToAddress(log.Topics[2].Bytes()) == trace.To {
			return false
		}
	}
	return true
}

// balanceChanges are the net ERC20 balance changes the Transfer logs of the
// USDL and pool-token contracts make, keyed token/holder, with the USDL the
// launchpad holds for counted as one custody holder.
func (tr goldenTranslation) balanceChanges(logs []goldenLog) map[string]string {
	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	deltas := map[string]*big.Int{}
	add := func(token, holder common.Address, amount *big.Int) {
		key := token.Hex() + "/" + holder.Hex()
		if token == tr.quote && tr.internal[holder] {
			key = token.Hex() + "/launchpad"
		}
		if deltas[key] == nil {
			deltas[key] = new(big.Int)
		}
		deltas[key].Add(deltas[key], amount)
	}
	for _, log := range logs {
		if !tr.tokens[log.Address] || len(log.Topics) != 3 || log.Topics[0] != transfer {
			continue
		}
		amount := new(big.Int).SetBytes(log.Data)
		add(log.Address, common.BytesToAddress(log.Topics[1].Bytes()), new(big.Int).Neg(amount))
		add(log.Address, common.BytesToAddress(log.Topics[2].Bytes()), amount)
	}
	out := map[string]string{}
	for key, delta := range deltas {
		if delta.Sign() != 0 {
			out[key] = delta.String()
		}
	}
	return out
}

// events are the logs other than the ERC20 Transfer and Approval logs of the
// token contracts, whose effect balanceChanges compares, as address and
// topics, sorted; golden logs are read at their H+1 emitter with moved topic
// words.
func (tr goldenTranslation) events(logs []goldenLog, golden bool) []string {
	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	approval := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	out := []string{}
	for _, log := range logs {
		if len(log.Topics) > 0 && tr.tokens[log.Address] && (log.Topics[0] == transfer || log.Topics[0] == approval) {
			continue
		}
		address, topics := log.Address, log.Topics
		if golden {
			if to, ok := tr.moves[address]; ok {
				address = to
			}
			if tr.emitters[address] || tr.emitters[log.Address] {
				address = precompile
			}
			topics = make([]common.Hash, len(log.Topics))
			for i, topic := range log.Topics {
				topics[i] = common.BytesToHash(tr.movedWords(topic.Bytes()))
			}
		}
		entry := address.Hex()
		for _, topic := range topics {
			entry += " " + topic.Hex()
		}
		out = append(out, entry)
	}
	sort.Strings(out)
	return out
}

// replayGolden replays every halt-height call and trace the golden record
// holds for role on the migrated harness. Calls give the same success and
// output bytes; traces the same success, output bytes, USDL and pool-token
// balance changes and logs, a trace with a pre-state override running on a
// harness migrated from the overridden halt state, and a Router or Factory
// trace pulling with the allowance its caller held for that role at H
// granted to 0x1017 (requirement 2.18).
func replayGolden(t *testing.T, role string) {
	t.Helper()
	g := loadGolden(t)
	h := migratedHarness(t, g, nil)
	tr := translation(h, g)
	generations := map[common.Address]int{}
	for _, c := range g.Contracts {
		generations[c.Address] = c.Generation
	}
	replayed := 0
	for _, c := range g.Contracts {
		if c.Role != role {
			continue
		}
		for _, call := range c.Calls {
			var from common.Address
			if call.From != nil {
				from = *call.From
			}
			_, reverted, want := h.expected(tr, call.To, role, c.Generation, call.Reverted, call.Signature, call.Input,
				call.Output)
			snapshot := h.stateDB.Snapshot()
			ret, err := h.send(from, call.To, call.Input)
			h.stateDB.RevertToSnapshot(snapshot)
			assert.Equal(t, reverted, err != nil, "%s %s at %s", role, call.Key, call.To.Hex())
			assert.Equal(t, want.String(), hexutil.Bytes(ret).String(), "%s %s at %s", role, call.Key, call.To.Hex())
			replayed++
		}
	}
	for _, trace := range g.Traces {
		if !strings.HasPrefix(trace.Key, role+".") {
			continue
		}
		th, ttr := h, tr
		if len(trace.PreState) > 0 {
			th = migratedHarness(t, g, trace.PreState)
			ttr = translation(th, g)
		}
		generation, ok := generations[trace.To]
		if !ok {
			generation = 2
		}
		th.replayTrace(ttr, role, generation, trace)
		replayed++
	}
	require.NotZero(t, replayed, "the golden record holds no call of %s", role)
}

func (h *harness) replayTrace(tr goldenTranslation, role string, generation int, trace *goldenTrace) {
	t := h.t
	t.Helper()
	retired, reverted, want := h.expected(tr, trace.To, role, generation, trace.CallTrace.Error != "",
		trace.Signature, trace.Input, trace.CallTrace.Output)
	unpaid := tr.unpaidSwap(generation, trace)
	if unpaid {
		reverted, want = true, errorData("InsufficientInput()")
	}
	snapshot := h.stateDB.Snapshot()
	defer h.stateDB.RevertToSnapshot(snapshot)
	surface, found, err := h.keeper.GetSurface(h.ctx(), trace.To)
	require.NoError(t, err)
	if found && (surface.Role == types.RoleRouter || surface.Role == types.RoleFactory) {
		tokens := make([]common.Address, 0, len(tr.tokens))
		for token := range tr.tokens {
			tokens = append(tokens, token)
		}
		sort.Slice(tokens, func(i, j int) bool { return bytes.Compare(tokens[i][:], tokens[j][:]) < 0 })
		for _, token := range tokens {
			allowance := h.view(token, roleERC20, "allowance", trace.From, trace.To)[0].(*big.Int)
			if allowance.Sign() > 0 {
				h.approve(token, trace.From, allowance)
			}
		}
	}
	start := h.logCount()
	ret, err := h.send(trace.From, trace.To, trace.Input)
	assert.Equal(t, reverted, err != nil, "%s trace %s", role, trace.Key)
	assert.Equal(t, want.String(), hexutil.Bytes(ret).String(), "%s trace %s", role, trace.Key)
	var golden, got []goldenLog
	if !retired && !unpaid {
		collectLogs(trace.CallTrace, &golden)
	}
	for _, log := range h.stateDB.GetAllLogs()[start:] {
		got = append(got, goldenLog{Address: log.Address, Topics: log.Topics, Data: log.Data})
	}
	assert.Equal(t, tr.balanceChanges(golden), tr.balanceChanges(got), "%s trace %s balance changes", role, trace.Key)
	assert.Equal(t, tr.events(golden, true), tr.events(got, false), "%s trace %s logs", role, trace.Key)
}

// collectLogs appends the logs of frame and of its calls that did not fail.
func collectLogs(frame goldenFrame, out *[]goldenLog) {
	if frame.Error != "" {
		return
	}
	*out = append(*out, frame.Logs...)
	for _, sub := range frame.Calls {
		collectLogs(sub, out)
	}
}

// view32 is the single 32-byte word role.name returns at at.
func view32(t *testing.T, h *harness, at common.Address, role, name string, args ...interface{}) interface{} {
	t.Helper()
	return h.view(at, role, name, args...)[0]
}

func TestPoolSurfaceParity(t *testing.T) {
	h, pool, token, _ := liveSurface(t, types.RolePool)
	requireServed(t, h, types.RolePool, pool)
	market := h.market(pool)
	require.Equal(t, token, view32(t, h, pool, types.RolePool, "tokenAddress"))
	same(t, []interface{}{market.VirtualQuoteReserve.BigInt(), market.RealQuoteBalance.BigInt(),
		market.TokenReserve.BigInt()}, h.view(pool, types.RolePool, "getReserves"))
	replayGolden(t, types.RolePool)
}

func TestSidioraPoolSurfaceParity(t *testing.T) {
	h, pool, _, _ := liveSurface(t, types.RolePool)
	requireServed(t, h, types.RolePool, pool)
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("PAUSER_ROLE"))),
		view32(t, h, pool, types.RolePool, "PAUSER_ROLE"))
	require.Equal(t, big.NewInt(60), view32(t, h, pool, types.RolePool, "SNAPSHOT_INTERVAL"))
	require.Equal(t, big.NewInt(1), view32(t, h, pool, types.RolePool, "SNAPSHOT_MIN_VOLUME_BPS"))
	require.Equal(t, factory, view32(t, h, pool, types.RolePool, "factory"))
	require.Equal(t, feeAccumulator, view32(t, h, pool, types.RolePool, "feeAccumulator"))
	require.Equal(t, protocolConfig, view32(t, h, pool, types.RolePool, "protocolConfig"))
	// A new pool is initialized with the factory's eventEmitter, the
	// generation 2 EventEmitter (Launch-x:contracts/contracts/core/SidioraFactory.sol:127-139,
	// contracts/launchpad.md req 2.24.5).
	require.Equal(t, h.keeper.Generation2().EventEmitter, view32(t, h, pool, types.RolePool, "eventEmitter"))
	require.Equal(t, errorData("Unauthorized()"), h.fails(h.account(), pool, types.RolePool, "acceptGuardian"))
	replayGolden(t, types.RolePool)
}

func TestSidioraFactorySurfaceParity(t *testing.T) {
	h, at, token, pool := liveSurface(t, types.RoleFactory)
	requireServed(t, h, types.RoleFactory, at)
	require.Equal(t, big.NewInt(1), view32(t, h, at, types.RoleFactory, "getNonce", h.caller))
	predicted, err := h.keeper.PredictPoolAddress(h.ctx(), token, h.caller, common.Address{})
	require.NoError(t, err)
	require.Equal(t, pool, predicted)
	require.Equal(t, pool, view32(t, h, at, types.RoleFactory, "predictPoolAddress", token, h.caller,
		common.Address{}))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("ROUTER_ROLE"))),
		view32(t, h, at, types.RoleFactory, "ROUTER_ROLE"))
	// Role membership answers from the migrated role table (requirement
	// 2.13); a fresh chain migrated none, so the Router holds no ROUTER_ROLE
	// here and the halt-height grant is checked by the golden replay.
	require.Equal(t, false, view32(t, h, at, types.RoleFactory, "hasRole",
		[32]byte(crypto.Keccak256Hash([]byte("ROUTER_ROLE"))), router))
	replayGolden(t, types.RoleFactory)
}

func TestRouterSurfaceParity(t *testing.T) {
	h, at, token, pool := liveSurface(t, types.RoleRouter)
	requireServed(t, h, types.RoleRouter, at)
	in := big.NewInt(1_000_000)
	quoted, _ := quoteResult(t, view32(t, h, quoter, types.RoleQuoter, "quoteExactInput", pool, in, true))
	h.approve(h.usdl, h.caller, in)
	before := h.balanceOf(token, h.caller)
	require.Equal(t, quoted, h.do(h.caller, at, types.RoleRouter, "buy", pool, in, quoted, deadline)[0])
	require.Equal(t, new(big.Int).Add(before, quoted), h.balanceOf(token, h.caller))
	replayGolden(t, types.RoleRouter)
}

func TestQuoterSurfaceParity(t *testing.T) {
	h, at, _, pool := liveSurface(t, types.RoleQuoter)
	requireServed(t, h, types.RoleQuoter, at)
	require.Equal(t, []common.Address{pool}, view32(t, h, at, types.RoleQuoter, "getAllPools", big.NewInt(0),
		big.NewInt(10)))
	require.Empty(t, view32(t, h, at, types.RoleQuoter, "getAllPools", big.NewInt(1), big.NewInt(10)))
	replayGolden(t, types.RoleQuoter)
}

func TestFeesRouterSurfaceParity(t *testing.T) {
	h, at, _, pool := liveSurface(t, types.RoleFeesRouter)
	requireServed(t, h, types.RoleFeesRouter, at)
	id := new(big.Int).SetUint64(h.market(pool).FeeRightID)
	require.NotEmpty(t, h.fails(h.account(), at, types.RoleFeesRouter, "claimFees", id))
	replayGolden(t, types.RoleFeesRouter)
}

func TestFeeAccumulatorSurfaceParity(t *testing.T) {
	h, at, _, pool := liveSurface(t, types.RoleFeeAccumulator)
	requireServed(t, h, types.RoleFeeAccumulator, at)
	require.Equal(t, common.HexToAddress("0x000000000000000000000000000000000000dEaD"),
		view32(t, h, at, types.RoleFeeAccumulator, "DEAD_ADDRESS"))
	require.Equal(t, true, view32(t, h, at, types.RoleFeeAccumulator, "hasRole", [32]byte(poolRole), pool))
	replayGolden(t, types.RoleFeeAccumulator)
}

func TestPoolRegistrySurfaceParity(t *testing.T) {
	h, at, token, pool := liveSurface(t, types.RolePoolRegistry)
	requireServed(t, h, types.RolePoolRegistry, at)
	require.Equal(t, big.NewInt(1), view32(t, h, at, types.RolePoolRegistry, "getPoolCount"))
	require.Equal(t, true, view32(t, h, at, types.RolePoolRegistry, "isRegisteredPool", pool))
	require.Equal(t, pool, view32(t, h, at, types.RolePoolRegistry, "getPoolByToken", token))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("FACTORY_ROLE"))),
		view32(t, h, at, types.RolePoolRegistry, "FACTORY_ROLE"))
	replayGolden(t, types.RolePoolRegistry)
}

func TestProtocolConfigSurfaceParity(t *testing.T) {
	h, at, _, _ := liveSurface(t, types.RoleProtocolConfig)
	requireServed(t, h, types.RoleProtocolConfig, at)
	params := h.keeper.GetParams(h.ctx())
	require.Equal(t, new(big.Int).SetUint64(params.BaseFeeBps), view32(t, h, at, types.RoleProtocolConfig,
		"baseFeeBps"))
	require.Equal(t, new(big.Int).SetUint64(params.MaxFeeBps), view32(t, h, at, types.RoleProtocolConfig,
		"maxFeeBps"))
	require.Equal(t, errorData("AccessControlUnauthorizedAccount(address,bytes32)", common.LeftPadBytes(
		h.caller.Bytes(), 32), make([]byte, 32)), h.fails(h.caller, at, types.RoleProtocolConfig, "setBaseFeeBps",
		big.NewInt(31)))
	replayGolden(t, types.RoleProtocolConfig)
}

func TestSidioraNFTSurfaceParity(t *testing.T) {
	h, at, _, pool := liveSurface(t, types.RoleFeeRights)
	requireServed(t, h, types.RoleFeeRights, at)
	id := h.market(pool).FeeRightID
	require.Equal(t, h.caller, view32(t, h, at, types.RoleFeeRights, "ownerOf", new(big.Int).SetUint64(id)))
	require.Equal(t, pool, view32(t, h, at, types.RoleFeeRights, "getPoolAddress", new(big.Int).SetUint64(id)))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("MINTER_ROLE"))),
		view32(t, h, at, types.RoleFeeRights, "MINTER_ROLE"))
	replayGolden(t, types.RoleFeeRights)
}

func TestSidioraERC20SurfaceParity(t *testing.T) {
	h, token, _, pool := liveSurface(t, roleERC20)
	require.NotEqual(t, surfaceRuntime, h.stateDB.GetCode(token), "the pool token keeps its contract code")
	requireServed(t, h, roleERC20, token)
	require.Equal(t, factory, view32(t, h, token, roleERC20, "deploymentFactory"))
	require.Equal(t, pool, view32(t, h, token, roleERC20, "pool"))
	require.Equal(t, uint8(6), view32(t, h, token, roleERC20, "decimals"))
	require.Len(t, view32(t, h, token, roleERC20, "DOMAIN_SEPARATOR"), 32)
	replayGolden(t, roleERC20)
}

// accessControlRoles are the role contracts that inherited AccessControl and
// UUPS upgrades.
var accessControlRoles = []string{types.RoleFactory, types.RoleRouter, types.RoleQuoter, types.RoleFeesRouter,
	types.RoleFeeAccumulator, types.RolePoolRegistry, types.RoleProtocolConfig, types.RoleFeeRights,
	roleTreasury}

func TestAccessControlAndUpgradeSurface(t *testing.T) {
	h, _, _, pool := liveSurface(t, types.RoleRouter)
	zero := [32]byte{}
	stranger := h.account()
	unauthorized := func(account common.Address) []byte {
		return errorData("AccessControlUnauthorizedAccount(address,bytes32)", common.LeftPadBytes(account.Bytes(), 32),
			make([]byte, 32))
	}
	for _, role := range accessControlRoles {
		at := launchpadkeeper.Generation2Surfaces[role]
		require.NotEqual(t, common.Address{}, at, role)
		require.Equal(t, zero, view32(t, h, at, role, "DEFAULT_ADMIN_ROLE"), role)
		require.Equal(t, zero, view32(t, h, at, role, "getRoleAdmin", zero), role)
		require.Equal(t, false, view32(t, h, at, role, "hasRole", zero, h.caller), role)
		if _, ok := roleABI(t, role).Methods["registeredDefaultAdminCount"]; ok {
			same(t, big.NewInt(0), view32(t, h, at, role, "registeredDefaultAdminCount"), role)
		}
		require.Equal(t, unauthorized(stranger), h.fails(stranger, at, role, "grantRole", zero, stranger), role)
		require.Equal(t, unauthorized(stranger), h.fails(stranger, at, role, "revokeRole", zero, h.caller), role)
		require.Equal(t, unauthorized(stranger), h.fails(stranger, at, role, "upgradeToAndCall", stranger, []byte{}),
			role)
		require.Equal(t, errorData("MissingRole(address,bytes32)", common.LeftPadBytes(stranger.Bytes(), 32),
			poolRole.Bytes()), h.fails(stranger, at, role, "renounceRole", [32]byte(poolRole), h.caller), role)
		require.Equal(t, errorData("UUPSNotThroughActiveProxy()"), h.fails(stranger, at, role, "proxiableUUID"),
			role)
	}

	for _, name := range []string{"grantRole", "revokeRole"} {
		ret, err := h.direct(stranger, pack(t, directRole, name, zero, stranger), nil, false, false)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
		require.Equal(t, unauthorized(stranger), ret, name)
	}
	upgrade := pack(t, directRole, "upgradeToAndCall", stranger, []byte{})
	ret, err := h.direct(stranger, upgrade, nil, false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, unauthorized(stranger), ret)
	ret, err = h.direct(stranger, upgrade, big.NewInt(1), false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.NotEqual(t, unauthorized(stranger), ret, "a value call reverts on the value check first")
	ret, err = h.direct(stranger, pack(t, directRole, "proxiableUUID"), nil, false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, errorData("UUPSNotThroughActiveProxy()"), ret)

	require.Equal(t, true, view32(t, h, feeAccumulator, types.RoleFeeAccumulator, "hasRole", [32]byte(poolRole), pool))
	start := h.logCount()
	h.do(pool, feeAccumulator, types.RoleFeeAccumulator, "renounceRole", [32]byte(poolRole), pool)
	require.Equal(t, false, view32(t, h, feeAccumulator, types.RoleFeeAccumulator, "hasRole", [32]byte(poolRole), pool))
	revoked := h.logs(start, feeAccumulator, "RoleRevoked(bytes32,address,address)")
	require.Len(t, revoked, 1)
	require.Equal(t, []common.Hash{revoked[0].Topics[0], poolRole, addressTopic(pool), addressTopic(pool)},
		revoked[0].Topics)
	replayGolden(t, types.RoleFeeAccumulator)
}
