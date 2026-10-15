package launchpad_test

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
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
	Family    string         `json:"family"`
	Key       string         `json:"key"`
	Signature string         `json:"signature"`
	From      common.Address `json:"from"`
	To        common.Address `json:"to"`
	Input     hexutil.Bytes  `json:"input"`
	CallTrace goldenFrame    `json:"callTrace"`
	PreState  map[common.Address]struct {
		StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
	} `json:"preState,omitempty"`
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

// migratedHarness is newEnv at the golden halt timestamp and the next height
// with the halt-height state migrated by the v6.12.0 launchpad hook.
func migratedHarness(t *testing.T, g goldenRecord) *harness {
	t.Helper()
	require.FailNow(t, "the golden replay runs on the state of launchpadkeeper.MigrateV6120(ctx, k, evm), "+
		"which modules/launchpad/keeper does not declare yet", "golden height %d", g.Height)
	return nil
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

// replayGolden replays every halt-height call and trace the golden record
// holds for role on the migrated harness and requires the same success and
// output bytes, and for traces the same quote and pool-token Transfer logs.
func replayGolden(t *testing.T, role string) {
	t.Helper()
	g := loadGolden(t)
	h := migratedHarness(t, g)
	replayed := 0
	for _, c := range g.Contracts {
		if c.Role != role {
			continue
		}
		for _, call := range c.Calls {
			from := h.caller
			if call.From != nil {
				from = *call.From
			}
			snapshot := h.stateDB.Snapshot()
			ret, err := h.send(from, call.To, call.Input)
			h.stateDB.RevertToSnapshot(snapshot)
			require.Equal(t, call.Reverted, err != nil, "%s %s at %s", role, call.Key, call.To.Hex())
			require.Equal(t, call.Output, hexutil.Bytes(ret), "%s %s at %s", role, call.Key, call.To.Hex())
			replayed++
		}
	}
	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	for _, trace := range g.Traces {
		if !strings.HasPrefix(trace.Key, role) {
			continue
		}
		snapshot := h.stateDB.Snapshot()
		for address, account := range trace.PreState {
			for slot, value := range account.StateDiff {
				h.stateDB.SetState(address, slot, value)
			}
		}
		start := h.logCount()
		ret, err := h.send(trace.From, trace.To, trace.Input)
		require.Equal(t, trace.CallTrace.Error != "", err != nil, "%s trace %s", role, trace.Key)
		require.Equal(t, trace.CallTrace.Output, hexutil.Bytes(ret), "%s trace %s", role, trace.Key)
		var want, got []goldenLog
		collectTransfers(trace.CallTrace, transfer, &want)
		for _, log := range h.stateDB.GetAllLogs()[start:] {
			if len(log.Topics) > 0 && log.Topics[0] == transfer {
				got = append(got, goldenLog{Address: log.Address, Topics: log.Topics, Data: log.Data})
			}
		}
		h.stateDB.RevertToSnapshot(snapshot)
		same(t, want, got, "%s trace %s Transfer logs", role, trace.Key)
		replayed++
	}
	require.NotZero(t, replayed, "the golden record holds no call of %s", role)
}

func collectTransfers(frame goldenFrame, topic common.Hash, out *[]goldenLog) {
	if frame.Error != "" {
		return
	}
	for _, log := range frame.Logs {
		if len(log.Topics) > 0 && log.Topics[0] == topic {
			*out = append(*out, log)
		}
	}
	for _, sub := range frame.Calls {
		collectTransfers(sub, topic, out)
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
	require.Equal(t, precompile, view32(t, h, pool, types.RolePool, "eventEmitter"))
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
	require.Equal(t, true, view32(t, h, at, types.RoleFactory, "hasRole",
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
			require.Equal(t, big.NewInt(0), view32(t, h, at, role, "registeredDefaultAdminCount"), role)
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
