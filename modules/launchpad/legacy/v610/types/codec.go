package typesv610

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/gogo/protobuf/jsonpb"
	"io"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "launchpad/MsgUpdateParams", nil)
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

type Int = sdk.Int
