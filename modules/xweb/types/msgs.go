package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// The governance messages are generated from api/xweb/tx.proto. Each is
// executed by the keeper only when Authority is the module authority (the gov
// module account by default), which is also its only signer.

const (
	TypeMsgRegisterAttestor = "register_attestor"
	TypeMsgRemoveAttestor   = "remove_attestor"
	TypeMsgSetThreshold     = "set_threshold"
	TypeMsgSetParams        = "set_params"
	TypeMsgPause            = "pause"
	TypeMsgUnpause          = "unpause"
)

var (
	_ sdk.Msg = &MsgRegisterAttestor{}
	_ sdk.Msg = &MsgRemoveAttestor{}
	_ sdk.Msg = &MsgSetThreshold{}
	_ sdk.Msg = &MsgSetParams{}
	_ sdk.Msg = &MsgPause{}
	_ sdk.Msg = &MsgUnpause{}
)

// authoritySigner is the one signer of every governance message. An authority
// that is not a bech32 account yields no signer, and ValidateBasic refuses it
// before any signature is checked.
func authoritySigner(authority string) []sdk.AccAddress {
	account, err := sdk.AccAddressFromBech32(authority)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{account}
}

func signBytes(msg sdk.Msg) []byte { return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) }

func (m MsgRegisterAttestor) Route() string                { return RouterKey }
func (m MsgRegisterAttestor) Type() string                 { return TypeMsgRegisterAttestor }
func (m MsgRegisterAttestor) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgRegisterAttestor) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgRegisterAttestor) String() string               { return jsonString(m) }

func (m MsgRemoveAttestor) Route() string                { return RouterKey }
func (m MsgRemoveAttestor) Type() string                 { return TypeMsgRemoveAttestor }
func (m MsgRemoveAttestor) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgRemoveAttestor) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgRemoveAttestor) String() string               { return jsonString(m) }

func (m MsgSetThreshold) Route() string                { return RouterKey }
func (m MsgSetThreshold) Type() string                 { return TypeMsgSetThreshold }
func (m MsgSetThreshold) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgSetThreshold) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgSetThreshold) String() string               { return jsonString(m) }

func (m MsgSetParams) Route() string                { return RouterKey }
func (m MsgSetParams) Type() string                 { return TypeMsgSetParams }
func (m MsgSetParams) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgSetParams) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgSetParams) String() string               { return jsonString(m) }

func (m MsgPause) Route() string                { return RouterKey }
func (m MsgPause) Type() string                 { return TypeMsgPause }
func (m MsgPause) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgPause) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgPause) String() string               { return jsonString(m) }

func (m MsgUnpause) Route() string                { return RouterKey }
func (m MsgUnpause) Type() string                 { return TypeMsgUnpause }
func (m MsgUnpause) GetSigners() []sdk.AccAddress { return authoritySigner(m.Authority) }
func (m MsgUnpause) GetSignBytes() []byte         { return signBytes(&m) }
func (m MsgUnpause) String() string               { return jsonString(m) }

func validateAuthority(authority string) error {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		return ErrUnauthorized.Wrapf("authority: %v", err)
	}
	return nil
}

func (m MsgRegisterAttestor) ValidateBasic() error {
	if err := validateAuthority(m.Authority); err != nil {
		return err
	}
	return m.Attestor.Validate()
}

func (m MsgRemoveAttestor) ValidateBasic() error {
	if err := validateAuthority(m.Authority); err != nil {
		return err
	}
	if m.Signer == (Address20{}) {
		return ErrInvalidAttestors.Wrap("zero signer")
	}
	return nil
}

func (m MsgSetThreshold) ValidateBasic() error {
	if err := validateAuthority(m.Authority); err != nil {
		return err
	}
	if m.Threshold == 0 {
		return ErrInvalidThreshold.Wrap("threshold is zero")
	}
	return nil
}

func (m MsgSetParams) ValidateBasic() error {
	if err := validateAuthority(m.Authority); err != nil {
		return err
	}
	return ValidateSettings(m.Fee, m.MaxPayloadBytes, m.MaxCallbackGas, m.TimeoutBlocks)
}

func (m MsgPause) ValidateBasic() error { return validateAuthority(m.Authority) }

func (m MsgUnpause) ValidateBasic() error { return validateAuthority(m.Authority) }
