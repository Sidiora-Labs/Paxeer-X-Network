package eddsa

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getamis/alice/types"
	"github.com/sidiora-labs/paxeer-network/layerxproof/codec"
	"github.com/sidiora-labs/paxeer-network/layerxproof/verify"
)

type memRouter struct {
	mu      sync.Mutex
	mains   map[string]types.MessageMain
	pending map[string][]routedMessage
}

type routedMessage struct {
	from string
	msg  types.Message
}

func newMemRouter() *memRouter {
	return &memRouter{
		mains:   make(map[string]types.MessageMain),
		pending: make(map[string][]routedMessage),
	}
}

func (r *memRouter) deliver(from, to string, msg types.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	main, ok := r.mains[to]
	if !ok {
		r.pending[to] = append(r.pending[to], routedMessage{from: from, msg: msg})
		return
	}
	_ = main.AddMessage(from, msg)
}

func (r *memRouter) register(id string, main types.MessageMain) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mains[id] = main
	for _, queued := range r.pending[id] {
		_ = main.AddMessage(queued.from, queued.msg)
	}
	delete(r.pending, id)
}

type memPeerManager struct {
	router *memRouter
	self   string
	peers  []string
}

func newMemPeerManagers(ids []string) map[string]*memPeerManager {
	router := newMemRouter()
	managers := make(map[string]*memPeerManager, len(ids))
	for _, self := range ids {
		peers := make([]string, 0, len(ids)-1)
		for _, id := range ids {
			if id != self {
				peers = append(peers, id)
			}
		}
		managers[self] = &memPeerManager{router: router, self: self, peers: peers}
	}
	return managers
}

func (m *memPeerManager) NumPeers() uint32 {
	return uint32(len(m.peers))
}

func (m *memPeerManager) PeerIDs() []string {
	return m.peers
}

func (m *memPeerManager) SelfID() string {
	return m.self
}

func (m *memPeerManager) MustSend(id string, msg interface{}) {
	m.router.deliver(m.self, id, msg.(types.Message))
}

func (m *memPeerManager) Register(main types.MessageMain) {
	m.router.register(m.self, main)
}

var _ PeerManager = (*memPeerManager)(nil)

func participantIDs() []string {
	ids := make([]string, Parties)
	for i := range ids {
		ids[i] = fmt.Sprintf("attestor-%d", i)
	}
	return ids
}

func runKeygen(t *testing.T) map[string]*KeyShare {
	t.Helper()
	ids := participantIDs()
	managers := newMemPeerManagers(ids)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	shares := make(map[string]*KeyShare, len(ids))
	errs := make(map[string]error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			share, err := Keygen(ctx, managers[id])
			mu.Lock()
			defer mu.Unlock()
			shares[id] = share
			errs[id] = err
		}(id)
	}
	wg.Wait()
	for _, id := range ids {
		if errs[id] != nil {
			t.Fatalf("keygen %s: %v", id, errs[id])
		}
	}
	return shares
}

func runSign(t *testing.T, shares map[string]*KeyShare, participants []string, message []byte) [64]byte {
	t.Helper()
	managers := newMemPeerManagers(participants)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	signatures := make(map[string][64]byte, len(participants))
	errs := make(map[string]error, len(participants))
	for _, id := range participants {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			signature, err := Sign(ctx, shares[id], managers[id], participants, message)
			mu.Lock()
			defer mu.Unlock()
			signatures[id] = signature
			errs[id] = err
		}(id)
	}
	wg.Wait()
	for _, id := range participants {
		if errs[id] != nil {
			t.Fatalf("sign %s: %v", id, errs[id])
		}
	}
	first := signatures[participants[0]]
	for _, id := range participants[1:] {
		if signatures[id] != first {
			t.Fatalf("participant %s produced a different signature", id)
		}
	}
	return first
}

func bindingMessage(t *testing.T, chainID uint64, address []byte, nonce uint64) []byte {
	t.Helper()
	if len(address) != 20 {
		t.Fatalf("address length %d", len(address))
	}
	message := []byte("LX:PAXEER-BIND:v1")
	chain := make([]byte, 32)
	new(big.Int).SetUint64(chainID).FillBytes(chain)
	message = append(message, chain...)
	message = append(message, address...)
	message = binary.BigEndian.AppendUint64(message, nonce)
	return message
}

func TestKeygenSignAndVerify(t *testing.T) {
	shares := runKeygen(t)
	ids := participantIDs()

	publicKey := shares[ids[0]].PublicKeyBytes()
	for _, id := range ids {
		share := shares[id]
		if share.ID() != id {
			t.Fatalf("share id %q, want %q", share.ID(), id)
		}
		if share.Threshold() != Threshold {
			t.Fatalf("share threshold %d, want %d", share.Threshold(), Threshold)
		}
		if got := share.Participants(); !equalStrings(got, ids) {
			t.Fatalf("participants %v, want %v", got, ids)
		}
		if share.PublicKeyBytes() != publicKey {
			t.Fatalf("participant %s holds a different public key", id)
		}
	}
	if !verify.PublicKeyIsCanonical(publicKey) {
		t.Fatalf("public key %x is not canonical", publicKey)
	}

	address, err := hex.DecodeString("5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	if err != nil {
		t.Fatal(err)
	}
	binding := bindingMessage(t, 125, address, 7)
	if len(binding) != 77 {
		t.Fatalf("binding message length %d, want 77", len(binding))
	}
	bindSigners := []string{ids[0], ids[1], ids[2]}
	bindSignature := runSign(t, shares, bindSigners, binding)
	if !ed25519.Verify(ed25519.PublicKey(publicKey[:]), binding, bindSignature[:]) {
		t.Fatal("binding signature rejected by crypto/ed25519")
	}
	if err := verify.Ed25519(publicKey, bindSignature, binding); err != nil {
		t.Fatalf("binding signature rejected by the strict verifier: %v", err)
	}
	tampered := append([]byte(nil), binding...)
	tampered[len(tampered)-1] ^= 1
	if ed25519.Verify(ed25519.PublicKey(publicKey[:]), tampered, bindSignature[:]) {
		t.Fatal("binding signature verified over a different nonce")
	}
	if err := verify.Ed25519(publicKey, bindSignature, tampered); !errors.Is(err, verify.ErrBadSignature) {
		t.Fatalf("strict verifier accepted a different nonce: %v", err)
	}

	canonical := bytes.Repeat([]byte{0xa5}, 32)
	for i := range canonical {
		canonical[i] ^= byte(i)
	}
	preimage := append([]byte("LXP/v1/receipt"), 0)
	preimage = append(preimage, canonical...)
	digest := sha256.Sum256(preimage)
	domainDigest, err := codec.DomainHash(codec.DomainReceipt, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if digest != domainDigest {
		t.Fatal("receipt digest differs from the codec's receipt domain hash")
	}
	receiptSigners := []string{ids[0], ids[2], ids[4]}
	receiptSignature := runSign(t, shares, receiptSigners, digest[:])
	if !ed25519.Verify(ed25519.PublicKey(publicKey[:]), digest[:], receiptSignature[:]) {
		t.Fatal("receipt signature rejected by crypto/ed25519")
	}
	if err := verify.Ed25519Digest(publicKey, receiptSignature, digest); err != nil {
		t.Fatalf("receipt signature rejected by the strict verifier: %v", err)
	}
	if err := verify.Ed25519Domain(publicKey, receiptSignature, codec.DomainReceipt, canonical); err != nil {
		t.Fatalf("receipt signature rejected by the strict domain verifier: %v", err)
	}
}

func TestKeyShareEncodingRoundTrip(t *testing.T) {
	shares := runKeygen(t)
	ids := participantIDs()

	loaded := make(map[string]*KeyShare, len(ids))
	for _, id := range ids {
		encoded, err := shares[id].Marshal()
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		share, err := Load(encoded)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		reencoded, err := share.Marshal()
		if err != nil {
			t.Fatalf("remarshal %s: %v", id, err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatalf("encoding of %s does not round-trip", id)
		}
		if share.PublicKeyBytes() != shares[id].PublicKeyBytes() || share.ID() != id || share.Threshold() != Threshold {
			t.Fatalf("loaded share %s differs from the original", id)
		}
		formatted := fmt.Sprintf("%v %+v %#v %s", share, share, share, share)
		if strings.Contains(formatted, share.share.String()) || strings.Contains(formatted, hex.EncodeToString(encodeScalar(share.share))) || strings.Contains(formatted, share.share.Text(16)) {
			t.Fatalf("formatted share %s exposes the secret scalar", id)
		}
		loaded[id] = share
	}

	message := []byte("loaded shares sign")
	signers := []string{ids[1], ids[3], ids[4]}
	signature := runSign(t, loaded, signers, message)
	publicKey := loaded[ids[0]].PublicKeyBytes()
	if err := verify.Ed25519(publicKey, signature, message); err != nil {
		t.Fatalf("signature from loaded shares rejected: %v", err)
	}

	encoded, err := shares[ids[0]].Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var record keyShareV1
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}

	wrongVersion := record
	wrongVersion.Version = EncodingVersion + 1
	if _, err := Load(marshalRecord(t, wrongVersion)); !errors.Is(err, ErrVersion) {
		t.Fatalf("load of another version: %v", err)
	}

	tamperedShare := record
	tamperedShare.Share = append([]byte(nil), record.Share...)
	tamperedShare.Share[0] ^= 1
	if _, err := Load(marshalRecord(t, tamperedShare)); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("load of a tampered share: %v", err)
	}

	tamperedKey := record
	tamperedKey.PublicKey = append([]byte(nil), record.Parties[1].PublicShare...)
	if _, err := Load(marshalRecord(t, tamperedKey)); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("load with a substituted public key: %v", err)
	}

	lowered := record
	lowered.Threshold = Threshold - 1
	if _, err := Load(marshalRecord(t, lowered)); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("load with a lowered threshold: %v", err)
	}

	if _, err := Load(append(encoded, []byte(`{}`)...)); !errors.Is(err, ErrEncoding) {
		t.Fatalf("load with trailing data: %v", err)
	}
}

func TestSignRefusesBelowThresholdAndStoppedParticipant(t *testing.T) {
	shares := runKeygen(t)
	ids := participantIDs()

	pair := []string{ids[0], ids[1]}
	pairManagers := newMemPeerManagers(pair)
	if _, err := Sign(context.Background(), shares[ids[0]], pairManagers[ids[0]], pair, []byte("two of five")); !errors.Is(err, ErrBelowThreshold) {
		t.Fatalf("two-party session: %v", err)
	}

	unsorted := []string{ids[2], ids[0], ids[1]}
	unsortedManagers := newMemPeerManagers(unsorted)
	if _, err := Sign(context.Background(), shares[ids[0]], unsortedManagers[ids[0]], unsorted, []byte("unsorted")); !errors.Is(err, ErrParticipants) {
		t.Fatalf("unsorted participants: %v", err)
	}

	trio := []string{ids[0], ids[1], ids[2]}
	sort.Strings(trio)
	managers := newMemPeerManagers(trio)
	deadline := 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range trio[:2] {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = Sign(ctx, shares[id], managers[id], trio, []byte("one participant stops"))
		}(i, id)
	}
	wg.Wait()
	elapsed := time.Since(start)
	for i, err := range errs {
		if !errors.Is(err, ErrSessionFailed) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("participant %s: %v", trio[i], err)
		}
	}
	if elapsed > deadline+5*time.Second {
		t.Fatalf("session returned after %v, beyond its deadline", elapsed)
	}
}

func marshalRecord(t *testing.T, record keyShareV1) []byte {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
