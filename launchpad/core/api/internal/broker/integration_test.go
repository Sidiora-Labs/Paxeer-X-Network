package broker_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
)

// TestBroker_RunFansOutRealRedisPublish drives the full path: a real Redis
// PUBLISH on a channel -> the broker's subscriber loop -> a client Subscription.
func TestBroker_RunFansOutRealRedisPublish(t *testing.T) {
	url := internaltest.NewRedisURL(t)
	opt, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	subClient := goredis.NewClient(opt)
	t.Cleanup(func() { _ = subClient.Close() })
	pubClient := goredis.NewClient(opt)
	t.Cleanup(func() { _ = pubClient.Close() })

	b := broker.New(broker.Options{
		Redis:  subClient,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(ctx) }()

	// Wait until the broker's subscription is live (Run confirms before reading,
	// but the publish below must land after SUBSCRIBE is registered server-side).
	waitForSubscribers(t, pubClient, constants.ChannelSwap)

	sub := b.Subscribe(broker.Filter{Channels: map[string]struct{}{constants.ChannelSwap: {}}}, 64)
	defer sub.Close()

	payload, _ := json.Marshal(map[string]any{"poolAddress": "0xAAA", "blockTimestamp": 99})
	if err := pubClient.Publish(ctx, constants.ChannelSwap, payload).Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-sub.Signal():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fan-out signal")
	}
	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("Drain len = %d, want 1", len(got))
	}
	var frame struct {
		Type string `json:"type"`
		Pool string `json:"pool"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Type != "swap" || frame.Pool != "0xaaa" {
		t.Errorf("unexpected frame: %+v", frame)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// waitForSubscribers polls PUBSUB NUMSUB until the broker has subscribed to the
// channel, so the test publish is not lost to a race with SUBSCRIBE.
func waitForSubscribers(t *testing.T, rdb *goredis.Client, channel string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		if err == nil && res[channel] >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("broker did not subscribe within timeout")
}
