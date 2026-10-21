package l16

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// A real Paxeer chain-125 block header as returned by eth_getBlockByNumber
// (height 33963460, captured from api2.mainnet-beta.paxeer.network). Its
// reported hash is CometBFT-derived; go-ethereum's header.Hash() of the same
// fields is 0x698b830a...c21e.
const paxeerHeaderJSON = `{"baseFeePerGas":"0x3b9aca00","difficulty":"0x0","extraData":"0x","gasLimit":"0x5f5e100","gasUsed":"0x0","hash":"0xd8db8045348a8d4dbddff36eae10f504a99f0eca3e1f7c5ed531963e8d204cc8","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","miner":"0xb3ad5a835b418958a9142d8681bfb4b81d2cf270","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","nonce":"0x0000000000000000","number":"0x2063dc4","parentHash":"0xa152b5a9f8fa0f190d531186721ce4162bf83f1426b31bdbed3f40b9184812d3","receiptsRoot":"0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347","size":"0x33f","stateRoot":"0xed516e3ade134a0feba7b14fcf19234a345b7a9dfd400062bbb4562a518da36f","timestamp":"0x6acdd9b8","transactions":[],"transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","uncles":[]}`

func TestDecodeReportedHeaderUsesNodeHash(t *testing.T) {
	header, reported, err := decodeReportedHeader(json.RawMessage(paxeerHeaderJSON), big.NewInt(33963460))
	if err != nil {
		t.Fatal(err)
	}
	if reported != "0xd8db8045348a8d4dbddff36eae10f504a99f0eca3e1f7c5ed531963e8d204cc8" {
		t.Fatalf("reported hash %s", reported)
	}
	if header.Number.Int64() != 33963460 || header.Time != 0x6acdd9b8 {
		t.Fatalf("header fields %v %d", header.Number, header.Time)
	}
	if computed := header.Hash().Hex(); computed == common.HexToHash(reported).Hex() {
		t.Fatalf("fixture no longer exercises a Cosmos-EVM hash: computed %s equals reported", computed)
	}
	if _, _, err = decodeReportedHeader(json.RawMessage(paxeerHeaderJSON), big.NewInt(33963461)); !errors.Is(err, delivery.ErrIntegrity) {
		t.Fatalf("height substitution accepted: %v", err)
	}
	var withoutHash map[string]any
	if err = json.Unmarshal([]byte(paxeerHeaderJSON), &withoutHash); err != nil {
		t.Fatal(err)
	}
	delete(withoutHash, "hash")
	raw, _ := json.Marshal(withoutHash)
	if _, _, err = decodeReportedHeader(raw, big.NewInt(33963460)); !errors.Is(err, delivery.ErrIntegrity) {
		t.Fatalf("header without reported hash accepted: %v", err)
	}
	if _, _, err = decodeReportedHeader(json.RawMessage("null"), big.NewInt(1)); err == nil {
		t.Fatal("missing block accepted")
	}
}

// TestReportedHeaderAgainstPaxeer reads a recent block from the live chain-125
// RPC named by NETWORK_RPC_URL and proves that the node-reported hash is the
// one the node accepts for EIP-1898 pinned reads and eth_getBlockByHash, which
// is what the live rebuild compares against the indexer's recorded hashes.
func TestReportedHeaderAgainstPaxeer(t *testing.T) {
	endpoint := os.Getenv("NETWORK_RPC_URL")
	if endpoint == "" {
		t.Skip("NETWORK_RPC_URL not set: live chain-125 read skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	chain, err := client.ChainID(ctx)
	if err != nil || chain.Int64() != 125 {
		t.Fatalf("chain %v %v", chain, err)
	}
	head, err := client.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	number := new(big.Int).SetUint64(head - 16)
	header, reported, err := reportedHeaderByNumber(ctx, client, number)
	if err != nil {
		t.Fatal(err)
	}
	byHash, err := client.HeaderByHash(ctx, common.HexToHash(reported))
	if err != nil || byHash == nil || byHash.Number.Cmp(number) != 0 || byHash.Time != header.Time {
		t.Fatalf("node does not resolve its reported hash: %v %v", byHash, err)
	}
	usdl := common.HexToAddress("0x85fcd13735f4309833a503ee804ea32395851479")
	code, err := client.CodeAtHash(ctx, usdl, common.HexToHash(reported))
	if err != nil || len(code) == 0 {
		t.Fatalf("pinned read by reported hash: %d bytes, %v", len(code), err)
	}
	block, err := reportedBlockByNumber(ctx, client, number)
	if err != nil {
		t.Fatal(err)
	}
	if block.Hash.Hex() != common.HexToHash(reported).Hex() || block.Number.Cmp(number) != 0 {
		t.Fatalf("block view %s/%v differs from header view %s", block.Hash.Hex(), block.Number, reported)
	}
	for index, transaction := range block.Transactions {
		receipt, err := client.TransactionReceipt(ctx, transaction)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.BlockHash != block.Hash || int(receipt.TransactionIndex) != index {
			t.Fatalf("receipt %s names block %s index %d, block reports %s", transaction.Hex(), receipt.BlockHash.Hex(), receipt.TransactionIndex, block.Hash.Hex())
		}
	}
}
