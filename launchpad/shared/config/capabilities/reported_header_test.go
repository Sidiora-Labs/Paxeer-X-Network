package capabilities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

// A Paxeer-style block: the node reports a consensus hash that is not the
// keccak of the RLP header, so go-ethereum's Header.Hash() can never match it.
var cometBlockJSON = `{"number":"0x1ebb6a7","parentHash":"0x1111111111111111111111111111111111111111111111111111111111111111","sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347","miner":"0x0000000000000000000000000000000000000000","stateRoot":"0x2222222222222222222222222222222222222222222222222222222222222222","transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","receiptsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","logsBloom":"0x` + strings.Repeat("00", 256) + `","difficulty":"0x0","gasLimit":"0x1c9c380","gasUsed":"0x0","timestamp":"0x68ec1b50","extraData":"0x","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","nonce":"0x0000000000000000","baseFeePerGas":"0x7","hash":"%s","transactions":[],"uncles":[]}`

func newBlockByHashServer(t *testing.T, serve func(requested common.Hash) string) *rpc.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &call); err != nil || call.Method != "eth_getBlockByHash" || len(call.Params) != 2 {
			http.Error(w, "unexpected call", http.StatusBadRequest)
			return
		}
		var requested common.Hash
		if err := json.Unmarshal(call.Params[0], &requested); err != nil {
			http.Error(w, "bad hash", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(call.ID)+`,"result":`+serve(requested)+`}`)
	}))
	t.Cleanup(srv.Close)
	client, err := rpc.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestHeaderByReportedHashTrustsNodeIdentityNotRecomputedHash(t *testing.T) {
	consensus := common.HexToHash("0xabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd")
	client := newBlockByHashServer(t, func(requested common.Hash) string {
		if requested != consensus {
			return "null"
		}
		return strings.Replace(cometBlockJSON, "%s", consensus.Hex(), 1)
	})
	reader, err := NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	var _ deployment.ReportedHeaderReader = reader

	h, err := reader.HeaderByReportedHash(context.Background(), consensus)
	if err != nil {
		t.Fatal(err)
	}
	if h.Number == nil || h.Number.Uint64() != 0x1ebb6a7 || h.Time != 0x68ec1b50 {
		t.Fatalf("header fields not decoded: %+v", h)
	}
	if h.Hash() == consensus {
		t.Fatal("fixture must model a consensus hash that differs from the recomputed header hash")
	}
	if _, err := reader.HeaderByReportedHash(context.Background(), common.HexToHash("0x01")); err == nil {
		t.Fatal("unknown block must not resolve")
	}
	if _, err := reader.HeaderByReportedHash(context.Background(), common.Hash{}); err == nil {
		t.Fatal("zero hash must be rejected before any RPC")
	}
}

func TestHeaderByReportedHashRejectsNodeReturningDifferentHash(t *testing.T) {
	requested := common.HexToHash("0xabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd")
	other := common.HexToHash("0xfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeed")
	client := newBlockByHashServer(t, func(common.Hash) string {
		return strings.Replace(cometBlockJSON, "%s", other.Hex(), 1)
	})
	reader, err := NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.HeaderByReportedHash(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "block_identity_unverified") {
		t.Fatalf("mismatched node hash accepted: %v", err)
	}
}
