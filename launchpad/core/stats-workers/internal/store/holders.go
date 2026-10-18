package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
)

// HolderRow is a stats.pool_holders row. JSON tags match the TS drizzle property
// names (camelCase) for response parity.
type HolderRow struct {
	PoolAddress   string `json:"poolAddress"`
	HolderAddress string `json:"holderAddress"`
	Balance       string `json:"balance"`
	PctOfSupply   string `json:"pctOfSupply"`
	LastUpdated   int64  `json:"lastUpdated"`
}

// computePctOfSupply returns balance basis points of total supply, byte-identical
// to the TS computePctOfSupply ((balance * 10000) / TOTAL_SUPPLY_RAW).
func computePctOfSupply(balance string) (string, error) {
	return shareddb.BigintMulDiv(balance, "10000", shareddb.TotalSupplyRaw)
}

// ApplyHolderDelta applies a swap's token-balance change to the sender's holder
// row under the per-holder advisory lock, deleting the row when the balance hits
// zero (parity with HolderTracker.processSwap). It reports whether a holder
// mutation was applied: false only for the "new holder selling" case, in which
// the TS returns early and skips the holder-stats refresh.
func (s *Store) ApplyHolderDelta(ctx context.Context, poolAddress, sender string, isBuy bool, amountIn, amountOut string, now int64) (bool, error) {
	tokenDelta := amountIn
	if isBuy {
		tokenDelta = amountOut
	}

	applied := false
	err := s.withXactLock(ctx, fmt.Sprintf("holder-%s-%s", poolAddress, sender), func(tx pgx.Tx) error {
		var existing string
		err := tx.QueryRow(ctx, `
			SELECT balance FROM stats.pool_holders
			WHERE pool_address = $1 AND holder_address = $2`, poolAddress, sender).Scan(&existing)

		var newBalance string
		switch {
		case err == nil:
			if isBuy {
				newBalance, err = shareddb.BigintAdd(existing, tokenDelta)
			} else {
				newBalance, err = shareddb.BigintSub(existing, tokenDelta)
			}
			if err != nil {
				return fmt.Errorf("store: holder balance math: %w", err)
			}
		case errors.Is(err, pgx.ErrNoRows):
			if !isBuy {
				return nil // new holder selling — no-op (applied stays false)
			}
			newBalance = tokenDelta
		default:
			return fmt.Errorf("store: read holder: %w", err)
		}

		pct, err := computePctOfSupply(newBalance)
		if err != nil {
			return fmt.Errorf("store: holder pct: %w", err)
		}

		bal, ok := new(big.Int).SetString(newBalance, 10)
		if !ok {
			return fmt.Errorf("store: invalid holder balance %q", newBalance)
		}

		if bal.Sign() <= 0 {
			if _, err := tx.Exec(ctx, `
				DELETE FROM stats.pool_holders
				WHERE pool_address = $1 AND holder_address = $2`, poolAddress, sender); err != nil {
				return fmt.Errorf("store: delete holder: %w", err)
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO stats.pool_holders (pool_address, holder_address, balance, pct_of_supply, last_updated)
				VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (pool_address, holder_address)
				DO UPDATE SET balance = excluded.balance, pct_of_supply = excluded.pct_of_supply, last_updated = excluded.last_updated`,
				poolAddress, sender, newBalance, pct, now); err != nil {
				return fmt.Errorf("store: upsert holder: %w", err)
			}
		}
		applied = true
		return nil
	})
	return applied, err
}

// RefreshPoolHolderStats recomputes holderCount, top-10 concentration, creator
// holdings, and the risk rating for a pool, writing them to pool_stats. Ports
// HolderTracker.refreshPoolHolderStats. The top-10 query orders by the text
// `balance` column descending, byte-for-byte identical to the live drizzle query
// (desc(poolHolders.balance)) for strangler parity (invariant i2).
func (s *Store) RefreshPoolHolderStats(ctx context.Context, poolAddress string, now int64) error {
	var holderCount int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM stats.pool_holders WHERE pool_address = $1`, poolAddress).
		Scan(&holderCount); err != nil {
		return fmt.Errorf("store: count holders: %w", err)
	}

	rows, err := s.db.Query(ctx, `
		SELECT balance FROM stats.pool_holders
		WHERE pool_address = $1 ORDER BY balance DESC LIMIT 10`, poolAddress)
	if err != nil {
		return fmt.Errorf("store: top10 holders: %w", err)
	}
	top10Total := new(big.Int)
	for rows.Next() {
		var bal string
		if err := rows.Scan(&bal); err != nil {
			rows.Close()
			return fmt.Errorf("store: scan top10 holder: %w", err)
		}
		if v, ok := new(big.Int).SetString(bal, 10); ok {
			top10Total.Add(top10Total, v)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: top10 rows: %w", err)
	}

	totalSupply, _ := new(big.Int).SetString(shareddb.TotalSupplyRaw, 10)
	top10Concentration := "0"
	if totalSupply.Sign() > 0 {
		c := new(big.Int).Mul(top10Total, tenThousand)
		c.Quo(c, totalSupply)
		top10Concentration = c.String()
	}

	// Current pool stats: creator address + created_at + 24h unique traders.
	var (
		creatorAddress   *string
		createdAt        = now
		uniqueTraders24h int
	)
	var ca *string
	var cAt int64
	var ut int
	err = s.db.QueryRow(ctx, `
		SELECT creator_address, created_at, unique_traders_24h
		FROM stats.pool_stats WHERE pool_address = $1 LIMIT 1`, poolAddress).
		Scan(&ca, &cAt, &ut)
	switch {
	case err == nil:
		creatorAddress = ca
		createdAt = cAt
		uniqueTraders24h = ut
	case errors.Is(err, pgx.ErrNoRows):
		// defaults (creator nil, createdAt=now, traders 0) — UPDATE no-ops.
	default:
		return fmt.Errorf("store: read pool stats for holder refresh: %w", err)
	}

	creatorHoldingsPct := "0"
	if creatorAddress != nil {
		var creatorBal string
		err := s.db.QueryRow(ctx, `
			SELECT balance FROM stats.pool_holders
			WHERE pool_address = $1 AND holder_address = $2 LIMIT 1`,
			poolAddress, strings.ToLower(*creatorAddress)).Scan(&creatorBal)
		if err == nil {
			if totalSupply.Sign() > 0 {
				bv, _ := new(big.Int).SetString(creatorBal, 10)
				if bv != nil {
					c := new(big.Int).Mul(bv, tenThousand)
					c.Quo(c, totalSupply)
					creatorHoldingsPct = c.String()
				}
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("store: read creator holdings: %w", err)
		}
	}

	result := risk.Calculate(risk.Input{
		CreatedAt:          createdAt,
		Top10Concentration: top10Concentration,
		CreatorHoldingsPct: creatorHoldingsPct,
		UniqueTraders24h:   uniqueTraders24h,
		HolderCount:        holderCount,
	}, now)

	factorsJSON, err := json.Marshal(result.Factors)
	if err != nil {
		return fmt.Errorf("store: marshal risk factors: %w", err)
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE stats.pool_stats
		SET holder_count = $2, top10_concentration = $3, creator_holdings_pct = $4,
		    risk_rating = $5, risk_factors = $6, updated_at = $7
		WHERE pool_address = $1`,
		poolAddress, holderCount, top10Concentration, creatorHoldingsPct,
		result.Score, string(factorsJSON), now); err != nil {
		return fmt.Errorf("store: update holder stats: %w", err)
	}
	return nil
}

// ListHolders returns a page of holders ordered by balance descending (text
// ordering, parity with the live drizzle query). Ports the GET
// /stats/:pool/holders query.
func (s *Store) ListHolders(ctx context.Context, poolAddress string, limit, offset int) ([]HolderRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT pool_address, holder_address, balance, pct_of_supply, last_updated
		FROM stats.pool_holders
		WHERE pool_address = $1
		ORDER BY balance DESC
		LIMIT $2 OFFSET $3`, poolAddress, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list holders: %w", err)
	}
	defer rows.Close()
	return scanHolders(rows)
}

// CountHolders returns the total holder count for a pool.
func (s *Store) CountHolders(ctx context.Context, poolAddress string) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM stats.pool_holders WHERE pool_address = $1`, poolAddress).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count holders: %w", err)
	}
	return n, nil
}

// ListHoldersByBalance returns ALL holders of a pool ordered by balance
// descending (text ordering, parity). Used by the distribution / whales /
// analytics routes that aggregate in application code.
func (s *Store) ListHoldersByBalance(ctx context.Context, poolAddress string) ([]HolderRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT pool_address, holder_address, balance, pct_of_supply, last_updated
		FROM stats.pool_holders
		WHERE pool_address = $1
		ORDER BY balance DESC`, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("store: list holders by balance: %w", err)
	}
	defer rows.Close()
	return scanHolders(rows)
}

// GetHolderBalance returns a single holder's balance, or ("", false) if absent.
func (s *Store) GetHolderBalance(ctx context.Context, poolAddress, holderAddress string) (balance string, found bool, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT balance FROM stats.pool_holders
		WHERE pool_address = $1 AND holder_address = $2 LIMIT 1`, poolAddress, holderAddress).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get holder balance: %w", err)
	}
	return balance, true, nil
}

func scanHolders(rows pgx.Rows) ([]HolderRow, error) {
	out := []HolderRow{}
	for rows.Next() {
		var h HolderRow
		if err := rows.Scan(&h.PoolAddress, &h.HolderAddress, &h.Balance, &h.PctOfSupply, &h.LastUpdated); err != nil {
			return nil, fmt.Errorf("store: scan holder: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HolderBalance is one wallet's balance on the development holder ledger
// (stats.pool_holders), lowercased and aggregated per wallet.
type HolderBalance struct {
	Wallet     string
	BalanceRaw string
}

// FindPoolStats resolves a pool's stats row case-insensitively. The dev webhook
// consumers store event addresses verbatim, so a lowercased request address
// must still find a checksummed row. Returns (nil, nil) when absent.
func (s *Store) FindPoolStats(ctx context.Context, poolAddress string) (*PoolStatsRow, error) {
	row := s.db.QueryRow(ctx, `SELECT`+poolStatsColumns+`
		FROM stats.pool_stats WHERE lower(pool_address) = lower($1)
		ORDER BY pool_address LIMIT 1`, poolAddress)
	r, err := scanPoolStats(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: find pool stats: %w", err)
	}
	return r, nil
}

// ListHolderBalances returns one page of a pool's positive holder balances in
// numeric balance order (wallet ascending as tie-break), aggregated per
// lowercased wallet. Rows whose address or balance is not well formed are
// excluded so every returned entry is a valid 20-byte address and unsigned
// decimal integer.
func (s *Store) ListHolderBalances(ctx context.Context, poolAddress string, limit, offset int) ([]HolderBalance, error) {
	rows, err := s.db.Query(ctx, `
		SELECT lower(holder_address), sum(balance::numeric)::text
		FROM stats.pool_holders
		WHERE pool_address = $1
		  AND holder_address ~ '^0x[0-9a-fA-F]{40}$'
		  AND balance ~ '^[0-9]{1,78}$'
		GROUP BY lower(holder_address)
		HAVING sum(balance::numeric) > 0
		ORDER BY sum(balance::numeric) DESC, lower(holder_address) ASC
		LIMIT $2 OFFSET $3`, poolAddress, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list holder balances: %w", err)
	}
	defer rows.Close()
	out := []HolderBalance{}
	for rows.Next() {
		var h HolderBalance
		if err := rows.Scan(&h.Wallet, &h.BalanceRaw); err != nil {
			return nil, fmt.Errorf("store: scan holder balance: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
