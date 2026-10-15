package launchpad

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

// errUnknownSelector marks a selector that no role of the call declares; Run
// reverts it with empty revert data, as a contract without a fallback does.
var errUnknownSelector = errors.New("launchpad: unknown selector")

// directCallRoles is the order in which a call sent to 0x1017 itself resolves
// its selector; the first role that declares a signature serves it.
var directCallRoles = []string{
	types.RoleRouter, types.RoleFactory, types.RoleQuoter, types.RolePoolRegistry, types.RoleFeesRouter,
	types.RoleFeeAccumulator, types.RoleProtocolConfig,
}

// directRole maps each signature of abi.json to the role serving it at 0x1017.
var directRole = loadDirectRoles()

func loadDirectRoles() map[string]string {
	owners := map[string]string{}
	for _, role := range directCallRoles {
		for _, method := range roleABIs[role].Methods {
			if _, taken := owners[method.Sig]; !taken {
				owners[method.Sig] = role
			}
		}
	}
	return owners
}

// call is one resolved launchpad call as its role handler serves it.
type call struct {
	ctx   sdk.Context
	evm   *vm.EVM
	hooks *tracing.Hooks
	// role is the role whose ABI resolved the selector.
	role string
	// target is the address the caller called: a surface, or 0x1017.
	target common.Address
	// caller is the original caller: the address a surface appended, or the
	// EVM caller of a direct call.
	caller common.Address
	// nested runs the nested EVM calls of the run with an explicit caller.
	nested   launchpadkeeper.NestedCaller
	method   *abi.Method
	args     []interface{}
	readOnly bool
	// market is the market of a pool surface; role surfaces and direct calls
	// carry none.
	market *types.Market
}

// roleHandler serves the methods of one role.
type roleHandler func(p PrecompileExecutor, c *call) ([]byte, error)

// roleHandlers maps a role to its handler; each role file registers its
// handler from an init function.
var roleHandlers = map[string]roleHandler{types.RolePool: servePool}

// servePool hands a SidioraPool surface call to poolHandler and continues the
// run on the context it returns.
func servePool(p PrecompileExecutor, c *call) ([]byte, error) {
	if c.market == nil {
		return nil, fmt.Errorf("%w: pool surface %s carries no market", types.ErrUnknownMarket, c.target.Hex())
	}
	// ponytail: asserted at run time because the keeper does not yet serve
	// every PoolKeeper method; pass p.keeper directly once it does.
	keeper, ok := any(p.keeper).(PoolKeeper)
	if !ok {
		return nil, fmt.Errorf("launchpad: the keeper does not serve the %s role", types.RolePool)
	}
	ctx, ret, err := p.poolHandler(poolCall{ctx: c.ctx, evm: c.evm, keeper: keeper, nested: c.nested,
		method: c.method, args: c.args, caller: c.caller, market: *c.market})
	c.ctx = ctx
	return ret, err
}

// Precompile is the launchpad precompile: the framework precompile supplies
// its ABI, name, address and gas, and Run resolves surface and direct calls.
type Precompile struct {
	*pcommon.Precompile
	executor *PrecompileExecutor
}

var (
	_ vm.PrecompiledContract           = (*Precompile)(nil)
	_ vm.DynamicGasPrecompiledContract = (*Precompile)(nil)
)

// RunAndCalculateGas charges Gas over the calldata the caller sent and the
// writes of the resolved method, then serves the call with one RunCaller
// whose nested calls are metered out of the gas left. It returns the gas the
// run did not use, also when the call reverts.
func (p *Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, _ common.Address, input []byte,
	suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, isFromDelegateCall bool,
) ([]byte, uint64, error) {
	return p.run(evm, caller, input, value, readOnly, isFromDelegateCall, hooks, &suppliedGas, true)
}

// Run serves the call without metering: the EVM runs the precompile through
// RunAndCalculateGas, and a call through Run leaves its nested calls no gas.
func (p *Precompile) Run(evm *vm.EVM, caller common.Address, _ common.Address, input []byte, value *big.Int,
	readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) ([]byte, error) {
	var gas uint64
	bz, _, err := p.run(evm, caller, input, value, readOnly, isFromDelegateCall, hooks, &gas, false)
	return bz, err
}

// run reads the original caller from the last 20 bytes of input only when
// the EVM caller is a registered surface, resolves the selector through the
// ABI of the surface's role or, for any other caller, through abi.json in the
// direct role order, and serves the call with the one RunCaller of the run
// over gas. When charge is set, Gas of the calldata the caller sent and the
// writes of the method is taken from gas first. An unknown selector reverts
// with empty revert data; any other failure reverts with its revert data.
func (p *Precompile) run(evm *vm.EVM, caller common.Address, input []byte, value *big.Int, readOnly bool,
	isFromDelegateCall bool, hooks *tracing.Hooks, gas *uint64, charge bool) (bz []byte, left uint64, err error) {
	operation := PrecompileName + "_unknown"
	defer func() {
		pcommon.HandlePrecompileError(err, evm, operation)
		if err != nil && !errors.Is(err, vm.ErrOutOfGas) {
			bz, err = runRevert(err), vm.ErrExecutionReverted
		}
	}()
	take := func(cost uint64) error {
		if !charge {
			return nil
		}
		if *gas < cost {
			*gas = 0
			return vm.ErrOutOfGas
		}
		*gas -= cost
		return nil
	}
	sdb := state.GetDBImpl(evm.StateDB)
	if sdb == nil {
		return nil, 0, errors.New("cannot get context from EVM")
	}
	ctx := sdb.Ctx().WithEVMPrecompileCalledFromDelegateCall(isFromDelegateCall)
	c, err := p.executor.resolve(ctx, caller, input)
	if err != nil {
		if gasErr := take(Gas(uint64(len(input)), 0)); gasErr != nil {
			return nil, 0, gasErr
		}
		return nil, *gas, err
	}
	operation = c.method.Name
	sent := uint64(len(input))
	if c.target != p.executor.address {
		sent -= common.AddressLength
	}
	if err := take(Gas(sent, Writes(c.method.RawName))); err != nil {
		return nil, 0, err
	}
	if c.nested, err = NewRunCaller(evm, gas); err != nil {
		return nil, *gas, err
	}
	em := ctx.EventManager()
	c.ctx = ctx.WithEventManager(sdk.NewEventManager())
	c.evm, c.hooks, c.readOnly = evm, hooks, readOnly
	bz, err = p.executor.serve(c, value)
	if err != nil {
		return nil, *gas, err
	}
	if events := c.ctx.EventManager().Events(); len(events) > 0 {
		em.EmitEvents(events)
	}
	return bz, *gas, nil
}

// runRevert is the revert data of a failed run: empty for an unknown
// selector and the revert data revertData maps err to otherwise; an error
// whose custom error cannot be encoded reverts with its reason.
func runRevert(err error) []byte {
	if errors.Is(err, errUnknownSelector) {
		return nil
	}
	data, encodeErr := revertData(err)
	if encodeErr != nil {
		return pcommon.RevertReason(err)
	}
	return data
}

// Execute serves a direct call to 0x1017 whose method the framework resolved
// through abi.json.
func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address,
	args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error) {
	role, resolved, err := p.resolveDirect(method.ID)
	if err != nil {
		return nil, err
	}
	return p.serve(&call{ctx: ctx, evm: evm, hooks: hooks, role: role, target: p.address, caller: caller,
		method: resolved, args: args, readOnly: readOnly}, value)
}

// resolve finds the role, method, arguments, original caller and market of
// input sent by caller.
func (p PrecompileExecutor) resolve(ctx sdk.Context, caller common.Address, input []byte) (*call, error) {
	if p.keeper == nil {
		return nil, errors.New("launchpad: the precompile has no launchpad keeper")
	}
	surface, isSurface, err := p.keeper.GetSurface(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !isSurface {
		if len(input) < 4 {
			return nil, errUnknownSelector
		}
		role, method, err := p.resolveDirect(input[:4])
		if err != nil {
			return nil, err
		}
		args, err := decodeWhole(method, input[4:])
		if err != nil {
			return nil, err
		}
		return &call{ctx: ctx, role: role, target: p.address, caller: caller, method: method, args: args}, nil
	}
	if len(input) < 4+common.AddressLength {
		return nil, errUnknownSelector
	}
	split := len(input) - common.AddressLength
	original, input := common.BytesToAddress(input[split:]), input[:split]
	roleABI, ok := roleABIs[surface.Role]
	if !ok {
		return nil, fmt.Errorf("launchpad: surface %s serves the unknown role %s", caller.Hex(), surface.Role)
	}
	method, err := roleABI.MethodById(input[:4])
	if err != nil {
		return nil, errUnknownSelector
	}
	args, err := decodeWhole(method, input[4:])
	if err != nil {
		return nil, err
	}
	c := &call{ctx: ctx, role: surface.Role, target: caller, caller: original, method: method, args: args}
	if surface.Market != "" {
		market, found := p.keeper.GetMarket(ctx, surface.Market)
		if !found {
			return nil, fmt.Errorf("%w: surface %s market %s", types.ErrUnknownMarket, caller.Hex(), surface.Market)
		}
		c.market = &market
	}
	return c, nil
}

// resolveDirect resolves selector through abi.json and names the first role
// of directCallRoles that declares the method.
func (p PrecompileExecutor) resolveDirect(selector []byte) (string, *abi.Method, error) {
	method, err := p.abi.MethodById(selector)
	if err != nil {
		return "", nil, errUnknownSelector
	}
	role, ok := directRole[method.Sig]
	if !ok {
		return "", nil, fmt.Errorf("launchpad: %s at %s is declared by no direct role", method.Sig, p.address.Hex())
	}
	return role, method, nil
}

// decodeWhole unpacks data as the arguments of method and refuses data that
// is not exactly their encoding, so bytes appended by a caller that is not a
// surface break the decoding instead of being ignored.
func decodeWhole(method *abi.Method, data []byte) ([]interface{}, error) {
	args, err := method.Inputs.Unpack(data)
	if err != nil {
		return nil, err
	}
	packed, err := method.Inputs.Pack(args...)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(packed, data) {
		return nil, fmt.Errorf("launchpad: calldata of %s is not its exact ABI encoding", method.Sig)
	}
	return args, nil
}

// callKindRefusal refuses a delegatecall, any attached value and a state
// change under staticcall, in that order, so a value call to the payable
// upgradeToAndCall reverts on the value check as every other function does.
func callKindRefusal(ctx sdk.Context, method *abi.Method, value *big.Int, readOnly bool) error {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return errors.New("cannot delegatecall launchpad")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return err
	}
	if readOnly && !method.IsConstant() {
		return errors.New("cannot call a launchpad state change from staticcall")
	}
	return nil
}

// serve applies the call-kind refusals and hands c to its role handler.
func (p PrecompileExecutor) serve(c *call, value *big.Int) (ret []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ret = nil
			err = fmt.Errorf("launchpad: %v", recovered)
		}
	}()
	if err := callKindRefusal(c.ctx, c.method, value, c.readOnly); err != nil {
		return nil, err
	}
	handler, ok := roleHandlers[c.role]
	if !ok {
		return nil, fmt.Errorf("launchpad: the %s role serves no handler for %s", c.role, c.method.Sig)
	}
	return handler(p, c)
}
