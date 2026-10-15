package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	TypeMsgUpdateParams     = "update_params"
	TypeMsgTreasuryWithdraw = "treasury_withdraw"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgTreasuryWithdraw{}
)

func (m MsgUpdateParams) Route() string { return RouterKey }
func (m MsgUpdateParams) Type() string  { return TypeMsgUpdateParams }
func (m MsgUpdateParams) GetSigners() []sdk.AccAddress {
	a, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{a}
}
func (m MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}
func (m MsgUpdateParams) String() string { return jsonString(m) }
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("authority: %v", err)
	}
	if err := m.Params.Validate(); err != nil {
		return err
	}
	for i, grant := range m.RoleGrants {
		if err := grant.validate(); err != nil {
			return ErrInvalidParams.Wrapf("role_grants[%d]: %v", i, err)
		}
	}
	for i, revoke := range m.RoleRevokes {
		if err := revoke.validate(); err != nil {
			return ErrInvalidParams.Wrapf("role_revokes[%d]: %v", i, err)
		}
	}
	return nil
}

// validate checks the checksummed contract and account addresses and the
// 0x-prefixed lowercase hex role word of a role assignment.
func (a RoleAssignment) validate() error {
	if !canonicalAddress(a.Contract) {
		return ErrZeroAddress.Wrapf("contract %q is not a checksummed non-zero address", a.Contract)
	}
	if !word(a.Role) {
		return ErrMissingRole.Wrapf("role %q is not a non-zero 32-byte word", a.Role)
	}
	if !canonicalAddress(a.Account) {
		return ErrInvalidRoleAccount.Wrapf("account %q is not a checksummed non-zero address", a.Account)
	}
	return nil
}

func (m MsgTreasuryWithdraw) Route() string { return RouterKey }
func (m MsgTreasuryWithdraw) Type() string  { return TypeMsgTreasuryWithdraw }
func (m MsgTreasuryWithdraw) GetSigners() []sdk.AccAddress {
	a, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{a}
}
func (m MsgTreasuryWithdraw) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}
func (m MsgTreasuryWithdraw) String() string { return jsonString(m) }
func (m MsgTreasuryWithdraw) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("authority: %v", err)
	}
	if !canonicalAddress(m.Token) {
		return ErrInvalidToken.Wrapf("token %q is not a checksummed non-zero address", m.Token)
	}
	if !canonicalAddress(m.To) {
		return ErrZeroAddress.Wrapf("to %q is not a checksummed non-zero address", m.To)
	}
	if !amount(m.Amount) || !m.Amount.IsPositive() {
		return ErrZeroAmount.Wrapf("amount %s is not a positive uint256", m.Amount)
	}
	return nil
}
