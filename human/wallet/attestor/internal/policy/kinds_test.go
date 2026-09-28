package policy

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
)

func kernelDocument(t *testing.T) *Document {
	t.Helper()
	raw := strings.Replace(string(testDocument(t)), `"eip7702_authorization"]`, `"eip7702_authorization", "lx_activity", "lx_bind", "lx_grant"]`, 1)
	doc, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestRegisterKernelKinds(t *testing.T) {
	h := newHarness(t)
	engine := New(kernelDocument(t))
	for _, kind := range KernelKinds() {
		expect(t, engine.Evaluate(testAccount, Request{Kind: kind, View: []byte{1}}, h.ledger), CodeUnknownKind)
	}

	seen := map[string]common.Address{}
	capture := func(kind string) Inspector {
		return func(ctx Context, _ any) (Inspection, error) {
			seen[kind] = ctx.Account
			if ctx.ChainID.Cmp(chainID) != 0 {
				t.Fatalf("%s inspector saw chain %v", kind, ctx.ChainID)
			}
			return Inspection{}, nil
		}
	}
	refusing := func(Context, any) (Inspection, error) {
		return Inspection{}, &Refusal{Code: "stale_bind_nonce", Reason: "nonce differs from the chain"}
	}
	if err := engine.RegisterKernel(capture(KindLXActivity), nil, capture(KindLXGrant)); err == nil {
		t.Fatal("kernel kinds registered without a bind inspector")
	}
	if err := engine.RegisterKernel(capture(KindLXActivity), refusing, capture(KindLXGrant)); err != nil {
		t.Fatal(err)
	}
	if err := engine.RegisterKernel(capture(KindLXActivity), refusing, capture(KindLXGrant)); err == nil {
		t.Fatal("kernel kinds registered twice")
	}

	expect(t, engine.Evaluate(testAccount, Request{Kind: KindLXActivity, View: []byte{1}}, h.ledger), CodeAllowed)
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindLXGrant, View: []byte{1}}, h.ledger), CodeAllowed)
	h.now = h.now.Add(RateWindow)
	refused := engine.Evaluate(testAccount, Request{Kind: KindLXBind, View: []byte{1}}, h.ledger)
	expect(t, refused, "stale_bind_nonce")
	for _, kind := range []string{KindLXActivity, KindLXGrant} {
		if seen[kind] != common.HexToAddress(testAccount) {
			t.Fatalf("%s inspector saw account %s", kind, seen[kind].Hex())
		}
	}
	expect(t, engine.Evaluate(strictUser, Request{Kind: KindLXActivity, View: []byte{1}}, h.ledger), CodeKindNotAllowed)

	other := New(kernelDocument(t))
	if err := other.Register(KindLXGrant, capture(KindLXGrant)); err != nil {
		t.Fatal(err)
	}
	if err := other.RegisterKernel(capture(KindLXActivity), refusing, capture(KindLXGrant)); err == nil {
		t.Fatal("kernel kinds registered over an existing grant inspector")
	}
	expect(t, other.Evaluate(testAccount, Request{Kind: KindLXActivity, View: []byte{1}}, h.ledger), CodeUnknownKind)
}

func TestVerificationMessageLayout(t *testing.T) {
	pub := bytes.Repeat([]byte{0x04}, 65)
	msg := VerificationMessage("wallet:a:secp256k1", pub, "import-a")
	var want []byte
	want = append(want, "LX:PAXEER-CEREMONY-VERIFY:v1"...)
	for _, f := range [][]byte{[]byte("wallet:a:secp256k1"), pub, []byte("import-a")} {
		want = binary.BigEndian.AppendUint16(want, uint16(len(f)))
		want = append(want, f...)
	}
	if !bytes.Equal(msg, want) {
		t.Fatalf("verification message %x, want %x", msg, want)
	}
	if bytes.Equal(msg, VerificationMessage("wallet:a:secp256k1", pub, "import-b")) || bytes.Equal(msg, VerificationMessage("wallet:b:secp256k1", pub, "import-a")) {
		t.Fatal("verification message does not bind the key id and the import session")
	}
}

func TestOperatorVerificationAcceptsOnlyTheVerificationMessage(t *testing.T) {
	h := newHarness(t)
	pub := bytes.Repeat([]byte{0x07}, 32)
	good := &Verification{KeyID: "wallet:a:ed25519", PublicKey: pub, ImportSession: "import-a", Message: VerificationMessage("wallet:a:ed25519", pub, "import-a")}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindOperatorVerification, View: good}, h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(strictUser, Request{Kind: KindOperatorVerification, View: good}, h.ledger), CodeAllowed)

	forged := *good
	forged.Message = VerificationMessage("wallet:a:ed25519", pub, "import-b")
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindOperatorVerification, View: &forged}, h.ledger), CodeVerificationView)
	empty := *good
	empty.ImportSession = ""
	empty.Message = VerificationMessage(empty.KeyID, pub, "")
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindOperatorVerification, View: &empty}, h.ledger), CodeVerificationView)
	for _, view := range []any{good.Message, evm.DecodePersonalMessage([]byte("hello")), []byte("arbitrary payload")} {
		expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindOperatorVerification, View: view}, h.ledger), CodeVerificationView)
	}
	if err := h.policy.Register(KindOperatorVerification, func(Context, any) (Inspection, error) { return Inspection{}, nil }); err == nil {
		t.Fatal("operator verification registered over its own inspector")
	}
}

func TestNoOtherKindSignsTheVerificationMessage(t *testing.T) {
	h := newHarness(t)
	engine := New(kernelDocument(t))
	accept := func(Context, any) (Inspection, error) { return Inspection{}, nil }
	if err := engine.RegisterKernel(accept, accept, accept); err != nil {
		t.Fatal(err)
	}
	pub := bytes.Repeat([]byte{0x04}, 65)
	msg := VerificationMessage("wallet:a:secp256k1", pub, "import-a")
	view := &Verification{KeyID: "wallet:a:secp256k1", PublicKey: pub, ImportSession: "import-a", Message: msg}
	expect(t, engine.Evaluate(testAccount, h.tx(t, common.HexToAddress(friend), big.NewInt(0), msg), h.ledger), CodeVerificationIsolated)
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage(msg)}, h.ledger), CodeVerificationIsolated)
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage(append([]byte("prefix "), msg...))}, h.ledger), CodeVerificationIsolated)
	batch := &evm.SponsoredBatchClaim{Batch: evm.SponsoredBatch{ChainID: chainID, Calls: []evm.BatchCall{{To: common.HexToAddress(friend), Value: big.NewInt(0), Data: msg}}}}
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindSponsoredBatch, View: batch}, h.ledger), CodeVerificationIsolated)
	typed, err := evm.DecodeTypedData([]byte(`{"types":{"EIP712Domain":[{"name":"chainId","type":"uint256"}],"Note":[{"name":"text","type":"string"}]},"primaryType":"Note","domain":{"chainId":125},"message":{"text":"LX:PAXEER-CEREMONY-VERIFY:v1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindTypedData, View: typed}, h.ledger), CodeVerificationIsolated)
	for _, kind := range append([]string{KindEVMTransaction, KindTypedData, KindPersonalMessage, KindSponsoredBatch, KindAuthorization}, KernelKinds()...) {
		expect(t, engine.Evaluate(testAccount, Request{Kind: kind, View: view}, h.ledger), CodeVerificationIsolated)
		expect(t, engine.Evaluate(testAccount, Request{Kind: kind, View: msg}, h.ledger), CodeVerificationIsolated)
	}
	expect(t, engine.Evaluate(testAccount, Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage([]byte("hello"))}, h.ledger), CodeAllowed)
}
