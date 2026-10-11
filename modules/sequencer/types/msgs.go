package types

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const (
	// SequenceBatchDomain separates the batch signature from every other
	// signed object.
	SequenceBatchDomain = "PAXEER/v1/sequence-batch\x00"

	HashLength      = 32
	KeyIDLength     = 32
	SignatureLength = 64
)

var (
	ErrInvalidSequenceBatch = sdkerrors.Register(ModuleName, 2, "invalid sequence batch")
	ErrBadSignature         = sdkerrors.Register(ModuleName, 3, "bad sequence batch signature")
)

// EmptyTxRoot is the txRoot of a block with no sequenced transactions: the
// sha256 of the empty concatenation of activity ids.
var EmptyTxRoot = sha256.Sum256(nil)

// MsgSequenceBatch records the sequence range a block carries. It is tx 0 of
// every block at or above the activation height, carries no Cosmos signer and
// is authenticated by the ed25519 signature of a pinned receipt key.
type MsgSequenceBatch struct {
	Height      uint64
	FromSeq     uint64
	ToSeq       uint64
	PrevSeqHash []byte
	LastSeqHash []byte
	TxRoot      []byte
	KeyId       []byte
	Sig         []byte
}

// ValidateBasic checks the field lengths and the range shape. An empty block
// carries ToSeq = FromSeq - 1, LastSeqHash = PrevSeqHash and the empty root.
func (m MsgSequenceBatch) ValidateBasic() error {
	if err := m.validateSigned(); err != nil {
		return err
	}
	if len(m.Sig) != SignatureLength {
		return ErrInvalidSequenceBatch.Wrapf("sig is %d bytes, want %d", len(m.Sig), SignatureLength)
	}
	return nil
}

// validateSigned checks every field the signature covers, and keyId.
func (m MsgSequenceBatch) validateSigned() error {
	if m.Height == 0 {
		return ErrInvalidSequenceBatch.Wrap("height is zero")
	}
	if m.FromSeq == 0 {
		return ErrInvalidSequenceBatch.Wrap("fromSeq is zero")
	}
	if m.ToSeq < m.FromSeq-1 {
		return ErrInvalidSequenceBatch.Wrapf("toSeq %d below fromSeq %d - 1", m.ToSeq, m.FromSeq)
	}
	for _, field := range []struct {
		name   string
		value  []byte
		length int
	}{
		{"prevSeqHash", m.PrevSeqHash, HashLength},
		{"lastSeqHash", m.LastSeqHash, HashLength},
		{"txRoot", m.TxRoot, HashLength},
		{"keyId", m.KeyId, KeyIDLength},
	} {
		if len(field.value) != field.length {
			return ErrInvalidSequenceBatch.Wrapf("%s is %d bytes, want %d", field.name, len(field.value), field.length)
		}
	}
	if m.IsEmpty() {
		if string(m.LastSeqHash) != string(m.PrevSeqHash) {
			return ErrInvalidSequenceBatch.Wrap("empty block lastSeqHash differs from prevSeqHash")
		}
		if string(m.TxRoot) != string(EmptyTxRoot[:]) {
			return ErrInvalidSequenceBatch.Wrap("empty block txRoot is not the empty root")
		}
	}
	return nil
}

// IsEmpty reports whether the batch covers no sequence position.
func (m MsgSequenceBatch) IsEmpty() bool { return m.ToSeq == m.FromSeq-1 }

// SignBytes returns the digest the receipt key signs:
// sha256(domain || chain-id || u64be height || u64be fromSeq || u64be toSeq ||
// prevSeqHash || lastSeqHash || txRoot).
func (m MsgSequenceBatch) SignBytes(chainID string) ([HashLength]byte, error) {
	if chainID == "" {
		return [HashLength]byte{}, ErrInvalidSequenceBatch.Wrap("chain-id is empty")
	}
	if err := m.validateSigned(); err != nil {
		return [HashLength]byte{}, err
	}
	preimage := make([]byte, 0, len(SequenceBatchDomain)+len(chainID)+3*8+3*HashLength)
	preimage = append(preimage, SequenceBatchDomain...)
	preimage = append(preimage, chainID...)
	preimage = binary.BigEndian.AppendUint64(preimage, m.Height)
	preimage = binary.BigEndian.AppendUint64(preimage, m.FromSeq)
	preimage = binary.BigEndian.AppendUint64(preimage, m.ToSeq)
	preimage = append(preimage, m.PrevSeqHash...)
	preimage = append(preimage, m.LastSeqHash...)
	preimage = append(preimage, m.TxRoot...)
	return sha256.Sum256(preimage), nil
}

// VerifySignature checks Sig as a strict ed25519 signature by the receipt
// key publicKey over SignBytes(chainID).
func (m MsgSequenceBatch) VerifySignature(chainID string, publicKey [32]byte) error {
	if err := m.ValidateBasic(); err != nil {
		return err
	}
	digest, err := m.SignBytes(chainID)
	if err != nil {
		return err
	}
	var signature [SignatureLength]byte
	copy(signature[:], m.Sig)
	if err := verify.Ed25519Digest(publicKey, signature, digest); err != nil {
		return ErrBadSignature.Wrap(err.Error())
	}
	return nil
}
