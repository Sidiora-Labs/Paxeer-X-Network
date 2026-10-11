package types

import (
	"unicode/utf8"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const TypeMsgUpdateParams = "update_params"

var _ sdk.Msg = &MsgUpdateParams{}

func (m MsgUpdateParams) Route() string { return RouterKey }
func (m MsgUpdateParams) Type() string  { return TypeMsgUpdateParams }
func (m MsgUpdateParams) GetSigners() []sdk.AccAddress {
	decoded, _ := sdk.AccAddressFromBech32(m.Authority)
	return []sdk.AccAddress{decoded}
}
func (m MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address (%s)", err)
	}
	return m.Params.Validate()
}

// Kernel payload bounds (include/layerx/lx_asset.h:18-20,
// include/layerx/lxp_ledger.h:77, src/ledger/lxp_send.c:10).
const (
	AssetSymbolMax           = 16
	AssetNameMax             = 32
	AssetCustodyReferenceMax = 128
	AssetDecimalsMax         = 38
	AssetIssuerNative        = 1
	AssetIssuerPaxeerCustody = 2
	PausePayloadLength       = 34
	SendMaxConditions        = 8
	sendWireTag              = 0x5301
	sendFieldCount           = 10
	// tag, field count, from, to, asset, amount, sequence, idempotency key,
	// expires at, context hash and the condition count.
	sendHeadLength = 2 + 2 + 32 + 32 + 32 + 16 + 8 + 32 + 8 + 32 + 1
	// kind and timestamp of one condition.
	sendConditionLength = 1 + 8
	// kind, controller, public key, signature, signed context hash, network
	// id and protocol version.
	sendAuthorizationLength = 1 + 32 + 32 + 64 + 32 + 4 + 2
)

func versionOne(payload []byte) bool {
	return len(payload) >= 2 && payload[0] == 0 && payload[1] == 1
}

// ValidateRegisterPayload applies the kernel register decode rules
// (src/modules/asset/lx_asset_decode.c:38-82): version 1, asset id, salt,
// an ASCII symbol of 1-16 bytes, a UTF-8 name of 1-32 bytes, decimals 0-38,
// a u128 supply cap, issuer kind 1 or 2 and a custody reference of 0-128
// bytes that is empty for kind 1 and ends the payload.
func ValidateRegisterPayload(payload []byte) error {
	length := len(payload)
	if length < 89 || !versionOne(payload) {
		return ErrNonCanonical
	}
	cursor := 66
	symbolLength := int(payload[cursor])
	cursor++
	if symbolLength == 0 || symbolLength > AssetSymbolMax || symbolLength > length-cursor {
		return ErrNonCanonical
	}
	for _, b := range payload[cursor : cursor+symbolLength] {
		if b > 0x7f {
			return ErrNonCanonical
		}
	}
	cursor += symbolLength
	if cursor == length {
		return ErrNonCanonical
	}
	nameLength := int(payload[cursor])
	cursor++
	if nameLength == 0 || nameLength > AssetNameMax || nameLength > length-cursor ||
		!utf8.Valid(payload[cursor:cursor+nameLength]) {
		return ErrNonCanonical
	}
	cursor += nameLength
	if length-cursor < 19 {
		return ErrNonCanonical
	}
	if payload[cursor] > AssetDecimalsMax {
		return ErrNonCanonical
	}
	cursor += 1 + 16
	issuerKind := payload[cursor]
	custodyLength := int(payload[cursor+1])
	cursor += 2
	if (issuerKind != AssetIssuerNative && issuerKind != AssetIssuerPaxeerCustody) ||
		custodyLength > AssetCustodyReferenceMax || custodyLength != length-cursor ||
		(issuerKind == AssetIssuerNative && custodyLength != 0) {
		return ErrNonCanonical
	}
	return nil
}

// ValidatePausePayload applies the kernel pause and unpause decode rule
// (src/modules/asset/lx_asset_registry.c:33-40): version 1 and an asset id,
// exactly 34 bytes.
func ValidatePausePayload(payload []byte) error {
	if len(payload) != PausePayloadLength || !versionOne(payload) {
		return ErrNonCanonical
	}
	return nil
}

// ValidateSendEnvelope applies the kernel send wire rules
// (src/ledger/lxp_send.c:180-230): tag 0x5301, ten fields, at most
// SendMaxConditions conditions and no byte past the authorization.
func ValidateSendEnvelope(envelope []byte) error {
	if len(envelope) < sendHeadLength {
		return ErrMalformedSend
	}
	tag := uint16(envelope[0])<<8 | uint16(envelope[1])
	fields := uint16(envelope[2])<<8 | uint16(envelope[3])
	if tag != sendWireTag || fields != sendFieldCount {
		return ErrMalformedSend
	}
	conditions := int(envelope[sendHeadLength-1])
	if conditions > SendMaxConditions ||
		len(envelope) != sendHeadLength+conditions*sendConditionLength+sendAuthorizationLength {
		return ErrMalformedSend
	}
	return nil
}
