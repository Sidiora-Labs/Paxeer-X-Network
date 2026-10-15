package spot

import (
	"embed"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

//go:embed abi/orders.json abi/vault.json
var ordersFiles embed.FS

type ordersExecutor struct {
	keeper   *spotkeeper.Keeper
	abi      abi.ABI
	vaultABI abi.ABI
}

func NewOrdersExecutor(k *spotkeeper.Keeper) (pcommon.PrecompileExecutor, error) {
	if k == nil {
		return nil, errors.New("spot: missing order keeper")
	}
	read := func(path string) (abi.ABI, error) {
		raw, err := ordersFiles.ReadFile(path)
		if err != nil {
			return abi.ABI{}, err
		}
		return abi.JSON(strings.NewReader(string(raw)))
	}
	orders, err := read("abi/orders.json")
	if err != nil {
		return nil, err
	}
	vault, err := read("abi/vault.json")
	if err != nil {
		return nil, err
	}
	return &ordersExecutor{keeper: k, abi: orders, vaultABI: vault}, nil
}

func (p *ordersExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil {
		return math.MaxUint64
	}
	if _, ok := p.abi.Methods[method.Name]; !ok {
		return math.MaxUint64
	}
	base := uint64(3000)
	switch method.Name {
	case "placeLimitBuy", "placeLimitSell":
		base = 50000
	case "cancelLimitOrder", "modifyLimitOrder", "collectFees":
		base = 30000
	case "executeLimitOrder":
		base = 60000
	case "getAccumulatedFees", "getActiveLimitOrderCount", "getExecutableLimitOrders", "getLimitOrder", "getUserLimitOrders":
		base = 6000
	case "batchExecuteLimitOrders", "batchExpireOrders":
		args, err := method.Inputs.Unpack(input)
		if err != nil || len(args) != 1 {
			return math.MaxUint64
		}
		ids, ok := args[0].([]*big.Int)
		if !ok || uint64(len(ids)) > (math.MaxUint64-20000)/60000 {
			return math.MaxUint64
		}
		base = 20000 + uint64(len(ids))*60000
	}
	if uint64(len(input)) > (math.MaxUint64-base)/16 {
		return math.MaxUint64
	}
	return base + uint64(len(input))*16
}

func (p *ordersExecutor) Execute(ctx sdk.Context, method *abi.Method, caller, _ common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if method == nil {
		return nil, errors.New("spot: missing orders method")
	}
	canonical, ok := p.abi.Methods[method.Name]
	if !ok || canonical.Sig != method.Sig {
		return nil, errors.New("spot: unknown orders method")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("spot: cannot delegatecall orders")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	view := canonical.StateMutability == "view" || canonical.StateMutability == "pure"
	if readOnly && !view {
		return nil, vm.ErrWriteProtection
	}
	encoded, err := canonical.Inputs.Pack(args...)
	if err != nil {
		return nil, err
	}
	args, err = canonical.Inputs.Unpack(encoded)
	if err != nil {
		return nil, err
	}
	if view {
		return p.dispatch(ctx, &canonical, caller, args, evm)
	}
	return p.atomic(ctx, evm, func(inner sdk.Context) ([]byte, error) {
		var result []byte
		_, err := p.keeper.NonReentrant(inner, func(guarded sdk.Context) (sdk.Context, error) {
			var callErr error
			result, callErr = p.dispatch(guarded, &canonical, caller, args, evm)
			return guarded, callErr
		})
		return result, err
	})
}

func (p *ordersExecutor) atomic(ctx sdk.Context, evm *vm.EVM, fn func(sdk.Context) ([]byte, error)) ([]byte, error) {
	if evm != nil {
		db := state.GetDBImpl(evm.StateDB)
		if db == nil {
			return nil, errors.New("spot: EVM state has no context")
		}
		revision := db.Snapshot()
		branch := db.Ctx().WithContext(ctx.Context()).WithGasMeter(ctx.GasMeter()).WithEventManager(sdk.NewEventManager()).WithEvmEventManager(sdk.NewEVMEventManager())
		result, err := fn(branch)
		if err != nil {
			db.RevertToSnapshot(revision)
			return nil, err
		}
		ctx.EventManager().EmitEvents(branch.EventManager().Events())
		for _, log := range branch.EVMEventManager().Events() {
			evm.StateDB.AddLog(log)
		}
		return result, nil
	}
	branch, write := ctx.CacheContext()
	branch = branch.WithEvmEventManager(sdk.NewEVMEventManager())
	result, err := fn(branch)
	if err != nil {
		return nil, err
	}
	write()
	ctx.EventManager().EmitEvents(branch.EventManager().Events())
	ctx.EVMEventManager().EmitEvents(branch.EVMEventManager().Events())
	return result, nil
}

func ordersKeeperAllowed(params types.Params, caller common.Address) bool {
	if params.KeeperOpen {
		return true
	}
	for _, address := range params.Keepers {
		if address == caller {
			return true
		}
	}
	return false
}

func ordersID(value interface{}) (uint64, bool) {
	n, ok := value.(*big.Int)
	if !ok || n == nil || !n.IsUint64() {
		return 0, false
	}
	return n.Uint64(), true
}

func ordersIDs(ids []uint64) []*big.Int {
	out := make([]*big.Int, len(ids))
	for i, id := range ids {
		out[i] = new(big.Int).SetUint64(id)
	}
	return out
}

func (p *ordersExecutor) dispatch(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, evm *vm.EVM) ([]byte, error) {
	switch method.Name {
	case "renounceOwnership", "transferOwnership", "pause", "unpause", "setKeeper", "setOrderFee", "setFillQualityTwapPeriod", "setFeeCollector", "setPriceOracle", "setTransactionTracker":
		return nil, types.OwnableUnauthorizedAccount(caller)
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	pack := method.Outputs.Pack
	packUint := func(n uint64, err error) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return pack(new(big.Int).SetUint64(n))
	}
	rail := common.HexToAddress(types.PECOROrdersAddress)
	switch method.Name {
	case "executeLimitOrder", "batchExecuteLimitOrders", "batchExpireOrders":
		if !ordersKeeperAllowed(params, caller) {
			return nil, types.ErrNotAuthorized
		}
	}
	switch method.Name {
	case "placeLimitBuy", "placeLimitSell", "executeLimitOrder", "batchExecuteLimitOrders":
		if params.OrdersPaused {
			return nil, types.ErrEnforcedPause
		}
	}
	switch method.Name {
	case "owner":
		return pack(types.GovernanceEVMAddress())
	case "paused":
		return pack(params.OrdersPaused)
	case "keepers":
		return pack(ordersKeeperAllowed(params, args[0].(common.Address)))
	case "orderFeeBps":
		return packUint(params.OrderFeeBps, nil)
	case "MAX_ORDER_FEE_BPS":
		return packUint(types.MaxOrderFeeBps, nil)
	case "BPS_DENOMINATOR":
		return packUint(types.BpsDenominator, nil)
	case "fillQualityTwapPeriod":
		return packUint(params.FillQualityTwapPeriod, nil)
	case "feeCollector":
		return pack(params.FeeCollector)
	case "priceOracle":
		return pack(common.HexToAddress("0x1020"))
	case "vault":
		return pack(common.HexToAddress(types.PECORVaultAddress))
	case "transactionTracker":
		return pack(common.Address{})
	case "nextOrderId":
		return packUint(p.keeper.NextOrderID(ctx))
	case "accumulatedFees", "getAccumulatedFees":
		amount, err := p.keeper.AccumulatedFees(ctx, rail, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return pack(amount.BigInt())
	case "collectFees":
		if caller != params.FeeCollector {
			return nil, types.ErrNotFeeCollector
		}
		logs, err := p.keeper.CollectFees(ctx, rail, caller, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		ctx.EVMEventManager().EmitEvents(logs)
		return pack()
	case "placeLimitBuy", "placeLimitSell":
		expiry, ok := ordersID(args[4])
		if !ok {
			return nil, types.ErrInvalidExpiry
		}
		stable, token := args[0].(common.Address), args[1].(common.Address)
		kind := types.OrderTypeLimitBuy
		if method.Name == "placeLimitSell" {
			stable, token, kind = token, stable, types.OrderTypeLimitSell
		}
		id, events, err := p.keeper.PlaceLimit(ctx, caller, stable, token, sdk.NewIntFromBigInt(args[2].(*big.Int)), sdk.NewIntFromBigInt(args[3].(*big.Int)), expiry, kind)
		if err != nil {
			return nil, err
		}
		if err := p.emit(ctx, events); err != nil {
			return nil, err
		}
		return packUint(id, nil)
	case "cancelLimitOrder", "modifyLimitOrder", "executeLimitOrder":
		id, ok := ordersID(args[0])
		if !ok {
			if method.Name == "executeLimitOrder" {
				return nil, types.ErrOrderNotPending
			}
			return nil, types.ErrNotOrderOwner
		}
		var events []spotkeeper.LimitOrderEvent
		switch method.Name {
		case "cancelLimitOrder":
			events, err = p.keeper.CancelLimit(ctx, caller, id)
		case "executeLimitOrder":
			events, err = p.keeper.ExecuteLimit(ctx, id)
		case "modifyLimitOrder":
			expiry, ok := ordersID(args[2])
			if !ok {
				return nil, types.ErrInvalidExpiry
			}
			events, err = p.keeper.ModifyLimit(ctx, caller, id, sdk.NewIntFromBigInt(args[1].(*big.Int)), expiry)
		}
		if err != nil {
			return nil, err
		}
		if err := p.emit(ctx, events); err != nil {
			return nil, err
		}
		return pack()
	case "batchExecuteLimitOrders", "batchExpireOrders":
		var count uint64
		for _, value := range args[0].([]*big.Int) {
			id, ok := ordersID(value)
			if !ok {
				continue
			}
			current := ctx
			if evm != nil {
				db := state.GetDBImpl(evm.StateDB)
				if db == nil {
					return nil, errors.New("spot: EVM state has no context")
				}
				current = db.Ctx().WithContext(ctx.Context()).WithGasMeter(ctx.GasMeter()).WithEventManager(ctx.EventManager()).WithEvmEventManager(ctx.EVMEventManager())
			}
			_, err := p.atomic(current, evm, func(branch sdk.Context) ([]byte, error) {
				var events []spotkeeper.LimitOrderEvent
				var err error
				if method.Name == "batchExecuteLimitOrders" {
					events, err = p.keeper.ExecuteLimit(branch, id)
				} else {
					events, err = p.keeper.ExpireLimit(branch, id)
				}
				if err != nil {
					return nil, err
				}
				return nil, p.emit(branch, events)
			})
			if err == nil {
				count++
			}
		}
		return packUint(count, nil)
	case "getLimitOrder", "limitOrders":
		id, ok := ordersID(args[0])
		order := types.LimitOrder{Amount: sdk.ZeroInt(), TargetPrice: sdk.ZeroInt()}
		if ok {
			order, err = p.keeper.GetLimitOrder(ctx, id)
			if err != nil {
				return nil, err
			}
		}
		fields := []interface{}{new(big.Int).SetUint64(order.ID), order.User, order.Stablecoin, order.Token, order.Amount.BigInt(), order.TargetPrice.BigInt(), uint8(order.OrderType), uint8(order.Status), new(big.Int).SetUint64(order.CreatedAt), new(big.Int).SetUint64(order.ExpiresAt)}
		if method.Name == "limitOrders" {
			return pack(fields...)
		}
		tuple := struct {
			Id          *big.Int
			User        common.Address
			Stablecoin  common.Address
			Token       common.Address
			Amount      *big.Int
			TargetPrice *big.Int
			OrderType   uint8
			Status      uint8
			CreatedAt   *big.Int
			ExpiresAt   *big.Int
		}{
			fields[0].(*big.Int), order.User, order.Stablecoin, order.Token, order.Amount.BigInt(), order.TargetPrice.BigInt(), uint8(order.OrderType), uint8(order.Status), fields[8].(*big.Int), fields[9].(*big.Int),
		}
		return pack(tuple)
	case "canExecuteLimitOrder":
		id, ok := ordersID(args[0])
		if !ok {
			return pack(false, "Order not found")
		}
		executable, reason, err := p.keeper.CanExecuteLimitOrder(ctx, id)
		if err != nil {
			return nil, err
		}
		return pack(executable, reason)
	case "getUserLimitOrders", "userLimitOrders":
		ids, err := p.keeper.GetUserLimitOrders(ctx, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		if method.Name == "getUserLimitOrders" {
			return pack(ordersIDs(ids))
		}
		index, ok := ordersID(args[1])
		if !ok || index >= uint64(len(ids)) {
			return nil, errors.New("spot: user limit index out of bounds")
		}
		return packUint(ids[index], nil)
	case "activeLimitOrderIds":
		index, ok := ordersID(args[0])
		if !ok {
			return nil, errors.New("spot: active limit index out of bounds")
		}
		return packUint(p.keeper.ActiveLimitOrderID(ctx, index))
	case "getActiveLimitOrderCount":
		return packUint(p.keeper.GetActiveLimitOrderCount(ctx))
	case "getExecutableLimitOrders":
		limit := uint64(100)
		if n := args[0].(*big.Int); n.Cmp(big.NewInt(100)) < 0 {
			limit = n.Uint64()
		}
		ids, err := p.keeper.GetExecutableLimitOrders(ctx, limit)
		if err != nil {
			return nil, err
		}
		return pack(ordersIDs(ids))
	default:
		return nil, errors.New("spot: unhandled orders method")
	}
}

func (p *ordersExecutor) emit(ctx sdk.Context, events []spotkeeper.LimitOrderEvent) error {
	logs := make([]*ethtypes.Log, 0, len(events))
	for _, record := range events {
		source := p.abi
		if record.Address == common.HexToAddress(types.PECORVaultAddress) {
			source = p.vaultABI
		} else if record.Address != common.HexToAddress(types.PECOROrdersAddress) {
			return errors.New("spot: unexpected order event address")
		}
		event, ok := source.Events[record.Name]
		if !ok || len(record.Args) != len(event.Inputs) {
			return fmt.Errorf("spot: invalid order event %s", record.Name)
		}
		topics := []common.Hash{event.ID}
		var values []interface{}
		for i, arg := range event.Inputs {
			if arg.Indexed {
				word, err := (abi.Arguments{arg}).Pack(record.Args[i])
				if err != nil {
					return err
				}
				if len(word) != common.HashLength {
					return errors.New("spot: invalid order event topic")
				}
				topics = append(topics, common.BytesToHash(word))
			} else {
				values = append(values, record.Args[i])
			}
		}
		data, err := event.Inputs.NonIndexed().Pack(values...)
		if err != nil {
			return err
		}
		if ctx.BlockHeight() < 0 {
			return errors.New("spot: negative block height")
		}
		logs = append(logs, &ethtypes.Log{Address: record.Address, Topics: topics, Data: data, BlockNumber: uint64(ctx.BlockHeight())})
	}
	ctx.EVMEventManager().EmitEvents(logs)
	return nil
}
