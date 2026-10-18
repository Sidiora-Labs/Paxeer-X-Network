package processor

import (
	"context"
	"errors"
)

type Backfill struct {
	p         *Processor
	fromBlock *int64
	batchSize int
}

func NewBackfill(p *Processor, fromBlock *int64, batchSize int) *Backfill {
	if batchSize <= 0 {
		batchSize = 1000
	}
	return &Backfill{p: p, fromBlock: fromBlock, batchSize: batchSize}
}
func (b *Backfill) Run(ctx context.Context) error {
	if b == nil || b.p == nil || b.p.deps.HistoricalReplay == nil || b.p.deps.HistoricalRequest == nil {
		return errors.New("backfill requires an authorized explicit historical replay request and real durable replay runner")
	}
	request := *b.p.deps.HistoricalRequest
	if request.Mode != "backfill" {
		return errors.New("backfill request mode required")
	}
	if b.fromBlock != nil && request.FromHeight != *b.fromBlock {
		return errors.New("backfill requested range conflicts with configured operation")
	}
	runner := *b.p.deps.HistoricalReplay
	if b.batchSize < 1 || b.batchSize > 1000 {
		return errors.New("backfill batch bound must be at most 1000")
	}
	runner.BatchSize = b.batchSize
	return runner.Run(ctx, request)
}
