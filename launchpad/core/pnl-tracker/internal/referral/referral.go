// Package referral handles the PnL share attribution funnel: logging view /
// click / wallet_bind events against a card's short code, promoting a bind to a
// conversion when the referred wallet has actually traded, and serving the
// sharer dashboard aggregate. Ports services/referral-service.ts. Events are
// deduplicated at the store layer (partial unique index) so attribution can be
// retried safely (fire-and-forget on the client).
package referral

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// ErrUnknownReferral is returned when neither a known short code nor a known card
// id can be resolved for an event.
var ErrUnknownReferral = errors.New("referral: unknown short code or card")

// Service logs attribution events and computes sharer stats.
type Service struct {
	store               *store.Store
	logger              *slog.Logger
	rewardPerConversion int
	now                 func() int64
}

// New builds a referral Service.
func New(st *store.Store, logger *slog.Logger, rewardPerConversion int, now func() int64) *Service {
	if rewardPerConversion <= 0 {
		rewardPerConversion = 1
	}
	return &Service{store: st, logger: logger, rewardPerConversion: rewardPerConversion, now: now}
}

// Event is an attribution event to log. Either ShortCode or CardID must resolve
// to a known referral. Type must be one of "view", "click", "wallet_bind".
type Event struct {
	Type          string
	ShortCode     string
	CardID        string
	WalletAddress string
}

// Log resolves the referral, records the event, and — for a wallet_bind whose
// wallet has already traded — also records a conversion (idempotent). It returns
// ErrUnknownReferral when the code/card can't be resolved, so the route can 400.
func (s *Service) Log(ctx context.Context, e Event) error {
	code, err := s.resolveCode(ctx, e.ShortCode, e.CardID)
	if err != nil {
		return err
	}
	if code == "" {
		return ErrUnknownReferral
	}

	eventType := strings.TrimSpace(e.Type)
	switch eventType {
	case store.EventView, store.EventClick, store.EventWalletBind:
	default:
		return errors.New("referral: invalid event type")
	}

	if _, err := s.store.LogReferralEvent(ctx, store.ReferralEvent{
		ShortCode:     code,
		EventType:     eventType,
		WalletAddress: e.WalletAddress,
		CardID:        e.CardID,
		CreatedAt:     s.now(),
	}); err != nil {
		return err
	}

	// A bind by a wallet that has traded converts.
	if eventType == store.EventWalletBind && e.WalletAddress != "" {
		traded, err := s.store.UserHasAnyPosition(ctx, e.WalletAddress)
		if err != nil {
			return err
		}
		if traded {
			if _, err := s.store.LogReferralEvent(ctx, store.ReferralEvent{
				ShortCode:     code,
				EventType:     store.EventConversion,
				WalletAddress: e.WalletAddress,
				CardID:        e.CardID,
				CreatedAt:     s.now(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// LogView records a card view (called when a card landing is hydrated by short
// code). Best-effort: a failure is logged, never surfaced.
func (s *Service) LogView(ctx context.Context, shortCode string) {
	if shortCode == "" {
		return
	}
	if _, err := s.store.LogReferralEvent(ctx, store.ReferralEvent{
		ShortCode: shortCode,
		EventType: store.EventView,
		CreatedAt: s.now(),
	}); err != nil {
		s.logger.Warn("referral: log view failed", slog.String("shortCode", shortCode), slog.Any("err", err))
	}
}

// Stats returns the sharer dashboard aggregate for an address.
func (s *Service) Stats(ctx context.Context, sharer string) (store.SharerStats, error) {
	stats, err := s.store.GetSharerStats(ctx, sharer, s.rewardPerConversion)
	// Attribution counts are analytics, not an asset-denominated funded liability.
	// Preserve the legacy wire field without promising conversion-count rewards.
	stats.PendingRewards = 0
	stats.CreditedRewards = 0
	return stats, err
}

// resolveCode returns the short code for an event, preferring an explicit (and
// known) short code, else resolving via the card id. Returns "" when neither
// resolves to a real referral.
func (s *Service) resolveCode(ctx context.Context, shortCode, cardID string) (string, error) {
	if c := strings.TrimSpace(shortCode); c != "" {
		ok, err := s.store.ShortCodeExists(ctx, c)
		if err != nil {
			return "", err
		}
		if ok {
			return c, nil
		}
	}
	if cardID != "" {
		return s.store.ShortCodeForCard(ctx, cardID)
	}
	return "", nil
}
