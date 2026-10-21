package publisher

import (
	"context"
	"errors"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type ShadowStager struct {
	durable *DurableStager
	store   *delivery.Store
}

func NewShadowStager(durable *DurableStager, store *delivery.Store) (*ShadowStager, error) {
	if durable == nil || store == nil {
		return nil, errors.New("publisher: shadow providers required")
	}
	return &ShadowStager{durable, store}, nil
}
func (s *ShadowStager) ObserveCanonicalTx(ctx context.Context, tx pgx.Tx, rangeDigest, path string, event RawCanonicalEvent) error {
	subject, envelope, err := s.durable.Envelope(event)
	if err != nil {
		return err
	}
	return s.store.RecordShadowTx(ctx, tx, rangeDigest, path, subject, envelope)
}
func (s *ShadowStager) ObservePublicationTx(ctx context.Context, tx pgx.Tx, rangeDigest, path, subject string, envelope delivery.Envelope) error {
	return s.store.RecordShadowTx(ctx, tx, rangeDigest, path, subject, envelope)
}
