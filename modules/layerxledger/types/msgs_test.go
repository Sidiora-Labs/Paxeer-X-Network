package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

func TestMsgUpdateParams(t *testing.T) {
	account := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	msg := MsgUpdateParams{Authority: account.String(), Params: Params{Paused: true}}

	if msg.Route() != "layerxledger" || msg.Type() != "update_params" {
		t.Fatalf("route/type = %q/%q", msg.Route(), msg.Type())
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("valid message refused: %v", err)
	}
	signers := msg.GetSigners()
	if len(signers) != 1 || !signers[0].Equals(account) {
		t.Fatalf("signers = %v, want [%s]", signers, account)
	}

	signBytes := msg.GetSignBytes()
	if !bytes.Equal(sdk.MustSortJSON(signBytes), signBytes) {
		t.Fatalf("sign bytes not sorted JSON: %s", signBytes)
	}
	var document struct {
		Type  string `json:"type"`
		Value struct {
			Authority string `json:"authority"`
			Params    struct {
				Paused bool `json:"paused"`
			} `json:"params"`
		} `json:"value"`
	}
	if err := json.Unmarshal(signBytes, &document); err != nil {
		t.Fatalf("sign bytes: %v", err)
	}
	if document.Type != "layerxledger/MsgUpdateParams" || document.Value.Authority != account.String() ||
		!document.Value.Params.Paused {
		t.Fatalf("sign document = %+v", document)
	}

	for _, authority := range []string{"", "not-an-address"} {
		bad := MsgUpdateParams{Authority: authority}
		if err := bad.ValidateBasic(); !errors.Is(err, sdkerrors.ErrInvalidAddress) {
			t.Fatalf("authority %q: err = %v, want ErrInvalidAddress", authority, err)
		}
	}

	registry := cdctypes.NewInterfaceRegistry()
	sdk.RegisterInterfaces(registry)
	RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	packed, err := cdctypes.NewAnyWithValue(&msg)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if packed.TypeUrl != "/paxprotocol.paxchain.layerxledger.MsgUpdateParams" {
		t.Fatalf("type url = %q", packed.TypeUrl)
	}
	encoded, err := cdc.MarshalInterface(&msg)
	if err != nil {
		t.Fatalf("marshal interface: %v", err)
	}
	var decoded sdk.Msg
	if err := cdc.UnmarshalInterface(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal interface: %v", err)
	}
	if got, ok := decoded.(*MsgUpdateParams); !ok || *got != msg {
		t.Fatalf("round trip = %#v, want %#v", decoded, msg)
	}
}

// ledgerMsgsRegisterPayload builds a register payload in the kernel layout of
// src/modules/asset/lx_asset_decode.c:38-82.
func ledgerMsgsRegisterPayload(symbol, name []byte, decimals, issuerKind byte, custody []byte) []byte {
	payload := []byte{0, 1}
	payload = append(payload, bytes.Repeat([]byte{0xa1}, 32)...)
	payload = append(payload, bytes.Repeat([]byte{0xb2}, 32)...)
	payload = append(payload, byte(len(symbol)))
	payload = append(payload, symbol...)
	payload = append(payload, byte(len(name)))
	payload = append(payload, name...)
	payload = append(payload, decimals)
	payload = append(payload, bytes.Repeat([]byte{0xff}, 16)...)
	payload = append(payload, issuerKind, byte(len(custody)))
	return append(payload, custody...)
}

// ledgerMsgsSendEnvelope builds a send envelope in the kernel wire layout of
// src/ledger/lxp_send.c:118-150 with the given number of conditions.
func ledgerMsgsSendEnvelope(conditions int) []byte {
	envelope := []byte{0x53, 0x01, 0x00, 0x0a}
	envelope = append(envelope, bytes.Repeat([]byte{0x01}, 32)...) // from
	envelope = append(envelope, bytes.Repeat([]byte{0x02}, 32)...) // to
	envelope = append(envelope, bytes.Repeat([]byte{0x03}, 32)...) // asset
	envelope = append(envelope, make([]byte, 15)...)
	envelope = append(envelope, 7)                                 // amount
	envelope = append(envelope, 0, 0, 0, 0, 0, 0, 0, 1)            // sequence
	envelope = append(envelope, bytes.Repeat([]byte{0x04}, 32)...) // idempotency key
	envelope = append(envelope, 0, 0, 0, 0, 0, 0, 0x10, 0)         // expires at
	envelope = append(envelope, bytes.Repeat([]byte{0x05}, 32)...) // context hash
	envelope = append(envelope, byte(conditions))
	for i := 0; i < conditions; i++ {
		envelope = append(envelope, 1, 0, 0, 0, 0, 0, 0, 0, byte(i))
	}
	envelope = append(envelope, 1)                                 // authorization kind
	envelope = append(envelope, bytes.Repeat([]byte{0x06}, 32)...) // controller
	envelope = append(envelope, bytes.Repeat([]byte{0x07}, 32)...) // public key
	envelope = append(envelope, bytes.Repeat([]byte{0x08}, 64)...) // signature
	envelope = append(envelope, bytes.Repeat([]byte{0x05}, 32)...) // signed context hash
	envelope = append(envelope, 0, 0, 0, 9)                        // network id
	return append(envelope, 0, 1)                                  // protocol version
}

func TestPayloadValidators(t *testing.T) {
	symbol := []byte("PAX")
	name := []byte("Paxeer")
	custody := bytes.Repeat([]byte{0xc3}, AssetCustodyReferenceMax)

	registerCases := []struct {
		label   string
		payload []byte
		want    error
	}{
		{"native minimum", ledgerMsgsRegisterPayload([]byte("P"), []byte("P"), 0, 1, nil), nil},
		{"native", ledgerMsgsRegisterPayload(symbol, name, 18, 1, nil), nil},
		{"symbol 16 bytes", ledgerMsgsRegisterPayload(bytes.Repeat([]byte("S"), 16), name, 6, 1, nil), nil},
		{"name 32 bytes utf8", ledgerMsgsRegisterPayload(symbol, []byte("ééééééééééééééßß"), 6, 1, nil), nil},
		{"decimals 38", ledgerMsgsRegisterPayload(symbol, name, 38, 1, nil), nil},
		{"custody 128 bytes", ledgerMsgsRegisterPayload(symbol, name, 6, 2, custody), nil},
		{"custody empty", ledgerMsgsRegisterPayload(symbol, name, 6, 2, nil), nil},
		{"symbol 17 bytes", ledgerMsgsRegisterPayload(bytes.Repeat([]byte("S"), 17), name, 6, 1, nil), ErrNonCanonical},
		{"symbol empty", ledgerMsgsRegisterPayload(nil, append([]byte("PP"), name...), 6, 1, nil), ErrNonCanonical},
		{"symbol not ascii", ledgerMsgsRegisterPayload([]byte{'P', 0x80}, name, 6, 1, nil), ErrNonCanonical},
		{"name empty", ledgerMsgsRegisterPayload(symbol, nil, 6, 2, []byte{1}), ErrNonCanonical},
		{"name 33 bytes", ledgerMsgsRegisterPayload(symbol, bytes.Repeat([]byte("n"), 33), 6, 1, nil), ErrNonCanonical},
		{"name not utf8", ledgerMsgsRegisterPayload(symbol, []byte{'a', 0xc0, 0x80}, 6, 1, nil), ErrNonCanonical},
		{"name surrogate", ledgerMsgsRegisterPayload(symbol, []byte{0xed, 0xa0, 0x80}, 6, 1, nil), ErrNonCanonical},
		{"decimals 39", ledgerMsgsRegisterPayload(symbol, name, 39, 1, nil), ErrNonCanonical},
		{"issuer kind 0", ledgerMsgsRegisterPayload(symbol, name, 6, 0, nil), ErrNonCanonical},
		{"issuer kind 3", ledgerMsgsRegisterPayload(symbol, name, 6, 3, nil), ErrNonCanonical},
		{"native with custody", ledgerMsgsRegisterPayload(symbol, name, 6, 1, []byte{1}), ErrNonCanonical},
		{"custody 129 bytes", ledgerMsgsRegisterPayload(symbol, name, 6, 2, append(custody, 0)), ErrNonCanonical},
		{"trailing byte", append(ledgerMsgsRegisterPayload(symbol, name, 6, 1, nil), 0), ErrNonCanonical},
		{"truncated", ledgerMsgsRegisterPayload(symbol, name, 6, 1, nil)[:88], ErrNonCanonical},
		{"version 0", append([]byte{0, 0}, ledgerMsgsRegisterPayload(symbol, name, 6, 1, nil)[2:]...), ErrNonCanonical},
		{"version 2", append([]byte{0, 2}, ledgerMsgsRegisterPayload(symbol, name, 6, 1, nil)[2:]...), ErrNonCanonical},
		{"nil", nil, ErrNonCanonical},
	}
	for _, tc := range registerCases {
		if err := ValidateRegisterPayload(tc.payload); err != tc.want {
			t.Errorf("register %s: err = %v, want %v", tc.label, err, tc.want)
		}
	}

	pause := append([]byte{0, 1}, bytes.Repeat([]byte{0xa1}, 32)...)
	pauseCases := []struct {
		label   string
		payload []byte
		want    error
	}{
		{"34 bytes", pause, nil},
		{"33 bytes", pause[:33], ErrNonCanonical},
		{"35 bytes", append(append([]byte{}, pause...), 0), ErrNonCanonical},
		{"version 2", append([]byte{0, 2}, pause[2:]...), ErrNonCanonical},
		{"nil", nil, ErrNonCanonical},
	}
	for _, tc := range pauseCases {
		if err := ValidatePausePayload(tc.payload); err != tc.want {
			t.Errorf("pause %s: err = %v, want %v", tc.label, err, tc.want)
		}
	}

	if got := len(ledgerMsgsSendEnvelope(0)); got != 364 {
		t.Fatalf("empty-condition envelope = %d bytes, want 364", got)
	}
	badTag := ledgerMsgsSendEnvelope(1)
	badTag[1] = 0x02
	badFields := ledgerMsgsSendEnvelope(1)
	badFields[3] = 0x09
	sendCases := []struct {
		label    string
		envelope []byte
		want     error
	}{
		{"no conditions", ledgerMsgsSendEnvelope(0), nil},
		{"one condition", ledgerMsgsSendEnvelope(1), nil},
		{"eight conditions", ledgerMsgsSendEnvelope(SendMaxConditions), nil},
		{"nine conditions", ledgerMsgsSendEnvelope(SendMaxConditions + 1), ErrMalformedSend},
		{"255 conditions", ledgerMsgsSendEnvelope(255), ErrMalformedSend},
		{"wrong tag", badTag, ErrMalformedSend},
		{"wrong field count", badFields, ErrMalformedSend},
		{"truncated", ledgerMsgsSendEnvelope(2)[:363+9], ErrMalformedSend},
		{"trailing byte", append(ledgerMsgsSendEnvelope(2), 0), ErrMalformedSend},
		{"head only", ledgerMsgsSendEnvelope(0)[:197], ErrMalformedSend},
		{"nil", nil, ErrMalformedSend},
	}
	for _, tc := range sendCases {
		if err := ValidateSendEnvelope(tc.envelope); err != tc.want {
			t.Errorf("send %s: err = %v, want %v", tc.label, err, tc.want)
		}
	}
	if ErrNonCanonical.Error() != "LXP_NON_CANONICAL" || ErrMalformedSend.Error() != "LXP_MALFORMED_SEND" {
		t.Fatalf("validator errors = %q, %q", ErrNonCanonical.Error(), ErrMalformedSend.Error())
	}
}
