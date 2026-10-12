package layerxpay

import (
	"math"
	"math/big"
	"testing"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestGasTable(t *testing.T) {
	require.NoError(t, gasABIError)
	parsed := pcommon.MustGetABI(abiTestFS, "abi.json")
	rows := []struct {
		flat  uint64
		write bool
		names []string
	}{
		{60000, true, []string{"escrowOpen", "escrowCapture", "escrowPartialCapture", "escrowRelease", "escrowTimeout"}},
		{30000, true, []string{"escrowDisputeOpen"}},
		{80000, true, []string{"escrowDisputeResolve"}},
		{50000, true, []string{"budgetCreate", "budgetCreateNative"}},
		{40000, true, []string{"budgetFund", "budgetFundNative"}},
		{30000, true, []string{"budgetAmend", "budgetDelegateAdd", "budgetDelegateRemove"}},
		{50000, true, []string{"budgetSpend", "budgetClose", "budgetDefund", "budgetRevoke"}},
		{58000, true, []string{"budgetPull"}},
		{60000, true, []string{"budgetDelegateSpend"}},
		{50000, true, []string{"streamOpen", "streamTopUp"}},
		{38000, true, []string{"streamMeter"}},
		{60000, true, []string{"streamSettle"}},
		{30000, true, []string{"streamPause", "streamResume"}},
		{80000, true, []string{"streamClose"}},
		{30000, true, []string{"serviceOfferPublish", "serviceOfferWithdraw", "serviceAgreementPropose", "serviceProgressReport", "serviceAccept", "serviceReject"}},
		{40000, true, []string{"serviceAgreementAccept", "serviceCommitTask", "serviceCommitAbandon"}},
		{38000, true, []string{"serviceToolExecAttest"}},
		{50000, true, []string{"serviceDeliver", "serviceDisputeOpen"}},
		{100000, true, []string{"serviceDisputeResolve"}},
		{1000, false, []string{"getEscrow", "escrowRemaining", "getEscrowResult", "getBudget", "budgetRemaining", "getStream", "streamAccrued", "getStreamResult", "getOffer", "getAgreement", "getCommitment", "getExecution", "getDelivery", "latestDelivery", "getProgress", "progressHighWater", "getDispute"}},
	}

	t.Run("kv_configuration", func(t *testing.T) {
		config := storetypes.KVGasConfig()
		require.Equal(t, uint64(1000), config.ReadCostFlat)
		require.Equal(t, uint64(3), config.ReadCostPerByte)
		require.Equal(t, uint64(2000), config.WriteCostFlat)
		require.Equal(t, uint64(30), config.WriteCostPerByte)
	})

	seen := make(map[string]bool)
	for _, expected := range rows {
		for _, name := range expected.names {
			require.False(t, seen[name], name)
			seen[name] = true
			t.Run(name, func(t *testing.T) {
				method, ok := parsed.Methods[name]
				require.True(t, ok)
				row, ok := methodGas[name]
				require.True(t, ok)
				require.Equal(t, expected.write, row.Write)
				require.Equal(t, !expected.write, method.IsConstant())
				require.Equal(t, expected.flat, row.Gas(nil))
				perByte := uint64(3)
				if expected.write {
					perByte = 30
				}
				args := make([]interface{}, len(method.Inputs))
				for i, argument := range method.Inputs {
					args[i] = zeroArgument(argument.Type)
				}
				input, err := parsed.Pack(name, args...)
				require.NoError(t, err)
				require.Equal(t, expected.flat+perByte*uint64(len(input)), RequiredGas(input))
				for _, length := range []int{4, 36, 132, 356, 4096} {
					calldata := make([]byte, length)
					copy(calldata, method.ID)
					require.Equal(t, expected.flat+perByte*uint64(length), RequiredGas(calldata))
				}
				input = append(input, 0)
				require.Equal(t, expected.flat+perByte*uint64(len(input)), RequiredGas(input))
			})
		}
	}
	require.Len(t, seen, 57)
	require.Len(t, parsed.Methods, len(seen))
	require.Len(t, methodGas, len(seen))
	for name := range parsed.Methods {
		require.True(t, seen[name], name)
		_, ok := methodGas[name]
		require.True(t, ok, name)
	}

	t.Run("acceptance_values", func(t *testing.T) {
		escrow, err := parsed.Pack("escrowOpen", [32]byte{1}, [32]byte{2}, [32]byte{3}, [32]byte{4}, [32]byte{5}, [32]byte{6}, big.NewInt(7), uint64(8), uint64(9), [32]byte{10}, [32]byte{11})
		require.NoError(t, err)
		require.Len(t, escrow, 356)
		require.Equal(t, uint64(70680), RequiredGas(escrow))
		dispute, err := parsed.Pack("serviceDisputeResolve", [32]byte{1}, uint16(2), uint32(4000), [32]byte{3})
		require.NoError(t, err)
		require.Len(t, dispute, 132)
		require.Equal(t, uint64(103960), RequiredGas(dispute))
		view, err := parsed.Pack("getEscrow", [32]byte{1})
		require.NoError(t, err)
		require.Len(t, view, 36)
		require.Equal(t, uint64(1108), RequiredGas(view))
	})

	t.Run("input_contents_do_not_change_price", func(t *testing.T) {
		for _, name := range []string{"streamOpen", "streamMeter", "budgetPull", "serviceToolExecAttest", "serviceDisputeResolve"} {
			method := parsed.Methods[name]
			input := make([]byte, 484)
			copy(input, method.ID)
			want := rowFlat(t, name) + 30*484
			require.Equal(t, want, RequiredGas(input), name)
			for i := 4; i < len(input); i++ {
				input[i] = 0xff
			}
			require.Equal(t, want, RequiredGas(input), name)
		}
	})

	t.Run("unknown_method", func(t *testing.T) {
		unknown := []byte{0xde, 0xad, 0xbe, 0xef}
		_, err := parsed.MethodById(unknown)
		require.Error(t, err)
		draw := crypto.Keccak256([]byte(drawSignature))[:4]
		_, err = parsed.MethodById(draw)
		require.Error(t, err)
		for _, input := range [][]byte{nil, {}, {0xde}, {0xde, 0xad}, {0xde, 0xad, 0xbe}, unknown, draw, append(unknown, make([]byte, 128)...)} {
			require.Equal(t, uint64(100000)+30*uint64(len(input)), RequiredGas(input))
		}
	})

	t.Run("overflow", func(t *testing.T) {
		for _, name := range []string{"getEscrow", "escrowOpen", "serviceDisputeResolve"} {
			row := methodGas[name]
			perByte := uint64(3)
			if row.Write {
				perByte = 30
			}
			flat := rowFlat(t, name)
			limit := (math.MaxUint64 - flat) / perByte
			require.Equal(t, flat+perByte*limit, gasForLength(row, limit))
			require.Equal(t, uint64(math.MaxUint64), gasForLength(row, limit+1))
			require.Equal(t, uint64(math.MaxUint64), gasForLength(row, math.MaxUint64))
		}
		require.Equal(t, uint64(math.MaxUint64), gasForLength(pcommon.MethodGas{Surcharge: math.MaxUint64}, 0))
		require.Equal(t, uint64(math.MaxUint64), gasForLength(pcommon.MethodGas{Write: true, Surcharge: math.MaxUint64}, 0))
	})
}

func rowFlat(t *testing.T, name string) uint64 {
	t.Helper()
	row, ok := methodGas[name]
	require.True(t, ok)
	return row.Gas(nil)
}
