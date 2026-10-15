package launchpad

import (
	"errors"
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

// Router trade kinds of EventEmitter.emitRouterTrade
// (Launch-x:contracts/contracts/periphery/Router.sol:203, :240, :275).
const (
	tradeKindBuy uint8 = iota
	tradeKindSell
	tradeKindMultihop
)

var (
	routerABI       = roleABIs[types.RoleRouter]
	sidioraERC20ABI = roleABIs["SidioraERC20"]
	permitSelector  = crypto.Keccak256([]byte("permit(address,address,uint256,uint256,uint8,bytes32,bytes32)"))[:4]
)

func init() {
	roleHandlers[types.RoleRouter] = PrecompileExecutor.routerHandler
	roleHandlers[types.RoleFactory] = PrecompileExecutor.factoryHandler
}

// permitParams is the IRouter.PermitParams tuple of the WithPermit variants.
type permitParams struct {
	Value    *big.Int
	Deadline *big.Int
	V        uint8
	R        [32]byte
	S        [32]byte
}

// multicallFailure is MulticallFailed(index, reason) of
// Launch-x:contracts/contracts/base/Multicall.sol:18: the revert data of the
// failed call wrapped with its index.
type multicallFailure struct {
	index  int
	reason []byte
	cause  error
}

func (e *multicallFailure) Error() string {
	return fmt.Sprintf("%s: call %d: %v", types.ErrMulticallFailed.Error(), e.index, e.cause)
}

func (e *multicallFailure) Unwrap() error { return types.ErrMulticallFailed }

// Revert is the EVM revert data of the failure.
func (e *multicallFailure) Revert() ([]byte, error) {
	contractError, ok := types.ContractErrorOf(types.ErrMulticallFailed)
	if !ok {
		return nil, fmt.Errorf("%w: MulticallFailed has no contract error", types.ErrInvalidParams)
	}
	return contractError.Revert(big.NewInt(int64(e.index)), e.reason)
}

// routerHandler serves the Router role at the generation 2 Router address and
// at 0x1017 (Launch-x:contracts/contracts/periphery/Router.sol).
func (p PrecompileExecutor) routerHandler(c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	roles := p.keeper.Generation2()
	if ret, served, err := p.accessControl(c, roles.Router); served {
		return ret, err
	}
	switch c.method.Name {
	case "factory":
		return c.method.Outputs.Pack(roles.SidioraFactory)
	case "poolRegistry":
		return c.method.Outputs.Pack(roles.PoolRegistry)
	case "protocolConfig":
		return c.method.Outputs.Pack(roles.ProtocolConfig)
	case "usdlAddress":
		return c.method.Outputs.Pack(p.usdl(c.ctx))
	case "multicall":
		return p.multicall(c)
	case "createMarket", "createMarketWithPermit", "buy", "buyWithPermit", "sell", "sellWithPermit",
		"swapTokenForToken", "swapTokenForTokenWithPermit":
		release, err := p.keeper.EnterLock(c.evm.StateDB, launchpadkeeper.RouterLock)
		if err != nil {
			return nil, err
		}
		defer release()
		nested, err := runNested(c)
		if err != nil {
			return nil, err
		}
		return p.routerEntry(c, nested)
	default:
		return nil, fmt.Errorf("launchpad: %s is not a Router method", c.method.Name)
	}
}

// routerEntry serves one nonReentrant Router entry point under the Router
// lock.
func (p PrecompileExecutor) routerEntry(c *call, nested launchpadkeeper.NestedCaller) ([]byte, error) {
	switch c.method.Name {
	case "createMarket":
		return p.routerCreateMarket(c, nested)
	case "createMarketWithPermit":
		p.applyPermit(c, nested, p.usdl(c.ctx), c.args[4])
		return p.routerCreateMarket(c, nested)
	case "buy":
		return p.buy(c, nested)
	case "buyWithPermit":
		p.applyPermit(c, nested, p.usdl(c.ctx), c.args[4])
		return p.buy(c, nested)
	case "sell":
		return p.sell(c, nested)
	case "sellWithPermit":
		market, err := p.registeredMarket(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		p.applyPermit(c, nested, common.HexToAddress(market.TokenAddress), c.args[4])
		return p.sell(c, nested)
	case "swapTokenForToken":
		return p.swapTokenForToken(c, nested)
	default:
		p.applyPermit(c, nested, c.args[0].(common.Address), c.args[5])
		return p.swapTokenForToken(c, nested)
	}
}

// accessControl serves the AccessControl, Initializable and UUPS methods the
// Router and SidioraFactory share, over the migrated role table of contract.
// It reports whether the method is one of them.
func (p PrecompileExecutor) accessControl(c *call, contract common.Address) ([]byte, bool, error) {
	switch c.method.Name {
	case "DEFAULT_ADMIN_ROLE":
		ret, err := c.method.Outputs.Pack(launchpadkeeper.DefaultAdminRole)
		return ret, true, err
	case "getRoleAdmin":
		ret, err := c.method.Outputs.Pack(p.keeper.RoleAdmin(c.ctx, contract, c.args[0].([32]byte)))
		return ret, true, err
	case "hasRole":
		ret, err := c.method.Outputs.Pack(p.keeper.HasRole(c.ctx, contract, c.args[0].([32]byte),
			c.args[1].(common.Address)))
		return ret, true, err
	case "registeredDefaultAdminCount":
		ret, err := c.method.Outputs.Pack(new(big.Int).SetUint64(p.keeper.RegisteredDefaultAdminCount(c.ctx, contract)))
		return ret, true, err
	case "grantRole", "revokeRole", "upgradeToAndCall":
		return nil, true, launchpadkeeper.AdminRefusal(c.caller)
	case "renounceRole":
		role := c.args[0].([32]byte)
		revoked, err := p.keeper.RenounceRole(c.ctx, contract, role, c.caller, c.args[1].(common.Address))
		if err != nil {
			return nil, true, err
		}
		if revoked {
			if err := emitEvent(c.evm, contract, roleABIs[c.role].Events["RoleRevoked"], role, c.caller,
				c.caller); err != nil {
				return nil, true, err
			}
		}
		ret, err := c.method.Outputs.Pack()
		return ret, true, err
	case "proxiableUUID":
		return nil, true, types.ErrUUPSNotThroughActiveProxy
	case "initialize":
		return nil, true, types.ErrAlreadyInitialized
	default:
		return nil, false, nil
	}
}

// multicall is Multicall.multicall: every entry is served as a Router call
// from the same caller in order, and the first failure reverts the batch with
// MulticallFailed(index, revert data of the entry).
func (p PrecompileExecutor) multicall(c *call) ([]byte, error) {
	data := c.args[0].([][]byte)
	results := make([][]byte, len(data))
	for i, input := range data {
		if len(input) < 4 {
			return nil, &multicallFailure{index: i, reason: []byte{}, cause: errUnknownSelector}
		}
		method, err := routerABI.MethodById(input[:4])
		if err != nil {
			return nil, &multicallFailure{index: i, reason: []byte{}, cause: errUnknownSelector}
		}
		args, err := decodeWhole(method, input[4:])
		if err != nil {
			return nil, &multicallFailure{index: i, reason: []byte{}, cause: err}
		}
		entry := *c
		entry.method, entry.args = method, args
		ret, err := p.routerHandler(&entry)
		c.ctx = entry.ctx
		if err != nil {
			return nil, &multicallFailure{index: i, reason: entryRevert(err), cause: err}
		}
		results[i] = ret
	}
	return c.method.Outputs.Pack(results)
}

// entryRevert is the revert data a failed multicall entry returned.
func entryRevert(err error) []byte {
	if data := runRevert(err); data != nil {
		return data
	}
	return []byte{}
}

// routerCreateMarket is Router._createMarket: the creation fee is charged
// from the caller and the market created for the caller as
// SidioraFactory.createMarketFor does, then MarketCreated is emitted at the
// Router (Launch-x:contracts/contracts/periphery/Router.sol:155-175).
func (p PrecompileExecutor) routerCreateMarket(c *call, nested launchpadkeeper.NestedCaller) ([]byte, error) {
	market, err := p.createMarket(c, nested, c.caller, c.args[0].(string), c.args[1].(string), c.args[2].(uint8),
		c.args[3].(common.Address))
	if err != nil {
		return nil, err
	}
	token, pool, id := common.HexToAddress(market.TokenAddress), common.HexToAddress(market.PoolAddress),
		new(big.Int).SetUint64(market.FeeRightID)
	if err := emitEvent(c.evm, c.target, routerABI.Events["MarketCreated"], token, pool, c.caller, id); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(token, pool, id)
}

// buy is Router._buy (Launch-x:contracts/contracts/periphery/Router.sol:178-204).
func (p PrecompileExecutor) buy(c *call, nested launchpadkeeper.NestedCaller) ([]byte, error) {
	pool, amountIn, minOut, deadline := c.args[0].(common.Address), c.args[1].(*big.Int), c.args[2].(*big.Int),
		c.args[3].(*big.Int)
	market, err := p.tradeChecks(c, pool, amountIn, deadline)
	if err != nil {
		return nil, err
	}
	usdl, token := p.usdl(c.ctx), common.HexToAddress(market.TokenAddress)
	if err := p.pullInput(c, nested, true, pool, usdl, amountIn); err != nil {
		return nil, err
	}
	amountOut, err := p.routerSwapFor(c, nested, market, true, amountIn, minOut, c.caller, deadline)
	if err != nil {
		return nil, err
	}
	if err := emitEvent(c.evm, c.target, routerABI.Events["Buy"], pool, c.caller, amountIn,
		amountOut); err != nil {
		return nil, err
	}
	if err := p.mirrorTrade(c, tradeKindBuy, pool, usdl, token, amountIn, amountOut, new(big.Int)); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(amountOut)
}

// sell is Router._sell (Launch-x:contracts/contracts/periphery/Router.sol:206-232).
func (p PrecompileExecutor) sell(c *call, nested launchpadkeeper.NestedCaller) ([]byte, error) {
	pool, amountIn, minOut, deadline := c.args[0].(common.Address), c.args[1].(*big.Int), c.args[2].(*big.Int),
		c.args[3].(*big.Int)
	market, err := p.tradeChecks(c, pool, amountIn, deadline)
	if err != nil {
		return nil, err
	}
	usdl, token := p.usdl(c.ctx), common.HexToAddress(market.TokenAddress)
	if err := p.pullInput(c, nested, false, pool, token, amountIn); err != nil {
		return nil, err
	}
	amountOut, err := p.routerSwapFor(c, nested, market, false, amountIn, minOut, c.caller, deadline)
	if err != nil {
		return nil, err
	}
	if err := emitEvent(c.evm, c.target, routerABI.Events["Sell"], pool, c.caller, amountIn,
		amountOut); err != nil {
		return nil, err
	}
	if err := p.mirrorTrade(c, tradeKindSell, pool, token, usdl, amountIn, amountOut, new(big.Int)); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(amountOut)
}

// swapTokenForToken is Router._swapTokenForToken
// (Launch-x:contracts/contracts/periphery/Router.sol:237-276): leg 1 sells
// tokenIn on its pool, leg 2 buys tokenOut on its pool with the intermediate
// USDL, and the end-to-end slippage bound applies to leg 2.
func (p PrecompileExecutor) swapTokenForToken(c *call, nested launchpadkeeper.NestedCaller) ([]byte, error) {
	tokenIn, tokenOut, amountIn, minOut, deadline := c.args[0].(common.Address), c.args[1].(common.Address),
		c.args[2].(*big.Int), c.args[3].(*big.Int), c.args[4].(*big.Int)
	if expired(c.ctx, deadline) {
		return nil, types.ErrDeadlineExpired
	}
	if amountIn.Sign() == 0 {
		return nil, types.ErrZeroAmount
	}
	if tokenIn == tokenOut {
		return nil, types.ErrSameToken
	}
	if tokenIn == (common.Address{}) || tokenOut == (common.Address{}) {
		return nil, types.ErrZeroAddress
	}
	poolA, foundA := p.keeper.PoolByToken(c.ctx, tokenIn)
	poolB, foundB := p.keeper.PoolByToken(c.ctx, tokenOut)
	if !foundA || !foundB {
		return nil, types.ErrUnknownMarket
	}
	marketA, err := p.registeredMarket(c.ctx, poolA)
	if err != nil {
		return nil, err
	}
	marketB, err := p.registeredMarket(c.ctx, poolB)
	if err != nil {
		return nil, err
	}
	if err := p.pullInput(c, nested, false, poolA, tokenIn, amountIn); err != nil {
		return nil, err
	}
	// The leg 1 output is paid to the second pool, whose swap sweeps it into
	// HeldQuote(poolB) as prepaid input.
	intermediate, err := p.routerSwapFor(c, nested, marketA, false, amountIn, new(big.Int), poolB, deadline)
	if err != nil {
		return nil, err
	}
	amountOut, err := p.routerSwapFor(c, nested, marketB, true, intermediate, minOut, c.caller, deadline)
	if err != nil {
		return nil, err
	}
	if err := emitEvent(c.evm, c.target, routerABI.Events["MultihopSwap"], c.caller, tokenIn, tokenOut, amountIn,
		intermediate, amountOut); err != nil {
		return nil, err
	}
	if err := p.mirrorTrade(c, tradeKindMultihop, poolA, tokenIn, tokenOut, amountIn, amountOut,
		intermediate); err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(amountOut, intermediate)
}

// tradeChecks are the _buy and _sell preconditions in contract order:
// DeadlineExpired, ZeroAmount, ZeroAddress, then PoolNotFound for a pool the
// PoolRegistry does not hold. It returns the market of the pool.
func (p PrecompileExecutor) tradeChecks(c *call, pool common.Address, amountIn, deadline *big.Int) (types.Market,
	error) {
	if expired(c.ctx, deadline) {
		return types.Market{}, types.ErrDeadlineExpired
	}
	if amountIn.Sign() == 0 {
		return types.Market{}, types.ErrZeroAmount
	}
	if pool == (common.Address{}) {
		return types.Market{}, types.ErrZeroAddress
	}
	return p.registeredMarket(c.ctx, pool)
}

// expired is block.timestamp > deadline.
func expired(ctx sdk.Context, deadline *big.Int) bool {
	return big.NewInt(ctx.BlockTime().Unix()).Cmp(deadline) > 0
}

// registeredMarket is the market of a pool PoolRegistry.isRegisteredPool
// holds; any other address reverts PoolNotFound().
func (p PrecompileExecutor) registeredMarket(ctx sdk.Context, pool common.Address) (types.Market, error) {
	_, registered, err := p.keeper.GetPoolMetadata(ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !registered {
		return types.Market{}, types.ErrUnknownMarket
	}
	surface, found, err := p.keeper.GetSurface(ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !found || surface.Role != types.RolePool {
		return types.Market{}, fmt.Errorf("%w: registered pool %s has no pool surface", types.ErrUnknownMarket,
			pool.Hex())
	}
	market, found := p.keeper.GetMarket(ctx, surface.Market)
	if !found {
		return types.Market{}, fmt.Errorf("%w: pool %s market %s", types.ErrUnknownMarket, pool.Hex(), surface.Market)
	}
	return market, nil
}

// pullInput pulls the trade input from the caller with the allowance it
// granted 0x1017: USDL.transferFrom(caller, launchpad module EVM address,
// amount) into HeldQuote(pool) for a buy, and token.transferFrom(caller, pool,
// amount) called by 0x1017 for a sell, reverting TransferFromFailed() as
// TransferHelper.safeTransferFrom does
// (Launch-x:contracts/contracts/periphery/Router.sol:177-188).
func (p PrecompileExecutor) pullInput(c *call, nested launchpadkeeper.NestedCaller, isBuy bool, pool,
	token common.Address, amount *big.Int) error {
	if isBuy {
		ctx, _, err := p.keeper.PullQuote(c.ctx, nested, c.caller, launchpadkeeper.HeldQuoteLedger(pool),
			sdk.NewIntFromBigInt(amount))
		if err != nil {
			return err
		}
		c.ctx = ctx
		return nil
	}
	input, err := sidioraERC20ABI.Pack("transferFrom", c.caller, pool, amount)
	if err != nil {
		return err
	}
	ctx, ret, err := nested.Call(c.ctx, p.address, token, input)
	if err != nil {
		return fmt.Errorf("%w: %v", types.ErrTransferFromFailed, err)
	}
	if len(ret) != 0 && (len(ret) != 32 || new(big.Int).SetBytes(ret).Cmp(common.Big1) != 0) {
		return fmt.Errorf("%w: return data %x", types.ErrTransferFromFailed, ret)
	}
	c.ctx = ctx
	return nil
}

// applyPermit is Router._executePermit: permit(caller, 0x1017, value,
// deadline, v, r, s) on token as a nested call whose failure is ignored, so a
// front-run or unsupported permit leaves the pull to the existing allowance
// (Launch-x:contracts/contracts/periphery/Router.sol:292-314).
func (p PrecompileExecutor) applyPermit(c *call, nested launchpadkeeper.NestedCaller, token common.Address,
	tuple interface{}) {
	permit, ok := abi.ConvertType(tuple, new(permitParams)).(*permitParams)
	if !ok {
		return
	}
	words := []interface{}{c.caller, p.address, permit.Value, permit.Deadline, permit.V, permit.R, permit.S}
	args := abi.Arguments{}
	for _, kind := range []string{"address", "address", "uint256", "uint256", "uint8", "bytes32", "bytes32"} {
		typ, err := abi.NewType(kind, "", nil)
		if err != nil {
			return
		}
		args = append(args, abi.Argument{Type: typ})
	}
	packed, err := args.Pack(words...)
	if err != nil {
		return
	}
	if ctx, _, err := nested.Call(c.ctx, p.address, token, append(common.CopyBytes(permitSelector), packed...)); err == nil {
		c.ctx = ctx
	}
}

// routerSwapFor is SidioraPool.swapFor(amountIn, minAmountOut, isBuy,
// caller, recipient, deadline) sent by the Router: Keeper.Swap under the pool
// lock, then Swap of the EventEmitter family at 0x1017 as SidioraPool._swap
// emits it (Launch-x:contracts/contracts/core/SidioraPool.sol:270-274). It
// returns the amount out.
func (p PrecompileExecutor) routerSwapFor(c *call, nested launchpadkeeper.NestedCaller, market types.Market,
	isBuy bool, amountIn, minOut *big.Int, recipient common.Address, deadline *big.Int) (*big.Int, error) {
	ctx, result, err := p.keeper.Swap(c.ctx, nested, c.evm.StateDB, market.Denom, isBuy, amountIn, minOut, c.caller,
		recipient, clampUint64(deadline))
	if err != nil {
		return nil, err
	}
	c.ctx = ctx
	updated, found := p.keeper.GetMarket(c.ctx, market.Denom)
	if !found {
		return nil, fmt.Errorf("%w: %s", types.ErrUnknownMarket, market.Denom)
	}
	if common.HexToAddress(updated.EventEmitter) != (common.Address{}) {
		price, err := updated.Price()
		if err != nil {
			return nil, err
		}
		pool := common.HexToAddress(market.PoolAddress)
		if err := p.log(c.evm, "Swap", []common.Hash{poolID(pool), addressTopic(c.caller)}, isBuy, amountIn,
			result.AmountOut, result.FeeAmount, price, big.NewInt(c.ctx.BlockTime().Unix()),
			big.NewInt(c.ctx.BlockHeight())); err != nil {
			return nil, err
		}
	}
	return result.AmountOut, nil
}

// runNested is the nested caller of the run.
func runNested(c *call) (launchpadkeeper.NestedCaller, error) {
	if c.nested == nil {
		return nil, errors.New("launchpad: the run has no nested caller")
	}
	return c.nested, nil
}

// mirrorTrade is Router._mirrorTrade: RouterTrade of the EventEmitter family
// at 0x1017 when the factory names an event emitter
// (Launch-x:contracts/contracts/periphery/Router.sol:280-283).
func (p PrecompileExecutor) mirrorTrade(c *call, kind uint8, pool, tokenIn, tokenOut common.Address, amountIn,
	amountOut, intermediate *big.Int) error {
	if !p.familyEnabled() {
		return nil
	}
	return p.emitFamilyEvent(c, "RouterTrade", kind, pool, c.caller, tokenIn, tokenOut, amountIn, amountOut,
		intermediate)
}

// factoryHandler serves the SidioraFactory role at the generation 2 factory
// address and, for the selectors the Router does not declare, at 0x1017
// (Launch-x:contracts/contracts/core/SidioraFactory.sol). migrateFactory is
// a SidioraPool method and every DEFAULT_ADMIN_ROLE method is refused under
// ruling 7.
func (p PrecompileExecutor) factoryHandler(c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	roles := p.keeper.Generation2()
	if ret, served, err := p.accessControl(c, roles.SidioraFactory); served {
		return ret, err
	}
	switch c.method.Name {
	case "ROUTER_ROLE":
		return c.method.Outputs.Pack(routerRole)
	case "poolBeacon":
		return c.method.Outputs.Pack(roles.PoolBeacon)
	case "nftContract":
		return c.method.Outputs.Pack(roles.SidioraNFT)
	case "poolRegistry":
		return c.method.Outputs.Pack(roles.PoolRegistry)
	case "eventEmitter":
		return c.method.Outputs.Pack(roles.EventEmitter)
	case "protocolConfig":
		return c.method.Outputs.Pack(roles.ProtocolConfig)
	case "treasury":
		return c.method.Outputs.Pack(roles.Treasury)
	case "feeAccumulator":
		return c.method.Outputs.Pack(roles.FeeAccumulator)
	case "usdlAddress":
		return c.method.Outputs.Pack(p.usdl(c.ctx))
	case "getNonce":
		return c.method.Outputs.Pack(new(big.Int).SetUint64(p.keeper.FactoryNonce(c.ctx, c.args[0].(common.Address))))
	case "predictPoolAddress":
		pool, err := p.keeper.PredictPoolAddress(c.ctx, c.args[0].(common.Address), c.args[1].(common.Address),
			c.args[2].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(pool)
	case "createMarket":
		nested, err := runNested(c)
		if err != nil {
			return nil, err
		}
		market, err := p.createMarket(c, nested, c.caller, c.args[0].(string), c.args[1].(string),
			c.args[2].(uint8), c.args[3].(common.Address))
		if err != nil {
			return nil, err
		}
		return packMarket(c, market)
	case "createMarketFor":
		if err := p.keeper.CheckRole(c.ctx, roles.SidioraFactory, routerRole, c.caller); err != nil {
			return nil, err
		}
		creator := c.args[0].(common.Address)
		if creator == (common.Address{}) {
			return nil, types.ErrZeroAddress
		}
		nested, err := runNested(c)
		if err != nil {
			return nil, err
		}
		market, err := p.createMarket(c, nested, creator, c.args[1].(string), c.args[2].(string),
			c.args[3].(uint8), c.args[4].(common.Address))
		if err != nil {
			return nil, err
		}
		return packMarket(c, market)
	default:
		return nil, fmt.Errorf("launchpad: %s is not a SidioraFactory method", c.method.Name)
	}
}

func packMarket(c *call, market types.Market) ([]byte, error) {
	return c.method.Outputs.Pack(common.HexToAddress(market.TokenAddress), common.HexToAddress(market.PoolAddress),
		new(big.Int).SetUint64(market.FeeRightID))
}

// createMarket is SidioraFactory._createMarket for creator over
// Keeper.CreateMarket, which charges the creation fee by
// USDL.transferFrom(creator, launchpad module EVM address, fee), followed by
// the logs of the contract flow in contract order: Deposited at the
// Treasury, Initialized(1) at the pool, Transfer and PoolNFTMinted at the
// fee-right collection, NftMint, PoolRegistered at the PoolRegistry and in
// the EventEmitter family, TokenDeployed and MarketCreated of the family, and
// MarketCreated at the factory (Launch-x:contracts/contracts/core/SidioraFactory.sol:72-163).
func (p PrecompileExecutor) createMarket(c *call, nested launchpadkeeper.NestedCaller, creator common.Address, name,
	symbol string, strategy uint8, optical common.Address) (types.Market, error) {
	nonce := p.keeper.FactoryNonce(c.ctx, creator)
	fee := p.keeper.GetParams(c.ctx).CreationFee
	ctx, market, err := p.keeper.CreateMarket(c.ctx, nested, c.evm, creator, name, symbol, types.FeeStrategy(strategy),
		optical)
	if err != nil {
		return types.Market{}, err
	}
	c.ctx = ctx
	roles := p.keeper.Generation2()
	token, pool := common.HexToAddress(market.TokenAddress), common.HexToAddress(market.PoolAddress)
	id := new(big.Int).SetUint64(market.FeeRightID)
	if fee.IsPositive() {
		if err := emitEvent(c.evm, roles.Treasury, roleABIs[types.RoleTreasury].Events["Deposited"], p.usdl(c.ctx),
			c.target, fee.BigInt()); err != nil {
			return types.Market{}, err
		}
	}
	if err := emitEvent(c.evm, pool, roleABIs[types.RolePool].Events["Initialized"], uint64(1)); err != nil {
		return types.Market{}, err
	}
	collection := common.HexToAddress(market.FeeRightCollection)
	nft := roleABIs[types.RoleFeeRights]
	if err := emitEvent(c.evm, collection, nft.Events["Transfer"], common.Address{}, creator, id); err != nil {
		return types.Market{}, err
	}
	if err := emitEvent(c.evm, collection, nft.Events["PoolNFTMinted"], id, creator, pool); err != nil {
		return types.Market{}, err
	}
	family := p.familyEnabled()
	if family {
		if err := p.emitFamilyEvent(c, "NftMint", id, creator, pool, strategy); err != nil {
			return types.Market{}, err
		}
	}
	if err := emitEvent(c.evm, roles.PoolRegistry, roleABIs[types.RolePoolRegistry].Events["PoolRegistered"], pool,
		token, creator, optical, id, big.NewInt(c.ctx.BlockTime().Unix())); err != nil {
		return types.Market{}, err
	}
	if family {
		salt := crypto.Keccak256Hash(creator.Bytes(), []byte(name), []byte(symbol),
			common.LeftPadBytes(new(big.Int).SetUint64(nonce).Bytes(), 32))
		marketID := poolID(pool)
		if err := p.emitFamilyEvent(c, "PoolRegistered", pool, token, creator, optical, id); err != nil {
			return types.Market{}, err
		}
		if err := p.emitFamilyEvent(c, "TokenDeployed", token, pool, creator, salt, name, symbol, uint8(6),
			market.TotalSupply.BigInt()); err != nil {
			return types.Market{}, err
		}
		if err := p.emitFamilyEvent(c, "MarketCreated", marketID, token, creator, pool, optical); err != nil {
			return types.Market{}, err
		}
	}
	if err := emitEvent(c.evm, roles.SidioraFactory, roleABIs[types.RoleFactory].Events["MarketCreated"], token, pool,
		creator, id, optical); err != nil {
		return types.Market{}, err
	}
	return market, nil
}

// usdl is the USDL contract address, Params.QuoteToken.
func (p PrecompileExecutor) usdl(ctx sdk.Context) common.Address {
	return common.HexToAddress(p.keeper.GetParams(ctx).QuoteToken)
}

// familyEnabled reports whether the factory names an event emitter, the
// condition under which the contract flow calls the EventEmitter.
func (p PrecompileExecutor) familyEnabled() bool {
	return p.keeper.Generation2().EventEmitter != (common.Address{})
}

// emitFamilyEvent emits an EventEmitter family event of abi.json at 0x1017
// with block.timestamp and block.number appended, as the EventEmitter does.
func (p PrecompileExecutor) emitFamilyEvent(c *call, name string, values ...interface{}) error {
	event, ok := p.abi.Events[name]
	if !ok {
		return fmt.Errorf("launchpad: abi.json declares no event %s", name)
	}
	values = append(values, big.NewInt(c.ctx.BlockTime().Unix()), big.NewInt(c.ctx.BlockHeight()))
	return emitEvent(c.evm, p.address, event, values...)
}

// emitEvent emits event at address with values in declaration order: the
// indexed values become the topics after the event id and the rest the ABI
// encoded data.
func emitEvent(evm *vm.EVM, address common.Address, event abi.Event, values ...interface{}) error {
	if len(values) != len(event.Inputs) {
		return fmt.Errorf("launchpad: event %s takes %d values, got %d", event.Sig, len(event.Inputs), len(values))
	}
	topics := []common.Hash{event.ID}
	data := make([]interface{}, 0, len(values))
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, values[i])
			continue
		}
		topic, err := abi.MakeTopics([]interface{}{values[i]})
		if err != nil {
			return fmt.Errorf("launchpad: event %s topic %s: %w", event.Sig, input.Name, err)
		}
		topics = append(topics, topic[0][0])
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return fmt.Errorf("launchpad: event %s data: %w", event.Sig, err)
	}
	return pcommon.EmitEVMLog(evm, address, topics, packed)
}
