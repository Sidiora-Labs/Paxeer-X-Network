package launchpad

import (
	"fmt"
	"math/big"
	"reflect"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

func (p PrecompileExecutor) emitAt(evm *vm.EVM, role string, calledAt common.Address, name string, values ...interface{}) error {
	roleABI, ok := roleABIs[role]
	if !ok || role == "SidioraERC20" {
		return fmt.Errorf("launchpad: unsupported event role %s", role)
	}
	event, ok := roleABI.Events[name]
	if !ok {
		return fmt.Errorf("launchpad: unknown %s event %s", role, name)
	}
	switch name {
	case "RoleAdminChanged", "Upgraded", "MirrorFailed":
		return fmt.Errorf("launchpad: retired event %s", name)
	case "Initialized":
		if role != types.RolePool {
			return fmt.Errorf("launchpad: initialization event requires a new pool")
		}
	}
	address := launchpadkeeper.Generation2Surfaces[role]
	precompile := common.HexToAddress(types.LaunchpadAddress)
	switch {
	case role == types.RolePool:
		if calledAt == (common.Address{}) || calledAt == precompile {
			return fmt.Errorf("launchpad: pool event requires a pool address")
		}
		address = calledAt
	case name == "RoleGranted" || name == "RoleRevoked":
	case role == types.RoleRouter || role == types.RoleFeesRouter:
		if calledAt != address && calledAt != precompile {
			return fmt.Errorf("launchpad: invalid %s event surface %s", role, calledAt.Hex())
		}
		address = calledAt
	case role == types.RoleProtocolConfig:
		address = precompile
	}
	if address == (common.Address{}) {
		return fmt.Errorf("launchpad: missing event surface for %s", role)
	}
	return emitABIEvent(evm, address, event, values)
}

func (p PrecompileExecutor) emitFamily(evm *vm.EVM, name string, values ...interface{}) error {
	switch name {
	case "Swap", "PoolStateUpdated", "TokenDeployed", "MarketCreated", "NftMint", "FeeStrategyChanged",
		"NftTransfer", "PoolRegistered", "RouterTrade", "ConfigUpdated", "FeeFlow", "FeeDistributed":
	default:
		return fmt.Errorf("launchpad: unknown family event %s", name)
	}
	event, ok := p.abi.Events[name]
	if !ok {
		return fmt.Errorf("launchpad: missing family event %s", name)
	}
	if evm == nil {
		return fmt.Errorf("launchpad: family event requires an EVM")
	}
	if evm.Context.BlockNumber == nil || evm.Context.BlockNumber.Sign() < 0 || evm.Context.BlockNumber.BitLen() > 256 {
		return fmt.Errorf("launchpad: invalid event block number")
	}
	args := make([]interface{}, len(values), len(values)+2)
	copy(args, values)
	args = append(args, new(big.Int).SetUint64(evm.Context.Time), new(big.Int).Set(evm.Context.BlockNumber))
	return emitABIEvent(evm, common.HexToAddress(types.LaunchpadAddress), event, args)
}

func emitABIEvent(evm *vm.EVM, address common.Address, event abi.Event, values []interface{}) error {
	if len(values) != len(event.Inputs) {
		return fmt.Errorf("launchpad: %s expects %d event arguments, got %d", event.RawName, len(event.Inputs), len(values))
	}
	topics := []common.Hash{event.ID}
	data := make([]interface{}, 0, len(values))
	for i, input := range event.Inputs {
		value := values[i]
		if value == nil || (reflect.ValueOf(value).Kind() == reflect.Ptr && reflect.ValueOf(value).IsNil()) {
			return fmt.Errorf("launchpad: %s argument %s is nil", event.RawName, input.Name)
		}
		if input.Type.T == abi.UintTy {
			if integer, ok := value.(*big.Int); ok && (integer.Sign() < 0 || integer.BitLen() > input.Type.Size) {
				return fmt.Errorf("launchpad: %s argument %s exceeds %s", event.RawName, input.Name, input.Type.String())
			}
		}
		if !input.Indexed {
			data = append(data, value)
			continue
		}
		encoded, err := (abi.Arguments{input}).Pack(value)
		if err != nil {
			return fmt.Errorf("launchpad: %s argument %s: %w", event.RawName, input.Name, err)
		}
		if len(encoded) != common.HashLength {
			return fmt.Errorf("launchpad: unsupported indexed type %s", input.Type.String())
		}
		topics = append(topics, common.BytesToHash(encoded))
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return fmt.Errorf("launchpad: %s event data: %w", event.RawName, err)
	}
	return pcommon.EmitEVMLog(evm, address, topics, packed)
}
