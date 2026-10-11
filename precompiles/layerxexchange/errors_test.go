package layerxexchange_test

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/lxphost"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxexchange"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

var kernelCodeCases = []struct {
	code lxphost.Code
	text string
}{
	{0, "LXP_OK"},
	{-1, "LXP_TRUNCATED"},
	{-2, "LXP_TRAILING_BYTES"},
	{-3, "LXP_NON_CANONICAL"},
	{-4, "LXP_UNSORTED_SEQUENCE"},
	{-5, "LXP_LENGTH_LIMIT"},
	{-6, "LXP_INVALID_TAG"},
	{-7, "LXP_UNKNOWN_FIELD"},
	{-8, "LXP_DUPLICATE_ENTRY"},
	{-100, "LXP_WRONG_NETWORK"},
	{-101, "LXP_VERSION_UNSUPPORTED"},
	{-102, "LXP_UNKNOWN_MODULE"},
	{-103, "LXP_MODULE_DISABLED"},
	{-104, "LXP_PAYLOAD_HASH_MISMATCH"},
	{-105, "LXP_MALFORMED_ENVELOPE"},
	{-106, "LXP_UNKNOWN_ACTIVITY"},
	{-107, "LXP_MALFORMED_SEND"},
	{-108, "LXP_MALFORMED_RECEIVE"},
	{-200, "LXP_UNKNOWN_DID"},
	{-201, "LXP_BAD_SIGNATURE"},
	{-202, "LXP_AUTH_EXPIRED"},
	{-203, "LXP_AUTH_REVOKED"},
	{-204, "LXP_AUTH_SCOPE"},
	{-205, "LXP_AUTH_ALLOWANCE"},
	{-206, "LXP_IDENTITY_FROZEN"},
	{-207, "LXP_UNAUTHORIZED_DEBIT"},
	{-208, "LXP_UNKNOWN_ACCOUNT_NAMESPACE"},
	{-209, "LXP_MALFORMED_GRANT"},
	{-210, "LXP_UNKNOWN_AUTHORITY_KIND"},
	{-211, "LXP_GRANT_EXHAUSTED"},
	{-212, "LXP_STALE_REVOCATION"},
	{-213, "LXP_CONTEXT_MISMATCH"},
	{-214, "LXP_NO_PAYER_GRANT"},
	{-215, "LXP_GRANT_SCOPE_VIOLATION"},
	{-216, "LXP_PURPOSE_MISMATCH"},
	{-217, "LXP_INVOICE_ALREADY_SETTLED"},
	{-218, "LXP_GRANT_EXPIRED"},
	{-219, "LXP_GRANT_REVOKED"},
	{-220, "LXP_AUTH_THRESHOLD_UNMET"},
	{-221, "LXP_AUTH_DUPLICATE_SIGNER"},
	{-222, "LXP_AUTH_NOT_MATURE"},
	{-300, "LXP_SEQUENCE_GAP"},
	{-301, "LXP_SEQUENCE_REUSED"},
	{-302, "LXP_IDEMPOTENT_REPLAY"},
	{-303, "LXP_EXPIRED"},
	{-304, "LXP_NOT_YET_VALID"},
	{-305, "LXP_SEQUENCE_MISMATCH"},
	{-306, "LXP_CONDITION_UNMET"},
	{-307, "LXP_SEQUENCE_EXHAUSTED"},
	{-400, "LXP_INSUFFICIENT_BALANCE"},
	{-401, "LXP_ZERO_AMOUNT"},
	{-402, "LXP_ASSET_MISMATCH"},
	{-403, "LXP_ASSET_PAUSED"},
	{-404, "LXP_ACCOUNT_FROZEN"},
	{-405, "LXP_CONSERVATION"},
	{-406, "LXP_TOO_MANY_LEGS"},
	{-407, "LXP_ACCOUNT_NOT_EMPTY"},
	{-408, "LXP_BALANCE_BYPASS"},
	{-409, "LXP_ACCOUNT_ID_MISMATCH"},
	{-410, "LXP_CLIENT_SUPPLIED_BALANCE"},
	{-411, "LXP_ASSET_ALREADY_REGISTERED"},
	{-412, "LXP_INVALID_AMOUNT"},
	{-413, "LXP_DEPOSIT_PROOF_NOT_FINAL"},
	{-414, "LXP_DEPOSIT_ALREADY_CREDITED"},
	{-415, "LXP_WITHDRAWAL_ALREADY_SETTLED"},
	{-416, "LXP_CHALLENGE_WINDOW_OPEN"},
	{-417, "LXP_WITHDRAWAL_CANCELLED"},
	{-418, "LXP_WITHDRAWAL_ASSET_MISMATCH"},
	{-500, "LXP_OVERFLOW"},
	{-501, "LXP_UNDERFLOW"},
	{-502, "LXP_DIV_ZERO"},
	{-503, "LXP_PRECISION"},
	{-600, "LXP_FEE_LIMIT"},
	{-601, "LXP_GAS_EXHAUSTED"},
	{-602, "LXP_FEE_UNPAYABLE"},
	{-700, "LXP_ESCROW_STATE"},
	{-701, "LXP_BUDGET_PERIOD_CAP"},
	{-702, "LXP_STREAM_UNDERFUNDED"},
	{-703, "LXP_MARKET_HALTED"},
	{-704, "LXP_ORACLE_STALE"},
	{-705, "LXP_MARGIN_INSUFFICIENT"},
	{-706, "LXP_AGREEMENT_STATE"},
	{-707, "LXP_CAPTURE_EXCEEDS_HOLD"},
	{-708, "LXP_UNAUTHORIZED_CAPTURE"},
	{-709, "LXP_HOLD_EXPIRED"},
	{-710, "LXP_UNAUTHORIZED_ESCROW_SPEND"},
	{-711, "LXP_DISPUTE_WINDOW_CLOSED"},
	{-712, "LXP_HOLD_DISPUTED"},
	{-713, "LXP_BUDGET_ALLOWANCE_EXCEEDED"},
	{-714, "LXP_INSUFFICIENT_BUDGET_FUNDS"},
	{-715, "LXP_UNAUTHORIZED_DELEGATE"},
	{-716, "LXP_BUDGET_REVOKED"},
	{-717, "LXP_NON_MONOTONIC_TIME"},
	{-718, "LXP_ACCRUAL_OVERFLOW"},
	{-719, "LXP_UNAUTHORIZED_METER"},
	{-720, "LXP_METER_REGRESSION"},
	{-721, "LXP_STREAM_CLOSED"},
	{-722, "LXP_MODULE_MAY_NOT_WRITE_BALANCE"},
	{-723, "LXP_OFFER_UNAVAILABLE"},
	{-724, "LXP_TERMS_MISMATCH"},
	{-725, "LXP_INVALID_ATTESTATION"},
	{-726, "LXP_DELIVERABLE_MISMATCH"},
	{-727, "LXP_DELIVERY_DEADLINE_PASSED"},
	{-728, "LXP_UNAUTHORIZED_DISPUTANT"},
	{-729, "LXP_UNAUTHORIZED_ORACLE"},
	{-730, "LXP_ORACLE_SEQUENCE"},
	{-731, "LXP_ORACLE_BOUNDS"},
	{-732, "LXP_ORACLE_DEVIATION"},
	{-733, "LXP_MARKET_ALREADY_EXISTS"},
	{-734, "LXP_PARAMETER_BOUNDS"},
	{-735, "LXP_PAUSED_SCOPE"},
	{-736, "LXP_PROGRAM_REFUSED"},
	{-800, "LXP_BATCH_GAP"},
	{-801, "LXP_ROOT_MISMATCH"},
	{-802, "LXP_TIMESTAMP_REGRESSION"},
	{-803, "LXP_ATTESTATION_THRESHOLD"},
	{-804, "LXP_DA_MISSING"},
	{-805, "LXP_EQUIVOCATION"},
	{-900, "LXP_LOG_CORRUPT"},
	{-901, "LXP_LOG_TRUNCATED"},
	{-902, "LXP_SNAPSHOT_MISMATCH"},
	{-903, "LXP_PROJECTION_STALE"},
	{-904, "LXP_IO"},
	{-905, "LXP_ARENA_EXHAUSTED"},
	{-906, "LXP_SNAPSHOT_BLOBS_MISSING"},
	{-1001, "LXP_FATAL_INVARIANT"},
	{-1002, "LXP_FATAL_REPLAY_DIVERGENCE"},
	{-1003, "LXP_FATAL_SUPPLY_MISMATCH"},
}

func assertLxpRevert(t *testing.T, data []byte, text string) {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	decoded, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	require.Equal(t, text, decoded)
	require.Equal(t, pcommon.RevertReason(errors.New(text)), data)
}

func TestRevertReasonIsLxpCode(t *testing.T) {
	t.Run("kernelMarginRefusalEncoding", func(t *testing.T) {
		original := lxphost.Code(-705)
		for _, err := range []error{original, fmt.Errorf("order refused: %w", original), sdkerrors.Wrap(original, "order refused")} {
			converted := layerxexchange.RevertError(err)
			require.Equal(t, "LXP_MARGIN_INSUFFICIENT", converted.Error())
			require.ErrorIs(t, converted, original)
			var result lxphost.Code
			require.ErrorAs(t, converted, &result)
			require.Equal(t, original, result)
			assertLxpRevert(t, pcommon.RevertReason(converted), "LXP_MARGIN_INSUFFICIENT")
		}
	})

	t.Run("exchangeRegistry", func(t *testing.T) {
		cases := []struct {
			err     *sdkerrors.Error
			number  uint32
			message string
			text    string
		}{
			{types.ErrInvalidParams, 2, "invalid params", "LXP_EXCHANGE_INVALID_PARAMS"},
			{types.ErrInvalidMarket, 3, "invalid market", "LXP_EXCHANGE_INVALID_MARKET"},
			{types.ErrUnknownMarket, 4, "market is not listed or is disabled", "LXP_EXCHANGE_UNKNOWN_MARKET"},
			{types.ErrNotMarginAsset, 5, "asset is not the margin asset of an enabled market", "LXP_EXCHANGE_NOT_MARGIN_ASSET"},
			{types.ErrInvalidIntent, 6, "invalid exchange intent", "LXP_EXCHANGE_INVALID_INTENT"},
			{types.ErrIntentExists, 7, "intent already recorded", "LXP_EXCHANGE_INTENT_EXISTS"},
			{types.ErrNotFinalized, 8, "batch is not a finalized checkpoint", "LXP_EXCHANGE_NOT_FINALIZED"},
			{types.ErrInvalidProof, 9, "LayerX state proof refused", "LXP_EXCHANGE_INVALID_PROOF"},
			{types.ErrStateMismatch, 10, "state proof does not prove the requested entry", "LXP_EXCHANGE_STATE_MISMATCH"},
			{types.ErrInvalidGenesis, 11, "invalid genesis", "LXP_EXCHANGE_INVALID_GENESIS"},
			{types.ErrKeeperMissing, 12, "layerxexchange keeper is not wired", "LXP_EXCHANGE_KEEPER_MISSING"},
			{types.ErrEvidenceTooLong, 13, "state proof exceeds the exchange bound", "LXP_EXCHANGE_EVIDENCE_TOO_LONG"},
		}
		for _, test := range cases {
			t.Run(test.text, func(t *testing.T) {
				require.Equal(t, test.number, test.err.ABCICode())
				require.Equal(t, types.ModuleName, test.err.Codespace())
				require.Equal(t, test.message, test.err.Error())
				for _, err := range []error{test.err, fmt.Errorf("detail: %w", test.err), sdkerrors.Wrap(test.err, "detail")} {
					require.Equal(t, test.text, types.ErrorCode(err))
					converted := layerxexchange.RevertError(err)
					require.ErrorIs(t, converted, test.err)
					var registered *sdkerrors.Error
					require.ErrorAs(t, converted, &registered)
					require.Same(t, test.err, registered)
					assertLxpRevert(t, pcommon.RevertReason(converted), test.text)
				}
			})
		}
		require.Empty(t, types.ErrorCode(nil))
		unknown := errors.New("unregistered failure")
		require.Empty(t, types.ErrorCode(unknown))
		require.Same(t, unknown, layerxexchange.RevertError(unknown))
		require.Nil(t, layerxexchange.RevertError(nil))
	})

	t.Run("placeOrderUnknownMarket", func(t *testing.T) {
		h := newHarness(t)
		data, err := h.precompile.Run(h.evm, h.caller, h.caller,
			h.input(layerxexchange.PlaceOrderMethod, [32]byte{0x99}, types.SideBuy,
				big.NewInt(1), big.NewInt(1), uint8(0)), nil, false, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		assertLxpRevert(t, data, "LXP_EXCHANGE_UNKNOWN_MARKET")
		require.Zero(t, h.keeper.GetIntentCount(h.ctx))
		require.Zero(t, h.keeper.GetOwnerNonce(h.ctx, h.caller))
	})

	t.Run("placeOrderMarginRefusal", func(t *testing.T) {
		h := newHarness(t)
		require.Zero(t, h.keeper.GetIntentCount(h.ctx))
		data, err := h.precompile.Run(h.evm, h.caller, h.caller,
			h.input(layerxexchange.PlaceOrderMethod, marketID, types.SideBuy,
				big.NewInt(1), big.NewInt(1), uint8(0)), nil, false, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		assertLxpRevert(t, data, "LXP_MARGIN_INSUFFICIENT")
		require.Zero(t, h.keeper.GetIntentCount(h.ctx))
		require.Zero(t, h.keeper.GetOwnerNonce(h.ctx, h.caller))
	})
}

func TestKernelCodeTable(t *testing.T) {
	header, err := os.ReadFile("../../include/layerx/lxp_result.h")
	require.NoError(t, err)
	pattern, err := regexp.Compile(`X\((LXP_[A-Z_]+),\s*(-?\d+)\)`)
	require.NoError(t, err)
	declarations := pattern.FindAllSubmatch(header, -1)
	require.Len(t, declarations, len(kernelCodeCases))
	expected := make(map[lxphost.Code]string, len(kernelCodeCases))
	seen := make(map[string]lxphost.Code, len(kernelCodeCases))
	for _, test := range kernelCodeCases {
		_, duplicate := expected[test.code]
		require.False(t, duplicate, "duplicate numeric code %d", test.code)
		expected[test.code] = test.text
		actual := layerxexchange.KernelCodeText(test.code)
		require.Equal(t, test.text, actual, "numeric code %d", test.code)
		previous, duplicate := seen[actual]
		require.False(t, duplicate, "codes %d and %d map to %q", previous, test.code, actual)
		seen[actual] = test.code
		if test.code < 0 && test.code > -1000 {
			assertLxpRevert(t, pcommon.RevertReason(layerxexchange.RevertError(test.code)), test.text)
		}
	}
	for _, declaration := range declarations {
		numeric, err := strconv.ParseInt(string(declaration[2]), 10, 32)
		require.NoError(t, err)
		_, present := expected[lxphost.Code(numeric)]
		require.True(t, present, "canonical code %s (%d) has no expected mapping", declaration[1], numeric)
	}
	for _, unknown := range []lxphost.Code{1, -9, -737, -1004, -2147483648, 2147483647} {
		require.Equal(t, "LXP_UNKNOWN", layerxexchange.KernelCodeText(unknown))
	}
}
