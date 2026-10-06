package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	typesrest "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/rest"
	govclient "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client"
	govcli "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client/cli"
	govrest "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client/rest"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
)

const (
	ProposalCommandName = "layerx-proposal"

	ProposalRESTSubRoute = "layerx"
)

var LayerXProposalHandler = govclient.NewProposalHandler(NewSubmitLayerXProposalCmd, LayerXProposalRESTHandler)

func NewSubmitLayerXProposalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   ProposalCommandName + " [proposal-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Submit a LayerX authority-message proposal",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			contents, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			depositInput, err := cmd.Flags().GetString(govcli.FlagDeposit)
			if err != nil {
				return err
			}

			msg, err := NewSubmitLayerXProposalMsg(clientCtx.Codec, contents, depositInput, clientCtx.GetFromAddress())
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(govcli.FlagDeposit, "", "The proposal deposit")

	return cmd
}

func NewSubmitLayerXProposalMsg(cdc codec.JSONCodec, body []byte, depositInput string, proposer sdk.AccAddress) (*govtypes.MsgSubmitProposal, error) {
	content, err := DecodeLayerXProposal(cdc, body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(depositInput) == "" {
		return nil, fmt.Errorf("the proposal deposit is missing: pass --%s", govcli.FlagDeposit)
	}
	deposit, err := sdk.ParseCoinsNormalized(depositInput)
	if err != nil {
		return nil, fmt.Errorf("the proposal deposit %q: %w", depositInput, err)
	}
	if deposit.IsZero() {
		return nil, fmt.Errorf("the proposal deposit %q is zero", depositInput)
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

func DecodeLayerXProposal(cdc codec.JSONCodec, body []byte) (*types.LayerXProposal, error) {
	var content govtypes.Content
	if err := cdc.UnmarshalInterfaceJSON(body, &content); err != nil {
		return nil, fmt.Errorf("decode the proposal: %w", err)
	}
	proposal, ok := content.(*types.LayerXProposal)
	if !ok {
		return nil, fmt.Errorf("the proposal is a %T, not a %T", content, proposal)
	}
	written, err := cdc.MarshalInterfaceJSON(proposal)
	if err != nil {
		return nil, fmt.Errorf("encode the decoded proposal: %w", err)
	}
	var given, want any
	if err := decodeJSON(body, &given); err != nil {
		return nil, fmt.Errorf("decode the proposal: %w", err)
	}
	if err := decodeJSON(written, &want); err != nil {
		return nil, fmt.Errorf("decode the encoded proposal: %w", err)
	}
	if err := requireFields("", given, want); err != nil {
		return nil, err
	}
	if err := proposal.ValidateBasic(); err != nil {
		return nil, err
	}
	return proposal, nil
}

func decodeJSON(raw []byte, out *any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("more than one JSON value")
	}
	return nil
}

func requireFields(path string, given, want any) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := given.(map[string]any)
		if !ok {
			return fmt.Errorf("the field %s is not an object", fieldName(path))
		}
		keys := make([]string, 0, len(w))
		for key := range w {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, present := g[key]
			if !present {
				return fmt.Errorf("the field %s is missing", fieldName(path+"."+key))
			}
			if err := requireFields(path+"."+key, value, w[key]); err != nil {
				return err
			}
		}
	case []any:
		g, ok := given.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("the field %s does not carry %d entries", fieldName(path), len(w))
		}
		for i := range w {
			if err := requireFields(fmt.Sprintf("%s[%d]", path, i), g[i], w[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldName(path string) string {
	if path == "" {
		return "(document)"
	}
	return strings.TrimPrefix(path, ".")
}

type LayerXProposalRequest struct {
	BaseReq  typesrest.BaseReq `json:"base_req" yaml:"base_req"`
	Deposit  string            `json:"deposit" yaml:"deposit"`
	Proposal json.RawMessage   `json:"proposal" yaml:"proposal"`
}

func LayerXProposalRESTHandler(clientCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: ProposalRESTSubRoute,
		Handler:  newLayerXProposalPostHandler(clientCtx),
	}
}

func newLayerXProposalPostHandler(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if typesrest.CheckBadRequestError(w, err) {
			return
		}
		var req LayerXProposalRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if typesrest.CheckBadRequestError(w, decoder.Decode(&req)) {
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			typesrest.WriteErrorResponse(w, http.StatusBadRequest, "more than one JSON value")
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		fromAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if typesrest.CheckBadRequestError(w, err) {
			return
		}

		msg, err := NewSubmitLayerXProposalMsg(clientCtx.Codec, req.Proposal, req.Deposit, fromAddr)
		if typesrest.CheckBadRequestError(w, err) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}
