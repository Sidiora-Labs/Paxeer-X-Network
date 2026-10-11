// Package wal is the sequencer write-ahead log.
//
// Every executor step is written as one frame, u32be crc32c(entry) ‖ u32be len(entry) ‖ entry,
// the framing of the consensus WAL. An entry is u8 version ‖ u8 kind ‖ payload.
// Frames go to a head segment; GroupSync covers every frame appended since the previous
// GroupSync with one fdatasync and only then releases their Pending handles, so a receipt is
// never returned for a frame that is not on disk. When the head reaches the size limit it is
// sealed under a name carrying its index and the highest height it holds; Prune deletes sealed
// segments below a retain height from a background worker, never from the Append path.
//
//	Dir/
//	- wal.lock                    // flock guard
//	- <index>-<maxHeight>.seg     // sealed segments, hex, ascending index
//	- head.seg                    // segment being appended
package wal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/holiman/uint256"
	"golang.org/x/sys/unix"
)

// Version is the only entry version this package writes and accepts.
const Version byte = 1

// MaxEntrySize bounds one entry; a frame header announcing more is treated as a torn frame.
const MaxEntrySize = 8 << 20

const (
	headerSize   = 8
	filePerms    = os.FileMode(0o600)
	headName     = "head.seg"
	lockName     = "wal.lock"
	sealedSuffix = ".seg"
)

var crc32c = crc32.MakeTable(crc32.Castagnoli)

var (
	// ErrClosed is returned by every operation after Close or after a write or sync failed.
	ErrClosed = errors.New("sequencer wal closed")
	// ErrUnknownVersion is returned when an entry carries a version other than Version.
	ErrUnknownVersion = errors.New("unknown wal entry version")
	// ErrUnknownKind is returned when an entry carries a kind outside 1..5.
	ErrUnknownKind = errors.New("unknown wal entry kind")
	// ErrMalformed is returned when an entry payload does not match the layout of its kind.
	ErrMalformed = errors.New("malformed wal entry")
	// ErrEntryTooLarge is returned when an encoded entry exceeds MaxEntrySize.
	ErrEntryTooLarge = errors.New("wal entry too large")
	// ErrChecksum marks a frame whose crc32c does not match its entry.
	ErrChecksum = errors.New("wal frame checksum mismatch")
	// ErrTorn marks a frame cut short by the end of its segment.
	ErrTorn = errors.New("wal frame torn")
	// ErrCorruptSegment is returned by OpenWAL when a sealed segment holds a bad frame.
	ErrCorruptSegment = errors.New("sealed wal segment corrupt")
	// ErrReceiptMismatch is returned by Replay when a re-executed ACCEPT gives other receipt bytes.
	ErrReceiptMismatch = errors.New("replayed receipt differs from stored receipt")
)

// EntryKind names the executor step an entry records.
type EntryKind uint8

const (
	KindOpen      EntryKind = 1
	KindAccept    EntryKind = 2
	KindSeal      EntryKind = 3
	KindCommitted EntryKind = 4
	KindLease     EntryKind = 5
)

func (k EntryKind) String() string {
	switch k {
	case KindOpen:
		return "OPEN"
	case KindAccept:
		return "ACCEPT"
	case KindSeal:
		return "SEAL"
	case KindCommitted:
		return "COMMITTED"
	case KindLease:
		return "LEASE"
	default:
		return fmt.Sprintf("EntryKind(%d)", uint8(k))
	}
}

// Entry is one of *Open, *Accept, *Seal, *Committed or *Lease.
type Entry interface {
	Kind() EntryKind
	appendPayload(dst []byte) ([]byte, error)
}

// Open records the opening of the pending block.
type Open struct {
	Height     uint64
	OpenTimeMs uint64
	BaseFee    *uint256.Int
}

// Accept records one executed transaction and its canonical signed receipt.
type Accept struct {
	Seq     uint64
	Height  uint64
	Tx      []byte
	Receipt []byte
}

// Seal records the sealing of seqs FromSeq..ToSeq into the block at Height.
type Seal struct {
	Height      uint64
	FromSeq     uint64
	ToSeq       uint64
	LastSeqHash [32]byte
}

// Committed records the commit of the block at Height.
type Committed struct {
	Height  uint64
	LastSeq uint64
	AppHash [32]byte
}

// Lease records the open-block lease held by the host with KeyID.
type Lease struct {
	Epoch    uint64
	KeyID    [32]byte
	ExpiryMs uint64
}

func (*Open) Kind() EntryKind      { return KindOpen }
func (*Accept) Kind() EntryKind    { return KindAccept }
func (*Seal) Kind() EntryKind      { return KindSeal }
func (*Committed) Kind() EntryKind { return KindCommitted }
func (*Lease) Kind() EntryKind     { return KindLease }

func (e *Open) appendPayload(dst []byte) ([]byte, error) {
	if e.BaseFee == nil {
		return nil, fmt.Errorf("%w: OPEN without base fee", ErrMalformed)
	}
	dst = binary.BigEndian.AppendUint64(dst, e.Height)
	dst = binary.BigEndian.AppendUint64(dst, e.OpenTimeMs)
	fee := e.BaseFee.Bytes32()
	return append(dst, fee[:]...), nil
}

func (e *Accept) appendPayload(dst []byte) ([]byte, error) {
	dst = binary.BigEndian.AppendUint64(dst, e.Seq)
	dst = binary.BigEndian.AppendUint64(dst, e.Height)
	dst = appendBytes(dst, e.Tx)
	return appendBytes(dst, e.Receipt), nil
}

func (e *Seal) appendPayload(dst []byte) ([]byte, error) {
	if e.FromSeq > e.ToSeq {
		return nil, fmt.Errorf("%w: SEAL fromSeq %d above toSeq %d", ErrMalformed, e.FromSeq, e.ToSeq)
	}
	dst = binary.BigEndian.AppendUint64(dst, e.Height)
	dst = binary.BigEndian.AppendUint64(dst, e.ToSeq)
	dst = append(dst, e.LastSeqHash[:]...)
	return binary.BigEndian.AppendUint64(dst, e.FromSeq), nil
}

func (e *Committed) appendPayload(dst []byte) ([]byte, error) {
	dst = binary.BigEndian.AppendUint64(dst, e.Height)
	dst = binary.BigEndian.AppendUint64(dst, e.LastSeq)
	return append(dst, e.AppHash[:]...), nil
}

func (e *Lease) appendPayload(dst []byte) ([]byte, error) {
	dst = binary.BigEndian.AppendUint64(dst, e.Epoch)
	dst = append(dst, e.KeyID[:]...)
	return binary.BigEndian.AppendUint64(dst, e.ExpiryMs), nil
}

func appendBytes(dst, b []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(b))) //nolint:gosec // bounded by MaxEntrySize in EncodeEntry
	return append(dst, b...)
}

// EncodeEntry encodes e as u8 version ‖ u8 kind ‖ payload.
func EncodeEntry(e Entry) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: nil entry", ErrMalformed)
	}
	buf, err := e.appendPayload([]byte{Version, byte(e.Kind())})
	if err != nil {
		return nil, err
	}
	if len(buf) > MaxEntrySize {
		return nil, fmt.Errorf("%w: %d bytes, max %d", ErrEntryTooLarge, len(buf), MaxEntrySize)
	}
	return buf, nil
}

// DecodeEntry decodes one entry, refusing unknown versions and kinds and any byte left over.
func DecodeEntry(b []byte) (Entry, error) {
	if len(b) < 2 {
		return nil, fmt.Errorf("%w: %d bytes", ErrMalformed, len(b))
	}
	if b[0] != Version {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, b[0])
	}
	kind := EntryKind(b[1])
	d := decoder{b: b[2:]}
	var e Entry
	switch kind {
	case KindOpen:
		e = &Open{Height: d.u64(), OpenTimeMs: d.u64(), BaseFee: new(uint256.Int).SetBytes32(d.fixed(32))}
	case KindAccept:
		e = &Accept{Seq: d.u64(), Height: d.u64(), Tx: d.bytes(), Receipt: d.bytes()}
	case KindSeal:
		s := &Seal{Height: d.u64(), ToSeq: d.u64()}
		copy(s.LastSeqHash[:], d.fixed(32))
		s.FromSeq = d.u64()
		if !d.failed && s.FromSeq > s.ToSeq {
			return nil, fmt.Errorf("%w: SEAL fromSeq %d above toSeq %d", ErrMalformed, s.FromSeq, s.ToSeq)
		}
		e = s
	case KindCommitted:
		c := &Committed{Height: d.u64(), LastSeq: d.u64()}
		copy(c.AppHash[:], d.fixed(32))
		e = c
	case KindLease:
		l := &Lease{Epoch: d.u64()}
		copy(l.KeyID[:], d.fixed(32))
		l.ExpiryMs = d.u64()
		e = l
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownKind, b[1])
	}
	if d.failed || len(d.b) != 0 {
		return nil, fmt.Errorf("%w: %s payload length", ErrMalformed, kind)
	}
	return e, nil
}

type decoder struct {
	b      []byte
	failed bool
}

func (d *decoder) fixed(n int) []byte {
	if d.failed || len(d.b) < n {
		d.failed = true
		return make([]byte, n)
	}
	out := d.b[:n]
	d.b = d.b[n:]
	return out
}

func (d *decoder) u64() uint64 { return binary.BigEndian.Uint64(d.fixed(8)) }

func (d *decoder) bytes() []byte {
	n := binary.BigEndian.Uint32(d.fixed(4))
	if d.failed || uint64(n) > uint64(len(d.b)) {
		d.failed = true
		return nil
	}
	return append([]byte(nil), d.fixed(int(n))...)
}

// Frame is one encoded record: u32be crc32c(entry) ‖ u32be len(entry) ‖ entry.
type Frame []byte

// NewFrame encodes e and frames it.
func NewFrame(e Entry) (Frame, error) {
	entry, err := EncodeEntry(e)
	if err != nil {
		return nil, err
	}
	f := make(Frame, headerSize, headerSize+len(entry))
	binary.BigEndian.PutUint32(f[0:4], crc32.Checksum(entry, crc32c))
	binary.BigEndian.PutUint32(f[4:8], uint32(len(entry))) //nolint:gosec // bounded by MaxEntrySize
	return append(f, entry...), nil
}

func entryHeight(e Entry) (uint64, bool) {
	switch v := e.(type) {
	case *Open:
		return v.Height, true
	case *Accept:
		return v.Height, true
	case *Seal:
		return v.Height, true
	case *Committed:
		return v.Height, true
	default:
		return 0, false
	}
}

// CorruptTail locates the frame at which the head segment stopped being readable.
type CorruptTail struct {
	Segment string
	Offset  int64
	Reason  error
}

// scanSegment reads every good frame of path, calling fn with its decoded entry, and returns the
// size of the good prefix and the tail error (ErrChecksum or ErrTorn), nil when the file ends cleanly.
func scanSegment(path string, fn func(Entry) error) (int64, error, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	left := info.Size()
	r := bufio.NewReader(f)
	var good int64
	var header [headerSize]byte
	for left > 0 {
		if left < headerSize {
			return good, ErrTorn, nil
		}
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return 0, nil, err
		}
		n := int64(binary.BigEndian.Uint32(header[4:8]))
		if n > MaxEntrySize || n > left-headerSize {
			return good, ErrTorn, nil
		}
		entry := make([]byte, n)
		if _, err := io.ReadFull(r, entry); err != nil {
			return 0, nil, err
		}
		if crc32.Checksum(entry, crc32c) != binary.BigEndian.Uint32(header[0:4]) {
			return good, ErrChecksum, nil
		}
		e, err := DecodeEntry(entry)
		if err != nil {
			return 0, nil, fmt.Errorf("%s at offset %d: %w", path, good, err)
		}
		if err := fn(e); err != nil {
			return 0, nil, err
		}
		left -= headerSize + n
		good += headerSize + n
	}
	return good, nil, nil
}

type segment struct {
	index     uint64
	maxHeight uint64
}

func (s segment) name() string {
	return fmt.Sprintf("%016x-%016x%s", s.index, s.maxHeight, sealedSuffix)
}

func parseSegment(name string) (segment, bool) {
	var s segment
	if len(name) != 16+1+16+len(sealedSuffix) {
		return s, false
	}
	if _, err := fmt.Sscanf(name, "%016x-%016x.seg", &s.index, &s.maxHeight); err != nil {
		return s, false
	}
	return s, s.name() == name
}

// Config bounds the head segment.
type Config struct {
	// SegmentSizeLimit seals the head once a GroupSync leaves it at or above this many bytes.
	SegmentSizeLimit int64
}

// DefaultConfig seals segments at 64 MiB.
func DefaultConfig() Config { return Config{SegmentSizeLimit: 64 << 20} }

type group struct {
	done chan struct{}
	err  error
	n    int
}

// Pending is the handle of one appended frame; its receipt may be released once Wait returns nil.
type Pending struct {
	g *group
}

// Wait blocks until a GroupSync covering the frame has returned and reports its result.
func (p *Pending) Wait(ctx context.Context) error {
	select {
	case <-p.g.done:
		return p.g.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WAL is the sequencer write-ahead log of one directory. All methods are safe for concurrent use.
type WAL struct {
	dir string
	cfg Config

	syncMu  sync.Mutex // serialises GroupSync
	pruneMu sync.Mutex // serialises Prune and Replay

	mu            sync.Mutex
	lock          *os.File
	head          *os.File
	buf           *bufio.Writer
	headSize      int64
	headMax       uint64
	nextIndex     uint64
	sealed        []segment
	lastCommitted *Committed
	tail          *CorruptTail
	cur           *group
	err           error
}

// OpenWAL locks dir, checks every sealed segment, truncates a corrupt or torn head tail to its last
// good frame (reported by Replay) and opens the head for appending.
func OpenWAL(dir string, cfg Config) (w *WAL, err error) {
	if cfg.SegmentSizeLimit <= 0 {
		return nil, fmt.Errorf("segment size limit %d must be positive", cfg.SegmentSizeLimit)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDONLY, filePerms)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // fd fits int
		_ = lock.Close()
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	w = &WAL{dir: dir, cfg: cfg, lock: lock, cur: &group{done: make(chan struct{})}}
	defer func() {
		if err != nil {
			_ = lock.Close()
		}
	}()
	names, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, de := range names {
		if s, ok := parseSegment(de.Name()); ok {
			w.sealed = append(w.sealed, s)
		}
	}
	sort.Slice(w.sealed, func(i, j int) bool { return w.sealed[i].index < w.sealed[j].index })
	if n := len(w.sealed); n > 0 {
		w.nextIndex = w.sealed[n-1].index + 1
		w.headMax = w.sealed[n-1].maxHeight
	}
	track := func(e Entry) error {
		if h, ok := entryHeight(e); ok && h > w.headMax {
			w.headMax = h
		}
		if c, ok := e.(*Committed); ok {
			w.lastCommitted = c
		}
		return nil
	}
	for _, s := range w.sealed {
		_, tailErr, err := scanSegment(filepath.Join(dir, s.name()), track)
		if err != nil {
			return nil, err
		}
		if tailErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrCorruptSegment, s.name(), tailErr)
		}
	}
	headPath := filepath.Join(dir, headName)
	head, err := os.OpenFile(headPath, os.O_CREATE|os.O_WRONLY, filePerms)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = head.Close()
		}
	}()
	if err := syncDir(dir); err != nil {
		return nil, err
	}
	good, tailErr, err := scanSegment(headPath, track)
	if err != nil {
		return nil, err
	}
	if tailErr != nil {
		w.tail = &CorruptTail{Segment: headName, Offset: good, Reason: tailErr}
		if err := head.Truncate(good); err != nil {
			return nil, err
		}
		if err := unix.Fdatasync(int(head.Fd())); err != nil { //nolint:gosec // fd fits int
			return nil, err
		}
	}
	if _, err := head.Seek(good, io.SeekStart); err != nil {
		return nil, err
	}
	w.head, w.headSize = head, good
	w.buf = bufio.NewWriterSize(head, 64<<10)
	return w, nil
}

func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// Append frames e into the head buffer and joins it to the current group. The frame is durable,
// and its receipt may be released, only once the returned Pending's Wait returns nil.
func (w *WAL) Append(e Entry) (*Pending, error) {
	frame, err := NewFrame(e)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return nil, w.err
	}
	if _, err := w.buf.Write(frame); err != nil {
		w.fail(err)
		return nil, err
	}
	w.headSize += int64(len(frame))
	if h, ok := entryHeight(e); ok && h > w.headMax {
		w.headMax = h
	}
	if c, ok := e.(*Committed); ok {
		w.lastCommitted = c
	}
	w.cur.n++
	return &Pending{g: w.cur}, nil
}

// GroupSync flushes every frame appended since the previous GroupSync, issues one fdatasync for
// them, releases their Pending handles and returns how many frames it covered. Appends made while
// the fdatasync runs join the next group. A failed write or sync fails the group and closes the WAL.
func (w *WAL) GroupSync() (int, error) {
	w.syncMu.Lock()
	defer w.syncMu.Unlock()

	w.mu.Lock()
	if w.err != nil {
		err := w.err
		w.mu.Unlock()
		return 0, err
	}
	g := w.cur
	if g.n == 0 {
		w.mu.Unlock()
		return 0, nil
	}
	w.cur = &group{done: make(chan struct{})}
	err := w.buf.Flush()
	head := w.head
	if err != nil {
		w.fail(err)
	}
	w.mu.Unlock()

	if err == nil {
		err = unix.Fdatasync(int(head.Fd())) //nolint:gosec // fd fits int
	}
	if err == nil {
		err = w.rotateIfFull()
	}
	if err != nil {
		w.mu.Lock()
		w.fail(err)
		w.mu.Unlock()
	}
	g.err = err
	close(g.done)
	if err != nil {
		return 0, err
	}
	return g.n, nil
}

// rotateIfFull seals a full head under its index and highest height and opens a fresh head.
func (w *WAL) rotateIfFull() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil || w.headSize < w.cfg.SegmentSizeLimit {
		return w.err
	}
	if err := w.buf.Flush(); err != nil {
		return err
	}
	if err := unix.Fdatasync(int(w.head.Fd())); err != nil { //nolint:gosec // fd fits int
		return err
	}
	if err := w.head.Close(); err != nil {
		return err
	}
	s := segment{index: w.nextIndex, maxHeight: w.headMax}
	if err := os.Rename(filepath.Join(w.dir, headName), filepath.Join(w.dir, s.name())); err != nil {
		return err
	}
	head, err := os.OpenFile(filepath.Join(w.dir, headName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerms)
	if err != nil {
		return err
	}
	w.head, w.headSize = head, 0
	w.buf.Reset(head)
	w.sealed = append(w.sealed, s)
	w.nextIndex++
	return syncDir(w.dir)
}

// fail records the first error, closes the WAL and fails the open group. Callers hold w.mu.
func (w *WAL) fail(err error) {
	if w.err != nil {
		return
	}
	w.err = fmt.Errorf("%w: %w", ErrClosed, err)
	w.cur.err = w.err
	close(w.cur.done)
	_ = w.head.Close()
	_ = w.lock.Close()
}

// Close fails any frames not yet covered by a GroupSync and releases the files.
func (w *WAL) Close() {
	w.syncMu.Lock()
	defer w.syncMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail(errors.New("closed"))
}

// LastCommitted returns the last COMMITTED entry written, nil when there is none.
func (w *WAL) LastCommitted() *Committed {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastCommitted
}

// Prune deletes the sealed segments whose every entry lies below retainHeight and below the last
// COMMITTED height. It is meant for the background pruning worker: it never blocks Append or
// GroupSync for the file deletions. It returns the number of segments removed.
func (w *WAL) Prune(retainHeight uint64) (int, error) {
	w.pruneMu.Lock()
	defer w.pruneMu.Unlock()

	w.mu.Lock()
	if w.err != nil {
		err := w.err
		w.mu.Unlock()
		return 0, err
	}
	var victims []segment
	if w.lastCommitted != nil {
		limit := min(retainHeight, w.lastCommitted.Height)
		i := 0
		for i < len(w.sealed) && w.sealed[i].maxHeight < limit {
			i++
		}
		victims = append(victims, w.sealed[:i]...)
		w.sealed = append([]segment(nil), w.sealed[i:]...)
	}
	w.mu.Unlock()

	for n, s := range victims {
		if err := os.Remove(filepath.Join(w.dir, s.name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
	}
	if len(victims) == 0 {
		return 0, nil
	}
	return len(victims), syncDir(w.dir)
}

// Executor re-executes the steps recorded after the last COMMITTED entry.
type Executor interface {
	// Apply is called for every OPEN, SEAL and LEASE entry, in WAL order.
	Apply(e Entry) error
	// Execute re-executes one ACCEPT entry and returns the receipt bytes it produces.
	Execute(a *Accept) ([]byte, error)
}

// ReplayResult describes a completed replay.
type ReplayResult struct {
	// From is the last COMMITTED entry replay started after, nil when the WAL holds none.
	From *Committed
	// Accepts is the number of ACCEPT entries re-executed with byte-identical receipts.
	Accepts int
	// LastAccept is the last ACCEPT re-executed, nil when there was none.
	LastAccept *Accept
	// Tail is the corrupt or torn head tail that OpenWAL cut off, nil when the head ended cleanly.
	Tail *CorruptTail
}

// Replay feeds every entry after the last COMMITTED entry to exec in WAL order and stops with
// ErrReceiptMismatch at the first ACCEPT whose re-executed receipt differs from the stored bytes.
// Appends wait while the segments are read; exec runs after they are read.
func (w *WAL) Replay(exec Executor) (ReplayResult, error) {
	w.pruneMu.Lock()
	defer w.pruneMu.Unlock()

	w.mu.Lock()
	if w.err != nil {
		err := w.err
		w.mu.Unlock()
		return ReplayResult{}, err
	}
	if err := w.buf.Flush(); err != nil {
		w.fail(err)
		w.mu.Unlock()
		return ReplayResult{}, err
	}
	res := ReplayResult{From: w.lastCommitted, Tail: w.tail}
	var after []Entry
	collect := func(e Entry) error {
		if _, ok := e.(*Committed); ok {
			after = after[:0]
			return nil
		}
		after = append(after, e)
		return nil
	}
	paths := make([]string, 0, len(w.sealed)+1)
	for _, s := range w.sealed {
		paths = append(paths, filepath.Join(w.dir, s.name()))
	}
	paths = append(paths, filepath.Join(w.dir, headName))
	for _, p := range paths {
		_, tailErr, err := scanSegment(p, collect)
		if err == nil && tailErr != nil {
			err = fmt.Errorf("%s: %w", p, tailErr)
		}
		if err != nil {
			w.mu.Unlock()
			return ReplayResult{}, err
		}
	}
	w.mu.Unlock()

	for _, e := range after {
		a, ok := e.(*Accept)
		if !ok {
			if err := exec.Apply(e); err != nil {
				return res, err
			}
			continue
		}
		got, err := exec.Execute(a)
		if err != nil {
			return res, fmt.Errorf("execute seq %d: %w", a.Seq, err)
		}
		if !bytes.Equal(got, a.Receipt) {
			return res, fmt.Errorf("%w: seq %d", ErrReceiptMismatch, a.Seq)
		}
		res.Accepts++
		res.LastAccept = a
	}
	return res, nil
}
