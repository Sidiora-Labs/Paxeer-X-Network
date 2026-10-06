package client

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/distribution/client/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/distribution/client/rest"
	govclient "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client"
)

// ProposalHandler is the community spend proposal handler.
var (
	ProposalHandler = govclient.NewProposalHandler(cli.GetCmdSubmitProposal, rest.ProposalRESTHandler)
)
