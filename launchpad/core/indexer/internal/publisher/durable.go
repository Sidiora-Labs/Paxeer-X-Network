package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

// DurableStager is additive to the legacy Publisher. The indexer must call Stage
// inside the same PostgreSQL transaction as canonical rows and cursor updates.
// PublishBatch is never used as the durable handoff.
type DurableStager struct {
	store            *delivery.Store
	deployment       string
	subjects         map[string]string
	bridge           *delivery.WebhookBridge
	bridgeGeneration string
}

var canonicalHash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var deploymentToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func NewDurableStager(store *delivery.Store, deployment string, subjects map[string]string) (*DurableStager, error) {
	if store == nil || !deploymentToken.MatchString(deployment) || len(subjects) == 0 {
		return nil, errors.New("publisher: invalid durable stager configuration")
	}
	copySubjects := map[string]string{}
	for event, subject := range subjects {
		if event == "" || subject == "" {
			return nil, errors.New("publisher: empty event subject mapping")
		}
		copySubjects[event] = subject
	}
	return &DurableStager{store: store, deployment: deployment, subjects: copySubjects}, nil
}

// RawCanonicalEvent carries exact JSON from the schema-aware decoder. Never
// convert raw uint256 values through float64 or infer transaction order from time.
type RawCanonicalEvent struct {
	EventType, TxHash, TraceID string
	SourceOrder                delivery.SourceOrder
	EmittedAt                  time.Time
	Payload                    json.RawMessage
}

func (s *DurableStager) Envelope(event RawCanonicalEvent) (string, delivery.Envelope, error) {
	subject, ok := s.subjects[event.EventType]
	if !ok || !canonicalHash.MatchString(event.TxHash) {
		return "", delivery.Envelope{}, errors.New("publisher: unknown event type or noncanonical transaction hash")
	}
	if _, err := strconv.ParseUint(event.SourceOrder.LogIndex, 10, 63); err != nil {
		return "", delivery.Envelope{}, errors.New("publisher: invalid log index")
	}
	// Deployment identity is server configured, never wallet or request selected.
	id := delivery.Hash([]byte("125/" + s.deployment + "/" + event.TxHash + "/" + event.SourceOrder.LogIndex))
	payload := append(json.RawMessage(nil), event.Payload...)
	return subject, delivery.Envelope{EventID: id, EventType: event.EventType, SchemaVersion: 1, ChainID: 125, SourceOrder: event.SourceOrder, EmittedAt: event.EmittedAt, TraceID: event.TraceID, PayloadHash: delivery.Hash(payload), Payload: payload}, nil
}

func (s *DurableStager) Stage(ctx context.Context, tx pgx.Tx, event RawCanonicalEvent) error {
	subject, envelope, err := s.Envelope(event)
	if err != nil {
		return err
	}
	return s.store.Enqueue(ctx, tx, subject, envelope)
}

func (s *DurableStager) StagePublication(ctx context.Context, tx pgx.Tx, subject string, publication accounting.PublicationV1) error {
	if s == nil || s.store == nil || tx == nil {
		return errors.New("publisher: durable store and transaction required")
	}
	expected, err := accounting.SubjectForFacet(publication.Facet)
	if err != nil || subject != expected || s.subjects[subject] != subject {
		return errors.New("publisher: unapproved accounting subject")
	}
	wire, err := accounting.CanonicalPublicationV1(publication)
	if err != nil {
		return err
	}
	fill := publication.Fill
	if fill == nil {
		var raw []byte
		var storedDigest string
		if err = tx.QueryRow(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills WHERE source_fill_id=$1`, publication.SourceFillID).Scan(&raw, &storedDigest); err != nil {
			return err
		}
		original, decodeErr := accounting.DecodeFillV1(raw)
		if decodeErr != nil {
			return decodeErr
		}
		digest, digestErr := accounting.FillDigest(original)
		if digestErr != nil || digest != publication.SourceFactDigest || storedDigest != string(digest) || original.SourceFillID != publication.SourceFillID || original.Position != publication.Positions[0].SourceOrder {
			return errors.New("publisher: enrichment original source binding mismatch")
		}
		fill = &original
	}
	if fill.Provenance.DeploymentID != s.deployment {
		return errors.New("publisher: accounting deployment mismatch")
	}
	seconds, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil || seconds <= 0 || seconds > math.MaxInt64/1000 {
		return errors.New("publisher: invalid authoritative block timestamp")
	}
	position := fill.Position
	envelope := delivery.Envelope{
		EventID:       publication.PublicationID,
		EventType:     subject,
		SchemaVersion: 1,
		ChainID:       125,
		SourceOrder:   delivery.SourceOrder{Height: position.Height, TransactionIndex: position.TransactionIndex, LogIndex: position.LogIndex},
		EmittedAt:     time.Unix(seconds, 0).UTC(),
		TraceID:       publication.PublicationID,
		PayloadHash:   delivery.Hash(wire),
		Payload:       json.RawMessage(wire),
	}
	if err = s.store.Enqueue(ctx, tx, subject, envelope); err != nil {
		return err
	}
	if s.bridge != nil {
		for _, position := range publication.Positions {
			sequence, err := strconv.ParseInt(position.Sequence, 10, 64)
			if err != nil {
				return err
			}
			if err = s.bridge.StageTx(ctx, tx, s.bridgeGeneration, position.PartitionKind+":"+position.PartitionKey, sequence, position.EffectIdentity, position.PreviousEffectIdentity, subject, envelope); err != nil {
				return err
			}
		}
	}
	return nil
}

func NewDurableStagerWithWebhook(store *delivery.Store, deployment string, subjects map[string]string, bridge *delivery.WebhookBridge, generation string) (*DurableStager, error) {
	if bridge == nil || !deploymentToken.MatchString(generation) {
		return nil, errors.New("publisher: durable webhook configuration required")
	}
	s, err := NewDurableStager(store, deployment, subjects)
	if err != nil {
		return nil, err
	}
	s.bridge = bridge
	s.bridgeGeneration = generation
	return s, nil
}

const CanonicalCreationSubject = "canonical.creation.v1"
const CanonicalCreationEventType = "canonical_creation"

func (s *DurableStager) StageCanonicalCreationTx(ctx context.Context, tx pgx.Tx, fact store.CreationFact) error {
	if s == nil || s.store == nil || ctx == nil || tx == nil || s.subjects[CanonicalCreationEventType] != CanonicalCreationSubject || fact.Provenance.DeploymentID != s.deployment || accounting.ValidateSourcePosition(fact.Position) != nil {
		return errors.New("publisher: approved canonical creation source and transaction required")
	}
	seconds, err := strconv.ParseInt(fact.BlockTimestampSeconds, 10, 64)
	if err != nil || seconds <= 0 || seconds > math.MaxInt64/1000 {
		return errors.New("publisher: invalid authoritative creation timestamp")
	}
	wire, err := json.Marshal(fact)
	if err != nil || len(wire) > delivery.MaxEnvelopeBytes {
		return errors.New("publisher: bounded canonical creation fact required")
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT c.payload=$2::jsonb AND c.pool=$3 AND c.creator=$4 AND c.factory=$5 AND c.height=$6::bigint AND c.block_hash=$7 AND c.transaction_hash=$8 AND c.transaction_index=$9::bigint AND c.log_index=$10::bigint AND c.block_timestamp=$11 AND r.height=c.height AND r.block_hash=c.block_hash AND r.transaction_index=c.transaction_index AND r.emitter=c.factory FROM indexer.indexer_canonical_creations c JOIN indexer.indexer_raw_logs r ON r.chain_id=125 AND r.transaction_hash=c.transaction_hash AND r.log_index=c.log_index WHERE c.token=$1`, fact.Token, wire, fact.Pool, fact.Creator, fact.Factory, fact.Position.Height, fact.Position.BlockHash, fact.Position.TransactionHash, fact.Position.TransactionIndex, fact.Position.LogIndex, seconds).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return delivery.ErrIntegrity
	}
	id := accounting.HashTuple("canonical-creation-v1", "125", s.deployment, fact.Position.BlockHash, fact.Position.TransactionHash, fact.Position.LogIndex)
	envelope := delivery.Envelope{EventID: id, EventType: CanonicalCreationEventType, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: fact.Position.Height, TransactionIndex: fact.Position.TransactionIndex, LogIndex: fact.Position.LogIndex}, EmittedAt: time.Unix(seconds, 0).UTC(), TraceID: id, PayloadHash: delivery.Hash(wire), Payload: json.RawMessage(wire)}
	return s.store.Enqueue(ctx, tx, CanonicalCreationSubject, envelope)
}
