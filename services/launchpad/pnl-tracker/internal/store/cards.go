package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CardRow is a pnl.pnl_cards row. Snapshot is the immutable CardSnapshot JSON
// captured at mint time.
type CardRow struct {
	CardID       string
	ShortCode    string
	OwnerAddress string
	PoolAddress  string
	TokenAddress string
	Snapshot     json.RawMessage
	CreatedAt    int64
}

// InsertCard persists a minted card AND its referral binding (short_code ->
// sharer) atomically, so a short code always resolves to both a card and a
// sharer. Idempotent on card_id / short_code (ON CONFLICT DO NOTHING).
func (s *Store) InsertCard(ctx context.Context, c CardRow) error {
	owner := strings.ToLower(c.OwnerAddress)
	pool := strings.ToLower(c.PoolAddress)
	token := strings.ToLower(c.TokenAddress)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin card tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit; rollback is best-effort cleanup

	if _, err := tx.Exec(ctx, `
		INSERT INTO pnl.pnl_cards (card_id, short_code, owner_address, pool_address, token_address, snapshot, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (card_id) DO NOTHING`,
		c.CardID, c.ShortCode, owner, pool, token, []byte(c.Snapshot), c.CreatedAt); err != nil {
		return fmt.Errorf("store: insert card: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO pnl.referrals (short_code, sharer_address, card_id, created_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (short_code) DO NOTHING`,
		c.ShortCode, owner, c.CardID, c.CreatedAt); err != nil {
		return fmt.Errorf("store: insert referral: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit card tx: %w", err)
	}
	return nil
}

func scanCard(row pgx.Row) (*CardRow, error) {
	var c CardRow
	var snap []byte
	if err := row.Scan(&c.CardID, &c.ShortCode, &c.OwnerAddress, &c.PoolAddress,
		&c.TokenAddress, &snap, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Snapshot = json.RawMessage(snap)
	return &c, nil
}

const cardColumns = `card_id, short_code, owner_address, pool_address, token_address, snapshot, created_at`

// GetCard returns a card by its id, or (nil, nil) when absent.
func (s *Store) GetCard(ctx context.Context, cardID string) (*CardRow, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+cardColumns+` FROM pnl.pnl_cards WHERE card_id = $1`, cardID)
	c, err := scanCard(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get card: %w", err)
	}
	return c, nil
}

// GetCardByShortCode returns a card by its public short code, or (nil, nil).
func (s *Store) GetCardByShortCode(ctx context.Context, shortCode string) (*CardRow, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+cardColumns+` FROM pnl.pnl_cards WHERE short_code = $1`, shortCode)
	c, err := scanCard(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get card by short code: %w", err)
	}
	return c, nil
}
