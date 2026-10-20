package delivery

import (
	"context"
	"errors"
	"time"
)

// PersistentPublisher must await the server JetStream persistence acknowledgement
// (not socket write completion), set Nats-Msg-Id to eventID, and return the actual
// stream and sequence. natsdriver.Publisher supplies this contract.
type PersistentPublisher interface {
	Publish(context.Context, string, string, []byte) (PersistenceAck, error)
}

// BatchPublisher publishes leases in their order on one connection, so the
// broker assigns stream sequences in Perps-Seq order, and returns one
// Publication per lease. Each Publication carries the server JetStream
// persistence acknowledgement of a message whose Nats-Msg-Id is the lease
// event id, or the error of that message. A message must persist only after
// every earlier message of the batch persisted, so a failed message never
// lands behind a later one.
type BatchPublisher interface {
	PublishBatch(context.Context, []Lease) []Publication
}

// Publication is the result of publishing one lease.
type Publication struct {
	Ack PersistenceAck
	Err error
}

type Relay struct {
	Store                                     *Store
	Publisher                                 BatchPublisher
	Owner                                     string
	Batch                                     int
	LeaseDuration, PublishTimeout, RetryDelay time.Duration
	MaxAttempts                               int
}

func (r Relay) valid() error {
	if r.Store == nil || r.Publisher == nil || !token.MatchString(r.Owner) || r.Batch < 1 || r.Batch > 1000 || r.MaxAttempts < 1 || r.MaxAttempts > 100 || r.PublishTimeout <= 0 || r.PublishTimeout >= r.LeaseDuration || r.RetryDelay < time.Second || r.RetryDelay > time.Hour {
		return errors.New("delivery: invalid relay configuration")
	}
	return nil
}

// Pass is the relay loop step: it requeues the transient quarantined rows
// whose backoff elapsed and runs RunOnce over the rows ahead of any transient
// quarantined row still backing off, so a requeued row is never published
// behind a later one.
func (r Relay) Pass(ctx context.Context) error {
	if err := r.valid(); err != nil {
		return err
	}
	below, err := r.Store.Requeue(ctx)
	if err != nil {
		return err
	}
	return r.run(ctx, below)
}

// RunOnce claims up to Batch pending rows in Perps-Seq order, checks their
// attempt budget, publishes them as one pipelined batch bounded by
// PublishTimeout and records the acknowledged rows, each step in one
// transaction. Every lease of the batch outlives PublishTimeout, so no row
// expires behind a slow publication. A row whose publication failed keeps its
// attempt count and is put back by Retry with backoff; the unacknowledged
// rows after it are released without spending an attempt and wait behind it,
// and the acknowledged rows of the batch are recorded. One relay runs per
// writer lease; fenced leases refuse a stale one.
func (r Relay) RunOnce(ctx context.Context) error {
	if err := r.valid(); err != nil {
		return err
	}
	return r.run(ctx, 0)
}

// run is RunOnce over the rows below the Perps-Seq below; 0 sets no limit.
func (r Relay) run(ctx context.Context, below int64) error {
	leases, err := r.Store.claim(ctx, r.Owner, r.Batch, r.LeaseDuration, below)
	if err != nil || len(leases) == 0 {
		return err
	}
	leases, failure := r.Store.CheckAttemptBudgetBatch(ctx, leases, r.MaxAttempts)
	if failure != nil && !errors.Is(failure, ErrLease) {
		return failure
	}
	var retry []Lease
	var classes []FailureClass
	send := make([]Lease, 0, len(leases))
	for _, l := range leases {
		if _, err := Decode(l.Wire, r.Store.policy, l.Subject); err != nil {
			class := SchemaViolation
			if errors.Is(err, ErrIntegrity) {
				class = IntegrityConflict
			}
			retry, classes = append(retry, l), append(classes, class)
			failure = errors.Join(failure, err)
			continue
		}
		send = append(send, l)
	}
	var published, release []Lease
	var acks []PersistenceAck
	if len(send) > 0 {
		pubctx, cancel := context.WithTimeout(ctx, r.PublishTimeout)
		results := r.Publisher.PublishBatch(pubctx, send)
		cancel()
		// Only the first failed row is a failure; the rows after it that the
		// broker did not persist wait behind it.
		blocked := false
		for i, l := range send {
			res := Publication{Err: errors.New("delivery: publication result missing")}
			if i < len(results) {
				res = results[i]
			}
			switch {
			case res.Err == nil:
				published, acks = append(published, l), append(acks, res.Ack)
			case blocked:
				release = append(release, l)
			default:
				retry, classes = append(retry, l), append(classes, publicationFailureClass(res.Err))
				failure = errors.Join(failure, res.Err)
				blocked = true
			}
		}
	}
	if len(published) > 0 {
		if err := r.Store.MarkPublishedBatch(ctx, published, acks); err != nil {
			failure = errors.Join(failure, err)
			// A rolled-back record leaves the published rows leased; put them back
			// so the next pass republishes them under the same Nats-Msg-Id.
			if !errors.Is(err, ErrLease) {
				for _, l := range published {
					retry, classes = append(retry, l), append(classes, Transient)
				}
			}
		}
	}
	if len(retry) > 0 {
		// A crash before this leaves the exact immutable rows reclaimable after lease expiry.
		if err := r.Store.RetryBatch(ctx, retry, classes, r.MaxAttempts, r.RetryDelay); err != nil {
			failure = errors.Join(failure, err)
		}
	}
	if len(release) > 0 {
		if err := r.Store.ReleaseBatch(ctx, release); err != nil {
			failure = errors.Join(failure, err)
		}
	}
	return failure
}

func publicationFailureClass(err error) FailureClass {
	if errors.Is(err, ErrAuthorization) {
		return Authorization
	}
	if errors.Is(err, ErrIntegrity) {
		return IntegrityConflict
	}
	if errors.Is(err, ErrSchema) {
		return SchemaViolation
	}
	return Transient
}
