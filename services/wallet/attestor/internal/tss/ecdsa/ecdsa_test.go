package ecdsa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
	alicetypes "github.com/getamis/alice/types"
)

type queuedMessage struct {
	from string
	msg  alicetypes.Message
}

type memHub struct {
	mu        sync.Mutex
	receivers map[string]Receiver
	queued    map[string][]queuedMessage
	stopped   map[string]bool
}

func newMemHub() *memHub {
	return &memHub{receivers: make(map[string]Receiver), queued: make(map[string][]queuedMessage), stopped: make(map[string]bool)}
}

func (h *memHub) stop(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped[id] = true
}

func (h *memHub) deliver(from, to string, msg interface{}) {
	m, ok := msg.(alicetypes.Message)
	if !ok {
		return
	}
	h.mu.Lock()
	if h.stopped[from] || h.stopped[to] {
		h.mu.Unlock()
		return
	}
	r, bound := h.receivers[to]
	if !bound {
		h.queued[to] = append(h.queued[to], queuedMessage{from: from, msg: m})
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	go func() {
		_ = r.AddMessage(from, m)
	}()
}

type memPeer struct {
	hub   *memHub
	id    string
	peers []string
}

func (h *memHub) peer(id string, all []string) *memPeer {
	peers := make([]string, 0, len(all)-1)
	for _, other := range all {
		if other != id {
			peers = append(peers, other)
		}
	}
	return &memPeer{hub: h, id: id, peers: peers}
}

func (p *memPeer) NumPeers() uint32 { return uint32(len(p.peers)) }

func (p *memPeer) PeerIDs() []string { return p.peers }

func (p *memPeer) SelfID() string { return p.id }

func (p *memPeer) MustSend(id string, msg interface{}) { p.hub.deliver(p.id, id, msg) }

func (p *memPeer) Bind(r Receiver) {
	p.hub.mu.Lock()
	p.hub.receivers[p.id] = r
	queued := p.hub.queued[p.id]
	delete(p.hub.queued, p.id)
	p.hub.mu.Unlock()
	for _, q := range queued {
		go func(q queuedMessage) {
			_ = r.AddMessage(q.from, q.msg)
		}(q)
	}
}

var (
	participantIDs = []string{"attestor-1", "attestor-2", "attestor-3", "attestor-4", "attestor-5"}
	keygenOnce     sync.Once
	keygenShares   map[string]*KeyShare
	keygenErr      error
)

func fixtureShares(t *testing.T) map[string]*KeyShare {
	t.Helper()
	keygenOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		hub := newMemHub()
		type out struct {
			id    string
			share *KeyShare
			err   error
		}
		results := make(chan out, len(participantIDs))
		for _, id := range participantIDs {
			go func(id string) {
				share, err := Keygen(ctx, hub.peer(id, participantIDs), []byte("keygen-session"))
				results <- out{id: id, share: share, err: err}
			}(id)
		}
		keygenShares = make(map[string]*KeyShare, len(participantIDs))
		for range participantIDs {
			o := <-results
			if o.err != nil && keygenErr == nil {
				keygenErr = fmt.Errorf("%s: %w", o.id, o.err)
			}
			keygenShares[o.id] = o.share
		}
	})
	if keygenErr != nil {
		t.Fatalf("keygen: %v", keygenErr)
	}
	return keygenShares
}

type signOutcome struct {
	id  string
	sig EthereumSignature
	err error
}

func signWith(ctx context.Context, shares map[string]*KeyShare, signers []string, running []string, stopped []string, digest []byte) map[string]signOutcome {
	hub := newMemHub()
	for _, id := range stopped {
		hub.stop(id)
	}
	results := make(chan signOutcome, len(running))
	for _, id := range running {
		go func(id string) {
			sig, err := Sign(ctx, shares[id], hub.peer(id, signers), signers, digest)
			results <- signOutcome{id: id, sig: sig, err: err}
		}(id)
	}
	out := make(map[string]signOutcome, len(running))
	for range running {
		o := <-results
		out[o.id] = o
	}
	return out
}

func TestKeygenFiveParticipants(t *testing.T) {
	shares := fixtureShares(t)
	if len(shares) != Participants {
		t.Fatalf("got %d shares, want %d", len(shares), Participants)
	}
	first := shares[participantIDs[0]]
	wantAddr, err := first.Address()
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	seenShares := make(map[string]bool, len(shares))
	for _, id := range participantIDs {
		share := shares[id]
		if share.SelfID != id {
			t.Fatalf("share of %s names %s", id, share.SelfID)
		}
		if share.Threshold != Threshold {
			t.Fatalf("threshold %d, want %d", share.Threshold, Threshold)
		}
		if !share.PublicKey.Equal(first.PublicKey) {
			t.Fatalf("%s holds a different public key", id)
		}
		addr, err := share.Address()
		if err != nil || addr != wantAddr {
			t.Fatalf("%s address %s err %v, want %s", id, addr, err, wantAddr)
		}
		if got := share.ParticipantIDs(); fmt.Sprint(got) != fmt.Sprint(participantIDs) {
			t.Fatalf("%s participants %v", id, got)
		}
		if string(share.SSID) != string(first.SSID) {
			t.Fatalf("%s ssid differs", id)
		}
		for _, other := range participantIDs {
			if !share.PartialPublicKeys[other].Equal(shares[other].PartialPublicKeys[other]) {
				t.Fatalf("%s disagrees on the partial public key of %s", id, other)
			}
		}
		key := share.Share.Text(16)
		if seenShares[key] {
			t.Fatalf("duplicate share value")
		}
		seenShares[key] = true
		if err := share.Validate(); err != nil {
			t.Fatalf("%s validate: %v", id, err)
		}
	}
}

func TestKeyShareRoundTrip(t *testing.T) {
	shares := fixtureShares(t)
	original := shares[participantIDs[2]]
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, err := LoadKeyShare(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.SelfID != original.SelfID || loaded.Threshold != original.Threshold {
		t.Fatalf("identity fields differ")
	}
	if loaded.Share.Cmp(original.Share) != 0 || !loaded.PublicKey.Equal(original.PublicKey) {
		t.Fatalf("share or public key differ")
	}
	if string(loaded.SSID) != string(original.SSID) || string(loaded.Rid) != string(original.Rid) {
		t.Fatalf("ssid or rid differ")
	}
	lp, lq := loaded.PaillierKey.GetPQ()
	op, oq := original.PaillierKey.GetPQ()
	if lp.Cmp(op) != 0 || lq.Cmp(oq) != 0 {
		t.Fatalf("paillier key differs")
	}
	for _, id := range participantIDs {
		if !loaded.PartialPublicKeys[id].Equal(original.PartialPublicKeys[id]) {
			t.Fatalf("partial public key of %s differs", id)
		}
		if loaded.Bks[id].GetX().Cmp(original.Bks[id].GetX()) != 0 || loaded.Bks[id].GetRank() != original.Bks[id].GetRank() {
			t.Fatalf("birkhoff parameter of %s differs", id)
		}
		lped, oped := loaded.Pedersen[id], original.Pedersen[id]
		if lped.GetN().Cmp(oped.GetN()) != 0 || lped.GetS().Cmp(oped.GetS()) != 0 || lped.GetT().Cmp(oped.GetT()) != 0 {
			t.Fatalf("pedersen parameters of %s differ", id)
		}
	}
	again, err := loaded.Marshal()
	if err != nil {
		t.Fatalf("marshal loaded: %v", err)
	}
	if string(again) != string(data) {
		t.Fatalf("encoding is not stable")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw["version"] = json.RawMessage("2")
	wrongVersion, _ := json.Marshal(raw)
	if _, err := LoadKeyShare(wrongVersion); !errors.Is(err, ErrUnsupportedShareVersion) {
		t.Fatalf("version 2 accepted: %v", err)
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	other := shares[participantIDs[0]].Share
	raw["share"], _ = json.Marshal(other.Text(16))
	tampered, _ := json.Marshal(raw)
	if _, err := LoadKeyShare(tampered); !errors.Is(err, ErrInvalidKeyShare) {
		t.Fatalf("share of another participant accepted: %v", err)
	}
}

func TestSignEIP1559Transaction(t *testing.T) {
	shares := fixtureShares(t)
	address, err := shares[participantIDs[0]].Address()
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	chainID := big.NewInt(125)
	to := common.HexToAddress("0x21f7000000000000000000000000000000000125")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     7,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(30_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1_000_000_000_000_000),
	})
	signer := types.NewLondonSigner(chainID)
	digest := signer.Hash(tx)

	signers := []string{participantIDs[0], participantIDs[2], participantIDs[4]}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	outcomes := signWith(ctx, shares, signers, signers, nil, digest.Bytes())

	var sig EthereumSignature
	for i, id := range signers {
		o := outcomes[id]
		if o.err != nil {
			t.Fatalf("%s sign: %v", id, o.err)
		}
		if i == 0 {
			sig = o.sig
			continue
		}
		if o.sig != sig {
			t.Fatalf("%s produced a different signature", id)
		}
	}
	if sig.V > 1 {
		t.Fatalf("recovery id %d", sig.V)
	}
	s := new(big.Int).SetBytes(sig.S[:])
	halfN := new(big.Int).Rsh(gethcrypto.S256().Params().N, 1)
	if s.Sign() <= 0 || s.Cmp(halfN) > 0 {
		t.Fatalf("s is not low: %s", s)
	}
	pub, err := gethcrypto.SigToPub(digest.Bytes(), sig.Bytes())
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if gethcrypto.PubkeyToAddress(*pub) != address {
		t.Fatalf("recovered %s, want %s", gethcrypto.PubkeyToAddress(*pub), address)
	}
	signed, err := tx.WithSignature(signer, sig.Bytes())
	if err != nil {
		t.Fatalf("attach signature: %v", err)
	}
	sender, err := types.Sender(signer, signed)
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	if sender != address {
		t.Fatalf("sender %s, want %s", sender, address)
	}
}

func TestSignBelowThresholdFails(t *testing.T) {
	shares := fixtureShares(t)
	digest := gethcrypto.Keccak256([]byte("below threshold"))
	signers := []string{participantIDs[1], participantIDs[3], participantIDs[4]}
	running := []string{participantIDs[1], participantIDs[3]}
	stopped := []string{participantIDs[4]}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	started := time.Now()
	outcomes := signWith(ctx, shares, signers, running, stopped, digest)
	if elapsed := time.Since(started); elapsed > 2*time.Minute {
		t.Fatalf("session did not end promptly: %s", elapsed)
	}
	for _, id := range running {
		o := outcomes[id]
		if o.err == nil {
			t.Fatalf("%s returned a signature without a quorum", id)
		}
		if !errors.Is(o.err, ErrSessionFailed) || !errors.Is(o.err, context.DeadlineExceeded) {
			t.Fatalf("%s error %v, want a failed session at the deadline", id, o.err)
		}
	}

	pair := []string{participantIDs[1], participantIDs[3]}
	_, err := Sign(context.Background(), shares[pair[0]], newMemHub().peer(pair[0], pair), pair, digest)
	if !errors.Is(err, ErrBelowThreshold) {
		t.Fatalf("two signers accepted: %v", err)
	}
}

func TestSignRejectsMalformedRequests(t *testing.T) {
	shares := fixtureShares(t)
	signers := []string{participantIDs[0], participantIDs[1], participantIDs[2]}
	share := shares[signers[0]]
	hub := newMemHub()
	if _, err := Sign(context.Background(), share, hub.peer(signers[0], signers), signers, make([]byte, 31)); !errors.Is(err, ErrInvalidDigest) {
		t.Fatalf("short digest accepted: %v", err)
	}
	unsorted := []string{signers[2], signers[0], signers[1]}
	if _, err := Sign(context.Background(), share, hub.peer(signers[0], unsorted), unsorted, make([]byte, 32)); !errors.Is(err, ErrInvalidParticipants) {
		t.Fatalf("unsorted participants accepted: %v", err)
	}
	withoutSelf := []string{signers[1], signers[2], participantIDs[3]}
	if _, err := Sign(context.Background(), share, hub.peer(signers[0], signers), withoutSelf, make([]byte, 32)); !errors.Is(err, ErrInvalidParticipants) {
		t.Fatalf("participants without self accepted: %v", err)
	}
	if _, err := Sign(context.Background(), share, hub.peer(signers[1], signers), signers, make([]byte, 32)); !errors.Is(err, ErrInvalidNetwork) {
		t.Fatalf("network of another participant accepted: %v", err)
	}
}

func TestEthereumSignatureNormalisesHighS(t *testing.T) {
	key, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	address := gethcrypto.PubkeyToAddress(key.PublicKey)
	digest := gethcrypto.Keccak256([]byte("normalise high s"))
	ref, err := gethcrypto.Sign(digest, key)
	if err != nil {
		t.Fatalf("reference sign: %v", err)
	}
	r := new(big.Int).SetBytes(ref[:32])
	lowS := new(big.Int).SetBytes(ref[32:64])
	n := gethcrypto.S256().Params().N
	highS := new(big.Int).Sub(n, lowS)

	for _, s := range []*big.Int{lowS, highS} {
		sig, err := ethereumSignature(address, digest, r, s)
		if err != nil {
			t.Fatalf("normalise: %v", err)
		}
		if new(big.Int).SetBytes(sig.S[:]).Cmp(lowS) != 0 {
			t.Fatalf("s not normalised to the low value")
		}
		if sig.V != ref[64] {
			t.Fatalf("recovery id %d, want %d", sig.V, ref[64])
		}
		if string(sig.Bytes()) != string(ref) {
			t.Fatalf("signature differs from the reference signature")
		}
	}

	stranger, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if _, err := ethereumSignature(gethcrypto.PubkeyToAddress(stranger.PublicKey), digest, r, highS); !errors.Is(err, ErrSignatureDoesNotRecover) {
		t.Fatalf("signature for another address returned: %v", err)
	}
	if _, err := ethereumSignature(address, digest, r, new(big.Int)); !errors.Is(err, ErrSignatureOutOfRange) {
		t.Fatalf("zero s accepted: %v", err)
	}
	if _, err := ethereumSignature(address, digest, n, lowS); !errors.Is(err, ErrSignatureOutOfRange) {
		t.Fatalf("r equal to the group order accepted: %v", err)
	}
}

func TestKeyShareRejectsInconsistentPartialKeys(t *testing.T) {
	shares := fixtureShares(t)
	original := shares[participantIDs[1]]
	curve := elliptic.Secp256k1()
	partials := make(map[string]*pt.ECPoint, len(original.PartialPublicKeys))
	ids := make([]string, 0, len(original.PartialPublicKeys))
	for id, p := range original.PartialPublicKeys {
		partials[id] = p
		ids = append(ids, id)
	}
	sort.Strings(ids)
	victim := ids[len(ids)-1]
	if victim == original.SelfID {
		victim = ids[0]
	}
	partials[victim] = pt.ScalarBaseMult(curve, big.NewInt(12345))
	forged := *original
	forged.PartialPublicKeys = partials
	if err := forged.Validate(); !errors.Is(err, ErrInvalidKeyShare) {
		t.Fatalf("forged partial public key accepted: %v", err)
	}
	if _, err := forged.Marshal(); !errors.Is(err, ErrInvalidKeyShare) {
		t.Fatalf("forged share serialised: %v", err)
	}
}
