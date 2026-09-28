package transport

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestPendingBufferBoundedPerPeer(t *testing.T) {
	c := newCluster(t, 3)
	receiver := c.nodes[0]
	perPeer := receiver.pendingSessionsPerPeer()
	if perPeer >= receiver.limits.MaxPendingSessions {
		t.Fatalf("per-peer share %d does not bound the %d pending sessions", perPeer, receiver.limits.MaxPendingSessions)
	}
	envelope := func(sender, session string, seq uint64) *Envelope {
		return &Envelope{Session: session, Protocol: testProtocol, Sender: sender, Seq: seq, Type: "attestor.test"}
	}
	for i := 0; i < perPeer; i++ {
		if err := receiver.Deliver("node-1", envelope("node-1", fmt.Sprintf("flood-%d", i), 1)); err != nil {
			t.Fatalf("pending session %d: %v", i, err)
		}
	}
	err := receiver.Deliver("node-1", envelope("node-1", "flood-over", 1))
	if !errors.Is(err, ErrPeerQuota) || statusFor(err) != http.StatusServiceUnavailable {
		t.Fatalf("pending session beyond the peer share: %v", err)
	}
	if err := receiver.Deliver("node-2", envelope("node-2", "honest", 1)); err != nil {
		t.Fatalf("an honest peer is locked out by a flooding peer: %v", err)
	}
	perMessages := receiver.pendingMessagesPerPeer()
	for seq := uint64(1); seq <= uint64(perMessages); seq++ {
		if err := receiver.Deliver("node-1", envelope("node-1", "honest", seq)); err != nil {
			t.Fatalf("pending message %d: %v", seq, err)
		}
	}
	if err := receiver.Deliver("node-1", envelope("node-1", "honest", uint64(perMessages)+1)); !errors.Is(err, ErrPeerQuota) {
		t.Fatalf("pending message beyond the peer share: %v", err)
	}
	if err := receiver.Deliver("node-2", envelope("node-2", "honest", 2)); err != nil {
		t.Fatalf("the session's other sender is locked out: %v", err)
	}
}

func TestStalledSessionNamesTheSilentPeer(t *testing.T) {
	c := newCluster(t, 3)
	s0, err := c.nodes[0].OpenKind("stall", c.ids, testProtocol, KindGeneric)
	if err != nil {
		t.Fatal(err)
	}
	defer s0.Close()
	attachReceiver(t, s0)
	s1, err := c.nodes[1].OpenKind("stall", c.ids, testProtocol, KindGeneric)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	if _, stalled := s0.Stalled(time.Minute); stalled {
		t.Fatal("a fresh session reports a stall")
	}
	e1, err := s1.envelope("node-0", peerMessage("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	e1.Seq = 1
	if err := c.nodes[0].Deliver("node-1", e1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	laggards, stalled := s0.Stalled(50 * time.Millisecond)
	if !stalled || len(laggards) != 1 || laggards[0] != "node-2" {
		t.Fatalf("stall report %v %v, want node-2", laggards, stalled)
	}
}
