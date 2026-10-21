package chainreader

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// On a CometBFT-backed chain (Paxeer) a receipt names the node-reported block
// hash, which differs from go-ethereum's recomputed Header.Hash(). Resolution
// must bind the receipt to the node-reported hash of its height.
func TestResolveBindsReceiptToNodeReportedBlockHash(t *testing.T) {
	catalog, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := decode.NewRegistry(catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	header := &ethtypes.Header{Number: big.NewInt(33963716), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: 1, Extra: []byte{}}
	consensus := crypto.Keccak256Hash([]byte("cometbft"), header.Hash().Bytes())
	txHash := common.HexToHash("0xe200486fb275ad363101dd09455b23842f1a002b839062732230fd4c908cbfca")
	receiptBlock := consensus
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getTransactionReceipt":
			receipt := &ethtypes.Receipt{Type: 2, Status: 0, CumulativeGasUsed: 21000, GasUsed: 21000, Logs: []*ethtypes.Log{}, TxHash: txHash, BlockHash: receiptBlock, BlockNumber: header.Number}
			result = receipt
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(req.Params[0], &tag)
			if tag == hexutil.EncodeBig(header.Number) {
				encoded, _ := json.Marshal(header)
				var out map[string]any
				json.Unmarshal(encoded, &out)
				out["hash"] = consensus.Hex()
				result = out
			}
		default:
			t.Error("unexpected method", req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	client, err := ethclient.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	resolver, err := NewCreationResolver(context.Background(), client, registry)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolver.Resolve(context.Background(), 125, txHash.Hex(), nil)
	if err != nil || out.Status != "failed" || out.BlockHash == nil || *out.BlockHash != strings.ToLower(consensus.Hex()) {
		t.Fatalf("receipt under node-reported hash: %+v %v", out, err)
	}
	// A receipt naming the recomputed header hash names no block on chain 125.
	receiptBlock = header.Hash()
	if out, err := resolver.Resolve(context.Background(), 125, txHash.Hex(), nil); err == nil || out.Status != "integrity_error" {
		t.Fatalf("recomputed hash accepted: %+v %v", out, err)
	}
}
