package fanout

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
)

// TestStaleRoomCleanupKeepsNewMemberSubscribed pauses the last member's leave
// after its refcount reaches zero, completes a new join, then resumes the
// leave: the room's Redis channel must stay subscribed and deliver to the new
// member.
func TestStaleRoomCleanupKeepsNewMemberSubscribed(t *testing.T) {
	redisURL := "redis://:kindle_fixture_password@127.0.0.1:26379/0"
	pub, err := sharedredis.NewClient(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sharedredis.NewClient(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pub.Close(); _ = sub.Close() })

	ns := "fanout-race-" + uuid.NewString() + ":"
	h := New(Deps{ChannelNamespace: ns, Pub: pub, Sub: sub})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = h.Run(ctx) }()

	pool := fmt.Sprintf("0x%040x", 1)
	channel := ns + poolChannelPrefix + pool
	numSub := func(ch string) int64 {
		n, err := pub.PubSubNumSub(ctx, ch).Result()
		if err != nil {
			t.Fatal(err)
		}
		return n[ch]
	}
	waitNumSub := func(ch string, want int64) {
		deadline := time.Now().Add(5 * time.Second)
		for numSub(ch) != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s subscribers = %d, want %d", ch, numSub(ch), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	member := func(n int) *conn {
		c := newConn(h, nil, fmt.Sprintf("0x%040x", n))
		c.principal = Principal{Wallet: c.actor, SessionID: "fixture", ExpiresAt: time.Now().Add(time.Minute), Validate: func(context.Context) error { return nil }}
		return c
	}

	a, b := member(0xa), member(0xb)
	h.joinRoom(a, pool)
	waitNumSub(channel, 1)

	// Hold A's own lock so its leave stops after the refcount reached zero and
	// before its Redis cleanup runs.
	a.mu.Lock()
	left := make(chan struct{})
	go func() { h.leaveRoom(a, pool); close(left) }()
	for {
		h.mu.RLock()
		_, live := h.poolChanRefs[pool]
		h.mu.RUnlock()
		if !live {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !h.joinRoom(b, pool) {
		t.Fatal("new member not joined")
	}
	a.mu.Unlock()
	<-left

	// A later subscription on the same Redis connection is acknowledged only
	// after every earlier command on it, including A's cleanup, was applied.
	sentinel := ns + "sentinel"
	if err := h.ps.Subscribe(ctx, sentinel); err != nil {
		t.Fatal(err)
	}
	waitNumSub(sentinel, 1)
	if n := numSub(channel); n != 1 {
		t.Fatalf("room channel subscribers after stale cleanup = %d, want 1", n)
	}

	if err := pub.Publish(ctx, channel, `{"type":"pool_message"}`).Err(); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-b.send:
		if string(frame) != `{"type":"pool_message"}` {
			t.Fatalf("frame = %s", frame)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new member received no live room delivery")
	}
}
