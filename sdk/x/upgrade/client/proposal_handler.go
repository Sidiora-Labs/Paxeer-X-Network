package client

import (
	govclient "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/client/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/client/rest"
)

var ProposalHandler = govclient.NewProposalHandler(cli.NewCmdSubmitUpgradeProposal, rest.ProposalRESTHandler)
var CancelProposalHandler = govclient.NewProposalHandler(cli.NewCmdSubmitCancelUpgradeProposal, rest.ProposalCancelRESTHandler)
