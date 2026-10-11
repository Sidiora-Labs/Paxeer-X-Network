package feetoken

import (
	"embed"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	SetFeeDenomMethod          = "setFeeDenom"
	GetFeeDenomMethod          = "getFeeDenom"
	ClearFeeDenomMethod        = "clearFeeDenom"
	FeeTokenAddress            = "0x0000000000000000000000000000000000001018"
	PrecompileName             = "feeToken"
	BaseGas             uint64 = 3000
	GasPerByte          uint64 = 16
	GasPerWrite         uint64 = 5000
)

const (
	codeKeeperMissing   = "LXP_FEETOKEN_KEEPER_MISSING"
	codeDelegateCall    = "LXP_FEETOKEN_DELEGATECALL"
	codeStaticCall      = "LXP_FEETOKEN_STATICCALL"
	codeUnknownMethod   = "LXP_FEETOKEN_UNKNOWN_METHOD"
	codeSwitchOff       = "LXP_FEETOKEN_SWITCH_OFF"
	codeDenomNotAllowed = "LXP_FEETOKEN_DENOM_NOT_ALLOWED"
	codeNonPayable      = "LXP_PRECOMPILE_NON_PAYABLE"
	codeArgCount        = "LXP_PRECOMPILE_ARG_COUNT"
	codeUnknown         = "LXP_UNKNOWN"
)

const keeperMissingMessage = "feetoken: fee-token EVM keeper is unavailable"

// refusal is a failed call: its text is the revert reason below the merge
// plan's height, and code with detail form the registry revert from it on.
type refusal struct {
	code   string
	detail string
	err    error
}

func (r refusal) Error() string { return r.err.Error() }

func (r refusal) Unwrap() error { return r.err }

func refuse(code, detail string, err error) error {
	return refusal{code: code, detail: detail, err: err}
}

//go:embed abi.json
var f embed.FS

type FeeTokenKeeper interface {
	GetAccountFeeDenom(sdk.Context, common.Address) string
	SetAccountFeeDenom(sdk.Context, common.Address, string) error
	ClearAccountFeeDenom(sdk.Context, common.Address)
}

type PrecompileExecutor struct {
	keeper FeeTokenKeeper
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	keeper, ok := keepers.EVMK().(FeeTokenKeeper)
	if !ok || keeper == nil {
		return nil, fmt.Errorf("%s: %s", codeKeeperMissing, keeperMissingMessage)
	}
	return NewPrecompileWithKeeper(keeper), nil
}

func NewPrecompileWithKeeper(keeper FeeTokenKeeper) *pcommon.Precompile {
	newABI := pcommon.MustGetABI(f, "abi.json")
	return pcommon.NewPrecompile(newABI, &PrecompileExecutor{keeper: keeper},
		common.HexToAddress(FeeTokenAddress), PrecompileName).WithRevertReasons()
}

func (PrecompileExecutor) IsTransaction(method string) bool {
	switch method {
	case SetFeeDenomMethod, ClearFeeDenomMethod:
		return true
	default:
		return false
	}
}

func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	gas := BaseGas + GasPerByte*uint64(len(input))
	if p.IsTransaction(method.Name) {
		gas += GasPerWrite
	}
	return gas
}

// Execute runs one call. From the merge plan's height on a refusal reverts
// with its registry code as "LXP_<code>: <message>"; below that height it
// keeps the revert text it carried before the plan.
func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address,
	args []interface{}, value *big.Int, readOnly bool, _ *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	ret, err := p.execute(ctx, method, caller, args, value, readOnly)
	if err != nil && p.mergePlanActive(ctx) {
		return nil, lxpError(err)
	}
	return ret, err
}

func (p PrecompileExecutor) execute(ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, value *big.Int, readOnly bool) ([]byte, error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, refuse(codeDelegateCall, "", errors.New("cannot delegatecall feeToken"))
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, refuse(codeNonPayable, "", err)
	}
	if readOnly && p.IsTransaction(method.Name) {
		return nil, refuse(codeStaticCall, "", errors.New("cannot call a feeToken state change from staticcall"))
	}
	if p.keeper == nil {
		return nil, refuse(codeKeeperMissing, "", errors.New(keeperMissingMessage))
	}
	switch method.Name {
	case SetFeeDenomMethod:
		if err := validateArgsLength(args, 1); err != nil {
			return nil, err
		}
		denom := args[0].(string)
		if err := p.keeper.SetAccountFeeDenom(ctx, caller, denom); err != nil {
			return nil, preferenceRefusal(denom, err)
		}
		return method.Outputs.Pack()
	case GetFeeDenomMethod:
		if err := validateArgsLength(args, 1); err != nil {
			return nil, err
		}
		return method.Outputs.Pack(p.keeper.GetAccountFeeDenom(ctx, args[0].(common.Address)))
	case ClearFeeDenomMethod:
		if err := validateArgsLength(args, 0); err != nil {
			return nil, err
		}
		p.keeper.ClearAccountFeeDenom(ctx, caller)
		return method.Outputs.Pack()
	default:
		return nil, refuse(codeUnknownMethod, method.Name, fmt.Errorf("feetoken: unknown method %s", method.Name))
	}
}

func validateArgsLength(args []interface{}, length int) error {
	if err := pcommon.ValidateArgsLength(args, length); err != nil {
		return refuse(codeArgCount, err.Error(), err)
	}
	return nil
}

// preferenceRefusal codes the keeper's preference refusals by their text,
// since the keeper returns them as formatted errors.
func preferenceRefusal(denom string, err error) error {
	switch err.Error() {
	case "fee token preference: fee-token switch is off":
		return refuse(codeSwitchOff, "", err)
	case fmt.Sprintf("fee token preference: denom %q is not allowed", denom):
		return refuse(codeDenomNotAllowed, strconv.Quote(denom), err)
	default:
		return err
	}
}

// mergePlanActive reports whether the merge plan is active at the height of
// the call's block. It reads the plan without charging the call.
func (p PrecompileExecutor) mergePlanActive(ctx sdk.Context) bool {
	getter, ok := p.keeper.(interface {
		UpgradeKeeper() *upgradekeeper.Keeper
	})
	if !ok {
		return false
	}
	upgrades := getter.UpgradeKeeper()
	if upgrades == nil {
		return false
	}
	return upgrades.IsUpgradeActiveAtHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), pcommon.MergeUpgrade, ctx.BlockHeight())
}

// lxpError returns an error whose text is the registry wire form of err, so
// the precompile's RevertReason renders exactly the LXPRevert bytes. An
// uncoded error reverts as LXP_UNKNOWN carrying its text.
func lxpError(err error) error {
	code, detail := codeUnknown, err.Error()
	var r refusal
	if errors.As(err, &r) {
		code, detail = r.code, r.detail
	}
	reason, unpackErr := abi.UnpackRevert(pcommon.LXPRevert(code, detail))
	if unpackErr != nil {
		return fmt.Errorf("%s: %w", code, err)
	}
	return errors.New(reason)
}
