package catalog

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"testing"
)

func registry(t *testing.T) *Registry {
	t.Helper()
	r, e := New([]Deployment{{125, common.HexToAddress("0x1"), "SidioraERC20", revision(t, "SidioraERC20"), 10, 20, true}, {125, common.HexToAddress("0x2"), "SidioraNFT", revision(t, "SidioraNFT"), 10, 0, true}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestTransferOriginAndLayout(t *testing.T) {
	r := registry(t)
	a := common.HexToAddress("0xaa")
	b := common.HexToAddress("0xbb")
	for _, role := range []string{"SidioraERC20", "SidioraNFT"} {
		ev := r.abis[role].Events["Transfer"]
		l := types.Log{Address: common.HexToAddress("0x1"), BlockNumber: 12, Topics: []common.Hash{ev.ID, common.BytesToHash(a.Bytes()), common.BytesToHash(b.Bytes())}}
		if role == "SidioraNFT" {
			l.Address = common.HexToAddress("0x2")
			l.Topics = append(l.Topics, common.BigToHash(big.NewInt(9)))
		} else {
			l.Data = common.LeftPadBytes(big.NewInt(9).Bytes(), 32)
		}
		out := r.Decode(125, l)
		if out.Status != "decoded" || out.Fields["to"] != b {
			t.Fatalf("%s %#v", role, out)
		}
		l.Address = common.HexToAddress("0x3")
		if r.Decode(125, l).Status != "origin_unrecognized" {
			t.Fatal("trusted unknown emitter")
		}
		l.Address = common.HexToAddress("0x1")
		l.Topics = append(l.Topics, common.Hash{})
		if r.Decode(125, l).Status != "quarantined" {
			t.Fatal("invalid layout accepted")
		}
	}
}
func TestBoundsAndActivation(t *testing.T) {
	r := registry(t)
	ev := r.abis["SidioraERC20"].Events["Transfer"]
	l := types.Log{Address: common.HexToAddress("0x1"), BlockNumber: 21, Topics: []common.Hash{ev.ID}}
	if r.Decode(125, l).Status != "origin_unrecognized" {
		t.Fatal("expired version accepted")
	}
	l.BlockNumber = 12
	l.Data = make([]byte, MaxPayload+1)
	if r.Decode(125, l).Status != "quarantined" {
		t.Fatal("oversize accepted")
	}
	if _, e := New([]Deployment{{125, l.Address, "Unknown", "v1", 0, 0, true}}); e == nil {
		t.Fatal("unknown family accepted")
	}
}
func TestEveryReviewedABIParses(t *testing.T) {
	bs, e := Bundles()
	if e != nil || len(bs) != 24 {
		t.Fatal(e, len(bs))
	}
	for i, b := range bs {
		if b.Compiler == "" || b.SourceHash == "" || b.ArtifactHash == "" {
			t.Fatal("missing provenance", i)
		}
	}
	registry(t)
}
func FuzzDecode(f *testing.F) {
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := registry(t)
		ev := r.abis["SidioraERC20"].Events["Transfer"]
		r.Decode(125, types.Log{Address: common.HexToAddress("0x1"), BlockNumber: 12, Topics: []common.Hash{ev.ID, {}, {}}, Data: data})
	})
}
func TestDeterministicGenerationIndex(t *testing.T) {
	a, e := Index()
	if e != nil {
		t.Fatal(e)
	}
	b, e := Index()
	if e != nil || string(a) != string(b) {
		t.Fatal("nondeterministic index", e)
	}
}
func TestUnknownArchivedCopy(t *testing.T) {
	r := registry(t)
	l := types.Log{Address: common.HexToAddress("0x1"), BlockNumber: 12, Topics: []common.Hash{{1}}, Data: []byte{1, 2}}
	out := r.Decode(125, l)
	l.Data[0] = 9
	if out.Status != "signature_unrecognized" || out.Raw.Data[0] != 1 {
		t.Fatal("raw not independently archived")
	}
}

func revision(t *testing.T, family string) string {
	t.Helper()
	bs, e := Bundles()
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range bs {
		if b.Name == family {
			return b.ABIHash
		}
	}
	t.Fatal("family missing", family)
	return ""
}
func TestRevisionIdentityAndExclusiveRetirement(t *testing.T) {
	address := common.HexToAddress("0x1")
	erc20 := revision(t, "SidioraERC20")
	nft := revision(t, "SidioraNFT")
	if _, e := New([]Deployment{{125, address, "SidioraERC20", "arbitrary-historical-v1", 10, 20, true}}); e == nil {
		t.Fatal("unknown ABI revision accepted")
	}
	r, e := New([]Deployment{{125, address, "SidioraERC20", erc20, 10, 20, true}, {125, address, "SidioraNFT", nft, 20, 30, true}})
	if e != nil {
		t.Fatal("adjacent epochs rejected", e)
	}
	erc20Event := r.abis["SidioraERC20"].Events["Transfer"]
	l := types.Log{Address: address, BlockNumber: 19, Topics: []common.Hash{erc20Event.ID, {}, {}}, Data: make([]byte, 32)}
	if got := r.Decode(125, l); got.Status != "decoded" || got.Version != erc20 {
		t.Fatal("accepted revision not selected", got)
	}
	l.BlockNumber = 20
	if got := r.Decode(125, l); got.Status != "quarantined" || got.Version != nft || got.EmitterRole != "SidioraNFT" {
		t.Fatal("old ABI selected at retirement", got)
	}
	l.Topics = append(l.Topics, common.Hash{})
	l.Data = nil
	if got := r.Decode(125, l); got.Status != "decoded" || got.Version != nft {
		t.Fatal("new epoch missing at boundary", got)
	}
	l.BlockNumber = 30
	if got := r.Decode(125, l); got.Status != "origin_unrecognized" {
		t.Fatal("retired epoch accepted", got)
	}
	if _, e := New([]Deployment{{125, address, "SidioraERC20", erc20, 20, 20, true}}); e == nil {
		t.Fatal("empty epoch accepted")
	}
}

func TestNoncanonicalTransferBytesAndIndexedAddressesAreQuarantined(t *testing.T) {
	r := registry(t)
	event := r.abis["SidioraERC20"].Events["Transfer"]
	l := types.Log{Address: common.HexToAddress("0x1"), BlockNumber: 12, Topics: []common.Hash{event.ID, {}, {}}, Data: make([]byte, 64)}
	if r.Decode(125, l).Status != "quarantined" {
		t.Fatal("trailing ABI words accepted")
	}
	l.Data = make([]byte, 32)
	l.Topics[1][0] = 1
	if r.Decode(125, l).Status != "quarantined" {
		t.Fatal("noncanonical indexed address accepted")
	}
}
