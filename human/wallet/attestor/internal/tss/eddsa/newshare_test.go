package eddsa

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

func TestNewKeyShareFromDealerBundle(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, 32)
	secret, err := dealer.Ed25519ScalarFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"a", "b", "c", "d", "e"}
	bundles, pub, err := dealer.Split(dealer.Ed25519, secret, ids)
	if err != nil {
		t.Fatal(err)
	}
	want, err := dealer.EncodeEd25519(pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bundles {
		k, err := NewKeyShare(b.ParticipantID, b.Threshold, b.PublicKey, b.Share, b.Bks, b.PartialPublicKeys)
		if err != nil {
			t.Fatalf("%s: %v", b.ParticipantID, err)
		}
		got := k.PublicKeyBytes()
		if !bytes.Equal(got[:], want) || k.ID() != b.ParticipantID || k.Threshold() != dealer.Threshold {
			t.Fatalf("%s: key share does not carry the bundle's public data", b.ParticipantID)
		}
		pub2, share2, bks2, ys2 := k.Material()
		if !pub2.Equal(b.PublicKey) || share2.Cmp(b.Share) != 0 || len(bks2) != len(b.Bks) || len(ys2) != len(b.PartialPublicKeys) {
			t.Fatalf("%s: material differs from the bundle", b.ParticipantID)
		}
		for pid, y := range ys2 {
			if !y.Equal(b.PartialPublicKeys[pid]) || bks2[pid].GetX().Cmp(b.Bks[pid].GetX()) != 0 {
				t.Fatalf("%s: material for %s differs", b.ParticipantID, pid)
			}
		}
		share2.SetInt64(0)
		if _, again, _, _ := k.Material(); again.Cmp(b.Share) != 0 {
			t.Fatalf("%s: material shares storage with the key share", b.ParticipantID)
		}
		encoded, err := k.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.PublicKeyBytes() != got {
			t.Fatalf("%s: round trip changed the public key", b.ParticipantID)
		}
	}
	b := bundles[0]
	wrong := new(big.Int).Add(b.Share, big.NewInt(1))
	if _, err := NewKeyShare(b.ParticipantID, b.Threshold, b.PublicKey, wrong, b.Bks, b.PartialPublicKeys); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("mismatched share accepted: %v", err)
	}
	if _, err := NewKeyShare(b.ParticipantID, b.Threshold, nil, b.Share, b.Bks, b.PartialPublicKeys); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("missing public key accepted: %v", err)
	}
}
