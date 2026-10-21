package ranker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
)

// TestWritePublishesRankingsUpdate proves write() emits a rankings:update signal
// carrying the de-prefixed category and the ordered items, so the client can
// replace that one list without polling.
func TestWritePublishesRankingsUpdate(t *testing.T) {
	rdb := internaltest.NewRedis(t)
	ctx := context.Background()
	svc := NewService(nil, rdb, 10, nil)

	sub := rdb.Subscribe(ctx, constants.ChannelRankingsUpdate)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if err := svc.write(ctx, "ranking:trending", []Scored{
		{Address: "0xb", Score: 5}, {Address: "0xa", Score: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var payload []byte
	select {
	case msg := <-sub.Channel():
		payload = []byte(msg.Payload)
	case <-time.After(3 * time.Second):
		t.Fatal("expected rankings:update publish")
	}

	var got struct {
		Category string `json:"category"`
		Items    []struct {
			Address string  `json:"address"`
			Score   float64 `json:"score"`
		} `json:"items"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal rankings:update: %v", err)
	}
	if got.Category != "trending" {
		t.Errorf("category = %q, want trending (ranking: prefix stripped)", got.Category)
	}
	if len(got.Items) != 2 || got.Items[0].Address != "0xb" || got.Items[0].Score != 5 {
		t.Errorf("items = %+v, want score-desc [0xb=5, 0xa=1]", got.Items)
	}
}

func TestWriteSortsTruncatesAndPublishes(t *testing.T) {
	rdb := internaltest.NewRedis(t)
	ctx := context.Background()

	// src is unused by write(); pass nil to test the publish path in isolation
	// against a REAL Redis (no fake data source involved).
	svc := NewService(nil, rdb, 3, nil)

	entries := []Scored{
		{Address: "0xa", Score: 1},
		{Address: "0xb", Score: 5},
		{Address: "0xc", Score: 3},
		{Address: "0xd", Score: 4},
		{Address: "0xe", Score: 2},
	}
	if err := svc.write(ctx, "ranking:test", entries); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := rdb.ZRevRangeWithScores(ctx, "ranking:test", 0, -1).Result()
	if err != nil {
		t.Fatalf("zrevrange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 (truncated to maxEntries), got %d", len(got))
	}
	wantOrder := []string{"0xb", "0xd", "0xc"} // scores 5,4,3
	for i, z := range got {
		if z.Member.(string) != wantOrder[i] {
			t.Errorf("position %d = %v, want %s", i, z.Member, wantOrder[i])
		}
	}
}

func TestWriteEmptyDeletesKey(t *testing.T) {
	rdb := internaltest.NewRedis(t)
	ctx := context.Background()
	svc := NewService(nil, rdb, 10, nil)

	// Pre-populate, then overwrite with an empty ranking.
	if err := svc.write(ctx, "ranking:test", []Scored{{Address: "0xa", Score: 1}}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if err := svc.write(ctx, "ranking:test", nil); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	n, err := rdb.Exists(ctx, "ranking:test").Result()
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected key deleted, exists = %d", n)
	}
}

func TestWriteAtomicReplace(t *testing.T) {
	rdb := internaltest.NewRedis(t)
	ctx := context.Background()
	svc := NewService(nil, rdb, 10, nil)

	if err := svc.write(ctx, "ranking:test", []Scored{{Address: "0xold", Score: 9}}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := svc.write(ctx, "ranking:test", []Scored{{Address: "0xnew", Score: 1}}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	members, err := rdb.ZRange(ctx, "ranking:test", 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange: %v", err)
	}
	if len(members) != 1 || members[0] != "0xnew" {
		t.Fatalf("expected only 0xnew after replace, got %v", members)
	}
	// No leftover staging keys.
	tmp, err := rdb.Keys(ctx, "ranking:test:tmp:*").Result()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(tmp) != 0 {
		t.Fatalf("expected no staging keys, got %v", tmp)
	}
}

func TestWriteTruncationKeepsHighestStably(t *testing.T) {
	rdb := internaltest.NewRedis(t)
	ctx := context.Background()
	svc := NewService(nil, rdb, 2, nil)

	// Two entries share the top score; stable sort keeps the first-inserted when
	// truncating below the tie count is not triggered here, but the two highest
	// (score 5) must both survive over the score-1 entry.
	entries := []Scored{
		{Address: "0xfirst", Score: 5},
		{Address: "0xsecond", Score: 5},
		{Address: "0xlow", Score: 1},
	}
	if err := svc.write(ctx, "ranking:test", entries); err != nil {
		t.Fatalf("write: %v", err)
	}
	members, err := rdb.ZRange(ctx, "ranking:test", 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 survivors, got %v", members)
	}
	for _, m := range members {
		if m == "0xlow" {
			t.Fatalf("score-1 entry should have been truncated, got %v", members)
		}
	}
}
