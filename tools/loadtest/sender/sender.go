package sender

import (
	"context"
	"github.com/paxeer-network/pax-load/types"
)

type TxSender interface {
	Send(ctx context.Context, tx *types.LoadTx) error
}
