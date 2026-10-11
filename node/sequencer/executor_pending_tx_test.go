package sequencer_test

import (
	"math/big"
	"testing"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

func executorPendingTxRequire(t *testing.T, exec sequencer.Executor, hash [32]byte, txBytes []byte, accepted *sequencer.Accepted) {
	t.Helper()
	got, ok := exec.PendingTx(hash)
	require.True(t, ok)
	require.Equal(t, txBytes, got.TxBytes)
	require.Equal(t, openHeight, got.Height)
	require.Equal(t, accepted.TxIndex, got.TxIndex)
	require.Same(t, accepted.Result, got.Result)
}

func TestExecutorPendingTxByHash(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)

	beforeOpen, _ := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	_, ok := h.exec.PendingTx(beforeOpen.Hash())
	require.False(t, ok)

	h.open()
	first, firstBytes := transfer(t, sender, 0, recipient, big.NewInt(onePaxInWei))
	firstAccepted, err := h.exec.Accept(firstBytes)
	require.NoError(t, err)
	require.Equal(t, 1, firstAccepted.TxIndex)
	second, secondBytes := transfer(t, sender, 1, recipient, big.NewInt(onePaxInWei))
	secondAccepted, err := h.exec.Accept(secondBytes)
	require.NoError(t, err)
	require.Equal(t, 2, secondAccepted.TxIndex)
	require.Equal(t, abci.CodeTypeOK, secondAccepted.Result.Code)

	executorPendingTxRequire(t, h.exec, first.Hash(), firstBytes, firstAccepted)
	executorPendingTxRequire(t, h.exec, second.Hash(), secondBytes, secondAccepted)

	unknown, _ := transfer(t, sender, 2, recipient, big.NewInt(onePaxInWei))
	got, ok := h.exec.PendingTx(unknown.Hash())
	require.False(t, ok)
	require.Equal(t, sequencer.PendingTx{}, got)

	_, err = h.exec.Seal(openHeight)
	require.NoError(t, err)
	got, ok = h.exec.PendingTx(first.Hash())
	require.False(t, ok)
	require.Equal(t, sequencer.PendingTx{}, got)
}
