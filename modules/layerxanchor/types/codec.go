package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/msgservice"
	"github.com/gogo/protobuf/jsonpb"
	"io"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "layerxanchor/MsgUpdateParams", nil)
}
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init()                                                      { RegisterCodec(amino); sdk.RegisterLegacyAminoCodec(amino); amino.Seal() }
func (p Params) String() string                                  { return jsonString(p) }
func (p Params) MarshalJSONPB(*jsonpb.Marshaler) ([]byte, error) { return json.Marshal(p) }
func (p *Params) UnmarshalJSONPB(u *jsonpb.Unmarshaler, raw []byte) error {
	var next Params
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if u == nil || !u.AllowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&next); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("params: trailing JSON value")
	}
	*p = next
	return nil
}
func jsonString(value interface{}) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%T: %v", value, err)
	}
	return string(raw)
}

func (a Address20) Size() int                { return len(a) }
func (a Address20) Marshal() ([]byte, error) { return append([]byte(nil), a[:]...), nil }
func (a Address20) MarshalTo(data []byte) (int, error) {
	if len(data) < len(a) {
		return 0, fmt.Errorf("address20: short buffer")
	}
	return copy(data, a[:]), nil
}
func (a *Address20) Unmarshal(data []byte) error {
	if len(data) != len(a) {
		return fmt.Errorf("address20: expected 20 bytes")
	}
	copy(a[:], data)
	return nil
}

type Int = sdk.Int
type Dec = sdk.Dec
