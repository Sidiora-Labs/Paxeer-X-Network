package policy

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/evm"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
)

func openLedgerStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(dir, bytes.Repeat([]byte{7}, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSpendLedgerCapsHoldAcrossRestart(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	st := openLedgerStore(t, dir)
	ledger, err := NewSpendLedger(st, func() time.Time { return h.now })
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress(friend)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), ledger.ForRequest("key/first")), CodeAllowed)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(pax(10).String())) || bytes.Contains(raw, []byte(`"spends"`)) {
		t.Fatal("the ledger record is stored in plaintext")
	}
	h.now = h.now.Add(2 * RateWindow)
	reopened := openLedgerStore(t, dir)
	defer reopened.Close()
	restarted, err := NewSpendLedger(reopened, func() time.Time { return h.now })
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), restarted.ForRequest("key/second")), CodeDailyCap)
	spent, err := restarted.Spent(AccountKey(testAccount), AssetNative, h.now.Add(-SpendWindow))
	if err != nil || spent.Cmp(pax(10)) != 0 {
		t.Fatalf("spent after restart %v (%v), want %v", spent, err, pax(10))
	}
	h.now = h.now.Add(SpendWindow)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(10), nil), restarted.ForRequest("key/third")), CodeAllowed)
}

func TestSpendLedgerCountsAnnouncedRequestsOnce(t *testing.T) {
	h := newHarness(t)
	st := openLedgerStore(t, t.TempDir())
	defer st.Close()
	ledger, err := NewSpendLedger(st, func() time.Time { return h.now })
	if err != nil {
		t.Fatal(err)
	}
	account := AccountKey(testAccount)
	to := common.HexToAddress(friend)
	if err := ledger.Apply(account, "key/shared", []Spend{{Asset: AssetNative, Amount: pax(8)}}, h.now); err != nil {
		t.Fatal(err)
	}
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(8), nil), ledger.ForRequest("key/shared")), CodeAllowed)
	if err := ledger.Apply(account, "key/shared", []Spend{{Asset: AssetNative, Amount: pax(1)}}, h.now); err != nil {
		t.Fatal(err)
	}
	spent, err := ledger.Spent(account, AssetNative, h.now.Add(-SpendWindow))
	if err != nil || spent.Cmp(pax(8)) != 0 {
		t.Fatalf("one request announced by several signers counts %v (%v), want %v", spent, err, pax(8))
	}
	requests, err := ledger.Requests(account, h.now.Add(-RateWindow))
	if err != nil || requests != 1 {
		t.Fatalf("one request announced by several signers counts %d requests (%v)", requests, err)
	}
	if err := ledger.Apply(account, "key/elsewhere", []Spend{{Asset: AssetNative, Amount: pax(6)}}, h.now); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(2 * RateWindow)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(2), nil), ledger.ForRequest("key/next")), CodeDailyCap)
	expect(t, h.policy.Evaluate(testAccount, h.tx(t, to, pax(1), nil), ledger.ForRequest("key/small")), CodeAllowed)
}

func permitData(t *testing.T, value string) *evm.TypedData {
	t.Helper()
	body := `{"types":{"EIP712Domain":[{"name":"name","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"verifyingContract","type":"address"}],"Permit":[{"name":"owner","type":"address"},{"name":"spender","type":"address"},{"name":"value","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"deadline","type":"uint256"}]},"primaryType":"Permit","domain":{"name":"Sidiora","version":"1","chainId":125,"verifyingContract":"` + sidiora + `"},"message":{"owner":"` + testAccount + `","spender":"` + friend + `","value":"` + value + `","nonce":"0","deadline":"4102444800"}}`
	typed, err := evm.DecodeTypedData([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return typed
}

func TestPermitCountsAgainstTheTokenCap(t *testing.T) {
	h := newHarness(t)
	st := openLedgerStore(t, t.TempDir())
	defer st.Close()
	ledger, err := NewSpendLedger(st, func() time.Time { return h.now })
	if err != nil {
		t.Fatal(err)
	}
	decision := h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: permitData(t, "5000000")}, ledger.ForRequest("key/permit-1"))
	expect(t, decision, CodeAllowed)
	if len(decision.Spends) != 1 || decision.Spends[0].Asset != TokenAsset(common.HexToAddress(sidiora)) || decision.Spends[0].Amount.String() != "5000000" {
		t.Fatalf("permit spends %+v", decision.Spends)
	}
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: permitData(t, "6000000")}, ledger.ForRequest("key/permit-2")), CodeValueCap)
	h.now = h.now.Add(2 * RateWindow)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: permitData(t, "4000000")}, ledger.ForRequest("key/permit-3")), CodeDailyCap)
	expect(t, h.policy.Evaluate(testAccount, Request{Kind: KindTypedData, View: permitData(t, "3000000")}, ledger.ForRequest("key/permit-4")), CodeAllowed)
}
