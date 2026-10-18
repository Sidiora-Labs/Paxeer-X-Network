package readpolicy

import (
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionState struct {
	State             string       `json:"state"`
	ChainID           string       `json:"chainId"`
	SubmittedHash     common.Hash  `json:"submittedHash"`
	ReplacementHash   *common.Hash `json:"replacementHash,omitempty"`
	EvidenceType      string       `json:"evidenceType,omitempty"`
	BlockNumber       string       `json:"blockNumber,omitempty"`
	BlockHash         *common.Hash `json:"blockHash,omitempty"`
	DeploymentVersion string       `json:"deploymentVersion"`
	FailureCode       string       `json:"failureCode,omitempty"`
	ObservedAt        time.Time    `json:"observedAt"`
}

func Submitted(hash common.Hash, deployment string) (TransactionState, error) {
	if hash == (common.Hash{}) || deployment == "" {
		return TransactionState{}, errors.New("transaction identity required")
	}
	return TransactionState{State: "submitted", ChainID: "125", SubmittedHash: hash, DeploymentVersion: deployment, ObservedAt: time.Now().UTC()}, nil
}
func (s TransactionState) AwaitingReceipt() TransactionState {
	if s.State == "submitted" {
		s.State = "awaiting_receipt"
	}
	return s
}
func (s TransactionState) Replaced(hash common.Hash) (TransactionState, error) {
	if (s.State != "submitted" && s.State != "awaiting_receipt") || hash == (common.Hash{}) || hash == s.SubmittedHash {
		return TransactionState{}, errors.New("invalid transaction replacement")
	}
	s.State = "replaced"
	s.ReplacementHash = &hash
	return s, nil
}
func WalletRejected(deployment string) TransactionState {
	return TransactionState{State: "wallet_rejected", ChainID: "125", DeploymentVersion: deployment, FailureCode: "WALLET_SIGNATURE_REJECTED", ObservedAt: time.Now().UTC()}
}

// WithReceipt projects committed receipt evidence onto the state. blockHash is
// the hash the node reports for header's block. On chain 125
// (CometBFT-backed) it is not header.Hash(), so the receipt's block identity is
// compared with the reported hash only.
func (s TransactionState) WithReceipt(receipt *types.Receipt, header *types.Header, blockHash common.Hash) (TransactionState, error) {
	if s.ChainID != "125" || s.SubmittedHash == (common.Hash{}) || s.DeploymentVersion == "" || (s.State != "submitted" && s.State != "awaiting_receipt") || receipt == nil || header == nil || header.Number == nil || !header.Number.IsUint64() || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.TxHash != s.SubmittedHash || receipt.BlockNumber.Cmp(header.Number) != 0 || blockHash == (common.Hash{}) || receipt.BlockHash != blockHash || receipt.Status > 1 {
		return TransactionState{}, errors.New("receipt committed identity mismatch")
	}
	for i, log := range receipt.Logs {
		if log == nil || log.Removed || log.TxHash != receipt.TxHash || log.BlockHash != receipt.BlockHash || log.BlockNumber != receipt.BlockNumber.Uint64() || log.TxIndex != receipt.TransactionIndex {
			return TransactionState{}, errors.New("receipt log identity mismatch")
		}
		if i > 0 && log.Index <= receipt.Logs[i-1].Index {
			return TransactionState{}, errors.New("receipt log ordering mismatch")
		}
	}
	s.State = "included_success"
	if receipt.Status == 0 {
		s.State = "included_failure"
		s.FailureCode = "TRANSACTION_REVERTED"
	}
	s.EvidenceType = "rpc_receipt"
	s.BlockNumber = receipt.BlockNumber.String()
	hash := receipt.BlockHash
	s.BlockHash = &hash
	s.ObservedAt = time.Now().UTC()
	return s, nil
}
