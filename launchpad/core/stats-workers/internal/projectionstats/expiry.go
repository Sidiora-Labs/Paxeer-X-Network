package projectionstats

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"time"
)

type ExpiryScheduler struct {
	Service  *ReadService
	Interval time.Duration
	Windows  []int64
	Clock    func() time.Time
}

func (e ExpiryScheduler) Refresh(ctx context.Context, observedAt int64) error {
	if e.Service == nil || observedAt < 0 || len(e.Windows) == 0 || len(e.Windows) > 16 {
		return ErrInvalid
	}
	for _, seconds := range e.Windows {
		if seconds < 1 || seconds > 86400 {
			return ErrInvalid
		}
	}
	if _, err := e.Service.PruneExpiredSnapshots(ctx, 1000); err != nil {
		return err
	}
	var failures error
	refresh := func(pool string) {
		for _, seconds := range e.Windows {
			if _, err := e.Service.Window(ctx, pool, seconds, observedAt); err != nil {
				failures = errors.Join(failures, err)
			}
		}
	}
	refresh("")
	after := ""
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rows, err := e.Service.repository.pool.Query(ctx, "SELECT pool FROM "+e.Service.source("indexer_canonical_creations_v1")+" WHERE pool>$1 ORDER BY pool LIMIT 100", after)
		if err != nil {
			return err
		}
		pools := make([]string, 0, 100)
		for rows.Next() {
			var pool string
			if err = rows.Scan(&pool); err != nil {
				rows.Close()
				return err
			}
			pools = append(pools, pool)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, pool := range pools {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			refresh(pool)
			after = pool
		}
		if len(pools) < 100 {
			return failures
		}
	}
}
func (e ExpiryScheduler) Run(ctx context.Context) error {
	if e.Interval < time.Second || e.Interval > time.Minute {
		return ErrInvalid
	}
	now := func() int64 {
		if e.Clock != nil {
			return e.Clock().Unix()
		}
		return time.Now().Unix()
	}
	if err := e.Refresh(ctx, now()); err != nil {
		return err
	}
	ticker := time.NewTicker(e.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.Refresh(ctx, now()); err != nil {
				return err
			}
		}
	}
}

type DurablePublisher struct {
	Repository     *Repository
	Publisher      *natsdriver.Publisher
	Subject        string
	Batch          int
	AttemptTimeout time.Duration
}

func (p DurablePublisher) Run(ctx context.Context) error {
	if p.Repository == nil || p.Publisher == nil || p.Subject == "" || p.Batch < 1 || p.Batch > 100 || p.AttemptTimeout < time.Second || p.AttemptTimeout > 30*time.Second {
		return ErrInvalid
	}
	backoff := 100 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		bounded, cancel := context.WithTimeout(ctx, p.AttemptTimeout)
		count, err := p.Repository.PublishDurable(bounded, p.Publisher, p.Subject, p.Batch)
		cancel()
		delay := 10 * time.Millisecond
		if err != nil {
			delay = backoff
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		} else {
			backoff = 100 * time.Millisecond
			if count == 0 {
				delay = 100 * time.Millisecond
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
