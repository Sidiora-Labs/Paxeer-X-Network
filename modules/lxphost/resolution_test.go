//go:build cgo

package lxphost

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
)

const (
	resolutionBlockTime      = int64(1700000000000)
	resolutionProgramsModule = 9
	resolutionDeployType     = 0x00090001
	resolutionProgramRecord  = 71
)

func resolutionBankAddress(id [32]byte) sdk.AccAddress {
	return sdk.AccAddress(address.Hash("module", append([]byte("layerx\x00"), id[:]...)))
}

func resolutionDIDKey(seed byte) [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = seed
	}
	return key
}

// resolutionSeedMetering writes the genesis metering schedule version 1 of
// the programs module into the host store.
func resolutionSeedMetering(t *testing.T, chain *testChain) {
	t.Helper()
	record := append([]byte("LXMR1"), 0, 0, 0, 1)
	for _, coefficient := range []uint64{1, 1, 1, 1, 1, 8, 8, 64, 8} {
		record = binary.BigEndian.AppendUint64(record, coefficient)
	}
	record = binary.BigEndian.AppendUint64(record, 1)
	record = append(record, 1)
	record = append(record, bytes.Repeat([]byte{0xa5}, 32)...)
	require.Len(t, record, 122)
	store := chain.context(1, resolutionBlockTime, 1).KVStore(chain.key)
	store.Set(moduleKVKey(resolutionProgramsModule, []byte("progmet/active/v1")), record)
	store.Set(moduleKVKey(resolutionProgramsModule, append([]byte("progmet/history/v1/"), 0, 0, 0, 1)), record)
	chain.commit()
}

// resolutionGuest builds a guest exporting layerx_reserve, layerx_call and
// memory that imports one function of params i32 arguments returning i32.
func resolutionGuest(module, name string, params int) []byte {
	section := func(id byte, body []byte) []byte { return append([]byte{id, byte(len(body))}, body...) }
	label := func(s string) []byte { return append([]byte{byte(len(s))}, s...) }
	types := []byte{3, 0x60, 1, 0x7f, 1, 0x7f, 0x60, 2, 0x7f, 0x7f, 1, 0x7f, 0x60, byte(params)}
	types = append(append(types, bytes.Repeat([]byte{0x7f}, params)...), 1, 0x7f)
	imports := append(append(append([]byte{1}, label(module)...), label(name)...), 0x00, 2)
	exports := append(append([]byte{3}, label("layerx_reserve")...), 0x00, 1)
	exports = append(append(exports, label("layerx_call")...), 0x00, 2)
	exports = append(append(exports, label("memory")...), 0x02, 0)
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = append(wasm, section(1, types)...)
	wasm = append(wasm, section(2, imports)...)
	wasm = append(wasm, section(3, []byte{2, 0, 1})...)
	wasm = append(wasm, section(5, []byte{1, 1, 1, 1})...)
	wasm = append(wasm, section(7, exports)...)
	return append(wasm, section(10, []byte{2, 4, 0, 0x41, 0, 0x0b, 4, 0, 0x41, 0, 0x0b})...)
}

func resolutionDeploy(t *testing.T, h *Host, actor testActor, accountSequence uint64, program [32]byte, abi uint16, wasm []byte) Activity {
	t.Helper()
	hash := sha256.Sum256(wasm)
	payload := append(append([]byte(nil), program[:]...), byte(abi>>8), byte(abi), 0, 0)
	payload = append(payload, make([]byte, 32)...)
	payload = append(payload, hash[:]...)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(wasm)))
	return actor.activity(t, h, resolutionDeployType, accountSequence, resolutionBlockTime, append(payload, wasm...))
}

func resolutionProgramKey(program [32]byte) []byte {
	return moduleKVKey(resolutionProgramsModule, append([]byte("program\x00"), program[:]...))
}

func TestResolutionAccountAddress(t *testing.T) {
	didKey := resolutionDIDKey(0xab)
	account, err := DeriveKernelAccount(didKey, 1, [32]byte{})
	require.NoError(t, err)
	require.Equal(t, "agent:did:layerx:"+strings.Repeat("ab", 32)+":main", account.Name)
	kernelID, err := kernelAccountID([]byte(account.Name))
	require.NoError(t, err)
	require.Equal(t, kernelID, account.ID)
	_, mainID, err := MainAccountID(didKey)
	require.NoError(t, err)
	require.Equal(t, mainID, account.ID)
	require.Equal(t, uint16(1), account.Kind)
	did, err := DIDID([]byte(evmtypes.LayerXDid(didKey)))
	require.NoError(t, err)
	require.Equal(t, did, account.Parent)
	require.Equal(t, sdk.AccAddress(address.Module("layerx", account.ID[:])), account.Address)
	require.Equal(t, resolutionBankAddress(account.ID), account.Address)
	require.Equal(t, BankAddress(account.ID), account.Address)
	require.Len(t, account.Address, 32)

	_, err = DeriveKernelAccount(didKey, 1, resolutionDIDKey(0x01))
	require.Equal(t, codeNonCanonical, err)
	for _, kind := range []uint16{0, 2, 3, 4, 6, 10, 13, 15} {
		_, err = DeriveKernelAccount(didKey, kind, resolutionDIDKey(0x01))
		require.Equal(t, codeNonCanonical, err, "kind %d", kind)
	}
}

func TestResolutionMarginNamespace(t *testing.T) {
	didKey := resolutionDIDKey(0x5c)
	market := resolutionDIDKey(0x3e)
	asset := resolutionDIDKey(0x77)
	main, err := DeriveKernelAccount(didKey, 1, [32]byte{})
	require.NoError(t, err)
	margin, err := DeriveKernelAccount(didKey, 5, market)
	require.NoError(t, err)
	holding, err := DeriveKernelAccount(didKey, 14, asset)
	require.NoError(t, err)

	did := evmtypes.LayerXDid(didKey)
	require.Equal(t, "agent:"+did+":margin:"+hex.EncodeToString(market[:]), margin.Name)
	require.Equal(t, "agent:"+did+":asset:"+hex.EncodeToString(asset[:]), holding.Name)
	require.Equal(t, uint16(5), margin.Kind)
	require.Equal(t, uint16(14), holding.Kind)
	require.Equal(t, main.Parent, margin.Parent)
	require.Equal(t, main.Parent, holding.Parent)
	for _, account := range []KernelAccount{main, margin, holding} {
		require.Equal(t, resolutionBankAddress(account.ID), account.Address)
		require.Equal(t, sdk.AccAddress(address.Module("layerx", account.ID[:])), account.Address)
	}
	require.NotEqual(t, main.ID, margin.ID)
	require.NotEqual(t, main.Address, margin.Address)
	require.NotEqual(t, margin.Address, holding.Address)
	require.NotEqual(t, main.Address, holding.Address)

	other, err := DeriveKernelAccount(didKey, 5, resolutionDIDKey(0x3f))
	require.NoError(t, err)
	require.NotEqual(t, margin.Address, other.Address)
	foreign, err := DeriveKernelAccount(resolutionDIDKey(0x5d), 5, market)
	require.NoError(t, err)
	require.NotEqual(t, margin.Address, foreign.Address)
	require.NotEqual(t, margin.Parent, foreign.Parent)

	_, err = DeriveKernelAccount(didKey, 5, [32]byte{})
	require.Equal(t, codeNonCanonical, err)
	_, err = DeriveKernelAccount(didKey, 14, [32]byte{})
	require.Equal(t, codeNonCanonical, err)
}

func TestResolutionPerpsMarginAccount(t *testing.T) {
	actor := newTestActor(t, 0x41)
	market := resolutionDIDKey(0x9a)
	margin, err := DeriveKernelAccount(actor.public, 5, market)
	require.NoError(t, err)

	// The perps dispatch accepts a margin account when its kernel kind is
	// AGENT_MARGIN and its name is "agent:" + actor DID + ":margin:" + the
	// lowercase hex market id, as staged by lxp_ctx_account_stage_perps_margin.
	staged := append(append([]byte("agent:"), actor.did...), ":margin:"...)
	staged = append(staged, hex.EncodeToString(market[:])...)
	require.Equal(t, string(staged), margin.Name)
	require.True(t, bytes.HasPrefix([]byte(margin.Name), append(append([]byte("agent:"), actor.did...), ":margin:"...)))
	stagedID, err := kernelAccountID(staged)
	require.NoError(t, err)
	require.Equal(t, stagedID, margin.ID)
	owner, err := HolderDid(staged)
	require.NoError(t, err)
	require.Equal(t, actor.id, owner)
	require.Equal(t, actor.id, margin.Parent)
	require.Equal(t, BankAddress(stagedID), margin.Address)
	require.Equal(t, resolutionBankAddress(stagedID), margin.Address)
}

func TestResolutionExecuteEntry(t *testing.T) {
	actor := newTestActor(t, 0x42)
	program := resolutionDIDKey(0x61)
	guest := resolutionGuest("layerx_v5", "market_step_adjudicate", 6)
	type step struct {
		build    func(*Host) Activity
		sequence uint64
	}
	steps := []step{
		{func(h *Host) Activity { return actor.register(t, h, 0, resolutionBlockTime) }, 41},
		{func(h *Host) Activity { return actor.registry(t, h, 1, resolutionBlockTime, 0xC4, []byte("entry")) }, 42},
		{func(h *Host) Activity { return resolutionDeploy(t, h, actor, 2, program, 5, guest) }, 43},
	}
	run := func() ([]Receipt, []byte, *testChain) {
		chain := newTestChain(t)
		resolutionSeedMetering(t, chain)
		host := chain.host(t)
		receipts := make([]Receipt, 0, len(steps))
		for _, s := range steps {
			receipt, err := host.HostExecute(chain.context(2, resolutionBlockTime, s.sequence), s.build(host), actor.authority())
			require.NoError(t, err)
			require.Equal(t, Code(0), receipt.ResultCode)
			require.Equal(t, s.sequence, receipt.GlobalSequence)
			receipts = append(receipts, receipt)
		}
		return receipts, chain.commit(), chain
	}
	first, firstHash, chain := run()
	time.Sleep(5 * time.Millisecond)
	second, secondHash, _ := run()
	require.Equal(t, first, second)
	require.Equal(t, firstHash, secondHash)
	for i := 1; i < len(first); i++ {
		require.Equal(t, first[i-1].ResultingStateRoot, first[i].PreviousStateRoot)
		require.NotEqual(t, first[i].PreviousStateRoot, first[i].ResultingStateRoot)
	}

	record := chain.get(resolutionProgramKey(program))
	require.Len(t, record, resolutionProgramRecord)
	codeHash := sha256.Sum256(guest)
	require.Equal(t, byte(0), record[0])
	require.Equal(t, codeHash[:], record[33:65])
	require.Equal(t, uint16(5), binary.BigEndian.Uint16(record[65:67]))
	require.Equal(t, uint32(1), binary.BigEndian.Uint32(record[67:71]))
	require.Equal(t, guest, chain.get(blobKey(resolutionProgramsModule, codeHash[:])))
}

func TestResolutionGuestImports(t *testing.T) {
	actor := newTestActor(t, 0x43)
	chain := newTestChain(t)
	resolutionSeedMetering(t, chain)
	host := chain.host(t)
	registered := execute(t, host, chain.context(2, resolutionBlockTime, 41), actor.register(t, host, 0, resolutionBlockTime), actor.authority())
	require.Equal(t, Code(0), registered.ResultCode)

	accepted := []struct {
		abi    uint16
		module string
		name   string
		params int
	}{
		{1, "layerx_v1", "storage_read", 4},
		{2, "layerx_v2", "response_write", 3},
		{3, "layerx_v3", "oracle_read", 4},
		{4, "layerx_v4", "web_read", 4},
		{5, "layerx_v5", "market_step_adjudicate", 6},
	}
	sequence := uint64(41)
	accountSequence := uint64(1)
	for index, guest := range accepted {
		sequence++
		program := resolutionDIDKey(byte(0x70 + index))
		wasm := resolutionGuest(guest.module, guest.name, guest.params)
		receipt := execute(t, host, chain.context(2, resolutionBlockTime, sequence),
			resolutionDeploy(t, host, actor, accountSequence, program, guest.abi, wasm), actor.authority())
		accountSequence++
		require.Equal(t, Code(0), receipt.ResultCode, guest.module)
		record := chain.get(resolutionProgramKey(program))
		require.Len(t, record, resolutionProgramRecord, guest.module)
		require.Equal(t, guest.abi, binary.BigEndian.Uint16(record[65:67]), guest.module)
		codeHash := sha256.Sum256(wasm)
		require.Equal(t, wasm, chain.get(blobKey(resolutionProgramsModule, codeHash[:])), guest.module)
	}

	refused := []struct {
		abi    uint16
		module string
		name   string
		params int
	}{
		{5, "env", "db_read", 1},
		{5, "env", "query_chain", 1},
		{4, "layerx_v5", "market_step_adjudicate", 6},
	}
	for index, guest := range refused {
		sequence++
		program := resolutionDIDKey(byte(0x90 + index))
		wasm := resolutionGuest(guest.module, guest.name, guest.params)
		before := chain.dump()
		receipt := execute(t, host, chain.context(2, resolutionBlockTime, sequence),
			resolutionDeploy(t, host, actor, accountSequence, program, guest.abi, wasm), actor.authority())
		accountSequence++
		require.Equal(t, codeNonCanonical, receipt.ResultCode, guest.module+"."+guest.name)
		require.Equal(t, "LXP_ERR_NON_CANONICAL", receipt.ResultCode.Error())
		require.Nil(t, chain.get(resolutionProgramKey(program)))
		codeHash := sha256.Sum256(wasm)
		blob := string(blobKey(resolutionProgramsModule, codeHash[:]))
		require.Equal(t, before[blob], chain.get([]byte(blob)))
		after := chain.dump()
		for key := range after {
			if bytes.HasPrefix([]byte(key), []byte{blobPrefix}) || bytes.HasPrefix([]byte(key), moduleKVKey(resolutionProgramsModule, nil)) {
				require.Equal(t, before[key], after[key])
			}
		}
	}

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			require.NotContains(t, path, "/wasmbinding", file)
			require.NotContains(t, path, "/wasm/", file)
			require.NotContains(t, path, "wasmvm", file)
		}
	}
}

func TestResolutionBlockOpenTime(t *testing.T) {
	chain := newTestChain(t)
	host := chain.host(t)
	first := newTestActor(t, 0x44)
	second := newTestActor(t, 0x45)

	early := first.register(t, host, 0, resolutionBlockTime)
	early.NotBefore = uint64(resolutionBlockTime) - 1
	early.NotAfter = uint64(resolutionBlockTime) + 1
	preimage, err := host.SigningHash(early)
	require.NoError(t, err)
	early.Signature = ed25519.Sign(first.private, preimage[:])
	late := second.register(t, host, 0, resolutionBlockTime)
	late.NotBefore = uint64(resolutionBlockTime) - 20000
	late.NotAfter = uint64(resolutionBlockTime) + 20000
	preimage, err = host.SigningHash(late)
	require.NoError(t, err)
	late.Signature = ed25519.Sign(second.private, preimage[:])

	one, err := host.HostExecute(chain.context(5, resolutionBlockTime, 41), early, first.authority())
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	two, err := host.HostExecute(chain.context(5, resolutionBlockTime, 42), late, second.authority())
	require.NoError(t, err)
	require.Equal(t, Code(0), one.ResultCode)
	require.Equal(t, Code(0), two.ResultCode)
	require.Equal(t, uint64(1700000000000), one.Timestamp)
	require.Equal(t, uint64(1700000000000), two.Timestamp)
	require.Equal(t, one.Timestamp, two.Timestamp)

}
