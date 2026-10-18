package broker

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"time"
)

var ErrFrame = errors.New("invalid stream frame")
var ErrStale = errors.New("stale projection revision")
var ErrSequence = errors.New("backwards topic sequence")

// DurableConsumer is the gateway boundary for an authenticated JetStream pull
// consumer. Ack must confirm processing; failures remain available for redelivery.
// An implementation must resume its durable name, never a Redis subscription.
type DurableConsumer interface {
	Next(context.Context) (DurableDelivery, error)
	Close() error
}
type DurableDelivery interface {
	Payload() []byte
	StreamSequence() uint64
	Partition() string
	Ack(context.Context) error
	Retry(context.Context) error
}

// Frame preserves the frozen AsyncAPI camelCase envelope. Schema version is
// selected by the consumer binding, not invented as a required wire field.
type Frame struct {
	EventID    string          `json:"eventId"`
	Topic      string          `json:"topic"`
	Sequence   string          `json:"sequence"`
	Revision   string          `json:"revision"`
	OccurredAt string          `json:"occurredAt"`
	Watermark  json.RawMessage `json:"watermark"`
	Payload    json.RawMessage `json:"payload"`
	Coalesced  bool            `json:"coalesced"`
	First      *string         `json:"sourceSequenceFirst"`
	Last       *string         `json:"sourceSequenceLast"`
}

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func number(s string, bits int, positive bool) bool {
	if !decimal.MatchString(s) {
		return false
	}
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.BitLen() <= bits && (!positive || n.Sign() > 0)
}
func compare(a, b string) int {
	x, _ := new(big.Int).SetString(a, 10)
	y, _ := new(big.Int).SetString(b, 10)
	return x.Cmp(y)
}
func ParseFrame(version string, raw []byte) (Frame, error) {
	var f Frame
	if version != "1" || len(raw) > 256*1024 {
		return f, ErrFrame
	}
	if err := validateFrozenFrame(raw); err != nil {
		return f, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &f) != nil {
		return f, ErrFrame
	}
	for _, k := range []string{"eventId", "topic", "sequence", "revision", "occurredAt", "watermark", "payload", "coalesced", "sourceSequenceFirst", "sourceSequenceLast"} {
		v, ok := fields[k]
		if !ok || (string(v) == "null" && k != "watermark" && k != "sourceSequenceFirst" && k != "sourceSequenceLast") {
			return f, ErrFrame
		}
	}
	if f.EventID == "" || len(f.EventID) > 160 || f.Topic == "" || len(f.Topic) > 200 || !number(f.Sequence, 63, false) || !number(f.Revision, 256, true) {
		return f, ErrFrame
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", f.OccurredAt); err != nil {
		return f, ErrFrame
	}
	if f.Coalesced {
		if f.First == nil || f.Last == nil || !number(*f.First, 63, false) || !number(*f.Last, 63, false) || compare(*f.First, *f.Last) > 0 || compare(*f.Last, f.Sequence) > 0 {
			return f, ErrFrame
		}
	} else if f.First != nil || f.Last != nil {
		return f, ErrFrame
	}
	return f, nil
}

// RevisionTracker is bounded to explicit subscribed topic state. A state key
// identifies a candle/statistic; immutable fills use their event identity.
type RevisionTracker struct {
	topics    map[string]string
	revisions map[string]string
	limit     int
}

func NewRevisionTracker(limit int) *RevisionTracker {
	return &RevisionTracker{map[string]string{}, map[string]string{}, limit}
}
func (t *RevisionTracker) Apply(f Frame, stateKey string) error {
	if !number(f.Sequence, 63, false) || !number(f.Revision, 256, true) {
		return ErrFrame
	}
	key := f.Topic + "\x00" + stateKey
	if r, ok := t.revisions[key]; ok && compare(f.Revision, r) <= 0 {
		return ErrStale
	}
	if s, ok := t.topics[f.Topic]; ok && compare(f.Sequence, s) <= 0 {
		return ErrSequence
	}
	if _, ok := t.revisions[key]; !ok && (t.limit <= 0 || len(t.revisions) >= t.limit) {
		return errors.New("tracker capacity: resnapshot required")
	}
	t.topics[f.Topic] = f.Sequence
	t.revisions[key] = f.Revision
	return nil
}

// ConsumeDurable acknowledges only after the committed envelope was accepted by
// the gateway processing callback. Retry/ack failures surface to supervision;
// there is no fallback transport that could advertise false continuity.
func ConsumeDurable(ctx context.Context, consumer DurableConsumer, version string, accept func(context.Context, Frame, string, uint64) error) error {
	if consumer == nil || accept == nil {
		return errors.New("durable consumer and processor required")
	}
	defer consumer.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		delivery, err := consumer.Next(ctx)
		if err != nil {
			return err
		}
		if delivery == nil {
			return errors.New("nil durable delivery")
		}
		frame, err := ParseFrame(version, delivery.Payload())
		if err == nil && (delivery.Partition() == "" || delivery.StreamSequence() == 0) {
			err = ErrFrame
		}
		if err == nil {
			err = accept(ctx, frame, delivery.Partition(), delivery.StreamSequence())
		}
		if err != nil {
			if retryErr := delivery.Retry(ctx); retryErr != nil {
				return errors.Join(err, retryErr)
			}
			return err
		}
		if err := delivery.Ack(ctx); err != nil {
			return err
		}
	}
}
