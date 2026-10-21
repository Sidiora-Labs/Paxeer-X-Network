package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/projectionstats"
)

// The bootstrap check compares the indexer's committed block hash with the
// hash the node reports for that height. A CometBFT-backed node (Paxeer)
// reports a hash that is not go-ethereum's recomputed Header.Hash().
func TestReportedBlockHashUsesNodeIdentity(t *testing.T) {
	header := &types.Header{Number: big.NewInt(33963716), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: 1, Extra: []byte{}}
	consensus := crypto.Keccak256Hash([]byte("cometbft"), header.Hash().Bytes())
	var misreport atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		var tag string
		json.Unmarshal(req.Params[0], &tag)
		var result any
		if req.Method == "eth_getBlockByNumber" && tag == hexutil.EncodeBig(header.Number) {
			encoded, _ := json.Marshal(header)
			var out map[string]any
			json.Unmarshal(encoded, &out)
			out["hash"] = consensus.Hex()
			if misreport.Load() {
				out["number"] = "0x1"
			}
			result = out
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	client, err := ethclient.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	got, err := reportedBlockHash(context.Background(), client, 33963716)
	if err != nil || got != consensus || got == header.Hash() {
		t.Fatalf("reported hash = %s %v, want %s (recomputed %s)", got, err, consensus, header.Hash())
	}
	if _, err := reportedBlockHash(context.Background(), client, 5); err == nil {
		t.Fatal("missing height resolved")
	}
	misreport.Store(true)
	if _, err := reportedBlockHash(context.Background(), client, 33963716); !errors.Is(err, projectionstats.ErrIntegrity) {
		t.Fatal("height mismatch accepted", err)
	}
}
