package capabilities

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// cometChain is a JSON-RPC chain-125 node with one block (and its parent) that
// reports consensus hashes different from go-ethereum's recomputed header
// hashes and, like Paxeer, refuses EIP-1898 {blockHash} state reads by any
// other hash.
type cometChain struct {
	t       *testing.T
	block   *types.Header
	parent  *types.Header
	tx      *types.Transaction
	mu      sync.Mutex
	heights []string
}

func cometHash(h *types.Header) common.Hash {
	return crypto.Keccak256Hash([]byte("cometbft"), h.Hash().Bytes())
}

func (c *cometChain) blockJSON(h *types.Header, full bool) map[string]any {
	encoded, _ := json.Marshal(h)
	var out map[string]any
	json.Unmarshal(encoded, &out)
	out["hash"] = cometHash(h).Hex()
	txs := []any{}
	if h == c.block && c.tx != nil {
		if full {
			raw, _ := json.Marshal(c.tx)
			var tx map[string]any
			json.Unmarshal(raw, &tx)
			tx["blockHash"] = cometHash(h).Hex()
			tx["blockNumber"] = hexutil.EncodeBig(h.Number)
			tx["transactionIndex"] = "0x0"
			txs = append(txs, tx)
		} else {
			txs = append(txs, c.tx.Hash().Hex())
		}
	}
	out["transactions"] = txs
	return out
}

func (c *cometChain) serve() *rpc.Client {
	t := c.t
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		reply := func(result any) {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
		switch req.Method {
		case "eth_chainId":
			reply("0x7d")
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(req.Params[0], &tag)
			for _, h := range []*types.Header{c.block, c.parent} {
				if tag == hexutil.EncodeBig(h.Number) {
					reply(c.blockJSON(h, false))
					return
				}
			}
			reply(nil)
		case "eth_getBlockByHash":
			var hash common.Hash
			var full bool
			json.Unmarshal(req.Params[0], &hash)
			json.Unmarshal(req.Params[1], &full)
			for _, h := range []*types.Header{c.block, c.parent} {
				if hash == cometHash(h) {
					reply(c.blockJSON(h, full))
					return
				}
			}
			reply(nil)
		case "eth_getCode", "eth_call", "eth_getStorageAt":
			at := req.Params[len(req.Params)-1]
			if strings.Contains(string(at), "blockHash") {
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "block not found by hash"}})
				return
			}
			var height string
			json.Unmarshal(at, &height)
			c.mu.Lock()
			c.heights = append(c.heights, height)
			c.mu.Unlock()
			if req.Method == "eth_getCode" {
				reply("0x6080")
			} else {
				reply(hexutil.Bytes(common.LeftPadBytes([]byte{0x2a}, 32)))
			}
		default:
			t.Error("unexpected method", req.Method)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := rpc.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func newCometChain(t *testing.T) *cometChain {
	parent := &types.Header{Number: big.NewInt(33963715), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: 1000, Extra: []byte{}}
	block := &types.Header{Number: big.NewInt(33963716), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: 1001, Extra: []byte{}}
	block.ParentHash = cometHash(parent)
	key, _ := crypto.GenerateKey()
	to := common.HexToAddress("0x1")
	tx, err := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(125)), &types.DynamicFeeTx{ChainID: big.NewInt(125), Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 21000, To: &to, Data: []byte{1, 2, 3, 4}})
	if err != nil {
		t.Fatal(err)
	}
	return &cometChain{t: t, block: block, parent: parent, tx: tx}
}

func TestPinnedRPCReaderReportsNodeBlockIdentity(t *testing.T) {
	chain := newCometChain(t)
	reader, err := NewPinnedRPCReader(chain.serve())
	if err != nil {
		t.Fatal(err)
	}
	var _ CreationReceiptReader = reader
	ctx := context.Background()
	reported := cometHash(chain.block)
	if reported == chain.block.Hash() {
		t.Fatal("fixture must report a hash different from Header.Hash()")
	}
	header, hash, err := reader.ReportedHeaderByNumber(ctx, chain.block.Number)
	if err != nil || hash != reported || header.Number.Cmp(chain.block.Number) != 0 {
		t.Fatalf("reported header by number: %v %s", err, hash)
	}
	block, err := reader.ReportedBlockByHash(ctx, reported)
	if err != nil {
		t.Fatal(err)
	}
	if block.Number().Cmp(chain.block.Number) != 0 || len(block.Transactions()) != 1 || block.Transactions()[0].Hash() != chain.tx.Hash() || block.ParentHash() != cometHash(chain.parent) {
		t.Fatalf("block by reported hash decoded wrongly: %+v", block.Header())
	}
	if _, err := reader.ReportedBlockByHash(ctx, chain.block.Hash()); err == nil {
		t.Fatal("recomputed header hash resolved a block")
	}
	if _, _, err := reader.ReportedHeaderByNumber(ctx, big.NewInt(5)); err == nil {
		t.Fatal("missing height resolved")
	}
}

// The pinned reader used by creation-fee reconciliation accepts only the
// node-reported hashes it was built for and reads state by their heights.
func TestReportedPinnedReaderReadsByHeight(t *testing.T) {
	chain := newCometChain(t)
	reader, err := NewPinnedRPCReader(chain.serve())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	blockHash, parentHash := cometHash(chain.block), cometHash(chain.parent)
	pinned := reportedPinnedReader{chain: reader, reads: reader, blocks: map[common.Hash]*types.Header{blockHash: chain.block, parentHash: chain.parent}}
	address := common.HexToAddress("0x2")
	if code, err := pinned.CodeAtHash(ctx, address, blockHash); err != nil || len(code) != 2 {
		t.Fatal("code at reported hash", code, err)
	}
	if _, err := pinned.StorageAtHash(ctx, address, common.Hash{}, parentHash); err != nil {
		t.Fatal("storage at reported parent hash", err)
	}
	if out, err := pinned.CallContractAtHash(ctx, ethereum.CallMsg{To: &address}, blockHash); err != nil || len(out) != 32 {
		t.Fatal("call at reported hash", out, err)
	}
	want := []string{hexutil.EncodeBig(chain.block.Number), hexutil.EncodeBig(chain.parent.Number), hexutil.EncodeBig(chain.block.Number)}
	if strings.Join(chain.heights, ",") != strings.Join(want, ",") {
		t.Fatalf("reads addressed %v, want %v", chain.heights, want)
	}
	if _, err := pinned.CodeAtHash(ctx, address, chain.block.Hash()); err == nil {
		t.Fatal("recomputed header hash accepted by the pinned reader")
	}
	if h, err := pinned.HeaderByReportedHash(ctx, blockHash); err != nil || h.Number.Cmp(chain.block.Number) != 0 {
		t.Fatal("pinned header", err)
	}
}

// TestLivePaxeerCreationBlockIdentity reads the live pool-creation block
// (33963716) on the chain-125 archive RPC named by PAXEER_ARCHIVE_RPC_URL
// (e.g. https://api2.mainnet-beta.paxeer.network) the way creation-fee
// reconciliation does.
func TestLivePaxeerCreationBlockIdentity(t *testing.T) {
	endpoint := os.Getenv("PAXEER_ARCHIVE_RPC_URL")
	if endpoint == "" {
		t.Skip("PAXEER_ARCHIVE_RPC_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader, err := NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	height := big.NewInt(33963716)
	reported := common.HexToHash("0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b")
	recomputed := common.HexToHash("0xe83bd786ee2456fe683b1cc6a7c4b9755783d09071d189ec20dc5a565c6f6292")
	creation := common.HexToHash("0xe200486fb275ad363101dd09455b23842f1a002b839062732230fd4c908cbfca")
	pool := common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	header, hash, err := reader.ReportedHeaderByNumber(ctx, height)
	if err != nil || hash != reported || header.Hash() != recomputed {
		t.Fatalf("creation block identity: %v reported %s recomputed %s", err, hash, header.Hash())
	}
	block, err := reader.ReportedBlockByHash(ctx, reported)
	if err != nil || len(block.Transactions()) != 1 || block.Transactions()[0].Hash() != creation {
		t.Fatalf("creation block by reported hash: %v", err)
	}
	if block.Hash() == reported {
		t.Fatal("expected recomputed block hash to differ on chain 125")
	}
	receipt, err := reader.TransactionReceipt(ctx, creation)
	if err != nil || receipt.BlockHash != reported || receipt.TransactionIndex != 0 {
		t.Fatalf("receipt identity: %v %s", err, receipt.BlockHash)
	}
	parent, err := reader.HeaderByReportedHash(ctx, block.ParentHash())
	if err != nil || parent.Number.Uint64() != height.Uint64()-1 {
		t.Fatalf("parent hash is not the node-reported parent identity: %v", err)
	}
	if _, parentHash, err := reader.ReportedHeaderByNumber(ctx, parent.Number); err != nil || parentHash != block.ParentHash() {
		t.Fatalf("parent canonical identity: %v %s", err, parentHash)
	}
	if _, err := reader.CodeAtHash(ctx, pool, recomputed); err == nil {
		t.Fatal("node served EIP-1898 by the recomputed header hash")
	}
	pinned := reportedPinnedReader{chain: reader, reads: reader, blocks: map[common.Hash]*types.Header{reported: block.Header(), block.ParentHash(): parent}}
	code, err := pinned.CodeAtHash(ctx, pool, reported)
	if err != nil || len(code) == 0 {
		t.Fatalf("pool code at the creation block: %v", err)
	}
	if before, err := pinned.CodeAtHash(ctx, pool, block.ParentHash()); err != nil || len(before) != 0 {
		t.Fatalf("pool must not exist at the parent block: %d bytes %v", len(before), err)
	}
	t.Logf("block %s reported %s recomputed %s; creation tx %s; pool code %d bytes", height, reported.Hex(), block.Hash().Hex(), creation.Hex(), len(code))
}
