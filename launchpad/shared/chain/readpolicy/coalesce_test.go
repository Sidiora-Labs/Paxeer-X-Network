package readpolicy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func awaitWaiters(t *testing.T, c *Coalescer, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		g := c.groups[key]
		n := 0
		if g != nil {
			n = g.waiters
		}
		c.mu.Unlock()
		if n == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("%d waiters did not join one read group", want)
}
func TestThousandCoalescedReadsAndCopiedResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actual-input")
	if err := os.WriteFile(path, []byte("committed-public-result"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewCoalescer()
	release := make(chan struct{})
	var count atomic.Int64
	read := func(ctx context.Context) ([][]byte, error) {
		count.Add(1)
		value, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return [][]byte{value}, nil
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	results := make(chan [][]byte, 1000)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := c.Do(context.Background(), "same-pinned-public-key", read)
			errs <- err
			results <- value
		}()
	}
	awaitWaiters(t, c, "same-pinned-public-key", 1000)
	close(release)
	wg.Wait()
	close(errs)
	close(results)
	if count.Load() != 1 {
		t.Fatalf("upstream groups=%d, want1", count.Load())
	}
	stats := c.Stats()
	if stats.UpstreamGroups != 1 || stats.JoinedWaiters != 999 || stats.CapacityRejections != 0 {
		t.Fatal("coalescing counters lost actual group identities", stats)
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for value := range results {
		if len(value) != 1 || string(value[0]) != "committed-public-result" {
			t.Fatal("incorrect fanout")
		}
		value[0][0] = 'X'
	}
	value, err := c.Do(context.Background(), "same-pinned-public-key", read)
	if err != nil || string(value[0]) != "committed-public-result" {
		t.Fatal("caller mutated cached result", err)
	}
}
func TestLeaderCancellationPreservesSurvivingWaiter(t *testing.T) {
	c := NewCoalescer()
	release := make(chan struct{})
	started := make(chan struct{})
	leader, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := func(ctx context.Context) ([][]byte, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return [][]byte{[]byte("result")}, nil
		}
	}
	leaderResult := make(chan error, 1)
	go func() { _, err := c.Do(leader, "leader-key", read); leaderResult <- err }()
	<-started
	followerResult := make(chan error, 1)
	go func() {
		result, err := c.Do(context.Background(), "leader-key", read)
		if err == nil && (len(result) != 1 || string(result[0]) != "result") {
			err = errors.New("incorrect surviving result")
		}
		followerResult <- err
	}()
	awaitWaiters(t, c, "leader-key", 2)
	cancel()
	if err := <-leaderResult; !errors.Is(err, context.Canceled) {
		t.Fatal("leader cancellation lost", err)
	}
	close(release)
	if err := <-followerResult; err != nil {
		t.Fatal(err)
	}
}
func TestDistinctPublicIdentityDoesNotCoalesce(t *testing.T) {
	c := NewCoalescer()
	var count atomic.Int64
	read := func(context.Context) ([][]byte, error) { count.Add(1); return [][]byte{[]byte("bounded")}, nil }
	for _, key := range []string{"deploymentA/blockA/argsA", "deploymentA/blockA/argsB", "deploymentB/blockA/argsA", "deploymentA/blockB/argsA"} {
		if _, err := c.Do(context.Background(), key, read); err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 4 {
		t.Fatal("distinct identities shared a group")
	}
}

func TestCanceledWorkersRetainCapacityUntilActualIOExits(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	c := NewCoalescer()
	var workers sync.WaitGroup
	workers.Add(128)
	read := func(context.Context) ([][]byte, error) {
		defer workers.Done()
		var byteValue [1]byte
		_, err := reader.Read(byteValue[:])
		return nil, err
	}
	for i := 0; i < 128; i++ {
		key := strconv.Itoa(i)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := c.Do(ctx, key, read); result <- err }()
		awaitWaiters(t, c, key, 1)
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("canceled read waiter lost identity", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled waiter stranded by actual IO")
		}
	}
	if _, err := c.Do(context.Background(), "overflow", read); !errors.Is(err, ErrReadCapacity) {
		t.Fatal("cancellation bypassed actual worker limit", err)
	}
	if _, err := c.Do(context.Background(), "0", read); !errors.Is(err, ErrReadCapacity) {
		t.Fatal("canceled live worker spawned a replacement", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		remaining := len(c.groups)
		c.mu.Unlock()
		if remaining == 0 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("completed canceled IO retained group resources")
}
