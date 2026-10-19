package shadow

import "testing"

// closeableState is a StateReader with a no-return Close() (the shape
// *ethclient.Client uses), recording whether Close was called.
type closeableState struct {
	*mockState
	closed *bool
}

func (c closeableState) Close() { *c.closed = true }

// closeableKeySource is a KeySource with a no-return Close().
type closeableKeySource struct {
	mockKeySource
	closed *bool
}

func (c closeableKeySource) Close() { *c.closed = true }

// Comparator.Close must close the configured readers/key source. *ethclient.Client
// and *rpc.Client expose Close() with no return (not io.Closer), so this guards
// against the regression where an io.Closer assertion silently skipped them.
func TestComparator_CloseClosesReaders(t *testing.T) {
	var shadowClosed, canonClosed, ksClosed bool
	shadow := closeableState{mockState: newMockState(), closed: &shadowClosed}
	canon := closeableState{mockState: newMockState(), closed: &canonClosed}
	ks := closeableKeySource{closed: &ksClosed}

	comp := NewComparator("http://shadow", "http://canon", WithLayer2(shadow, canon, ks))
	comp.Close()

	if !shadowClosed || !canonClosed {
		t.Errorf("state readers not closed: shadow=%v canon=%v", shadowClosed, canonClosed)
	}
	if !ksClosed {
		t.Error("key source not closed")
	}
}

// Close must be safe when Layer 2 was never configured (nil readers).
func TestComparator_CloseNoLayer2(t *testing.T) {
	comp := NewComparator("http://shadow", "http://canon")
	comp.Close() // must not panic
}

// compile-time guard: TraceKeySource matches the no-return Close() shape that
// Comparator.Close asserts (the same shape *ethclient.Client / *rpc.Client use).
var _ interface{ Close() } = (*TraceKeySource)(nil)
