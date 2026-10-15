package launchpad

import (
	"bytes"
	"math/big"
	"testing"

	ibctransferkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/keeper"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

type eventVector struct {
	role      string
	name      string
	signature string
	address   common.Address
	values    []interface{}
	indexed   []int
}

func eventAddress(n byte) common.Address {
	return common.BytesToAddress(bytes.Repeat([]byte{n}, common.AddressLength))
}

func eventHash(n byte) [32]byte {
	var word [32]byte
	copy(word[:], bytes.Repeat([]byte{n}, len(word)))
	return word
}

func eventInteger(n int64) *big.Int {
	return new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 96), big.NewInt(n))
}

func eventState(t *testing.T) (PrecompileExecutor, *vm.EVM, *state.DBImpl) {
	t.Helper()
	key := sdk.NewKVStoreKey("event_evm")
	transient := sdk.NewTransientStoreKey("event_transient")
	ctx := testutil.DefaultContext(key, transient)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	subspace := func(name string) paramtypes.Subspace {
		return paramtypes.NewSubspace(cdc, codec.NewLegacyAmino(), key, transient, name)
	}
	accounts := authkeeper.NewAccountKeeper(cdc, key, subspace("event_auth"), authtypes.ProtoBaseAccount,
		map[string][]string{authtypes.FeeCollectorName: nil})
	keeper := evmkeeper.NewKeeper(key, transient, subspace("event_evm"), nil, nil, &accounts, nil,
		ibctransferkeeper.Keeper{}, nil, nil, nil)
	db := state.NewDBImpl(ctx, keeper, false)
	t.Cleanup(db.Cleanup)
	evm := &vm.EVM{StateDB: db, Context: vm.BlockContext{Time: 73, BlockNumber: big.NewInt(41)}}
	return PrecompileExecutor{abi: pcommon.MustGetABI(f, "abi.json")}, evm, db
}

func eventWord(t *testing.T, value interface{}) []byte {
	t.Helper()
	switch v := value.(type) {
	case common.Address:
		return common.LeftPadBytes(v.Bytes(), 32)
	case [32]byte:
		return append([]byte(nil), v[:]...)
	case *big.Int:
		return common.LeftPadBytes(v.Bytes(), 32)
	case uint8:
		return common.LeftPadBytes([]byte{v}, 32)
	case uint64:
		return common.LeftPadBytes(new(big.Int).SetUint64(v).Bytes(), 32)
	case bool:
		if v {
			return common.LeftPadBytes([]byte{1}, 32)
		}
		return make([]byte, 32)
	default:
		t.Fatalf("unsupported event word %T", value)
		return nil
	}
}

func eventData(t *testing.T, values []interface{}) []byte {
	t.Helper()
	head := make([]byte, 0, 32*len(values))
	var tail []byte
	for _, value := range values {
		if s, ok := value.(string); ok {
			head = append(head, eventWord(t, big.NewInt(int64(32*len(values)+len(tail))))...)
			tail = append(tail, eventWord(t, big.NewInt(int64(len(s))))...)
			tail = append(tail, []byte(s)...)
			tail = append(tail, make([]byte, (32-len(s)%32)%32)...)
		} else {
			head = append(head, eventWord(t, value)...)
		}
	}
	return append(head, tail...)
}

func assertEventVector(t *testing.T, db *state.DBImpl, v eventVector, position int, family bool) {
	t.Helper()
	topics := []common.Hash{common.HexToHash(v.signature)}
	var data []interface{}
	next := 0
	for i, value := range v.values {
		if next < len(v.indexed) && v.indexed[next] == i {
			topics = append(topics, common.BytesToHash(eventWord(t, value)))
			next++
		} else {
			data = append(data, value)
		}
	}
	if family {
		data = append(data, big.NewInt(73), big.NewInt(41))
	}
	logs := db.GetAllLogs()
	require.Len(t, logs, position+1)
	require.Equal(t, v.address, logs[position].Address)
	require.Equal(t, topics, logs[position].Topics)
	require.Equal(t, eventData(t, data), logs[position].Data)
	require.Equal(t, uint(position), logs[position].Index)
}

func TestLogsMatchContractEvents(t *testing.T) {
	p, evm, db := eventState(t)
	pool := eventAddress(9)
	precompile := common.HexToAddress(LaunchpadAddress)
	vectors := []eventVector{
		{"SidioraPool", "FactoryMigrated", "0xdc152171996ab31ea044d1f083998178a0515971a850651a133f8d13027bb277", pool, []interface{}{eventAddress(1)}, []int{0}},
		{"SidioraPool", "GuardianTransferStarted", "0x112d7ce5d0717de1e8d403be44596423dc61e85438d8ba49eaf0d31b1992bce0", pool, []interface{}{eventAddress(1), eventAddress(2)}, []int{0, 1}},
		{"SidioraPool", "GuardianTransferred", "0x9fe65984b511eb7998b3cd481cfd80c807fcc7f28721a790d33c87dc9c879485", pool, []interface{}{eventAddress(1), eventAddress(2)}, []int{0, 1}},
		{"SidioraPool", "Initialized", "0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2", pool, []interface{}{uint64(1)}, []int{}},
		{"SidioraPool", "InputAdjustment", "0xc6dfd6050e713ed5ff35fd38843c7c60822498a3d56ae5dc062e563a5b157d5c", pool, []interface{}{eventInteger(1), eventInteger(2)}, []int{}},
		{"SidioraPool", "PauseToggled", "0x9077d36bc00859b5c3f320310707208543dd35092cb0a0fe117d0c6a558b148b", pool, []interface{}{true}, []int{}},
		{"SidioraFactory", "MarketCreated", "0xde3c5cd27ce87044b7bc6c8182270f302e537f55f8c943271a34e5163ef2b284", common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventInteger(4), eventAddress(5)}, []int{0, 1, 2}},
		{"SidioraFactory", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"SidioraFactory", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Router", "Buy", "0x89f5adc174562e07c9c9b1cae7109bbecb21cf9d1b2847e550042b8653c54a0e", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3), eventInteger(4)}, []int{0, 1}},
		{"Router", "MarketCreated", "0xfe7e33f239f268e882861cae62e631558d814973588d97738cc084b8b3489599", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventInteger(4)}, []int{0, 1, 2}},
		{"Router", "MultihopSwap", "0xef2dce1faa347130cf25c6d059af1f6f181983e69a2fadd79ed8751a532d3d96", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventInteger(4), eventInteger(5), eventInteger(6)}, []int{0, 1, 2}},
		{"Router", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Router", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Router", "Sell", "0xa082022e93cfcd9f1da5f9236718053910f7e840da080c789c7845698dc032ff", common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3), eventInteger(4)}, []int{0, 1}},
		{"Quoter", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Quoter", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"FeesRouter", "AirdropClaimed", "0x752d3a5de250652e9111e24fea2886c837dadfe50c8479ad3b5511f4c36f20a2", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"FeesRouter", "AirdropExecuted", "0x0fa3c377438bb545a4c9ecaa3e574d96bb1b2c6416976df85919d2b83a57bd9f", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), eventInteger(2)}, []int{0}},
		{"FeesRouter", "FeeStrategyChanged", "0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), uint8(2), uint8(3)}, []int{0}},
		{"FeesRouter", "FeesBurned", "0x9768e1fe058fe58a8022c0c5a88689f80a64c3a69cc5ba498c518407fc04ced5", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), eventInteger(2)}, []int{0}},
		{"FeesRouter", "FeesClaimed", "0x2227733fc4c8a9034cb58087dcf6995128b9c0233b038b03366aaf30c92b92d6", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"FeesRouter", "LpRewardsExecuted", "0xf831aaeb2aae759a8843e359900e1b8a3fe4cdefa90fc390825acb66a4374408", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventInteger(1), eventInteger(2)}, []int{0}},
		{"FeesRouter", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"FeesRouter", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"FeeAccumulator", "AccountingMigrated", "0x75b17ca7274960709421f611b666d569ceebe0cbb24821254bfe2e5e4fce9df2", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventInteger(1)}, []int{}},
		{"FeeAccumulator", "AirdropClaimed", "0xd399c6e7fad358fc300beda3f056717c94a04c7233ce92683de6500ba509022e", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3), eventInteger(4)}, []int{0, 1}},
		{"FeeAccumulator", "AirdropTriggered", "0x8c39ea0d52c00d579184e8070640104228f1a618d77e55429e09cfbb049c2dc1", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2), eventInteger(3)}, []int{0}},
		{"FeeAccumulator", "FeeRecorded", "0xb4d4d3bd2f97a7d6f1657ee69f7191d7aa7dbd5b6864a2d7a9d14efc1322552f", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2), eventInteger(3), eventInteger(4)}, []int{0}},
		{"FeeAccumulator", "FeesBurned", "0x0d9575a73e2a7da16cfde907df749d23d901528ff2e7c832b731babdecca000b", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2)}, []int{0}},
		{"FeeAccumulator", "FeesClaimed", "0xfe3464cd748424446c37877c28ce5b700222c5bc9f90d908afcc4e5cb22707ff", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"FeeAccumulator", "LegacyAirdropAllocated", "0x66f288a74d4384624c871c51e16635a78cbc49fc5c471db184fae9b5860ee216", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2), eventHash(3), eventInteger(4)}, []int{0, 1}},
		{"FeeAccumulator", "LpRewardsSent", "0x86b634da567bed9efe3e74597f5b2f7c9a880ed2e05e28f4c7c2fae1f0a9cfdb", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2)}, []int{0}},
		{"FeeAccumulator", "OpticalSurplusClaimed", "0x879606fa2db265e0664b2683c3e39e5ca7c90eae54628f7af6b70f239af8d0a6", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"FeeAccumulator", "OpticalSurplusRecorded", "0xc15bf759695c18fd9ddf60f8638036310ac80c3da55f86b717f102c53a44a9e3", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventAddress(1), eventInteger(2)}, []int{0}},
		{"FeeAccumulator", "ProtocolFeeSwept", "0xe50aaa1965bcc0030c0dedb0b575635e1da17cf4989a55fa0f691ab35ff7ff13", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventInteger(1)}, []int{}},
		{"FeeAccumulator", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"FeeAccumulator", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"PoolRegistry", "PoolRegistered", "0xdd85b374a142757b7745e1d5957bddced32d0fae0d5a038c41e7f79dc162b5ce", common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"), []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventAddress(4), eventInteger(5), eventInteger(6)}, []int{0, 1, 2}},
		{"PoolRegistry", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"PoolRegistry", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"ProtocolConfig", "ConfigUpdated", "0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e", precompile, []interface{}{eventHash(1), eventInteger(2), eventInteger(3)}, []int{0}},
		{"ProtocolConfig", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"ProtocolConfig", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"SidioraNFT", "Approval", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1, 2}},
		{"SidioraNFT", "ApprovalForAll", "0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventAddress(1), eventAddress(2), true}, []int{0, 1}},
		{"SidioraNFT", "FeeStrategyChanged", "0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventInteger(1), uint8(2), uint8(3)}, []int{0}},
		{"SidioraNFT", "PoolNFTMinted", "0xa1ff1e0bedf4ae07c5f17ac3f0d64625d9c386b9681a705499c8595ebea1a732", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventInteger(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"SidioraNFT", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"SidioraNFT", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"SidioraNFT", "Transfer", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1, 2}},
		{"Treasury", "Deposited", "0x8752a472e571a816aea92eec8dae9baf628e840f4929fbcc2d155e6233ff68a7", common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"Treasury", "RoleGranted", "0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d", common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Treasury", "RoleRevoked", "0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b", common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), []interface{}{eventHash(1), eventAddress(2), eventAddress(3)}, []int{0, 1, 2}},
		{"Treasury", "SurplusRecovered", "0x00c571bcd43634ce4043e2bd7088ab9a1d36bd5dc21f5f4246e9eddda3c222f6", common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
		{"Treasury", "Withdrawn", "0xd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb", common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), []interface{}{eventAddress(1), eventAddress(2), eventInteger(3)}, []int{0, 1}},
	}
	snapshot := db.Snapshot()
	position := 0
	for _, v := range vectors {
		t.Run(v.role+"/"+v.name, func(t *testing.T) {
			calledAt := v.address
			require.NoError(t, p.emitAt(evm, v.role, calledAt, v.name, v.values...))
			assertEventVector(t, db, v, position, false)
			position++
			if v.role == "Router" || v.role == "FeesRouter" {
				require.NoError(t, p.emitAt(evm, v.role, precompile, v.name, v.values...))
				if v.name != "RoleGranted" && v.name != "RoleRevoked" {
					v.address = precompile
				}
				assertEventVector(t, db, v, position, false)
				position++
			}
		})
	}
	db.RevertToSnapshot(snapshot)
	require.Empty(t, db.GetAllLogs())
	testNativeEventRefusals(t, p, evm, db)
}

func TestEmitterFamilyAtThePrecompile(t *testing.T) {
	p, evm, db := eventState(t)
	precompile := common.HexToAddress(LaunchpadAddress)
	vectors := []eventVector{
		{"EventEmitter", "ConfigUpdated", "0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e", precompile, []interface{}{eventHash(1), eventInteger(2), eventInteger(3)}, []int{0}},
		{"EventEmitter", "FeeDistributed", "0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc", precompile, []interface{}{eventHash(1), eventInteger(2), uint8(3), eventInteger(4), eventAddress(5)}, []int{0}},
		{"EventEmitter", "FeeFlow", "0x626e3fd5d9323ca4b7ffa3ca51c21c02766b638d15eb8896a36a4e0e97ae6de4", precompile, []interface{}{uint8(1), eventAddress(2), eventAddress(3), eventInteger(4), eventInteger(5), eventInteger(6), eventInteger(7)}, []int{0, 1, 2}},
		{"EventEmitter", "FeeStrategyChanged", "0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd", precompile, []interface{}{eventHash(1), eventInteger(2), uint8(3), uint8(4)}, []int{0}},
		{"EventEmitter", "MarketCreated", "0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b", precompile, []interface{}{eventHash(1), eventAddress(2), eventAddress(3), eventAddress(4), eventAddress(5)}, []int{0, 1, 2}},
		{"EventEmitter", "NftMint", "0xd2917d37031c0dfe5016665c7a83360cbf2ff32063096b2caf61ba66c06832e5", precompile, []interface{}{eventInteger(1), eventAddress(2), eventAddress(3), uint8(4)}, []int{0, 1, 2}},
		{"EventEmitter", "NftTransfer", "0x021f03e8a1315704c74c03e8c022c12bc80c4a2bd4fec8bd36b217322c340ad3", precompile, []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventInteger(4)}, []int{0, 1, 2}},
		{"EventEmitter", "PoolRegistered", "0xda5568642b67660133b1de538c867c58f3037a5722b0e231a959ee8ca11b2ed7", precompile, []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventAddress(4), eventInteger(5)}, []int{0, 1, 2}},
		{"EventEmitter", "PoolStateUpdated", "0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd", precompile, []interface{}{eventHash(1), eventInteger(2), eventInteger(3), eventInteger(4), eventInteger(5)}, []int{0}},
		{"EventEmitter", "RouterTrade", "0x17c1899a3fd431f2b2200b90e738d270bb856a4222040c5650bfa208acb3b650", precompile, []interface{}{uint8(1), eventAddress(2), eventAddress(3), eventAddress(4), eventAddress(5), eventInteger(6), eventInteger(7), eventInteger(8)}, []int{0, 1, 2}},
		{"EventEmitter", "Swap", "0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc", precompile, []interface{}{eventHash(1), eventAddress(2), true, eventInteger(4), eventInteger(5), eventInteger(6), eventInteger(7)}, []int{0, 1}},
		{"EventEmitter", "TokenDeployed", "0xad187874b793634c55d977f35d4828526eb48170a9531c71d88f8fd10cf9c237", precompile, []interface{}{eventAddress(1), eventAddress(2), eventAddress(3), eventHash(4), "Token with a long UTF-8 name: λλλλλλλλλλ", "TOK", uint8(7), eventInteger(8)}, []int{0, 1, 2}},
	}
	require.Len(t, vectors, 12)
	snapshot := db.Snapshot()
	for i, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			require.NoError(t, p.emitFamily(evm, v.name, v.values...))
			assertEventVector(t, db, v, i, true)
		})
	}
	db.RevertToSnapshot(snapshot)
	require.Empty(t, db.GetAllLogs())
	testFamilyEventRefusals(t, p, evm, db)
}

func testNativeEventRefusals(t *testing.T, p PrecompileExecutor, evm *vm.EVM, db *state.DBImpl) {
	t.Helper()
	pool := eventAddress(9)
	precompile := common.HexToAddress(LaunchpadAddress)
	for _, name := range []string{"RoleAdminChanged", "Upgraded", "MirrorFailed"} {
		require.EqualError(t, p.emitAt(evm, "FeesRouter", precompile, name), "launchpad: retired event "+name)
	}
	require.EqualError(t, p.emitAt(evm, "SidioraFactory", precompile, "Initialized", uint64(1)),
		"launchpad: initialization event requires a new pool")
	require.EqualError(t, p.emitAt(evm, "SidioraERC20", pool, "Transfer"), "launchpad: unsupported event role SidioraERC20")
	require.EqualError(t, p.emitAt(evm, "unknown", pool, "Transfer"), "launchpad: unsupported event role unknown")
	require.EqualError(t, p.emitAt(evm, "SidioraPool", pool, "Transfer"), "launchpad: unknown SidioraPool event Transfer")
	for _, address := range []common.Address{{}, precompile} {
		require.EqualError(t, p.emitAt(evm, "SidioraPool", address, "PauseToggled", false),
			"launchpad: pool event requires a pool address")
	}
	require.EqualError(t, p.emitAt(evm, "Router", pool, "Buy"), "launchpad: invalid Router event surface "+pool.Hex())
	require.EqualError(t, p.emitAt(evm, "SidioraPool", pool, "PauseToggled"),
		"launchpad: PauseToggled expects 1 event arguments, got 0")
	require.EqualError(t, p.emitAt(evm, "SidioraPool", pool, "PauseToggled", true, false),
		"launchpad: PauseToggled expects 1 event arguments, got 2")
	for _, value := range []interface{}{nil, (*big.Int)(nil)} {
		require.EqualError(t, p.emitAt(evm, "FeeAccumulator", precompile, "ProtocolFeeSwept", value),
			"launchpad: ProtocolFeeSwept argument amount is nil")
	}
	for _, value := range []*big.Int{big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		require.EqualError(t, p.emitAt(evm, "FeeAccumulator", precompile, "ProtocolFeeSwept", value),
			"launchpad: ProtocolFeeSwept argument amount exceeds uint256")
		require.EqualError(t, p.emitAt(evm, "SidioraNFT", precompile, "Transfer", eventAddress(1), eventAddress(2), value),
			"launchpad: Transfer argument tokenId exceeds uint256")
	}
	require.Error(t, p.emitAt(evm, "SidioraPool", pool, "FactoryMigrated", uint64(1)))
	require.Error(t, p.emitAt(evm, "SidioraPool", pool, "PauseToggled", "true"))
	require.EqualError(t, p.emitAt(nil, "SidioraPool", pool, "PauseToggled", false), "EVM is nil")
	require.EqualError(t, p.emitAt(&vm.EVM{}, "SidioraPool", pool, "PauseToggled", false), "EVM StateDB is nil")
	require.Empty(t, db.GetAllLogs())

	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	before := new(big.Int).Set(maximum)
	require.NoError(t, p.emitAt(evm, "FeeAccumulator", precompile, "ProtocolFeeSwept", maximum))
	require.Equal(t, bytes.Repeat([]byte{0xff}, 32), db.GetAllLogs()[0].Data)
	require.Equal(t, before, maximum)
	require.NoError(t, p.emitAt(evm, "SidioraPool", pool, "PauseToggled", false))
	require.Equal(t, make([]byte, 32), db.GetAllLogs()[1].Data)
}

func testFamilyEventRefusals(t *testing.T, p PrecompileExecutor, evm *vm.EVM, db *state.DBImpl) {
	t.Helper()
	require.EqualError(t, p.emitFamily(evm, "FeeRecorded"), "launchpad: unknown family event FeeRecorded")
	require.EqualError(t, (PrecompileExecutor{}).emitFamily(evm, "Swap"), "launchpad: missing family event Swap")
	require.EqualError(t, p.emitFamily(nil, "Swap"), "launchpad: family event requires an EVM")
	for _, number := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		evm.Context.BlockNumber = number
		require.EqualError(t, p.emitFamily(evm, "Swap"), "launchpad: invalid event block number")
	}
	evm.Context.BlockNumber = big.NewInt(41)
	require.EqualError(t, p.emitFamily(evm, "Swap"), "launchpad: Swap expects 9 event arguments, got 2")
	require.Empty(t, db.GetAllLogs())

	args := make([]interface{}, 3, 5)
	args[0], args[1], args[2] = eventHash(1), big.NewInt(0), big.NewInt(1)
	backing := args[:cap(args)]
	backing[3], backing[4] = "untouched", "untouched"
	require.NoError(t, p.emitFamily(evm, "ConfigUpdated", args...))
	require.Equal(t, "untouched", backing[3])
	require.Equal(t, "untouched", backing[4])
	require.Equal(t, int64(41), evm.Context.BlockNumber.Int64())
	evm.Context.BlockNumber.SetInt64(42)
	evm.Context.Time = 74
	require.NoError(t, p.emitFamily(evm, "ConfigUpdated", args...))
	logs := db.GetAllLogs()
	require.Len(t, logs, 2)
	require.Equal(t, eventData(t, []interface{}{big.NewInt(0), big.NewInt(1), big.NewInt(73), big.NewInt(41)}), logs[0].Data)
	require.Equal(t, eventData(t, []interface{}{big.NewInt(0), big.NewInt(1), big.NewInt(74), big.NewInt(42)}), logs[1].Data)
}
