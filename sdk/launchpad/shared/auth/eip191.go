package auth

import (
	"errors"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"strings"
	"time"
)

// VerificationError deliberately excludes signatures and private message bytes.
type VerificationError struct {
	Category  string
	Retryable bool
}

func (e *VerificationError) Error() string { return "wallet verification: " + e.Category }

type ProofResult struct {
	Method          string
	Wallet          string
	ChallengeDigest string
	ChainID         uint64
	BlockHeight     uint64
	BlockHash       string
	CodeHash        string
}
type ObserveVerification func(method, category string, duration time.Duration)

func NormalizeWallet(wallet string) (string, error) {
	if len(wallet) != 42 || !common.IsHexAddress(wallet) || common.HexToAddress(wallet) == (common.Address{}) {
		return "", &VerificationError{Category: "address"}
	}
	return strings.ToLower(wallet), nil
}

// VerifyEOA verifies exact personal_sign bytes, including low-S canonicality.
func VerifyEOA(wallet, message, signature string, observe ObserveVerification) (result ProofResult, err error) {
	start := time.Now()
	defer func() {
		if recover() != nil {
			err = &VerificationError{Category: "recovery"}
			result = ProofResult{}
		}
		if observe != nil {
			category := "accepted"
			var v *VerificationError
			if errors.As(err, &v) {
				category = v.Category
			}
			observe("eip191", category, time.Since(start))
		}
	}()
	normalized, err := NormalizeWallet(wallet)
	if err != nil {
		return result, err
	}
	if len(message) > 8192 {
		return result, &VerificationError{Category: "message_size"}
	}
	if len(signature) != 132 {
		return result, &VerificationError{Category: "signature_length"}
	}
	sig, err := hexutil.Decode(signature)
	if err != nil || len(sig) != 65 {
		return result, &VerificationError{Category: "signature_length"}
	}
	switch sig[64] {
	case 27, 28:
		sig[64] -= 27
	case 0, 1:
	default:
		return result, &VerificationError{Category: "recovery_id"}
	}
	if !crypto.ValidateSignatureValues(sig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return result, &VerificationError{Category: "noncanonical"}
	}
	hash := accounts.TextHash([]byte(message))
	pub, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return result, &VerificationError{Category: "recovery"}
	}
	if strings.ToLower(crypto.PubkeyToAddress(*pub).Hex()) != normalized {
		return result, &VerificationError{Category: "signer"}
	}
	return ProofResult{Method: "eip191", Wallet: normalized, ChallengeDigest: hexutil.Encode(hash)}, nil
}
func VerifyWalletSignature(address, message, signature string) bool {
	_, err := VerifyEOA(address, message, signature, nil)
	return err == nil
}
