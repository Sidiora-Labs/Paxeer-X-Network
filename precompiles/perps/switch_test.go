package perps_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxexchange"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
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

// findFunc returns the top-level function name declares in file, or nil.
func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func TestVersionSwitchNoVersionEntry(t *testing.T) {
	fset := token.NewFileSet()

	// GetVersioned of the exchange keys its one contract by the upgrade it is
	// handed and by nothing else.
	setup, err := parser.ParseFile(fset, "../layerxexchange/setup.go", nil, 0)
	require.NoError(t, err)
	getVersioned := findFunc(setup, "GetVersioned")
	require.NotNil(t, getVersioned)
	require.Len(t, getVersioned.Body.List, 1)
	ret, ok := getVersioned.Body.List[0].(*ast.ReturnStmt)
	require.True(t, ok)
	require.Len(t, ret.Results, 1)
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	require.True(t, ok)
	require.Len(t, lit.Elts, 1)
	entry, ok := lit.Elts[0].(*ast.KeyValueExpr)
	require.True(t, ok)
	key, ok := entry.Key.(*ast.Ident)
	require.True(t, ok)
	require.Equal(t, "latestUpgrade", key.Name)

	testApp := app.Setup(t, false, true, false)
	versioned := layerxexchange.GetVersioned(app.LatestUpgrade, testApp.GetPrecompileKeepers())
	require.Len(t, versioned, 1)
	require.Contains(t, versioned, app.LatestUpgrade)
	require.NotContains(t, versioned, app.ActivationUpgrade)
	require.NotContains(t, versioned, app.V6120Upgrade)

	// The exchange version list and the tag list carry neither the v6.9 nor
	// the v6.12.0 plan.
	versions, err := os.ReadFile("../layerxexchange/versions")
	require.NoError(t, err)
	require.Equal(t, "v6.5\n", string(versions))
	tags, err := os.Open("../../node/tags")
	require.NoError(t, err)
	defer tags.Close()
	scanner := bufio.NewScanner(tags)
	lines := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		require.NotEqual(t, app.V6120Upgrade, line)
		lines++
	}
	require.NoError(t, scanner.Err())
	require.NotZero(t, lines)

	// The perpetuals package has no version setup of its own.
	_, err = os.Stat("setup.go")
	require.True(t, os.IsNotExist(err), "precompiles/perps/setup.go exists")

	// The switch of the exchange address is exchangeVersionSwitch of the node.
	node, err := parser.ParseFile(fset, "../../node/precompiles_v6120.go", nil, 0)
	require.NoError(t, err)
	exchangeSwitch := findFunc(node, "exchangeVersionSwitch")
	require.NotNil(t, exchangeSwitch)
	params := make([]string, 0, 3)
	for _, field := range exchangeSwitch.Type.Params.List {
		for _, name := range field.Names {
			params = append(params, name.Name)
		}
	}
	require.Equal(t, []string{"perps", "legacy", "live"}, params)
	require.Len(t, exchangeSwitch.Type.Results.List, 1)
}

// methodSigs lists the method signatures of a, sorted.
func methodSigs(a abi.ABI) []string {
	sigs := make([]string, 0, len(a.Methods))
	for _, m := range a.Methods {
		sigs = append(sigs, m.Sig)
	}
	sort.Strings(sigs)
	return sigs
}

func TestVersionSwitchHeightSelection(t *testing.T) {
	const (
		activationHeight = int64(10)
		doneHeight       = int64(1000)
	)
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	testApp.UpgradeKeeper.SetDone(ctx.WithBlockHeight(activationHeight), app.ActivationUpgrade)
	exchange := common.HexToAddress(layerxexchange.ExchangeAddress)

	keepers := testApp.GetPrecompileKeepers()
	legacy, err := layerxexchange.NewPrecompile(keepers)
	require.NoError(t, err)
	next, err := perps.NewPrecompile(keepers)
	require.NoError(t, err)
	wantPerps, err := perpsabi.ABI()
	require.NoError(t, err)
	require.Equal(t, methodSigs(wantPerps), methodSigs(next.ABI))
	require.NotEqual(t, methodSigs(legacy.ABI), methodSigs(next.ABI))
	require.NotEqual(t, legacy.GetName(), next.GetName())

	served := func(c sdk.Context, want *pcommon.Precompile) {
		t.Helper()
		contract, ok := testApp.EvmKeeper.CustomPrecompiles(c)[exchange]
		require.True(t, ok, "height %d tracing %v", c.BlockHeight(), c.IsTracing())
		p, ok := contract.(*pcommon.Precompile)
		require.True(t, ok, "height %d tracing %v", c.BlockHeight(), c.IsTracing())
		require.Equal(t, want.GetName(), p.GetName(), "height %d tracing %v", c.BlockHeight(), c.IsTracing())
		require.Equal(t, methodSigs(want.ABI), methodSigs(p.ABI), "height %d tracing %v", c.BlockHeight(), c.IsTracing())
	}

	// With the live flag false, a call at 999 resolves to the v6.9 exchange.
	below := ctx.WithBlockHeight(doneHeight - 1)
	served(below, legacy)
	require.Equal(t, app.ActivationUpgrade, testApp.EvmKeeper.GetCustomPrecompilesVersions(below)[exchange])

	// The v6.12.0 handler sets the flag at 1000.
	at := ctx.WithBlockHeight(doneHeight)
	plan := upgradetypes.Plan{Name: app.V6120Upgrade, Height: doneHeight}
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(at, plan))
	testApp.UpgradeKeeper.ApplyUpgrade(at, plan)
	require.Equal(t, doneHeight, testApp.UpgradeKeeper.GetDoneHeight(at, app.V6120Upgrade))

	// From 1000 the address serves the perpetuals executor.
	served(at, next)
	served(at.WithIsTracing(true), next)
	require.Equal(t, app.V6120Upgrade, testApp.EvmKeeper.GetCustomPrecompilesVersions(at)[exchange])
	served(ctx.WithBlockHeight(doneHeight+1), next)
	require.Equal(t, app.V6120Upgrade, testApp.EvmKeeper.GetCustomPrecompilesVersions(ctx.WithBlockHeight(doneHeight + 1))[exchange])

	// A trace of 999 still resolves the v6.9 exchange.
	traced := ctx.WithBlockHeight(doneHeight - 1).WithIsTracing(true)
	require.Equal(t, app.ActivationUpgrade, testApp.EvmKeeper.GetCustomPrecompilesVersions(traced)[exchange])
	served(traced, legacy)
}
