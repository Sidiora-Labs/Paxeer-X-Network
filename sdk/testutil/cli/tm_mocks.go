package cli

import (
	"context"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmbytes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/bytes"
	rpcclient "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client"
	rpcclientmock "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client/mock"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
)

var _ client.TendermintRPC = (*MockTendermintRPC)(nil)

type MockTendermintRPC struct {
	rpcclientmock.Client

	responseQuery abci.ResponseQuery
}

// NewMockTendermintRPC returns a mock TendermintRPC implementation.
// It is used for CLI testing.
func NewMockTendermintRPC(respQuery abci.ResponseQuery, client rpcclientmock.Client) MockTendermintRPC {
	return MockTendermintRPC{
		Client:        client,
		responseQuery: respQuery,
	}
}

func (MockTendermintRPC) BroadcastTxSync(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return &coretypes.ResultBroadcastTx{Code: 0}, nil
}

func (m MockTendermintRPC) ABCIQueryWithOptions(
	_ context.Context,
	_ string,
	_ tmbytes.HexBytes,
	_ rpcclient.ABCIQueryOptions,
) (*coretypes.ResultABCIQuery, error) {
	return &coretypes.ResultABCIQuery{Response: m.responseQuery}, nil
}
