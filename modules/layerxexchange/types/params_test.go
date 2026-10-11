package types

import (
	"bytes"
	"errors"
	"testing"

	custodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/gogo/protobuf/proto"
)

func TestParamsPauseAndTifFlags(t *testing.T) {
	defaults := DefaultParams()
	if !proto.Equal(&defaults, &Params{Paused: false, TimeInForce: false}) {
		t.Fatalf("default params = %+v, want empty params with both flags false", defaults)
	}
	if err := defaults.Validate(); err != nil {
		t.Fatalf("default params invalid: %v", err)
	}
	for _, tc := range []struct {
		name        string
		paused      bool
		timeInForce bool
		wire        []byte
	}{
		{name: "neither", wire: []byte{}},
		{name: "paused", paused: true, wire: []byte{0x18, 0x01}},
		{name: "time_in_force", timeInForce: true, wire: []byte{0x20, 0x01}},
		{name: "both", paused: true, timeInForce: true, wire: []byte{0x18, 0x01, 0x20, 0x01}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := Params{Paused: tc.paused, TimeInForce: tc.timeInForce}
			if err := params.Validate(); err != nil {
				t.Fatalf("flag combination invalid: %v", err)
			}
			encoded, err := proto.Marshal(&params)
			if err != nil {
				t.Fatalf("marshal flags: %v", err)
			}
			if !bytes.Equal(encoded, tc.wire) {
				t.Fatalf("encoded flags = %x, want %x", encoded, tc.wire)
			}
			decoded := Params{Paused: !tc.paused, TimeInForce: !tc.timeInForce}
			if err := proto.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal flags: %v", err)
			}
			if decoded.Paused != tc.paused || decoded.TimeInForce != tc.timeInForce {
				t.Fatalf("decoded flags = (%t, %t), want (%t, %t)",
					decoded.Paused, decoded.TimeInForce, tc.paused, tc.timeInForce)
			}
			params.Authority = sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20)).String()
			params.Markets = []Market{{
				MarketId:      custodytypes.Hash32([32]byte{0x01}),
				MarginAssetId: custodytypes.Hash32([32]byte{0x02}),
				Enabled:       true,
			}}
			if err := params.Validate(); err != nil {
				t.Fatalf("populated params invalid: %v", err)
			}
			encoded, err = proto.Marshal(&params)
			if err != nil {
				t.Fatalf("marshal populated params: %v", err)
			}
			if err := proto.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal populated params: %v", err)
			}
			if !proto.Equal(&params, &decoded) {
				t.Fatalf("decoded params = %+v, want %+v", decoded, params)
			}
		})
	}
}

func TestMarketValidateEmptyId(t *testing.T) {
	market := Market{MarginAssetId: custodytypes.Hash32([32]byte{0x01}), Enabled: true}
	err := market.Validate()
	if !errors.Is(err, ErrInvalidMarket) {
		t.Fatalf("empty market id error = %v, want ErrInvalidMarket", err)
	}
	var registered *sdkerrors.Error
	if !errors.As(err, &registered) || registered.Codespace() != ModuleName || registered.ABCICode() != 3 {
		t.Fatalf("empty market id error = %v, want layerxexchange refusal code 3", err)
	}
}
