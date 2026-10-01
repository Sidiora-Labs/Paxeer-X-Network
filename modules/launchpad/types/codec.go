package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	cdctypes "github.com/sidiora-labs/paxeer-network/sdk/codec/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	"github.com/sidiora-labs/paxeer-network/sdk/types/msgservice"
	"io"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "launchpad/MsgUpdateParams", nil)
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
