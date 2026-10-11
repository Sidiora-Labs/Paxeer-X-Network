package keeper

import (
	"bytes"
	"errors"
	"math/big"

	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
)

var (
	errProgramAttestationThreshold = errors.New("LXP_ATTESTATION_THRESHOLD")
	errProgramBadSignature         = errors.New("LXP_BAD_SIGNATURE")
	errProgramDuplicateSigner      = errors.New("LXP_AUTH_DUPLICATE_SIGNER")
	errProgramUnsortedSequence     = errors.New("LXP_UNSORTED_SEQUENCE")
	errProgramInvalidAttestation   = errors.New("LXP_INVALID_ATTESTATION")
	errProgramNonCanonical         = errors.New("LXP_NON_CANONICAL")
)

func verifyProgramSignatures(set types.AttestorSet, observation types.Observation) ([]types.Address20, error) {
	if err := set.Validate(); err != nil || len(set.Attestors) == 0 ||
		len(set.Attestors) > types.ObservationMaxSignatures {
		return nil, errProgramAttestationThreshold
	}
	if len(observation.Signatures) < int(set.Threshold) {
		return nil, errProgramAttestationThreshold
	}
	if len(observation.Signatures) > types.ObservationMaxSignatures {
		return nil, errProgramNonCanonical
	}
	attested := types.Digest(types.Attestation{
		Origin:        types.OriginProgram,
		NetworkID:     new(big.Int).SetUint64(uint64(observation.NetworkID)),
		Requester:     observation.ProgramID,
		RequestID:     observation.RequestID,
		Kind:          observation.Kind,
		PayloadHash:   observation.PayloadHash,
		ContentDigest: observation.ContentDigest,
		ResponseHash:  types.Keccak(observation.Response),
		FullLength:    observation.FullLength,
	})
	signers := make([]types.Address20, 0, len(observation.Signatures))
	for index, signature := range observation.Signatures {
		recovered, err := bridgetypes.RecoverSigner(bridgetypes.Hash32(attested), signature)
		if err != nil {
			return nil, errProgramBadSignature
		}
		signer := types.Address20(recovered)
		if index > 0 {
			order := bytes.Compare(signers[index-1][:], signer[:])
			if order == 0 {
				return nil, errProgramDuplicateSigner
			}
			if order > 0 {
				return nil, errProgramUnsortedSequence
			}
		}
		if _, found := set.Find(signer); !found {
			return nil, errProgramInvalidAttestation
		}
		signers = append(signers, signer)
	}
	return signers, nil
}
