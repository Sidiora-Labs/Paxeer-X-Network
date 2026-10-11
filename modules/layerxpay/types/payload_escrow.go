package types

import (
	"encoding/binary"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const (
	EscrowOpenPayloadBytes           = 288
	EscrowCapturePayloadBytes        = 80
	EscrowReleasePayloadBytes        = 64
	EscrowDisputeOpenPayloadBytes    = 32
	EscrowDisputeResolvePayloadBytes = 68
)

func EncodeEscrowOpen(escrowID, owner, escrowAccount, beneficiary, arbiter, assetID [32]byte, amount codec.U128, expiryMs, disputeWindowMs uint64, termsHash, agreementReference [32]byte) []byte {
	payload := make([]byte, EscrowOpenPayloadBytes)
	copy(payload[:32], escrowID[:])
	copy(payload[32:64], owner[:])
	copy(payload[64:96], escrowAccount[:])
	copy(payload[96:128], beneficiary[:])
	copy(payload[128:160], arbiter[:])
	copy(payload[160:192], assetID[:])
	value := amount.Bytes()
	copy(payload[192:208], value[:])
	binary.BigEndian.PutUint64(payload[208:216], expiryMs)
	binary.BigEndian.PutUint64(payload[216:224], disputeWindowMs)
	copy(payload[224:256], termsHash[:])
	copy(payload[256:288], agreementReference[:])
	return payload
}

func EncodeEscrowCapture(escrowID [32]byte, amount codec.U128, idempotencyKey [32]byte) []byte {
	payload := make([]byte, EscrowCapturePayloadBytes)
	copy(payload[:32], escrowID[:])
	value := amount.Bytes()
	copy(payload[32:48], value[:])
	copy(payload[48:80], idempotencyKey[:])
	return payload
}

func EncodeEscrowRelease(escrowID, idempotencyKey [32]byte) []byte {
	payload := make([]byte, EscrowReleasePayloadBytes)
	copy(payload[:32], escrowID[:])
	copy(payload[32:64], idempotencyKey[:])
	return payload
}

func EncodeEscrowDisputeOpen(escrowID [32]byte) []byte {
	payload := make([]byte, EscrowDisputeOpenPayloadBytes)
	copy(payload, escrowID[:])
	return payload
}

func EncodeEscrowDisputeResolve(escrowID [32]byte, beneficiaryBps uint32, idempotencyKey [32]byte) []byte {
	payload := make([]byte, EscrowDisputeResolvePayloadBytes)
	copy(payload[:32], escrowID[:])
	binary.BigEndian.PutUint32(payload[32:36], beneficiaryBps)
	copy(payload[36:68], idempotencyKey[:])
	return payload
}
