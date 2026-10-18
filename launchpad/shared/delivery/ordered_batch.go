package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
)

type OrderedDelivery struct {
	Subject  string
	Wire     []byte
	Delivery BrokerDelivery
}
type OrderedEffect struct {
	Envelope Envelope
	Position GuardedPosition
}
type OrderedBatchFailure struct {
	EventID string
	Cause   error
}

func (e *OrderedBatchFailure) Error() string {
	return "delivery: ordered batch effect " + e.EventID + ": " + e.Cause.Error()
}
func (e *OrderedBatchFailure) Unwrap() error { return e.Cause }

type OrderedBatchHooks struct {
	Guard  func(context.Context, pgx.Tx, Envelope) (GuardedPosition, error)
	Effect func(context.Context, pgx.Tx, []OrderedEffect) error
	Freeze func(context.Context, pgx.Tx, []string) error
}

func (s *Store) ConsumeOrderedBatch(ctx context.Context, consumer, generation string, inputs []OrderedDelivery, h OrderedBatchHooks) error {
	if s == nil || ctx == nil || !token.MatchString(consumer) || !token.MatchString(generation) || len(inputs) < 1 || len(inputs) > 64 || h.Guard == nil || h.Effect == nil {
		return errors.New("delivery: bounded ordered batch and hooks required")
	}
	events := make([]Envelope, len(inputs))
	brokers := make([]BrokerPosition, len(inputs))
	owned := make([]OrderedDelivery, len(inputs))
	total := 0
	for i, in := range inputs {
		if in.Delivery == nil {
			return ErrIntegrity
		}
		total += len(in.Wire)
		if total > 8*MaxEnvelopeBytes {
			return errors.New("delivery: ordered batch byte limit")
		}
		owned[i] = OrderedDelivery{Subject: in.Subject, Wire: append([]byte(nil), in.Wire...), Delivery: in.Delivery}
		var err error
		events[i], err = Decode(owned[i].Wire, s.policy, in.Subject)
		if err != nil {
			if len(in.Wire) <= MaxEnvelopeBytes && len(in.Subject) <= 256 && subjectToken.MatchString(in.Subject) {
				qerr := s.QuarantineAfterRollback(ctx, QuarantineInput{ConsumerID: consumer, Generation: generation, Subject: in.Subject, Wire: owned[i].Wire, WireSHA256: accounting.Digest(Hash(in.Wire)), Class: InnerSchema, EvidenceDigest: accounting.Digest(Hash(in.Wire))}, nil)
				return errors.Join(err, qerr)
			}
			return err
		}
		brokers[i], err = in.Delivery.BrokerPosition()
		if err != nil {
			return err
		}
		b := brokers[i]
		if b.Subject != in.Subject || b.EventID != events[i].EventID || b.WireSHA256 != Hash(in.Wire) || !token.MatchString(b.Stream) || b.Sequence == 0 || b.Sequence > 1<<63-1 {
			return ErrIntegrity
		}
	}
	partitions := &PartitionStore{pool: s.pool, schema: s.schema}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		positions := make([]GuardedPosition, len(inputs))
		failed := 0
		err = func() error {
			if err := s.assertEpochTx(ctx, tx); err != nil {
				return err
			}
			effects := make([]OrderedEffect, 0, len(inputs))
			for i, in := range owned {
				failed = i
				e := events[i]
				publication, err := accounting.DecodePublicationV1(e.Payload)
				if err != nil {
					return &IntegrityFailure{Class: InnerSchema, Cause: err}
				}
				g, err := h.Guard(ctx, tx, e)
				positions[i] = g
				if err != nil {
					return err
				}
				pools := []string{}
				if publication.Fill != nil {
					pools = append(pools, publication.Fill.PoolAddress)
				}
				binding := func() error {
					return &IntegrityFailure{Class: PartitionBinding, AffectedPools: pools, EvidenceDigest: accounting.Digest(Hash(e.Payload)), Cause: ErrIntegrity}
				}
				if g.Lease.Key.ConsumerID != consumer || g.Lease.Key.ProjectionGeneration != generation || publication.PublicationID != e.EventID || in.Subject != "accounting."+publication.Facet+".v1" {
					return binding()
				}
				matched := false
				for _, p := range publication.Positions {
					if reflect.DeepEqual(p, g.Position) {
						matched = true
						break
					}
				}
				if !matched || g.Position.SourceOrder.ChainID != "125" || g.Position.SourceOrder.Height != e.SourceOrder.Height || g.Position.SourceOrder.TransactionIndex != e.SourceOrder.TransactionIndex || g.Position.SourceOrder.LogIndex != e.SourceOrder.LogIndex {
					return binding()
				}
				checked, err := partitions.CheckAndLockTx(ctx, tx, g.Lease, g.Position)
				if err != nil {
					return err
				}
				if checked.DuplicateCommitted != g.DuplicateCommitted {
					return binding()
				}
				tag, err := tx.Exec(ctx, "INSERT INTO "+s.table("delivery_inbox")+"(consumer_id,projection_generation,event_id,envelope_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", consumer, generation, e.EventID, Hash(in.Wire))
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 1 {
					if g.DuplicateCommitted {
						return binding()
					}
					effects = append(effects, OrderedEffect{e, g})
				} else {
					var prior string
					if err = tx.QueryRow(ctx, "SELECT envelope_hash FROM "+s.table("delivery_inbox")+" WHERE consumer_id=$1 AND projection_generation=$2 AND event_id=$3", consumer, generation, e.EventID).Scan(&prior); err != nil {
						return err
					}
					if prior != Hash(in.Wire) || !g.DuplicateCommitted {
						return binding()
					}
				}
				if err = partitions.AdvanceTx(ctx, tx, g); err != nil {
					return err
				}
				if !g.DuplicateCommitted {
					sequence, err := strconv.ParseInt(g.Position.Sequence, 10, 64)
					if err != nil {
						return err
					}
					raw, err := json.Marshal(e.SourceOrder)
					if err != nil {
						return err
					}
					b := brokers[i]
					tag, err = tx.Exec(ctx, "INSERT INTO "+s.table("delivery_consumer_watermarks")+`(consumer_id,generation,partition_kind,partition_key,broker_stream,broker_sequence,source_order,position_sequence,event_id,envelope_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(consumer_id,generation,partition_kind,partition_key) DO UPDATE SET broker_sequence=EXCLUDED.broker_sequence,source_order=EXCLUDED.source_order,position_sequence=EXCLUDED.position_sequence,event_id=EXCLUDED.event_id,envelope_hash=EXCLUDED.envelope_hash,committed_at=clock_timestamp() WHERE delivery_consumer_watermarks.broker_stream=EXCLUDED.broker_stream AND delivery_consumer_watermarks.position_sequence=EXCLUDED.position_sequence-1`, consumer, generation, g.Position.PartitionKind, g.Position.PartitionKey, b.Stream, int64(b.Sequence), raw, sequence, e.EventID, Hash(in.Wire))
					if err != nil {
						return err
					}
					if tag.RowsAffected() != 1 {
						return binding()
					}
				}
			}
			if len(effects) > 0 {
				if err := h.Effect(ctx, tx, effects); err != nil {
					var failure *OrderedBatchFailure
					if errors.As(err, &failure) {
						found := false
						for i, e := range events {
							if e.EventID == failure.EventID {
								failed = i
								found = true
								break
							}
						}
						if !found {
							return errors.Join(ErrIntegrity, err)
						}
					}
					return err
				}
			}
			if err := s.assertEpochTx(ctx, tx); err != nil {
				return err
			}
			for _, g := range positions {
				if err := partitions.finalFenceTx(ctx, tx, g.Lease); err != nil {
					return err
				}
				if err := partitions.verifyCommitTx(ctx, tx, g); err != nil {
					return err
				}
			}
			return tx.Commit(ctx)
		}()
		rollbackBounded(tx)
		if err == nil {
			for _, in := range owned {
				if err = in.Delivery.Ack(ctx); err != nil {
					return err
				}
			}
			return nil
		}
		var integrity *IntegrityFailure
		if errors.As(err, &integrity) && poisonClass(integrity.Class) {
			in := owned[failed]
			evidence := integrity.EvidenceDigest
			if evidence == "" {
				evidence = accounting.Digest(Hash(in.Wire))
			}
			q := QuarantineInput{ConsumerID: consumer, Generation: generation, Subject: in.Subject, PublicationID: events[failed].EventID, Wire: in.Wire, WireSHA256: accounting.Digest(Hash(in.Wire)), Class: integrity.Class, AffectedPools: append([]string(nil), integrity.AffectedPools...), EvidenceDigest: evidence}
			if validConsumerKey(positions[failed].Lease.Key) {
				lease := positions[failed].Lease
				q.Lease = &lease
			}
			return errors.Join(err, s.QuarantineAfterRollback(ctx, q, h.Freeze))
		}
		if !retryWholeTransaction(err) || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ErrIntegrity
}
