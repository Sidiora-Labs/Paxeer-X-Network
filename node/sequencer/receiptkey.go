package sequencer

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
)

var (
	// ErrReceiptKeyFile refuses an unreadable, malformed or exposed receipt key file.
	ErrReceiptKeyFile = errors.New("sequencer: receipt key file")
	// ErrReceiptKeyIsConsensusKey refuses a receipt key that is the validator consensus key.
	ErrReceiptKeyIsConsensusKey = errors.New("sequencer: receipt key is the consensus key")
	// ErrReceiptKeyPin refuses a ReceiptKeys entry with a zero key id or firstHeight above lastHeight.
	ErrReceiptKeyPin = errors.New("sequencer: invalid receipt key pin")
	// ErrReceiptKeyNotPinned refuses a receipt public key no ReceiptKeys entry covers at the height.
	ErrReceiptKeyNotPinned = errors.New("sequencer: receipt key is not pinned for the height")
)

// ReceiptKey is the host's ed25519 receipt signing key with the ReceiptKeys
// entries that pin its public key.
type ReceiptKey struct {
	private ed25519.PrivateKey
	public  [32]byte
	pins    []verify.SequencerAuthorization
}

// LoadReceiptKey loads the receipt key from keyFile, a hex 32-byte ed25519
// seed readable by its owner only, held apart from the consensus key in
// consensusKeyFile. It refuses start unless an entry of pins covers its
// public key at openHeight.
func LoadReceiptKey(keyFile, consensusKeyFile string, pins []verify.SequencerAuthorization, openHeight int64) (*ReceiptKey, error) {
	info, err := os.Stat(keyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReceiptKeyFile, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s must be a regular file with mode 0600 or stricter, has %v", ErrReceiptKeyFile, keyFile, info.Mode())
	}
	if consensusInfo, err := os.Stat(consensusKeyFile); err == nil && os.SameFile(info, consensusInfo) {
		return nil, fmt.Errorf("%w: %s is the consensus key file", ErrReceiptKeyIsConsensusKey, keyFile)
	}
	raw, err := os.ReadFile(filepath.Clean(keyFile))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReceiptKeyFile, err)
	}
	seed, err := hex.DecodeString(strings.TrimSuffix(string(raw), "\n"))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%w: %s must hold a hex %d-byte seed", ErrReceiptKeyFile, keyFile, ed25519.SeedSize)
	}
	private := ed25519.NewKeyFromSeed(seed)
	clear(seed)
	key := &ReceiptKey{private: private}
	copy(key.public[:], private.Public().(ed25519.PublicKey))

	consensus, err := privval.LoadFilePVEmptyState(consensusKeyFile, "")
	if err != nil {
		return nil, fmt.Errorf("%w: consensus key: %v", ErrReceiptKeyFile, err)
	}
	if bytes.Equal(consensus.Key.PubKey.Bytes(), key.public[:]) {
		return nil, ErrReceiptKeyIsConsensusKey
	}
	for _, pin := range pins {
		if pin.SequencerID == ([32]byte{}) || pin.FirstBatchNumber > pin.LastBatchNumber {
			return nil, fmt.Errorf("%w: key id %x, heights %d..%d", ErrReceiptKeyPin, pin.SequencerID, pin.FirstBatchNumber, pin.LastBatchNumber)
		}
		if pin.PublicKey == key.public {
			key.pins = append(key.pins, pin)
		}
	}
	if _, err := key.KeyID(openHeight); err != nil {
		return nil, err
	}
	return key, nil
}

// PublicKey is the receipt public key pinned in ReceiptKeys.
func (k *ReceiptKey) PublicKey() [32]byte { return k.public }

// KeyID is the key id of the ReceiptKeys entry covering height.
func (k *ReceiptKey) KeyID(height int64) ([32]byte, error) {
	if height > 0 {
		for _, pin := range k.pins {
			if pin.FirstBatchNumber <= uint64(height) && uint64(height) <= pin.LastBatchNumber {
				return pin.SequencerID, nil
			}
		}
	}
	return [32]byte{}, fmt.Errorf("%w: public key %x at height %d", ErrReceiptKeyNotPinned, k.public, height)
}

// SignReceipt encodes the receipt with context_hash = the key id pinned at
// the receipt height and appends the ed25519 signature over
// sha256("LXP/v1/receipt\0" || unsigned) as flag 1, u32be 64, signature.
func (k *ReceiptKey) SignReceipt(in *ReceiptInput) ([]byte, error) {
	if in == nil || in.Accepted == nil {
		return nil, inputErr("missing accepted transaction")
	}
	keyID, err := k.KeyID(in.Accepted.Height)
	if err != nil {
		return nil, err
	}
	unsigned, err := BuildReceipt(in, keyID)
	if err != nil {
		return nil, err
	}
	digest := codec.ReceiptDigest(unsigned)
	sig := ed25519.Sign(k.private, digest[:])
	signed := append(unsigned[:len(unsigned)-1], 1)
	signed = binary.BigEndian.AppendUint32(signed, ed25519.SignatureSize)
	return append(signed, sig...), nil
}
