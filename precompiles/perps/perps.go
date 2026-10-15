// Package perps is the perpetuals precompile served at 0x1015 from the v6.12.0
// activation height. Every method is nonpayable, charged from the fixed gas
// table of contracts/perps.md section 21, and fails with the 4-byte selector
// of its parameterless custom error as the whole revert data.
package perps

import (
	"fmt"
	"math/big"

	exchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	// PerpsAddress is the address the venue serves from activation, the
	// address of the v6.9 exchange precompile below it.
	PerpsAddress   = exchangetypes.ExchangeAddress
	PrecompileName = "perps"

	// TokenCallGas caps the nested USDL ERC20 call of a token leg; every
	// method with a token leg carries it in its surcharge.
	TokenCallGas uint64 = 100_000
)

// PerpsKeepers is implemented by application keepers that carry the perps
// keeper. Keepers without it leave the precompile refusing every call.
type PerpsKeepers interface {
	PerpsK() *perpskeeper.Keeper
}

// GasTable is the fixed charge of every method of contracts/perps.md section
// 21, keyed by the go-ethereum overload names of the ABI.
var GasTable = pcommon.GasTable{
	"createVault":             {Write: true, Surcharge: 20_000},
	"getVault":                {},
	"deposit":                 {Write: true, Surcharge: TokenCallGas + 30_000},
	"withdraw":                {Write: true, Surcharge: TokenCallGas + 30_000},
	"emergencyWithdraw":       {Write: true, Surcharge: TokenCallGas + 30_000},
	"transferCollateral":      {Write: true, Surcharge: 30_000},
	"getBalance":              {},
	"getBalance0":             {},
	"getLockedBalance":        {},
	"getLockedBalance0":       {},
	"getAccountSummary":       {},
	"getAccountSummary0":      {},
	"getAccount":              {},
	"setMarginMode":           {Write: true, Surcharge: 5_000},
	"getMarginMode":           {},
	"getMarginMode0":          {},
	"topUpPosition":           {Write: true, Surcharge: TokenCallGas + 30_000},
	"transferMargin":          {Write: true, Surcharge: 5_000},
	"getPositionMargin":       {},
	"getMarginTiers":          {},
	"checkLiquidatable":       {},
	"placeOrder":              {Write: true, Surcharge: 250_000},
	"placeLimitOrder":         {Write: true, Surcharge: TokenCallGas + 250_000},
	"placeStopLimitOrder":     {Write: true, Surcharge: TokenCallGas + 250_000},
	"cancelOrder":             {Write: true, Surcharge: 20_000},
	"cancelByCloid":           {Write: true, Surcharge: 20_000},
	"getOrder":                {},
	"getOrderDetails":         {},
	"getUserOrderIds":         {},
	"placeTakeProfitOrder":    {Write: true, Surcharge: 60_000},
	"placeStopLossOrder":      {Write: true, Surcharge: 60_000},
	"setPositionTpSl":         {Write: true, Surcharge: 60_000},
	"executeOrder":            {Write: true, Surcharge: 250_000},
	"openPosition":            {Write: true, Surcharge: TokenCallGas + 250_000},
	"addSize":                 {Write: true, Surcharge: TokenCallGas + 250_000},
	"addCollateral":           {Write: true, Surcharge: TokenCallGas + 30_000},
	"removeCollateral":        {Write: true, Surcharge: TokenCallGas + 30_000},
	"closePosition":           {Write: true, Surcharge: 250_000},
	"partialClose":            {Write: true, Surcharge: 250_000},
	"closePositionWithParams": {Write: true, Surcharge: 250_000},
	"getPosition":             {},
	"getPositionDetails":      {},
	"getUserPositionIds":      {},
	"getUserMarketPosition":   {},
	"getMarket":               {},
	"getMarketConfig":         {},
	"getActiveMarketIds":      {},
	"totalMarkets":            {},
	"isMarketActive":          {},
	"getFees":                 {},
	"getFeeTier":              {},
	"getProtocolAccounts":     {},
	"getMarkPrice":            {},
	"getMarkPriceDetails":     {},
	"quoteMarket":             {},
	"getCurrentFundingRate":   {},
	"getFundingState":         {},
	"getFundingDetails":       {},
	"getPositionFunding":      {},
	"getOpenInterest":         {},
	"liquidate":               {Write: true, Surcharge: 300_000},
	"getLiquidationPrice":     {},
	"getInsuranceBalance":     {},
	"getADLRank":              {},
	"quoteOpenPosition":       {},
	"quoteClosePosition":      {},
	"quotePartialClose":       {},
}

// handler serves one ABI method for caller after Execute has passed the
// call-kind, value, keeper and pause checks.
type handler func(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, evm *vm.EVM) ([]byte, error)

// handlers maps an ABI method name to its handler; the account, order,
// position, view and quote files of this package register theirs.
var handlers = map[string]handler{}

type PrecompileExecutor struct {
	address common.Address
	keeper  *perpskeeper.Keeper
}

// NewPrecompile builds the perps precompile on the application's keepers. It
// carries no LXP reasons, so Run keeps the custom error selector Execute
// returns as the whole revert data.
func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	parsed, err := perpsabi.ABI()
	if err != nil {
		return nil, err
	}
	p := &PrecompileExecutor{address: common.HexToAddress(PerpsAddress)}
	if wired, ok := keepers.(PerpsKeepers); ok {
		p.keeper = wired.PerpsK()
	}
	return pcommon.NewPrecompile(parsed, p, p.address, PrecompileName).WithGasTable(GasTable), nil
}

func (PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	row, ok := GasTable[method.Name]
	if !ok {
		return pcommon.UnknownMethodCallGas
	}
	return row.Gas(input)
}

// Execute refuses DELEGATECALL, writes under STATICCALL, value, a missing
// keeper and paused writes outside the exit set, then dispatches by method
// name. A failure returns the revert data of revertOf beside the error.
func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address,
	args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) (ret []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ret, err = nil, fmt.Errorf("perps: %s: %v", method.Name, recovered)
		}
		if err != nil {
			ret = revertOf(err)
		}
	}()
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, types.ErrDelegatecall
	}
	write := GasTable[method.Name].Write
	if readOnly && write {
		return nil, types.ErrStaticCall
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if p.keeper == nil {
		return nil, ErrKeeperMissing
	}
	if write {
		if err := p.keeper.CheckPause(ctx, method.Name); err != nil {
			return nil, err
		}
	}
	serve, ok := handlers[method.Name]
	if !ok {
		return nil, fmt.Errorf("perps: method %s is not served", method.Name)
	}
	return serve(p, ctx, method, caller, args, evm)
}
