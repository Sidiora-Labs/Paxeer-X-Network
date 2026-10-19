package sequencer_test

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer/wal"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const replicationTimeout = 2 * time.Second

// appReplayer drives a real Sequential executor from WAL records; the receipt bytes are the
// executed outcome: u64be seq ‖ seqHash ‖ u64be open time ‖ ExecTxResult protobuf.
type appReplayer struct {
	exec sequencer.Executor
}

func (r appReplayer) Apply(e wal.Entry) error {
	switch v := e.(type) {
	case *wal.Open:
		return r.exec.Open(int64(v.Height), int64(v.OpenTimeMs)) //nolint:gosec // test heights and times fit int64
	case *wal.Seal:
		_, err := r.exec.Seal(int64(v.Height)) //nolint:gosec // test heights fit int64
		return err
	default:
		return nil
	}
}

func (r appReplayer) Execute(a *wal.Accept) ([]byte, error) {
	acc, err := r.exec.Accept(a.Tx)
	if err != nil {
		return nil, err
	}
	return receiptBytes(acc)
}

func receiptBytes(acc *sequencer.Accepted) ([]byte, error) {
	res, err := acc.Result.Marshal()
	if err != nil {
		return nil, err
	}
	b := binary.BigEndian.AppendUint64(nil, acc.Seq)
	b = append(b, acc.SeqHash[:]...)
	b = binary.BigEndian.AppendUint64(b, uint64(acc.TimestampMs)) //nolint:gosec // positive open time
	return append(b, res...), nil
}

// primary is the sequencer side of a test: a real executor, its WAL and its replicator.
type primary struct {
	t       *testing.T
	h       *harness
	wal     *wal.WAL
	repl    *sequencer.Replicator
	pv      *privval.FilePV
	receipt ed25519.PublicKey
	openMs  int64
}

func newPrimary(t *testing.T, addr string, timeout time.Duration, funded ...utils.TestAcct) *primary {
	t.Helper()
	h := newHarness(t, funded...)
	w, err := wal.OpenWAL(filepath.Join(t.TempDir(), "primary-wal"), wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(w.Close)
	repl, err := sequencer.NewReplicator(sequencer.ReplicatorConfig{
		WAL: w,
		Dial: func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
		ReplicationTimeout: timeout,
	})
	require.NoError(t, err)
	t.Cleanup(repl.Close)
	dir := t.TempDir()
	pv, err := privval.GenFilePV(filepath.Join(dir, "key.json"), filepath.Join(dir, "state.json"), "")
	require.NoError(t, err)
	receipt, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return &primary{t: t, h: h, wal: w, repl: repl, pv: pv, receipt: receipt}
}

// open opens the pending block a minute ahead so both hosts' genesis times lie before it.
func (p *primary) open() *wal.Open {
	p.t.Helper()
	p.openMs = time.Now().Add(time.Minute).UnixMilli()
	require.NoError(p.t, p.h.exec.Open(openHeight, p.openMs))
	return &wal.Open{Height: uint64(openHeight), OpenTimeMs: uint64(p.openMs), BaseFee: uint256.NewInt(0)}
}

func (p *primary) accept(txBytes []byte) *wal.Accept {
	p.t.Helper()
	acc, err := p.h.exec.Accept(txBytes)
	require.NoError(p.t, err)
	receipt, err := receiptBytes(acc)
	require.NoError(p.t, err)
	return &wal.Accept{Seq: acc.Seq, Height: uint64(acc.Height), Tx: txBytes, Receipt: receipt}
}

// standby is the hot-standby side: its own app, executor, WAL and keys.
type standby struct {
	h   *harness
	s   *sequencer.Standby
	wal *wal.WAL
	dir string
}

func newStandby(t *testing.T, p *primary, funded ...utils.TestAcct) *standby {
	t.Helper()
	h := newHarness(t, funded...)
	dir := filepath.Join(t.TempDir(), "standby-wal")
	w, err := wal.OpenWAL(dir, wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(w.Close)
	keyDir := t.TempDir()
	pv, err := privval.GenFilePV(filepath.Join(keyDir, "key.json"), filepath.Join(keyDir, "state.json"), "")
	require.NoError(t, err)
	_, receiptKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	primaryPub, err := p.pv.GetPubKey(context.Background())
	require.NoError(t, err)
	s, err := sequencer.NewStandby(context.Background(), sequencer.StandbyConfig{
		WAL:                 w,
		Executor:            appReplayer{exec: h.exec},
		ConsensusKey:        pv,
		ReceiptKey:          receiptKey,
		PrimaryConsensusKey: primaryPub,
		PrimaryReceiptKey:   p.receipt,
		CommittedHeight:     uint64(committedTip),
		LastSeq:             startSeq,
	})
	require.NoError(t, err)
	return &standby{h: h, s: s, wal: w, dir: dir}
}

func (sb *standby) serve(t *testing.T, l net.Listener) {
	t.Helper()
	t.Cleanup(func() { _ = l.Close() })
	go func() { _ = sb.s.Serve(l) }()
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return l
}

// requireStandbyState checks that both hosts hold the same balances and nonces on their L_open.
func requireStandbyState(t *testing.T, p *primary, sb *standby, addrs ...common.Address) {
	t.Helper()
	pctx, sctx := p.h.openCtx(), sb.h.openCtx()
	for _, a := range addrs {
		requireWei(t, p.h.weiBalance(pctx, a), sb.h.weiBalance(sctx, a))
		require.Equal(t, p.h.app.EvmKeeper.GetNonce(pctx, a), sb.h.app.EvmKeeper.GetNonce(sctx, a))
	}
}

func TestStandbyNoAckNoReceipt(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress

	// An unreachable standby: the send fails and no receipt is released.
	l := listen(t)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	p := newPrimary(t, addr, replicationTimeout, sender)
	_, bz := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	open := p.open()
	ack, err := p.repl.Release(context.Background(), open, p.accept(bz))
	require.ErrorIs(t, err, sequencer.ErrNoAck)
	require.Equal(t, sequencer.Ack{}, ack)

	// A standby whose re-execution gives other receipt bytes refuses, acknowledges nothing and
	// stays stopped.
	l = listen(t)
	p2 := newPrimary(t, l.Addr().String(), replicationTimeout, sender)
	sb := newStandby(t, p2, sender)
	sb.serve(t, l)
	open = p2.open()
	tampered := p2.accept(bz)
	tampered.Receipt = append([]byte(nil), tampered.Receipt...)
	tampered.Receipt[len(tampered.Receipt)-1] ^= 0xff
	ack, err = p2.repl.Release(context.Background(), open, tampered)
	require.ErrorIs(t, err, sequencer.ErrStandbyRefused)
	require.Equal(t, sequencer.Ack{}, ack)
	require.Equal(t, startSeq, sb.s.LastSeq())
	require.ErrorIs(t, sb.s.Err(), wal.ErrReceiptMismatch)

	_, err = p2.repl.Release(context.Background(), &wal.Lease{Epoch: 1, ExpiryMs: uint64(p2.openMs)})
	require.ErrorIs(t, err, sequencer.ErrStandbyRefused)
}

func TestStandbyReplayMatchesPrimary(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	l := listen(t)
	p := newPrimary(t, l.Addr().String(), replicationTimeout, sender)
	sb := newStandby(t, p, sender)
	sb.serve(t, l)

	_, first := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	_, second := transfer(t, sender, 1, recipient, big.NewInt(2*onePaxInWei))
	open := p.open()
	a1, a2 := p.accept(first), p.accept(second)
	require.Equal(t, startSeq+1, a1.Seq)
	require.Equal(t, startSeq+2, a2.Seq)

	ack, err := p.repl.Release(context.Background(), open, a1, a2)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: startSeq + 2}, ack)
	require.Equal(t, startSeq+2, sb.s.LastSeq())
	require.NoError(t, sb.s.Err())
	requireStandbyState(t, p, sb, sender.EvmAddress, recipient)
	requireWei(t, big.NewInt(3*onePaxInWei), sb.h.weiBalance(sb.h.openCtx(), recipient))
	require.Equal(t, uint64(2), sb.h.app.EvmKeeper.GetNonce(sb.h.openCtx(), sender.EvmAddress))
}

func TestStandbyAckBeforeRelease(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	l := listen(t)
	p := newPrimary(t, l.Addr().String(), replicationTimeout, sender)
	sb := newStandby(t, p, sender)
	sb.serve(t, l)

	_, bz := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	open := p.open()
	a := p.accept(bz)
	ack, err := p.repl.Release(context.Background(), open, a)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: startSeq + 1}, ack)
	// When Release returns, the standby has re-executed the record ...
	require.Equal(t, startSeq+1, sb.s.LastSeq())

	// ... and synced it: a fresh host replaying the standby WAL reproduces the receipt.
	require.NoError(t, l.Close())
	sb.wal.Close()
	fresh := newHarness(t, sender)
	w, err := wal.OpenWAL(sb.dir, wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(w.Close)
	res, err := w.Replay(appReplayer{exec: fresh.exec})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepts)
	require.Equal(t, a.Seq, res.LastAccept.Seq)
	require.Equal(t, a.Receipt, res.LastAccept.Receipt)
}

func TestStandbyTimeoutRefusesSend(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress

	// A standby host that takes the connection and never answers.
	l := listen(t)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()

	timeout := 300 * time.Millisecond
	p := newPrimary(t, l.Addr().String(), timeout, sender)
	_, bz := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	open := p.open()
	a := p.accept(bz)
	start := time.Now()
	ack, err := p.repl.Release(context.Background(), open, a)
	elapsed := time.Since(start)
	require.ErrorIs(t, err, sequencer.ErrNoAck)
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout())
	require.Equal(t, sequencer.Ack{}, ack)
	require.GreaterOrEqual(t, elapsed, timeout)
	require.Less(t, elapsed, timeout+5*time.Second)

	_, err = sequencer.NewReplicator(sequencer.ReplicatorConfig{WAL: p.wal, Dial: func(context.Context) (net.Conn, error) { return nil, nil }})
	require.EqualError(t, err, "sequencer: replication timeout 0s must be positive")
}

func TestStandbyAppliesInSeqOrder(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	l := listen(t)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	p := newPrimary(t, addr, replicationTimeout, sender)
	_, first := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	_, second := transfer(t, sender, 1, recipient, big.NewInt(2*onePaxInWei))
	open := p.open()

	// The standby is down: OPEN and seq 42 are on the primary disk but unacknowledged.
	_, err := p.repl.Release(context.Background(), open, p.accept(first))
	require.ErrorIs(t, err, sequencer.ErrNoAck)

	// The standby comes up and is sent seq 43 alone: it requests seq 42 before acknowledging,
	// then takes OPEN, 42 and 43 in order.
	sb := newStandby(t, p, sender)
	l, err = net.Listen("tcp", addr)
	require.NoError(t, err)
	sb.serve(t, l)
	ack, err := p.repl.Release(context.Background(), p.accept(second))
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: startSeq + 2, GapFrom: startSeq + 1, GapTo: startSeq + 1}, ack)
	require.Equal(t, startSeq+2, sb.s.LastSeq())
	requireStandbyState(t, p, sb, sender.EvmAddress, recipient)
	require.Equal(t, uint64(2), sb.h.app.EvmKeeper.GetNonce(sb.h.openCtx(), sender.EvmAddress))
}

func TestStandbyRefusesSharedKeys(t *testing.T) {
	p := newPrimary(t, "127.0.0.1:1", replicationTimeout)
	h := newHarness(t)
	w, err := wal.OpenWAL(filepath.Join(t.TempDir(), "standby-wal"), wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(w.Close)
	primaryPub, err := p.pv.GetPubKey(context.Background())
	require.NoError(t, err)
	keyDir := t.TempDir()
	ownPV, err := privval.GenFilePV(filepath.Join(keyDir, "key.json"), filepath.Join(keyDir, "state.json"), "")
	require.NoError(t, err)
	_, ownReceipt, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	cfg := sequencer.StandbyConfig{
		WAL:                 w,
		Executor:            appReplayer{exec: h.exec},
		ConsensusKey:        p.pv,
		ReceiptKey:          ownReceipt,
		PrimaryConsensusKey: primaryPub,
		PrimaryReceiptKey:   p.receipt,
		CommittedHeight:     uint64(committedTip),
		LastSeq:             startSeq,
	}
	_, err = sequencer.NewStandby(context.Background(), cfg)
	require.ErrorIs(t, err, sequencer.ErrSharedKey)

	_, primaryReceiptKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	cfg.ConsensusKey = ownPV
	cfg.ReceiptKey = primaryReceiptKey
	cfg.PrimaryReceiptKey = primaryReceiptKey.Public().(ed25519.PublicKey)
	_, err = sequencer.NewStandby(context.Background(), cfg)
	require.ErrorIs(t, err, sequencer.ErrSharedKey)
}

// replicatorResendDialer dials the standby at addr, records every group written on its links,
// and can cut the current link and refuse new ones.
type replicatorResendDialer struct {
	mu     sync.Mutex
	addr   string
	down   bool
	conn   net.Conn
	writes [][]byte
}

func (d *replicatorResendDialer) dial(ctx context.Context) (net.Conn, error) {
	d.mu.Lock()
	addr, down := d.addr, d.down
	d.mu.Unlock()
	if down {
		return nil, errors.New("standby link down")
	}
	var nd net.Dialer
	conn, err := nd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()
	return replicatorResendConn{Conn: conn, d: d}, nil
}

// cut closes the current link and refuses new ones until restore.
func (d *replicatorResendDialer) cut() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.down = true
	if d.conn != nil {
		_ = d.conn.Close()
	}
}

func (d *replicatorResendDialer) restore(addr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.down, d.addr = false, addr
}

// groups returns the record kinds of every group written so far, in write order.
func (d *replicatorResendDialer) groups(t *testing.T) [][]wal.EntryKind {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]wal.EntryKind, len(d.writes))
	for i, b := range d.writes {
		require.GreaterOrEqual(t, len(b), 4)
		n := binary.BigEndian.Uint32(b[:4])
		off := 4
		for range n {
			require.GreaterOrEqual(t, len(b), off+8)
			end := off + 8 + int(binary.BigEndian.Uint32(b[off+4:off+8]))
			require.GreaterOrEqual(t, len(b), end)
			e, err := wal.DecodeEntry(b[off+8 : end])
			require.NoError(t, err)
			out[i] = append(out[i], e.Kind())
			off = end
		}
		require.Equal(t, len(b), off)
	}
	return out
}

type replicatorResendConn struct {
	net.Conn
	d *replicatorResendDialer
}

func (c replicatorResendConn) Write(b []byte) (int, error) {
	c.d.mu.Lock()
	c.d.writes = append(c.d.writes, append([]byte(nil), b...))
	c.d.mu.Unlock()
	return c.Conn.Write(b)
}

// replicatorResendRecorder records the OPEN, SEAL, COMMITTED and LEASE records it applies.
type replicatorResendRecorder struct {
	appReplayer
	applied *[]wal.Entry
}

func (r replicatorResendRecorder) Apply(e wal.Entry) error {
	*r.applied = append(*r.applied, e)
	return r.appReplayer.Apply(e)
}

func TestReplicatorResendsAfterLostCommitted(t *testing.T) {
	ctx := context.Background()
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	l := listen(t)
	p := newPrimary(t, l.Addr().String(), replicationTimeout, sender)
	d := &replicatorResendDialer{addr: l.Addr().String()}
	repl, err := sequencer.NewReplicator(sequencer.ReplicatorConfig{WAL: p.wal, Dial: d.dial, ReplicationTimeout: replicationTimeout})
	require.NoError(t, err)
	t.Cleanup(repl.Close)
	sb := newStandby(t, p, sender)
	sb.serve(t, l)

	// OPEN(2), ACCEPT(seq 42, height 2) and SEAL(2, 42) are acknowledged.
	_, bz := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	open := p.open()
	a := p.accept(bz)
	require.Equal(t, startSeq+1, a.Seq)
	require.Equal(t, uint64(openHeight), a.Height)
	batch, err := p.h.exec.Seal(openHeight)
	require.NoError(t, err)
	seal := &wal.Seal{Height: uint64(openHeight), FromSeq: batch.FromSeq, ToSeq: batch.LastSeq, LastSeqHash: batch.LastSeqHash}
	require.Equal(t, a.Seq, seal.ToSeq)
	ack, err := repl.Release(ctx, open, a, seal)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: a.Seq}, ack)

	// The link is cut: COMMITTED(2, 42) is durable on the primary and retained, but never reaches
	// the wire.
	d.cut()
	committed := &wal.Committed{Height: uint64(openHeight), LastSeq: a.Seq}
	ack, err = repl.Release(ctx, committed)
	require.ErrorIs(t, err, sequencer.ErrNoAck)
	require.Equal(t, sequencer.Ack{}, ack)
	require.Nil(t, sb.wal.LastCommitted())

	// The link is back: LEASE(1) has no committed-height prerequisite, yet the fresh link carries
	// the retained window first, so the standby takes COMMITTED(2) before the lease.
	d.restore(l.Addr().String())
	lease := &wal.Lease{Epoch: 1, ExpiryMs: uint64(p.openMs)}
	ack, err = repl.Release(ctx, lease)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: a.Seq, GapFrom: a.Seq, GapTo: a.Seq}, ack)
	require.Equal(t, committed, sb.wal.LastCommitted())
	require.Equal(t, a.Seq, sb.s.LastSeq())
	require.NoError(t, sb.s.Err())
	require.Equal(t, [][]wal.EntryKind{
		{wal.KindOpen, wal.KindAccept, wal.KindSeal},
		{wal.KindOpen, wal.KindAccept, wal.KindSeal, wal.KindCommitted, wal.KindLease},
	}, d.groups(t))

	// The standby recovers from its WAL on a host that committed height 2: replay starts after
	// COMMITTED(2) and applies only the lease, so the next block opens at height 3.
	repl.Close()
	require.NoError(t, l.Close())
	sb.wal.Close()
	rec := newHarness(t, sender)
	require.Equal(t, openHeight+1, commitBlock(rec, func(sdk.Context) {}))
	w, err := wal.OpenWAL(sb.dir, wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(w.Close)
	primaryPub, err := p.pv.GetPubKey(ctx)
	require.NoError(t, err)
	var applied []wal.Entry
	recovered, err := sequencer.NewStandby(ctx, sequencer.StandbyConfig{
		WAL:                 w,
		Executor:            replicatorResendRecorder{appReplayer: appReplayer{exec: rec.exec}, applied: &applied},
		ConsensusKey:        sb.s.ConsensusKey(),
		ReceiptKey:          sb.s.ReceiptKey(),
		PrimaryConsensusKey: primaryPub,
		PrimaryReceiptKey:   p.receipt,
		CommittedHeight:     uint64(openHeight),
		LastSeq:             a.Seq,
	})
	require.NoError(t, err)
	require.Equal(t, []wal.Entry{lease}, applied)
	require.Equal(t, a.Seq, recovered.LastSeq())

	// The acknowledged LEASE pruned the window through COMMITTED(2): the next fresh link carries
	// only the lease ahead of OPEN(3), and the recovered standby opens height 3.
	l2 := listen(t)
	t.Cleanup(func() { _ = l2.Close() })
	go func() { _ = recovered.Serve(l2) }()
	d.restore(l2.Addr().String())
	open3 := &wal.Open{Height: uint64(openHeight + 1), OpenTimeMs: uint64(p.openMs + 1000), BaseFee: uint256.NewInt(0)}
	ack, err = repl.Release(ctx, open3)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: a.Seq}, ack)
	require.NoError(t, recovered.Err())
	require.Equal(t, []wal.EntryKind{wal.KindLease, wal.KindOpen}, d.groups(t)[2])
	openCtx, err := rec.exec.Snapshot()
	require.NoError(t, err)
	require.Equal(t, openHeight+1, openCtx.BlockHeight())
}
