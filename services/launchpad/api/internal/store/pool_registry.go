package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
)

type TokenCreationMetadata struct {
	Name        string                    `json:"name"`
	Symbol      string                    `json:"symbol"`
	Decimals    uint8                     `json:"decimals"`
	TotalSupply string                    `json:"totalSupply"`
	Position    accounting.SourcePosition `json:"position"`
}

type PoolRegistryRow struct {
	Pool           string
	Token          string
	Creator        string
	Position       accounting.SourcePosition
	BlockTimestamp int64
	Metadata       *TokenCreationMetadata
}

type PoolReserves struct {
	VirtualUsdl UInt256
	RealUsdl    UInt256
	Token       UInt256
	Height      int64
}

func (s *Store) PoolRegistry(ctx context.Context, pools []string) (map[string]PoolRegistryRow, error) {
	if len(pools) == 0 || len(pools) > 100 {
		return nil, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `SELECT pool,token,creator,height::text,block_hash,transaction_hash,transaction_index::text,log_index::text,block_timestamp,payload FROM indexer.indexer_canonical_creations_v1 WHERE pool=ANY($1)`, pools)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PoolRegistryRow{}
	for rows.Next() {
		var r PoolRegistryRow
		var payload []byte
		if err = rows.Scan(&r.Pool, &r.Token, &r.Creator, &r.Position.Height, &r.Position.BlockHash, &r.Position.TransactionHash, &r.Position.TransactionIndex, &r.Position.LogIndex, &r.BlockTimestamp, &payload); err != nil {
			return nil, err
		}
		r.Position.ChainID = "125"
		var fact struct {
			Metadata *TokenCreationMetadata `json:"metadata"`
		}
		if len(payload) > PublicJSONMaxBytes || json.Unmarshal(payload, &fact) != nil || accounting.ValidateSourcePosition(r.Position) != nil || r.BlockTimestamp <= 0 {
			return nil, ErrDataIntegrity
		}
		for _, address := range []string{r.Pool, r.Token, r.Creator} {
			if normalized, addressErr := Address(address); addressErr != nil || normalized != address {
				return nil, ErrDataIntegrity
			}
		}
		if fact.Metadata != nil {
			if _, supplyErr := ParseUInt256(fact.Metadata.TotalSupply); supplyErr != nil || accounting.ValidateSourcePosition(fact.Metadata.Position) != nil {
				return nil, ErrDataIntegrity
			}
		}
		r.Metadata = fact.Metadata
		out[r.Pool] = r
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) PoolReserves(ctx context.Context, pools []string) (map[string]PoolReserves, error) {
	if len(pools) == 0 || len(pools) > 100 {
		return nil, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (lower(p.pool_address)) lower(p.pool_address),st.virtual_reserve,st.real_reserve,st.token_reserve,st.block_number FROM indexer.pools p JOIN indexer.pool_state_snapshots st ON st.pool_id=p.pool_id WHERE lower(p.pool_address)=ANY($1) ORDER BY lower(p.pool_address),st.block_number DESC,st.log_index DESC`, pools)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PoolReserves{}
	for rows.Next() {
		var pool, virtual, real, token string
		var r PoolReserves
		if err = rows.Scan(&pool, &virtual, &real, &token, &r.Height); err != nil {
			return nil, err
		}
		if r.VirtualUsdl, err = ParseUInt256(virtual); err != nil {
			return nil, ErrDataIntegrity
		}
		if r.RealUsdl, err = ParseUInt256(real); err != nil {
			return nil, ErrDataIntegrity
		}
		if r.Token, err = ParseUInt256(token); err != nil {
			return nil, ErrDataIntegrity
		}
		out[pool] = r
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *AccountingReadModel) ObservedFeeBps(ctx context.Context, pools []string) (map[string]int, error) {
	if s == nil || s.pool == nil || len(pools) == 0 || len(pools) > 100 {
		return nil, ErrAccountingNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (pool) pool,fill->'execution'->>'feeBps' FROM `+pgx.Identifier{s.config.StatsSchema}.Sanitize()+`.accounting_market_commits_v1 WHERE pool=ANY($1) AND projection_version=$2 AND fill->'execution'->>'quality'='qualified_execution' AND fill->'execution'->>'feeBps' IS NOT NULL ORDER BY pool,sequence DESC`, pools, s.config.ProjectionVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var pool, raw string
		if err = rows.Scan(&pool, &raw); err != nil {
			return nil, err
		}
		fee, parseErr := strconv.Atoi(raw)
		if parseErr != nil || strconv.Itoa(fee) != raw || fee < 0 || fee > 10000 {
			return nil, ErrDataIntegrity
		}
		out[pool] = fee
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
