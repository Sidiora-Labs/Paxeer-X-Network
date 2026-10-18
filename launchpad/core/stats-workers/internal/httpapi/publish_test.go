package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// subscribeConfirmed subscribes to channel and blocks until the subscription is
// confirmed so a following publish cannot race ahead of registration.
func subscribeConfirmed(t *testing.T, ctx context.Context, rdb *goredis.Client, channel string) *goredis.PubSub {
	t.Helper()
	sub := rdb.Subscribe(ctx, channel)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe %s: %v", channel, err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func awaitPublish(t *testing.T, sub *goredis.PubSub) []byte {
	t.Helper()
	select {
	case msg := <-sub.Channel():
		return []byte(msg.Payload)
	case <-time.After(3 * time.Second):
		t.Fatal("expected a published message, got none")
		return nil
	}
}

// TestReactionVotePublishesReactionsUpdate proves a vote mutation pushes the
// fresh aggregate tally on reactions:update (so every viewer updates in place),
// carrying poolAddress for broker routing and the recomputed counts — but NOT
// the per-wallet userVote.
func TestReactionVotePublishesReactionsUpdate(t *testing.T) {
	ctx := context.Background()
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterReactions(r, rdb)

	const pool = "0xpublish_react"
	key, wallet := newWallet(t)
	msg := "vote on " + pool
	sig := signWallet(t, key, msg)

	sub := subscribeConfirmed(t, ctx, rdb, constants.ChannelReactionsUpdate)

	body := mustJSON(t, map[string]string{
		"reaction": "bullish", "walletAddress": wallet, "signature": sig, "message": msg,
	})
	if rec := serve(t, r, http.MethodPost, "/stats/"+pool+"/reactions", body,
		map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusOK {
		t.Fatalf("vote status = %d, want 200", rec.Code)
	}

	var got struct {
		PoolAddress string `json:"poolAddress"`
		Reactions   struct {
			Bullish int `json:"bullish"`
		} `json:"reactions"`
		Total    int             `json:"total"`
		UserVote json.RawMessage `json:"userVote"`
	}
	if err := json.Unmarshal(awaitPublish(t, sub), &got); err != nil {
		t.Fatalf("unmarshal reactions:update payload: %v", err)
	}
	if got.PoolAddress != pool {
		t.Errorf("published poolAddress = %q, want %q", got.PoolAddress, pool)
	}
	if got.Reactions.Bullish != 1 || got.Total != 1 {
		t.Errorf("published tally = %+v, want one bullish vote", got)
	}
	if got.UserVote != nil {
		t.Errorf("userVote must NOT be broadcast (per-wallet), got %s", got.UserVote)
	}
}

// TestPrecomputePublishesPlatformUpdate proves the background precompute pushes
// platform:update (a global event) carrying the same metrics it cached.
func TestPrecomputePublishesPlatformUpdate(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)

	sub := subscribeConfirmed(t, ctx, rdb, constants.ChannelPlatformUpdate)

	if err := httpapi.PrecomputePlatformMetrics(ctx, st, rdb); err != nil {
		t.Fatalf("precompute platform metrics: %v", err)
	}

	payload := awaitPublish(t, sub)
	// The published payload must equal the cached payload byte-for-byte.
	cached, err := rdb.Get(ctx, "platform:metrics").Result()
	if err != nil {
		t.Fatalf("read platform cache: %v", err)
	}
	if string(payload) != cached {
		t.Errorf("published platform payload != cached payload\npub=%s\ncache=%s", payload, cached)
	}
	// And it must be valid JSON object.
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatalf("platform:update payload not a JSON object: %v", err)
	}
}
