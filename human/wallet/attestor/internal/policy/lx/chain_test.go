package lx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
)

type nonceServer struct {
	*httptest.Server

	mu     sync.Mutex
	nonce  uint64
	calls  int
	asked  []common.Address
	failed string
}

func (s *nonceServer) setNonce(nonce uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonce = nonce
}

func (s *nonceServer) seen() (int, []common.Address, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]common.Address{}, s.asked...), s.failed
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *rpcError) {
	body := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		body["error"] = rpcErr
	} else {
		body["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func newNonceServer(t *testing.T, nonce uint64) *nonceServer {
	t.Helper()
	contract, err := evm.PrecompileABI("addr")
	if err != nil {
		t.Fatal(err)
	}
	method := contract.Methods["layerXBindNonce"]
	to, _ := evm.PrecompileAddress("addr")
	s := &nonceServer{nonce: nonce}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		fail := func(reason string) {
			s.failed = reason
			writeRPC(w, req.ID, nil, &rpcError{Code: -32602, Message: reason})
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			fail("not a json post")
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fail("body is not json")
			return
		}
		if req.JSONRPC != "2.0" || req.Method != "eth_call" || len(req.Params) != 2 {
			fail("not an eth_call")
			return
		}
		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		var block string
		if json.Unmarshal(req.Params[0], &call) != nil || json.Unmarshal(req.Params[1], &block) != nil || block != "latest" {
			fail("malformed call object")
			return
		}
		if !strings.EqualFold(call.To, to.Hex()) {
			fail("call is not addressed to the addr precompile")
			return
		}
		data, err := hexutil.Decode(call.Data)
		if err != nil || len(data) < 4 || !bytes.Equal(data[:4], method.ID) {
			fail("call is not layerXBindNonce")
			return
		}
		args, err := method.Inputs.Unpack(data[4:])
		if err != nil || len(args) != 1 {
			fail("layerXBindNonce arguments are malformed")
			return
		}
		address, ok := args[0].(common.Address)
		if !ok {
			fail("layerXBindNonce argument is not an address")
			return
		}
		s.asked = append(s.asked, address)
		out, err := method.Outputs.Pack(s.nonce)
		if err != nil {
			fail(err.Error())
			return
		}
		writeRPC(w, req.ID, hexutil.Encode(out), nil)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestChainReadsBindNonce(t *testing.T) {
	server := newNonceServer(t, 42)
	chain, err := NewChain(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	nonce, err := chain.LayerXBindNonce(context.Background(), address)
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 42 {
		t.Fatalf("nonce %d, want 42", nonce)
	}
	server.setNonce(1 << 40)
	if nonce, err = chain.LayerXBindNonce(context.Background(), address); err != nil || nonce != 1<<40 {
		t.Fatalf("nonce %d (%v), want %d", nonce, err, uint64(1<<40))
	}
	calls, asked, failed := server.seen()
	if calls != 2 || failed != "" || len(asked) != 2 || asked[0] != address || asked[1] != address {
		t.Fatalf("server saw %d calls for %v (%q)", calls, asked, failed)
	}
}

func TestChainRefusesFailedReads(t *testing.T) {
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	rpcFailure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRPC(w, json.RawMessage("1"), nil, &rpcError{Code: -32000, Message: "execution reverted"})
	}))
	defer rpcFailure.Close()
	statusFailure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer statusFailure.Close()
	shortResult := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRPC(w, json.RawMessage("1"), "0x01", nil)
	}))
	defer shortResult.Close()
	wrongID := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRPC(w, json.RawMessage("7"), hexutil.Encode(make([]byte, 32)), nil)
	}))
	defer wrongID.Close()
	for name, server := range map[string]*httptest.Server{"rpc error": rpcFailure, "status": statusFailure, "short result": shortResult, "wrong id": wrongID} {
		chain, err := NewChain(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := chain.LayerXBindNonce(context.Background(), address); !errors.Is(err, ErrChain) {
			t.Fatalf("%s: read returned %v", name, err)
		}
	}

	server := newNonceServer(t, 1)
	for name, raw := range map[string]string{"empty": "", "scheme": "ftp://127.0.0.1:1", "no host": "http://"} {
		if _, err := NewChain(raw, server.Client()); err == nil || !strings.Contains(err.Error(), config.EnvRPCURL) {
			t.Fatalf("%s: url %q accepted (%v)", name, raw, err)
		}
	}
	if _, err := NewChain(server.URL, nil); err == nil {
		t.Fatal("chain reader built without a client")
	}
	if _, err := NewChainFromConfig(nil); err == nil {
		t.Fatal("chain reader built without a configuration")
	}
	fromConfig, err := NewChainFromConfig(&config.Config{RPCURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if nonce, err := fromConfig.LayerXBindNonce(context.Background(), address); err != nil || nonce != 1 {
		t.Fatalf("configured reader returned %d (%v)", nonce, err)
	}
}
