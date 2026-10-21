package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"math/big"
	"sort"
	"strconv"
	"time"
)

var ErrWatermarkRegression = errors.New("source_watermark_regression")
var ErrNoData = errors.New("no_data")

const SeriesAlgorithm = "qualified-exact-v2"

type SeriesCheckpoint struct {
	Height    uint64
	Timestamp int64
	HasGaps   bool
}
type SeriesCandle struct {
	QualifiedCandle
	AlgorithmVersion         string `json:"algorithmVersion"`
	SourceWatermarkHeight    uint64 `json:"sourceWatermarkHeight"`
	SourceWatermarkTimestamp int64  `json:"sourceWatermarkTimestamp"`
	IntervalEnd              int64  `json:"intervalEnd"`
	CompletenessStatus       string `json:"completenessStatus"`
	Synthetic                bool   `json:"synthetic"`
	Provenance               string `json:"provenance"`
	DisplayRevision          uint64 `json:"displayRevision"`
	CorrectionReason         string `json:"correctionReason,omitempty"`
}

func timeNowUTC() time.Time { return time.Now().UTC() }
func ChainOrderFromSource(p accounting.SourcePosition) ChainOrder {
	h, _ := strconv.ParseUint(p.Height, 10, 64)
	t, _ := strconv.ParseUint(p.TransactionIndex, 10, 64)
	l, _ := strconv.ParseUint(p.LogIndex, 10, 64)
	return ChainOrder{h, t, l}
}
func IntervalSeconds(interval string) (int64, error) {
	switch interval {
	case "1m":
		return 60, nil
	case "5m":
		return 300, nil
	case "15m":
		return 900, nil
	case "1h":
		return 3600, nil
	case "4h":
		return 14400, nil
	case "1d":
		return 86400, nil
	case "1w":
		return 604800, nil
	}
	return 0, ErrUnsupportedResolution
}
func ValidateCheckpoint(c SeriesCheckpoint, f accounting.FillV1) error {
	if accounting.ValidateFillV1(f) != nil || c.Height > uint64(^uint64(0)>>1) || c.Timestamp < 0 {
		return ErrInvalidFill
	}
	return nil
}
func DescribeSeries(c QualifiedCandle, checkpoint SeriesCheckpoint, now time.Time) (SeriesCandle, error) {
	duration, err := IntervalSeconds(c.Interval)
	if err != nil || c.Trades == 0 || c.Start > int64(^uint64(0)>>1)-duration {
		return SeriesCandle{}, ErrInvalidFill
	}
	state := SeriesCandle{QualifiedCandle: c, AlgorithmVersion: SeriesAlgorithm, SourceWatermarkHeight: checkpoint.Height, SourceWatermarkTimestamp: checkpoint.Timestamp, IntervalEnd: c.Start + duration, Provenance: "actual_trade"}
	switch {
	case checkpoint.HasGaps:
		state.CompletenessStatus = "backfill_incomplete"
	case now.Unix() < state.IntervalEnd:
		state.CompletenessStatus = "live_interval"
	case checkpoint.Timestamp >= state.IntervalEnd:
		state.CompletenessStatus = "complete_through_watermark"
	default:
		state.CompletenessStatus = "interval_elapsed"
	}
	if now.Unix() >= state.IntervalEnd {
		state.CorrectionReason = "delayed_ingestion"
	}
	return state, nil
}
func DenseView(actual []SeriesCandle, from, to int64, interval string, seed *SeriesCandle, displayRevision uint64) ([]SeriesCandle, error) {
	step, err := IntervalSeconds(interval)
	if err != nil || from < 0 || to <= from || (to-from-1)/step+1 > 10000 {
		return nil, ErrInvalidFill
	}
	start, err := BucketStart(from, interval)
	if err != nil || start != from {
		return nil, ErrInvalidFill
	}
	rows := append([]SeriesCandle(nil), actual...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Start < rows[j].Start })
	byStart := map[int64]SeriesCandle{}
	pool := ""
	var previous *SeriesCandle
	if seed != nil {
		if seed.Synthetic || seed.Trades == 0 || seed.Interval != interval || seed.Start >= from {
			return nil, ErrInvalidFill
		}
		copy := *seed
		previous = &copy
		pool = copy.PoolAddress
	}
	for _, bar := range rows {
		bucket, e := BucketStart(bar.Start, interval)
		if e != nil || bucket != bar.Start || bar.Interval != interval || bar.Synthetic || bar.Trades == 0 || bar.Start < from || bar.Start >= to {
			return nil, ErrInvalidFill
		}
		if _, exists := byStart[bar.Start]; exists {
			return nil, ErrInvalidFill
		}
		if pool != "" && pool != bar.PoolAddress {
			return nil, ErrInvalidFill
		}
		pool = bar.PoolAddress
		byStart[bar.Start] = bar
	}
	out := []SeriesCandle{}
	for bucket := from; bucket < to; bucket += step {
		if bar, exists := byStart[bucket]; exists {
			copy := bar
			previous = &copy
			out = append(out, bar)
			continue
		}
		if previous == nil {
			continue
		}
		carry := *previous
		carry.Start = bucket
		carry.IntervalEnd = bucket + step
		carry.Open = previous.Close
		carry.High = previous.Close
		carry.Low = previous.Close
		carry.Close = previous.Close
		carry.Trades = 0
		carry.UniqueTraders = 0
		carry.UnknownActorTradeCount = 0
		carry.VolumeUsdlRaw = "0"
		carry.VolumeTokenRaw = "0"
		carry.BuyVolumeUsdlRaw = "0"
		carry.SellVolumeUsdlRaw = "0"
		carry.OpenSource = previous.CloseSource
		carry.HighSource = previous.CloseSource
		carry.LowSource = previous.CloseSource
		carry.FirstPosition = previous.LastPosition
		carry.LastPosition = previous.LastPosition
		carry.Synthetic = true
		carry.Provenance = "synthetic_carry"
		carry.CorrectionReason = ""
		carry.DisplayRevision = displayRevision
		out = append(out, carry)
	}
	if len(out) == 0 {
		return out, ErrNoData
	}
	return out, nil
}

type ReconstructionReport struct {
	RunID            string   `json:"runId"`
	RequestedFrom    int64    `json:"requestedFrom"`
	RequestedTo      int64    `json:"requestedTo"`
	RebuiltFrom      int64    `json:"rebuiltFrom"`
	RebuiltTo        int64    `json:"rebuiltTo"`
	AlgorithmVersion string   `json:"algorithmVersion"`
	ManifestDigests  []string `json:"manifestDigests"`
	SourceFills      uint64   `json:"sourceFills"`
	DuplicateFills   uint64   `json:"duplicateFills"`
	BarCount         uint64   `json:"barCount"`
	ExpectedDigest   string   `json:"expectedDigest"`
	ActualDigest     string   `json:"actualDigest"`
	PromotionEpoch   uint64   `json:"promotionEpoch"`
}

func projectionDigest(bars []QualifiedCandle) (string, error) {
	rows := append([]QualifiedCandle(nil), bars...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Interval != rows[j].Interval {
			return rows[i].Interval < rows[j].Interval
		}
		return rows[i].Start < rows[j].Start
	})
	hashes := []string{}
	for _, bar := range rows {
		digest, err := EconomicDigest(bar)
		if err != nil {
			return "", err
		}
		hashes = append(hashes, digest)
	}
	raw, _ := json.Marshal(hashes)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func ReconstructArchive(ctx context.Context, st *store.ExactStore, reader store.ArchiveReader, id, pool string, from, to int64, manifests []store.ArchiveManifest) (ReconstructionReport, error) {
	report := ReconstructionReport{RunID: id, RequestedFrom: from, RequestedTo: to, AlgorithmVersion: SeriesAlgorithm, ManifestDigests: []string{}}
	if st == nil || reader.Exact != st || reader.BlockTime == nil || from < 0 || to <= from || len(manifests) == 0 || len(manifests) > 10000 || to-from > 366*24*3600 {
		return report, ErrInvalidFill
	}
	expandedFrom, err := BucketStart(from, "1w")
	if err != nil {
		return report, err
	}
	last, err := BucketStart(to-1, "1w")
	if err != nil {
		return report, err
	}
	expandedTo := last + 604800
	for _, m := range manifests {
		if m.Validate() != nil {
			return report, store.ErrIntegrity
		}
	}
	from, to = expandedFrom, expandedTo
	report.RebuiltFrom = from
	report.RebuiltTo = to
	ordered := append([]store.ArchiveManifest(nil), manifests...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FromHeight < ordered[j].FromHeight })
	firstTime, err := reader.BlockTime(ctx, ordered[0].FromHeight, ordered[0].Blocks[0].Hash)
	if err != nil {
		return report, err
	}
	lastManifest := ordered[len(ordered)-1]
	lastTime, err := reader.BlockTime(ctx, lastManifest.ToHeight, lastManifest.Blocks[len(lastManifest.Blocks)-1].Hash)
	if err != nil {
		return report, err
	}
	if firstTime > expandedFrom || lastTime < expandedTo {
		return report, errors.New("archive_does_not_cover_full_parent_boundaries")
	}
	if err = st.HoldReplayRange(ctx, id, pool, expandedFrom, expandedTo, 30*time.Minute); err != nil {
		return report, err
	}
	fills := []accounting.FillV1{}
	seen := map[string]accounting.Digest{}
	seenManifests := map[string]bool{}
	previousEnd := int64(-1)
	stream := ""
	for _, manifest := range ordered {
		raw, _ := json.Marshal(manifest)
		sum := sha256.Sum256(raw)
		digest := hex.EncodeToString(sum[:])
		if seenManifests[digest] {
			continue
		}
		seenManifests[digest] = true
		if stream != "" && (stream != manifest.SourceStream || manifest.FromHeight != previousEnd+1) {
			return report, errors.New("archive_missing_or_overlapping_height_range")
		}
		stream = manifest.SourceStream
		previousEnd = manifest.ToHeight
		segment, err := reader.Read(ctx, manifest)
		if err != nil {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer stop()
			incidentErr := st.RecordRecoverabilityIncident(cleanup, pool, "archive_missing_or_corrupt", digest)
			return report, errors.Join(fmt.Errorf("archive [%d,%d]: %w", manifest.FromHeight, manifest.ToHeight, err), incidentErr)
		}
		report.ManifestDigests = append(report.ManifestDigests, digest)
		for _, source := range segment {
			if source.PoolAddress != pool {
				continue
			}
			timestamp, err := strconv.ParseInt(source.BlockTimestampSeconds, 10, 64)
			if err != nil {
				return report, err
			}
			if timestamp < expandedFrom || timestamp >= expandedTo {
				continue
			}
			digest, err := accounting.FillDigest(source)
			if err != nil {
				return report, err
			}
			if prior, exists := seen[source.SourceFillID]; exists {
				if prior != digest {
					return report, store.ErrIntegrity
				}
				report.DuplicateFills++
				continue
			}
			seen[source.SourceFillID] = digest
			if len(fills) >= 100000 {
				return report, errors.New("reconstruction_fill_bound")
			}
			fills = append(fills, source)
			report.SourceFills++
		}
	}
	if len(fills) > 1000000 {
		return report, errors.New("reconstruction_fill_bound")
	}
	buckets := map[string]QualifiedCandle{}
	members := map[string]map[string]bool{}
	for _, fill := range fills {
		copied, err := QualifiedFill(fill)
		if err != nil {
			return report, err
		}
		for _, interval := range ExactIntervals {
			start, err := BucketStart(copied.BlockTimestamp, interval)
			if err != nil {
				return report, err
			}
			duration, _ := IntervalSeconds(interval)
			if start >= to || start+duration <= from {
				continue
			}
			key := interval + ":" + strconv.FormatInt(start, 10)
			var old *QualifiedCandle
			if previous, exists := buckets[key]; exists {
				p := previous
				old = &p
			}
			if members[key] == nil {
				members[key] = map[string]bool{}
			}
			newTrader := false
			if copied.Sender != "" {
				newTrader = !members[key][copied.Sender]
				members[key][copied.Sender] = true
			}
			next, err := FoldQualified(old, fill, interval, newTrader)
			if err != nil {
				return report, err
			}
			buckets[key] = next
		}
	}
	actual := []QualifiedCandle{}
	stage := []store.StagedCandle{}
	traders := []store.StagedTrader{}
	checkpoint := SeriesCheckpoint{Height: uint64(lastManifest.ToHeight), Timestamp: lastTime}
	for key, bar := range buckets {
		actual = append(actual, bar)
		state, err := DescribeSeries(bar, checkpoint, timeNowUTC())
		if err != nil {
			return report, err
		}
		state.CorrectionReason = "archive_reconstruction"
		raw, err := json.Marshal(state)
		if err != nil {
			return report, err
		}
		stage = append(stage, store.StagedCandle{Interval: bar.Interval, Start: bar.Start, Snapshot: raw})
		for actor := range members[key] {
			traders = append(traders, store.StagedTrader{Interval: bar.Interval, Start: bar.Start, Actor: actor})
		}
	}
	for _, parent := range actual {
		if parent.Interval == "1m" {
			continue
		}
		duration, _ := IntervalSeconds(parent.Interval)
		children := []QualifiedCandle{}
		actors := []string{}
		for key, child := range buckets {
			if child.Interval == "1m" && child.Start >= parent.Start && child.Start < parent.Start+duration {
				children = append(children, child)
				for actor := range members[key] {
					actors = append(actors, actor)
				}
			}
		}
		rolled, _, err := RollupQualified(children, actors, parent.Interval, parent.Start)
		if err != nil {
			return report, err
		}
		a, _ := EconomicDigest(parent)
		b, _ := EconomicDigest(rolled)
		if a != b {
			return report, store.ErrIntegrity
		}
	}
	report.BarCount = uint64(len(actual))
	report.ActualDigest, err = projectionDigest(actual)
	if err != nil {
		return report, err
	}
	tx, err := st.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	var revision uint64
	if err = tx.QueryRow(ctx, `SELECT revision FROM `+st.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1`, pool).Scan(&revision); err != nil {
		return report, err
	}
	rows, err := tx.Query(ctx, `SELECT snapshot FROM `+st.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND start>=$2 AND start<$3 ORDER BY interval,start`, pool, expandedFrom, expandedTo)
	if err != nil {
		return report, err
	}
	expected := []QualifiedCandle{}
	for rows.Next() {
		var raw []byte
		var bar QualifiedCandle
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return report, err
		}
		if err = json.Unmarshal(raw, &bar); err != nil {
			rows.Close()
			return report, err
		}
		duration, err := IntervalSeconds(bar.Interval)
		if err != nil {
			rows.Close()
			return report, err
		}
		if bar.Start < to && bar.Start+duration > from {
			expected = append(expected, bar)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return report, err
	}
	if err = tx.Commit(ctx); err != nil {
		return report, err
	}
	report.ExpectedDigest, err = projectionDigest(expected)
	if err != nil {
		return report, err
	}
	if report.ExpectedDigest != report.ActualDigest {
		hash := sha256.Sum256([]byte(report.ExpectedDigest + report.ActualDigest))
		incidentErr := st.RecordRecoverabilityIncident(ctx, pool, "reconstruction_economic_checksum_mismatch", hex.EncodeToString(hash[:]))
		return report, errors.Join(errors.New("reconstruction_economic_checksum_mismatch"), incidentErr)
	}
	manifestWire, _ := json.Marshal(report.ManifestDigests)
	hash := sha256.Sum256(manifestWire)
	if err = st.StageRebuild(ctx, id, pool, from, to, hex.EncodeToString(hash[:]), report.ExpectedDigest, report.ActualDigest, revision, stage, traders, fills); err != nil {
		return report, err
	}
	report.PromotionEpoch, err = st.PromoteRebuild(ctx, id)
	return report, err
}

func ReadSeries(ctx context.Context, st *store.ExactStore, pool, interval string, from, to int64, dense bool) ([]SeriesCandle, error) {
	if st == nil {
		return nil, ErrInvalidFill
	}
	raw, seedRaw, revision, err := st.SeriesSnapshots(ctx, pool, interval, from, to, 10000)
	if err != nil {
		return nil, err
	}
	bars := []SeriesCandle{}
	for _, value := range raw {
		var bar SeriesCandle
		if err = json.Unmarshal(value, &bar); err != nil {
			return nil, err
		}
		if bar.AlgorithmVersion != SeriesAlgorithm || bar.Synthetic || bar.Trades == 0 {
			return nil, ErrInvalidFill
		}
		bars = append(bars, bar)
	}
	if !dense {
		return bars, nil
	}
	var seed *SeriesCandle
	if len(seedRaw) > 0 {
		seed = &SeriesCandle{}
		if err = json.Unmarshal(seedRaw, seed); err != nil {
			return nil, err
		}
	}
	return DenseView(bars, from, to, interval, seed, revision)
}

type MeasurementBar struct {
	Pool              string                    `json:"pool"`
	Interval          string                    `json:"interval"`
	Start             int64                     `json:"start"`
	Open              Rational                  `json:"open"`
	High              Rational                  `json:"high"`
	Low               Rational                  `json:"low"`
	Close             Rational                  `json:"close"`
	VolumeUsdlRaw     string                    `json:"volumeUsdlRaw"`
	VolumeTokenRaw    string                    `json:"volumeTokenRaw"`
	BuyVolumeUsdlRaw  string                    `json:"buyVolumeUsdlRaw"`
	SellVolumeUsdlRaw string                    `json:"sellVolumeUsdlRaw"`
	Trades            uint64                    `json:"trades"`
	UniqueTraders     uint64                    `json:"uniqueTraders"`
	UnknownActors     uint64                    `json:"unknownActors"`
	First             accounting.SourcePosition `json:"first"`
	Last              accounting.SourcePosition `json:"last"`
}
type measurementAccumulator struct {
	bar                    MeasurementBar
	open, high, low, close *big.Rat
	usdl, token, buy, sell *big.Int
	actors                 map[string]bool
}
type MeasurementReference struct {
	MaximumBars int
	bars        map[string]*measurementAccumulator
}

func NewMeasurementReference(maximumBars int) (*MeasurementReference, error) {
	if maximumBars < 1 || maximumBars > 2000000 {
		return nil, errors.New("measurement_reference_bar_budget_required")
	}
	return &MeasurementReference{MaximumBars: maximumBars, bars: map[string]*measurementAccumulator{}}, nil
}
func measurementBucket(timestamp int64, interval string) (int64, error) {
	if timestamp < 0 {
		return 0, ErrInvalidFill
	}
	durations := map[string]int64{"1m": 60, "5m": 300, "15m": 900, "1h": 3600, "4h": 14400, "1d": 86400}
	if interval == "1w" {
		stamp := time.Unix(timestamp, 0).UTC()
		midnight := time.Date(stamp.Year(), stamp.Month(), stamp.Day(), 0, 0, 0, 0, time.UTC)
		return midnight.AddDate(0, 0, -(int(stamp.Weekday())+6)%7).Unix(), nil
	}
	duration, ok := durations[interval]
	if !ok {
		return 0, ErrUnsupportedResolution
	}
	return timestamp / duration * duration, nil
}
func (r *MeasurementReference) Add(fill accounting.FillV1) error {
	if r == nil || accounting.ValidateFillV1(fill) != nil {
		return ErrInvalidFill
	}
	timestamp, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return err
	}
	token, ok := new(big.Int).SetString(fill.TokenAmountRaw, 10)
	if !ok {
		return ErrInvalidFill
	}
	usdl, ok := new(big.Int).SetString(fill.USDLAmountRaw, 10)
	if !ok {
		return ErrInvalidFill
	}
	numerator := new(big.Int).Mul(new(big.Int).Set(usdl), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(fill.Token.Decimals)), nil))
	denominator := new(big.Int).Mul(new(big.Int).Set(token), big.NewInt(1000000))
	price := new(big.Rat).SetFrac(numerator, denominator)
	intervals := []string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"}
	newBars := 0
	for _, interval := range intervals {
		start, err := measurementBucket(timestamp, interval)
		if err != nil {
			return err
		}
		key := fill.PoolAddress + ":" + interval + ":" + strconv.FormatInt(start, 10)
		if _, exists := r.bars[key]; !exists {
			newBars++
		}
	}
	if newBars > r.MaximumBars-len(r.bars) {
		return errors.New("measurement_reference_bar_budget_exceeded")
	}
	for _, interval := range intervals {
		start, err := measurementBucket(timestamp, interval)
		if err != nil {
			return err
		}
		key := fill.PoolAddress + ":" + interval + ":" + strconv.FormatInt(start, 10)
		acc, exists := r.bars[key]
		if !exists {
			if len(r.bars) >= r.MaximumBars {
				return errors.New("measurement_reference_bar_budget_exceeded")
			}
			acc = &measurementAccumulator{bar: MeasurementBar{Pool: fill.PoolAddress, Interval: interval, Start: start, First: fill.Position, Last: fill.Position}, open: new(big.Rat).Set(price), high: new(big.Rat).Set(price), low: new(big.Rat).Set(price), close: new(big.Rat).Set(price), usdl: new(big.Int), token: new(big.Int), buy: new(big.Int), sell: new(big.Int), actors: map[string]bool{}}
			r.bars[key] = acc
		} else {
			first, err := accounting.CompareSourcePosition(fill.Position, acc.bar.First)
			if err != nil {
				return err
			}
			last, err := accounting.CompareSourcePosition(fill.Position, acc.bar.Last)
			if err != nil {
				return err
			}
			if first < 0 {
				acc.bar.First = fill.Position
				acc.open.Set(price)
			}
			if last > 0 {
				acc.bar.Last = fill.Position
				acc.close.Set(price)
			}
			if price.Cmp(acc.high) > 0 {
				acc.high.Set(price)
			}
			if price.Cmp(acc.low) < 0 {
				acc.low.Set(price)
			}
		}
		if acc.bar.Trades == ^uint64(0) {
			return ErrInvalidFill
		}
		acc.bar.Trades++
		acc.usdl.Add(acc.usdl, usdl)
		acc.token.Add(acc.token, token)
		if fill.Side == "buy" {
			acc.buy.Add(acc.buy, usdl)
		} else {
			acc.sell.Add(acc.sell, usdl)
		}
		if fill.Attribution.Trader == nil {
			acc.bar.UnknownActors++
		} else {
			acc.actors[*fill.Attribution.Trader] = true
		}
	}
	return nil
}
func (r *MeasurementReference) Bars() []MeasurementBar {
	if r == nil {
		return nil
	}
	out := make([]MeasurementBar, 0, len(r.bars))
	price := func(p *big.Rat) Rational {
		return Rational{Numerator: p.Num().String(), Denominator: p.Denom().String()}
	}
	for _, acc := range r.bars {
		bar := acc.bar
		bar.Open = price(acc.open)
		bar.High = price(acc.high)
		bar.Low = price(acc.low)
		bar.Close = price(acc.close)
		bar.VolumeUsdlRaw = acc.usdl.String()
		bar.VolumeTokenRaw = acc.token.String()
		bar.BuyVolumeUsdlRaw = acc.buy.String()
		bar.SellVolumeUsdlRaw = acc.sell.String()
		bar.UniqueTraders = uint64(len(acc.actors))
		out = append(out, bar)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pool != out[j].Pool {
			return out[i].Pool < out[j].Pool
		}
		if out[i].Interval != out[j].Interval {
			return out[i].Interval < out[j].Interval
		}
		return out[i].Start < out[j].Start
	})
	return out
}
func MeasurementActual(bar SeriesCandle) (MeasurementBar, error) {
	if bar.Synthetic || bar.Trades == 0 || bar.Provenance != "actual_trade" || bar.AlgorithmVersion != SeriesAlgorithm {
		return MeasurementBar{}, ErrInvalidFill
	}
	return MeasurementBar{Pool: bar.PoolAddress, Interval: bar.Interval, Start: bar.Start, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, VolumeUsdlRaw: bar.VolumeUsdlRaw, VolumeTokenRaw: bar.VolumeTokenRaw, BuyVolumeUsdlRaw: bar.BuyVolumeUsdlRaw, SellVolumeUsdlRaw: bar.SellVolumeUsdlRaw, Trades: bar.Trades, UniqueTraders: bar.UniqueTraders, UnknownActors: bar.UnknownActorTradeCount, First: bar.OpenSource.Position, Last: bar.CloseSource.Position}, nil
}
func MeasurementDigest(bars []MeasurementBar) (string, error) {
	rows := append([]MeasurementBar(nil), bars...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Pool != rows[j].Pool {
			return rows[i].Pool < rows[j].Pool
		}
		if rows[i].Interval != rows[j].Interval {
			return rows[i].Interval < rows[j].Interval
		}
		return rows[i].Start < rows[j].Start
	})
	for i, row := range rows {
		if i > 0 && rows[i-1].Pool == row.Pool && rows[i-1].Interval == row.Interval && rows[i-1].Start == row.Start {
			return "", ErrInvalidFill
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
