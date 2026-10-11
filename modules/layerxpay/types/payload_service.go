package types

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const (
	ServiceMaxDeliverables = 16
	ServiceExecutionBytes  = 416
	serviceCompleteBPS     = 10000
)

var (
	errServiceNonCanonical    = errors.New("LXP_NON_CANONICAL")
	errServiceLengthLimit     = errors.New("LXP_LENGTH_LIMIT")
	errServiceDuplicateEntry  = errors.New("LXP_DUPLICATE_ENTRY")
	errServiceParameterBounds = errors.New("LXP_PARAMETER_BOUNDS")
)

type ServiceDeliverable struct {
	Hash                  [32]byte
	ArtifactSize          uint64
	AvailabilityReference [32]byte
}

func EncodeServiceOffer(offerID, assetID [32]byte, price codec.U128, termsHash, deliverableSpecHash [32]byte, deliveryDeadline, acceptanceWindow, disputeWindow uint64, defaultOutcome uint8, offerExpiry uint64) ([]byte, error) {
	if !serviceNonzero(offerID, assetID, termsHash, deliverableSpecHash) || price.IsZero() || deliveryDeadline == 0 || acceptanceWindow == 0 || disputeWindow == 0 || defaultOutcome < 1 || defaultOutcome > 2 || offerExpiry == 0 {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(179)
	copy(payload[2:34], offerID[:])
	copy(payload[34:66], assetID[:])
	amount := price.Bytes()
	copy(payload[66:82], amount[:])
	copy(payload[82:114], termsHash[:])
	copy(payload[114:146], deliverableSpecHash[:])
	binary.BigEndian.PutUint64(payload[146:154], deliveryDeadline)
	binary.BigEndian.PutUint64(payload[154:162], acceptanceWindow)
	binary.BigEndian.PutUint64(payload[162:170], disputeWindow)
	payload[170] = defaultOutcome
	binary.BigEndian.PutUint64(payload[171:179], offerExpiry)
	return payload, nil
}

func EncodeServiceIdentifier(identifier [32]byte) ([]byte, error) {
	if !serviceNonzero(identifier) {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(34)
	copy(payload[2:], identifier[:])
	return payload, nil
}

func EncodeServicePropose(agreementID, offerID, termsHash, escrowID [32]byte) ([]byte, error) {
	if !serviceNonzero(agreementID, offerID, termsHash, escrowID) {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(130)
	copy(payload[2:34], agreementID[:])
	copy(payload[34:66], offerID[:])
	copy(payload[66:98], termsHash[:])
	copy(payload[98:130], escrowID[:])
	return payload, nil
}

func EncodeServiceCommit(commitmentID, agreementID, taskHash, escrowID [32]byte, deadline, resourceBound uint64) ([]byte, error) {
	if !serviceNonzero(commitmentID, agreementID, taskHash) || deadline == 0 || resourceBound == 0 {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(146)
	copy(payload[2:34], commitmentID[:])
	copy(payload[34:66], agreementID[:])
	copy(payload[66:98], taskHash[:])
	copy(payload[98:130], escrowID[:])
	binary.BigEndian.PutUint64(payload[130:138], deadline)
	binary.BigEndian.PutUint64(payload[138:146], resourceBound)
	return payload, nil
}

func EncodeServiceAbandon(commitmentID [32]byte, reason uint16) ([]byte, error) {
	if !serviceNonzero(commitmentID) || reason == 0 {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(36)
	copy(payload[2:34], commitmentID[:])
	binary.BigEndian.PutUint16(payload[34:36], reason)
	return payload, nil
}

func EncodeServiceProgress(reportID, commitmentID, noteHash, availabilityReference [32]byte, progressBPS uint32) ([]byte, error) {
	if !serviceNonzero(reportID, commitmentID, noteHash, availabilityReference) || progressBPS == 0 || progressBPS > serviceCompleteBPS {
		return nil, errServiceParameterBounds
	}
	payload := servicePayload(134)
	copy(payload[2:34], reportID[:])
	copy(payload[34:66], commitmentID[:])
	copy(payload[66:98], noteHash[:])
	copy(payload[98:130], availabilityReference[:])
	binary.BigEndian.PutUint32(payload[130:134], progressBPS)
	return payload, nil
}

func EncodeServiceDeliver(deliveryID, agreementID [32]byte, deliverables []ServiceDeliverable) ([]byte, error) {
	if len(deliverables) == 0 || len(deliverables) > ServiceMaxDeliverables {
		return nil, errServiceLengthLimit
	}
	if !serviceNonzero(deliveryID, agreementID) {
		return nil, errServiceNonCanonical
	}
	for _, item := range deliverables {
		if !serviceNonzero(item.Hash, item.AvailabilityReference) || item.ArtifactSize == 0 {
			return nil, errServiceNonCanonical
		}
	}
	payload := servicePayload(67 + 72*len(deliverables))
	copy(payload[2:34], deliveryID[:])
	copy(payload[34:66], agreementID[:])
	payload[66] = byte(len(deliverables))
	for i, item := range deliverables {
		offset := 67 + 72*i
		copy(payload[offset:offset+32], item.Hash[:])
		binary.BigEndian.PutUint64(payload[offset+32:offset+40], item.ArtifactSize)
		copy(payload[offset+40:offset+72], item.AvailabilityReference[:])
	}
	return payload, nil
}

func EncodeServiceReject(agreementID [32]byte, reason uint16, contestedHashes [][32]byte) ([]byte, error) {
	hashes, err := serviceHashes(contestedHashes)
	if err != nil {
		return nil, err
	}
	if !serviceNonzero(agreementID) || reason == 0 {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(37 + 32*len(hashes))
	copy(payload[2:34], agreementID[:])
	binary.BigEndian.PutUint16(payload[34:36], reason)
	payload[36] = byte(len(hashes))
	for i, hash := range hashes {
		copy(payload[37+32*i:69+32*i], hash[:])
	}
	return payload, nil
}

func EncodeServiceDisputeOpen(disputeID, agreementID [32]byte, evidenceHashes [][32]byte) ([]byte, error) {
	hashes, err := serviceHashes(evidenceHashes)
	if err != nil {
		return nil, err
	}
	if !serviceNonzero(disputeID, agreementID) {
		return nil, errServiceNonCanonical
	}
	payload := servicePayload(67 + 32*len(hashes))
	copy(payload[2:34], disputeID[:])
	copy(payload[34:66], agreementID[:])
	payload[66] = byte(len(hashes))
	for i, hash := range hashes {
		copy(payload[67+32*i:99+32*i], hash[:])
	}
	return payload, nil
}

func EncodeServiceDisputeResolve(disputeID [32]byte, ruling uint16, providerBPS uint32, escrowResolutionID [32]byte) ([]byte, error) {
	if !serviceNonzero(disputeID, escrowResolutionID) || ruling == 0 || providerBPS > serviceCompleteBPS {
		return nil, errServiceParameterBounds
	}
	payload := servicePayload(72)
	copy(payload[2:34], disputeID[:])
	binary.BigEndian.PutUint16(payload[34:36], ruling)
	binary.BigEndian.PutUint32(payload[36:40], providerBPS)
	copy(payload[40:72], escrowResolutionID[:])
	return payload, nil
}

func EncodeServiceToolExecAttest(execution []byte) ([]byte, error) {
	if len(execution) != ServiceExecutionBytes {
		return nil, errServiceNonCanonical
	}
	return bytes.Clone(execution), nil
}

func servicePayload(length int) []byte {
	payload := make([]byte, length)
	payload[1] = 1
	return payload
}

func serviceNonzero(values ...[32]byte) bool {
	for _, value := range values {
		if value == ([32]byte{}) {
			return false
		}
	}
	return true
}

func serviceHashes(hashes [][32]byte) ([][32]byte, error) {
	if len(hashes) == 0 || len(hashes) > ServiceMaxDeliverables {
		return nil, errServiceLengthLimit
	}
	for i, hash := range hashes {
		if !serviceNonzero(hash) {
			return nil, errServiceNonCanonical
		}
		for _, previous := range hashes[:i] {
			if hash == previous {
				return nil, errServiceDuplicateEntry
			}
		}
	}
	ordered := slices.Clone(hashes)
	slices.SortFunc(ordered, func(a, b [32]byte) int {
		return bytes.Compare(a[:], b[:])
	})
	return ordered, nil
}
