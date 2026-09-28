package lx

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
)

const (
	vectorPath       = "../../lxwire/testdata/activities.json"
	kernelVectorPath = "testdata/kernel_activities.json"
	ownerKeyHex      = "af06a3e3291714e4f356c19c9b15cd1951ec6e6662aa77be07547f289383341d"
	peerKeyHex       = "5151515151515151515151515151515151515151515151515151515151515151"
	ownerMain        = "8109bd8602f591480606a006552834a9fa46f3f5a0ec1be754163d6cda0810e7"
	peerMain         = "1eb1fd9ca7a0fb3a45a93cf1506f4b6c85b30e567272a5631fcbeef4270d0994"
	tokenAsset       = "7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b"
	grantAsset       = "0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"
	walletAddr       = "0x1111111111111111111111111111111111111111"
	generousAddr     = "0x2222222222222222222222222222222222222222"
	narrowAddr       = "0x3333333333333333333333333333333333333333"
)

type vectors struct {
	Activities []struct {
		Name     string `json:"name"`
		Unsigned struct {
			Hex *string `json:"hex"`
		} `json:"unsigned"`
		SignaturePreimage string `json:"signature_preimage"`
	} `json:"activities"`
	Grants   []grantVector `json:"grants"`
	Receives []struct {
		Name              string      `json:"name"`
		Grant             grantVector `json:"grant"`
		Amount            string      `json:"amount"`
		Sequence          string      `json:"sequence"`
		IdempotencyKey    string      `json:"idempotency_key"`
		AuthorizationKind uint8       `json:"authorization_kind"`
		NetworkID         uint32      `json:"network_id"`
		ProtocolVersion   uint16      `json:"protocol_version"`
		Preimage          string      `json:"preimage"`
	} `json:"receives"`
}

type kernelVector struct {
	Name              string `json:"name"`
	Module            uint16 `json:"module"`
	Operation         uint16 `json:"operation"`
	Accepted          bool   `json:"accepted"`
	Sequence          uint64 `json:"sequence"`
	NotBefore         uint64 `json:"not_before"`
	NotAfter          uint64 `json:"not_after"`
	From              string `json:"from"`
	To                string `json:"to"`
	Asset             string `json:"asset"`
	Amount            string `json:"amount"`
	Payload           string `json:"payload"`
	Unsigned          string `json:"unsigned"`
	SignaturePreimage string `json:"signature_preimage"`
}

type kernelVectors struct {
	PublicKey     string         `json:"public_key"`
	PeerPublicKey string         `json:"peer_public_key"`
	NetworkID     uint32         `json:"network_id"`
	Activities    []kernelVector `json:"activities"`
}

func loadKernelVectors(t *testing.T) *kernelVectors {
	t.Helper()
	raw, err := os.ReadFile(kernelVectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var v kernelVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Activities) == 0 {
		t.Fatal("kernel vector file carries no activities")
	}
	return &v
}

func (k *kernelVectors) vector(t *testing.T, name string) kernelVector {
	t.Helper()
	for _, v := range k.Activities {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("kernel vector %s is missing", name)
	return kernelVector{}
}

func (v kernelVector) disclosure(t *testing.T) Disclosure {
	t.Helper()
	module, ok := ModuleName(lxwire.ModuleID(v.Module))
	if !ok {
		t.Fatalf("%s names module %d", v.Name, v.Module)
	}
	amount, ok := new(big.Int).SetString(v.Amount, 10)
	if !ok {
		t.Fatalf("%s amount %q", v.Name, v.Amount)
	}
	return Disclosure{
		Account:      id(t, v.From),
		Module:       module,
		Operation:    v.Operation,
		Amounts:      []Amount{{Asset: id(t, v.Asset), Amount: amount}},
		Destinations: []ID{id(t, v.To)},
		Sequence:     v.Sequence,
		NotBefore:    v.NotBefore,
		NotAfter:     v.NotAfter,
	}
}

func (v kernelVector) activity(t *testing.T) *lxwire.Activity {
	t.Helper()
	registry, err := decodedOperations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(v.Unsigned)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := lxwire.DecodeUnsignedActivity(raw, registry)
	if err != nil {
		t.Fatalf("%s: %v", v.Name, err)
	}
	return activity
}

type grantVector struct {
	Name               string `json:"name"`
	From               string `json:"from"`
	Recipient          string `json:"recipient"`
	Asset              string `json:"asset"`
	PerDrawMaximum     string `json:"per_draw_maximum"`
	Allowance          string `json:"allowance"`
	Recurring          bool   `json:"recurring"`
	WindowLength       string `json:"window_length"`
	Expiration         string `json:"expiration"`
	PurposeHash        string `json:"purpose_hash"`
	HasReference       bool   `json:"has_reference"`
	ReferenceHash      string `json:"reference_hash"`
	RevocationSequence string `json:"revocation_sequence"`
	PublicKey          string `json:"public_key"`
	Preimage           string `json:"preimage"`
}

func loadVectors(t *testing.T) *vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Activities) == 0 || len(v.Grants) == 0 || len(v.Receives) == 0 {
		t.Fatal("vector file is missing a section")
	}
	return &v
}

func hex32(t *testing.T, text string) [32]byte {
	t.Helper()
	var out [32]byte
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != 32 {
		t.Fatalf("%q is not 32 hex bytes", text)
	}
	copy(out[:], raw)
	return out
}

func id(t *testing.T, text string) ID {
	t.Helper()
	return ID(hex32(t, text))
}

func u64(t *testing.T, text string) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func u128(t *testing.T, text string) lxwire.Uint128 {
	t.Helper()
	v, ok := new(big.Int).SetString(text, 10)
	if !ok || v.Sign() < 0 || v.BitLen() > 128 {
		t.Fatalf("%q is not a uint128", text)
	}
	var raw [16]byte
	v.FillBytes(raw[:])
	return lxwire.Uint128{Hi: new(big.Int).SetBytes(raw[:8]).Uint64(), Lo: new(big.Int).SetBytes(raw[8:]).Uint64()}
}

func (g grantVector) grant(t *testing.T) lxwire.Grant {
	t.Helper()
	return lxwire.Grant{
		From:               hex32(t, g.From),
		Recipient:          hex32(t, g.Recipient),
		Asset:              hex32(t, g.Asset),
		PerDrawMaximum:     u128(t, g.PerDrawMaximum),
		Allowance:          u128(t, g.Allowance),
		Recurring:          g.Recurring,
		WindowLength:       u64(t, g.WindowLength),
		Expiration:         u64(t, g.Expiration),
		PurposeHash:        hex32(t, g.PurposeHash),
		HasReference:       g.HasReference,
		ReferenceHash:      hex32(t, g.ReferenceHash),
		RevocationSequence: u64(t, g.RevocationSequence),
		PublicKey:          hex32(t, g.PublicKey),
	}
}

const engineDocument = `{
  "version": 1,
  "defaults": {"chain_id": 125, "kinds": ["lx_activity", "lx_bind", "lx_grant"], "rate_per_minute": 100}
}`

const kernelDocument = `{
  "version": 1,
  "defaults": {
    "modules": {"asset": [5, 7], "programs": [5]},
    "caps": {
      "native": {"per_operation": "6000000", "daily": "8000000"},
      "` + tokenAsset + `": {"per_operation": "100000"},
      "` + grantAsset + `": {"per_operation": "60000", "daily": "60000"}
    },
    "grants": {"` + grantAsset + `": {"per_grant": "10000", "daily": "20000"}}
  },
  "accounts": {
    "` + generousAddr + `": {"grants": {"` + grantAsset + `": {"per_grant": "60000", "daily": "60000"}}},
    "` + narrowAddr + `": {"modules": {"asset": [5]}, "destinations_allow": ["` + ownerMain + `"]}
  }
}`

type harness struct {
	evaluator *Evaluator
	ledger    *policy.SpendLedger
	store     *store.Store
	dir       string
	server    *nonceServer
	now       time.Time
	vectors   *vectors
	kernel    *kernelVectors
	requests  int
}

func (h *harness) openLedger(t *testing.T) {
	t.Helper()
	st, err := store.Open(h.dir, bytes.Repeat([]byte{9}, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ledger, err := policy.NewSpendLedger(st, func() time.Time { return h.now })
	if err != nil {
		t.Fatal(err)
	}
	h.store, h.ledger = st, ledger
}

func (h *harness) restart(t *testing.T) {
	t.Helper()
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	h.openLedger(t)
}

func (h *harness) request() policy.Ledger {
	h.requests++
	return h.ledger.ForRequest(fmt.Sprintf("key-1/session-%d", h.requests))
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	engineDoc, err := policy.Parse([]byte(engineDocument))
	if err != nil {
		t.Fatal(err)
	}
	kernelDoc, err := Parse([]byte(kernelDocument))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{server: newNonceServer(t, 3), now: time.Unix(1_800_000_100, 0).UTC(), vectors: loadVectors(t), kernel: loadKernelVectors(t), dir: t.TempDir()}
	h.openLedger(t)
	chain, err := NewChain(h.server.URL, h.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.New(engineDoc)
	if h.evaluator, err = New(engine, kernelDoc, chain); err != nil {
		t.Fatal(err)
	}
	if _, err := New(engine, kernelDoc, chain); err == nil {
		t.Fatal("kernel kinds registered twice on one engine")
	}
	if _, err := New(policy.New(engineDoc), kernelDoc, nil); err == nil {
		t.Fatal("a kernel evaluator was built without a chain reader")
	}
	return h
}

func (h *harness) activity(t *testing.T, name string) *ActivityRequest {
	t.Helper()
	for _, v := range h.vectors.Activities {
		if v.Name != name {
			continue
		}
		if v.Unsigned.Hex == nil {
			t.Fatalf("%s has no unsigned encoding", name)
		}
		envelope, err := hex.DecodeString(*v.Unsigned.Hex)
		if err != nil {
			t.Fatal(err)
		}
		return &ActivityRequest{Envelope: envelope, Digest: hex32(t, v.SignaturePreimage), PublicKey: hex32(t, ownerKeyHex)}
	}
	t.Fatalf("vector %s is missing", name)
	return nil
}

func (h *harness) kernelActivity(t *testing.T, name string) *ActivityRequest {
	t.Helper()
	v := h.kernel.vector(t, name)
	envelope, err := hex.DecodeString(v.Unsigned)
	if err != nil {
		t.Fatal(err)
	}
	return &ActivityRequest{Envelope: envelope, Digest: hex32(t, v.SignaturePreimage), PublicKey: hex32(t, h.kernel.PublicKey), Disclosure: v.disclosure(t)}
}

func requestFor(t *testing.T, activity *lxwire.Activity, key [32]byte, disclosure Disclosure) *ActivityRequest {
	t.Helper()
	envelope, err := lxwire.EncodeUnsignedActivity(activity)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := lxwire.SignaturePreimage(activity)
	if err != nil {
		t.Fatal(err)
	}
	return &ActivityRequest{Envelope: envelope, Digest: digest, PublicKey: key, Disclosure: disclosure}
}

func nativeSendDisclosure(t *testing.T) Disclosure {
	t.Helper()
	return loadKernelVectors(t).vector(t, "native-send").disclosure(t)
}

func expect(t *testing.T, got policy.Decision, code string) {
	t.Helper()
	if got.Code != code || got.Allowed != (code == policy.CodeAllowed) || got.Reason == "" {
		t.Fatalf("decision %+v, want code %s", got, code)
	}
}

func (h *harness) spent(t *testing.T, account, key string) *big.Int {
	t.Helper()
	total, err := h.ledger.Spent(policy.AccountKey(account), key, h.now.Add(-policy.SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func TestActivityMatchingDisclosureAccepted(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	native := "lx:" + strings.Repeat("00", 32)

	send := h.kernelActivity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	expect(t, h.evaluator.EvaluateActivity(wallet, send, h.request()), policy.CodeAllowed)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("native spent %s", got)
	}

	tokenSend := h.kernelActivity(t, "token-send")
	ownerTokenAccount, err := lxwire.AssetAccountName(lxwire.DIDFromKey(hex32(t, h.kernel.PublicKey)), hex32(t, tokenAsset), [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	peerTokenAccount, err := lxwire.AssetAccountName(lxwire.DIDFromKey(hex32(t, h.kernel.PeerPublicKey)), hex32(t, tokenAsset), [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	from, err := lxwire.AccountID([]byte(ownerTokenAccount))
	if err != nil {
		t.Fatal(err)
	}
	to, err := lxwire.AccountID([]byte(peerTokenAccount))
	if err != nil {
		t.Fatal(err)
	}
	if tokenSend.Disclosure.Account != from || tokenSend.Disclosure.Destinations[0] != to ||
		tokenSend.Disclosure.Amounts[0].Asset != id(t, tokenAsset) || tokenSend.Disclosure.Amounts[0].Amount.Cmp(big.NewInt(75_000)) != 0 {
		t.Fatalf("token send vector fields %+v", tokenSend.Disclosure)
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, tokenSend, h.request()), policy.CodeAllowed)

	approval := h.activity(t, "approval")
	approval.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "asset", Operation: 7,
		Amounts:      []Amount{{Asset: id(t, grantAsset), Amount: big.NewInt(50_000)}},
		Destinations: []ID{id(t, peerMain)}, Sequence: 3, NotBefore: 1_800_000_000, NotAfter: 1_800_003_600,
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, approval, h.request()), policy.CodeAllowed)

	action := h.activity(t, "agent-action")
	action.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "programs", Operation: 5,
		Amounts:      []Amount{{Asset: ID{}, Amount: big.NewInt(1_000)}, {Asset: id(t, tokenAsset), Amount: big.NewInt(2_000)}},
		Destinations: []ID{id(t, peerMain), id(t, ownerMain)}, Sequence: 5, NotBefore: 0, NotAfter: ^uint64(0),
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, action, h.request()), policy.CodeAllowed)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_001_000)) != 0 {
		t.Fatalf("native spent after the program call %s", got)
	}
	if got := h.spent(t, walletAddr, "lx:"+tokenAsset); got.Cmp(big.NewInt(75_000)) != 0 {
		t.Fatalf("incoming program leg counted as a spend: %s", got)
	}

	expect(t, h.evaluator.EvaluateActivity(wallet, send, h.request()), policy.CodeDailyCap)
	h.now = time.Unix(1_800_000_601, 0).UTC()
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(generousAddr), send, h.request()), CodeOutsideValidity)
}

func TestActivityMismatchedAmountRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	send := h.kernelActivity(t, "native-send")

	send.Disclosure = nativeSendDisclosure(t)
	send.Disclosure.Amounts[0].Amount = big.NewInt(5_000_001)
	refused := h.evaluator.EvaluateActivity(wallet, send, h.request())
	expect(t, refused, CodeDisclosureMismatch)
	if !strings.Contains(refused.Reason, "amounts") {
		t.Fatalf("refusal does not name the amount: %s", refused.Reason)
	}
	if got := h.spent(t, walletAddr, "lx:"+strings.Repeat("00", 32)); got.Sign() != 0 {
		t.Fatalf("refused activity recorded a spend of %s", got)
	}

	for field, change := range map[string]func(d *Disclosure){
		"account":      func(d *Disclosure) { d.Account = id(t, peerMain) },
		"module":       func(d *Disclosure) { d.Module = "spot" },
		"operation":    func(d *Disclosure) { d.Operation = 7 },
		"asset":        func(d *Disclosure) { d.Amounts[0].Asset = id(t, tokenAsset) },
		"destinations": func(d *Disclosure) { d.Destinations = []ID{id(t, ownerMain)} },
		"sequence":     func(d *Disclosure) { d.Sequence = 2 },
		"validity":     func(d *Disclosure) { d.NotAfter = 1_800_000_601 },
	} {
		send.Disclosure = nativeSendDisclosure(t)
		change(&send.Disclosure)
		if got := h.evaluator.EvaluateActivity(wallet, send, h.request()); got.Code != CodeDisclosureMismatch || got.Allowed {
			t.Fatalf("%s mismatch: %+v", field, got)
		}
	}

	send.Disclosure = nativeSendDisclosure(t)
	send.Digest[0] ^= 1
	expect(t, h.evaluator.EvaluateActivity(wallet, send, h.request()), policy.CodeDigestMismatch)
	send.Digest[0] ^= 1
	send.PublicKey = hex32(t, peerKeyHex)
	expect(t, h.evaluator.EvaluateActivity(wallet, send, h.request()), CodeAuthorityMismatch)

	over := h.kernelActivity(t, "native-send")
	over.Disclosure = nativeSendDisclosure(t)
	doc, err := Parse([]byte(strings.Replace(kernelDocument, `"per_operation": "6000000"`, `"per_operation": "4999999"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	h.evaluator.doc = doc
	expect(t, h.evaluator.EvaluateActivity(wallet, over, h.request()), policy.CodeValueCap)
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(narrowAddr), over, h.request()), policy.CodeDestinationBlocked)
}

func TestActivityDisallowedModuleRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)

	budget := h.kernelActivity(t, "budget-fund")
	expect(t, h.evaluator.EvaluateActivity(wallet, budget, h.request()), CodeModuleNotAllowed)

	approval := h.activity(t, "approval")
	approval.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "asset", Operation: 7,
		Amounts:      []Amount{{Asset: id(t, grantAsset), Amount: big.NewInt(50_000)}},
		Destinations: []ID{id(t, peerMain)}, Sequence: 3, NotBefore: 1_800_000_000, NotAfter: 1_800_003_600,
	}
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(narrowAddr), approval, h.request()), CodeOperationNotAllowed)
	expect(t, h.evaluator.EvaluateActivity(wallet, approval, h.request()), policy.CodeAllowed)

	unknownModule := h.kernelActivity(t, "native-send")
	unknownModule.Envelope[activityTypeOffset] = 0
	unknownModule.Envelope[activityTypeOffset+1] = 12
	expect(t, h.evaluator.EvaluateActivity(wallet, unknownModule, h.request()), CodeUnknownModule)
	unknownOperation := h.kernelActivity(t, "native-send")
	unknownOperation.Envelope[activityTypeOffset+3] = 6
	expect(t, h.evaluator.EvaluateActivity(wallet, unknownOperation, h.request()), CodeUnknownOperation)
	truncated := h.kernelActivity(t, "native-send")
	truncated.Envelope = truncated.Envelope[:len(truncated.Envelope)-1]
	expect(t, h.evaluator.EvaluateActivity(wallet, truncated, h.request()), policy.CodeDecodeError)
	expect(t, h.evaluator.EvaluateActivity(wallet, &ActivityRequest{}, h.request()), policy.CodeMissingField)
	expect(t, h.evaluator.EvaluateActivity(wallet, nil, h.request()), policy.CodeMissingField)
}

func TestBindForAnotherAddressRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	other := common.HexToAddress(generousAddr)
	refused := h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, other, 3)}, h.request())
	expect(t, refused, CodeBindAddress)
	if calls, _, _ := h.server.seen(); calls != 0 {
		t.Fatalf("a binding for another address reached the chain %d times", calls)
	}
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(1, wallet, 3)}, h.request()), policy.CodeChainMismatch)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)[:76]}, h.request()), policy.CodeDecodeError)
	expect(t, h.evaluator.EvaluateBind(wallet, nil, h.request()), policy.CodeMissingField)
}

func TestBindStaleNonceRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	h.server.setNonce(4)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)}, h.request()), CodeStaleBindNonce)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 4)}, h.request()), policy.CodeAllowed)
	calls, asked, failed := h.server.seen()
	if calls != 2 || failed != "" || asked[0] != wallet || asked[1] != wallet {
		t.Fatalf("chain saw %d calls for %v (%q)", calls, asked, failed)
	}
	h.server.Close()
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 4)}, h.request()), CodeChainUnavailable)
}

func TestGrantOverCapRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	var oneShot grantVector
	for _, g := range h.vectors.Grants {
		if g.Name == "grant-one-shot" {
			oneShot = g
		}
	}
	if oneShot.Name == "" {
		t.Fatal("grant-one-shot vector is missing")
	}
	grant := oneShot.grant(t)
	request := &GrantRequest{PublicKey: hex32(t, ownerKeyHex), Grant: &grant, Digest: hex32(t, oneShot.Preimage)}
	refused := h.evaluator.EvaluateGrant(wallet, request, h.request())
	expect(t, refused, CodeGrantCap)
	generous := common.HexToAddress(generousAddr)
	expect(t, h.evaluator.EvaluateGrant(generous, request, h.request()), policy.CodeAllowed)
	if got := h.spent(t, generousAddr, "lx-grant:"+grantAsset); got.Cmp(big.NewInt(50_000)) != 0 {
		t.Fatalf("granted allowance recorded as %s", got)
	}
	expect(t, h.evaluator.EvaluateGrant(generous, request, h.request()), policy.CodeDailyCap)

	tampered := *request
	tampered.Digest[31] ^= 1
	expect(t, h.evaluator.EvaluateGrant(generous, &tampered, h.request()), policy.CodeDigestMismatch)
	foreign := *request
	foreign.PublicKey = hex32(t, peerKeyHex)
	expect(t, h.evaluator.EvaluateGrant(generous, &foreign, h.request()), CodeAuthorityMismatch)
	h.now = time.Unix(1_900_000_000, 0).UTC()
	expect(t, h.evaluator.EvaluateGrant(generous, request, h.request()), CodeOutsideValidity)
	h.now = time.Unix(1_800_000_100, 0).UTC()

	v := h.vectors.Receives[0]
	receiveGrant := v.Grant.grant(t)
	grantID, err := lxwire.GrantPreimage(receiveGrant)
	if err != nil {
		t.Fatal(err)
	}
	context := lxwire.GrantContextHash(receiveGrant)
	receive := lxwire.Receive{
		From: receiveGrant.From, To: receiveGrant.Recipient, Asset: receiveGrant.Asset,
		Amount: u128(t, v.Amount), Grant: grantID, Sequence: u64(t, v.Sequence),
		IdempotencyKey: hex32(t, v.IdempotencyKey), ContextHash: context, AuthorizationKind: v.AuthorizationKind,
		Controller: receiveGrant.Recipient, SignedContextHash: context, NetworkID: v.NetworkID, ProtocolVersion: v.ProtocolVersion,
	}
	receiveRequest := &GrantRequest{PublicKey: hex32(t, peerKeyHex), Receive: &receive, Digest: hex32(t, v.Preimage)}
	expect(t, h.evaluator.EvaluateGrant(wallet, receiveRequest, h.request()), policy.CodeAllowed)
	ownerReceive := *receiveRequest
	ownerReceive.PublicKey = hex32(t, ownerKeyHex)
	expect(t, h.evaluator.EvaluateGrant(wallet, &ownerReceive, h.request()), CodeAccountNotOwned)
	large := receive
	large.Amount = lxwire.Uint128{Lo: 10_001}
	largeDigest, err := lxwire.ReceivePreimage(large)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.evaluator.EvaluateGrant(wallet, &GrantRequest{PublicKey: hex32(t, peerKeyHex), Receive: &large, Digest: largeDigest}, h.request()), CodeGrantCap)
	expect(t, h.evaluator.EvaluateGrant(wallet, &GrantRequest{PublicKey: hex32(t, ownerKeyHex)}, h.request()), policy.CodeMissingField)
}

func TestRecordDecision(t *testing.T) {
	h := newHarness(t)
	log, err := audit.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	wallet := common.HexToAddress(walletAddr)
	send := h.kernelActivity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	send.Disclosure.Amounts[0].Amount = big.NewInt(1)
	refused := h.evaluator.EvaluateActivity(wallet, send, h.request())
	record, err := Record(log, policy.KindLXActivity, "key-1", "session-1", send.Envelope, refused)
	if err != nil {
		t.Fatal(err)
	}
	if record.Decision != DecisionDeny || record.Reason != CodeDisclosureMismatch || record.Kind != policy.KindLXActivity || record.Sequence != 1 {
		t.Fatalf("refusal recorded as %+v", record)
	}
	send.Disclosure = nativeSendDisclosure(t)
	allowed := h.evaluator.EvaluateActivity(wallet, send, h.request())
	record, err = Record(log, policy.KindLXActivity, "key-1", "session-2", send.Envelope, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if record.Decision != DecisionAllow || record.Reason != policy.CodeAllowed || record.Sequence != 2 {
		t.Fatalf("allowance recorded as %+v", record)
	}
	if err := log.Verify(); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(log, policy.KindLXBind, "key-1", "session-3", nil, policy.Decision{}); err == nil {
		t.Fatal("a decision without a typed reason was recorded")
	}
	if _, err := Record(nil, policy.KindLXBind, "key-1", "session-3", nil, allowed); err == nil {
		t.Fatal("a decision was recorded without a log")
	}
}

func TestParseKernelPolicy(t *testing.T) {
	for name, raw := range map[string]string{
		"version":         strings.Replace(kernelDocument, `"version": 1`, `"version": 2`, 1),
		"unknown module":  strings.Replace(kernelDocument, `"programs": [5]`, `"lending": [5]`, 1),
		"zero operation":  strings.Replace(kernelDocument, `"programs": [5]`, `"programs": [0]`, 1),
		"bad asset":       strings.Replace(kernelDocument, `"native": {`, `"NATIVE": {`, 1),
		"negative cap":    strings.Replace(kernelDocument, `"6000000"`, `"-1"`, 1),
		"bad destination": strings.Replace(kernelDocument, `"destinations_allow": ["`+ownerMain+`"]`, `"destinations_allow": ["00"]`, 1),
		"bad account":     strings.Replace(kernelDocument, generousAddr, "wallet", 1),
		"unknown field":   strings.Replace(kernelDocument, `"version": 1`, `"version": 1, "extra": true`, 1),
		"missing modules": `{"version": 1, "defaults": {}}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("%s: policy accepted", name)
		} else if name == "version" && !errors.Is(err, ErrUnknownVersion) {
			t.Fatalf("version refusal is %v", err)
		}
	}
	path := t.TempDir() + "/kernel.json"
	if err := os.WriteFile(path, []byte(kernelDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != Version || len(doc.Accounts) != 2 {
		t.Fatalf("document %+v", doc)
	}
	var parsed ID
	if err := parsed.UnmarshalText([]byte(ownerMain)); err != nil || parsed != id(t, ownerMain) {
		t.Fatalf("id round trip %x (%v)", parsed, err)
	}
	text, err := parsed.MarshalText()
	if err != nil || string(text) != ownerMain {
		t.Fatalf("id text %q (%v)", text, err)
	}
}

func (h *harness) requestsSeen(t *testing.T, account string) int {
	t.Helper()
	count, err := h.ledger.Requests(policy.AccountKey(account), h.now.Add(-policy.RateWindow))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func expectSpends(t *testing.T, got policy.Decision, want map[string]int64) {
	t.Helper()
	if !got.Allowed || got.Spends == nil || len(got.Spends) != len(want) {
		t.Fatalf("decision %+v, want spends %v", got, want)
	}
	for _, spend := range got.Spends {
		amount, ok := want[spend.Asset]
		if !ok || spend.Amount == nil || spend.Amount.Cmp(big.NewInt(amount)) != 0 {
			t.Fatalf("spend %s of %v, want %v", spend.Asset, spend.Amount, want)
		}
	}
}

func TestKernelSpendsReachTheStoreLedgerOnce(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	native := "lx:" + strings.Repeat("00", 32)

	send := h.kernelActivity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	view := h.ledger.ForRequest("key-1/announced")
	first := h.evaluator.EvaluateActivity(wallet, send, view)
	expect(t, first, policy.CodeAllowed)
	expectSpends(t, first, map[string]int64{native: 5_000_000})
	again := h.evaluator.EvaluateActivity(wallet, send, view)
	expect(t, again, policy.CodeAllowed)
	expectSpends(t, again, map[string]int64{native: 5_000_000})
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("one request evaluated twice counted %s", got)
	}
	if got := h.requestsSeen(t, walletAddr); got != 1 {
		t.Fatalf("one request evaluated twice counted %d requests", got)
	}
	if err := h.ledger.Apply(policy.AccountKey(walletAddr), "key-1/announced", first.Spends, h.now); err != nil {
		t.Fatal(err)
	}
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("an announcement of the evaluated request counted %s", got)
	}

	bind := h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)}, h.request())
	expect(t, bind, policy.CodeAllowed)
	if bind.Spends == nil || len(bind.Spends) != 0 {
		t.Fatalf("binding decision spends %+v", bind.Spends)
	}
	if got := h.requestsSeen(t, walletAddr); got != 2 {
		t.Fatalf("a binding left %d requests in the ledger", got)
	}

	h.restart(t)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("native spent after a restart %s", got)
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, send, h.request()), policy.CodeDailyCap)

	raw, err := os.ReadFile(filepath.Join(h.dir, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(native)) || bytes.Contains(raw, []byte("5000000")) {
		t.Fatal("the kernel spend is stored in plaintext")
	}
}

func TestKernelGrantSpendsHoldAcrossRestart(t *testing.T) {
	h := newHarness(t)
	var oneShot grantVector
	for _, g := range h.vectors.Grants {
		if g.Name == "grant-one-shot" {
			oneShot = g
		}
	}
	if oneShot.Name == "" {
		t.Fatal("grant-one-shot vector is missing")
	}
	grant := oneShot.grant(t)
	request := &GrantRequest{PublicKey: hex32(t, ownerKeyHex), Grant: &grant, Digest: hex32(t, oneShot.Preimage)}
	generous := common.HexToAddress(generousAddr)
	allowed := h.evaluator.EvaluateGrant(generous, request, h.request())
	expect(t, allowed, policy.CodeAllowed)
	expectSpends(t, allowed, map[string]int64{"lx-grant:" + grantAsset: 50_000})
	h.restart(t)
	expect(t, h.evaluator.EvaluateGrant(generous, request, h.request()), policy.CodeDailyCap)
	if got := h.requestsSeen(t, generousAddr); got != 1 {
		t.Fatalf("grant requests counted %d, want the allowed one", got)
	}
}

func TestKernelEvaluationRefusesWithoutALedger(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	send := h.kernelActivity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	expect(t, h.evaluator.EvaluateActivity(wallet, send, nil), policy.CodeLedgerError)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)}, nil), policy.CodeLedgerError)
	expect(t, h.evaluator.EvaluateGrant(wallet, &GrantRequest{PublicKey: hex32(t, ownerKeyHex)}, nil), policy.CodeLedgerError)
	if got := h.spent(t, walletAddr, "lx:"+strings.Repeat("00", 32)); got.Sign() != 0 {
		t.Fatalf("an evaluation without a ledger recorded %s", got)
	}
	var missing *Evaluator
	expect(t, missing.EvaluateActivity(wallet, send, h.request()), policy.CodeNoPolicy)
}

func TestKernelVectorsDecodeInTheKernelLayout(t *testing.T) {
	k := loadKernelVectors(t)
	decoded := map[lxwire.ActivityType]int{}
	for _, v := range k.Activities {
		activity := v.activity(t)
		if !activity.PayloadHashMatches() {
			t.Fatalf("%s: payload hash", v.Name)
		}
		if hex.EncodeToString(activity.Payload) != v.Payload {
			t.Fatalf("%s: envelope payload is not the vector payload", v.Name)
		}
		preimage, err := lxwire.SignaturePreimage(activity)
		if err != nil || hex.EncodeToString(preimage[:]) != v.SignaturePreimage {
			t.Fatalf("%s: signature preimage %x (%v)", v.Name, preimage, err)
		}
		effect, err := DecodeEffect(activity)
		if !v.Accepted {
			if err == nil {
				t.Fatalf("%s: refused vector decoded to %+v", v.Name, effect)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		decoded[activity.Type]++
		want := v.disclosure(t)
		if effect.Account != want.Account || len(effect.Amounts) != 1 || effect.Amounts[0].Asset != want.Amounts[0].Asset ||
			effect.Amounts[0].Amount.Cmp(want.Amounts[0].Amount) != 0 || len(effect.Destinations) != 1 || effect.Destinations[0] != want.Destinations[0] {
			t.Fatalf("%s: effect %+v, want %+v", v.Name, effect, want)
		}
		if len(effect.Legs) != 1 || effect.Legs[0].From != want.Account || effect.Legs[0].To != want.Destinations[0] {
			t.Fatalf("%s: legs %+v", v.Name, effect.Legs)
		}
	}
	for _, kind := range []lxwire.ActivityType{OpAssetTransfer, OpAssetApprove, OpBudgetFund, OpProgramCall} {
		if decoded[kind] == 0 {
			t.Fatalf("no accepted vector for activity type %#x", uint32(kind))
		}
	}
	if decoded[OpAssetTransfer] < 2 {
		t.Fatalf("%d accepted asset sends", decoded[OpAssetTransfer])
	}
}

func TestKernelVectorsEvaluated(t *testing.T) {
	h := newHarness(t)
	doc, err := Parse([]byte(strings.Replace(kernelDocument, `"programs": [5]}`, `"programs": [5], "budget": [2]}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	h.evaluator.doc = doc
	wallet := common.HexToAddress(walletAddr)
	for _, v := range h.kernel.Activities {
		req := h.kernelActivity(t, v.Name)
		got := h.evaluator.EvaluateActivity(wallet, req, h.request())
		if v.Accepted {
			expect(t, got, policy.CodeAllowed)
			continue
		}
		expect(t, got, policy.CodeDecodeError)
	}
	native := "lx:" + strings.Repeat("00", 32)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000+250_000+1_000)) != 0 {
		t.Fatalf("native spent %s", got)
	}
	if got := h.spent(t, walletAddr, "lx:"+tokenAsset); got.Cmp(big.NewInt(75_000)) != 0 {
		t.Fatalf("token spent %s", got)
	}
}

func TestKernelSendEnvelopeChecks(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	var ownerSeed, peerSeed [32]byte
	for i := range ownerSeed {
		ownerSeed[i], peerSeed[i] = 0x07, 0x08
	}
	ownerKey, peerKey := ed25519.NewKeyFromSeed(ownerSeed[:]), ed25519.NewKeyFromSeed(peerSeed[:])
	owner, peer := hex32(t, h.kernel.PublicKey), hex32(t, h.kernel.PeerPublicKey)
	if [32]byte(ownerKey.Public().(ed25519.PublicKey)) != owner || [32]byte(peerKey.Public().(ed25519.PublicKey)) != peer {
		t.Fatal("vector keys are not the generator's seeds")
	}
	v := h.kernel.vector(t, "native-send")
	resigned := func(signer ed25519.PrivateKey, change func(s *lxwire.Send)) *ActivityRequest {
		activity := v.activity(t)
		send, err := lxwire.DecodeSend(activity.Payload)
		if err != nil {
			t.Fatal(err)
		}
		change(send)
		digest, err := send.AuthorizationDigest()
		if err != nil {
			t.Fatal(err)
		}
		copy(send.Signature[:], ed25519.Sign(signer, digest[:]))
		if activity.Payload, err = send.Encode(); err != nil {
			t.Fatal(err)
		}
		activity.PayloadHash = lxwire.PayloadHash(activity.Payload)
		return requestFor(t, activity, owner, v.disclosure(t))
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, resigned(ownerKey, func(*lxwire.Send) {}), h.request()), policy.CodeAllowed)
	for name, c := range map[string]struct {
		signer ed25519.PrivateKey
		change func(s *lxwire.Send)
	}{
		"session kind":       {ownerKey, func(s *lxwire.Send) { s.AuthorizationKind = 2 }},
		"controller":         {ownerKey, func(s *lxwire.Send) { s.Controller = s.To }},
		"signed context":     {ownerKey, func(s *lxwire.Send) { s.SignedContextHash[0] ^= 1 }},
		"network":            {ownerKey, func(s *lxwire.Send) { s.NetworkID++ }},
		"protocol version":   {ownerKey, func(s *lxwire.Send) { s.ProtocolVersion = lxwire.ProtocolVersion }},
		"idempotency key":    {ownerKey, func(s *lxwire.Send) { s.IdempotencyKey[0] ^= 1 }},
		"foreign authorizer": {peerKey, func(s *lxwire.Send) { s.PublicKey = peer }},
	} {
		got := h.evaluator.EvaluateActivity(wallet, resigned(c.signer, c.change), h.request())
		if got.Allowed || got.Code != policy.CodeDecodeError {
			t.Fatalf("%s: %+v", name, got)
		}
	}
}

func TestKernelBudgetFundChecks(t *testing.T) {
	k := loadKernelVectors(t)
	v := k.vector(t, "budget-fund")
	if _, err := DecodeEffect(v.activity(t)); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(a *lxwire.Activity){
		"defund tag":      func(a *lxwire.Activity) { a.Payload[1] = 0x07 },
		"field count":     func(a *lxwire.Activity) { a.Payload[3] = 7 },
		"no budget":       func(a *lxwire.Activity) { copy(a.Payload[4:36], make([]byte, 32)) },
		"same accounts":   func(a *lxwire.Activity) { copy(a.Payload[68:100], a.Payload[36:68]) },
		"zero amount":     func(a *lxwire.Activity) { copy(a.Payload[132:148], make([]byte, 16)) },
		"idempotency key": func(a *lxwire.Activity) { a.IdempotencyKey[0] ^= 1 },
		"trailing byte":   func(a *lxwire.Activity) { a.Payload = append(a.Payload, 0) },
		"truncated":       func(a *lxwire.Activity) { a.Payload = a.Payload[:len(a.Payload)-1] },
		"oversized":       func(a *lxwire.Activity) { a.Payload = append(a.Payload, make([]byte, lxwire.MaxSendPayloadBytes)...) },
	} {
		activity := v.activity(t)
		change(activity)
		if effect, err := DecodeEffect(activity); err == nil {
			t.Fatalf("%s: decoded to %+v", name, effect)
		}
	}
}
