package policy

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

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
    "selectors": {"` + addr.Hex() + `": ["` + selectorHex(t, "addr", "bindLayerX") + `"]},
    "authorization_delegates": ["0x4444444444444444444444444444444444444444"]
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

func TestShippedPolicyAllowsWalletSDKWrites(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	effective, err := compile(doc.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	engine := New(doc)
	for _, kind := range doc.Defaults.Kinds {
		if _, ok := engine.inspector(kind); !ok && !slices.Contains(KernelKinds(), kind) {
			t.Fatalf("shipped kind %q has no inspector", kind)
		}
	}
	for _, kind := range []string{KindSponsoredBatch, KindAuthorization} {
		if !effective.kinds[kind] {
			t.Fatalf("shipped policy does not allow %s", kind)
		}
	}
	sid := TokenAsset(common.HexToAddress(sidiora))
	if _, ok := effective.caps[sid]; ok {
		t.Fatal("shipped policy caps SID for every kind; SID is bounded only as the sponsored fee")
	}
	if fee, ok := effective.feeCaps[sid]; !ok || len(effective.feeCaps) != 1 || fee.perTransaction.Cmp(big.NewInt(5_000_000)) != 0 || fee.daily.Cmp(big.NewInt(50_000_000)) != 0 {
		t.Fatalf("shipped fee caps %+v, want only SID at 5 per transaction and 50 daily", effective.feeCaps)
	}
	for _, row := range []struct {
		precompile string
		writes     []string
		unlisted   string
	}{
		{"launchpad", []string{
			"buy(address,uint256,uint256,uint256)",
			"sell(address,uint256,uint256,uint256)",
			"createMarket(string,string,uint8,address)",
			"setFeeStrategy(uint256,uint8)",
			"claimFees(uint256)",
			"claimAirdrop(uint256)",
			"executeAirdrop(uint256)",
			"executeBurn(uint256)",
			"executeLpRewards(uint256)",
		}, "claimAirdrop(address,address)"},
		{"perps", []string{
			"deposit(address,uint256)",
			"withdraw(address,uint256)",
			"openPosition(uint256,address,uint256,uint256,bool)",
			"cancelOrder(uint256)",
			"closePosition(uint256)",
		}, "transferCollateral(address,uint256)"},
		{"layerxbridge", []string{
			"bridgeIn(uint64,address,bytes32,uint64,bytes32,address,uint256,bytes[])",
			"bridgeOut(uint64,address,uint256,address)",
		}, "getCap(uint64,address)"},
		{"feetoken", []string{"setFeeDenom(string)", "clearFeeDenom()"}, "getFeeDenom(address)"},
		{"xweb", []string{"request(uint8,bytes,uint64)", "refund(uint64)"}, "fulfilProgram(bytes)"},
	} {
		contract, err := evm.PrecompileABI(row.precompile)
		if err != nil {
			t.Fatal(err)
		}
		address, _ := evm.PrecompileAddress(row.precompile)
		selector := func(sig string) []byte {
			method, err := contract.MethodById(crypto.Keccak256([]byte(sig))[:4])
			if err != nil || method.Sig != sig {
				t.Fatalf("%s abi has no %s: %v", row.precompile, sig, err)
			}
			return method.ID
		}
		want := map[[4]byte]bool{}
		for _, sig := range row.writes {
			id := selector(sig)
			want[[4]byte(id)] = true
			if decision, ok := checkDestination(effective, Destination{Address: address, Selector: id, Precompile: true}); !ok {
				t.Fatalf("%s %s refused: %+v", row.precompile, sig, decision)
			}
		}
		if len(effective.selectors[address]) != len(want) {
			t.Fatalf("shipped %s selectors %v, want exactly the wallet writes %v", row.precompile, effective.selectors[address], want)
		}
		decision, ok := checkDestination(effective, Destination{Address: address, Selector: selector(row.unlisted), Precompile: true})
		if ok || decision.Code != CodeSelectorNotAllowed {
			t.Fatalf("%s %s decision %+v, want %s", row.precompile, row.unlisted, decision, CodeSelectorNotAllowed)
		}
	}

	contract, err := evm.PrecompileABI("launchpad")
	if err != nil {
		t.Fatal(err)
	}
	launchpad, _ := evm.PrecompileAddress("launchpad")
	h := newHarness(t)
	h.policy = New(doc)
	buy, err := contract.Pack("buy", common.HexToAddress(sidiora), big.NewInt(10), big.NewInt(9), big.NewInt(1_900_000_000))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, launchpad, big.NewInt(0), buy), h.ledger), CodeAllowed)
	unlisted, err := contract.Pack("claimAirdrop0", common.HexToAddress(friend), common.HexToAddress(sidiora))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, launchpad, big.NewInt(0), unlisted), h.ledger), CodeSelectorNotAllowed)

	feeABI, err := evm.PrecompileABI("feetoken")
	if err != nil {
		t.Fatal(err)
	}
	feetoken, _ := evm.PrecompileAddress("feetoken")
	setDenom, err := feeABI.Pack("setFeeDenom", "usid")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, feetoken, big.NewInt(0), setDenom), h.ledger), CodeAllowed)
}

func TestShippedPolicyAllowsPerpsApproveThenDeposit(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	perpsABI, err := evm.PrecompileABI("perps")
	if err != nil {
		t.Fatal(err)
	}
	erc20, err := evm.PrecompileABI("erc20")
	if err != nil {
		t.Fatal(err)
	}
	perps, _ := evm.PrecompileAddress("perps")
	usdl := common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	amount := big.NewInt(50_000_000)
	h := newHarness(t)
	h.policy = New(doc)

	approve, err := erc20.Pack("approve", perps, amount)
	if err != nil {
		t.Fatal(err)
	}
	decision := h.policy.Evaluate(testAccount, h.tx(t, usdl, big.NewInt(0), approve), h.ledger)
	expect(t, decision, CodeAllowed)
	if len(decision.Spends) != 1 || decision.Spends[0].Asset != TokenAsset(usdl) || decision.Spends[0].Amount.Cmp(amount) != 0 {
		t.Fatalf("approve spends %+v, want %s of USDL", decision.Spends, amount)
	}
	deposit, err := perpsABI.Pack("deposit", usdl, amount)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), deposit), h.ledger), CodeAllowed)
	open, err := perpsABI.Pack("openPosition", big.NewInt(3), usdl, amount, new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18)), true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), open), h.ledger), CodeAllowed)
	transfer, err := perpsABI.Pack("transferCollateral", common.HexToAddress(friend), amount)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), transfer), h.ledger), CodeSelectorNotAllowed)
	legacy := append(crypto.Keccak256([]byte("placeOrder(bytes32,uint8,uint256,uint256,uint8)"))[:4], make([]byte, 160)...)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), legacy), h.ledger), CodeDecodeError)
}

func TestShippedPolicyAllowsLaunchpadApproveThenBuy(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	launchpadABI, err := evm.PrecompileABI("launchpad")
	if err != nil {
		t.Fatal(err)
	}
	erc20, err := evm.PrecompileABI("erc20")
	if err != nil {
		t.Fatal(err)
	}
	launchpad, _ := evm.PrecompileAddress("launchpad")
	custody, _ := evm.PrecompileAddress("layerxcustody")
	usdl := common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	amount := big.NewInt(25_000_000)
	h := newHarness(t)
	h.policy = New(doc)

	approve, err := erc20.Pack("approve", launchpad, amount)
	if err != nil {
		t.Fatal(err)
	}
	decision := h.policy.Evaluate(testAccount, h.tx(t, usdl, big.NewInt(0), approve), h.ledger)
	expect(t, decision, CodeAllowed)
	if len(decision.Spends) != 1 || decision.Spends[0].Asset != TokenAsset(usdl) || decision.Spends[0].Amount.Cmp(amount) != 0 {
		t.Fatalf("approve spends %+v, want %s of USDL", decision.Spends, amount)
	}
	buy, err := launchpadABI.Pack("buy", common.HexToAddress(friend), amount, big.NewInt(1), big.NewInt(1_900_000_000))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, launchpad, big.NewInt(0), buy), h.ledger), CodeAllowed)

	unlistedSpender, err := erc20.Pack("approve", custody, amount)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, usdl, big.NewInt(0), unlistedSpender), h.ledger), CodeSelectorNotAllowed)

	launchToken := common.HexToAddress("0x4444444444444444444444444444444444444444")
	h.now = h.now.Add(RateWindow)
	approveToken := func(spender common.Address, value *big.Int) Decision {
		data, err := erc20.Pack("approve", spender, value)
		if err != nil {
			t.Fatal(err)
		}
		return h.policy.Evaluate(testAccount, h.tx(t, launchToken, big.NewInt(0), data), h.ledger)
	}
	expect(t, approveToken(launchpad, big.NewInt(7_000_000_000_000_000)), CodeAllowed)
	expect(t, approveToken(launchpad, maxUint256), CodeUnlimitedApproval)
	expect(t, approveToken(common.HexToAddress(friend), big.NewInt(7_000_000_000_000_000)), CodeNoCap)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, usdl, big.NewInt(0), func() []byte {
		data, err := erc20.Pack("approve", launchpad, big.NewInt(1_000_000_001))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}()), h.ledger), CodeValueCap)

	unlisted, err := launchpadABI.Pack("claimAirdrop0", common.HexToAddress(friend), common.HexToAddress(sidiora))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, launchpad, big.NewInt(0), unlisted), h.ledger), CodeSelectorNotAllowed)
}

func TestShippedPolicySponsoredFeeAndPaymasterDelegation(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	paymaster := common.HexToAddress("0x5555555555555555555555555555555555555555")
	custody, _ := evm.PrecompileAddress("layerxcustody")
	authorization := func(delegate common.Address) Request {
		digest, err := evm.AuthorizationDigest(chainID, delegate, 1)
		if err != nil {
			t.Fatal(err)
		}
		return Request{Kind: KindAuthorization, View: &evm.AuthorizationClaim{ChainID: chainID, Address: delegate, Nonce: 1, ClaimedDigest: digest}}
	}
	h := newHarness(t)
	h.policy = New(doc)

	batch := sponsoredBatch()
	digest, err := evm.SponsoredBatchDigest(batch)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindSponsoredBatch, View: &evm.SponsoredBatchClaim{Batch: batch, ClaimedDigest: digest}}, h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(testAccount, authorization(paymaster), h.ledger), CodeDelegateNotAllowed)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: "eth_sign_digest", View: []byte{1}}, h.ledger), CodeUnknownKind)

	doc.Defaults.AuthorizationDelegates = []string{paymaster.Hex()}
	h.policy = New(doc)
	expect(t, h.policy.Evaluate(testAccount, authorization(paymaster), h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(testAccount, authorization(common.HexToAddress(friend)), h.ledger), CodeDelegateNotAllowed)
	expect(t, h.policy.Evaluate(testAccount, authorization(custody), h.ledger), CodeDelegateNotAllowed)

	to := common.HexToAddress(friend)
	setCode := func(delegate common.Address) Request {
		return Request{Kind: KindEVMTransaction, View: &evm.Transaction{
			Type: 4, ChainID: chainID, To: &to, Value: big.NewInt(0),
			Authorizations: []types.SetCodeAuthorization{{ChainID: *uint256.NewInt(125), Address: delegate, Nonce: 1}},
		}}
	}
	h.now = h.now.Add(RateWindow)
	expect(t, h.policy.Evaluate(testAccount, setCode(paymaster), h.ledger), CodeAllowed)
	expect(t, h.policy.Evaluate(testAccount, setCode(common.HexToAddress(friend)), h.ledger), CodeDelegateNotAllowed)
}

func TestShippedPolicySIDCapIsSponsoredFeeOnly(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	erc20, err := evm.PrecompileABI("erc20")
	if err != nil {
		t.Fatal(err)
	}
	token := common.HexToAddress(sidiora)
	trusted := common.HexToAddress("0x0000000000000000000000000000000000001017")
	h := newHarness(t)
	h.policy = New(doc)
	call := func(method string, to common.Address, value *big.Int) Request {
		data, err := erc20.Pack(method, to, value)
		if err != nil {
			t.Fatal(err)
		}
		return h.tx(t, token, big.NewInt(0), data)
	}

	expect(t, h.policy.Evaluate(testAccount, call("transfer", common.HexToAddress(friend), big.NewInt(1_000_000)), h.ledger), CodeNoCap)
	expect(t, h.policy.Evaluate(testAccount, call("approve", common.HexToAddress(friend), big.NewInt(1_000_000)), h.ledger), CodeNoCap)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: permitData(t, "1000000")}, h.ledger), CodeNoCap)

	approval := h.policy.Evaluate(testAccount, call("approve", trusted, big.NewInt(100_000_000)), h.ledger)
	expect(t, approval, CodeAllowed)
	if len(approval.Spends) != 0 {
		t.Fatalf("trusted SID approval counted %+v against a cap", approval.Spends)
	}
	expect(t, h.policy.Evaluate(testAccount, call("approve", trusted, maxUint256), h.ledger), CodeUnlimitedApproval)

	sponsored := func(fee int64, calls ...evm.BatchCall) Request {
		batch := sponsoredBatch()
		if calls != nil {
			batch.Calls = calls
		}
		batch.Quote.TokenAmount = big.NewInt(fee)
		batch.Quote.MaxTokenAmount = big.NewInt(fee)
		digest, err := evm.SponsoredBatchDigest(batch)
		if err != nil {
			t.Fatal(err)
		}
		return Request{Kind: KindSponsoredBatch, View: &evm.SponsoredBatchClaim{Batch: batch, ClaimedDigest: digest}}
	}
	h.now = h.now.Add(RateWindow)
	fee := h.policy.Evaluate(testAccount, sponsored(4_000_000), h.ledger)
	expect(t, fee, CodeAllowed)
	if len(fee.Spends) != 1 || fee.Spends[0].Asset != TokenAsset(token) || fee.Spends[0].Amount.Cmp(big.NewInt(4_000_000)) != 0 {
		t.Fatalf("sponsored fee spends %+v", fee.Spends)
	}
	expect(t, h.policy.Evaluate(testAccount, sponsored(5_000_001), h.ledger), CodeValueCap)
	transfer, err := erc20.Pack("transfer", common.HexToAddress(friend), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, sponsored(1_000_000, evm.BatchCall{To: token, Value: big.NewInt(0), Data: transfer}), h.ledger), CodeNoCap)
	for i := 0; i < 9; i++ {
		expect(t, h.policy.Evaluate(testAccount, sponsored(5_000_000), h.ledger), CodeAllowed)
	}
	expect(t, h.policy.Evaluate(testAccount, sponsored(5_000_000), h.ledger), CodeDailyCap)
	expect(t, h.policy.Evaluate(testAccount, sponsored(1_000_000), h.ledger), CodeAllowed)
}

func TestShippedPolicyCountsPerpsPulls(t *testing.T) {
	doc, err := LoadFile(filepath.Join("..", "..", "..", "deploy", "attestor-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	perpsABI, err := evm.PrecompileABI("perps")
	if err != nil {
		t.Fatal(err)
	}
	perps, _ := evm.PrecompileAddress("perps")
	usdl := common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	leverage := new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18))
	h := newHarness(t)
	h.policy = New(doc)
	deposit := func(amount *big.Int) Decision {
		data, err := perpsABI.Pack("deposit", usdl, amount)
		if err != nil {
			t.Fatal(err)
		}
		return h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), data), h.ledger)
	}
	open := func(amount *big.Int) Decision {
		data, err := perpsABI.Pack("openPosition", big.NewInt(3), usdl, amount, leverage, true)
		if err != nil {
			t.Fatal(err)
		}
		return h.policy.Evaluate(testAccount, h.tx(t, perps, big.NewInt(0), data), h.ledger)
	}
	counted := func(decision Decision, amount *big.Int) {
		t.Helper()
		expect(t, decision, CodeAllowed)
		if len(decision.Spends) != 1 || decision.Spends[0].Asset != TokenAsset(usdl) || decision.Spends[0].Amount.Cmp(amount) != 0 {
			t.Fatalf("perps spends %+v, want %s of USDL", decision.Spends, amount)
		}
	}

	expect(t, deposit(big.NewInt(1_000_000_001)), CodeValueCap)
	expect(t, open(big.NewInt(1_000_000_001)), CodeValueCap)
	full := big.NewInt(1_000_000_000)
	for i := 0; i < 5; i++ {
		h.now = h.now.Add(RateWindow)
		counted(deposit(full), full)
		counted(open(full), full)
	}
	spent, err := h.ledger.Spent(strings.ToLower(testAccount), TokenAsset(usdl), h.now.Add(-SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	if spent.Cmp(big.NewInt(10_000_000_000)) != 0 {
		t.Fatalf("perps pulls counted %s of USDL, want the full daily cap", spent)
	}
	h.now = h.now.Add(RateWindow)
	expect(t, deposit(big.NewInt(1)), CodeDailyCap)
	expect(t, open(big.NewInt(1)), CodeDailyCap)
}
