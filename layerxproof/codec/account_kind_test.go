package codec_test

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

// agentAssetKindVectors are the names, kinds and identifiers pinned by the
// kernel's tests/ledger/test_account_id.c for lx_account_id_from_string.
var agentAssetKindVectors = []struct {
	name string
	kind uint8
	id   string
}{
	{"agent:did:key:alice:asset:0300000000000000000000000000000000000000000000000000000000000000", 14, "cd99e00b8c6897b6ba6e250492de75103b65fcb5e7c48313bbb9183237a2c3e7"},
	{"agent:did:key:alice:main", 1, "efc9802f76722dfc48ebfed35bfd8b20dbc2775fe2f027d6cbd595aff1307454"},
	{"agent:did:key:alice:budget:daily", 2, "6f27c8a878c055eeb4056e5bf32864cbf113748af82e6b72e2060c39b4a18829"},
	{"agent:did:key:alice:escrow:order-7", 3, "a781282c755f899aeff3881c53d68b14ed796f97c5f05d9b5b9da90d7c7aecc2"},
	{"agent:did:key:alice:stream:salary", 4, "5ab8f69df1347e5fe0f8f8fbe2db3d7e3d84d5af2c6641033aef5b08ff95a636"},
	{"agent:did:key:alice:margin:btc-usd", 5, "4fe1c45e2186de26cb1b5970605e3b30e37b56b8ec21e08f3b9c4c721dd091b3"},
	{"system:liquidity:btc-usd", 6, "b3b2bb3b51a162524acd6f07af8f0f3993c35144c6fa1ad41493c9384c5c42de"},
	{"system:funding:btc-usd:long", 7, "fb75d9a061bf420ddf370b14d320d78b972d448959f6b4b46bb00b0a6b6d5869"},
	{"system:funding:btc-usd:short", 8, "db2afd847c8e447f9cdab3034072e2ff8bf8b2c49e65dccaede6906b0d94bbe3"},
	{"system:insurance", 9, "4c16a537b274bd21ce39ebc501c464f4c7ac6a60d7eb4d396948570cbb764568"},
	{"system:fees", 10, "dbf940aa4c1f587b73f3b65da0dec92760e0bd0187f1f33c659ea043caebdcdf"},
	{"system:paxeer-reserve", 11, "6e0e5cca5cfaa1b20ddd1c6174321eeaf00dd74bce2adcd78b61e15aa9e26f7c"},
	{"system:paxeer-withdrawals", 12, "36e5bfab4a0143c723aaac6a61d6eae554e684f94cfff2a24f5fdf0574c468f7"},
}

func TestAccountKindAgentAsset(t *testing.T) {
	for _, vector := range agentAssetKindVectors {
		kind, ok := codec.AccountKind([]byte(vector.name))
		if !ok || kind != vector.kind {
			t.Fatalf("AccountKind(%q) = %d, %v; want %d, true", vector.name, kind, ok, vector.kind)
		}
		id, err := codec.DeriveAccountID([]byte(vector.name))
		if err != nil {
			t.Fatalf("DeriveAccountID(%q): %v", vector.name, err)
		}
		if got := hex.EncodeToString(id[:]); got != vector.id {
			t.Fatalf("DeriveAccountID(%q) = %s; want %s", vector.name, got, vector.id)
		}
	}

	const prefix = "agent:did:key:alice:asset:"
	refused := []string{
		prefix + "03000000000000000000000000000000000000000000000000000000000000AB",
		prefix + strings.Repeat("0", 63),
		prefix + strings.Repeat("0", 63) + "g",
		"agent::asset:" + strings.Repeat("0", 64),
	}
	for _, name := range refused {
		if kind, ok := codec.AccountKind([]byte(name)); ok {
			t.Fatalf("AccountKind(%q) = %d, true; want refusal", name, kind)
		}
		if _, err := codec.DeriveAccountID([]byte(name)); !errors.Is(err, codec.ErrAccountIdentity) {
			t.Fatalf("DeriveAccountID(%q) error = %v; want %v", name, err, codec.ErrAccountIdentity)
		}
	}
}
