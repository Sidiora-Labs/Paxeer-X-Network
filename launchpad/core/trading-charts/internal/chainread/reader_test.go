package chainread

import (
	"context"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// TestReportedIdentityLive reads Paxeer through the node-reported block
// identity: the reported hash differs from go-ethereum's recomputed header
// hash, reads bound to it succeed, and reads bound to any other hash fail.
func TestReportedIdentityLive(t *testing.T) {
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
	height := int64(head) - 16
	reader := Reader{RPC: client}
	block, err := reader.Block(ctx, height)
	if err != nil {
		t.Fatal(err)
	}
	header, err := client.HeaderByNumber(ctx, big.NewInt(height))
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != height || block.Time != int64(header.Time) || block.Hash != strings.ToLower(block.Hash) {
		t.Fatal("reported block identity malformed", block)
	}
	if block.Hash == header.Hash().Hex() {
		t.Fatal("chain 125 reported hash unexpectedly equals the recomputed header hash")
	}
	usdl := common.HexToAddress("0x85fcd13735f4309833a503ee804ea32395851479")
	decimals := ethereum.CallMsg{To: &usdl, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}
	values, err := reader.CallAt(ctx, height, block.Hash, []ethereum.CallMsg{decimals})
	if err != nil || len(values) != 1 || new(big.Int).SetBytes(values[0]).Int64() != 6 {
		t.Fatal("pinned USDL decimals read failed", values, err)
	}
	if _, err = reader.CallAt(ctx, height, header.Hash().Hex(), []ethereum.CallMsg{decimals}); !errors.Is(err, ErrConflict) {
		t.Fatal("read bound to the recomputed header hash was not refused", err)
	}
	if _, err = reader.Block(ctx, int64(head)+1_000_000); !errors.Is(err, ErrUnavailable) {
		t.Fatal("future block not reported unavailable", err)
	}
}
