package audit

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func entry(i int) Entry {
	return Entry{
		Kind:      "evm_tx",
		KeyID:     fmt.Sprintf("key-%04d", i%17),
		Subject:   []byte(fmt.Sprintf("0x02f87083%06x", i)),
		Decision:  "allow",
		Reason:    "policy_ok",
		SessionID: fmt.Sprintf("session-%06d", i),
	}
}

func openLog(t *testing.T, dir string) *Log {
	t.Helper()
	l, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestConcurrentAppendsFormOneDenseChain(t *testing.T) {
	dir := t.TempDir()
	l := openLog(t, dir)
	const workers, perWorker = 8, 125
	records := make(chan Record, workers*perWorker)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				rec, err := l.Append(entry(w*perWorker + i))
				if err != nil {
					t.Errorf("Append: %v", err)
					return
				}
				records <- rec
			}
		}(w)
	}
	wg.Wait()
	close(records)
	seen := make(map[uint64]Record, workers*perWorker)
	for rec := range records {
		if _, dup := seen[rec.Sequence]; dup {
			t.Fatalf("sequence %d issued twice", rec.Sequence)
		}
		seen[rec.Sequence] = rec
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("got %d records, want %d", len(seen), workers*perWorker)
	}
	for seq := uint64(1); seq <= workers*perWorker; seq++ {
		rec, ok := seen[seq]
		if !ok {
			t.Fatalf("sequence %d missing", seq)
		}
		if seq > 1 && rec.PrevHash != seen[seq-1].Hash {
			t.Fatalf("record %d does not link to record %d", seq, seq-1)
		}
	}
	if err := l.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	seq, head := l.Head()
	if seq != workers*perWorker || head != seen[seq].Hash {
		t.Fatalf("Head = %d %x, want %d %x", seq, head, workers*perWorker, seen[workers*perWorker].Hash)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened := openLog(t, dir)
	rseq, rhead := reopened.Head()
	if rseq != seq || rhead != head {
		t.Fatalf("reopened Head = %d %x, want %d %x", rseq, rhead, seq, head)
	}
	next, err := reopened.Append(entry(workers * perWorker))
	if err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}
	if next.Sequence != seq+1 || next.PrevHash != head {
		t.Fatalf("append after reopen got sequence %d prev %x", next.Sequence, next.PrevHash)
	}
	if err := reopened.Verify(); err != nil {
		t.Fatalf("Verify after reopen: %v", err)
	}
}

func TestRecordStoresSubjectHashOnly(t *testing.T) {
	dir := t.TempDir()
	l := openLog(t, dir)
	e := entry(7)
	rec, err := l.Append(e)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if rec.SubjectHash != sha256.Sum256(e.Subject) {
		t.Fatalf("subject hash = %x, want %x", rec.SubjectHash, sha256.Sum256(e.Subject))
	}
	if rec.Sequence != 1 || rec.PrevHash != ([32]byte{}) {
		t.Fatalf("first record sequence %d prev %x", rec.Sequence, rec.PrevHash)
	}
	if rec.Kind != e.Kind || rec.KeyID != e.KeyID || rec.Decision != e.Decision || rec.Reason != e.Reason || rec.SessionID != e.SessionID {
		t.Fatalf("record fields %+v do not match entry %+v", rec, e)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for i := 0; i+len(e.Subject) <= len(raw); i++ {
		if string(raw[i:i+len(e.Subject)]) == string(e.Subject) {
			t.Fatalf("log file contains the raw subject at offset %d", i)
		}
	}
}

func TestHeadMatchesLastAppend(t *testing.T) {
	l := openLog(t, t.TempDir())
	seq, head := l.Head()
	if seq != 0 || head != ([32]byte{}) {
		t.Fatalf("empty Head = %d %x", seq, head)
	}
	var last Record
	for i := 0; i < 25; i++ {
		rec, err := l.Append(entry(i))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		last = rec
	}
	seq, head = l.Head()
	if seq != last.Sequence || head != last.Hash || seq != 25 {
		t.Fatalf("Head = %d %x, want %d %x", seq, head, last.Sequence, last.Hash)
	}
}

func TestFlippedByteFailsVerifyAtOffendingRecord(t *testing.T) {
	dir := t.TempDir()
	l := openLog(t, dir)
	path := filepath.Join(dir, FileName)
	const n = 200
	ends := make([]int64, n)
	for i := 0; i < n; i++ {
		if _, err := l.Append(entry(i)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		ends[i] = info.Size()
	}
	if err := l.Verify(); err != nil {
		t.Fatalf("Verify before tamper: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	mid := int64(len(raw) / 2)
	var want uint64
	for i, end := range ends {
		if mid < end {
			want = uint64(i + 1)
			break
		}
	}
	raw[mid] ^= 0x01
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write tampered log: %v", err)
	}
	err = l.Verify()
	var ce *CorruptionError
	if !errors.As(err, &ce) {
		t.Fatalf("Verify after tamper = %v, want CorruptionError", err)
	}
	if ce.Sequence != want {
		t.Fatalf("Verify reported sequence %d, want %d", ce.Sequence, want)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Open(dir); !errors.As(err, &ce) || ce.Sequence != want {
		t.Fatalf("Open of tampered log = %v, want CorruptionError at %d", err, want)
	}
}

func TestTruncatedTailRefusedByOpen(t *testing.T) {
	dir := t.TempDir()
	l := openLog(t, dir)
	for i := 0; i < 40; i++ {
		if _, err := l.Append(entry(i)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	path := filepath.Join(dir, FileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := os.Truncate(path, info.Size()-5); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, err = Open(dir)
	var ce *CorruptionError
	if !errors.As(err, &ce) {
		t.Fatalf("Open of truncated log = %v, want CorruptionError", err)
	}
	if ce.Sequence != 40 {
		t.Fatalf("Open reported sequence %d, want 40", ce.Sequence)
	}
}

func TestVerifyDetectsTruncationWhileOpen(t *testing.T) {
	dir := t.TempDir()
	l := openLog(t, dir)
	path := filepath.Join(dir, FileName)
	var sizeAfterTen int64
	for i := 0; i < 12; i++ {
		if _, err := l.Append(entry(i)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if i == 9 {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			sizeAfterTen = info.Size()
		}
	}
	if err := os.Truncate(path, sizeAfterTen); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := l.Verify(); !errors.Is(err, ErrHeadMismatch) {
		t.Fatalf("Verify after whole-record truncation = %v, want ErrHeadMismatch", err)
	}
}

func TestAppendAfterCloseRefused(t *testing.T) {
	l := openLog(t, t.TempDir())
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := l.Append(entry(1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Append after Close = %v, want ErrClosed", err)
	}
}
