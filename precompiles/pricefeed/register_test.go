package pricefeed_test

import (
	"encoding/hex"
	"testing"

	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func (h *harness) requireFeedKey(operator sdk.ValAddress, key []byte) {
	h.t.Helper()
	account := feedAccount(key)
	stored, found := h.keeper.GetFeedKey(h.ctx(), operator)
	require.True(h.t, found)
	require.Equal(h.t, pricefeedtypes.FeedKey{PubKey: hex.EncodeToString(key), FeedAccount: account.String()}, stored)
	holder, found := h.keeper.GetValidatorByFeedAccount(h.ctx(), account)
	require.True(h.t, found)
	require.Equal(h.t, operator, holder)
	out := h.view("getFeedKey", common.BytesToAddress(operator))
	require.Equal(h.t, []interface{}{key, common.BytesToAddress(account)}, out)
	require.Equal(h.t, []interface{}{common.BytesToAddress(operator)}, h.view("getValidatorByFeedAccount", common.BytesToAddress(account)))
}

func (h *harness) requireNoFeedKey(operator sdk.ValAddress) {
	h.t.Helper()
	stored, found := h.keeper.GetFeedKey(h.ctx(), operator)
	require.False(h.t, found)
	require.Equal(h.t, pricefeedtypes.FeedKey{}, stored)
	out := h.view("getFeedKey", common.BytesToAddress(operator))
	require.Len(h.t, out, 2)
	require.IsType(h.t, []byte{}, out[0])
	require.Empty(h.t, out[0])
	require.Equal(h.t, common.Address{}, out[1])
}

func (h *harness) requireNoHolder(account sdk.AccAddress) {
	h.t.Helper()
	holder, found := h.keeper.GetValidatorByFeedAccount(h.ctx(), account)
	require.False(h.t, found)
	require.Nil(h.t, holder)
	require.Equal(h.t, []interface{}{common.Address{}}, h.view("getValidatorByFeedAccount", common.BytesToAddress(account)))
}

func (h *harness) register(caller common.Address, key []byte) {
	h.t.Helper()
	out, left, reason := h.call(caller, "registerFeedKey", key)
	require.Empty(h.t, reason)
	require.Equal(h.t, []interface{}{common.BytesToAddress(feedAccount(key))}, out)
	require.Equal(h.t, suppliedGas-32048, left)
}

func (h *harness) requireLog(log *ethtypes.Log, eventName, signature string, operator sdk.ValAddress, account sdk.AccAddress) []interface{} {
	h.t.Helper()
	event, found := h.precompile.GetABI().Events[eventName]
	require.True(h.t, found)
	require.Equal(h.t, pricefeedAddress, log.Address)
	require.Equal(h.t, []common.Hash{
		crypto.Keccak256Hash([]byte(signature)),
		common.BytesToHash(operator),
		common.BytesToHash(account),
	}, log.Topics)
	var indexed abi.Arguments
	for _, input := range event.Inputs {
		if input.Indexed {
			indexed = append(indexed, input)
		}
	}
	decoded := make(map[string]interface{})
	require.NoError(h.t, abi.ParseTopicsIntoMap(decoded, indexed, log.Topics[1:]))
	require.Equal(h.t, map[string]interface{}{
		"validator":   common.BytesToAddress(operator),
		"feedAccount": common.BytesToAddress(account),
	}, decoded)
	data, err := event.Inputs.NonIndexed().Unpack(log.Data)
	require.NoError(h.t, err)
	return data
}

func TestRegisterFeedKey(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("register", stakingtypes.Bonded, true)
	key := feedPubKey("register")
	h.register(caller, key)
	h.requireFeedKey(operator, key)
	require.Len(t, h.stateDB.GetAllLogs(), 1)
}

func TestRegisterFeedKeyRefusesNonValidator(t *testing.T) {
	for _, status := range []stakingtypes.BondStatus{stakingtypes.Unbonded, stakingtypes.Unbonding} {
		t.Run(status.String(), func(t *testing.T) {
			h := newHarness(t)
			operator, caller := h.validator("inactive", status, true)
			key := feedPubKey("inactive")
			_, _, reason := h.call(caller, "registerFeedKey", key)
			requireCode(t, reason, "LXP_PRICEFEED_NOT_VALIDATOR")
			h.requireNoFeedKey(operator)
			h.requireNoHolder(feedAccount(key))
			require.False(t, testkeeper.EVMTestApp.AccountKeeper.HasAccount(h.ctx(), feedAccount(key)))
			require.Empty(t, h.stateDB.GetAllLogs())
		})
	}
	t.Run("no_validator", func(t *testing.T) {
		h := newHarness(t)
		key := feedPubKey("unknown")
		_, _, reason := h.call(h.caller, "registerFeedKey", key)
		requireCode(t, reason, "LXP_PRICEFEED_NOT_VALIDATOR")
		h.requireNoFeedKey(sdk.ValAddress(testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(h.ctx(), h.caller)))
		h.requireNoHolder(feedAccount(key))
		require.False(t, testkeeper.EVMTestApp.AccountKeeper.HasAccount(h.ctx(), feedAccount(key)))
		require.Empty(t, h.stateDB.GetAllLogs())
	})
}

func TestRegisterFeedKeyRefusesInvalidKey(t *testing.T) {
	key := feedPubKey("valid")
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{"uncompressed_length", make([]byte, 65)},
		{"invalid_prefix", append([]byte{0x05}, key[1:]...)},
		{"not_on_curve", append([]byte{0x02}, make([]byte, 32)...)},
		{"empty", []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			operator, caller := h.validator("invalid", stakingtypes.Bonded, true)
			_, _, reason := h.call(caller, "registerFeedKey", tc.key)
			requireCode(t, reason, "LXP_PRICEFEED_INVALID_KEY")
			h.requireNoFeedKey(operator)
			require.Empty(t, h.stateDB.GetAllLogs())
			h.register(caller, key)
			_, _, reason = h.call(caller, "registerFeedKey", tc.key)
			requireCode(t, reason, "LXP_PRICEFEED_INVALID_KEY")
			h.requireFeedKey(operator, key)
			require.Len(t, h.stateDB.GetAllLogs(), 1)
		})
	}
}

func TestRegisterFeedKeyRefusesKeyInUse(t *testing.T) {
	h := newHarness(t)
	first, firstCaller := h.validator("first", stakingtypes.Bonded, true)
	second, secondCaller := h.validator("second", stakingtypes.Bonded, true)
	key, secondKey := feedPubKey("first"), feedPubKey("second")
	h.register(firstCaller, key)
	_, _, reason := h.call(secondCaller, "registerFeedKey", key)
	requireCode(t, reason, "LXP_PRICEFEED_KEY_IN_USE")
	h.requireFeedKey(first, key)
	h.requireNoFeedKey(second)
	require.Len(t, h.stateDB.GetAllLogs(), 1)
	h.register(secondCaller, secondKey)
	_, _, reason = h.call(secondCaller, "registerFeedKey", key)
	requireCode(t, reason, "LXP_PRICEFEED_KEY_IN_USE")
	h.requireFeedKey(first, key)
	h.requireFeedKey(second, secondKey)
	require.Len(t, h.stateDB.GetAllLogs(), 2)
}

func TestRegisterFeedKeyReplaces(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("replace", stakingtypes.Bonded, true)
	first, second := feedPubKey("before"), feedPubKey("after")
	h.register(caller, first)
	h.register(caller, second)
	h.requireFeedKey(operator, second)
	h.requireNoHolder(feedAccount(first))
	logs := h.stateDB.GetAllLogs()
	require.Len(t, logs, 2)
	data := h.requireLog(logs[1], "FeedKeyRegistered", "FeedKeyRegistered(address,address,bytes,address)", operator, feedAccount(second))
	require.Equal(t, []interface{}{second, common.BytesToAddress(feedAccount(first))}, data)
	other, otherCaller := h.validator("reuse", stakingtypes.Bonded, true)
	h.register(otherCaller, first)
	h.requireFeedKey(other, first)
	h.requireFeedKey(operator, second)
}

func TestRegisterFeedKeyAssociation(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("association", stakingtypes.Bonded, false)
	key := feedPubKey("association")
	evmKeeper := &testkeeper.EVMTestApp.EvmKeeper
	_, associated := evmKeeper.GetPaxAddress(h.ctx(), caller)
	require.False(t, associated)
	require.Equal(t, sdk.AccAddress(caller.Bytes()), evmKeeper.GetPaxAddressOrDefault(h.ctx(), caller))
	_, _, reason := h.call(caller, "registerFeedKey", key)
	requireCode(t, reason, "LXP_PRICEFEED_NOT_VALIDATOR")
	h.requireNoFeedKey(operator)
	h.requireNoHolder(feedAccount(key))
	require.Empty(t, h.stateDB.GetAllLogs())
	evmKeeper.SetAddressMapping(h.ctx(), sdk.AccAddress(operator), caller)
	require.Equal(t, sdk.AccAddress(operator), evmKeeper.GetPaxAddressOrDefault(h.ctx(), caller))
	h.register(caller, key)
	h.requireFeedKey(operator, key)

	forwarder := common.HexToAddress("0x9003")
	h.stateDB.SetCode(forwarder, forwardingCode(false))
	paxAddress, associated := evmKeeper.GetPaxAddress(h.ctx(), forwarder)
	require.True(t, associated)
	require.Equal(t, sdk.AccAddress(forwarder.Bytes()), paxAddress)
	ret, _, err := h.evm.Call(caller, forwarder, h.input("getMarketCount"), suppliedGas, uint256.NewInt(0))
	require.NoError(t, err)
	expected, err := h.method("getMarketCount").Outputs.Pack(sdk.NewInt(18).BigInt())
	require.NoError(t, err)
	require.Equal(t, expected, ret)
	replacement := feedPubKey("forwarded")
	ret, _, err = h.evm.Call(caller, forwarder, h.input("registerFeedKey", replacement), suppliedGas, uint256.NewInt(0))
	requireRevert(t, ret, err, "LXP_PRICEFEED_NOT_VALIDATOR")
	h.requireFeedKey(operator, key)
	h.requireNoFeedKey(sdk.ValAddress(forwarder.Bytes()))
	h.requireNoHolder(feedAccount(replacement))
	require.Len(t, h.stateDB.GetAllLogs(), 1)
}

func TestRevokeFeedKey(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("revoke", stakingtypes.Bonded, true)
	key := feedPubKey("revoke")
	h.register(caller, key)
	out, left, reason := h.call(caller, "revokeFeedKey")
	require.Empty(t, reason)
	require.Empty(t, out)
	require.Equal(t, suppliedGas-30000, left)
	h.requireNoFeedKey(operator)
	h.requireNoHolder(feedAccount(key))
	logs := h.stateDB.GetAllLogs()
	require.Len(t, logs, 2)
	data := h.requireLog(logs[1], "FeedKeyRevoked", "FeedKeyRevoked(address,address)", operator, feedAccount(key))
	require.Empty(t, data)
	require.Empty(t, logs[1].Data)
	_, _, reason = h.call(caller, "revokeFeedKey")
	requireCode(t, reason, "LXP_PRICEFEED_NO_KEY")
	h.requireNoFeedKey(operator)
	h.requireNoHolder(feedAccount(key))
	require.Len(t, h.stateDB.GetAllLogs(), 2)
	other, otherCaller := h.validator("revoked_reuse", stakingtypes.Bonded, true)
	h.register(otherCaller, key)
	h.requireFeedKey(other, key)
}

func TestRevokeFeedKeyRefusesOtherCaller(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("owner", stakingtypes.Bonded, true)
	key := feedPubKey("owner")
	h.register(caller, key)
	_, other := h.validator("other", stakingtypes.Bonded, true)
	_, _, reason := h.call(other, "revokeFeedKey")
	requireCode(t, reason, "LXP_PRICEFEED_NO_KEY")
	_, _, reason = h.call(h.caller, "revokeFeedKey")
	requireCode(t, reason, "LXP_PRICEFEED_NOT_VALIDATOR")
	h.requireFeedKey(operator, key)
	require.Len(t, h.stateDB.GetAllLogs(), 1)
}

func TestFeedKeyLogs(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("logs", stakingtypes.Bonded, true)
	key := feedPubKey("logs")
	h.register(caller, key)
	logs := h.stateDB.GetAllLogs()
	require.Len(t, logs, 1)
	data := h.requireLog(logs[0], "FeedKeyRegistered", "FeedKeyRegistered(address,address,bytes,address)", operator, feedAccount(key))
	require.Equal(t, []interface{}{key, common.Address{}}, data)
	replacement := feedPubKey("logs_replacement")
	h.register(caller, replacement)
	logs = h.stateDB.GetAllLogs()
	require.Len(t, logs, 2)
	data = h.requireLog(logs[1], "FeedKeyRegistered", "FeedKeyRegistered(address,address,bytes,address)", operator, feedAccount(replacement))
	require.Equal(t, []interface{}{replacement, common.BytesToAddress(feedAccount(key))}, data)
	_, _, reason := h.call(caller, "revokeFeedKey")
	require.Empty(t, reason)
	logs = h.stateDB.GetAllLogs()
	require.Len(t, logs, 3)
	data = h.requireLog(logs[2], "FeedKeyRevoked", "FeedKeyRevoked(address,address)", operator, feedAccount(replacement))
	require.Empty(t, data)
	require.Empty(t, logs[2].Data)
}
