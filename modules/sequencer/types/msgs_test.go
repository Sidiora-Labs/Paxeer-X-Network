package types_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	"github.com/stretchr/testify/require"
)

const testChainID = "sequencer-test-1"

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	out, err := hex.DecodeString(value)
	require.NoError(t, err)
	return out
}

func receiptKey() (ed25519.PrivateKey, [32]byte) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x07}, ed25519.SeedSize))
	var public [32]byte
	copy(public[:], private.Public().(ed25519.PublicKey))
	return private, public
}

func signedBatch(t *testing.T, private ed25519.PrivateKey, batch types.MsgSequenceBatch) types.MsgSequenceBatch {
	t.Helper()
	digest, err := batch.SignBytes(testChainID)
	require.NoError(t, err)
	batch.Sig = ed25519.Sign(private, digest[:])
	return batch
}

func batch31To42() types.MsgSequenceBatch {
	return types.MsgSequenceBatch{
		Height:      1001,
		FromSeq:     31,
		ToSeq:       42,
		PrevSeqHash: bytes.Repeat([]byte{0x11}, types.HashLength),
		LastSeqHash: bytes.Repeat([]byte{0x22}, types.HashLength),
		TxRoot:      bytes.Repeat([]byte{0x33}, types.HashLength),
		KeyId:       bytes.Repeat([]byte{0x44}, types.KeyIDLength),
	}
}

func TestMsgSequenceBatchSignature(t *testing.T) {
	private, public := receiptKey()
	require.Equal(t, mustHex(t, "ea4a6c63e29c520abef5507b132ec5f9954776aebebe7b92421eea691446d22c"), public[:])

	batch := signedBatch(t, private, batch31To42())
	require.NoError(t, batch.ValidateBasic())

	digest, err := batch.SignBytes(testChainID)
	require.NoError(t, err)
	require.Equal(t, mustHex(t, "ab4b5265f05e5759468a3454c5e697c480d9d0833d0691edd0cc9722a09c12a9"), digest[:])
	require.Equal(t, mustHex(t, "74e4f0f707e8063a3a3ef68097ef3677f6fcee8c7c17f4219e1fda61cd7e09f6f84c1fb7977121b4fe22cd87c077817adf8391df6b25b65d6e63023695dd2604"), batch.Sig)
	require.NoError(t, batch.VerifySignature(testChainID, public))

	otherRoot := batch
	otherRoot.TxRoot = bytes.Repeat([]byte{0x34}, types.HashLength)
	require.ErrorIs(t, otherRoot.VerifySignature(testChainID, public), types.ErrBadSignature)

	require.ErrorIs(t, batch.VerifySignature("sequencer-test-2", public), types.ErrBadSignature)

	var otherPublic [32]byte
	copy(otherPublic[:], ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x08}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	require.ErrorIs(t, batch.VerifySignature(testChainID, otherPublic), types.ErrBadSignature)

	var identity [32]byte
	identity[0] = 0x01
	require.ErrorIs(t, batch.VerifySignature(testChainID, identity), types.ErrBadSignature)

	_, err = batch.SignBytes("")
	require.ErrorIs(t, err, types.ErrInvalidSequenceBatch)

	for name, mutate := range map[string]func(*types.MsgSequenceBatch){
		"height zero":       func(m *types.MsgSequenceBatch) { m.Height = 0 },
		"fromSeq zero":      func(m *types.MsgSequenceBatch) { m.FromSeq = 0; m.ToSeq = 0 },
		"toSeq below range": func(m *types.MsgSequenceBatch) { m.ToSeq = 29 },
		"short prevSeqHash": func(m *types.MsgSequenceBatch) { m.PrevSeqHash = m.PrevSeqHash[:31] },
		"long lastSeqHash":  func(m *types.MsgSequenceBatch) { m.LastSeqHash = append(m.LastSeqHash, 0) },
		"missing txRoot":    func(m *types.MsgSequenceBatch) { m.TxRoot = nil },
		"short keyId":       func(m *types.MsgSequenceBatch) { m.KeyId = m.KeyId[:16] },
		"short sig":         func(m *types.MsgSequenceBatch) { m.Sig = m.Sig[:63] },
	} {
		mutated := batch
		mutated.PrevSeqHash = bytes.Clone(batch.PrevSeqHash)
		mutated.LastSeqHash = bytes.Clone(batch.LastSeqHash)
		mutated.Sig = bytes.Clone(batch.Sig)
		mutate(&mutated)
		require.ErrorIs(t, mutated.ValidateBasic(), types.ErrInvalidSequenceBatch, name)
		require.ErrorIs(t, mutated.VerifySignature(testChainID, public), types.ErrInvalidSequenceBatch, name)
	}
}

func TestMsgSequenceBatchEmptyBlock(t *testing.T) {
	private, public := receiptKey()
	require.Equal(t, mustHex(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"), types.EmptyTxRoot[:])

	empty := types.MsgSequenceBatch{
		Height:      1002,
		FromSeq:     43,
		ToSeq:       42,
		PrevSeqHash: bytes.Repeat([]byte{0x22}, types.HashLength),
		LastSeqHash: bytes.Repeat([]byte{0x22}, types.HashLength),
		TxRoot:      types.EmptyTxRoot[:],
		KeyId:       bytes.Repeat([]byte{0x44}, types.KeyIDLength),
	}
	empty = signedBatch(t, private, empty)
	require.True(t, empty.IsEmpty())
	require.Equal(t, empty.FromSeq-1, empty.ToSeq)
	require.NoError(t, empty.ValidateBasic())
	require.NoError(t, empty.VerifySignature(testChainID, public))

	first := empty
	first.FromSeq, first.ToSeq = 1, 0
	first.PrevSeqHash = make([]byte, types.HashLength)
	first.LastSeqHash = make([]byte, types.HashLength)
	require.True(t, first.IsEmpty())
	require.NoError(t, first.ValidateBasic())

	gap := empty
	gap.ToSeq = 41
	require.False(t, gap.IsEmpty())
	require.ErrorIs(t, gap.ValidateBasic(), types.ErrInvalidSequenceBatch)

	wrongRoot := empty
	wrongRoot.TxRoot = bytes.Repeat([]byte{0x33}, types.HashLength)
	require.ErrorIs(t, wrongRoot.ValidateBasic(), types.ErrInvalidSequenceBatch)

	movedHash := empty
	movedHash.LastSeqHash = bytes.Repeat([]byte{0x23}, types.HashLength)
	require.ErrorIs(t, movedHash.ValidateBasic(), types.ErrInvalidSequenceBatch)

	single := empty
	single.ToSeq = 43
	single.LastSeqHash = bytes.Repeat([]byte{0x24}, types.HashLength)
	single.TxRoot = bytes.Repeat([]byte{0x33}, types.HashLength)
	require.False(t, single.IsEmpty())
	require.NoError(t, single.ValidateBasic())
}
