// Package chainreader implements the on-chain reads the indexer needs while
// enriching events — currently only PoolRegistry.getNftIdByPool for the
// MarketCreated handler. It binds the protocol/ abigen bindings to the shared
// failover-aware chain client.
package chainreader

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain"
)

// NFTReader resolves a pool's NFT id via PoolRegistry.getNftIdByPool.
type NFTReader struct {
	caller *bindings.PoolRegistryCaller
}

// NewNFTReader binds the PoolRegistry contract at registryAddr to the chain
// client's primary RPC.
func NewNFTReader(client *chain.Client, registryAddr string) (*NFTReader, error) {
	caller, err := bindings.NewPoolRegistryCaller(common.HexToAddress(registryAddr), client.Eth())
	if err != nil {
		return nil, fmt.Errorf("chainreader: bind PoolRegistry: %w", err)
	}
	return &NFTReader{caller: caller}, nil
}

// NftIDByPool returns the NFT id minted for a pool (or an error the caller
// treats as a null nft_id).
func (r *NFTReader) NftIDByPool(ctx context.Context, pool common.Address) (*big.Int, error) {
	return r.caller.GetNftIdByPool(&bind.CallOpts{Context: ctx}, pool)
}
