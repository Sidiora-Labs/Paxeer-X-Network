package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Referral event types (the attribution funnel). view/click are append-only;
// wallet_bind/conversion are deduplicated per (short_code, wallet).
const (
	EventView       = "view"
	EventClick      = "click"
	EventWalletBind = "wallet_bind"
	EventConversion = "conversion"
)

// ReferralEvent is one attribution event to log.
type ReferralEvent struct {
	ShortCode     string
	EventType     string
	WalletAddress string // optional ("" -> stored NULL)
	CardID        string // optional
	CreatedAt     int64
}

// ShortCodeExists reports whether a referral short code is known (a real minted
// card). Used to reject attribution events for codes that were never issued.
func (s *Store) ShortCodeExists(ctx context.Context, shortCode string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pnl.referrals WHERE short_code = $1)`, shortCode).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: short code exists: %w", err)
	}
	return exists, nil
}

// ShortCodeForCard resolves a card id to its referral short code, or "" when the
// card is unknown.
func (s *Store) ShortCodeForCard(ctx context.Context, cardID string) (string, error) {
	var code string
	err := s.pool.QueryRow(ctx, `SELECT short_code FROM pnl.referrals WHERE card_id = $1 LIMIT 1`, cardID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: short code for card: %w", err)
	}
	return code, nil
}

// UserHasAnyPosition reports whether a wallet holds any folded position (i.e. has
// traded on-platform). Used to decide whether a wallet_bind also counts as a
// conversion.
func (s *Store) UserHasAnyPosition(ctx context.Context, user string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pnl.user_positions WHERE user_address = $1)`, strings.ToLower(user)).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: user has position: %w", err)
	}
	return exists, nil
}

// LogReferralEvent records one attribution event. wallet_bind and conversion are
// idempotent per (short_code, wallet) via the partial unique index; the returned
// bool reports whether a NEW row was written (false on a duplicate bind). view
// and click always insert. Returns an error for an unknown event type.
func (s *Store) LogReferralEvent(ctx context.Context, e ReferralEvent) (bool, error) {
	switch e.EventType {
	case EventView, EventClick, EventWalletBind, EventConversion:
	default:
		return false, fmt.Errorf("store: unknown referral event type %q", e.EventType)
	}

	var wallet *string
	if w := strings.ToLower(strings.TrimSpace(e.WalletAddress)); w != "" {
		wallet = &w
	}
	var card *string
	if e.CardID != "" {
		card = &e.CardID
	}

	conflict := ""
	if e.EventType == EventWalletBind || e.EventType == EventConversion {
		conflict = ` ON CONFLICT (short_code, wallet_address, event_type)
			WHERE event_type IN ('wallet_bind','conversion') DO NOTHING`
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO pnl.referral_events (short_code, event_type, wallet_address, card_id, created_at)
		VALUES ($1,$2,$3,$4,$5)`+conflict,
		e.ShortCode, e.EventType, wallet, card, e.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("store: log referral event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SharerStats is the aggregate funnel for a sharer across all their short codes
// (pnl.ts SharerStats). Rewards are integer unit counts derived from
// conversions: pending = not-yet-credited conversions, credited = credited ones,
// each scaled by the per-conversion reward.
type SharerStats struct {
	Address          string   `json:"address"`
	ShortCodes       []string `json:"shortCodes"`
	TotalViews       int      `json:"totalViews"`
	TotalClicks      int      `json:"totalClicks"`
	TotalWalletBinds int      `json:"totalWalletBinds"`
	TotalConversions int      `json:"totalConversions"`
	PendingRewards   int      `json:"pendingRewards"`
	CreditedRewards  int      `json:"creditedRewards"`
}

// GetSharerStats aggregates the funnel for every short code owned by a sharer.
// rewardPerConversion scales the conversion counts into reward units.
func (s *Store) GetSharerStats(ctx context.Context, sharer string, rewardPerConversion int) (SharerStats, error) {
	addr := strings.ToLower(sharer)
	out := SharerStats{Address: addr, ShortCodes: []string{}}

	codeRows, err := s.pool.Query(ctx,
		`SELECT short_code FROM pnl.referrals WHERE sharer_address = $1 ORDER BY created_at ASC`, addr)
	if err != nil {
		return out, fmt.Errorf("store: sharer codes: %w", err)
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var code string
		if err := codeRows.Scan(&code); err != nil {
			return out, fmt.Errorf("store: scan sharer code: %w", err)
		}
		out.ShortCodes = append(out.ShortCodes, code)
	}
	if err := codeRows.Err(); err != nil {
		return out, err
	}
	if len(out.ShortCodes) == 0 {
		return out, nil
	}

	var pendingConversions, creditedConversions int
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE e.event_type = 'view'),
			COUNT(*) FILTER (WHERE e.event_type = 'click'),
			COUNT(*) FILTER (WHERE e.event_type = 'wallet_bind'),
			COUNT(*) FILTER (WHERE e.event_type = 'conversion'),
			COUNT(*) FILTER (WHERE e.event_type = 'conversion' AND NOT e.credited),
			COUNT(*) FILTER (WHERE e.event_type = 'conversion' AND e.credited)
		FROM pnl.referral_events e
		JOIN pnl.referrals r ON r.short_code = e.short_code
		WHERE r.sharer_address = $1`, addr).
		Scan(&out.TotalViews, &out.TotalClicks, &out.TotalWalletBinds, &out.TotalConversions,
			&pendingConversions, &creditedConversions)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("store: sharer stats: %w", err)
	}
	out.PendingRewards = pendingConversions * rewardPerConversion
	out.CreditedRewards = creditedConversions * rewardPerConversion
	return out, nil
}
