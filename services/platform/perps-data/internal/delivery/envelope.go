// Package delivery supplies the transactional outbox, inbox and relay of the
// perps data services. It originates in Launch-x shared/delivery and drives
// Postgres through database/sql and lib/pq instead of pgx.
package delivery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxEnvelopeBytes = 1 << 20

var (
	ErrIntegrity     = errors.New("delivery: immutable identity conflict")
	ErrSchema        = errors.New("delivery: unsupported envelope version")
	ErrLease         = errors.New("delivery: stale or expired lease")
	ErrAuthorization = errors.New("delivery: unapproved subject")
	token            = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	subjectToken     = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)
	decimal          = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

type SourceOrder struct {
	Height           string `json:"height"`
	TransactionIndex string `json:"transaction_index"`
	LogIndex         string `json:"log_index"`
}

type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion uint32          `json:"schema_version"`
	ChainID       uint64          `json:"chain_id"`
	SourceOrder   SourceOrder     `json:"source_order"`
	EmittedAt     time.Time       `json:"emitted_at"`
	TraceID       string          `json:"trace_id"`
	PayloadHash   string          `json:"payload_hash"`
	Payload       json.RawMessage `json:"payload"`
}

// Policy is server-owned and must never be constructed from a public request.
// Exact subjects prevent a producer from choosing arbitrary broker destinations.
// Patterns admit the subjects of a server-owned shape whose entity token is
// data, such as an account; '*' matches exactly one token.
type Policy struct {
	Subjects   map[string]bool
	Patterns   []string
	EventTypes map[string]bool
}

func (p Policy) admits(subject string) bool {
	if p.Subjects[subject] {
		return true
	}
	tokens := strings.Split(subject, ".")
	for _, pattern := range p.Patterns {
		f := strings.Split(pattern, ".")
		if len(f) != len(tokens) {
			continue
		}
		match := true
		for i, t := range f {
			if t != "*" && t != tokens[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (e Envelope) Validate(p Policy, subject string) error {
	if len(subject) > 256 || !subjectToken.MatchString(subject) || !p.admits(subject) {
		return ErrAuthorization
	}
	if !token.MatchString(e.EventID) || !token.MatchString(e.TraceID) || !p.EventTypes[e.EventType] {
		return errors.New("delivery: invalid event identity or type")
	}
	if e.SchemaVersion != 1 {
		return ErrSchema
	}
	// The chain is the second token of every server-owned subject
	// (perps.<chain>.…), so the envelope carries the configured chain.
	if tokens := strings.SplitN(subject, ".", 3); e.ChainID == 0 || len(tokens) < 3 || tokens[1] != strconv.FormatUint(e.ChainID, 10) {
		return errors.New("delivery: wrong chain")
	}
	for _, v := range []string{e.SourceOrder.Height, e.SourceOrder.TransactionIndex, e.SourceOrder.LogIndex} {
		if _, err := strconv.ParseUint(v, 10, 63); err != nil || !decimal.MatchString(v) {
			return errors.New("delivery: invalid source order")
		}
	}
	if e.EmittedAt.IsZero() || e.EmittedAt.Location() != time.UTC {
		return errors.New("delivery: UTC emission time required")
	}
	if len(e.Payload) == 0 || len(e.Payload) > MaxEnvelopeBytes || !bytes.Equal(bytes.TrimSpace(e.Payload), e.Payload) || !json.Valid(e.Payload) {
		return errors.New("delivery: invalid or oversized payload")
	}
	if e.PayloadHash != Hash(e.Payload) {
		return ErrIntegrity
	}
	// Preserve raw payload bytes. Never deserialize amounts through float64.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &payload); err != nil || payload == nil {
		return errors.New("delivery: payload object required")
	}
	b, err := e.encodeBytes()
	if err != nil || len(b) > MaxEnvelopeBytes {
		return errors.New("delivery: oversized envelope")
	}
	return nil
}

// Encode produces an owned byte copy; the repository stores these exact bytes.
func (e Envelope) Encode(p Policy, subject string) ([]byte, error) {
	if err := e.Validate(p, subject); err != nil {
		return nil, err
	}
	return e.encodeBytes()
}

// encoding/json compacts RawMessage, which would invalidate a hash made over
// original payload bytes. Append the last field verbatim after encoding metadata.
func (e Envelope) encodeBytes() ([]byte, error) {
	payload := append([]byte(nil), e.Payload...)
	e.Payload = nil
	metadata, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	suffix := []byte(`"payload":null}`)
	if !bytes.HasSuffix(metadata, suffix) {
		return nil, errors.New("delivery: invalid encoding layout")
	}
	wire := append(metadata[:len(metadata)-len(suffix)], []byte(`"payload":`)...)
	wire = append(wire, payload...)
	return append(wire, '}'), nil
}

func Decode(b []byte, p Policy, subject string) (Envelope, error) {
	var e Envelope
	if len(subject) > 256 || !subjectToken.MatchString(subject) || !p.admits(subject) {
		return e, ErrAuthorization
	}
	if len(b) > MaxEnvelopeBytes {
		return e, errors.New("delivery: oversized envelope")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, fmt.Errorf("delivery: invalid envelope: %w", err)
	}
	if !json.Valid(b) {
		return e, errors.New("delivery: invalid envelope")
	}
	return e, e.Validate(p, subject)
}
