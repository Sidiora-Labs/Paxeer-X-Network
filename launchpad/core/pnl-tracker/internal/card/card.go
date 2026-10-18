// Package card mints and hydrates shareable PnL cards. A mint validates that the
// (owner, pool) pair has a real position (rejecting garbage mints), captures an
// immutable CardSnapshot (position + market context), persists it with a public
// short code, and returns the share + OG image URLs. The wire shapes match the
// TS client (pnl.ts CardSnapshot / MintedCard) exactly for response parity; all
// numeric fields stay decimal strings (invariant i1) and image display strings
// are formatted with math/big, never float.
package card

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlmath"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// ErrNoPosition is returned when a mint is attempted for a pair that never traded.
var ErrNoPosition = errors.New("card: no position for owner/pool")

// Position is the numeric subset of a position embedded in a snapshot
// (pnl.ts: Omit<UserPosition,'userAddress'|'poolAddress'|'tokenAddress'>).
type Position struct {
	TotalUsdlSpent    string `json:"totalUsdlSpent"`
	TotalTokensBought string `json:"totalTokensBought"`
	TotalUsdlReceived string `json:"totalUsdlReceived"`
	TotalTokensSold   string `json:"totalTokensSold"`
	AvgCostBasis      string `json:"avgCostBasis"`
	CurrentHoldings   string `json:"currentHoldings"`
	RealizedPnlUsdl   string `json:"realizedPnlUsdl"`
	FirstBuyTs        *int64 `json:"firstBuyTs"`
	LastTradeTs       int64  `json:"lastTradeTs"`
	TradeCount        int    `json:"tradeCount"`
}

// Market is the captured market context (pnl.ts CardSnapshot.market). Nullable.
type Market struct {
	PriceWad          *string `json:"priceWad"`
	MarketCapUsdl     *string `json:"marketCapUsdl"`
	PriceChange24hBps *string `json:"priceChange24hBps"`
}

// Snapshot is the immutable card payload (pnl.ts CardSnapshot).
type Snapshot struct {
	Version      int      `json:"version"`
	OwnerAddress string   `json:"ownerAddress"`
	PoolAddress  string   `json:"poolAddress"`
	TokenAddress string   `json:"tokenAddress"`
	TokenSymbol  string   `json:"tokenSymbol,omitempty"`
	TokenName    string   `json:"tokenName,omitempty"`
	Position     Position `json:"position"`
	Market       Market   `json:"market"`
	CapturedAt   int64    `json:"capturedAt"`
}

// Minted is the mint/hydrate response (pnl.ts MintedCard).
type Minted struct {
	CardID    string   `json:"cardId"`
	ShortCode string   `json:"shortCode"`
	ShareURL  string   `json:"shareUrl"`
	OgURL     string   `json:"ogUrl"`
	Snapshot  Snapshot `json:"snapshot"`
	CreatedAt int64    `json:"createdAt,omitempty"`
}

// Service mints and reads cards.
type Service struct {
	store       *store.Store
	shareOrigin string
	ogOrigin    string
	now         func() int64
}

// New builds a card Service. shareOrigin/ogOrigin are the trimmed origins used to
// build shareUrl/ogUrl.
func New(st *store.Store, shareOrigin, ogOrigin string, now func() int64) *Service {
	return &Service{store: st, shareOrigin: shareOrigin, ogOrigin: ogOrigin, now: now}
}

// shareURL / ogURL build the public links for a card.
func (s *Service) shareURL(shortCode string) string { return s.shareOrigin + "/pnl/" + shortCode }
func (s *Service) ogURL(cardID string) string {
	return s.ogOrigin + "/pnl/cards/" + cardID + "/og.png"
}

// Mint validates the position, captures a snapshot, persists the card and its
// referral binding, and returns the minted card. It returns ErrNoPosition when
// the pair never traded (the route maps that to 400).
func (s *Service) Mint(ctx context.Context, owner, pool string) (*Minted, error) {
	pos, err := s.store.GetPosition(ctx, owner, pool)
	if err != nil {
		return nil, err
	}
	if pos == nil {
		return nil, ErrNoPosition
	}

	market, err := s.store.GetMarket(ctx, pos.PoolAddress)
	if err != nil {
		return nil, err
	}
	meta, err := s.store.GetTokenMeta(ctx, pos.TokenAddress)
	if err != nil {
		return nil, err
	}

	snap := Snapshot{
		Version:      1,
		OwnerAddress: pos.UserAddress,
		PoolAddress:  pos.PoolAddress,
		TokenAddress: pos.TokenAddress,
		Position: Position{
			TotalUsdlSpent:    pos.TotalUsdlSpent,
			TotalTokensBought: pos.TotalTokensBought,
			TotalUsdlReceived: pos.TotalUsdlReceived,
			TotalTokensSold:   pos.TotalTokensSold,
			AvgCostBasis:      pos.AvgCostBasis,
			CurrentHoldings:   pos.CurrentHoldings,
			RealizedPnlUsdl:   pos.RealizedPnlUsdl,
			FirstBuyTs:        pos.FirstBuyTs,
			LastTradeTs:       pos.LastTradeTs,
			TradeCount:        pos.TradeCount,
		},
		CapturedAt: s.now(),
	}
	if meta != nil {
		snap.TokenSymbol = meta.Symbol
		snap.TokenName = meta.Name
	}
	if market != nil {
		snap.Market = Market{
			PriceWad:          ptr(market.PriceWad),
			MarketCapUsdl:     ptr(market.MarketCapUsdl),
			PriceChange24hBps: ptr(market.PriceChange24hBps),
		}
	}

	payload, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("card: marshal snapshot: %w", err)
	}

	cardID, err := randomID(16)
	if err != nil {
		return nil, err
	}
	shortCode, err := s.freeShortCode(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.store.InsertCard(ctx, store.CardRow{
		CardID:       cardID,
		ShortCode:    shortCode,
		OwnerAddress: pos.UserAddress,
		PoolAddress:  pos.PoolAddress,
		TokenAddress: pos.TokenAddress,
		Snapshot:     payload,
		CreatedAt:    snap.CapturedAt,
	}); err != nil {
		return nil, err
	}

	return &Minted{
		CardID:    cardID,
		ShortCode: shortCode,
		ShareURL:  s.shareURL(shortCode),
		OgURL:     s.ogURL(cardID),
		Snapshot:  snap,
		CreatedAt: snap.CapturedAt,
	}, nil
}

// Get hydrates a minted card by id, or (nil, nil) when unknown.
func (s *Service) Get(ctx context.Context, cardID string) (*Minted, error) {
	row, err := s.store.GetCard(ctx, cardID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	var snap Snapshot
	if err := json.Unmarshal(row.Snapshot, &snap); err != nil {
		return nil, fmt.Errorf("card: decode snapshot: %w", err)
	}
	return &Minted{
		CardID:    row.CardID,
		ShortCode: row.ShortCode,
		ShareURL:  s.shareURL(row.ShortCode),
		OgURL:     s.ogURL(row.CardID),
		Snapshot:  snap,
		CreatedAt: row.CreatedAt,
	}, nil
}

// freeShortCode generates a short code not already issued (a handful of retries
// makes a collision astronomically unlikely).
func (s *Service) freeShortCode(ctx context.Context) (string, error) {
	for i := 0; i < 5; i++ {
		code, err := randomShortCode(7)
		if err != nil {
			return "", err
		}
		exists, err := s.store.ShortCodeExists(ctx, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("card: could not allocate a unique short code")
}

func ptr(s string) *string { return &s }

// base62 alphabet for short codes.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// randomShortCode returns an n-char base62 code from crypto/rand.
func randomShortCode(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("card: read random: %w", err)
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = base62[int(b)%len(base62)]
	}
	return string(out), nil
}

// randomID returns a hex id of nBytes random bytes (2*nBytes chars).
func randomID(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("card: read random: %w", err)
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, nBytes*2)
	for i, b := range buf {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out), nil
}

// wad is the 1e18 fixed-point scale used by price/cost basis.
var wad = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// BuildRenderInput maps a snapshot to the flat, formatted ogrender.Input. The
// multiple and signed-PnL strings are derived with math/big so the displayed
// figures match the client's computeMultiple/computeTotalPnlUsdl (no float).
func BuildRenderInput(snap Snapshot, footer string) ogrender.Input {
	priceWad := ""
	if snap.Market.PriceWad != nil {
		priceWad = *snap.Market.PriceWad
	}
	pnlText, positive := formatSignedUsdl(snap.Position.RealizedPnlUsdl, snap.Position.AvgCostBasis, snap.Position.CurrentHoldings, priceWad)
	return ogrender.Input{
		Title:       titleFor(snap),
		Multiple:    formatMultiple(snap.Position, priceWad),
		Pnl:         pnlText,
		PnlPositive: positive,
		Holdings:    formatHoldings(snap.Position.CurrentHoldings, snap.TokenSymbol),
		Footer:      footer,
	}
}

func titleFor(snap Snapshot) string {
	if snap.TokenName != "" {
		return snap.TokenName
	}
	if snap.TokenSymbol != "" {
		return "$" + snap.TokenSymbol
	}
	return snap.PoolAddress
}

// formatMultiple renders (received + unrealized) / spent as "N.NNx".
func formatMultiple(p Position, priceWad string) string {
	spent, ok := new(big.Int).SetString(orZero(p.TotalUsdlSpent), 10)
	if !ok || spent.Sign() == 0 {
		return "0.00x"
	}
	received, _ := new(big.Int).SetString(orZero(p.TotalUsdlReceived), 10)
	unrealizedStr, err := pnlmath.HoldingValue(priceWad, p.CurrentHoldings)
	if err != nil {
		unrealizedStr = "0"
	}
	unrealized, _ := new(big.Int).SetString(orZero(unrealizedStr), 10)

	// milli = (received + unrealized) * 1000 / spent.
	num := new(big.Int).Add(received, unrealized)
	num.Mul(num, big.NewInt(1000))
	milli := num.Quo(num, spent)
	whole := new(big.Int).Quo(milli, big.NewInt(1000))
	frac := new(big.Int).Mod(milli, big.NewInt(1000))
	cents := new(big.Int).Quo(frac, big.NewInt(10)) // two-digit precision
	return fmt.Sprintf("%s.%02dx", whole.String(), cents.Int64())
}

// formatSignedUsdl renders the total PnL as "+$D.CC" / "-$D.CC" and its sign.
func formatSignedUsdl(realized, avgCost, holdings, priceWad string) (string, bool) {
	total, err := pnlmath.TotalPnl(realized, avgCost, holdings, priceWad)
	if err != nil {
		return "$0.00", true
	}
	v, _ := new(big.Int).SetString(total, 10)
	if v == nil {
		return "$0.00", true
	}
	positive := v.Sign() >= 0
	abs := new(big.Int).Abs(v)
	million := big.NewInt(1_000_000)
	dollars := new(big.Int).Quo(abs, million)
	rem := new(big.Int).Mod(abs, million)
	cents := new(big.Int).Quo(rem, big.NewInt(10_000))
	sign := "+"
	if !positive {
		sign = "-"
	}
	return fmt.Sprintf("%s$%s.%02d", sign, dollars.String(), cents.Int64()), positive
}

// formatHoldings renders a token balance (18-dec raw) as "N.NN SYMBOL held".
func formatHoldings(holdings, symbol string) string {
	v, ok := new(big.Int).SetString(orZero(holdings), 10)
	if !ok {
		v = new(big.Int)
	}
	whole := new(big.Int).Quo(v, wad)
	frac := new(big.Int).Mod(v, wad)
	// two decimals: frac / 1e16.
	cents := new(big.Int).Quo(frac, new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil))
	sym := symbol
	if sym == "" {
		sym = "tokens"
	}
	return fmt.Sprintf("%s.%02d %s held", whole.String(), cents.Int64(), sym)
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
