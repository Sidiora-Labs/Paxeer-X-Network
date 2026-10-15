// Package launchpad is the precompile at 0x1017 that serves the Launch-x
// contract roles of the native launchpad module.
//
// A call sent to 0x1017 itself resolves its selector through abi.json in the
// role order Router, SidioraFactory, Quoter, PoolRegistry, FeesRouter,
// FeeAccumulator, ProtocolConfig. A call forwarded by a registered surface
// (a generation 2 role address or a pool address holding the surface
// runtime) carries the original caller in its last 20 bytes and resolves
// through the ABI of the surface's role.
//
// Gas is charged by the EVM from RequiredGas before Run:
//
//	gas = BaseGas
//	    + GasPerByte    * len(calldata after the selector)
//	    + GasPerWrite   * writes(method)
//	    + GasPerPointer * pointers(method)
//
// writes is the fixed number of state slots a method may touch: createMarket
// 12, buy 8, sell 6, claimFees and executeBurn 3, executeAirdrop and
// claimAirdrop 4, setFeeStrategy, executeLpRewards, pause and unpause 1,
// views 0. pointers is 1 for createMarket and 0 otherwise.
package launchpad

import (
	"embed"
	"errors"
	"fmt"
	"math"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	LaunchpadAddress = types.LaunchpadAddress
	PrecompileName   = "launchpad"

	BaseGas       uint64 = 3000
	GasPerByte    uint64 = 16
	GasPerWrite   uint64 = 5000
	GasPerPointer uint64 = 9_000_000
)

//go:embed abi.json
var f embed.FS

// LaunchpadKeepers is the precompile keepers set once it carries the
// launchpad keeper.
type LaunchpadKeepers interface {
	LaunchpadK() *launchpadkeeper.Keeper
}

// PrecompileExecutor holds what the role handlers serve calls from: the
// direct ABI, the precompile address, the launchpad keeper and the EVM
// keeper.
type PrecompileExecutor struct {
	abi       abi.ABI
	address   common.Address
	keeper    *launchpadkeeper.Keeper
	evmKeeper utils.EVMKeeper
}

// NewPrecompile builds the precompile from a keepers set that provides
// LaunchpadK.
func NewPrecompile(keepers utils.Keepers) (*Precompile, error) {
	k := keepers.LaunchpadK()
	if k == nil {
		return nil, errors.New("launchpad: keepers do not provide the launchpad keeper")
	}
	return NewPrecompileWithKeeper(k, keepers.EVMK())
}

// NewPrecompileWithKeeper builds the precompile over k.
func NewPrecompileWithKeeper(k *launchpadkeeper.Keeper, evmKeeper utils.EVMKeeper) (*Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")
	p := &PrecompileExecutor{abi: newAbi, address: common.HexToAddress(LaunchpadAddress), keeper: k, evmKeeper: evmKeeper}
	return &Precompile{Precompile: pcommon.NewPrecompile(newAbi, p, p.address, PrecompileName).WithRevertReasons(),
		executor: p}, nil
}

// Writes returns the fixed state-slot bound of a method.
func Writes(method string) uint64 {
	switch method {
	case "createMarket":
		return 12
	case "buy":
		return 8
	case "sell":
		return 6
	case "claimFees", "executeBurn":
		return 3
	case "executeAirdrop", "claimAirdrop", "claimAirdropForEpoch":
		return 4
	case "setFeeStrategy", "executeLpRewards", "pause", "unpause":
		return 1
	default:
		return 0
	}
}

// Pointers returns the ERC20 pointer deployments of a method.
func Pointers(method string) uint64 {
	if method == "createMarket" {
		return 1
	}
	return 0
}

// Gas is the documented formula over already-measured quantities.
func Gas(inputBytes, writes, pointers uint64) uint64 {
	return BaseGas + GasPerByte*inputBytes + GasPerWrite*writes + GasPerPointer*pointers
}

func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return Gas(uint64(len(input)), Writes(method.Name), Pointers(method.Name))
}

func clampUint64(value *big.Int) uint64 {
	if !value.IsUint64() {
		return math.MaxUint64
	}
	return value.Uint64()
}

// account maps an EVM address to its bank account; the zero address maps to
// no account so the module rejects it as Solidity does.
func (p PrecompileExecutor) account(ctx sdk.Context, address common.Address) sdk.AccAddress {
	if address == (common.Address{}) {
		return nil
	}
	return p.evmKeeper.GetPaxAddressOrDefault(ctx, address)
}

func (p PrecompileExecutor) evmAddress(ctx sdk.Context, bech32 string) common.Address {
	account, err := sdk.AccAddressFromBech32(bech32)
	if err != nil {
		return common.Address{}
	}
	return p.evmKeeper.GetEVMAddressOrDefault(ctx, account)
}

// log emits one ABI event from the launchpad address: indexed values become
// topics in declaration order and the rest is ABI-encoded data.
func (p PrecompileExecutor) log(evm *vm.EVM, name string, topics []common.Hash, data ...interface{}) error {
	event, ok := p.abi.Events[name]
	if !ok {
		return fmt.Errorf("launchpad: unknown event %s", name)
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(evm, p.address, append([]common.Hash{event.ID}, topics...), packed)
}

func addressTopic(value common.Address) common.Hash { return common.BytesToHash(value.Bytes()) }
