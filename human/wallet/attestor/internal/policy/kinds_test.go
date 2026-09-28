package policy

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
