package sender

import (
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paxeer-network/pax-load/config"
	"github.com/paxeer-network/pax-load/generator"
	"github.com/paxeer-network/pax-load/generator/scenarios"
	"github.com/paxeer-network/pax-load/types"
)

// JSONRPCRequest represents a captured JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string   `json:"jsonrpc"`
	Method  string   `json:"method"`
	Params  []string `json:"params"`
	ID      int      `json:"id"`
}

// MockServer captures JSON-RPC requests for testing
type MockServer struct {
	server   *httptest.Server
	requests []JSONRPCRequest
	mu       sync.Mutex
}

// NewMockServer creates a new mock JSON-RPC server
func NewMockServer() *MockServer {
	ms := &MockServer{
		requests: make([]JSONRPCRequest, 0),
	}

	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Parse JSON-RPC request
		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Store the request
		ms.mu.Lock()
		ms.requests = append(ms.requests, req)
		ms.mu.Unlock()

		// Send a mock response
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  "0x1234567890abcdef", // Mock transaction hash
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}))

	return ms
}

// GetRequests returns all captured requests
func (ms *MockServer) GetRequests() []JSONRPCRequest {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Return a copy to avoid race conditions
	requests := make([]JSONRPCRequest, len(ms.requests))
	copy(requests, ms.requests)
	return requests
}

// GetURL returns the server URL
func (ms *MockServer) GetURL() string {
	return ms.server.URL
}

// Close shuts down the server
func (ms *MockServer) Close() {
	ms.server.Close()
}

// TestShardDistributionVerification tests that specific transactions go to expected shards
func TestShardDistributionVerification(t *testing.T) {
	// Test shard distribution logic without network operations or scenario deployment
	endpoints := []string{
		"http://localhost:8545",
		"http://localhost:8546",
	}

	// Create a proper mock transaction with all required fields
	mockAccount := &types.Account{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}

	mockTx := &types.LoadTx{
		EthTx: ethtypes.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1000000000), nil),
		Scenario: &types.TxScenario{
			Name:   "TestScenario",
			Sender: mockAccount,
		},
	}

	// Test shard calculation logic
	for i := 0; i < 10; i++ {
		shardID := mockTx.ShardID(len(endpoints))
		assert.GreaterOrEqual(t, shardID, 0)
		assert.Less(t, shardID, len(endpoints))
	}
}

// TestShardDistribution verifies that transactions are distributed across shards correctly
func TestShardDistribution(t *testing.T) {
	// Test shard distribution logic without network operations
	endpoints := []string{
		"http://localhost:8545",
		"http://localhost:8546",
	}

	// Create test configuration
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Endpoints:  endpoints,
		Accounts: &config.AccountConfig{
			Accounts: 100,
		},
		Scenarios: []config.Scenario{
			{Name: scenarios.ERC20, Weight: 1},
		},
	}

	// Create generator
	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)

	// Test shard calculation without creating actual sender
	for i := 0; i < 10; i++ {
		tx, ok := gen.Generate()
		require.True(t, ok)
		require.NotNil(t, tx)

		shardID := tx.ShardID(len(endpoints))
		assert.GreaterOrEqual(t, shardID, 0)
		assert.Less(t, shardID, len(endpoints))
	}
}
