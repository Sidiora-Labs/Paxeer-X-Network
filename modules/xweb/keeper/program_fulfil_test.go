package keeper

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"sort"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	paramskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/keeper"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func programSignatureKeys(t *testing.T, count int) (types.AttestorSet, []*ecdsa.PrivateKey) {
	t.Helper()
	keys := make([]*ecdsa.PrivateKey, count)
	for i := range keys {
		seed := make([]byte, 32)
		seed[0] = 0x3c
		seed[31] = byte(i + 1)
		key, err := crypto.ToECDSA(seed)
		require.NoError(t, err)
		keys[i] = key
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := crypto.PubkeyToAddress(keys[i].PublicKey), crypto.PubkeyToAddress(keys[j].PublicKey)
		return bytes.Compare(a[:], b[:]) < 0
	})
	set := types.AttestorSet{Threshold: types.Majority(count)}
	for _, key := range keys {
		signer := types.Address20(crypto.PubkeyToAddress(key.PublicKey))
		set.Attestors = append(set.Attestors, types.Attestor{
			Signer:    signer,
			Payout:    sdk.AccAddress(signer[:]).String(),
			PublicKey: crypto.CompressPubkey(&key.PublicKey),
		})
	}
	if count <= types.MaxAttestorSetSize {
		require.NoError(t, set.Validate())
	} else {
		require.ErrorIs(t, set.Validate(), types.ErrInvalidAttestors)
	}
	return set, keys
}

func programSignatureKeeper(t *testing.T, set types.AttestorSet) (Keeper, sdk.Context) {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_xweb"))
	k := Keeper{storeKey: key}
	k.setAttestorSet(ctx, set)
	require.Equal(t, set, k.GetAttestorSet(ctx))
	return k, ctx
}

func programSignatureObservation() types.Observation {
	return types.Observation{
		Origin:        types.OriginProgram,
		NetworkID:     7,
		ProgramID:     types.Hash32{0x41, 0x42},
		RequestID:     11,
		Kind:          types.KindSearch,
		PayloadHash:   types.Keccak([]byte("query")),
		ContentDigest: types.Keccak([]byte("answer")),
		FullLength:    8,
		Response:      []byte("answer"),
	}
}

func programSignatureDigest(observation types.Observation) types.Hash32 {
	return types.Digest(types.Attestation{
		Origin:        observation.Origin,
		NetworkID:     new(big.Int).SetUint64(uint64(observation.NetworkID)),
		Requester:     observation.ProgramID,
		RequestID:     observation.RequestID,
		Kind:          observation.Kind,
		PayloadHash:   observation.PayloadHash,
		ContentDigest: observation.ContentDigest,
		ResponseHash:  types.Keccak(observation.Response),
		FullLength:    observation.FullLength,
	})
}

func programSignatures(t *testing.T, observation types.Observation, keys ...*ecdsa.PrivateKey) [][]byte {
	t.Helper()
	digest := programSignatureDigest(observation)
	signatures := make([][]byte, len(keys))
	for i, key := range keys {
		signature, err := crypto.Sign(digest[:], key)
		require.NoError(t, err)
		signature[64] += 27
		signatures[i] = signature
	}
	return signatures
}

func programSignatureRefusal(t *testing.T, set types.AttestorSet, observation types.Observation, code string) {
	t.Helper()
	k, ctx := programSignatureKeeper(t, set)
	before := ctx.KVStore(k.storeKey).Get(types.AttestorSetKey)
	signatures := make([][]byte, len(observation.Signatures))
	for i, signature := range observation.Signatures {
		signatures[i] = bytes.Clone(signature)
	}
	signers, err := verifyProgramSignatures(k.GetAttestorSet(ctx), observation)
	require.EqualError(t, err, code)
	require.Nil(t, signers)
	require.Equal(t, before, ctx.KVStore(k.storeKey).Get(types.AttestorSetKey))
	for i := range signatures {
		require.Equal(t, signatures[i], observation.Signatures[i])
	}
}

func TestFulfilProgramThreshold(t *testing.T) {
	set, keys := programSignatureKeys(t, 4)
	observation := programSignatureObservation()
	for _, count := range []int{0, 1, 2} {
		t.Run("below_"+big.NewInt(int64(count)).String(), func(t *testing.T) {
			observation.Signatures = programSignatures(t, observation, keys[:count]...)
			if count > 0 {
				observation.Signatures[0][64] = 29
			}
			programSignatureRefusal(t, set, observation, "LXP_ATTESTATION_THRESHOLD")
		})
	}
	t.Run("empty_set", func(t *testing.T) {
		observation.Signatures = programSignatures(t, observation, keys[0])
		programSignatureRefusal(t, types.AttestorSet{}, observation, "LXP_ATTESTATION_THRESHOLD")
	})
	for _, threshold := range []uint32{0, 1, 2, 5, ^uint32(0)} {
		t.Run("invalid_threshold_"+new(big.Int).SetUint64(uint64(threshold)).String(), func(t *testing.T) {
			invalid := set
			invalid.Threshold = threshold
			observation.Signatures = [][]byte{make([]byte, types.SignatureLength)}
			programSignatureRefusal(t, invalid, observation, "LXP_ATTESTATION_THRESHOLD")
		})
	}
	t.Run("over_cap", func(t *testing.T) {
		invalid, _ := programSignatureKeys(t, 17)
		observation.Signatures = [][]byte{make([]byte, types.SignatureLength)}
		programSignatureRefusal(t, invalid, observation, "LXP_ATTESTATION_THRESHOLD")
	})
	for name, change := range map[string]func(*types.AttestorSet){
		"duplicate_member":      func(s *types.AttestorSet) { s.Attestors[1] = s.Attestors[0] },
		"zero_member":           func(s *types.AttestorSet) { s.Attestors[0].Signer = types.Address20{} },
		"invalid_payout":        func(s *types.AttestorSet) { s.Attestors[0].Payout = "invalid" },
		"invalid_public_key":    func(s *types.AttestorSet) { s.Attestors[0].PublicKey = []byte{1} },
		"mismatched_public_key": func(s *types.AttestorSet) { s.Attestors[0].PublicKey = s.Attestors[1].PublicKey },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := types.AttestorSet{Threshold: set.Threshold, Attestors: append([]types.Attestor(nil), set.Attestors...)}
			change(&invalid)
			observation.Signatures = [][]byte{make([]byte, types.SignatureLength)}
			programSignatureRefusal(t, invalid, observation, "LXP_ATTESTATION_THRESHOLD")
		})
	}
	for _, count := range []int{3, 4, 16} {
		t.Run("valid_"+big.NewInt(int64(count)).String(), func(t *testing.T) {
			valid, members := set, keys
			if count == 16 {
				valid, members = programSignatureKeys(t, count)
			}
			observation.Signatures = programSignatures(t, observation, members[:count]...)
			raw, err := types.EncodeObservation(observation)
			require.NoError(t, err)
			decoded, err := types.DecodeObservation(raw)
			require.NoError(t, err)
			k, ctx := programSignatureKeeper(t, valid)
			signers, err := verifyProgramSignatures(k.GetAttestorSet(ctx), decoded)
			require.NoError(t, err)
			want := make([]types.Address20, count)
			for i := range want {
				want[i] = valid.Attestors[i].Signer
			}
			require.Equal(t, want, signers)
		})
	}
	t.Run("majority_through_fulfil_program", func(t *testing.T) {
		s := newProgramFulfilSuite(t, 4)
		require.Equal(t, uint32(3), s.k.Threshold(s.ctx))
		s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(9))
		observation := s.observation(programFulfilP, 3, []byte("answer"), 6)
		s.refuse(s.signed(observation, s.keys[:2]...), "LXP_ATTESTATION_THRESHOLD")
		require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(observation, s.keys[:3]...)))
		require.True(t, s.pending(programFulfilP, 3).Fulfilled)
	})
}

func programUnrecoverableSignature(t *testing.T) []byte {
	t.Helper()
	curve := crypto.S256().Params()
	for i := int64(1); i < 256; i++ {
		r := big.NewInt(i)
		ySquared := new(big.Int).Exp(r, big.NewInt(3), curve.P)
		ySquared.Add(ySquared, curve.B).Mod(ySquared, curve.P)
		if new(big.Int).ModSqrt(ySquared, curve.P) == nil {
			signature := make([]byte, types.SignatureLength)
			r.FillBytes(signature[:32])
			signature[63], signature[64] = 1, 27
			require.True(t, crypto.ValidateSignatureValues(0, r, big.NewInt(1), true))
			return signature
		}
	}
	t.Fatal("no non-curve recovery point found")
	return nil
}

func TestFulfilProgramSignatureMatrix(t *testing.T) {
	all, keys := programSignatureKeys(t, 4)
	set := types.AttestorSet{Threshold: 2, Attestors: all.Attestors[:3]}
	observation := programSignatureObservation()
	sign := func(members ...*ecdsa.PrivateKey) [][]byte { return programSignatures(t, observation, members...) }
	for name, tc := range map[string]struct {
		signatures [][]byte
		code       string
	}{
		"duplicate":               {sign(keys[0], keys[0], keys[2]), "LXP_AUTH_DUPLICATE_SIGNER"},
		"descending":              {sign(keys[1], keys[0], keys[2]), "LXP_UNSORTED_SEQUENCE"},
		"non_member":              {sign(keys[0], keys[1], keys[3]), "LXP_INVALID_ATTESTATION"},
		"order_before_membership": {sign(keys[2], keys[1]), "LXP_UNSORTED_SEQUENCE"},
		"first_membership_before_later_bad_signature": {append(sign(keys[3]), make([]byte, types.SignatureLength)), "LXP_INVALID_ATTESTATION"},
	} {
		t.Run(name, func(t *testing.T) {
			members := set
			if name == "order_before_membership" {
				members.Attestors = []types.Attestor{all.Attestors[0], all.Attestors[2], all.Attestors[3]}
			}
			observation.Signatures = tc.signatures
			programSignatureRefusal(t, members, observation, tc.code)
		})
	}
	for _, v := range []byte{0, 1, 26, 29, 255} {
		t.Run("invalid_v_"+big.NewInt(int64(v)).String(), func(t *testing.T) {
			observation.Signatures = sign(keys[0], keys[1], keys[2])
			observation.Signatures[1][64] = v
			programSignatureRefusal(t, set, observation, "LXP_BAD_SIGNATURE")
		})
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"short":      func(signature []byte) []byte { return signature[:64] },
		"long":       func(signature []byte) []byte { return append(signature, 0) },
		"nil":        func([]byte) []byte { return nil },
		"zero_r":     func(signature []byte) []byte { clear(signature[:32]); return signature },
		"zero_s":     func(signature []byte) []byte { clear(signature[32:64]); return signature },
		"r_at_order": func(signature []byte) []byte { crypto.S256().Params().N.FillBytes(signature[:32]); return signature },
		"s_at_order": func(signature []byte) []byte { crypto.S256().Params().N.FillBytes(signature[32:64]); return signature },
		"high_s": func(signature []byte) []byte {
			s := new(big.Int).SetBytes(signature[32:64])
			new(big.Int).Sub(crypto.S256().Params().N, s).FillBytes(signature[32:64])
			signature[64] = 55 - signature[64]
			return signature
		},
		"s_just_above_half": func(signature []byte) []byte {
			s := new(big.Int).Rsh(new(big.Int).Set(crypto.S256().Params().N), 1)
			s.Add(s, big.NewInt(1)).FillBytes(signature[32:64])
			return signature
		},
		"recovery_failure": func([]byte) []byte { return programUnrecoverableSignature(t) },
	} {
		t.Run(name, func(t *testing.T) {
			observation.Signatures = sign(keys[0], keys[1], keys[2])
			observation.Signatures[1] = mutate(observation.Signatures[1])
			programSignatureRefusal(t, set, observation, "LXP_BAD_SIGNATURE")
		})
	}
	t.Run("per_signature_before_later_failure", func(t *testing.T) {
		observation.Signatures = sign(keys[0], keys[0], keys[2])
		observation.Signatures[2][64] = 29
		programSignatureRefusal(t, set, observation, "LXP_AUTH_DUPLICATE_SIGNER")
	})
	t.Run("signature_bound", func(t *testing.T) {
		observation.Signatures = make([][]byte, 17)
		programSignatureRefusal(t, set, observation, "LXP_NON_CANONICAL")
	})
	t.Run("signer_identity", TestFulfilProgramSignerIdentity)
}

func TestFulfilProgramSignerIdentity(t *testing.T) {
	set, keys := programSignatureKeys(t, 1)
	k, ctx := programSignatureKeeper(t, set)
	observation := programSignatureObservation()
	observation.Signatures = programSignatures(t, observation, keys[0])
	before := bytes.Clone(observation.Signatures[0])
	originTwo, err := verifyProgramSignatures(k.GetAttestorSet(ctx), observation)
	require.NoError(t, err)
	require.Equal(t, []types.Address20{set.Attestors[0].Signer}, originTwo)
	require.Equal(t, before, observation.Signatures[0])
	attestation := types.Attestation{
		Origin:        types.OriginEVM,
		NetworkID:     big.NewInt(125),
		Requester:     types.EVMRequester(set.Attestors[0].Signer),
		RequestID:     observation.RequestID,
		Kind:          observation.Kind,
		PayloadHash:   observation.PayloadHash,
		ContentDigest: observation.ContentDigest,
		ResponseHash:  types.Keccak(observation.Response),
		FullLength:    observation.FullLength,
	}
	originOneDigest := types.Digest(attestation)
	require.NotEqual(t, originOneDigest, programSignatureDigest(observation))
	signature, err := crypto.Sign(originOneDigest[:], keys[0])
	require.NoError(t, err)
	signature[64] += 27
	originOne, err := verifySignatures(k.GetAttestorSet(ctx), originOneDigest, [][]byte{signature})
	require.NoError(t, err)
	require.Equal(t, originTwo, originOne)
	listed, found := k.GetAttestorSet(ctx).Find(originTwo[0])
	require.True(t, found)
	require.Equal(t, set.Attestors[0], listed)
	for _, v := range []byte{27, 28} {
		signed := bytes.Clone(observation.Signatures[0])
		signed[64] = v
		recovered, err := bridgetypes.RecoverSigner(bridgetypes.Hash32(programSignatureDigest(observation)), signed)
		require.NoError(t, err)
		member := set.Attestors[0]
		member.Signer = types.Address20(recovered)
		member.PublicKey = nil
		observation.Signatures = [][]byte{signed}
		members := types.AttestorSet{Threshold: 1, Attestors: []types.Attestor{member}}
		signers, err := verifyProgramSignatures(members, observation)
		require.NoError(t, err)
		require.Equal(t, []types.Address20{member.Signer}, signers)
	}
}

// programFulfilAsset is a kernel asset id and the bank denom it is held in.
type programFulfilAsset struct {
	id    types.Hash32
	denom string
}

const programFulfilMinter = "xweb_program_fulfil_minter"

var (
	paxProgramAsset      = programFulfilAsset{types.Hash32{9}, types.ProgramFeeDenomPAX}
	sidProgramAsset      = programFulfilAsset{types.Hash32{10}, types.ProgramFeeDenomSID}
	programFulfilP       = types.Hash32(bytes.Repeat([]byte{0x50}, 32))
	programFulfilPayload = []byte("paxeer x network")
	programFulfilPayer   = sdk.AccAddress(bytes.Repeat([]byte{0xa1}, 20))
	programFulfilDigest  = types.Hash32{0xcd, 0x01}
)

// programFulfilSuite is the keeper over a real bank, unpaused, with kernel
// network id 1, the four mapped fee denoms and an attestor set of the given
// size at the majority threshold.
type programFulfilSuite struct {
	t    *testing.T
	ctx  sdk.Context
	k    Keeper
	bank bankkeeper.BaseKeeper
	keys []*ecdsa.PrivateKey
	set  types.AttestorSet
	bkey *sdk.KVStoreKey
}

func newProgramFulfilSuite(t *testing.T, attestors int) *programFulfilSuite {
	t.Helper()
	registry := cdctypes.NewInterfaceRegistry()
	authtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	xwebKey := sdk.NewKVStoreKey(types.StoreKey)
	accountKey := sdk.NewKVStoreKey(authtypes.StoreKey)
	bankKey := sdk.NewKVStoreKey(banktypes.StoreKey)
	paramsKey := sdk.NewKVStoreKey("params")
	paramsTransientKey := sdk.NewTransientStoreKey("transient_params")
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db)
	for _, key := range []sdk.StoreKey{xwebKey, accountKey, bankKey, paramsKey} {
		cms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	cms.MountStoreWithDB(paramsTransientKey, sdk.StoreTypeTransient, db)
	require.NoError(t, cms.LoadLatestVersion())
	ctx := sdk.NewContext(cms, tmproto.Header{Height: 8}, false)
	params := paramskeeper.NewKeeper(cdc, codec.NewLegacyAmino(), paramsKey, paramsTransientKey)
	accounts := authkeeper.NewAccountKeeper(cdc, accountKey, params.Subspace(authtypes.ModuleName),
		authtypes.ProtoBaseAccount, map[string][]string{programFulfilMinter: {authtypes.Minter}})
	accounts.SetParams(ctx, authtypes.DefaultParams())
	bank := bankkeeper.NewBaseKeeper(cdc, bankKey, accounts, params.Subspace(banktypes.ModuleName), nil)
	bank.SetParams(ctx, banktypes.DefaultParams())

	set, keys := programSignatureKeys(t, attestors)
	genesis := types.GenesisState{Params: types.DefaultParams(types.DefaultAuthority()), Attestors: set}
	genesis.Params.KernelNetworkId = 1
	genesis.Params.ProgramFeeDenoms = []string{types.ProgramFeeDenomPAX, types.ProgramFeeDenomSID,
		types.ProgramFeeDenomUSDC, types.ProgramFeeDenomUSDL}
	k := Keeper{storeKey: xwebKey, bankKeeper: bank}
	k.InitGenesis(ctx, genesis)
	require.False(t, k.IsPaused(ctx))
	return &programFulfilSuite{t: t, ctx: ctx, k: k, bank: bank, keys: keys, set: set, bkey: bankKey}
}

// record runs one paid program call: the fee leg moves amount from the
// payer into the asset's web fee account through the bank, then the call's
// request event goes through RequestFromProgram.
func (s *programFulfilSuite) record(programID types.Hash32, requestID uint64, kind uint8, payload []byte,
	asset programFulfilAsset, amount sdk.Int) {
	s.t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(asset.denom, amount))
	require.NoError(s.t, s.bank.MintCoins(s.ctx, programFulfilMinter, coins))
	require.NoError(s.t, s.bank.SendCoinsFromModuleToAccount(s.ctx, programFulfilMinter, programFulfilPayer, coins))
	leg := ProgramLeg{From: programFulfilPayer, To: ProgramFeeAccount(asset.id), Asset: asset.id, Denom: asset.denom,
		Amount: amount}
	require.NoError(s.t, s.bank.SendCoins(s.ctx, leg.From, leg.To, coins))
	data := binary.BigEndian.AppendUint64(nil, requestID)
	data = append(data, kind)
	data = binary.BigEndian.AppendUint32(data, uint32(len(payload)))
	data = append(data, payload...)
	call := &ProgramCall{ProgramID: programID, Principal: programFulfilPayer, Sequence: 7, Legs: []ProgramLeg{leg}}
	require.NoError(s.t, s.k.RequestFromProgram(s.ctx, call, []byte(ProgramRequestTopic), data))
}

func (s *programFulfilSuite) observation(programID types.Hash32, requestID uint64, response []byte,
	fullLength uint32) types.Observation {
	return types.Observation{
		Origin:        types.OriginProgram,
		NetworkID:     1,
		ProgramID:     programID,
		RequestID:     requestID,
		Kind:          types.KindSearch,
		PayloadHash:   types.Keccak(programFulfilPayload),
		ContentDigest: programFulfilDigest,
		FullLength:    fullLength,
		Response:      response,
	}
}

// signed is the canonical observation bytes carrying the keys' signatures.
func (s *programFulfilSuite) signed(observation types.Observation, keys ...*ecdsa.PrivateKey) []byte {
	s.t.Helper()
	observation.Signatures = programSignatures(s.t, observation, keys...)
	raw, err := types.EncodeObservation(observation)
	require.NoError(s.t, err)
	return raw
}

func (s *programFulfilSuite) pending(programID types.Hash32, requestID uint64) ProgramPendingRecord {
	s.t.Helper()
	record, found, err := s.k.GetProgramPendingRecord(s.ctx, programID, requestID)
	require.NoError(s.t, err)
	require.True(s.t, found)
	return record
}

func (s *programFulfilSuite) fulfilledByte(programID types.Hash32, requestID uint64) byte {
	s.t.Helper()
	raw := s.ctx.KVStore(s.k.storeKey).Get(ProgramPendingKey(programID, requestID))
	require.Len(s.t, raw, ProgramPendingRecordBytes)
	return raw[122]
}

func (s *programFulfilSuite) answerPart(programID types.Hash32, requestID uint64, part uint8) []byte {
	return s.ctx.KVStore(s.k.storeKey).Get(ProgramAnswerKey(programID, requestID, part))
}

func (s *programFulfilSuite) balance(of sdk.AccAddress, denom string) sdk.Int {
	return s.bank.GetBalance(s.ctx, of, denom).Amount
}

func (s *programFulfilSuite) payout(index int) sdk.AccAddress {
	return sdk.AccAddress(s.set.Attestors[index].Signer[:])
}

// state is every entry of the xweb and bank stores.
func (s *programFulfilSuite) state() map[string][]byte {
	out := map[string][]byte{}
	for _, key := range []sdk.StoreKey{s.k.storeKey, s.bkey} {
		iterator := s.ctx.KVStore(key).Iterator(nil, nil)
		for ; iterator.Valid(); iterator.Next() {
			out[key.Name()+"/"+string(iterator.Key())] = bytes.Clone(iterator.Value())
		}
		require.NoError(s.t, iterator.Close())
	}
	return out
}

func programFulfilCode(err error) string {
	if name, ok := types.LXPName(err); ok {
		return name
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// refuse submits raw and asserts the refusal code and that no store entry
// changed.
func (s *programFulfilSuite) refuse(raw []byte, code string) {
	s.t.Helper()
	before := s.state()
	require.Equal(s.t, code, programFulfilCode(s.k.FulfilProgram(s.ctx, raw)))
	require.Equal(s.t, before, s.state())
}

func TestFulfilProgramAdmissionOrder(t *testing.T) {
	s := newProgramFulfilSuite(t, 4)
	s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(75))
	observation := s.observation(programFulfilP, 3, []byte("answer"), 6)
	valid := s.signed(observation, s.keys[:3]...)

	require.NoError(t, s.k.Pause(s.ctx, types.MsgPause{Authority: types.DefaultAuthority()}))
	s.refuse([]byte{1}, "LXP_XWEB_PAUSED")
	s.refuse(valid, "LXP_XWEB_PAUSED")
	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: types.DefaultAuthority()}))

	s.refuse([]byte{1}, "LXP_NON_CANONICAL")
	wrongNetwork := observation
	wrongNetwork.NetworkID = 2
	s.refuse(s.signed(wrongNetwork, s.keys[:3]...), "LXP_WRONG_NETWORK")
	unknown := s.observation(programFulfilP, 4, []byte("answer"), 6)
	unknownWrongNetwork := unknown
	unknownWrongNetwork.NetworkID = 2
	s.refuse(s.signed(unknownWrongNetwork, s.keys[:3]...), "LXP_WRONG_NETWORK")
	s.refuse(s.signed(unknown, s.keys[:3]...), "LXP_UNKNOWN_FIELD")

	s.ctx.KVStore(s.k.storeKey).Set(ProgramPendingKey(programFulfilP, 5), []byte{ProgramPendingRecordVersion})
	s.refuse(s.signed(s.observation(programFulfilP, 5, nil, 0), s.keys[:3]...), "LXP_NON_CANONICAL")

	wrongKind := observation
	wrongKind.Kind = types.KindFetch
	s.refuse(s.signed(wrongKind, s.keys[:3]...), "LXP_CONTEXT_MISMATCH")
	s.refuse(s.signed(wrongKind, s.keys[0]), "LXP_CONTEXT_MISMATCH")
	wrongPayload := observation
	wrongPayload.PayloadHash = types.Keccak([]byte("another query"))
	s.refuse(s.signed(wrongPayload, s.keys[:3]...), "LXP_CONTEXT_MISMATCH")
	s.refuse(s.signed(observation, s.keys[:2]...), "LXP_ATTESTATION_THRESHOLD")
	s.refuse(s.signed(observation, s.keys[1], s.keys[0], s.keys[2]), "LXP_UNSORTED_SEQUENCE")

	require.NoError(t, s.k.FulfilProgram(s.ctx, valid))
	require.Equal(t, byte(1), s.fulfilledByte(programFulfilP, 3))
	s.refuse(valid, "LXP_SEQUENCE_REUSED")
	s.refuse(s.signed(wrongKind, s.keys[0]), "LXP_SEQUENCE_REUSED")
}

func TestFulfilProgramCommitsChunkedAnswer(t *testing.T) {
	s := newProgramFulfilSuite(t, 4)
	s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(75))
	response := make([]byte, 2500)
	for i := range response {
		response[i] = byte(i*7 + 1)
	}
	require.Equal(t, uint64(1), s.k.ProgramPendingCount(s.ctx))
	require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, 3, response, 4000), s.keys[:3]...)))

	key := ProgramAnswerKey(programFulfilP, 3, 2)
	require.Len(t, key, 51)
	require.Equal(t, append(append([]byte("web/answer"), programFulfilP[:]...), 0, 0, 0, 0, 0, 0, 0, 3, 2), key)
	header := append(bytes.Clone(programFulfilDigest[:]), 0, 0, 0x0f, 0xa0, 0, 0, 0x09, 0xc4)
	require.Len(t, header, ProgramAnswerHeaderBytes)
	require.Equal(t, header, s.answerPart(programFulfilP, 3, 0))
	require.Equal(t, response[:1024], s.answerPart(programFulfilP, 3, 1))
	require.Equal(t, response[1024:2048], s.answerPart(programFulfilP, 3, 2))
	require.Equal(t, response[2048:], s.answerPart(programFulfilP, 3, 3))
	require.Len(t, s.answerPart(programFulfilP, 3, 3), 452)
	require.Nil(t, s.answerPart(programFulfilP, 3, 4))
	require.Equal(t, byte(1), s.fulfilledByte(programFulfilP, 3))
	require.Zero(t, s.k.ProgramPendingCount(s.ctx))
	require.Equal(t, uint64(1), s.k.ProgramCommittedCount(s.ctx))

	s.record(programFulfilP, 4, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(75))
	require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, 4, nil, 9), s.keys[:3]...)))
	require.Equal(t, append(bytes.Clone(programFulfilDigest[:]), 0, 0, 0, 9, 0, 0, 0, 0), s.answerPart(programFulfilP, 4, 0))
	require.Nil(t, s.answerPart(programFulfilP, 4, 1))
	require.Equal(t, uint64(2), s.k.ProgramCommittedCount(s.ctx))
}

func TestFulfilProgramSplitsTheFee(t *testing.T) {
	t.Run("seventy_five_over_four", func(t *testing.T) {
		s := newProgramFulfilSuite(t, 4)
		s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(75))
		account := ProgramFeeAccount(paxProgramAsset.id)
		require.Equal(t, sdk.NewInt(75), s.balance(account, paxProgramAsset.denom))
		require.Equal(t, byte(0), s.fulfilledByte(programFulfilP, 3))
		require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, 3, []byte("answer"), 6), s.keys...)))
		for index, want := range []int64{21, 18, 18, 18} {
			require.Equal(t, sdk.NewInt(want), s.balance(s.payout(index), paxProgramAsset.denom), "signer %d", index)
		}
		require.True(t, s.balance(account, paxProgramAsset.denom).IsZero())
		require.Equal(t, byte(1), s.fulfilledByte(programFulfilP, 3))
	})
	t.Run("two_over_three", func(t *testing.T) {
		s := newProgramFulfilSuite(t, 4)
		s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, sidProgramAsset, sdk.NewInt(2))
		account := ProgramFeeAccount(sidProgramAsset.id)
		require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, 3, []byte("answer"), 6), s.keys[:3]...)))
		for index, want := range []int64{2, 0, 0, 0} {
			require.Equal(t, sdk.NewInt(want), s.balance(s.payout(index), sidProgramAsset.denom), "signer %d", index)
			require.True(t, s.balance(s.payout(index), paxProgramAsset.denom).IsZero(), "signer %d", index)
		}
		require.True(t, s.balance(account, sidProgramAsset.denom).IsZero())
		require.Equal(t, byte(1), s.fulfilledByte(programFulfilP, 3))
	})
}

func TestFulfilProgramIsAtomic(t *testing.T) {
	s := newProgramFulfilSuite(t, 4)
	s.record(programFulfilP, 3, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(75))
	refused := s.payout(1)
	s.bank.RegisterRecipientChecker(func(_ sdk.Context, recipient sdk.AccAddress) bool { return !recipient.Equals(refused) })
	before := s.state()
	err := s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, 3, make([]byte, 1500), 1500), s.keys[:3]...))
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRecipient)
	require.Equal(t, before, s.state())
	for part := uint8(0); part <= 2; part++ {
		require.Nil(t, s.answerPart(programFulfilP, 3, part), "part %d", part)
	}
	require.Equal(t, byte(0), s.fulfilledByte(programFulfilP, 3))
	require.Equal(t, sdk.NewInt(75), s.balance(ProgramFeeAccount(paxProgramAsset.id), paxProgramAsset.denom))
	require.True(t, s.balance(s.payout(0), paxProgramAsset.denom).IsZero())
	require.Equal(t, uint64(1), s.k.ProgramPendingCount(s.ctx))
	require.Zero(t, s.k.ProgramCommittedCount(s.ctx))
}

func TestFulfilProgramCommittedCapacity(t *testing.T) {
	s := newProgramFulfilSuite(t, 4)
	for id := uint64(1); id <= ProgramCommittedCapacity; id++ {
		s.record(programFulfilP, id, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(5))
		require.NoError(t, s.k.FulfilProgram(s.ctx, s.signed(s.observation(programFulfilP, id, []byte("answer"), 6), s.keys[:3]...)))
	}
	require.Equal(t, uint64(64), s.k.ProgramCommittedCount(s.ctx))
	require.Zero(t, s.k.ProgramPendingCount(s.ctx))
	account := ProgramFeeAccount(paxProgramAsset.id)
	require.True(t, s.balance(account, paxProgramAsset.denom).IsZero())

	s.record(programFulfilP, 65, types.KindSearch, programFulfilPayload, paxProgramAsset, sdk.NewInt(5))
	s.refuse(s.signed(s.observation(programFulfilP, 65, []byte("answer"), 6), s.keys[:3]...), "LXP_ARENA_EXHAUSTED")
	require.Equal(t, sdk.NewInt(5), s.balance(account, paxProgramAsset.denom))
	require.Equal(t, byte(0), s.fulfilledByte(programFulfilP, 65))
	require.Nil(t, s.answerPart(programFulfilP, 65, 0))
	require.Equal(t, uint64(64), s.k.ProgramCommittedCount(s.ctx))
	require.Equal(t, uint64(1), s.k.ProgramPendingCount(s.ctx))
}

func TestFeeAccountDerivation(t *testing.T) {
	domain := []byte{'P', 'A', 'X', 'E', 'E', 'R', 'X', '_', 'W', 'E', 'B', '_', 'F', 'E', 'E', 'S', '_', 'V', '1'}
	require.Len(t, domain, 19)
	require.Equal(t, domain, []byte(ProgramFeeDomain))
	asset := paxProgramAsset.id
	preimage := append(bytes.Clone(domain), asset[:]...)
	require.Len(t, preimage, 51)
	id := ProgramFeeAccountID(asset)
	require.Equal(t, types.Hash32(sha256.Sum256(preimage)), id)
	require.Equal(t, address.Module("layerx", id[:]), ProgramFeeAccount(asset))
	require.NotEqual(t, ProgramFeeAccount(asset), ProgramFeeAccount(sidProgramAsset.id))
}
