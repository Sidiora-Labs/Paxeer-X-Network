package chainreader_test

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/chainreader"
)

const registry = "0x00000000000000000000000000000000000000a1"

// nftRPC is a mock node: it answers eth_chainId (needed on dial) and eth_call.
// callResult is the 0x-hex returned for eth_call; callErr (if set) is returned
// as a JSON-RPC error instead.
func nftRPC(t *testing.T, callResult string, callErr bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(s, "eth_chainId"):
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x7d"}`)
		case strings.Contains(s, "eth_call"):
			if callErr {
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`)
				return
			}
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": callResult})
			_, _ = w.Write(out)
		default:
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNftIDByPool(t *testing.T) {
	t.Parallel()
	// getNftIdByPool returns uint256(42), abi-encoded as a single 32-byte word.
	srv := nftRPC(t, common.BigToHash(big.NewInt(42)).Hex(), false)
	cl, err := chain.NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("chain client: %v", err)
	}
	defer cl.Close()

	r, err := chainreader.NewNFTReader(cl, registry)
	if err != nil {
		t.Fatalf("NewNFTReader: %v", err)
	}
	got, err := r.NftIDByPool(context.Background(), common.HexToAddress("0xpool"))
	if err != nil {
		t.Fatalf("NftIDByPool: %v", err)
	}
	if got == nil || got.Int64() != 42 {
		t.Errorf("nftId = %v, want 42", got)
	}
}

func TestNftIDByPoolRevert(t *testing.T) {
	t.Parallel()
	srv := nftRPC(t, "", true) // eth_call reverts
	cl, err := chain.NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("chain client: %v", err)
	}
	defer cl.Close()

	r, err := chainreader.NewNFTReader(cl, registry)
	if err != nil {
		t.Fatalf("NewNFTReader: %v", err)
	}
	if _, err := r.NftIDByPool(context.Background(), common.HexToAddress("0xpool")); err == nil {
		t.Error("NftIDByPool should error when the call reverts")
	}
}
