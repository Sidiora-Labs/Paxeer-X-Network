package main

import (
	"os"

	"github.com/Sidiora-Labs/Paxeer-X-Network/daemon/paxd/cmd"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/params"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	svrcmd "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/cmd"
)

func main() {
	params.SetAddressPrefixes()
	rootCmd, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
