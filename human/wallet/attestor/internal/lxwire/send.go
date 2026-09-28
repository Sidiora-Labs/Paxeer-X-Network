package lxwire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"filippo.io/edwards25519"
)

const (
	SendTag               uint16 = 0x5301
	SendFieldCount        uint16 = 10
	MaxSendConditions            = 8
	MaxSendPayloadBytes          = 512
	SendConditionAfter    uint8  = 1
	SendConditionBefore   uint8  = 2
	OwnerAuthorization    uint8  = 1
	sendAuthorizationTail        = 1 + 32 + 32 + 64 + 32 + 4 + 2
)

var ErrSend = errors.New("lxwire: malformed asset send")

type SendCondition struct {
	Kind      uint8
	Timestamp uint64
}

type Send struct {
	From              [32]byte
	To                [32]byte
	Asset             [32]byte
	Amount            Uint128
	SourceSequence    uint64
	IdempotencyKey    [32]byte
	ExpiresAt         uint64
	ContextHash       [32]byte
	Conditions        []SendCondition
	AuthorizationKind uint8
	Controller        [32]byte
	PublicKey         [32]byte
	Signature         [64]byte
	SignedContextHash [32]byte
	NetworkID         uint32
	ProtocolVersion   uint16
}

func (d *decoder) raw32() ([32]byte, error) {
	var out [32]byte
	b, err := d.take(32)
	if err != nil {
		return out, err
	}
	copy(out[:], b)
	return out, nil
}

func DecodeSend(payload []byte) (*Send, error) {
	if len(payload) > MaxSendPayloadBytes {
		return nil, ErrSend
	}
	d := &decoder{data: payload}
	tag, err := d.u16()
	if err != nil {
		return nil, ErrSend
	}
	count, err := d.u16()
	if err != nil || tag != SendTag || count != SendFieldCount {
		return nil, ErrSend
	}
	s := &Send{}
	if s.From, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.To, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.Asset, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.Amount, err = d.u128(); err != nil {
		return nil, ErrSend
	}
	if s.SourceSequence, err = d.u64(); err != nil {
		return nil, ErrSend
	}
	if s.IdempotencyKey, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.ExpiresAt, err = d.u64(); err != nil {
		return nil, ErrSend
	}
	if s.ContextHash, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	conditions, err := d.u8()
	if err != nil {
		return nil, ErrSend
	}
	if int(conditions) > MaxSendConditions {
		return nil, ErrSend
	}
	for i := 0; i < int(conditions); i++ {
		var c SendCondition
		if c.Kind, err = d.u8(); err != nil {
			return nil, ErrSend
		}
		if c.Kind != SendConditionAfter && c.Kind != SendConditionBefore {
			return nil, ErrSend
		}
		if c.Timestamp, err = d.u64(); err != nil {
			return nil, ErrSend
		}
		s.Conditions = append(s.Conditions, c)
	}
	if s.AuthorizationKind, err = d.u8(); err != nil {
		return nil, ErrSend
	}
	if s.AuthorizationKind < 1 || s.AuthorizationKind > 6 {
		return nil, ErrSend
	}
	if s.Controller, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.PublicKey, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	signature, err := d.take(64)
	if err != nil {
		return nil, ErrSend
	}
	copy(s.Signature[:], signature)
	if s.SignedContextHash, err = d.raw32(); err != nil {
		return nil, ErrSend
	}
	if s.NetworkID, err = d.u32(); err != nil {
		return nil, ErrSend
	}
	if s.ProtocolVersion, err = d.u16(); err != nil {
		return nil, ErrSend
	}
	if err := d.finish(); err != nil {
		return nil, ErrSend
	}
	encoded, err := s.Encode()
	if err != nil || !bytes.Equal(encoded, payload) {
		return nil, ErrSend
	}
	if !s.AuthorizationValid() {
		return nil, ErrSend
	}
	return s, nil
}

func (s *Send) common() ([]byte, error) {
	if s.Amount.IsZero() || s.From == s.To || len(s.Conditions) > MaxSendConditions ||
		s.AuthorizationKind < 1 || s.AuthorizationKind > 6 || s.NetworkID == 0 ||
		s.ProtocolVersion < LegacyProtocolVersion || s.ProtocolVersion > MaxProtocolVersion {
		return nil, ErrSend
	}
	out := make([]byte, 0, 256)
	out = append(out, s.From[:]...)
	out = append(out, s.To[:]...)
	out = append(out, s.Asset[:]...)
	out = appendUint128(out, s.Amount)
	out = binary.BigEndian.AppendUint64(out, s.SourceSequence)
	out = append(out, s.IdempotencyKey[:]...)
	out = binary.BigEndian.AppendUint64(out, s.ExpiresAt)
	out = append(out, s.ContextHash[:]...)
	out = append(out, uint8(len(s.Conditions)))
	for _, c := range s.Conditions {
		if c.Kind != SendConditionAfter && c.Kind != SendConditionBefore {
			return nil, ErrSend
		}
		out = append(out, c.Kind)
		out = binary.BigEndian.AppendUint64(out, c.Timestamp)
	}
	return out, nil
}

func (s *Send) Encode() ([]byte, error) {
	common, err := s.common()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 4+len(common)+sendAuthorizationTail)
	out = binary.BigEndian.AppendUint16(out, SendTag)
	out = binary.BigEndian.AppendUint16(out, SendFieldCount)
	out = append(out, common...)
	out = append(out, s.AuthorizationKind)
	out = append(out, s.Controller[:]...)
	out = append(out, s.PublicKey[:]...)
	out = append(out, s.Signature[:]...)
	out = append(out, s.SignedContextHash[:]...)
	out = binary.BigEndian.AppendUint32(out, s.NetworkID)
	out = binary.BigEndian.AppendUint16(out, s.ProtocolVersion)
	if len(out) > MaxSendPayloadBytes {
		return nil, ErrSend
	}
	return out, nil
}

func (s *Send) AuthorizationMessage() ([]byte, error) {
	common, err := s.common()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 2+len(common)+1+32+32+4+2)
	out = binary.BigEndian.AppendUint16(out, SendTag)
	out = append(out, common...)
	out = append(out, s.AuthorizationKind)
	out = append(out, s.Controller[:]...)
	out = append(out, s.SignedContextHash[:]...)
	out = binary.BigEndian.AppendUint32(out, s.NetworkID)
	return binary.BigEndian.AppendUint16(out, s.ProtocolVersion), nil
}

func (s *Send) AuthorizationDigest() ([32]byte, error) {
	var out [32]byte
	message, err := s.AuthorizationMessage()
	if err != nil {
		return out, err
	}
	h := sha256.New()
	h.Write([]byte(domainSignaturePreimage))
	h.Write(message)
	h.Sum(out[:0])
	return out, nil
}

func strictPoint(encoded []byte) bool {
	point, err := new(edwards25519.Point).SetBytes(encoded)
	if err != nil || !bytes.Equal(point.Bytes(), encoded) {
		return false
	}
	return new(edwards25519.Point).MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) != 1
}

func (s *Send) AuthorizationValid() bool {
	digest, err := s.AuthorizationDigest()
	if err != nil {
		return false
	}
	if !strictPoint(s.PublicKey[:]) || !strictPoint(s.Signature[:32]) {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(s.PublicKey[:]), digest[:], s.Signature[:])
}
