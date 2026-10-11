package sequencer

import (
	"crypto/sha256"
	"math"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// Accept runs txBytes alone on a fresh branch of the open layer through the
// deliver path. The branch is written only when the transaction is receipted
// (success or EVM revert); any refusal or recovered panic drops it, leaving
// the open layer and the seq counter unchanged.
func (s *sequential) Accept(txBytes []byte) (*Accepted, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return nil, ErrLayerNotOpen
	}
	if s.lastSeq == math.MaxUint64 {
		return nil, ErrSeqExhausted
	}
	if len(txBytes) == 0 {
		return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "empty transaction")
	}
	tx, err := s.host.GetTxConfig().TxDecoder()(txBytes)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
	}
	checksum := sha256.Sum256(txBytes)
	txIndex := len(s.open.txs) + 1
	res, branch, err := s.deliver(tx, txBytes, checksum, txIndex)
	if err != nil {
		return nil, err
	}
	if !receipted(res) {
		return nil, sdkerrors.ABCIError(res.Codespace, res.Code, res.Log)
	}
	branch.Write()

	activityID := checksum
	evmMsg := s.host.GetEVMMsg(tx)
	if evmMsg != nil {
		if etx, _ := evmMsg.AsTransaction(); etx != nil {
			activityID = etx.Hash()
		}
	}
	seq := s.lastSeq + 1
	prev := s.lastSeqHash
	seqHash := nextSeqHash(prev, seq, activityID)
	result := &abci.ExecTxResult{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Info:      res.Info,
		GasWanted: res.GasWanted,
		GasUsed:   res.GasUsed,
		Events:    res.Events,
		Codespace: res.Codespace,
		EvmTxInfo: res.EvmTxInfo,
	}
	s.open.txs = append(s.open.txs, txBytes)
	s.open.results = append(s.open.results, result)
	s.open.evmMsgs = append(s.open.evmMsgs, evmMsg)
	s.lastSeq = seq
	s.lastSeqHash = seqHash
	return &Accepted{
		Seq:         seq,
		PrevSeqHash: prev,
		SeqHash:     seqHash,
		ActivityID:  activityID,
		Height:      s.open.height,
		TxIndex:     txIndex,
		TimestampMs: s.open.timestampMs,
		Result:      result,
	}, nil
}

// deliver runs DeliverTx on a branch of the open layer made the way the
// deliver path makes its per-transaction cache. A panic escaping DeliverTx is
// recovered into ErrPanic and the branch is discarded.
func (s *sequential) deliver(tx sdk.Tx, txBytes []byte, checksum [32]byte, txIndex int) (res abci.ResponseDeliverTx, branch sdk.CacheMultiStore, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, branch = abci.ResponseDeliverTx{}, nil
			err = sdkerrors.Wrapf(sdkerrors.ErrPanic, "recovered: %v", r)
		}
	}()
	ctx, branch := s.host.CacheTxContext(s.open.ctx.WithTxIndex(txIndex), checksum)
	res = s.host.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txBytes}, tx, checksum)
	return res, branch, nil
}

// receipted reports whether the deliver path keeps the transaction's writes:
// code 0, or an EVM execution that reverted after its fees were charged.
func receipted(res abci.ResponseDeliverTx) bool {
	return res.Code == abci.CodeTypeOK || (res.EvmTxInfo != nil && res.EvmTxInfo.VmError != "")
}
