package bank_test

import (
	"embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/ante"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/bank"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	cryptotypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

//go:embed abi.json
var f embed.FS

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }
func (tx mockTx) GetGasEstimate() uint64                          { return 0 }

func TestRun(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	upgradeKeeper := &testApp.UpgradeKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10000000))))
	require.Nil(t, err)

	// Setup receiving addresses
	paxAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, paxAddr, evmAddr)
	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile send test
	send, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "uhpx", big.NewInt(25))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, true, false) // should error because of read only call
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, big.NewInt(1), nil, false, false) // should error because it's not payable
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, false, false) // should error because address is not whitelisted
	require.NotNil(t, err)
	invalidDenomArgs, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "", big.NewInt(25))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, invalidDenomArgs...), 100000, nil, nil, false, false) // should error because denom is empty
	require.NotNil(t, err)

	// Precompile sendNative test error
	sendNative, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID)
	require.Nil(t, err)
	paxAddrString := paxAddr.String()
	argsNativeError, err := sendNative.Inputs.Pack(paxAddrString)
	require.Nil(t, err)
	// 0 amount disallowed
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(0), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("invalidaddr")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack(senderAddr.String())
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, true, false)
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, true)
	require.NotNil(t, err)

	// Send native 10_000_000_000_100, split into 10 uhpx 100wei
	// Test payable with eth LegacyTx
	abi := pcommon.MustGetABI(f, "abi.json")
	argsNative, err := abi.Pack(bank.SendNativeMethod, paxAddr.String())
	require.Nil(t, err)
	require.Nil(t, err)
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(bank.BankAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &addr,
		Value:    big.NewInt(10_000_000_000_100),
		Data:     argsNative,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	// send the transaction
	msgServer := keeper.NewMsgServerImpl(k)
	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx, err = ante.NewEVMFeeCheckDecorator(k, upgradeKeeper).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	evts := ctx.EventManager().ABCIEvents()

	for _, evt := range evts {
		var lines []string
		for _, attr := range evt.Attributes {
			lines = append(lines, fmt.Sprintf("%s=%s", string(attr.Key), string(attr.Value)))
		}
		fmt.Printf("type=%s\t%s\n", evt.Type, strings.Join(lines, "\t"))
	}

	var expectedEvts sdk.Events = []sdk.Event{
		// gas is sent from sender
		banktypes.NewCoinSpentEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(200000)))),
		// wei events
		banktypes.NewWeiSpentEvent(senderAddr, sdk.NewInt(100)),
		banktypes.NewWeiReceivedEvent(paxAddr, sdk.NewInt(100)),
		sdk.NewEvent(
			banktypes.EventTypeWeiTransfer,
			sdk.NewAttribute(banktypes.AttributeKeyRecipient, paxAddr.String()),
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, sdk.NewInt(100).String()),
		),
		// sender sends coin to the receiver
		banktypes.NewCoinSpentEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10)))),
		banktypes.NewCoinReceivedEvent(paxAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10)))),
		sdk.NewEvent(
			banktypes.EventTypeTransfer,
			sdk.NewAttribute(banktypes.AttributeKeyRecipient, paxAddr.String()),
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, sdk.NewCoin("uhpx", sdk.NewInt(10)).String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
		),
		// gas refund to the sender
		banktypes.NewCoinReceivedEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(132401)))),
		// tip is paid to the validator
		banktypes.NewCoinReceivedEvent(sdk.MustAccAddressFromBech32("pax1v4mx6hmrda5kucnpwdjsqqqqqqqqqqqqw04fc9"), sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(67599)))),
	}
	require.EqualValues(t, expectedEvts.ToABCIEvents(), evts)

	// Use precompile balance to verify sendNative uhpx amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "uhpx")
	require.Nil(t, err)
	precompileRes, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	is, err := balance.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(10), is[0].(*big.Int))
	weiBalance := k.BankKeeper().GetWeiBalance(ctx, paxAddr)
	require.Equal(t, big.NewInt(100), weiBalance.BigInt())

	newAddr, _ := testkeeper.MockAddressPair()
	require.Nil(t, k.AccountKeeper().GetAccount(ctx, newAddr))
	argsNewAccount, err := sendNative.Inputs.Pack(newAddr.String())
	require.Nil(t, err)
	require.Nil(t, k.BankKeeper().SendCoins(ctx, paxAddr, k.GetPaxAddressOrDefault(ctx, p.Address()), sdk.NewCoins(sdk.NewCoin("uhpx", sdk.OneInt()))))
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNewAccount...), 100000, big.NewInt(1), nil, false, false)
	require.Nil(t, err)
	// should create account if not exists
	require.NotNil(t, k.AccountKeeper().GetAccount(statedb.Ctx(), newAddr))

	// test get all balances
	allBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID)
	require.Nil(t, err)
	args, err = allBalances.Inputs.Pack(senderEVMAddr)
	require.Nil(t, err)
	precompileRes, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	balances, err := allBalances.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(balances))
	parsedBalances := balances[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})

	require.Equal(t, 2, len(parsedBalances))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(10000000),
		Denom:  "ufoo",
	}, bank.CoinBalance(parsedBalances[0]))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(9932390),
		Denom:  "uhpx",
	}, bank.CoinBalance(parsedBalances[1]))

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), 100000, nil, nil, false, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.NotNil(t, err)

	// invalid input
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, 100000, nil, nil, false, false)
	require.NotNil(t, err)
}

func TestBankRevertReasons(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	const planHeight = int64(100)
	ctx, _ := testApp.NewContext(false, tmtypes.Header{}).CacheContext()
	k := &testApp.EvmKeeper
	_, callerEVMAddr := testkeeper.MockAddressPair()

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	executor := p.GetExecutor().(*bank.PrecompileExecutor)
	balance, err := p.ABI.MethodById(executor.BalanceID)
	require.Nil(t, err)

	executeShortArgs := func(height int64) ([]byte, uint64, error) {
		return executor.Execute(ctx.WithBlockHeight(height), balance, callerEVMAddr, callerEVMAddr, []interface{}{callerEVMAddr}, nil, true, nil, 100000, nil)
	}
	run := func(height int64, input []byte, value *big.Int) []byte {
		evm := vm.EVM{StateDB: state.NewDBImpl(ctx.WithBlockHeight(height), k, false)}
		ret, _, err := p.RunAndCalculateGas(&evm, callerEVMAddr, callerEVMAddr, input, 100000, value, nil, true, false)
		require.Equal(t, vm.ErrExecutionReverted, err)
		return ret
	}
	nonPayableArgs, err := balance.Inputs.Pack(callerEVMAddr, "uhpx")
	require.Nil(t, err)
	nonPayableInput := append(append([]byte{}, executor.BalanceID...), nonPayableArgs...)
	invalidAddrArgs, err := balance.Inputs.Pack(common.Address{}, "uhpx")
	require.Nil(t, err)
	invalidAddrInput := append(append([]byte{}, executor.BalanceID...), invalidAddrArgs...)

	// Before the plan is applied every refusal keeps today's text and bytes.
	ret, gas, err := executeShortArgs(planHeight)
	require.Nil(t, ret)
	require.Equal(t, uint64(0), gas)
	require.EqualError(t, err, "expected 2 arguments but got 1")
	require.Nil(t, run(planHeight, nonPayableInput, big.NewInt(1)))
	require.Nil(t, run(planHeight, invalidAddrInput, nil))

	testApp.UpgradeKeeper.SetDone(ctx.WithBlockHeight(planHeight), pcommon.MergeUpgrade)

	// Below the plan height every refusal keeps today's text and bytes.
	ret, gas, err = executeShortArgs(planHeight - 1)
	require.Nil(t, ret)
	require.Equal(t, uint64(0), gas)
	require.EqualError(t, err, "expected 2 arguments but got 1")
	require.Nil(t, run(planHeight-1, nonPayableInput, big.NewInt(1)))
	require.Nil(t, run(planHeight-1, invalidAddrInput, nil))

	// At and above the plan height each refusal reverts with its LXP_ code.
	for _, height := range []int64{planHeight, planHeight + 1} {
		ret, gas, err = executeShortArgs(height)
		require.Nil(t, ret)
		require.Equal(t, uint64(0), gas)
		require.EqualError(t, err, "LXP_PRECOMPILE_ARG_COUNT: expected N arguments but got M; expected 2 arguments but got 1")
		require.Equal(t, pcommon.LXPRevert("LXP_PRECOMPILE_ARG_COUNT", "expected 2 arguments but got 1"), pcommon.RevertReason(err))

		nonPayable := run(height, nonPayableInput, big.NewInt(1))
		require.Equal(t, pcommon.LXPRevert("LXP_PRECOMPILE_NON_PAYABLE", ""), nonPayable)
		reason, err := abi.UnpackRevert(nonPayable)
		require.Nil(t, err)
		require.Equal(t, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function", reason)

		invalidAddr := run(height, invalidAddrInput, nil)
		require.Equal(t, pcommon.LXPRevert("LXP_PRECOMPILE_INVALID_ADDR", ""), invalidAddr)
		reason, err = abi.UnpackRevert(invalidAddr)
		require.Nil(t, err)
		require.Equal(t, "LXP_PRECOMPILE_INVALID_ADDR: invalid addr", reason)
	}
}

func TestSendForUnlinkedReceiver(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	// testPrivHex := hex.EncodeToString(privKey.Bytes())
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10000000))))
	require.Nil(t, err)

	_, pointerAddr := testkeeper.MockAddressPair()
	k.SetERC20NativePointer(ctx, "ufoo", pointerAddr)

	// Setup receiving addresses - NOT linked
	_, evmAddr := testkeeper.MockAddressPair()
	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile send test
	send, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "ufoo", big.NewInt(100))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, pointerAddr, pointerAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, false, false) // should not error
	require.Nil(t, err)

	// Use precompile balance to verify sendNative uhpx amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "ufoo")
	require.Nil(t, err)
	precompileRes, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	is, err := balance.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(100), is[0].(*big.Int))

	// test get all balances
	allBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID)
	require.Nil(t, err)
	args, err = allBalances.Inputs.Pack(senderEVMAddr)
	require.Nil(t, err)
	precompileRes, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	balances, err := allBalances.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(balances))
	parsedBalances := balances[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})

	require.Equal(t, 2, len(parsedBalances))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(9999900),
		Denom:  "ufoo",
	}, bank.CoinBalance(parsedBalances[0]))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(10000000),
		Denom:  "uhpx",
	}, bank.CoinBalance(parsedBalances[1]))

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), 100000, nil, nil, false, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.NotNil(t, err)

	// invalid input
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, 100000, nil, nil, false, false)
	require.NotNil(t, err)
}

func TestMetadata(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	k.BankKeeper().SetDenomMetaData(ctx, banktypes.Metadata{Name: "PAX", Symbol: "uhpx", Base: "uhpx"})
	p, err := bank.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	name, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).NameID)
	require.Nil(t, err)
	args, err := name.Inputs.Pack("uhpx")
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).NameID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := name.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "PAX", outputs[0])

	symbol, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID)
	require.Nil(t, err)
	args, err = symbol.Inputs.Pack("uhpx")
	require.Nil(t, err)
	res, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = symbol.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "uhpx", outputs[0])

	decimal, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID)
	require.Nil(t, err)
	args, err = decimal.Inputs.Pack("uhpx")
	require.Nil(t, err)
	res, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = decimal.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, uint8(0), outputs[0])
}

func TestAddress(t *testing.T) {
	p, err := bank.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	require.Equal(t, common.HexToAddress(bank.BankAddress), p.Address())
}

const (
	sendContextDenom      = "factory/p/usdl"
	sendContextPlanHeight = int64(500)
	sendContextTopic0     = "0x4b11729a809ec729868d9ba54f3192aa4179fe63668e9f742aa9b25de21de65f"
)

var sendContextHash = common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")

type sendContextEnv struct {
	ctx      sdk.Context
	k        *keeper.Keeper
	p        *pcommon.DynamicGasPrecompile
	executor *bank.PrecompileExecutor
	pointer  common.Address
	from     common.Address
	to       common.Address
	fromPax  sdk.AccAddress
	toPax    sdk.AccAddress
}

// newSendContextEnv funds A with 1000 factory/p/usdl, binds A and B, makes P
// the denom's ERC20 native pointer and applies the merge plan at its height.
func newSendContextEnv(t *testing.T) *sendContextEnv {
	testApp := testkeeper.EVMTestApp
	ctx, _ := testApp.NewContext(false, tmtypes.Header{}).CacheContext()
	ctx = ctx.WithBlockHeight(sendContextPlanHeight)
	k := &testApp.EvmKeeper
	fromPax, from := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, fromPax, from)
	toPax, to := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, toPax, to)
	funds := sdk.NewCoins(sdk.NewCoin(sendContextDenom, sdk.NewInt(1000)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, funds))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, fromPax, funds))
	_, pointer := testkeeper.MockAddressPair()
	k.SetERC20NativePointer(ctx, sendContextDenom, pointer)
	testApp.UpgradeKeeper.SetDone(ctx, pcommon.MergeUpgrade)
	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	return &sendContextEnv{
		ctx:      ctx,
		k:        k,
		p:        p,
		executor: p.GetExecutor().(*bank.PrecompileExecutor),
		pointer:  pointer,
		from:     from,
		to:       to,
		fromPax:  fromPax,
		toPax:    toPax,
	}
}

func (e *sendContextEnv) input(t *testing.T, denom string, amount int64) []byte {
	input, err := e.p.ABI.Pack(bank.SendContextMethod, e.from, e.to, denom, big.NewInt(amount), sendContextHash)
	require.NoError(t, err)
	return input
}

func (e *sendContextEnv) call(height int64, caller common.Address, input []byte, value *big.Int, readOnly bool) ([]byte, *state.DBImpl, error) {
	statedb := state.NewDBImpl(e.ctx.WithBlockHeight(height), e.k, false)
	ret, _, err := e.p.RunAndCalculateGas(&vm.EVM{StateDB: statedb}, caller, caller, input, 1_000_000, value, nil, readOnly, false)
	return ret, statedb, err
}

func (e *sendContextEnv) balance(statedb *state.DBImpl, addr sdk.AccAddress) int64 {
	return e.k.BankKeeper().GetBalance(statedb.Ctx(), addr, sendContextDenom).Amount.Int64()
}

func sendContextReason(t *testing.T, ret []byte) string {
	reason, err := abi.UnpackRevert(ret)
	require.NoError(t, err)
	return reason
}

func sendContextTrue(t *testing.T, p *pcommon.DynamicGasPrecompile, ret []byte) {
	out, err := p.ABI.Unpack(bank.SendContextMethod, ret)
	require.NoError(t, err)
	require.Equal(t, []interface{}{true}, out)
}

func TestSendContextHashSelector(t *testing.T) {
	e := newSendContextEnv(t)
	send0, ok := e.p.ABI.Methods[bank.SendContextMethod]
	require.True(t, ok)
	require.Equal(t, "send(address,address,string,uint256,bytes32)", send0.Sig)
	require.Equal(t, common.FromHex("0x2e4c85ac"), send0.ID)
	require.Equal(t, send0.ID, e.executor.SendContextID)
	send, ok := e.p.ABI.Methods[bank.SendMethod]
	require.True(t, ok)
	require.Equal(t, "send(address,address,string,uint256)", send.Sig)
	require.Equal(t, common.FromHex("0x5c05961b"), send.ID)
	require.Equal(t, send.ID, e.executor.SendID)

	// Input under the literal selector 0x2e4c85ac runs the overload: coins
	// move and the receipt log is emitted.
	args, err := send0.Inputs.Pack(e.from, e.to, sendContextDenom, big.NewInt(7), sendContextHash)
	require.NoError(t, err)
	ret, statedb, err := e.call(sendContextPlanHeight, e.pointer, append(common.FromHex("0x2e4c85ac"), args...), nil, false)
	require.NoError(t, err)
	sendContextTrue(t, e.p, ret)
	require.Equal(t, int64(993), e.balance(statedb, e.fromPax))
	require.Equal(t, int64(7), e.balance(statedb, e.toPax))
	logs := statedb.GetAllLogs()
	require.Len(t, logs, 1)
	require.Equal(t, common.HexToHash(sendContextTopic0), logs[0].Topics[0])
}

func TestSendContextHashRefusals(t *testing.T) {
	e := newSendContextEnv(t)
	valid := e.input(t, sendContextDenom, 5)
	refused := func(caller common.Address, input []byte, value *big.Int, readOnly bool) string {
		ret, statedb, err := e.call(sendContextPlanHeight, caller, input, value, readOnly)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Empty(t, statedb.GetAllLogs())
		require.Equal(t, int64(1000), e.balance(statedb, e.fromPax))
		require.Equal(t, int64(0), e.balance(statedb, e.toPax))
		return sendContextReason(t, ret)
	}

	require.Equal(t, "LXP_NON_CANONICAL: non canonical; cannot call send from staticcall", refused(e.pointer, valid, nil, true))
	require.Equal(t, "LXP_NON_CANONICAL: non canonical; sending funds to a non-payable function", refused(e.pointer, valid, big.NewInt(1), false))
	require.Equal(t, "invalid denom", refused(e.pointer, e.input(t, "", 5), nil, false))
	_, q := testkeeper.MockAddressPair()
	require.Equal(t, "only pointer "+e.pointer.Hex()+" can send "+sendContextDenom+" but got "+q.Hex(), refused(q, valid, nil, false))

	// A paused denom is refused by the bank send-enabled check.
	params := e.k.BankKeeper().GetParams(e.ctx)
	e.k.BankKeeper().SetParams(e.ctx, params.SetSendEnabledParam(sendContextDenom, false))
	require.Equal(t, "LXP_ASSET_PAUSED: asset paused; "+sendContextDenom+" transfers are currently disabled: send transactions are disabled", refused(e.pointer, valid, nil, false))
}

func TestSendContextHashZeroAmount(t *testing.T) {
	e := newSendContextEnv(t)
	ret, statedb, err := e.call(sendContextPlanHeight, e.pointer, e.input(t, sendContextDenom, 0), nil, false)
	require.NoError(t, err)
	sendContextTrue(t, e.p, ret)
	require.Empty(t, statedb.GetAllLogs())
	require.Equal(t, int64(1000), e.balance(statedb, e.fromPax))
	require.Equal(t, int64(0), e.balance(statedb, e.toPax))
}

func TestSendContextHashReceipt(t *testing.T) {
	e := newSendContextEnv(t)
	ret, statedb, err := e.call(sendContextPlanHeight, e.pointer, e.input(t, sendContextDenom, 25), nil, false)
	require.NoError(t, err)
	sendContextTrue(t, e.p, ret)
	require.Equal(t, int64(975), e.balance(statedb, e.fromPax))
	require.Equal(t, int64(25), e.balance(statedb, e.toPax))

	event := e.p.ABI.Events[bank.LayerXReceiptEvent]
	require.Equal(t, common.HexToHash(sendContextTopic0), event.ID)
	logs := statedb.GetAllLogs()
	require.Len(t, logs, 1)
	require.Equal(t, common.HexToAddress(bank.BankAddress), logs[0].Address)
	require.Equal(t, []common.Hash{common.HexToHash(sendContextTopic0), common.BytesToHash(e.from.Bytes()), common.BytesToHash(e.to.Bytes())}, logs[0].Topics)
	data, err := event.Inputs.NonIndexed().Pack(sendContextDenom, big.NewInt(25), sendContextHash)
	require.NoError(t, err)
	require.Equal(t, data, logs[0].Data)
	fields, err := event.Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{sendContextDenom, big.NewInt(25), [32]byte(sendContextHash)}, fields)

	// The four-argument send moves coins and still emits no log.
	input, err := e.p.ABI.Pack(bank.SendMethod, e.from, e.to, sendContextDenom, big.NewInt(25))
	require.NoError(t, err)
	ret, statedb, err = e.call(sendContextPlanHeight, e.pointer, input, nil, false)
	require.NoError(t, err)
	out, err := e.p.ABI.Unpack(bank.SendMethod, ret)
	require.NoError(t, err)
	require.Equal(t, []interface{}{true}, out)
	require.Equal(t, int64(975), e.balance(statedb, e.fromPax))
	require.Equal(t, int64(25), e.balance(statedb, e.toPax))
	require.Empty(t, statedb.GetAllLogs())
}

func TestSendReasonsFromPlanHeight(t *testing.T) {
	e := newSendContextEnv(t)
	valid := e.input(t, sendContextDenom, 5)

	// From the plan height a staticcall of the overload reverts LXP_NON_CANONICAL.
	for _, height := range []int64{sendContextPlanHeight, sendContextPlanHeight + 1} {
		ret, _, err := e.call(height, e.pointer, valid, nil, true)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Equal(t, "LXP_NON_CANONICAL: non canonical; cannot call send from staticcall", sendContextReason(t, ret))
	}

	// One height below the plan the overload does not exist: every call,
	// staticcall or not, reverts with no data, and the refusal is the
	// unknown-method text with no transfer and no log.
	for _, readOnly := range []bool{true, false} {
		ret, statedb, err := e.call(sendContextPlanHeight-1, e.pointer, valid, nil, readOnly)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Nil(t, ret)
		require.Empty(t, statedb.GetAllLogs())
		require.Equal(t, int64(1000), e.balance(statedb, e.fromPax))
		require.Equal(t, int64(0), e.balance(statedb, e.toPax))
	}
	send0 := e.p.ABI.Methods[bank.SendContextMethod]
	args, err := send0.Inputs.Unpack(valid[4:])
	require.NoError(t, err)
	below := e.ctx.WithBlockHeight(sendContextPlanHeight - 1)
	ret, gas, err := e.executor.Execute(below, &send0, e.pointer, e.pointer, args, nil, false, &vm.EVM{StateDB: state.NewDBImpl(below, e.k, false)}, 1_000_000, nil)
	require.Nil(t, ret)
	require.Equal(t, uint64(0), gas)
	require.EqualError(t, err, "no method with id: 0x2e4c85ac")
}
