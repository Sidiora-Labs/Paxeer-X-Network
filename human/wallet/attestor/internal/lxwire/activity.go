package lxwire

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	LegacyProtocolVersion          uint16 = 1
	ProtocolVersion                uint16 = 2
	StateCommitmentProtocolVersion uint16 = 3
	MaxProtocolVersion                    = StateCommitmentProtocolVersion

	MaxMessageBytes   = 1_048_576
	MaxDIDBytes       = 255
	MaxAuthorityBytes = 524_288
	MaxPayloadBytes   = 524_288
	MaxSignatureBytes = 128

	activityStructureTag uint16 = 0x1001
	signedFieldCount     uint8  = 12
	unsignedFieldCount   uint8  = 11
)

const (
	domainPayloadHash       = "LXP/v1/payload-hash\x00"
	domainSignaturePreimage = "LXP/v1/signature-preimage\x00"
	domainAuthorityHash     = "LXP/v1/authority-hash\x00"
	domainContextHash       = "LXP/v1/context-hash\x00"
)

type Reason string

const (
	ReasonTruncated          Reason = "truncated"
	ReasonLengthLimit        Reason = "length_limit"
	ReasonNonCanonical       Reason = "non_canonical"
	ReasonInvalidTag         Reason = "invalid_tag"
	ReasonVersionUnsupported Reason = "version_unsupported"
	ReasonUnknownField       Reason = "unknown_field"
	ReasonTrailingBytes      Reason = "trailing_bytes"
	ReasonMalformedEnvelope  Reason = "malformed_envelope"
	ReasonUnknownActivity    Reason = "unknown_activity"
)

type Error struct {
	Reason Reason
	Offset int
}

func (e *Error) Error() string {
	return fmt.Sprintf("lxwire: %s at byte %d", e.Reason, e.Offset)
}

func wireError(reason Reason, offset int) error {
	return &Error{Reason: reason, Offset: offset}
}

type Uint128 struct {
	Hi uint64
	Lo uint64
}

func (v Uint128) Bytes() [16]byte {
	var out [16]byte
	binary.BigEndian.PutUint64(out[:8], v.Hi)
	binary.BigEndian.PutUint64(out[8:], v.Lo)
	return out
}

func (v Uint128) IsZero() bool {
	return v.Hi == 0 && v.Lo == 0
}

func (v Uint128) Cmp(other Uint128) int {
	switch {
	case v.Hi < other.Hi:
		return -1
	case v.Hi > other.Hi:
		return 1
	case v.Lo < other.Lo:
		return -1
	case v.Lo > other.Lo:
		return 1
	}
	return 0
}

type ModuleID uint16

const (
	ModuleAsset      ModuleID = 1
	ModuleEscrow     ModuleID = 2
	ModuleBudget     ModuleID = 3
	ModuleStream     ModuleID = 4
	ModuleService    ModuleID = 5
	ModulePerps      ModuleID = 6
	ModuleGovernance ModuleID = 7
	ModuleBridge     ModuleID = 8
	ModulePrograms   ModuleID = 9
	ModuleSpot       ModuleID = 10
	ModuleWeb        ModuleID = 11
)

func (m ModuleID) Known() bool {
	return m >= ModuleAsset && m <= ModuleWeb
}

type ActivityType uint32

func NewActivityType(module ModuleID, ordinal uint16) (ActivityType, error) {
	if !module.Known() || ordinal == 0 {
		return 0, wireError(ReasonUnknownActivity, 0)
	}
	return ActivityType(uint32(module)<<16 | uint32(ordinal)), nil
}

func (t ActivityType) Module() ModuleID {
	return ModuleID(uint32(t) >> 16)
}

func (t ActivityType) Ordinal() uint16 {
	return uint16(uint32(t) & 0xffff)
}

func (t ActivityType) Valid() bool {
	return t.Module().Known() && t.Ordinal() != 0
}

type Registry struct {
	declared map[ActivityType]struct{}
}

func NewRegistry(types ...ActivityType) (*Registry, error) {
	registry := &Registry{declared: make(map[ActivityType]struct{}, len(types))}
	for _, kind := range types {
		if !kind.Valid() {
			return nil, wireError(ReasonUnknownActivity, 0)
		}
		registry.declared[kind] = struct{}{}
	}
	return registry, nil
}

func (r *Registry) Declares(kind ActivityType) bool {
	if r == nil {
		return false
	}
	_, ok := r.declared[kind]
	return ok
}

type AuthorityKind uint8

const (
	AuthorityMalformed AuthorityKind = iota
	AuthorityOwner
	AuthorityGrant
)

type Activity struct {
	ProtocolVersion uint16
	NetworkID       uint32
	Type            ActivityType
	ActorDID        []byte
	Authority       []byte
	AccountSequence uint64
	NotBefore       uint64
	NotAfter        uint64
	IdempotencyKey  [32]byte
	FeeLimit        Uint128
	PayloadHash     [32]byte
	Payload         []byte
	Signed          bool
	Signature       []byte
}

func (a *Activity) AuthorityKind(ownerKey [32]byte) AuthorityKind {
	if len(a.Authority) != 32 {
		return AuthorityMalformed
	}
	if bytes.Equal(a.Authority, ownerKey[:]) {
		return AuthorityOwner
	}
	return AuthorityGrant
}

func (a *Activity) AuthorityKey() ([32]byte, bool) {
	var key [32]byte
	if len(a.Authority) != len(key) {
		return key, false
	}
	copy(key[:], a.Authority)
	return key, true
}

func PayloadHash(payload []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(domainPayloadHash))
	h.Write(payload)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func (a *Activity) PayloadHashMatches() bool {
	return PayloadHash(a.Payload) == a.PayloadHash
}

type decoder struct {
	data      []byte
	offset    int
	allocated int
}

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.data)-d.offset {
		return nil, wireError(ReasonTruncated, d.offset)
	}
	value := d.data[d.offset : d.offset+n]
	d.offset += n
	return value, nil
}

func (d *decoder) u8() (uint8, error) {
	b, err := d.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (d *decoder) u16() (uint16, error) {
	b, err := d.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (d *decoder) u32() (uint32, error) {
	b, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (d *decoder) u64() (uint64, error) {
	b, err := d.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

func (d *decoder) u128() (Uint128, error) {
	b, err := d.take(16)
	if err != nil {
		return Uint128{}, err
	}
	return Uint128{Hi: binary.BigEndian.Uint64(b[:8]), Lo: binary.BigEndian.Uint64(b[8:])}, nil
}

func (d *decoder) bytes(maximum int) ([]byte, error) {
	lengthOffset := d.offset
	length, err := d.u32()
	if err != nil {
		return nil, err
	}
	if uint64(length) > uint64(maximum) {
		return nil, wireError(ReasonLengthLimit, lengthOffset)
	}
	return d.take(int(length))
}

func (d *decoder) bytesOwned(maximum int) ([]byte, error) {
	offset := d.offset
	value, err := d.bytes(maximum)
	if err != nil {
		return nil, err
	}
	if d.allocated+len(value) > MaxMessageBytes {
		return nil, wireError(ReasonLengthLimit, offset)
	}
	d.allocated += len(value)
	return append([]byte{}, value...), nil
}

func (d *decoder) fixed32() ([32]byte, error) {
	var out [32]byte
	offset := d.offset
	value, err := d.bytes(32)
	if err != nil {
		return out, err
	}
	if len(value) != len(out) {
		return out, wireError(ReasonNonCanonical, offset)
	}
	copy(out[:], value)
	return out, nil
}

func (d *decoder) field(expected uint8) error {
	offset := d.offset
	actual, err := d.u8()
	if err != nil || actual > signedFieldCount || actual != expected {
		return wireError(ReasonUnknownField, offset)
	}
	return nil
}

func (d *decoder) finish() error {
	if d.offset != len(d.data) {
		return wireError(ReasonTrailingBytes, d.offset)
	}
	return nil
}

func decodeActivity(data []byte, registry *Registry, signed bool) (*Activity, error) {
	d := &decoder{data: data}
	headerOffset := d.offset
	envelopeVersion, err := d.u16()
	if err != nil {
		return nil, err
	}
	tag, err := d.u16()
	if err != nil {
		return nil, err
	}
	if envelopeVersion < LegacyProtocolVersion || envelopeVersion > MaxProtocolVersion || tag != activityStructureTag {
		return nil, wireError(ReasonVersionUnsupported, headerOffset)
	}
	expected := unsignedFieldCount
	if signed {
		expected = signedFieldCount
	}
	count, err := d.u8()
	if err != nil {
		return nil, err
	}
	if count != expected {
		return nil, wireError(ReasonMalformedEnvelope, d.offset)
	}
	a := &Activity{Signed: signed}
	if err := d.field(1); err != nil {
		return nil, err
	}
	if a.ProtocolVersion, err = d.u16(); err != nil {
		return nil, err
	}
	if a.ProtocolVersion != envelopeVersion {
		return nil, wireError(ReasonVersionUnsupported, d.offset)
	}
	if err := d.field(2); err != nil {
		return nil, err
	}
	if a.NetworkID, err = d.u32(); err != nil {
		return nil, err
	}
	if err := d.field(3); err != nil {
		return nil, err
	}
	rawType, err := d.u32()
	if err != nil {
		return nil, err
	}
	a.Type = ActivityType(rawType)
	if !a.Type.Valid() || !registry.Declares(a.Type) {
		return nil, wireError(ReasonUnknownActivity, d.offset)
	}
	if err := d.field(4); err != nil {
		return nil, err
	}
	if a.ActorDID, err = d.bytesOwned(MaxDIDBytes); err != nil {
		return nil, err
	}
	if err := d.field(5); err != nil {
		return nil, err
	}
	if a.Authority, err = d.bytesOwned(MaxAuthorityBytes); err != nil {
		return nil, err
	}
	if err := d.field(6); err != nil {
		return nil, err
	}
	if a.AccountSequence, err = d.u64(); err != nil {
		return nil, err
	}
	if err := d.field(7); err != nil {
		return nil, err
	}
	if a.NotBefore, err = d.u64(); err != nil {
		return nil, err
	}
	if a.NotAfter, err = d.u64(); err != nil {
		return nil, err
	}
	if a.NotAfter < a.NotBefore {
		return nil, wireError(ReasonMalformedEnvelope, d.offset)
	}
	if err := d.field(8); err != nil {
		return nil, err
	}
	if a.IdempotencyKey, err = d.fixed32(); err != nil {
		return nil, err
	}
	if err := d.field(9); err != nil {
		return nil, err
	}
	if a.FeeLimit, err = d.u128(); err != nil {
		return nil, err
	}
	if err := d.field(10); err != nil {
		return nil, err
	}
	if a.PayloadHash, err = d.fixed32(); err != nil {
		return nil, err
	}
	if err := d.field(11); err != nil {
		return nil, err
	}
	if a.Payload, err = d.bytesOwned(MaxPayloadBytes); err != nil {
		return nil, err
	}
	if signed {
		if err := d.field(12); err != nil {
			return nil, err
		}
		if a.Signature, err = d.bytesOwned(MaxSignatureBytes); err != nil {
			return nil, err
		}
	}
	if err := d.finish(); err != nil {
		return nil, err
	}
	return a, nil
}

func DecodeActivity(data []byte, registry *Registry) (*Activity, error) {
	return decodeActivity(data, registry, true)
}

func DecodeUnsignedActivity(data []byte, registry *Registry) (*Activity, error) {
	return decodeActivity(data, registry, false)
}

type encoder struct {
	out []byte
}

func (e *encoder) write(b []byte) error {
	if len(b) > MaxMessageBytes-len(e.out) {
		return wireError(ReasonLengthLimit, len(e.out))
	}
	e.out = append(e.out, b...)
	return nil
}

func (e *encoder) u8(v uint8) error {
	return e.write([]byte{v})
}

func (e *encoder) u16(v uint16) error {
	return e.write(binary.BigEndian.AppendUint16(nil, v))
}

func (e *encoder) u32(v uint32) error {
	return e.write(binary.BigEndian.AppendUint32(nil, v))
}

func (e *encoder) u64(v uint64) error {
	return e.write(binary.BigEndian.AppendUint64(nil, v))
}

func (e *encoder) u128(v Uint128) error {
	b := v.Bytes()
	return e.write(b[:])
}

func (e *encoder) bytes(b []byte, maximum int) error {
	offset := len(e.out)
	if len(b) > maximum || 4+len(b) > MaxMessageBytes-offset {
		return wireError(ReasonLengthLimit, offset)
	}
	if err := e.u32(uint32(len(b))); err != nil {
		return err
	}
	return e.write(b)
}

func (e *encoder) tag(field uint8) error {
	return e.u8(field)
}

func encodeActivity(a *Activity, signed bool) ([]byte, error) {
	e := &encoder{}
	if a.ProtocolVersion < LegacyProtocolVersion || a.ProtocolVersion > MaxProtocolVersion {
		return nil, wireError(ReasonVersionUnsupported, 0)
	}
	if !a.Type.Valid() {
		return nil, wireError(ReasonUnknownActivity, 0)
	}
	if a.NotAfter < a.NotBefore {
		return nil, wireError(ReasonMalformedEnvelope, 0)
	}
	if signed && !a.Signed {
		return nil, wireError(ReasonMalformedEnvelope, 0)
	}
	count := unsignedFieldCount
	if signed {
		count = signedFieldCount
	}
	steps := []func() error{
		func() error { return e.u16(a.ProtocolVersion) },
		func() error { return e.u16(activityStructureTag) },
		func() error { return e.u8(count) },
		func() error { return e.tag(1) },
		func() error { return e.u16(a.ProtocolVersion) },
		func() error { return e.tag(2) },
		func() error { return e.u32(a.NetworkID) },
		func() error { return e.tag(3) },
		func() error { return e.u32(uint32(a.Type)) },
		func() error { return e.tag(4) },
		func() error { return e.bytes(a.ActorDID, MaxDIDBytes) },
		func() error { return e.tag(5) },
		func() error { return e.bytes(a.Authority, MaxAuthorityBytes) },
		func() error { return e.tag(6) },
		func() error { return e.u64(a.AccountSequence) },
		func() error { return e.tag(7) },
		func() error { return e.u64(a.NotBefore) },
		func() error { return e.u64(a.NotAfter) },
		func() error { return e.tag(8) },
		func() error { return e.bytes(a.IdempotencyKey[:], 32) },
		func() error { return e.tag(9) },
		func() error { return e.u128(a.FeeLimit) },
		func() error { return e.tag(10) },
		func() error { return e.bytes(a.PayloadHash[:], 32) },
		func() error { return e.tag(11) },
		func() error { return e.bytes(a.Payload, MaxPayloadBytes) },
	}
	if signed {
		steps = append(steps,
			func() error { return e.tag(12) },
			func() error { return e.bytes(a.Signature, MaxSignatureBytes) },
		)
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return e.out, nil
}

func EncodeActivity(a *Activity) ([]byte, error) {
	return encodeActivity(a, true)
}

func EncodeUnsignedActivity(a *Activity) ([]byte, error) {
	return encodeActivity(a, false)
}

func SignaturePreimage(a *Activity) ([32]byte, error) {
	var out [32]byte
	unsigned, err := EncodeUnsignedActivity(a)
	if err != nil {
		return out, err
	}
	h := sha256.New()
	h.Write([]byte(domainSignaturePreimage))
	h.Write(unsigned)
	h.Sum(out[:0])
	return out, nil
}
