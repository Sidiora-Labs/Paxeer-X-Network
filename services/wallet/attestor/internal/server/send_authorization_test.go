package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/lxwire"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/lx"
)

type pendingSend struct {
	send     *lxwire.Send
	activity *lxwire.Activity
}

func mainAccountOf(t *testing.T, key [32]byte) [32]byte {
	t.Helper()
	account, err := lxwire.AccountID([]byte(lxwire.MainAccountName(lxwire.DIDFromKey(key))))
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func newPendingSend(t *testing.T, pub ed25519.PublicKey, from *[32]byte, sequence uint64, amount uint64, notBefore, notAfter uint64) *pendingSend {
	t.Helper()
	var key, peer [32]byte
	copy(key[:], pub)
	for i := range peer {
		peer[i] = 0x52
	}
	debit := mainAccountOf(t, key)
	if from != nil {
		debit = *from
	}
	idempotency := sha256.Sum256([]byte(fmt.Sprintf("attestor send authorization %d", sequence)))
	context := sha256.Sum256([]byte(fmt.Sprintf("attestor send authorization context %d", sequence)))
	send := &lxwire.Send{
		From:              debit,
		To:                mainAccountOf(t, peer),
		Amount:            lxwire.Uint128{Lo: amount},
		SourceSequence:    sequence,
		IdempotencyKey:    idempotency,
		ExpiresAt:         notAfter,
		ContextHash:       context,
		AuthorizationKind: lxwire.OwnerAuthorization,
		Controller:        debit,
		PublicKey:         key,
		SignedContextHash: context,
		NetworkID:         testChainID,
		ProtocolVersion:   lxwire.MaxProtocolVersion,
	}
	return &pendingSend{send: send, activity: &lxwire.Activity{
		ProtocolVersion: lxwire.MaxProtocolVersion,
		NetworkID:       testChainID,
		Type:            lx.OpAssetTransfer,
		ActorDID:        []byte(lxwire.DIDFromKey(key)),
		Authority:       key[:],
		AccountSequence: sequence,
		NotBefore:       notBefore,
		NotAfter:        notAfter,
		IdempotencyKey:  idempotency,
		FeeLimit:        lxwire.Uint128{Lo: 1000},
	}}
}

func (p *pendingSend) envelope(t *testing.T) []byte {
	t.Helper()
	payload, err := p.send.Encode()
	if err != nil {
		t.Fatal(err)
	}
	p.activity.Payload = payload
	p.activity.PayloadHash = lxwire.PayloadHash(payload)
	unsigned, err := lxwire.EncodeUnsignedActivity(p.activity)
	if err != nil {
		t.Fatal(err)
	}
	return unsigned
}

func (p *pendingSend) digest(t *testing.T) []byte {
	t.Helper()
	digest, err := p.send.AuthorizationDigest()
	if err != nil {
		t.Fatal(err)
	}
	return digest[:]
}

// approvedSend plays the approval boundary the way the KMS signer stages a send: it discloses
// the approved debit itself (never a decoding of the placeholder) and binds the owner
// authorization digest, the stored key owner and the debit expiry capped by not_after.
func approvedSend(t *testing.T, p *pendingSend, keyID, session string) (*lx.Disclosure, *lx.Approval) {
	t.Helper()
	module, ok := lx.ModuleName(p.activity.Type.Module())
	if !ok {
		t.Fatalf("activity module %d is unknown", p.activity.Type.Module())
	}
	amount := p.send.Amount.Bytes()
	disclosure := &lx.Disclosure{
		Account: lx.ID(p.send.From), Module: module, Operation: p.activity.Type.Ordinal(),
		Amounts:      []lx.Amount{{Asset: lx.ID(p.send.Asset), Amount: new(big.Int).SetBytes(amount[:])}},
		Destinations: []lx.ID{lx.ID(p.send.To)}, Sequence: p.activity.AccountSequence, NotBefore: p.activity.NotBefore, NotAfter: p.activity.NotAfter,
	}
	var digest lx.ID
	copy(digest[:], p.digest(t))
	approval := &lx.Approval{
		Version: lx.ApprovalVersion, Principal: testOwner, KeyID: keyID, NetworkID: p.activity.NetworkID,
		ProtocolVersion: p.activity.ProtocolVersion, SessionID: session, ActivityDigest: digest, ExpiresAt: min(p.send.ExpiresAt, p.activity.NotAfter),
	}
	return disclosure, approval
}

func authorizeSend(t *testing.T, c *testCluster, keyID, session string, signers []string, p *pendingSend) []apiResult {
	t.Helper()
	unsigned := p.envelope(t)
	disclosure, approval := approvedSend(t, p, keyID, session)
	return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: session, KeyID: keyID, Kind: KindLXSendAuth, Signers: signers, Activity: hex.EncodeToString(unsigned), Disclosure: disclosure, Approval: approval}
	}, c.idp.mint(t, c.idp.key, testOwner))
}

func auditHeads(c *testCluster, signers []string) map[string]uint64 {
	heads := map[string]uint64{}
	for _, n := range c.byID(signers...) {
		seq, _ := n.audit.Head()
		heads[n.id] = seq
	}
	return heads
}

func expectAuditedRefusal(t *testing.T, c *testCluster, label string, signers []string, before map[string]uint64, results []apiResult, code string) {
	t.Helper()
	expectPolicyCode(t, label, results, code)
	for _, n := range c.byID(signers...) {
		seq, _ := n.audit.Head()
		if seq != before[n.id]+1 {
			t.Fatalf("%s: %s audit head moved from %d to %d", label, n.id, before[n.id], seq)
		}
		if err := n.audit.Verify(); err != nil {
			t.Fatalf("%s: %s audit chain: %v", label, n.id, err)
		}
	}
}

func TestSendAuthorizationSignedByTheHeldKeyCompletesAnAcceptedSend(t *testing.T) {
	c := newTestCluster(t, 5, true)
	account := common.HexToAddress("0x9999999999999999999999999999999999999999").Hex()
	pub := importKernelKey(t, c, "send-lx", "attestor send authorization ed25519 key", account)
	signers := []string{"node-1", "node-2", "node-3"}
	now := uint64(time.Now().Unix())

	pending := newPendingSend(t, pub, nil, 1, 5_000_000, now-60, now+600)
	placeholder := pending.envelope(t)
	digest := pending.digest(t)
	results := decodeOK[SignResponse](t, "a send authorization", authorizeSend(t, c, "send-lx", "authorize-1", signers, pending))
	signature := results[0].Signature
	for _, r := range results {
		sig, _ := hex.DecodeString(r.Signature)
		if r.Kind != KindLXSendAuth || r.SignedBytes != hex.EncodeToString(digest) || !ed25519.Verify(pub, digest, sig) {
			t.Fatalf("%s: authorization %+v does not sign the owner authorization digest", r.NodeID, r)
		}
		if r.Signature != signature {
			t.Fatalf("%s: signers returned different signatures", r.NodeID)
		}
	}
	for _, n := range c.nodes {
		if spent, _ := nodeLedger(t, n, account); spent.Sign() != 0 {
			t.Fatalf("%s: an authorization alone counted %s", n.id, spent)
		}
	}
	placeholderPre, err := lxwire.SignaturePreimage(pending.activity)
	if err != nil {
		t.Fatal(err)
	}
	placeholderDisclosure, placeholderApproval := approvedSend(t, pending, "send-lx", "authorize-replayed")
	placeholderApproval.ActivityDigest = lx.ID(placeholderPre)
	for _, r := range c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "authorize-replayed", KeyID: "send-lx", Kind: KindLXActivity, Signers: signers, Activity: hex.EncodeToString(placeholder), Disclosure: placeholderDisclosure, Approval: placeholderApproval}
	}, c.idp.mint(t, c.idp.key, testOwner)) {
		if e := expectError(t, "an unsigned placeholder signed as an activity", r, CodePolicyDenied); e.PolicyCode != policy.CodeDecodeError {
			t.Fatalf("an unsigned placeholder signed as an activity: %s", e.PolicyCode)
		}
	}

	sig, err := hex.DecodeString(signature)
	if err != nil {
		t.Fatal(err)
	}
	copy(pending.send.Signature[:], sig)
	completed := pending.envelope(t)
	strict, err := lxwire.DecodeSend(pending.activity.Payload)
	if err != nil || !strict.AuthorizationValid() {
		t.Fatalf("the completed send does not pass the strict decoder (%v)", err)
	}
	decoded, err := lxwire.DecodeUnsignedActivity(completed, c.nodes[0].server.opts.Activities)
	if err != nil {
		t.Fatal(err)
	}
	preimage, err := lxwire.SignaturePreimage(decoded)
	if err != nil {
		t.Fatal(err)
	}
	verifyKernel(t, "the completed send signed as an activity", pub, preimage[:], signKernel(t, c, "send-lx", "activity-1", []string{"node-3", "node-4", "node-5"}, completed))
	for _, n := range c.nodes {
		if spent, _ := nodeLedger(t, n, account); spent.Cmp(big.NewInt(5_000_000)) != 0 {
			t.Fatalf("%s: an authorized and signed send counted %s", n.id, spent)
		}
	}

	second := newPendingSend(t, pub, nil, 2, 5_000_000, now-60, now+600)
	before := auditHeads(c, signers)
	expectAuditedRefusal(t, c, "a second authorization past the daily cap", signers, before, authorizeSend(t, c, "send-lx", "authorize-2", signers, second), policy.CodeDailyCap)
	auditLog := auditBytes(t, c, c.byID("node-1")[0])
	if !bytes.Contains(auditLog, []byte("sign."+KindLXSendAuth)) {
		t.Fatal("the audit log names no send authorization")
	}
}

func TestSendAuthorizationRefusals(t *testing.T) {
	c := newTestCluster(t, 5, true)
	account := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	pub := importKernelKey(t, c, "refuse-lx", "attestor send authorization refusal ed25519 key", account)
	signers := []string{"node-2", "node-4", "node-5"}
	now := uint64(time.Now().Unix())
	var stranger [32]byte
	for i := range stranger {
		stranger[i] = 0x61
	}
	foreign := mainAccountOf(t, stranger)

	signed := newPendingSend(t, pub, nil, 4, 1_000, now-60, now+600)
	signed.send.Signature[0] = 1
	cases := []struct {
		label   string
		pending *pendingSend
		code    string
	}{
		{"a foreign debit account", newPendingSend(t, pub, &foreign, 1, 1_000, now-60, now+600), lx.CodeAccountNotOwned},
		{"an expired validity window", newPendingSend(t, pub, nil, 2, 1_000, now-1200, now-600), lx.CodeOutsideValidity},
		{"a window that has not opened", newPendingSend(t, pub, nil, 3, 1_000, now+600, now+1200), lx.CodeOutsideValidity},
		{"a send that already carries a signature", signed, policy.CodeDecodeError},
		{"an amount over the operation cap", newPendingSend(t, pub, nil, 5, 6_000_001, now-60, now+600), policy.CodeValueCap},
	}
	for i, tc := range cases {
		before := auditHeads(c, signers)
		expectAuditedRefusal(t, c, tc.label, signers, before, authorizeSend(t, c, "refuse-lx", fmt.Sprintf("refuse-%d", i), signers, tc.pending), tc.code)
	}

	pending := newPendingSend(t, pub, nil, 6, 1_000, now-60, now+600)
	pending.activity.NetworkID = testChainID + 1
	pending.send.NetworkID = testChainID + 1
	before := auditHeads(c, signers)
	expectAuditedRefusal(t, c, "a send on another chain", signers, before, authorizeSend(t, c, "refuse-lx", "refuse-chain", signers, pending), policy.CodeChainMismatch)

	pending = newPendingSend(t, pub, nil, 7, 1_000, now-60, now+600)
	pending.activity.ActorDID = []byte(lxwire.DIDFromKey(stranger))
	before = auditHeads(c, signers)
	expectAuditedRefusal(t, c, "an actor that is not the held key", signers, before, authorizeSend(t, c, "refuse-lx", "refuse-actor", signers, pending), lx.CodeAuthorityMismatch)

	for _, n := range c.nodes {
		if spent, _ := nodeLedger(t, n, account); spent.Sign() != 0 {
			t.Fatalf("%s: refused authorizations counted %s", n.id, spent)
		}
	}
}
