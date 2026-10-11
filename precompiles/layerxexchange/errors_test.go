package layerxexchange_test

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	pattern, err := regexp.Compile(`(?m)^\s*X\(\s*(LXP_[A-Z0-9_]+)\s*,\s*(-?\d+)\s*\)`)
	require.NoError(t, err)
	declarations := pattern.FindAllSubmatch(header, -1)
	require.NotEmpty(t, declarations)
	values := make(map[lxphost.Code]struct{}, len(declarations))
	names := make(map[string]struct{}, len(declarations))
	mappedNames := make(map[string]lxphost.Code, len(declarations))
	lowest, highest := int64(math.MaxInt32), int64(math.MinInt32)
	for _, declaration := range declarations {
		numeric, err := strconv.ParseInt(string(declaration[2]), 10, 32)
		require.NoError(t, err)
		code := lxphost.Code(numeric)
		name := strings.Replace(string(declaration[1]), "LXP_ERR_", "LXP_", 1)
		require.NotContains(t, values, code, "duplicate numeric code %d", numeric)
		require.NotContains(t, names, name, "duplicate kernel name %q", name)
		values[code] = struct{}{}
		names[name] = struct{}{}
		if numeric < lowest {
			lowest = numeric
		}
		if numeric > highest {
			highest = numeric
		}
		actual := layerxexchange.KernelCodeText(code)
		previous, duplicate := mappedNames[actual]
		mappedNames[actual] = code
		t.Run(name, func(t *testing.T) {
			require.Equal(t, name, actual, "numeric code %d", numeric)
			require.True(t, strings.HasPrefix(actual, "LXP_"))
			require.False(t, duplicate, "codes %d and %d map to %q", previous, code, actual)
			if code != 0 {
				assertLxpRevert(t, pcommon.RevertReason(layerxexchange.RevertError(code)), name)
			}
		})
	}
	require.Greater(t, lowest, int64(math.MinInt32))
	require.Less(t, highest, int64(math.MaxInt32))
	for _, numeric := range []int64{lowest - 1, highest + 1} {
		t.Run(fmt.Sprintf("unknown_%d", numeric), func(t *testing.T) {
			code := lxphost.Code(numeric)
			require.NotContains(t, values, code)
			require.Equal(t, "LXP_UNKNOWN", layerxexchange.KernelCodeText(code))
			assertLxpRevert(t, pcommon.RevertReason(layerxexchange.RevertError(code)), "LXP_UNKNOWN")
		})
	}
}
