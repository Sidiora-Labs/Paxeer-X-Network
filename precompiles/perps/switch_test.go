package perps_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/testvectors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	custodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	exchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
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
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
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
	for _, height := range []int64{doneHeight, doneHeight + 1} {
		for _, tracing := range []bool{false, true} {
			inactive := ctx.WithBlockHeight(height).WithIsTracing(tracing)
			served(inactive, legacy)
			require.Equal(t, app.ActivationUpgrade, testApp.EvmKeeper.GetCustomPrecompilesVersions(inactive)[exchange])
		}
	}

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

func loadVersionSwitchVectors(t *testing.T) v69Vectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/v69_vectors.json")
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var vectors v69Vectors
	require.NoError(t, decoder.Decode(&vectors))
	require.Len(t, vectors.Cases, 8)
	calls := 0
	for _, c := range vectors.Cases {
		require.NotEmpty(t, c.Calls, c.Name)
		calls += len(c.Calls)
	}
	require.Equal(t, 34, calls)
	require.Equal(t, common.HexToAddress(layerxexchange.ExchangeAddress).Hex(), vectors.Precompile)
	return vectors
}

func versionSwitchContract(t *testing.T, a *app.App, ctx sdk.Context, version string) *pcommon.Precompile {
	t.Helper()
	address := common.HexToAddress(layerxexchange.ExchangeAddress)
	require.Equal(t, version, a.EvmKeeper.GetCustomPrecompilesVersions(ctx)[address])
	contract, ok := a.EvmKeeper.CustomPrecompiles(ctx)[address]
	require.True(t, ok)
	selected, ok := contract.(*pcommon.Precompile)
	require.True(t, ok)
	return selected
}

func activateVersionSwitch(t *testing.T, a *app.App, ctx sdk.Context) {
	t.Helper()
	plan := upgradetypes.Plan{Name: app.V6120Upgrade, Height: ctx.BlockHeight()}
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
	require.NotPanics(t, func() { a.UpgradeKeeper.ApplyUpgrade(ctx, plan) })
	require.Equal(t, ctx.BlockHeight(), a.UpgradeKeeper.GetDoneHeight(ctx, app.V6120Upgrade))
}

func TestVersionSwitchConstructors(t *testing.T) {
	const doneHeight = int64(1000)
	vectors := loadVersionSwitchVectors(t)
	a := app.Setup(t, false, true, false)
	ctx := a.GetContextForDeliverTx(nil).WithBlockTime(time.Unix(vectors.Harness.BlockTimeUnix, 0).UTC())
	a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(vectors.Harness.BlockHeight-1), app.ActivationUpgrade)
	keepers := a.GetPrecompileKeepers()
	legacy, err := layerxexchange.NewPrecompile(keepers)
	require.NoError(t, err)
	next, err := perps.NewPrecompile(keepers)
	require.NoError(t, err)
	legacyJSON, err := os.ReadFile("../layerxexchange/abi.json")
	require.NoError(t, err)
	legacyABI, err := abi.JSON(bytes.NewReader(legacyJSON))
	require.NoError(t, err)
	nextABI, err := perpsabi.ABI()
	require.NoError(t, err)
	require.Equal(t, legacyABI, legacy.GetABI())
	require.Equal(t, nextABI, next.GetABI())
	require.NotEqual(t, legacy.GetABI(), next.GetABI())

	t.Run("below_activation", func(t *testing.T) {
		for _, tracing := range []bool{false, true} {
			selected := versionSwitchContract(t, a, ctx.WithBlockHeight(doneHeight-1).WithIsTracing(tracing), app.ActivationUpgrade)
			require.Equal(t, legacy, selected)
			require.Equal(t, legacyABI, selected.GetABI())
		}
	})
	t.Run("keeper_wiring", func(t *testing.T) {
		provider, ok := interface{}(keepers).(perps.PerpsKeepers)
		require.True(t, ok, "application keepers must expose PerpsK")
		require.NotNil(t, provider.PerpsK())
	})
	t.Run("from_activation", func(t *testing.T) {
		activateVersionSwitch(t, a, ctx.WithBlockHeight(doneHeight))
		for _, height := range []int64{doneHeight, doneHeight + 1} {
			for _, tracing := range []bool{false, true} {
				selected := versionSwitchContract(t, a, ctx.WithBlockHeight(height).WithIsTracing(tracing), app.V6120Upgrade)
				require.Equal(t, next, selected)
				require.Equal(t, nextABI, selected.GetABI())
			}
		}
		historical := versionSwitchContract(t, a, ctx.WithBlockHeight(doneHeight-1).WithIsTracing(true), app.ActivationUpgrade)
		require.Equal(t, legacy, historical)
		require.Equal(t, legacyABI, historical.GetABI())
	})
}

func TestVersionSwitchV69Vectors(t *testing.T) {
	vectors := loadVersionSwitchVectors(t)
	fixture, err := testvectors.Load()
	require.NoError(t, err)
	require.NotEmpty(t, fixture["withdrawal"])
	require.NotEmpty(t, fixture["exit"])
	native, err := fixture["withdrawal"][0].Array32("asset")
	require.NoError(t, err)
	token, err := fixture["exit"][0].Array32("asset")
	require.NoError(t, err)

	for _, c := range vectors.Cases {
		t.Run("legacy/"+c.Name, func(t *testing.T) {
			a := app.Setup(t, false, true, false)
			ctx := a.GetContextForDeliverTx(nil).
				WithBlockHeight(vectors.Harness.BlockHeight).
				WithBlockTime(time.Unix(vectors.Harness.BlockTimeUnix, 0).UTC())
			require.Equal(t, vectors.Harness.ChainID, a.EvmKeeper.ChainID(ctx).String())
			a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(ctx.BlockHeight()-1), app.ActivationUpgrade)
			require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, app.V6120Upgrade))
			a.LayerXCustodyKeeper.SetAnchorReader(nil)
			a.LayerXCustodyKeeper.InitGenesis(ctx, *custodytypes.DefaultGenesis())
			a.LayerXExchangeKeeper.InitGenesis(ctx, *exchangetypes.DefaultGenesis())
			require.NoError(t, a.LayerXCustodyKeeper.SetAsset(ctx, custodytypes.AssetMapping{
				AssetId: custodytypes.Hash32(native), Denom: sdk.MustGetBaseDenom(), Enabled: true,
			}))
			require.NoError(t, a.LayerXCustodyKeeper.SetAsset(ctx, custodytypes.AssetMapping{
				AssetId: custodytypes.Hash32(token), Denom: "ufoo", Pointer: common.HexToAddress("0xf00f0").Hex(), Enabled: true,
			}))
			for _, market := range []exchangetypes.Market{
				{MarketId: custodytypes.Hash32([32]byte{0x6d, 0x01}), MarginAssetId: custodytypes.Hash32(native), Enabled: true},
				{MarketId: custodytypes.Hash32([32]byte{0x6d, 0x02}), MarginAssetId: custodytypes.Hash32(token), Enabled: true},
			} {
				require.NoError(t, a.LayerXExchangeKeeper.SetMarket(ctx, market))
			}
			funded := map[common.Address]sdk.AccAddress{}
			for _, call := range c.Calls {
				require.True(t, common.IsHexAddress(call.Caller))
				caller := common.HexToAddress(call.Caller)
				account, err := sdk.AccAddressFromBech32(call.CallerPax)
				require.NoError(t, err)
				if previous, ok := funded[caller]; ok {
					require.Equal(t, previous, account)
					continue
				}
				a.EvmKeeper.SetAddressMapping(ctx, account, caller)
				for _, denom := range []string{sdk.MustGetBaseDenom(), "ufoo"} {
					coins := sdk.NewCoins(sdk.NewInt64Coin(denom, 50_000_000))
					require.NoError(t, a.BankKeeper.MintCoins(ctx, "evm", coins))
					require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", account, coins))
				}
				funded[caller] = account
			}
			for _, checkpoint := range c.Checkpoints {
				root := decodeHex(t, "checkpoint", checkpoint.StateRoot)
				require.Len(t, root, 32)
				require.NoError(t, a.LayerXCustodyKeeper.RegisterCheckpoint(ctx, checkpoint.Batch, [32]byte(root), [32]byte{1}))
			}
			db := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), &a.EvmKeeper, true)
			for i, call := range c.Calls {
				t.Run(fmt.Sprintf("%02d_%s", i, call.Method), func(t *testing.T) {
					selected := versionSwitchContract(t, a, db.Ctx(), app.ActivationUpgrade)
					require.Equal(t, layerxexchange.PrecompileName, selected.GetName())
					if !call.Keeper {
						missing, err := layerxexchange.NewPrecompile(bareKeepers{Keepers: a.GetPrecompileKeepers()})
						require.NoError(t, err)
						require.Equal(t, selected.GetABI(), missing.GetABI())
						saved := *selected
						*selected = *missing
						t.Cleanup(func() { *selected = saved })
						selected = versionSwitchContract(t, a, db.Ctx(), app.ActivationUpgrade)
					}
					input := decodeHex(t, "input", call.Input)
					require.Equal(t, call.RequiredGas, selected.RequiredGas(input))
					if !call.Executed {
						require.False(t, call.Reverted)
						require.Equal(t, "0x", call.Return)
						require.Equal(t, "0x", call.Revert)
						require.Empty(t, call.Logs)
						return
					}
					caller := common.HexToAddress(call.Caller)
					value, ok := new(big.Int).SetString(call.Value, 10)
					require.True(t, ok)
					require.GreaterOrEqual(t, value.Sign(), 0)
					if value.Sign() > 0 {
						base, wei := new(big.Int).QuoRem(value, big.NewInt(1_000_000_000_000), new(big.Int))
						require.NoError(t, a.BankKeeper.SendCoinsAndWei(db.Ctx(), funded[caller],
							a.EvmKeeper.GetPaxAddressOrDefault(db.Ctx(), common.HexToAddress(vectors.Precompile)),
							sdk.NewIntFromBigInt(base), sdk.NewIntFromBigInt(wei)))
					}
					evm := &vm.EVM{StateDB: db, TxContext: vm.TxContext{Origin: caller}}
					before := len(db.GetAllLogs())
					output, err := selected.Run(evm, caller, caller, input, value, call.ReadOnly, call.Delegate, nil)
					var returned, reverted []byte
					if call.Reverted {
						require.ErrorIs(t, err, vm.ErrExecutionReverted)
						reverted = output
					} else {
						require.NoError(t, err)
						returned = output
					}
					require.Equal(t, decodeHex(t, "return", call.Return), append([]byte{}, returned...))
					require.Equal(t, decodeHex(t, "revert", call.Revert), append([]byte{}, reverted...))
					logs := db.GetAllLogs()[before:]
					require.Len(t, logs, len(call.Logs))
					for j, want := range call.Logs {
						require.Equal(t, decodeHex(t, "log address", want.Address), logs[j].Address.Bytes())
						require.Len(t, logs[j].Topics, len(want.Topics))
						for k, topic := range want.Topics {
							require.Equal(t, decodeHex(t, "log topic", topic), logs[j].Topics[k].Bytes())
						}
						require.Equal(t, decodeHex(t, "log data", want.Data), logs[j].Data)
					}
				})
			}
		})
	}

	t.Run("live_rejects_legacy_selectors", func(t *testing.T) {
		a := app.Setup(t, false, true, false)
		ctx := a.GetContextForDeliverTx(nil).WithBlockHeight(vectors.Harness.BlockHeight).
			WithBlockTime(time.Unix(vectors.Harness.BlockTimeUnix, 0).UTC())
		a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(ctx.BlockHeight()-1), app.ActivationUpgrade)
		legacy, err := layerxexchange.NewPrecompile(a.GetPrecompileKeepers())
		require.NoError(t, err)
		ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
		activateVersionSwitch(t, a, ctx)
		selected := versionSwitchContract(t, a, ctx, app.V6120Upgrade)
		wantABI, err := perpsabi.ABI()
		require.NoError(t, err)
		require.Equal(t, wantABI, selected.GetABI())
		refuse := func(t *testing.T, input []byte, value *big.Int, readOnly, delegate bool) {
			t.Helper()
			_, err := wantABI.MethodById(input[:4])
			require.Error(t, err)
			db := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), &a.EvmKeeper, true)
			caller := common.HexToAddress(vectors.Cases[0].Calls[0].Caller)
			evm := &vm.EVM{StateDB: db, TxContext: vm.TxContext{Origin: caller}}
			output, err := selected.Run(evm, caller, caller, input, value, readOnly, delegate, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			require.Empty(t, output)
			require.Empty(t, db.GetAllLogs())
		}
		for _, c := range vectors.Cases {
			for i, call := range c.Calls {
				input := decodeHex(t, "input", call.Input)
				if _, err := wantABI.MethodById(input[:4]); err == nil {
					continue
				}
				t.Run(fmt.Sprintf("%s/%02d_%s", c.Name, i, call.Method), func(t *testing.T) {
					value, ok := new(big.Int).SetString(call.Value, 10)
					require.True(t, ok)
					refuse(t, input, value, call.ReadOnly, call.Delegate)
				})
			}
		}
		for _, signature := range methodSigs(legacy.GetABI()) {
			selector := crypto.Keccak256([]byte(signature))[:4]
			if _, err := wantABI.MethodById(selector); err == nil {
				continue
			}
			t.Run(signature, func(t *testing.T) {
				method, err := legacy.MethodById(selector)
				require.NoError(t, err)
				args := make([]interface{}, len(method.Inputs))
				for i, arg := range method.Inputs {
					args[i] = reflect.Zero(arg.Type.GetType()).Interface()
					if arg.Type.GetType() == reflect.TypeOf((*big.Int)(nil)) {
						args[i] = new(big.Int)
					}
				}
				packed, err := method.Inputs.Pack(args...)
				require.NoError(t, err)
				refuse(t, append(selector, packed...), nil, false, false)
			})
		}
		t.Run("pushOracle(bytes,bytes)", func(t *testing.T) {
			bytesType, err := abi.NewType("bytes", "", nil)
			require.NoError(t, err)
			packed, err := (abi.Arguments{{Type: bytesType}, {Type: bytesType}}).Pack(make([]byte, 72), make([]byte, 64))
			require.NoError(t, err)
			refuse(t, append(crypto.Keccak256([]byte("pushOracle(bytes,bytes)"))[:4], packed...), nil, false, false)
		})
	})
}
