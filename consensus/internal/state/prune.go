package state

import (
	"sync"
	"time"
)

// pruneWorker runs block and state pruning off the ApplyBlock path. At most
// one prune runs at a time; requests that arrive while it runs coalesce to the
// highest retain height.
type pruneWorker struct {
	prune   func(retainHeight int64) (uint64, error)
	latency func(time.Duration)

	mtx     sync.Mutex
	pending int64
	stopped bool
	// idle is closed when the running prune goroutine exits; nil while no
	// goroutine runs.
	idle chan struct{}
}

func newPruneWorker(prune func(int64) (uint64, error), latency func(time.Duration)) *pruneWorker {
	return &pruneWorker{prune: prune, latency: latency}
}

// requestPrune schedules a prune below retainHeight. It never blocks on the
// prune itself and is a no-op for a non-positive height or after stop.
func (w *pruneWorker) requestPrune(retainHeight int64) {
	if retainHeight <= 0 {
		return
	}
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.stopped {
		return
	}
	if retainHeight > w.pending {
		w.pending = retainHeight
	}
	if w.idle != nil {
		return
	}
	w.idle = make(chan struct{})
	go w.run(w.idle)
}

func (w *pruneWorker) run(idle chan struct{}) {
	defer close(idle)
	for {
		w.mtx.Lock()
		retainHeight := w.pending
		if retainHeight == 0 || w.stopped {
			w.idle = nil
			w.mtx.Unlock()
			return
		}
		w.pending = 0
		w.mtx.Unlock()

		start := time.Now()
		pruned, err := w.prune(retainHeight)
		w.latency(time.Since(start))
		if err != nil {
			logger.Error("failed to prune blocks", "retain_height", retainHeight, "err", err)
		} else {
			logger.Debug("pruned blocks", "pruned", pruned, "retain_height", retainHeight)
		}
	}
}

// stop refuses further requests, drops any request not yet started and waits
// for an in-flight prune to finish.
func (w *pruneWorker) stop() {
	w.mtx.Lock()
	w.stopped = true
	w.pending = 0
	idle := w.idle
	w.mtx.Unlock()
	if idle != nil {
		<-idle
	}
}
