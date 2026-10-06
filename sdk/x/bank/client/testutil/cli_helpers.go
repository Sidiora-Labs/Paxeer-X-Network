package testutil

import (
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/cli"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	clitestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/cli"
	bankcli "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/client/cli"
)

func MsgSendExec(clientCtx client.Context, from, to, amount fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := make([]string, 0, 3+len(extraArgs))
	args = append(args, from.String(), to.String(), amount.String())
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.NewSendTxCmd(), args)
}

func QueryBalancesExec(clientCtx client.Context, address fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := make([]string, 0, 2+len(extraArgs))
	args = append(args, address.String(), fmt.Sprintf("--%s=json", cli.OutputFlag))
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.GetBalancesCmd(), args)
}
