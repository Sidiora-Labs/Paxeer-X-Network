package control

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// cometNode is a chain-125 JSON-RPC node that, like Paxeer (CometBFT-backed),
// reports block hashes that differ from go-ethereum's recomputed Header.Hash().
type cometNode struct {
	mu           sync.Mutex
	header       *types.Header
	receiptBlock common.Hash
	balanceAt    []string
}

func (n *cometNode) hash() common.Hash {
	return crypto.Keccak256Hash([]byte("cometbft"), n.header.Hash().Bytes())
}

func (n *cometNode) serve(t *testing.T) *ethclient.Client {
	settlement := common.HexToHash("0xab00000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		n.mu.Lock()
		defer n.mu.Unlock()
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(req.Params[0], &tag)
			if tag == "latest" || tag == hexutil.EncodeBig(n.header.Number) {
				encoded, _ := json.Marshal(n.header)
				var out map[string]any
				json.Unmarshal(encoded, &out)
				out["hash"] = n.hash().Hex()
				result = out
			}
		case "eth_getBalance":
			var at string
			json.Unmarshal(req.Params[1], &at)
			n.balanceAt = append(n.balanceAt, at)
			result = "0x64"
		case "eth_getTransactionReceipt":
			log := &types.Log{Address: common.HexToAddress(USDLAddress), Topics: []common.Hash{common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"), common.BytesToHash(common.HexToAddress(custody).Bytes()), common.BytesToHash(common.HexToAddress(beneficiary).Bytes())}, Data: common.LeftPadBytes(big.NewInt(80).Bytes(), 32), BlockNumber: n.header.Number.Uint64(), TxHash: settlement, BlockHash: n.receiptBlock}
			result = &types.Receipt{Type: 2, Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 50000, GasUsed: 50000, Logs: []*types.Log{log}, TxHash: settlement, BlockHash: n.receiptBlock, BlockNumber: n.header.Number}
		default:
			t.Error("unexpected method", req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	client, err := ethclient.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestChainVerifierUsesNodeReportedBlockHash(t *testing.T) {
	node := &cometNode{header: &types.Header{Number: big.NewInt(33963716), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: 1, Extra: []byte{}}}
	if node.hash() == node.header.Hash() {
		t.Fatal("fixture must report a hash different from Header.Hash()")
	}
	v, err := NewChainVerifier(node.serve(t), custody, fixtureManifest, fixtureAuthority{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	o, err := v.Inventory(ctx, pax)
	if err != nil || o.Reference != node.hash().Hex() || o.Balance != "100" {
		t.Fatalf("inventory reference must be the node-reported hash: %+v %v", o, err)
	}
	if len(node.balanceAt) != 1 || node.balanceAt[0] != hexutil.EncodeBig(node.header.Number) {
		t.Fatalf("balance must be read by the pinned height: %v", node.balanceAt)
	}
	reference := "0xab00000000000000000000000000000000000000000000000000000000000001"
	obligation := Obligation{Amount: "80", Asset: usdl, Destination: beneficiary}
	node.mu.Lock()
	node.receiptBlock = node.hash()
	node.mu.Unlock()
	if evidence, err := v.Settlement(ctx, obligation, reference); err != nil || !evidence.Successful {
		t.Fatalf("settlement in the node-reported block: %+v %v", evidence, err)
	}
	node.mu.Lock()
	node.receiptBlock = node.header.Hash()
	node.mu.Unlock()
	if _, err := v.Settlement(ctx, obligation, reference); err != Denied {
		t.Fatalf("receipt naming the recomputed header hash = %v, want Denied", err)
	}
}
