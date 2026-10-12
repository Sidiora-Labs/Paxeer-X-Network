package keeper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/big"

	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
)

const (
	// ProgramAnswerHeaderBytes is part 0 of a committed answer: the content
	// digest, the u32 full length and the u32 response length.
	ProgramAnswerHeaderBytes = 40

	// ProgramAnswerChunkBytes bounds each response chunk, parts 1 and above.
	ProgramAnswerChunkBytes = 1024

	programAnswerPrefix = "web/answer"
)

var (
	errProgramAttestationThreshold = errors.New("LXP_ATTESTATION_THRESHOLD")
	errProgramBadSignature         = errors.New("LXP_BAD_SIGNATURE")
	errProgramDuplicateSigner      = errors.New("LXP_AUTH_DUPLICATE_SIGNER")
	errProgramUnsortedSequence     = errors.New("LXP_UNSORTED_SEQUENCE")
	errProgramInvalidAttestation   = errors.New("LXP_INVALID_ATTESTATION")
	errProgramNonCanonical         = errors.New("LXP_NON_CANONICAL")
	errProgramWrongNetwork         = errors.New("LXP_WRONG_NETWORK")
	errProgramUnknownField         = errors.New("LXP_UNKNOWN_FIELD")
	errProgramContextMismatch      = errors.New("LXP_CONTEXT_MISMATCH")
	errProgramInsufficientBalance  = errors.New("LXP_INSUFFICIENT_BALANCE")

	// programMappedFeeDenoms are the bank denoms of the four mapped assets a
	// recorded program fee can be held in.
	programMappedFeeDenoms = []string{types.ProgramFeeDenomPAX, types.ProgramFeeDenomSID,
		types.ProgramFeeDenomUSDC, types.ProgramFeeDenomUSDL}
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

// ProgramAnswerKey is "web/answer" || programID || u64 big-endian requestID
// || u8 part: 51 bytes.
func ProgramAnswerKey(programID types.Hash32, requestID uint64, part uint8) []byte {
	key := make([]byte, 0, len(programAnswerPrefix)+len(programID)+8+1)
	key = append(key, programAnswerPrefix...)
	key = append(key, programID[:]...)
	key = binary.BigEndian.AppendUint64(key, requestID)
	return append(key, part)
}

func (k Keeper) setProgramCommittedCount(ctx sdk.Context, count uint64) {
	k.set(ctx, programCommittedCountKey, count)
}

// FulfilProgram admits the attested answer to a program request, the raw
// canonical observation. It refuses, in the kernel order: a paused module
// (LXP_XWEB_PAUSED), an observation that does not decode
// (LXP_NON_CANONICAL), a network id other than Params.KernelNetworkId
// (LXP_WRONG_NETWORK), a corrupt pending record (LXP_NON_CANONICAL), no
// pending record (LXP_UNKNOWN_FIELD), a fulfilled one (LXP_SEQUENCE_REUSED),
// a kind or payload hash other than the recorded ones (LXP_CONTEXT_MISMATCH),
// an invalid attestor set or signatures that do not reach its threshold over
// the origin-2 preimage (verifyProgramSignatures) and a full answer store
// (LXP_ARENA_EXHAUSTED). In one cached context it then writes the answer
// parts, splits the fee among the signers, marks the record fulfilled and
// moves it from the pending to the committed count; a failure of any of
// these writes nothing.
func (k Keeper) FulfilProgram(ctx sdk.Context, raw []byte) error {
	if k.IsPaused(ctx) {
		return types.ErrPaused
	}
	observation, err := types.DecodeObservation(raw)
	if err != nil {
		return err
	}
	if observation.NetworkID != k.GetParams(ctx).KernelNetworkId {
		return errProgramWrongNetwork
	}
	record, found, err := k.GetProgramPendingRecord(ctx, observation.ProgramID, observation.RequestID)
	if err != nil {
		return err
	}
	if !found {
		return errProgramUnknownField
	}
	if record.Fulfilled {
		return errProgramSequenceReused
	}
	if record.Kind != observation.Kind || record.PayloadHash != observation.PayloadHash {
		return errProgramContextMismatch
	}
	set := k.GetAttestorSet(ctx)
	signers, err := verifyProgramSignatures(set, observation)
	if err != nil {
		return err
	}
	committed := k.ProgramCommittedCount(ctx)
	if committed >= ProgramCommittedCapacity {
		return errProgramArenaExhausted
	}
	pending := k.ProgramPendingCount(ctx)
	if pending == 0 {
		return errProgramFatalInvariant
	}

	cached, write := ctx.CacheContext()
	k.putProgramAnswer(cached, observation)
	if err := k.splitProgramFee(cached, set, record, signers); err != nil {
		return err
	}
	record.Fulfilled = true
	encoded, err := record.Bytes()
	if err != nil {
		return err
	}
	k.setProgramPendingRecord(cached, ProgramPendingKey(observation.ProgramID, observation.RequestID), encoded)
	k.setProgramPendingCount(cached, pending-1)
	k.setProgramCommittedCount(cached, committed+1)
	write()
	ctx.EventManager().EmitEvents(cached.EventManager().Events())
	return nil
}

// putProgramAnswer writes part 0, the content digest, full length and
// response length, then the response in 1024-byte chunks as parts 1 and up.
func (k Keeper) putProgramAnswer(ctx sdk.Context, observation types.Observation) {
	store := ctx.KVStore(k.storeKey)
	header := make([]byte, ProgramAnswerHeaderBytes)
	copy(header[:32], observation.ContentDigest[:])
	binary.BigEndian.PutUint32(header[32:36], observation.FullLength)
	binary.BigEndian.PutUint32(header[36:40], uint32(len(observation.Response)))
	store.Set(ProgramAnswerKey(observation.ProgramID, observation.RequestID, 0), header)
	for part, offset := uint8(1), 0; offset < len(observation.Response); part, offset = part+1, offset+ProgramAnswerChunkBytes {
		end := min(offset+ProgramAnswerChunkBytes, len(observation.Response))
		store.Set(ProgramAnswerKey(observation.ProgramID, observation.RequestID, part),
			bytes.Clone(observation.Response[offset:end]))
	}
}

// splitProgramFee pays the recorded amount out of the recorded fee account to
// the signers' payout accounts in equal shares, floor(amount / signers), the
// remainder to signers[0], the lowest address; a zero share pays nothing.
func (k Keeper) splitProgramFee(ctx sdk.Context, set types.AttestorSet, record ProgramPendingRecord,
	signers []types.Address20) error {
	if len(signers) == 0 || len(signers) > types.ObservationMaxSignatures {
		return errProgramNonCanonical
	}
	if record.Amount.IsZero() {
		return nil
	}
	source := address.Module(ProgramFeeAccountModule, record.FeeAccount[:])
	denom, err := k.programFeeDenom(ctx, source, record.Amount)
	if err != nil {
		return err
	}
	share := record.Amount.QuoRaw(int64(len(signers)))
	remainder := record.Amount.Sub(share.MulRaw(int64(len(signers))))
	for index, signer := range signers {
		amount := share
		if index == 0 {
			amount = amount.Add(remainder)
		}
		if amount.IsZero() {
			continue
		}
		attestor, found := set.Find(signer)
		if !found {
			return errProgramUnknownField
		}
		payout, err := sdk.AccAddressFromBech32(attestor.Payout)
		if err != nil {
			return types.ErrInvalidAttestors.Wrapf("payout of %s: %v", signer.Hex(), err)
		}
		if err := k.bankKeeper.SendCoins(ctx, source, payout, sdk.NewCoins(sdk.NewCoin(denom, amount))); err != nil {
			return err
		}
	}
	return nil
}

// programFeeDenom resolves the bank denom the recorded asset is held in: the
// one mapped program fee denom of which the fee account holds at least the
// amount. None is LXP_INSUFFICIENT_BALANCE; two are LXP_ASSET_MISMATCH.
func (k Keeper) programFeeDenom(ctx sdk.Context, account sdk.AccAddress, amount sdk.Int) (string, error) {
	denom := ""
	for _, candidate := range programMappedFeeDenoms {
		if k.bankKeeper.GetBalance(ctx, account, candidate).Amount.LT(amount) {
			continue
		}
		if denom != "" {
			return "", errProgramAssetMismatch
		}
		denom = candidate
	}
	if denom == "" {
		return "", errProgramInsufficientBalance
	}
	return denom, nil
}
