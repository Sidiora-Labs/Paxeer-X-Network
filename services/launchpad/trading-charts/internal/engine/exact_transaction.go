package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

// ProcessExactFill is intentionally separate from the legacy spot-price API.
// It performs no network publication; a durable relay consumes chart_outbox.
func ProcessExactFill(ctx context.Context, st *store.ExactStore, f Fill, ownerEpoch int64) error {
	if _, e := f.Price(); e != nil {
		return e
	}
	if f.EmittingContract == "" || f.CodeEpoch == "" || f.PoolAddress == "" || f.TransactionHash == "" {
		return ErrInvalidFill
	}
	f.EmittingContract = strings.ToLower(f.EmittingContract)
	f.PoolAddress = strings.ToLower(f.PoolAddress)
	f.TransactionHash = strings.ToLower(f.TransactionHash)
	f.Sender = strings.ToLower(f.Sender)
	body, e := json.Marshal(f)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(body)
	return st.Transaction(ctx, store.Claim{ChainID: f.ChainID, Pool: f.PoolAddress, Emitter: f.EmittingContract, CodeEpoch: f.CodeEpoch, TxHash: f.TransactionHash, LogIndex: strconv.FormatUint(f.Position.LogIndex, 10), Epoch: ownerEpoch, Digest: digest[:], Fill: body}, func(tx pgx.Tx, q string) error {
		for _, interval := range ExactIntervals {
			start, e := BucketStart(f.BlockTimestamp, interval)
			if e != nil {
				return e
			}
			var previous []byte
			e = tx.QueryRow(ctx, `SELECT snapshot FROM `+q+`.chart_exact WHERE chain_id=$1 AND pool=$2 AND interval=$3 AND start=$4`, f.ChainID, f.PoolAddress, interval, start).Scan(&previous)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			old, e := decodeExact(previous)
			if e != nil {
				return e
			}
			newTrader := false
			if f.Sender != "" {
				ct, e := tx.Exec(ctx, `INSERT INTO `+q+`.chart_traders(chain_id,pool,interval,start,actor) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, f.ChainID, f.PoolAddress, interval, start, f.Sender)
				if e != nil {
					return e
				}
				newTrader = ct.RowsAffected() == 1
			}
			next, e := FoldExact(old, f, interval, newTrader)
			if e != nil {
				return e
			}
			snapshot, e := json.Marshal(next)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_exact(chain_id,pool,interval,start,snapshot) VALUES($1,$2,$3,$4,$5) ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET snapshot=EXCLUDED.snapshot`, f.ChainID, f.PoolAddress, interval, start, snapshot)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_outbox(chain_id,pool,interval,start,revision,payload) VALUES($1,$2,$3,$4,$5,$6)`, f.ChainID, f.PoolAddress, interval, start, strconv.FormatUint(next.Revision, 10), snapshot)
			if e != nil {
				return e
			}
		}
		return nil
	})
}

func ProcessExactFillTx(ctx context.Context, tx pgx.Tx, st *store.ExactStore, f Fill, ownerEpoch int64) error {
	if _, err := f.Price(); err != nil {
		return err
	}
	if st == nil || tx == nil || f.EmittingContract == "" || f.CodeEpoch == "" || f.PoolAddress == "" || f.TransactionHash == "" {
		return ErrInvalidFill
	}
	f.EmittingContract = strings.ToLower(f.EmittingContract)
	f.PoolAddress = strings.ToLower(f.PoolAddress)
	f.TransactionHash = strings.ToLower(f.TransactionHash)
	f.Sender = strings.ToLower(f.Sender)
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	return st.TransactionTx(ctx, tx, store.Claim{ChainID: f.ChainID, Pool: f.PoolAddress, Emitter: f.EmittingContract, CodeEpoch: f.CodeEpoch, TxHash: f.TransactionHash, LogIndex: strconv.FormatUint(f.Position.LogIndex, 10), Epoch: ownerEpoch, Digest: digest[:], Fill: body}, func(tx pgx.Tx, q string) error {
		for _, interval := range ExactIntervals {
			start, e := BucketStart(f.BlockTimestamp, interval)
			if e != nil {
				return e
			}
			var previous []byte
			e = tx.QueryRow(ctx, `SELECT snapshot FROM `+q+`.chart_exact WHERE chain_id=$1 AND pool=$2 AND interval=$3 AND start=$4`, f.ChainID, f.PoolAddress, interval, start).Scan(&previous)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			old, e := decodeExact(previous)
			if e != nil {
				return e
			}
			newTrader := false
			if f.Sender != "" {
				ct, e := tx.Exec(ctx, `INSERT INTO `+q+`.chart_traders(chain_id,pool,interval,start,actor) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, f.ChainID, f.PoolAddress, interval, start, f.Sender)
				if e != nil {
					return e
				}
				newTrader = ct.RowsAffected() == 1
			}
			next, e := FoldExact(old, f, interval, newTrader)
			if e != nil {
				return e
			}
			snapshot, e := json.Marshal(next)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_exact(chain_id,pool,interval,start,snapshot) VALUES($1,$2,$3,$4,$5) ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET snapshot=EXCLUDED.snapshot`, f.ChainID, f.PoolAddress, interval, start, snapshot)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_outbox(chain_id,pool,interval,start,revision,payload) VALUES($1,$2,$3,$4,$5,$6)`, f.ChainID, f.PoolAddress, interval, start, strconv.FormatUint(next.Revision, 10), snapshot)
			if e != nil {
				return e
			}
		}
		return nil
	})
}

type QualifiedPriceSource struct {
	SourceFillID  string                    `json:"sourceFillId"`
	Position      accounting.SourcePosition `json:"position"`
	ActualEmitter string                    `json:"actualEmitter"`
	Provenance    accounting.Provenance     `json:"provenance"`
}
type QualifiedCandle struct {
	ExactCandle
	Token                  accounting.Asset     `json:"token"`
	Settlement             accounting.Asset     `json:"settlement"`
	OpenSource             QualifiedPriceSource `json:"openSource"`
	HighSource             QualifiedPriceSource `json:"highSource"`
	LowSource              QualifiedPriceSource `json:"lowSource"`
	CloseSource            QualifiedPriceSource `json:"closeSource"`
	UnknownActorTradeCount uint64               `json:"unknownActorTradeCount"`
	ActorSemantics         string               `json:"actorSemantics"`
}

func QualifiedFill(f accounting.FillV1) (Fill, error) {
	if err := accounting.ValidateFillV1(f); err != nil {
		return Fill{}, err
	}
	height, err := strconv.ParseUint(f.Position.Height, 10, 63)
	if err != nil {
		return Fill{}, err
	}
	transactionIndex, err := strconv.ParseUint(f.Position.TransactionIndex, 10, 32)
	if err != nil {
		return Fill{}, err
	}
	logIndex, err := strconv.ParseUint(f.Position.LogIndex, 10, 32)
	if err != nil {
		return Fill{}, err
	}
	timestamp, err := strconv.ParseInt(f.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return Fill{}, err
	}
	sender := ""
	if f.Attribution.Trader != nil {
		sender = *f.Attribution.Trader
	}
	copied := Fill{ChainID: f.Position.ChainID, EmittingContract: f.ActualEmitter, CodeEpoch: f.Provenance.EmitterCodeEpoch, TransactionHash: f.Position.TransactionHash, PoolAddress: f.PoolAddress, Sender: sender, Position: ChainOrder{Height: height, TransactionIndex: transactionIndex, LogIndex: logIndex}, BlockTimestamp: timestamp, TimestampAuthenticated: true, Side: f.Side, TokenAmountRaw: f.TokenAmountRaw, UsdlAmountRaw: f.USDLAmountRaw, PaidInputRaw: f.PaidInputRaw, PricedInputRaw: f.PricedInputRaw, AmmInputAfterFeeRaw: f.AMMInputAfterFeeRaw, FeeRaw: f.FeeRaw, FeeDisposition: f.FeeDisposition, InputDerivationQuality: f.InputDerivationQuality, TokenDecimals: int(f.Token.Decimals), UsdlDecimals: int(f.Settlement.Decimals)}
	if _, err = copied.Price(); err != nil {
		return Fill{}, err
	}
	return copied, nil
}
func qualifiedSource(f accounting.FillV1) QualifiedPriceSource {
	return QualifiedPriceSource{SourceFillID: f.SourceFillID, Position: f.Position, ActualEmitter: f.ActualEmitter, Provenance: f.Provenance}
}
func equalOrder(source accounting.SourcePosition, legacy ChainOrder) bool {
	return source.Height == strconv.FormatUint(legacy.Height, 10) && source.TransactionIndex == strconv.FormatUint(legacy.TransactionIndex, 10) && source.LogIndex == strconv.FormatUint(legacy.LogIndex, 10)
}
func FoldQualified(c *QualifiedCandle, source accounting.FillV1, interval string, newTrader bool) (QualifiedCandle, error) {
	f, err := QualifiedFill(source)
	if err != nil {
		return QualifiedCandle{}, err
	}
	var old *ExactCandle
	incoming := qualifiedSource(source)
	out := QualifiedCandle{Token: source.Token, Settlement: source.Settlement, OpenSource: incoming, HighSource: incoming, LowSource: incoming, CloseSource: incoming}
	if c != nil {
		if c.Token != source.Token || c.Settlement != source.Settlement || !equalOrder(c.OpenSource.Position, c.FirstPosition) || !equalOrder(c.CloseSource.Position, c.LastPosition) {
			return out, ErrInvalidFill
		}
		for _, s := range []QualifiedPriceSource{c.OpenSource, c.HighSource, c.LowSource, c.CloseSource} {
			if accounting.ValidateSourcePosition(s.Position) != nil || s.SourceFillID != accounting.SourceFillIdentity(s.Position, s.Provenance.DeploymentID) {
				return out, ErrInvalidFill
			}
		}
		out = *c
		old = &c.ExactCandle
		order, err := accounting.CompareSourcePosition(source.Position, c.OpenSource.Position)
		if err != nil {
			return out, err
		}
		if order < 0 {
			out.OpenSource = incoming
		}
		order, err = accounting.CompareSourcePosition(source.Position, c.CloseSource.Position)
		if err != nil {
			return out, err
		}
		if order > 0 {
			out.CloseSource = incoming
		}
		price, err := f.Price()
		if err != nil {
			return out, err
		}
		compared, err := price.Compare(c.High)
		if err != nil {
			return out, err
		}
		if compared > 0 || (compared == 0 && f.Position.Compare(ChainOrderFromSource(c.HighSource.Position)) < 0) {
			out.HighSource = incoming
		}
		compared, err = price.Compare(c.Low)
		if err != nil {
			return out, err
		}
		if compared < 0 || (compared == 0 && f.Position.Compare(ChainOrderFromSource(c.LowSource.Position)) < 0) {
			out.LowSource = incoming
		}
	}
	next, err := FoldExact(old, f, interval, newTrader)
	if err != nil {
		return out, err
	}
	out.ExactCandle = next
	out.ActorSemantics = "canonical_swap_actor_v1"
	if source.Attribution.Trader == nil {
		if out.UnknownActorTradeCount == ^uint64(0) {
			return out, ErrInvalidFill
		}
		out.UnknownActorTradeCount++
	}
	return out, nil
}
func ApplyQualifiedTx(ctx context.Context, tx pgx.Tx, st *store.ExactStore, source accounting.FillV1, ownerEpoch int64) error {
	return applyQualified(ctx, tx, st, source, ownerEpoch, nil, nil, accounting.PartitionPosition{}, "")
}
func ApplySeriesTx(ctx context.Context, tx pgx.Tx, st *store.ExactStore, source accounting.FillV1, ownerEpoch int64, checkpoint SeriesCheckpoint) error {
	return applyQualified(ctx, tx, st, source, ownerEpoch, &checkpoint, nil, accounting.PartitionPosition{}, "")
}
func ApplySeriesStreamTx(ctx context.Context, tx pgx.Tx, st *store.ExactStore, source accounting.FillV1, ownerEpoch int64, checkpoint SeriesCheckpoint, writer *delivery.StreamWriter, position accounting.PartitionPosition, sourceStream string) error {
	return applyQualified(ctx, tx, st, source, ownerEpoch, &checkpoint, writer, position, sourceStream)
}
func applyQualified(ctx context.Context, tx pgx.Tx, st *store.ExactStore, source accounting.FillV1, ownerEpoch int64, checkpoint *SeriesCheckpoint, writer *delivery.StreamWriter, position accounting.PartitionPosition, sourceStream string) error {
	f, err := QualifiedFill(source)
	if err != nil {
		return err
	}
	if tx == nil || st == nil {
		return ErrInvalidFill
	}
	body, err := json.Marshal(source)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	claim := store.Claim{ChainID: f.ChainID, Pool: f.PoolAddress, Emitter: f.EmittingContract, CodeEpoch: f.CodeEpoch, TxHash: f.TransactionHash, LogIndex: strconv.FormatUint(f.Position.LogIndex, 10), Epoch: ownerEpoch, Digest: digest[:], Fill: body}
	return st.TransactionTx(ctx, tx, claim, func(tx pgx.Tx, q string) error {
		var displayRevision uint64
		if checkpoint != nil {
			copy := *checkpoint
			unresolved, err := st.HasUnresolvedDeliveriesTx(ctx, tx)
			if err != nil {
				return err
			}
			copy.HasGaps = copy.HasGaps || unresolved
			checkpoint = &copy
			if err := ValidateCheckpoint(*checkpoint, source); err != nil {
				return err
			}
			revision, err := st.AdvanceSeriesTx(ctx, tx, f.PoolAddress, checkpoint.Height, checkpoint.Timestamp, checkpoint.HasGaps)
			if err != nil {
				return err
			}
			displayRevision = revision
		}
		for _, interval := range ExactIntervals {
			start, err := BucketStart(f.BlockTimestamp, interval)
			if err != nil {
				return err
			}
			var previous []byte
			err = tx.QueryRow(ctx, `SELECT snapshot FROM `+q+`.chart_exact WHERE chain_id=$1 AND pool=$2 AND interval=$3 AND start=$4`, f.ChainID, f.PoolAddress, interval, start).Scan(&previous)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			var old *QualifiedCandle
			if len(previous) > 0 {
				old = &QualifiedCandle{}
				if err = json.Unmarshal(previous, old); err != nil {
					return err
				}
			}
			newTrader := false
			if f.Sender != "" {
				tag, err := tx.Exec(ctx, `INSERT INTO `+q+`.chart_traders(chain_id,pool,interval,start,actor) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, f.ChainID, f.PoolAddress, interval, start, f.Sender)
				if err != nil {
					return err
				}
				newTrader = tag.RowsAffected() == 1
			}
			next, err := FoldQualified(old, source, interval, newTrader)
			if err != nil {
				return err
			}
			var snapshot []byte
			if checkpoint == nil {
				snapshot, err = json.Marshal(next)
			} else {
				state, e := DescribeSeries(next, *checkpoint, timeNowUTC())
				if e != nil {
					return e
				}
				state.DisplayRevision = displayRevision
				snapshot, err = json.Marshal(state)
			}
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO `+q+`.chart_exact(chain_id,pool,interval,start,snapshot) VALUES($1,$2,$3,$4,$5) ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET snapshot=EXCLUDED.snapshot`, f.ChainID, f.PoolAddress, interval, start, snapshot); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO `+q+`.chart_outbox(chain_id,pool,interval,start,revision,payload) VALUES($1,$2,$3,$4,$5,$6)`, f.ChainID, f.PoolAddress, interval, start, strconv.FormatUint(next.Revision, 10), snapshot); err != nil {
				return err
			}
			if writer != nil {
				var state SeriesCandle
				if err = json.Unmarshal(snapshot, &state); err != nil {
					return err
				}
				if err = appendCandleStreamTx(ctx, tx, st, *writer, state, source, position, sourceStream, snapshot); err != nil {
					return err
				}
			}
		}
		if checkpoint != nil {
			for _, parent := range ExactIntervals[1:] {
				start, e := BucketStart(f.BlockTimestamp, parent)
				if e != nil {
					return e
				}
				if e = st.InvalidateRollupTx(ctx, tx, f.PoolAddress, parent, start, displayRevision); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
