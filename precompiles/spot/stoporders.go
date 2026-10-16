package spot

import (
	"embed"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

//go:embed abi/stoporders.json abi/vault.json
var stopOrdersABI embed.FS

type stopOrdersExecutor struct {
	keeper    *spotkeeper.Keeper
	evmKeeper utils.EVMKeeper
	abi       abi.ABI
	vaultABI  abi.ABI
}

var _ pcommon.PrecompileExecutor = (*stopOrdersExecutor)(nil)

func NewStopOrdersExecutor(k *spotkeeper.Keeper, evmKeeper utils.EVMKeeper) (pcommon.PrecompileExecutor, error) {
	if k == nil || evmKeeper == nil {
		return nil, fmt.Errorf("spot: stop orders require keepers")
	}
	raw, err := stopOrdersABI.ReadFile("abi/stoporders.json")
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	raw, err = stopOrdersABI.ReadFile("abi/vault.json")
	if err != nil {
		return nil, err
	}
	vault, err := abi.JSON(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	return &stopOrdersExecutor{keeper: k, evmKeeper: evmKeeper, abi: parsed, vaultABI: vault}, nil
}

func (p *stopOrdersExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil {
		return pcommon.UnknownMethodCallGas
	}
	if _, ok := p.abi.Methods[method.Name]; !ok {
		return pcommon.UnknownMethodCallGas
	}
	var base uint64 = 3000
	switch method.Name {
	case "placeStopLimitBuy", "placeStopLimitSell", "placeStopLoss":
		base = 50000
	case "cancelStopLimitOrder", "cancelStopLossOrder", "checkStopLimitActivation", "collectFees":
		base = 30000
	case "executeStopLimitOrder", "executeStopLossOrder":
		base = 60000
	case "batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders":
		args, err := method.Inputs.Unpack(input)
		if err != nil {
			return pcommon.UnknownMethodCallGas
		}
		ids, ok := args[0].([]*big.Int)
		if !ok || uint64(len(ids)) > (math.MaxUint64-20000)/60000 {
			return math.MaxUint64
		}
		base = 20000 + uint64(len(ids))*60000
	case "getAccumulatedFees", "getActivatableStopLimits", "getActiveStopLimitOrderCount", "getActiveStopLossOrderCount", "getExecutableStopLimits", "getStopLimitOrder", "getStopLossOrder", "getUserStopLimitOrders", "getUserStopLossOrders":
		base = 6000
	}
	if uint64(len(input)) > (math.MaxUint64-base)/16 {
		return math.MaxUint64
	}
	return base + 16*uint64(len(input))
}

func stopOrdersArgs(method *abi.Method, args []interface{}) error {
	if err := pcommon.ValidateArgsLength(args, len(method.Inputs)); err != nil {
		return err
	}
	for i, input := range method.Inputs {
		valid := false
		switch input.Type.T {
		case abi.AddressTy:
			_, valid = args[i].(common.Address)
		case abi.BoolTy:
			_, valid = args[i].(bool)
		case abi.UintTy:
			n, ok := args[i].(*big.Int)
			valid = ok && n != nil && n.Sign() >= 0 && n.BitLen() <= input.Type.Size
		case abi.SliceTy:
			ids, ok := args[i].([]*big.Int)
			valid = ok
			for _, id := range ids {
				if id == nil || id.Sign() < 0 || id.BitLen() > 256 {
					valid = false
					break
				}
			}
		}
		if !valid {
			return fmt.Errorf("spot: invalid argument %d to %s", i, method.Name)
		}
	}
	return nil
}

func (p *stopOrdersExecutor) Execute(ctx sdk.Context, method *abi.Method, caller, _ common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if method == nil {
		return nil, fmt.Errorf("spot: missing stop method")
	}
	canonical, ok := p.abi.Methods[method.Name]
	if !ok || canonical.Sig != method.Sig {
		return nil, fmt.Errorf("spot: unknown stop method %s", method.Name)
	}
	method = &canonical
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, fmt.Errorf("spot: cannot delegatecall stop orders")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if readOnly && !method.IsConstant() {
		return nil, vm.ErrWriteProtection
	}
	if err := stopOrdersArgs(method, args); err != nil {
		return nil, err
	}
	switch method.Name {
	case "renounceOwnership", "transferOwnership", "pause", "unpause", "setKeeper", "setOrderFee", "setFillQualityTwapPeriod", "setFeeCollector", "setPriceOracle", "setTransactionTracker":
		return nil, types.OwnableUnauthorizedAccount(caller)
	}
	if method.IsConstant() {
		return p.view(ctx, method, args)
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case "placeStopLimitBuy", "placeStopLimitSell", "placeStopLoss", "checkStopLimitActivation", "executeStopLimitOrder", "executeStopLossOrder", "batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders":
		if params.OrdersPaused {
			return nil, types.ErrEnforcedPause
		}
	}
	switch method.Name {
	case "checkStopLimitActivation", "executeStopLimitOrder", "executeStopLossOrder", "batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders":
		if !stopOrdersKeeperAllowed(params, caller) {
			return nil, types.ErrNotAuthorized
		}
	}
	if evm == nil || evm.StateDB == nil || state.GetDBImpl(evm.StateDB) == nil {
		return nil, fmt.Errorf("spot: stop mutation requires EVM state")
	}
	var output []byte
	_, err = p.keeper.NonReentrant(ctx, func(inner sdk.Context) (sdk.Context, error) {
		var callErr error
		output, callErr = p.change(inner, method, caller, args, evm)
		return inner, callErr
	})
	return output, err
}

func stopOrdersKeeperAllowed(params types.Params, caller common.Address) bool {
	if params.KeeperOpen {
		return true
	}
	for _, keeper := range params.Keepers {
		if keeper == caller {
			return true
		}
	}
	return false
}

func stopOrdersUint(n uint64) *big.Int { return new(big.Int).SetUint64(n) }
func stopOrdersIDs(ids []uint64) []*big.Int {
	result := make([]*big.Int, len(ids))
	for i, id := range ids {
		result[i] = stopOrdersUint(id)
	}
	return result
}

type stopLimitTuple struct {
	Id         *big.Int
	User       common.Address
	Stablecoin common.Address
	Token      common.Address
	Amount     *big.Int
	StopPrice  *big.Int
	LimitPrice *big.Int
	OrderType  uint8
	Status     uint8
	CreatedAt  *big.Int
	ExpiresAt  *big.Int
}

type stopLossTuple struct {
	Id           *big.Int
	User         common.Address
	Stablecoin   common.Address
	Token        common.Address
	Amount       *big.Int
	TriggerPrice *big.Int
	Status       uint8
	CreatedAt    *big.Int
	ExpiresAt    *big.Int
}

func stopOrdersAmount(value sdk.Int) *big.Int {
	if value.IsNil() {
		return new(big.Int)
	}
	return value.BigInt()
}

func stopLimitProjection(o types.StopLimitOrder) stopLimitTuple {
	return stopLimitTuple{stopOrdersUint(o.ID), o.User, o.Stablecoin, o.Token, stopOrdersAmount(o.Amount), stopOrdersAmount(o.StopPrice), stopOrdersAmount(o.LimitPrice), uint8(o.OrderType), uint8(o.Status), stopOrdersUint(o.CreatedAt), stopOrdersUint(o.ExpiresAt)}
}
func stopLossProjection(o types.StopLossOrder) stopLossTuple {
	return stopLossTuple{stopOrdersUint(o.ID), o.User, o.Stablecoin, o.Token, stopOrdersAmount(o.Amount), stopOrdersAmount(o.TriggerPrice), uint8(o.Status), stopOrdersUint(o.CreatedAt), stopOrdersUint(o.ExpiresAt)}
}

func (p *stopOrdersExecutor) view(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	pack := method.Outputs.Pack
	switch method.Name {
	case "owner":
		account, err := sdk.AccAddressFromBech32(p.keeper.Authority())
		if err != nil {
			return nil, err
		}
		return pack(p.evmKeeper.GetEVMAddressOrDefault(ctx, account))
	case "BPS_DENOMINATOR":
		return pack(big.NewInt(types.BpsDenominator))
	case "MAX_ORDER_FEE_BPS":
		return pack(big.NewInt(types.MaxOrderFeeBps))
	case "priceOracle":
		return pack(common.HexToAddress("0x1020"))
	case "vault":
		return pack(common.HexToAddress(types.PECORVaultAddress))
	case "transactionTracker":
		return pack(common.Address{})
	case "paused", "keepers", "orderFeeBps", "fillQualityTwapPeriod", "feeCollector":
		params, err := p.keeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		switch method.Name {
		case "paused":
			return pack(params.OrdersPaused)
		case "keepers":
			return pack(stopOrdersKeeperAllowed(params, args[0].(common.Address)))
		case "orderFeeBps":
			return pack(stopOrdersUint(params.OrderFeeBps))
		case "fillQualityTwapPeriod":
			return pack(stopOrdersUint(params.FillQualityTwapPeriod))
		case "feeCollector":
			return pack(params.FeeCollector)
		}
	case "nextOrderId", "getActiveStopLimitOrderCount", "getActiveStopLossOrderCount":
		var count uint64
		var err error
		switch method.Name {
		case "nextOrderId":
			count, err = p.keeper.NextOrderID(ctx)
		case "getActiveStopLimitOrderCount":
			count, err = p.keeper.GetActiveStopLimitOrderCount(ctx)
		case "getActiveStopLossOrderCount":
			count, err = p.keeper.GetActiveStopLossOrderCount(ctx)
		}
		if err != nil {
			return nil, err
		}
		return pack(stopOrdersUint(count))
	case "accumulatedFees", "getAccumulatedFees":
		fees, err := p.keeper.AccumulatedFees(ctx, common.HexToAddress(types.PECORStopOrdersAddress), args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return pack(fees.BigInt())
	case "getActivatableStopLimits", "getExecutableStopLimits":
		max := args[0].(*big.Int)
		limit := uint64(100)
		if max.IsUint64() && max.Uint64() < limit {
			limit = max.Uint64()
		}
		var ids []uint64
		var err error
		if method.Name == "getActivatableStopLimits" {
			ids, err = p.keeper.GetActivatableStopLimits(ctx, limit)
		} else {
			ids, err = p.keeper.GetExecutableStopLimits(ctx, limit)
		}
		if err != nil {
			return nil, err
		}
		return pack(stopOrdersIDs(ids))
	case "getStopLimitOrder", "stopLimitOrders":
		var order types.StopLimitOrder
		var err error
		id := args[0].(*big.Int)
		if id.IsUint64() {
			order, err = p.keeper.GetStopLimitOrder(ctx, id.Uint64())
		}
		if err != nil {
			return nil, err
		}
		o := stopLimitProjection(order)
		if method.Name == "getStopLimitOrder" {
			return pack(o)
		}
		return pack(o.Id, o.User, o.Stablecoin, o.Token, o.Amount, o.StopPrice, o.LimitPrice, o.OrderType, o.Status, o.CreatedAt, o.ExpiresAt)
	case "getStopLossOrder", "stopLossOrders":
		var order types.StopLossOrder
		var err error
		id := args[0].(*big.Int)
		if id.IsUint64() {
			order, err = p.keeper.GetStopLossOrder(ctx, id.Uint64())
		}
		if err != nil {
			return nil, err
		}
		o := stopLossProjection(order)
		if method.Name == "getStopLossOrder" {
			return pack(o)
		}
		return pack(o.Id, o.User, o.Stablecoin, o.Token, o.Amount, o.TriggerPrice, o.Status, o.CreatedAt, o.ExpiresAt)
	case "activeStopLimitOrderIds", "activeStopLossOrderIds":
		index := args[0].(*big.Int)
		if !index.IsUint64() {
			return nil, fmt.Errorf("spot: active index out of bounds")
		}
		var id uint64
		var err error
		if method.Name == "activeStopLimitOrderIds" {
			id, err = p.keeper.ActiveStopLimitOrderIDs(ctx, index.Uint64())
		} else {
			id, err = p.keeper.ActiveStopLossOrderIDs(ctx, index.Uint64())
		}
		if err != nil {
			return nil, err
		}
		return pack(stopOrdersUint(id))
	case "getUserStopLimitOrders", "getUserStopLossOrders", "userStopLimitOrders", "userStopLossOrders":
		user := args[0].(common.Address)
		var ids []uint64
		var err error
		if method.Name == "getUserStopLimitOrders" || method.Name == "userStopLimitOrders" {
			ids, err = p.keeper.GetUserStopLimitOrders(ctx, user)
		} else {
			ids, err = p.keeper.GetUserStopLossOrders(ctx, user)
		}
		if err != nil {
			return nil, err
		}
		if len(args) == 1 {
			return pack(stopOrdersIDs(ids))
		}
		index := args[1].(*big.Int)
		if !index.IsUint64() || index.Uint64() >= uint64(len(ids)) {
			return nil, fmt.Errorf("spot: user order index out of bounds")
		}
		return pack(stopOrdersUint(ids[index.Uint64()]))
	}
	return nil, fmt.Errorf("spot: unknown stop view %s", method.Name)
}

func (p *stopOrdersExecutor) log(evm *vm.EVM, name string, indexed []common.Hash, data ...interface{}) error {
	event, ok := p.abi.Events[name]
	address := common.HexToAddress(types.PECORStopOrdersAddress)
	if !ok {
		event, ok = p.vaultABI.Events[name]
		address = common.HexToAddress(types.PECORVaultAddress)
	}
	if !ok {
		return fmt.Errorf("spot: unknown stop event %s", name)
	}
	encoded, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(evm, address, append([]common.Hash{event.ID}, indexed...), encoded)
}

func (p *stopOrdersExecutor) change(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, evm *vm.EVM) ([]byte, error) {
	pack := method.Outputs.Pack
	switch method.Name {
	case "placeStopLimitBuy", "placeStopLimitSell", "placeStopLoss":
		expiry := args[len(args)-1].(*big.Int)
		if !expiry.IsUint64() {
			return nil, types.ErrInvalidExpiry
		}
		first, second := args[0].(common.Address), args[1].(common.Address)
		amount, trigger := sdk.NewIntFromBigInt(args[2].(*big.Int)), sdk.NewIntFromBigInt(args[3].(*big.Int))
		if method.Name == "placeStopLoss" {
			o, err := p.keeper.PlaceStopLossOrder(ctx, caller, first, second, amount, trigger, expiry.Uint64())
			if err != nil {
				return nil, err
			}
			if err = p.log(evm, "StopLossOrderCreated", []common.Hash{common.BigToHash(stopOrdersUint(o.ID)), common.BytesToHash(caller.Bytes())}, o.Amount.BigInt(), o.TriggerPrice.BigInt()); err != nil {
				return nil, err
			}
			return pack(stopOrdersUint(o.ID))
		}
		kind := types.OrderTypeStopLimitBuy
		if method.Name == "placeStopLimitSell" {
			first, second = second, first
			kind = types.OrderTypeStopLimitSell
		}
		o, err := p.keeper.PlaceStopLimitOrder(ctx, caller, first, second, amount, trigger, sdk.NewIntFromBigInt(args[4].(*big.Int)), expiry.Uint64(), kind)
		if err != nil {
			return nil, err
		}
		if err = p.log(evm, "StopLimitOrderCreated", []common.Hash{common.BigToHash(stopOrdersUint(o.ID)), common.BytesToHash(caller.Bytes())}, uint8(o.OrderType), o.Amount.BigInt(), o.StopPrice.BigInt(), o.LimitPrice.BigInt()); err != nil {
			return nil, err
		}
		return pack(stopOrdersUint(o.ID))
	case "collectFees":
		params, err := p.keeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		if caller != params.FeeCollector {
			return nil, types.ErrNotFeeCollector
		}
		accrued, err := p.keeper.AccumulatedFees(ctx, common.HexToAddress(types.PECORStopOrdersAddress), args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		if accrued.IsZero() {
			return nil, types.ErrNoFeesToCollect
		}
		logs, err := p.keeper.CollectFees(ctx, common.HexToAddress(types.PECORStopOrdersAddress), caller, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		for _, log := range logs {
			if err = pcommon.EmitEVMLog(evm, log.Address, log.Topics, log.Data); err != nil {
				return nil, err
			}
		}
		return pack()
	case "batchCheckAndExecuteStopLimits", "batchExecuteStopLossOrders":
		var activated, executed uint64
		for _, value := range args[0].([]*big.Int) {
			if !value.IsUint64() {
				continue
			}
			a, e, err := p.batchElement(ctx, caller, value.Uint64(), method.Name == "batchExecuteStopLossOrders", evm)
			if err != nil {
				return nil, err
			}
			activated += a
			executed += e
			if evm != nil && evm.StateDB != nil {
				if db := state.GetDBImpl(evm.StateDB); db != nil {
					ctx = db.Ctx().WithContext(ctx.Context()).WithEventManager(ctx.EventManager()).WithGasMeter(ctx.GasMeter())
				}
			}
		}
		if method.Name == "batchCheckAndExecuteStopLimits" {
			return pack(stopOrdersUint(activated), stopOrdersUint(executed))
		}
		return pack(stopOrdersUint(executed))
	case "cancelStopLimitOrder", "cancelStopLossOrder", "checkStopLimitActivation", "executeStopLimitOrder", "executeStopLossOrder":
		value := args[0].(*big.Int)
		id := uint64(0)
		if value.IsUint64() {
			id = value.Uint64()
		}
		var err error
		switch method.Name {
		case "cancelStopLimitOrder":
			err = p.keeper.CancelStopLimit(ctx, caller, id)
			if err == nil {
				err = p.log(evm, "StopLimitCancelled", []common.Hash{common.BigToHash(stopOrdersUint(id))})
			}
		case "cancelStopLossOrder":
			err = p.keeper.CancelStopLoss(ctx, caller, id)
			if err == nil {
				err = p.log(evm, "StopLossOrderCancelled", []common.Hash{common.BigToHash(stopOrdersUint(id))})
			}
		case "checkStopLimitActivation":
			_, err = p.activate(ctx, id, evm)
		case "executeStopLimitOrder":
			err = p.fill(ctx, id, false, evm)
		case "executeStopLossOrder":
			err = p.fill(ctx, id, true, evm)
		}
		if err != nil {
			return nil, err
		}
		return pack()
	}
	return nil, fmt.Errorf("spot: unknown stop mutation %s", method.Name)
}

func (p *stopOrdersExecutor) activate(ctx sdk.Context, id uint64, evm *vm.EVM) (bool, error) {
	activated, err := p.keeper.ActivateStopLimit(ctx, id)
	if err != nil || !activated {
		return activated, err
	}
	order, err := p.keeper.GetStopLimitOrder(ctx, id)
	if err != nil {
		return false, err
	}
	price, err := p.keeper.IndexPrice(ctx, order.Token)
	if err != nil {
		return false, err
	}
	return true, p.log(evm, "StopLimitActivated", []common.Hash{common.BigToHash(stopOrdersUint(id))}, price.BigInt())
}

func (p *stopOrdersExecutor) fill(ctx sdk.Context, id uint64, loss bool, evm *vm.EVM) error {
	var input, output, token common.Address
	name := "StopLimitExecuted"
	if loss {
		o, err := p.keeper.GetStopLossOrder(ctx, id)
		if err != nil {
			return err
		}
		if o.ID == 0 || o.Status != types.OrderStatusPending {
			return types.ErrNotPending
		}
		input, output, token = o.Token, o.Stablecoin, o.Token
		name = "StopLossOrderExecuted"
	} else {
		o, err := p.keeper.GetStopLimitOrder(ctx, id)
		if err != nil {
			return err
		}
		if o.ID == 0 || o.Status != types.OrderStatusActivated {
			return types.ErrNotActivated
		}
		input, output, token = o.EscrowToken(), o.Stablecoin, o.Token
		if o.OrderType.IsBuy() {
			output = o.Token
		}
	}
	before := make([]types.TokenBook, 2)
	for i, token := range []common.Address{input, output} {
		book, err := p.keeper.GetBook(ctx, token)
		if err != nil {
			return err
		}
		before[i] = book
	}
	var err error
	if loss {
		err = p.keeper.ExecuteStopLoss(ctx, id)
	} else {
		err = p.keeper.ExecuteStopLimit(ctx, id)
	}
	if err != nil {
		return err
	}
	for _, book := range before {
		after, err := p.keeper.GetBook(ctx, book.Token)
		if err != nil {
			return err
		}
		if err = p.log(evm, "ReservesUpdated", []common.Hash{common.BytesToHash(book.Token.Bytes())}, book.Reserves.BigInt(), after.Reserves.BigInt()); err != nil {
			return err
		}
	}
	price, err := p.keeper.IndexPrice(ctx, token)
	if err != nil {
		return err
	}
	return p.log(evm, name, []common.Hash{common.BigToHash(stopOrdersUint(id))}, price.BigInt())
}

func (p *stopOrdersExecutor) batchElement(ctx sdk.Context, caller common.Address, id uint64, loss bool, evm *vm.EVM) (uint64, uint64, error) {
	var input, output, token common.Address
	if loss {
		o, err := p.keeper.GetStopLossOrder(ctx, id)
		if err != nil {
			return 0, 0, err
		}
		if o.ID == 0 || !o.IsActive() {
			return 0, 0, nil
		}
		input, output, token = o.Token, o.Stablecoin, o.Token
	} else {
		o, err := p.keeper.GetStopLimitOrder(ctx, id)
		if err != nil {
			return 0, 0, err
		}
		if o.ID == 0 || !o.IsActive() {
			return 0, 0, nil
		}
		input, output, token = o.EscrowToken(), o.Stablecoin, o.Token
		if o.OrderType.IsBuy() {
			output = o.Token
		}
	}
	before := make([]types.TokenBook, 2)
	for i, token := range []common.Address{input, output} {
		book, err := p.keeper.GetBook(ctx, token)
		if err != nil {
			return 0, 0, err
		}
		before[i] = book
	}
	var activated, executed uint64
	var err error
	if loss {
		executed, err = p.keeper.BatchExecuteStopLossOrders(ctx, caller, []uint64{id})
	} else {
		activated, executed, err = p.keeper.BatchCheckAndExecuteStopLimits(ctx, caller, []uint64{id})
	}
	if err != nil {
		return 0, 0, err
	}
	if activated == 0 && executed == 0 {
		return 0, 0, nil
	}
	price, err := p.keeper.IndexPrice(ctx, token)
	if err != nil {
		return 0, 0, err
	}
	topics := []common.Hash{common.BigToHash(stopOrdersUint(id))}
	if activated != 0 {
		if err = p.log(evm, "StopLimitActivated", topics, price.BigInt()); err != nil {
			return 0, 0, err
		}
	}
	if executed != 0 {
		for _, book := range before {
			after, err := p.keeper.GetBook(ctx, book.Token)
			if err != nil {
				return 0, 0, err
			}
			if err = p.log(evm, "ReservesUpdated", []common.Hash{common.BytesToHash(book.Token.Bytes())}, book.Reserves.BigInt(), after.Reserves.BigInt()); err != nil {
				return 0, 0, err
			}
		}
		name := "StopLimitExecuted"
		if loss {
			name = "StopLossOrderExecuted"
		}
		if err = p.log(evm, name, topics, price.BigInt()); err != nil {
			return 0, 0, err
		}
	}
	return activated, executed, nil
}
