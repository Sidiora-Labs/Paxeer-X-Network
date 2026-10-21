package readpolicy

import (
	"context"
	"testing"
	"time"
)

func TestHeadWorkerRejectsAbsentProvider(t *testing.T) {
	var worker HeadWorker
	if HeadPollBaseline != 100*time.Millisecond {
		t.Fatal("head baseline changed")
	}
	if err := worker.Run(context.Background(), nil, func(context.Context, HeadObservation) error { return nil }); err == nil {
		t.Fatal("absent head provider accepted")
	}
}
