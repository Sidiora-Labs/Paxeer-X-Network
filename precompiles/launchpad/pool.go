package launchpad

import (
	"fmt"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// SidioraPool constants (Launch-x:contracts/contracts/core/SidioraPool.sol:80-81,
// Launch-x:contracts/contracts/base/Pausable.sol:7).
const (
	snapshotInterval     int64 = 60
	snapshotMinVolumeBps int64 = 1
)

var (
	pauserRole = [32]byte(crypto.Keccak256Hash([]byte("PAUSER_ROLE")))
	// routerRole is SidioraFactory.ROUTER_ROLE, the role swapFor requires of
	// its caller in the factory's role table.
	routerRole = [32]byte(crypto.Keccak256Hash([]byte("ROUTER_ROLE")))
	poolABI    = roleABIs[types.RolePool]
	erc20ABI   = roleABIs["SidioraERC20"]
)

// PoolKeeper is the launchpad keeper surface the SidioraPool role drives.
type PoolKeeper interface {
	GetParams(ctx sdk.Context) types.Params
	GetMarket(ctx sdk.Context, denom string) (types.Market, bool)
	FeeBps(ctx sdk.Context, market types.Market) (*big.Int, error)
	GetQuoteLedger(ctx sdk.Context, ledger launchpadkeeper.QuoteLedger) (sdk.Int, error)
	HasRole(ctx sdk.Context, contract common.Address, role [32]byte, account common.Address) bool
	LockHeld(store launchpadkeeper.TransientStore, lock launchpadkeeper.Lock) bool
	Swap(ctx sdk.Context, caller launchpadkeeper.NestedCaller, locks launchpadkeeper.SwapState, denom string,
		isBuy bool, amountIn, minAmountOut *big.Int, trader, recipient common.Address,
		deadline uint64) (sdk.Context, launchpadkeeper.SwapResult, error)
	SyncReserves(ctx sdk.Context, caller launchpadkeeper.NestedCaller, locks launchpadkeeper.TransientStore,
		denom string) (sdk.Context, types.Market, error)
	Pause(ctx sdk.Context, caller sdk.AccAddress, denom string) error
	Unpause(ctx sdk.Context, caller sdk.AccAddress, denom string) error
	TransferGuardian(ctx sdk.Context, caller sdk.AccAddress, denom string, next sdk.AccAddress) error
	AcceptGuardian(ctx sdk.Context, caller sdk.AccAddress, denom string) error
}

// poolCall is one call to a SidioraPool surface as Run resolves it: the
// SidioraPool method and its arguments, the original EVM caller, the market
// of the pool and the nested caller of the run.
type poolCall struct {
	ctx    sdk.Context
	evm    *vm.EVM
	keeper PoolKeeper
	nested launchpadkeeper.NestedCaller
	method *abi.Method
	args   []interface{}
	caller common.Address
	market types.Market
}

func (c poolCall) pool() common.Address  { return common.HexToAddress(c.market.PoolAddress) }
func (c poolCall) token() common.Address { return common.HexToAddress(c.market.TokenAddress) }

// PoolInfo is the ISidioraPool.PoolInfo tuple of getPoolInfo.
type PoolInfo struct {
	TokenAddress       common.Address
	OpticalAddress     common.Address
	VirtualUsdlReserve *big.Int
	RealUsdlBalance    *big.Int
	TokenReserve       *big.Int
	CreationTimestamp  *big.Int
	CumulativeVolume   *big.Int
}

// solidityPanic is the Panic(uint256) revert of compiled Solidity; code 0x32
// is the out-of-bounds read of a public array getter.
type solidityPanic uint64

const panicArrayOutOfBounds solidityPanic = 0x32

var panicSelector = crypto.Keccak256([]byte("Panic(uint256)"))[:4]

func (e solidityPanic) Error() string { return fmt.Sprintf("panic: 0x%02x", uint64(e)) }

// Revert is the EVM revert data of the panic.
func (e solidityPanic) Revert() ([]byte, error) {
	return append(common.CopyBytes(panicSelector), common.LeftPadBytes(new(big.Int).SetUint64(uint64(e)).Bytes(), 32)...), nil
}

// poolHandler serves the SidioraPool role at a pool address. It returns the
// context the run continues on.
func (p PrecompileExecutor) poolHandler(c poolCall) (sdk.Context, []byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return c.ctx, nil, err
	}
	switch c.method.Name {
	case "swap":
		return p.poolSwap(c, c.caller, c.args[0].(*big.Int), c.args[1].(*big.Int), c.args[2].(bool),
			c.args[3].(common.Address), c.args[4].(*big.Int))
	case "swapFor":
		return p.poolSwapFor(c)
	case "syncReserves":
		return p.syncReserves(c)
	case "pause", "unpause", "transferGuardian", "acceptGuardian", "migrateFactory", "initialize":
		ret, err := p.guardianHandler(c)
		return c.ctx, ret, err
	default:
		ret, err := p.poolViews(c)
		return c.ctx, ret, err
	}
}

// poolSwapFor is swapFor: the pool lock and the pause flag are checked before
// the router role and the trader, in the modifier order of
// Launch-x:contracts/contracts/core/SidioraPool.sol:158-166.
func (p PrecompileExecutor) poolSwapFor(c poolCall) (sdk.Context, []byte, error) {
	if c.keeper.LockHeld(c.evm.StateDB, launchpadkeeper.PoolLock(c.pool())) {
		return c.ctx, nil, types.ErrReentrancyGuardReentrantCall
	}
	if c.market.Paused {
		return c.ctx, nil, types.ErrPaused
	}
	if !c.keeper.HasRole(c.ctx, common.HexToAddress(c.market.Factory), routerRole, c.caller) {
		return c.ctx, nil, types.ErrUnauthorized
	}
	trader := c.args[3].(common.Address)
	if trader == (common.Address{}) {
		return c.ctx, nil, types.ErrZeroAddress
	}
	return p.poolSwap(c, trader, c.args[0].(*big.Int), c.args[1].(*big.Int), c.args[2].(bool),
		c.args[4].(common.Address), c.args[5].(*big.Int))
}

// poolSwap is SidioraPool._swap over Keeper.Swap, which holds the pool lock:
// after the swap the booked backing must still be held at the pool, and Swap
// is emitted at 0x1017 as EventEmitter.emitSwap does.
func (p PrecompileExecutor) poolSwap(c poolCall, trader common.Address, amountIn, minAmountOut *big.Int, isBuy bool,
	recipient common.Address, deadline *big.Int) (sdk.Context, []byte, error) {
	ctx, result, err := c.keeper.Swap(c.ctx, c.nested, c.evm.StateDB, c.market.Denom, isBuy, amountIn, minAmountOut,
		trader, recipient, clampUint64(deadline))
	if err != nil {
		return ctx, nil, err
	}
	c.ctx = ctx
	updated, err := p.backed(c)
	if err != nil {
		return ctx, nil, err
	}
	price, err := updated.Price()
	if err != nil {
		return ctx, nil, err
	}
	if common.HexToAddress(updated.EventEmitter) != (common.Address{}) {
		if err := p.emitFamily(c.evm, "Swap", poolID(c.pool()), trader, isBuy, amountIn, result.AmountOut,
			result.FeeAmount, price); err != nil {
			return ctx, nil, err
		}
	}
	ret, err := c.method.Outputs.Pack(result.AmountOut)
	return ctx, ret, err
}

// backed is the InvalidBalance check of
// Launch-x:contracts/contracts/core/SidioraPool.sol:268: HeldQuote(pool) at
// least realUsdlBalance and token.balanceOf(pool) at least tokenReserve. It
// returns the market as the swap left it.
func (p PrecompileExecutor) backed(c poolCall) (types.Market, error) {
	market, found := c.keeper.GetMarket(c.ctx, c.market.Denom)
	if !found {
		return types.Market{}, fmt.Errorf("%w: %s", types.ErrUnknownMarket, c.market.Denom)
	}
	held, err := c.keeper.GetQuoteLedger(c.ctx, launchpadkeeper.HeldQuoteLedger(c.pool()))
	if err != nil {
		return types.Market{}, err
	}
	tokens, err := p.tokenBalance(c)
	if err != nil {
		return types.Market{}, err
	}
	if held.LT(market.RealQuoteBalance) || tokens.Cmp(market.TokenReserve.BigInt()) < 0 {
		return types.Market{}, types.ErrInvalidBalance
	}
	return market, nil
}

// tokenBalance is token.balanceOf(pool), read by the pool.
func (p PrecompileExecutor) tokenBalance(c poolCall) (*big.Int, error) {
	input, err := erc20ABI.Pack("balanceOf", c.pool())
	if err != nil {
		return nil, err
	}
	ret, err := c.nested.StaticCall(c.ctx, c.pool(), c.token(), input)
	if err != nil {
		return nil, err
	}
	out, err := erc20ABI.Unpack("balanceOf", ret)
	if err != nil {
		return nil, fmt.Errorf("%w: balanceOf: %w", types.ErrInvalidBalance, err)
	}
	return out[0].(*big.Int), nil
}

// syncReserves sweeps the pool, books HeldQuote(pool) as realUsdlBalance and
// token.balanceOf(pool) as tokenReserve under the pool lock and emits
// PoolStateUpdated at 0x1017 (Launch-x:contracts/contracts/core/SidioraPool.sol:279-291).
func (p PrecompileExecutor) syncReserves(c poolCall) (sdk.Context, []byte, error) {
	ctx, market, err := c.keeper.SyncReserves(c.ctx, c.nested, c.evm.StateDB, c.market.Denom)
	if err != nil {
		return ctx, nil, err
	}
	price, err := market.Price()
	if err != nil {
		return ctx, nil, err
	}
	if common.HexToAddress(market.EventEmitter) != (common.Address{}) {
		if err := p.emitFamily(c.evm, "PoolStateUpdated", poolID(c.pool()), market.VirtualQuoteReserve.BigInt(),
			market.RealQuoteBalance.BigInt(), market.TokenReserve.BigInt(), price); err != nil {
			return ctx, nil, err
		}
	}
	ret, err := c.method.Outputs.Pack(market.RealQuoteBalance.BigInt(), market.TokenReserve.BigInt())
	return ctx, ret, err
}

// guardianHandler serves pause, unpause, transferGuardian and acceptGuardian
// over the migrated guardian and Paused flag, Unauthorized() for any other
// caller, and refuses migrateFactory with Unauthorized() and initialize with
// AlreadyInitialized() for every caller.
func (p PrecompileExecutor) guardianHandler(c poolCall) ([]byte, error) {
	caller := p.account(c.ctx, c.caller)
	switch c.method.Name {
	case "pause", "unpause":
		paused := c.method.Name == "pause"
		var err error
		if paused {
			err = c.keeper.Pause(c.ctx, caller, c.market.Denom)
		} else {
			err = c.keeper.Unpause(c.ctx, caller, c.market.Denom)
		}
		if err != nil {
			return nil, err
		}
		if err := p.poolLog(c, "PauseToggled", nil, paused); err != nil {
			return nil, err
		}
	case "transferGuardian":
		next := c.args[0].(common.Address)
		if err := c.keeper.TransferGuardian(c.ctx, caller, c.market.Denom, p.account(c.ctx, next)); err != nil {
			return nil, err
		}
		guardian := p.evmAddress(c.ctx, c.market.Guardian)
		if err := p.poolLog(c, "GuardianTransferStarted",
			[]common.Hash{addressTopic(guardian), addressTopic(next)}); err != nil {
			return nil, err
		}
	case "acceptGuardian":
		if err := c.keeper.AcceptGuardian(c.ctx, caller, c.market.Denom); err != nil {
			return nil, err
		}
		previous := p.evmAddress(c.ctx, c.market.Guardian)
		if err := p.poolLog(c, "GuardianTransferred",
			[]common.Hash{addressTopic(previous), addressTopic(c.caller)}); err != nil {
			return nil, err
		}
	case "migrateFactory":
		return nil, types.ErrUnauthorized
	case "initialize":
		return nil, types.ErrAlreadyInitialized
	default:
		return nil, fmt.Errorf("launchpad: %s is not a SidioraPool guardian method", c.method.Name)
	}
	return c.method.Outputs.Pack()
}

// poolLog emits a SidioraPool event from the pool address.
func (p PrecompileExecutor) poolLog(c poolCall, name string, topics []common.Hash, data ...interface{}) error {
	event, ok := poolABI.Events[name]
	if !ok {
		return fmt.Errorf("launchpad: unknown SidioraPool event %s", name)
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(c.evm, c.pool(), append([]common.Hash{event.ID}, topics...), packed)
}

// poolViews answers the SidioraPool views from the market record, whose
// factory, feeAccumulator, eventEmitter and protocolConfig are the generation
// 2 addresses MigrateV6120 writes into every market (ruling 5). Views take no
// lock.
func (p PrecompileExecutor) poolViews(c poolCall) ([]byte, error) {
	m := c.market
	address := func(hex string) common.Address { return common.HexToAddress(hex) }
	var out []interface{}
	switch c.method.Name {
	case "PAUSER_ROLE":
		out = []interface{}{pauserRole}
	case "SNAPSHOT_INTERVAL":
		out = []interface{}{big.NewInt(snapshotInterval)}
	case "SNAPSHOT_MIN_VOLUME_BPS":
		out = []interface{}{big.NewInt(snapshotMinVolumeBps)}
	case "tokenAddress":
		out = []interface{}{address(m.TokenAddress)}
	case "usdlAddress":
		out = []interface{}{address(c.keeper.GetParams(c.ctx).QuoteToken)}
	case "opticalAddress":
		out = []interface{}{address(m.Optical)}
	case "feeAccumulator":
		out = []interface{}{address(m.FeeAccumulator)}
	case "eventEmitter":
		out = []interface{}{address(m.EventEmitter)}
	case "protocolConfig":
		out = []interface{}{address(m.ProtocolConfig)}
	case "factory":
		out = []interface{}{address(m.Factory)}
	case "guardian":
		out = []interface{}{p.evmAddress(c.ctx, m.Guardian)}
	case "pendingGuardian":
		out = []interface{}{p.evmAddress(c.ctx, m.PendingGuardian)}
	case "paused", "emergencyPaused":
		out = []interface{}{m.Paused}
	case "virtualUsdlReserve":
		out = []interface{}{m.VirtualQuoteReserve.BigInt()}
	case "realUsdlBalance":
		out = []interface{}{m.RealQuoteBalance.BigInt()}
	case "tokenReserve":
		out = []interface{}{m.TokenReserve.BigInt()}
	case "creationTimestamp":
		out = []interface{}{big.NewInt(m.CreationTime)}
	case "cumulativeVolume":
		out = []interface{}{m.CumulativeVolume.BigInt()}
	case "cumulativeBuyUsdlVolume":
		out = []interface{}{m.CumulativeBuyQuoteVolume.BigInt()}
	case "cumulativeSellUsdlVolume":
		out = []interface{}{m.CumulativeSellQuoteVolume.BigInt()}
	case "accumulatedUsdlFees":
		out = []interface{}{m.AccumulatedQuoteFees.BigInt()}
	case "accumulatedTokenFees":
		out = []interface{}{m.AccumulatedTokenFees.BigInt()}
	case "lastSnapshotTimestamp":
		out = []interface{}{big.NewInt(m.LastSnapshotTimestamp)}
	case "snapshotIndex":
		out = []interface{}{new(big.Int).SetUint64(m.SnapshotIndex)}
	case "snapshotCount":
		out = []interface{}{new(big.Int).SetUint64(m.SnapshotCount)}
	case "priceSnapshots":
		index := c.args[0].(*big.Int)
		if !index.IsUint64() || index.Uint64() >= types.SnapshotSlots {
			return nil, panicArrayOutOfBounds
		}
		out = []interface{}{m.Snapshots()[index.Uint64()]}
	case "getPriceSnapshots":
		out = []interface{}{orderedSnapshots(m)}
	case "getReserves":
		out = []interface{}{m.VirtualQuoteReserve.BigInt(), m.RealQuoteBalance.BigInt(), m.TokenReserve.BigInt()}
	case "getEffectiveReserves":
		out = []interface{}{m.EffectiveQuote(), m.TokenReserve.BigInt()}
	case "getPrice":
		price, err := m.Price()
		if err != nil {
			return nil, err
		}
		out = []interface{}{price}
	case "currentFeeBps":
		fee, err := c.keeper.FeeBps(c.ctx, m)
		if err != nil {
			return nil, err
		}
		out = []interface{}{fee}
	case "getPoolInfo":
		out = []interface{}{PoolInfo{TokenAddress: address(m.TokenAddress), OpticalAddress: address(m.Optical),
			VirtualUsdlReserve: m.VirtualQuoteReserve.BigInt(), RealUsdlBalance: m.RealQuoteBalance.BigInt(),
			TokenReserve: m.TokenReserve.BigInt(), CreationTimestamp: big.NewInt(m.CreationTime),
			CumulativeVolume: m.CumulativeVolume.BigInt()}}
	default:
		return nil, fmt.Errorf("launchpad: %s is not a SidioraPool method", c.method.Name)
	}
	return c.method.Outputs.Pack(out...)
}

// orderedSnapshots is SidioraPool._orderedSnapshots: the snapshotCount prices
// oldest first, from snapshotIndex once the ring is full, zero after.
func orderedSnapshots(m types.Market) [types.SnapshotSlots]*big.Int {
	ring := m.Snapshots()
	var start uint64
	if m.SnapshotCount == types.SnapshotSlots {
		start = m.SnapshotIndex
	}
	var ordered [types.SnapshotSlots]*big.Int
	for i := range ordered {
		ordered[i] = new(big.Int)
		if uint64(i) < m.SnapshotCount {
			ordered[i] = ring[(start+uint64(i))%types.SnapshotSlots]
		}
	}
	return ordered
}

// poolID is the EventEmitter pool id, bytes32(uint256(uint160(pool))).
func poolID(pool common.Address) common.Hash { return common.BytesToHash(pool.Bytes()) }
