package lxwire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/testvectors"
)

const (
	vectorCopyPath   = "testdata/activities.json"
	vectorSourcePath = "../../../../../agent/crates/layerx-wire/tests/support/wallet_vectors/activities.json"
)

type bytesField struct {
	Hex    *string `json:"hex"`
	Repeat *string `json:"repeat"`
	Count  int     `json:"count"`
}

type encodedField struct {
	Hex    *string `json:"hex"`
	SHA256 *string `json:"sha256"`
	Length int     `json:"length"`
}

type activityVector struct {
	Name              string       `json:"name"`
	ProtocolVersion   uint16       `json:"protocol_version"`
	NetworkID         uint32       `json:"network_id"`
	ActivityType      uint32       `json:"activity_type"`
	ActorDID          bytesField   `json:"actor_did"`
	Authority         bytesField   `json:"authority"`
	AccountSequence   string       `json:"account_sequence"`
	NotBefore         string       `json:"not_before"`
	NotAfter          string       `json:"not_after"`
	IdempotencyKey    string       `json:"idempotency_key"`
	FeeLimit          string       `json:"fee_limit"`
	PayloadHash       string       `json:"payload_hash"`
	Payload           bytesField   `json:"payload"`
	Signature         bytesField   `json:"signature"`
	Unsigned          encodedField `json:"unsigned"`
	Signed            encodedField `json:"signed"`
	SignaturePreimage string       `json:"signature_preimage"`
}

type grantVector struct {
	Name               string `json:"name"`
	From               string `json:"from"`
	Recipient          string `json:"recipient"`
	Asset              string `json:"asset"`
	PerDrawMaximum     string `json:"per_draw_maximum"`
	Allowance          string `json:"allowance"`
	Recurring          bool   `json:"recurring"`
	WindowLength       string `json:"window_length"`
	Expiration         string `json:"expiration"`
	PurposeHash        string `json:"purpose_hash"`
	HasReference       bool   `json:"has_reference"`
	ReferenceHash      string `json:"reference_hash"`
	RevocationSequence string `json:"revocation_sequence"`
	PublicKey          string `json:"public_key"`
	GrantID            string `json:"grant_id"`
	Preimage           string `json:"preimage"`
}

type receiveVector struct {
	Name              string      `json:"name"`
	Grant             grantVector `json:"grant"`
	Amount            string      `json:"amount"`
	Sequence          string      `json:"sequence"`
	IdempotencyKey    string      `json:"idempotency_key"`
	ContextHash       string      `json:"context_hash"`
	AuthorizationKind uint8       `json:"authorization_kind"`
	NetworkID         uint32      `json:"network_id"`
	ProtocolVersion   uint16      `json:"protocol_version"`
	Preimage          string      `json:"preimage"`
}

type vectorFile struct {
	Activities []activityVector `json:"activities"`
	Accounts   []struct {
		Name      string `json:"name"`
		AccountID string `json:"account_id"`
	} `json:"accounts"`
	Grants   []grantVector   `json:"grants"`
	Receives []receiveVector `json:"receives"`
}

func loadVectors(t *testing.T) *vectorFile {
	t.Helper()
	raw, err := os.ReadFile(vectorCopyPath)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var file vectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(file.Activities) == 0 || len(file.Accounts) == 0 || len(file.Grants) == 0 || len(file.Receives) == 0 {
		t.Fatalf("vector file is missing a section")
	}
	return &file
}

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	out, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("hex %q: %v", text, err)
	}
	return out
}

func mustHex32(t *testing.T, text string) [32]byte {
	t.Helper()
	var out [32]byte
	raw := mustHex(t, text)
	if len(raw) != len(out) {
		t.Fatalf("%q is %d bytes, want 32", text, len(raw))
	}
	copy(out[:], raw)
	return out
}

func mustUint64(t *testing.T, text string) uint64 {
	t.Helper()
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		t.Fatalf("uint64 %q: %v", text, err)
	}
	return value
}

func mustUint128(t *testing.T, text string) Uint128 {
	t.Helper()
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.Sign() < 0 || value.BitLen() > 128 {
		t.Fatalf("uint128 %q is not representable", text)
	}
	var raw [16]byte
	value.FillBytes(raw[:])
	return Uint128{Hi: beUint64(raw[:8]), Lo: beUint64(raw[8:])}
}

func beUint64(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

func (f bytesField) materialise(t *testing.T) []byte {
	t.Helper()
	switch {
	case f.Hex != nil:
		return mustHex(t, *f.Hex)
	case f.Repeat != nil:
		fill := mustHex(t, *f.Repeat)
		if len(fill) != 1 || f.Count <= 0 {
			t.Fatalf("malformed repeat field")
		}
		return bytes.Repeat(fill, f.Count)
	}
	t.Fatalf("byte field has neither hex nor repeat")
	return nil
}

func (f encodedField) assertEqual(t *testing.T, name string, got []byte) {
	t.Helper()
	switch {
	case f.Hex != nil:
		if want := mustHex(t, *f.Hex); !bytes.Equal(got, want) {
			t.Fatalf("%s: encoding differs from the Rust encoder\n got %x\nwant %x", name, got, want)
		}
	case f.SHA256 != nil:
		sum := sha256.Sum256(got)
		if len(got) != f.Length || hex.EncodeToString(sum[:]) != *f.SHA256 {
			t.Fatalf("%s: encoding of %d bytes differs from the Rust encoder (%d bytes)", name, len(got), f.Length)
		}
	default:
		t.Fatalf("%s: encoded field has neither hex nor sha256", name)
	}
}

func (v activityVector) activity(t *testing.T) *Activity {
	t.Helper()
	return &Activity{
		ProtocolVersion: v.ProtocolVersion,
		NetworkID:       v.NetworkID,
		Type:            ActivityType(v.ActivityType),
		ActorDID:        v.ActorDID.materialise(t),
		Authority:       v.Authority.materialise(t),
		AccountSequence: mustUint64(t, v.AccountSequence),
		NotBefore:       mustUint64(t, v.NotBefore),
		NotAfter:        mustUint64(t, v.NotAfter),
		IdempotencyKey:  mustHex32(t, v.IdempotencyKey),
		FeeLimit:        mustUint128(t, v.FeeLimit),
		PayloadHash:     mustHex32(t, v.PayloadHash),
		Payload:         v.Payload.materialise(t),
		Signed:          true,
		Signature:       v.Signature.materialise(t),
	}
}

func vectorRegistry(t *testing.T, vectors []activityVector) *Registry {
	t.Helper()
	types := make([]ActivityType, 0, len(vectors))
	for _, v := range vectors {
		types = append(types, ActivityType(v.ActivityType))
	}
	registry, err := NewRegistry(types...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return registry
}

func assertReason(t *testing.T, name string, err error, want Reason) {
	t.Helper()
	var wireErr *Error
	if !errors.As(err, &wireErr) {
		t.Fatalf("%s: error %v, want %s", name, err, want)
	}
	if wireErr.Reason != want {
		t.Fatalf("%s: reason %s, want %s", name, wireErr.Reason, want)
	}
}

func TestVectorCopyMatchesRustGenerator(t *testing.T) {
	copyBytes, err := os.ReadFile(vectorCopyPath)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	source, err := os.ReadFile(filepath.FromSlash(vectorSourcePath))
	if err != nil {
		t.Fatalf("read generator output: %v", err)
	}
	if !bytes.Equal(copyBytes, source) {
		t.Fatalf("%s drifted from %s", vectorCopyPath, vectorSourcePath)
	}
}

func TestActivityVectorsRoundTripByteForByte(t *testing.T) {
	file := loadVectors(t)
	registry := vectorRegistry(t, file.Activities)
	names := map[string]bool{}
	for _, v := range file.Activities {
		names[v.Name] = true
		want := v.activity(t)

		signed, err := EncodeActivity(want)
		if err != nil {
			t.Fatalf("%s: encode signed: %v", v.Name, err)
		}
		v.Signed.assertEqual(t, v.Name+" signed", signed)
		unsigned, err := EncodeUnsignedActivity(want)
		if err != nil {
			t.Fatalf("%s: encode unsigned: %v", v.Name, err)
		}
		v.Unsigned.assertEqual(t, v.Name+" unsigned", unsigned)

		decoded, err := DecodeActivity(signed, registry)
		if err != nil {
			t.Fatalf("%s: decode signed: %v", v.Name, err)
		}
		if !reflect.DeepEqual(decoded, want) {
			t.Fatalf("%s: decoded signed activity differs from its fields", v.Name)
		}
		again, err := EncodeActivity(decoded)
		if err != nil || !bytes.Equal(again, signed) {
			t.Fatalf("%s: signed re-encode differs: %v", v.Name, err)
		}

		decodedUnsigned, err := DecodeUnsignedActivity(unsigned, registry)
		if err != nil {
			t.Fatalf("%s: decode unsigned: %v", v.Name, err)
		}
		if decodedUnsigned.Signed || decodedUnsigned.Signature != nil {
			t.Fatalf("%s: unsigned decode carries a signature", v.Name)
		}
		unsignedWant := *want
		unsignedWant.Signed = false
		unsignedWant.Signature = nil
		if !reflect.DeepEqual(decodedUnsigned, &unsignedWant) {
			t.Fatalf("%s: decoded unsigned activity differs from its fields", v.Name)
		}
		againUnsigned, err := EncodeUnsignedActivity(decodedUnsigned)
		if err != nil || !bytes.Equal(againUnsigned, unsigned) {
			t.Fatalf("%s: unsigned re-encode differs: %v", v.Name, err)
		}
		if _, err := EncodeActivity(decodedUnsigned); err == nil {
			t.Fatalf("%s: an unsigned activity encoded as signed", v.Name)
		}

		preimage := mustHex32(t, v.SignaturePreimage)
		for label, activity := range map[string]*Activity{"signed": decoded, "unsigned": decodedUnsigned} {
			got, err := SignaturePreimage(activity)
			if err != nil || got != preimage {
				t.Fatalf("%s: %s preimage %x, want %x (%v)", v.Name, label, got, preimage, err)
			}
		}
		if !decoded.PayloadHashMatches() {
			t.Fatalf("%s: payload hash does not match the payload", v.Name)
		}
		if decoded.AccountSequence != mustUint64(t, v.AccountSequence) {
			t.Fatalf("%s: account sequence differs", v.Name)
		}
	}
	for _, name := range []string{"native-send", "token-send", "approval", "budget-change", "agent-action", "maximum-field-sizes"} {
		if !names[name] {
			t.Fatalf("vector %s is missing", name)
		}
	}
}

func TestActivityAuthorityKind(t *testing.T) {
	file := loadVectors(t)
	registry := vectorRegistry(t, file.Activities)
	fixture, err := testvectors.LoadPaxeerBind()
	if err != nil {
		t.Fatalf("load bind fixture: %v", err)
	}
	kinds := map[string]AuthorityKind{}
	for _, v := range file.Activities {
		signed, err := EncodeActivity(v.activity(t))
		if err != nil {
			t.Fatalf("%s: encode: %v", v.Name, err)
		}
		decoded, err := DecodeActivity(signed, registry)
		if err != nil {
			t.Fatalf("%s: decode: %v", v.Name, err)
		}
		kinds[v.Name] = decoded.AuthorityKind(fixture.PublicKey)
		if key, ok := decoded.AuthorityKey(); ok && kinds[v.Name] == AuthorityMalformed {
			t.Fatalf("%s: authority key %x reported malformed", v.Name, key)
		}
	}
	want := map[string]AuthorityKind{
		"native-send":         AuthorityOwner,
		"token-send":          AuthorityOwner,
		"approval":            AuthorityOwner,
		"budget-change":       AuthorityGrant,
		"agent-action":        AuthorityOwner,
		"maximum-field-sizes": AuthorityMalformed,
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("authority kinds %v, want %v", kinds, want)
	}
}

func smallVector(t *testing.T, file *vectorFile) (activityVector, []byte, []byte) {
	t.Helper()
	for _, v := range file.Activities {
		if v.Name == "native-send" {
			signed, err := EncodeActivity(v.activity(t))
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			unsigned, err := EncodeUnsignedActivity(v.activity(t))
			if err != nil {
				t.Fatalf("encode unsigned: %v", err)
			}
			return v, signed, unsigned
		}
	}
	t.Fatalf("native-send vector missing")
	return activityVector{}, nil, nil
}

func TestActivityDecodeRefusals(t *testing.T) {
	file := loadVectors(t)
	registry := vectorRegistry(t, file.Activities)
	v, signed, unsigned := smallVector(t, file)

	for n := 0; n < len(signed); n++ {
		if _, err := DecodeActivity(signed[:n], registry); err == nil {
			t.Fatalf("truncated activity of %d bytes decoded", n)
		}
	}
	_, err := DecodeActivity(append(append([]byte{}, signed...), 0), registry)
	assertReason(t, "trailing byte", err, ReasonTrailingBytes)

	_, err = DecodeActivity(unsigned, registry)
	assertReason(t, "unsigned as signed", err, ReasonMalformedEnvelope)
	_, err = DecodeUnsignedActivity(signed, registry)
	assertReason(t, "signed as unsigned", err, ReasonMalformedEnvelope)

	mutate := func(offset int, value byte) []byte {
		out := append([]byte{}, signed...)
		out[offset] = value
		return out
	}
	_, err = DecodeActivity(mutate(1, 4), registry)
	assertReason(t, "header version four", err, ReasonVersionUnsupported)
	_, err = DecodeActivity(mutate(1, 0), registry)
	assertReason(t, "header version zero", err, ReasonVersionUnsupported)
	_, err = DecodeActivity(mutate(3, 0x02), registry)
	assertReason(t, "structure tag", err, ReasonVersionUnsupported)
	_, err = DecodeActivity(mutate(5, 2), registry)
	assertReason(t, "field one tag", err, ReasonUnknownField)
	_, err = DecodeActivity(mutate(7, 3), registry)
	assertReason(t, "protocol version differs from header", err, ReasonVersionUnsupported)

	other, err := NewRegistry(ActivityType(0x0001_0006))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = DecodeActivity(signed, other)
	assertReason(t, "undeclared activity", err, ReasonUnknownActivity)
	_, err = DecodeActivity(signed, nil)
	assertReason(t, "no registry", err, ReasonUnknownActivity)

	inverted := v.activity(t)
	inverted.NotBefore, inverted.NotAfter = inverted.NotAfter, inverted.NotBefore
	if _, err := EncodeActivity(inverted); err == nil {
		t.Fatalf("inverted timestamp bound encoded")
	}
	typeOffset := 4 + 1 + 1 + 2 + 1 + 4 + 1
	unknownModule := append([]byte{}, signed...)
	unknownModule[typeOffset] = 0
	unknownModule[typeOffset+1] = 12
	_, err = DecodeActivity(unknownModule, registry)
	assertReason(t, "unknown module", err, ReasonUnknownActivity)
	didLengthOffset := typeOffset + 4 + 1
	oversizedDID := append([]byte{}, signed...)
	oversizedDID[didLengthOffset+2] = 0x01
	oversizedDID[didLengthOffset+3] = 0x00
	_, err = DecodeActivity(oversizedDID, registry)
	assertReason(t, "did over bound", err, ReasonLengthLimit)
}

func TestActivityEncodeBounds(t *testing.T) {
	file := loadVectors(t)
	var maximum *Activity
	for _, v := range file.Activities {
		if v.Name == "maximum-field-sizes" {
			maximum = v.activity(t)
		}
	}
	if maximum == nil {
		t.Fatalf("maximum vector missing")
	}
	signed, err := EncodeActivity(maximum)
	if err != nil || len(signed) != MaxMessageBytes {
		t.Fatalf("maximum activity is %d bytes (%v), want %d", len(signed), err, MaxMessageBytes)
	}
	if len(maximum.ActorDID) != MaxDIDBytes || len(maximum.Payload) != MaxPayloadBytes || len(maximum.Signature) != MaxSignatureBytes {
		t.Fatalf("maximum vector does not carry the field maxima")
	}
	cases := map[string]func(a *Activity){
		"did":       func(a *Activity) { a.ActorDID = append(a.ActorDID, 'a') },
		"payload":   func(a *Activity) { a.Payload = append(a.Payload, 0) },
		"signature": func(a *Activity) { a.Signature = append(a.Signature, 0) },
		"message":   func(a *Activity) { a.Authority = append(a.Authority, 0) },
	}
	for name, grow := range cases {
		a := *maximum
		a.ActorDID = append([]byte{}, maximum.ActorDID...)
		a.Authority = append([]byte{}, maximum.Authority...)
		a.Payload = append([]byte{}, maximum.Payload...)
		a.Signature = append([]byte{}, maximum.Signature...)
		grow(&a)
		_, err := EncodeActivity(&a)
		assertReason(t, name, err, ReasonLengthLimit)
	}
	version := *maximum
	version.ProtocolVersion = 4
	_, err = EncodeActivity(&version)
	assertReason(t, "protocol four", err, ReasonVersionUnsupported)
	kind := *maximum
	kind.Type = ActivityType(0x0001_0000)
	_, err = EncodeActivity(&kind)
	assertReason(t, "zero ordinal", err, ReasonUnknownActivity)
	if _, err := NewActivityType(ModuleWeb+1, 1); err == nil {
		t.Fatalf("unknown module accepted")
	}
	if kind, err := NewActivityType(ModuleAsset, 5); err != nil || uint32(kind) != 0x0001_0005 || kind.Module() != ModuleAsset || kind.Ordinal() != 5 {
		t.Fatalf("asset send type %x (%v)", kind, err)
	}
}

func TestAccountIDsMatchRustAndChain(t *testing.T) {
	file := loadVectors(t)
	for _, account := range file.Accounts {
		got, err := AccountID([]byte(account.Name))
		if err != nil {
			t.Fatalf("%s: %v", account.Name, err)
		}
		if want := mustHex32(t, account.AccountID); got != want {
			t.Fatalf("%s: %x, Rust derives %x", account.Name, got, want)
		}
		chain, err := codec.DeriveAccountID([]byte(account.Name))
		if bytes.Contains([]byte(account.Name), []byte(assetMarker)) {
			if !errors.Is(err, codec.ErrAccountIdentity) {
				t.Fatalf("%s: chain now derives %x (%v); compare it with the Rust identifier", account.Name, chain, err)
			}
			continue
		}
		if err != nil || chain != got {
			t.Fatalf("%s: %x, chain derives %x (%v)", account.Name, got, chain, err)
		}
	}
	for _, key := range [][32]byte{{}, {0x01}, {0xff, 0xee, 0xdd}} {
		did := DIDFromKey(key)
		for _, name := range []string{
			MainAccountName(did),
			"agent:" + did + ":budget:weekly",
			"agent:" + did + ":escrow:e1",
			"agent:" + did + ":margin:p9",
		} {
			got, err := AccountID([]byte(name))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			chain, err := codec.DeriveAccountID([]byte(name))
			if err != nil || chain != got {
				t.Fatalf("%s: %x, chain derives %x (%v)", name, got, chain, err)
			}
		}
	}
	for _, name := range []string{
		"",
		"agent:did:layerx:Alice:main",
		"agent::main",
		"agent:did:layerx:alice:unknown:x",
		"agent:did:layerx:alice:stream:s1",
		"agent:did:layerx:alice:budget:a:b",
		"agent:did::alice:main",
		"system:liquidity:",
		"system:liquidity:a:b",
		"system:other",
		"agent:did:layerx:alice:asset:" + string(bytes.Repeat([]byte("AB"), 32)),
		"agent:" + string(bytes.Repeat([]byte("a"), 256)) + ":main",
	} {
		if _, err := AccountID([]byte(name)); !errors.Is(err, ErrAccountName) {
			t.Fatalf("%q: accepted or wrong error %v", name, err)
		}
	}
}

func TestDIDAndAccountNames(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i * 7)
	}
	did := DIDFromKey(key)
	if len(did) != len(DIDPrefix)+64 || did != "did:layerx:"+hex.EncodeToString(key[:]) {
		t.Fatalf("did %q", did)
	}
	back, err := KeyFromDID(did)
	if err != nil || back != key {
		t.Fatalf("did key round trip: %x %v", back, err)
	}
	for _, bad := range []string{"did:layerx:" + hex.EncodeToString(key[:31]), "did:other:" + hex.EncodeToString(key[:]), did[:len(did)-1] + "G", "did:layerx:" + string(bytes.ToUpper([]byte(hex.EncodeToString(key[:]))))} {
		if _, err := KeyFromDID(bad); !errors.Is(err, ErrDID) {
			t.Fatalf("%q accepted", bad)
		}
	}
	native := [32]byte{}
	token := [32]byte{0x7b}
	main, err := AssetAccountName(did, native, native)
	if err != nil || main != MainAccountName(did) {
		t.Fatalf("native asset account %q (%v)", main, err)
	}
	asset, err := AssetAccountName(did, token, native)
	if err != nil || asset != "agent:"+did+":asset:"+hex.EncodeToString(token[:]) {
		t.Fatalf("token asset account %q (%v)", asset, err)
	}
	for _, bad := range []string{"", ":" + did, did + ":", "did::x", "did:layerx:X", "did:x:asset:y"} {
		if _, err := AssetAccountName(bad, token, native); !errors.Is(err, ErrAccountName) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestBindMessageAgainstChainVectors(t *testing.T) {
	fixture, err := testvectors.LoadPaxeerBind()
	if err != nil {
		t.Fatalf("load bind fixture: %v", err)
	}
	if DIDFromKey(fixture.PublicKey) != fixture.Did || MainAccountName(fixture.Did) != fixture.MainAccountName {
		t.Fatalf("fixture did or main account name differs")
	}
	id, err := AccountID([]byte(fixture.MainAccountName))
	if err != nil || id != fixture.MainAccountID {
		t.Fatalf("main account id %x, fixture %x (%v)", id, fixture.MainAccountID, err)
	}
	valid := 0
	for _, bind := range fixture.Binds {
		message := BindMessage(bind.ChainID, bind.EVMAddress, bind.Nonce)
		if len(message) != BindMessageLength || !bytes.Equal(message, bind.Message) {
			t.Fatalf("%s: message %x, fixture %x", bind.Name, message, bind.Message)
		}
		parsed, err := ParseBindMessage(message)
		if err != nil || parsed != (Binding{ChainID: bind.ChainID, EVMAddress: bind.EVMAddress, Nonce: bind.Nonce}) {
			t.Fatalf("%s: parse %+v (%v)", bind.Name, parsed, err)
		}
		if got := ed25519.Verify(ed25519.PublicKey(fixture.PublicKey[:]), message, bind.Signature[:]); got != bind.Valid {
			t.Fatalf("%s: signature verdict %v, fixture %v", bind.Name, got, bind.Valid)
		}
		if bind.Valid {
			valid++
		}
	}
	if valid == 0 || valid == len(fixture.Binds) {
		t.Fatalf("fixture must hold valid and invalid cases, has %d of %d valid", valid, len(fixture.Binds))
	}
	if BindMessageLength != 77 {
		t.Fatalf("bind message length %d", BindMessageLength)
	}
	good := fixture.Binds[0].Message
	wrongPrefix := append([]byte{}, good...)
	wrongPrefix[0] = 'M'
	wideChain := append([]byte{}, good...)
	wideChain[len(BindDomain)] = 1
	for name, message := range map[string][]byte{
		"short":        good[:len(good)-1],
		"long":         append(append([]byte{}, good...), 0),
		"empty":        nil,
		"wrong prefix": wrongPrefix,
		"wide chain":   wideChain,
	} {
		if _, err := ParseBindMessage(message); !errors.Is(err, ErrBindMessage) {
			t.Fatalf("%s: parsed", name)
		}
	}
}

func (g grantVector) grant(t *testing.T) Grant {
	t.Helper()
	return Grant{
		From:               mustHex32(t, g.From),
		Recipient:          mustHex32(t, g.Recipient),
		Asset:              mustHex32(t, g.Asset),
		PerDrawMaximum:     mustUint128(t, g.PerDrawMaximum),
		Allowance:          mustUint128(t, g.Allowance),
		Recurring:          g.Recurring,
		WindowLength:       mustUint64(t, g.WindowLength),
		Expiration:         mustUint64(t, g.Expiration),
		PurposeHash:        mustHex32(t, g.PurposeHash),
		HasReference:       g.HasReference,
		ReferenceHash:      mustHex32(t, g.ReferenceHash),
		RevocationSequence: mustUint64(t, g.RevocationSequence),
		PublicKey:          mustHex32(t, g.PublicKey),
	}
}

func TestGrantAndReceivePreimages(t *testing.T) {
	file := loadVectors(t)
	for _, v := range file.Grants {
		got, err := GrantPreimage(v.grant(t))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if got != mustHex32(t, v.Preimage) || got != mustHex32(t, v.GrantID) {
			t.Fatalf("%s: grant preimage %x differs from the Rust side", v.Name, got)
		}
	}
	for _, v := range file.Receives {
		grant := v.Grant.grant(t)
		grantID, err := GrantPreimage(grant)
		if err != nil || grantID != mustHex32(t, v.Grant.GrantID) {
			t.Fatalf("%s: grant id %x (%v)", v.Name, grantID, err)
		}
		context := GrantContextHash(grant)
		if context != mustHex32(t, v.ContextHash) {
			t.Fatalf("%s: context hash %x differs", v.Name, context)
		}
		receive := Receive{
			From:              grant.From,
			To:                grant.Recipient,
			Asset:             grant.Asset,
			Amount:            mustUint128(t, v.Amount),
			Grant:             grantID,
			Sequence:          mustUint64(t, v.Sequence),
			IdempotencyKey:    mustHex32(t, v.IdempotencyKey),
			ContextHash:       context,
			AuthorizationKind: v.AuthorizationKind,
			Controller:        grant.Recipient,
			SignedContextHash: context,
			NetworkID:         v.NetworkID,
			ProtocolVersion:   v.ProtocolVersion,
		}
		got, err := ReceivePreimage(receive)
		if err != nil || got != mustHex32(t, v.Preimage) {
			t.Fatalf("%s: receive preimage %x differs (%v)", v.Name, got, err)
		}
		refusals := map[string]func(r *Receive){
			"zero amount":        func(r *Receive) { r.Amount = Uint128{} },
			"self receive":       func(r *Receive) { r.To = r.From; r.Controller = r.From },
			"kind zero":          func(r *Receive) { r.AuthorizationKind = 0 },
			"kind seven":         func(r *Receive) { r.AuthorizationKind = 7 },
			"foreign controller": func(r *Receive) { r.Controller = r.From },
			"context mismatch":   func(r *Receive) { r.SignedContextHash[0] ^= 1 },
			"network zero":       func(r *Receive) { r.NetworkID = 0 },
			"protocol four":      func(r *Receive) { r.ProtocolVersion = 4 },
		}
		for name, change := range refusals {
			r := receive
			change(&r)
			if _, err := ReceivePreimage(r); !errors.Is(err, ErrReceive) {
				t.Fatalf("%s: %s accepted", v.Name, name)
			}
		}
	}
	base := file.Grants[0].grant(t)
	grantRefusals := map[string]func(g *Grant){
		"zero per draw":       func(g *Grant) { g.PerDrawMaximum = Uint128{} },
		"zero allowance":      func(g *Grant) { g.Allowance = Uint128{} },
		"zero expiration":     func(g *Grant) { g.Expiration = 0 },
		"recurring no window": func(g *Grant) { g.Recurring = true },
		"window no recurring": func(g *Grant) { g.WindowLength = 60 },
		"zero recipient":      func(g *Grant) { g.Recipient = [32]byte{} },
		"zero asset":          func(g *Grant) { g.Asset = [32]byte{} },
		"zero purpose":        func(g *Grant) { g.PurposeHash = [32]byte{} },
	}
	for name, change := range grantRefusals {
		g := base
		change(&g)
		if _, err := GrantPreimage(g); !errors.Is(err, ErrGrant) {
			t.Fatalf("grant %s accepted", name)
		}
	}
}
