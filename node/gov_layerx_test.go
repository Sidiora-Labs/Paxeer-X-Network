package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	daemon "github.com/Sidiora-Labs/Paxeer-X-Network/daemon/paxd/cmd"
	anchor "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	bridge "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	layerxgov "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov"
	govcli "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/client/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestLayerXProposalAtomicExecution(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockHeight(8).WithBlockTime(time.Unix(1800000000, 0))
	router := testApp.GovKeeper.Router()
	require.True(t, router.HasRoute(types.RouterKey))
	handler := router.GetRoute(types.RouterKey)
	before := testApp.LayerXAnchorKeeper.GetParams(ctx)
	params := before
	params.ReporterShare = sdk.NewDecWithPrec(2, 1)
	authority := types.GovernanceAuthority()
	msg := &anchor.MsgUpdateParams{Authority: authority, Params: params}
	p, err := types.NewLayerXProposal("Configure fork", "Update anchor and pause bridge", msg, &bridge.MsgPause{Authority: authority})
	require.NoError(t, err)
	stored, err := testApp.GovKeeper.SubmitProposal(ctx, p)
	require.NoError(t, err)
	proposal, found := testApp.GovKeeper.GetProposal(ctx, stored.ProposalId)
	require.True(t, found)
	require.Equal(t, before, testApp.LayerXAnchorKeeper.GetParams(ctx))
	require.NoError(t, handler(ctx, proposal.GetContent()))
	require.Equal(t, params, testApp.LayerXAnchorKeeper.GetParams(ctx))
	require.True(t, testApp.LayerXBridgeKeeper.IsPaused(ctx))
	params.ReporterShare = sdk.NewDecWithPrec(3, 1)
	failing := &bridge.MsgSetCap{Authority: authority, ChainID: 999999, MaxInFlight: sdk.NewInt(20), MaxPerTx: sdk.NewInt(10)}
	require.NoError(t, failing.ValidateBasic())
	p, err = types.NewLayerXProposal("Atomic refusal", "Retain both prior values", &anchor.MsgUpdateParams{Authority: authority, Params: params}, failing)
	require.NoError(t, err)
	before = testApp.LayerXAnchorKeeper.GetParams(ctx)
	events := len(ctx.EventManager().Events())
	_, expected := testApp.MsgServiceRouter().Handler(failing)(ctx, failing)
	require.Error(t, expected)
	actual := handler(ctx, p)
	require.ErrorIs(t, actual, bridge.ErrUnknownChain)
	require.Equal(t, expected.Error(), actual.Error())
	require.Equal(t, before, testApp.LayerXAnchorKeeper.GetParams(ctx))
	require.True(t, testApp.LayerXBridgeKeeper.IsPaused(ctx))
	_, found = testApp.LayerXBridgeKeeper.GetAsset(ctx, failing.ChainID, failing.Asset)
	require.False(t, found)
	require.Len(t, ctx.EventManager().Events(), events)
	p, err = types.NewLayerXProposal("Ordering", "Pause then unpause", &bridge.MsgPause{Authority: authority}, &bridge.MsgUnpause{Authority: authority})
	require.NoError(t, err)
	require.NoError(t, handler(ctx, p))
	require.False(t, testApp.LayerXBridgeKeeper.IsPaused(ctx))
	require.Error(t, handler(ctx, &govtypes.TextProposal{Title: "Other", Description: "Other content"}))
	emptyRouter := baseapp.NewMsgServiceRouter()
	require.Error(t, layerxgov.NewProposalHandler(emptyRouter)(ctx, p))
	require.Equal(t, before, testApp.LayerXAnchorKeeper.GetParams(ctx))
}
func TestLayerXProposalCommandAndREST(t *testing.T) {
	root, encoding := daemon.NewRootCmd()
	command, rest, err := root.Find([]string{"tx", "gov", "submit-proposal", "layerx-proposal", "proposal.json"})
	require.NoError(t, err)
	require.Equal(t, govcli.ProposalCommandName, command.Name())
	require.Equal(t, []string{"proposal.json"}, rest)
	for _, flag := range []string{"from", "deposit", "generate-only", "gas"} {
		require.NotNil(t, command.Flags().Lookup(flag))
	}
	p, err := types.NewLayerXProposal("Pause bridge", "Governance authority", &bridge.MsgPause{Authority: types.GovernanceAuthority()})
	require.NoError(t, err)
	body, err := encoding.Marshaler.MarshalInterfaceJSON(p)
	require.NoError(t, err)
	proposer := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	submit, err := govcli.NewSubmitLayerXProposalMsg(encoding.Marshaler, body, "1uhpx", proposer)
	require.NoError(t, err)
	require.NoError(t, submit.ValidateBasic())
	require.NotEmpty(t, submit.GetSignBytes())
	for _, deposit := range []string{"", "0uhpx", "invalid"} {
		_, err := govcli.NewSubmitLayerXProposalMsg(encoding.Marshaler, body, deposit, proposer)
		require.Error(t, err)
	}
	_, err = govcli.NewSubmitLayerXProposalMsg(encoding.Marshaler, body, "1uhpx", nil)
	require.Error(t, err)
	for _, bad := range [][]byte{[]byte(`{}`), append(append([]byte{}, body...), []byte(` {}`)...), bytes.Replace(body, []byte(`"title"`), []byte(`"unknown"`), 1)} {
		_, err := govcli.DecodeLayerXProposal(encoding.Marshaler, bad)
		require.Error(t, err)
	}
	var out bytes.Buffer
	clientCtx := client.Context{}.WithCodec(encoding.Marshaler).WithInterfaceRegistry(encoding.InterfaceRegistry).WithTxConfig(encoding.TxConfig).WithLegacyAmino(encoding.Amino).WithChainID("paxeer-x-test").WithOutput(&out)
	file := filepath.Join(t.TempDir(), "proposal.json")
	require.NoError(t, os.WriteFile(file, body, 0600))
	command.SetArgs([]string{file, "--deposit", "1uhpx", "--from", proposer.String(), "--generate-only", "--keyring-backend", "test", "--keyring-dir", t.TempDir()})
	command.SetContext(context.WithValue(context.Background(), client.ClientContextKey, &clientCtx))

	require.NoError(t, command.Flags().Set("deposit", "1uhpx"))
	require.NoError(t, command.Flags().Set("from", proposer.String()))
	require.NoError(t, command.Flags().Set("generate-only", "true"))
	require.NoError(t, command.Flags().Set("keyring-backend", "test"))
	require.NoError(t, command.Flags().Set("keyring-dir", t.TempDir()))
	require.NoError(t, command.RunE(command, []string{file}), out.String())
	decoded, err := encoding.TxConfig.TxJSONDecoder()([]byte(strings.TrimSpace(out.String())))
	require.NoError(t, err)
	require.Len(t, decoded.GetMsgs(), 1)
	require.IsType(t, &govtypes.MsgSubmitProposal{}, decoded.GetMsgs()[0])
	endpoint := govcli.LayerXProposalRESTHandler(clientCtx)
	require.Equal(t, "layerx", endpoint.SubRoute)
	request := map[string]any{"base_req": map[string]any{"from": proposer.String(), "chain_id": "paxeer-x-test", "gas": "200000", "fees": []any{}}, "deposit": "1uhpx", "proposal": json.RawMessage(body)}
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	endpoint.Handler(recorder, httptest.NewRequest(http.MethodPost, "/gov/proposals/layerx", bytes.NewReader(raw)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "MsgSubmitProposal")
	request["unknown"] = true
	raw, err = json.Marshal(request)
	require.NoError(t, err)
	recorder = httptest.NewRecorder()
	endpoint.Handler(recorder, httptest.NewRequest(http.MethodPost, "/gov/proposals/layerx", bytes.NewReader(raw)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
