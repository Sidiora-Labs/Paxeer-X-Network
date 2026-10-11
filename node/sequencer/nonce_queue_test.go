package sequencer_test

import (
	"cmp"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
)

var (
	senderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	senderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
)

type accepted struct {
	sender common.Address
	nonce  uint64
	tx     string
}

// openState executes at receipt on the real App: Accept bumps the sender nonce through
// the EVM keeper, the effect the deliver-mode ante has, and the queue reads next back
// through App.EvmNonce.
type openState struct {
	t      *testing.T
	app    *app.App
	order  []accepted
	refuse map[uint64]error
}

func newOpenState(t *testing.T) *openState {
	tw := app.NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	return &openState{t: t, app: tw.App, refuse: map[uint64]error{}}
}

func (s *openState) setNext(sender common.Address, nonce uint64) {
	s.app.EvmKeeper.SetNonce(s.app.GetCheckCtx(), sender, nonce)
	require.Equal(s.t, nonce, s.app.EvmNonce(sender))
}

func (s *openState) accept(sender common.Address, nonce uint64, tx []byte) error {
	if err := s.refuse[nonce]; err != nil {
		return err
	}
	s.app.EvmKeeper.SetNonce(s.app.GetCheckCtx(), sender, nonce+1)
	s.order = append(s.order, accepted{sender, nonce, string(tx)})
	return nil
}

func (s *openState) queue(cfg sequencer.NonceQueueConfig) *sequencer.NonceQueue {
	q, err := sequencer.NewNonceQueue(cfg, s.app, s.accept)
	require.NoError(s.t, err)
	return q
}

var defaultCfg = sequencer.NonceQueueConfig{PerSender: 16, Total: 64, Lifetime: time.Minute}

func TestNonceQueueExecutableNow(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 7)
	q := s.queue(defaultCfg)

	held, err := q.Hold(senderA, 7, []byte("a7"))
	require.NoError(t, err)
	require.False(t, held)
	require.Equal(t, []accepted{{senderA, 7, "a7"}}, s.order)
	require.Equal(t, uint64(8), s.app.EvmNonce(senderA))
	require.Equal(t, 0, q.Len())

	_, err = sequencer.NewNonceQueue(sequencer.NonceQueueConfig{PerSender: 0, Total: 1, Lifetime: time.Second}, s.app, s.accept)
	require.EqualError(t, err, "nonce queue: per-sender bound 0 must be positive")
	_, err = sequencer.NewNonceQueue(sequencer.NonceQueueConfig{PerSender: 4, Total: 3, Lifetime: time.Second}, s.app, s.accept)
	require.EqualError(t, err, "nonce queue: total bound 3 below per-sender bound 4")
	_, err = sequencer.NewNonceQueue(sequencer.NonceQueueConfig{PerSender: 4, Total: 4}, s.app, s.accept)
	require.EqualError(t, err, "nonce queue: lifetime 0s must be positive")
	_, err = sequencer.NewNonceQueue(defaultCfg, nil, s.accept)
	require.EqualError(t, err, "nonce queue: nil nonce reader")
	_, err = sequencer.NewNonceQueue(defaultCfg, s.app, nil)
	require.EqualError(t, err, "nonce queue: nil accept")
}

func TestNonceQueueDrainsOnGapFill(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 7)
	q := s.queue(defaultCfg)

	held, err := q.Hold(senderA, 9, []byte("a9"))
	require.NoError(t, err)
	require.True(t, held)
	held, err = q.Hold(senderA, 8, []byte("a8"))
	require.NoError(t, err)
	require.True(t, held)
	require.Empty(t, s.order)
	require.True(t, q.Held(senderA, 8))
	require.True(t, q.Held(senderA, 9))
	require.Equal(t, 2, q.Len())

	held, err = q.Hold(senderA, 7, []byte("a7"))
	require.NoError(t, err)
	require.False(t, held)
	require.Equal(t, []accepted{{senderA, 7, "a7"}, {senderA, 8, "a8"}, {senderA, 9, "a9"}}, s.order)
	require.Equal(t, uint64(10), s.app.EvmNonce(senderA))
	require.Equal(t, 0, q.Len())

	// A held transaction Accept refuses is dropped with no receipt and the drain stops at its gap.
	refused := errors.New("insufficient funds")
	s.refuse[11] = refused
	for _, n := range []uint64{12, 11} {
		held, err = q.Hold(senderA, n, []byte{byte(n)})
		require.NoError(t, err)
		require.True(t, held)
	}
	_, err = q.Hold(senderA, 10, []byte("a10"))
	require.NoError(t, err)
	require.Equal(t, accepted{senderA, 10, "a10"}, s.order[len(s.order)-1])
	require.Len(t, s.order, 4)
	require.Equal(t, uint64(11), s.app.EvmNonce(senderA))
	require.False(t, q.Held(senderA, 11))
	require.True(t, q.Held(senderA, 12))
	require.Equal(t, 1, q.Len())

	// An executable transaction Accept refuses returns that refusal and is not receipted.
	_, err = q.Hold(senderA, 11, []byte("a11"))
	require.ErrorIs(t, err, refused)
	delete(s.refuse, 11)

	// A nonce filled outside Hold drains on Drain.
	s.setNext(senderA, 12)
	q.Drain(senderA)
	require.Equal(t, accepted{senderA, 12, string([]byte{12})}, s.order[len(s.order)-1])
	require.Equal(t, uint64(13), s.app.EvmNonce(senderA))
	require.Equal(t, 0, q.Len())
}

func TestNonceQueueTooLowRefused(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 7)
	q := s.queue(defaultCfg)

	held, err := q.Hold(senderA, 5, []byte("a5"))
	require.ErrorIs(t, err, sequencer.ErrNonceTooLow)
	require.Contains(t, err.Error(), "nonce too low")
	require.False(t, held)
	require.Empty(t, s.order)
	require.Equal(t, uint64(7), s.app.EvmNonce(senderA))
	require.Equal(t, 0, q.Len())
}

func TestNonceQueueBoundAndExpiry(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 8)
	s.setNext(senderB, 1)
	q := s.queue(sequencer.NonceQueueConfig{PerSender: 4, Total: 5, Lifetime: time.Minute})

	for n := uint64(9); n <= 12; n++ {
		held, err := q.Hold(senderA, n, []byte{byte(n)})
		require.NoError(t, err)
		require.True(t, held)
	}
	held, err := q.Hold(senderA, 13, []byte{13})
	require.ErrorIs(t, err, sequencer.ErrSenderQueueFull)
	require.False(t, held)
	require.False(t, q.Held(senderA, 13))

	held, err = q.Hold(senderB, 3, []byte("b3"))
	require.NoError(t, err)
	require.True(t, held)
	_, err = q.Hold(senderB, 4, []byte("b4"))
	require.ErrorIs(t, err, sequencer.ErrQueueFull)
	require.Equal(t, 5, q.Len())

	require.Empty(t, q.Expire(time.Now()))
	require.Equal(t, 5, q.Len())

	// B's nonce moves past its held entry outside the queue: it is dropped as stale.
	s.setNext(senderB, 4)
	dropped := q.Expire(time.Now())
	require.Len(t, dropped, 1)
	require.Equal(t, senderB, dropped[0].Sender)
	require.Equal(t, uint64(3), dropped[0].Nonce)
	require.Equal(t, []byte("b3"), dropped[0].Tx)

	dropped = q.Expire(time.Now().Add(time.Minute + time.Second))
	slices.SortFunc(dropped, func(a, b sequencer.HeldTx) int { return cmp.Compare(a.Nonce, b.Nonce) })
	require.Len(t, dropped, 4)
	for i, h := range dropped {
		n := uint64(9 + i)
		require.Equal(t, senderA, h.Sender)
		require.Equal(t, n, h.Nonce)
		require.Equal(t, []byte{byte(n)}, h.Tx)
		require.False(t, q.Held(senderA, n))
	}
	require.Equal(t, 0, q.Len())
	require.Empty(t, s.order)

	// Expired entries leave no trace: the nonces are free again and nothing was receipted.
	held, err = q.Hold(senderA, 9, []byte("a9-again"))
	require.NoError(t, err)
	require.True(t, held)
	_, err = q.Hold(senderA, 8, []byte("a8"))
	require.NoError(t, err)
	require.Equal(t, []accepted{{senderA, 8, "a8"}, {senderA, 9, "a9-again"}}, s.order)
}

func TestNonceQueueArrivalOrderAcrossSenders(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 7)
	s.setNext(senderB, 3)
	q := s.queue(defaultCfg)

	for _, a := range []accepted{{senderA, 7, "a7"}, {senderB, 3, "b3"}, {senderA, 8, "a8"}, {senderB, 4, "b4"}} {
		held, err := q.Hold(a.sender, a.nonce, []byte(a.tx))
		require.NoError(t, err)
		require.False(t, held)
	}
	require.Equal(t, []accepted{{senderA, 7, "a7"}, {senderB, 3, "b3"}, {senderA, 8, "a8"}, {senderB, 4, "b4"}}, s.order)

	// A held sender drains in its own slot without reordering the other sender's arrivals.
	_, err := q.Hold(senderA, 10, []byte("a10"))
	require.NoError(t, err)
	_, err = q.Hold(senderB, 5, []byte("b5"))
	require.NoError(t, err)
	_, err = q.Hold(senderA, 9, []byte("a9"))
	require.NoError(t, err)
	_, err = q.Hold(senderB, 6, []byte("b6"))
	require.NoError(t, err)
	require.Equal(t, []accepted{{senderB, 5, "b5"}, {senderA, 9, "a9"}, {senderA, 10, "a10"}, {senderB, 6, "b6"}}, s.order[4:])
}

func TestNonceQueueSameNonceRefused(t *testing.T) {
	s := newOpenState(t)
	s.setNext(senderA, 7)
	q := s.queue(defaultCfg)

	_, err := q.Hold(senderA, 7, []byte("a7"))
	require.NoError(t, err)
	held, err := q.Hold(senderA, 7, []byte("a7-higher-fee"))
	require.ErrorIs(t, err, sequencer.ErrSameNonce)
	require.False(t, held)
	require.Equal(t, []accepted{{senderA, 7, "a7"}}, s.order)
	require.Equal(t, uint64(8), s.app.EvmNonce(senderA))

	held, err = q.Hold(senderA, 9, []byte("a9"))
	require.NoError(t, err)
	require.True(t, held)
	held, err = q.Hold(senderA, 9, []byte("a9-higher-fee"))
	require.ErrorIs(t, err, sequencer.ErrSameNonce)
	require.False(t, held)
	_, err = q.Hold(senderA, 8, []byte("a8"))
	require.NoError(t, err)
	require.Equal(t, []accepted{{senderA, 7, "a7"}, {senderA, 8, "a8"}, {senderA, 9, "a9"}}, s.order)

	// Once the open layer is sealed the receipted nonces are committed state and refused as too low.
	q.Sealed()
	_, err = q.Hold(senderA, 7, []byte("a7-again"))
	require.ErrorIs(t, err, sequencer.ErrNonceTooLow)
	require.NotErrorIs(t, err, sequencer.ErrSameNonce)
	require.Len(t, s.order, 3)
}
