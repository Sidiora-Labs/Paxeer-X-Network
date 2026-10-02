package cli_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	tmtypes "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/client/cli"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	"github.com/sidiora-labs/paxeer-network/sdk/client"
	"github.com/sidiora-labs/paxeer-network/sdk/client/flags"
	"github.com/sidiora-labs/paxeer-network/sdk/client/tx"
	"github.com/sidiora-labs/paxeer-network/sdk/crypto/hd"
	"github.com/sidiora-labs/paxeer-network/sdk/crypto/keyring"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	"github.com/sidiora-labs/paxeer-network/sdk/types/tx/signing"
	authsigning "github.com/sidiora-labs/paxeer-network/sdk/x/auth/signing"
	banktypes "github.com/sidiora-labs/paxeer-network/sdk/x/bank/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	testkeeper "github.com/sidiora-labs/paxeer-network/testutil/keeper"
)

const (
	testChainID = "paxeer-custody-cli-test"
	proposerKey = "proposer"
	sidPointer  = "0x21f7b20a555199fa73A238B1a91FD0f549068fEe"
)

func hash32(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func assetID(symbol string) string {
	sum := sha256.Sum256([]byte("layerx-asset:125:" + symbol))
	return hex.EncodeToString(sum[:])
}

func depositRootAuthority() string {
	seed := sha256.Sum256([]byte("custody cli test"))
	key := ed25519.NewKeyFromSeed(seed[:])
	return hex.EncodeToString(key.Public().(ed25519.PublicKey))
}

// governanceMessages returns the five custody governance messages in the
// order a proposal executes them.
func governanceMessages(t *testing.T) []sdk.Msg {
	t.Helper()
	governance := types.GovernanceAuthority()
	ctx, _ := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(7).CacheContext()
	params := testkeeper.EVMTestApp.LayerXCustodyKeeper.GetParams(ctx)
	params.DepositRootAuthority = depositRootAuthority()
	return []sdk.Msg{
		&types.MsgUpdateParams{Authority: governance, Params: params},
		&types.MsgSetAsset{Authority: governance, Asset: types.AssetMapping{
			AssetId: assetID("SID"), Denom: "usid", Pointer: sidPointer, Enabled: true, MinimumDeposit: "1"}},
		&types.MsgRegisterCheckpoint{Authority: governance, BatchNumber: 42,
			StateRoot: hash32("state root 42"), ReceiptRoot: hash32("receipt root 42")},
		&types.MsgSetEmergency{Authority: governance, Enabled: true},
		&types.MsgCancelClaim{Authority: governance, ClaimId: hash32("pending claim")},
	}
}

func messageJSON(t *testing.T, msgs ...sdk.Msg) []json.RawMessage {
	t.Helper()
	cdc := testkeeper.EVMTestApp.AppCodec()
	out := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		raw, err := cdc.MarshalInterfaceJSON(msg)
		require.NoError(t, err)
		out = append(out, raw)
	}
	return out
}

func proposalFile(t *testing.T, title, description, deposit string, msgs ...sdk.Msg) []byte {
	t.Helper()
	body, err := json.Marshal(cli.ProposalFile{Title: title, Description: description, Deposit: deposit, Messages: messageJSON(t, msgs...)})
	require.NoError(t, err)
	return body
}

func writeFile(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proposal.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

// newKeyring returns an in-memory keyring holding one fresh secp256k1 key.
func newKeyring(t *testing.T) (keyring.Keyring, sdk.AccAddress) {
	t.Helper()
	kr := keyring.NewInMemory()
	info, _, err := kr.NewMnemonic(proposerKey, keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	require.NoError(t, err)
	return kr, info.GetAddress()
}

func clientContext(kr keyring.Keyring, out *bytes.Buffer) client.Context {
	app := testkeeper.EVMTestApp
	return client.Context{}.
		WithCodec(app.AppCodec()).
		WithInterfaceRegistry(app.InterfaceRegistry()).
		WithTxConfig(app.GetTxConfig()).
		WithLegacyAmino(app.LegacyAmino()).
		WithKeyring(kr).
		WithChainID(testChainID).
		WithOutputFormat("json").
		WithOutput(out)
}

// runSubmit executes paxd tx layerxcustody submit-proposal custody through the
// module's real command tree and returns the command's output.
func runSubmit(t *testing.T, kr keyring.Keyring, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	clientCtx := clientContext(kr, &out)
	cmd := layerxcustody.NewAppModuleBasic().GetTxCmd()
	cmd.SetArgs(append([]string{"submit-proposal", "custody"}, args...))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)
	err := cmd.ExecuteContext(ctx)
	return out.String(), err
}

func generateArgs(path string) []string {
	return []string{path, "--" + flags.FlagFrom, proposerKey, "--" + flags.FlagGenerateOnly,
		"--" + flags.FlagChainID, testChainID, "--" + flags.FlagFees, "2000usei", "--" + flags.FlagGas, "300000"}
}

func decodeGenerated(t *testing.T, output string) authsigning.Tx {
	t.Helper()
	decoded, err := testkeeper.EVMTestApp.GetTxConfig().TxJSONDecoder()([]byte(strings.TrimSpace(output)))
	require.NoError(t, err)
	signable, ok := decoded.(authsigning.Tx)
	require.True(t, ok)
	return signable
}

func requireCarriedProposal(t *testing.T, msg *govtypes.MsgSubmitProposal, proposer sdk.AccAddress, deposit sdk.Coins, want []sdk.Msg) {
	t.Helper()
	require.Equal(t, proposer.String(), msg.Proposer)
	require.Equal(t, []sdk.AccAddress{proposer}, msg.GetSigners())
	require.True(t, deposit.IsEqual(msg.GetInitialDeposit()), "deposit %s", msg.GetInitialDeposit())
	content, ok := msg.GetContent().(*types.CustodyProposal)
	require.True(t, ok)
	require.Equal(t, "Custody governance", content.Title)
	require.Equal(t, "Every custody governance message", content.Description)
	got, err := content.GetMessages()
	require.NoError(t, err)
	require.Len(t, got, len(want))
	require.Equal(t, want[1].(*types.MsgSetAsset).Asset, got[1].(*types.MsgSetAsset).Asset)
	require.Equal(t, want[2], got[2])
	require.Equal(t, want[3], got[3])
	require.Equal(t, want[4], got[4])
	require.Equal(t, want[0].(*types.MsgUpdateParams).Params.DepositRootAuthority, got[0].(*types.MsgUpdateParams).Params.DepositRootAuthority)
	for i := range want {
		require.Equal(t, sdk.MsgTypeURL(want[i]), sdk.MsgTypeURL(got[i]), "message %d", i)
		wantBytes, err := testkeeper.EVMTestApp.AppCodec().MarshalInterface(want[i])
		require.NoError(t, err)
		gotBytes, err := testkeeper.EVMTestApp.AppCodec().MarshalInterface(got[i])
		require.NoError(t, err)
		require.Equal(t, wantBytes, gotBytes, "message %d preserves every field", i)
	}
}

func TestCustodyProposalCommandRegistration(t *testing.T) {
	root := layerxcustody.NewAppModuleBasic().GetTxCmd()
	require.Equal(t, types.ModuleName, root.Use)
	found, rest, err := root.Find([]string{"submit-proposal", "custody", "proposal.json"})
	require.NoError(t, err)
	require.Equal(t, []string{"proposal.json"}, rest)
	require.Equal(t, "custody [proposal-file]", found.Use)
	require.Equal(t, "submit-proposal", found.Parent().Use)
	require.Same(t, root, found.Parent().Parent())
	require.Error(t, found.Args(found, nil))
	require.Error(t, found.Args(found, []string{"a.json", "b.json"}))
	require.NoError(t, found.Args(found, []string{"a.json"}))
	for _, name := range []string{flags.FlagFrom, flags.FlagGenerateOnly, flags.FlagChainID, flags.FlagFees,
		flags.FlagGas, flags.FlagSignMode, flags.FlagAccountNumber, flags.FlagSequence, flags.FlagOffline} {
		require.NotNil(t, found.Flags().Lookup(name), "flag %s", name)
	}
	var names []string
	for _, sub := range root.Commands() {
		names = append(names, sub.Name())
	}
	require.Equal(t, []string{"submit-proposal"}, names)
	require.IsType(t, &cobra.Command{}, found)
}

func TestCustodyProposalCommandPreservesProposalAndSigner(t *testing.T) {
	kr, proposer := newKeyring(t)
	want := governanceMessages(t)
	deposit := sdk.NewCoins(sdk.NewInt64Coin("usei", 1000))
	body := proposalFile(t, "Custody governance", "Every custody governance message", "1000usei", want...)

	msg, err := cli.NewSubmitCustodyProposalMsg(testkeeper.EVMTestApp.AppCodec(), body, proposer)
	require.NoError(t, err)
	requireCarriedProposal(t, msg, proposer, deposit, want)

	// Trailing whitespace after the proposal is accepted.
	withSpace := append(append([]byte{}, body...), []byte(" \n\t\r\n")...)
	spaced, err := cli.NewSubmitCustodyProposalMsg(testkeeper.EVMTestApp.AppCodec(), withSpace, proposer)
	require.NoError(t, err)
	requireCarriedProposal(t, spaced, proposer, deposit, want)

	// A decimal deposit normalizes to its integer coin.
	decimal := proposalFile(t, "Custody governance", "Every custody governance message", "1000.0usei", want...)
	normalized, err := cli.NewSubmitCustodyProposalMsg(testkeeper.EVMTestApp.AppCodec(), decimal, proposer)
	require.NoError(t, err)
	requireCarriedProposal(t, normalized, proposer, deposit, want)

	output, err := runSubmit(t, kr, generateArgs(writeFile(t, withSpace))...)
	require.NoError(t, err)
	generated := decodeGenerated(t, output)
	msgs := generated.GetMsgs()
	require.Len(t, msgs, 1)
	submitted, ok := msgs[0].(*govtypes.MsgSubmitProposal)
	require.True(t, ok)
	requireCarriedProposal(t, submitted, proposer, deposit, want)
	require.Equal(t, []sdk.AccAddress{proposer}, generated.GetSigners())
}

func TestCustodyProposalCommandRefusesMalformedFiles(t *testing.T) {
	kr, proposer := newKeyring(t)
	cdc := testkeeper.EVMTestApp.AppCodec()
	valid := proposalFile(t, "Custody governance", "Every custody governance message", "1000usei", governanceMessages(t)...)

	var outer map[string]any
	require.NoError(t, json.Unmarshal(valid, &outer))
	outer["extra"] = true
	unknownOuter, err := json.Marshal(outer)
	require.NoError(t, err)

	var nested map[string]any
	require.NoError(t, json.Unmarshal(valid, &nested))
	first := nested["messages"].([]any)[1].(map[string]any)
	first["bogus"] = 1
	unknownNested, err := json.Marshal(nested)
	require.NoError(t, err)

	var deep map[string]any
	require.NoError(t, json.Unmarshal(valid, &deep))
	deep["messages"].([]any)[1].(map[string]any)["asset"].(map[string]any)["bogus"] = "x"
	unknownDeep, err := json.Marshal(deep)
	require.NoError(t, err)

	cases := map[string][]byte{
		"empty":                 {},
		"whitespace only":       []byte(" \n"),
		"truncated":             valid[:len(valid)/2],
		"malformed":             []byte(`{"title":"Custody",}`),
		"array top level":       []byte(`[]`),
		"string top level":      []byte(`"proposal"`),
		"number top level":      []byte(`7`),
		"unknown outer field":   unknownOuter,
		"unknown message field": unknownNested,
		"unknown nested field":  unknownDeep,
		"second JSON object":    append(append([]byte{}, valid...), valid...),
		"trailing JSON value":   append(append([]byte{}, valid...), []byte(" {}")...),
		"trailing garbage":      append(append([]byte{}, valid...), []byte("garbage")...),
		"trailing bracket":      append(append([]byte{}, valid...), '}'),
		"wrong messages type":   []byte(`{"title":"t","description":"d","deposit":"1usei","messages":{}}`),
		"message without type":  []byte(`{"title":"t","description":"d","deposit":"1usei","messages":[{"enabled":true}]}`),
		"unresolved type":       []byte(`{"title":"t","description":"d","deposit":"1usei","messages":[{"@type":"/paxprotocol.paxchain.layerxcustody.MsgUnknown"}]}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
			require.Error(t, err)
			output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
			require.Error(t, err)
			require.Empty(t, output, "a refused file must emit no transaction")
		})
	}

	output, err := runSubmit(t, kr, generateArgs(filepath.Join(t.TempDir(), "missing.json"))...)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, output)
}

func TestCustodyProposalCommandRefusesInvalidDeposits(t *testing.T) {
	kr, proposer := newKeyring(t)
	cdc := testkeeper.EVMTestApp.AppCodec()
	msgs := governanceMessages(t)
	for _, deposit := range []string{"", "0usei", "0", "-5usei", "1000", "usei", "10 usei x", "1000U$D", "1.5.5usei", "0.0usei"} {
		t.Run(deposit, func(t *testing.T) {
			body := proposalFile(t, "Custody governance", "Every custody governance message", deposit, msgs...)
			_, err := cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
			require.Error(t, err)
			output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
			require.Error(t, err)
			require.Empty(t, output)
		})
	}
	var omitted map[string]any
	require.NoError(t, json.Unmarshal(proposalFile(t, "Custody governance", "Every custody governance message", "1usei", msgs...), &omitted))
	delete(omitted, "deposit")
	body, err := json.Marshal(omitted)
	require.NoError(t, err)
	_, err = cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
	require.Error(t, err)
}

func TestCustodyProposalCommandRefusesUnsupportedMessagesAndAuthorities(t *testing.T) {
	kr, proposer := newKeyring(t)
	cdc := testkeeper.EVMTestApp.AppCodec()
	governance := types.GovernanceAuthority()
	outsider := sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String()

	unsupported := map[string]sdk.Msg{
		"bank send": &banktypes.MsgSend{FromAddress: governance, ToAddress: outsider,
			Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", 1))},
		"custody user withdrawal": &types.MsgRequestWithdrawal{Sender: governance, Receipt: []byte{1}, Proof: []byte{1},
			Header: []byte{1}, HeaderSignature: []byte{1}},
	}
	for name, msg := range unsupported {
		t.Run(name, func(t *testing.T) {
			body := proposalFile(t, "Custody governance", "Unsupported", "1usei", msg)
			_, err := cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
			require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)
			output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
			require.Error(t, err)
			require.Empty(t, output)
		})
	}

	for i := range governanceMessages(t) {
		for _, bad := range []struct {
			authority string
			want      error
		}{{outsider, sdkerrors.ErrUnauthorized}, {"not-an-address", sdkerrors.ErrInvalidAddress}} {
			msgs := governanceMessages(t)
			setAuthority(t, msgs[i], bad.authority)
			body := proposalFile(t, "Custody governance", "Bad authority", "1usei", msgs...)
			_, err := cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
			require.ErrorIs(t, err, bad.want, "message %d authority %q", i, bad.authority)
			output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
			require.Error(t, err)
			require.Empty(t, output)
		}
	}
}

func setAuthority(t *testing.T, msg sdk.Msg, authority string) {
	t.Helper()
	switch m := msg.(type) {
	case *types.MsgUpdateParams:
		m.Authority = authority
	case *types.MsgSetAsset:
		m.Authority = authority
	case *types.MsgRegisterCheckpoint:
		m.Authority = authority
	case *types.MsgSetEmergency:
		m.Authority = authority
	case *types.MsgCancelClaim:
		m.Authority = authority
	default:
		t.Fatalf("unexpected message %T", msg)
	}
}

func TestCustodyProposalCommandRefusesMissingMetadataAndSigner(t *testing.T) {
	kr, proposer := newKeyring(t)
	cdc := testkeeper.EVMTestApp.AppCodec()
	msgs := governanceMessages(t)
	for name, body := range map[string][]byte{
		"no title":       proposalFile(t, "", "Every custody governance message", "1usei", msgs...),
		"no description": proposalFile(t, "Custody governance", "", "1usei", msgs...),
		"no messages":    proposalFile(t, "Custody governance", "Every custody governance message", "1usei"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cli.NewSubmitCustodyProposalMsg(cdc, body, proposer)
			require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)
			output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
			require.Error(t, err)
			require.Empty(t, output)
		})
	}

	valid := proposalFile(t, "Custody governance", "Every custody governance message", "1usei", msgs...)
	_, err := cli.NewSubmitCustodyProposalMsg(cdc, valid, nil)
	require.Error(t, err)
	output, err := runSubmit(t, kr, writeFile(t, valid), "--"+flags.FlagGenerateOnly, "--"+flags.FlagChainID, testChainID)
	require.Error(t, err)
	require.Empty(t, output)
	output, err = runSubmit(t, kr, writeFile(t, valid), "--"+flags.FlagFrom, "absent-key", "--"+flags.FlagGenerateOnly)
	require.Error(t, err)
	require.Empty(t, output)
}

func TestCustodyProposalTransactionSignsWithProposer(t *testing.T) {
	kr, proposer := newKeyring(t)
	app := testkeeper.EVMTestApp
	want := governanceMessages(t)
	body := proposalFile(t, "Custody governance", "Every custody governance message", "1000usei", want...)

	output, err := runSubmit(t, kr, generateArgs(writeFile(t, body))...)
	require.NoError(t, err)
	generated := decodeGenerated(t, output)
	unsigned, err := generated.GetSignaturesV2()
	require.NoError(t, err)
	require.Empty(t, unsigned, "generate-only output is unsigned")

	builder, err := app.GetTxConfig().WrapTxBuilder(generated)
	require.NoError(t, err)
	const accountNumber, sequence = uint64(11), uint64(3)
	txf := tx.Factory{}.
		WithTxConfig(app.GetTxConfig()).
		WithKeybase(kr).
		WithChainID(testChainID).
		WithAccountNumber(accountNumber).
		WithSequence(sequence).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, tx.Sign(txf, proposerKey, builder, true))

	signed := builder.GetTx()
	encoded, err := app.GetTxConfig().TxEncoder()(signed)
	require.NoError(t, err)
	decoded, err := app.GetTxConfig().TxDecoder()(encoded)
	require.NoError(t, err)
	final, ok := decoded.(authsigning.Tx)
	require.True(t, ok)

	submitted, ok := final.GetMsgs()[0].(*govtypes.MsgSubmitProposal)
	require.True(t, ok)
	requireCarriedProposal(t, submitted, proposer, sdk.NewCoins(sdk.NewInt64Coin("usei", 1000)), want)
	require.Equal(t, []sdk.AccAddress{proposer}, final.GetSigners())

	sigs, err := final.GetSignaturesV2()
	require.NoError(t, err)
	require.Len(t, sigs, 1)
	require.Equal(t, proposer, sdk.AccAddress(sigs[0].PubKey.Address()))
	require.Equal(t, sequence, sigs[0].Sequence)
	signerData := authsigning.SignerData{ChainID: testChainID, AccountNumber: accountNumber, Sequence: sequence}
	handler := app.GetTxConfig().SignModeHandler()
	require.NoError(t, authsigning.VerifySignature(sigs[0].PubKey, signerData, sigs[0].Data, handler, final))

	wrongChain := authsigning.SignerData{ChainID: testChainID + "-other", AccountNumber: accountNumber, Sequence: sequence}
	require.Error(t, authsigning.VerifySignature(sigs[0].PubKey, wrongChain, sigs[0].Data, handler, final))
	wrongAccount := authsigning.SignerData{ChainID: testChainID, AccountNumber: accountNumber + 1, Sequence: sequence}
	require.Error(t, authsigning.VerifySignature(sigs[0].PubKey, wrongAccount, sigs[0].Data, handler, final))

	otherKeys, _ := newKeyring(t)
	other, err := otherKeys.Key(proposerKey)
	require.NoError(t, err)
	require.Error(t, authsigning.VerifySignature(other.GetPubKey(), signerData, sigs[0].Data, handler, final))
}
