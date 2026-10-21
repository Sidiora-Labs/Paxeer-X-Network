package source

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
)

func swapTopic(t *testing.T) common.Hash {
	t.Helper()
	for _, d := range protocol.Events() {
		if d.Name == "Swap" {
			return d.Topic0
		}
	}
	t.Fatal("Swap event missing from registry")
	return common.Hash{}
}

type rpcReq struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	ID     int64             `json:"id"`
}

// jsonRPC starts a mock EVM node. head feeds eth_blockNumber; logs feeds
// eth_getLogs; blocks feeds eth_getBlockByNumber (keyed by block number).
func jsonRPC(t *testing.T, head int64, logs []map[string]any, blocks map[uint64]map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		_ = json.Unmarshal(body, &req)
		var result any
		switch req.Method {
		case "eth_blockNumber":
			result = fmt.Sprintf("0x%x", head)
		case "eth_getLogs":
			result = logs
		case "eth_getBlockByNumber":
			var hexNum string
			_ = json.Unmarshal(req.Params[0], &hexNum)
			bn, _ := parseHexUint64(hexNum)
			result = blocks[bn]
		default:
			result = nil
		}
		w.Header().Set("Content-Type", "application/json")
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func failingRPC(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func logObj(addr common.Address, topics []common.Hash, data []byte, block uint64, tx common.Hash, idx uint) map[string]any {
	ts := make([]string, len(topics))
	for i, tp := range topics {
		ts[i] = tp.Hex()
	}
	return map[string]any{
		"address":         addr.Hex(),
		"topics":          ts,
		"data":            "0x" + hex.EncodeToString(data),
		"blockNumber":     fmt.Sprintf("0x%x", block),
		"transactionHash": tx.Hex(),
		"logIndex":        fmt.Sprintf("0x%x", idx),
		"removed":         false,
	}
}

func blockObj(ts int64, txs ...[2]string) map[string]any {
	list := make([]map[string]any, 0, len(txs))
	for _, tx := range txs {
		list = append(list, map[string]any{"hash": tx[0], "from": tx[1]})
	}
	return map[string]any{"timestamp": fmt.Sprintf("0x%x", ts), "transactions": list}
}

func TestParseHexHelpers(t *testing.T) {
	t.Parallel()
	if v, err := parseHexUint64("0x1a4"); err != nil || v != 420 {
		t.Errorf("parseHexUint64 = %d, err %v", v, err)
	}
	if v, err := parseHexInt64("0X7d"); err != nil || v != 125 {
		t.Errorf("parseHexInt64 = %d, err %v", v, err)
	}
	if _, err := parseHexUint64("0xZZ"); err == nil {
		t.Error("parseHexUint64 bad hex should error")
	}
	if _, err := parseHexInt64("nothex"); err == nil {
		t.Error("parseHexInt64 bad hex should error")
	}
}

func TestEVMHeadAndFetchLogs(t *testing.T) {
	t.Parallel()
	swap := swapTopic(t)
	other := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	emitter := common.HexToAddress("0x00000000000000000000000000000000000000ee")

	swapTx := common.HexToHash("0xaaa1")
	otherTx := common.HexToHash("0xbbb2")
	from1 := "0x00000000000000000000000000000000000000f1"
	from2 := "0x00000000000000000000000000000000000000f2"

	logs := []map[string]any{
		// out-of-order to prove sorting by (block, logIndex)
		logObj(emitter, []common.Hash{other}, nil, 16, otherTx, 1),
		logObj(emitter, []common.Hash{swap}, nil, 16, swapTx, 0),
	}
	blocks := map[uint64]map[string]any{
		16: blockObj(1700000000, [2]string{swapTx.Hex(), from1}, [2]string{otherTx.Hex(), from2}),
	}
	srv := jsonRPC(t, 100, logs, blocks)

	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}, Name: "rpc"})
	if err != nil {
		t.Fatalf("NewEVM: %v", err)
	}
	defer s.Close()
	if s.Name() != "rpc" {
		t.Errorf("Name = %q", s.Name())
	}

	head, err := s.Head(context.Background())
	if err != nil || head != 100 {
		t.Fatalf("Head = %d, err %v", head, err)
	}

	res, err := s.FetchLogs(context.Background(), FetchOptions{
		MonitoredAddresses: []string{emitter.Hex()}, FromBlock: 16, ToBlock: 16,
	})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(res.Logs) != 2 || res.Logs[0].Index != 0 || res.Logs[1].Index != 1 {
		t.Fatalf("logs not sorted by index: %+v", res.Logs)
	}
	if res.BlockTimestamps[16] != 1700000000 {
		t.Errorf("timestamp = %d", res.BlockTimestamps[16])
	}
	// Only the Swap-topic tx needs tx-from; the other-topic tx is filtered out.
	if len(res.TxFromMap) != 1 {
		t.Fatalf("TxFromMap = %v, want exactly the swap tx", res.TxFromMap)
	}
	if got := res.TxFromMap[swapTx.Hex()]; got != from1 {
		t.Errorf("swap tx from = %q, want %q", got, from1)
	}
}

func TestEVMFetchLogsEmpty(t *testing.T) {
	t.Parallel()
	srv := jsonRPC(t, 50, []map[string]any{}, nil)
	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Name() != "rpc" {
		t.Errorf("single-URL Name = %q, want rpc", s.Name())
	}
	res, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 1, ToBlock: 10})
	if err != nil || len(res.Logs) != 0 {
		t.Fatalf("empty FetchLogs = %+v, err %v", res, err)
	}
}

func TestEVMMultiURLNameAndFailover(t *testing.T) {
	t.Parallel()
	primary := failingRPC(t)
	fallback := jsonRPC(t, 77, nil, nil)
	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{primary.URL, fallback.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Name() != "rpc-getlogs" {
		t.Errorf("multi-URL Name = %q, want rpc-getlogs", s.Name())
	}
	head, err := s.Head(context.Background())
	if err != nil || head != 77 {
		t.Fatalf("failover Head = %d, err %v", head, err)
	}
}

func TestEVMAllNodesFail(t *testing.T) {
	t.Parallel()
	srv := failingRPC(t)
	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Head(context.Background()); err == nil {
		t.Error("Head should error when all nodes fail")
	}
	if _, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 1, ToBlock: 2}); err == nil {
		t.Error("FetchLogs should error when all nodes fail")
	}
}

// TestEVMStaleQuarantine puts the STALE node first in slot order: if quarantine
// works, Head() must skip it and return the fresh node's height.
func TestEVMStaleQuarantine(t *testing.T) {
	t.Parallel()
	stale := jsonRPC(t, 10, nil, nil)  // far behind
	fresh := jsonRPC(t, 100, nil, nil) // at head
	s, err := NewEVM(context.Background(), EVMOptions{
		RPCURLs:        []string{stale.URL, fresh.URL},
		StaleThreshold: 50,
		HealthInterval: time.Hour, // no background re-rank during the test
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	head, err := s.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != 100 {
		t.Fatalf("Head = %d, want 100 (stale node #0 must be quarantined)", head)
	}
}

func TestNewEVMRequiresURL(t *testing.T) {
	t.Parallel()
	if _, err := NewEVM(context.Background(), EVMOptions{}); err == nil {
		t.Error("NewEVM with no URLs should error")
	}
}

func TestEVMFetchLogsBlockNull(t *testing.T) {
	t.Parallel()
	emitter := common.HexToAddress("0x00000000000000000000000000000000000000ee")
	logs := []map[string]any{logObj(emitter, []common.Hash{swapTopic(t)}, nil, 16, common.HexToHash("0xa1"), 0)}
	// blocks map empty -> eth_getBlockByNumber returns null -> getBlock errors.
	srv := jsonRPC(t, 100, logs, map[uint64]map[string]any{})
	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 16, ToBlock: 16}); err == nil {
		t.Error("FetchLogs should error when a block returns null")
	}
}

func TestEVMFetchLogsBadTimestamp(t *testing.T) {
	t.Parallel()
	emitter := common.HexToAddress("0x00000000000000000000000000000000000000ee")
	logs := []map[string]any{logObj(emitter, []common.Hash{swapTopic(t)}, nil, 16, common.HexToHash("0xa1"), 0)}
	blocks := map[uint64]map[string]any{16: {"timestamp": "notahex", "transactions": []map[string]any{}}}
	srv := jsonRPC(t, 100, logs, blocks)
	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 16, ToBlock: 16}); err == nil {
		t.Error("FetchLogs should error on un-parseable block timestamp")
	}
}

func TestEVMRPCErrorResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		out, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32000, "message": "execution reverted"},
		})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	s, err := NewEVM(context.Background(), EVMOptions{RPCURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Head(context.Background()); err == nil {
		t.Error("Head should propagate a JSON-RPC error object")
	}
}
