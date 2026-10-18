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
	"github.com/ethereum/go-ethereum/crypto"
)

// FeeAccumulator role constants (Launch-x:contracts/contracts/data/FeeAccumulator.sol:32-37).
var (
	feePoolRole           = [32]byte(crypto.Keccak256Hash([]byte("POOL_ROLE")))
	feeFactoryRole        = [32]byte(crypto.Keccak256Hash([]byte("FACTORY_ROLE")))
	feeOpticalClaimRole   = [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_CLAIM_ROLE")))
	feeOpticalGranterRole = [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_GRANTER_ROLE")))
)

var (
	feeTokenAddressCall   = roleABIs[types.RolePool].Methods["tokenAddress"].ID
	feePastTotalSupply    = roleABIs["SidioraERC20"].Methods["getPastTotalSupply"]
	feePastBalance        = roleABIs["SidioraERC20"].Methods["getPastBalance"]
	feeDeadAddress        = common.HexToAddress(types.DeadAddress)
	feeStrategyOfEntry    = map[string]types.FeeStrategy{"claimFees": types.FeeStrategyClaim, "executeBurn": types.FeeStrategyBurn, "executeAirdrop": types.FeeStrategyAirdrop, "executeLpRewards": types.FeeStrategyLpRewards}
	feeAccumulatorEntries = map[string]bool{"recordFee": true, "recordOpticalSurplus": true, "triggerAirdrop": true,
		"claimAirdrop": true, "claimAirdropEpoch": true, "claimLegacyAirdrop": true, "claimOpticalSurplus": true}
)

// feeRoleChecks is the onlyRole modifier of every role-gated write of the
// two roles, checked against the migrated role table of the role's
// generation 2 address before any other check: an account outside the table
// reverts MissingRole(account, role), and every DEFAULT_ADMIN_ROLE write
// reverts AccessControlUnauthorizedAccount(account, 0x00) under ruling 7
// (Launch-x:contracts/contracts/base/AccessControl.sol:73).
var feeRoleChecks = map[string]map[string][32]byte{
	types.RoleFeesRouter: {
		"grantRole": launchpadkeeper.DefaultAdminRole, "revokeRole": launchpadkeeper.DefaultAdminRole,
		"upgradeToAndCall": launchpadkeeper.DefaultAdminRole,
	},
	types.RoleFeeAccumulator: {
		"recordFee": feePoolRole, "recordOpticalSurplus": feePoolRole,
		"claim": launchpadkeeper.FeesRouterRole, "burn": launchpadkeeper.FeesRouterRole,
		"triggerAirdrop": launchpadkeeper.FeesRouterRole, "sendLpRewards": launchpadkeeper.FeesRouterRole,
		"authorizeOptical": feeOpticalGranterRole, "authorizePool": feeFactoryRole,
		"allocateLegacyAirdrop": launchpadkeeper.DefaultAdminRole, "migrateAccounting": launchpadkeeper.DefaultAdminRole,
		"migrateAdditionalAccounting": launchpadkeeper.DefaultAdminRole, "grantRole": launchpadkeeper.DefaultAdminRole,
		"revokeRole": launchpadkeeper.DefaultAdminRole, "upgradeToAndCall": launchpadkeeper.DefaultAdminRole,
	},
}

func init() {
	roleHandlers[types.RoleFeesRouter] = feeHandler
	roleHandlers[types.RoleFeeAccumulator] = feeHandler
}

// feeHandler serves FeesRouter and FeeAccumulator over the FeeAccumulator
// ledger of the launchpad keeper: the role check, then the AccessControl and
// UUPS functions both roles share, then the role's own functions.
func feeHandler(p PrecompileExecutor, c *call) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(c.args, len(c.method.Inputs)); err != nil {
		return nil, err
	}
	contract := feeContract(p, c.role)
	if role, gated := feeRoleChecks[c.role][c.method.Name]; gated {
		if err := p.keeper.CheckRole(c.ctx, contract, role, c.caller); err != nil {
			return nil, err
		}
	}
	switch c.method.Name {
	case "DEFAULT_ADMIN_ROLE":
		return c.method.Outputs.Pack(launchpadkeeper.DefaultAdminRole)
	case "hasRole":
		return c.method.Outputs.Pack(p.keeper.HasRole(c.ctx, contract, c.args[0].([32]byte), c.args[1].(common.Address)))
	case "getRoleAdmin":
		return c.method.Outputs.Pack(p.keeper.RoleAdmin(c.ctx, contract, c.args[0].([32]byte)))
	case "registeredDefaultAdminCount":
		return c.method.Outputs.Pack(new(big.Int).SetUint64(p.keeper.RegisteredDefaultAdminCount(c.ctx, contract)))
	case "renounceRole":
		return feeRenounce(p, c, contract)
	case "initialize":
		return nil, types.ErrAlreadyInitialized
	case "proxiableUUID":
		return nil, types.ErrUUPSNotThroughActiveProxy
	}
	if c.role == types.RoleFeesRouter {
		return feesRouter(p, c, contract)
	}
	return feeAccumulator(p, c, contract)
}

// feeContract is the generation 2 address of role, whose role table and logs
// the call uses whether it came through that surface or to 0x1017.
func feeContract(p PrecompileExecutor, role string) common.Address {
	if role == types.RoleFeesRouter {
		return p.keeper.Generation2().FeesRouter
	}
	return p.keeper.Generation2().FeeAccumulator
}

func feeRenounce(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	role := c.args[0].([32]byte)
	held, err := p.keeper.RenounceRole(c.ctx, contract, role, c.caller, c.args[1].(common.Address))
	if err != nil {
		return nil, err
	}
	if held {
		if err := feeLog(c, contract, c.role, "RoleRevoked", common.Hash(role), c.caller, c.caller); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// feesRouter serves the FeesRouter functions
// (Launch-x:contracts/contracts/periphery/FeesRouter.sol).
func feesRouter(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	roles := p.keeper.Generation2()
	switch c.method.Name {
	case "nftContract":
		return c.method.Outputs.Pack(roles.SidioraNFT)
	case "feeAccumulator":
		return c.method.Outputs.Pack(roles.FeeAccumulator)
	case "poolRegistry":
		return c.method.Outputs.Pack(roles.PoolRegistry)
	case "setFeeStrategy":
		return feeSetStrategy(p, c, contract)
	case "claimFees", "executeBurn", "executeAirdrop", "executeLpRewards", "claimAirdrop", "claimAirdropEpoch":
		return feeRoute(p, c, contract)
	}
	return nil, fmt.Errorf("launchpad: %s serves no %s", c.role, c.method.Sig)
}

// feeRight is _requireNftOwner and _requireStrategy of FeesRouter: caller owns
// token nftID of the generation 2 fee-right collection, an id that is no
// token refusing the same way, and the token carries strategy.
func feeRight(p PrecompileExecutor, c *call, nftID *big.Int, strategy types.FeeStrategy) (types.FeeRight, error) {
	if !nftID.IsUint64() {
		return types.FeeRight{}, types.ErrNotFeeRightsHolder
	}
	right, err := p.keeper.RequireFeeRightHolder(c.ctx, p.keeper.Generation2().SidioraNFT, c.caller, nftID.Uint64())
	if err != nil {
		return types.FeeRight{}, err
	}
	if right.Strategy != strategy {
		return types.FeeRight{}, types.ErrWrongStrategy
	}
	return right, nil
}

// feeRightPool is SidioraNFT.getPoolAddress(nftID), which reverts
// TokenNotFound for an id that is no token.
func feeRightPool(p PrecompileExecutor, c *call, nftID *big.Int) (common.Address, error) {
	if !nftID.IsUint64() {
		return common.Address{}, types.ErrTokenNotFound
	}
	right, found, err := p.keeper.GetFeeRight(c.ctx, p.keeper.Generation2().SidioraNFT, nftID.Uint64())
	if err != nil {
		return common.Address{}, err
	}
	if !found {
		return common.Address{}, types.ErrTokenNotFound
	}
	return common.HexToAddress(right.Pool), nil
}

// feePoolMarket is the market of the pool surface pool.
func feePoolMarket(p PrecompileExecutor, ctx sdk.Context, pool common.Address) (types.Market, bool, error) {
	surface, found, err := p.keeper.GetSurface(ctx, pool)
	if err != nil {
		return types.Market{}, false, err
	}
	if !found || surface.Role != types.RolePool {
		return types.Market{}, false, nil
	}
	market, found := p.keeper.GetMarket(ctx, surface.Market)
	return market, found, nil
}

// feeRoute serves the nonReentrant FeesRouter entry points: the FeesRouter
// lock, the owner and strategy checks of the strategy executions, then the
// FeeAccumulator call under the FeeAccumulator lock. The FeeAccumulator log
// precedes the FeesRouter log, as the inner call emits first.
func feeRoute(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	release, err := p.keeper.EnterLock(c.evm.StateDB, launchpadkeeper.FeesRouterLock)
	if err != nil {
		return nil, err
	}
	defer release()
	nftID := c.args[0].(*big.Int)
	var pool common.Address
	if strategy, execution := feeStrategyOfEntry[c.method.Name]; execution {
		right, err := feeRight(p, c, nftID, strategy)
		if err != nil {
			return nil, err
		}
		pool = common.HexToAddress(right.Pool)
	} else if pool, err = feeRightPool(p, c, nftID); err != nil {
		return nil, err
	}
	market, found, err := feePoolMarket(p, c.ctx, pool)
	if err != nil {
		return nil, err
	}
	accumulator := p.keeper.Generation2().FeeAccumulator
	releaseAccumulator, err := p.keeper.EnterLock(c.evm.StateDB, launchpadkeeper.FeeAccumulatorLock)
	if err != nil {
		return nil, err
	}
	defer releaseAccumulator()

	var amount sdk.Int
	switch c.method.Name {
	case "claimFees", "executeBurn", "executeLpRewards":
		if !found {
			return nil, types.ErrNoFeesAccumulated
		}
		switch c.method.Name {
		case "claimFees":
			c.ctx, amount, err = p.keeper.Claim(c.ctx, c.nested, c.caller, market.Denom, c.caller)
			if err == nil {
				err = feeLog(c, accumulator, types.RoleFeeAccumulator, "FeesClaimed", pool, c.caller, amount.BigInt())
			}
			if err == nil {
				err = feeLog(c, contract, c.role, "FeesClaimed", nftID, c.caller, amount.BigInt())
			}
		case "executeBurn":
			c.ctx, amount, err = p.keeper.Burn(c.ctx, c.nested, c.caller, market.Denom)
			if err == nil {
				err = feeLog(c, accumulator, types.RoleFeeAccumulator, "FeesBurned", pool, amount.BigInt())
			}
			if err == nil {
				err = feeLog(c, contract, c.role, "FeesBurned", nftID, amount.BigInt())
			}
		default:
			amount, err = p.keeper.ExecuteLpRewards(c.ctx, p.evmKeeper.GetPaxAddressOrDefault(c.ctx, c.caller), market.Denom)
			if err == nil {
				err = feeLog(c, accumulator, types.RoleFeeAccumulator, "LpRewardsSent", pool, amount.BigInt())
			}
			if err == nil {
				err = feeLog(c, contract, c.role, "LpRewardsExecuted", nftID, amount.BigInt())
			}
		}
	case "executeAirdrop":
		amount, err = p.keeper.TriggerAirdrop(c.ctx, c.nested, contract, pool)
		if err == nil {
			err = feeAirdropTriggered(p, c, pool, amount)
		}
		if err == nil {
			err = feeLog(c, contract, c.role, "AirdropExecuted", nftID, amount.BigInt())
		}
	default:
		epoch := market.AirdropEpoch
		if c.method.Name == "claimAirdropEpoch" {
			requested := c.args[1].(*big.Int)
			if !requested.IsUint64() {
				return nil, types.ErrAirdropNotTriggered
			}
			epoch = requested.Uint64()
		}
		amount, err = feeClaimAirdrop(p, c, pool, c.caller, epoch)
		if err == nil {
			err = feeLog(c, contract, c.role, "AirdropClaimed", nftID, c.caller, amount.BigInt())
		}
	}
	if err != nil {
		return nil, err
	}
	return c.method.Outputs.Pack(amount.BigInt())
}

// feeSetStrategy is FeesRouter.setFeeStrategy, which is not nonReentrant:
// the owner check, the airdrop support of strategy 2, then
// SidioraNFT.setFeeStrategy sent by the FeesRouter, through the market when
// the token is its market's fee right so the market copy follows.
func feeSetStrategy(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	nftID, strategy := c.args[0].(*big.Int), types.FeeStrategy(c.args[1].(uint8))
	roles := p.keeper.Generation2()
	if !nftID.IsUint64() {
		return nil, types.ErrNotFeeRightsHolder
	}
	id := nftID.Uint64()
	right, err := p.keeper.RequireFeeRightHolder(c.ctx, roles.SidioraNFT, c.caller, id)
	if err != nil {
		return nil, err
	}
	pool := common.HexToAddress(right.Pool)
	if strategy == types.FeeStrategyAirdrop {
		supported, err := feeSupportsAirdrop(p, c, pool)
		if err != nil {
			return nil, err
		}
		if !supported {
			return nil, types.ErrUnsupportedSnapshotToken
		}
	}
	old := right.Strategy
	market, found, err := feePoolMarket(p, c.ctx, pool)
	if err != nil {
		return nil, err
	}
	if found && common.IsHexAddress(market.FeeRightCollection) &&
		common.HexToAddress(market.FeeRightCollection) == roles.SidioraNFT && market.FeeRightID == id {
		err = p.keeper.SetFeeStrategy(c.ctx, p.evmKeeper.GetPaxAddressOrDefault(c.ctx, c.caller), market.Denom, strategy)
	} else {
		_, err = p.keeper.SetFeeRightStrategy(c.ctx, roles.SidioraNFT, contract, id, strategy)
	}
	if err != nil {
		return nil, err
	}
	if err := feeLog(c, roles.SidioraNFT, types.RoleFeeRights, "FeeStrategyChanged", nftID, uint8(old),
		uint8(strategy)); err != nil {
		return nil, err
	}
	if p.familyEnabled() {
		if err := p.emitFamily(c.evm, "FeeStrategyChanged", poolID(pool), nftID, uint8(old), uint8(strategy)); err != nil {
			return nil, err
		}
	}
	return nil, feeLog(c, contract, c.role, "FeeStrategyChanged", nftID, uint8(old), uint8(strategy))
}

// feeAccumulator serves the FeeAccumulator functions
// (Launch-x:contracts/contracts/data/FeeAccumulator.sol).
func feeAccumulator(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	roles := p.keeper.Generation2()
	switch c.method.Name {
	case "DEAD_ADDRESS":
		return c.method.Outputs.Pack(feeDeadAddress)
	case "POOL_ROLE":
		return c.method.Outputs.Pack(feePoolRole)
	case "FEES_ROUTER_ROLE":
		return c.method.Outputs.Pack(launchpadkeeper.FeesRouterRole)
	case "FACTORY_ROLE":
		return c.method.Outputs.Pack(feeFactoryRole)
	case "OPTICAL_CLAIM_ROLE":
		return c.method.Outputs.Pack(feeOpticalClaimRole)
	case "OPTICAL_GRANTER_ROLE":
		return c.method.Outputs.Pack(feeOpticalGranterRole)
	case "protocolConfig":
		return c.method.Outputs.Pack(roles.ProtocolConfig)
	case "treasury":
		return c.method.Outputs.Pack(roles.Treasury)
	case "poolRegistry":
		return c.method.Outputs.Pack(roles.PoolRegistry)
	case "eventEmitter":
		return c.method.Outputs.Pack(roles.EventEmitter)
	case "usdlAddress":
		return c.method.Outputs.Pack(common.HexToAddress(p.keeper.GetParams(c.ctx).QuoteToken))
	case "totalLiabilities":
		total, err := p.keeper.TotalLiabilities(c.ctx)
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(total.BigInt())
	case "getOpticalSurplus":
		surplus, err := p.keeper.OpticalSurplus(c.ctx, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(surplus.BigInt())
	case "getAccumulatedFees", "getAirdropBalance", "getAirdropEpoch":
		return feeMarketView(p, c)
	case "airdropSnapshotBlock", "airdropEligibleSupply", "airdropEpochRemaining", "airdropClaimedSupply",
		"legacyAirdropRoot", "legacyAirdropRemaining":
		return feeEpochView(p, c)
	case "hasClaimedAirdrop":
		epoch := c.args[2].(*big.Int)
		return c.method.Outputs.Pack(epoch.IsUint64() && p.keeper.HasClaimedPoolAirdrop(c.ctx,
			c.args[0].(common.Address), c.args[1].(common.Address), epoch.Uint64()))
	case "supportsAirdrop":
		supported, err := feeSupportsAirdrop(p, c, c.args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(supported)
	case "accountingInitialized", "accountingMigrationCursor", "getProtocolFeesPending", "getLpRewardsBalance",
		"legacyAirdropUnallocated":
		return nil, fmt.Errorf("%w: FeeAccumulator.%s has no keeper record", types.ErrInvalidConfiguration,
			c.method.Name)
	case "claim", "burn", "sendLpRewards":
		return nil, fmt.Errorf("%w: FeeAccumulator.%s has no pool-keyed keeper entry for a role holder",
			types.ErrInvalidConfiguration, c.method.Name)
	case "authorizeOptical":
		return feeGrant(p, c, contract, feeOpticalClaimRole)
	case "authorizePool":
		return feeGrant(p, c, contract, feePoolRole)
	}
	if feeAccumulatorEntries[c.method.Name] {
		return feeAccumulatorEntry(p, c, contract)
	}
	return nil, fmt.Errorf("launchpad: %s serves no %s", c.role, c.method.Sig)
}

func feeMarketView(p PrecompileExecutor, c *call) ([]byte, error) {
	market, found, err := feePoolMarket(p, c.ctx, c.args[0].(common.Address))
	if err != nil {
		return nil, err
	}
	value := new(big.Int)
	if found {
		switch c.method.Name {
		case "getAccumulatedFees":
			value = feeInt(market.AccumulatedFees)
		case "getAirdropBalance":
			value = feeInt(market.AirdropBalance)
		default:
			value.SetUint64(market.AirdropEpoch)
		}
	}
	return c.method.Outputs.Pack(value)
}

func feeEpochView(p PrecompileExecutor, c *call) ([]byte, error) {
	var record types.PoolAirdropEpoch
	if epoch := c.args[1].(*big.Int); epoch.IsUint64() {
		var err error
		record, _, err = p.keeper.GetPoolAirdropEpoch(c.ctx, c.args[0].(common.Address), epoch.Uint64())
		if err != nil {
			return nil, err
		}
	}
	switch c.method.Name {
	case "airdropSnapshotBlock":
		return c.method.Outputs.Pack(new(big.Int).SetUint64(record.SnapshotBlock))
	case "airdropEligibleSupply":
		return c.method.Outputs.Pack(feeInt(record.EligibleSupply))
	case "airdropEpochRemaining":
		return c.method.Outputs.Pack(feeInt(record.Remaining))
	case "airdropClaimedSupply":
		return c.method.Outputs.Pack(feeInt(record.ClaimedSupply))
	case "legacyAirdropRoot":
		var root [32]byte
		if record.LegacyRoot != "" {
			root = common.HexToHash(record.LegacyRoot)
		}
		return c.method.Outputs.Pack(root)
	default:
		return c.method.Outputs.Pack(feeInt(record.LegacyRemaining))
	}
}

// feeGrant is authorizePool and authorizeOptical: _grantRole(role, account),
// with RoleGranted(role, account, sender) when the account did not hold it.
func feeGrant(p PrecompileExecutor, c *call, contract common.Address, role [32]byte) ([]byte, error) {
	account := c.args[0].(common.Address)
	granted, err := p.keeper.GovernanceGrant(c.ctx, p.keeper.Authority(), contract, role, account)
	if err != nil {
		return nil, err
	}
	if granted {
		if err := feeLog(c, contract, c.role, "RoleGranted", common.Hash(role), account, c.caller); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// feeAccumulatorEntry serves the nonReentrant FeeAccumulator writes under the
// FeeAccumulator lock, taken after the role check of feeHandler.
func feeAccumulatorEntry(p PrecompileExecutor, c *call, contract common.Address) ([]byte, error) {
	release, err := p.keeper.EnterLock(c.evm.StateDB, launchpadkeeper.FeeAccumulatorLock)
	if err != nil {
		return nil, err
	}
	defer release()
	pool := c.args[0].(common.Address)
	switch c.method.Name {
	case "recordFee":
		return nil, feeRecord(p, c, contract, pool)
	case "recordOpticalSurplus":
		market, err := feeRequirePool(p, c, pool)
		if err != nil {
			return nil, err
		}
		amount := c.args[1].(*big.Int)
		if err := p.keeper.RecordOpticalSurplus(c.ctx, market.Denom, sdk.NewIntFromBigInt(amount)); err != nil {
			return nil, err
		}
		if amount.Sign() == 0 {
			return nil, nil
		}
		return nil, feeLog(c, contract, c.role, "OpticalSurplusRecorded", pool, amount)
	case "triggerAirdrop":
		amount, err := p.keeper.TriggerAirdrop(c.ctx, c.nested, c.caller, pool)
		if err != nil {
			return nil, err
		}
		if err := feeAirdropTriggered(p, c, pool, amount); err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(amount.BigInt())
	case "claimAirdrop", "claimAirdropEpoch":
		var epoch uint64
		if c.method.Name == "claimAirdrop" {
			market, found, err := feePoolMarket(p, c.ctx, pool)
			if err != nil {
				return nil, err
			}
			if found {
				epoch = market.AirdropEpoch
			}
		} else {
			requested := c.args[2].(*big.Int)
			if !requested.IsUint64() {
				return nil, types.ErrAirdropNotTriggered
			}
			epoch = requested.Uint64()
		}
		amount, err := feeClaimAirdrop(p, c, pool, c.args[1].(common.Address), epoch)
		if err != nil {
			return nil, err
		}
		return c.method.Outputs.Pack(amount.BigInt())
	case "claimLegacyAirdrop":
		return nil, feeClaimLegacy(p, c, pool)
	default:
		return nil, feeClaimSurplus(p, c, contract, pool)
	}
}

// feeRequirePool is _requirePool: the caller is pool, pool holds code and is
// a registered pool.
func feeRequirePool(p PrecompileExecutor, c *call, pool common.Address) (types.Market, error) {
	if c.caller != pool || len(c.evm.StateDB.GetCode(pool)) == 0 {
		return types.Market{}, types.ErrUnauthorized
	}
	market, found, err := feePoolMarket(p, c.ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !found {
		return types.Market{}, types.ErrUnauthorized
	}
	return market, nil
}

// feeRecord is recordFee: OpticalSurplusRecorded for the optical diversion,
// Treasury Deposited for the protocol cut, then FeeRecorded.
func feeRecord(p PrecompileExecutor, c *call, contract, pool common.Address) error {
	market, err := feeRequirePool(p, c, pool)
	if err != nil {
		return err
	}
	fee := sdk.NewIntFromBigInt(c.args[1].(*big.Int))
	ctx, protocolCut, poolCut, err := p.keeper.RecordFee(c.ctx, c.nested, market.Denom, fee)
	if err != nil {
		return err
	}
	c.ctx = ctx
	if divert := fee.Sub(protocolCut).Sub(poolCut); divert.IsPositive() {
		if err := feeLog(c, contract, c.role, "OpticalSurplusRecorded", pool, divert.BigInt()); err != nil {
			return err
		}
	}
	if protocolCut.IsPositive() {
		usdl := common.HexToAddress(p.keeper.GetParams(c.ctx).QuoteToken)
		if err := feeLog(c, p.keeper.Generation2().Treasury, types.RoleTreasury, "Deposited", usdl, contract,
			protocolCut.BigInt()); err != nil {
			return err
		}
	}
	return feeLog(c, contract, c.role, "FeeRecorded", pool, fee.BigInt(), protocolCut.BigInt(), poolCut.BigInt())
}

// feeAirdropTriggered logs AirdropTriggered(pool, amount, epoch) for the
// epoch triggerAirdrop opened.
func feeAirdropTriggered(p PrecompileExecutor, c *call, pool common.Address, amount sdk.Int) error {
	market, found, err := feePoolMarket(p, c.ctx, pool)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: pool %s lost its market", types.ErrInvalidConfiguration, pool.Hex())
	}
	return feeLog(c, p.keeper.Generation2().FeeAccumulator, types.RoleFeeAccumulator, "AirdropTriggered", pool,
		amount.BigInt(), new(big.Int).SetUint64(market.AirdropEpoch))
}

// feeClaimAirdrop is _claimAirdrop(pool, holder, epoch) with its
// AirdropClaimed log.
func feeClaimAirdrop(p PrecompileExecutor, c *call, pool, holder common.Address, epoch uint64) (sdk.Int, error) {
	ctx, amount, err := p.keeper.ClaimAirdropEpoch(c.ctx, c.nested, pool, holder, epoch)
	if err != nil {
		return sdk.Int{}, err
	}
	c.ctx = ctx
	return amount, feeLog(c, p.keeper.Generation2().FeeAccumulator, types.RoleFeeAccumulator, "AirdropClaimed", pool,
		holder, amount.BigInt(), new(big.Int).SetUint64(epoch))
}

// feeClaimLegacy is claimLegacyAirdrop(pool, epoch, holder, amount, proof);
// an epoch beyond uint64 has no root, so its proof cannot match.
func feeClaimLegacy(p PrecompileExecutor, c *call, pool common.Address) error {
	epoch, holder, amount := c.args[1].(*big.Int), c.args[2].(common.Address), c.args[3].(*big.Int)
	if !epoch.IsUint64() {
		return types.ErrUnauthorized
	}
	words := c.args[4].([][32]byte)
	proof := make([]common.Hash, len(words))
	for i, word := range words {
		proof[i] = word
	}
	ctx, err := p.keeper.ClaimLegacyAirdrop(c.ctx, c.nested, pool, epoch.Uint64(), holder,
		sdk.NewIntFromBigInt(amount), proof)
	if err != nil {
		return err
	}
	c.ctx = ctx
	return feeLog(c, p.keeper.Generation2().FeeAccumulator, types.RoleFeeAccumulator, "AirdropClaimed", pool, holder,
		amount, epoch)
}

// feeClaimSurplus is claimOpticalSurplus(pool, amount, recipient): only the
// optical of a registered pool may claim.
func feeClaimSurplus(p PrecompileExecutor, c *call, contract, pool common.Address) error {
	market, found, err := feePoolMarket(p, c.ctx, pool)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrUnauthorized
	}
	amount, recipient := c.args[1].(*big.Int), c.args[2].(common.Address)
	ctx, err := p.keeper.ClaimOpticalSurplus(c.ctx, c.nested, c.caller, market.Denom, sdk.NewIntFromBigInt(amount),
		recipient)
	if err != nil {
		return err
	}
	c.ctx = ctx
	return feeLog(c, contract, c.role, "OpticalSurplusClaimed", pool, recipient, amount)
}

// feeSupportsAirdrop is supportsAirdrop(pool): at a block above zero the
// pool's tokenAddress() names a contract answering getPastTotalSupply and
// getPastBalance at block.number-1 with one word each. A tokenAddress word
// that is no address reverts, as abi.decode does.
func feeSupportsAirdrop(p PrecompileExecutor, c *call, pool common.Address) (bool, error) {
	if c.ctx.BlockHeight() <= 0 {
		return false, nil
	}
	nested, accumulator := c.nested, p.keeper.Generation2().FeeAccumulator
	ret, err := nested.StaticCall(c.ctx, accumulator, pool, feeTokenAddressCall)
	if err != nil || len(ret) != 32 {
		return false, nil
	}
	if new(big.Int).SetBytes(ret).BitLen() > 8*common.AddressLength {
		return false, fmt.Errorf("launchpad: tokenAddress() of %s is not an address word", pool.Hex())
	}
	token := common.BytesToAddress(ret)
	if len(c.evm.StateDB.GetCode(token)) == 0 {
		return false, nil
	}
	snapshot := new(big.Int).SetInt64(c.ctx.BlockHeight() - 1)
	for _, probe := range []struct {
		method *abi.Method
		args   []interface{}
	}{{&feePastTotalSupply, []interface{}{snapshot}}, {&feePastBalance, []interface{}{pool, snapshot}}} {
		packed, err := probe.method.Inputs.Pack(probe.args...)
		if err != nil {
			return false, err
		}
		ret, err := nested.StaticCall(c.ctx, accumulator, token, append(common.CopyBytes(probe.method.ID), packed...))
		if err != nil || len(ret) != 32 {
			return false, nil
		}
	}
	return true, nil
}

// feeLog emits event name of role's ABI at address, splitting args into the
// indexed topics and the data in the event's input order.
func feeLog(c *call, address common.Address, role, name string, args ...interface{}) error {
	event, ok := roleABIs[role].Events[name]
	if !ok {
		return fmt.Errorf("launchpad: %s declares no event %s", role, name)
	}
	if len(args) != len(event.Inputs) {
		return fmt.Errorf("launchpad: %s.%s takes %d arguments, got %d", role, name, len(event.Inputs), len(args))
	}
	topics := []common.Hash{event.ID}
	var data []interface{}
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, args[i])
			continue
		}
		topic, err := abi.MakeTopics([]interface{}{args[i]})
		if err != nil {
			return err
		}
		topics = append(topics, topic[0][0])
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(c.evm, address, topics, packed)
}

func feeInt(value sdk.Int) *big.Int {
	if value.IsNil() {
		return new(big.Int)
	}
	return value.BigInt()
}
