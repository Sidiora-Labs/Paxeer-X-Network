package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
