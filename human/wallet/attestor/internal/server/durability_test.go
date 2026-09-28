package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/lx"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

func importEd25519(t *testing.T, c *testCluster, keyID, label string) (ed25519.PublicKey, string) {
	t.Helper()
	seed := sha256.Sum256([]byte(label))
	pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
	scalar, err := dealer.Ed25519ScalarFromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	account := common.HexToAddress("0x4444444444444444444444444444444444444444").Hex()
	importKey(t, c, keyID, dealer.Ed25519, scalar, account)
	return pub, account
}

func signBind(t *testing.T, c *testCluster, keyID, session string, signers []string, account string, token string) []apiResult {
	t.Helper()
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: session, KeyID: keyID, Kind: KindLXBind, Signers: signers, Message: hex.EncodeToString(bind)}
	}, token)
}

func verifyBind(t *testing.T, label string, pub ed25519.PublicKey, account string, results []apiResult) {
	t.Helper()
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	for _, r := range decodeOK[SignResponse](t, label, results) {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(pub, bind, sig) {
			t.Fatalf("%s: %s signature does not verify", label, r.NodeID)
		}
	}
}

func TestRefreshKeepsTheOldEpochWhenAParticipantStopsBeforeCommit(t *testing.T) {
	c := newTestClusterWith(t, 5, true, testPolicy(), func(o *Options) {
		o.PeerTimeout = 3 * time.Second
		o.RoundTimeout = 20 * time.Second
	})
	pub, account := importEd25519(t, c, "lx-key", "attestor two-phase refresh ed25519 key")

	stopped := c.byID("node-3")[0]
	staged := make(chan struct{})
	release := make(chan struct{})
	stopped.server.afterStage = func(string) {
		close(staged)
		<-release
	}
	others := c.byID("node-1", "node-2", "node-4", "node-5")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = c.post(stopped.apiAddr, PathRefresh, RefreshRequest{SessionID: "refresh-stopped", KeyID: "lx-key"}, "")
	}()
	results := c.callAll(t, others, PathRefresh, func(*testNode) any {
		return RefreshRequest{SessionID: "refresh-stopped", KeyID: "lx-key"}
	}, "")
	select {
	case <-staged:
	case <-time.After(time.Minute):
		t.Fatal("the stopped node never staged its share")
	}
	for i, r := range results {
		expectError(t, "refresh with a participant stopped before commit on "+others[i].id, r, CodeSessionFailed)
	}
	stage, err := stopped.store.GetStaged("lx-key")
	if err != nil || stage.Epoch != 1 {
		t.Fatalf("the stopped node holds no stage at epoch 1: %+v %v", stage, err)
	}
	stopped.stop()
	close(release)
	wg.Wait()

	for _, n := range others {
		rec, err := n.store.Get("lx-key")
		if err != nil || rec.Epoch != 0 {
			t.Fatalf("%s: committed epoch after an aborted refresh: %+v %v", n.id, rec, err)
		}
		if _, err := n.store.GetStaged("lx-key"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s: an aborted refresh left a stage: %v", n.id, err)
		}
	}

	returned := c.restart(t, stopped)
	rec, err := returned.store.Get("lx-key")
	if err != nil || rec.Epoch != 0 {
		t.Fatalf("the returned node committed epoch: %+v %v", rec, err)
	}
	if _, err := returned.store.GetStaged("lx-key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the returned node kept its uncommitted stage: %v", err)
	}
	logged, err := os.ReadFile(filepath.Join(c.configs[returned.index].nodeDir, "audit", audit.FileName))
	if err != nil || !bytes.Contains(logged, []byte("uncommitted stage discarded at start")) {
		t.Fatalf("the returned node did not audit the discarded stage: %v", err)
	}

	verifyBind(t, "sign at the old epoch after the node returned", pub, account, signBind(t, c, "lx-key", "sign-after-return", []string{"node-1", "node-3", "node-5"}, account, c.idp.mint(t, c.idp.key, testOwner)))

	refreshed := decodeOK[KeyResponse](t, "refresh with every participant", c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
		return RefreshRequest{SessionID: "refresh-all", KeyID: "lx-key"}
	}, ""))
	for _, r := range refreshed {
		if r.Epoch != 1 || !r.Refreshed {
			t.Fatalf("%s: refresh with every participant answered %+v", r.NodeID, r)
		}
	}
	for _, n := range c.nodes {
		if rec, err := n.store.Get("lx-key"); err != nil || rec.Epoch != 1 {
			t.Fatalf("%s: committed epoch after a full refresh: %+v %v", n.id, rec, err)
		}
	}
	verifyBind(t, "sign at the new epoch", pub, account, signBind(t, c, "lx-key", "sign-new-epoch", []string{"node-2", "node-4", "node-5"}, account, c.idp.mint(t, c.idp.key, testOwner)))

	_, account2 := importEd25519(t, c, "skew-key", "attestor epoch skew ed25519 key")
	skewed := c.byID("node-5")[0]
	skewRec, err := skewed.store.Get("skew-key")
	if err != nil {
		t.Fatal(err)
	}
	var plain []byte
	if err := skewed.store.WithShare("skew-key", func(p []byte) error { plain = append([]byte(nil), p...); return nil }); err != nil {
		t.Fatal(err)
	}
	skewRec.Epoch = 1
	if err := skewed.store.PutStaged(skewRec, plain); err != nil {
		t.Fatal(err)
	}
	if err := skewed.store.CommitStaged("skew-key", 1); err != nil {
		t.Fatal(err)
	}
	for i, r := range signBind(t, c, "skew-key", "sign-epoch-skew", []string{"node-1", "node-2", "node-5"}, account2, c.idp.mint(t, c.idp.key, testOwner)) {
		if r.status == http.StatusOK {
			t.Fatalf("signers at different epochs produced a signature on result %d: %s", i, r.body)
		}
	}
}

func TestSpendLedgerHoldsAcrossSignerSetsAndRestarts(t *testing.T) {
	doc := testPolicy()
	doc.Defaults.Caps = map[string]policy.Cap{policy.AssetNative: {PerTransaction: "1000000000000000000", Daily: "3000000000000000000"}}
	c := newTestClusterWith(t, 5, true, doc, nil)

	seed := sha256.Sum256([]byte("attestor shared ledger secp256k1 key"))
	key, err := gethcrypto.ToECDSA(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	address := gethcrypto.PubkeyToAddress(key.PublicKey)
	importKey(t, c, "evm-key", dealer.Secp256k1, new(big.Int).SetBytes(seed[:]), "")
	for _, r := range decodeOK[KeyResponse](t, "refresh", c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
		return RefreshRequest{SessionID: "refresh-ledger", KeyID: "evm-key"}
	}, "")) {
		if r.Epoch != 1 {
			t.Fatalf("%s: refresh committed epoch %d", r.NodeID, r.Epoch)
		}
	}

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	txSigner := types.LatestSignerForChainID(big.NewInt(testChainID))
	onePAX := big.NewInt(1_000_000_000_000_000_000)
	sign := func(session string, nonce uint64, signers []string) ([]apiResult, *types.Transaction) {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID: big.NewInt(testChainID), Nonce: nonce, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
			Gas: 21000, To: &to, Value: onePAX,
		})
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
			return SignRequest{SessionID: session, KeyID: "evm-key", Kind: KindEVMTransaction, Signers: signers, Transaction: hex.EncodeToString(raw)}
		}, c.idp.mint(t, c.idp.key, testOwner)), tx
	}
	for i, signers := range [][]string{{"node-1", "node-2", "node-3"}, {"node-3", "node-4", "node-5"}, {"node-1", "node-4", "node-5"}} {
		results, tx := sign("spread-"+signers[0]+signers[2], uint64(i), signers)
		for _, r := range decodeOK[SignResponse](t, "spend within the cap", results) {
			sig, _ := hex.DecodeString(r.Signature)
			signed, err := tx.WithSignature(txSigner, sig)
			if err != nil {
				t.Fatal(err)
			}
			if from, err := types.Sender(txSigner, signed); err != nil || from != address {
				t.Fatalf("%s: recovered %s (%v)", r.NodeID, from.Hex(), err)
			}
		}
	}
	expectDailyCap := func(label string, results []apiResult) {
		t.Helper()
		for _, r := range results {
			e := expectError(t, label, r, CodePolicyDenied)
			if e.PolicyCode != policy.CodeDailyCap {
				t.Fatalf("%s: policy code %s", label, e.PolicyCode)
			}
		}
	}
	results, _ := sign("spread-fourth", 3, []string{"node-2", "node-4", "node-5"})
	expectDailyCap("a fourth spend over a signer set that never signed together", results)

	c.restart(t, c.byID("node-2")[0])
	results, _ = sign("spread-after-restart", 4, []string{"node-2", "node-3", "node-4"})
	expectDailyCap("a spend after a signer restarted", results)
}

func TestStalledPeerAbortsTheSessionWithAnAuditEntry(t *testing.T) {
	c := newTestClusterWith(t, 5, true, testPolicy(), func(o *Options) {
		o.RoundTimeout = 2 * time.Second
	})
	pub, account := importEd25519(t, c, "lx-key", "attestor stalled peer ed25519 key")

	signers := []string{"node-1", "node-2", "node-3"}
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	start := time.Now()
	results := c.callAll(t, c.byID("node-1", "node-2"), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "sign-stalled", KeyID: "lx-key", Kind: KindLXBind, Signers: signers, Message: hex.EncodeToString(bind)}
	}, c.idp.mint(t, c.idp.key, testOwner))
	if elapsed := time.Since(start); elapsed > time.Minute {
		t.Fatalf("a stalled session took %s to abort", elapsed)
	}
	for _, r := range results {
		expectError(t, "a signer that never joined", r, CodeSessionTimeout)
	}
	for _, n := range c.byID("node-1", "node-2") {
		logged, err := os.ReadFile(filepath.Join(c.configs[n.index].nodeDir, "audit", audit.FileName))
		if err != nil || !bytes.Contains(logged, []byte("session.stalled")) || !bytes.Contains(logged, []byte("node-3")) {
			t.Fatalf("%s: no stalled-session audit entry naming the silent peer (%v)", n.id, err)
		}
		if err := n.audit.Verify(); err != nil {
			t.Fatalf("%s: audit chain: %v", n.id, err)
		}
	}
	verifyBind(t, "sign after a stalled session", pub, account, signBind(t, c, "lx-key", "sign-after-stall", signers, account, c.idp.mint(t, c.idp.key, testOwner)))
}

const kernelLedgerPolicy = `{"version":1,"defaults":{"modules":{"programs":[5]},"caps":{"native":{"per_operation":"6000000","daily":"%s"}}}}`

var kernelNative = "lx:" + hex.EncodeToString(make([]byte, 32))

func useKernelPolicy(t *testing.T, c *testCluster, daily string) {
	t.Helper()
	doc, err := lx.Parse([]byte(fmt.Sprintf(kernelLedgerPolicy, daily)))
	if err != nil {
		t.Fatal(err)
	}
	c.kernel = doc
	for _, n := range append([]*testNode(nil), c.nodes...) {
		c.restart(t, n)
	}
}

func importKernelKey(t *testing.T, c *testCluster, keyID, label, account string) ed25519.PublicKey {
	t.Helper()
	seed := sha256.Sum256([]byte(label))
	scalar, err := dealer.Ed25519ScalarFromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	importKey(t, c, keyID, dealer.Ed25519, scalar, account)
	return ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
}

func programTransfer(t *testing.T, pub ed25519.PublicKey, sequence uint64, amount int64) ([]byte, []byte) {
	t.Helper()
	var key [32]byte
	copy(key[:], pub)
	did := lxwire.DIDFromKey(key)
	from, err := lxwire.AccountID([]byte(lxwire.MainAccountName(did)))
	if err != nil {
		t.Fatal(err)
	}
	to, err := lxwire.AccountID([]byte(lxwire.MainAccountName(lxwire.DIDFromKey(sha256.Sum256([]byte("kernel ledger recipient"))))))
	if err != nil {
		t.Fatal(err)
	}
	program := sha256.Sum256([]byte("kernel ledger program"))
	value := make([]byte, 16)
	big.NewInt(amount).FillBytes(value)
	payload := append(append([]byte{}, program[:]...), 0, 1)
	payload = append(append(append(append(payload, from[:]...), make([]byte, 32)...), to[:]...), value...)
	now := uint64(time.Now().Unix())
	a := &lxwire.Activity{
		ProtocolVersion: lxwire.MaxProtocolVersion,
		NetworkID:       testChainID,
		Type:            lx.OpProgramCall,
		ActorDID:        []byte(did),
		Authority:       key[:],
		AccountSequence: sequence,
		NotBefore:       now - 60,
		NotAfter:        now + 600,
		IdempotencyKey:  sha256.Sum256([]byte(fmt.Sprintf("kernel ledger activity %d", sequence))),
		FeeLimit:        lxwire.Uint128{Lo: 1000},
		PayloadHash:     lxwire.PayloadHash(payload),
		Payload:         payload,
	}
	unsigned, err := lxwire.EncodeUnsignedActivity(a)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := lxwire.SignaturePreimage(a)
	if err != nil {
		t.Fatal(err)
	}
	return unsigned, pre[:]
}

func signKernel(t *testing.T, c *testCluster, keyID, session string, signers []string, unsigned []byte) []apiResult {
	t.Helper()
	return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: session, KeyID: keyID, Kind: KindLXActivity, Signers: signers, Activity: hex.EncodeToString(unsigned)}
	}, c.idp.mint(t, c.idp.key, testOwner))
}

func verifyKernel(t *testing.T, label string, pub ed25519.PublicKey, preimage []byte, results []apiResult) {
	t.Helper()
	for _, r := range decodeOK[SignResponse](t, label, results) {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(pub, preimage, sig) {
			t.Fatalf("%s: %s signature does not verify", label, r.NodeID)
		}
	}
}

func expectPolicyCode(t *testing.T, label string, results []apiResult, code string) {
	t.Helper()
	for _, r := range results {
		if e := expectError(t, label, r, CodePolicyDenied); e.PolicyCode != code {
			t.Fatalf("%s: policy code %s, want %s", label, e.PolicyCode, code)
		}
	}
}

func nodeLedger(t *testing.T, n *testNode, account string) (*big.Int, int) {
	t.Helper()
	ledger, err := policy.NewSpendLedger(n.store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	spent, err := ledger.Spent(policy.AccountKey(account), kernelNative, now.Add(-policy.SpendWindow))
	if err != nil {
		t.Fatal(err)
	}
	requests, err := ledger.Requests(policy.AccountKey(account), now.Add(-policy.RateWindow))
	if err != nil {
		t.Fatal(err)
	}
	return spent, requests
}

func TestRequestRateCountsEVMAndKernelSignsAcrossARestart(t *testing.T) {
	doc := testPolicy()
	rate := uint32(2)
	doc.Defaults.RatePerMinute = &rate
	c := newTestClusterWith(t, 5, true, doc, nil)
	useKernelPolicy(t, c, "100000000")

	seed := sha256.Sum256([]byte("attestor shared rate secp256k1 key"))
	key, err := gethcrypto.ToECDSA(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	address := gethcrypto.PubkeyToAddress(key.PublicKey)
	importKey(t, c, "rate-evm", dealer.Secp256k1, new(big.Int).SetBytes(seed[:]), "")
	decodeOK[KeyResponse](t, "refresh", c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
		return RefreshRequest{SessionID: "refresh-rate", KeyID: "rate-evm"}
	}, ""))
	pub := importKernelKey(t, c, "rate-lx", "attestor shared rate ed25519 key", address.Hex())

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	txSigner := types.LatestSignerForChainID(big.NewInt(testChainID))
	signEVM := func(session string, nonce uint64, signers []string) ([]apiResult, *types.Transaction) {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID: big.NewInt(testChainID), Nonce: nonce, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
			Gas: 21000, To: &to, Value: big.NewInt(1_000),
		})
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
			return SignRequest{SessionID: session, KeyID: "rate-evm", Kind: KindEVMTransaction, Signers: signers, Transaction: hex.EncodeToString(raw)}
		}, c.idp.mint(t, c.idp.key, testOwner)), tx
	}

	unsigned, preimage := programTransfer(t, pub, 1, 1_000_000)
	verifyKernel(t, "a kernel sign within the rate", pub, preimage, signKernel(t, c, "rate-lx", "rate-kernel-1", []string{"node-1", "node-2", "node-3"}, unsigned))
	c.restart(t, c.byID("node-1")[0])
	results, tx := signEVM("rate-evm-1", 0, []string{"node-1", "node-2", "node-3"})
	for _, r := range decodeOK[SignResponse](t, "an EVM sign within the rate after the signer restarted", results) {
		sig, _ := hex.DecodeString(r.Signature)
		signed, err := tx.WithSignature(txSigner, sig)
		if err != nil {
			t.Fatal(err)
		}
		if from, err := types.Sender(txSigner, signed); err != nil || from != address {
			t.Fatalf("%s: recovered %s (%v)", r.NodeID, from.Hex(), err)
		}
	}

	results, _ = signEVM("rate-evm-2", 1, []string{"node-1", "node-4", "node-5"})
	expectPolicyCode(t, "an EVM sign after one kernel and one EVM sign", results, policy.CodeRateLimited)
	unsigned, _ = programTransfer(t, pub, 2, 1_000_000)
	expectPolicyCode(t, "a kernel sign after one kernel and one EVM sign", signKernel(t, c, "rate-lx", "rate-kernel-2", []string{"node-2", "node-3", "node-4"}, unsigned), policy.CodeRateLimited)
}

func TestKernelDailyCapHoldsAfterRestart(t *testing.T) {
	c := newTestClusterWith(t, 5, true, testPolicy(), nil)
	useKernelPolicy(t, c, "8000000")
	account := common.HexToAddress("0x6666666666666666666666666666666666666666").Hex()
	pub := importKernelKey(t, c, "daily-lx", "attestor kernel daily cap ed25519 key", account)

	unsigned, preimage := programTransfer(t, pub, 1, 5_000_000)
	verifyKernel(t, "a kernel spend within the daily cap", pub, preimage, signKernel(t, c, "daily-lx", "daily-1", []string{"node-1", "node-2", "node-3"}, unsigned))

	restarted := c.restart(t, c.byID("node-2")[0])
	if spent, _ := nodeLedger(t, restarted, account); spent.Cmp(big.NewInt(5_000_000)) != 0 {
		t.Fatalf("the restarted signer holds a kernel spend of %s", spent)
	}
	unsigned, _ = programTransfer(t, pub, 2, 5_000_000)
	expectPolicyCode(t, "a kernel spend over the daily cap after a signer restarted", signKernel(t, c, "daily-lx", "daily-2", []string{"node-2", "node-4", "node-5"}, unsigned), policy.CodeDailyCap)
	unsigned, preimage = programTransfer(t, pub, 3, 3_000_000)
	verifyKernel(t, "a kernel spend that fills the daily cap after a restart", pub, preimage, signKernel(t, c, "daily-lx", "daily-3", []string{"node-1", "node-2", "node-5"}, unsigned))
	unsigned, _ = programTransfer(t, pub, 4, 1)
	expectPolicyCode(t, "a kernel spend past a full daily cap", signKernel(t, c, "daily-lx", "daily-4", []string{"node-3", "node-4", "node-5"}, unsigned), policy.CodeDailyCap)
}

func TestKernelSpendAnnouncedByOneSignerCountsOnceEverywhere(t *testing.T) {
	c := newTestClusterWith(t, 5, true, testPolicy(), nil)
	useKernelPolicy(t, c, "100000000")
	account := common.HexToAddress("0x7777777777777777777777777777777777777777").Hex()
	pub := importKernelKey(t, c, "once-lx", "attestor kernel count-once ed25519 key", account)

	unsigned, preimage := programTransfer(t, pub, 1, 5_000_000)
	verifyKernel(t, "a kernel spend announced by its signers", pub, preimage, signKernel(t, c, "once-lx", "once-1", []string{"node-1", "node-3", "node-5"}, unsigned))
	for _, n := range c.nodes {
		spent, requests := nodeLedger(t, n, account)
		if spent.Cmp(big.NewInt(5_000_000)) != 0 || requests != 1 {
			t.Fatalf("%s: one kernel request counted as %s spent over %d requests", n.id, spent, requests)
		}
	}
}

func TestKernelSpendsAcrossSignerSetsCannotExceedACap(t *testing.T) {
	c := newTestClusterWith(t, 5, true, testPolicy(), nil)
	useKernelPolicy(t, c, "15000000")
	account := common.HexToAddress("0x8888888888888888888888888888888888888888").Hex()
	pub := importKernelKey(t, c, "spread-lx", "attestor kernel spread ed25519 key", account)

	for i, signers := range [][]string{{"node-1", "node-2", "node-3"}, {"node-3", "node-4", "node-5"}, {"node-1", "node-4", "node-5"}} {
		unsigned, preimage := programTransfer(t, pub, uint64(i+1), 5_000_000)
		verifyKernel(t, "a kernel spend within the cap", pub, preimage, signKernel(t, c, "spread-lx", fmt.Sprintf("spread-%d", i), signers, unsigned))
	}
	for _, n := range c.nodes {
		if spent, _ := nodeLedger(t, n, account); spent.Cmp(big.NewInt(15_000_000)) != 0 {
			t.Fatalf("%s: three kernel spends counted as %s", n.id, spent)
		}
	}
	unsigned, _ := programTransfer(t, pub, 4, 1)
	expectPolicyCode(t, "a fourth kernel spend over a signer set that never signed together", signKernel(t, c, "spread-lx", "spread-3", []string{"node-2", "node-4", "node-5"}, unsigned), policy.CodeDailyCap)
}
