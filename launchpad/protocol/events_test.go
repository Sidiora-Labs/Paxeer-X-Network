package protocol

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestRegistryNonEmpty(t *testing.T) {
	t.Parallel()
	if Topic0Count() == 0 {
		t.Fatal("event registry is empty")
	}
	if got := len(Events()); got != Topic0Count() {
		t.Fatalf("Events() returned %d, Topic0Count() = %d", got, Topic0Count())
	}
}

// TestEveryTopicIsKeccakOfSig independently recomputes each topic0 from its
// canonical signature via crypto.Keccak256 and checks it equals both the
// registry key and the ABI event ID — verifying the registry through a second
// code path rather than trusting abigen alone.
func TestEveryTopicIsKeccakOfSig(t *testing.T) {
	t.Parallel()
	for _, d := range Events() {
		want := crypto.Keccak256Hash([]byte(d.Sig))
		if d.Topic0 != want {
			t.Errorf("%s (%s): Topic0 = %s, keccak(sig) = %s", d.Name, d.Sig, d.Topic0.Hex(), want.Hex())
		}
		if d.Event.ID != d.Topic0 {
			t.Errorf("%s: Event.ID %s != Topic0 %s", d.Name, d.Event.ID.Hex(), d.Topic0.Hex())
		}
		if len(d.Contracts) == 0 {
			t.Errorf("%s: no declaring contracts recorded", d.Name)
		}
	}
}

// TestKnownTopics pins the registry to well-known external golden topic0 values
// (OpenZeppelin Ownable + ERC-1967 proxy), both declared by PoolBeacon.
func TestKnownTopics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		topic0   string
		name     string
		contract string
	}{
		{"0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0", "OwnershipTransferred", "PoolBeacon"},
		{"0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b", "Upgraded", "PoolBeacon"},
	}
	for _, tc := range cases {
		d, ok := LookupEvent(common.HexToHash(tc.topic0))
		if !ok {
			t.Errorf("LookupEvent(%s) not found", tc.topic0)
			continue
		}
		if d.Name != tc.name {
			t.Errorf("topic %s: name = %q, want %q", tc.topic0, d.Name, tc.name)
		}
		found := false
		for _, c := range d.Contracts {
			if c == tc.contract {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("topic %s: %q not among declaring contracts %v", tc.topic0, tc.contract, d.Contracts)
		}
	}
}

func TestLookupUnknownEvent(t *testing.T) {
	t.Parallel()
	// A topic0 that is not keccak of any real event signature.
	if d, ok := LookupEvent(common.HexToHash("0xdeadbeef")); ok {
		t.Errorf("unexpected hit for unknown topic: %+v", d)
	}
}

func TestEventsDeterministicOrder(t *testing.T) {
	t.Parallel()
	a, b := Events(), Events()
	if len(a) != len(b) {
		t.Fatalf("Events() len unstable: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Topic0 != b[i].Topic0 {
			t.Fatalf("Events() order unstable at %d: %s vs %s", i, a[i].Topic0.Hex(), b[i].Topic0.Hex())
		}
	}
}
