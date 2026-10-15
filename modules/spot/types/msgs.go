package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/msgservice"
	"github.com/ethereum/go-ethereum/common"
)

const (
	RouterKey           = ModuleName
	TypeMsgUpdateParams = "update_params"
	TypeMsgSyncReserves = "sync_reserves"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgSyncReserves{}

	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "spot/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgSyncReserves{}, "spot/MsgSyncReserves", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{}, &MsgSyncReserves{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

func init() { RegisterCodec(amino); sdk.RegisterLegacyAminoCodec(amino); amino.Seal() }

// paramsJSON drops the Params methods so Params encodes with its field tags.
type paramsJSON Params

func (p Params) MarshalJSON() ([]byte, error) { return json.Marshal(paramsJSON(p)) }

func (p *Params) UnmarshalJSON(raw []byte) error { return json.Unmarshal(raw, (*paramsJSON)(p)) }

// Marshal, MarshalTo, Size and Unmarshal carry Params as the params bytes of
// MsgUpdateParams; Unmarshal refuses unknown fields and trailing values.
func (p Params) Marshal() ([]byte, error) { return p.MarshalJSON() }

func (p *Params) MarshalTo(data []byte) (int, error) {
	raw, err := p.Marshal()
	if err != nil {
		return 0, err
	}
	if len(data) < len(raw) {
		return 0, io.ErrShortBuffer
	}
	return copy(data, raw), nil
}

func (p *Params) Size() int {
	raw, err := p.Marshal()
	if err != nil {
		return 0
	}
	return len(raw)
}

func (p *Params) Unmarshal(data []byte) error {
	var next paramsJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("params: trailing JSON value")
	}
	*p = Params(next)
	return nil
}

func (m MsgUpdateParams) Route() string { return RouterKey }
func (m MsgUpdateParams) Type() string  { return TypeMsgUpdateParams }
func (m MsgUpdateParams) GetSigners() []sdk.AccAddress {
	return authoritySigners(m.Authority)
}
func (m MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}
func (m MsgUpdateParams) String() string { return jsonString(m) }
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("authority: %v", err)
	}
	return m.Params.Validate()
}

func (m MsgSyncReserves) Route() string { return RouterKey }
func (m MsgSyncReserves) Type() string  { return TypeMsgSyncReserves }
func (m MsgSyncReserves) GetSigners() []sdk.AccAddress {
	return authoritySigners(m.Authority)
}
func (m MsgSyncReserves) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}
func (m MsgSyncReserves) String() string { return jsonString(m) }
func (m MsgSyncReserves) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return sdkerrors.ErrInvalidAddress.Wrapf("authority: %v", err)
	}
	if len(m.Tokens) == 0 {
		return sdkerrors.ErrInvalidRequest.Wrap("empty token list")
	}
	seen := make(map[common.Address]struct{}, len(m.Tokens))
	for _, token := range m.Tokens {
		if !common.IsHexAddress(token) {
			return sdkerrors.ErrInvalidAddress.Wrapf("token %q is not a hex address", token)
		}
		address := common.HexToAddress(token)
		if address == (common.Address{}) {
			return sdkerrors.ErrInvalidAddress.Wrap("zero token address")
		}
		if _, ok := seen[address]; ok {
			return sdkerrors.ErrInvalidRequest.Wrapf("duplicate token %s", address.Hex())
		}
		seen[address] = struct{}{}
	}
	return nil
}

// authoritySigners returns the authority as the only signer, so the message
// is accepted from the governance module account alone.
func authoritySigners(authority string) []sdk.AccAddress {
	a, err := sdk.AccAddressFromBech32(authority)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{a}
}

func jsonString(value interface{}) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return err.Error()
	}
	return string(raw)
}
