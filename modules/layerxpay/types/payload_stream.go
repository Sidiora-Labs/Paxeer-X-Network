package types

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const (
	StreamMaxMeterAuthorities          = 8
	StreamOpenPayloadFixedBytes        = 204
	StreamOpenPayloadMaxBytes          = StreamOpenPayloadFixedBytes + StreamMaxMeterAuthorities*32
	StreamTopUpPayloadBytes            = 50
	StreamMeterPayloadBytes            = 138
	StreamKeyedPayloadBytes            = 66
	StreamRateChangePayloadBytes       = 82
	StreamIDPayloadBytes               = 34
	StreamModeTime               uint8 = 1
	StreamModeMetered            uint8 = 2

	streamPayloadVersion = 1
)

var errStreamNonCanonical = errors.New("LXP_NON_CANONICAL")

type StreamOpen struct {
	StreamID         [32]byte
	StreamAccount    [32]byte
	Recipient        [32]byte
	AssetID          [32]byte
	Mode             uint8
	Rate             codec.U128
	RateUnit         uint64
	StartTimestamp   uint64
	EndTimestamp     uint64
	TotalCap         codec.U128
	InitialFunding   codec.U128
	MeterAuthorities [][32]byte
}

func EncodeStreamOpen(open StreamOpen) ([]byte, error) {
	if err := streamOpenCheck(open); err != nil {
		return nil, err
	}
	payload := streamPayload(StreamOpenPayloadFixedBytes + 32*len(open.MeterAuthorities))
	copy(payload[2:34], open.StreamID[:])
	copy(payload[34:66], open.StreamAccount[:])
	copy(payload[66:98], open.Recipient[:])
	copy(payload[98:130], open.AssetID[:])
	payload[130] = open.Mode
	rate := open.Rate.Bytes()
	copy(payload[131:147], rate[:])
	binary.BigEndian.PutUint64(payload[147:155], open.RateUnit)
	binary.BigEndian.PutUint64(payload[155:163], open.StartTimestamp)
	binary.BigEndian.PutUint64(payload[163:171], open.EndTimestamp)
	totalCap := open.TotalCap.Bytes()
	copy(payload[171:187], totalCap[:])
	funding := open.InitialFunding.Bytes()
	copy(payload[187:203], funding[:])
	payload[203] = byte(len(open.MeterAuthorities))
	for i, authority := range open.MeterAuthorities {
		copy(payload[StreamOpenPayloadFixedBytes+32*i:], authority[:])
	}
	return payload, nil
}

func EncodeStreamTopUp(streamID [32]byte, amount codec.U128) ([]byte, error) {
	if !budgetNonzero(streamID) || amount.IsZero() {
		return nil, errStreamNonCanonical
	}
	payload := streamPayload(StreamTopUpPayloadBytes)
	copy(payload[2:34], streamID[:])
	value := amount.Bytes()
	copy(payload[34:50], value[:])
	return payload, nil
}

func EncodeStreamMeter(streamID [32]byte, cumulativeReading uint64, authorityKey [32]byte, signature [64]byte) ([]byte, error) {
	if !budgetNonzero(streamID, authorityKey) || signature == ([64]byte{}) {
		return nil, errStreamNonCanonical
	}
	payload := streamPayload(StreamMeterPayloadBytes)
	copy(payload[2:34], streamID[:])
	binary.BigEndian.PutUint64(payload[34:42], cumulativeReading)
	copy(payload[42:74], authorityKey[:])
	copy(payload[74:138], signature[:])
	return payload, nil
}

func EncodeStreamKeyed(streamID, idempotencyKey [32]byte) ([]byte, error) {
	if !budgetNonzero(streamID, idempotencyKey) {
		return nil, errStreamNonCanonical
	}
	payload := streamPayload(StreamKeyedPayloadBytes)
	copy(payload[2:34], streamID[:])
	copy(payload[34:66], idempotencyKey[:])
	return payload, nil
}

func EncodeStreamID(streamID [32]byte) ([]byte, error) {
	if !budgetNonzero(streamID) {
		return nil, errStreamNonCanonical
	}
	payload := streamPayload(StreamIDPayloadBytes)
	copy(payload[2:34], streamID[:])
	return payload, nil
}

func EncodeStreamRateChange(streamID [32]byte, newRate codec.U128, idempotencyKey [32]byte) ([]byte, error) {
	if !budgetNonzero(streamID, idempotencyKey) || newRate.IsZero() {
		return nil, errStreamNonCanonical
	}
	payload := streamPayload(StreamRateChangePayloadBytes)
	copy(payload[2:34], streamID[:])
	rate := newRate.Bytes()
	copy(payload[34:50], rate[:])
	copy(payload[50:82], idempotencyKey[:])
	return payload, nil
}

func streamOpenCheck(open StreamOpen) error {
	count := len(open.MeterAuthorities)
	if !budgetNonzero(open.StreamID, open.StreamAccount, open.Recipient, open.AssetID) ||
		open.StreamAccount == open.Recipient ||
		open.Rate.IsZero() || open.RateUnit == 0 || open.StartTimestamp == 0 ||
		(open.EndTimestamp != 0 && open.EndTimestamp <= open.StartTimestamp) ||
		open.TotalCap.IsZero() || open.InitialFunding.IsZero() ||
		count > StreamMaxMeterAuthorities ||
		!(open.Mode == StreamModeTime && count == 0 || open.Mode == StreamModeMetered && count != 0) {
		return errStreamNonCanonical
	}
	for i, authority := range open.MeterAuthorities {
		if !budgetNonzero(authority) || i != 0 && bytes.Compare(open.MeterAuthorities[i-1][:], authority[:]) >= 0 {
			return errStreamNonCanonical
		}
	}
	return nil
}

func streamPayload(length int) []byte {
	payload := make([]byte, length)
	payload[1] = streamPayloadVersion
	return payload
}
