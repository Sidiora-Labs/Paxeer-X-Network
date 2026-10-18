package referral_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/referral"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const oneToken = "1000000000000000000"

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newService(t *testing.T) (*referral.Service, *store.Store) {
	t.Helper()
	st := store.New(referralFixture(t))
	svc := referral.New(st, discardLogger(), 2, func() int64 { return 1 })
	return svc, st
}

func seedCard(t *testing.T, st *store.Store, cardID, code, sharer string) {
	t.Helper()
	if err := st.InsertCard(context.Background(), store.CardRow{
		CardID: cardID, ShortCode: code, OwnerAddress: sharer,
		PoolAddress: "0xp", TokenAddress: "0xt", Snapshot: json.RawMessage(`{}`), CreatedAt: 1,
	}); err != nil {
		t.Fatalf("seed card: %v", err)
	}
}

func TestLogClickAndBindToConversion(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	seedCard(t, st, "c1", "code1", "0xsharer")

	// A click against a known short code.
	if err := svc.Log(ctx, referral.Event{Type: store.EventClick, ShortCode: "code1"}); err != nil {
		t.Fatalf("click: %v", err)
	}

	// A bind by a wallet that HAS traded promotes to a conversion.
	if _, err := st.FoldTrade(ctx, store.TradeInput{
		ID: "0xb-0", UserAddress: "0xwallet", PoolAddress: "0xp", TokenAddress: "0xt",
		IsBuy: true, UsdlAmount: "1000000", TokenAmount: oneToken, Price: "0", Fee: "0",
		BlockNumber: 1, BlockTimestamp: 1, TxHash: "0xb",
	}); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if err := svc.Log(ctx, referral.Event{Type: store.EventWalletBind, ShortCode: "code1", WalletAddress: "0xwallet"}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	stats, err := svc.Stats(ctx, "0xsharer")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalClicks != 1 || stats.TotalWalletBinds != 1 || stats.TotalConversions != 1 {
		t.Fatalf("funnel = %+v", stats)
	}
	// Analytics conversion has no verified funding or payout authority.
	if stats.PendingRewards != 0 {
		t.Errorf("pendingRewards = %d, want 0", stats.PendingRewards)
	}
}

func TestBindWithoutTradeIsNotConversion(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	seedCard(t, st, "c1", "code1", "0xsharer")

	if err := svc.Log(ctx, referral.Event{Type: store.EventWalletBind, ShortCode: "code1", WalletAddress: "0xfresh"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	stats, _ := svc.Stats(ctx, "0xsharer")
	if stats.TotalWalletBinds != 1 || stats.TotalConversions != 0 {
		t.Fatalf("funnel = %+v, want 1 bind / 0 conversions", stats)
	}
}

func TestLogResolvesByCardID(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	seedCard(t, st, "card-xyz", "code1", "0xsharer")

	// No short code supplied; resolve via card id.
	if err := svc.Log(ctx, referral.Event{Type: store.EventClick, CardID: "card-xyz"}); err != nil {
		t.Fatalf("click by card id: %v", err)
	}
	stats, _ := svc.Stats(ctx, "0xsharer")
	if stats.TotalClicks != 1 {
		t.Fatalf("clicks = %d, want 1", stats.TotalClicks)
	}
}

func TestLogUnknownReferral(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	err := svc.Log(ctx, referral.Event{Type: store.EventClick, ShortCode: "nope"})
	if !errors.Is(err, referral.ErrUnknownReferral) {
		t.Fatalf("err = %v, want ErrUnknownReferral", err)
	}
}

func TestLogInvalidType(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	seedCard(t, st, "c1", "code1", "0xsharer")
	if err := svc.Log(ctx, referral.Event{Type: "bogus", ShortCode: "code1"}); err == nil {
		t.Fatal("expected error for invalid event type")
	}
}

func TestLogView(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)
	seedCard(t, st, "c1", "code1", "0xsharer")
	svc.LogView(ctx, "code1")
	svc.LogView(ctx, "") // no-op, must not panic
	stats, _ := svc.Stats(ctx, "0xsharer")
	if stats.TotalViews != 1 {
		t.Fatalf("views = %d, want 1", stats.TotalViews)
	}
}
