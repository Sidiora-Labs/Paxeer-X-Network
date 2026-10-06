package wasm

import (
	"encoding/base64"
	"encoding/json"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	wasmvmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm-runtime/types"
	wasmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasmbinding/bindings"
)

func EncodeCallEVM(rawMsg json.RawMessage, sender sdk.AccAddress, info wasmvmtypes.MessageInfo) ([]sdk.Msg, error) {
	encodedCallEVM := bindings.CallEVM{}
	if err := json.Unmarshal(rawMsg, &encodedCallEVM); err != nil {
		return []sdk.Msg{}, err
	}
	decodedData, err := base64.StdEncoding.DecodeString(encodedCallEVM.Data)
	if err != nil {
		return []sdk.Msg{}, err
	}
	internalCallEVMMsg := types.MsgInternalEVMCall{
		Sender: sender.String(),
		To:     encodedCallEVM.To,
		Value:  encodedCallEVM.Value,
		Data:   decodedData,
	}
	return []sdk.Msg{&internalCallEVMMsg}, nil
}

func EncodeDelegateCallEVM(rawMsg json.RawMessage, sender sdk.AccAddress, info wasmvmtypes.MessageInfo, codeInfo wasmtypes.CodeInfo) ([]sdk.Msg, error) {
	encodedCallEVM := bindings.DelegateCallEVM{}
	if err := json.Unmarshal(rawMsg, &encodedCallEVM); err != nil {
		return []sdk.Msg{}, err
	}
	decodedData, err := base64.StdEncoding.DecodeString(encodedCallEVM.Data)
	if err != nil {
		return []sdk.Msg{}, err
	}
	s := sender
	if origSender, err := sdk.AccAddressFromBech32(info.Sender); err == nil {
		s = origSender
	}
	internalCallEVMMsg := types.MsgInternalEVMDelegateCall{
		Sender:       s.String(),
		To:           encodedCallEVM.To,
		CodeHash:     codeInfo.CodeHash,
		Data:         decodedData,
		FromContract: sender.String(),
	}
	return []sdk.Msg{&internalCallEVMMsg}, nil
}
