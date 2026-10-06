package testutil

import (
	"context"
	"fmt"

	tmcfg "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/cli"
	"github.com/spf13/viper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	genutilcli "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/genutil/client/cli"
)

func ExecInitCmd(testMbm module.BasicManager, home string, cdc codec.Codec) error {

	cfg, err := CreateDefaultTendermintConfig(home)
	if err != nil {
		return err
	}

	cmd := genutilcli.InitCmd(testMbm, home)
	serverCtx := server.NewContext(viper.New(), cfg)
	clientCtx := client.Context{}.WithCodec(cdc).WithHomeDir(home)

	_, out := testutil.ApplyMockIO(cmd)
	clientCtx = clientCtx.WithOutput(out)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)

	cmd.SetArgs([]string{"appnode-test", fmt.Sprintf("--%s=%s", cli.HomeFlag, home)})

	return cmd.ExecuteContext(ctx)
}

func CreateDefaultTendermintConfig(rootDir string) (*tmcfg.Config, error) {
	conf := tmcfg.DefaultConfig()
	conf.SetRoot(rootDir)
	tmcfg.EnsureRoot(rootDir)

	if err := conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("error in config file: %v", err)
	}

	return conf, nil
}
