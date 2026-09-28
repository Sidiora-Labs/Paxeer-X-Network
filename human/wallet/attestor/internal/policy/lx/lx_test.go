package lx

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
)

const (
	vectorPath   = "../../lxwire/testdata/activities.json"
	ownerKeyHex  = "af06a3e3291714e4f356c19c9b15cd1951ec6e6662aa77be07547f289383341d"
	peerKeyHex   = "5151515151515151515151515151515151515151515151515151515151515151"
	ownerMain    = "8109bd8602f591480606a006552834a9fa46f3f5a0ec1be754163d6cda0810e7"
	peerMain     = "1eb1fd9ca7a0fb3a45a93cf1506f4b6c85b30e567272a5631fcbeef4270d0994"
	tokenAsset   = "7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b"
	grantAsset   = "0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"
	walletAddr   = "0x1111111111111111111111111111111111111111"
	generousAddr = "0x2222222222222222222222222222222222222222"
	narrowAddr   = "0x3333333333333333333333333333333333333333"
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
	ledger    *policy.MemoryLedger
	server    *nonceServer
	now       time.Time
	vectors   *vectors
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
	h := &harness{server: newNonceServer(t, 3), now: time.Unix(1_800_000_100, 0).UTC(), vectors: loadVectors(t)}
	h.ledger = policy.NewMemoryLedger(func() time.Time { return h.now })
	chain, err := NewChain(h.server.URL, h.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.New(engineDoc)
	if h.evaluator, err = New(engine, kernelDoc, h.ledger, chain); err != nil {
		t.Fatal(err)
	}
	if _, err := New(engine, kernelDoc, h.ledger, chain); err == nil {
		t.Fatal("kernel kinds registered twice on one engine")
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

func nativeSendDisclosure(t *testing.T) Disclosure {
	return Disclosure{
		Account:      id(t, ownerMain),
		Module:       "asset",
		Operation:    5,
		Amounts:      []Amount{{Asset: ID{}, Amount: big.NewInt(5_000_000)}},
		Destinations: []ID{id(t, peerMain)},
		Sequence:     1,
		NotBefore:    1_800_000_000,
		NotAfter:     1_800_000_600,
	}
}

func expect(t *testing.T, got policy.Decision, code string) {
	t.Helper()
	if got.Code != code || got.Allowed != (code == policy.CodeAllowed) || got.Reason == "" {
		t.Fatalf("decision %+v, want code %s", got, code)
	}
}

func (h *harness) spent(t *testing.T, account, key string) *big.Int {
	t.Helper()
	total, err := h.ledger.Spent(strings.ToLower(account), key, h.now.Add(-policy.SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func TestActivityMatchingDisclosureAccepted(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	native := "lx:" + strings.Repeat("00", 32)

	send := h.activity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	expect(t, h.evaluator.EvaluateActivity(wallet, send), policy.CodeAllowed)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("native spent %s", got)
	}

	tokenSend := h.activity(t, "token-send")
	ownerTokenAccount, err := lxwire.AssetAccountName(lxwire.DIDFromKey(hex32(t, ownerKeyHex)), hex32(t, tokenAsset), [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	peerTokenAccount, err := lxwire.AssetAccountName(lxwire.DIDFromKey(hex32(t, peerKeyHex)), hex32(t, tokenAsset), [32]byte{})
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
	tokenSend.Disclosure = Disclosure{
		Account: from, Module: "asset", Operation: 5,
		Amounts:      []Amount{{Asset: id(t, tokenAsset), Amount: big.NewInt(75_000)}},
		Destinations: []ID{to}, Sequence: 2, NotBefore: 1_800_000_000, NotAfter: 1_800_000_600,
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, tokenSend), policy.CodeAllowed)

	approval := h.activity(t, "approval")
	approval.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "asset", Operation: 7,
		Amounts:      []Amount{{Asset: id(t, grantAsset), Amount: big.NewInt(50_000)}},
		Destinations: []ID{id(t, peerMain)}, Sequence: 3, NotBefore: 1_800_000_000, NotAfter: 1_800_003_600,
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, approval), policy.CodeAllowed)

	action := h.activity(t, "agent-action")
	action.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "programs", Operation: 5,
		Amounts:      []Amount{{Asset: ID{}, Amount: big.NewInt(1_000)}, {Asset: id(t, tokenAsset), Amount: big.NewInt(2_000)}},
		Destinations: []ID{id(t, peerMain), id(t, ownerMain)}, Sequence: 5, NotBefore: 0, NotAfter: ^uint64(0),
	}
	expect(t, h.evaluator.EvaluateActivity(wallet, action), policy.CodeAllowed)
	if got := h.spent(t, walletAddr, native); got.Cmp(big.NewInt(5_001_000)) != 0 {
		t.Fatalf("native spent after the program call %s", got)
	}
	if got := h.spent(t, walletAddr, "lx:"+tokenAsset); got.Cmp(big.NewInt(75_000)) != 0 {
		t.Fatalf("incoming program leg counted as a spend: %s", got)
	}

	expect(t, h.evaluator.EvaluateActivity(wallet, send), policy.CodeDailyCap)
	h.now = time.Unix(1_800_000_601, 0).UTC()
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(generousAddr), send), CodeOutsideValidity)
}

func TestActivityMismatchedAmountRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	send := h.activity(t, "native-send")

	send.Disclosure = nativeSendDisclosure(t)
	send.Disclosure.Amounts[0].Amount = big.NewInt(5_000_001)
	refused := h.evaluator.EvaluateActivity(wallet, send)
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
		if got := h.evaluator.EvaluateActivity(wallet, send); got.Code != CodeDisclosureMismatch || got.Allowed {
			t.Fatalf("%s mismatch: %+v", field, got)
		}
	}

	send.Disclosure = nativeSendDisclosure(t)
	send.Digest[0] ^= 1
	expect(t, h.evaluator.EvaluateActivity(wallet, send), policy.CodeDigestMismatch)
	send.Digest[0] ^= 1
	send.PublicKey = hex32(t, peerKeyHex)
	expect(t, h.evaluator.EvaluateActivity(wallet, send), CodeAuthorityMismatch)

	over := h.activity(t, "native-send")
	over.Disclosure = nativeSendDisclosure(t)
	doc, err := Parse([]byte(strings.Replace(kernelDocument, `"per_operation": "6000000"`, `"per_operation": "4999999"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	h.evaluator.doc = doc
	expect(t, h.evaluator.EvaluateActivity(wallet, over), policy.CodeValueCap)
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(narrowAddr), over), policy.CodeDestinationBlocked)
}

func TestActivityDisallowedModuleRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)

	budget := h.activity(t, "budget-change")
	budget.Disclosure = Disclosure{Module: "budget", Operation: 2, Sequence: 4, NotBefore: 1_800_000_000, NotAfter: 1_800_000_600}
	expect(t, h.evaluator.EvaluateActivity(wallet, budget), CodeModuleNotAllowed)

	approval := h.activity(t, "approval")
	approval.Disclosure = Disclosure{
		Account: id(t, ownerMain), Module: "asset", Operation: 7,
		Amounts:      []Amount{{Asset: id(t, grantAsset), Amount: big.NewInt(50_000)}},
		Destinations: []ID{id(t, peerMain)}, Sequence: 3, NotBefore: 1_800_000_000, NotAfter: 1_800_003_600,
	}
	expect(t, h.evaluator.EvaluateActivity(common.HexToAddress(narrowAddr), approval), CodeOperationNotAllowed)
	expect(t, h.evaluator.EvaluateActivity(wallet, approval), policy.CodeAllowed)

	unknownModule := h.activity(t, "native-send")
	unknownModule.Envelope[activityTypeOffset] = 0
	unknownModule.Envelope[activityTypeOffset+1] = 12
	expect(t, h.evaluator.EvaluateActivity(wallet, unknownModule), CodeUnknownModule)
	unknownOperation := h.activity(t, "native-send")
	unknownOperation.Envelope[activityTypeOffset+3] = 6
	expect(t, h.evaluator.EvaluateActivity(wallet, unknownOperation), CodeUnknownOperation)
	truncated := h.activity(t, "native-send")
	truncated.Envelope = truncated.Envelope[:len(truncated.Envelope)-1]
	expect(t, h.evaluator.EvaluateActivity(wallet, truncated), policy.CodeDecodeError)
	expect(t, h.evaluator.EvaluateActivity(wallet, &ActivityRequest{}), policy.CodeMissingField)
	expect(t, h.evaluator.EvaluateActivity(wallet, nil), policy.CodeMissingField)
}

func TestBindForAnotherAddressRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	other := common.HexToAddress(generousAddr)
	refused := h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, other, 3)})
	expect(t, refused, CodeBindAddress)
	if calls, _, _ := h.server.seen(); calls != 0 {
		t.Fatalf("a binding for another address reached the chain %d times", calls)
	}
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(1, wallet, 3)}), policy.CodeChainMismatch)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)[:76]}), policy.CodeDecodeError)
	expect(t, h.evaluator.EvaluateBind(wallet, nil), policy.CodeMissingField)
}

func TestBindStaleNonceRefused(t *testing.T) {
	h := newHarness(t)
	wallet := common.HexToAddress(walletAddr)
	h.server.setNonce(4)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 3)}), CodeStaleBindNonce)
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 4)}), policy.CodeAllowed)
	calls, asked, failed := h.server.seen()
	if calls != 2 || failed != "" || asked[0] != wallet || asked[1] != wallet {
		t.Fatalf("chain saw %d calls for %v (%q)", calls, asked, failed)
	}
	h.server.Close()
	expect(t, h.evaluator.EvaluateBind(wallet, &BindRequest{Message: lxwire.BindMessage(125, wallet, 4)}), CodeChainUnavailable)
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
	refused := h.evaluator.EvaluateGrant(wallet, request)
	expect(t, refused, CodeGrantCap)
	generous := common.HexToAddress(generousAddr)
	expect(t, h.evaluator.EvaluateGrant(generous, request), policy.CodeAllowed)
	if got := h.spent(t, generousAddr, "lx-grant:"+grantAsset); got.Cmp(big.NewInt(50_000)) != 0 {
		t.Fatalf("granted allowance recorded as %s", got)
	}
	expect(t, h.evaluator.EvaluateGrant(generous, request), policy.CodeDailyCap)

	tampered := *request
	tampered.Digest[31] ^= 1
	expect(t, h.evaluator.EvaluateGrant(generous, &tampered), policy.CodeDigestMismatch)
	foreign := *request
	foreign.PublicKey = hex32(t, peerKeyHex)
	expect(t, h.evaluator.EvaluateGrant(generous, &foreign), CodeAuthorityMismatch)
	h.now = time.Unix(1_900_000_000, 0).UTC()
	expect(t, h.evaluator.EvaluateGrant(generous, request), CodeOutsideValidity)
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
	expect(t, h.evaluator.EvaluateGrant(wallet, receiveRequest), policy.CodeAllowed)
	ownerReceive := *receiveRequest
	ownerReceive.PublicKey = hex32(t, ownerKeyHex)
	expect(t, h.evaluator.EvaluateGrant(wallet, &ownerReceive), CodeAccountNotOwned)
	large := receive
	large.Amount = lxwire.Uint128{Lo: 10_001}
	largeDigest, err := lxwire.ReceivePreimage(large)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.evaluator.EvaluateGrant(wallet, &GrantRequest{PublicKey: hex32(t, peerKeyHex), Receive: &large, Digest: largeDigest}), CodeGrantCap)
	expect(t, h.evaluator.EvaluateGrant(wallet, &GrantRequest{PublicKey: hex32(t, ownerKeyHex)}), policy.CodeMissingField)
}

func TestRecordDecision(t *testing.T) {
	h := newHarness(t)
	log, err := audit.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	wallet := common.HexToAddress(walletAddr)
	send := h.activity(t, "native-send")
	send.Disclosure = nativeSendDisclosure(t)
	send.Disclosure.Amounts[0].Amount = big.NewInt(1)
	refused := h.evaluator.EvaluateActivity(wallet, send)
	record, err := Record(log, policy.KindLXActivity, "key-1", "session-1", send.Envelope, refused)
	if err != nil {
		t.Fatal(err)
	}
	if record.Decision != DecisionDeny || record.Reason != CodeDisclosureMismatch || record.Kind != policy.KindLXActivity || record.Sequence != 1 {
		t.Fatalf("refusal recorded as %+v", record)
	}
	send.Disclosure = nativeSendDisclosure(t)
	allowed := h.evaluator.EvaluateActivity(wallet, send)
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
