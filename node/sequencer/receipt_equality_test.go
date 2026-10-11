package sequencer_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmed25519 "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	receiptModuleID = uint16(12)
	createGas       = 200_000
	callGas         = 100_000
	sidFeeGas       = 30_000
	sidConverted    = 32_697
	sidFunding      = 1_000_000
	vectorHeight    = int64(26_400_000)
)

var (
	// logRuntime emits LOG2(0x2a word; 1, 2), LOG4(0x2a 00 00 00 00; 3, 4, 5, 6) and LOG0().
	logRuntime = mustHex("602a600052" + "6002600160206000a2" + "600660056004600360056" + "01fa4" + "60006000a0" + "00")
	logInit    = append(mustHex("6021600c60003960216000f3"), logRuntime...)
	// errorX is the ABI encoding of Error("x").
	errorX = mustHex("08c379a0" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"7800000000000000000000000000000000000000000000000000000000000000")
	revertInit    = append(mustHex("6064600c60003960646000fd"), errorX...)
	sponsorAddr   = common.BytesToAddress(repeat(0x5a, 20))
	sponsorToken  = common.BytesToAddress(repeat(0x7b, 20))
	sponsoredSig  = crypto.Keccak256Hash([]byte("Sponsored(address,address,uint256,uint256)"))
	sponsoredInit = mustHex("73" + hex.EncodeToString(sponsorToken[:]) + "73" + hex.EncodeToString(sponsorAddr[:]) +
		"7f" + hex.EncodeToString(sponsoredSig[:]) + "60406000a3" + "60006000f3")
	testKeySeed      = label("sequencer receipt test key")
	consensusKeySeed = label("sequencer receipt test consensus key")
	testKeyID        = [32]byte(label("sequencer receipt test key id"))
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func label(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func addr32(a common.Address) [32]byte {
	var w [32]byte
	copy(w[12:], a[:])
	return w
}

func receiptWord(b ...byte) [32]byte {
	var w [32]byte
	copy(w[32-len(b):], b)
	return w
}

// wireVector reads a shared receipt vector from the layerx-wire crate.
func wireVector(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "agent", "crates", "layerx-wire", "src", "receipt.rs"))
	require.NoError(t, err)
	m := regexp.MustCompile(`const ` + name + `: &str =\s*"([0-9a-f]+)"`).FindSubmatch(src)
	require.NotNil(t, m, name)
	return mustHex(string(m[1]))
}

func writeSeed(t *testing.T, dir, name string, seed []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), mode))
	require.NoError(t, os.Chmod(path, mode))
	return path
}

func writeConsensusKey(t *testing.T, dir string, seed []byte) (string, []byte) {
	t.Helper()
	priv, err := tmed25519.SecretKeyFromSecretBytes(ed25519.NewKeyFromSeed(seed))
	require.NoError(t, err)
	path := filepath.Join(dir, "priv_validator_key.json")
	pv := privval.NewFilePV(priv, path, filepath.Join(dir, "priv_validator_state.json"))
	require.NoError(t, pv.Key.Save())
	return path, pv.Key.PubKey.Bytes()
}

func publicOf(seed []byte) [32]byte {
	return [32]byte(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
}

func loadKey(t *testing.T, seed []byte, pins []verify.SequencerAuthorization, height int64) (*sequencer.ReceiptKey, error) {
	t.Helper()
	dir := t.TempDir()
	consensus, _ := writeConsensusKey(t, dir, consensusKeySeed)
	return sequencer.LoadReceiptKey(writeSeed(t, dir, "receipt_key", seed, 0o600), consensus, pins, height)
}

func vectorKey(t *testing.T) *sequencer.ReceiptKey {
	t.Helper()
	key, err := loadKey(t, label("paxeer receipt vector key seed"), []verify.SequencerAuthorization{{
		SequencerID:      [32]byte(repeat(0x33, 32)),
		PublicKey:        [32]byte(wireVector(t, "PINNED_RECEIPT_KEY")),
		FirstBatchNumber: 1,
		LastBatchNumber:  30_000_000,
	}}, vectorHeight)
	require.NoError(t, err)
	return key
}

func bigInt(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return v
}

// vectorInput holds the fields of the shared layerx-wire receipt vector.
func vectorInput() *sequencer.ReceiptInput {
	prev := [32]byte(label("paxeer receipt vector previous sequence"))
	activity := [32]byte(label("paxeer receipt vector transaction"))
	transfer := receiptWord(label("Transfer(address,address,uint256)")...)
	from := common.BytesToAddress(repeat(0xaa, 20))
	to := common.BytesToAddress(repeat(0xbb, 20))
	holder := addr32(from)
	return &sequencer.ReceiptInput{
		Accepted: &sequencer.Accepted{
			Seq:         7,
			PrevSeqHash: prev,
			SeqHash:     expectedSeqHash(prev, 7, activity),
			ActivityID:  activity,
			Height:      vectorHeight,
			TxIndex:     2,
			TimestampMs: 1_750_000_000_250,
		},
		EVM: &evmtypes.Receipt{
			Status:            1,
			GasUsed:           51234,
			CumulativeGasUsed: 73456,
			EffectiveGasPrice: 1_000_000_000,
			To:                to.Hex(),
			Logs: []*evmtypes.Log{
				{Address: common.BytesToAddress(repeat(0x11, 20)).Hex(), Topics: []string{common.Hash(transfer).Hex(), common.Hash(holder).Hex()}, Data: append(make([]byte, 31), 0x2a)},
				{Address: common.BytesToAddress(repeat(0x22, 20)).Hex()},
			},
		},
		Operation:         2,
		ReturnData:        append(make([]byte, 31), 1),
		FeeDenom:          "upax",
		FeeCharged:        bigInt("51234000000000"),
		Sponsor:           sponsorAddr,
		Amount:            bigInt("1000000000000000000"),
		From:              from,
		FromBalanceBefore: bigInt("5000000000000000000"),
		FromBalanceAfter:  bigInt("3999948766000000000"),
		FromSequence:      41,
		To:                to,
		ToBalanceBefore:   bigInt("2000000000000000000"),
		ToBalanceAfter:    bigInt("3000000000000000000"),
		ModuleID:          receiptModuleID,
	}
}

type receiptHarness struct {
	*harness
	key      *sequencer.ReceiptKey
	r        *sequencer.Receipter
	openMs   int64
	accepted int
}

// newReceiptHarness builds the App as newHarness does, runs setup on the
// genesis context before height 1 commits, continues the sequence chain from
// its activation value (seq 0, zero prevSeqHash), opens height 2 at openMs (or
// one second after genesis when zero) and signs with the test key pinned for
// heights 1..1000.
func newReceiptHarness(t *testing.T, openMs int64, setup func(ctx sdk.Context, a *app.App), funded ...utils.TestAcct) *receiptHarness {
	t.Helper()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, tm, valPub, false)
	a := tw.App
	for _, acct := range funded {
		a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(fundingUhpx))))
	}
	if setup != nil {
		setup(tw.Ctx, a)
	}
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: testChainID, Height: committedTip},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	proposer := valPub.Address().Bytes()
	exec, err := sequencer.NewExecutor(a, sequencer.Options{ChainID: testChainID, ProposerAddress: proposer})
	require.NoError(t, err)
	h := &harness{t: t, app: a, exec: exec, proposer: proposer, genesis: tm}
	if openMs == 0 {
		openMs = h.open()
	} else {
		require.NoError(t, h.exec.Open(openHeight, openMs))
	}
	key, err := loadKey(t, testKeySeed, []verify.SequencerAuthorization{{
		SequencerID: testKeyID, PublicKey: publicOf(testKeySeed), FirstBatchNumber: 1, LastBatchNumber: 1000,
	}}, openHeight)
	require.NoError(t, err)
	r, err := sequencer.NewReceipter(h.exec, h.app, &h.app.EvmKeeper, key, receiptModuleID)
	require.NoError(t, err)
	return &receiptHarness{harness: h, key: key, r: r, openMs: openMs}
}

func (rh *receiptHarness) accept(t *testing.T, bz []byte) (*sequencer.Accepted, *codec.Receipt, []byte) {
	t.Helper()
	acc, signed, err := rh.r.Accept(bz)
	require.NoError(t, err)
	rh.accepted++
	verified, err := verify.ReceiptSignature(signed, rh.key.PublicKey())
	require.NoError(t, err)
	return acc, verified.Receipt, signed
}

func (rh *receiptHarness) transient(t *testing.T, etx *ethtypes.Transaction, acc *sequencer.Accepted) *evmtypes.Receipt {
	t.Helper()
	r, err := rh.app.EvmKeeper.GetTransientReceipt(rh.openCtx(), etx.Hash(), uint64(acc.TxIndex))
	require.NoError(t, err)
	return r
}

func evmTx(t *testing.T, from utils.TestAcct, data ethtypes.TxData) (*ethtypes.Transaction, []byte) {
	t.Helper()
	signed, err := ethtypes.SignNewTx(from.EvmPrivateKey, ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), data)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(signed.Gas())
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return signed, bz
}

func receiptLegacyTx(nonce uint64, to *common.Address, value *big.Int, gas uint64, data []byte) *ethtypes.LegacyTx {
	return &ethtypes.LegacyTx{Nonce: nonce, GasPrice: big.NewInt(gasPriceWei), Gas: gas, To: to, Value: value, Data: data}
}

func dynamicTx(nonce uint64, to *common.Address, value *big.Int, gas uint64, data []byte) *ethtypes.DynamicFeeTx {
	return &ethtypes.DynamicFeeTx{
		ChainID: big.NewInt(config.DefaultChainID), Nonce: nonce, GasTipCap: big.NewInt(gasPriceWei),
		GasFeeCap: big.NewInt(gasPriceWei), Gas: gas, To: to, Value: value, Data: data,
	}
}

func summaryBody(gasUsed, cumulative, price uint64, txIndex uint32, contract, sponsor common.Address, vmError string, returnLen, logs uint32, denom string) []byte {
	b := binary.BigEndian.AppendUint64(nil, gasUsed)
	b = binary.BigEndian.AppendUint64(b, cumulative)
	b = binary.BigEndian.AppendUint64(b, price)
	b = binary.BigEndian.AppendUint32(b, txIndex)
	b = append(b, contract[:]...)
	b = append(b, sponsor[:]...)
	vm := sha256.Sum256([]byte(vmError))
	b = append(b, vm[:]...)
	b = binary.BigEndian.AppendUint32(b, returnLen)
	b = binary.BigEndian.AppendUint32(b, logs)
	b = append(b, uint8(len(denom)))
	return append(b, denom...)
}

func logBody(addr common.Address, data []byte, topics ...[32]byte) []byte {
	b := append([]byte(nil), addr[:]...)
	b = append(b, uint8(len(topics)))
	for _, tp := range topics {
		b = append(b, tp[:]...)
	}
	d := sha256.Sum256(data)
	return append(b, d[:]...)
}

func u128(v *big.Int) codec.U128 {
	var b [16]byte
	v.FillBytes(b[:])
	return codec.U128{Hi: binary.BigEndian.Uint64(b[:8]), Lo: binary.BigEndian.Uint64(b[8:])}
}

func TestReceiptHeaderAndOrder(t *testing.T) {
	signed, err := vectorKey(t).SignReceipt(vectorInput())
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(wireVector(t, "GO_SIGNED_RECEIPT")), hex.EncodeToString(signed))
	require.Equal(t, []byte{0x00, 0x02, 0x52, 0x01, 0x00, 0x02}, signed[:6])

	a, b := utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b)
	_, bz := transfer(t, a, 0, b.EvmAddress, big.NewInt(1))
	_, rec, raw := rh.accept(t, bz)
	require.Equal(t, []byte{0x00, 0x02, 0x52, 0x01, 0x00, 0x02}, raw[:6])
	require.Equal(t, uint16(2), rec.ProtocolVersion)
	require.Nil(t, rec.Supply)
	require.Nil(t, rec.ProgramOutcome)
	require.NotNil(t, rec.SequencerSignature)
	require.Equal(t, raw, rec.CanonicalBytes())
}

func TestReceiptActivityIdIsTxHash(t *testing.T) {
	a, b := utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b)
	etx, bz := transfer(t, a, 0, b.EvmAddress, big.NewInt(1))
	acc, rec, _ := rh.accept(t, bz)
	require.Equal(t, [32]byte(etx.Hash()), rec.ActivityID)
	require.Equal(t, acc.ActivityID, rec.ActivityID)

	cosmos := signCosmos(t, rh.openCtx(), rh.harness, a, 200_000,
		banktypes.NewMsgSend(a.AccountAddress, b.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(5)))))
	_, rec, _ = rh.accept(t, cosmos)
	require.Equal(t, sha256.Sum256(cosmos), rec.ActivityID)
	require.Equal(t, sequencer.CosmosOperation, rec.Operation)

	in := vectorInput()
	in.Accepted.ActivityID = [32]byte{}
	in.Accepted.SeqHash = expectedSeqHash(in.Accepted.PrevSeqHash, in.Accepted.Seq, [32]byte{})
	_, err := sequencer.BuildReceipt(in, testKeyID)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
}

func TestReceiptSeqHashChain(t *testing.T) {
	a, b := utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b)
	prev := [32]byte{}
	for i := uint64(0); i < 3; i++ {
		etx, bz := transfer(t, a, i, b.EvmAddress, big.NewInt(1))
		acc, rec, _ := rh.accept(t, bz)
		require.Equal(t, i+1, rec.GlobalSequence)
		require.Equal(t, prev, rec.PreviousStateRoot)
		want := expectedSeqHash(prev, i+1, [32]byte(etx.Hash()))
		require.Equal(t, want, rec.ResultingStateRoot)
		require.Equal(t, acc.SeqHash, rec.ResultingStateRoot)
		prev = want
	}

	in := vectorInput()
	in.Accepted.SeqHash[0] ^= 1
	_, err := sequencer.BuildReceipt(in, testKeyID)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
	in = vectorInput()
	in.Accepted.Seq = 0
	_, err = sequencer.BuildReceipt(in, testKeyID)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
}

func TestReceiptResultAndActivityRoot(t *testing.T) {
	a := utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a)
	_, bz := evmTx(t, a, receiptLegacyTx(0, nil, big.NewInt(0), createGas, logInit))
	_, rec, _ := rh.accept(t, bz)
	require.Equal(t, int32(0), rec.ResultCode)
	require.Equal(t, sha256.Sum256(logRuntime), rec.ActivityRoot)

	contract := crypto.CreateAddress(a.EvmAddress, 0)
	_, bz = evmTx(t, a, receiptLegacyTx(1, &contract, big.NewInt(0), callGas, nil))
	_, rec, _ = rh.accept(t, bz)
	require.Equal(t, int32(0), rec.ResultCode)
	require.Equal(t, sha256.Sum256(nil), rec.ActivityRoot)

	etx, bz := evmTx(t, a, receiptLegacyTx(2, nil, big.NewInt(0), createGas, revertInit))
	acc, rec, _ := rh.accept(t, bz)
	require.Equal(t, int32(1), rec.ResultCode)
	require.Equal(t, sha256.Sum256(errorX), rec.ActivityRoot)
	exec := rh.transient(t, etx, acc)
	require.Equal(t, uint32(0), exec.Status)
	require.NotEmpty(t, exec.VmError)
	vm := sha256.Sum256([]byte(exec.VmError))
	require.Equal(t, vm[:], rec.Effects[0].Body[68:100])
	require.Equal(t, uint32(len(errorX)), binary.BigEndian.Uint32(rec.Effects[0].Body[100:104]))
}

func TestReceiptFeeFields(t *testing.T) {
	a, b, sid := utils.NewSigner(), utils.NewSigner(), utils.NewSigner()
	denom := bridgetypes.SidioraDenom()
	rh := newReceiptHarness(t, 0, func(ctx sdk.Context, a *app.App) {
		params := a.EvmKeeper.GetParams(ctx)
		params.FeeTokenEnabled = true
		params.MaxFeeTokenRateAge = evmtypes.DefaultMaxFeeTokenRateAge
		params.AllowedFeeDenoms = []evmtypes.AllowedFeeDenom{{
			Denom: denom, Rate: sdk.NewDec(evmtypes.InitialSidioraBaseUnitsPerPax), RateUpdateHeight: committedTip,
		}}
		a.EvmKeeper.SetParams(ctx, params)
		coins := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(sidFunding)))
		require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", coins))
		require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", sid.AccountAddress, coins))
		require.NoError(t, a.EvmKeeper.SetAccountFeeDenom(ctx, sid.EvmAddress, denom))
	}, a, b, sid)

	value := big.NewInt(onePaxInWei)
	_, bz := transfer(t, a, 0, b.EvmAddress, value)
	_, rec, _ := rh.accept(t, bz)
	nativeFee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	require.Equal(t, u128(nativeFee), rec.FeeCharged)
	spent := new(big.Int).Sub(new(big.Int).SetBytes(sliceU128(rec.FromBalanceBefore)), new(big.Int).SetBytes(sliceU128(rec.FromBalanceAfter)))
	requireWei(t, new(big.Int).Add(nativeFee, value), spent)
	require.Equal(t, sha256.Sum256([]byte("uhpx")), rec.Asset)
	require.Equal(t, "uhpx", string(rec.Effects[0].Body[109:]))

	open := rh.openCtx()
	require.True(t, rh.app.EvmKeeper.GetFeeTokenEnabled(open))
	require.Equal(t, denom, rh.app.EvmKeeper.GetAccountFeeDenom(open, sid.EvmAddress))
	sidBefore := rh.app.BankKeeper.GetBalance(open, sid.AccountAddress, denom).Amount
	require.Equal(t, sdk.NewInt(sidFunding), sidBefore)
	_, bz = evmTx(t, sid, receiptLegacyTx(0, &b.EvmAddress, big.NewInt(0), sidFeeGas, nil))
	_, rec, _ = rh.accept(t, bz)
	sidAfter := rh.app.BankKeeper.GetBalance(rh.openCtx(), sid.AccountAddress, denom).Amount
	require.Equal(t, sdk.NewInt(sidConverted), sidBefore.Sub(sidAfter))
	require.Equal(t, u128(big.NewInt(sidConverted)), rec.FeeCharged)
	require.Equal(t, sha256.Sum256([]byte(denom)), rec.Asset)
	require.Equal(t, denom, string(rec.Effects[0].Body[109:]))
	require.Equal(t, uint8(len(denom)), rec.Effects[0].Body[108])

	cosmos := signCosmos(t, rh.openCtx(), rh.harness, a, 200_000,
		banktypes.NewMsgSend(a.AccountAddress, b.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(5)))))
	_, rec, _ = rh.accept(t, cosmos)
	require.Equal(t, u128(big.NewInt(cosmosFee)), rec.FeeCharged)
	require.Equal(t, sha256.Sum256([]byte("uhpx")), rec.Asset)
}

func sliceU128(v codec.U128) []byte {
	b := v.Bytes()
	return b[:]
}

func TestReceiptBatchAndModuleFields(t *testing.T) {
	a, b, c, d := utils.NewSigner(), utils.NewSigner(), utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b, c, d)
	contract := crypto.CreateAddress(a.EvmAddress, 0)
	auth, err := ethtypes.SignSetCode(c.EvmPrivateKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(uint64(config.DefaultChainID)), Address: contract, Nonce: 0,
	})
	require.NoError(t, err)
	type opTx struct {
		bz []byte
		op uint8
	}
	var txs []opTx
	_, bz := evmTx(t, a, receiptLegacyTx(0, nil, big.NewInt(0), createGas, logInit))
	txs = append(txs, opTx{bz, ethtypes.LegacyTxType})
	_, bz = evmTx(t, a, dynamicTx(1, &b.EvmAddress, big.NewInt(1), transferGas, nil))
	txs = append(txs, opTx{bz, ethtypes.DynamicFeeTxType})
	_, bz = evmTx(t, b, &ethtypes.SetCodeTx{
		ChainID: uint256.NewInt(uint64(config.DefaultChainID)), Nonce: 0, GasTipCap: uint256.NewInt(gasPriceWei),
		GasFeeCap: uint256.NewInt(gasPriceWei), Gas: callGas, To: c.EvmAddress, Value: uint256.NewInt(0),
		AuthList: []ethtypes.SetCodeAuthorization{auth},
	})
	txs = append(txs, opTx{bz, ethtypes.SetCodeTxType})
	cosmos := signCosmos(t, rh.openCtx(), rh.harness, d, 200_000,
		banktypes.NewMsgSend(d.AccountAddress, b.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(5)))))
	txs = append(txs, opTx{cosmos, sequencer.CosmosOperation})

	var batch [32]byte
	binary.BigEndian.PutUint64(batch[24:], uint64(openHeight))
	for i, tx := range txs {
		acc, rec, _ := rh.accept(t, tx.bz)
		require.Equal(t, tx.op, rec.Operation, "tx %d", i)
		require.Equal(t, batch, rec.BatchID)
		require.Equal(t, receiptModuleID, rec.ModuleID)
		require.Equal(t, uint32(1), rec.ModuleVersion)
		require.Equal(t, uint32(0), rec.ParameterVersion)
		require.Equal(t, uint64(rh.openMs), rec.Timestamp)
		require.Equal(t, testKeyID, rec.ContextHash)
		require.Equal(t, uint32(i+1), binary.BigEndian.Uint32(rec.Effects[0].Body[24:28]))
		require.Equal(t, i+1, acc.TxIndex)
		for _, e := range rec.Effects {
			require.Equal(t, receiptModuleID, e.ModuleID)
		}
	}

	_, err = sequencer.NewReceipter(rh.exec, rh.app, &rh.app.EvmKeeper, rh.key, sequencer.KernelReservedModules)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
	in := vectorInput()
	in.ModuleID = 11
	_, err = sequencer.BuildReceipt(in, testKeyID)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
}

func TestReceiptAccountFields(t *testing.T) {
	a, b, c := utils.NewSigner(), utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b, c)
	funded := new(big.Int).Mul(big.NewInt(fundingUhpx), big.NewInt(weiPerUhpx))
	value := big.NewInt(onePaxInWei)
	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))

	_, bz := transfer(t, a, 0, b.EvmAddress, value)
	_, rec, _ := rh.accept(t, bz)
	require.Equal(t, addr32(a.EvmAddress), rec.From)
	require.Equal(t, addr32(b.EvmAddress), rec.To)
	require.Equal(t, u128(value), rec.Amount)
	require.Equal(t, uint64(0), rec.FromSequence)
	require.Equal(t, u128(funded), rec.FromBalanceBefore)
	require.Equal(t, u128(new(big.Int).Sub(funded, new(big.Int).Add(value, fee))), rec.FromBalanceAfter)
	require.Equal(t, u128(funded), rec.ToBalanceBefore)
	require.Equal(t, u128(new(big.Int).Add(funded, value)), rec.ToBalanceAfter)
	require.Equal(t, [32]byte{}, rec.AuthorizationHash)
	require.Equal(t, [32]byte{}, rec.TransferSetRoot)

	_, bz = evmTx(t, a, receiptLegacyTx(1, nil, big.NewInt(0), createGas, logInit))
	_, rec, _ = rh.accept(t, bz)
	contract := crypto.CreateAddress(a.EvmAddress, 1)
	require.Equal(t, addr32(contract), rec.To)
	require.Equal(t, uint64(1), rec.FromSequence)
	require.Equal(t, codec.U128{}, rec.ToBalanceBefore)
	require.Equal(t, codec.U128{}, rec.ToBalanceAfter)

	auth, err := ethtypes.SignSetCode(c.EvmPrivateKey, ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(uint64(config.DefaultChainID)), Address: contract, Nonce: 0,
	})
	require.NoError(t, err)
	etx, bz := evmTx(t, a, &ethtypes.SetCodeTx{
		ChainID: uint256.NewInt(uint64(config.DefaultChainID)), Nonce: 2, GasTipCap: uint256.NewInt(gasPriceWei),
		GasFeeCap: uint256.NewInt(gasPriceWei), Gas: callGas, To: c.EvmAddress, Value: uint256.NewInt(0),
		AuthList: []ethtypes.SetCodeAuthorization{auth},
	})
	_, rec, _ = rh.accept(t, bz)
	enc, err := rlp.EncodeToBytes(etx.SetCodeAuthorizations())
	require.NoError(t, err)
	require.Equal(t, [32]byte(crypto.Keccak256Hash(enc)), rec.AuthorizationHash)
	require.NotEqual(t, [32]byte{}, rec.AuthorizationHash)
	require.Equal(t, addr32(c.EvmAddress), rec.To)
	require.Equal(t, uint64(2), rec.FromSequence)
	require.Equal(t, uint8(ethtypes.SetCodeTxType), rec.Operation)
}

func TestReceiptSummaryEffect(t *testing.T) {
	a := utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a)
	var cumulative uint64
	contract := crypto.CreateAddress(a.EvmAddress, 0)
	cases := []struct {
		data     ethtypes.TxData
		contract common.Address
		sponsor  common.Address
		retLen   uint32
		logs     uint32
	}{
		{receiptLegacyTx(0, nil, big.NewInt(0), createGas, logInit), contract, common.Address{}, uint32(len(logRuntime)), 0},
		{receiptLegacyTx(1, &contract, big.NewInt(0), callGas, nil), common.Address{}, common.Address{}, 0, 3},
		{receiptLegacyTx(2, nil, big.NewInt(0), createGas, sponsoredInit), crypto.CreateAddress(a.EvmAddress, 2), sponsorAddr, 0, 1},
	}
	for i, c := range cases {
		etx, bz := evmTx(t, a, c.data)
		acc, rec, _ := rh.accept(t, bz)
		exec := rh.transient(t, etx, acc)
		cumulative += exec.GasUsed
		e := rec.Effects[0]
		require.Equal(t, uint8(1), e.Kind, "case %d", i)
		require.Equal(t, uint16(0), e.Ordinal)
		require.Equal(t, uint16(0), e.EventType)
		require.False(t, e.Monetary)
		require.Equal(t, [32]byte{}, e.TransferSetRoot)
		want := summaryBody(exec.GasUsed, cumulative, gasPriceWei, uint32(acc.TxIndex), c.contract, c.sponsor, "", c.retLen, c.logs, "uhpx")
		require.Equal(t, hex.EncodeToString(want), hex.EncodeToString(e.Body), "case %d", i)
		require.LessOrEqual(t, len(e.Body), 191)
		require.Len(t, rec.Effects, int(c.logs)+1)
	}

	in := vectorInput()
	body, err := sequencer.SummaryEffect(in)
	require.NoError(t, err)
	require.Len(t, body, 113)
	in.FeeDenom = string(repeat('a', 65))
	_, err = sequencer.SummaryEffect(in)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
}

func TestReceiptLogEffects(t *testing.T) {
	a := utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a)
	_, bz := evmTx(t, a, receiptLegacyTx(0, nil, big.NewInt(0), createGas, logInit))
	rh.accept(t, bz)
	contract := crypto.CreateAddress(a.EvmAddress, 0)
	etx, bz := evmTx(t, a, receiptLegacyTx(1, &contract, big.NewInt(0), callGas, nil))
	acc, rec, _ := rh.accept(t, bz)
	wantData := receiptWord(0x2a)
	want := [][]byte{
		logBody(contract, wantData[:], receiptWord(1), receiptWord(2)),
		logBody(contract, []byte{0x2a, 0, 0, 0, 0}, receiptWord(3), receiptWord(4), receiptWord(5), receiptWord(6)),
		logBody(contract, nil),
	}
	require.Len(t, rec.Effects, 4)
	exec := rh.transient(t, etx, acc)
	for i, w := range want {
		e := rec.Effects[i+1]
		require.Equal(t, uint8(3), e.Kind)
		require.Equal(t, uint16(i+1), e.Ordinal)
		require.Equal(t, uint16(0), e.EventType)
		require.Equal(t, hex.EncodeToString(w), hex.EncodeToString(e.Body))
		body, err := sequencer.LogEffect(exec.Logs[i])
		require.NoError(t, err)
		require.Equal(t, w, body)
	}
	require.Len(t, want[1], 181)

	five := &evmtypes.Log{Address: contract.Hex(), Topics: make([]string, 5)}
	for i := range five.Topics {
		five.Topics[i] = common.Hash(receiptWord(byte(i))).Hex()
	}
	_, err := sequencer.LogEffect(five)
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
	_, err = sequencer.LogEffect(&evmtypes.Log{Address: contract.Hex(), Topics: []string{"0x01"}})
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)
	_, err = sequencer.LogEffect(&evmtypes.Log{Address: "0x1234"})
	require.ErrorIs(t, err, sequencer.ErrReceiptInput)

	// A transaction with more than 511 logs is encoded in full.
	unsigned, err := sequencer.BuildReceipt(largeFixtureLogsInput(600, contract), testKeyID)
	require.NoError(t, err)
	require.Equal(t, uint32(601), binary.BigEndian.Uint32(unsigned[162:166]))
}

// largeFixtureLogsInput is the shared receipt vector carrying n logs of contract.
func largeFixtureLogsInput(n int, contract common.Address) *sequencer.ReceiptInput {
	in := vectorInput()
	in.EVM.Logs = make([]*evmtypes.Log, n)
	for i := range in.EVM.Logs {
		in.EVM.Logs[i] = &evmtypes.Log{Address: contract.Hex(), Data: []byte{byte(i)}}
	}
	return in
}

func TestReceiptLargeFixtureVerifies(t *testing.T) {
	key := vectorKey(t)
	pub := key.PublicKey()
	contract := common.BytesToAddress(repeat(0x44, 20))
	// 600 logs exceed MaxEffects; 512 logs give 513 effects, the first count
	// MaxEffects refuses.
	for _, logs := range []int{600, 512} {
		signed, err := key.SignReceipt(largeFixtureLogsInput(logs, contract))
		require.NoError(t, err)
		require.Equal(t, uint32(logs+1), binary.BigEndian.Uint32(signed[162:166]))
		verified, err := verify.ReceiptSignature(signed, pub)
		require.NoError(t, err, "%d logs", logs)
		require.Len(t, verified.Receipt.Effects, logs+1)
		require.Equal(t, receiptModuleID, verified.Receipt.ModuleID)
		require.Equal(t, sequencer.ReceiptLayoutVersion, verified.Receipt.ModuleVersion)
		for i, e := range verified.Receipt.Effects {
			require.Equal(t, uint16(i), e.Ordinal)
			require.Equal(t, receiptModuleID, e.ModuleID)
		}
		tampered := append([]byte(nil), signed...)
		tampered[len(tampered)-1] ^= 1
		_, err = verify.ReceiptSignature(tampered, pub)
		require.ErrorIs(t, err, verify.ErrSequencerSignature)
	}
}

func TestReceiptEffectsNonMonetary(t *testing.T) {
	a, b := utils.NewSigner(), utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, a, b)
	contract := crypto.CreateAddress(a.EvmAddress, 0)
	_, create := evmTx(t, a, receiptLegacyTx(0, nil, big.NewInt(0), createGas, logInit))
	_, call := evmTx(t, a, receiptLegacyTx(1, &contract, big.NewInt(0), callGas, nil))
	_, revert := evmTx(t, a, receiptLegacyTx(2, nil, big.NewInt(0), createGas, revertInit))
	_, send := transfer(t, b, 0, a.EvmAddress, big.NewInt(1))
	for _, bz := range [][]byte{create, call, revert, send} {
		_, rec, _ := rh.accept(t, bz)
		for i, e := range rec.Effects {
			require.False(t, e.Monetary)
			require.Equal(t, [32]byte{}, e.TransferSetRoot)
			if i == 0 {
				require.Equal(t, uint8(1), e.Kind)
			} else {
				require.Equal(t, uint8(3), e.Kind)
			}
		}
	}
	unsigned, err := sequencer.BuildReceipt(vectorInput(), testKeyID)
	require.NoError(t, err)
	// The monetary byte follows module_id, ordinal, event_type and kind.
	for _, off := range []int{166, 166 + 48 + 113, 166 + 48 + 113 + 48 + 117} {
		require.Equal(t, byte(0), unsigned[off+7])
	}
}

func TestReceiptDigestMatchesWireCrate(t *testing.T) {
	unsigned, err := sequencer.BuildReceipt(vectorInput(), [32]byte(repeat(0x33, 32)))
	require.NoError(t, err)
	digest := codec.ReceiptDigest(unsigned)
	require.Equal(t, hex.EncodeToString(wireVector(t, "GO_RECEIPT_DIGEST")), hex.EncodeToString(digest[:]))
	require.Equal(t, sha256.Sum256(append([]byte("LXP/v1/receipt\x00"), unsigned...)), digest)
	signed := wireVector(t, "GO_SIGNED_RECEIPT")
	require.Equal(t, signed[:len(signed)-69], unsigned[:len(unsigned)-1])
	require.Equal(t, byte(0), unsigned[len(unsigned)-1])
	rec, err := codec.DecodeReceipt(signed)
	require.NoError(t, err)
	require.Equal(t, digest, rec.Digest())
}

func TestReceiptKeySeparateFromConsensusKey(t *testing.T) {
	dir := t.TempDir()
	consensusFile, consensusPub := writeConsensusKey(t, dir, consensusKeySeed)
	pins := []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: publicOf(testKeySeed), FirstBatchNumber: 1, LastBatchNumber: 10},
		{SequencerID: [32]byte(label("consensus id")), PublicKey: publicOf(consensusKeySeed), FirstBatchNumber: 1, LastBatchNumber: 10},
	}
	_, err := sequencer.LoadReceiptKey(writeSeed(t, dir, "reused", consensusKeySeed, 0o600), consensusFile, pins, 2)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyIsConsensusKey)
	_, err = sequencer.LoadReceiptKey(consensusFile, consensusFile, pins, 2)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyIsConsensusKey)
	_, err = sequencer.LoadReceiptKey(writeSeed(t, dir, "exposed", testKeySeed, 0o644), consensusFile, pins, 2)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyFile)
	short := filepath.Join(dir, "short")
	require.NoError(t, os.WriteFile(short, []byte("abcd\n"), 0o600))
	_, err = sequencer.LoadReceiptKey(short, consensusFile, pins, 2)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyFile)

	key, err := sequencer.LoadReceiptKey(writeSeed(t, dir, "receipt_key", testKeySeed, 0o600), consensusFile, pins, 2)
	require.NoError(t, err)
	pub := key.PublicKey()
	require.Equal(t, publicOf(testKeySeed), pub)
	require.NotEqual(t, consensusPub, pub[:])
	in := vectorInput()
	in.Accepted.Height = 2
	signed, err := key.SignReceipt(in)
	require.NoError(t, err)
	_, err = verify.ReceiptSignature(signed, pub)
	require.NoError(t, err)
	_, err = verify.ReceiptSignature(signed, [32]byte(consensusPub))
	require.ErrorIs(t, err, verify.ErrSequencerSignature)
}

func TestReceiptSignatureEncoding(t *testing.T) {
	key := vectorKey(t)
	in := vectorInput()
	unsigned, err := sequencer.BuildReceipt(in, [32]byte(repeat(0x33, 32)))
	require.NoError(t, err)
	signed, err := key.SignReceipt(in)
	require.NoError(t, err)
	require.Len(t, signed, len(unsigned)-1+1+4+64)
	tail := signed[len(unsigned)-1:]
	require.Equal(t, byte(1), tail[0])
	require.Equal(t, uint32(64), binary.BigEndian.Uint32(tail[1:5]))
	digest := codec.ReceiptDigest(unsigned)
	pub := key.PublicKey()
	require.True(t, ed25519.Verify(pub[:], digest[:], tail[5:]))
	require.Equal(t, [32]byte(wireVector(t, "PINNED_RECEIPT_KEY")), pub)

	verified, err := verify.ReceiptSignature(signed, pub)
	require.NoError(t, err)
	require.Equal(t, digest, verified.Digest)
	_, err = verify.ReceiptSignature(unsigned, pub)
	require.ErrorIs(t, err, verify.ErrReceiptMissingSignature)
	tampered := append([]byte(nil), signed...)
	tampered[len(tampered)-1] ^= 1
	_, err = verify.ReceiptSignature(tampered, pub)
	require.ErrorIs(t, err, verify.ErrSequencerSignature)
}

func TestReceiptKeyMustBePinned(t *testing.T) {
	pub := publicOf(testKeySeed)
	_, err := loadKey(t, testKeySeed, nil, openHeight)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyNotPinned)
	_, err = loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: publicOf(label("another host")), FirstBatchNumber: 1, LastBatchNumber: 100},
	}, openHeight)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyNotPinned)
	_, err = loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: pub, FirstBatchNumber: 3, LastBatchNumber: 100},
	}, openHeight)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyNotPinned)
	_, err = loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: pub, FirstBatchNumber: 100, LastBatchNumber: 1},
	}, openHeight)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyPin)
	_, err = loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{PublicKey: pub, FirstBatchNumber: 1, LastBatchNumber: 100},
	}, openHeight)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyPin)

	// Rotation: K1's entry closes at 4, the next key id covers 5 onwards.
	nextID := [32]byte(label("rotated key id"))
	key, err := loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: pub, FirstBatchNumber: 1, LastBatchNumber: 4},
		{SequencerID: nextID, PublicKey: pub, FirstBatchNumber: 5, LastBatchNumber: 9},
	}, openHeight)
	require.NoError(t, err)
	id, err := key.KeyID(4)
	require.NoError(t, err)
	require.Equal(t, testKeyID, id)
	id, err = key.KeyID(5)
	require.NoError(t, err)
	require.Equal(t, nextID, id)
	in := vectorInput()
	in.Accepted.Height = 10
	_, err = key.SignReceipt(in)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyNotPinned)

	// A key pinned only below the open height refuses to receipt it.
	a, b := utils.NewSigner(), utils.NewSigner()
	h := newHarness(t, a, b)
	h.open()
	stale, err := loadKey(t, testKeySeed, []verify.SequencerAuthorization{
		{SequencerID: testKeyID, PublicKey: pub, FirstBatchNumber: 1, LastBatchNumber: 1},
	}, committedTip)
	require.NoError(t, err)
	r, err := sequencer.NewReceipter(h.exec, h.app, &h.app.EvmKeeper, stale, receiptModuleID)
	require.NoError(t, err)
	_, bz := transfer(t, a, 0, b.EvmAddress, big.NewInt(1))
	acc, signed, err := r.Accept(bz)
	require.ErrorIs(t, err, sequencer.ErrReceiptKeyNotPinned)
	require.Nil(t, acc)
	require.Nil(t, signed)
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Empty(t, batch.Txs)
}

// mixedTxs builds 200 EVM transactions from four senders: log-contract
// creations and calls, legacy and dynamic-fee transfers and reverting creations.
func mixedTxs(t *testing.T, senders []utils.TestAcct) ([][]byte, []*ethtypes.Transaction) {
	var txs [][]byte
	var etxs []*ethtypes.Transaction
	for n := uint64(0); n < 50; n++ {
		for s, from := range senders {
			contract := crypto.CreateAddress(from.EvmAddress, 0)
			peer := senders[(s+1)%len(senders)].EvmAddress
			var data ethtypes.TxData
			switch {
			case n == 0:
				data = receiptLegacyTx(n, nil, big.NewInt(0), createGas, logInit)
			case n%5 == 1:
				data = receiptLegacyTx(n, &contract, big.NewInt(0), callGas, nil)
			case n%5 == 2:
				data = dynamicTx(n, &peer, big.NewInt(int64(n)), transferGas, nil)
			case n%5 == 3:
				data = dynamicTx(n, nil, big.NewInt(0), createGas, revertInit)
			case n%5 == 4:
				data = receiptLegacyTx(n, &peer, big.NewInt(int64(s+1)), transferGas, nil)
			default:
				data = dynamicTx(n, &contract, big.NewInt(0), callGas, nil)
			}
			etx, bz := evmTx(t, from, data)
			txs = append(txs, bz)
			etxs = append(etxs, etx)
		}
	}
	return txs, etxs
}

func TestReceiptEqualsCommittedReceipt(t *testing.T) {
	senders := []utils.TestAcct{utils.NewSigner(), utils.NewSigner(), utils.NewSigner(), utils.NewSigner()}
	txs, etxs := mixedTxs(t, senders)
	require.Len(t, txs, 200)

	first := newReceiptHarness(t, 0, nil, senders...)
	receipts := make([][]byte, len(txs))
	decoded := make([]*codec.Receipt, len(txs))
	for i, bz := range txs {
		_, decoded[i], receipts[i] = first.accept(t, bz)
	}

	// The same transactions on a fresh executor give byte-identical receipts.
	replay := newReceiptHarness(t, first.openMs, nil, senders...)
	for i, bz := range txs {
		_, _, again := replay.accept(t, bz)
		require.Equal(t, receipts[i], again, "tx %d", i)
	}

	// The committed block of the same transactions holds the same results.
	twin := newHarness(t, senders...)
	resp, err := twin.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:    txs,
		Header: &tmproto.Header{ChainID: testChainID, Height: openHeight, Time: time.UnixMilli(first.openMs).UTC()},
	})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, len(txs))
	twin.app.SetDeliverStateToCommit()
	_, err = twin.app.Commit(context.Background())
	require.NoError(t, err)
	ctx := sdk.NewContext(twin.app.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: testChainID, Height: openHeight}, false)

	reverted := 0
	for i, etx := range etxs {
		committed, err := twin.app.EvmKeeper.GetReceiptWithRetry(ctx, etx.Hash(), 5)
		require.NoError(t, err, "tx %d", i)
		rec := decoded[i]
		_, err = verify.ReceiptSignature(receipts[i], first.key.PublicKey())
		require.NoError(t, err)
		require.Equal(t, [32]byte(etx.Hash()), rec.ActivityID)
		require.Equal(t, int32(1-committed.Status), rec.ResultCode, "tx %d", i)
		if committed.Status == 0 {
			reverted++
		}
		body := rec.Effects[0].Body
		require.Equal(t, committed.GasUsed, binary.BigEndian.Uint64(body[0:8]), "tx %d gasUsed", i)
		require.Equal(t, committed.CumulativeGasUsed, binary.BigEndian.Uint64(body[8:16]), "tx %d cumulativeGasUsed", i)
		require.Equal(t, committed.EffectiveGasPrice, binary.BigEndian.Uint64(body[16:24]), "tx %d effectiveGasPrice", i)
		var contract common.Address
		if committed.To == "" {
			contract = common.HexToAddress(committed.ContractAddress)
		}
		require.Equal(t, contract[:], body[28:48], "tx %d contractAddress", i)
		require.Equal(t, uint32(len(committed.Logs)), binary.BigEndian.Uint32(body[104:108]))
		require.Len(t, rec.Effects, len(committed.Logs)+1)
		for j, log := range committed.Logs {
			want, err := sequencer.LogEffect(log)
			require.NoError(t, err)
			require.Equal(t, want, rec.Effects[j+1].Body, "tx %d log %d", i, j)
		}
	}
	require.Equal(t, 40, reverted)
}
