package launchpad

import (
	"fmt"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// FactoryRole is PoolRegistry FACTORY_ROLE.
var FactoryRole = crypto.Keccak256Hash([]byte("FACTORY_ROLE"))

var bpsDenominator = big.NewInt(10_000)

// QuoterQuoteResult is the IQuoter.QuoteResult tuple of quoteExactInput.
type QuoterQuoteResult struct {
	AmountOut      *big.Int
	FeeAmount      *big.Int
	PriceImpactBps *big.Int
}

// QuoterPoolStats is the IQuoter.PoolStats tuple of getPoolStats.
type QuoterPoolStats struct {
	VirtualUsdl      *big.Int
	RealUsdl         *big.Int
	TokenReserve     *big.Int
	CumulativeVolume *big.Int
	CurrentFeeBps    *big.Int
	PoolAge          *big.Int
	MarketCap        *big.Int
	Price            *big.Int
}

// QuoterMultihopResult is the IQuoter.MultihopQuoteResult tuple of
// quoteMultihop.
type QuoterMultihopResult struct {
	AmountOut              *big.Int
	IntermediateUsdl       *big.Int
	SellFeeAmount          *big.Int
	BuyFeeAmount           *big.Int
	SellPriceImpactBps     *big.Int
	BuyPriceImpactBps      *big.Int
	CombinedPriceImpactBps *big.Int
	PoolA                  common.Address
	PoolB                  common.Address
}

// RegistryPoolMetadata is the IPoolRegistry.PoolMetadata tuple of
// getPoolMetadata.
type RegistryPoolMetadata struct {
	Creator      common.Address
	Token        common.Address
	Optical      common.Address
	NftId        *big.Int
	CreatedAt    *big.Int
	CreatedBlock *big.Int
}

func init() {
	for _, role := range []string{types.RoleQuoter, types.RolePoolRegistry, types.RoleProtocolConfig} {
		roleHandlers[role] = PrecompileExecutor.viewHandler
	}
}

// viewHandler serves the Quoter, PoolRegistry and ProtocolConfig roles at
// their generation 2 addresses and at 0x1017: the AccessControl, UUPS and
// initializer methods every role shares, then the methods of the role.
func (p PrecompileExecutor) viewHandler(c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	contract, err := p.viewRoleContract(c.role)
	if err != nil {
		return nil, err
	}
	if ret, served, err := p.accessViews(c, contract); served {
		return ret, err
	}
	switch c.role {
	case types.RoleQuoter:
		return p.quoterViews(c)
	case types.RolePoolRegistry:
		return p.registryViews(c)
	default:
		return p.configViews(c)
	}
}

// viewRoleContract is the generation 2 address of role, whose role table and
// events the role's AccessControl methods use.
func (p PrecompileExecutor) viewRoleContract(role string) (common.Address, error) {
	roles := p.keeper.Generation2()
	switch role {
	case types.RoleQuoter:
		return roles.Quoter, nil
	case types.RolePoolRegistry:
		return roles.PoolRegistry, nil
	case types.RoleProtocolConfig:
		return roles.ProtocolConfig, nil
	}
	return common.Address{}, fmt.Errorf("launchpad: %s is not a view role", role)
}

// accessViews serves the AccessControl, UUPSUpgradeable and Initializable
// methods of the role contract and reports whether the method is one of
// them. Every DEFAULT_ADMIN_ROLE method is refused (ruling 7).
func (p PrecompileExecutor) accessViews(c *call, contract common.Address) ([]byte, bool, error) {
	var (
		ret []byte
		err error
	)
	switch c.method.Name {
	case "DEFAULT_ADMIN_ROLE":
		ret, err = c.method.Outputs.Pack(launchpadkeeper.DefaultAdminRole)
	case "getRoleAdmin":
		ret, err = c.method.Outputs.Pack(p.keeper.RoleAdmin(c.ctx, contract, c.args[0].([32]byte)))
	case "hasRole":
		ret, err = c.method.Outputs.Pack(p.keeper.HasRole(c.ctx, contract, c.args[0].([32]byte),
			c.args[1].(common.Address)))
	case "registeredDefaultAdminCount":
		ret, err = c.method.Outputs.Pack(new(big.Int).SetUint64(p.keeper.RegisteredDefaultAdminCount(c.ctx, contract)))
	case "grantRole", "revokeRole", "upgradeToAndCall":
		err = launchpadkeeper.AdminRefusal(c.caller)
	case "renounceRole":
		ret, err = p.renounceRole(c, contract)
	case "proxiableUUID":
		err = fmt.Errorf("%w: proxiableUUID through the proxy", types.ErrUUPSNotThroughActiveProxy)
	case "initialize":
		err = fmt.Errorf("%w: %s", types.ErrAlreadyInitialized, c.role)
	default:
		return nil, false, nil
	}
	return ret, true, err
}

// renounceRole is AccessControl.renounceRole: the caller drops its own role
// and RoleRevoked(role, caller, caller) is emitted at the role contract when
// the caller held it.
func (p PrecompileExecutor) renounceRole(c *call, contract common.Address) ([]byte, error) {
	role := c.args[0].([32]byte)
	held, err := p.keeper.RenounceRole(c.ctx, contract, role, c.caller, c.args[1].(common.Address))
	if err != nil {
		return nil, err
	}
	if held {
		event, ok := roleABIs[c.role].Events["RoleRevoked"]
		if !ok {
			return nil, fmt.Errorf("launchpad: the %s role declares no RoleRevoked", c.role)
		}
		caller := common.BytesToHash(c.caller.Bytes())
		if err := pcommon.EmitEVMLog(c.evm, contract, []common.Hash{event.ID, role, caller, caller}, nil); err != nil {
			return nil, err
		}
	}
	return c.method.Outputs.Pack()
}

// quoterViews serves the Quoter methods as Launch-x Quoter computes them
// from its PoolRegistry and pools, both answered from the keeper.
func (p PrecompileExecutor) quoterViews(c *call) ([]byte, error) {
	roles := p.keeper.Generation2()
	switch c.method.Name {
	case "poolRegistry":
		return c.method.Outputs.Pack(roles.PoolRegistry)
	case "protocolConfig":
		return c.method.Outputs.Pack(roles.ProtocolConfig)
	case "getAllPools", "getPoolsByCreator":
		return p.registryViews(c)
	case "getPoolPrice":
		market, err := p.viewPoolMarket(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		price, err := market.Price()
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(price)
	case "getPoolStats":
		stats, err := p.poolStats(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(stats)
	case "getMarketCap":
		market, err := p.viewPoolMarket(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		marketCap, err := types.GetMarketCap(market.EffectiveQuote(), market.TokenReserve.BigInt(),
			market.TotalSupply.BigInt())
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(marketCap)
	case "quoteExactInput":
		market, err := p.quotablePool(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		leg, err := p.quoteLeg(c.ctx, market, c.args[1].(*big.Int), c.args[2].(bool), false)
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(QuoterQuoteResult{AmountOut: leg.amountOut, FeeAmount: leg.feeAmount,
			PriceImpactBps: leg.impactBps})
	case "quoteMultihop":
		result, err := p.quoteMultihop(c.ctx, c.args[0].(common.Address), c.args[1].(common.Address),
			c.args[2].(*big.Int))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(result)
	}
	return nil, fmt.Errorf("launchpad: the Quoter role serves no %s", c.method.Sig)
}

// viewPoolMarket is the market of pool for the pool reads of the Quoter; an
// address that is no pool reverts as the call into it does.
func (p PrecompileExecutor) viewPoolMarket(ctx sdk.Context, pool common.Address) (types.Market, error) {
	market, found, err := p.registeredPool(ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !found {
		return types.Market{}, fmt.Errorf("%w: %s is no pool", types.ErrUnknownMarket, pool.Hex())
	}
	return market, nil
}

func (p PrecompileExecutor) poolStats(ctx sdk.Context, pool common.Address) (QuoterPoolStats, error) {
	market, err := p.viewPoolMarket(ctx, pool)
	if err != nil {
		return QuoterPoolStats{}, err
	}
	feeBps, err := p.keeper.FeeBps(ctx, market)
	if err != nil {
		return QuoterPoolStats{}, err
	}
	age := ctx.BlockTime().Unix() - market.CreationTime
	if age < 0 {
		return QuoterPoolStats{}, fmt.Errorf("%w: pool %s created after the block time", types.ErrOverflow, pool.Hex())
	}
	price, err := market.Price()
	if err != nil {
		return QuoterPoolStats{}, err
	}
	marketCap := new(big.Int)
	if market.TokenReserve.IsPositive() {
		marketCap, err = types.GetMarketCap(market.EffectiveQuote(), market.TokenReserve.BigInt(),
			market.TotalSupply.BigInt())
		if err != nil {
			return QuoterPoolStats{}, err
		}
	}
	return QuoterPoolStats{VirtualUsdl: market.VirtualQuoteReserve.BigInt(), RealUsdl: market.RealQuoteBalance.BigInt(),
		TokenReserve: market.TokenReserve.BigInt(), CumulativeVolume: market.CumulativeVolume.BigInt(),
		CurrentFeeBps: feeBps, PoolAge: big.NewInt(age), MarketCap: marketCap, Price: price}, nil
}

// quotablePool is Quoter._validatePool: a registered pool without an
// optical that is not paused.
func (p PrecompileExecutor) quotablePool(ctx sdk.Context, pool common.Address) (types.Market, error) {
	market, found, err := p.registeredPool(ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !found {
		return types.Market{}, fmt.Errorf("%w: %s is not a registered pool", types.ErrUnexecutableQuote, pool.Hex())
	}
	if common.HexToAddress(market.Optical) != (common.Address{}) {
		return types.Market{}, fmt.Errorf("%w: pool %s", types.ErrOpticalQuoteUnsupported, pool.Hex())
	}
	if market.Paused {
		return types.Market{}, fmt.Errorf("%w: pool %s is paused", types.ErrUnexecutableQuote, pool.Hex())
	}
	return market, nil
}

type quotedLeg struct {
	amountOut *big.Int
	feeAmount *big.Int
	impactBps *big.Int
}

// quoteLeg is one swap of Quoter.quoteExactInput or one leg of
// quoteMultihop on market: the fee at the current fee rate, the
// constant-product output on the effective reserves and the price impact.
// The sell leg of quoteMultihop measures impact only while the effective
// reserve stays positive (multihopSell).
func (p PrecompileExecutor) quoteLeg(ctx sdk.Context, market types.Market, amountIn *big.Int, isBuy,
	multihopSell bool) (quotedLeg, error) {
	feeBps, err := p.keeper.FeeBps(ctx, market)
	if err != nil {
		return quotedLeg{}, err
	}
	fee, err := types.MulDiv(amountIn, feeBps, bpsDenominator)
	if err != nil {
		return quotedLeg{}, err
	}
	afterFee := new(big.Int).Sub(amountIn, fee)
	effective := market.EffectiveQuote()
	tokenReserve := market.TokenReserve.BigInt()
	var out *big.Int
	if isBuy {
		out, err = types.GetAmountOut(effective, tokenReserve, afterFee)
	} else {
		out, err = types.GetAmountOut(tokenReserve, effective, afterFee)
	}
	if err != nil {
		return quotedLeg{}, err
	}
	if !isBuy && out.Cmp(market.RealQuoteBalance.BigInt()) > 0 {
		return quotedLeg{}, fmt.Errorf("%w: output %s above the real USDL balance", types.ErrUnexecutableQuote, out)
	}
	if out.Sign() == 0 {
		return quotedLeg{}, fmt.Errorf("%w: zero output", types.ErrUnexecutableQuote)
	}
	leg := quotedLeg{amountOut: out, feeAmount: fee, impactBps: new(big.Int)}
	if tokenReserve.Sign() == 0 || effective.Sign() == 0 {
		return leg, nil
	}
	before, err := types.GetPrice(effective, tokenReserve)
	if err != nil || before.Sign() == 0 {
		return leg, err
	}
	var newEffective, newTokenReserve *big.Int
	if isBuy {
		newEffective, err = viewAdd(effective, afterFee)
		newTokenReserve = new(big.Int).Sub(tokenReserve, out)
	} else {
		newEffective = new(big.Int).Sub(effective, out)
		newTokenReserve, err = viewAdd(tokenReserve, amountIn)
	}
	if err != nil {
		return quotedLeg{}, err
	}
	if newTokenReserve.Sign() == 0 || (multihopSell && newEffective.Sign() == 0) {
		return leg, nil
	}
	after, err := types.GetPrice(newEffective, newTokenReserve)
	if err != nil {
		return quotedLeg{}, err
	}
	leg.impactBps, err = types.MulDiv(new(big.Int).Abs(new(big.Int).Sub(after, before)), bpsDenominator, before)
	if err != nil {
		return quotedLeg{}, err
	}
	return leg, nil
}

// quoteMultihop is Quoter.quoteMultihop: a sell of tokenIn into USDL on the
// pool of tokenIn, then a buy of tokenOut with that USDL on the pool of
// tokenOut.
func (p PrecompileExecutor) quoteMultihop(ctx sdk.Context, tokenIn, tokenOut common.Address,
	amountIn *big.Int) (QuoterMultihopResult, error) {
	if tokenIn == (common.Address{}) || tokenOut == (common.Address{}) || tokenIn == tokenOut {
		return QuoterMultihopResult{}, fmt.Errorf("%w: tokens %s and %s", types.ErrZeroAddress, tokenIn.Hex(),
			tokenOut.Hex())
	}
	poolA, poolB := p.poolByToken(ctx, tokenIn), p.poolByToken(ctx, tokenOut)
	if poolA == (common.Address{}) || poolB == (common.Address{}) {
		return QuoterMultihopResult{}, fmt.Errorf("%w: no pool for %s or %s", types.ErrZeroAddress, tokenIn.Hex(),
			tokenOut.Hex())
	}
	marketA, err := p.quotablePool(ctx, poolA)
	if err != nil {
		return QuoterMultihopResult{}, err
	}
	marketB, err := p.quotablePool(ctx, poolB)
	if err != nil {
		return QuoterMultihopResult{}, err
	}
	sell, err := p.quoteLeg(ctx, marketA, amountIn, false, true)
	if err != nil {
		return QuoterMultihopResult{}, err
	}
	buy, err := p.quoteLeg(ctx, marketB, sell.amountOut, true, false)
	if err != nil {
		return QuoterMultihopResult{}, err
	}
	combined, err := viewAdd(sell.impactBps, buy.impactBps)
	if err != nil {
		return QuoterMultihopResult{}, err
	}
	return QuoterMultihopResult{AmountOut: buy.amountOut, IntermediateUsdl: sell.amountOut,
		SellFeeAmount: sell.feeAmount, BuyFeeAmount: buy.feeAmount, SellPriceImpactBps: sell.impactBps,
		BuyPriceImpactBps: buy.impactBps, CombinedPriceImpactBps: combined, PoolA: poolA, PoolB: poolB}, nil
}

// viewAdd is checked uint256 addition.
func viewAdd(a, b *big.Int) (*big.Int, error) {
	sum := new(big.Int).Add(a, b)
	if sum.Cmp(types.MaxUint256()) > 0 {
		return nil, types.ErrOverflow
	}
	return sum, nil
}

// registryViews serves the PoolRegistry methods from the keeper market index:
// every generation 1 and generation 2 market is a registered pool, listed in
// market creation order.
func (p PrecompileExecutor) registryViews(c *call) ([]byte, error) {
	switch c.method.Name {
	case "FACTORY_ROLE":
		return c.method.Outputs.Pack([32]byte(FactoryRole))
	case "eventEmitter":
		return c.method.Outputs.Pack(p.keeper.Generation2().EventEmitter)
	case "getAllPools":
		pools, err := p.registryPools(c.ctx, nil)
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(pagePools(pools, c.args[0].(*big.Int), c.args[1].(*big.Int)))
	case "getPoolCount":
		pools, err := p.registryPools(c.ctx, nil)
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(big.NewInt(int64(len(pools))))
	case "getPoolsByCreator", "getPoolCountByCreator", "getPoolsByCreatorPage":
		creator := c.args[0].(common.Address)
		pools, err := p.registryPools(c.ctx, &creator)
		if err != nil {
			return nil, err
		}
		switch c.method.Name {
		case "getPoolsByCreator":
			return c.method.Outputs.Pack(pools)
		case "getPoolCountByCreator":
			return c.method.Outputs.Pack(big.NewInt(int64(len(pools))))
		default:
			return c.method.Outputs.Pack(pagePools(pools, c.args[1].(*big.Int), c.args[2].(*big.Int)))
		}
	case "getPoolByToken":
		return c.method.Outputs.Pack(p.poolByToken(c.ctx, c.args[0].(common.Address)))
	case "isRegisteredPool":
		_, found, err := p.registeredPool(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(found)
	case "getNftIdByPool":
		market, found, err := p.registeredPool(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		id := new(big.Int)
		if found {
			id.SetUint64(market.FeeRightID)
		}
		return c.method.Outputs.Pack(id)
	case "getPoolMetadata":
		metadata, err := p.poolMetadata(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(metadata)
	case "register":
		return nil, p.register(c)
	}
	return nil, fmt.Errorf("launchpad: the PoolRegistry role serves no %s", c.method.Sig)
}

// registeredPool is the market of pool when pool is a registered pool
// surface of any generation.
func (p PrecompileExecutor) registeredPool(ctx sdk.Context, pool common.Address) (types.Market, bool, error) {
	surface, found, err := p.keeper.GetSurface(ctx, pool)
	if err != nil || !found || surface.Role != types.RolePool {
		return types.Market{}, false, err
	}
	market, found := p.keeper.GetMarket(ctx, surface.Market)
	if !found {
		return types.Market{}, false, fmt.Errorf("%w: pool %s market %s", types.ErrUnknownMarket, pool.Hex(),
			surface.Market)
	}
	return market, true, nil
}

// registryPools lists the pool addresses in market creation order, only the
// pools of creator when creator is set.
// ponytail: scans every market per call; a creator- and pool-ordered index
// would page directly once the market count makes the scan costly.
func (p PrecompileExecutor) registryPools(ctx sdk.Context, creator *common.Address) ([]common.Address, error) {
	pools := []common.Address{}
	var err error
	p.keeper.IterateMarkets(ctx, func(market types.Market) bool {
		if market.Generation == types.GenerationNative {
			return false
		}
		if creator != nil {
			var owner common.Address
			if owner, err = p.marketCreator(ctx, market); err != nil {
				return true
			}
			if owner != *creator {
				return false
			}
		}
		pools = append(pools, common.HexToAddress(market.PoolAddress))
		return false
	})
	if err != nil {
		return nil, err
	}
	return pools, nil
}

// marketCreator is the EVM address of the creator account of market.
func (p PrecompileExecutor) marketCreator(ctx sdk.Context, market types.Market) (common.Address, error) {
	account, err := sdk.AccAddressFromBech32(market.Creator)
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: market %s creator: %v", types.ErrInvalidMarket, market.Denom, err)
	}
	return p.evmKeeper.GetEVMAddressOrDefault(ctx, account), nil
}

// poolByToken is PoolRegistry.getPoolByToken: the pool of the market whose
// pool token is token, or the zero address.
func (p PrecompileExecutor) poolByToken(ctx sdk.Context, token common.Address) common.Address {
	pool := common.Address{}
	if token == (common.Address{}) {
		return pool
	}
	p.keeper.IterateMarkets(ctx, func(market types.Market) bool {
		if market.Generation != types.GenerationNative && common.HexToAddress(market.TokenAddress) == token {
			pool = common.HexToAddress(market.PoolAddress)
			return true
		}
		return false
	})
	return pool
}

// pagePools is the getAllPools and getPoolsByCreatorPage window: empty from
// an offset at or past the end, otherwise at most limit pools.
func pagePools(pools []common.Address, offset, limit *big.Int) []common.Address {
	total := big.NewInt(int64(len(pools)))
	if offset.Cmp(total) >= 0 {
		return []common.Address{}
	}
	count := new(big.Int).Sub(total, offset)
	if limit.Cmp(count) < 0 {
		count = limit
	}
	start := offset.Int64()
	return pools[start : start+count.Int64()]
}

// poolMetadata is PoolRegistry.getPoolMetadata: the registration record of a
// registered pool, the zero tuple for any other address.
func (p PrecompileExecutor) poolMetadata(ctx sdk.Context, pool common.Address) (RegistryPoolMetadata, error) {
	empty := RegistryPoolMetadata{NftId: new(big.Int), CreatedAt: new(big.Int), CreatedBlock: new(big.Int)}
	_, registered, err := p.registeredPool(ctx, pool)
	if err != nil || !registered {
		return empty, err
	}
	metadata, found, err := p.keeper.GetPoolMetadata(ctx, pool)
	if err != nil {
		return empty, err
	}
	if !found {
		return empty, fmt.Errorf("%w: registered pool %s has no registry metadata", types.ErrInvalidMarket, pool.Hex())
	}
	return RegistryPoolMetadata{Creator: metadata.Creator, Token: metadata.Token, Optical: metadata.Optical,
		NftId: new(big.Int).SetUint64(metadata.NFTID), CreatedAt: big.NewInt(metadata.CreatedAt),
		CreatedBlock: big.NewInt(metadata.CreatedBlock)}, nil
}

// register is PoolRegistry.register: onlyRole(FACTORY_ROLE) first, then the
// argument and duplicate checks of the contract. The keeper registers pools
// only inside CreateMarket and exposes no registration of a pool without a
// market, so a FACTORY_ROLE holder's call that passes every check reverts.
func (p PrecompileExecutor) register(c *call) error {
	if err := p.keeper.CheckRole(c.ctx, p.keeper.Generation2().PoolRegistry, FactoryRole, c.caller); err != nil {
		return err
	}
	pool, token, creator := c.args[0].(common.Address), c.args[1].(common.Address), c.args[2].(common.Address)
	if pool == (common.Address{}) || token == (common.Address{}) || creator == (common.Address{}) {
		return fmt.Errorf("%w: register pool %s token %s creator %s", types.ErrZeroAddress, pool.Hex(), token.Hex(),
			creator.Hex())
	}
	if p.poolByToken(c.ctx, token) != (common.Address{}) {
		return fmt.Errorf("%w: token %s", types.ErrDuplicateToken, token.Hex())
	}
	if _, registered, err := p.registeredPool(c.ctx, pool); err != nil {
		return err
	} else if registered {
		return fmt.Errorf("%w: pool %s", types.ErrDuplicatePool, pool.Hex())
	}
	return fmt.Errorf("%w: register of pool %s outside market creation has no keeper entry point",
		types.ErrInvalidConfiguration, pool.Hex())
}

// configViews serves the ProtocolConfig getters from the one launchpad Params
// set and refuses every setter, all DEFAULT_ADMIN_ROLE methods (ruling 7).
func (p PrecompileExecutor) configViews(c *call) ([]byte, error) {
	params := p.keeper.GetParams(c.ctx)
	u := func(value uint64) *big.Int { return new(big.Int).SetUint64(value) }
	switch c.method.Name {
	case "usdlAddress":
		return c.method.Outputs.Pack(common.HexToAddress(params.QuoteToken))
	case "virtualUsdlDefault":
		return c.method.Outputs.Pack(params.VirtualQuoteDefault.BigInt())
	case "virtualTokenDefault":
		return c.method.Outputs.Pack(params.VirtualTokenDefault.BigInt())
	case "minFeeBps":
		return c.method.Outputs.Pack(u(params.MinFeeBps))
	case "maxFeeBps":
		return c.method.Outputs.Pack(u(params.MaxFeeBps))
	case "baseFeeBps":
		return c.method.Outputs.Pack(u(params.BaseFeeBps))
	case "protocolFeeBps":
		return c.method.Outputs.Pack(u(params.ProtocolFeeBps))
	case "feeDecayRate":
		return c.method.Outputs.Pack(u(params.FeeDecayRate))
	case "volatilityWeight":
		return c.method.Outputs.Pack(u(params.VolatilityWeight))
	case "concentrationWeight":
		return c.method.Outputs.Pack(u(params.ConcentrationWeight))
	case "creationFee":
		return c.method.Outputs.Pack(params.CreationFee.BigInt())
	case "eventEmitter":
		return c.method.Outputs.Pack(p.keeper.Generation2().EventEmitter)
	case "setBaseFeeBps", "setCreationFee", "setFeeWeights", "setProtocolFeeBps", "setVirtualDefaults":
		return nil, launchpadkeeper.AdminRefusal(c.caller)
	}
	return nil, fmt.Errorf("launchpad: the ProtocolConfig role serves no %s", c.method.Sig)
}
