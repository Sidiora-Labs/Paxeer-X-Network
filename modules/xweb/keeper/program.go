package keeper

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"slices"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
)

const (
	// ProgramRequestTopic is the guest event topic under which a program
	// emits a web request (include/layerx/lx_web.h LX_WEB_REQUEST_TOPIC).
	ProgramRequestTopic = "PAXEERX_WEB_REQUEST_V1"

	// ProgramFeeDomain prefixes the asset id in the fee account id preimage.
	ProgramFeeDomain = "PAXEERX_WEB_FEES_V1"

	// ProgramFeeAccountModule is the module name the fee account id is mapped
	// to a bank address under.
	ProgramFeeAccountModule = "layerx"

	// ProgramPendingCapacity bounds the program requests recorded and not yet
	// fulfilled; ProgramCommittedCapacity bounds the committed answers.
	ProgramPendingCapacity   = 256
	ProgramCommittedCapacity = 64

	// ProgramRequestHeaderBytes is the u64 request id, u8 kind and u32 payload
	// length that precede the payload in the request event data.
	ProgramRequestHeaderBytes = 13

	// ProgramPendingRecordVersion and ProgramPendingRecordBytes are the
	// version byte and the exact length of a pending record.
	ProgramPendingRecordVersion = 1
	ProgramPendingRecordBytes   = 123

	programPendingPrefix = "web/pending"
	programAmountBits    = 128
)

var (
	errProgramDuplicateEntry = errors.New("LXP_DUPLICATE_ENTRY")
	errProgramSequenceReused = errors.New("LXP_SEQUENCE_REUSED")
	errProgramFeeUnpayable   = errors.New("LXP_FEE_UNPAYABLE")
	errProgramAssetMismatch  = errors.New("LXP_ASSET_MISMATCH")
	errProgramAuthScope      = errors.New("LXP_AUTH_SCOPE")
	errProgramArenaExhausted = errors.New("LXP_ARENA_EXHAUSTED")
	errProgramOverflow       = errors.New("LXP_OVERFLOW")
	errProgramFatalInvariant = errors.New("LXP_FATAL_INVARIANT")
)

// ProgramLeg is one transfer leg a hosted program call applied: the debited
// and credited bank accounts, the kernel asset id, the bank denom that asset
// is held in and the amount moved.
type ProgramLeg struct {
	From   sdk.AccAddress
	To     sdk.AccAddress
	Asset  types.Hash32
	Denom  string
	Amount sdk.Int
}

// ProgramCall is one hosted program call as the programs module runs it: the
// emitting program, the call principal, the kernel recording sequence of the
// call and the transfer legs it applied. RequestFromProgram marks the call
// once it records a web request, so the call records at most one.
type ProgramCall struct {
	ProgramID types.Hash32
	Principal sdk.AccAddress
	Sequence  uint64
	Legs      []ProgramLeg
	requested bool
}

// ProgramPendingRecord is the 123-byte pending record of one program request:
// kind, keccak256 of the payload, the fee asset, the fee account id, the u128
// amount paid, the recording sequence and the fulfilled flag. It carries no
// refund or timeout field: an origin-2 fee is released only by the split at
// fulfilment.
type ProgramPendingRecord struct {
	Kind        uint8
	PayloadHash types.Hash32
	FeeAsset    types.Hash32
	FeeAccount  types.Hash32
	Amount      sdk.Int
	Sequence    uint64
	Fulfilled   bool
}

// Bytes is the record in the kernel byte order: [0] version, [1] kind,
// [2..33] payload hash, [34..65] fee asset, [66..97] fee account, [98..113]
// u128 big-endian amount, [114..121] u64 recording sequence, [122] fulfilled.
func (r ProgramPendingRecord) Bytes() ([]byte, error) {
	if r.Amount.IsNil() || r.Amount.IsNegative() || r.Amount.BigInt().BitLen() > programAmountBits {
		return nil, errProgramOverflow
	}
	out := make([]byte, ProgramPendingRecordBytes)
	out[0] = ProgramPendingRecordVersion
	out[1] = r.Kind
	copy(out[2:34], r.PayloadHash[:])
	copy(out[34:66], r.FeeAsset[:])
	copy(out[66:98], r.FeeAccount[:])
	r.Amount.BigInt().FillBytes(out[98:114])
	binary.BigEndian.PutUint64(out[114:122], r.Sequence)
	if r.Fulfilled {
		out[122] = 1
	}
	return out, nil
}

// DecodeProgramPendingRecord reads a stored record and refuses one that is
// not 123 bytes, not version 1, of a kind other than fetch or search, or with
// a fulfilled byte above 1, as the kernel load does.
func DecodeProgramPendingRecord(raw []byte) (ProgramPendingRecord, error) {
	if len(raw) != ProgramPendingRecordBytes || raw[0] != ProgramPendingRecordVersion ||
		!programKind(raw[1]) || raw[122] > 1 {
		return ProgramPendingRecord{}, errProgramNonCanonical
	}
	record := ProgramPendingRecord{
		Kind:      raw[1],
		Amount:    sdk.NewIntFromBigInt(new(big.Int).SetBytes(raw[98:114])),
		Sequence:  binary.BigEndian.Uint64(raw[114:122]),
		Fulfilled: raw[122] == 1,
	}
	copy(record.PayloadHash[:], raw[2:34])
	copy(record.FeeAsset[:], raw[34:66])
	copy(record.FeeAccount[:], raw[66:98])
	return record, nil
}

// ProgramPendingKey is "web/pending" || programID || u64 big-endian requestID.
func ProgramPendingKey(programID types.Hash32, requestID uint64) []byte {
	key := make([]byte, 0, len(programPendingPrefix)+len(programID)+8)
	key = append(key, programPendingPrefix...)
	key = append(key, programID[:]...)
	return binary.BigEndian.AppendUint64(key, requestID)
}

// ProgramFeeAccountID is the 32-byte kernel id of the web fee account of
// asset: sha256("PAXEERX_WEB_FEES_V1" || asset).
func ProgramFeeAccountID(asset types.Hash32) types.Hash32 {
	preimage := make([]byte, 0, len(ProgramFeeDomain)+len(asset))
	preimage = append(preimage, ProgramFeeDomain...)
	preimage = append(preimage, asset[:]...)
	return sha256.Sum256(preimage)
}

// ProgramFeeAccount is the bank address that holds the web fees paid in
// asset: address.Module("layerx", ProgramFeeAccountID(asset)).
func ProgramFeeAccount(asset types.Hash32) sdk.AccAddress {
	id := ProgramFeeAccountID(asset)
	return address.Module(ProgramFeeAccountModule, id[:])
}

func programKind(kind uint8) bool { return kind == types.KindFetch || kind == types.KindSearch }

// decodeProgramRequest reads the request event data: u64 request id, u8 kind
// fetch or search, u32 payload length above zero and exactly that many
// payload bytes.
func decodeProgramRequest(data []byte) (uint64, uint8, []byte, error) {
	if len(data) < ProgramRequestHeaderBytes {
		return 0, 0, nil, errProgramNonCanonical
	}
	length := binary.BigEndian.Uint32(data[9:13])
	if !programKind(data[8]) || length == 0 || uint64(len(data)-ProgramRequestHeaderBytes) != uint64(length) {
		return 0, 0, nil, errProgramNonCanonical
	}
	return binary.BigEndian.Uint64(data[:8]), data[8], data[ProgramRequestHeaderBytes:], nil
}

// RequestFromProgram is the origin-2 entry the hosted programs module calls
// for every guest event a program call emits. An event under any topic other
// than ProgramRequestTopic is not a web request and records nothing. For a
// request event it refuses, in order: a paused module (LXP_XWEB_PAUSED), a
// second request in the same call (LXP_DUPLICATE_ENTRY), event data that does
// not decode (LXP_NON_CANONICAL), a request id the program already recorded,
// pending or answered (LXP_SEQUENCE_REUSED), a full pending store
// (LXP_ARENA_EXHAUSTED), a leg without a destination (LXP_FATAL_INVARIANT), a
// fee leg not debiting the call principal (LXP_AUTH_SCOPE), fee legs of two
// assets or an asset outside Params.ProgramFeeDenoms (LXP_ASSET_MISMATCH), a
// fee total over u128 (LXP_OVERFLOW) and no positive fee total
// (LXP_FEE_UNPAYABLE). The fee is the sum of the call's legs into the web
// fee account of one asset, any positive amount; it stays there with no
// refund and no timeout until the fulfilment splits it. On success it stores
// the pending record and counts it as pending; the caller discards the call,
// its legs and the record together when the call fails.
func (k Keeper) RequestFromProgram(ctx sdk.Context, call *ProgramCall, topic, data []byte) error {
	if !bytes.Equal(topic, []byte(ProgramRequestTopic)) {
		return nil
	}
	if k.IsPaused(ctx) {
		return types.ErrPaused
	}
	if call == nil || call.ProgramID == (types.Hash32{}) || call.Principal.Empty() {
		return errProgramNonCanonical
	}
	if call.requested {
		return errProgramDuplicateEntry
	}
	requestID, kind, payload, err := decodeProgramRequest(data)
	if err != nil {
		return err
	}
	key := ProgramPendingKey(call.ProgramID, requestID)
	if k.hasProgramPendingRecord(ctx, key) {
		return errProgramSequenceReused
	}
	pending := k.ProgramPendingCount(ctx)
	if pending >= ProgramPendingCapacity {
		return errProgramArenaExhausted
	}
	record, err := programFee(call, k.GetParams(ctx).ProgramFeeDenoms)
	if err != nil {
		return err
	}
	record.Kind = kind
	record.PayloadHash = types.Keccak(payload)
	record.Sequence = call.Sequence
	raw, err := record.Bytes()
	if err != nil {
		return err
	}
	k.setProgramPendingRecord(ctx, key, raw)
	k.setProgramPendingCount(ctx, pending+1)
	call.requested = true
	return nil
}

// programFee sums the call's legs into the web fee account of their asset
// and returns the record with its fee asset, fee account and amount set.
func programFee(call *ProgramCall, denoms []string) (ProgramPendingRecord, error) {
	record := ProgramPendingRecord{Amount: sdk.ZeroInt()}
	denom, paid := "", false
	for _, leg := range call.Legs {
		if leg.To.Empty() || leg.Amount.IsNil() || leg.Amount.IsNegative() {
			return ProgramPendingRecord{}, errProgramFatalInvariant
		}
		if !leg.To.Equals(ProgramFeeAccount(leg.Asset)) {
			continue
		}
		if !leg.From.Equals(call.Principal) {
			return ProgramPendingRecord{}, errProgramAuthScope
		}
		if paid && (leg.Asset != record.FeeAsset || leg.Denom != denom) {
			return ProgramPendingRecord{}, errProgramAssetMismatch
		}
		if !paid && !slices.Contains(denoms, leg.Denom) {
			return ProgramPendingRecord{}, errProgramAssetMismatch
		}
		if leg.Amount.BigInt().BitLen() > programAmountBits {
			return ProgramPendingRecord{}, errProgramOverflow
		}
		record.Amount = record.Amount.Add(leg.Amount)
		if record.Amount.BigInt().BitLen() > programAmountBits {
			return ProgramPendingRecord{}, errProgramOverflow
		}
		record.FeeAsset, record.FeeAccount, denom, paid = leg.Asset, ProgramFeeAccountID(leg.Asset), leg.Denom, true
	}
	if !paid || record.Amount.IsZero() {
		return ProgramPendingRecord{}, errProgramFeeUnpayable
	}
	return record, nil
}
