//go:build cgo

package lxphost

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func TestReadBridgeNoRawKeys(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, prefix := range []string{"asset", "grant"} {
			require.NotContains(t, string(source), prefix+":", path)
		}
	}
}

func readBridgeSession(t *testing.T, actor, session testActor, action [32]byte,
	notBefore, notAfter, revocation, expiry uint64) ([]byte, [32]byte, []byte) {
	t.Helper()
	body := []byte{0, 1, 0x20, 1, 1}
	field := func(value []byte) {
		body = binary.BigEndian.AppendUint32(body, uint32(len(value)))
		body = append(body, value...)
	}
	u64 := func(value uint64) { body = binary.BigEndian.AppendUint64(body, value) }
	field(actor.id[:])
	field(actor.id[:])
	body = append(body, 2)
	field(session.public[:])
	u64(1 << 9)
	body = binary.BigEndian.AppendUint16(body, 4)
	body = binary.BigEndian.AppendUint16(body, 4)
	field(make([]byte, 32))
	body = append(body, make([]byte, 48)...)
	u64(0)
	body = append(body, make([]byte, 32)...)
	u64(0)
	field(make([]byte, 32))
	u64(notBefore)
	u64(notAfter)
	u64(revocation)
	body = append(body, 0)
	u64(0)
	field(make([]byte, ed25519.SignatureSize))
	id, err := codec.DomainHash(codec.DomainAuthorityHash, body)
	require.NoError(t, err)

	payload := binary.BigEndian.AppendUint32([]byte{0x71, 5, 1, 3}, uint32(len(body)))
	payload = append(payload, body...)
	payload = binary.BigEndian.AppendUint64(payload, expiry)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(action)))
	payload = append(payload, action[:]...)

	summary := append([]byte("LXGS2"), id[:]...)
	summary = append(summary, actor.id[:]...)
	summary = append(summary, actor.public[:]...)
	summary = append(summary, action[:]...)
	summary = append(summary, session.public[:]...)
	summary = binary.BigEndian.AppendUint64(summary, expiry)
	summary = binary.BigEndian.AppendUint64(summary, 1<<9)
	summary = binary.BigEndian.AppendUint16(summary, 4)
	summary = binary.BigEndian.AppendUint16(summary, 4)
	summary = binary.BigEndian.AppendUint64(summary, notBefore)
	summary = binary.BigEndian.AppendUint64(summary, notAfter)
	summary = binary.BigEndian.AppendUint64(summary, revocation)
	require.Len(t, summary, 209)
	return payload, id, summary
}

func readBridgePayerGrant(t *testing.T, actor testActor, from, recipient, asset [32]byte,
	expiration uint64) ([]byte, [32]byte) {
	t.Helper()
	body := append([]byte(nil), from[:]...)
	body = append(body, recipient[:]...)
	body = append(body, asset[:]...)
	body = binary.BigEndian.AppendUint64(body, 0)
	body = binary.BigEndian.AppendUint64(body, 40)
	body = binary.BigEndian.AppendUint64(body, 0)
	body = binary.BigEndian.AppendUint64(body, 100)
	body = append(body, 0)
	body = binary.BigEndian.AppendUint64(body, 0)
	body = binary.BigEndian.AppendUint64(body, expiration)
	body = append(body, bytes.Repeat([]byte{0x22}, 32)...)
	body = append(body, 0)
	body = append(body, make([]byte, 32)...)
	body = binary.BigEndian.AppendUint64(body, 0)
	body = append(body, actor.public[:]...)
	message := append([]byte("LXP:GRANT:v1"), body...)
	id, err := codec.DomainHash(codec.DomainAuthorityHash, message)
	require.NoError(t, err)
	grant := append(append([]byte(nil), id[:]...), body...)
	return append(grant, ed25519.Sign(actor.private, id[:])...), id
}

func TestReadBridgeValues(t *testing.T) {
	const blockTime = int64(10000)

	t.Run("session", func(t *testing.T) {
		chain := newTestChain(t)
		host := chain.host(t)
		actor := newTestActor(t, 31)
		session := newTestActor(t, 32)
		registration := actor.register(t, host, 0, blockTime)
		receipt := execute(t, host, chain.context(1, blockTime, 41), registration, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		var action [32]byte
		copy(action[:], bytes.Repeat([]byte{0x33}, 32))
		payload, id, expected := readBridgeSession(t, actor, session, action,
			uint64(blockTime)-1000, uint64(blockTime)+1000, 41, 100)
		activity := actor.activity(t, host, 0x00070005, 1, blockTime, payload)
		receipt = execute(t, host, chain.context(1, blockTime, 42), activity, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		ctx := chain.context(1, blockTime, 42)
		before := chain.dump()
		summary, err := host.ReadSessionKey(ctx, id)
		require.NoError(t, err)
		require.Equal(t, expected, summary)
		require.Equal(t, before, chain.dump())

		fresh := chain.host(t)
		freshSummary, err := fresh.ReadSessionKey(ctx, id)
		require.NoError(t, err)
		require.Equal(t, expected, freshSummary)
		summary[0] = 0
		again, err := host.ReadSessionKey(ctx, id)
		require.NoError(t, err)
		require.Equal(t, expected, again)

		speculative, _ := ctx.CacheContext()
		other := newTestActor(t, 33)
		otherPayload, otherID, otherExpected := readBridgeSession(t, actor, other, action,
			uint64(blockTime)-1000, uint64(blockTime)+1000, 41, 101)
		otherActivity := actor.activity(t, host, 0x00070005, 2, blockTime, otherPayload)
		receipt = execute(t, host, WithSequence(speculative, 43), otherActivity, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		otherSummary, err := host.ReadSessionKey(speculative, otherID)
		require.NoError(t, err)
		require.Equal(t, otherExpected, otherSummary)
		missing, err := host.ReadSessionKey(ctx, otherID)
		require.Equal(t, Code(-7), err)
		require.Nil(t, missing)
		require.Equal(t, before, chain.dump())
	})

	t.Run("receipt", func(t *testing.T) {
		chain := newTestChain(t)
		host := chain.host(t)
		actor := newTestActor(t, 34)
		activity := actor.register(t, host, 0, blockTime)
		original := execute(t, host, chain.context(1, blockTime, 41), activity, actor.authority())
		require.Equal(t, Code(0), original.ResultCode)
		ctx := chain.context(1, blockTime, 41)
		before := chain.dump()
		stored, err := host.ReadIdempotentReceipt(ctx, actor.did, activity.IdempotencyKey)
		require.NoError(t, err)
		require.Equal(t, original.ResultCode, stored.ResultCode)
		require.Equal(t, original.GlobalSequence, stored.GlobalSequence)
		require.Equal(t, original.Timestamp, stored.Timestamp)
		require.Equal(t, original.PreviousStateRoot, stored.PreviousStateRoot)
		require.Equal(t, original.ResultingStateRoot, stored.ResultingStateRoot)
		decoded, err := codec.DecodeReceipt(stored.Encoded)
		require.NoError(t, err)
		require.Equal(t, original.Encoded, decoded.UnsignedBytes())
		require.Equal(t, before, chain.dump())

		fresh := chain.host(t)
		again, err := fresh.ReadIdempotentReceipt(ctx, actor.did, activity.IdempotencyKey)
		require.NoError(t, err)
		require.Equal(t, stored, again)
		replayed, err := fresh.Execute(chain.context(1, blockTime, 42), activity, actor.authority())
		require.Equal(t, Code(-302), err)
		require.Equal(t, Receipt{}, replayed)
		require.Equal(t, before, chain.dump())
		again, err = fresh.ReadIdempotentReceipt(ctx, actor.did, activity.IdempotencyKey)
		require.NoError(t, err)
		require.Equal(t, stored, again)
		other := newTestActor(t, 35)
		missing, err := host.ReadIdempotentReceipt(ctx, other.did, activity.IdempotencyKey)
		require.Equal(t, Code(-7), err)
		require.Equal(t, Receipt{}, missing)
		missing, err = host.ReadIdempotentReceipt(ctx, actor.did, [32]byte{1})
		require.Equal(t, Code(-7), err)
		require.Equal(t, Receipt{}, missing)
		missing, err = host.ReadIdempotentReceipt(ctx, make([]byte, 513), activity.IdempotencyKey)
		require.Equal(t, codeNonCanonical, err)
		require.Equal(t, Receipt{}, missing)
	})

	t.Run("assets_and_accounts", func(t *testing.T) {
		chain := newTestChain(t)
		host := chain.host(t)
		actor := newTestActor(t, 36)
		var asset, salt [32]byte
		copy(asset[:], bytes.Repeat([]byte{0xaa}, 32))
		copy(salt[:], bytes.Repeat([]byte{0x11}, 32))
		registration := append([]byte{0, 1}, asset[:]...)
		registration = append(registration, salt[:]...)
		registration = append(registration, 3, 'T', 'K', 'N', 5, 'T', 'o', 'k', 'e', 'n', 6)
		registration = binary.BigEndian.AppendUint64(registration, 0)
		registration = binary.BigEndian.AppendUint64(registration, 1000)
		registration = append(registration, 2, 7)
		registration = append(registration, []byte("reserve")...)
		activity := actor.activity(t, host, 0x00010001, 0, blockTime, registration)
		receipt := execute(t, host, chain.context(1, blockTime, 41), activity, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		opened := actor.activity(t, host, 0x00010004, 1, blockTime, append([]byte{0, 1}, asset[:]...))
		receipt = execute(t, host, chain.context(1, blockTime, 42), opened, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		name := "agent:" + string(actor.did) + ":" + "asset" + ":" + hex.EncodeToString(asset[:])
		accountID, err := codec.DeriveAccountID([]byte(name))
		require.NoError(t, err)
		supply := append(append([]byte{0, 1}, asset[:]...), accountID[:]...)
		supply = binary.BigEndian.AppendUint64(supply, 0)
		supply = binary.BigEndian.AppendUint64(supply, 250)
		minted := actor.activity(t, host, 0x0001000a, 2, blockTime, supply)
		receipt = execute(t, host, chain.context(1, blockTime, 43), minted, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		chain.commit()
		ctx := chain.context(1, blockTime, 43)
		before := chain.dump()
		record, err := host.ReadAsset(ctx, asset)
		require.NoError(t, err)
		require.Equal(t, AssetRecord{AssetID: asset, Symbol: "TKN", Name: "Token", Decimals: 6,
			SupplyCap: sdk.NewInt(1000), IssuerDID: actor.id, Salt: salt, IssuerKind: 2,
			CustodyKind: 2, CustodyReference: []byte("reserve"), TotalUnits: sdk.NewInt(250)}, record)
		balance, err := host.ReadBalance(ctx, accountID, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(250), balance)
		total, err := host.ReadTotalUnits(ctx, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(250), total)
		account, err := host.ReadAccount(ctx, name, accountID)
		require.NoError(t, err)
		require.Equal(t, accountID, account.ID)
		require.Equal(t, name, account.Name)
		require.Equal(t, uint8(14), account.Kind)
		require.Equal(t, asset, account.AssetID)
		require.Equal(t, sdk.NewInt(250), account.Balance)
		require.True(t, account.HasAsset)
		require.True(t, account.HasAuthorityKey)
		require.Equal(t, actor.public, account.AuthorityKey)
		require.Equal(t, uint64(42), account.CreatedAtSequence)
		require.Equal(t, uint64(0), account.NextSequence)
		require.False(t, account.Frozen)
		require.False(t, account.HasOpenReference)
		owner, err := host.ReadAccountOwner(ctx, accountID)
		require.NoError(t, err)
		require.Equal(t, actor.id, owner)
		require.Equal(t, before, chain.dump())
		fresh := chain.host(t)
		freshRecord, err := fresh.ReadAsset(ctx, asset)
		require.NoError(t, err)
		require.Equal(t, record, freshRecord)
		freshBalance, err := fresh.ReadBalance(ctx, accountID, asset)
		require.NoError(t, err)
		require.Equal(t, balance, freshBalance)
		freshTotal, err := fresh.ReadTotalUnits(ctx, asset)
		require.NoError(t, err)
		require.Equal(t, total, freshTotal)
		freshAccount, err := fresh.ReadAccount(ctx, name, accountID)
		require.NoError(t, err)
		require.Equal(t, account, freshAccount)
		freshOwner, err := fresh.ReadAccountOwner(ctx, accountID)
		require.NoError(t, err)
		require.Equal(t, owner, freshOwner)
		require.Equal(t, before, chain.dump())

		speculative, _ := ctx.CacheContext()
		additional := binary.BigEndian.AppendUint64(append([]byte(nil), supply[:66]...), 0)
		additional = binary.BigEndian.AppendUint64(additional, 100)
		receipt = execute(t, host, WithSequence(speculative, 44),
			actor.activity(t, host, 0x0001000a, 3, blockTime, additional), actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		branchBalance, err := fresh.ReadBalance(speculative, accountID, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(350), branchBalance)
		branchTotal, err := fresh.ReadTotalUnits(speculative, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(350), branchTotal)
		branchAccount, err := fresh.ReadAccount(speculative, name, accountID)
		require.NoError(t, err)
		expectedBranchAccount := account
		expectedBranchAccount.Balance = sdk.NewInt(350)
		require.Equal(t, expectedBranchAccount, branchAccount)
		freshBalance, err = fresh.ReadBalance(ctx, accountID, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(250), freshBalance)
		require.Equal(t, before, chain.dump())
		_, err = host.ReadAccount(ctx, name, [32]byte{1})
		require.Equal(t, Code(-409), err)
		_, err = host.ReadBalance(ctx, accountID, [32]byte{1})
		require.Equal(t, Code(-402), err)

		paused := actor.activity(t, host, 0x00010002, 3, blockTime, append([]byte{0, 1}, asset[:]...))
		receipt = execute(t, host, chain.context(1, blockTime, 44), paused, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		record, err = host.ReadAsset(ctx, asset)
		require.NoError(t, err)
		require.True(t, record.Paused)
		require.Equal(t, sdk.NewInt(250), record.TotalUnits)
		total, err = host.ReadTotalUnits(ctx, asset)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(250), total)
		unpaused := actor.activity(t, host, 0x00010003, 4, blockTime, append([]byte{0, 1}, asset[:]...))
		receipt = execute(t, host, chain.context(1, blockTime, 45), unpaused, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		grant, grantID := readBridgePayerGrant(t, actor, accountID, accountID, asset, uint64(blockTime)+1000)
		issued := actor.activity(t, host, 0x00010007, 5, blockTime, grant)
		receipt = execute(t, host, chain.context(1, blockTime, 46), issued, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		grantState, err := host.ReadPayerGrant(ctx, grantID)
		require.NoError(t, err)
		require.Equal(t, PayerGrantState{Grant: grant, DrawnTotal: sdk.ZeroInt(),
			DrawnThisPeriod: sdk.ZeroInt()}, grantState)
		revocation := append([]byte{0, 1}, grantID[:]...)
		revocation = binary.BigEndian.AppendUint64(revocation, 6)
		revoked := actor.activity(t, host, 0x00010008, 6, blockTime, revocation)
		receipt = execute(t, host, chain.context(1, blockTime, 47), revoked, actor.authority())
		require.Equal(t, Code(0), receipt.ResultCode)
		grantState, err = host.ReadPayerGrant(ctx, grantID)
		require.NoError(t, err)
		require.Equal(t, PayerGrantState{Grant: grant, DrawnTotal: sdk.ZeroInt(),
			DrawnThisPeriod: sdk.ZeroInt(), Revoked: true, RevokedAt: 47}, grantState)
		before = chain.dump()
		freshGrant, err := fresh.ReadPayerGrant(ctx, grantID)
		require.NoError(t, err)
		require.Equal(t, grantState, freshGrant)
		freshGrant.Grant[0] ^= 0xff
		again, err := host.ReadPayerGrant(ctx, grantID)
		require.NoError(t, err)
		require.Equal(t, grantState, again)
		require.Equal(t, before, chain.dump())
	})
}

func TestReadBridgeRefusals(t *testing.T) {
	chain := newTestChain(t)
	host := chain.host(t)
	ctx := chain.context(1, 10000, 1)
	var id [32]byte
	_, err := host.ReadAsset(ctx, id)
	require.Equal(t, Code(-402), err)
	amount, err := host.ReadBalance(ctx, id, id)
	require.Equal(t, Code(-208), err)
	require.Equal(t, sdk.ZeroInt(), amount)
	amount, err = host.ReadTotalUnits(ctx, id)
	require.Equal(t, Code(-402), err)
	require.Equal(t, sdk.ZeroInt(), amount)
	owner, err := host.ReadAccountOwner(ctx, id)
	require.Equal(t, Code(-208), err)
	require.Equal(t, [32]byte{}, owner)
	name, accountID, err := MainAccountID([32]byte{1})
	require.NoError(t, err)
	account, err := host.ReadAccount(ctx, name, accountID)
	require.Equal(t, Code(-208), err)
	require.Equal(t, AccountRecord{}, account)
	summary, err := host.ReadSessionKey(ctx, id)
	require.Equal(t, Code(-7), err)
	require.Nil(t, summary)
	grant, err := host.ReadPayerGrant(ctx, id)
	require.Equal(t, Code(-214), err)
	require.Equal(t, PayerGrantState{}, grant)
	host.Close()
	_, err = host.ReadAsset(ctx, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadBalance(ctx, id, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadTotalUnits(ctx, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadAccountOwner(ctx, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadAccount(ctx, "", id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadSessionKey(ctx, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadPayerGrant(ctx, id)
	require.Equal(t, codeNonCanonical, err)
	_, err = host.ReadIdempotentReceipt(ctx, nil, id)
	require.Equal(t, codeNonCanonical, err)
}

func TestReadBridgeInvalidStore(t *testing.T) {
	for _, test := range []struct {
		name       string
		key, value []byte
		code       Code
	}{
		{"sequence_length", []byte{lastSequenceKey}, []byte{1}, codeFatalInvariant},
		{"sequence_overflow", []byte{lastSequenceKey}, binary.BigEndian.AppendUint64(nil, math.MaxUint64), Code(-307)},
		{"module_key", []byte{moduleKVPrefix, 1}, []byte{1}, codeFatalInvariant},
		{"replay_key", []byte{idempotencyPrefix, 1}, []byte{1}, codeFatalInvariant},
		{"account_key", []byte{accountPrefix, 1}, []byte{1}, codeFatalInvariant},
	} {
		t.Run(test.name, func(t *testing.T) {
			chain := newTestChain(t)
			host := chain.host(t)
			ctx := chain.context(1, 10000, 1)
			ctx.KVStore(chain.key).Set(test.key, test.value)
			_, err := host.ReadSessionKey(ctx, [32]byte{})
			require.Equal(t, test.code, err)
			require.Equal(t, test.value, ctx.KVStore(chain.key).Get(test.key))
		})
	}
}
