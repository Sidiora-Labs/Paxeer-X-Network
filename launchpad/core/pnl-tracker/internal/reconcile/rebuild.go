package reconcile

import (
	"context"
	"errors"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l16"
)

type InventoryRebuilder struct{ inventory *l16.Service }

func NewInventoryRebuilder(inventory *l16.Service) (*InventoryRebuilder, error) {
	if inventory == nil {
		return nil, errors.New("inventory rebuild service required")
	}
	return &InventoryRebuilder{inventory: inventory}, nil
}
func (r *InventoryRebuilder) RebuildPartition(ctx context.Context, wallet, token string, committedHeight int64) (l16.RebuildSnapshot, error) {
	return r.inventory.Rebuild(ctx, wallet, token, committedHeight)
}
