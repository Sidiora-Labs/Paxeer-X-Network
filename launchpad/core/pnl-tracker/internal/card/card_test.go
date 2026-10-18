package card_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/card"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const oneToken = "1000000000000000000"

func newService(t *testing.T) (*card.Service, *store.Store) {
	t.Helper()
	st := store.New(internaltest.NewPostgres(t))
	internaltest.EnsureStatsSchema(t, st.Pool())
	internaltest.EnsureMetadataSchema(t, st.Pool())
	svc := card.New(st, "https://sidiora.fun", "https://og.example", func() int64 { return 1234 })
	return svc, st
}

func TestMintCapturesSnapshotAndPersists(t *testing.T) {
	ctx := context.Background()
	svc, st := newService(t)

	// A real position is required to mint.
	if _, err := st.FoldTrade(ctx, store.TradeInput{
		ID: "0xb-0", UserAddress: "0xOwner", PoolAddress: "0xPool", TokenAddress: "0xToken",
		IsBuy: true, UsdlAmount: "1000000", TokenAmount: oneToken, Price: "0", Fee: "0",
		BlockNumber: 1, BlockTimestamp: 100, TxHash: "0xb",
	}); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, price_change_24h)
		VALUES ('0xpool','0xtoken','2500000','5000000','120')`); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, created_at)
		VALUES ('0xtoken','0xpool','Dogecoin','DOGE',1)`); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	minted, err := svc.Mint(ctx, "0xowner", "0xpool")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if minted.Snapshot.Version != 1 || minted.Snapshot.OwnerAddress != "0xowner" {
		t.Errorf("snapshot = %+v", minted.Snapshot)
	}
	if minted.Snapshot.TokenSymbol != "DOGE" || minted.Snapshot.TokenName != "Dogecoin" {
		t.Errorf("snapshot meta = %s/%s", minted.Snapshot.TokenSymbol, minted.Snapshot.TokenName)
	}
	if minted.Snapshot.Market.PriceWad == nil || *minted.Snapshot.Market.PriceWad != "2500000" {
		t.Errorf("snapshot priceWad = %v", minted.Snapshot.Market.PriceWad)
	}
	if minted.Snapshot.CapturedAt != 1234 {
		t.Errorf("capturedAt = %d, want 1234", minted.Snapshot.CapturedAt)
	}
	if !strings.HasPrefix(minted.ShareURL, "https://sidiora.fun/pnl/") || minted.ShortCode == "" {
		t.Errorf("shareUrl = %s shortCode = %s", minted.ShareURL, minted.ShortCode)
	}
	wantOG := "https://og.example/pnl/cards/" + minted.CardID + "/og.png"
	if minted.OgURL != wantOG {
		t.Errorf("ogUrl = %s, want %s", minted.OgURL, wantOG)
	}

	// Hydrate by id returns an equivalent card.
	got, err := svc.Get(ctx, minted.CardID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.ShortCode != minted.ShortCode || got.Snapshot.TokenSymbol != "DOGE" {
		t.Errorf("hydrated = %+v", got)
	}
}

func TestMintRejectsMissingPosition(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	if _, err := svc.Mint(ctx, "0xnobody", "0xpool"); !errors.Is(err, card.ErrNoPosition) {
		t.Fatalf("err = %v, want ErrNoPosition", err)
	}
}

func TestGetUnknownCard(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	got, err := svc.Get(ctx, "missing")
	if err != nil || got != nil {
		t.Fatalf("got = %v err = %v, want nil/nil", got, err)
	}
}

func TestBuildRenderInputFormatting(t *testing.T) {
	price := "2500000"
	snap := card.Snapshot{
		Version:      1,
		TokenName:    "Dogecoin",
		TokenSymbol:  "DOGE",
		TokenAddress: "0xtoken",
		Position: card.Position{
			TotalUsdlSpent:    "4000000",
			TotalUsdlReceived: "5000000",
			AvgCostBasis:      "2000000",
			CurrentHoldings:   oneToken,
			RealizedPnlUsdl:   "3000000",
		},
		Market: card.Market{PriceWad: &price},
	}
	in := card.BuildRenderInput(snap, "sidiora.fun/pnl/abc1234")
	if in.Title != "Dogecoin" {
		t.Errorf("title = %s", in.Title)
	}
	// (5 + 2.5) / 4 = 1.875 -> "1.87x" (truncated to 2 dp).
	if in.Multiple != "1.87x" {
		t.Errorf("multiple = %s, want 1.87x", in.Multiple)
	}
	// 3 + (2.5 - 2) = 3.5 USDL.
	if in.Pnl != "+$3.50" || !in.PnlPositive {
		t.Errorf("pnl = %s positive = %v", in.Pnl, in.PnlPositive)
	}
	if in.Holdings != "1.00 DOGE held" {
		t.Errorf("holdings = %s", in.Holdings)
	}
}

func TestBuildRenderInputLossNoMarket(t *testing.T) {
	snap := card.Snapshot{
		TokenSymbol: "LOSS",
		Position: card.Position{
			TotalUsdlSpent:  "10000000",
			AvgCostBasis:    "10000000",
			CurrentHoldings: "0",
			RealizedPnlUsdl: "-6000000",
		},
	}
	in := card.BuildRenderInput(snap, "f")
	if in.Title != "$LOSS" {
		t.Errorf("title = %s, want $LOSS", in.Title)
	}
	if in.Pnl != "-$6.00" || in.PnlPositive {
		t.Errorf("pnl = %s positive = %v, want -$6.00 / false", in.Pnl, in.PnlPositive)
	}
	// Nothing received and nothing held -> 0.00x.
	if in.Multiple != "0.00x" {
		t.Errorf("multiple = %s, want 0.00x", in.Multiple)
	}
}
