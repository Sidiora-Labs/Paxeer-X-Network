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

const (
	assetRegisterType    = 0x00010001
	assetAccountOpenType = 0x00010004
	assetMintType        = 0x0001000a
	moduleValueKind      = 13
	agentAssetKind       = 14
)

type accountRow struct {
	id        [32]byte
	name      string
	kind      uint8
	balance   uint64
	asset     [32]byte
	hasAsset  bool
	next      uint64
	created   uint64
	frozen    bool
	reference bool
	key       [32]byte
	hasKey    bool
}

func parseAccountRow(t *testing.T, key, value []byte) accountRow {
	t.Helper()
	var row accountRow
	cursor := 0
	take := func(n int) []byte {
		require.LessOrEqual(t, cursor+n, len(value))
		out := value[cursor : cursor+n]
		cursor += n
		return out
	}
	fixed := func(out *[32]byte) {
		require.Equal(t, uint32(32), binary.BigEndian.Uint32(take(4)))
		copy(out[:], take(32))
	}
	flag := func() bool {
		b := take(1)[0]
		require.LessOrEqual(t, b, byte(1))
		return b == 1
	}
	fixed(&row.id)
	row.name = string(take(int(binary.BigEndian.Uint32(take(4)))))
	row.kind = take(1)[0]
	balance := take(16)
	require.Equal(t, make([]byte, 8), balance[:8])
	row.balance = binary.BigEndian.Uint64(balance[8:])
	fixed(&row.asset)
	row.hasAsset = flag()
	row.next = binary.BigEndian.Uint64(take(8))
	row.created = binary.BigEndian.Uint64(take(8))
	row.frozen = flag()
	row.reference = flag()
	fixed(&row.key)
	row.hasKey = flag()
	require.Equal(t, len(value), cursor)
	require.Equal(t, accountKey(row.id), key)
	derived, err := kernelAccountID([]byte(row.name))
	require.NoError(t, err)
	require.Equal(t, row.id, derived)
	return row
}

func accountRows(t *testing.T, c *testChain) map[[32]byte]accountRow {
	t.Helper()
	rows := map[[32]byte]accountRow{}
	for key, value := range c.dump() {
		if key[0] == accountPrefix {
			row := parseAccountRow(t, []byte(key), value)
			rows[row.id] = row
		}
	}
	return rows
}

func rawAccountRows(c *testChain) map[string][]byte {
	rows := map[string][]byte{}
	for key, value := range c.dump() {
		if key[0] == accountPrefix {
			rows[key] = value
		}
	}
	return rows
}

type testAsset struct {
	id       [32]byte
	issuance [32]byte
	account  [32]byte
	name     string
}

func newTestAsset(t *testing.T, issuer testActor, salt byte) testAsset {
	t.Helper()
	var asset testAsset
	material := append([]byte("LX:ASSET:v1"), issuer.id[:]...)
	asset.id = sha256.Sum256(append(material, bytes.Repeat([]byte{salt}, 32)...))
	_, issuance, err := IssuanceAccountID(asset.id)
	require.NoError(t, err)
	asset.issuance = issuance
	asset.name = "agent:" + string(issuer.did) + ":asset:" + hex.EncodeToString(asset.id[:])
	account, err := kernelAccountID([]byte(asset.name))
	require.NoError(t, err)
	asset.account = account
	return asset
}

func (a testActor) registerAsset(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64, asset testAsset, salt byte, supplyCap uint64) Activity {
	payload := append([]byte{0, 1}, asset.id[:]...)
	payload = append(payload, bytes.Repeat([]byte{salt}, 32)...)
	payload = append(payload, 1, 'T', 1, 'T', 6)
	payload = binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(payload, 0), supplyCap)
	payload = append(payload, 1, 0)
	return a.activity(t, h, assetRegisterType, accountSequence, blockTimeMs, payload)
}

func (a testActor) openAssetAccount(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64, asset testAsset) Activity {
	return a.activity(t, h, assetAccountOpenType, accountSequence, blockTimeMs, append([]byte{0, 1}, asset.id[:]...))
}

func (a testActor) mint(t *testing.T, h *Host, accountSequence uint64, blockTimeMs int64, asset testAsset, amount uint64) Activity {
	payload := append(append([]byte{0, 1}, asset.id[:]...), asset.account[:]...)
	payload = binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(payload, 0), amount)
	return a.activity(t, h, assetMintType, accountSequence, blockTimeMs, payload)
}

func TestHostAssetLedger(t *testing.T) {
	const blockTime = int64(1700000000000)
	chain := newTestChain(t)
	host := chain.host(t)
	issuer := newTestActor(t, 21)
	asset := newTestAsset(t, issuer, 0x33)

	registered := execute(t, host, chain.context(1, blockTime, 41), issuer.registerAsset(t, host, 0, blockTime, asset, 0x33, 1000), issuer.authority())
	require.Equal(t, Code(0), registered.ResultCode)
	chain.commit()
	rows := accountRows(t, chain)
	require.Len(t, rows, 1)
	issuance := rows[asset.issuance]
	require.Equal(t, uint8(moduleValueKind), issuance.kind)
	require.Equal(t, uint64(1000), issuance.balance)
	require.True(t, issuance.hasAsset)
	require.Equal(t, asset.id, issuance.asset)
	require.Equal(t, uint64(41), issuance.created)
	require.False(t, issuance.hasKey)

	before := chain.dump()
	ctx := chain.context(2, blockTime+1000, 42)
	branch, _ := ctx.CacheContext()
	aborted := execute(t, host, branch, issuer.openAssetAccount(t, host, 1, blockTime+1000, asset), issuer.authority())
	require.Equal(t, Code(0), aborted.ResultCode)
	require.Nil(t, chain.get(accountKey(asset.account)))
	require.Equal(t, before, chain.dump())

	opened := execute(t, host, ctx, issuer.openAssetAccount(t, host, 1, blockTime+1000, asset), issuer.authority())
	require.Equal(t, Code(0), opened.ResultCode)
	require.Equal(t, aborted.PreviousStateRoot, opened.PreviousStateRoot)
	chain.commit()
	rows = accountRows(t, chain)
	require.Len(t, rows, 2)
	holder := rows[asset.account]
	require.Equal(t, asset.name, holder.name)
	require.Equal(t, uint8(agentAssetKind), holder.kind)
	require.Equal(t, uint64(0), holder.balance)
	require.Equal(t, asset.id, holder.asset)
	require.True(t, holder.hasKey)
	require.Equal(t, issuer.public, holder.key)
	require.Equal(t, uint64(42), holder.created)
	require.Equal(t, issuance, rows[asset.issuance])

	minted := execute(t, host, chain.context(3, blockTime+2000, 43), issuer.mint(t, host, 2, blockTime+2000, asset, 250), issuer.authority())
	require.Equal(t, Code(0), minted.ResultCode)
	require.Equal(t, opened.ResultingStateRoot, minted.PreviousStateRoot)
	chain.commit()
	rows = accountRows(t, chain)
	require.Len(t, rows, 2)
	require.Equal(t, uint64(750), rows[asset.issuance].balance)
	require.Equal(t, uint64(250), rows[asset.account].balance)
	require.Equal(t, holder.next, rows[asset.account].next)
}

func TestHostAccountsRestore(t *testing.T) {
	const blockTime = int64(1700000000000)
	chain := newTestChain(t)
	first := chain.host(t)
	issuer := newTestActor(t, 22)
	asset := newTestAsset(t, issuer, 0x44)
	steps := []Activity{
		issuer.registerAsset(t, first, 0, blockTime, asset, 0x44, 1000),
		issuer.openAssetAccount(t, first, 1, blockTime, asset),
		issuer.mint(t, first, 2, blockTime, asset, 250),
	}
	for index, activity := range steps {
		receipt := execute(t, first, chain.context(1, blockTime, uint64(41+index)), activity, issuer.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
	}
	chain.commit()
	committed := rawAccountRows(chain)
	require.Len(t, committed, 2)

	second := chain.host(t)
	refused := execute(t, second, chain.context(2, blockTime+1000, 44), issuer.mint(t, second, 3, blockTime+1000, asset, 751), issuer.authority())
	require.Equal(t, "LXP_ERR_INSUFFICIENT_BALANCE", refused.ResultCode.Error())
	restored, err := committedAccounts(second.accountRegistry())
	require.NoError(t, err)
	require.Equal(t, committed, restored)
	require.Equal(t, committed, rawAccountRows(chain))

	minted := execute(t, second, chain.context(2, blockTime+1000, 45), issuer.mint(t, second, 4, blockTime+1000, asset, 100), issuer.authority())
	require.Equal(t, Code(0), minted.ResultCode)
	chain.commit()
	rows := accountRows(t, chain)
	require.Len(t, rows, 2)
	require.Equal(t, uint64(650), rows[asset.issuance].balance)
	require.Equal(t, uint64(350), rows[asset.account].balance)

	third := chain.host(t)
	replayed, err := third.Execute(chain.context(3, blockTime+2000, 45), issuer.mint(t, third, 5, blockTime+2000, asset, 1), issuer.authority())
	require.Equal(t, codeSequenceReused, err)
	require.Equal(t, Receipt{}, replayed)
	require.Equal(t, rows, accountRows(t, chain))
}
