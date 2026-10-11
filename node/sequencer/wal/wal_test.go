package wal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiman/uint256"
)

// chainExecutor derives every receipt from a running hash over the executed history, so a
// receipt depends on the committed state and on the order of every earlier ACCEPT.
type chainExecutor struct {
	state   [32]byte
	applied []Entry
}

func (c *chainExecutor) Apply(e Entry) error {
	c.applied = append(c.applied, e)
	return nil
}

func (c *chainExecutor) Execute(a *Accept) ([]byte, error) {
	h := sha256.New()
	h.Write(c.state[:])
	h.Write(binary.BigEndian.AppendUint64(nil, a.Seq))
	h.Write(a.Tx)
	copy(c.state[:], h.Sum(nil))
	return append([]byte("receipt:"), c.state[:]...), nil
}

func mustOpen(t *testing.T, dir string, cfg Config) *WAL {
	t.Helper()
	w, err := OpenWAL(dir, cfg)
	if err != nil {
		t.Fatalf("OpenWAL(%s): %v", dir, err)
	}
	return w
}

func mustAppend(t *testing.T, w *WAL, e Entry) *Pending {
	t.Helper()
	p, err := w.Append(e)
	if err != nil {
		t.Fatalf("Append(%s): %v", e.Kind(), err)
	}
	return p
}

func mustSync(t *testing.T, w *WAL, want int) {
	t.Helper()
	n, err := w.GroupSync()
	if err != nil {
		t.Fatalf("GroupSync: %v", err)
	}
	if n != want {
		t.Fatalf("GroupSync covered %d frames, want %d", n, want)
	}
}

func frameLen(t *testing.T, e Entry) int64 {
	t.Helper()
	f, err := NewFrame(e)
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}
	return int64(len(f))
}

func tx(seq uint64) []byte { return []byte{0xf8, byte(seq >> 8), byte(seq), 0x01} }

func TestWALGroupSyncBeforeRelease(t *testing.T) {
	dir := t.TempDir()
	w := mustOpen(t, dir, DefaultConfig())
	const sends = 32
	var wantSize int64
	for i := uint64(1); i <= sends; i++ {
		wantSize += frameLen(t, &Accept{Seq: i, Height: 7, Tx: tx(i), Receipt: tx(i + 1000)})
	}

	var appended, done sync.WaitGroup
	var released atomic.Int32
	errs := make([]error, sends)
	sizes := make([]int64, sends)
	appended.Add(sends)
	done.Add(sends)
	for i := 0; i < sends; i++ {
		go func(i int) {
			defer done.Done()
			seq := uint64(i + 1)
			p, err := w.Append(&Accept{Seq: seq, Height: 7, Tx: tx(seq), Receipt: tx(seq + 1000)})
			appended.Done()
			if err != nil {
				errs[i] = err
				return
			}
			if errs[i] = p.Wait(context.Background()); errs[i] != nil {
				return
			}
			released.Add(1)
			info, err := os.Stat(filepath.Join(dir, headName))
			if err != nil {
				errs[i] = err
				return
			}
			sizes[i] = info.Size()
		}(i)
	}
	appended.Wait()
	time.Sleep(50 * time.Millisecond)
	if got := released.Load(); got != 0 {
		t.Fatalf("%d receipts released before GroupSync", got)
	}

	mustSync(t, w, sends)
	done.Wait()
	if got := released.Load(); got != sends {
		t.Fatalf("released %d receipts, want %d", got, sends)
	}
	for i := 0; i < sends; i++ {
		if errs[i] != nil {
			t.Fatalf("send %d: %v", i, errs[i])
		}
		if sizes[i] != wantSize {
			t.Fatalf("send %d released with %d bytes on disk, want all %d frame bytes", i, sizes[i], wantSize)
		}
	}
	mustSync(t, w, 0)

	p := mustAppend(t, w, &Accept{Seq: sends + 1, Height: 7, Tx: tx(sends + 1), Receipt: tx(1)})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait before GroupSync = %v, want DeadlineExceeded", err)
	}
	w.Close()
	if err := p.Wait(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait after Close without GroupSync = %v, want ErrClosed", err)
	}
	if _, err := w.Append(&Lease{Epoch: 1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Append after Close = %v, want ErrClosed", err)
	}
	if _, err := w.GroupSync(); !errors.Is(err, ErrClosed) {
		t.Fatalf("GroupSync after Close = %v, want ErrClosed", err)
	}
}

func TestWALCorruptTailStopsReplay(t *testing.T) {
	dir := t.TempDir()
	open := &Open{Height: 5, OpenTimeMs: 1_000, BaseFee: uint256.NewInt(67_000_000)}
	a1 := &Accept{Seq: 1, Height: 5, Tx: tx(1)}
	a2 := &Accept{Seq: 2, Height: 5, Tx: tx(2)}
	primary := &chainExecutor{}
	for _, a := range []*Accept{a1, a2} {
		r, err := primary.Execute(a)
		if err != nil {
			t.Fatal(err)
		}
		a.Receipt = r
	}
	w := mustOpen(t, dir, DefaultConfig())
	for _, e := range []Entry{open, a1, a2} {
		mustAppend(t, w, e)
	}
	mustSync(t, w, 3)
	w.Close()

	headPath := filepath.Join(dir, headName)
	raw, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x01
	if err := os.WriteFile(headPath, raw, filePerms); err != nil {
		t.Fatal(err)
	}

	w = mustOpen(t, dir, DefaultConfig())
	defer w.Close()
	exec := &chainExecutor{}
	res, err := w.Replay(exec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	goodSize := frameLen(t, open) + frameLen(t, a1)
	want := &CorruptTail{Segment: headName, Offset: goodSize, Reason: ErrChecksum}
	if !reflect.DeepEqual(res.Tail, want) {
		t.Fatalf("Tail = %+v, want %+v", res.Tail, want)
	}
	if res.Accepts != 1 || res.LastAccept.Seq != 1 {
		t.Fatalf("replayed %d accepts ending at %+v, want only seq 1", res.Accepts, res.LastAccept)
	}
	if !reflect.DeepEqual(exec.applied, []Entry{open}) {
		t.Fatalf("applied %+v, want the OPEN only", exec.applied)
	}
	info, err := os.Stat(headPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != goodSize {
		t.Fatalf("head is %d bytes after open, want truncated to %d", info.Size(), goodSize)
	}

	mustAppend(t, w, a2)
	mustSync(t, w, 1)
	res, err = w.Replay(&chainExecutor{})
	if err != nil {
		t.Fatalf("Replay after re-append: %v", err)
	}
	if res.Accepts != 2 || res.LastAccept.Seq != 2 {
		t.Fatalf("replayed %d accepts ending at %+v, want seqs 1 and 2", res.Accepts, res.LastAccept)
	}

	torn := t.TempDir()
	tw := mustOpen(t, torn, DefaultConfig())
	mustAppend(t, tw, open)
	mustAppend(t, tw, a1)
	mustSync(t, tw, 2)
	tw.Close()
	tornPath := filepath.Join(torn, headName)
	if err := os.Truncate(tornPath, frameLen(t, open)+frameLen(t, a1)-3); err != nil {
		t.Fatal(err)
	}
	tw = mustOpen(t, torn, DefaultConfig())
	defer tw.Close()
	res, err = tw.Replay(&chainExecutor{})
	if err != nil {
		t.Fatalf("Replay of torn head: %v", err)
	}
	want = &CorruptTail{Segment: headName, Offset: frameLen(t, open), Reason: ErrTorn}
	if !reflect.DeepEqual(res.Tail, want) || res.Accepts != 0 {
		t.Fatalf("torn replay = %+v, want tail %+v and no accepts", res, want)
	}

	sealedDir := t.TempDir()
	sw := mustOpen(t, sealedDir, Config{SegmentSizeLimit: 1})
	mustAppend(t, sw, open)
	mustSync(t, sw, 1)
	sw.Close()
	sealedPath := filepath.Join(sealedDir, segment{index: 0, maxHeight: 5}.name())
	raw, err = os.ReadFile(sealedPath)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x80
	if err := os.WriteFile(sealedPath, raw, filePerms); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWAL(sealedDir, DefaultConfig()); !errors.Is(err, ErrCorruptSegment) || !errors.Is(err, ErrChecksum) {
		t.Fatalf("Open with corrupt sealed segment = %v, want ErrCorruptSegment and ErrChecksum", err)
	}
}

func TestWALEntryKindsRoundTrip(t *testing.T) {
	var hash, appHash, key [32]byte
	for i := range hash {
		hash[i], appHash[i], key[i] = byte(i), byte(0xa0+i), byte(0xff-i)
	}
	fee := new(uint256.Int).Lsh(uint256.NewInt(0xdeadbeef), 200)
	committed := &Committed{Height: 41, LastSeq: 900, AppHash: appHash}
	accept := &Accept{Seq: 901, Height: 42, Tx: []byte{0x02, 0xf8, 0x70}}
	receipt, err := (&chainExecutor{}).Execute(accept)
	if err != nil {
		t.Fatal(err)
	}
	accept.Receipt = receipt
	entries := []Entry{
		&Open{Height: 42, OpenTimeMs: 1_760_000_000_123, BaseFee: fee},
		accept,
		&Seal{Height: 42, FromSeq: 901, ToSeq: 901, LastSeqHash: hash},
		&Lease{Epoch: 3, KeyID: key, ExpiryMs: 1_760_000_000_500},
	}
	wantKinds := []EntryKind{KindOpen, KindAccept, KindSeal, KindLease}
	for i, e := range append([]Entry{committed}, entries...) {
		b, err := EncodeEntry(e)
		if err != nil {
			t.Fatalf("EncodeEntry(%s): %v", e.Kind(), err)
		}
		if b[0] != Version || b[1] != byte(e.Kind()) {
			t.Fatalf("entry %d header = %x, want version 1 kind %d", i, b[:2], e.Kind())
		}
		got, err := DecodeEntry(b)
		if err != nil {
			t.Fatalf("DecodeEntry(%s): %v", e.Kind(), err)
		}
		if !reflect.DeepEqual(got, e) {
			t.Fatalf("round trip of %s = %+v, want %+v", e.Kind(), got, e)
		}
	}
	for i, e := range entries {
		if e.Kind() != wantKinds[i] {
			t.Fatalf("entry %d kind = %s, want %s", i, e.Kind(), wantKinds[i])
		}
	}
	if KindOpen != 1 || KindAccept != 2 || KindSeal != 3 || KindCommitted != 4 || KindLease != 5 {
		t.Fatal("entry kind numbers moved")
	}

	dir := t.TempDir()
	w := mustOpen(t, dir, DefaultConfig())
	mustAppend(t, w, committed)
	for _, e := range entries {
		mustAppend(t, w, e)
	}
	mustSync(t, w, 5)
	w.Close()
	w = mustOpen(t, dir, DefaultConfig())
	exec := &chainExecutor{}
	res, err := w.Replay(exec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !reflect.DeepEqual(res.From, committed) || !reflect.DeepEqual(w.LastCommitted(), committed) {
		t.Fatalf("COMMITTED read back as %+v / %+v, want %+v", res.From, w.LastCommitted(), committed)
	}
	if !reflect.DeepEqual(res.LastAccept, entries[1]) || res.Accepts != 1 {
		t.Fatalf("ACCEPT read back as %+v (%d), want %+v", res.LastAccept, res.Accepts, entries[1])
	}
	if want := []Entry{entries[0], entries[2], entries[3]}; !reflect.DeepEqual(exec.applied, want) {
		t.Fatalf("applied %+v, want %+v", exec.applied, want)
	}
	if res.Tail != nil {
		t.Fatalf("clean WAL reported tail %+v", res.Tail)
	}
	w.Close()

	good, err := EncodeEntry(committed)
	if err != nil {
		t.Fatal(err)
	}
	refusals := []struct {
		name string
		b    []byte
		want error
	}{
		{"version 0", append([]byte{0}, good[1:]...), ErrUnknownVersion},
		{"version 2", append([]byte{2}, good[1:]...), ErrUnknownVersion},
		{"kind 0", append([]byte{Version, 0}, good[2:]...), ErrUnknownKind},
		{"kind 6", append([]byte{Version, 6}, good[2:]...), ErrUnknownKind},
		{"short payload", good[:len(good)-1], ErrMalformed},
		{"trailing byte", append(append([]byte(nil), good...), 0), ErrMalformed},
		{"header only", good[:1], ErrMalformed},
		{"accept length past end", []byte{Version, byte(KindAccept), 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0xff, 0xff, 0xff, 0xff}, ErrMalformed},
		{"seal from above to", mustEncode(t, &Seal{Height: 1, FromSeq: 2, ToSeq: 2}, func(b []byte) { b[len(b)-1] = 3 }), ErrMalformed},
	}
	for _, r := range refusals {
		if _, err := DecodeEntry(r.b); !errors.Is(err, r.want) {
			t.Fatalf("DecodeEntry(%s) = %v, want %v", r.name, err, r.want)
		}
	}
	if _, err := EncodeEntry(&Open{Height: 1}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("EncodeEntry(OPEN without base fee) = %v, want ErrMalformed", err)
	}
	if _, err := EncodeEntry(&Seal{FromSeq: 5, ToSeq: 4}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("EncodeEntry(SEAL from above to) = %v, want ErrMalformed", err)
	}
	if _, err := EncodeEntry(&Accept{Tx: make([]byte, MaxEntrySize)}); !errors.Is(err, ErrEntryTooLarge) {
		t.Fatalf("EncodeEntry(oversized ACCEPT) = %v, want ErrEntryTooLarge", err)
	}

	unknown := t.TempDir()
	bad := append([]byte{Version, 9}, good[2:]...)
	frame := binary.BigEndian.AppendUint32(nil, crc32.Checksum(bad, crc32c))
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(bad)))
	if err := os.WriteFile(filepath.Join(unknown, headName), append(frame, bad...), filePerms); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWAL(unknown, DefaultConfig()); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Open over a well-checksummed unknown kind = %v, want ErrUnknownKind", err)
	}
}

func mustEncode(t *testing.T, e Entry, mutate func([]byte)) []byte {
	t.Helper()
	b, err := EncodeEntry(e)
	if err != nil {
		t.Fatal(err)
	}
	mutate(b)
	return b
}

// writeHeight appends OPEN, the ACCEPTs and SEAL of one block followed by its COMMITTED, executing
// each ACCEPT on exec to obtain its receipt.
func writeHeight(t *testing.T, w *WAL, exec *chainExecutor, height uint64, seq *uint64, txs int) []*Accept {
	t.Helper()
	mustAppend(t, w, &Open{Height: height, OpenTimeMs: height * 400, BaseFee: uint256.NewInt(67_000_000)})
	from := *seq + 1
	var accepts []*Accept
	for i := 0; i < txs; i++ {
		*seq++
		a := &Accept{Seq: *seq, Height: height, Tx: tx(*seq)}
		r, err := exec.Execute(a)
		if err != nil {
			t.Fatal(err)
		}
		a.Receipt = r
		mustAppend(t, w, a)
		accepts = append(accepts, a)
	}
	mustAppend(t, w, &Seal{Height: height, FromSeq: from, ToSeq: *seq, LastSeqHash: exec.state})
	return accepts
}

func TestWALReplayReproducesReceipts(t *testing.T) {
	build := func(dir string, alter int) (*chainExecutor, [32]byte, []*Accept) {
		w := mustOpen(t, dir, DefaultConfig())
		defer w.Close()
		primary := &chainExecutor{}
		var seq uint64
		writeHeight(t, w, primary, 1000, &seq, 7)
		mustAppend(t, w, &Committed{Height: 1000, LastSeq: seq, AppHash: primary.state})
		committedState := primary.state
		mustAppend(t, w, &Open{Height: 1001, OpenTimeMs: 400_400, BaseFee: uint256.NewInt(67_000_000)})
		var accepts []*Accept
		for i := 0; i < 40; i++ {
			seq++
			a := &Accept{Seq: seq, Height: 1001, Tx: tx(seq)}
			r, err := primary.Execute(a)
			if err != nil {
				t.Fatal(err)
			}
			a.Receipt = r
			if i == alter {
				a.Receipt = append([]byte(nil), r...)
				a.Receipt[len(a.Receipt)-1] ^= 0x01
			}
			mustAppend(t, w, a)
			accepts = append(accepts, a)
		}
		if _, err := w.GroupSync(); err != nil {
			t.Fatal(err)
		}
		return primary, committedState, accepts
	}

	dir := t.TempDir()
	primary, committedState, accepts := build(dir, -1)
	w := mustOpen(t, dir, DefaultConfig())
	fresh := &chainExecutor{state: committedState}
	res, err := w.Replay(fresh)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.From == nil || res.From.Height != 1000 || res.From.LastSeq != 7 {
		t.Fatalf("replay started after %+v, want COMMITTED(1000) at seq 7", res.From)
	}
	if res.Accepts != 40 || !reflect.DeepEqual(res.LastAccept, accepts[39]) {
		t.Fatalf("replayed %d accepts ending at %+v, want 40 ending at seq 47", res.Accepts, res.LastAccept)
	}
	if fresh.state != primary.state {
		t.Fatalf("replayed state %x, want primary state %x", fresh.state, primary.state)
	}
	if len(fresh.applied) != 1 || fresh.applied[0].Kind() != KindOpen {
		t.Fatalf("applied %+v, want the OPEN of height 1001 only", fresh.applied)
	}
	w.Close()

	altered := t.TempDir()
	_, committedState, _ = build(altered, 16)
	w = mustOpen(t, altered, DefaultConfig())
	defer w.Close()
	res, err = w.Replay(&chainExecutor{state: committedState})
	if !errors.Is(err, ErrReceiptMismatch) {
		t.Fatalf("Replay with an altered receipt = %v, want ErrReceiptMismatch", err)
	}
	if res.Accepts != 16 || res.LastAccept.Seq != 23 {
		t.Fatalf("replay stopped after %d accepts at %+v, want 16 ending at seq 23", res.Accepts, res.LastAccept)
	}
}

func sealedSegments(t *testing.T, dir string) []segment {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []segment
	for _, de := range des {
		if s, ok := parseSegment(de.Name()); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestWALSegmentsPrunedInBackground(t *testing.T) {
	dir := t.TempDir()
	w := mustOpen(t, dir, Config{SegmentSizeLimit: 512})
	defer w.Close()
	exec := &chainExecutor{}
	var seq uint64
	for h := uint64(1800); h <= 2000; h++ {
		writeHeight(t, w, exec, h, &seq, 2)
		mustAppend(t, w, &Committed{Height: h, LastSeq: seq, AppHash: exec.state})
		mustSync(t, w, 5)
	}

	before := sealedSegments(t, dir)
	if len(before) < 20 {
		t.Fatalf("only %d sealed segments, want many rotations", len(before))
	}
	var below int
	for i, s := range before {
		if s.index != uint64(i) {
			t.Fatalf("segment %d has index %d: the Append path deleted a segment", i, s.index)
		}
		if s.maxHeight < 1900 {
			below++
		}
	}
	if below == 0 || below == len(before) {
		t.Fatalf("%d of %d segments below 1900, want some on each side", below, len(before))
	}

	removed := make(chan int, 1)
	pruneErr := make(chan error, 1)
	go func() {
		n, err := w.Prune(1900)
		removed <- n
		pruneErr <- err
	}()
	mustAppend(t, w, &Open{Height: 2001, OpenTimeMs: 2001 * 400, BaseFee: uint256.NewInt(67_000_000)})
	for i := 0; i < 8; i++ {
		seq++
		a := &Accept{Seq: seq, Height: 2001, Tx: tx(seq)}
		r, err := exec.Execute(a)
		if err != nil {
			t.Fatal(err)
		}
		a.Receipt = r
		p := mustAppend(t, w, a)
		if _, err := w.GroupSync(); err != nil {
			t.Fatal(err)
		}
		if err := p.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := <-removed, <-pruneErr; err != nil || n != below {
		t.Fatalf("Prune(1900) = %d, %v; want %d, nil", n, err, below)
	}

	after := sealedSegments(t, dir)
	if len(after) < len(before)-below {
		t.Fatalf("%d segments left, want at least %d", len(after), len(before)-below)
	}
	for i, s := range after {
		if s.maxHeight < 1900 {
			t.Fatalf("segment %s holds only heights below 1900 and survived", s.name())
		}
		if s.index != uint64(below+i) {
			t.Fatalf("segment %d has index %d, want %d", i, s.index, below+i)
		}
	}
	if c := w.LastCommitted(); c == nil || c.Height != 2000 {
		t.Fatalf("LastCommitted = %+v, want height 2000", c)
	}
	if n, err := w.Prune(1900); err != nil || n != 0 {
		t.Fatalf("second Prune(1900) = %d, %v; want 0, nil", n, err)
	}

	fresh := t.TempDir()
	fw := mustOpen(t, fresh, Config{SegmentSizeLimit: 1})
	defer fw.Close()
	mustAppend(t, fw, &Open{Height: 10, BaseFee: uint256.NewInt(1)})
	mustSync(t, fw, 1)
	if n, err := fw.Prune(1_000_000); err != nil || n != 0 {
		t.Fatalf("Prune without any COMMITTED = %d, %v; want 0, nil", n, err)
	}
}
