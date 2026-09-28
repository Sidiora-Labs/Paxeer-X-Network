package lxwire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

const kernelVectorPath = "../policy/lx/testdata/kernel_activities.json"

type kernelVector struct {
	Name              string `json:"name"`
	Module            uint16 `json:"module"`
	Operation         uint16 `json:"operation"`
	Accepted          bool   `json:"accepted"`
	Sequence          uint64 `json:"sequence"`
	From              string `json:"from"`
	To                string `json:"to"`
	Asset             string `json:"asset"`
	Amount            string `json:"amount"`
	Payload           string `json:"payload"`
	Unsigned          string `json:"unsigned"`
	SignaturePreimage string `json:"signature_preimage"`
}

type kernelVectorFile struct {
	PublicKey  string         `json:"public_key"`
	NetworkID  uint32         `json:"network_id"`
	Activities []kernelVector `json:"activities"`
}

func loadKernelVectors(t *testing.T) *kernelVectorFile {
	t.Helper()
	raw, err := os.ReadFile(kernelVectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var file kernelVectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Activities) == 0 {
		t.Fatal("kernel vector file carries no activities")
	}
	return &file
}

func (f *kernelVectorFile) vector(t *testing.T, name string) kernelVector {
	t.Helper()
	for _, v := range f.Activities {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("kernel vector %s is missing", name)
	return kernelVector{}
}

func sendVectors(t *testing.T, file *kernelVectorFile) []kernelVector {
	t.Helper()
	var out []kernelVector
	for _, v := range file.Activities {
		if ModuleID(v.Module) == ModuleAsset && v.Operation == 5 {
			out = append(out, v)
		}
	}
	if len(out) < 4 {
		t.Fatalf("kernel vector file carries %d asset sends", len(out))
	}
	return out
}

func TestSendKernelVectorsDecodeAndReencode(t *testing.T) {
	file := loadKernelVectors(t)
	owner := mustHex32(t, file.PublicKey)
	accepted, refused := 0, 0
	for _, v := range sendVectors(t, file) {
		payload := mustHex(t, v.Payload)
		send, err := DecodeSend(payload)
		if !v.Accepted {
			if !errors.Is(err, ErrSend) || send != nil {
				t.Fatalf("%s: refused vector decoded (%v)", v.Name, err)
			}
			refused++
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		accepted++
		if send.From != mustHex32(t, v.From) || send.To != mustHex32(t, v.To) || send.Asset != mustHex32(t, v.Asset) ||
			send.Amount != mustUint128(t, v.Amount) {
			t.Fatalf("%s: decoded policy fields %x %x %x %v", v.Name, send.From, send.To, send.Asset, send.Amount)
		}
		if send.AuthorizationKind != OwnerAuthorization || send.Controller != send.From || send.PublicKey != owner ||
			send.SignedContextHash != send.ContextHash || send.NetworkID != file.NetworkID {
			t.Fatalf("%s: decoded authorization %+v", v.Name, send)
		}
		encoded, err := send.Encode()
		if err != nil || !bytes.Equal(encoded, payload) {
			t.Fatalf("%s: re-encoding differs (%v)", v.Name, err)
		}
		if !send.AuthorizationValid() {
			t.Fatalf("%s: authorization does not verify", v.Name)
		}
		message, err := send.AuthorizationMessage()
		if err != nil {
			t.Fatal(err)
		}
		tail := len(payload) - sendAuthorizationTail
		want := append(append(append([]byte{}, payload[:2]...), payload[4:tail+33]...), payload[tail+129:]...)
		if !bytes.Equal(message, want) {
			t.Fatalf("%s: authorization message is not the payload without the field count, key and signature", v.Name)
		}
	}
	if accepted < 2 || refused < 2 {
		t.Fatalf("accepted %d and refused %d asset send vectors", accepted, refused)
	}
	token, err := DecodeSend(mustHex(t, file.vector(t, "token-send").Payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(token.Conditions) != 2 || token.Conditions[0].Kind != SendConditionAfter || token.Conditions[1].Kind != SendConditionBefore {
		t.Fatalf("token send conditions %+v", token.Conditions)
	}
}

func TestSendTamperedAuthorizationRefused(t *testing.T) {
	file := loadKernelVectors(t)
	tampered := mustHex(t, file.vector(t, "tampered-authorization-send").Payload)
	if _, err := DecodeSend(tampered); !errors.Is(err, ErrSend) {
		t.Fatalf("tampered authorization decoded (%v)", err)
	}
	tail := len(tampered) - sendAuthorizationTail
	tampered[tail+65] ^= 1
	send, err := DecodeSend(tampered)
	if err != nil {
		t.Fatalf("restored signature refused: %v", err)
	}
	if !send.AuthorizationValid() {
		t.Fatal("restored signature does not verify")
	}
	send.Signature[10] ^= 1
	if send.AuthorizationValid() {
		t.Fatal("altered signature verifies")
	}
	send.Signature[10] ^= 1
	send.Amount = Uint128{Lo: send.Amount.Lo + 1}
	if send.AuthorizationValid() {
		t.Fatal("signature verifies over an altered amount")
	}
}

func TestSendMalformedLayoutsRefused(t *testing.T) {
	file := loadKernelVectors(t)
	native := mustHex(t, file.vector(t, "native-send").Payload)
	token := mustHex(t, file.vector(t, "token-send").Payload)
	conditionsAt := 2 + 2 + 32*3 + 16 + 8 + 32 + 8 + 32
	mutate := func(base []byte, change func([]byte) []byte) []byte {
		return change(append([]byte{}, base...))
	}
	cases := map[string][]byte{
		"bare transfer":     mustHex(t, file.vector(t, "bare-transfer").Payload),
		"empty":             {},
		"tag":               mutate(native, func(b []byte) []byte { b[1] = 0x02; return b }),
		"receive tag":       mutate(native, func(b []byte) []byte { binary.BigEndian.PutUint16(b, 0x5201); return b }),
		"field count":       mutate(native, func(b []byte) []byte { binary.BigEndian.PutUint16(b[2:], 9); return b }),
		"condition count":   mutate(native, func(b []byte) []byte { b[conditionsAt] = MaxSendConditions + 1; return b }),
		"condition kind":    mutate(token, func(b []byte) []byte { b[conditionsAt+1] = 3; return b }),
		"authorization low": mutate(native, func(b []byte) []byte { b[len(b)-sendAuthorizationTail] = 0; return b }),
		"authorization hi":  mutate(native, func(b []byte) []byte { b[len(b)-sendAuthorizationTail] = 7; return b }),
		"trailing byte":     mutate(native, func(b []byte) []byte { return append(b, 0) }),
		"truncated":         native[:len(native)-1],
		"oversized":         mutate(native, func(b []byte) []byte { return append(b, make([]byte, MaxSendPayloadBytes)...) }),
		"network zero":      mutate(native, func(b []byte) []byte { copy(b[len(b)-6:], []byte{0, 0, 0, 0}); return b }),
	}
	for name, payload := range cases {
		if send, err := DecodeSend(payload); !errors.Is(err, ErrSend) || send != nil {
			t.Fatalf("%s: decoded (%v)", name, err)
		}
	}
}

func TestSendEncodeRefusesUnemittableFields(t *testing.T) {
	file := loadKernelVectors(t)
	send, err := DecodeSend(mustHex(t, file.vector(t, "native-send").Payload))
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(s *Send){
		"zero amount":      func(s *Send) { s.Amount = Uint128{} },
		"same accounts":    func(s *Send) { s.To = s.From },
		"condition kind":   func(s *Send) { s.Conditions = []SendCondition{{Kind: 3}} },
		"nine conditions":  func(s *Send) { s.Conditions = make([]SendCondition, MaxSendConditions+1) },
		"network zero":     func(s *Send) { s.NetworkID = 0 },
		"protocol version": func(s *Send) { s.ProtocolVersion = MaxProtocolVersion + 1 },
	} {
		changed := *send
		change(&changed)
		if _, err := changed.Encode(); !errors.Is(err, ErrSend) {
			t.Fatalf("%s: encoded (%v)", name, err)
		}
		if _, err := changed.AuthorizationMessage(); !errors.Is(err, ErrSend) {
			t.Fatalf("%s: authorization message built (%v)", name, err)
		}
		if changed.AuthorizationValid() {
			t.Fatalf("%s: authorization verifies", name)
		}
	}
	identity := *send
	identity.PublicKey = [32]byte{1}
	if identity.AuthorizationValid() {
		t.Fatal("authorization verifies under the identity point")
	}
}
