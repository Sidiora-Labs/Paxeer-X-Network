package main

import (
	"os"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server"
	svrcmd "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/cmd"

	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/testing/simapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/testing/simapp/simd/cmd"
)

func main() {
	rootCmd, _ := cmd.NewRootCmd()

	if err := svrcmd.Execute(rootCmd, simapp.DefaultNodeHome); err != nil {
		switch e := err.(type) {
		case server.ErrorCode:
			os.Exit(e.Code)

		default:
			os.Exit(1)
		}
	}
}
