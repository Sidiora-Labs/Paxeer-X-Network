// Package cli is the custody module's transaction command. The module's
// authority-gated messages are sent only by governance, so the command's one
// subcommand submits a custody proposal file as a governance proposal signed
// by the proposer.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	"github.com/sidiora-labs/paxeer-network/sdk/client"
	"github.com/sidiora-labs/paxeer-network/sdk/client/flags"
	"github.com/sidiora-labs/paxeer-network/sdk/client/tx"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
)

// ProposalFile is the custody proposal file: the proposal's title and
// description, the deposit in the form paxd takes coins, and every message in
// execution order, each as its protobuf JSON under its "@type".
type ProposalFile struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Deposit     string            `json:"deposit"`
	Messages    []json.RawMessage `json:"messages"`
}

// GetTxCmd returns paxd tx layerxcustody with its submit-proposal custody
// subcommand.
func GetTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Custody transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	submit := &cobra.Command{
		Use:                        "submit-proposal",
		Short:                      "Submit a custody governance proposal",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	submit.AddCommand(NewSubmitCustodyProposalCmd())
	txCmd.AddCommand(submit)
	return txCmd
}

// NewSubmitCustodyProposalCmd submits one custody proposal file, signed by
// the proposer the standard transaction flags name.
func NewSubmitCustodyProposalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "custody [proposal-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Submit a custody proposal file",
		Long: "Submit a custody governance proposal.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " submit-proposal custody proposal.json --from [key]\n" +
			"The file carries title, description, deposit and messages; every message is a custody\n" +
			"authority-gated message under its @type naming the governance module account as authority.\n" +
			"A file with an unknown field is refused.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			body, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			msg, err := NewSubmitCustodyProposalMsg(clientCtx.Codec, body, clientCtx.GetFromAddress())
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSubmitCustodyProposalMsg decodes a custody proposal file into the
// MsgSubmitProposal that carries it. It refuses a file with an unknown field,
// a file carrying anything but whitespace after the proposal, a message that
// is not one of the module's authority-gated messages, a
// proposal the governance route would refuse, a missing or zero deposit and a
// missing proposer.
func NewSubmitCustodyProposalMsg(cdc codec.JSONCodec, body []byte, proposer sdk.AccAddress) (*govtypes.MsgSubmitProposal, error) {
	var file ProposalFile
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode the proposal: %w", err)
	}
	if err := decoder.Decode(&json.RawMessage{}); err != io.EOF {
		return nil, fmt.Errorf("decode the proposal: the file carries more than one JSON value")
	}
	msgs := make([]sdk.Msg, 0, len(file.Messages))
	for i, raw := range file.Messages {
		var msg sdk.Msg
		if err := cdc.UnmarshalInterfaceJSON(raw, &msg); err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		msgs = append(msgs, msg)
	}
	content, err := types.NewCustodyProposal(file.Title, file.Description, msgs...)
	if err != nil {
		return nil, err
	}
	if err := content.ValidateBasic(); err != nil {
		return nil, err
	}
	deposit, err := sdk.ParseCoinsNormalized(file.Deposit)
	if err != nil {
		return nil, fmt.Errorf("the proposal deposit %q: %w", file.Deposit, err)
	}
	if deposit.IsZero() {
		return nil, fmt.Errorf("the proposal deposit %q is missing or zero", file.Deposit)
	}
	if proposer.Empty() {
		return nil, fmt.Errorf("the proposer is missing: name the signer with --from")
	}
	msg, err := govtypes.NewMsgSubmitProposal(content, deposit, proposer)
	if err != nil {
		return nil, err
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	return msg, nil
}
