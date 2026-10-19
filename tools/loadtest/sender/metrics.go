package sender

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("paxload/sender")

	meteredChainWorkers = &chainWorkerObserver{
		workers: make(map[chainWorkerID]*Worker),
	}

	metrics = struct {
		sendLatency       metric.Float64Histogram
		receiptLatency    metric.Float64Histogram
		workerQueueLength metric.Int64ObservableGauge
	}{
		sendLatency: must(meter.Float64Histogram(
			"send_latency",
			metric.WithDescription("Latency of sending transactions in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.5, 1.0, 2.0, 3.0, 5.0, 10.0, 20.0))),
		receiptLatency: must(meter.Float64Histogram(
			"receipt_latency",
			metric.WithDescription("Latency of sending transactions in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.5, 1.0, 2.0, 3.0, 5.0, 10.0, 20.0))),
		workerQueueLength: must(meter.Int64ObservableGauge(
			"worker_queue_length",
			metric.WithDescription("Length of the worker's queue"),
			metric.WithUnit("{count}"),
			metric.WithInt64Callback(func(ctx context.Context, observer metric.Int64Observer) error {
				meteredChainWorkers.lock.RLock()
				defer meteredChainWorkers.lock.RUnlock()
				for _, worker := range meteredChainWorkers.workers {
					observer.Observe(int64(worker.GetChannelLength()), metric.WithAttributes(
						attribute.String("endpoint", worker.GetEndpoint()),
						attribute.Int("worker_id", worker.id),
						attribute.String("chain_id", worker.paxChainID),
					))
				}
				return nil
			}))),
	}
)

type chainWorkerObserver struct {
	lock    sync.RWMutex
	workers map[chainWorkerID]*Worker
}
type chainWorkerID struct {
	workerID int
	chainID  string
}

func meterWorkerQueueLength(worker *Worker) {
	meteredChainWorkers.lock.Lock()
	defer meteredChainWorkers.lock.Unlock()
	id := chainWorkerID{
		workerID: worker.id,
		chainID:  worker.paxChainID,
	}
	if _, exists := meteredChainWorkers.workers[id]; !exists {
		meteredChainWorkers.workers[id] = worker
	}
}

func statusAttrFromError(err error) attribute.KeyValue {
	const key = "status"
	if err == nil {
		return attribute.String(key, "success")
	}
	return attribute.String(key, "failure")
}

// must panics if err is non-nil, otherwise returns v.
func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
