package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const TypeMsgUpdateParams = "update_params"

// Governance payload header (src/modules/governance/lxp_governance.c:155-168):
// byte 0 is the 0x71 tag, byte 1 the sub-op ordinal, byte 2 the variant and
// byte 3 the field count.
const (
	governancePayloadTag      = 0x71
	governancePayloadMaxBytes = 1024

	sessionOrdinal          = 5
	sessionVariantGrant     = 1
	sessionVariantSuccessor = 2
	sessionGrantFields      = 3
	sessionSuccessorFields  = 5
	sessionMinBytes         = 52

	revokeOrdinal     = 6
	revokeVariant     = 0
	revokeFields      = 3
	revokeBytes       = 45
	revokeReasonIndex = 36
	revokeReasonMin   = 1
	revokeReasonMax   = 5
)

var _ sdk.Msg = &MsgUpdateParams{}

func (m MsgUpdateParams) Route() string { return RouterKey }
func (m MsgUpdateParams) Type() string  { return TypeMsgUpdateParams }
func (m MsgUpdateParams) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(m.Authority)
	return []sdk.AccAddress{authority}
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

// ValidateSessionPayload checks the governance sub-op 5 header the kernel
// decode enforces: variant 1 (grantSession) carries 3 fields, variant 2
// (grantSessionSuccessor) carries 5, and the payload spans 52 to 1024 bytes.
func ValidateSessionPayload(payload []byte) error {
	if len(payload) < sessionMinBytes || len(payload) > governancePayloadMaxBytes {
		return ErrNonCanonical.Wrapf("session payload length %d outside %d..%d", len(payload), sessionMinBytes, governancePayloadMaxBytes)
	}
	if payload[0] != governancePayloadTag || payload[1] != sessionOrdinal {
		return ErrNonCanonical.Wrapf("session payload header %#02x %d", payload[0], payload[1])
	}
	var fields byte
	switch payload[2] {
	case sessionVariantGrant:
		fields = sessionGrantFields
	case sessionVariantSuccessor:
		fields = sessionSuccessorFields
	default:
		return ErrNonCanonical.Wrapf("session payload variant %d", payload[2])
	}
	if payload[3] != fields {
		return ErrNonCanonical.Wrapf("session payload variant %d has %d fields, want %d", payload[2], payload[3], fields)
	}
	return nil
}

// ValidateRevokePayload checks the governance sub-op 6 payload: header with
// variant 0 and 3 fields, exactly 45 bytes, and a reason byte in 1..5
// (src/modules/governance/lxp_governance.c:169, :625-628).
func ValidateRevokePayload(payload []byte) error {
	if len(payload) != revokeBytes {
		return ErrNonCanonical.Wrapf("revoke payload length %d, want %d", len(payload), revokeBytes)
	}
	if payload[0] != governancePayloadTag || payload[1] != revokeOrdinal || payload[2] != revokeVariant {
		return ErrNonCanonical.Wrapf("revoke payload header %#02x %d %d", payload[0], payload[1], payload[2])
	}
	if payload[3] != revokeFields {
		return ErrNonCanonical.Wrapf("revoke payload has %d fields, want %d", payload[3], revokeFields)
	}
	if reason := payload[revokeReasonIndex]; reason < revokeReasonMin || reason > revokeReasonMax {
		return ErrAuthScope.Wrapf("revoke reason %d outside %d..%d", reason, revokeReasonMin, revokeReasonMax)
	}
	return nil
}
