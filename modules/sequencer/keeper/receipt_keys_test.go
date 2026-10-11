package keeper_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/testvectors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	sdkcodec "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params"
	paramskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/keeper"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

type receiptKeysEnv struct {
	ctx        sdk.Context
	amino      *sdkcodec.LegacyAmino
	paramSpace paramtypes.Subspace
	module     sequencer.AppModule
	handler    govtypes.Handler
}

func newReceiptKeysEnv(t *testing.T, height int64) receiptKeysEnv {
	t.Helper()
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	key := sdk.NewKVStoreKey(paramtypes.StoreKey)
	tkey := sdk.NewTransientStoreKey(paramtypes.TStoreKey)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tkey, sdk.StoreTypeTransient, db)
	require.NoError(t, ms.LoadLatestVersion())
	amino := sdkcodec.NewLegacyAmino()
	paramsKeeper := paramskeeper.NewKeeper(sdkcodec.NewProtoCodec(codectypes.NewInterfaceRegistry()), amino, key, tkey)
	paramSpace := paramsKeeper.Subspace(types.ModuleName)
	return receiptKeysEnv{
		ctx:        sdk.NewContext(ms, tmproto.Header{Height: height}, false),
		amino:      amino,
		paramSpace: paramSpace,
		module:     sequencer.NewAppModule(paramSpace),
		handler:    sequencer.NewParamChangeProposalHandler(paramSpace, params.NewParamChangeProposalHandler(paramsKeeper)),
	}
}

func (env receiptKeysEnv) initGenesis(t *testing.T, keys keeper.ReceiptKeys) {
	t.Helper()
	encoded, err := json.Marshal(map[string]keeper.ReceiptKeys{"receipt_keys": keys})
	require.NoError(t, err)
	require.NoError(t, env.module.ValidateGenesis(nil, nil, encoded))
	env.module.InitGenesis(env.ctx, nil, encoded)
}

func (env receiptKeysEnv) proposal(t *testing.T, keys keeper.ReceiptKeys) *proposal.ParameterChangeProposal {
	t.Helper()
	value, err := env.amino.MarshalAsJSON(keys)
	require.NoError(t, err)
	return proposal.NewParameterChangeProposal("receipt keys", "rotate receipt keys", []proposal.ParamChange{
		proposal.NewParamChange(types.ModuleName, string(keeper.KeyReceiptKeys), string(value)),
	}, false)
}

type receiptSigner struct {
	keyID   [32]byte
	private ed25519.PrivateKey
}

func newReceiptSigner(seed byte) receiptSigner {
	var keyID [32]byte
	for i := range keyID {
		keyID[i] = seed
	}
	return receiptSigner{keyID: keyID, private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))}
}

func (s receiptSigner) entry(first, last uint64) keeper.ReceiptKeyEntry {
	return keeper.ReceiptKeyEntry{
		KeyId:       bytes.Clone(s.keyID[:]),
		PublicKey:   bytes.Clone(s.private.Public().(ed25519.PublicKey)),
		FirstHeight: first,
		LastHeight:  last,
	}
}

// signedReceipt rewrites the batch_id and context_hash of a valid fixture
// receipt and signs it with signer.
func signedReceipt(t *testing.T, batchID, contextHash [32]byte, signer receiptSigner) []byte {
	t.Helper()
	fixture, err := testvectors.Load()
	require.NoError(t, err)
	var raw []byte
	for _, vector := range fixture["receipts"] {
		if vector.Name == "valid-0" {
			raw, err = vector.Bytes("receipt")
			require.NoError(t, err)
		}
	}
	require.NotEmpty(t, raw)
	original, err := codec.DecodeReceipt(raw)
	require.NoError(t, err)
	require.NotNil(t, original.SequencerSignature)
	raw = bytes.Clone(raw)
	batchAt := bytes.Index(raw, original.BatchID[:])
	contextAt := bytes.Index(raw, original.ContextHash[:])
	require.Positive(t, batchAt)
	require.Positive(t, contextAt)
	copy(raw[batchAt:], batchID[:])
	copy(raw[contextAt:], contextHash[:])

	const signatureBlock = 1 + 4 + ed25519.SignatureSize
	body := raw[:len(raw)-signatureBlock]
	digest := codec.ReceiptDigest(append(bytes.Clone(body), 0))
	signed := append(bytes.Clone(raw[:len(raw)-ed25519.SignatureSize]), ed25519.Sign(signer.private, digest[:])...)

	decoded, err := codec.DecodeReceipt(signed)
	require.NoError(t, err)
	require.Equal(t, batchID, decoded.BatchID)
	require.Equal(t, contextHash, decoded.ContextHash)
	require.Equal(t, digest, decoded.Digest())
	return signed
}

func heightBatchID(height uint64) [32]byte {
	var batchID [32]byte
	binary.BigEndian.PutUint64(batchID[24:], height)
	return batchID
}

func TestReceiptKeysRangeValidation(t *testing.T) {
	k1, k2 := newReceiptSigner(0x51), newReceiptSigner(0x52)

	reversed := k2.entry(7000, 6999)
	require.ErrorIs(t, reversed.Validate(), keeper.ErrReceiptKeyRange)
	require.ErrorIs(t, keeper.ReceiptKeys{k1.entry(5000, 6999), reversed}.Validate(), keeper.ErrReceiptKeyRange)
	require.NoError(t, k2.entry(7000, 7000).Validate())
	require.NoError(t, keeper.ReceiptKeys{k1.entry(5000, 7999), k2.entry(7000, math.MaxUint64)}.Validate())
	require.NoError(t, keeper.ReceiptKeys{}.Validate())

	for name, mutate := range map[string]func(*keeper.ReceiptKeyEntry){
		"short keyId":          func(e *keeper.ReceiptKeyEntry) { e.KeyId = e.KeyId[:31] },
		"long keyId":           func(e *keeper.ReceiptKeyEntry) { e.KeyId = append(e.KeyId, 0) },
		"zero keyId":           func(e *keeper.ReceiptKeyEntry) { e.KeyId = make([]byte, 32) },
		"short public key":     func(e *keeper.ReceiptKeyEntry) { e.PublicKey = e.PublicKey[:31] },
		"missing public key":   func(e *keeper.ReceiptKeyEntry) { e.PublicKey = nil },
		"identity public key":  func(e *keeper.ReceiptKeyEntry) { e.PublicKey = append([]byte{0x01}, make([]byte, 31)...) },
		"unreduced public key": func(e *keeper.ReceiptKeyEntry) { e.PublicKey = bytes.Repeat([]byte{0xff}, 32) },
		"zero public key":      func(e *keeper.ReceiptKeyEntry) { e.PublicKey = make([]byte, 32) },
		"field prime public key": func(e *keeper.ReceiptKeyEntry) {
			e.PublicKey = append([]byte{0xed}, append(bytes.Repeat([]byte{0xff}, 30), 0x7f)...)
		},
	} {
		entry := k1.entry(5000, 6999)
		mutate(&entry)
		require.ErrorIs(t, entry.Validate(), keeper.ErrInvalidReceiptKey, name)
	}
	require.ErrorIs(t, keeper.ReceiptKeys{k1.entry(5000, 6999), k1.entry(7000, 7999)}.Validate(), keeper.ErrInvalidReceiptKey)

	env := newReceiptKeysEnv(t, 6000)
	pinned := keeper.ReceiptKeys{k1.entry(5000, math.MaxUint64)}
	env.initGenesis(t, pinned)

	err := env.handler(env.ctx, env.proposal(t, keeper.ReceiptKeys{k1.entry(5000, 6999), reversed}))
	require.ErrorIs(t, err, proposal.ErrSettingParameter)
	require.ErrorContains(t, err, keeper.ErrReceiptKeyRange.Error())
	require.Equal(t, pinned, keeper.GetReceiptKeys(env.ctx, env.paramSpace))

	err = env.handler(env.ctx, env.proposal(t, keeper.ReceiptKeys{k1.entry(5000, 6999), k1.entry(7000, math.MaxUint64)}))
	require.ErrorContains(t, err, "pinned twice")
	require.Equal(t, pinned, keeper.GetReceiptKeys(env.ctx, env.paramSpace))

	encoded, err := json.Marshal(map[string]keeper.ReceiptKeys{"receipt_keys": {reversed}})
	require.NoError(t, err)
	require.ErrorIs(t, env.module.ValidateGenesis(nil, nil, encoded), keeper.ErrReceiptKeyRange)
	require.ErrorIs(t, keeper.InitReceiptKeys(env.ctx, env.paramSpace, keeper.ReceiptKeys{reversed}), keeper.ErrReceiptKeyRange)
	require.Equal(t, pinned, keeper.GetReceiptKeys(env.ctx, env.paramSpace))
}

func TestReceiptKeysGovernanceOnly(t *testing.T) {
	k1, k2, k3 := newReceiptSigner(0x51), newReceiptSigner(0x52), newReceiptSigner(0x53)
	env := newReceiptKeysEnv(t, 6000)

	require.Empty(t, keeper.GetReceiptKeys(env.ctx, env.paramSpace))
	require.JSONEq(t, `{"receipt_keys":[]}`, string(env.module.DefaultGenesis(nil)))
	pinned := keeper.ReceiptKeys{k1.entry(5000, math.MaxUint64)}
	env.initGenesis(t, pinned)
	require.Equal(t, pinned, keeper.GetReceiptKeys(env.ctx, env.paramSpace))

	// The module routes no message, so no Msg reaches the list.
	require.True(t, env.module.Route().Empty())
	require.Nil(t, env.module.LegacyQuerierHandler(nil))

	for name, refused := range map[string]keeper.ReceiptKeys{
		"new key starting at the current height": {k1.entry(5000, math.MaxUint64), k2.entry(6000, math.MaxUint64)},
		"new key starting in the past":           {k1.entry(5000, math.MaxUint64), k2.entry(4000, math.MaxUint64)},
		"closing a key below the current height": {k1.entry(5000, 5999), k2.entry(6001, math.MaxUint64)},
		"moving a pinned firstHeight":            {k1.entry(5001, math.MaxUint64)},
		"replacing a pinned public key": {{
			KeyId: bytes.Clone(k1.keyID[:]), PublicKey: bytes.Clone(k2.private.Public().(ed25519.PublicKey)), FirstHeight: 5000, LastHeight: math.MaxUint64,
		}},
	} {
		require.ErrorIs(t, env.handler(env.ctx, env.proposal(t, refused)), keeper.ErrReceiptKeyRotation, name)
		require.Equal(t, pinned, keeper.GetReceiptKeys(env.ctx, env.paramSpace), name)
	}

	rotated := keeper.ReceiptKeys{k1.entry(5000, 6999), k2.entry(7000, math.MaxUint64), k3.entry(6001, math.MaxUint64)}
	require.NoError(t, env.handler(env.ctx, env.proposal(t, rotated)))
	require.Equal(t, rotated, keeper.GetReceiptKeys(env.ctx, env.paramSpace))

	var exported map[string]keeper.ReceiptKeys
	require.NoError(t, json.Unmarshal(env.module.ExportGenesis(env.ctx, nil), &exported))
	require.Equal(t, map[string]keeper.ReceiptKeys{"receipt_keys": rotated}, exported)

	later := env.ctx.WithBlockHeight(7500)
	reopened := keeper.ReceiptKeys{k1.entry(5000, math.MaxUint64), k2.entry(7000, math.MaxUint64), k3.entry(6001, math.MaxUint64)}
	require.ErrorIs(t, env.handler(later, env.proposal(t, reopened)), keeper.ErrReceiptKeyRotation)
	withoutK1 := keeper.ReceiptKeys{k2.entry(7000, math.MaxUint64), k3.entry(6001, 7999)}
	require.NoError(t, env.handler(later, env.proposal(t, withoutK1)))
	require.Equal(t, withoutK1, keeper.GetReceiptKeys(later, env.paramSpace))

	require.ErrorIs(t, keeper.ValidateRotation(pinned, pinned, -1), keeper.ErrReceiptKeyRotation)

	other := proposal.NewParameterChangeProposal("other", "other", []proposal.ParamChange{
		proposal.NewParamChange("absent", "Key", "1"),
	}, false)
	require.ErrorIs(t, env.handler(env.ctx, other), proposal.ErrUnknownSubspace)
}

func TestReceiptKeysRotationBoundary(t *testing.T) {
	k1, k2, k3 := newReceiptSigner(0x51), newReceiptSigner(0x52), newReceiptSigner(0x53)
	keys := keeper.ReceiptKeys{k1.entry(5000, 6999), k2.entry(7000, math.MaxUint64)}
	require.NoError(t, keys.Validate())

	entry, err := keys.KeyForHeight(k1.keyID, 6999)
	require.NoError(t, err)
	require.Equal(t, k1.entry(5000, 6999), entry)
	entry, err = keys.KeyForHeight(k2.keyID, 7000)
	require.NoError(t, err)
	require.Equal(t, k2.entry(7000, math.MaxUint64), entry)
	entry, err = keys.KeyForHeight(k2.keyID, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, k2.entry(7000, math.MaxUint64), entry)
	for _, refused := range []struct {
		keyID  [32]byte
		height uint64
	}{{k1.keyID, 7000}, {k1.keyID, 4999}, {k2.keyID, 6999}, {k3.keyID, 7000}} {
		entry, err = keys.KeyForHeight(refused.keyID, refused.height)
		require.ErrorIs(t, err, keeper.ErrReceiptKeyNotPinned)
		require.Equal(t, keeper.ReceiptKeyEntry{}, entry)
	}

	below := signedReceipt(t, heightBatchID(6999), k1.keyID, k1)
	verified, err := keys.VerifyReceipt(below)
	require.NoError(t, err)
	require.Equal(t, k1.keyID, verified.Receipt.ContextHash)
	height, err := keeper.ReceiptHeight(verified.Receipt)
	require.NoError(t, err)
	require.Equal(t, uint64(6999), height)

	at := signedReceipt(t, heightBatchID(7000), k2.keyID, k2)
	verified, err = keys.VerifyReceipt(at)
	require.NoError(t, err)
	require.Equal(t, k2.keyID, verified.Receipt.ContextHash)

	_, err = keys.VerifyReceipt(signedReceipt(t, heightBatchID(7000), k1.keyID, k1))
	require.ErrorIs(t, err, keeper.ErrReceiptKeyNotPinned)
	_, err = keys.VerifyReceipt(signedReceipt(t, heightBatchID(7000), k2.keyID, k1))
	require.ErrorIs(t, err, verify.ErrSequencerSignature)
	_, err = keys.VerifyReceipt(signedReceipt(t, heightBatchID(6999), k2.keyID, k2))
	require.ErrorIs(t, err, keeper.ErrReceiptKeyNotPinned)
	notHeight := heightBatchID(7000)
	notHeight[0] = 0x01
	_, err = keys.VerifyReceipt(signedReceipt(t, notHeight, k2.keyID, k2))
	require.ErrorIs(t, err, keeper.ErrReceiptBatchID)
	_, err = keys.VerifyReceipt(below[:len(below)-1])
	require.Error(t, err)

	// Receipts below the rotation verify under K1 only while the list holds it.
	_, err = keeper.ReceiptKeys{k2.entry(7000, math.MaxUint64)}.VerifyReceipt(below)
	require.ErrorIs(t, err, keeper.ErrReceiptKeyNotPinned)
}
