//go:build cgo

package lxphost

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	testNetworkID     = 7
	registerType      = 0x00070001
	grantType         = 0x00070008
	programsRegistry  = 0x00090004
	stateCommitment   = 3
	governanceModule  = 7
	identityStateSize = 223
	revocationOffset  = 69
	sequenceOffset    = 215
)

type testChain struct {
	key *sdk.KVStoreKey
	cms sdk.CommitMultiStore
}

func newTestChain(t *testing.T) *testChain {
	t.Helper()
	key := sdk.NewKVStoreKey("lxphost")
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db)
	cms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())
	return &testChain{key: key, cms: cms}
}

func (c *testChain) context(height int64, blockTimeMs int64, sequence uint64) sdk.Context {
	header := tmproto.Header{ChainID: "lxphost", Height: height, Time: time.UnixMilli(blockTimeMs)}
	return WithSequence(sdk.NewContext(c.cms, header, false), sequence)
}

func (c *testChain) commit() []byte {
	c.cms.Commit(true)
	state := c.dump()
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	for _, key := range keys {
		for _, part := range [][]byte{[]byte(key), state[key]} {
			digest.Write(binary.BigEndian.AppendUint32(nil, uint32(len(part))))
			digest.Write(part)
		}
	}
	return digest.Sum(nil)
}

func (c *testChain) host(t *testing.T) *Host {
	t.Helper()
	host, err := NewHost(Config{
		StoreKey:          c.key,
		NetworkID:         testNetworkID,
		ParameterVersion:  1,
		TimestampWindowMs: 60000,
		GasLimit:          1000000,
	})
	require.NoError(t, err)
	t.Cleanup(host.Close)
	return host
}

func (c *testChain) dump() map[string][]byte {
	out := map[string][]byte{}
	ctx := sdk.NewContext(c.cms, tmproto.Header{}, false)
	iterator := ctx.KVStore(c.key).Iterator(nil, nil)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		out[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return out
}

func (c *testChain) get(key []byte) []byte {
	return sdk.NewContext(c.cms, tmproto.Header{}, false).KVStore(c.key).Get(key)
}

type testActor struct {
	private ed25519.PrivateKey
	public  [32]byte
	did     []byte
	id      [32]byte
}

func newTestActor(t *testing.T, seed byte) testActor {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	var actor testActor
	actor.private = private
	copy(actor.public[:], private.Public().(ed25519.PublicKey))
	actor.did = []byte("did:layerx:" + hex.EncodeToString(actor.public[:]))
	id, err := DIDID(actor.did)
	require.NoError(t, err)
	actor.id = id
	return actor
}

func (a testActor) authority() KernelAuthority {
	return KernelAuthority{Actor: a.id, Principal: a.id, VerifiedKey: a.public}
}

func (a testActor) activity(t *testing.T, h *Host, activityType uint32, accountSequence uint64, blockTimeMs int64, payload []byte) Activity {
	t.Helper()
	idempotency := sha256.Sum256(append(append([]byte(nil), a.did...), byte(accountSequence), byte(activityType)))
	activity := Activity{
		ProtocolVersion: stateCommitment,
		Type:            activityType,
		ActorDID:        a.did,
		AuthorityKey:    a.public,
		AccountSequence: accountSequence,
		NotBefore:       uint64(blockTimeMs) - 1000,
		NotAfter:        uint64(blockTimeMs) + 1000,
		IdempotencyKey:  idempotency,
		Payload:         payload,
	}
	preimage, err := h.SigningHash(activity)
	require.NoError(t, err)
	activity.Signature = ed25519.Sign(a.private, preimage[:])
	return activity
}

func (a testActor) register(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64) Activity {
	payload := append([]byte{0x71, 1, 0, 2}, a.id[:]...)
	return a.activity(t, h, registerType, accountSequence, blockTimeMs, append(payload, a.public[:]...))
}

func (a testActor) registry(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64, program byte, body []byte) Activity {
	payload := append(bytes.Repeat([]byte{program}, 32), body...)
	return a.activity(t, h, programsRegistry, accountSequence, blockTimeMs, payload)
}

func (a testActor) grant(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64, key [32]byte, notBefore, notAfter, revocation uint64) Activity {
	var body []byte
	u8 := func(v uint8) { body = append(body, v) }
	u16 := func(v uint16) { body = binary.BigEndian.AppendUint16(body, v) }
	u64 := func(v uint64) { body = binary.BigEndian.AppendUint64(body, v) }
	u128 := func(v uint64) { u64(0); u64(v) }
	field := func(b []byte) { body = binary.BigEndian.AppendUint32(body, uint32(len(b))); body = append(body, b...) }
	u16(1)
	u16(0x2001)
	u8(1)
	field(a.id[:])
	field(a.id[:])
	u8(3)
	field(key[:])
	u64(1 << 9)
	u16(4)
	u16(4)
	field(bytes.Repeat([]byte{0x11}, 32))
	u128(1)
	u128(10)
	u128(0)
	u64(0)
	u128(0)
	u128(0)
	u64(0)
	field(bytes.Repeat([]byte{0x22}, 32))
	u64(notBefore)
	u64(notAfter)
	u64(revocation)
	u8(0)
	u64(0)
	field(make([]byte, 64))
	payload := binary.BigEndian.AppendUint32([]byte{0x71, 8, 1, 1}, uint32(len(body)))
	return a.activity(t, h, grantType, accountSequence, blockTimeMs, append(payload, body...))
}

func execute(t *testing.T, h *Host, ctx sdk.Context, activity Activity, authority KernelAuthority) Receipt {
	t.Helper()
	receipt, err := h.Execute(ctx, activity, authority)
	require.NoError(t, err)
	return receipt
}

func governanceKV(c *testChain, prefix []byte) map[string][]byte {
	out := map[string][]byte{}
	full := moduleKVKey(governanceModule, prefix)
	for key, value := range c.dump() {
		if bytes.HasPrefix([]byte(key), full) {
			out[key] = value
		}
	}
	return out
}

func TestHostLinksKernel(t *testing.T) {
	chain := newTestChain(t)
	host := chain.host(t)
	require.NotNil(t, host.node)
	require.Equal(t, "LXP_OK", Code(0).Error())
	require.Equal(t, Code(-301), codeSequenceReused)
	require.Equal(t, "LXP_ERR_SEQUENCE_REUSED", codeSequenceReused.Error())
	require.Equal(t, "LXP_ERR_AUTH_SCOPE", Code(-204).Error())

	_, err := NewHost(Config{NetworkID: testNetworkID, ParameterVersion: 1, GasLimit: 1})
	require.Equal(t, codeNonCanonical, err)

	actor := newTestActor(t, 1)
	receipt := execute(t, host, chain.context(1, 1700000000000, 1), actor.register(t, host, 0, 1700000000000), actor.authority())
	require.Equal(t, Code(0), receipt.ResultCode)
	require.Equal(t, uint64(1), receipt.GlobalSequence)
	require.NotEmpty(t, receipt.Encoded)
	require.NotEqual(t, receipt.PreviousStateRoot, receipt.ResultingStateRoot)
}

func TestHostGlobalSequence(t *testing.T) {
	const blockTime = int64(1700000000123)
	chain := newTestChain(t)
	host := chain.host(t)
	actor := newTestActor(t, 2)

	receipt := execute(t, host, chain.context(3, blockTime, 41), actor.register(t, host, 0, blockTime), actor.authority())
	require.Equal(t, Code(0), receipt.ResultCode)
	require.Equal(t, uint64(41), receipt.GlobalSequence)
	require.Equal(t, uint64(1700000000123), receipt.Timestamp)

	state := chain.get(moduleKVKey(governanceModule, actor.id[:]))
	require.Len(t, state, identityStateSize)
	require.Equal(t, []byte("LXGI1"), state[:5])
	require.Equal(t, uint64(41), binary.BigEndian.Uint64(state[revocationOffset:]))
	require.Equal(t, uint64(41), binary.BigEndian.Uint64(state[sequenceOffset:]))
	require.Equal(t, uint64(41), binary.BigEndian.Uint64(chain.get([]byte{lastSequenceKey})))
	require.Equal(t, uint64(1), binary.BigEndian.Uint64(chain.get(identitySequenceKey(actor.id))))

	other := newTestActor(t, 3)
	next := execute(t, host, chain.context(3, blockTime, 42), other.register(t, host, 0, blockTime), other.authority())
	require.Equal(t, Code(0), next.ResultCode)
	require.Equal(t, uint64(42), next.GlobalSequence)
	require.Equal(t, receipt.ResultingStateRoot, next.PreviousStateRoot)
	otherState := chain.get(moduleKVKey(governanceModule, other.id[:]))
	require.Equal(t, uint64(42), binary.BigEndian.Uint64(otherState[sequenceOffset:]))
}

func TestHostNoAmbientInputs(t *testing.T) {
	const blockTime = int64(1700000000123)
	run := func(seed byte) Receipt {
		chain := newTestChain(t)
		host := chain.host(t)
		actor := newTestActor(t, seed)
		return execute(t, host, chain.context(9, blockTime, 41), actor.register(t, host, 0, blockTime), actor.authority())
	}
	first := run(4)
	require.Equal(t, uint64(1700000000123), first.Timestamp)
	require.Equal(t, uint64(41), first.GlobalSequence)
	time.Sleep(5 * time.Millisecond)
	second := run(4)
	require.Equal(t, first, second)

	const grantTime = int64(1700000000000)
	chain := newTestChain(t)
	host := chain.host(t)
	actor := newTestActor(t, 5)
	delegate := newTestActor(t, 6)
	registered := execute(t, host, chain.context(1, 1699999999000, 40), actor.register(t, host, 0, 1699999999000), actor.authority())
	require.Equal(t, Code(0), registered.ResultCode)
	chain.commit()
	before := governanceKV(chain, nil)
	require.Len(t, before, 1)

	refused := execute(t, host, chain.context(2, grantTime, 41),
		actor.grant(t, host, 1, grantTime, delegate.public, 1699999990000, uint64(grantTime), 40), actor.authority())
	require.Equal(t, Code(-204), refused.ResultCode)
	require.Equal(t, "LXP_ERR_AUTH_SCOPE", refused.ResultCode.Error())
	require.Equal(t, uint64(grantTime), refused.Timestamp)
	require.Equal(t, before, governanceKV(chain, nil))
	require.Empty(t, governanceKV(chain, []byte{5}))

	issued := execute(t, host, chain.context(2, grantTime, 42),
		actor.grant(t, host, 2, grantTime, delegate.public, 1699999990000, uint64(grantTime)+1, 40), actor.authority())
	require.Equal(t, Code(0), issued.ResultCode)
	require.Len(t, governanceKV(chain, []byte{5}), 1)
}

func TestHostSequenceMonotonic(t *testing.T) {
	const blockTime = int64(1700000000000)
	chain := newTestChain(t)
	host := chain.host(t)
	actor := newTestActor(t, 7)
	other := newTestActor(t, 8)
	receipt := execute(t, host, chain.context(1, blockTime, 41), actor.register(t, host, 0, blockTime), actor.authority())
	require.Equal(t, Code(0), receipt.ResultCode)
	chain.commit()
	before := chain.dump()

	for _, sequence := range []uint64{41, 40, 1} {
		_, err := host.Execute(chain.context(2, blockTime+1000, sequence), other.register(t, host, 0, blockTime+1000), other.authority())
		require.Equal(t, codeSequenceReused, err)
		require.Equal(t, "LXP_ERR_SEQUENCE_REUSED", err.Error())
		require.Equal(t, before, chain.dump())
	}

	_, err := host.Execute(sdk.NewContext(chain.cms, tmproto.Header{Height: 2, Time: time.UnixMilli(blockTime)}, false),
		other.register(t, host, 0, blockTime), other.authority())
	require.Equal(t, codeNonCanonical, err)
	require.Equal(t, before, chain.dump())

	next := execute(t, host, chain.context(2, blockTime+1000, 42), other.register(t, host, 0, blockTime+1000), other.authority())
	require.Equal(t, Code(0), next.ResultCode)
	require.Equal(t, uint64(42), next.GlobalSequence)
}

func TestHostNoStateAcrossTxs(t *testing.T) {
	const blockTime = int64(1700000000000)
	first := newTestActor(t, 9)
	second := newTestActor(t, 10)

	chain := newTestChain(t)
	host := chain.host(t)
	ctx := chain.context(1, blockTime, 41)
	branch, _ := ctx.CacheContext()
	aborted := execute(t, host, branch, first.register(t, host, 0, blockTime), first.authority())
	require.Equal(t, Code(0), aborted.ResultCode)
	require.Empty(t, chain.dump())

	withAbort := execute(t, host, ctx, second.register(t, host, 0, blockTime), second.authority())
	require.Equal(t, Code(0), withAbort.ResultCode)
	require.Nil(t, chain.get(moduleKVKey(governanceModule, first.id[:])))
	require.Nil(t, chain.get(identitySequenceKey(first.id)))
	hash := chain.commit()

	reference := newTestChain(t)
	referenceHost := reference.host(t)
	withoutAbort := execute(t, referenceHost, reference.context(1, blockTime, 41), second.register(t, referenceHost, 0, blockTime), second.authority())
	require.Equal(t, withoutAbort, withAbort)
	require.Equal(t, aborted.PreviousStateRoot, withAbort.PreviousStateRoot)
	require.Equal(t, reference.commit(), hash)

	again := execute(t, host, chain.context(2, blockTime+1000, 42), first.register(t, host, 0, blockTime+1000), first.authority())
	require.Equal(t, Code(0), again.ResultCode)
	require.Equal(t, withAbort.ResultingStateRoot, again.PreviousStateRoot)
}

func TestHostDeterministicAppHash(t *testing.T) {
	const blockTime = int64(1700000000000)
	alice := newTestActor(t, 11)
	bob := newTestActor(t, 12)
	type step struct {
		actor    testActor
		build    func(*Host) Activity
		expected Code
	}
	blocks := [][]step{
		{
			{alice, func(h *Host) Activity { return alice.register(t, h, 0, blockTime) }, 0},
			{bob, func(h *Host) Activity { return bob.register(t, h, 0, blockTime) }, 0},
			{alice, func(h *Host) Activity { return alice.registry(t, h, 1, blockTime, 0xA1, []byte("manifest")) }, 0},
		},
		{
			{bob, func(h *Host) Activity { return bob.registry(t, h, 1, blockTime+1000, 0xB2, nil) }, 0},
			{alice, func(h *Host) Activity { return alice.registry(t, h, 2, blockTime+1000, 0x00, []byte("zero")) }, Code(-7)},
			{bob, func(h *Host) Activity { return bob.registry(t, h, 2, blockTime+1000, 0xB3, []byte{1, 2, 3}) }, 0},
		},
	}
	run := func() ([][]byte, [][]byte) {
		chain := newTestChain(t)
		host := chain.host(t)
		var receipts, hashes [][]byte
		sequence := uint64(100)
		for index, block := range blocks {
			height := int64(index + 1)
			timestamp := blockTime + int64(index)*1000
			for _, s := range block {
				sequence++
				receipt := execute(t, host, chain.context(height, timestamp, sequence), s.build(host), s.actor.authority())
				require.Equal(t, s.expected, receipt.ResultCode)
				require.Equal(t, sequence, receipt.GlobalSequence)
				require.Equal(t, uint64(timestamp), receipt.Timestamp)
				receipts = append(receipts, receipt.Encoded)
			}
			hashes = append(hashes, chain.commit())
		}
		return receipts, hashes
	}
	receipts, hashes := run()
	time.Sleep(5 * time.Millisecond)
	replayReceipts, replayHashes := run()
	require.Len(t, receipts, 6)
	require.Equal(t, receipts, replayReceipts)
	require.Equal(t, hashes, replayHashes)
	require.NotEqual(t, hashes[0], hashes[1])
}
