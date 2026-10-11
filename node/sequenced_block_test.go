package app_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"math"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	sequencermodule "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer"
	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const sequencedChainID = "pax-test"

var (
	sequencerKeySeed = bytes.Repeat([]byte{0x11}, ed25519.SeedSize)
	sequencerKeyID   = bytes.Repeat([]byte{0x44}, seqtypes.KeyIDLength)
	sequencerLeader  = bytes.Repeat([]byte{0x55}, 32)
	zeroSeqHash      = make([]byte, seqtypes.HashLength)
)

func sequencerPublicKey() []byte {
	return ed25519.NewKeyFromSeed(sequencerKeySeed).Public().(ed25519.PublicKey)
}

// activationGenesis is the sequencer genesis with one leader and one receipt key pinned from
// height 1 on.
func activationGenesis(t *testing.T) json.RawMessage {
	t.Helper()
	bz, err := json.Marshal(map[string]any{
		"leaders": [][]byte{sequencerLeader},
		"receipt_keys": seqkeeper.ReceiptKeys{{
			KeyId:       sequencerKeyID,
			PublicKey:   sequencerPublicKey(),
			FirstHeight: 1,
			LastHeight:  math.MaxUint64,
		}},
	})
	require.NoError(t, err)
	return bz
}

func newSequencerWrapper(t *testing.T, tm time.Time, valPub cryptotypes.PubKey, occ bool) *app.TestWrapper {
	t.Helper()
	return app.NewTestWrapper(t, tm, valPub, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(occ)
		ba.SetConcurrencyWorkers(4)
	})
}

// initSequencerGenesis runs the App InitChainer with only the sequencer genesis entry.
func initSequencerGenesis(t *testing.T, w *app.TestWrapper, genesis json.RawMessage) {
	t.Helper()
	appState, err := json.Marshal(map[string]json.RawMessage{seqtypes.ModuleName: genesis})
	require.NoError(t, err)
	w.App.InitChainer(w.Ctx, abci.RequestInitChain{AppStateBytes: appState})
}

func activate(t *testing.T, w *app.TestWrapper) {
	t.Helper()
	initSequencerGenesis(t, w, activationGenesis(t))
	require.NoError(t, w.App.SetActivationHeight(1))
	require.True(t, w.App.Sequenced(w.Ctx.BlockHeight()))
}

// evmTransfer funds from and returns a signed one-wei EVM transfer to to and its hash.
func evmTransfer(t *testing.T, w *app.TestWrapper, from utils.TestAcct, to common.Address) ([]byte, common.Hash) {
	t.Helper()
	w.FundAcc(from.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1_000_000_000_000_000_000))))
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		GasFeeCap: new(big.Int).SetUint64(100_000_000_000),
		GasTipCap: new(big.Int).SetUint64(100_000_000_000),
		Gas:       21000,
		ChainID:   big.NewInt(config.DefaultChainID),
		To:        &to,
		Value:     big.NewInt(1),
	}), from.EvmSigner, from.EvmPrivateKey)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(10_000_000_000)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz, signed.Hash()
}

// txRootOf is sha256 over the activity ids of the sequenced EVM transactions in order.
func txRootOf(hashes ...common.Hash) []byte {
	root := sha256.New()
	for _, h := range hashes {
		root.Write(h[:])
	}
	return root.Sum(nil)
}

// signedBatch encodes tx 0 for height 1 signed by the test receipt key.
func signedBatch(t *testing.T, fromSeq, toSeq uint64, prev, last, txRoot, keyID []byte) []byte {
	t.Helper()
	m := seqtypes.MsgSequenceBatch{
		Height:      1,
		FromSeq:     fromSeq,
		ToSeq:       toSeq,
		PrevSeqHash: prev,
		LastSeqHash: last,
		TxRoot:      txRoot,
		KeyId:       keyID,
	}
	digest, err := m.SignBytes(sequencedChainID)
	require.NoError(t, err)
	m.Sig = ed25519.Sign(ed25519.NewKeyFromSeed(sequencerKeySeed), digest[:])
	bz, err := app.EncodeSequenceBatch(m)
	require.NoError(t, err)
	return bz
}

func processBlock(w *app.TestWrapper, txs [][]byte) ([]*abci.ExecTxResult, error) {
	_, results, _, err := w.App.ProcessBlock(w.Ctx, txs, &app.BlockProcessRequest{Height: w.Ctx.BlockHeight()}, abci.CommitInfo{}, false, nil)
	return results, err
}

func requireSequencerState(t *testing.T, w *app.TestWrapper, lastSeq uint64, lastSeqHash []byte) {
	t.Helper()
	gotSeq, err := w.App.SequencerKeeper.LastSeq(w.Ctx)
	require.NoError(t, err)
	require.Equal(t, lastSeq, gotSeq)
	gotHash, err := w.App.SequencerKeeper.LastSeqHash(w.Ctx)
	require.NoError(t, err)
	require.Equal(t, lastSeqHash, gotHash[:])
}

func requireReceiptIndex(t *testing.T, w *app.TestWrapper, hash common.Hash, index uint64) {
	t.Helper()
	receipt, err := w.App.EvmKeeper.GetTransientReceipt(w.Ctx, hash, index)
	require.NoError(t, err)
	require.Equal(t, uint32(index), receipt.TransactionIndex)
}

func TestSequencedBlockDeliversBatchAtTxZero(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	alice, bob := utils.NewSigner(), utils.NewSigner()
	toAlice, toBob := utils.NewSigner().EvmAddress, utils.NewSigner().EvmAddress

	t.Run("sequenced", func(t *testing.T) {
		w := newSequencerWrapper(t, tm, valPub, false)
		activate(t, w)
		tx1, hash1 := evmTransfer(t, w, alice, toAlice)
		tx2, hash2 := evmTransfer(t, w, bob, toBob)
		last := bytes.Repeat([]byte{0x77}, seqtypes.HashLength)
		batch := signedBatch(t, 1, 2, zeroSeqHash, last, txRootOf(hash1, hash2), sequencerKeyID)

		results, err := processBlock(w, [][]byte{batch, tx1, tx2})
		require.NoError(t, err)
		require.Len(t, results, 3)
		require.Equal(t, abci.ExecTxResult{}, *results[0])
		for i, res := range results[1:] {
			require.Equal(t, uint32(0), res.Code, "tx %d: %s", i+1, res.Log)
			require.Equal(t, int64(21000), res.GasUsed, "tx %d", i+1)
			require.NotNil(t, res.EvmTxInfo, "tx %d", i+1)
		}
		requireReceiptIndex(t, w, hash1, 1)
		requireReceiptIndex(t, w, hash2, 2)
		_, err = w.App.EvmKeeper.GetTransientReceipt(w.Ctx, hash1, 0)
		require.Error(t, err)
		requireSequencerState(t, w, 2, last)
	})

	t.Run("below activation", func(t *testing.T) {
		w := newSequencerWrapper(t, tm, valPub, false)
		initSequencerGenesis(t, w, activationGenesis(t))
		require.False(t, w.App.Sequenced(w.Ctx.BlockHeight()))
		tx1, hash1 := evmTransfer(t, w, alice, toAlice)
		tx2, hash2 := evmTransfer(t, w, bob, toBob)

		results, err := processBlock(w, [][]byte{tx1, tx2})
		require.NoError(t, err)
		require.Len(t, results, 2)
		for i, res := range results {
			require.Equal(t, uint32(0), res.Code, "tx %d: %s", i, res.Log)
			require.Equal(t, int64(21000), res.GasUsed, "tx %d", i)
		}
		requireReceiptIndex(t, w, hash1, 0)
		requireReceiptIndex(t, w, hash2, 1)
		requireSequencerState(t, w, 0, zeroSeqHash)
	})
}

func TestSequencedBlockRefusesBadBatch(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	alice := utils.NewSigner()
	to := utils.NewSigner().EvmAddress
	last := bytes.Repeat([]byte{0x77}, seqtypes.HashLength)

	for _, tc := range []struct {
		name  string
		batch func(t *testing.T, tx []byte, hash common.Hash) [][]byte
		want  error
	}{
		{
			name: "wrong prevSeqHash",
			batch: func(t *testing.T, tx []byte, hash common.Hash) [][]byte {
				prev := bytes.Repeat([]byte{0x01}, seqtypes.HashLength)
				return [][]byte{signedBatch(t, 1, 1, prev, last, txRootOf(hash), sequencerKeyID), tx}
			},
			want: seqkeeper.ErrSeqDiscontinuity,
		},
		{
			name: "fromSeq not after the stored lastSeq",
			batch: func(t *testing.T, tx []byte, hash common.Hash) [][]byte {
				return [][]byte{signedBatch(t, 2, 2, zeroSeqHash, last, txRootOf(hash), sequencerKeyID), tx}
			},
			want: seqkeeper.ErrSeqDiscontinuity,
		},
		{
			name: "unknown keyId",
			batch: func(t *testing.T, tx []byte, hash common.Hash) [][]byte {
				unknown := bytes.Repeat([]byte{0x66}, seqtypes.KeyIDLength)
				return [][]byte{signedBatch(t, 1, 1, zeroSeqHash, last, txRootOf(hash), unknown), tx}
			},
			want: seqkeeper.ErrUnknownReceiptKey,
		},
		{
			name: "tampered signature",
			batch: func(t *testing.T, tx []byte, hash common.Hash) [][]byte {
				bz := signedBatch(t, 1, 1, zeroSeqHash, last, txRootOf(hash), sequencerKeyID)
				bz[len(bz)-1] ^= 0x01
				return [][]byte{bz, tx}
			},
			want: seqtypes.ErrBadSignature,
		},
		{
			name: "tx 0 is not a batch",
			batch: func(t *testing.T, tx []byte, _ common.Hash) [][]byte {
				return [][]byte{tx}
			},
			want: seqtypes.ErrInvalidSequenceBatch,
		},
		{
			name: "no tx 0",
			batch: func(*testing.T, []byte, common.Hash) [][]byte {
				return nil
			},
			want: seqtypes.ErrInvalidSequenceBatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSequencerWrapper(t, tm, valPub, false)
			activate(t, w)
			tx, hash := evmTransfer(t, w, alice, to)

			results, err := processBlock(w, tc.batch(t, tx, hash))
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, results)
			requireSequencerState(t, w, 0, zeroSeqHash)
			_, err = w.App.EvmKeeper.GetTransientReceipt(w.Ctx, hash, 1)
			require.Error(t, err)
		})
	}
}

func TestSequencedBlockOCCEqualsSequential(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	senders := utils.NewTestAccounts(6)
	recipients := utils.NewTestAccounts(6)

	occ := newSequencerWrapper(t, tm, valPub, true)
	sequential := newSequencerWrapper(t, tm, valPub, false)
	require.True(t, occ.App.OccEnabled())
	require.False(t, sequential.App.OccEnabled())
	// InitChain stamps the epoch with the wall clock; both apps start from the same record.
	sequential.App.EpochKeeper.SetEpoch(sequential.Ctx, occ.App.EpochKeeper.GetEpoch(occ.Ctx))

	last := bytes.Repeat([]byte{0x77}, seqtypes.HashLength)
	run := func(w *app.TestWrapper) ([]*abci.ExecTxResult, []common.Hash, []byte) {
		activate(t, w)
		txs := [][]byte{nil}
		hashes := make([]common.Hash, 0, len(senders))
		for i, sender := range senders {
			tx, hash := evmTransfer(t, w, sender, recipients[i].EvmAddress)
			txs = append(txs, tx)
			hashes = append(hashes, hash)
		}
		txs[0] = signedBatch(t, 1, uint64(len(senders)), zeroSeqHash, last, txRootOf(hashes...), sequencerKeyID)
		results, err := processBlock(w, txs)
		require.NoError(t, err)
		require.Len(t, results, len(txs))
		require.Equal(t, abci.ExecTxResult{}, *results[0])
		for i, hash := range hashes {
			require.Equal(t, uint32(0), results[i+1].Code, "tx %d: %s", i+1, results[i+1].Log)
			requireReceiptIndex(t, w, hash, uint64(i+1))
		}
		requireSequencerState(t, w, uint64(len(senders)), last)
		w.App.SetDeliverStateToCommit()
		w.App.WriteState()
		return results, hashes, w.App.GetWorkingHash()
	}

	occResults, occHashes, occAppHash := run(occ)
	seqResults, seqHashes, seqAppHash := run(sequential)
	require.Equal(t, seqHashes, occHashes)
	require.Len(t, occResults, len(seqResults))
	for i := range seqResults {
		require.Equal(t, seqResults[i].Code, occResults[i].Code, "tx %d", i)
		require.Equal(t, seqResults[i].GasUsed, occResults[i].GasUsed, "tx %d", i)
		require.Equal(t, seqResults[i].GasWanted, occResults[i].GasWanted, "tx %d", i)
	}
	require.NotEmpty(t, seqAppHash)
	require.Equal(t, seqAppHash, occAppHash)
}

func TestSequencedBlockGenesisOneKeySet(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	cdc := app.MakeEncodingConfig().Marshaler
	w := newSequencerWrapper(t, tm, valPub, false)
	subspace := w.App.GetSubspace(seqtypes.ModuleName)

	// The default genesis pins no key and writes no activation state.
	require.JSONEq(t, `{"receipt_keys":[]}`, string(app.ModuleBasics.DefaultGenesis(cdc)[seqtypes.ModuleName]))
	require.Empty(t, seqkeeper.GetReceiptKeys(w.Ctx, subspace))
	storeKeys, err := w.App.SequencerKeeper.ReceiptKeys(w.Ctx)
	require.NoError(t, err)
	require.Empty(t, storeKeys)

	genesis := activationGenesis(t)
	require.NoError(t, sequencermodule.AppModuleBasic{}.ValidateGenesis(cdc, nil, genesis))
	noKeys, err := json.Marshal(map[string]any{"leaders": [][]byte{sequencerLeader}, "receipt_keys": seqkeeper.ReceiptKeys{}})
	require.NoError(t, err)
	require.ErrorIs(t, sequencermodule.AppModuleBasic{}.ValidateGenesis(cdc, nil, noKeys), seqtypes.ErrInvalidGenesis)
	activate(t, w)

	var want seqtypes.ReceiptKey
	copy(want.KeyId[:], sequencerKeyID)
	copy(want.PublicKey[:], sequencerPublicKey())
	want.FirstHeight, want.LastHeight = 1, math.MaxUint64
	storeKeys, err = w.App.SequencerKeeper.ReceiptKeys(w.Ctx)
	require.NoError(t, err)
	require.Equal(t, []seqtypes.ReceiptKey{want}, storeKeys)

	paramKeys := seqkeeper.GetReceiptKeys(w.Ctx, subspace)
	require.Len(t, paramKeys, 1)
	require.Equal(t, want.KeyId[:], paramKeys[0].KeyId)
	require.Equal(t, want.PublicKey[:], paramKeys[0].PublicKey)
	require.Equal(t, want.FirstHeight, paramKeys[0].FirstHeight)
	require.Equal(t, want.LastHeight, paramKeys[0].LastHeight)

	leaders, err := w.App.SequencerKeeper.Leaders(w.Ctx)
	require.NoError(t, err)
	require.Equal(t, [][32]byte{[32]byte(sequencerLeader)}, leaders)
	requireSequencerState(t, w, 0, zeroSeqHash)

	exported := sequencermodule.NewAppModule(w.App.SequencerKeeper, subspace).ExportGenesis(w.Ctx, cdc)
	require.JSONEq(t, string(genesis), string(exported))

	// An empty batch signed by the pinned key delivers against the same key set.
	batch := signedBatch(t, 1, 0, zeroSeqHash, zeroSeqHash, seqtypes.EmptyTxRoot[:], sequencerKeyID)
	results, err := processBlock(w, [][]byte{batch})
	require.NoError(t, err)
	require.Equal(t, []*abci.ExecTxResult{{}}, results)
	requireSequencerState(t, w, 0, zeroSeqHash)
}
