package keeper

import (
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"sort"
	"testing"

	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, set.Validate())
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
