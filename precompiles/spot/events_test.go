package spot

import (
	"bytes"
	"math/big"
	"testing"

	ibctransferkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/keeper"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

const evtHeight = 41

var (
	evtPECOR      = common.HexToAddress(types.PECORAddress)
	evtOrders     = common.HexToAddress(types.PECOROrdersAddress)
	evtStopOrders = common.HexToAddress(types.PECORStopOrdersAddress)
	evtQuoter     = common.HexToAddress(types.PECORQuoterAddress)
	evtVault      = common.HexToAddress(types.PECORVaultAddress)
	evtShare      = evtAddress(0x5a)
)

type evtEnv struct {
	ctx    sdk.Context
	keeper *evmkeeper.Keeper
	db     *state.DBImpl
	evm    *vm.EVM
}

func evtState(t *testing.T) evtEnv {
	t.Helper()
	key := sdk.NewKVStoreKey("spot_event_evm")
	transient := sdk.NewTransientStoreKey("spot_event_transient")
	ctx := testutil.DefaultContext(key, transient).WithBlockHeight(evtHeight)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	subspace := func(name string) paramtypes.Subspace {
		return paramtypes.NewSubspace(cdc, codec.NewLegacyAmino(), key, transient, name)
	}
	accounts := authkeeper.NewAccountKeeper(cdc, key, subspace("spot_event_auth"), authtypes.ProtoBaseAccount,
		map[string][]string{authtypes.FeeCollectorName: nil})
	keeper := evmkeeper.NewKeeper(key, transient, subspace("spot_event_evm"), nil, nil, &accounts, nil,
		ibctransferkeeper.Keeper{}, nil, nil, nil)
	db := state.NewDBImpl(ctx, keeper, false)
	t.Cleanup(db.Cleanup)
	evm := &vm.EVM{StateDB: db, Context: vm.BlockContext{Time: 73, BlockNumber: big.NewInt(evtHeight)}}
	return evtEnv{ctx: ctx, keeper: keeper, db: db, evm: evm}
}

func evtAddress(n byte) common.Address {
	return common.BytesToAddress(bytes.Repeat([]byte{n}, common.AddressLength))
}

func evtInt(n int64) *big.Int {
	return new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 96), big.NewInt(n))
}

func evtWord(t *testing.T, value interface{}) []byte {
	t.Helper()
	switch v := value.(type) {
	case common.Address:
		return common.LeftPadBytes(v.Bytes(), 32)
	case *big.Int:
		return common.LeftPadBytes(v.Bytes(), 32)
	case uint8:
		return common.LeftPadBytes([]byte{v}, 32)
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

// evtExpected is the log the event writes: topic0, the first indexed values
// as topics and the rest as 32-byte data words.
func evtExpected(t *testing.T, address common.Address, topic0 string, indexed int, values []interface{}) *ethtypes.Log {
	t.Helper()
	topics := []common.Hash{common.HexToHash(topic0)}
	var data []byte
	for i, value := range values {
		if i < indexed {
			topics = append(topics, common.BytesToHash(evtWord(t, value)))
		} else {
			data = append(data, evtWord(t, value)...)
		}
	}
	return &ethtypes.Log{Address: address, Topics: topics, Data: data}
}

// checkSpotEvent emits name at every address through emitEvent, replays the
// written log as a keeper list through emitLogs, appends it to the spot
// synthetic receipt, and asserts the exact log on all three paths; each
// refused address must write nothing.
func checkSpotEvent(t *testing.T, name, topic0 string, served, refused []common.Address, indexed int, values ...interface{}) {
	t.Helper()
	for _, address := range served {
		env := evtState(t)
		want := evtExpected(t, address, topic0, indexed, values)

		require.NoError(t, emitEvent(env.evm, address, name, values...), address.Hex())
		logs := env.db.GetAllLogs()
		require.Len(t, logs, 1)
		require.Equal(t, want.Address, logs[0].Address)
		require.Equal(t, want.Topics, logs[0].Topics)
		require.Equal(t, want.Data, logs[0].Data)
		require.Equal(t, uint(0), logs[0].Index)

		require.NoError(t, emitLogs(env.evm, []*ethtypes.Log{want}))
		logs = env.db.GetAllLogs()
		require.Len(t, logs, 2)
		require.Equal(t, want.Address, logs[1].Address)
		require.Equal(t, want.Topics, logs[1].Topics)
		require.Equal(t, want.Data, logs[1].Data)
		require.Equal(t, uint(1), logs[1].Index)

		require.NoError(t, AppendEndBlockLogs(env.ctx, env.keeper, []*ethtypes.Log{want}))
		kind := evmkeeper.SyntheticReceiptSpot
		rcpt, err := env.keeper.GetTransientReceipt(env.ctx, evmkeeper.SyntheticReceiptHash(kind, evtHeight),
			uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind)))
		require.NoError(t, err)
		require.Equal(t, evmkeeper.SyntheticReceiptHash(kind, evtHeight).Hex(), rcpt.TxHashHex)
		require.Equal(t, evmkeeper.SyntheticReceiptProvisionalIndex(kind), rcpt.TransactionIndex)
		require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), rcpt.Status)
		require.Len(t, rcpt.Logs, 1)
		require.Equal(t, address.Hex(), rcpt.Logs[0].Address)
		wantTopics := make([]string, len(want.Topics))
		for i, topic := range want.Topics {
			wantTopics[i] = topic.Hex()
		}
		require.Equal(t, wantTopics, rcpt.Logs[0].Topics)
		require.Equal(t, want.Data, rcpt.Logs[0].Data)
		require.True(t, rcpt.Logs[0].Synthetic)
		require.Equal(t, uint32(0), rcpt.Logs[0].Index)
		bloom := ethtypes.BytesToBloom(rcpt.LogsBloom)
		require.True(t, bloom.Test(address.Bytes()))
		require.True(t, bloom.Test(want.Topics[0].Bytes()))
	}
	for _, address := range refused {
		env := evtState(t)
		require.Error(t, emitEvent(env.evm, address, name, values...), address.Hex())
		require.Error(t, emitLogs(env.evm, []*ethtypes.Log{evtExpected(t, address, topic0, indexed, values)}), address.Hex())
		require.Empty(t, env.db.GetAllLogs())
	}
}

func TestEventTieredFeeApplied(t *testing.T) {
	checkSpotEvent(t, "TieredFeeApplied", "0x2e5d94e9d6c18964a316947fbe6368a89eae77275308006b99685ed7624d33b3",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtStopOrders, evtQuoter, evtVault, evtShare},
		2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4), evtInt(5))
}

func TestEventPriceImpactApplied(t *testing.T) {
	checkSpotEvent(t, "PriceImpactApplied", "0x2c967c87f38e293c4c2df8277028656cf8d42c2f6b5c06cd97b53eaebc8f7466",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtVault, evtShare},
		2, evtAddress(1), evtAddress(2), big.NewInt(500), evtInt(4))
}

func TestEventMarketOrderExecuted(t *testing.T) {
	checkSpotEvent(t, "MarketOrderExecuted", "0x0812c3288c5d540088b34db30524fa26b13055f197af021d88b59427a0dbdbbe",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtVault, evtShare},
		3, evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5), true)
	checkSpotEvent(t, "MarketOrderExecuted", "0x0812c3288c5d540088b34db30524fa26b13055f197af021d88b59427a0dbdbbe",
		[]common.Address{evtPECOR}, nil, 3, evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5), false)
}

func TestEventSimpleSwap(t *testing.T) {
	checkSpotEvent(t, "SimpleSwap", "0x1075e5ad13b06b3fb5457eb645716bfaea9f0117bfefda8d940bf2b926b41c1f",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtVault, evtShare},
		3, evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5), evtInt(6))
}

func TestEventNativeSwap(t *testing.T) {
	checkSpotEvent(t, "NativeSwap", "0xab06313637ec959a5d89f14a6be0f060979206219814eee5ed4d74dc7525ba20",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtVault, evtShare},
		2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4), true)
	checkSpotEvent(t, "NativeSwap", "0xab06313637ec959a5d89f14a6be0f060979206219814eee5ed4d74dc7525ba20",
		[]common.Address{evtPECOR}, nil, 2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4), false)
}

func TestEventOwnershipTransferred(t *testing.T) {
	checkSpotEvent(t, "OwnershipTransferred", "0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0",
		[]common.Address{evtPECOR, evtOrders, evtStopOrders, evtVault}, []common.Address{evtQuoter, evtShare, {}},
		2, common.Address{}, types.GovernanceEVMAddress())
}

func TestEventPaused(t *testing.T) {
	checkSpotEvent(t, "Paused", "0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258",
		[]common.Address{evtPECOR, evtOrders, evtStopOrders}, []common.Address{evtQuoter, evtVault, evtShare},
		0, types.GovernanceEVMAddress())
}

func TestEventUnpaused(t *testing.T) {
	checkSpotEvent(t, "Unpaused", "0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa",
		[]common.Address{evtPECOR, evtOrders, evtStopOrders}, []common.Address{evtQuoter, evtVault, evtShare},
		0, types.GovernanceEVMAddress())
}

func TestEventTwapSanityUpdated(t *testing.T) {
	checkSpotEvent(t, "TwapSanityUpdated", "0x53c5e83e6ce0cd0796333da83f54e0738e25b57e301950baab492700fc969808",
		[]common.Address{evtPECOR}, []common.Address{evtOrders, evtStopOrders, evtVault, evtShare},
		0, true, big.NewInt(1800), big.NewInt(500))
}

func TestEventLimitOrderCreated(t *testing.T) {
	checkSpotEvent(t, "LimitOrderCreated", "0x7df754202cad14218ee77bcea0466d971b8145466b889f56634e2e8e66d17404",
		[]common.Address{evtOrders}, []common.Address{evtPECOR, evtStopOrders, evtVault, evtShare},
		2, evtInt(1), evtAddress(2), uint8(1), evtInt(4), evtInt(5))
}

func TestEventLimitOrderCancelled(t *testing.T) {
	checkSpotEvent(t, "LimitOrderCancelled", "0xa79034042a790ce528ef3275727fa99fa364bebccd18f9d6094e524e8c075f86",
		[]common.Address{evtOrders}, []common.Address{evtPECOR, evtStopOrders, evtVault, evtShare},
		1, evtInt(1))
}

func TestEventLimitOrderExecuted(t *testing.T) {
	checkSpotEvent(t, "LimitOrderExecuted", "0x8473464af225412ba591d2bcb9cbddb9456a28347037e7fd76b14ef834e7caff",
		[]common.Address{evtOrders}, []common.Address{evtPECOR, evtStopOrders, evtVault, evtShare},
		1, evtInt(1), evtInt(2))
}

func TestEventLimitOrderExpired(t *testing.T) {
	checkSpotEvent(t, "LimitOrderExpired", "0xdf3c7dca4ba1afef3742d2720d6e060c9abc5bfcfcaa653ee459d8b22ae17217",
		[]common.Address{evtOrders}, []common.Address{evtPECOR, evtStopOrders, evtVault, evtShare},
		1, evtInt(1))
}

func TestEventLimitOrderModified(t *testing.T) {
	checkSpotEvent(t, "LimitOrderModified", "0x4faf905c59d1d8f751a461274695b2985d465bc0ab5818e8c53e12c6973085c1",
		[]common.Address{evtOrders}, []common.Address{evtPECOR, evtStopOrders, evtVault, evtShare},
		1, evtInt(1), evtInt(2), evtInt(3))
}

func TestEventStopLimitOrderCreated(t *testing.T) {
	checkSpotEvent(t, "StopLimitOrderCreated", "0x49fa34ac3a10ddec0a58aa62cf941b37b9cf4a0f50bed03f51b4d5fc63e05df8",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		2, evtInt(1), evtAddress(2), uint8(0), evtInt(4), evtInt(5), evtInt(6))
}

func TestEventStopLimitActivated(t *testing.T) {
	checkSpotEvent(t, "StopLimitActivated", "0xc72ba33cc42222338fd43a4280362b9b929d178adfe8f78b2c5bf80fa7cbd6a6",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		1, evtInt(1), evtInt(2))
}

func TestEventStopLimitExecuted(t *testing.T) {
	checkSpotEvent(t, "StopLimitExecuted", "0x692c4dc9926d7938b6b75f64cc78d24b5e08f43ad0ca442f8cc2dccb3b4d5273",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		1, evtInt(1), evtInt(2))
}

func TestEventStopLimitCancelled(t *testing.T) {
	checkSpotEvent(t, "StopLimitCancelled", "0x42fdbbee01a2dbf337b473004600b09fe13bf50911fef7df30d522b2e8f90fcc",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		1, evtInt(1))
}

func TestEventStopLossOrderCreated(t *testing.T) {
	checkSpotEvent(t, "StopLossOrderCreated", "0xb9007dd5be35138b4f9bfc3c734722460cae1a8b97da401020d3ac2762d7655b",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		2, evtInt(1), evtAddress(2), evtInt(3), evtInt(4))
}

func TestEventStopLossOrderExecuted(t *testing.T) {
	checkSpotEvent(t, "StopLossOrderExecuted", "0x029daf69e7abd7c123fcc3c78a61be884c0f62c4e0780faf5de3b66d02b5b8f8",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		1, evtInt(1), evtInt(2))
}

func TestEventStopLossOrderCancelled(t *testing.T) {
	checkSpotEvent(t, "StopLossOrderCancelled", "0x79403a00e8e6e0c47f9212a62ecd75191e2f197b251c042b5cacf631da0b34c0",
		[]common.Address{evtStopOrders}, []common.Address{evtPECOR, evtOrders, evtVault, evtShare},
		1, evtInt(1))
}

func TestEventDeposit(t *testing.T) {
	checkSpotEvent(t, "Deposit", "0xdcbc1c05240f31ff3ad067ef1ee35ce4997762752e3a095284754544f4c709d7",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4))
}

func TestEventLPWithdrawal(t *testing.T) {
	checkSpotEvent(t, "LPWithdrawal", "0xbacc4c5632df305652cdf9d17913383ae2857b949b623a2ce7b3537458bd0075",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4))
}

func TestEventNativeDeposit(t *testing.T) {
	checkSpotEvent(t, "NativeDeposit", "0x2d45cf213b0878e6582a0bddc676c202f8f57ed3cd74e8ccf8955ce1359bd427",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), evtInt(2), evtInt(3))
}

func TestEventNativeWithdrawal(t *testing.T) {
	checkSpotEvent(t, "NativeWithdrawal", "0x4c556479041161db341af3dc4010b53fa70282739dc38d464ff9628ee9c9d29f",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), evtInt(2), evtInt(3))
}

func TestEventReservesSync(t *testing.T) {
	checkSpotEvent(t, "ReservesSync", "0x055fb2ac115b8a1258ed74cd751869f95f1fb10b2c889147acbaca28ba479e5f",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), evtInt(2), evtInt(3), evtInt(4))
}

func TestEventReservesUpdated(t *testing.T) {
	checkSpotEvent(t, "ReservesUpdated", "0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), evtInt(2), evtInt(3))
}

func TestEventWithdrawal(t *testing.T) {
	checkSpotEvent(t, "Withdrawal", "0xc2b4a290c20fb28939d29f102514fbffd2b73c059ffba8b78250c94161d5fcc6",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		2, evtAddress(1), evtAddress(2), evtInt(3), evtInt(4))
}

func TestEventOperatorUpdated(t *testing.T) {
	checkSpotEvent(t, "OperatorUpdated", "0x966c160e1c4dbc7df8d69af4ace01e9297c3cf016397b7914971f2fbfa32672d",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtPECOR, true)
}

func TestEventShareTokenCreated(t *testing.T) {
	checkSpotEvent(t, "ShareTokenCreated", "0x6f64ce3fea7909e3e9ebc85ea8450670af3fe5d23143325c4f1ebd38c5343256",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		2, evtAddress(1), evtShare)
}

func TestEventTokenPauseUpdated(t *testing.T) {
	checkSpotEvent(t, "TokenPauseUpdated", "0x253b2f7aa4de4ccf23ab8e9c2078ab1cc589649293d26006ecdf802a5d6b451d",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), true)
}

func TestEventTokenRegistered(t *testing.T) {
	checkSpotEvent(t, "TokenRegistered", "0x3247537f6efe5e5d3f25242f42522e1bfbabeea906fc7005a128b11bcdb3f99b",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), uint8(18), true)
}

func TestEventTransactionTrackerUpdated(t *testing.T) {
	checkSpotEvent(t, "TransactionTrackerUpdated", "0x3dd2b7e16ddea78bec31b5b95991ba3d03fda27bc5c8a8441775b76c8f88033e",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, common.Address{})
}

func TestEventUtilizationCapUpdated(t *testing.T) {
	checkSpotEvent(t, "UtilizationCapUpdated", "0xbcfb6f6e36e3fb105ffb3042d755b572cec796e9c4c634f0bb7ef3c8967331ac",
		[]common.Address{evtVault}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtShare},
		1, evtAddress(1), big.NewInt(8000))
}

func TestEventApproval(t *testing.T) {
	checkSpotEvent(t, "Approval", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925",
		[]common.Address{evtShare}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtQuoter, evtVault, {}},
		2, evtAddress(1), evtAddress(2), evtInt(3))
}

func TestEventTransfer(t *testing.T) {
	checkSpotEvent(t, "Transfer", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		[]common.Address{evtShare}, []common.Address{evtPECOR, evtOrders, evtStopOrders, evtQuoter, evtVault, {}},
		2, common.Address{}, evtAddress(2), evtInt(3))
	checkSpotEvent(t, "Transfer", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		[]common.Address{evtShare}, nil, 2, evtAddress(2), common.Address{}, evtInt(3))
}

func TestEventTopicsInAbiOrder(t *testing.T) {
	served := []abi.ABI{shareEventABI}
	for _, railABI := range railEventABIs {
		served = append(served, railABI)
	}
	ids := map[common.Hash]bool{}
	for _, contract := range served {
		for name, event := range contract.Events {
			args := make([]interface{}, len(event.Inputs))
			var wantTopics []common.Hash
			wantData := []interface{}{}
			for i, input := range event.Inputs {
				switch input.Type.T {
				case abi.AddressTy:
					args[i] = evtAddress(byte(i + 1))
				case abi.UintTy:
					if input.Type.Size == 8 {
						args[i] = uint8(i + 1)
					} else {
						args[i] = evtInt(int64(i + 1))
					}
				case abi.BoolTy:
					args[i] = i%2 == 0
				default:
					t.Fatalf("%s input %s has unsupported type %s", name, input.Name, input.Type.String())
				}
				if input.Indexed {
					wantTopics = append(wantTopics, common.BytesToHash(evtWord(t, args[i])))
				} else {
					wantData = append(wantData, args[i])
				}
			}
			topics, data, err := eventTopics(event, args)
			require.NoError(t, err, name)
			require.Equal(t, append([]common.Hash{event.ID}, wantTopics...), topics, name)
			require.Equal(t, wantData, data, name)
			ids[event.ID] = true
		}
	}
	require.Len(t, ids, 36)

	event := railEventABIs[evtPECOR].Events["SimpleSwap"]
	_, _, err := eventTopics(event, []interface{}{evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5)})
	require.ErrorContains(t, err, "expects 6 event arguments, got 5")
	_, _, err = eventTopics(event, []interface{}{evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), (*big.Int)(nil), evtInt(6)})
	require.ErrorContains(t, err, "argument amountOut is nil")
	_, _, err = eventTopics(event, []interface{}{evtAddress(1), evtAddress(2), evtAddress(3), big.NewInt(-1), evtInt(5), evtInt(6)})
	require.ErrorContains(t, err, "argument amountIn exceeds uint256")
	_, _, err = eventTopics(event, []interface{}{evtAddress(1), evtAddress(2), evtAddress(3), new(big.Int).Lsh(big.NewInt(1), 256), evtInt(5), evtInt(6)})
	require.ErrorContains(t, err, "argument amountIn exceeds uint256")
	_, _, err = eventTopics(event, []interface{}{evtInt(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5), evtInt(6)})
	require.ErrorContains(t, err, "argument user")

	valid := evtExpected(t, evtVault, "0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5", 1,
		[]interface{}{evtAddress(1), evtInt(2), evtInt(3)})
	require.NoError(t, checkLog(valid))
	require.ErrorContains(t, checkLog(nil), "nil log")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Address: evtVault}), "has no topic0")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Address: evtVault, Topics: valid.Topics[:1], Data: valid.Data}),
		"ReservesUpdated log has 1 topics, the ABI declares 2")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Address: evtVault, Topics: valid.Topics, Data: valid.Data[:32]}),
		"ReservesUpdated log data")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Address: evtVault, Topics: valid.Topics, Data: append(append([]byte(nil), valid.Data...), make([]byte, 32)...)}),
		"not the ABI encoding")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Address: evtQuoter, Topics: valid.Topics, Data: valid.Data}),
		"serves no event with topic0")
	require.ErrorContains(t, checkLog(&ethtypes.Log{Topics: valid.Topics, Data: valid.Data}), "zero address")
	badBool := evtExpected(t, evtVault, "0x253b2f7aa4de4ccf23ab8e9c2078ab1cc589649293d26006ecdf802a5d6b451d", 1,
		[]interface{}{evtAddress(1), big.NewInt(2)})
	require.Error(t, checkLog(badBool))
}

func TestRevertWritesNoLog(t *testing.T) {
	env := evtState(t)
	snapshot := env.db.Snapshot()
	require.NoError(t, emitEvent(env.evm, evtPECOR, "SimpleSwap", evtAddress(1), evtAddress(2), evtAddress(3), evtInt(4), evtInt(5), evtInt(6)))
	require.NoError(t, emitLogs(env.evm, []*ethtypes.Log{evtExpected(t, evtVault,
		"0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5", 1, []interface{}{evtAddress(1), evtInt(2), evtInt(3)})}))
	require.Len(t, env.db.GetAllLogs(), 2)
	env.db.RevertToSnapshot(snapshot)
	require.Empty(t, env.db.GetAllLogs())

	valid := evtExpected(t, evtVault, "0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5", 1,
		[]interface{}{evtAddress(1), evtInt(2), evtInt(3)})
	invalid := &ethtypes.Log{Address: evtOrders, Topics: valid.Topics, Data: valid.Data}
	require.ErrorContains(t, emitLogs(env.evm, []*ethtypes.Log{valid, invalid}), "serves no event with topic0")
	require.Empty(t, env.db.GetAllLogs())

	kind := evmkeeper.SyntheticReceiptSpot
	hash := evmkeeper.SyntheticReceiptHash(kind, evtHeight)
	index := uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind))
	require.ErrorContains(t, AppendEndBlockLogs(env.ctx, env.keeper, []*ethtypes.Log{valid, invalid}), "serves no event with topic0")
	_, err := env.keeper.GetTransientReceipt(env.ctx, hash, index)
	require.ErrorIs(t, err, receipt.ErrNotFound)

	cached, _ := env.ctx.CacheContext()
	require.NoError(t, AppendEndBlockLogs(cached, env.keeper, []*ethtypes.Log{valid}))
	rcpt, err := env.keeper.GetTransientReceipt(cached, hash, index)
	require.NoError(t, err)
	require.Len(t, rcpt.Logs, 1)
	_, err = env.keeper.GetTransientReceipt(env.ctx, hash, index)
	require.ErrorIs(t, err, receipt.ErrNotFound)

	cached, write := env.ctx.CacheContext()
	require.NoError(t, AppendEndBlockLogs(cached, env.keeper, []*ethtypes.Log{valid}))
	require.NoError(t, AppendEndBlockLogs(cached, env.keeper, []*ethtypes.Log{valid}))
	write()
	rcpt, err = env.keeper.GetTransientReceipt(env.ctx, hash, index)
	require.NoError(t, err)
	require.Len(t, rcpt.Logs, 2)
	require.Equal(t, uint32(0), rcpt.Logs[0].Index)
	require.Equal(t, uint32(1), rcpt.Logs[1].Index)
}
