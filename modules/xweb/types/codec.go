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
)

// RegisterCodec registers the governance messages on the legacy amino codec
// that signs them in legacy amino JSON mode.
func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterAttestor{}, "xweb/MsgRegisterAttestor", nil)
	cdc.RegisterConcrete(&MsgRemoveAttestor{}, "xweb/MsgRemoveAttestor", nil)
	cdc.RegisterConcrete(&MsgSetThreshold{}, "xweb/MsgSetThreshold", nil)
	cdc.RegisterConcrete(&MsgSetParams{}, "xweb/MsgSetParams", nil)
	cdc.RegisterConcrete(&MsgPause{}, "xweb/MsgPause", nil)
	cdc.RegisterConcrete(&MsgUnpause{}, "xweb/MsgUnpause", nil)
}

// RegisterInterfaces registers every governance message as an sdk.Msg under
// its type URL and the Msg service that executes them.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterAttestor{},
		&MsgRemoveAttestor{},
		&MsgSetThreshold{},
		&MsgSetParams{},
		&MsgPause{},
		&MsgUnpause{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterCodec(amino)
	sdk.RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// Address20 is a protobuf custom type: on the wire it is exactly its twenty
// bytes, and anything of another length is refused.

func (a Address20) Size() int { return len(a) }

func (a Address20) Marshal() ([]byte, error) { return append([]byte(nil), a[:]...), nil }

func (a Address20) MarshalTo(data []byte) (int, error) {
	if len(data) < len(a) {
		return 0, fmt.Errorf("address20: buffer of %d bytes, need %d", len(data), len(a))
	}
	return copy(data, a[:]), nil
}

func (a *Address20) Unmarshal(data []byte) error {
	if len(data) != len(a) {
		return fmt.Errorf("address20: expected %d bytes, got %d", len(a), len(data))
	}
	copy(a[:], data)
	return nil
}

// Attestor is the module's state type and the protobuf message the attestor
// registration carries. Its protobuf JSON is the JSON the module's state and
// genesis already use, decoded with unknown fields forbidden unless the caller
// allows them.

func (a Attestor) MarshalJSONPB(*jsonpb.Marshaler) ([]byte, error) { return json.Marshal(a) }

func (a *Attestor) UnmarshalJSONPB(u *jsonpb.Unmarshaler, raw []byte) error {
	var decoded Attestor
	if err := decodeJSONPB(u, raw, &decoded); err != nil {
		return err
	}
	*a = decoded
	return nil
}

func (a Attestor) String() string { return jsonString(a) }

func decodeJSONPB(u *jsonpb.Unmarshaler, raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if u == nil || !u.AllowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("%T: more than one JSON value", out)
	}
	return nil
}

func jsonString(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%T: %v", value, err)
	}
	return string(encoded)
}
