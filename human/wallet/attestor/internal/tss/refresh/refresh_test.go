package refresh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/tss/dkg"
	"github.com/getamis/alice/crypto/tss/ecdsa/cggmp/sign"
	"github.com/getamis/alice/crypto/tss/eddsa/frost/signer"
	"github.com/getamis/alice/types"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

const (
	secpKeyHex = "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"
	edSeedHex  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

	ecdsaDeadline = 9 * time.Minute
	eddsaDeadline = time.Minute
)

var participantIDs = []string{"p1", "p2", "p3", "p4", "p5"}

type envelope struct {
	from string
	msg  types.Message
}

type mailbox struct {
	mu     sync.Mutex
	cond   *sync.Cond
	queue  []envelope
	recv   Receiver
	closed bool
}

type hub struct {
	boxes map[string]*mailbox
	wg    sync.WaitGroup
}

func newHub(ids ...string) *hub {
	h := &hub{boxes: make(map[string]*mailbox, len(ids))}
	for _, id := range ids {
		b := &mailbox{}
		b.cond = sync.NewCond(&b.mu)
		h.boxes[id] = b
		h.wg.Add(1)
		go h.pump(b)
	}
	return h
}

func (h *hub) pump(b *mailbox) {
	defer h.wg.Done()
	for {
		b.mu.Lock()
		for !b.closed && (b.recv == nil || len(b.queue) == 0) {
			b.cond.Wait()
		}
		if b.closed {
			b.mu.Unlock()
			return
		}
		e := b.queue[0]
		b.queue = b.queue[1:]
		r := b.recv
		b.mu.Unlock()
		_ = r.AddMessage(e.from, e.msg)
	}
}

func (h *hub) deliver(from, to string, msg types.Message) {
	b, ok := h.boxes[to]
	if !ok {
		return
	}
	b.mu.Lock()
	b.queue = append(b.queue, envelope{from: from, msg: msg})
	b.cond.Signal()
	b.mu.Unlock()
}

func (h *hub) close() {
	for _, b := range h.boxes {
		b.mu.Lock()
		b.closed = true
		b.cond.Broadcast()
		b.mu.Unlock()
	}
	h.wg.Wait()
}

func (h *hub) node(self string, peers []string) *memNet {
	return &memNet{hub: h, self: self, peers: append([]string(nil), peers...)}
}

type memNet struct {
	hub   *hub
	self  string
	peers []string
}

func (n *memNet) NumPeers() uint32 { return uint32(len(n.peers)) }

func (n *memNet) PeerIDs() []string { return append([]string(nil), n.peers...) }

func (n *memNet) SelfID() string { return n.self }

func (n *memNet) MustSend(id string, msg interface{}) {
	n.hub.deliver(n.self, id, msg.(types.Message))
}

func (n *memNet) Bind(r Receiver) {
	b := n.hub.boxes[n.self]
	b.mu.Lock()
	b.recv = r
	b.cond.Broadcast()
	b.mu.Unlock()
}

func others(ids []string, self string) []string {
	out := make([]string, 0, len(ids)-1)
	for _, id := range ids {
		if id != self {
			out = append(out, id)
		}
	}
	return out
}

func sessionID(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func runAll[T any](ids []string, fn func(id string) (T, error)) (map[string]T, map[string]error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make(map[string]T, len(ids))
	errs := make(map[string]error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r, err := fn(id)
			mu.Lock()
			defer mu.Unlock()
			results[id] = r
			errs[id] = err
		}(id)
	}
	wg.Wait()
	return results, errs
}

func requireNoErrors(t *testing.T, errs map[string]error) {
	t.Helper()
	for id, err := range errs {
		if err != nil {
			t.Fatalf("participant %s: %v", id, err)
		}
	}
}

func sortedKeys[T any](m map[string]T) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func bundlesByID(bundles []dealer.ShareBundle) map[string]dealer.ShareBundle {
	out := make(map[string]dealer.ShareBundle, len(bundles))
	for _, b := range bundles {
		out[b.ParticipantID] = b
	}
	return out
}

func refreshECDSAAll(t *testing.T, bundles map[string]dealer.ShareBundle) (map[string]*ECDSAShare, map[string]error) {
	t.Helper()
	ids := sortedKeys(bundles)
	ctx, cancel := context.WithTimeout(context.Background(), ecdsaDeadline)
	defer cancel()
	h := newHub(ids...)
	defer h.close()
	ssid := sessionID(t)
	return runAll(ids, func(id string) (*ECDSAShare, error) {
		return RefreshECDSA(ctx, h.node(id, others(ids, id)), bundles[id], ssid)
	})
}

func refreshEdDSAAll(t *testing.T, bundles map[string]dealer.ShareBundle) (map[string]dealer.ShareBundle, map[string]error) {
	t.Helper()
	ids := sortedKeys(bundles)
	ctx, cancel := context.WithTimeout(context.Background(), eddsaDeadline)
	defer cancel()
	h := newHub(ids...)
	defer h.close()
	ssid := sessionID(t)
	return runAll(ids, func(id string) (dealer.ShareBundle, error) {
		return RefreshEdDSA(ctx, h.node(id, others(ids, id)), bundles[id], ssid)
	})
}

func addShareAll(t *testing.T, curve dealer.Curve, pub *pt.ECPoint, contributors map[string]dealer.ShareBundle, newID string, deadline time.Duration) map[string]dealer.ShareBundle {
	t.Helper()
	quorum := sortedKeys(contributors)
	all := append(append([]string(nil), quorum...), newID)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	h := newHub(all...)
	defer h.close()
	results, errs := runAll(all, func(id string) (dealer.ShareBundle, error) {
		req := AddShareRequest{Curve: curve, PublicKey: pub, Threshold: dealer.Threshold, NewParticipantID: newID}
		if id == newID {
			return AddShare(ctx, h.node(id, quorum), req)
		}
		existing := contributors[id]
		req.Existing = &existing
		return AddShare(ctx, h.node(id, others(quorum, id)), req)
	})
	requireNoErrors(t, errs)
	return results
}

func subsetBks(all map[string]*birkhoffinterpolation.BkParameter, ids []string) map[string]*birkhoffinterpolation.BkParameter {
	out := make(map[string]*birkhoffinterpolation.BkParameter, len(ids))
	for _, id := range ids {
		out[id] = all[id]
	}
	return out
}

func signECDSA(t *testing.T, shares map[string]*ECDSAShare, signers []string, digest []byte, want common.Address) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ecdsaDeadline)
	defer cancel()
	h := newHub(signers...)
	defer h.close()
	ssid := sessionID(t)
	type started struct {
		s *sign.Sign
		l *stateListener
	}
	sessions := make(map[string]started, len(signers))
	for _, id := range signers {
		sh := shares[id]
		node := h.node(id, others(signers, id))
		l := newStateListener()
		s, err := sign.NewSign(dealer.Threshold, ssid, sh.Share, sh.PublicKey, sh.PartialPublicKeys, sh.PaillierKey, sh.Pedersen, subsetBks(sh.Bks, signers), digest, node, l)
		if err != nil {
			t.Fatalf("signer %s: %v", id, err)
		}
		node.Bind(s)
		sessions[id] = started{s: s, l: l}
	}
	results, errs := runAll(signers, func(id string) (*sign.Result, error) {
		st := sessions[id]
		if err := runMain(ctx, st.s, st.l, st.s.Start); err != nil {
			return nil, err
		}
		return st.s.GetResult()
	})
	requireNoErrors(t, errs)
	first := results[signers[0]]
	for _, id := range signers[1:] {
		if results[id].R.Cmp(first.R) != 0 || results[id].S.Cmp(first.S) != 0 {
			t.Fatalf("signer %s produced a different signature", id)
		}
	}
	n := crypto.S256().Params().N
	s := new(big.Int).Set(first.S)
	if s.Cmp(new(big.Int).Rsh(n, 1)) > 0 {
		s.Sub(n, s)
	}
	sig := make([]byte, 65)
	first.R.FillBytes(sig[:32])
	s.FillBytes(sig[32:64])
	for v := byte(0); v < 2; v++ {
		sig[64] = v
		pub, err := crypto.SigToPub(digest, sig)
		if err != nil {
			continue
		}
		if crypto.PubkeyToAddress(*pub) == want {
			recovered, err := crypto.Ecrecover(digest, sig)
			if err != nil {
				t.Fatal(err)
			}
			if !crypto.VerifySignature(recovered, digest, sig[:64]) {
				t.Fatal("recovered key does not verify the low-s signature")
			}
			return sig
		}
	}
	t.Fatalf("no recovery id recovers %s", want.Hex())
	return nil
}

func signEdDSA(t *testing.T, bundles map[string]dealer.ShareBundle, signers []string, msg []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), eddsaDeadline)
	defer cancel()
	h := newHub(signers...)
	defer h.close()
	type started struct {
		s *signer.Signer
		l *stateListener
	}
	sessions := make(map[string]started, len(signers))
	for _, id := range signers {
		b := bundles[id]
		ys := make(map[string]*pt.ECPoint, len(signers))
		for _, sid := range signers {
			ys[sid] = b.PartialPublicKeys[sid]
		}
		node := h.node(id, others(signers, id))
		l := newStateListener()
		s, err := signer.NewSigner(b.PublicKey, node, dealer.Threshold, b.Share, &dkg.Result{
			PublicKey: b.PublicKey,
			Share:     b.Share,
			Bks:       subsetBks(b.Bks, signers),
			Ys:        ys,
		}, msg, l)
		if err != nil {
			t.Fatalf("signer %s: %v", id, err)
		}
		node.Bind(s)
		sessions[id] = started{s: s, l: l}
	}
	results, errs := runAll(signers, func(id string) (*signer.Result, error) {
		st := sessions[id]
		if err := runMain(ctx, st.s, st.l, st.s.Start); err != nil {
			return nil, err
		}
		return st.s.GetResult()
	})
	requireNoErrors(t, errs)
	first := results[signers[0]]
	for _, id := range signers[1:] {
		if !results[id].R.Equal(first.R) || results[id].S.Cmp(first.S) != 0 {
			t.Fatalf("signer %s produced a different signature", id)
		}
	}
	r, err := dealer.EncodeEd25519(first.R)
	if err != nil {
		t.Fatal(err)
	}
	be := first.S.FillBytes(make([]byte, 32))
	sig := make([]byte, 64)
	copy(sig, r)
	for i := 0; i < 32; i++ {
		sig[32+i] = be[31-i]
	}
	return sig
}

func splitSecp(t *testing.T) (map[string]dealer.ShareBundle, *pt.ECPoint, common.Address) {
	t.Helper()
	key, err := crypto.HexToECDSA(secpKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	bundles, pub, err := dealer.Split(dealer.Secp256k1, new(big.Int).Set(key.D), participantIDs)
	if err != nil {
		t.Fatal(err)
	}
	return bundlesByID(bundles), pub, crypto.PubkeyToAddress(key.PublicKey)
}

func splitEd(t *testing.T) (map[string]dealer.ShareBundle, *pt.ECPoint, ed25519.PublicKey) {
	t.Helper()
	seed, err := hex.DecodeString(edSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := dealer.Ed25519ScalarFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	bundles, pub, err := dealer.Split(dealer.Ed25519, scalar, participantIDs)
	if err != nil {
		t.Fatal(err)
	}
	return bundlesByID(bundles), pub, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}

func assertRefreshed(t *testing.T, before map[string]dealer.ShareBundle, after map[string]dealer.ShareBundle, pub *pt.ECPoint) {
	t.Helper()
	ids := sortedKeys(before)
	if len(after) != len(ids) {
		t.Fatalf("got %d refreshed participants want %d", len(after), len(ids))
	}
	ref := after[ids[0]]
	for _, id := range ids {
		a := after[id]
		if !a.PublicKey.Equal(pub) {
			t.Fatalf("participant %s: public key changed by refresh", id)
		}
		if a.Share.Cmp(before[id].Share) == 0 {
			t.Fatalf("participant %s: share unchanged by refresh", id)
		}
		if err := a.Validate(); err != nil {
			t.Fatalf("participant %s: %v", id, err)
		}
		for _, other := range ids {
			if !a.PartialPublicKeys[other].Equal(ref.PartialPublicKeys[other]) {
				t.Fatalf("participants %s and %s disagree on the partial public key of %s", id, ids[0], other)
			}
			if a.PartialPublicKeys[other].Equal(before[id].PartialPublicKeys[other]) {
				t.Fatalf("participant %s: partial public key of %s unchanged by refresh", id, other)
			}
		}
	}
}

func ecdsaBundles(shares map[string]*ECDSAShare) map[string]dealer.ShareBundle {
	out := make(map[string]dealer.ShareBundle, len(shares))
	for id, s := range shares {
		out[id] = s.Bundle()
	}
	return out
}

func TestECDSAImportRefreshSignAndReplace(t *testing.T) {
	bundles, pub, address := splitSecp(t)
	digest := crypto.Keccak256([]byte("paxeer x wallet dealer import check"))

	refreshed, errs := refreshECDSAAll(t, bundles)
	requireNoErrors(t, errs)
	assertRefreshed(t, bundles, ecdsaBundles(refreshed), pub)
	for id, s := range refreshed {
		if s.PaillierKey == nil || len(s.Pedersen) != len(participantIDs) {
			t.Fatalf("participant %s: refresh produced no auxiliary material", id)
		}
	}
	signECDSA(t, refreshed, []string{"p1", "p3", "p5"}, digest, address)

	contributors := make(map[string]dealer.ShareBundle)
	for id, s := range refreshed {
		if id != "p2" {
			contributors[id] = s.Bundle()
		}
	}
	enlarged := addShareAll(t, dealer.Secp256k1, pub, contributors, "p6", ecdsaDeadline)
	for id, b := range enlarged {
		if _, removed := b.Bks["p2"]; removed {
			t.Fatalf("participant %s still lists the removed participant", id)
		}
		if len(b.Bks) != len(participantIDs) {
			t.Fatalf("participant %s: enlarged set has %d members", id, len(b.Bks))
		}
		if !b.PartialPublicKeys["p6"].Equal(enlarged["p6"].PartialPublicKeys["p6"]) {
			t.Fatalf("participant %s disagrees on the replacement's partial public key", id)
		}
	}
	replaced, errs := refreshECDSAAll(t, enlarged)
	requireNoErrors(t, errs)
	assertRefreshed(t, enlarged, ecdsaBundles(replaced), pub)
	signECDSA(t, replaced, []string{"p6", "p1", "p4"}, digest, address)
}

func TestECDSAMismatchedShareRefusedByEveryParticipant(t *testing.T) {
	bundles, _, _ := splitSecp(t)
	tampered := bundles["p3"].Clone()
	tampered.Share.Add(tampered.Share, big.NewInt(1))
	bundles["p3"] = tampered

	_, errs := refreshECDSAAll(t, bundles)
	for _, id := range participantIDs {
		err := errs[id]
		if err == nil {
			t.Fatalf("participant %s accepted a mismatched share", id)
		}
		want := ErrPeerShareRefused
		if id == "p3" {
			want = ErrShareMismatch
		}
		if !errors.Is(err, want) {
			t.Fatalf("participant %s: got %v want %v", id, err, want)
		}
	}
}

func TestEdDSAImportRefreshSignAndReplace(t *testing.T) {
	bundles, pub, stdPub := splitEd(t)
	encoded, err := dealer.EncodeEd25519(pub)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.PublicKey(encoded).Equal(stdPub) {
		t.Fatal("dealer public key does not match the imported key")
	}
	msg := []byte("LX:PAXEER-BIND:v1 dealer import check")

	refreshed, errs := refreshEdDSAAll(t, bundles)
	requireNoErrors(t, errs)
	assertRefreshed(t, bundles, refreshed, pub)

	again, errs := refreshEdDSAAll(t, refreshed)
	requireNoErrors(t, errs)
	assertRefreshed(t, refreshed, again, pub)

	sig := signEdDSA(t, again, []string{"p2", "p4", "p5"}, msg)
	if !ed25519.Verify(stdPub, msg, sig) {
		t.Fatal("refreshed shares do not sign under the imported public key")
	}

	contributors := make(map[string]dealer.ShareBundle)
	for id, b := range again {
		if id != "p2" {
			contributors[id] = b
		}
	}
	enlarged := addShareAll(t, dealer.Ed25519, pub, contributors, "p6", eddsaDeadline)
	for id, b := range enlarged {
		if _, removed := b.Bks["p2"]; removed {
			t.Fatalf("participant %s still lists the removed participant", id)
		}
		if len(b.Bks) != len(participantIDs) {
			t.Fatalf("participant %s: enlarged set has %d members", id, len(b.Bks))
		}
	}
	replaced, errs := refreshEdDSAAll(t, enlarged)
	requireNoErrors(t, errs)
	assertRefreshed(t, enlarged, replaced, pub)

	sig = signEdDSA(t, replaced, []string{"p6", "p1", "p3"}, msg)
	if !ed25519.Verify(stdPub, msg, sig) {
		t.Fatal("quorum including the replacement does not sign under the imported public key")
	}
}

func TestEdDSAMismatchedShareRefusedByEveryParticipant(t *testing.T) {
	bundles, _, _ := splitEd(t)
	tampered := bundles["p4"].Clone()
	tampered.Share.Add(tampered.Share, big.NewInt(1))
	bundles["p4"] = tampered

	_, errs := refreshEdDSAAll(t, bundles)
	for _, id := range participantIDs {
		err := errs[id]
		if err == nil {
			t.Fatalf("participant %s accepted a mismatched share", id)
		}
		want := ErrPeerShareRefused
		if id == "p4" {
			want = ErrShareMismatch
		}
		if !errors.Is(err, want) {
			t.Fatalf("participant %s: got %v want %v", id, err, want)
		}
	}
}

func TestRefreshRefusesWrongCurveAndMembership(t *testing.T) {
	secp, _, _ := splitSecp(t)
	ed, _, _ := splitEd(t)
	h := newHub(participantIDs...)
	defer h.close()
	ctx, cancel := context.WithTimeout(context.Background(), eddsaDeadline)
	defer cancel()
	if _, err := RefreshECDSA(ctx, h.node("p1", others(participantIDs, "p1")), ed["p1"], sessionID(t)); !errors.Is(err, ErrCurve) {
		t.Fatalf("ecdsa refresh of an ed25519 bundle: %v", err)
	}
	if _, err := RefreshEdDSA(ctx, h.node("p1", others(participantIDs, "p1")), secp["p1"], sessionID(t)); !errors.Is(err, ErrCurve) {
		t.Fatalf("eddsa refresh of a secp256k1 bundle: %v", err)
	}
	if _, err := RefreshEdDSA(ctx, h.node("p2", others(participantIDs, "p2")), ed["p1"], sessionID(t)); !errors.Is(err, ErrSelf) {
		t.Fatalf("refresh under another participant's id: %v", err)
	}
	if _, err := RefreshEdDSA(ctx, h.node("p1", []string{"p2", "p3"}), ed["p1"], sessionID(t)); !errors.Is(err, ErrParticipants) {
		t.Fatalf("refresh with a partial peer set: %v", err)
	}
	pub := ed["p1"].PublicKey
	if _, err := AddShare(ctx, h.node("p1", []string{"p2"}), AddShareRequest{Curve: dealer.Ed25519, PublicKey: pub, Threshold: dealer.Threshold, NewParticipantID: "p1"}); !errors.Is(err, ErrQuorum) {
		t.Fatalf("add-share below quorum: %v", err)
	}
	existing := ed["p1"]
	if _, err := AddShare(ctx, h.node("p1", []string{"p2", "p3"}), AddShareRequest{Curve: dealer.Ed25519, PublicKey: pub, Threshold: dealer.Threshold, NewParticipantID: "p5", Existing: &existing}); !errors.Is(err, ErrAddShareRequest) {
		t.Fatalf("add-share naming an existing participant: %v", err)
	}
}
