package testutil

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	clitestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/network"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/authz/client/cli"
)

func ExecGrant(val *network.Validator, args []string) (testutil.BufferWriter, error) {
	cmd := cli.NewCmdGrantAuthorization()
	clientCtx := val.ClientCtx
	return clitestutil.ExecTestCLICmd(clientCtx, cmd, args)
}
