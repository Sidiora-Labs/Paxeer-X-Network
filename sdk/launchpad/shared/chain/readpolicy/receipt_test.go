package readpolicy

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestReceiptSuccessRequiresCommittedEvidence(t *testing.T) {
	hash := common.HexToHash("0x1234")
	s, err := Submitted(hash, "deployment-v2")
	if err != nil {
		t.Fatal(err)
	}
	before := s.ObservedAt
	s = s.AwaitingReceipt()
	s.ObservedAt = before.Add(10 * time.Second)
	if s.State != "awaiting_receipt" || s.EvidenceType != "" {
		t.Fatal("elapsed time produced success")
	}
	header := &types.Header{Number: big.NewInt(100), Time: 1000}
	// Chain 125 reports a consensus block hash that is not header.Hash().
	reported := common.HexToHash("0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b")
	receipt := &types.Receipt{Status: 1, TxHash: hash, BlockNumber: big.NewInt(100), BlockHash: reported, TransactionIndex: 2}
	if _, err := s.WithReceipt(receipt, header, header.Hash()); err == nil {
		t.Fatal("recomputed header hash accepted as the receipt block identity")
	}
	next, err := s.WithReceipt(receipt, header, reported)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "included_success" || next.EvidenceType != "rpc_receipt" || next.BlockHash == nil || *next.BlockHash != reported || next.BlockNumber != "100" {
		t.Fatal("valid committed receipt not projected")
	}
	receipt.Status = 0
	next, err = s.WithReceipt(receipt, header, reported)
	if err != nil || next.State != "included_failure" || next.FailureCode != "TRANSACTION_REVERTED" {
		t.Fatal("revert became success", err)
	}
	receipt.Status = 1
	receipt.BlockHash = common.HexToHash("0xabcd")
	if _, err := s.WithReceipt(receipt, header, reported); err == nil {
		t.Fatal("mixed committed identity accepted")
	}
	if _, err := s.WithReceipt(nil, header, reported); err == nil {
		t.Fatal("absent receipt accepted")
	}
}
func TestReplacementAndWalletRejectionAreDistinct(t *testing.T) {
	s, err := Submitted(common.HexToHash("0x1234"), "deployment-v2")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Replaced(common.HexToHash("0xabcd"))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "replaced" || r.ReplacementHash == nil || r.SubmittedHash != s.SubmittedHash {
		t.Fatal("replacement linkage lost")
	}
	if WalletRejected("deployment-v2").State != "wallet_rejected" {
		t.Fatal("wallet rejection conflated")
	}
	header := &types.Header{Number: big.NewInt(100)}
	receipt := &types.Receipt{Status: 1, TxHash: s.SubmittedHash, BlockNumber: big.NewInt(100), BlockHash: header.Hash()}
	if _, err := r.WithReceipt(receipt, header, header.Hash()); err == nil {
		t.Fatal("replaced transaction accepted unrelated terminal transition")
	}
}
