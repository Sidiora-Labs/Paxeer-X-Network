package httpapi

import (
	"context"
	"math/big"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/ethclient"
)

// TestRPCSupplySourceLive values the live Sidiora token at a recent block bound
// to the node-reported (CometBFT) block hash the indexer records on Paxeer.
func TestRPCSupplySourceLive(t *testing.T) {
	rpcURL := os.Getenv("NETWORK_RPC_URL")
	if rpcURL == "" {
		t.Fatal("NETWORK_RPC_URL must name the Paxeer RPC")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	head, err := client.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := &chainread.Reader{RPC: client}
	block, err := reader.Block(ctx, int64(head)-16)
	if err != nil {
		t.Fatal(err)
	}
	token := "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	position := interfaces.ChainPosition{ChainId: "125", Height: interfaces.UInt256(strconv.FormatInt(block.Height, 10)), BlockHash: block.Hash, TransactionHash: "0x" + "00", TransactionIndex: "0", LogIndex: "0"}
	snapshot, err := (RPCSupplySource{Chain: reader}).At(ctx, token, position)
	if err != nil {
		t.Fatal(err)
	}
	supply, ok := new(big.Int).SetString(snapshot.TotalSupplyRaw, 10)
	if !ok || supply.Sign() <= 0 || snapshot.Decimals != 6 || snapshot.SourceHash != block.Hash || snapshot.SourceBlock != strconv.FormatInt(block.Height, 10) || snapshot.Alignment != "canonical_block_end" {
		t.Fatal("live supply snapshot", snapshot)
	}
	position.BlockHash = "0x" + "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	if _, err = (RPCSupplySource{Chain: reader}).At(ctx, token, position); err == nil {
		t.Fatal("supply bound to a foreign block hash was served")
	}
}
