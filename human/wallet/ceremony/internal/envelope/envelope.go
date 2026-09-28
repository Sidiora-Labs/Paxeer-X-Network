package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	EnvMasterKey  = "CEREMONY_MASTER_KEY"
	Version1      = 1
	MasterKeyLen  = 32
	PrivateKeyLen = 32
	versionLen    = 1
	ivLen         = 12
	tagLen        = 16
)

var (
	ErrMasterKey          = errors.New("envelope: master key must decode from base64 to exactly 32 bytes")
	ErrFormat             = errors.New("envelope: malformed envelope")
	ErrUnsupportedVersion = errors.New("envelope: unsupported envelope version")
	ErrDecrypt            = errors.New("envelope: decryption failed")
	ErrKeyLength          = errors.New("envelope: decrypted key is not 32 bytes")
	ErrInvalidKey         = errors.New("envelope: decrypted key is not a valid secp256k1 scalar")
	ErrAddress            = errors.New("envelope: stored address is not a valid address")
	ErrAddressMismatch    = errors.New("envelope: decrypted key does not derive the stored address")
	ErrZeroed             = errors.New("envelope: key already zeroed")
)

type MismatchError struct {
	Stored  common.Address
	Derived common.Address
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("%v: stored %s, derived %s", ErrAddressMismatch, e.Stored.Hex(), e.Derived.Hex())
}

func (e *MismatchError) Unwrap() error { return ErrAddressMismatch }

type Key struct {
	secret  []byte
	address common.Address
}

func (k *Key) Address() common.Address { return k.address }

func (k *Key) Scalar() (*big.Int, error) {
	if k.secret == nil {
		return nil, ErrZeroed
	}
	return new(big.Int).SetBytes(k.secret), nil
}

func (k *Key) Zero() {
	zero(k.secret)
	k.secret = nil
}

func LoadMasterKey(getenv func(string) string) ([]byte, error) {
	raw := strings.TrimSpace(getenv(EnvMasterKey))
	if raw == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrMasterKey, EnvMasterKey)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrMasterKey
	}
	if len(key) != MasterKeyLen {
		zero(key)
		return nil, ErrMasterKey
	}
	return key, nil
}

func OpenEnvelope(envelopeB64 string, masterKey []byte, storedAddress string) (*Key, error) {
	if len(masterKey) != MasterKeyLen {
		return nil, ErrMasterKey
	}
	if !common.IsHexAddress(storedAddress) {
		return nil, ErrAddress
	}
	stored := common.HexToAddress(storedAddress)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelopeB64))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64", ErrFormat)
	}
	if len(raw) < versionLen+ivLen+tagLen+1 {
		return nil, fmt.Errorf("%w: envelope too short", ErrFormat)
	}
	if raw[0] != Version1 {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, raw[0])
	}
	iv := raw[versionLen : versionLen+ivLen]
	tag := raw[versionLen+ivLen : versionLen+ivLen+tagLen]
	body := raw[versionLen+ivLen+tagLen:]

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, ErrMasterKey
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, ivLen)
	if err != nil {
		return nil, ErrMasterKey
	}
	sealed := make([]byte, 0, len(body)+tagLen)
	sealed = append(sealed, body...)
	sealed = append(sealed, tag...)
	plaintext, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	if len(plaintext) != PrivateKeyLen {
		zero(plaintext)
		return nil, ErrKeyLength
	}
	priv, err := crypto.ToECDSA(plaintext)
	if err != nil {
		zero(plaintext)
		return nil, ErrInvalidKey
	}
	derived := crypto.PubkeyToAddress(priv.PublicKey)
	wipeInt(priv.D)
	if derived != stored {
		zero(plaintext)
		return nil, &MismatchError{Stored: stored, Derived: derived}
	}
	return &Key{secret: plaintext, address: derived}, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func wipeInt(x *big.Int) {
	if x == nil {
		return
	}
	words := x.Bits()
	for i := range words {
		words[i] = 0
	}
	x.SetInt64(0)
}
