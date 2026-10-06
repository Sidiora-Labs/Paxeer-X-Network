package client

import (
	govclient "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/client/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/client/rest"
)

// ProposalHandler is the param change proposal handler.
var ProposalHandler = govclient.NewProposalHandler(cli.NewSubmitParamChangeProposalTxCmd, rest.ProposalRESTHandler)
