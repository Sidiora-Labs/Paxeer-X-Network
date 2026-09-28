package dealer

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
)

const (
	secpKeyHex = "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"
	edSeedHex  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	edPubHex   = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
)

var participantIDs = []string{"p1", "p2", "p3", "p4", "p5"}

func secpScalar(t *testing.T) *big.Int {
	t.Helper()
	b, err := hex.DecodeString(secpKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return new(big.Int).SetBytes(b)
}

func edScalar(t *testing.T) *big.Int {
	t.Helper()
	seed, err := hex.DecodeString(edSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Ed25519ScalarFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func interpolate(t *testing.T, curve Curve, bundles []ShareBundle) *big.Int {
	t.Helper()
	ec, err := curve.Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	order := ec.Params().N
	bks := make(birkhoffinterpolation.BkParameters, len(bundles))
	for i, b := range bundles {
		bks[i] = b.Bks[b.ParticipantID]
	}
	co, err := bks.ComputeBkCoefficient(Threshold, order)
	if err != nil {
		t.Fatal(err)
	}
	sum := new(big.Int)
	for i, b := range bundles {
		sum.Add(sum, new(big.Int).Mul(co[i], b.Share))
		sum.Mod(sum, order)
	}
	return sum
}

func TestSplitSecp256k1MatchesKnownKey(t *testing.T) {
	key, err := crypto.HexToECDSA(secpKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	expected := new(big.Int).Set(key.D)
	secret := secpScalar(t)
	bundles, pub, err := Split(Secp256k1, secret, participantIDs)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Sign() != 0 {
		t.Fatal("secret scalar not wiped after split")
	}
	if pub.GetX().Cmp(key.PublicKey.X) != 0 || pub.GetY().Cmp(key.PublicKey.Y) != 0 {
		t.Fatal("public key does not match the imported key")
	}
	if len(bundles) != Participants {
		t.Fatalf("got %d bundles", len(bundles))
	}
	for i, b := range bundles {
		if b.ParticipantID != participantIDs[i] || b.Threshold != Threshold || b.Curve != Secp256k1 {
			t.Fatalf("bundle %d metadata wrong", i)
		}
		if b.Bks[b.ParticipantID].GetRank() != 0 || b.Bks[b.ParticipantID].GetX().Cmp(big.NewInt(int64(i+1))) != 0 {
			t.Fatalf("bundle %d Birkhoff parameter wrong", i)
		}
		if !b.PublicKey.Equal(pub) {
			t.Fatalf("bundle %d public key wrong", i)
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("bundle %d: %v", i, err)
		}
	}
	for _, quorum := range [][]int{{0, 1, 2}, {1, 3, 4}, {0, 2, 4}, {0, 1, 2, 3, 4}} {
		subset := make([]ShareBundle, len(quorum))
		for i, q := range quorum {
			subset[i] = bundles[q]
		}
		if interpolate(t, Secp256k1, subset).Cmp(expected) != 0 {
			t.Fatalf("quorum %v does not reconstruct the key", quorum)
		}
	}
}

func TestSplitEd25519MatchesKnownKey(t *testing.T) {
	seed, _ := hex.DecodeString(edSeedHex)
	wantPub, _ := hex.DecodeString(edPubHex)
	std := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if hex.EncodeToString(std) != edPubHex {
		t.Fatal("standard library public key differs from the vector")
	}
	secret := edScalar(t)
	expected := new(big.Int).Set(secret)
	bundles, pub, err := Split(Ed25519, secret, participantIDs)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Sign() != 0 {
		t.Fatal("secret scalar not wiped after split")
	}
	encoded, err := EncodeEd25519(pub)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(encoded) != hex.EncodeToString(wantPub) {
		t.Fatalf("public key %x does not match %x", encoded, wantPub)
	}
	for i, b := range bundles {
		if err := b.Validate(); err != nil {
			t.Fatalf("bundle %d: %v", i, err)
		}
	}
	if interpolate(t, Ed25519, []ShareBundle{bundles[4], bundles[0], bundles[3]}).Cmp(expected) != 0 {
		t.Fatal("quorum does not reconstruct the key")
	}
}

func TestSplitRefusesBadInput(t *testing.T) {
	cases := []struct {
		name  string
		curve Curve
		ids   []string
		want  error
	}{
		{"unknown curve", Curve(9), participantIDs, ErrUnknownCurve},
		{"four ids", Secp256k1, participantIDs[:4], ErrParticipants},
		{"duplicate id", Secp256k1, []string{"p1", "p2", "p3", "p4", "p1"}, ErrParticipants},
		{"empty id", Ed25519, []string{"p1", "p2", "", "p4", "p5"}, ErrParticipants},
	}
	for _, c := range cases {
		secret := secpScalar(t)
		_, _, err := Split(c.curve, secret, c.ids)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: got %v want %v", c.name, err, c.want)
		}
		if secret.Sign() != 0 {
			t.Fatalf("%s: secret not wiped on refusal", c.name)
		}
	}
	over := new(big.Int).Set(mustCurve(t, Ed25519).Params().N)
	if _, _, err := Split(Ed25519, over, participantIDs); !errors.Is(err, ErrScalarRange) {
		t.Fatalf("scalar equal to the order: %v", err)
	}
	if _, _, err := Split(Secp256k1, big.NewInt(0), participantIDs); !errors.Is(err, ErrScalarRange) {
		t.Fatalf("zero scalar: %v", err)
	}
	if _, err := Ed25519ScalarFromSeed(make([]byte, 31)); !errors.Is(err, ErrSeedLength) {
		t.Fatalf("short seed: %v", err)
	}
}

func TestValidateRefusesMismatchedShare(t *testing.T) {
	for _, curve := range []Curve{Secp256k1, Ed25519} {
		secret := secpScalar(t)
		if curve == Ed25519 {
			secret = edScalar(t)
		}
		bundles, _, err := Split(curve, secret, participantIDs)
		if err != nil {
			t.Fatal(err)
		}
		tampered := bundles[2].Clone()
		tampered.Share.Add(tampered.Share, big.NewInt(1))
		if err := tampered.Validate(); !errors.Is(err, ErrShareMismatch) {
			t.Fatalf("%s: tampered share: %v", curve, err)
		}
		if err := bundles[2].Validate(); err != nil {
			t.Fatalf("%s: clone mutated the original: %v", curve, err)
		}
		forged := bundles[0].Clone()
		ec := mustCurve(t, curve)
		forged.PartialPublicKeys["p4"] = pt.ScalarBaseMult(ec, big.NewInt(7))
		if err := forged.ValidatePublicData(); !errors.Is(err, ErrPublicKeyMismatch) {
			t.Fatalf("%s: forged partial public key: %v", curve, err)
		}
	}
}

func mustCurve(t *testing.T, c Curve) elliptic.Curve {
	t.Helper()
	ec, err := c.Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	return ec
}
