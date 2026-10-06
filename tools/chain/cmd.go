package tools

import (
	"github.com/spf13/cobra"

	scanner "github.com/Sidiora-Labs/Paxeer-X-Network/tools/tx-scanner/cmd"
)

func ToolCmd() *cobra.Command {
	toolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "A set of useful tools for pax chain",
	}
	toolsCmd.AddCommand(scanner.ScanCmd())
	return toolsCmd
}
