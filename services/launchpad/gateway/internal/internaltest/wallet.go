package internaltest

import (
	"crypto/ecdsa"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet is a freshly-generated EVM identity for signing EIP-191 messages in
// integration tests.
type Wallet struct {
	key  *ecdsa.PrivateKey
	Addr string // lowercased, matching the gateway's normalization
}

// NewWallet generates a random test wallet.
func NewWallet(t *testing.T) Wallet {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	return Wallet{key: k, Addr: strings.ToLower(crypto.PubkeyToAddress(k.PublicKey).Hex())}
}

// Sign returns the EIP-191 (personal_sign) signature of msg as a 0x-hex string
// with the Ethereum {27,28} recovery id.
func (w Wallet) Sign(t *testing.T, msg string) string {
	t.Helper()
	hash := accounts.TextHash([]byte(msg))
	sig, err := crypto.Sign(hash, w.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig)
}
