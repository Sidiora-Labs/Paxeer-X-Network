package fanout

import (
	"sync"
	"testing"
)

func TestSlowQueueEvictionIsImmediateAndBounded(t *testing.T) {
	hub := &Hub{sendBuffer: 256}
	c := newConn(hub, nil, "fixture")
	frame := make([]byte, 16384)
	for i := 0; i < 64; i++ {
		c.enqueue(frame)
	}
	if len(c.send) != 64 || c.queuedBytes != 1024*1024 {
		t.Fatal(len(c.send), c.queuedBytes)
	}
	c.enqueue(frame)
	select {
	case <-c.closed:
	default:
		t.Fatal("slow client not closed")
	}
	if hub.metrics.evicted.Load() != 1 || len(c.send) != 64 {
		t.Fatal("eviction not bounded")
	}
	c.enqueue(frame)
	if hub.metrics.evicted.Load() != 1 {
		t.Fatal("duplicate eviction")
	}
	healthy := newConn(hub, nil, "healthy")
	healthy.enqueue([]byte("hello"))
	if string(<-healthy.send) != "hello" {
		t.Fatal("healthy peer blocked")
	}
}

func TestAdversarialConcurrentQueueEvictionAndFrameLimit(t *testing.T) {
	hub := &Hub{sendBuffer: 256}
	slow := newConn(hub, nil, "slow")
	for i := 0; i < 256; i++ {
		slow.enqueue([]byte("durable-frame"))
	}
	if len(slow.send) != 256 || slow.queuedBytes != 256*len("durable-frame") {
		t.Fatal("frame limit not retained", len(slow.send), slow.queuedBytes)
	}
	var publishers sync.WaitGroup
	for i := 0; i < 32; i++ {
		publishers.Add(1)
		go func() { defer publishers.Done(); slow.enqueue([]byte("overflow")) }()
	}
	publishers.Wait()
	if hub.metrics.evicted.Load() != 1 || len(slow.send) != 256 {
		t.Fatal("eviction must occur once without extra queued frames", hub.metrics.evicted.Load(), len(slow.send))
	}
	for i := 0; i < 1000; i++ {
		healthy := newConn(hub, nil, "healthy")
		healthy.enqueue([]byte("durable-frame"))
		select {
		case payload := <-healthy.send:
			if string(payload) != "durable-frame" {
				t.Fatal("changed healthy frame")
			}
		default:
			t.Fatal("healthy recipient delayed by stalled queue")
		}
	}
}

func TestMeasuredFrameBudgetIncludesInFlightFrame(t *testing.T) {
	hub := &Hub{sendBuffer: 256}
	c := newConn(hub, nil, "actual-queue")
	c.enqueue([]byte("in-flight"))
	<-c.send
	for i := 1; i < 256; i++ {
		c.enqueue([]byte("queued"))
	}
	if c.queuedFrames != 256 || len(c.send) != 255 {
		t.Fatal("in-flight frame escaped total frame accounting", c.queuedFrames, len(c.send))
	}
	c.enqueue([]byte("over-budget"))
	select {
	case <-c.closed:
	default:
		t.Fatal("in-flight plus256 queued frames exceeded bound")
	}
	if hub.metrics.queueFramesPeak.Load() != 256 || hub.metrics.evicted.Load() != 1 {
		t.Fatal("actual queue measurements disagree with admission")
	}
}
