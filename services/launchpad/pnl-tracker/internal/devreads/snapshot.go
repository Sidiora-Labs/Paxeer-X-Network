package devreads

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	inventory "github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l16"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
)

// maxWalletPositions bounds the request-time projection of one wallet.
const maxWalletPositions = 5000

// observedLayout is the millisecond UTC layout every watermark carries.
const observedLayout = "2006-01-02T15:04:05.000Z"

// snapshotNamespace scopes the deterministic development snapshot ids.
var snapshotNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://sidiora.fun/pnl/development-portfolio-snapshot/v1"))

// Snapshot is one wallet's development projection: every dev position folded
// into the production l17 document shape at a single chain watermark.
type Snapshot struct {
	Wallet    string
	ID        string
	Revision  string
	Watermark l17.Watermark
	Positions []l17.Position
	Summary   l17.Summary
}

// Reader projects the development pnl schema.
type Reader struct {
	Pool  *pgxpool.Pool
	Chain *Chain
}

type tokenState struct {
	known    *big.Int
	cost     *big.Int
	realized *big.Int
}

// Snapshot builds the wallet's projection. A wallet without positions yields a
// valid empty snapshot (revision 1), never an error.
func (r Reader) Snapshot(ctx context.Context, wallet string) (Snapshot, error) {
	wallet, err := l17.Wallet(wallet)
	if err != nil {
		return Snapshot{}, err
	}
	if r.Pool == nil {
		return Snapshot{}, l17.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	states, trades, maxBlock, err := loadPositions(ctx, tx, wallet)
	if err != nil {
		return Snapshot{}, err
	}
	cursor, err := indexerCursor(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Snapshot{}, err
	}
	watermark, err := r.watermark(ctx, cursor, maxBlock, trades)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Wallet: wallet, Revision: watermark.ProjectionRevision, Watermark: watermark}
	out.ID = uuid.NewSHA1(snapshotNamespace, []byte(wallet+":"+out.Revision)).String()
	tokens := make([]string, 0, len(states))
	for token := range states {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	out.Positions = make([]l17.Position, 0, len(tokens))
	for _, token := range tokens {
		s := states[token]
		state := inventory.Empty()
		state.Known, state.Cost, state.Realized = s.known.String(), s.cost.String(), s.realized.String()
		position, err := inventory.ProjectPosition(wallet, token, state, watermark, nil, "unpriced")
		if err != nil {
			return Snapshot{}, fmt.Errorf("devreads: project %s: %w", token, err)
		}
		out.Positions = append(out.Positions, position)
	}
	out.Summary, err = l17.Aggregate(wallet, out.ID, watermark, out.Positions)
	if err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

// loadPositions reads the wallet's folded dev positions, merged per token (the
// production position identity is chain:wallet:token). The remaining cost is
// the exact fold residual spent - (received - realized), the same quantity
// pnlmath.Fold carries between trades. Rows whose token cannot be resolved to
// an address (pool not yet indexed) are left out until the indexer catches up.
func loadPositions(ctx context.Context, tx pgx.Tx, wallet string) (map[string]*tokenState, int64, int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT lower(COALESCE(NULLIF(p.token_address, ''), ip.token_address, '')),
		       p.total_usdl_spent, p.total_usdl_received, p.realized_pnl_usdl,
		       p.current_holdings, p.trade_count
		FROM pnl.user_positions p
		LEFT JOIN indexer.pools ip ON ip.pool_address = p.pool_address
		WHERE p.user_address = $1
		ORDER BY p.pool_address
		LIMIT $2`, wallet, maxWalletPositions+1)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("devreads: positions: %w", err)
	}
	defer rows.Close()
	states := map[string]*tokenState{}
	var trades int64
	count := 0
	for rows.Next() {
		var token, spent, received, realized, holdings string
		var tradeCount int64
		if err := rows.Scan(&token, &spent, &received, &realized, &holdings, &tradeCount); err != nil {
			return nil, 0, 0, fmt.Errorf("devreads: scan position: %w", err)
		}
		count++
		if count > maxWalletPositions {
			return nil, 0, 0, l17.ErrBusy
		}
		trades += tradeCount
		if _, e := l17.Wallet(token); e != nil {
			continue
		}
		values := make([]*big.Int, 4)
		for i, raw := range []string{spent, received, realized, holdings} {
			v, ok := new(big.Int).SetString(raw, 10)
			if !ok {
				return nil, 0, 0, fmt.Errorf("devreads: noncanonical position amount %q", raw)
			}
			values[i] = v
		}
		cost := new(big.Int).Sub(values[0], new(big.Int).Sub(values[1], values[2]))
		if cost.Sign() < 0 || values[3].Sign() < 0 || (values[3].Sign() == 0 && cost.Sign() != 0) {
			return nil, 0, 0, fmt.Errorf("devreads: corrupt folded basis for %s", token)
		}
		s := states[token]
		if s == nil {
			s = &tokenState{known: new(big.Int), cost: new(big.Int), realized: new(big.Int)}
			states[token] = s
		}
		s.known.Add(s.known, values[3])
		s.cost.Add(s.cost, cost)
		s.realized.Add(s.realized, values[2])
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	rows.Close()
	var maxBlock int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(block_number), 0) FROM pnl.user_trades WHERE user_address = $1`, wallet).Scan(&maxBlock); err != nil {
		return nil, 0, 0, fmt.Errorf("devreads: trade height: %w", err)
	}
	return states, trades, maxBlock, nil
}

// indexerCursor returns the dev indexer's last processed Paxeer block, or 0
// when the indexer has not written a cursor (or its schema is absent).
func indexerCursor(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, `SAVEPOINT devreads_cursor`); err != nil {
		return 0, err
	}
	var height int64
	err := tx.QueryRow(ctx, `SELECT last_processed_block FROM indexer.cursors WHERE chain_id = 125`).Scan(&height)
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		height = 0
	case errors.As(err, &pgErr) && pgErr.Code == "42P01":
		if _, e := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT devreads_cursor`); e != nil {
			return 0, e
		}
		height = 0
	default:
		return 0, fmt.Errorf("devreads: indexer cursor: %w", err)
	}
	return height, nil
}

// watermark anchors the projection to a real Paxeer block. The height is the
// dev indexer's processed height, raised to the wallet's newest folded trade
// when the webhook ran ahead of (or without) the cursor. When neither exists,
// or the node no longer serves that block (the public RPC is pruned), the
// node's head is used. The projection revision is 1 + the wallet's folded trade count:
// the dev fold is append-only, so it only grows when the wallet's data changes.
func (r Reader) watermark(ctx context.Context, cursor, maxBlock, trades int64) (l17.Watermark, error) {
	height := cursor
	if maxBlock > height {
		height = maxBlock
	}
	var block Block
	found := false
	var err error
	if height > 0 {
		block, found, err = r.Chain.BlockAt(ctx, uint64(height))
		if err != nil {
			return l17.Watermark{}, err
		}
	}
	if !found {
		block, err = r.Chain.Head(ctx)
		if err != nil {
			return l17.Watermark{}, err
		}
		if block.Number < uint64(maxBlock) {
			return l17.Watermark{}, fmt.Errorf("%w: node head %d behind folded trade %d", ErrChainUnavailable, block.Number, maxBlock)
		}
	}
	return l17.Watermark{
		ChainID:            "125",
		Height:             strconv.FormatUint(block.Number, 10),
		BlockHash:          strings.ToLower(block.Hash),
		ProjectionRevision: strconv.FormatInt(trades+1, 10),
		ObservedAt:         block.Timestamp.UTC().Format(observedLayout),
		Integrity:          "healthy",
	}, nil
}
