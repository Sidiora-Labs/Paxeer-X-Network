package position

import (
	"context"
	"errors"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l16"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
)

type CanonicalConsumer struct{ inventory *l16.AccountingConsumer }

func NewCanonicalConsumer(inventory *l16.AccountingConsumer) (*CanonicalConsumer, error) {
	if inventory == nil {
		return nil, errors.New("canonical inventory consumer required")
	}
	return &CanonicalConsumer{inventory: inventory}, nil
}
func (c *CanonicalConsumer) ProcessDelivery(ctx context.Context, message delivery.Delivery) error {
	actual, ok := message.(*natsdriver.Message)
	if !ok || actual == nil {
		return errors.New("actual inventory broker message required")
	}
	return c.inventory.PrepareAndConsume(ctx, actual.Subject, actual.Wire, actual)
}
