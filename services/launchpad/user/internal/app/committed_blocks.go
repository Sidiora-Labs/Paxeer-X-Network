package app

import (
	"context"

	"github.com/ethereum/go-ethereum/rpc"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// dialAccountChain opens the chain-125 JSON-RPC connection the account
// operation verifier reads through. The returned sharedauth.ReportedHashBackend
// identifies blocks by the node-reported hash, which is the hash the indexer
// commits to users.auth_committed_blocks: sharedauth.CommittedProofVerifier
// requires the node to report the committed hash for the committed height and
// reads code and ERC-1271 results by that height with the reported hash
// checked before and after. On Paxeer (CometBFT-backed) go-ethereum's
// recomputed header hash names no block, so it is never used.
func dialAccountChain(ctx context.Context, endpoint string) (*sharedauth.ReportedHashBackend, error) {
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return sharedauth.NewReportedHashBackend(client), nil
}
