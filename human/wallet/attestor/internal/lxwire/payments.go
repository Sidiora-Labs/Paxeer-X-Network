package lxwire

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	GrantDomain   = "LXP:GRANT:v1"
	ReceiveDomain = "LXP:RECEIVE:v1"
)

var (
	ErrGrant   = errors.New("lxwire: malformed 402 grant")
	ErrReceive = errors.New("lxwire: malformed 402 receive")
)

type Grant struct {
	From               [32]byte
	Recipient          [32]byte
	Asset              [32]byte
	PerDrawMaximum     Uint128
	Allowance          Uint128
	Recurring          bool
	WindowLength       uint64
	Expiration         uint64
	PurposeHash        [32]byte
	HasReference       bool
	ReferenceHash      [32]byte
	RevocationSequence uint64
	PublicKey          [32]byte
}

type Receive struct {
	From              [32]byte
	To                [32]byte
	Asset             [32]byte
	Amount            Uint128
	Grant             [32]byte
	Sequence          uint64
	IdempotencyKey    [32]byte
	ContextHash       [32]byte
	AuthorizationKind uint8
	Controller        [32]byte
	SignedContextHash [32]byte
	NetworkID         uint32
	ProtocolVersion   uint16
}

func appendBool(out []byte, value bool) []byte {
	if value {
		return append(out, 1)
	}
	return append(out, 0)
}

func appendUint128(out []byte, value Uint128) []byte {
	b := value.Bytes()
	return append(out, b[:]...)
}

func (g *Grant) fields() []byte {
	out := make([]byte, 0, 250)
	out = append(out, g.From[:]...)
	out = append(out, g.Recipient[:]...)
	out = append(out, g.Asset[:]...)
	out = appendUint128(out, g.PerDrawMaximum)
	out = appendUint128(out, g.Allowance)
	out = appendBool(out, g.Recurring)
	out = binary.BigEndian.AppendUint64(out, g.WindowLength)
	out = binary.BigEndian.AppendUint64(out, g.Expiration)
	out = append(out, g.PurposeHash[:]...)
	out = appendBool(out, g.HasReference)
	out = append(out, g.ReferenceHash[:]...)
	out = binary.BigEndian.AppendUint64(out, g.RevocationSequence)
	return append(out, g.PublicKey[:]...)
}

func GrantPreimage(g Grant) ([32]byte, error) {
	var out [32]byte
	var zero [32]byte
	if g.PerDrawMaximum.IsZero() || g.Allowance.IsZero() || g.Expiration == 0 ||
		g.Recurring != (g.WindowLength != 0) || g.Recipient == zero || g.Asset == zero || g.PurposeHash == zero {
		return out, ErrGrant
	}
	h := sha256.New()
	h.Write([]byte(domainAuthorityHash))
	h.Write([]byte(GrantDomain))
	h.Write(g.fields())
	h.Sum(out[:0])
	return out, nil
}

func GrantContextHash(g Grant) [32]byte {
	h := sha256.New()
	h.Write([]byte(domainContextHash))
	h.Write(g.PurposeHash[:])
	if g.HasReference {
		h.Write(g.ReferenceHash[:])
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func ReceivePreimage(r Receive) ([32]byte, error) {
	var out [32]byte
	if r.From == r.To || r.Amount.IsZero() || r.AuthorizationKind < 1 || r.AuthorizationKind > 6 ||
		r.Controller != r.To || r.SignedContextHash != r.ContextHash || r.NetworkID == 0 ||
		r.ProtocolVersion < LegacyProtocolVersion || r.ProtocolVersion > MaxProtocolVersion {
		return out, ErrReceive
	}
	message := make([]byte, 0, 512)
	message = append(message, ReceiveDomain...)
	message = append(message, r.From[:]...)
	message = append(message, r.To[:]...)
	message = append(message, r.Asset[:]...)
	message = appendUint128(message, r.Amount)
	message = append(message, r.Grant[:]...)
	message = binary.BigEndian.AppendUint64(message, r.Sequence)
	message = append(message, r.IdempotencyKey[:]...)
	message = append(message, r.ContextHash[:]...)
	message = append(message, r.AuthorizationKind)
	message = append(message, r.Controller[:]...)
	message = append(message, r.SignedContextHash[:]...)
	message = binary.BigEndian.AppendUint32(message, r.NetworkID)
	message = binary.BigEndian.AppendUint16(message, r.ProtocolVersion)
	h := sha256.New()
	h.Write([]byte(domainSignaturePreimage))
	h.Write(message)
	h.Sum(out[:0])
	return out, nil
}
