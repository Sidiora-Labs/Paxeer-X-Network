package readpolicy

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"
)

const HeadPollBaseline = 100 * time.Millisecond

type HeadReader interface {
	Head(context.Context) (int64, error)
}
type HeadObservation struct {
	Height     int64
	ObservedAt time.Time
	Latency    time.Duration
	Err        error
}
type HeadWorker struct {
	mu      sync.Mutex
	running bool
}

func (w *HeadWorker) Run(ctx context.Context, reader HeadReader, deliver func(context.Context, HeadObservation) error) error {
	if reader == nil || deliver == nil {
		return errors.New("head reader and durable delivery required")
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return errors.New("head worker already running")
	}
	w.running = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.running = false; w.mu.Unlock() }()
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		attempt, cancel := context.WithTimeout(ctx, DefaultDeadline)
		height, err := reader.Head(attempt)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && height < 0 {
			err = errors.New("invalid committed height")
		}
		failed := err != nil
		if deliveryError := deliver(ctx, HeadObservation{height, time.Now().UTC(), time.Since(started), err}); deliveryError != nil {
			return deliveryError
		}
		if failed {
			failures++
		} else {
			failures = 0
		}
		interval := HeadPollBaseline
		if failures > 0 {
			interval *= time.Duration(1 << min(failures, 4))
		}
		interval += time.Duration(rand.Int64N(int64(20*time.Millisecond)+1)) - 10*time.Millisecond
		timer := time.NewTimer(max(time.Duration(0), interval-time.Since(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
