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
//	    + GasPerByte  * len(calldata including the selector)
//	    + GasPerWrite * Writes(method)
//
// Writes is the fixed number of state slots a method may touch; views touch
// none.
package launchpad

import (
	"embed"
	"errors"
	"math"
	"math/big"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const (
	LaunchpadAddress = types.LaunchpadAddress
	PrecompileName   = "launchpad"
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

func addressTopic(value common.Address) common.Hash { return common.BytesToHash(value.Bytes()) }
