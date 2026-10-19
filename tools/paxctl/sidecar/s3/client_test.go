package s3

import (
	"sync"
	"testing"
)

// WriteAtBuffer is a goroutine-safe in-memory io.WriterAt, used for
// downloading small S3 objects (e.g. latest.txt) via DownloadObject.
type WriteAtBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (w *WriteAtBuffer) WriteAt(p []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	end := int(off) + len(p)
	if end > len(w.buf) {
		grown := make([]byte, end)
		copy(grown, w.buf)
		w.buf = grown
	}
	copy(w.buf[off:], p)
	return len(p), nil
}

func (w *WriteAtBuffer) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]byte, len(w.buf))
	copy(out, w.buf)
	return out
}

func TestWriteAtBuffer_BasicWriteAndRead(t *testing.T) {
	var buf WriteAtBuffer
	data := []byte("hello world")
	n, err := buf.WriteAt(data, 0)
	if err != nil {
		t.Fatalf("WriteAt error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("WriteAt returned %d, want %d", n, len(data))
	}
	got := string(buf.Bytes())
	if got != "hello world" {
		t.Errorf("Bytes() = %q, want %q", got, "hello world")
	}
}

func TestWriteAtBuffer_NonZeroOffset(t *testing.T) {
	var buf WriteAtBuffer
	buf.WriteAt([]byte("aaa"), 0)
	buf.WriteAt([]byte("bbb"), 3)

	got := string(buf.Bytes())
	if got != "aaabbb" {
		t.Errorf("Bytes() = %q, want %q", got, "aaabbb")
	}
}

func TestWriteAtBuffer_OverlappingWrite(t *testing.T) {
	var buf WriteAtBuffer
	buf.WriteAt([]byte("AAAA"), 0)
	buf.WriteAt([]byte("BB"), 1)

	got := string(buf.Bytes())
	if got != "ABBA" {
		t.Errorf("Bytes() = %q, want %q", got, "ABBA")
	}
}

func TestWriteAtBuffer_SparseWrite(t *testing.T) {
	var buf WriteAtBuffer
	buf.WriteAt([]byte("X"), 5)

	got := buf.Bytes()
	if len(got) != 6 {
		t.Fatalf("len(Bytes()) = %d, want 6", len(got))
	}
	for i := 0; i < 5; i++ {
		if got[i] != 0 {
			t.Errorf("Bytes()[%d] = %d, want 0", i, got[i])
		}
	}
	if got[5] != 'X' {
		t.Errorf("Bytes()[5] = %q, want 'X'", got[5])
	}
}

func TestWriteAtBuffer_GrowsBuffer(t *testing.T) {
	var buf WriteAtBuffer
	buf.WriteAt([]byte("ab"), 0)
	buf.WriteAt([]byte("cdef"), 2)

	got := string(buf.Bytes())
	if got != "abcdef" {
		t.Errorf("Bytes() = %q, want %q", got, "abcdef")
	}
}

func TestWriteAtBuffer_BytesReturnsDefensiveCopy(t *testing.T) {
	var buf WriteAtBuffer
	buf.WriteAt([]byte("original"), 0)

	snapshot := buf.Bytes()
	snapshot[0] = 'X'

	got := string(buf.Bytes())
	if got != "original" {
		t.Errorf("internal buffer was mutated via Bytes() return: got %q, want %q", got, "original")
	}
}

func TestWriteAtBuffer_EmptyBuffer(t *testing.T) {
	var buf WriteAtBuffer
	got := buf.Bytes()
	if len(got) != 0 {
		t.Errorf("Bytes() on empty buffer has len %d, want 0", len(got))
	}
}

func TestWriteAtBuffer_ConcurrentWrites(t *testing.T) {
	var buf WriteAtBuffer
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			buf.WriteAt([]byte{byte(offset)}, int64(offset))
		}(i)
	}
	wg.Wait()

	got := buf.Bytes()
	if len(got) != 100 {
		t.Fatalf("len(Bytes()) = %d, want 100", len(got))
	}
	for i := range 100 {
		if got[i] != byte(i) {
			t.Errorf("Bytes()[%d] = %d, want %d", i, got[i], i)
		}
	}
}
