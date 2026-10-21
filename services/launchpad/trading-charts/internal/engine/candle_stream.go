package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type CanonicalSeriesCandle struct {
	interfaces.Candle
	AlgorithmVersion       string `json:"algorithmVersion"`
	SourceWatermarkHeight  string `json:"sourceWatermarkHeight"`
	IntervalEnd            string `json:"intervalEnd"`
	CompletenessStatus     string `json:"completenessStatus"`
	Provenance             string `json:"provenance"`
	DisplayRevision        string `json:"displayRevision"`
	CorrectionReason       string `json:"correctionReason,omitempty"`
	UnknownActorTradeCount string `json:"unknownActorTradeCount"`
	ActorSemantics         string `json:"actorSemantics"`
}

func sourceTimestamp(seconds int64) (string, error) {
	if seconds < 0 {
		return "", ErrInvalidFill
	}
	stamp := time.Unix(seconds, 0).UTC()
	if stamp.Year() < 1 || stamp.Year() > 9999 {
		return "", ErrInvalidFill
	}
	return stamp.Format("2006-01-02T15:04:05.000Z"), nil
}
func canonicalPosition(p accounting.SourcePosition) interfaces.ChainPosition {
	return interfaces.ChainPosition{BlockHash: p.BlockHash, ChainId: p.ChainID, Height: interfaces.UInt256(p.Height), TransactionHash: p.TransactionHash, TransactionIndex: interfaces.UInt256(p.TransactionIndex), LogIndex: interfaces.UInt256(p.LogIndex)}
}
func canonicalAsset(a accounting.Asset) interfaces.AssetRef {
	chain := "125"
	address := a.Address
	decimals := interfaces.SafeInteger(a.Decimals)
	return interfaces.AssetRef{Kind: "erc20", ChainId: &chain, Address: &address, Decimals: &decimals}
}
func canonicalPrice(price Rational, source QualifiedPriceSource, bar SeriesCandle, fill accounting.FillV1) (interfaces.Price, error) {
	if accounting.ValidateFillV1(fill) != nil || qualifiedSource(fill) != source || fill.PoolAddress != bar.PoolAddress || fill.Token != bar.Token || fill.Settlement != bar.Settlement {
		return interfaces.Price{}, ErrInvalidFill
	}
	folded, err := QualifiedFill(fill)
	if err != nil {
		return interfaces.Price{}, err
	}
	actual, err := folded.Price()
	if err != nil {
		return interfaces.Price{}, err
	}
	cmp, err := actual.Compare(price)
	if err != nil || cmp != 0 {
		return interfaces.Price{}, ErrInvalidFill
	}
	observed, err := sourceTimestamp(folded.BlockTimestamp)
	if err != nil {
		return interfaces.Price{}, err
	}
	position := canonicalPosition(source.Position)
	identity := source.SourceFillID
	receipt := fill.Provenance.ReceiptSHA256
	return interfaces.Price{Numerator: interfaces.PriceComponent(price.Numerator), Denominator: interfaces.PositivePriceComponent(price.Denominator), BaseAsset: canonicalAsset(bar.Token), QuoteAsset: canonicalAsset(bar.Settlement), Source: interfaces.PriceSource{Kind: "gross_execution", AmountBasis: "paid_input", SourceEventId: &identity, Position: &position, ObservedAt: observed, SourceReference: &receipt}}, nil
}
func CanonicalCandle(bar SeriesCandle, sources map[string]accounting.FillV1) (CanonicalSeriesCandle, error) {
	var out CanonicalSeriesCandle
	if bar.AlgorithmVersion != SeriesAlgorithm || bar.Synthetic || bar.Provenance != "actual_trade" || bar.Trades == 0 || bar.Revision == 0 || bar.DisplayRevision == 0 || bar.ActorSemantics != "canonical_swap_actor_v1" || !equalOrder(bar.OpenSource.Position, bar.FirstPosition) || !equalOrder(bar.CloseSource.Position, bar.LastPosition) {
		return out, ErrInvalidFill
	}
	duration, err := IntervalSeconds(bar.Interval)
	if err != nil || bar.IntervalEnd != bar.Start+duration {
		return out, ErrInvalidFill
	}
	start, err := sourceTimestamp(bar.Start)
	if err != nil {
		return out, err
	}
	end, err := sourceTimestamp(bar.IntervalEnd)
	if err != nil {
		return out, err
	}
	switch bar.CompletenessStatus {
	case "live_interval", "interval_elapsed", "backfill_incomplete", "complete_through_watermark":
	default:
		return out, ErrInvalidFill
	}
	coverage := "partial"
	if bar.CompletenessStatus == "complete_through_watermark" {
		coverage = "complete"
	}
	prices := []Rational{bar.Open, bar.High, bar.Low, bar.Close}
	refs := []QualifiedPriceSource{bar.OpenSource, bar.HighSource, bar.LowSource, bar.CloseSource}
	mapped := make([]interfaces.Price, 4)
	for i, ref := range refs {
		fill, ok := sources[ref.SourceFillID]
		if !ok {
			return out, ErrInvalidFill
		}
		mapped[i], err = canonicalPrice(prices[i], ref, bar, fill)
		if err != nil {
			return out, err
		}
	}
	unique := interfaces.UInt256(strconv.FormatUint(bar.UniqueTraders, 10))
	revision := strconv.FormatUint(bar.Revision, 10)
	raw, err := json.Marshal(bar)
	if err != nil {
		return out, err
	}
	hash := sha256.Sum256(raw)
	hash[6] = (hash[6] & 0x0f) | 0x50
	hash[8] = (hash[8] & 0x3f) | 0x80
	snapshot := fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
	out = CanonicalSeriesCandle{Candle: interfaces.Candle{PoolAddress: bar.PoolAddress, Interval: bar.Interval, Start: start, Open: mapped[0], High: mapped[1], Low: mapped[2], Close: mapped[3], VolumeUsdlRaw: interfaces.AggregateUInt(bar.VolumeUsdlRaw), VolumeTokenRaw: interfaces.AggregateUInt(bar.VolumeTokenRaw), BuyVolumeUsdlRaw: interfaces.AggregateUInt(bar.BuyVolumeUsdlRaw), SellVolumeUsdlRaw: interfaces.AggregateUInt(bar.SellVolumeUsdlRaw), Trades: interfaces.PositiveUInt256(strconv.FormatUint(bar.Trades, 10)), Revision: interfaces.PositiveUInt256(revision), Coverage: coverage, FirstPosition: canonicalPosition(bar.OpenSource.Position), LastPosition: canonicalPosition(bar.CloseSource.Position), SeriesModelVersion: bar.AlgorithmVersion, SnapshotId: snapshot, UniqueTraderCount: &unique}, AlgorithmVersion: bar.AlgorithmVersion, SourceWatermarkHeight: strconv.FormatUint(bar.SourceWatermarkHeight, 10), IntervalEnd: end, CompletenessStatus: bar.CompletenessStatus, Provenance: bar.Provenance, DisplayRevision: strconv.FormatUint(bar.DisplayRevision, 10), CorrectionReason: bar.CorrectionReason, UnknownActorTradeCount: strconv.FormatUint(bar.UnknownActorTradeCount, 10), ActorSemantics: bar.ActorSemantics}
	payload, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if interfaces.ValidateJSON("Candle", payload) != nil {
		return out, ErrInvalidFill
	}
	return out, nil
}
func appendCandleStreamTx(ctx context.Context, tx pgx.Tx, st *store.ExactStore, writer delivery.StreamWriter, bar SeriesCandle, source accounting.FillV1, position accounting.PartitionPosition, sourceStream string, snapshot []byte) error {
	if st == nil || tx == nil || writer.Schema != "api_stream" || sourceStream == "" || accounting.ValidatePartitionPosition(position) != nil || position.PartitionKind != "market" || position.PartitionKey != "125:"+source.PoolAddress || position.SourceEventID != source.SourceFillID {
		return ErrInvalidFill
	}
	digest, err := accounting.FillDigest(source)
	if err != nil {
		return err
	}
	if _, err = accounting.NewMarketPublication(source, []accounting.PartitionPosition{position}); err != nil {
		return err
	}
	sequence, err := strconv.ParseInt(position.Sequence, 10, 64)
	if err != nil || sequence < 1 {
		return ErrInvalidFill
	}
	sources := map[string]accounting.FillV1{}
	for _, ref := range []QualifiedPriceSource{bar.OpenSource, bar.HighSource, bar.LowSource, bar.CloseSource} {
		if _, exists := sources[ref.SourceFillID]; exists {
			continue
		}
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT fill FROM `+st.Schema+`.chart_inbox WHERE chain_id='125' AND pool=$1 AND emitter=$2 AND code_epoch=$3 AND tx_hash=$4 AND log_index=$5::numeric`, bar.PoolAddress, ref.ActualEmitter, ref.Provenance.EmitterCodeEpoch, ref.Position.TransactionHash, ref.Position.LogIndex).Scan(&raw); err != nil {
			return err
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			return err
		}
		sources[ref.SourceFillID] = fill
	}
	mapped, err := CanonicalCandle(bar, sources)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(mapped)
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	occurred := now.UTC().Format("2006-01-02T15:04:05.000Z")
	var watermark *interfaces.Watermark
	if bar.SourceWatermarkHeight > 0 {
		var blockHash string
		if err = tx.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete`, sourceStream, bar.SourceWatermarkHeight).Scan(&blockHash); err != nil {
			return err
		}
		observed, err := sourceTimestamp(bar.SourceWatermarkTimestamp)
		if err != nil {
			return err
		}
		watermark = &interfaces.Watermark{ChainId: "125", Height: interfaces.UInt256(strconv.FormatUint(bar.SourceWatermarkHeight, 10)), BlockHash: blockHash, ProjectionRevision: interfaces.PositiveUInt256(strconv.FormatUint(bar.DisplayRevision, 10)), ObservedAt: observed, Integrity: "healthy"}
	}
	watermarkWire, err := json.Marshal(watermark)
	if err != nil {
		return err
	}
	revision := strconv.FormatUint(bar.Revision, 10)
	start := strconv.FormatInt(bar.Start, 10)
	identity := "candle:" + accounting.HashTuple(SeriesAlgorithm, bar.PoolAddress, bar.Interval, start, revision, position.ProjectionVersion, position.Sequence, string(digest))
	if _, err = tx.Exec(ctx, `INSERT INTO `+st.Schema+`.chart_stream_sources(chain_id,pool,interval,start,revision,event_id,snapshot,payload,watermark,occurred_at,projection_version,source_sequence,source_fill_id,source_fact_digest) VALUES('125',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, bar.PoolAddress, bar.Interval, bar.Start, revision, identity, snapshot, payload, watermarkWire, occurred, position.ProjectionVersion, sequence, source.SourceFillID, string(digest)); err != nil {
		return err
	}
	return writer.AppendTx(ctx, tx, delivery.StreamUpdate{EventID: identity, Topic: "market.candles." + bar.PoolAddress + "." + bar.Interval, Revision: revision, OccurredAt: occurred, Watermark: watermarkWire, Payload: payload, Source: delivery.StreamSourceProof{Kind: "candle_commit", Pool: bar.PoolAddress, ProjectionVersion: position.ProjectionVersion, Sequence: position.Sequence, SourceFillID: source.SourceFillID, SourceFactDigest: string(digest), Interval: bar.Interval, BucketStart: start}})
}
