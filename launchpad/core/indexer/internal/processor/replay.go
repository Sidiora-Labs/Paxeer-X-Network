package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

const ReplaySubjectV2 = "indexer.replay.v2"

type ReplayEnvelopeV2 struct {
	Version                  string          `json:"version"`
	Mode                     string          `json:"mode"`
	Target                   string          `json:"target"`
	Generation               string          `json:"generation"`
	ProjectionRevision       string          `json:"projectionRevision"`
	SourceStream             string          `json:"sourceStream"`
	RawDigest                string          `json:"rawDigest"`
	Raw                      store.RawRecord `json:"raw"`
	OriginalTimestampSeconds string          `json:"originalTimestampSeconds"`
	EventKind                string          `json:"eventKind"`
	Event                    json.RawMessage `json:"event"`
}
type ReplayRunner struct {
	Store                 *store.Store
	Source                QualifiedBatchSource
	Deliveries            *delivery.Store
	Archive               *Archive
	Owner, DeliverySchema string
	BatchSize             int
}

func (r *ReplayRunner) validate() error {
	if r == nil || r.Store == nil || !r.Store.CanonicalEpochGuarded() || r.Source == nil || r.Deliveries == nil || r.Owner == "" || r.DeliverySchema == "" || r.BatchSize < 1 || r.BatchSize > 1000 {
		return errors.New("replay requires actual guarded store, qualified source, durable outbox and bounded owner configuration")
	}
	return nil
}
func qualifiedSegment(ctx context.Context, stream string, b store.CanonicalBatch, receipts []*receipt.QualifiedReceipt) (store.ReplaySegment, error) {
	var segment store.ReplaySegment
	if len(b.Blocks) == 0 {
		return segment, errors.New("complete source blocks required")
	}
	if err := receipt.ValidateBindings(ctx, b, receipts); err != nil {
		return segment, err
	}
	segment = store.ReplaySegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: stream, FromHeight: b.Blocks[0].Height, ToHeight: b.Blocks[len(b.Blocks)-1].Height, Blocks: b.Blocks, Records: b.Records, Events: []store.ReplayEvent{}}
	raw := map[string]store.RawRecord{}
	for _, record := range b.Records {
		raw[record.TransactionHash+":"+strconv.FormatUint(uint64(record.LogIndex), 10)] = record
	}
	creations, err := qualifiedCreationFacts(ctx, receipts)
	if err != nil {
		return segment, err
	}
	for _, creation := range creations {
		record, ok := raw[creation.Position.TransactionHash+":"+creation.Position.LogIndex]
		if !ok {
			return segment, store.ErrIntegrity
		}
		payload, err := json.Marshal(creation)
		if err != nil {
			return segment, err
		}
		digest, err := store.RawDigest(record)
		if err != nil {
			return segment, err
		}
		timestamp, err := strconv.ParseInt(creation.BlockTimestampSeconds, 10, 64)
		if err != nil {
			return segment, err
		}
		segment.Events = append(segment.Events, store.ReplayEvent{RawDigest: digest, Kind: "canonical.creation.v1", Timestamp: timestamp, Raw: record, Payload: payload})
	}
	for _, q := range receipts {
		_, _, timestamp, err := q.SourceContext(ctx)
		if err != nil {
			return segment, err
		}
		fills, err := q.Fills(ctx)
		if err != nil {
			return segment, err
		}
		for _, fill := range fills {
			record, ok := raw[fill.Position.TransactionHash+":"+fill.Position.LogIndex]
			if !ok {
				return segment, store.ErrIntegrity
			}
			payload, err := json.Marshal(fill)
			if err != nil {
				return segment, err
			}
			digest, err := store.RawDigest(record)
			if err != nil {
				return segment, err
			}
			segment.Events = append(segment.Events, store.ReplayEvent{RawDigest: digest, Kind: accounting.MarketSubject, Timestamp: timestamp, Raw: record, Payload: payload})
		}
		events, err := qualifiedEventsWithActors(ctx, q)
		if err != nil {
			return segment, err
		}
		for _, event := range events {
			if event == nil {
				return segment, store.ErrIntegrity
			}
			if event.EventName == "Swap" || event.EventName == "MarketCreated" {
				continue
			}
			record, ok := raw[event.TxHash+":"+strconv.FormatUint(uint64(event.LogIndex), 10)]
			if !ok {
				return segment, store.ErrIntegrity
			}
			payload, err := json.Marshal(event)
			if err != nil {
				return segment, err
			}
			digest, err := store.RawDigest(record)
			if err != nil {
				return segment, err
			}
			segment.Events = append(segment.Events, store.ReplayEvent{RawDigest: digest, Kind: event.EventName, Timestamp: timestamp, Raw: record, Payload: payload})
		}
	}
	return segment, store.ValidateReplaySegment(segment)
}
func (r *ReplayRunner) fetch(ctx context.Context, request store.ReplayRequest, from, to int64) (store.ReplaySegment, error) {
	batch, receipts, err := r.Source.FetchQualifiedBatch(ctx, uint64(from), uint64(to))
	if err != nil {
		return store.ReplaySegment{}, err
	}
	segment, err := qualifiedSegment(ctx, request.SourceStream, batch, receipts)
	if err != nil {
		return segment, err
	}
	if segment.FromHeight != from || segment.ToHeight != to {
		return segment, store.ErrIntegrity
	}
	return segment, nil
}
func (r *ReplayRunner) Run(ctx context.Context, request store.ReplayRequest) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := r.Store.AdmitReplay(ctx, request); err != nil {
		return err
	}
	active, err := r.Store.AcquireReplay(ctx, request.JobID, r.Owner, 30*time.Second)
	if errors.Is(err, store.ErrReplayComplete) {
		return nil
	}
	if err != nil {
		return err
	}
	checkpoint, err := r.fetch(ctx, request, request.CheckpointHeight, request.CheckpointHeight)
	if err != nil {
		return r.pauseOwned(ctx, active, err, request.CheckpointHeight, request.CheckpointHeight)
	}
	if checkpoint.Blocks[0].Hash != request.CheckpointHash {
		retained := r.Store.RecordReplayConflict(ctx, request, checkpoint, "authorized checkpoint disagreement")
		return r.pauseOwned(ctx, active, errors.Join(store.ErrIntegrity, retained), request.CheckpointHeight, request.CheckpointHeight)
	}
	if err = r.Store.BindReplayCheckpoint(ctx, request, checkpoint.Blocks[0]); err != nil {
		return r.pauseOwned(ctx, active, err, request.CheckpointHeight, request.CheckpointHeight)
	}
	for {
		if err = ctx.Err(); err != nil {
			return r.pauseOwned(ctx, active, err, active.Progress+1, request.ToHeight)
		}
		job, err := r.Store.AcquireReplay(ctx, request.JobID, r.Owner, 30*time.Second)
		if errors.Is(err, store.ErrReplayComplete) {
			return nil
		}
		if err != nil {
			return err
		}
		active = job
		from := job.Progress + 1
		to := from + int64(r.BatchSize) - 1
		if to > request.ToHeight {
			to = request.ToHeight
		}
		readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		var segment store.ReplaySegment
		if request.Mode == "reconstruct" {
			if r.Archive == nil {
				cancel()
				return r.pauseOwned(ctx, job, errors.New("archive provider unavailable"), from, to)
			}
			segment, err = r.Archive.Restore(readCtx, request.SourceStream, from, to)
		} else {
			segment, err = r.fetch(readCtx, request, from, to)
		}
		cancel()
		if err != nil {
			return r.pauseOwned(ctx, job, err, from, to)
		}
		job, err = r.Store.AcquireReplay(ctx, request.JobID, r.Owner, 30*time.Second)
		if err != nil {
			return err
		}
		err = r.Store.CommitReplaySegment(ctx, job, segment, func(ctx context.Context, tx pgx.Tx, event store.ReplayEvent) (string, error) {
			wrapper := ReplayEnvelopeV2{Version: "indexer-replay-v2", Mode: "historical", Target: request.Target, Generation: request.Generation, ProjectionRevision: request.ProjectionRevision, SourceStream: request.SourceStream, RawDigest: event.RawDigest, Raw: event.Raw, OriginalTimestampSeconds: strconv.FormatInt(event.Timestamp, 10), EventKind: event.Kind, Event: event.Payload}
			payload, err := canonicalReplayPayload(wrapper)
			if err != nil {
				return "", err
			}
			effect := ReplayEffectIdentity(request, event)
			envelope := delivery.Envelope{EventID: effect, EventType: ReplaySubjectV2, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(event.Raw.Height, 10), TransactionIndex: strconv.FormatUint(uint64(event.Raw.TransactionIndex), 10), LogIndex: strconv.FormatUint(uint64(event.Raw.LogIndex), 10)}, EmittedAt: time.Unix(event.Timestamp, 0).UTC(), TraceID: effect, PayloadHash: delivery.Hash(payload), Payload: payload}
			return effect, r.Deliveries.Enqueue(ctx, tx, ReplaySubjectV2, envelope)
		})
		if err != nil {
			if ctx.Err() != nil {
				return r.pauseOwned(ctx, job, ctx.Err(), from, to)
			}
			if errors.Is(err, store.ErrIntegrity) {
				err = errors.Join(err, r.Store.RecordReplayConflict(ctx, request, segment, "immutable source disagreement"))
			}
			return r.pauseOwned(ctx, job, err, from, to)
		}
		active = job
		active.Progress = to
		if to == request.ToHeight {
			return nil
		}
	}
}
func ReplayEffectIdentity(request store.ReplayRequest, event store.ReplayEvent) string {
	return accounting.HashTuple("indexer-replay-effect-v2", request.SourceStream, request.Target, request.Generation, request.ProjectionRevision, event.RawDigest, event.Kind)
}
func (r *ReplayRunner) pauseOwned(ctx context.Context, job store.ReplayJob, cause error, from, to int64) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return errors.Join(cause, r.Store.PauseReplayLease(cleanup, job, cause.Error(), from, to))
}
func (r *ReplayRunner) RecoverMissedRange(ctx context.Context, request store.ReplayRequest) error {
	if request.Mode != "replay" {
		return fmt.Errorf("missed range requires explicit replay mode")
	}
	return r.Run(ctx, request)
}

type GenerationReplayProjection struct {
	Runner  *ReplayRunner
	Request store.ReplayRequest
}
type generationFact struct {
	RawDigest string
	Raw       store.RawRecord
	Events    []store.ReplayEvent
}

func NewGenerationReplayProjection(runner *ReplayRunner, request store.ReplayRequest) (*GenerationReplayProjection, error) {
	if err := runner.validate(); err != nil {
		return nil, err
	}
	if err := store.ValidateReplayRequest(request); err != nil {
		return nil, err
	}
	if request.Mode != "reconstruct" {
		return nil, errors.New("isolated reconstruction request required")
	}
	return &GenerationReplayProjection{Runner: runner, Request: request}, nil
}
func generationFacts(segment store.ReplaySegment) []generationFact {
	facts := make([]generationFact, 0, len(segment.Records))
	for _, raw := range segment.Records {
		digest, _ := store.RawDigest(raw)
		fact := generationFact{RawDigest: digest, Raw: raw, Events: []store.ReplayEvent{}}
		for _, event := range segment.Events {
			if event.RawDigest == digest {
				fact.Events = append(fact.Events, event)
			}
		}
		facts = append(facts, fact)
	}
	return facts
}
func (p *GenerationReplayProjection) ApplyRawTx(ctx context.Context, tx pgx.Tx, generation string, raw delivery.ArchiveRaw) error {
	if p == nil || p.Runner == nil || tx == nil || generation != p.Request.Generation || raw.Height < p.Request.FromHeight || raw.Height > p.Request.ToHeight {
		return store.ErrIntegrity
	}
	segment, err := p.Runner.fetch(ctx, p.Request, raw.Height, raw.Height)
	if err != nil {
		return err
	}
	original := store.RawRecord{TransactionHash: raw.TransactionHash, BlockHash: raw.BlockHash, Emitter: raw.Emitter, Height: raw.Height, TransactionIndex: raw.TransactionIndex, LogIndex: raw.LogIndex, Topics: raw.Topics, Data: raw.Data}
	digest, err := store.RawDigest(original)
	if err != nil {
		return err
	}
	var found *generationFact
	for _, fact := range generationFacts(segment) {
		if fact.RawDigest == digest {
			copy := fact
			found = &copy
			break
		}
	}
	if found == nil {
		return store.ErrIntegrity
	}
	body, err := json.Marshal(found)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_generation_raw(target,generation,projection_revision,height,transaction_index,log_index,transaction_hash,raw_digest,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, p.Request.Target, generation, p.Request.ProjectionRevision, raw.Height, int64(raw.TransactionIndex), int64(raw.LogIndex), raw.TransactionHash, digest, body)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT raw_digest=$8 AND payload=$9::jsonb FROM indexer.indexer_generation_raw WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND height=$4 AND transaction_index=$5 AND log_index=$6 AND transaction_hash=$7`, p.Request.Target, generation, p.Request.ProjectionRevision, raw.Height, int64(raw.TransactionIndex), int64(raw.LogIndex), raw.TransactionHash, digest, body).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return store.ErrIntegrity
	}
	for _, event := range found.Events {
		wrapper := ReplayEnvelopeV2{Version: "indexer-replay-v2", Mode: "historical", Target: p.Request.Target, Generation: generation, ProjectionRevision: p.Request.ProjectionRevision, SourceStream: p.Request.SourceStream, RawDigest: event.RawDigest, Raw: event.Raw, OriginalTimestampSeconds: strconv.FormatInt(event.Timestamp, 10), EventKind: event.Kind, Event: event.Payload}
		payload, err := canonicalReplayPayload(wrapper)
		if err != nil {
			return err
		}
		effect := ReplayEffectIdentity(p.Request, event)
		envelope := delivery.Envelope{EventID: effect, EventType: ReplaySubjectV2, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(raw.Height, 10), TransactionIndex: strconv.FormatUint(uint64(raw.TransactionIndex), 10), LogIndex: strconv.FormatUint(uint64(raw.LogIndex), 10)}, EmittedAt: time.Unix(event.Timestamp, 0).UTC(), TraceID: effect, PayloadHash: delivery.Hash(payload), Payload: payload}
		if err = p.Runner.Deliveries.Enqueue(ctx, tx, ReplaySubjectV2, envelope); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_effects(target,generation,projection_revision,effect_id,raw_digest,height,event_kind,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, p.Request.Target, generation, p.Request.ProjectionRevision, effect, event.RawDigest, event.Raw.Height, event.Kind, event.Payload)
		if err != nil {
			return err
		}
	}
	return nil
}
func (p *GenerationReplayProjection) ProjectionDigestTx(ctx context.Context, tx pgx.Tx, generation string) (string, error) {
	if p == nil || tx == nil || generation != p.Request.Generation {
		return "", store.ErrIntegrity
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM indexer.indexer_generation_raw WHERE target=$1 AND generation=$2 AND projection_revision=$3 ORDER BY height,transaction_index,log_index,transaction_hash`, p.Request.Target, generation, p.Request.ProjectionRevision)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	facts := []generationFact{}
	size := 0
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return "", err
		}
		size += len(body)
		if size > 64<<20 || len(facts) >= 100000 {
			return "", errors.New("generation comparison exceeds bounded admission")
		}
		var fact generationFact
		if err = json.Unmarshal(body, &fact); err != nil {
			return "", err
		}
		facts = append(facts, fact)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	for _, fact := range facts {
		for _, event := range fact.Events {
			effect := ReplayEffectIdentity(p.Request, event)
			var valid bool
			err = tx.QueryRow(ctx, `SELECT raw_digest=$5 AND event_kind=$6 AND payload=$7::jsonb FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND effect_id=$4`, p.Request.Target, generation, p.Request.ProjectionRevision, effect, event.RawDigest, event.Kind, event.Payload).Scan(&valid)
			if err != nil {
				return "", err
			}
			if !valid {
				return "", store.ErrIntegrity
			}
		}
	}
	expectedCount := 0
	for _, fact := range facts {
		for _, event := range fact.Events {
			expectedCount++
			wrapper := ReplayEnvelopeV2{Version: "indexer-replay-v2", Mode: "historical", Target: p.Request.Target, Generation: generation, ProjectionRevision: p.Request.ProjectionRevision, SourceStream: p.Request.SourceStream, RawDigest: event.RawDigest, Raw: event.Raw, OriginalTimestampSeconds: strconv.FormatInt(event.Timestamp, 10), EventKind: event.Kind, Event: event.Payload}
			payload, err := canonicalReplayPayload(wrapper)
			if err != nil {
				return "", err
			}
			effect := ReplayEffectIdentity(p.Request, event)
			var outbox bool
			err = tx.QueryRow(ctx, "SELECT payload_hash=$2 AND subject=$3 FROM "+pgx.Identifier{p.Runner.DeliverySchema}.Sanitize()+".delivery_outbox WHERE event_id=$1", effect, delivery.Hash(payload), ReplaySubjectV2).Scan(&outbox)
			if err != nil {
				return "", err
			}
			if !outbox {
				return "", store.ErrIntegrity
			}
		}
	}
	var actualCount int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3`, p.Request.Target, generation, p.Request.ProjectionRevision).Scan(&actualCount)
	if err != nil {
		return "", err
	}
	if actualCount != expectedCount {
		return "", store.ErrIntegrity
	}
	return generationDigest(facts)
}
func (p *GenerationReplayProjection) CanonicalDigestTx(ctx context.Context, tx pgx.Tx, manifest delivery.ArchiveManifest) (string, error) {
	if p == nil || tx == nil || manifest.SourceStream != p.Request.SourceStream || manifest.FromHeight != p.Request.FromHeight || manifest.ToHeight != p.Request.ToHeight {
		return "", store.ErrIntegrity
	}
	if err := delivery.ValidateArchiveManifest(manifest); err != nil {
		return "", err
	}
	facts := []generationFact{}
	size := 0
	for _, block := range manifest.Blocks {
		segment, err := p.Runner.fetch(ctx, p.Request, block.Height, block.Height)
		if err != nil {
			return "", err
		}
		if segment.Blocks[0].Hash != block.BlockHash || int64(len(segment.Records)) != block.RawCount {
			return "", store.ErrIntegrity
		}
		for _, fact := range generationFacts(segment) {
			body, err := json.Marshal(fact)
			if err != nil {
				return "", err
			}
			size += len(body)
			if size > 64<<20 || len(facts) >= 100000 {
				return "", errors.New("canonical generation comparison exceeds bounded admission")
			}
			facts = append(facts, fact)
		}
	}
	return generationDigest(facts)
}

func generationDigest(facts []generationFact) (string, error) {
	wire, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	var exact any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err = decoder.Decode(&exact); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(exact)
	if err != nil {
		return "", err
	}
	return delivery.Hash(canonical), nil
}

func canonicalReplayPayload(wrapper ReplayEnvelopeV2) ([]byte, error) {
	wire, err := json.Marshal(wrapper)
	if err != nil {
		return nil, err
	}
	var exact any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err = decoder.Decode(&exact); err != nil {
		return nil, err
	}
	return json.Marshal(exact)
}
