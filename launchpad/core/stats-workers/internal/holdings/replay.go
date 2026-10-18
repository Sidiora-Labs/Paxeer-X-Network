package holdings

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"strconv"
	"strings"
)

type PersistedCheckpoint struct {
	Token             string    `json:"tokenAddress"`
	ProjectionVersion string    `json:"projectionVersion"`
	Height            string    `json:"height"`
	BlockHash         string    `json:"blockHash"`
	CoverageStart     string    `json:"coverageStart"`
	Revision          int64     `json:"revision"`
	Supply            string    `json:"supplyRaw"`
	Digest            string    `json:"digest"`
	Position          *Transfer `json:"lastTransferPosition"`
}

func (r *Repository) checkpointTx(ctx context.Context, tx pgx.Tx, token, height, blockHash, version string) (out PersistedCheckpoint, err error) {
	if version != "holdings_v1" || !hexHash.MatchString(blockHash) || strings.Trim(blockHash[2:], "0") == "" {
		return out, ErrIntegrity
	}
	token, err = Address(token)
	if err != nil {
		return out, err
	}
	through, err := UInt(height)
	if err != nil {
		return out, err
	}
	blockHash = strings.ToLower(blockHash)
	out = PersistedCheckpoint{Token: token, Height: height, BlockHash: blockHash, ProjectionVersion: version}
	var position []byte
	var coverage string
	err = tx.QueryRow(ctx, "SELECT creation_height::text,supply::text,revision,last_event,coverage FROM "+r.schema+".tokens WHERE token=$1 FOR UPDATE", token).Scan(&out.CoverageStart, &out.Supply, &out.Revision, &position, &coverage)
	if err != nil {
		return out, err
	}
	if coverage == "quarantined" {
		return out, ErrIntegrity
	}
	start, _ := UInt(out.CoverageStart)
	if start.Cmp(through) > 0 {
		return out, ErrOrder
	}
	if len(position) > 0 {
		var e Transfer
		if err = json.Unmarshal(position, &e); err != nil {
			return out, err
		}
		out.Position = &e
		last, _ := UInt(e.Height)
		if last.Cmp(through) > 0 || (e.Height == height && e.BlockHash != blockHash) {
			return out, ErrConflict
		}
	}
	digest, err := NewCheckpointDigest(token, out.Supply, height)
	if err != nil {
		return out, err
	}
	sum := new(big.Int)
	rows, err := tx.Query(ctx, "SELECT address,balance::text FROM "+r.schema+".balances WHERE token=$1 ORDER BY address", token)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var address, raw string
		if err = rows.Scan(&address, &raw); err != nil {
			rows.Close()
			return out, err
		}
		if err = digest.Add(address, raw); err != nil {
			rows.Close()
			return out, err
		}
		value, _ := UInt(raw)
		sum.Add(sum, value)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if sum.String() != out.Supply {
		return out, ErrIntegrity
	}
	out.Digest = digest.Hex()
	watermark, _ := json.Marshal(map[string]any{"chainId": "125", "height": height, "blockHash": blockHash, "lastTransferPosition": out.Position})
	var prior, priorBlock string
	var priorRevision int64
	err = tx.QueryRow(ctx, "SELECT digest,position->>'blockHash',revision FROM "+r.schema+".checkpoints WHERE token=$1 AND projection_version=$2 AND height=$3", token, version, height).Scan(&prior, &priorBlock, &priorRevision)
	if err == nil {
		if prior != out.Digest || priorBlock != blockHash {
			return out, ErrConflict
		}
		out.Revision = priorRevision
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".checkpoints(token,projection_version,height,revision,coverage_start,digest,position) VALUES($1,$2,$3,$4,$5,$6,$7)", token, version, height, out.Revision, out.CoverageStart, out.Digest, watermark)
	return out, err
}

func (r *Repository) Checkpoint(ctx context.Context, token, height, blockHash, version string) (PersistedCheckpoint, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	out, err := r.checkpointTx(ctx, tx, token, height, blockHash, version)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (r *Repository) RebuildFromCanonical(ctx context.Context, source *pgxpool.Pool, stream, token string, decimals uint8, creation, through, version string) (PersistedCheckpoint, error) {
	if source == nil || stream == "" || len(stream) > 128 || version != "holdings_v1" {
		return PersistedCheckpoint{}, ErrIntegrity
	}
	token, err := Address(token)
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	for _, value := range []string{creation, through} {
		if _, err = UInt(value); err != nil {
			return PersistedCheckpoint{}, err
		}
	}
	first, err := strconv.ParseInt(creation, 10, 64)
	if err != nil || first < 0 {
		return PersistedCheckpoint{}, ErrOrder
	}
	last, err := strconv.ParseInt(through, 10, 64)
	if err != nil || last < first {
		return PersistedCheckpoint{}, ErrOrder
	}
	src, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	defer src.Rollback(context.WithoutCancel(ctx))
	var matches bool
	var initialSupply, creationTransaction string
	err = src.QueryRow(ctx, "SELECT height=$2::bigint AND (payload->'metadata'->>'decimals')::integer=$3,payload->'metadata'->>'totalSupply',transaction_hash FROM indexer.indexer_canonical_creations_v1 WHERE token=$1", token, first, int(decimals)).Scan(&matches, &initialSupply, &creationTransaction)
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	if !matches {
		return PersistedCheckpoint{}, ErrConflict
	}
	if _, err = UInt(initialSupply); err != nil {
		return PersistedCheckpoint{}, err
	}
	var count string
	err = src.QueryRow(ctx, "SELECT count(DISTINCT height)::text FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height BETWEEN $2 AND $3 AND fetch_complete AND required_decode_complete", stream, first, last).Scan(&count)
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	expected := new(big.Int).Sub(big.NewInt(last), big.NewInt(first))
	expected.Add(expected, big.NewInt(1))
	if count != expected.String() {
		return PersistedCheckpoint{}, ErrIntegrity
	}
	var hash string
	if err = src.QueryRow(ctx, "SELECT block_hash FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete", stream, last).Scan(&hash); err != nil {
		return PersistedCheckpoint{}, err
	}
	if err = r.Discover(ctx, token, decimals, creation, false); err != nil {
		return PersistedCheckpoint{}, err
	}
	var coverage string
	var liveDiscovery bool
	if err = r.pool.QueryRow(ctx, "SELECT coverage,expected_initial_supply IS NOT NULL OR creation_transaction_hash IS NOT NULL FROM "+r.schema+".tokens WHERE token=$1", token).Scan(&coverage, &liveDiscovery); err != nil {
		return PersistedCheckpoint{}, err
	}
	if coverage != "incomplete" || liveDiscovery {
		return PersistedCheckpoint{}, ErrConflict
	}
	signature := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	rows, err := src.Query(ctx, `SELECT r.emitter,r.block_hash,r.transaction_hash,r.height,r.transaction_index,r.log_index,r.topics,r.data FROM indexer.indexer_raw_logs_v1 r JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.height=r.height AND c.block_hash=r.block_hash AND c.stream_id=$1 WHERE r.chain_id=125 AND r.emitter=$2 AND r.height BETWEEN $3 AND $4 AND r.topics->>0=$5 AND c.fetch_complete AND c.required_decode_complete ORDER BY r.height,r.transaction_index,r.log_index`, stream, token, first, last, signature.Hex())
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	genesis := new(big.Int)
	var replayed int64
	for rows.Next() {
		var address, block, txHash string
		var h, ti, li uint64
		var topicBytes, data []byte
		if err = rows.Scan(&address, &block, &txHash, &h, &ti, &li, &topicBytes, &data); err != nil {
			rows.Close()
			return PersistedCheckpoint{}, err
		}
		var topicStrings []string
		if err = json.Unmarshal(topicBytes, &topicStrings); err != nil {
			rows.Close()
			return PersistedCheckpoint{}, err
		}
		topics := make([]common.Hash, len(topicStrings))
		for i, s := range topicStrings {
			if !hexHash.MatchString(s) {
				rows.Close()
				return PersistedCheckpoint{}, ErrIntegrity
			}
			topics[i] = common.HexToHash(s)
		}
		e, decodeErr := TransferFromLog(types.Log{Address: common.HexToAddress(address), BlockHash: common.HexToHash(block), TxHash: common.HexToHash(txHash), BlockNumber: h, TxIndex: uint(ti), Index: uint(li), Topics: topics, Data: data})
		if decodeErr != nil {
			rows.Close()
			return PersistedCheckpoint{}, decodeErr
		}
		if e.TransactionHash == creationTransaction {
			amount, _ := UInt(e.Amount)
			if e.From == Zero {
				genesis.Add(genesis, amount)
			}
			if e.To == Zero {
				genesis.Sub(genesis, amount)
			}
		}
		replayed++
		if _, err = r.Apply(ctx, e); err != nil {
			rows.Close()
			return PersistedCheckpoint{}, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return PersistedCheckpoint{}, err
	}
	if genesis.String() != initialSupply {
		return PersistedCheckpoint{}, ErrIntegrity
	}
	if err = src.Commit(ctx); err != nil {
		return PersistedCheckpoint{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return PersistedCheckpoint{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var retained int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+".transfers WHERE token=$1", token).Scan(&retained); err != nil {
		return PersistedCheckpoint{}, err
	}
	if retained != replayed {
		return PersistedCheckpoint{}, ErrConflict
	}
	out, err := r.checkpointTx(ctx, tx, token, through, hash, version)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET coverage='complete' WHERE token=$1 AND coverage='incomplete'", token); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
