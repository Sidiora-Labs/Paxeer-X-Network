package perps_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxexchange"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

type v69Log struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type v69Call struct {
	Method      string   `json:"method"`
	Caller      string   `json:"caller"`
	CallerPax   string   `json:"caller_pax"`
	Value       string   `json:"value"`
	ReadOnly    bool     `json:"read_only"`
	Delegate    bool     `json:"delegate"`
	Keeper      bool     `json:"keeper"`
	Executed    bool     `json:"executed"`
	Input       string   `json:"input"`
	RequiredGas uint64   `json:"required_gas"`
	Reverted    bool     `json:"reverted"`
	Return      string   `json:"return"`
	Revert      string   `json:"revert"`
	Logs        []v69Log `json:"logs"`
}

type v69Checkpoint struct {
	Batch     uint64 `json:"batch"`
	StateRoot string `json:"state_root"`
}

type v69Case struct {
	Name        string          `json:"name"`
	Checkpoints []v69Checkpoint `json:"checkpoints"`
	Calls       []v69Call       `json:"calls"`
}

type v69Harness struct {
	BlockHeight   int64  `json:"block_height"`
	BlockTimeUnix int64  `json:"block_time_unix"`
	ChainID       string `json:"chain_id"`
}

type v69Vectors struct {
	Precompile string     `json:"precompile"`
	Harness    v69Harness `json:"harness"`
	Cases      []v69Case  `json:"cases"`
}

func decodeHex(t *testing.T, field, value string) []byte {
	t.Helper()
	out, err := hexutil.Decode(value)
	require.NoError(t, err, field)
	return out
}

func TestVersionSwitchVectorsRecorded(t *testing.T) {
	raw, err := os.ReadFile("testdata/v69_vectors.json")
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var vectors v69Vectors
	require.NoError(t, decoder.Decode(&vectors))
	require.Equal(t, common.HexToAddress(layerxexchange.ExchangeAddress).Hex(), vectors.Precompile)
	require.Equal(t, v69Harness{BlockHeight: 11, BlockTimeUnix: 1_800_000_000, ChainID: vectors.Harness.ChainID}, vectors.Harness)
	require.NotEmpty(t, vectors.Harness.ChainID)

	abiJSON, err := os.ReadFile("../layerxexchange/abi.json")
	require.NoError(t, err)
	exchangeABI, err := abi.JSON(bytes.NewReader(abiJSON))
	require.NoError(t, err)

	want := []string{
		"TestDepositMarginCustodiesNativeValue",
		"TestDepositMarginTokenCustodiesTheDenom",
		"TestOrderIntentsAreRecordedAndEmitted",
		"TestWithdrawMarginRecordsARequestAndMovesNothing",
		"TestStateViewsVerifyFinalizedWitnesses",
		"TestMarginViewVerifiesTheAccountLeaf",
		"TestRequiredGasFollowsTheFormula",
		"TestMissingKeeperRefuses",
	}
	names := make([]string, 0, len(vectors.Cases))
	for _, c := range vectors.Cases {
		names = append(names, c.Name)
	}
	require.Equal(t, want, names)

	for _, c := range vectors.Cases {
		require.NotEmpty(t, c.Calls, c.Name)
		for _, cp := range c.Checkpoints {
			require.Len(t, decodeHex(t, c.Name+" checkpoint", cp.StateRoot), 32, c.Name)
		}
		for i, call := range c.Calls {
			require.True(t, common.IsHexAddress(call.Caller), "%s call %d caller", c.Name, i)
			require.NotEmpty(t, call.CallerPax, "%s call %d caller_pax", c.Name, i)
			input := decodeHex(t, c.Name+" input", call.Input)
			require.GreaterOrEqual(t, len(input), 4, "%s call %d input", c.Name, i)
			method, err := exchangeABI.MethodById(input[:4])
			require.NoError(t, err, "%s call %d selector %x", c.Name, i, input[:4])
			require.Equal(t, call.Method, method.Name, "%s call %d", c.Name, i)
			require.NotZero(t, call.RequiredGas, "%s call %d", c.Name, i)
			ret := decodeHex(t, c.Name+" return", call.Return)
			revert := decodeHex(t, c.Name+" revert", call.Revert)
			if !call.Executed {
				require.False(t, call.Reverted, "%s call %d", c.Name, i)
				require.Empty(t, ret, "%s call %d", c.Name, i)
				require.Empty(t, revert, "%s call %d", c.Name, i)
				require.Empty(t, call.Logs, "%s call %d", c.Name, i)
				continue
			}
			if call.Reverted {
				require.Empty(t, ret, "%s call %d", c.Name, i)
				reason, err := abi.UnpackRevert(revert)
				require.NoError(t, err, "%s call %d revert", c.Name, i)
				require.NotEmpty(t, reason, "%s call %d revert", c.Name, i)
				require.Empty(t, call.Logs, "%s call %d", c.Name, i)
				continue
			}
			require.Empty(t, revert, "%s call %d", c.Name, i)
			_, err = method.Outputs.Unpack(ret)
			require.NoError(t, err, "%s call %d return", c.Name, i)
			for _, log := range call.Logs {
				require.Equal(t, vectors.Precompile, log.Address, "%s call %d log", c.Name, i)
				require.NotEmpty(t, log.Topics, "%s call %d log", c.Name, i)
				event, err := exchangeABI.EventByID(common.HexToHash(log.Topics[0]))
				require.NoError(t, err, "%s call %d log topic", c.Name, i)
				_, err = event.Inputs.NonIndexed().Unpack(decodeHex(t, c.Name+" log data", log.Data))
				require.NoError(t, err, "%s call %d log data", c.Name, i)
			}
		}
	}
}
