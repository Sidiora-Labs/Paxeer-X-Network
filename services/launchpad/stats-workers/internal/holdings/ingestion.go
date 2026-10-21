package holdings

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strconv"
	"strings"
)

func TransferFromLog(log types.Log) (Transfer, error) {
	signature := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	if log.Removed || len(log.Topics) != 3 || log.Topics[0] != signature || len(log.Data) != 32 {
		return Transfer{}, ErrIntegrity
	}
	for _, topic := range log.Topics[1:] {
		for _, b := range topic[:12] {
			if b != 0 {
				return Transfer{}, ErrIntegrity
			}
		}
	}
	from := "0x" + hex.EncodeToString(log.Topics[1][12:])
	to := "0x" + hex.EncodeToString(log.Topics[2][12:])
	return (Transfer{ChainID: "125", Token: strings.ToLower(log.Address.Hex()), TransactionHash: log.TxHash.Hex(), BlockHash: log.BlockHash.Hex(), Height: strconv.FormatUint(log.BlockNumber, 10), TransactionIndex: strconv.FormatUint(uint64(log.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(log.Index), 10), From: from, To: to, Amount: new(big.Int).SetBytes(log.Data).String()}).Normalize()
}
func (r *Repository) ApplyCanonicalTransfer(ctx context.Context, event Transfer, stream string) (result ApplyResult, err error) {
	defer func() {
		if err != nil && !(result.Incident && errors.Is(err, ErrIntegrity)) {
			r.observeFailure(ctx, event, err)
		}
	}()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	result, err = r.ApplyCanonicalTransferTx(ctx, tx, event, stream)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	r.observer.Committed(ctx, event, result)
	if result.Incident {
		return result, ErrIntegrity
	}
	return result, nil
}
func (r *Repository) ApplyCanonicalTransferTx(ctx context.Context, tx pgx.Tx, event Transfer, stream string) (ApplyResult, error) {
	e, err := event.Normalize()
	if err != nil {
		return ApplyResult{}, err
	}
	if tx == nil || stream == "" || len(stream) > 128 {
		return ApplyResult{}, ErrIntegrity
	}
	var emitter, block string
	var height, index, logIndex string
	var topicBytes, data []byte
	err = tx.QueryRow(ctx, `SELECT r.emitter,r.block_hash,r.height::text,r.transaction_index::text,r.log_index::text,r.topics,r.data FROM indexer.indexer_raw_logs_v1 r JOIN indexer.indexer_token_transfers_v1 t ON t.chain_id=r.chain_id AND t.transaction_hash=r.transaction_hash AND t.log_index=r.log_index JOIN indexer.indexer_canonical_creations_v1 k ON k.token=t.token JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.height=r.height AND c.block_hash=r.block_hash AND c.stream_id=$3 WHERE r.chain_id=125 AND r.transaction_hash=$1 AND r.log_index=$2::bigint AND c.fetch_complete AND c.required_decode_complete AND t.token=$4 AND t.from_address=$5 AND t.to_address=$6 AND t.amount_raw=$7::numeric AND t.height=r.height AND t.transaction_index=r.transaction_index AND t.block_hash=r.block_hash AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_watermarks_v1 w WHERE w.chain_id=125 AND w.stream_id=$3 AND w.typed_complete_height>=r.height) AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$3 AND a.previous_height<r.height AND a.last_complete_height>=r.height)`, e.TransactionHash, e.LogIndex, stream, e.Token, e.From, e.To, e.Amount).Scan(&emitter, &block, &height, &index, &logIndex, &topicBytes, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, ErrIntegrity
	}
	if err != nil {
		return ApplyResult{}, err
	}
	var topics []string
	if err = json.Unmarshal(topicBytes, &topics); err != nil {
		return ApplyResult{}, err
	}
	signature := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	if emitter != e.Token || block != e.BlockHash || height != e.Height || index != e.TransactionIndex || logIndex != e.LogIndex || len(topics) != 3 || topics[0] != signature.Hex() || len(data) != 32 {
		return ApplyResult{}, ErrIntegrity
	}
	expectedFrom := "0x" + strings.Repeat("0", 24) + e.From[2:]
	expectedTo := "0x" + strings.Repeat("0", 24) + e.To[2:]
	if strings.ToLower(topics[1]) != expectedFrom || strings.ToLower(topics[2]) != expectedTo || new(big.Int).SetBytes(data).String() != e.Amount {
		return ApplyResult{}, ErrIntegrity
	}
	return r.ApplyTx(ctx, tx, e)
}
func (r *Repository) enrichActorTx(ctx context.Context, tx pgx.Tx, event Transfer, actor ChainActor) error {
	e, err := event.Normalize()
	if err != nil {
		return err
	}
	provenance, err := RecipientProvenance(e)
	if err != nil {
		return err
	}
	provenance, err = provenance.WithActor(actor)
	if err != nil {
		return err
	}
	var digest string
	if err = tx.QueryRow(ctx, "SELECT digest FROM "+r.schema+".transfers WHERE identity=$1 FOR UPDATE", e.Identity()).Scan(&digest); err != nil {
		return err
	}
	if digest != e.Digest() {
		return ErrConflict
	}
	var priorBody []byte
	var prior Provenance
	if err = tx.QueryRow(ctx, "SELECT payload FROM "+r.schema+".provenance WHERE identity=$1 FOR UPDATE", e.Identity()).Scan(&priorBody); err != nil {
		return err
	}
	if err = json.Unmarshal(priorBody, &prior); err != nil {
		return err
	}
	if err = ValidateProvenance(e, prior); err != nil {
		return err
	}
	if prior.Actor != nil {
		if provenance.Actor == nil || *prior.Actor != *provenance.Actor {
			return ErrConflict
		}
		return nil
	}
	raw, err := json.Marshal(provenance)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".provenance SET payload=$2 WHERE identity=$1", e.Identity(), raw); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET revision=revision+1 WHERE token=$1", e.Token)
	return err
}

func (r *Repository) ImportLegacyRowsTx(ctx context.Context, tx pgx.Tx, pool string) error {
	pool, err := Address(pool)
	if err != nil {
		return err
	}
	if tx == nil {
		return ErrIntegrity
	}
	after := ""
	for {
		rows, queryErr := tx.Query(ctx, "SELECT holder_address,balance FROM stats.pool_holders WHERE pool_address=$1 AND holder_address>$2 ORDER BY holder_address LIMIT 1000", pool, after)
		if queryErr != nil {
			return queryErr
		}
		type legacyRow struct{ address, raw string }
		batch := make([]legacyRow, 0, 1000)
		for rows.Next() {
			var v legacyRow
			if err = rows.Scan(&v.address, &v.raw); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, v)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, v := range batch {
			address, addressErr := Address(v.address)
			_, amountErr := UInt(v.raw)
			reason := "legacy_swap_projection_not_authoritative"
			var numeric, normalized any
			if addressErr != nil {
				reason = "malformed_address"
			} else {
				normalized = address
				if amountErr != nil {
					reason = "malformed_or_negative_balance"
				} else {
					numeric = v.raw
				}
			}
			if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".legacy_rows(pool,address,raw,normalized_address,numeric_balance,reason) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(pool,address) DO UPDATE SET raw=excluded.raw,normalized_address=excluded.normalized_address,numeric_balance=excluded.numeric_balance,reason=excluded.reason", pool, v.address, v.raw, normalized, numeric, reason); err != nil {
				return err
			}
		}
		after = batch[len(batch)-1].address
	}
}

func (r *Repository) EnrichCanonicalActorTx(ctx context.Context, tx pgx.Tx, event Transfer, fill accounting.FillV1, stream string) error {
	if tx == nil || stream == "" || len(stream) > 128 {
		return ErrIntegrity
	}
	e, err := event.Normalize()
	if err != nil {
		return err
	}
	if err = accounting.ValidateFillV1(fill); err != nil {
		return err
	}
	if fill.Position.ChainID != e.ChainID || fill.Position.TransactionHash != e.TransactionHash || fill.Position.BlockHash != e.BlockHash || fill.Position.Height != e.Height || fill.Position.TransactionIndex != e.TransactionIndex || fill.Token.Address != e.Token {
		return ErrConflict
	}
	raw, err := json.Marshal(fill)
	if err != nil {
		return err
	}
	digest, err := accounting.FillDigest(fill)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT f.source_fact_digest=$2 AND f.payload=$3::jsonb AND r.block_hash=$4 AND r.height=$5::bigint AND r.emitter=$6 AND c.fetch_complete AND c.required_decode_complete AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits_v1 a WHERE a.chain_id=125 AND a.stream_id=$7 AND a.previous_height<r.height AND a.last_complete_height>=r.height) FROM indexer.indexer_accounting_fills_v1 f JOIN indexer.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.height=r.height AND c.block_hash=r.block_hash AND c.stream_id=$7 WHERE f.source_fill_id=$1`, fill.SourceFillID, string(digest), raw, fill.Position.BlockHash, fill.Position.Height, fill.ActualEmitter, stream).Scan(&same)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
		return ErrIntegrity
	}
	if err != nil {
		return err
	}
	actor := fill.Attribution.TransactionSender
	if fill.Attribution.RouterCaller != nil {
		actor = *fill.Attribution.RouterCaller
	}
	return r.enrichActorTx(ctx, tx, e, ChainActor{ChainID: e.ChainID, TransactionHash: e.TransactionHash, BlockHash: e.BlockHash, Address: actor})
}

func (r *Repository) DiscoverCanonical(ctx context.Context, token, stream string) error {
	token, err := Address(token)
	if err != nil {
		return err
	}
	if stream == "" || len(stream) > 128 {
		return ErrIntegrity
	}
	var payload []byte
	var creation, creationTransaction string
	err = r.pool.QueryRow(ctx, `SELECT k.payload,k.height::text,k.transaction_hash FROM indexer.indexer_canonical_creations_v1 k JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=125 AND r.transaction_hash=k.transaction_hash AND r.log_index=k.log_index AND r.block_hash=k.block_hash AND r.height=k.height JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=r.chain_id AND c.height=r.height AND c.block_hash=r.block_hash AND c.stream_id=$2 WHERE k.token=$1 AND c.fetch_complete AND c.required_decode_complete`, token, stream).Scan(&payload, &creation, &creationTransaction)
	if err != nil {
		return err
	}
	var fact struct {
		Metadata *struct {
			Decimals    *uint8  `json:"decimals"`
			TotalSupply *string `json:"totalSupply"`
		} `json:"metadata"`
	}
	if err = json.Unmarshal(payload, &fact); err != nil {
		return err
	}
	if fact.Metadata == nil || fact.Metadata.Decimals == nil || fact.Metadata.TotalSupply == nil {
		return ErrUnknownToken
	}
	if _, err = UInt(*fact.Metadata.TotalSupply); err != nil {
		return err
	}
	if err = r.Discover(ctx, token, *fact.Metadata.Decimals, creation, false); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, "UPDATE "+r.schema+".tokens SET expected_initial_supply=$2,creation_transaction_hash=$3 WHERE token=$1 AND (expected_initial_supply IS NULL OR (expected_initial_supply=$2::numeric AND creation_transaction_hash=$3))", token, *fact.Metadata.TotalSupply, creationTransaction)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

type CanonicalBatch struct {
	Applied  int
	More     bool
	Revision int64
	Coverage string
	Incident bool
}

func (r *Repository) CatchUpCanonical(ctx context.Context, token, stream string, limit int) (CanonicalBatch, error) {
	return r.CatchUpCanonicalAt(ctx, token, stream, "", limit)
}
func (r *Repository) CatchUpCanonicalAt(ctx context.Context, token, stream, through string, limit int) (out CanonicalBatch, err error) {
	if limit < 1 || limit > 1000 || stream == "" || len(stream) > 128 {
		return out, ErrIntegrity
	}
	token, err = Address(token)
	if err != nil {
		return out, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var creation string
	var previous []byte
	if err = tx.QueryRow(ctx, "SELECT creation_height::text,last_event,revision,coverage FROM "+r.schema+".tokens WHERE token=$1 FOR UPDATE", token).Scan(&creation, &previous, &out.Revision, &out.Coverage); err != nil {
		return out, err
	}
	if out.Coverage == "quarantined" {
		out.Incident = true
		return out, tx.Commit(ctx)
	}
	var watermark int64
	if err = tx.QueryRow(ctx, "SELECT typed_complete_height FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1", stream).Scan(&watermark); err != nil {
		return out, err
	}
	if through != "" {
		if _, err = UInt(through); err != nil {
			return out, err
		}
		bound, parseErr := strconv.ParseInt(through, 10, 64)
		if parseErr != nil || bound < 0 || bound > watermark {
			return out, ErrOrder
		}
		watermark = bound
	}
	priorCoverage := out.Coverage
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET coverage='incomplete' WHERE token=$1", token); err != nil {
		return out, err
	}
	out.Coverage = "incomplete"
	height, ti, li := "-1", "-1", "-1"
	if len(previous) > 0 {
		var e Transfer
		if err = json.Unmarshal(previous, &e); err != nil {
			return out, err
		}
		height, ti, li = e.Height, e.TransactionIndex, e.LogIndex
		if n, parseErr := strconv.ParseInt(height, 10, 64); parseErr != nil || n > watermark {
			return out, ErrOrder
		}
	}
	rows, err := tx.Query(ctx, `SELECT token,from_address,to_address,amount_raw::text,height::text,transaction_index::text,log_index::text,transaction_hash,block_hash FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND height>=$2::numeric AND height<=$3 AND (height,transaction_index,log_index)>($4::numeric,$5::numeric,$6::numeric) ORDER BY height,transaction_index,log_index LIMIT $7`, token, creation, watermark, height, ti, li, limit+1)
	if err != nil {
		return out, err
	}
	events := make([]Transfer, 0, limit+1)
	for rows.Next() {
		e := Transfer{ChainID: "125"}
		if err = rows.Scan(&e.Token, &e.From, &e.To, &e.Amount, &e.Height, &e.TransactionIndex, &e.LogIndex, &e.TransactionHash, &e.BlockHash); err != nil {
			rows.Close()
			return out, err
		}
		events = append(events, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(events) > limit {
		out.More = true
		events = events[:limit]
	}
	results := make([]ApplyResult, 0, len(events))
	for _, e := range events {
		result, applyErr := r.ApplyCanonicalTransferTx(ctx, tx, e, stream)
		if applyErr != nil {
			return out, applyErr
		}
		results = append(results, result)
		out.Revision, out.Coverage = result.Revision, result.Coverage
		if result.Incident {
			out.Incident = true
			break
		}
		if !result.Duplicate {
			out.Applied++
		}
	}
	if !out.More && !out.Incident {
		var count string
		if err = tx.QueryRow(ctx, "SELECT count(DISTINCT height)::text FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height BETWEEN $2::numeric AND $3 AND fetch_complete AND required_decode_complete", stream, creation, watermark).Scan(&count); err != nil {
			return out, err
		}
		start, _ := UInt(creation)
		expected := new(big.Int).Sub(big.NewInt(watermark), start)
		expected.Add(expected, big.NewInt(1))
		if expected.Sign() > 0 && count == expected.String() {
			var genesis bool
			if err = tx.QueryRow(ctx, "SELECT t.expected_initial_supply IS NOT NULL AND t.expected_initial_supply=COALESCE((SELECT sum(CASE WHEN x.from_address=$2 THEN x.amount_raw WHEN x.to_address=$2 THEN -x.amount_raw ELSE 0 END) FROM indexer.indexer_token_transfers_v1 x WHERE x.chain_id=125 AND x.token=t.token AND x.transaction_hash=t.creation_transaction_hash),0) FROM "+r.schema+".tokens t WHERE t.token=$1", token, Zero).Scan(&genesis); err != nil {
				return out, err
			}
			if genesis {
				if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET coverage='complete' WHERE token=$1", token); err != nil {
					return out, err
				}
				out.Coverage = "complete"
			}
		}
	}
	if priorCoverage != out.Coverage {
		if err = tx.QueryRow(ctx, "UPDATE "+r.schema+".tokens SET revision=revision+1 WHERE token=$1 RETURNING revision", token).Scan(&out.Revision); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		r.observer.Failed(ctx)
		return out, err
	}
	for i, result := range results {
		r.observer.Committed(ctx, events[i], result)
	}
	return out, nil
}
