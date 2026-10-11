package types

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	ObservationHeaderBytes   = 146
	ObservationMaxSignatures = 16
	ObservationMaxBytes      = ObservationHeaderBytes + MaxResponseBytes + 1 + ObservationMaxSignatures*SignatureLength
)

var errNonCanonicalObservation = errors.New("LXP_NON_CANONICAL")

type Observation struct {
	Origin        uint8
	NetworkID     uint32
	ProgramID     Hash32
	RequestID     uint64
	Kind          uint8
	PayloadHash   Hash32
	ContentDigest Hash32
	FullLength    uint32
	Response      []byte
	Signatures    [][]byte
}

func EncodeObservation(observation Observation) ([]byte, error) {
	if observation.Origin != OriginProgram || observation.NetworkID == 0 ||
		observation.ProgramID == (Hash32{}) ||
		(observation.Kind != KindFetch && observation.Kind != KindSearch) ||
		len(observation.Response) > MaxResponseBytes ||
		observation.FullLength < uint32(len(observation.Response)) ||
		len(observation.Signatures) == 0 || len(observation.Signatures) > ObservationMaxSignatures {
		return nil, errNonCanonicalObservation
	}
	for _, signature := range observation.Signatures {
		if len(signature) != SignatureLength {
			return nil, errNonCanonicalObservation
		}
	}
	raw := make([]byte, ObservationHeaderBytes+len(observation.Response)+1+len(observation.Signatures)*SignatureLength)
	raw[0] = observation.Origin
	binary.BigEndian.PutUint32(raw[29:33], observation.NetworkID)
	copy(raw[33:65], observation.ProgramID[:])
	binary.BigEndian.PutUint64(raw[65:73], observation.RequestID)
	raw[73] = observation.Kind
	copy(raw[74:106], observation.PayloadHash[:])
	copy(raw[106:138], observation.ContentDigest[:])
	binary.BigEndian.PutUint32(raw[138:142], observation.FullLength)
	binary.BigEndian.PutUint32(raw[142:146], uint32(len(observation.Response)))
	offset := ObservationHeaderBytes + copy(raw[ObservationHeaderBytes:], observation.Response)
	raw[offset] = byte(len(observation.Signatures))
	offset++
	for _, signature := range observation.Signatures {
		offset += copy(raw[offset:], signature)
	}
	return raw, nil
}

func DecodeObservation(raw []byte) (Observation, error) {
	var padding [28]byte
	if len(raw) < ObservationHeaderBytes+1 || len(raw) > ObservationMaxBytes ||
		!bytes.Equal(raw[1:29], padding[:]) {
		return Observation{}, errNonCanonicalObservation
	}
	observation := Observation{
		Origin:     raw[0],
		NetworkID:  binary.BigEndian.Uint32(raw[29:33]),
		RequestID:  binary.BigEndian.Uint64(raw[65:73]),
		Kind:       raw[73],
		FullLength: binary.BigEndian.Uint32(raw[138:142]),
	}
	copy(observation.ProgramID[:], raw[33:65])
	copy(observation.PayloadHash[:], raw[74:106])
	copy(observation.ContentDigest[:], raw[106:138])
	responseLength := binary.BigEndian.Uint32(raw[142:146])
	if responseLength > MaxResponseBytes || len(raw) < ObservationHeaderBytes+int(responseLength)+1 {
		return Observation{}, errNonCanonicalObservation
	}
	offset := ObservationHeaderBytes + int(responseLength)
	signatureCount := int(raw[offset])
	offset++
	if signatureCount == 0 || signatureCount > ObservationMaxSignatures ||
		len(raw) != offset+signatureCount*SignatureLength {
		return Observation{}, errNonCanonicalObservation
	}
	observation.Response = bytes.Clone(raw[ObservationHeaderBytes : offset-1])
	observation.Signatures = make([][]byte, signatureCount)
	for i := range observation.Signatures {
		observation.Signatures[i] = bytes.Clone(raw[offset : offset+SignatureLength])
		offset += SignatureLength
	}
	canonical, err := EncodeObservation(observation)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Observation{}, errNonCanonicalObservation
	}
	return observation, nil
}
