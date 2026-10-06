// Package cli carries the xweb module's transaction command: one subcommand
// per authority message. Each builds its message from the command's arguments
// with the signer the standard transaction flags name as the authority, so the
// same message the governance route carries is also sendable by the account
// the module's authority parameter records.
package cli

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/flags"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// GetTxCmd returns the xweb transaction command and its authority subcommands.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewRegisterAttestorCmd(),
		NewRemoveAttestorCmd(),
		NewSetThresholdCmd(),
		NewSetParamsCmd(),
		NewPauseCmd(),
		NewUnpauseCmd(),
	)

	return cmd
}

// NewRegisterAttestorCmd sends MsgRegisterAttestor. The public key is the
// attestor's 33-byte compressed secp256k1 key and is given only for an
// attestor that accepts api credential envelopes.
func NewRegisterAttestorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-attestor [signer] [payout] [public-key]",
		Short: "Register one web attestor with its payout account",
		Long: "Register one web attestor.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " register-attestor 0x… pax1… --from [key]\n" +
			"The signer is the EVM address the attestor's signatures recover to, the payout is the bech32\n" +
			"account its share of each fee is paid to, and the optional public key is the 33-byte compressed\n" +
			"secp256k1 key of the same signing key in hex, which api credential envelopes are sealed to.",
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			signer, err := parseSigner(args[0])
			if err != nil {
				return err
			}
			attestor := types.Attestor{Signer: signer, Payout: args[1]}
			if len(args) == 3 {
				if attestor.PublicKey, err = parseHexBytes(args[2]); err != nil {
					return fmt.Errorf("the public key %q: %w", args[2], err)
				}
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgRegisterAttestor{Authority: authority, Attestor: attestor}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewRemoveAttestorCmd sends MsgRemoveAttestor.
func NewRemoveAttestorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-attestor [signer]",
		Short: "Remove the registered web attestor with this signer",
		Long: "Remove one registered web attestor.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " remove-attestor 0x… --from [key]\n" +
			"The signer is the EVM address the attestor was registered under.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			signer, err := parseSigner(args[0])
			if err != nil {
				return err
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgRemoveAttestor{Authority: authority, Signer: signer}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewSetThresholdCmd sends MsgSetThreshold.
func NewSetThresholdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-threshold [threshold]",
		Short: "Set the number of attestor signatures a fulfilment needs",
		Long: "Set the attestor threshold.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " set-threshold 3 --from [key]\n" +
			"The threshold must be above half the registered set and at most its size.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			threshold, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("the threshold %q: %w", args[0], err)
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgSetThreshold{Authority: authority, Threshold: uint32(threshold)}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewSetParamsCmd sends MsgSetParams.
func NewSetParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-params [fee] [max-payload-bytes] [max-callback-gas] [timeout-blocks]",
		Short: "Set the request fee, the payload and callback caps and the timeout",
		Long: "Set the module's settable parameters.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " set-params 1000000 8192 500000 3600 --from [key]\n" +
			"The fee is the amount in base units one request pays, the caps bound one request's payload and\n" +
			"its callback gas, and the timeout is the number of blocks after which an unfulfilled request is\n" +
			"refundable. The authority itself is unchanged.",
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			fee, ok := sdk.NewIntFromString(args[0])
			if !ok {
				return fmt.Errorf("the fee %q is not an integer", args[0])
			}
			maxPayloadBytes, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("the payload cap %q: %w", args[1], err)
			}
			maxCallbackGas, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("the callback cap %q: %w", args[2], err)
			}
			timeoutBlocks, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return fmt.Errorf("the timeout %q: %w", args[3], err)
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgSetParams{Authority: authority, Fee: fee,
					MaxPayloadBytes: uint32(maxPayloadBytes), MaxCallbackGas: maxCallbackGas,
					TimeoutBlocks: timeoutBlocks}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewPauseCmd sends MsgPause.
func NewPauseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Stop every request and fulfilment; refunds stay open",
		Long: "Pause the module.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " pause --from [key]",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgPause{Authority: authority}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewUnpauseCmd sends MsgUnpause.
func NewUnpauseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpause",
		Short: "Resume requests and fulfilments",
		Long: "Unpause the module.\n" +
			"E.g. $ paxd tx " + types.ModuleName + " unpause --from [key]",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			return submit(cmd, clientCtx, func(authority string) sdk.Msg {
				return &types.MsgUnpause{Authority: authority}
			})
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// submit names the signer the standard transaction flags carry as the
// message's authority and broadcasts it once it passes its own ValidateBasic.
func submit(cmd *cobra.Command, clientCtx client.Context, build func(authority string) sdk.Msg) error {
	from := clientCtx.GetFromAddress()
	if from.Empty() {
		return fmt.Errorf("the authority is missing: name the signer with --%s", flags.FlagFrom)
	}
	msg := build(from.String())
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
}

// parseSigner reads an attestor's EVM signer address.
func parseSigner(arg string) (types.Address20, error) {
	if !common.IsHexAddress(arg) {
		return types.Address20{}, fmt.Errorf("the signer %q is not a 20-byte hex address", arg)
	}
	return types.Address20(common.HexToAddress(arg)), nil
}

// parseHexBytes reads hex with or without the 0x prefix.
func parseHexBytes(arg string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(arg, "0x"), "0X"))
}
