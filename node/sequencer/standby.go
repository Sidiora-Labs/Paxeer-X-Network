package sequencer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer/wal"
)

// Replication wire format. The primary sends a group as u32be count ‖ count WAL frames, each
// u32be crc32c(entry) ‖ u32be len(entry) ‖ entry exactly as the WAL writes it. The standby answers
// every group with one Ack: u8 status ‖ u64be lastSeq ‖ u64be gapFrom ‖ u64be gapTo.
const (
	maxGroupFrames  = 1 << 20
	maxGroupBytes   = 256 << 20
	frameHeaderSize = 8
	ackSize         = 25
)

var frameCRC = crc32.MakeTable(crc32.Castagnoli)

var (
	// ErrNoAck is returned by Release when the standby did not acknowledge the group within the
	// replication timeout; no receipt of the group may be released.
	ErrNoAck = errors.New("sequencer: standby did not acknowledge")
	// ErrStandbyRefused is returned by Release when the standby answered with a refusal; no receipt
	// of the group may be released.
	ErrStandbyRefused = errors.New("sequencer: standby refused the group")
	// ErrGroupTooLarge marks a replication group over maxGroupFrames frames or maxGroupBytes bytes.
	ErrGroupTooLarge = errors.New("sequencer: replication group too large")
	// ErrSharedKey is returned by NewStandby when the standby would sign with a primary key.
	ErrSharedKey = errors.New("sequencer: standby shares a key with the primary")
	// ErrStaleLease marks a LEASE record whose epoch is below the epoch the standby holds.
	ErrStaleLease = errors.New("sequencer: lease epoch below the held epoch")
	// ErrSealContinuity marks a SEAL record that closes fewer seqs than the standby holds.
	ErrSealContinuity = errors.New("sequencer: seal does not cover the replicated seqs")
)

// AckStatus is the standby's verdict on one replicated group.
type AckStatus uint8

const (
	// AckOK: every record of the group is synced on the standby and re-executed.
	AckOK AckStatus = 1
	// AckGap: records before the group are missing; nothing of the group was taken.
	AckGap AckStatus = 2
	// AckRefused: the group breaks fencing or continuity, or the standby has stopped.
	AckRefused AckStatus = 3
)

// Ack is the standby's answer to one replicated group.
type Ack struct {
	Status AckStatus
	// LastSeq is the last seq the standby holds synced and re-executed.
	LastSeq uint64
	// GapFrom..GapTo is the seq range a gap answer requests; GapTo below GapFrom means only
	// non-ACCEPT records are missing. On an Ack returned by Release they name the ACCEPT seqs the
	// primary resent ahead of the group, either requested by a gap answer or retained and sent on
	// a fresh link, zero when there were none.
	GapFrom uint64
	GapTo   uint64
}

func (a Ack) encode() [ackSize]byte {
	var b [ackSize]byte
	b[0] = byte(a.Status)
	binary.BigEndian.PutUint64(b[1:9], a.LastSeq)
	binary.BigEndian.PutUint64(b[9:17], a.GapFrom)
	binary.BigEndian.PutUint64(b[17:25], a.GapTo)
	return b
}

func decodeAck(b [ackSize]byte) (Ack, error) {
	a := Ack{
		Status:  AckStatus(b[0]),
		LastSeq: binary.BigEndian.Uint64(b[1:9]),
		GapFrom: binary.BigEndian.Uint64(b[9:17]),
		GapTo:   binary.BigEndian.Uint64(b[17:25]),
	}
	switch a.Status {
	case AckOK, AckGap, AckRefused:
		return a, nil
	default:
		return Ack{}, fmt.Errorf("sequencer: unknown ack status %d", b[0])
	}
}

func encodeGroup(frames []wal.Frame) ([]byte, error) {
	if len(frames) == 0 {
		return nil, errors.New("sequencer: empty replication group")
	}
	if len(frames) > maxGroupFrames {
		return nil, fmt.Errorf("%w: %d frames, max %d", ErrGroupTooLarge, len(frames), maxGroupFrames)
	}
	size := 0
	for _, f := range frames {
		size += len(f)
	}
	if size > maxGroupBytes {
		return nil, fmt.Errorf("%w: %d bytes, max %d", ErrGroupTooLarge, size, maxGroupBytes)
	}
	buf := make([]byte, 4, 4+size)
	binary.BigEndian.PutUint32(buf, uint32(len(frames))) //nolint:gosec // bounded by maxGroupFrames
	for _, f := range frames {
		buf = append(buf, f...)
	}
	return buf, nil
}

// readGroup reads one group, refusing oversized groups and frames whose checksum or entry is bad.
func readGroup(r io.Reader) ([]wal.Entry, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > maxGroupFrames {
		return nil, fmt.Errorf("%w: %d frames", ErrGroupTooLarge, n)
	}
	entries := make([]wal.Entry, 0, min(n, 1024))
	total := 0
	for range n {
		var fh [frameHeaderSize]byte
		if _, err := io.ReadFull(r, fh[:]); err != nil {
			return nil, err
		}
		size := binary.BigEndian.Uint32(fh[4:8])
		if size > wal.MaxEntrySize {
			return nil, fmt.Errorf("%w: %d bytes", wal.ErrEntryTooLarge, size)
		}
		total += frameHeaderSize + int(size)
		if total > maxGroupBytes {
			return nil, fmt.Errorf("%w: over %d bytes", ErrGroupTooLarge, maxGroupBytes)
		}
		entry := make([]byte, size)
		if _, err := io.ReadFull(r, entry); err != nil {
			return nil, err
		}
		if crc32.Checksum(entry, frameCRC) != binary.BigEndian.Uint32(fh[0:4]) {
			return nil, wal.ErrChecksum
		}
		e, err := wal.DecodeEntry(entry)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// ReplicatorConfig configures the primary side of standby replication.
type ReplicatorConfig struct {
	// WAL is the primary's sequencer WAL.
	WAL *wal.WAL
	// Dial opens the authenticated link to the standby (mutual TLS on a private network).
	Dial func(ctx context.Context) (net.Conn, error)
	// ReplicationTimeout bounds dialling, sending a group and reading the standby's ack, gap
	// refill included. It is set at activation rehearsal; there is no default.
	ReplicationTimeout time.Duration
}

// Replicator makes groups of WAL records durable on the primary and the standby before their
// receipts are released.
type Replicator struct {
	// ponytail: one Release at a time keeps the wire in WAL order; pipeline groups if one fsync
	// plus one round trip per group caps throughput.
	mu       sync.Mutex
	wal      *wal.WAL
	dial     func(ctx context.Context) (net.Conn, error)
	timeout  time.Duration
	conn     net.Conn
	retained []retainedFrame
}

// retainedFrame is a record kept until the standby acknowledges a later COMMITTED; the retained
// window is resent on every fresh link and on a gap answer.
type retainedFrame struct {
	frame     wal.Frame
	seq       uint64
	committed bool
}

// NewReplicator builds the primary side of replication over cfg.
func NewReplicator(cfg ReplicatorConfig) (*Replicator, error) {
	if cfg.WAL == nil {
		return nil, errors.New("sequencer: replicator without a wal")
	}
	if cfg.Dial == nil {
		return nil, errors.New("sequencer: replicator without a standby dialer")
	}
	if cfg.ReplicationTimeout <= 0 {
		return nil, fmt.Errorf("sequencer: replication timeout %s must be positive", cfg.ReplicationTimeout)
	}
	return &Replicator{wal: cfg.WAL, dial: cfg.Dial, timeout: cfg.ReplicationTimeout}, nil
}

// Release appends entries to the local WAL, waits for the group fsync that covers them, streams
// them to the standby and waits for its ack. The receipts of the ACCEPT entries may be returned
// only when Release returns nil; on ErrNoAck or ErrStandbyRefused the send fails and no receipt
// is issued. Records the standby has not acknowledged are resent on every fresh link and when it
// requests a gap.
func (r *Replicator) Release(ctx context.Context, entries ...wal.Entry) (Ack, error) {
	if len(entries) == 0 {
		return Ack{}, errors.New("sequencer: empty release group")
	}
	frames := make([]wal.Frame, len(entries))
	var maxSeq uint64
	for i, e := range entries {
		f, err := wal.NewFrame(e)
		if err != nil {
			return Ack{}, err
		}
		frames[i] = f
		if a, ok := e.(*wal.Accept); ok && a.Seq > maxSeq {
			maxSeq = a.Seq
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	pending := make([]*wal.Pending, 0, len(entries))
	for i, e := range entries {
		p, err := r.wal.Append(e)
		if err != nil {
			return Ack{}, err
		}
		rf := retainedFrame{frame: frames[i]}
		switch v := e.(type) {
		case *wal.Accept:
			rf.seq = v.Seq
		case *wal.Committed:
			rf.committed = true
		}
		r.retained = append(r.retained, rf)
		pending = append(pending, p)
	}
	if _, err := r.wal.GroupSync(); err != nil {
		return Ack{}, err
	}
	for _, p := range pending {
		if err := p.Wait(ctx); err != nil {
			return Ack{}, err
		}
	}
	ack, err := r.replicate(ctx, frames, maxSeq)
	if err != nil {
		return Ack{}, err
	}
	r.pruneRetained()
	return ack, nil
}

// replicate sends frames, the tail of the retained window, all within the replication timeout.
// A fresh link may follow a failed Release whose records are retained but were never
// acknowledged, so it carries the whole retained window; the standby skips the records it holds.
// On an open link a gap answer is refilled with the whole retained window.
func (r *Replicator) replicate(ctx context.Context, frames []wal.Frame, maxSeq uint64) (Ack, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var gap Ack
	fresh := r.conn == nil
	if fresh {
		gap.GapFrom, gap.GapTo = r.retainedSeqs(len(r.retained) - len(frames))
		frames = r.window()
	}
	ack, err := r.exchange(ctx, frames)
	if err == nil && ack.Status == AckGap && !fresh {
		gap = ack
		ack, err = r.exchange(ctx, r.window())
	}
	if err != nil {
		r.dropConn()
		return Ack{}, fmt.Errorf("%w: %w", ErrNoAck, err)
	}
	switch ack.Status {
	case AckOK:
		if ack.LastSeq < maxSeq {
			r.dropConn()
			return Ack{}, fmt.Errorf("%w: standby acknowledged seq %d, group holds seq %d", ErrNoAck, ack.LastSeq, maxSeq)
		}
		ack.GapFrom, ack.GapTo = gap.GapFrom, gap.GapTo
		return ack, nil
	case AckGap:
		return Ack{}, fmt.Errorf("%w: standby still misses seqs %d..%d after the retained window", ErrNoAck, ack.GapFrom, ack.GapTo)
	default:
		return Ack{}, fmt.Errorf("%w: standby holds seq %d", ErrStandbyRefused, ack.LastSeq)
	}
}

// window returns the frames of the retained window in WAL order.
func (r *Replicator) window() []wal.Frame {
	window := make([]wal.Frame, len(r.retained))
	for i, rf := range r.retained {
		window[i] = rf.frame
	}
	return window
}

// retainedSeqs returns the first and last ACCEPT seq among the first n retained records, zero
// when they hold none.
func (r *Replicator) retainedSeqs(n int) (from, to uint64) {
	for _, rf := range r.retained[:n] {
		if rf.seq == 0 {
			continue
		}
		if from == 0 {
			from = rf.seq
		}
		to = rf.seq
	}
	return from, to
}

func (r *Replicator) exchange(ctx context.Context, frames []wal.Frame) (Ack, error) {
	msg, err := encodeGroup(frames)
	if err != nil {
		return Ack{}, err
	}
	if r.conn == nil {
		conn, err := r.dial(ctx)
		if err != nil {
			return Ack{}, err
		}
		r.conn = conn
	}
	deadline, _ := ctx.Deadline()
	if err := r.conn.SetDeadline(deadline); err != nil {
		return Ack{}, err
	}
	if _, err := r.conn.Write(msg); err != nil {
		return Ack{}, err
	}
	var b [ackSize]byte
	if _, err := io.ReadFull(r.conn, b[:]); err != nil {
		return Ack{}, err
	}
	return decodeAck(b)
}

// dropConn closes a link whose stream position is unknown; the next Release dials again.
func (r *Replicator) dropConn() {
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
}

// pruneRetained drops the records up to the last retained COMMITTED once the standby has
// acknowledged a group. The retained window is resent on every fresh link, so that COMMITTED was
// sent and acknowledged on the link that carried the group.
func (r *Replicator) pruneRetained() {
	for i := len(r.retained) - 1; i >= 0; i-- {
		if r.retained[i].committed {
			r.retained = append([]retainedFrame(nil), r.retained[i+1:]...)
			return
		}
	}
}

// Close closes the link to the standby.
func (r *Replicator) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropConn()
}

// StandbyConfig configures the standby side of replication.
type StandbyConfig struct {
	// WAL is the standby's own sequencer WAL.
	WAL *wal.WAL
	// Executor re-executes ACCEPT records through the same executor and DeliverTx path as the
	// primary into the standby's own L_N and L_open. Its Apply receives OPEN, SEAL, COMMITTED and
	// LEASE records.
	Executor wal.Executor
	// ConsensusKey is the standby's own privval; it never equals the primary's.
	ConsensusKey tmtypes.PrivValidator
	// ReceiptKey is the standby's own receipt signing key; it never equals the primary's.
	ReceiptKey ed25519.PrivateKey
	// PrimaryConsensusKey and PrimaryReceiptKey are the primary's public keys.
	PrimaryConsensusKey crypto.PubKey
	PrimaryReceiptKey   ed25519.PublicKey
	// CommittedHeight and LastSeq are the committed height and seq the Executor starts from.
	CommittedHeight uint64
	LastSeq         uint64
}

// standbyCursor is the position of the standby's replicated state.
type standbyCursor struct {
	lastSeq         uint64
	openHeight      uint64
	sealedHeight    uint64
	committedHeight uint64
	leaseEpoch      uint64
}

// gapError reports records missing before the one admitted.
type gapError struct {
	from, to uint64
}

func (g gapError) Error() string {
	return fmt.Sprintf("sequencer: records before seq %d..%d missing", g.from, g.to)
}

// admit reports whether e is new (true) or already held (false), a gapError when records before
// it are missing, or a refusal when it breaks fencing or continuity.
func (c *standbyCursor) admit(e wal.Entry) (bool, error) {
	missing := gapError{from: c.lastSeq + 1, to: c.lastSeq}
	switch v := e.(type) {
	case *wal.Open:
		if v.Height <= c.openHeight {
			return false, nil
		}
		if v.Height != c.sealedHeight+1 {
			return false, missing
		}
	case *wal.Accept:
		if v.Seq <= c.lastSeq {
			return false, nil
		}
		if v.Seq != c.lastSeq+1 || v.Height != c.openHeight || c.openHeight == c.sealedHeight {
			missing.to = v.Seq - 1
			return false, missing
		}
	case *wal.Seal:
		if v.Height <= c.sealedHeight {
			return false, nil
		}
		if v.Height != c.openHeight || v.ToSeq > c.lastSeq {
			missing.to = max(v.ToSeq, c.lastSeq)
			return false, missing
		}
		if v.ToSeq < c.lastSeq {
			return false, fmt.Errorf("%w: seal to seq %d, held seq %d", ErrSealContinuity, v.ToSeq, c.lastSeq)
		}
	case *wal.Committed:
		if v.Height <= c.committedHeight {
			return false, nil
		}
		if v.Height > c.sealedHeight {
			return false, missing
		}
	case *wal.Lease:
		if v.Epoch < c.leaseEpoch {
			return false, fmt.Errorf("%w: epoch %d, held %d", ErrStaleLease, v.Epoch, c.leaseEpoch)
		}
	default:
		return false, fmt.Errorf("%w: entry %T", wal.ErrUnknownKind, e)
	}
	return true, nil
}

func (c *standbyCursor) advance(e wal.Entry) {
	switch v := e.(type) {
	case *wal.Open:
		c.openHeight = v.Height
	case *wal.Accept:
		c.lastSeq = v.Seq
	case *wal.Seal:
		c.sealedHeight = v.Height
	case *wal.Committed:
		c.committedHeight = v.Height
	case *wal.Lease:
		c.leaseEpoch = v.Epoch
	}
}

// Standby is the hot standby: it appends and syncs every replicated group to its own WAL,
// re-executes it in seq order and only then acknowledges it.
type Standby struct {
	mu           sync.Mutex
	wal          *wal.WAL
	exec         wal.Executor
	consensusKey tmtypes.PrivValidator
	receiptKey   ed25519.PrivateKey
	cur          standbyCursor
	failed       error
}

// NewStandby checks that the standby holds its own consensus and receipt keys, then replays the
// records of its WAL after the last COMMITTED through cfg.Executor.
func NewStandby(ctx context.Context, cfg StandbyConfig) (*Standby, error) {
	if cfg.WAL == nil {
		return nil, errors.New("sequencer: standby without a wal")
	}
	if cfg.Executor == nil {
		return nil, errors.New("sequencer: standby without an executor")
	}
	if cfg.ConsensusKey == nil {
		return nil, errors.New("sequencer: standby without a consensus key")
	}
	if len(cfg.ReceiptKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("sequencer: standby receipt key is %d bytes, want %d", len(cfg.ReceiptKey), ed25519.PrivateKeySize)
	}
	if len(cfg.PrimaryReceiptKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("sequencer: primary receipt key is %d bytes, want %d", len(cfg.PrimaryReceiptKey), ed25519.PublicKeySize)
	}
	if cfg.PrimaryConsensusKey.Compare(crypto.PubKey{}) == 0 {
		return nil, errors.New("sequencer: standby without the primary consensus key")
	}
	consensusPub, err := cfg.ConsensusKey.GetPubKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("sequencer: standby consensus key: %w", err)
	}
	if consensusPub.Compare(cfg.PrimaryConsensusKey) == 0 {
		return nil, fmt.Errorf("%w: consensus key %s", ErrSharedKey, consensusPub)
	}
	if receiptPub, _ := cfg.ReceiptKey.Public().(ed25519.PublicKey); receiptPub.Equal(cfg.PrimaryReceiptKey) {
		return nil, fmt.Errorf("%w: receipt key %x", ErrSharedKey, []byte(receiptPub))
	}

	cur := standbyCursor{
		lastSeq:         cfg.LastSeq,
		openHeight:      cfg.CommittedHeight,
		sealedHeight:    cfg.CommittedHeight,
		committedHeight: cfg.CommittedHeight,
	}
	if c := cfg.WAL.LastCommitted(); c != nil && c.Height >= cfg.CommittedHeight {
		cur = standbyCursor{lastSeq: c.LastSeq, openHeight: c.Height, sealedHeight: c.Height, committedHeight: c.Height}
	}
	s := &Standby{
		wal:          cfg.WAL,
		exec:         cfg.Executor,
		consensusKey: cfg.ConsensusKey,
		receiptKey:   cfg.ReceiptKey,
		cur:          cur,
	}
	if _, err := cfg.WAL.Replay(standbyReplay{s}); err != nil {
		return nil, fmt.Errorf("sequencer: standby wal replay: %w", err)
	}
	return s, nil
}

// standbyReplay advances the cursor while the WAL replays into the executor.
type standbyReplay struct{ s *Standby }

func (r standbyReplay) Apply(e wal.Entry) error {
	if err := r.s.exec.Apply(e); err != nil {
		return err
	}
	r.s.cur.advance(e)
	return nil
}

func (r standbyReplay) Execute(a *wal.Accept) ([]byte, error) {
	receipt, err := r.s.exec.Execute(a)
	if err != nil {
		return nil, err
	}
	r.s.cur.advance(a)
	return receipt, nil
}

// LastSeq returns the last seq the standby holds synced and re-executed.
func (s *Standby) LastSeq() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur.lastSeq
}

// Err returns the failure that stopped the standby, nil while it acknowledges.
func (s *Standby) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// ConsensusKey returns the standby's own privval.
func (s *Standby) ConsensusKey() tmtypes.PrivValidator { return s.consensusKey }

// ReceiptKey returns the standby's own receipt signing key.
func (s *Standby) ReceiptKey() ed25519.PrivateKey { return s.receiptKey }

// Serve answers the groups the primary streams on every connection l accepts, until l fails.
func (s *Standby) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.serveConn(conn)
	}
}

func (s *Standby) serveConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		entries, err := readGroup(r)
		if err != nil {
			return
		}
		reply := s.receive(entries).encode()
		if _, err := conn.Write(reply[:]); err != nil {
			return
		}
	}
}

// receive takes one group: records already held are skipped, a gap before the group is answered
// with AckGap and nothing taken; otherwise the new records are appended and synced to the
// standby WAL, re-executed in order, and acknowledged. A WAL or execution failure stops the
// standby: it refuses every later group.
func (s *Standby) receive(entries []wal.Entry) Ack {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return Ack{Status: AckRefused, LastSeq: s.cur.lastSeq}
	}
	next := s.cur
	fresh := make([]wal.Entry, 0, len(entries))
	for _, e := range entries {
		ok, err := next.admit(e)
		var gap gapError
		if errors.As(err, &gap) {
			return Ack{Status: AckGap, LastSeq: s.cur.lastSeq, GapFrom: gap.from, GapTo: gap.to}
		}
		if err != nil {
			return Ack{Status: AckRefused, LastSeq: s.cur.lastSeq}
		}
		if ok {
			fresh = append(fresh, e)
			next.advance(e)
		}
	}
	if len(fresh) == 0 {
		return Ack{Status: AckOK, LastSeq: s.cur.lastSeq}
	}
	if err := s.persist(fresh); err != nil {
		s.failed = err
		return Ack{Status: AckRefused, LastSeq: s.cur.lastSeq}
	}
	for _, e := range fresh {
		if err := s.execute(e); err != nil {
			s.failed = err
			return Ack{Status: AckRefused, LastSeq: s.cur.lastSeq}
		}
		s.cur.advance(e)
	}
	return Ack{Status: AckOK, LastSeq: s.cur.lastSeq}
}

// persist appends records to the standby WAL and returns once one fdatasync covers them.
func (s *Standby) persist(entries []wal.Entry) error {
	pending := make([]*wal.Pending, 0, len(entries))
	for _, e := range entries {
		p, err := s.wal.Append(e)
		if err != nil {
			return err
		}
		pending = append(pending, p)
	}
	if _, err := s.wal.GroupSync(); err != nil {
		return err
	}
	for _, p := range pending {
		if err := p.Wait(context.Background()); err != nil {
			return err
		}
	}
	return nil
}

// execute re-executes one record; an ACCEPT must give the primary's receipt bytes.
func (s *Standby) execute(e wal.Entry) error {
	a, ok := e.(*wal.Accept)
	if !ok {
		return s.exec.Apply(e)
	}
	receipt, err := s.exec.Execute(a)
	if err != nil {
		return fmt.Errorf("sequencer: standby execute seq %d: %w", a.Seq, err)
	}
	if !bytes.Equal(receipt, a.Receipt) {
		return fmt.Errorf("%w: seq %d", wal.ErrReceiptMismatch, a.Seq)
	}
	return nil
}
