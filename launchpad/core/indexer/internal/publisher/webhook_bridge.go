package publisher

import (
	"context"
	"errors"
	"strconv"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type DurableWebhookStager struct {
	durable    *DurableStager
	bridge     *delivery.WebhookBridge
	generation string
}

func NewDurableWebhookStager(durable *DurableStager, bridge *delivery.WebhookBridge, generation string) (*DurableWebhookStager, error) {
	if durable == nil || bridge == nil || !deploymentToken.MatchString(generation) {
		return nil, errors.New("publisher: durable webhook providers required")
	}
	return &DurableWebhookStager{durable, bridge, generation}, nil
}
func (s *DurableWebhookStager) StagePublication(ctx context.Context, tx pgx.Tx, subject string, p accounting.PublicationV1) error {
	if err := s.durable.StagePublication(ctx, tx, subject, p); err != nil {
		return err
	}
	originalSubject, e, err := s.durable.store.OriginalEnvelopeTx(ctx, tx, p.PublicationID)
	if err != nil {
		return err
	}
	if originalSubject != subject {
		return delivery.ErrIntegrity
	}
	for _, position := range p.Positions {
		sequence, err := strconv.ParseInt(position.Sequence, 10, 64)
		if err != nil {
			return err
		}
		if err = s.bridge.StageTx(ctx, tx, s.generation, position.PartitionKind+":"+position.PartitionKey, sequence, position.EffectIdentity, position.PreviousEffectIdentity, subject, e); err != nil {
			return err
		}
	}
	return nil
}
