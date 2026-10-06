package policy

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/evm"
)

const (
	testAccount = "0x1111111111111111111111111111111111111111"
	strictUser  = "0x6666666666666666666666666666666666666666"
	sidiora     = "0x21f7b20a555199fa73A238B1a91FD0f549068fEe"
	deniedAddr  = "0x000000000000000000000000000000000000dEaD"
	friend      = "0x3333333333333333333333333333333333333333"
)

var chainID = big.NewInt(125)

func pax(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

func selectorHex(t *testing.T, precompile, method string) string {
	t.Helper()
	contract, err := evm.PrecompileABI(precompile)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + common.Bytes2Hex(contract.Methods[method].ID)
}

func testDocument(t *testing.T) []byte {
	t.Helper()
	addr, _ := evm.PrecompileAddress("addr")
	return []byte(`{
  "version": 1,
  "defaults": {
    "chain_id": 125,
    "kinds": ["evm_tx", "eip712", "personal_message", "sponsored_batch", "eip7702_authorization"],
    "caps": {
      "native": {"per_transaction": "` + pax(10).String() + `", "daily": "` + pax(15).String() + `"},
      "` + sidiora + `": {"per_transaction": "5000000", "daily": "8000000"}
    },
    "rate_per_minute": 3,
    "destinations_deny": ["` + deniedAddr + `"],
    "selectors": {"` + addr.Hex() + `": ["` + selectorHex(t, "addr", "bindLayerX") + `"]}
  },
  "accounts": {
    "` + strictUser + `": {"kinds": ["personal_message"], "rate_per_minute": 1}
  }
}`)
}

type harness struct {
	policy *Policy
	ledger *MemoryLedger
	now    time.Time
	key    *ecdsa.PrivateKey
	nonce  uint64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	doc, err := Parse(testDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{policy: New(doc), now: time.Unix(1_800_000_000, 0).UTC(), key: key}
	h.ledger = NewMemoryLedger(func() time.Time { return h.now })
	return h
}

func (h *harness) tx(t *testing.T, to common.Address, value *big.Int, data []byte) Request {
	t.Helper()
	h.nonce++
	signed, err := types.SignNewTx(h.key, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: h.nonce, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 100_000, To: &to, Value: value, Data: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	view, err := evm.DecodeTransaction(raw, chainID)
	if err != nil {
		t.Fatal(err)
	}
	return Request{Kind: KindEVMTransaction, View: view}
}

func expect(t *testing.T, got Decision, code string) {
	t.Helper()
	if got.Code != code || got.Allowed != (code == CodeAllowed) || got.Reason == "" {
		t.Fatalf("decision %+v, want code %s", got, code)
	}
}

func TestValueCap(t *testing.T) {
	h := newHarness(t)
	to := common.HexToAddress(friend)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(11), nil), h.ledger), CodeValueCap)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), h.ledger), CodeAllowed)

	erc20, err := evm.PrecompileABI("erc20")
	if err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	over, err := erc20.Pack("transfer", to, big.NewInt(5_000_001))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, common.HexToAddress(sidiora), big.NewInt(0), over), h.ledger), CodeValueCap)
	unknownToken, err := erc20.Pack("transfer", to, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, common.HexToAddress("0x7777777777777777777777777777777777777777"), big.NewInt(0), unknownToken), h.ledger), CodeNoCap)
}

func TestRollingCapAcrossWindowBoundary(t *testing.T) {
	h := newHarness(t)
	to := common.HexToAddress(friend)
	start := h.now
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), h.ledger), CodeAllowed)
	h.now = start.Add(time.Hour)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(6), nil), h.ledger), CodeDailyCap)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(5), nil), h.ledger), CodeAllowed)
	h.now = start.Add(SpendWindow - time.Second)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(1), nil), h.ledger), CodeDailyCap)
	h.now = start.Add(SpendWindow)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), h.ledger), CodeAllowed)
	h.now = start.Add(SpendWindow + time.Hour)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(1), nil), h.ledger), CodeAllowed)
	spent, err := h.ledger.Spent(strings.ToLower(testAccount), AssetNative, h.now.Add(-SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	if spent.Cmp(pax(11)) != 0 {
		t.Fatalf("spent %s want %s", spent, pax(11))
	}
}

func TestRateRefusal(t *testing.T) {
	h := newHarness(t)
	to := common.HexToAddress(friend)
	for i := 0; i < 3; i++ {
		expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, big.NewInt(1), nil), h.ledger), CodeAllowed)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, big.NewInt(1), nil), h.ledger), CodeRateLimited)
	h.now = h.now.Add(RateWindow)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, big.NewInt(1), nil), h.ledger), CodeAllowed)

	message := Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage([]byte("hello"))}
	expect(t, h.policy.Evaluate(strictUser, message, h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(strictUser, message, h.ledger), CodeRateLimited)
}

func TestDestinationDeny(t *testing.T) {
	h := newHarness(t)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, common.HexToAddress(deniedAddr), big.NewInt(1), nil), h.ledger), CodeDestinationDenied)
	erc20, err := evm.PrecompileABI("erc20")
	if err != nil {
		t.Fatal(err)
	}
	data, err := erc20.Pack("transfer", common.HexToAddress(deniedAddr), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, common.HexToAddress(sidiora), big.NewInt(0), data), h.ledger), CodeDestinationDenied)

	doc, err := Parse(testDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.Accounts[strings.ToLower(testAccount)] = Rules{DestinationsAllow: []string{friend}}
	allowOnly := New(doc)
	expect(t, allowOnly.Evaluate(testAccount, h.tx(t, common.HexToAddress("0x8888888888888888888888888888888888888888"), big.NewInt(1), nil), h.ledger), CodeDestinationBlocked)
	expect(t, allowOnly.Evaluate(testAccount, h.tx(t, common.HexToAddress(friend), big.NewInt(1), nil), h.ledger), CodeAllowed)
}

func TestDisallowedSelector(t *testing.T) {
	h := newHarness(t)
	addrABI, err := evm.PrecompileABI("addr")
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := evm.PrecompileAddress("addr")
	unbind, err := addrABI.Pack("unbindLayerX")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, addr, big.NewInt(0), unbind), h.ledger), CodeSelectorNotAllowed)
	bind, err := addrABI.Pack("bindLayerX", [32]byte{1}, []byte{2})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, addr, big.NewInt(0), bind), h.ledger), CodeAllowed)

	feeABI, err := evm.PrecompileABI("feetoken")
	if err != nil {
		t.Fatal(err)
	}
	feetoken, _ := evm.PrecompileAddress("feetoken")
	setDenom, err := feeABI.Pack("setFeeDenom", "usid")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, feetoken, big.NewInt(0), setDenom), h.ledger), CodeSelectorNotAllowed)
	h.now = h.now.Add(RateWindow)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, addr, big.NewInt(0), []byte{0xde, 0xad, 0xbe, 0xef}), h.ledger), CodeDecodeError)
}

func TestUnknownKind(t *testing.T) {
	h := newHarness(t)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: "lx_activity", View: []byte{1}}, h.ledger), CodeUnknownKind)
	inspect := func(Context, any) (Inspection, error) { return Inspection{}, nil }
	if err := h.policy.Register("lx_activity", inspect); err != nil {
		t.Fatal(err)
	}
	if err := h.policy.Register(KindEVMTransaction, inspect); err == nil {
		t.Fatal("duplicate kind registered")
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: "lx_activity", View: []byte{1}}, h.ledger), CodeKindNotAllowed)
	message := Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage([]byte("hello"))}
	expect(t, h.policy.Evaluate(testAccount, message, h.ledger), CodeAllowed)
	h.now = h.now.Add(RateWindow)
	expect(t, h.policy.Evaluate(strictUser, h.tx(t, common.HexToAddress(friend), big.NewInt(1), nil), h.ledger), CodeKindNotAllowed)
}

func TestUnknownVersion(t *testing.T) {
	raw := strings.Replace(string(testDocument(t)), `"version": 1`, `"version": 2`, 1)
	if _, err := Parse([]byte(raw)); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("parse accepted version 2: %v", err)
	}
	doc, err := Parse(testDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.Version = 2
	h := newHarness(t)
	message := Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage([]byte("hello"))}
	expect(t, New(doc).Evaluate(testAccount, message, h.ledger), CodeUnknownVersion)
	expect(t, New(nil).Evaluate(testAccount, message, h.ledger), CodeNoPolicy)
}

func TestMissingField(t *testing.T) {
	raw := strings.Replace(string(testDocument(t)), `"rate_per_minute": 3,`, ``, 1)
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("parse accepted a policy without a rate")
	}
	doc, err := Parse(testDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.Defaults.RatePerMinute = nil
	h := newHarness(t)
	message := Request{Kind: KindPersonalMessage, View: evm.DecodePersonalMessage([]byte("hello"))}
	expect(t, New(doc).Evaluate(testAccount, message, h.ledger), CodeMissingField)
	expect(t, h.policy.Evaluate("", message, h.ledger), CodeMissingField)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindPersonalMessage}, h.ledger), CodeMissingField)
	expect(t, h.policy.Evaluate(testAccount, Request{View: message.View}, h.ledger), CodeMissingField)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindEVMTransaction, View: message.View}, h.ledger), CodeMissingField)
	expect(t, h.policy.Evaluate(testAccount, message, nil), CodeLedgerError)
	expect(t, h.policy.Evaluate(testAccount, message, &MemoryLedger{}), CodeLedgerError)
}

func sponsoredBatch() evm.SponsoredBatch {
	return evm.SponsoredBatch{
		ChainID: chainID,
		Account: common.HexToAddress(testAccount),
		Nonce:   big.NewInt(0),
		Calls:   []evm.BatchCall{{To: common.HexToAddress(friend), Value: big.NewInt(0), Data: nil}},
		Quote: evm.GasQuote{
			Sponsor:        common.HexToAddress("0x2222222222222222222222222222222222222222"),
			Token:          common.HexToAddress(sidiora),
			MaxTokenAmount: big.NewInt(2_100_000),
			TokenAmount:    big.NewInt(2_000_000),
			Deadline:       big.NewInt(1_900_000_000),
			QuoteNonce:     big.NewInt(7),
			GasCost:        big.NewInt(1_000_000_000_000),
		},
	}
}

func TestDigestConstructions(t *testing.T) {
	h := newHarness(t)
	batch := sponsoredBatch()
	digest, err := evm.SponsoredBatchDigest(batch)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindSponsoredBatch, View: &evm.SponsoredBatchClaim{Batch: batch, ClaimedDigest: common.Hash{1}}}, h.ledger), CodeDigestMismatch)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindSponsoredBatch, View: &evm.SponsoredBatchClaim{Batch: batch, ClaimedDigest: digest}}, h.ledger), CodeAllowed)
	spent, err := h.ledger.Spent(strings.ToLower(testAccount), TokenAsset(common.HexToAddress(sidiora)), h.now.Add(-SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	if spent.Cmp(big.NewInt(2_000_000)) != 0 {
		t.Fatalf("sponsored fee spent %s", spent)
	}

	h.now = h.now.Add(RateWindow)
	delegate := common.HexToAddress("0x4444444444444444444444444444444444444444")
	authDigest, err := evm.AuthorizationDigest(chainID, delegate, 1)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindAuthorization, View: &evm.AuthorizationClaim{ChainID: chainID, Address: delegate, Nonce: 2, ClaimedDigest: authDigest}}, h.ledger), CodeDigestMismatch)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindAuthorization, View: &evm.AuthorizationClaim{ChainID: chainID, Address: delegate, Nonce: 1, ClaimedDigest: authDigest}}, h.ledger), CodeAllowed)
	otherChain, err := evm.AuthorizationDigest(big.NewInt(1), delegate, 1)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindAuthorization, View: &evm.AuthorizationClaim{ChainID: big.NewInt(1), Address: delegate, Nonce: 1, ClaimedDigest: otherChain}}, h.ledger), CodeChainMismatch)

	h.now = h.now.Add(RateWindow)
	broken := sponsoredBatch()
	broken.Nonce = nil
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindSponsoredBatch, View: &evm.SponsoredBatchClaim{Batch: broken, ClaimedDigest: digest}}, h.ledger), CodeDecodeError)
}

func TestTypedDataChainPinned(t *testing.T) {
	h := newHarness(t)
	body := `{"types":{"EIP712Domain":[{"name":"name","type":"string"},{"name":"chainId","type":"uint256"}],"Login":[{"name":"user","type":"address"}]},` +
		`"primaryType":"Login","domain":{"name":"Paxeer X","chainId":CHAIN},"message":{"user":"` + testAccount + `"}}`
	right, err := evm.DecodeTypedData([]byte(strings.Replace(body, "CHAIN", "125", 1)))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := evm.DecodeTypedData([]byte(strings.Replace(body, "CHAIN", "1", 1)))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: right}, h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: wrong}, h.ledger), CodeChainMismatch)
	tampered := *right
	tampered.Digest = common.Hash{2}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: &tampered}, h.ledger), CodeDigestMismatch)
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, testDocument(t), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != Version || *doc.Defaults.ChainID != 125 || len(doc.Accounts) != 1 {
		t.Fatalf("document %+v", doc)
	}
	if _, err := Parse([]byte(`{"version":1,"defaults":{"chain_id":125,"kinds":[],"rate_per_minute":1},"unknown":true}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Parse([]byte(`{"version":1,"defaults":{"chain_id":125,"kinds":[],"rate_per_minute":1,"caps":{"native":{"daily":"-1"}}}}`)); err == nil {
		t.Fatal("negative cap accepted")
	}
	if _, err := Parse([]byte(`{"version":1,"defaults":{"chain_id":125,"kinds":[],"rate_per_minute":1,"selectors":{"` + friend + `":["0x1234"]}}}`)); err == nil {
		t.Fatal("short selector accepted")
	}
}
