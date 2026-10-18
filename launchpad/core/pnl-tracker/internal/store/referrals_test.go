package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// seedCard inserts a card (and its referral binding) for a sharer.
func seedCard(t *testing.T, st *store.Store, cardID, shortCode, sharer string) {
	t.Helper()
	if err := st.InsertCard(context.Background(), store.CardRow{
		CardID: cardID, ShortCode: shortCode, OwnerAddress: sharer,
		PoolAddress: "0xp", TokenAddress: "0xt", Snapshot: json.RawMessage(`{}`), CreatedAt: 1,
	}); err != nil {
		t.Fatalf("seed card: %v", err)
	}
}

func TestLogReferralEventDedupAndStats(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedCard(t, st, "c1", "code1", "0xSharer")

	// Append-only events.
	for i := 0; i < 3; i++ {
		if _, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: store.EventView, CreatedAt: 1}); err != nil {
			t.Fatalf("view: %v", err)
		}
	}
	if _, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: store.EventClick, CreatedAt: 1}); err != nil {
		t.Fatalf("click: %v", err)
	}

	// wallet_bind is deduplicated per (short_code, wallet).
	first, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: store.EventWalletBind, WalletAddress: "0xWALLET", CreatedAt: 1})
	if err != nil || !first {
		t.Fatalf("first bind inserted=%v err=%v", first, err)
	}
	dup, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: store.EventWalletBind, WalletAddress: "0xwallet", CreatedAt: 1})
	if err != nil {
		t.Fatalf("dup bind: %v", err)
	}
	if dup {
		t.Fatal("duplicate wallet_bind should not insert a second row")
	}

	// One conversion (credited via per-conversion reward = 2).
	if _, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: store.EventConversion, WalletAddress: "0xwallet", CreatedAt: 1}); err != nil {
		t.Fatalf("conversion: %v", err)
	}

	stats, err := st.GetSharerStats(ctx, "0xsharer", 2)
	if err != nil {
		t.Fatalf("sharer stats: %v", err)
	}
	if len(stats.ShortCodes) != 1 || stats.ShortCodes[0] != "code1" {
		t.Errorf("shortCodes = %v", stats.ShortCodes)
	}
	if stats.TotalViews != 3 || stats.TotalClicks != 1 || stats.TotalWalletBinds != 1 || stats.TotalConversions != 1 {
		t.Errorf("funnel = %+v", stats)
	}
	if stats.PendingRewards != 2 || stats.CreditedRewards != 0 {
		t.Errorf("rewards pending=%d credited=%d, want 2/0", stats.PendingRewards, stats.CreditedRewards)
	}
}

func TestLogReferralEventRejectsUnknownType(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	seedCard(t, st, "c1", "code1", "0xs")
	if _, err := st.LogReferralEvent(ctx, store.ReferralEvent{ShortCode: "code1", EventType: "bogus", CreatedAt: 1}); err == nil {
		t.Fatal("expected error for unknown event type")
	}
}

func TestSharerStatsEmpty(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	stats, err := st.GetSharerStats(ctx, "0xnobody", 1)
	if err != nil {
		t.Fatalf("sharer stats: %v", err)
	}
	if len(stats.ShortCodes) != 0 || stats.TotalViews != 0 {
		t.Errorf("expected empty stats, got %+v", stats)
	}
}

func TestUserHasAnyPosition(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	has, err := st.UserHasAnyPosition(ctx, "0xu")
	if err != nil || has {
		t.Fatalf("expected no position, has=%v err=%v", has, err)
	}
	mustFold(t, st, trade("0xa-0", "0xu", "0xp", "0xt", true, usdl1, oneToken, 1, 1))
	has, err = st.UserHasAnyPosition(ctx, "0xU")
	if err != nil || !has {
		t.Fatalf("expected a position after fold, has=%v err=%v", has, err)
	}
}
