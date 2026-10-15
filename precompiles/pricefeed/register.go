package pricefeed

import (
	"fmt"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

func (p PrecompileExecutor) registerFeedKey(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, evm *vm.EVM) ([]byte, error) {
	operator := sdk.ValAddress(p.evmKeeper.GetPaxAddressOrDefault(ctx, caller))
	pubKey := args[0].([]byte)
	account, previous, err := p.keeper.RegisterFeedKey(ctx, operator, pubKey)
	if err != nil {
		return nil, err
	}
	if err := p.log(evm, "FeedKeyRegistered", []common.Hash{common.BytesToHash(operator), common.BytesToHash(account)}, pubKey, common.BytesToAddress(previous)); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(common.BytesToAddress(account))
}

func (p PrecompileExecutor) revokeFeedKey(ctx sdk.Context, method *abi.Method, caller common.Address, evm *vm.EVM) ([]byte, error) {
	operator := sdk.ValAddress(p.evmKeeper.GetPaxAddressOrDefault(ctx, caller))
	account, err := p.keeper.RevokeFeedKey(ctx, operator)
	if err != nil {
		return nil, err
	}
	if err := p.log(evm, "FeedKeyRevoked", []common.Hash{common.BytesToHash(operator), common.BytesToHash(account)}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack()
}

func (p PrecompileExecutor) log(evm *vm.EVM, name string, indexed []common.Hash, data ...interface{}) error {
	event, ok := p.abi.Events[name]
	if !ok {
		return fmt.Errorf("pricefeed: no event %s", name)
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(evm, p.address, append([]common.Hash{event.ID}, indexed...), packed)
}
