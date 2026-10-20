package delivery

import (
	"context"
	"errors"
	"time"
)

// ErrBrokerDown marks a publication that failed because the broker could not
// be reached (no connection, no responders, JetStream unavailable, or no
// acknowledgement before any message of the batch persisted). Such a failure
// says nothing about the message: the relay puts the row back without
// spending an attempt and ends the pass. The BatchPublisher wraps its error
// with it.
var ErrBrokerDown = errors.New("delivery: broker unreachable")

// PersistentPublisher must await the server JetStream persistence acknowledgement
// (not socket write completion), set Nats-Msg-Id to eventID, and return the actual
// stream and sequence. natsdriver.Publisher supplies this contract.
type PersistentPublisher interface {
	Publish(context.Context, string, string, []byte) (PersistenceAck, error)
}

// BatchPublisher publishes leases in their order on one connection, so the
// broker assigns stream sequences in Perps-Seq order, and returns one
// Publication per lease. A control lease is published as its ControlMessage
// under the Nats-Msg-Id the message names, never as its envelope. Each Publication carries the server JetStream
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
	// OnQuarantine, when set, is called once for every row the relay
	// quarantines permanently, after the transition committed.
	OnQuarantine func(Quarantine)
}

func (r Relay) valid() error {
	if r.Store == nil || r.Publisher == nil || !token.MatchString(r.Owner) || r.Batch < 1 || r.Batch > 1000 || r.MaxAttempts < 1 || r.MaxAttempts > 100 || r.PublishTimeout <= 0 || r.PublishTimeout >= r.LeaseDuration || r.RetryDelay < time.Second || r.RetryDelay > time.Hour {
		return errors.New("delivery: invalid relay configuration")
	}
	return nil
}

// Pass is the relay loop step: it requeues the rows an earlier release
// quarantined as transient whose backoff elapsed and runs RunOnce, which
// stops at any such row still backing off, so a requeued row is never
// published behind a later one.
func (r Relay) Pass(ctx context.Context) error {
	if err := r.valid(); err != nil {
		return err
	}
	if err := r.Store.Requeue(ctx); err != nil {
		return err
	}
	return r.run(ctx)
}

// RunOnce claims up to Batch open rows in Perps-Seq order, stopping at a row
// that is backing off, leased elsewhere or quarantined as transient, checks
// their attempt budget, publishes them as one pipelined batch bounded by
// PublishTimeout and records the acknowledged rows, each step in one
// transaction. Every lease of the batch outlives PublishTimeout, so no row
// expires behind a slow publication. A row whose publication failed keeps its
// attempt count and is put back by Retry with backoff; the unacknowledged
// rows after it are released without spending an attempt and wait behind it,
// and the acknowledged rows of the batch are recorded. A row that exhausted
// its budget, lost its lease or fails to decode holds back the rest of the
// batch the same way. A row whose publication failed with ErrBrokerDown is
// released like the rows behind it: an unreachable broker spends no attempt,
// and the pass reports the failure. A row that fails to decode, that the broker refuses for
// its size or whose transient failures spent MaxAttempts attempts is
// quarantined permanently; a later pass publishes its control message in its
// Perps-Seq place and then the rows behind it, so no row holds the outbox
// without end. Claims are serialised and stop at every leased row, so
// replicas publish one batch at a time; fenced leases refuse a stale relay.
func (r Relay) RunOnce(ctx context.Context) error {
	if err := r.valid(); err != nil {
		return err
	}
	return r.run(ctx)
}

func (r Relay) run(ctx context.Context) error {
	claimed, err := r.Store.claim(ctx, r.Owner, r.Batch, r.LeaseDuration, true)
	if err != nil || len(claimed) == 0 {
		return err
	}
	allowed, exhausted, failure := r.Store.CheckAttemptBudgetBatch(ctx, claimed, r.MaxAttempts)
	if failure != nil && !errors.Is(failure, ErrLease) {
		return failure
	}
	r.quarantined(exhausted)
	may := make(map[string]bool, len(allowed))
	for _, l := range allowed {
		may[l.EventID] = true
	}
	var retry, release []Lease
	var classes []FailureClass
	send := make([]Lease, 0, len(allowed))
	held := false
	for _, l := range claimed {
		if !may[l.EventID] {
			held = true
			continue
		}
		if held {
			release = append(release, l)
			continue
		}
		if !l.Control {
			if _, err := Decode(l.Wire, r.Store.policy, l.Subject); err != nil {
				class := SchemaViolation
				if errors.Is(err, ErrIntegrity) {
					class = IntegrityConflict
				}
				retry, classes = append(retry, l), append(classes, class)
				failure = errors.Join(failure, err)
				held = true
				continue
			}
		}
		send = append(send, l)
	}
	var published []Lease
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
			case errors.Is(res.Err, ErrBrokerDown):
				// The broker was not reached: the row goes back unspent.
				release = append(release, l)
				failure = errors.Join(failure, res.Err)
				blocked = true
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
		quarantined, err := r.Store.RetryBatch(ctx, retry, classes, r.MaxAttempts, r.RetryDelay)
		if err != nil {
			failure = errors.Join(failure, err)
		}
		r.quarantined(quarantined)
	}
	if len(release) > 0 {
		if err := r.Store.ReleaseBatch(ctx, release); err != nil {
			failure = errors.Join(failure, err)
		}
	}
	return failure
}

func (r Relay) quarantined(rows []Quarantine) {
	if r.OnQuarantine == nil {
		return
	}
	for _, q := range rows {
		r.OnQuarantine(q)
	}
}

func publicationFailureClass(err error) FailureClass {
	if errors.Is(err, ErrOversized) {
		return Oversized
	}
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
