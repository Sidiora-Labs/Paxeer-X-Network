package spot_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// The served JSON of each spot address and of the vault share pointer.
const (
	servedPECOR      = "abi/pecor.json"
	servedOrders     = "abi/orders.json"
	servedStopOrders = "abi/stoporders.json"
	servedQuoter     = "abi/quoter.json"
	servedVault      = "abi/vault.json"
	servedShare      = "abi/share.json"
)

var servedFiles = []string{servedPECOR, servedOrders, servedStopOrders, servedQuoter, servedVault, servedShare}

func servedABI(t *testing.T, file string) abi.ABI {
	t.Helper()
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(raw))
	require.NoError(t, err)
	return parsed
}

func word(value []byte) []byte { return common.LeftPadBytes(value, 32) }

// requireRevert asserts that err packs to exactly selector followed by words,
// that the selector is keccak256(signature)[:4], and that exactly the served
// files in carriers declare the error, each packing args to the same bytes.
func requireRevert(t *testing.T, err error, signature, selector string, carriers []string, words [][]byte, args ...interface{}) {
	t.Helper()
	want := hexutil.MustDecode(selector)
	require.Len(t, want, 4)
	require.Equal(t, crypto.Keccak256([]byte(signature))[:4], want, signature)
	for _, w := range words {
		want = append(want, w...)
	}
	got := spot.PackError(err)
	require.Equal(t, hexutil.Encode(want), hexutil.Encode(got), signature)

	var abiErr *types.AbiError
	require.True(t, errors.As(err, &abiErr))
	require.Equal(t, signature, abiErr.Signature())

	name := signature[:bytes.IndexByte([]byte(signature), '(')]
	carried := map[string]bool{}
	for _, file := range carriers {
		carried[file] = true
	}
	for _, file := range servedFiles {
		entry, ok := servedABI(t, file).Errors[name]
		require.Equal(t, carried[file], ok, "%s in %s", signature, file)
		if !ok {
			continue
		}
		require.Equal(t, signature, entry.Sig, file)
		require.Equal(t, selector, hexutil.Encode(entry.ID[:4]), file)
		packed, packErr := entry.Inputs.Pack(args...)
		require.NoError(t, packErr, file)
		require.Equal(t, got, append(entry.ID[:4:4], packed...), file)
	}
}

func requireNoArgs(t *testing.T, err error, signature, selector string, carriers ...string) {
	t.Helper()
	requireRevert(t, err, signature, selector, carriers, nil)
}

var (
	errorAccount = common.HexToAddress("0x00000000000000000000000000000000000a0001")
	errorToken   = common.HexToAddress("0x00000000000000000000000000000000000b0002")
)

func TestErrorSafeERC20FailedOperation(t *testing.T) {
	requireRevert(t, types.SafeERC20FailedOperation(errorToken), "SafeERC20FailedOperation(address)", "0x5274afe7",
		[]string{servedPECOR, servedOrders, servedStopOrders, servedVault}, [][]byte{word(errorToken.Bytes())}, errorToken)
}

func TestErrorReentrancyGuardReentrantCall(t *testing.T) {
	requireNoArgs(t, types.ErrReentrancyGuardReentrantCall, "ReentrancyGuardReentrantCall()", "0x3ee5aeb5",
		servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorUnsupportedTransfer(t *testing.T) {
	requireNoArgs(t, types.ErrUnsupportedTransfer, "UnsupportedTransfer()", "0xce2d28e1",
		servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorStalePrice(t *testing.T) {
	requireNoArgs(t, types.ErrStalePrice, "StalePrice()", "0x19abf40e", servedPECOR, servedOrders, servedStopOrders, servedQuoter)
}

func TestErrorInvalidOracle(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidOracle, "InvalidOracle()", "0x9589a27d", servedOrders, servedStopOrders)
}

func TestErrorNotAStablecoin(t *testing.T) {
	requireNoArgs(t, types.ErrNotAStablecoin, "NotAStablecoin()", "0x777a9be8", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorTokenIsStablecoin(t *testing.T) {
	requireNoArgs(t, types.ErrTokenIsStablecoin, "TokenIsStablecoin()", "0xc1343a37", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorZeroAmount(t *testing.T) {
	requireNoArgs(t, types.ErrZeroAmount, "ZeroAmount()", "0x1f2a2005", servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorExpired(t *testing.T) {
	requireNoArgs(t, types.ErrExpired, "Expired()", "0x203d82d8", servedPECOR)
}

func TestErrorInsufficientOutput(t *testing.T) {
	requireNoArgs(t, types.ErrInsufficientOutput, "InsufficientOutput()", "0xbb2875c3", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorInsufficientLiquidity(t *testing.T) {
	requireNoArgs(t, types.ErrInsufficientLiquidity, "InsufficientLiquidity()", "0xbb55fd27",
		servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorSameToken(t *testing.T) {
	requireNoArgs(t, types.ErrSameToken, "SameToken()", "0x201b580a", servedPECOR, servedOrders)
}

func TestErrorExcessiveInput(t *testing.T) {
	requireNoArgs(t, types.ErrExcessiveInput, "ExcessiveInput()", "0xbd750b82", servedPECOR)
}

func TestErrorUseWethDeposit(t *testing.T) {
	requireNoArgs(t, types.ErrUseWethDeposit, "UseWethDeposit()", "0xb1da5593", servedPECOR)
}

func TestErrorUseWethWithdraw(t *testing.T) {
	requireNoArgs(t, types.ErrUseWethWithdraw, "UseWethWithdraw()", "0x74ee4e2a", servedPECOR)
}

func TestErrorNativeTransferFailed(t *testing.T) {
	requireNoArgs(t, types.ErrNativeTransferFailed, "NativeTransferFailed()", "0xf4b3b1bc", servedPECOR)
}

func TestErrorMulticallFailed(t *testing.T) {
	requireRevert(t, types.MulticallFailed(uint256.NewInt(3)), "MulticallFailed(uint256)", "0x750ebb7d",
		[]string{servedPECOR}, [][]byte{word([]byte{3})}, big.NewInt(3))
}

func TestErrorUnsupportedInputTransfer(t *testing.T) {
	requireNoArgs(t, types.ErrUnsupportedInputTransfer, "UnsupportedInputTransfer()", "0x1c42e5ce",
		servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorOwnableUnauthorizedAccount(t *testing.T) {
	requireRevert(t, types.OwnableUnauthorizedAccount(errorAccount), "OwnableUnauthorizedAccount(address)", "0x118cdaa7",
		[]string{servedPECOR, servedOrders, servedStopOrders, servedQuoter, servedVault, servedShare},
		[][]byte{word(errorAccount.Bytes())}, errorAccount)
}

func TestErrorOwnableInvalidOwner(t *testing.T) {
	requireRevert(t, types.OwnableInvalidOwner(common.Address{}), "OwnableInvalidOwner(address)", "0x1e4fbdf7",
		[]string{servedPECOR, servedOrders, servedStopOrders, servedVault, servedShare},
		[][]byte{word(nil)}, common.Address{})
}

func TestErrorEnforcedPause(t *testing.T) {
	requireNoArgs(t, types.ErrEnforcedPause, "EnforcedPause()", "0xd93c0665", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorExpectedPause(t *testing.T) {
	requireNoArgs(t, types.ErrExpectedPause, "ExpectedPause()", "0x8dfc202b", servedPECOR, servedOrders, servedStopOrders)
}

// requireParamsRefusal asserts that Params.Validate refuses p with the module
// error named by the AbiError want, so the module error and the ABI selector
// carry the same signature.
func requireParamsRefusal(t *testing.T, p types.Params, refusal types.ParamsError, want *types.AbiError) {
	t.Helper()
	err := p.Validate()
	require.ErrorIs(t, err, refusal)
	require.Equal(t, want.Signature(), string(refusal))
}

func TestErrorFeeTooHigh(t *testing.T) {
	requireNoArgs(t, types.ErrFeeTooHigh, "FeeTooHigh()", "0xcd4e6167")
	p := types.DefaultParams()
	p.SwapFeeBps, p.Tier1FeeBps, p.Tier2FeeBps = 150, 30, 30
	requireParamsRefusal(t, p, types.ErrParamsFeeTooHigh, types.ErrFeeTooHigh)
}

func TestErrorTier2BelowTier1(t *testing.T) {
	requireNoArgs(t, types.ErrTier2BelowTier1, "Tier2BelowTier1()", "0x305ea8a9")
	p := types.DefaultParams()
	p.Tier1FeeBps, p.Tier2FeeBps = 10, 5
	requireParamsRefusal(t, p, types.ErrParamsTier2BelowTier1, types.ErrTier2BelowTier1)
}

func TestErrorScalarTooHigh(t *testing.T) {
	requireNoArgs(t, types.ErrScalarTooHigh, "ScalarTooHigh()", "0xf4e307b5")
	p := types.DefaultParams()
	p.PriceImpactScalarBps = types.MaxImpactBps + 1
	requireParamsRefusal(t, p, types.ErrParamsScalarTooHigh, types.ErrScalarTooHigh)
	p = types.DefaultParams()
	p.OrderPriceImpactScalarBps = types.MaxImpactBps + 1
	requireParamsRefusal(t, p, types.ErrParamsScalarTooHigh, types.ErrScalarTooHigh)
}

func TestErrorNoFeesToCollect(t *testing.T) {
	requireNoArgs(t, types.ErrNoFeesToCollect, "NoFeesToCollect()", "0x6a4ea9e4", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorZeroAddress(t *testing.T) {
	requireNoArgs(t, types.ErrZeroAddress, "ZeroAddress()", "0xd92e233d")
	p := types.DefaultParams()
	p.FeeCollector = common.Address{}
	requireParamsRefusal(t, p, types.ErrParamsZeroAddress, types.ErrZeroAddress)
}

func TestErrorOrderBelowMinimumNotional(t *testing.T) {
	requireNoArgs(t, types.ErrOrderBelowMinimumNotional, "OrderBelowMinimumNotional()", "0x9b2bea5b", servedOrders, servedStopOrders)
}

func TestErrorTooManyActiveOrders(t *testing.T) {
	requireNoArgs(t, types.ErrTooManyActiveOrders, "TooManyActiveOrders()", "0x28d7a641", servedOrders, servedStopOrders)
}

func TestErrorNotAuthorized(t *testing.T) {
	requireNoArgs(t, types.ErrNotAuthorized, "NotAuthorized()", "0xea8e4eb5", servedOrders, servedStopOrders)
}

func TestErrorNotFeeCollector(t *testing.T) {
	requireNoArgs(t, types.ErrNotFeeCollector, "NotFeeCollector()", "0x73fcd3fe", servedOrders, servedStopOrders)
	feeCollector := types.DefaultParams().FeeCollector
	for _, rail := range []string{types.PECOROrdersAddress, types.PECORStopOrdersAddress} {
		requireNoArgs(t, spot.CheckFeeCollector(common.HexToAddress(rail), errorAccount, feeCollector),
			"NotFeeCollector()", "0x73fcd3fe", servedOrders, servedStopOrders)
	}
}

func TestErrorFeeExceedsMax(t *testing.T) {
	requireNoArgs(t, types.ErrFeeExceedsMax, "FeeExceedsMax()", "0x5ff85e3f", servedOrders, servedStopOrders)
	p := types.DefaultParams()
	p.OrderFeeBps = types.MaxOrderFeeBps + 1
	requireParamsRefusal(t, p, types.ErrParamsFeeExceedsMax, types.ErrFeeExceedsMax)
}

func TestErrorCannotCancel(t *testing.T) {
	requireNoArgs(t, types.ErrCannotCancel, "CannotCancel()", "0xfbc03578", servedOrders, servedStopOrders)
}

func TestErrorInvalidExpiry(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidExpiry, "InvalidExpiry()", "0xd36c8500", servedOrders, servedStopOrders)
}

func TestErrorInvalidPrice(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidPrice, "InvalidPrice()", "0x00bfc921", servedOrders, servedStopOrders)
}

func TestErrorInvalidPrices(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidPrices, "InvalidPrices()", "0x2084e5f6", servedStopOrders)
}

func TestErrorInvalidVault(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidVault, "InvalidVault()", "0xd03a6320", servedPECOR, servedOrders, servedStopOrders)
}

func TestErrorLimitMustBeAboveStopBuy(t *testing.T) {
	requireNoArgs(t, types.ErrLimitMustBeAboveStopBuy, "LimitMustBeAboveStopBuy()", "0x896a5f19", servedStopOrders)
}

func TestErrorLimitMustBeBelowStopSell(t *testing.T) {
	requireNoArgs(t, types.ErrLimitMustBeBelowStopSell, "LimitMustBeBelowStopSell()", "0xba470259", servedStopOrders)
}

func TestErrorNotActivated(t *testing.T) {
	requireNoArgs(t, types.ErrNotActivated, "NotActivated()", "0x037c597f", servedStopOrders)
}

func TestErrorNotOrderOwner(t *testing.T) {
	requireNoArgs(t, types.ErrNotOrderOwner, "NotOrderOwner()", "0xf6412b5a", servedOrders, servedStopOrders)
}

func TestErrorNotPending(t *testing.T) {
	requireNoArgs(t, types.ErrNotPending, "NotPending()", "0x7dc6505a", servedStopOrders)
}

func TestErrorOrderExpired(t *testing.T) {
	requireNoArgs(t, types.ErrOrderExpired, "OrderExpired()", "0xc56873ba", servedOrders, servedStopOrders)
}

func TestErrorOrderNotPending(t *testing.T) {
	requireNoArgs(t, types.ErrOrderNotPending, "OrderNotPending()", "0xba254946", servedOrders, servedStopOrders)
}

func TestErrorPriceAboveLimit(t *testing.T) {
	requireNoArgs(t, types.ErrPriceAboveLimit, "PriceAboveLimit()", "0x6d27d70f", servedStopOrders)
}

func TestErrorPriceAboveTarget(t *testing.T) {
	requireNoArgs(t, types.ErrPriceAboveTarget, "PriceAboveTarget()", "0xbcfcc626", servedOrders)
}

func TestErrorPriceAboveTrigger(t *testing.T) {
	requireNoArgs(t, types.ErrPriceAboveTrigger, "PriceAboveTrigger()", "0x7df2e7d4", servedStopOrders)
}

func TestErrorPriceBelowLimit(t *testing.T) {
	requireNoArgs(t, types.ErrPriceBelowLimit, "PriceBelowLimit()", "0xb4d66e0f", servedStopOrders)
}

func TestErrorPriceBelowTarget(t *testing.T) {
	requireNoArgs(t, types.ErrPriceBelowTarget, "PriceBelowTarget()", "0xb8673d0c", servedOrders)
}

func TestErrorTokenPaused(t *testing.T) {
	requireNoArgs(t, types.ErrTokenPaused, "TokenPaused()", "0xe7792495", servedPECOR, servedOrders, servedStopOrders, servedVault)
}

func TestErrorInvalidArrayLength(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidArrayLength, "InvalidArrayLength()", "0x9d89020a", servedVault)
}

func TestErrorWethOnly(t *testing.T) {
	requireNoArgs(t, types.ErrWethOnly, "WethOnly()", "0xcfab6517", servedVault)
}

func TestErrorTokenNotRegistered(t *testing.T) {
	requireNoArgs(t, types.ErrTokenNotRegistered, "TokenNotRegistered()", "0x259ba1ad", servedVault)
}

func TestErrorTokenAlreadyRegistered(t *testing.T) {
	requireNoArgs(t, types.ErrTokenAlreadyRegistered, "TokenAlreadyRegistered()", "0x7d4fffb2")
	token := types.TokenParams{Address: errorToken, Kind: types.TokenKindERC20, Decimals: 6, MaxUtilizationBps: 9000}
	p := types.DefaultParams()
	p.Tokens = []types.TokenParams{token, token}
	requireParamsRefusal(t, p, types.ErrParamsTokenAlreadyRegistered, types.ErrTokenAlreadyRegistered)
}

func TestErrorUnsupportedToken(t *testing.T) {
	requireNoArgs(t, types.ErrUnsupportedToken, "UnsupportedToken()", "0x6a172882")
	p := types.DefaultParams()
	p.Tokens = []types.TokenParams{{Address: errorToken, Kind: types.TokenKindERC20, Decimals: types.MaxTokenDecimals + 1, MaxUtilizationBps: 9000}}
	requireParamsRefusal(t, p, types.ErrParamsUnsupportedToken, types.ErrUnsupportedToken)
}

func TestErrorERC20InsufficientAllowance(t *testing.T) {
	requireRevert(t, types.ERC20InsufficientAllowance(errorAccount, uint256.NewInt(7), uint256.NewInt(9)),
		"ERC20InsufficientAllowance(address,uint256,uint256)", "0xfb8f41b2", []string{servedShare},
		[][]byte{word(errorAccount.Bytes()), word([]byte{7}), word([]byte{9})}, errorAccount, big.NewInt(7), big.NewInt(9))
}

func TestErrorERC20InsufficientBalance(t *testing.T) {
	requireRevert(t, types.ERC20InsufficientBalance(errorAccount, uint256.NewInt(1), uint256.NewInt(2)),
		"ERC20InsufficientBalance(address,uint256,uint256)", "0xe450d38c", []string{servedShare},
		[][]byte{word(errorAccount.Bytes()), word([]byte{1}), word([]byte{2})}, errorAccount, big.NewInt(1), big.NewInt(2))
}

func TestErrorERC20InvalidApprover(t *testing.T) {
	requireRevert(t, types.ERC20InvalidApprover(common.Address{}), "ERC20InvalidApprover(address)", "0xe602df05",
		[]string{servedShare}, [][]byte{word(nil)}, common.Address{})
}

func TestErrorERC20InvalidReceiver(t *testing.T) {
	requireRevert(t, types.ERC20InvalidReceiver(common.Address{}), "ERC20InvalidReceiver(address)", "0xec442f05",
		[]string{servedShare}, [][]byte{word(nil)}, common.Address{})
}

func TestErrorERC20InvalidSender(t *testing.T) {
	requireRevert(t, types.ERC20InvalidSender(common.Address{}), "ERC20InvalidSender(address)", "0x96c6fd1e",
		[]string{servedShare}, [][]byte{word(nil)}, common.Address{})
}

func TestErrorERC20InvalidSpender(t *testing.T) {
	requireRevert(t, types.ERC20InvalidSpender(common.Address{}), "ERC20InvalidSpender(address)", "0x94280d62",
		[]string{servedShare}, [][]byte{word(nil)}, common.Address{})
}

func TestErrorTwapWindowInvalid(t *testing.T) {
	requireNoArgs(t, types.ErrTwapWindowInvalid, "TwapWindowInvalid()", "0xb307c2d2", servedQuoter)
}

func TestErrorInvalidTWAPPeriod(t *testing.T) {
	requireNoArgs(t, types.ErrInvalidTWAPPeriod, "InvalidTWAPPeriod()", "0x8bd931d1", servedOrders, servedStopOrders)
	p := types.DefaultParams()
	p.TwapSanityPeriod = p.TwapMaxPeriod + 1
	requireParamsRefusal(t, p, types.ErrParamsInvalidTWAPPeriod, types.ErrInvalidTWAPPeriod)
}

func TestServedAbiCarriesAddedErrors(t *testing.T) {
	added := map[string][]string{
		servedPECOR: {"ZeroAmount()", "SameToken()", "Expired()", "InsufficientOutput()", "ExcessiveInput()",
			"InsufficientLiquidity()", "NotAStablecoin()", "TokenIsStablecoin()", "UseWethDeposit()", "UseWethWithdraw()",
			"NoFeesToCollect()", "NativeTransferFailed()", "MulticallFailed(uint256)", "UnsupportedInputTransfer()",
			"StalePrice()", "TokenPaused()", "InvalidVault()", "UnsupportedTransfer()"},
		servedOrders: {"InsufficientOutput()", "StalePrice()", "UnsupportedTransfer()", "UnsupportedInputTransfer()",
			"SafeERC20FailedOperation(address)", "OrderBelowMinimumNotional()", "TooManyActiveOrders()"},
		servedStopOrders: {"InsufficientOutput()", "StalePrice()", "UnsupportedTransfer()", "UnsupportedInputTransfer()",
			"SafeERC20FailedOperation(address)", "OrderBelowMinimumNotional()", "TooManyActiveOrders()"},
		servedQuoter: {"TwapWindowInvalid()", "StalePrice()", "OwnableUnauthorizedAccount(address)"},
		servedVault: {"ZeroAmount()", "InvalidArrayLength()", "WethOnly()", "TokenNotRegistered()", "TokenPaused()",
			"InsufficientLiquidity()", "UnsupportedTransfer()", "UnsupportedInputTransfer()"},
	}
	for file, signatures := range added {
		served := map[string]string{}
		for _, entry := range servedABI(t, file).Errors {
			served[entry.Sig] = hexutil.Encode(entry.ID[:4])
		}
		for _, signature := range signatures {
			selector, ok := served[signature]
			require.True(t, ok, "%s missing from %s", signature, file)
			require.Equal(t, hexutil.Encode(crypto.Keccak256([]byte(signature))[:4]), selector, "%s in %s", signature, file)
		}
	}

	orders := servedABI(t, servedOrders).Errors
	require.Equal(t, "0x9b2bea5b", hexutil.Encode(orders["OrderBelowMinimumNotional"].ID.Bytes()[:4]))
	require.Equal(t, "0x28d7a641", hexutil.Encode(orders["TooManyActiveOrders"].ID.Bytes()[:4]))

	// Params.Validate and InitV6120 errors are module errors; no address
	// carries them.
	for _, name := range []string{"FeeTooHigh", "Tier2BelowTier1", "ScalarTooHigh", "TokenAlreadyRegistered", "UnsupportedToken", "ZeroAddress"} {
		for _, file := range servedFiles {
			_, ok := servedABI(t, file).Errors[name]
			require.False(t, ok, "%s in %s", name, file)
		}
	}
}

func TestCustomErrorRevertData(t *testing.T) {
	transfer := types.SafeERC20FailedOperation(errorToken)
	wantTransfer := append(hexutil.MustDecode("0x5274afe7"), word(errorToken.Bytes())...)
	for _, err := range []error{
		transfer,
		sdkerrors.Wrap(transfer, "marketBuy"),
		fmt.Errorf("marketBuy: %w", transfer),
	} {
		require.Equal(t, wantTransfer, spot.PackError(err), err.Error())
	}

	slippage := hexutil.MustDecode("0xbb2875c3")
	for _, err := range []error{
		types.ErrInsufficientOutput,
		sdkerrors.Wrap(types.ErrInsufficientOutput, "marketBuy"),
		fmt.Errorf("marketBuy: %w", types.ErrInsufficientOutput),
	} {
		require.Equal(t, slippage, spot.PackError(err), err.Error())
	}

	plain := errors.New("spot: corrupt params")
	require.Equal(t, pcommon.RevertReason(plain), spot.PackError(plain))
	require.Equal(t, hexutil.MustDecode("0x08c379a0"), spot.PackError(plain)[:4])
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	decoded, err := abi.Arguments{{Type: stringType}}.Unpack(spot.PackError(plain)[4:])
	require.NoError(t, err)
	require.Equal(t, []interface{}{"spot: corrupt params"}, decoded)
}

func TestNotFeeCollector(t *testing.T) {
	feeCollector := types.DefaultParams().FeeCollector
	notFeeCollector := hexutil.MustDecode("0x73fcd3fe")
	for _, rail := range []string{types.PECOROrdersAddress, types.PECORStopOrdersAddress} {
		address := common.HexToAddress(rail)
		require.Equal(t, notFeeCollector, spot.PackError(spot.CheckFeeCollector(address, errorAccount, feeCollector)), rail)
		require.NoError(t, spot.CheckFeeCollector(address, feeCollector, feeCollector), rail)
	}

	pecor := common.HexToAddress(types.PECORAddress)
	want := append(hexutil.MustDecode("0x118cdaa7"), word(errorAccount.Bytes())...)
	require.Equal(t, want, spot.PackError(spot.CheckFeeCollector(pecor, errorAccount, feeCollector)))
	require.NoError(t, spot.CheckFeeCollector(pecor, feeCollector, feeCollector))

	for _, rail := range []string{types.PECORQuoterAddress, types.PECORVaultAddress} {
		err := spot.CheckFeeCollector(common.HexToAddress(rail), feeCollector, feeCollector)
		require.EqualError(t, err, "spot: collectFees is not served at "+common.HexToAddress(rail).Hex())
		require.Equal(t, pcommon.RevertReason(err), spot.PackError(err))
	}
}

// spotEnv is a spot keeper over the shared EVM test app with two NATIVE
// tokens: a stablecoin and PAX priced by its pricefeed market.
type spotEnv struct {
	t             *testing.T
	ctx           sdk.Context
	k             *keeper.Keeper
	user, other   common.Address
	stable, pax   common.Address
	stableDenom   string
	paxDenom      string
	priceStoreKey sdk.StoreKey
}

func e18(numerator, denominator int64) sdk.Int {
	return sdk.NewIntWithDecimal(numerator, 18).QuoRaw(denominator)
}

func newSpotEnv(t *testing.T) *spotEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockTime(time.Unix(1000, 0).UTC()).WithBlockHeight(10)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, priceKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtest.WithPriceFeedStore(pricefeedtest.WithPriceFeedStore(ctx, priceKey), key)
	feed := pricefeedkeeper.NewKeeper(priceKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	e := &spotEnv{
		t: t, ctx: ctx, k: k, priceStoreKey: priceKey,
		user:        common.BytesToAddress(crypto.Keccak256([]byte("spot error user"))),
		other:       common.BytesToAddress(crypto.Keccak256([]byte("spot error other"))),
		stable:      common.BytesToAddress(crypto.Keccak256([]byte("spot error stable pointer"))),
		pax:         common.BytesToAddress(crypto.Keccak256([]byte("spot error pax pointer"))),
		stableDenom: "uspoterrusd",
		paxDenom:    "uhpx",
	}
	for _, token := range []struct {
		denom, display string
		pointer        common.Address
	}{{e.stableDenom, "SPOTERRUSD", e.stable}, {e.paxDenom, "PAX", e.pax}} {
		app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: token.denom, Display: token.display,
			DenomUnits: []*banktypes.DenomUnit{{Denom: token.denom, Exponent: 0}, {Denom: token.display, Exponent: 6}}})
		require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, token.denom, token.pointer))
	}
	coins := sdk.NewCoins(sdk.NewInt64Coin(e.stableDenom, 1_000_000_000), sdk.NewInt64Coin(e.paxDenom, 1_000_000_000))
	for _, holder := range []common.Address{e.user, e.other} {
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, app.EvmKeeper.GetPaxAddressOrDefault(ctx, holder), coins))
	}
	params := types.DefaultParams()
	params.Tokens = []types.TokenParams{
		{Address: e.stable, Kind: types.TokenKindNative, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: e.pax, Kind: types.TokenKindNative, MarketID: types.PAXMarketID, Decimals: 6, MaxUtilizationBps: 9000},
	}
	e.setParams(params)
	e.price(e18(1, 2))
	return e
}

func (e *spotEnv) setParams(params types.Params) {
	e.t.Helper()
	require.NoError(e.t, e.k.UpdateParams(e.ctx, e.k.Authority(), params))
}

func (e *spotEnv) price(price sdk.Int) {
	e.t.Helper()
	raw, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.priceStoreKey).Set(pricefeedtypes.AggregateKey(types.PAXMarketID), raw)
}

// balances is every bank balance of the user and of spot custody.
func (e *spotEnv) balances() map[string]string {
	e.t.Helper()
	app := testkeeper.EVMTestApp
	return map[string]string{
		"user":    app.BankKeeper.GetAllBalances(e.ctx, app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, e.user)).String(),
		"custody": app.BankKeeper.GetAllBalances(e.ctx, keeper.CustodyAddress()).String(),
	}
}

func (e *spotEnv) place(kind types.OrderType, amount int64, target sdk.Int) uint64 {
	e.t.Helper()
	id, _, err := e.k.PlaceLimit(e.ctx, e.user, e.stable, e.pax, sdk.NewInt(amount), target, 0, kind)
	require.NoError(e.t, err)
	return id
}

func requireExact(t *testing.T, err error, selector string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, selector, hexutil.Encode(spot.PackError(err)))
}

func TestEngineErrorMapping(t *testing.T) {
	e := newSpotEnv(t)

	// PriceNotMet on a limit sell is PriceBelowTarget().
	sell := e.place(types.OrderTypeLimitSell, 100_000_000, e18(55, 100))
	_, err := e.k.ExecuteLimit(e.ctx, sell)
	requireExact(t, err, "0xb8673d0c")

	// PriceNotMet on a limit buy is PriceAboveTarget().
	buy := e.place(types.OrderTypeLimitBuy, 100_000_000, e18(45, 100))
	_, err = e.k.ExecuteLimit(e.ctx, buy)
	requireExact(t, err, "0xbcfcc626")

	// Cancel by another address is NotOrderOwner().
	_, err = e.k.CancelLimit(e.ctx, e.other, sell)
	requireExact(t, err, "0xf6412b5a")

	// OrderNotPending on a limit cancel is CannotCancel(); a fill of the
	// cancelled order stays OrderNotPending().
	_, err = e.k.CancelLimit(e.ctx, e.user, sell)
	require.NoError(t, err)
	_, err = e.k.CancelLimit(e.ctx, e.user, sell)
	requireExact(t, err, "0xfbc03578")
	_, err = e.k.ExecuteLimit(e.ctx, sell)
	requireExact(t, err, "0xba254946")
}

func TestValidationBeforeTransfer(t *testing.T) {
	e := newSpotEnv(t)
	cases := []struct {
		name               string
		stablecoin, token  common.Address
		amount             int64
		target             sdk.Int
		expiry             uint64
		kind               types.OrderType
		selector           string
		maxActiveOrders    uint64
		placeBeforeRefusal bool
	}{
		{name: "non-stable named as the stablecoin", stablecoin: e.pax, token: e.stable, amount: 100_000_000, target: e18(1, 2),
			kind: types.OrderTypeLimitBuy, selector: "0x777a9be8"},
		{name: "stablecoin named as the token", stablecoin: e.stable, token: e.stable, amount: 100_000_000, target: e18(1, 2),
			kind: types.OrderTypeLimitBuy, selector: "0xc1343a37"},
		{name: "zero amount", stablecoin: e.stable, token: e.pax, amount: 0, target: e18(1, 2),
			kind: types.OrderTypeLimitSell, selector: "0x1f2a2005"},
		{name: "past expiry", stablecoin: e.stable, token: e.pax, amount: 100_000_000, target: e18(1, 2), expiry: 999,
			kind: types.OrderTypeLimitSell, selector: "0xd36c8500"},
		{name: "escrow below minOrderNotional", stablecoin: e.stable, token: e.pax, amount: 1_000_000, target: e18(1, 2),
			kind: types.OrderTypeLimitSell, selector: "0x9b2bea5b"},
		{name: "user at maxActiveOrdersPerUser", stablecoin: e.stable, token: e.pax, amount: 100_000_000, target: e18(1, 2),
			kind: types.OrderTypeLimitSell, selector: "0x28d7a641", maxActiveOrders: 1, placeBeforeRefusal: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.maxActiveOrders != 0 {
				params, err := e.k.GetParams(e.ctx)
				require.NoError(t, err)
				params.MaxActiveOrdersPerUser = tc.maxActiveOrders
				e.setParams(params)
			}
			if tc.placeBeforeRefusal {
				e.place(tc.kind, tc.amount, tc.target)
			}
			before := e.balances()
			_, _, err := e.k.PlaceLimit(e.ctx, e.user, tc.stablecoin, tc.token, sdk.NewInt(tc.amount), tc.target, tc.expiry, tc.kind)
			requireExact(t, err, tc.selector)
			require.Equal(t, before, e.balances())
		})
	}
}
