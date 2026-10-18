package broker

import (
	"testing"
	"time"
)

func TestDrainClosesAdmissionBeforeNotice(t *testing.T) {
	l := NewLifecycle()
	notice, leave, err := l.Join()
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	start := time.Now()
	l.Drain()
	if l.Ready() {
		t.Fatal("draining gateway ready")
	}
	if _, _, err = l.Join(); err == nil {
		t.Fatal("upgrade admitted during drain")
	}
	select {
	case n := <-notice:
		if n.Deadline.Before(start.Add(29*time.Second)) || n.Deadline.After(start.Add(31*time.Second)) {
			t.Fatal(n)
		}
	default:
		t.Fatal("active connection not notified")
	}
	l.Drain()
	leave()
	if l.Connections() != 0 {
		t.Fatal(l.Connections())
	}
	for i := 0; i < 100; i++ {
		delay, err := ReconnectDelay()
		if err != nil || delay < 0 || delay > 30000 {
			t.Fatal(delay, err)
		}
	}
}

func TestControlActionLimit(t *testing.T) {
	var limit ActionLimit
	now := time.Unix(1800000000, 0)
	for i := 0; i < 10; i++ {
		if !limit.Allow(now) {
			t.Fatal(i)
		}
	}
	if limit.Allow(now.Add(999 * time.Millisecond)) {
		t.Fatal("control burst exceeded10/s")
	}
	if !limit.Allow(now.Add(time.Second)) {
		t.Fatal("next bounded window denied")
	}
}
