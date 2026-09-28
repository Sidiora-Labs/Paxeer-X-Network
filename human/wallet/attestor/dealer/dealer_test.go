package dealer

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"

	tss "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

const (
	secpKeyHex = "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"
	edSeedHex  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
)

var ids = []string{"n1", "n2", "n3", "n4", "n5"}

func reconstruct(t *testing.T, bundles []ShareBundle) *big.Int {
	t.Helper()
	ec, err := bundles[0].Curve.Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	order := ec.Params().N
	bks := make(birkhoffinterpolation.BkParameters, len(bundles))
	for i, b := range bundles {
		bks[i] = b.Bks[b.ParticipantID]
	}
	co, err := bks.ComputeBkCoefficient(bundles[0].Threshold, order)
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

func TestSplitSecp256k1ThroughExportedPackage(t *testing.T) {
	key, err := crypto.HexToECDSA(secpKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	secret := new(big.Int).Set(key.D)
	var curve Curve = Secp256k1
	bundles, pub, err := Split(curve, secret, ids)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Sign() != 0 {
		t.Fatal("secret not wiped by split")
	}
	if pub.GetX().Cmp(key.PublicKey.X) != 0 || pub.GetY().Cmp(key.PublicKey.Y) != 0 {
		t.Fatal("public key differs from the imported key")
	}
	if len(bundles) != tss.Participants {
		t.Fatalf("got %d bundles", len(bundles))
	}
	for i, b := range bundles {
		var internal tss.ShareBundle = b
		if err := internal.Validate(); err != nil {
			t.Fatalf("bundle %d: %v", i, err)
		}
		if b.Threshold != tss.Threshold || b.Curve != tss.Secp256k1 {
			t.Fatalf("bundle %d metadata wrong", i)
		}
	}
	if reconstruct(t, []ShareBundle{bundles[1], bundles[3], bundles[4]}).Cmp(key.D) != 0 {
		t.Fatal("three shares do not reconstruct the key")
	}
	short := birkhoffinterpolation.BkParameters{bundles[0].Bks["n1"], bundles[1].Bks["n2"]}
	if _, err := short.ComputeBkCoefficient(bundles[0].Threshold, key.Curve.Params().N); err == nil {
		t.Fatal("two shares yield interpolation coefficients")
	}
}

func TestSplitEd25519ThroughExportedPackage(t *testing.T) {
	seed, _ := hex.DecodeString(edSeedHex)
	digest := sha512.Sum512(seed)
	digest[0] &= 248
	digest[31] &= 127
	digest[31] |= 64
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[i] = digest[31-i]
	}
	ec, err := Curve(Ed25519).Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	secret := new(big.Int).SetBytes(be)
	secret.Mod(secret, ec.Params().N)
	expected := new(big.Int).Set(secret)
	bundles, pub, err := Split(Ed25519, secret, ids)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := tss.EncodeEd25519(pub)
	if err != nil {
		t.Fatal(err)
	}
	std := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if hex.EncodeToString(encoded) != hex.EncodeToString(std) {
		t.Fatalf("public key %x differs from %x", encoded, std)
	}
	for i, b := range bundles {
		if err := b.Validate(); err != nil {
			t.Fatalf("bundle %d: %v", i, err)
		}
	}
	if reconstruct(t, []ShareBundle{bundles[4], bundles[0], bundles[2]}).Cmp(expected) != 0 {
		t.Fatal("three shares do not reconstruct the key")
	}
}

func TestSplitRefusesThroughExportedPackage(t *testing.T) {
	if _, _, err := Split(Curve(7), big.NewInt(5), ids); !errors.Is(err, tss.ErrUnknownCurve) {
		t.Fatalf("unknown curve: %v", err)
	}
	if _, _, err := Split(Secp256k1, big.NewInt(5), ids[:3]); !errors.Is(err, tss.ErrParticipants) {
		t.Fatalf("three ids: %v", err)
	}
}
