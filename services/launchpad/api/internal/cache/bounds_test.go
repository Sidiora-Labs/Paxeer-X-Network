package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryBoundsAndCopies(t *testing.T) {
	c := NewWithConfig(Config{Capacity: 100, MaxBytes: 2048, MaxEntryBytes: 128})
	source := []byte("original")
	b, _, e := c.GetOrFetch(context.Background(), "one", time.Minute, func(context.Context) ([]byte, error) { return source, nil })
	if e != nil {
		t.Fatal(e)
	}
	source[0] = 'X'
	b[0] = 'Y'
	b, _, _ = c.GetOrFetch(context.Background(), "one", time.Minute, nil)
	if string(b) != "original" {
		t.Fatal("mutable alias")
	}
	for i := 0; i < 1000; i++ {
		c.GetOrFetch(context.Background(), fmt.Sprint(i), time.Minute, func(context.Context) ([]byte, error) { return make([]byte, 4096), nil })
		used, _, _, _ := c.MemoryStats()
		if used > 2048 {
			t.Fatal(used)
		}
	}
	if c.Len() != 1 {
		t.Fatal("oversize evicted working set")
	}
}
func TestCancelledInitiatorAndThousandWaiters(t *testing.T) {
	c := New(10)
	start := make(chan struct{})
	release := make(chan struct{})
	var n atomic.Int64
	fetch := func(ctx context.Context) ([]byte, error) {
		n.Add(1)
		close(start)
		select {
		case <-release:
			return []byte("ok"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { _, _, e := c.GetOrFetch(ctx, "key", time.Minute, fetch); done <- e }()
	<-start
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _, e := c.GetOrFetch(context.Background(), "key", time.Minute, fetch)
			if e != nil || string(b) != "ok" {
				t.Error(e)
			}
		}()
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	close(release)
	wg.Wait()
	if n.Load() != 1 {
		t.Fatal(n.Load())
	}
}
func TestOriginBoundAndCompletionTTL(t *testing.T) {
	c := NewWithConfig(Config{MaxFetches: 1})
	start := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.GetOrFetch(context.Background(), "slow", 40*time.Millisecond, func(context.Context) ([]byte, error) { close(start); <-release; return []byte("ok"), nil })
	}()
	<-start
	if _, _, e := c.GetOrFetch(context.Background(), "other", time.Second, nil); !errors.Is(e, ErrOriginBusy) {
		t.Fatal(e)
	}
	time.Sleep(60 * time.Millisecond)
	close(release)
	<-done
	b, _, e := c.GetOrFetch(context.Background(), "slow", time.Second, func(context.Context) ([]byte, error) { t.Error("TTL started before completion"); return nil, nil })
	if e != nil || string(b) != "ok" {
		t.Fatal(e)
	}
}

func TestPerOriginIsolation(t *testing.T) {
	c := NewWithConfig(Config{MaxFetches: 3, MaxPerOrigin: 1})
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.GetOrFetchOrigin(context.Background(), "db", "a", time.Second, func(context.Context) ([]byte, error) { close(started); <-release; return []byte("db"), nil })
	}()
	<-started
	if _, _, e := c.GetOrFetchOrigin(context.Background(), "db", "b", time.Second, nil); !errors.Is(e, ErrOriginBusy) {
		t.Fatal(e)
	}
	if b, _, e := c.GetOrFetchOrigin(context.Background(), "broker", "a", time.Second, func(context.Context) ([]byte, error) { return []byte("broker"), nil }); e != nil || string(b) != "broker" {
		t.Fatal(e)
	}
	close(release)
	<-done
}

func TestOriginSeparatorRejected(t *testing.T) {
	c := New(10)
	fetch := func(context.Context) ([]byte, error) { return []byte("safe"), nil }
	if _, _, e := c.GetOrFetchOrigin(context.Background(), "a\x00b", "c", time.Minute, fetch); e == nil {
		t.Fatal("ambiguous origin accepted")
	}
	b, _, e := c.GetOrFetchOrigin(context.Background(), "a", "b\x00c", time.Minute, fetch)
	if e != nil || string(b) != "safe" {
		t.Fatal(e)
	}
	for _, origin := range []string{"a\nb", "a\tb", "a\x7fb"} {
		if _, _, e := c.GetOrFetchOrigin(context.Background(), origin, "k", time.Minute, fetch); e == nil {
			t.Fatal("control origin accepted")
		}
	}
}

func TestIndependentCancelledWaitersAndActiveCompletion(t *testing.T) {
	c := NewWithConfig(Config{MaxFetches: 1, MaxPerOrigin: 1, FetchTimeout: 2 * time.Second})
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int64
	fetch := func(work context.Context) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return []byte("completed"), nil
		case <-work.Done():
			return nil, work.Err()
		}
	}
	initiator, cancelInitiator := context.WithCancel(context.Background())
	defer cancelInitiator()
	first := make(chan error, 1)
	go func() { _, _, e := c.GetOrFetchOrigin(initiator, "postgres", "shared", time.Minute, fetch); first <- e }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}
	waiter, cancelWaiter := context.WithCancel(context.Background())
	defer cancelWaiter()
	cancelled := make(chan error, 1)
	go func() {
		_, _, e := c.GetOrFetchOrigin(waiter, "postgres", "shared", time.Minute, fetch)
		cancelled <- e
	}()
	active := make(chan error, 1)
	go func() {
		b, _, e := c.GetOrFetchOrigin(context.Background(), "postgres", "shared", time.Minute, fetch)
		if e == nil && string(b) != "completed" {
			e = fmt.Errorf("unexpected active body %q", b)
		}
		active <- e
	}()
	cancelWaiter()
	select {
	case e := <-cancelled:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter cancellation blocked")
	}
	cancelInitiator()
	select {
	case e := <-first:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("initiator cancellation blocked")
	}
	close(release)
	select {
	case e := <-active:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("active waiter lost useful work")
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate origin fetch", calls.Load())
	}
	if _, _, e := c.GetOrFetch(context.Background(), "absent", time.Second, nil); e == nil {
		t.Fatal("nil fetch accepted on miss")
	}
}
